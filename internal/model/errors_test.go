package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	goopenai "github.com/meguminnnnnnnnn/go-openai"
)

const freeTierMessage = `The free tier of the model has been exhausted. If you wish to continue access the model on a paid basis, please disable the "use free tier only" mode in the management console.`

func TestClassify(t *testing.T) {
	// What DashScope really returns when the free quota is used up: the marker
	// is in the structured Code field, NOT in Message or in Error().
	quotaAPIErr := &goopenai.APIError{
		Code:           "AllocationQuota.FreeTierOnly",
		Type:           "AllocationQuota.FreeTierOnly",
		Message:        freeTierMessage,
		HTTPStatus:     "403 Forbidden",
		HTTPStatusCode: http.StatusForbidden,
	}

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantModel  string // "" means the field must be absent
	}{
		{
			name:       "free tier exhausted: marker only in the Code field",
			err:        fmt.Errorf("failed to create chat completion: %w", quotaAPIErr),
			wantStatus: http.StatusPaymentRequired, wantCode: CodeQuotaExhausted, wantModel: "m1",
		},
		{
			name:       "free tier exhausted surfaced mid-stream",
			err:        fmt.Errorf("failed to receive stream chunk: %w", quotaAPIErr),
			wantStatus: http.StatusPaymentRequired, wantCode: CodeQuotaExhausted, wantModel: "m1",
		},
		{
			name: "eino-ext's own APIError type (errors from opening the stream)",
			err: &einoopenai.APIError{
				HTTPStatusCode: 403, HTTPStatus: "403 Forbidden",
				Code: "AllocationQuota.FreeTierOnly", Type: "AllocationQuota.FreeTierOnly", Message: freeTierMessage,
			},
			wantStatus: http.StatusPaymentRequired, wantCode: CodeQuotaExhausted, wantModel: "m1",
		},
		{
			name:       "eino-ext APIError: 401",
			err:        &einoopenai.APIError{HTTPStatusCode: 401, Message: "nope"},
			wantStatus: http.StatusBadGateway, wantCode: CodeInvalidAPIKey,
		},
		{
			name:       "eino-ext APIError: 429",
			err:        &einoopenai.APIError{HTTPStatusCode: 429, Message: "slow"},
			wantStatus: http.StatusTooManyRequests, wantCode: CodeRateLimited, wantModel: "m1",
		},
		{
			name: "403 RequestError with the marker only in the body",
			err: &goopenai.RequestError{
				HTTPStatusCode: 403, HTTPStatus: "403 Forbidden", Err: errors.New("forbidden"),
				Body: []byte(`{"error":{"code":"AllocationQuota.FreeTierOnly","message":"nope"}}`),
			},
			wantStatus: http.StatusPaymentRequired, wantCode: CodeQuotaExhausted, wantModel: "m1",
		},
		{
			name:       "plain text: Free quota exhausted (status parsed from text)",
			err:        errors.New("error, status code: 403, status: 403 Forbidden, message: Free quota exhausted"),
			wantStatus: http.StatusPaymentRequired, wantCode: CodeQuotaExhausted, wantModel: "m1",
		},
		{
			name:       "marker without any status is still quota exhaustion",
			err:        errors.New("AllocationQuota.FreeTierOnly"),
			wantStatus: http.StatusPaymentRequired, wantCode: CodeQuotaExhausted, wantModel: "m1",
		},
		{
			name:       "generic 'quota' wording (any case) on a 403",
			err:        &goopenai.APIError{HTTPStatusCode: 403, Message: "Your QUOTA has been used up"},
			wantStatus: http.StatusPaymentRequired, wantCode: CodeQuotaExhausted, wantModel: "m1",
		},
		{
			name:       "403 that is not about quota is an upstream error",
			err:        &goopenai.APIError{HTTPStatusCode: 403, Message: "Access denied"},
			wantStatus: http.StatusInternalServerError, wantCode: CodeUpstreamError, wantModel: "m1",
		},
		{
			name:       "401 is an invalid API key",
			err:        &goopenai.APIError{HTTPStatusCode: 401, Message: "bad credentials"},
			wantStatus: http.StatusBadGateway, wantCode: CodeInvalidAPIKey,
		},
		{
			name:       "Incorrect API key wording without a status",
			err:        errors.New("Incorrect API key provided: sk-****abcd"),
			wantStatus: http.StatusBadGateway, wantCode: CodeInvalidAPIKey,
		},
		{
			name:       "429 is rate limited",
			err:        fmt.Errorf("failed to create chat completion: %w", &goopenai.APIError{HTTPStatusCode: 429, Message: "slow down"}),
			wantStatus: http.StatusTooManyRequests, wantCode: CodeRateLimited, wantModel: "m1",
		},
		{
			name:       "429 that mentions quota is still a rate limit, not exhaustion",
			err:        &goopenai.APIError{HTTPStatusCode: 429, Code: "Throttling.AllocationQuota", Message: "Allocated quota exceeded"},
			wantStatus: http.StatusTooManyRequests, wantCode: CodeRateLimited, wantModel: "m1",
		},
		{
			name:       "500 from upstream",
			err:        &goopenai.APIError{HTTPStatusCode: 500, Message: "internal"},
			wantStatus: http.StatusInternalServerError, wantCode: CodeUpstreamError, wantModel: "m1",
		},
		{
			name:       "non-HTTP error (network, timeout, ...)",
			err:        errors.New("dial tcp: i/o timeout"),
			wantStatus: http.StatusInternalServerError, wantCode: CodeUpstreamError, wantModel: "m1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.err, "m1")
			if got == nil {
				t.Fatal("Classify returned nil")
			}
			if got.Status != tt.wantStatus || got.Code != tt.wantCode || got.Model != tt.wantModel {
				t.Fatalf("got status=%d code=%s model=%q, want status=%d code=%s model=%q",
					got.Status, got.Code, got.Model, tt.wantStatus, tt.wantCode, tt.wantModel)
			}
			if got.Message == "" {
				t.Error("message must not be empty")
			}
		})
	}
}

