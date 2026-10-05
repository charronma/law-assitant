package model

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	goopenai "github.com/meguminnnnnnnnn/go-openai"
)

// Machine-readable error codes returned to the frontend.
const (
	CodeQuotaExhausted = "QUOTA_EXHAUSTED"
	CodeInvalidAPIKey  = "INVALID_API_KEY"
	CodeRateLimited    = "RATE_LIMITED"
	CodeUpstreamError  = "UPSTREAM_ERROR"
	CodeInvalidModel   = "INVALID_MODEL"
)

// APIError is the client-facing form of a model failure. It deliberately
// carries only fixed, human-written messages: never the raw upstream response
// and never anything derived from credentials.
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Model   string `json:"model,omitempty"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

var statusInText = regexp.MustCompile(`status code:?\s*(\d{3})`)

// Classify turns an error from the model layer into the response to send to
// the client. modelID is the model the request used. It returns nil for a nil
// error. Classification is a pure function of the error value.
func Classify(err error, modelID string) *APIError {
	if err == nil {
		return nil
	}
	status, text := upstreamDetails(err)
	lower := strings.ToLower(text)

	switch {
	case status == http.StatusUnauthorized || strings.Contains(lower, "incorrect api key"):
		return &APIError{
			Status:  http.StatusBadGateway,
			Code:    CodeInvalidAPIKey,
			Message: "服务端 API Key 配置错误，请联系管理员",
		}
	case isQuotaExhausted(status, lower):
		return &APIError{
			Status:  http.StatusPaymentRequired,
			Code:    CodeQuotaExhausted,
			Model:   modelID,
			Message: "当前模型免费额度已用完",
		}
	case status == http.StatusTooManyRequests:
		return &APIError{
			Status:  http.StatusTooManyRequests,
			Code:    CodeRateLimited,
			Model:   modelID,
			Message: "请求太频繁，请稍后再试",
		}
	default:
		return &APIError{
			Status:  http.StatusInternalServerError,
			Code:    CodeUpstreamError,
			Model:   modelID,
			Message: "模型服务暂时不可用，请稍后重试",
		}
	}
}

// isQuotaExhausted recognises DashScope's "free tier used up" failure. The
// specific markers are unambiguous on their own; the bare word "quota" is only
// trusted on a 403 (a 429 mentioning quota is a rate limit, not exhaustion).
func isQuotaExhausted(status int, lower string) bool {
	if strings.Contains(lower, "allocationquota.freetieronly") ||
		strings.Contains(lower, "free quota exhausted") {
		return true
	}
	return status == http.StatusForbidden && strings.Contains(lower, "quota")
}

// upstreamDetails extracts the HTTP status and every piece of text that might
// name the failure. It reads the structured errors first (their Error() string
// omits the provider's error code, which is where DashScope puts
// "AllocationQuota.FreeTierOnly"), then falls back to the error string.
//
// Two different types carry the same HTTP failure depending on where it
// happens, and both must be handled:
//   - the eino-ext component converts errors from *opening* the stream into its
//     own *einoopenai.APIError (a distinct type with the same fields);
//   - errors raised while *reading* the stream keep go-openai's original types.
func upstreamDetails(err error) (status int, text string) {
	var b strings.Builder

	var compErr *einoopenai.APIError
	if errors.As(err, &compErr) && compErr != nil {
		status = compErr.HTTPStatusCode
		fmt.Fprintf(&b, "%v %s %s ", compErr.Code, compErr.Type, compErr.Message)
	}
	var apiErr *goopenai.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		if status == 0 {
			status = apiErr.HTTPStatusCode
		}
		fmt.Fprintf(&b, "%v %s %s ", apiErr.Code, apiErr.Type, apiErr.Message)
	}
	var reqErr *goopenai.RequestError
	if errors.As(err, &reqErr) && reqErr != nil {
		if status == 0 {
			status = reqErr.HTTPStatusCode
		}
		fmt.Fprintf(&b, "%s %v ", reqErr.Body, reqErr.Err)
	}

	msg := err.Error()
	if status == 0 {
		if m := statusInText.FindStringSubmatch(msg); m != nil {
			status, _ = strconv.Atoi(m[1])
		}
	}
	b.WriteString(msg)
	return status, b.String()
}
