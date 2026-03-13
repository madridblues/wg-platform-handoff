package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"wg-platform-handoff/internal/api/middleware"
	"wg-platform-handoff/internal/config"
	"wg-platform-handoff/internal/integrations/compat"
	"wg-platform-handoff/internal/integrations/mem0"
	"wg-platform-handoff/internal/integrations/paddle"
	"wg-platform-handoff/internal/integrations/supabase"
	"wg-platform-handoff/internal/store"
	"wg-platform-handoff/internal/store/postgres"
)

func NewRouter(cfg config.Config) (http.Handler, error) {
	storeImpl, err := postgres.New(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	return NewRouterWithDeps(
		cfg,
		storeImpl,
		supabase.NewVerifier(cfg.SupabaseJWTSecret),
		compat.NewManager(cfg.CompatTokenSecret, cfg.CompatTokenTTL),
		paddle.NewVerifier(cfg.PaddleWebhookSecret),
		mem0.NewClient(cfg.Mem0APIKey),
	), nil
}

func NewRouterWithDeps(
	cfg config.Config,
	storeImpl store.Store,
	supabaseVerifier *supabase.Verifier,
	compatTokenManager *compat.Manager,
	paddleVerifier *paddle.Verifier,
	mem0Client *mem0.Client,
) http.Handler {
	mux := http.NewServeMux()
	h := NewHandler(storeImpl, paddleVerifier, mem0Client, compatTokenManager)
	admin := NewAdminHandler(storeImpl, cfg.AdminMasterPassword, cfg.AdminSessionSecret, cfg.AdminSessionTTL)
	user := NewUserHandler(
		storeImpl,
		cfg.UserDashboardPassword,
		cfg.UserSessionSecret,
		cfg.UserSessionTTL,
		cfg.SupabaseURL,
		cfg.SupabaseAnonKey,
	)
	authMiddleware := func(next http.HandlerFunc) http.Handler {
		return middleware.CompatibilityBearerAuth(supabaseVerifier, compatTokenManager, next)
	}
	authRateLimiter := middleware.NewRateLimiter(cfg.AuthRateLimitPerMinute, time.Minute)
	webhookRateLimiter := middleware.NewRateLimiter(cfg.WebhookRateLimitPerMinute, time.Minute)

	mux.HandleFunc("GET /healthz", h.Health)
	mux.HandleFunc("GET /admin/login", admin.LoginPage)
	mux.Handle("POST /admin/login", middleware.RateLimit(authRateLimiter, "admin-login", http.HandlerFunc(admin.LoginSubmit)))
	mux.HandleFunc("GET /admin", admin.Dashboard)
	mux.HandleFunc("GET /admin/wireguard-config/{account}/{device}", admin.DownloadWireGuardConfig)
	mux.HandleFunc("POST /admin/wireguard-config-auto", admin.GenerateAndDownloadWireGuardConfig)
	mux.HandleFunc("GET /admin/wireguard-qr/{account}/{device}", admin.DownloadWireGuardQRCode)
	mux.HandleFunc("POST /admin/wireguard-key/{account}/{device}/generate", admin.GenerateAndSyncWireGuardKey)
	mux.HandleFunc("POST /admin/logout", admin.Logout)
	mux.HandleFunc("GET /user/login", user.LoginPage)
	mux.Handle("POST /user/login", middleware.RateLimit(authRateLimiter, "user-login", http.HandlerFunc(user.LoginSubmit)))
	mux.Handle("POST /user/login-email", middleware.RateLimit(authRateLimiter, "user-login-email", http.HandlerFunc(user.LoginEmailSubmit)))
	mux.HandleFunc("GET /user", user.Dashboard)
	mux.HandleFunc("POST /user/logout", user.Logout)
	mux.HandleFunc("POST /user/devices/create", user.CreateDeviceAndDownloadConfig)
	mux.HandleFunc("POST /user/devices/{id}/delete", user.DeleteDevice)
	mux.HandleFunc("POST /user/devices/{id}/config", user.RotateDeviceAndDownloadConfig)

	// Public compatibility API
	mux.Handle("POST /auth/v1/token", middleware.RateLimit(authRateLimiter, "auth-token", http.HandlerFunc(h.CreateAccessToken)))

	mux.Handle("GET /accounts/v1/accounts/me", authMiddleware(h.GetAccountMe))
	mux.Handle("POST /accounts/v1/accounts", authMiddleware(h.CreateAccount))
	mux.Handle("DELETE /accounts/v1/accounts/me", authMiddleware(h.DeleteAccountMe))

	mux.Handle("GET /accounts/v1/devices", authMiddleware(h.ListDevices))
	mux.Handle("POST /accounts/v1/devices", authMiddleware(h.CreateDevice))
	mux.Handle("GET /accounts/v1/devices/{id}", authMiddleware(h.GetDevice))
	mux.Handle("PUT /accounts/v1/devices/{id}", authMiddleware(h.ReplaceDeviceKey))
	mux.Handle("PUT /accounts/v1/devices/{id}/pubkey", authMiddleware(h.ReplaceDeviceKey))
	mux.Handle("DELETE /accounts/v1/devices/{id}", authMiddleware(h.DeleteDevice))

	mux.HandleFunc("GET /app/v1/relays", h.GetRelays)
	mux.HandleFunc("GET /app/v1/api-addrs", h.GetAPIAddrs)
	mux.HandleFunc("HEAD /app/v1/api-addrs", h.HeadAPIAddrs)
	mux.Handle("POST /app/v1/www-auth-token", authMiddleware(h.GetWWWAuthToken))
	mux.Handle("POST /app/v1/submit-voucher", authMiddleware(h.SubmitVoucher))
	mux.HandleFunc("POST /app/v1/problem-report", h.ProblemReport)

	// Internal gateway API
	mux.Handle("POST /internal/gateways/register", middleware.GatewayAuth(cfg.GatewayToken, http.HandlerFunc(h.RegisterGateway)))
	mux.Handle("GET /internal/gateways/{id}/desired-config", middleware.GatewayAuth(cfg.GatewayToken, http.HandlerFunc(h.GetDesiredGatewayConfig)))
	mux.Handle("POST /internal/gateways/{id}/heartbeat", middleware.GatewayAuth(cfg.GatewayToken, http.HandlerFunc(h.GatewayHeartbeat)))
	mux.Handle("POST /internal/gateways/{id}/apply-result", middleware.GatewayAuth(cfg.GatewayToken, http.HandlerFunc(h.GatewayApplyResult)))

	// Billing webhooks (Paddle only)
	mux.Handle(
		"POST /webhooks/paddle",
		middleware.RateLimit(
			webhookRateLimiter,
			"webhooks-paddle",
			webhookProxyAuth(cfg.WebhookProxyToken, http.HandlerFunc(h.PaddleWebhook)),
		),
	)

	return withJSONDefaults(mux)
}

func withJSONDefaults(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}

func webhookProxyAuth(expectedToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(expectedToken) == "" {
			next.ServeHTTP(w, r)
			return
		}

		token := strings.TrimSpace(r.Header.Get("X-Webhook-Proxy-Token"))
		if token == "" || token != expectedToken {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"UNAUTHORIZED_WEBHOOK_PROXY"}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}