func TestClassifyNil(t *testing.T) {
	if got := Classify(nil, "m1"); got != nil {
		t.Fatalf("Classify(nil) = %+v, want nil", got)
	}
}

// The response sent to the client must never echo the upstream error, the
// response body, or any key that happened to appear in them.
func TestClassifyNeverLeaksUpstreamDetail(t *testing.T) {
	secret := "sk-supersecret-1234567890"
	errs := []error{
		&goopenai.APIError{HTTPStatusCode: 401, Message: "Incorrect API key provided: " + secret},
		&goopenai.APIError{HTTPStatusCode: 403, Code: "AllocationQuota.FreeTierOnly", Message: freeTierMessage + " " + secret},
		&goopenai.RequestError{HTTPStatusCode: 429, Err: errors.New("rl"), Body: []byte(`{"key":"` + secret + `"}`)},
		fmt.Errorf("upstream said: %s", secret),
	}
	for _, err := range errs {
		got := Classify(err, "m1")
		raw, mErr := json.Marshal(got)
		if mErr != nil {
			t.Fatal(mErr)
		}
		out := string(raw)
		for _, leak := range []string{secret, "supersecret", "free tier of the model", "disable the", "management console"} {
			if strings.Contains(out, leak) {
				t.Errorf("client response leaks %q: %s", leak, out)
			}
		}
	}
}

func TestClassifyJSONShape(t *testing.T) {
	raw, err := json.Marshal(Classify(&goopenai.APIError{HTTPStatusCode: 403, Code: "AllocationQuota.FreeTierOnly"}, "qwen3.8-max"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["code"] != "QUOTA_EXHAUSTED" || m["model"] != "qwen3.8-max" || m["message"] != "当前模型免费额度已用完" {
		t.Errorf("unexpected body: %s", raw)
	}
	if _, has := m["Status"]; has {
		t.Error("the HTTP status must not be part of the JSON body")
	}
	if len(m) != 3 {
		t.Errorf("want exactly code/model/message, got %s", raw)
	}
}

// The cases above hand-build errors; this one runs the REAL eino client against
// an HTTP server, so a dependency upgrade that changes which error type reaches
// us (this already bit once: the component re-wraps errors from opening the
// stream in its own type) fails here instead of in production.
func TestClassifyRealClient(t *testing.T) {
	const quotaBody = `{"error":{"code":"AllocationQuota.FreeTierOnly","type":"AllocationQuota.FreeTierOnly","param":null,` +
		`"message":"The free tier of the model has been exhausted. If you wish to continue access the model on a paid basis, please disable the \"use free tier only\" mode in the management console."}}`

	reject := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	// HTTP 200 whose first (or later) SSE chunk is an error object.
	sseThenError := func(tokensFirst bool) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if tokensFirst {
				fmt.Fprint(w, `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"部分"},"finish_reason":null}]}`+"\n\n")
			}
			fmt.Fprint(w, "data: "+quotaBody+"\n\n")
		}
	}

	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantCode string
		wantHTTP int
	}{
		{"HTTP 403 free tier exhausted (the production failure)", reject(403, quotaBody), CodeQuotaExhausted, 402},
		{"HTTP 401 incorrect key", reject(401, `{"error":{"code":"invalid_api_key","message":"Incorrect API key provided: sk-****abcd.","type":"invalid_request_error"}}`), CodeInvalidAPIKey, 502},
		{"HTTP 429 throttled", reject(429, `{"error":{"code":"Throttling.RateQuota","message":"Requests rate limit exceeded","type":"Throttling"}}`), CodeRateLimited, 429},
		{"HTTP 403 with a non-JSON body", reject(403, `<html>Forbidden: quota</html>`), CodeQuotaExhausted, 402},
		{"HTTP 500", reject(500, `{"error":{"message":"boom","type":"server_error"}}`), CodeUpstreamError, 500},
		{"quota error as the first SSE chunk", sseThenError(false), CodeQuotaExhausted, 402},
		{"quota error after some tokens", sseThenError(true), CodeQuotaExhausted, 402},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			reg, err := NewRegistry(srv.URL+"/v1", "sk-test", []string{"m"}, "m")
			if err != nil {
				t.Fatal(err)
			}
			m, err := reg.Get(context.Background(), "m")
			if err != nil {
				t.Fatal(err)
			}

			// Collect the failure wherever it surfaces: opening the stream or reading it.
			var got error
			sr, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("hi")})
			if err != nil {
				got = err
			} else {
				defer sr.Close()
				for got == nil {
					if _, rerr := sr.Recv(); rerr != nil {
						got = rerr
					}
				}
			}

			apiErr := Classify(got, "m")
			if apiErr.Code != tt.wantCode || apiErr.Status != tt.wantHTTP {
				t.Fatalf("Classify(%T: %v) = %s/%d, want %s/%d", got, got, apiErr.Code, apiErr.Status, tt.wantCode, tt.wantHTTP)
			}
			raw, _ := json.Marshal(apiErr)
			for _, leak := range []string{"free tier of the model", "sk-****abcd", "<html>", "management console", "boom"} {
				if strings.Contains(string(raw), leak) {
					t.Errorf("client response leaks %q: %s", leak, raw)
				}
			}
		})
	}
}
