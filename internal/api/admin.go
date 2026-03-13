package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wg-platform-handoff/internal/domain"
	"wg-platform-handoff/internal/store"
)

const (
	adminSessionCookieName = "wg_admin_session"
)

type adminReadableStore interface {
	AdminListAccounts(ctx context.Context, limit int) ([]domain.AdminAccountSummary, error)
	AdminListGateways(ctx context.Context, limit int) ([]domain.AdminGatewaySummary, error)
}

type AdminHandler struct {
	store          store.Store
	masterPassword string
	session        *adminSessionManager
}

type adminSessionManager struct {
	secret []byte
	ttl    time.Duration
}

type adminDashboardView struct {
	GeneratedAt string
	Accounts    []adminAccountRow
	Gateways    []adminGatewayRow
}

type adminAccountRow struct {
	AccountNumber  string
	SupabaseUserID string
	Status         string
	Expiry         string
	DeviceCount    int64
	UpdatedAt      string
}

type adminGatewayRow struct {
	Hostname      string
	Region        string
	Provider      string
	Active        string
	PublicIPv4    string
	PublicIPv6    string
	LastStatus    string
	LastHeartbeat string
}

var adminLoginTemplate = template.Must(template.New("admin-login").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Admin Login</title>
  <style>
    body { font-family: Arial, sans-serif; margin: 2rem; max-width: 520px; }
    .card { border: 1px solid #ddd; border-radius: 8px; padding: 1rem; }
    input { width: 100%; padding: 0.6rem; margin: 0.5rem 0 1rem 0; }
    button { padding: 0.6rem 1rem; }
    .err { color: #b00020; margin-bottom: 1rem; }
  </style>
</head>
<body>
  <h1>Admin Login</h1>
  <div class="card">
    {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
    <form method="post" action="/admin/login">
      <label for="password">Master Password</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required />
      <button type="submit">Sign In</button>
    </form>
  </div>
</body>
</html>`))

var adminDashboardTemplate = template.Must(template.New("admin-dashboard").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Admin Dashboard</title>
  <style>
    body { font-family: Arial, sans-serif; margin: 1.5rem; }
    header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1rem; }
    table { border-collapse: collapse; width: 100%; margin-bottom: 1.5rem; }
    th, td { border: 1px solid #ddd; padding: 0.5rem; font-size: 14px; text-align: left; }
    th { background: #f5f5f5; }
    .muted { color: #666; font-size: 12px; margin-bottom: 1rem; }
    .section { margin-top: 1rem; }
    button { padding: 0.45rem 0.8rem; }
  </style>
</head>
<body>
  <header>
    <h1>VPN Admin Dashboard</h1>
    <form method="post" action="/admin/logout"><button type="submit">Logout</button></form>
  </header>
  <div class="muted">Generated: {{.GeneratedAt}}</div>

  <div class="section">
    <h2>Gateways</h2>
    <table>
      <thead>
        <tr>
          <th>Hostname</th><th>Region</th><th>Provider</th><th>Active</th>
          <th>Public IPv4</th><th>Public IPv6</th><th>Status</th><th>Last Heartbeat</th>
        </tr>
      </thead>
      <tbody>
        {{range .Gateways}}
        <tr>
          <td>{{.Hostname}}</td><td>{{.Region}}</td><td>{{.Provider}}</td><td>{{.Active}}</td>
          <td>{{.PublicIPv4}}</td><td>{{.PublicIPv6}}</td><td>{{.LastStatus}}</td><td>{{.LastHeartbeat}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>

  <div class="section">
    <h2>Users / Accounts</h2>
    <table>
      <thead>
        <tr>
          <th>Account Number</th><th>Supabase User</th><th>Status</th>
          <th>Expiry</th><th>Devices</th><th>Updated</th>
        </tr>
      </thead>
      <tbody>
        {{range .Accounts}}
        <tr>
          <td>{{.AccountNumber}}</td><td>{{.SupabaseUserID}}</td><td>{{.Status}}</td>
          <td>{{.Expiry}}</td><td>{{.DeviceCount}}</td><td>{{.UpdatedAt}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
</body>
</html>`))

func NewAdminHandler(storeImpl store.Store, masterPassword, sessionSecret string, sessionTTL time.Duration) *AdminHandler {
	return &AdminHandler{
		store:          storeImpl,
		masterPassword: strings.TrimSpace(masterPassword),
		session:        newAdminSessionManager(sessionSecret, sessionTTL),
	}
}

func (h *AdminHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if h.isAuthenticated(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	h.renderLogin(w, "")
}

func (h *AdminHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.enabled() {
		http.Error(w, "admin dashboard disabled", http.StatusServiceUnavailable)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.renderLogin(w, "Invalid form")
		return
	}

	password := r.Form.Get("password")
	if !secureEqual(password, h.masterPassword) {
		h.renderLogin(w, "Invalid password")
		return
	}

	token, expiresAt := h.session.Issue()
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		Expires:  expiresAt,
	})

	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *AdminHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *AdminHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if !h.enabled() {
		http.Error(w, "admin dashboard disabled", http.StatusServiceUnavailable)
		return
	}

	if !h.isAuthenticated(r) {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	backend, ok := h.store.(adminReadableStore)
	if !ok {
		http.Error(w, "admin backend unavailable", http.StatusInternalServerError)
		return
	}

	gateways, err := backend.AdminListGateways(r.Context(), 200)
	if err != nil {
		http.Error(w, "failed to load gateways", http.StatusInternalServerError)
		return
	}

	accounts, err := backend.AdminListAccounts(r.Context(), 200)
	if err != nil {
		http.Error(w, "failed to load accounts", http.StatusInternalServerError)
		return
	}

	view := adminDashboardView{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Accounts:    toAdminAccountRows(accounts),
		Gateways:    toAdminGatewayRows(gateways),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminDashboardTemplate.Execute(w, view)
}

func (h *AdminHandler) renderLogin(w http.ResponseWriter, errorText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminLoginTemplate.Execute(w, map[string]string{
		"Error": errorText,
	})
}

func (h *AdminHandler) enabled() bool {
	return h != nil && h.masterPassword != "" && h.session != nil && len(h.session.secret) > 0
}

func (h *AdminHandler) isAuthenticated(r *http.Request) bool {
	if !h.enabled() {
		return false
	}

	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil {
		return false
	}

	return h.session.Verify(cookie.Value, time.Now().UTC())
}

func newAdminSessionManager(secret string, ttl time.Duration) *adminSessionManager {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}

	secret = strings.TrimSpace(secret)
	if secret == "" {
		return &adminSessionManager{ttl: ttl}
	}

	return &adminSessionManager{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

func (m *adminSessionManager) Issue() (string, time.Time) {
	expiresAt := time.Now().UTC().Add(m.ttl)
	payload := strconv.FormatInt(expiresAt.Unix(), 10)
	signature := m.sign(payload)
	return payload + "." + signature, expiresAt
}

func (m *adminSessionManager) Verify(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}

	payload := strings.TrimSpace(parts[0])
	signature := strings.TrimSpace(parts[1])
	if payload == "" || signature == "" {
		return false
	}

	expected := m.sign(payload)
	if !secureEqual(expected, signature) {
		return false
	}

	unixExpiry, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}

	return time.Unix(unixExpiry, 0).UTC().After(now)
}

func (m *adminSessionManager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func secureEqual(a, b string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func toAdminAccountRows(items []domain.AdminAccountSummary) []adminAccountRow {
	out := make([]adminAccountRow, 0, len(items))
	for _, item := range items {
		out = append(out, adminAccountRow{
			AccountNumber:  item.AccountNumber,
			SupabaseUserID: item.SupabaseUserID,
			Status:         item.Status,
			Expiry:         formatTS(item.Expiry),
			DeviceCount:    item.DeviceCount,
			UpdatedAt:      formatTS(item.UpdatedAt),
		})
	}
	return out
}

func toAdminGatewayRows(items []domain.AdminGatewaySummary) []adminGatewayRow {
	out := make([]adminGatewayRow, 0, len(items))
	for _, item := range items {
		heartbeat := "never"
		if item.LastHeartbeat != nil {
			heartbeat = formatTS(*item.LastHeartbeat)
		}

		out = append(out, adminGatewayRow{
			Hostname:      item.Hostname,
			Region:        item.Region,
			Provider:      item.Provider,
			Active:        fmt.Sprintf("%t", item.Active),
			PublicIPv4:    item.PublicIPv4,
			PublicIPv6:    item.PublicIPv6,
			LastStatus:    item.LastStatus,
			LastHeartbeat: heartbeat,
		})
	}
	return out
}

func formatTS(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return t.UTC().Format(time.RFC3339)
}
