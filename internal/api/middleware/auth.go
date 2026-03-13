package middleware

import (
	"context"
	"net/http"
	"strings"

	"wg-platform-handoff/internal/integrations/compat"
	"wg-platform-handoff/internal/integrations/supabase"
)

type contextKey string

const (
	ContextAccountID  contextKey = "account_id"
	ContextAuthUserID contextKey = "auth_user_id"
	ContextAccountNum contextKey = "account_number"
)

func BearerAuth(verifier *supabase.Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			writeAuthError(w, "INVALID_ACCESS_TOKEN")
			return
		}

		token := strings.TrimSpace(auth[7:])
		claims, err := verifier.Verify(token)
		if err != nil {
			writeAuthError(w, "INVALID_ACCESS_TOKEN")
			return
		}

		ctx := context.WithValue(r.Context(), ContextAuthUserID, claims.Subject)
		if claims.AccountID != "" {
			ctx = context.WithValue(ctx, ContextAccountID, claims.AccountID)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func GatewayAuth(expectedToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expectedToken == "" {
			// Dev-only behavior: allow all internal traffic if no token configured.
			next.ServeHTTP(w, r)
			return
		}

		token := r.Header.Get("X-Gateway-Token")
		if token == "" || token != expectedToken {
			writeAuthError(w, "UNAUTHORIZED_GATEWAY")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func CompatibilityBearerAuth(supabaseVerifier *supabase.Verifier, compatVerifier *compat.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			writeAuthError(w, "INVALID_ACCESS_TOKEN")
			return
		}

		token := strings.TrimSpace(auth[7:])
		if token == "" {
			writeAuthError(w, "INVALID_ACCESS_TOKEN")
			return
		}

		if supabaseVerifier != nil {
			claims, err := supabaseVerifier.Verify(token)
			if err == nil {
				ctx := context.WithValue(r.Context(), ContextAuthUserID, claims.Subject)
				if claims.AccountID != "" {
					ctx = context.WithValue(ctx, ContextAccountID, claims.AccountID)
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		if compatVerifier != nil {
			claims, err := compatVerifier.Verify(token)
			if err == nil {
				ctx := context.WithValue(r.Context(), ContextAccountNum, claims.AccountNumber)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		writeAuthError(w, "INVALID_ACCESS_TOKEN")
	})
}

func SubjectFromContext(ctx context.Context) string {
	subject, _ := ctx.Value(ContextAuthUserID).(string)
	return strings.TrimSpace(subject)
}

func AccountIDFromContext(ctx context.Context) string {
	accountID, _ := ctx.Value(ContextAccountID).(string)
	return strings.TrimSpace(accountID)
}

func AccountNumberFromContext(ctx context.Context) string {
	accountNumber, _ := ctx.Value(ContextAccountNum).(string)
	return strings.TrimSpace(accountNumber)
}

func writeAuthError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"type":"` + code + `"}`))
}
