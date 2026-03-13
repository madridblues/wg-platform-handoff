package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wg-platform-handoff/internal/domain"
	"wg-platform-handoff/internal/store"
)

const userSessionCookieName = "wg_user_session"

type UserHandler struct {
	store             store.Store
	dashboardPassword string
	session           *userSessionManager
	supabaseURL       string
	supabaseAnonKey   string
	httpClient        *http.Client
}

type userSessionManager struct {
	secret []byte
	ttl    time.Duration
}

type userDashboardView struct {
	AccountNumber string
	AccountStatus string
	Expiry        string
	GeneratedAt   string
	CanEmailLogin bool
	Devices       []userDeviceRow
	RecentSessions []userSessionRow
}

type userDeviceRow struct {
	ID        string
	Name      string
	IPv4      string
	CreatedAt string
	Connected string
	Usage     string
	UsagePct  int
}

type userSessionRow struct {
	DeviceID    string
	Relay       string
	Endpoint    string
	HandshakeAt string
	TrafficAt   string
}

var userLoginTemplate = template.Must(template.New("user-login").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>User Login</title>
  <style>
    body { font-family: Arial, sans-serif; margin: 2rem; max-width: 560px; }
    .card { border: 1px solid #ddd; border-radius: 8px; padding: 1rem; }
    input { width: 100%; padding: 0.6rem; margin: 0.5rem 0 1rem 0; }
    button { padding: 0.6rem 1rem; }
    .err { color: #b00020; margin-bottom: 1rem; }
  </style>
</head>
<body>
  <h1>VPN User Dashboard</h1>
  <div class="card">
    {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
    <form method="post" action="/user/login">
      <label for="account">Account Number</label>
      <input id="account" name="account_number" type="text" autocomplete="username" required />
      <label for="password">Dashboard Password (optional)</label>
      <input id="password" name="password" type="password" autocomplete="current-password" />
      <button type="submit">Sign In</button>
    </form>
    {{if .CanEmailLogin}}
    <hr/>
    <form method="post" action="/user/login-email">
      <label for="email">Email</label>
      <input id="email" name="email" type="email" autocomplete="email" required />
      <label for="epassword">Password</label>
      <input id="epassword" name="password" type="password" autocomplete="current-password" required />
      <button type="submit">Sign In with Email</button>
    </form>
    {{end}}
  </div>
</body>
</html>`))

var userDashboardTemplate = template.Must(template.New("user-dashboard").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>User Dashboard</title>
  <style>
    body { font-family: Arial, sans-serif; margin: 1.5rem; }
    header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 1rem; }
    table { border-collapse: collapse; width: 100%; margin-bottom: 1.5rem; }
    th, td { border: 1px solid #ddd; padding: 0.5rem; font-size: 14px; text-align: left; }
    th { background: #f5f5f5; }
    .muted { color: #666; font-size: 12px; margin-bottom: 1rem; }
    .card { border: 1px solid #ddd; border-radius: 8px; padding: 0.8rem; margin-bottom: 1rem; background: #fafafa; }
    button { padding: 0.45rem 0.8rem; margin-right: 0.35rem; }
  </style>
  <meta http-equiv="refresh" content="20"/>
</head>
<body>
  <header>
    <h1>User Dashboard</h1>
    <form method="post" action="/user/logout"><button type="submit">Logout</button></form>
  </header>
  <div class="card">
    <div><strong>Account:</strong> {{.AccountNumber}}</div>
    <div><strong>Status:</strong> {{.AccountStatus}}</div>
    <div><strong>Expiry:</strong> {{.Expiry}}</div>
    <div class="muted">Generated: {{.GeneratedAt}}</div>
  </div>

  <div class="card">
    <form method="post" action="/user/devices/create">
      <label for="mode">Tunnel Mode</label>
      <select id="mode" name="mode">
        <option value="full">Full tunnel</option>
        <option value="split">Split tunnel</option>
      </select>
      <button type="submit">Create Device + Download Config</button>
    </form>
  </div>

  <h2>Your Devices</h2>
  <table>
    <thead>
      <tr>
        <th>ID</th><th>Name</th><th>IPv4</th><th>Status</th><th>Usage</th><th>Created</th><th>Actions</th>
      </tr>
    </thead>
    <tbody>
      {{range .Devices}}
      <tr>
        <td>{{.ID}}</td><td>{{.Name}}</td><td>{{.IPv4}}</td><td>{{.Connected}}</td>
        <td>{{.Usage}}<div class="bar"><span style="width: {{.UsagePct}}%;"></span></div></td>
        <td>{{.CreatedAt}}</td>
        <td>
          <form method="post" action="/user/devices/{{.ID}}/config" style="display:inline;">
            <input type="hidden" name="mode" value="full"/>
            <button type="submit">Rotate + Download</button>
          </form>
          <form method="post" action="/user/devices/{{.ID}}/delete" style="display:inline;">
            <button type="submit">Delete</button>
          </form>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
  <h2>Recent Connection History</h2>
  <table>
    <thead>
      <tr>
        <th>When</th><th>Device</th><th>Gateway</th><th>Endpoint</th><th>Traffic Snapshot</th>
      </tr>
    </thead>
    <tbody>
      {{range .RecentSessions}}
      <tr>
        <td>{{.HandshakeAt}}</td><td>{{.DeviceID}}</td><td>{{.Relay}}</td><td>{{.Endpoint}}</td><td>{{.TrafficAt}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>`))

type userSessionReadable interface {
	AdminListSessionEventsByAccount(ctx context.Context, accountNumber string, limit int) ([]domain.DeviceSessionEvent, error)
}

func NewUserHandler(storeImpl store.Store, dashboardPassword, sessionSecret string, sessionTTL time.Duration, supabaseURL, supabaseAnonKey string) *UserHandler {
	return &UserHandler{
		store:             storeImpl,
		dashboardPassword: strings.TrimSpace(dashboardPassword),
		session:           newUserSessionManager(sessionSecret, sessionTTL),
		supabaseURL:       strings.TrimSpace(supabaseURL),
		supabaseAnonKey:   strings.TrimSpace(supabaseAnonKey),
		httpClient:        &http.Client{Timeout: 10 * time.Second},
	}
}

func (h *UserHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticatedAccount(r); ok {
		http.Redirect(w, r, "/user", http.StatusSeeOther)
		return
	}
	h.renderLogin(w, "")
}

func (h *UserHandler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.enabled() {
		http.Error(w, "user dashboard disabled", http.StatusServiceUnavailable)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderLogin(w, "Invalid form")
		return
	}

	accountNumber := strings.TrimSpace(r.FormValue("account_number"))
	if accountNumber == "" {
		h.renderLogin(w, "Account number required")
		return
	}
	if strings.TrimSpace(h.dashboardPassword) != "" && !secureEqual(r.FormValue("password"), h.dashboardPassword) {
		h.renderLogin(w, "Invalid credentials")
		return
	}

	if _, err := h.store.GetAccountByNumber(r.Context(), accountNumber); err != nil {
		h.renderLogin(w, "Account not found")
		return
	}

	token, expiresAt := h.session.Issue(accountNumber)
	http.SetCookie(w, &http.Cookie{
		Name:     userSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		Expires:  expiresAt,
	})

	http.Redirect(w, r, "/user", http.StatusSeeOther)
}

func (h *UserHandler) LoginEmailSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.enabled() {
		http.Error(w, "user dashboard disabled", http.StatusServiceUnavailable)
		return
	}
	if !h.emailLoginEnabled() {
		h.renderLogin(w, "Email login is not configured")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderLogin(w, "Invalid form")
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := strings.TrimSpace(r.FormValue("password"))
	if email == "" || password == "" {
		h.renderLogin(w, "Email and password are required")
		return
	}

	userID, err := h.verifySupabasePasswordLogin(r.Context(), email, password)
	if err != nil {
		h.renderLogin(w, "Invalid email or password")
		return
	}
	account, err := h.store.GetOrCreateAccountByUserID(r.Context(), userID)
	if err != nil {
		h.renderLogin(w, "Failed to resolve account")
		return
	}

	token, expiresAt := h.session.Issue(account.Number)
	http.SetCookie(w, &http.Cookie{
		Name:     userSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		Expires:  expiresAt,
	})
	http.Redirect(w, r, "/user", http.StatusSeeOther)
}

func (h *UserHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     userSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/user/login", http.StatusSeeOther)
}

func (h *UserHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	accountNumber, ok := h.authenticatedAccount(r)
	if !ok {
		http.Redirect(w, r, "/user/login", http.StatusSeeOther)
		return
	}

	account, err := h.store.GetAccountByNumber(r.Context(), accountNumber)
	if err != nil {
		http.Redirect(w, r, "/user/login", http.StatusSeeOther)
		return
	}
	devices, err := h.store.ListDevices(r.Context(), account.ID)
	if err != nil {
		http.Error(w, "failed to load devices", http.StatusInternalServerError)
		return
	}

	backend, _ := h.store.(adminReadableStore)
	runtimeByID := map[string]domain.AdminDeviceSummary{}
	if backend != nil {
		if runtimeDevices, err := backend.AdminListDevices(r.Context(), 2000); err == nil {
			for _, item := range runtimeDevices {
				if strings.EqualFold(strings.TrimSpace(item.AccountNumber), account.Number) {
					runtimeByID[item.ID] = item
				}
			}
		}
	}

	rows := make([]userDeviceRow, 0, len(devices))
	var maxUsage int64
	for _, d := range devices {
		runtime := runtimeByID[d.ID]
		usage := runtime.RxBytes + runtime.TxBytes
		if usage > maxUsage {
			maxUsage = usage
		}
	}
	for _, d := range devices {
		runtime := runtimeByID[d.ID]
		usage := runtime.RxBytes + runtime.TxBytes
		usagePct := 0
		if maxUsage > 0 {
			usagePct = int((float64(usage) / float64(maxUsage)) * 100)
		}
		status := "offline"
		if runtime.Connected {
			status = "connected"
		}
		rows = append(rows, userDeviceRow{
			ID:        d.ID,
			Name:      d.Name,
			IPv4:      d.IPv4Address,
			CreatedAt: formatTS(d.Created),
			Connected: status,
			Usage:     formatBytes(usage),
			UsagePct:  usagePct,
		})
	}

	sessions := make([]userSessionRow, 0)
	if sessionReader, ok := h.store.(userSessionReadable); ok {
		if events, err := sessionReader.AdminListSessionEventsByAccount(r.Context(), account.Number, 50); err == nil {
			for _, e := range events {
				relay := strings.TrimSpace(e.RelayHostname)
				if relay == "" {
					relay = "n/a"
				}
				endpoint := strings.TrimSpace(e.Endpoint)
				if endpoint == "" {
					endpoint = "n/a"
				}
				sessions = append(sessions, userSessionRow{
					DeviceID:    e.DeviceID,
					Relay:       relay,
					Endpoint:    endpoint,
					HandshakeAt: formatTS(e.HandshakeAt),
					TrafficAt:   fmt.Sprintf("RX %s / TX %s", formatBytes(e.RXBytesSnapshot), formatBytes(e.TXBytesSnapshot)),
				})
			}
		}
	}

	view := userDashboardView{
		AccountNumber: account.Number,
		AccountStatus: account.Status,
		Expiry:        formatTS(account.Expiry),
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		CanEmailLogin: h.emailLoginEnabled(),
		Devices:       rows,
		RecentSessions: sessions,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = userDashboardTemplate.Execute(w, view)
}

func (h *UserHandler) CreateDeviceAndDownloadConfig(w http.ResponseWriter, r *http.Request) {
	accountNumber, ok := h.authenticatedAccount(r)
	if !ok {
		http.Redirect(w, r, "/user/login", http.StatusSeeOther)
		return
	}

	backend, ok := h.store.(adminReadableStore)
	if !ok {
		http.Error(w, "user backend unavailable", http.StatusInternalServerError)
		return
	}

	account, err := h.store.GetAccountByNumber(r.Context(), accountNumber)
	if err != nil {
		http.Error(w, "account lookup failed", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))

	privateKey, publicKey, err := generateWireGuardKeypair()
	if err != nil {
		http.Error(w, "failed to generate keypair", http.StatusInternalServerError)
		return
	}
	presharedKey, err := generateWireGuardPresharedKey()
	if err != nil {
		http.Error(w, "failed to generate preshared key", http.StatusInternalServerError)
		return
	}

	created, err := h.store.CreateDevice(r.Context(), account.ID, publicKey, false)
	if err != nil {
		http.Error(w, "failed to create device", http.StatusInternalServerError)
		return
	}
	if _, err := backend.AdminReplaceDeviceKeyByAccountNumber(r.Context(), accountNumber, created.ID, publicKey, presharedKey); err != nil {
		http.Error(w, "failed to configure preshared key", http.StatusInternalServerError)
		return
	}

	device, conf, err := h.buildConfig(r.Context(), backend, accountNumber, created.ID, privateKey, mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	filename := wireGuardFilename(device.AccountNumber, device.ID)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	_, _ = w.Write([]byte(conf))
}

func (h *UserHandler) RotateDeviceAndDownloadConfig(w http.ResponseWriter, r *http.Request) {
	accountNumber, ok := h.authenticatedAccount(r)
	if !ok {
		http.Redirect(w, r, "/user/login", http.StatusSeeOther)
		return
	}
	backend, ok := h.store.(adminReadableStore)
	if !ok {
		http.Error(w, "user backend unavailable", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))
	deviceID := strings.TrimSpace(r.PathValue("id"))
	if deviceID == "" {
		http.Error(w, "missing device id", http.StatusBadRequest)
		return
	}

	privateKey, publicKey, err := generateWireGuardKeypair()
	if err != nil {
		http.Error(w, "failed to generate keypair", http.StatusInternalServerError)
		return
	}
	presharedKey, err := generateWireGuardPresharedKey()
	if err != nil {
		http.Error(w, "failed to generate preshared key", http.StatusInternalServerError)
		return
	}

	if _, err := backend.AdminReplaceDeviceKeyByAccountNumber(r.Context(), accountNumber, deviceID, publicKey, presharedKey); err != nil {
		http.Error(w, "failed to rotate device key", http.StatusInternalServerError)
		return
	}
	device, conf, err := h.buildConfig(r.Context(), backend, accountNumber, deviceID, privateKey, mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	filename := wireGuardFilename(device.AccountNumber, device.ID)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	_, _ = w.Write([]byte(conf))
}

func (h *UserHandler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	accountNumber, ok := h.authenticatedAccount(r)
	if !ok {
		http.Redirect(w, r, "/user/login", http.StatusSeeOther)
		return
	}
	deviceID := strings.TrimSpace(r.PathValue("id"))
	if deviceID == "" {
		http.Error(w, "missing device id", http.StatusBadRequest)
		return
	}
	account, err := h.store.GetAccountByNumber(r.Context(), accountNumber)
	if err != nil {
		http.Error(w, "account lookup failed", http.StatusInternalServerError)
		return
	}
	if err := h.store.DeleteDevice(r.Context(), account.ID, deviceID); err != nil {
		http.Error(w, "failed to delete device", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/user", http.StatusSeeOther)
}

func (h *UserHandler) buildConfig(ctx context.Context, backend adminReadableStore, accountNumber, deviceID, privateKey, modeRaw string) (domain.AdminDeviceSummary, string, error) {
	device, err := backend.AdminGetDeviceByAccountNumber(ctx, accountNumber, deviceID)
	if err != nil {
		return domain.AdminDeviceSummary{}, "", fmt.Errorf("device not found")
	}
	gateways, err := backend.AdminListGateways(ctx, 200)
	if err != nil {
		return domain.AdminDeviceSummary{}, "", fmt.Errorf("failed to load gateways")
	}

	var selected *domain.AdminGatewaySummary
	for i := range gateways {
		candidate := gateways[i]
		if candidate.Active && strings.TrimSpace(candidate.WGPublicKey) != "" && strings.TrimSpace(candidate.PublicIPv4) != "" {
			selected = &candidate
			break
		}
	}
	if selected == nil {
		return domain.AdminDeviceSummary{}, "", fmt.Errorf("no active gateway with wireguard key")
	}

	allowedIPs := "0.0.0.0/0, ::/0"
	if strings.EqualFold(strings.TrimSpace(modeRaw), "split") {
		allowedIPs = "10.64.0.0/24, fd00::/64"
	}
	listenPort := selected.WGPort
	if listenPort <= 0 {
		listenPort = 51820
	}
	endpointIP := stripNetworkMask(strings.TrimSpace(selected.PublicIPv4))
	pskLine := ""
	if strings.TrimSpace(device.PresharedKey) != "" {
		pskLine = "PresharedKey = " + strings.TrimSpace(device.PresharedKey) + "\n"
	}

	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s, %s
DNS = 1.1.1.1

[Peer]
PublicKey = %s
%sEndpoint = %s:%d
AllowedIPs = %s
PersistentKeepalive = 25
`, privateKey, device.IPv4Address, device.IPv6Address, selected.WGPublicKey, pskLine, endpointIP, listenPort, allowedIPs)

	return device, conf, nil
}

func (h *UserHandler) renderLogin(w http.ResponseWriter, errorText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = userLoginTemplate.Execute(w, map[string]any{
		"Error":         errorText,
		"CanEmailLogin": h.emailLoginEnabled(),
	})
}

func (h *UserHandler) enabled() bool {
	return h != nil && h.session != nil && len(h.session.secret) > 0
}

func (h *UserHandler) emailLoginEnabled() bool {
	return h != nil && strings.TrimSpace(h.supabaseURL) != "" && strings.TrimSpace(h.supabaseAnonKey) != ""
}

func (h *UserHandler) authenticatedAccount(r *http.Request) (string, bool) {
	if !h.enabled() {
		return "", false
	}
	cookie, err := r.Cookie(userSessionCookieName)
	if err != nil {
		return "", false
	}
	return h.session.Verify(cookie.Value, time.Now().UTC())
}

func newUserSessionManager(secret string, ttl time.Duration) *userSessionManager {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return &userSessionManager{ttl: ttl}
	}
	return &userSessionManager{secret: []byte(secret), ttl: ttl}
}

func (m *userSessionManager) Issue(accountNumber string) (string, time.Time) {
	expiresAt := time.Now().UTC().Add(m.ttl)
	payload := strings.TrimSpace(accountNumber) + "|" + strconv.FormatInt(expiresAt.Unix(), 10)
	signature := m.sign(payload)
	return payload + "." + signature, expiresAt
}

func (m *userSessionManager) Verify(token string, now time.Time) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", false
	}
	payload := strings.TrimSpace(parts[0])
	signature := strings.TrimSpace(parts[1])
	if payload == "" || signature == "" {
		return "", false
	}
	expected := m.sign(payload)
	if !secureEqual(expected, signature) {
		return "", false
	}

	fields := strings.Split(payload, "|")
	if len(fields) != 2 {
		return "", false
	}
	accountNumber := strings.TrimSpace(fields[0])
	if accountNumber == "" {
		return "", false
	}
	unixExpiry, err := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
	if err != nil {
		return "", false
	}
	if !time.Unix(unixExpiry, 0).UTC().After(now) {
		return "", false
	}
	return accountNumber, true
}

func (m *userSessionManager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *UserHandler) verifySupabasePasswordLogin(ctx context.Context, email, password string) (string, error) {
	body := map[string]string{
		"email":    strings.TrimSpace(email),
		"password": password,
	}
	b, _ := json.Marshal(body)
	url := strings.TrimRight(h.supabaseURL, "/") + "/auth/v1/token?grant_type=password"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", h.supabaseAnonKey)
	req.Header.Set("Authorization", "Bearer "+h.supabaseAnonKey)

	res, err := h.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return "", fmt.Errorf("supabase auth failed: %s", strings.TrimSpace(string(payload)))
	}

	var payload struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.User.ID) == "" {
		return "", fmt.Errorf("supabase user id missing")
	}
	return strings.TrimSpace(payload.User.ID), nil
}
