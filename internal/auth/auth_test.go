package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testSecret = "super-secret-legacy-hs256-key-0123456789"
	testIssuer = "https://proj.supabase.co/auth/v1"
)

var testOpts = []jwt.ParserOption{
	jwt.WithValidMethods([]string{"HS256", "ES256", "RS256"}),
	jwt.WithAudience(supabaseAudience),
	jwt.WithExpirationRequired(),
	jwt.WithIssuer(testIssuer),
}

func claims(mod func(jwt.MapClaims)) jwt.MapClaims {
	c := jwt.MapClaims{
		"sub": "user-1",
		"aud": supabaseAudience,
		"iss": testIssuer,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	if mod != nil {
		mod(c)
	}
	return c
}

func signHS256(t *testing.T, secret string, c jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// do runs a request through the middleware and returns the status and the
// user ID the downstream handler saw ("" if it never ran).
func do(t *testing.T, a *Authenticator, authHeader string) (int, string) {
	t.Helper()
	var seen string
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = UserID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, seen
}

func TestNew_FailsClosedWhenUnconfigured(t *testing.T) {
	if _, err := New(context.Background(), Config{}); err == nil {
		t.Fatal("expected an error when neither SUPABASE_URL nor JWT secret is set")
	}
}

func TestDisabled_InjectsDevUser(t *testing.T) {
	a, err := New(context.Background(), Config{Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if code, uid := do(t, a, ""); code != http.StatusOK || uid != DevUserID {
		t.Fatalf("got %d %q, want 200 %q", code, uid, DevUserID)
	}
}

func TestHS256(t *testing.T) {
	a := newAuthenticator(selectKeyfunc(testSecret, nil), testOpts)

	tests := []struct {
		name   string
		header string
		want   int
		user   string
	}{
		{"valid", "Bearer " + signHS256(t, testSecret, claims(nil)), 200, "user-1"},
		{"scheme is case-insensitive", "bearer " + signHS256(t, testSecret, claims(nil)), 200, "user-1"},
		{"missing header", "", 401, ""},
		{"not a bearer scheme", "Basic abc", 401, ""},
		{"empty token", "Bearer ", 401, ""},
		{"garbage token", "Bearer not.a.jwt", 401, ""},
		{"wrong secret", "Bearer " + signHS256(t, "another-secret-another-secret-0000", claims(nil)), 401, ""},
		{"expired", "Bearer " + signHS256(t, testSecret, claims(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() })), 401, ""},
		{"no exp", "Bearer " + signHS256(t, testSecret, claims(func(c jwt.MapClaims) { delete(c, "exp") })), 401, ""},
		{"wrong audience (e.g. anon key)", "Bearer " + signHS256(t, testSecret, claims(func(c jwt.MapClaims) { c["aud"] = "anon" })), 401, ""},
		{"wrong issuer", "Bearer " + signHS256(t, testSecret, claims(func(c jwt.MapClaims) { c["iss"] = "https://evil.example/auth/v1" })), 401, ""},
		{"no subject", "Bearer " + signHS256(t, testSecret, claims(func(c jwt.MapClaims) { delete(c, "sub") })), 401, ""},
		{"empty subject", "Bearer " + signHS256(t, testSecret, claims(func(c jwt.MapClaims) { c["sub"] = "" })), 401, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, uid := do(t, a, tt.header)
			if code != tt.want || uid != tt.user {
				t.Fatalf("got %d %q, want %d %q", code, uid, tt.want, tt.user)
			}
		})
	}
}

func TestAlgNoneRejected(t *testing.T) {
	a := newAuthenticator(selectKeyfunc(testSecret, nil), testOpts)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims(nil)).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := do(t, a, "Bearer "+tok); code != http.StatusUnauthorized {
		t.Fatalf("alg=none accepted: got %d", code)
	}
}

func TestHS256RejectedWithoutSecret(t *testing.T) {
	// A JWKS-only deployment must not accept HS256 tokens (alg confusion).
	a := newAuthenticator(selectKeyfunc("", nil), testOpts)
	if code, _ := do(t, a, "Bearer "+signHS256(t, "", claims(nil))); code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", code)
	}
	if code, _ := do(t, a, "Bearer "+signHS256(t, testSecret, claims(nil))); code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", code)
	}
}

func jwksFor(t *testing.T, kid string, pub *ecdsa.PublicKey) keyfunc.Keyfunc {
	t.Helper()
	b64 := base64.RawURLEncoding.EncodeToString
	raw, err := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid,
		"x": b64(pub.X.FillBytes(make([]byte, 32))),
		"y": b64(pub.Y.FillBytes(make([]byte, 32))),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	kf, err := keyfunc.NewJWKSetJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return kf
}

func signES256(t *testing.T, kid string, key *ecdsa.PrivateKey, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, c)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestES256ViaJWKS(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a := newAuthenticator(selectKeyfunc("", jwksFor(t, "key-1", &key.PublicKey)), testOpts)

	if code, uid := do(t, a, "Bearer "+signES256(t, "key-1", key, claims(nil))); code != 200 || uid != "user-1" {
		t.Fatalf("valid ES256: got %d %q", code, uid)
	}
	if code, _ := do(t, a, "Bearer "+signES256(t, "key-1", other, claims(nil))); code != 401 {
		t.Fatalf("token signed by an unknown key: got %d, want 401", code)
	}
	if code, _ := do(t, a, "Bearer "+signES256(t, "unknown-kid", key, claims(nil))); code != 401 {
		t.Fatalf("unknown kid: got %d, want 401", code)
	}
	// Legacy secret must not unlock an asymmetric-only deployment's HS256 path.
	if code, _ := do(t, a, "Bearer "+signHS256(t, testSecret, claims(nil))); code != 401 {
		t.Fatalf("HS256 on JWKS-only deployment: got %d, want 401", code)
	}
}

func TestUnauthorizedResponseShape(t *testing.T) {
	a := newAuthenticator(selectKeyfunc(testSecret, nil), testOpts)
	h := a.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("handler must not run") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") == "" || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected response: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
}
