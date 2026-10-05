// Package auth verifies Supabase-issued JWTs and exposes the caller's user ID.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// DevUserID is the identity used when authentication is explicitly disabled.
const DevUserID = "dev-user"

// supabaseAudience is the `aud` claim Supabase sets on signed-in users' tokens.
const supabaseAudience = "authenticated"

type ctxKey struct{}
type tokenKey struct{}

// Config selects how tokens are verified.
//
// Supabase projects sign tokens either with asymmetric keys (published at
// <SupabaseURL>/auth/v1/.well-known/jwks.json) or, on legacy projects, with a
// shared HS256 secret. Set whichever applies; both may be set during a key
// rotation.
type Config struct {
	SupabaseURL string // e.g. https://xyzcompany.supabase.co
	JWTSecret   string // legacy HS256 shared secret
	Disabled    bool   // local development only: accept every request as DevUserID
}

// Authenticator validates bearer tokens and guards HTTP handlers.
type Authenticator struct {
	disabled bool
	parser   *jwt.Parser
	keyfunc  jwt.Keyfunc
}

// New builds an Authenticator. It fails when no verification method is
// configured so that a misconfigured deployment never runs unauthenticated;
// bypassing auth requires Config.Disabled to be set explicitly.
func New(ctx context.Context, cfg Config) (*Authenticator, error) {
	if cfg.Disabled {
		log.Printf("WARNING: authentication is DISABLED (AUTH_DISABLED=true); every request runs as %q. Never use this in production.", DevUserID)
		return &Authenticator{disabled: true}, nil
	}

	supabaseURL := strings.TrimRight(cfg.SupabaseURL, "/")
	if supabaseURL == "" && cfg.JWTSecret == "" {
		return nil, errors.New("authentication is not configured: set SUPABASE_URL (and/or SUPABASE_JWT_SECRET), or AUTH_DISABLED=true for local development")
	}

	var jwks keyfunc.Keyfunc
	if supabaseURL != "" {
		var err error
		jwks, err = keyfunc.NewDefaultCtx(ctx, []string{supabaseURL + "/auth/v1/.well-known/jwks.json"})
		if err != nil {
			return nil, fmt.Errorf("failed to load Supabase JWKS: %w", err)
		}
	}

	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"HS256", "ES256", "RS256"}),
		jwt.WithAudience(supabaseAudience),
		jwt.WithExpirationRequired(),
	}
	if supabaseURL != "" {
		opts = append(opts, jwt.WithIssuer(supabaseURL+"/auth/v1"))
	}

	return newAuthenticator(selectKeyfunc(cfg.JWTSecret, jwks), opts), nil
}

func newAuthenticator(kf jwt.Keyfunc, opts []jwt.ParserOption) *Authenticator {
	return &Authenticator{parser: jwt.NewParser(opts...), keyfunc: kf}
}

// selectKeyfunc picks the verification key by signing algorithm, so an
// HS256 token can only ever be checked against the shared secret and an
// asymmetric token only against the published JWKS (no algorithm confusion).
func selectKeyfunc(secret string, jwks keyfunc.Keyfunc) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); ok {
			if secret == "" {
				return nil, errors.New("HS256 tokens are not accepted: SUPABASE_JWT_SECRET is not set")
			}
			return []byte(secret), nil
		}
		if jwks == nil {
			return nil, errors.New("asymmetric tokens are not accepted: SUPABASE_URL is not set")
		}
		return jwks.Keyfunc(token)
	}
}

// Middleware rejects requests without a valid bearer token and stores the
// authenticated user ID on the request context.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.disabled {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, DevUserID)))
			return
		}

		userID, rawToken, err := a.verify(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="law-assistant"`)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, userID)
		ctx = context.WithValue(ctx, tokenKey{}, rawToken)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// verify validates the request's bearer token and returns the user ID (the
// `sub` claim) together with the raw, already-verified token.
func (a *Authenticator) verify(r *http.Request) (userID, rawToken string, err error) {
	scheme, raw, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	raw = strings.TrimSpace(raw)
	if !ok || !strings.EqualFold(scheme, "Bearer") || raw == "" {
		return "", "", errors.New("missing bearer token")
	}

	token, err := a.parser.Parse(raw, a.keyfunc)
	if err != nil {
		return "", "", err
	}
	sub, err := token.Claims.GetSubject()
	if err != nil || sub == "" {
		return "", "", errors.New("token has no subject")
	}
	return sub, raw, nil
}

// Token returns the caller's verified access token, so it can be forwarded to
// Supabase and have Row Level Security enforced as that user. It is absent when
// authentication is disabled.
func Token(ctx context.Context) (string, bool) {
	tok, ok := ctx.Value(tokenKey{}).(string)
	return tok, ok && tok != ""
}

// UserID returns the authenticated user's ID from the request context.
func UserID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok && id != ""
}
