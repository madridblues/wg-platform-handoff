package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr                  string
	ReadTimeout               time.Duration
	WriteTimeout              time.Duration
	AuthRateLimitPerMinute    int
	WebhookRateLimitPerMinute int
	DatabaseURL               string
	SupabaseJWTSecret         string
	SupabaseURL               string
	SupabaseAnonKey           string
	CompatTokenSecret         string
	CompatTokenTTL            time.Duration
	PaddleWebhookSecret       string
	Mem0APIKey                string
	AdminMasterPassword       string
	AdminSessionSecret        string
	AdminSessionTTL           time.Duration
	UserDashboardPassword     string
	UserSessionSecret         string
	UserSessionTTL            time.Duration
	ControlPlaneBaseURL       string
	GatewayID                 string
	GatewayRegion             string
	GatewayProvider           string
	GatewayToken              string
	GatewayPublicIPv4         string
	GatewayPublicIPv6         string
	GatewayWGPublicKey        string
	GatewayWGInterface        string
	GatewayWGPrivateKeyPath   string
	GatewayWGAddressIPv4      string
	GatewayWGAddressIPv6      string
	GatewayWGListenPort       int
	GatewayWGApplyEnabled     bool
	GatewayWGConfigDir        string
	WebhookProxyToken         string
	GatewayHeartbeatPeriod    time.Duration
}

func Load() Config {
	return Config{
		HTTPAddr:                  getEnv("HTTP_ADDR", ":8080"),
		ReadTimeout:               time.Duration(getEnvInt("HTTP_READ_TIMEOUT_SECONDS", 15)) * time.Second,
		WriteTimeout:              time.Duration(getEnvInt("HTTP_WRITE_TIMEOUT_SECONDS", 15)) * time.Second,
		AuthRateLimitPerMinute:    getEnvInt("AUTH_RATE_LIMIT_PER_MINUTE", 120),
		WebhookRateLimitPerMinute: getEnvInt("WEBHOOK_RATE_LIMIT_PER_MINUTE", 600),
		DatabaseURL:               os.Getenv("DATABASE_URL"),
		SupabaseJWTSecret:         os.Getenv("SUPABASE_JWT_SECRET"),
		SupabaseURL:               strings.TrimSpace(os.Getenv("SUPABASE_URL")),
		SupabaseAnonKey:           strings.TrimSpace(os.Getenv("SUPABASE_ANON_KEY")),
		CompatTokenSecret:         firstNonEmpty(strings.TrimSpace(os.Getenv("COMPAT_TOKEN_SECRET")), strings.TrimSpace(os.Getenv("SUPABASE_JWT_SECRET"))),
		CompatTokenTTL:            time.Duration(getEnvInt("COMPAT_TOKEN_TTL_SECONDS", 3600)) * time.Second,
		PaddleWebhookSecret:       os.Getenv("PADDLE_WEBHOOK_SECRET"),
		Mem0APIKey:                os.Getenv("MEM0_API_KEY"),
		AdminMasterPassword:       os.Getenv("ADMIN_MASTER_PASSWORD"),
		AdminSessionSecret:        firstNonEmpty(strings.TrimSpace(os.Getenv("ADMIN_SESSION_SECRET")), strings.TrimSpace(os.Getenv("COMPAT_TOKEN_SECRET")), strings.TrimSpace(os.Getenv("SUPABASE_JWT_SECRET"))),
		AdminSessionTTL:           time.Duration(getEnvInt("ADMIN_SESSION_TTL_SECONDS", 43200)) * time.Second,
		UserDashboardPassword:     os.Getenv("USER_DASHBOARD_PASSWORD"),
		UserSessionSecret:         firstNonEmpty(strings.TrimSpace(os.Getenv("USER_SESSION_SECRET")), strings.TrimSpace(os.Getenv("ADMIN_SESSION_SECRET")), strings.TrimSpace(os.Getenv("COMPAT_TOKEN_SECRET")), strings.TrimSpace(os.Getenv("SUPABASE_JWT_SECRET"))),
		UserSessionTTL:            time.Duration(getEnvInt("USER_SESSION_TTL_SECONDS", 43200)) * time.Second,
		ControlPlaneBaseURL:       getEnv("CONTROL_PLANE_BASE_URL", "http://localhost:8080"),
		GatewayID:                 getEnv("GATEWAY_ID", "gateway-dev"),
		GatewayRegion:             getEnv("GATEWAY_REGION", "dev-region"),
		GatewayProvider:           getEnv("GATEWAY_PROVIDER", "self"),
		GatewayToken:              os.Getenv("GATEWAY_TOKEN"),
		GatewayPublicIPv4:         os.Getenv("GATEWAY_PUBLIC_IPV4"),
		GatewayPublicIPv6:         os.Getenv("GATEWAY_PUBLIC_IPV6"),
		GatewayWGPublicKey:        os.Getenv("GATEWAY_WG_PUBLIC_KEY"),
		GatewayWGInterface:        getEnv("GATEWAY_WG_INTERFACE", "wg0"),
		GatewayWGPrivateKeyPath:   getEnv("GATEWAY_WG_PRIVATE_KEY_PATH", "/etc/wireguard/privatekey"),
		GatewayWGAddressIPv4:      getEnv("GATEWAY_WG_ADDRESS_IPV4", "10.64.0.1/24"),
		GatewayWGAddressIPv6:      getEnv("GATEWAY_WG_ADDRESS_IPV6", "fd00::1/64"),
		GatewayWGListenPort:       getEnvInt("GATEWAY_WG_LISTEN_PORT", 51820),
		GatewayWGApplyEnabled:     getEnvBool("GATEWAY_WG_APPLY_ENABLED", true),
		GatewayWGConfigDir:        getEnv("GATEWAY_WG_CONFIG_DIR", "/run/wg-platform"),
		WebhookProxyToken:         os.Getenv("WEBHOOK_PROXY_TOKEN"),
		GatewayHeartbeatPeriod:    time.Duration(getEnvInt("GATEWAY_HEARTBEAT_SECONDS", 10)) * time.Second,
	}
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}

	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
