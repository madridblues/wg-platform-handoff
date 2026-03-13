package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/crypto/curve25519"
	"wg-platform-handoff/internal/domain"
	"wg-platform-handoff/internal/store"
)

const (
	adminSessionCookieName = "wg_admin_session"
)

type adminReadableStore interface {
	AdminListAccounts(ctx context.Context, limit int) ([]domain.AdminAccountSummary, error)
	AdminListGateways(ctx context.Context, limit int) ([]domain.AdminGatewaySummary, error)
	AdminListDevices(ctx context.Context, limit int) ([]domain.AdminDeviceSummary, error)
	AdminGetDeviceByAccountNumber(ctx context.Context, accountNumber, deviceID string) (domain.AdminDeviceSummary, error)
	AdminReplaceDeviceKeyByAccountNumber(ctx context.Context, accountNumber, deviceID, pubkey, presharedKey string) (domain.AdminDeviceSummary, error)
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
	GeneratedAt         string
	TotalAccounts       int
	TotalDevices        int
	TotalGateways       int
	HealthyGateways     int
	ActiveConnections   int
	PaidAccounts        int
	ObservedTraffic     string
	GatewayUtilization  string
	Accounts            []adminAccountRow
	Gateways            []adminGatewayRow
	Devices             []adminDeviceRow
}

type adminAccountRow struct {
	AccountNumber     string
	SupabaseUserID    string
	Status            string
	PaymentStatus     string
	Plan              string
	Expiry            string
	CurrentPeriodEnd  string
	DeviceCount       int64
	LastSeen          string
	BandwidthUsed     string
	UpdatedAt         string
}

type adminGatewayRow struct {
	Hostname          string
	Region            string
	Provider          string
	WGPort            int
	Active            string
	PublicIPv4        string
	PublicIPv6        string
	LastStatus        string
	LastStatusClass   string
	LastHeartbeat     string
	LastHeartbeatRel  string
	LastApply         string
	Load              string
}

type adminDeviceRow struct {
	AccountNumber  string
	DeviceID       string
	DeviceName     string
	IPv4Address    string
	RelayHostname  string
	LastSeen       string
	BandwidthUsed  string
	ConnectionText string
	ConnectionClass string
	CreatedAt      string
	DownloadURL    string
	QRURL          string
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
    .cards { display: grid; grid-template-columns: repeat(4, minmax(0,1fr)); gap: 0.75rem; margin: 0.8rem 0 1.4rem 0; }
    .card { border: 1px solid #ddd; border-radius: 8px; padding: 0.7rem; background: #fafafa; }
    .label { font-size: 12px; color: #666; }
    .value { font-size: 22px; font-weight: 700; margin-top: 0.2rem; }
    .status { display: inline-flex; align-items: center; gap: 0.35rem; }
    .dot { width: 8px; height: 8px; border-radius: 9999px; display: inline-block; }
    .status-ok { background: #2e7d32; }
    .status-bad { background: #b71c1c; }
    .status-unknown { background: #757575; }
    .wg-form { display: flex; gap: 0.35rem; align-items: center; flex-wrap: wrap; }
    .wg-form input { min-width: 220px; padding: 0.35rem; }
    .hint { font-size: 12px; color: #666; }
    @media (max-width: 1100px) {
      .cards { grid-template-columns: repeat(2, minmax(0,1fr)); }
    }
    @media (max-width: 700px) {
      .cards { grid-template-columns: repeat(1, minmax(0,1fr)); }
    }
  </style>
  <meta http-equiv="refresh" content="20"/>
</head>
<body>
  <header>
    <h1>VPN Admin Dashboard</h1>
    <form method="post" action="/admin/logout"><button type="submit">Logout</button></form>
  </header>
  <div class="muted">Generated: {{.GeneratedAt}}</div>
  <div class="cards">
    <div class="card"><div class="label">Accounts</div><div class="value">{{.TotalAccounts}}</div></div>
    <div class="card"><div class="label">Devices</div><div class="value">{{.TotalDevices}}</div></div>
    <div class="card"><div class="label">Gateways</div><div class="value">{{.TotalGateways}}</div></div>
    <div class="card"><div class="label">Healthy Gateways</div><div class="value">{{.HealthyGateways}}</div></div>
    <div class="card"><div class="label">Active Connections</div><div class="value">{{.ActiveConnections}}</div></div>
    <div class="card"><div class="label">Paid Accounts</div><div class="value">{{.PaidAccounts}}</div></div>
    <div class="card"><div class="label">Observed Traffic</div><div class="value">{{.ObservedTraffic}}</div></div>
    <div class="card"><div class="label">Gateway Utilization</div><div class="value">{{.GatewayUtilization}}</div></div>
  </div>

  <div class="section">
    <h2>Gateways</h2>
    <table>
      <thead>
        <tr>
          <th>Hostname</th><th>Region</th><th>Provider</th><th>Port</th><th>Active</th>
          <th>Public IPv4</th><th>Status</th><th>Load</th><th>Last Heartbeat</th><th>Last Apply</th>
        </tr>
      </thead>
      <tbody>
        {{range .Gateways}}
        <tr>
          <td>{{.Hostname}}</td><td>{{.Region}}</td><td>{{.Provider}}</td><td>{{.WGPort}}</td><td>{{.Active}}</td>
          <td>{{.PublicIPv4}}</td>
          <td><span class="status"><span class="dot {{.LastStatusClass}}"></span>{{.LastStatus}}</span></td>
          <td>{{.Load}}</td>
          <td>{{.LastHeartbeatRel}}<div class="hint">{{.LastHeartbeat}}</div></td>
          <td>{{.LastApply}}</td>
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
          <th>Account Number</th><th>Supabase User</th><th>Status</th><th>Payment</th><th>Plan</th>
          <th>Expiry</th><th>Period End</th><th>Devices</th><th>Last Connected</th><th>Bandwidth</th><th>Updated</th>
        </tr>
      </thead>
      <tbody>
        {{range .Accounts}}
        <tr>
          <td>{{.AccountNumber}}</td><td>{{.SupabaseUserID}}</td><td>{{.Status}}</td>
          <td>{{.PaymentStatus}}</td><td>{{.Plan}}</td><td>{{.Expiry}}</td><td>{{.CurrentPeriodEnd}}</td><td>{{.DeviceCount}}</td>
          <td>{{.LastSeen}}</td><td>{{.BandwidthUsed}}</td><td>{{.UpdatedAt}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  <div class="section">
    <h2>Devices</h2>
    <table>
      <thead>
        <tr>
          <th>Account</th><th>Device ID</th><th>Name</th><th>IPv4</th><th>Gateway</th><th>Last Seen</th><th>Bandwidth</th><th>Status</th><th>Created</th><th>WireGuard Config</th>
        </tr>
      </thead>
      <tbody>
        {{range .Devices}}
        <tr>
          <td>{{.AccountNumber}}</td><td>{{.DeviceID}}</td><td>{{.DeviceName}}</td><td>{{.IPv4Address}}</td><td>{{.RelayHostname}}</td><td>{{.LastSeen}}</td><td>{{.BandwidthUsed}}</td>
          <td><span class="status"><span class="dot {{.ConnectionClass}}"></span>{{.ConnectionText}}</span></td><td>{{.CreatedAt}}</td>
          <td>
            <form class="wg-form" method="get" action="{{.DownloadURL}}">
              <input id="pk-{{.DeviceID}}" type="text" name="private_key" placeholder="Client private key (base64)" required />
              <select name="mode">
                <option value="full">Full tunnel</option>
                <option value="split">Split tunnel</option>
              </select>
              <button type="button" onclick="generateKey('{{.AccountNumber}}','{{.DeviceID}}')">Generate key</button>
              <button type="submit" formmethod="post" formaction="/admin/wireguard-config-auto/{{.AccountNumber}}/{{.DeviceID}}">Generate + Download</button>
              <button type="submit">Download</button>
              <button type="submit" formaction="{{.QRURL}}" formtarget="_blank">QR</button>
            </form>
            <div id="msg-{{.DeviceID}}" class="hint"></div>
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>
  <script>
    async function generateKey(account, device) {
      const msg = document.getElementById('msg-' + device);
      const input = document.getElementById('pk-' + device);
      if (!msg || !input) return;
      msg.textContent = 'Generating key...';
      try {
        const path = '/admin/wireguard-key/' + encodeURIComponent(account) + '/' + encodeURIComponent(device) + '/generate';
        const res = await fetch(path, { method: 'POST' });
        if (!res.ok) {
          const text = await res.text();
          throw new Error(text || ('HTTP ' + res.status));
        }
        const payload = await res.json();
        input.value = payload.private_key || '';
        msg.textContent = 'Key generated and synced to device.';
      } catch (err) {
        msg.textContent = 'Key generation failed: ' + err.message;
      }
    }
  </script>
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
	devices, err := backend.AdminListDevices(r.Context(), 1000)
	if err != nil {
		http.Error(w, "failed to load devices", http.StatusInternalServerError)
		return
	}

	view := adminDashboardView{
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		TotalAccounts:      len(accounts),
		TotalDevices:       len(devices),
		TotalGateways:      len(gateways),
		HealthyGateways:    countHealthyGateways(gateways),
		ActiveConnections:  countConnectedDevices(devices),
		PaidAccounts:       countPaidAccounts(accounts),
		ObservedTraffic:    formatBytes(totalTrafficBytes(accounts)),
		GatewayUtilization: summarizeGatewayUtilization(gateways),
		Accounts:           toAdminAccountRows(accounts),
		Gateways:           toAdminGatewayRows(gateways),
		Devices:            toAdminDeviceRows(devices),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = adminDashboardTemplate.Execute(w, view)
}

func (h *AdminHandler) DownloadWireGuardConfig(w http.ResponseWriter, r *http.Request) {
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

	device, conf, err := h.buildWireGuardConfig(r.Context(), backend, r.PathValue("account"), r.PathValue("device"), r.URL.Query().Get("private_key"), r.URL.Query().Get("mode"))
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "private_key") || strings.Contains(err.Error(), "missing account/device") {
			status = http.StatusBadRequest
		}
		if strings.Contains(err.Error(), "device not found") {
			status = http.StatusNotFound
		}
		if strings.Contains(err.Error(), "no active gateway") {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}

	filename := wireGuardFilename(device.AccountNumber, device.ID)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", filename, filename))
	_, _ = w.Write([]byte(conf))
}

func (h *AdminHandler) GenerateAndDownloadWireGuardConfig(w http.ResponseWriter, r *http.Request) {
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

	accountNumber := strings.TrimSpace(r.PathValue("account"))
	deviceID := strings.TrimSpace(r.PathValue("device"))
	if accountNumber == "" || deviceID == "" {
		http.Error(w, "missing account/device path params", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))

	privateKey, publicKey, err := generateWireGuardKeypair()
	if err != nil {
		http.Error(w, "failed to generate wireguard keypair", http.StatusInternalServerError)
		return
	}
	presharedKey, err := generateWireGuardPresharedKey()
	if err != nil {
		http.Error(w, "failed to generate wireguard preshared key", http.StatusInternalServerError)
		return
	}

	if _, err := backend.AdminReplaceDeviceKeyByAccountNumber(r.Context(), accountNumber, deviceID, publicKey, presharedKey); err != nil {
		http.Error(w, "failed to sync generated key", http.StatusInternalServerError)
		return
	}

	device, conf, err := h.buildWireGuardConfig(r.Context(), backend, accountNumber, deviceID, privateKey, mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	filename := wireGuardFilename(device.AccountNumber, device.ID)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	_, _ = w.Write([]byte(conf))
}

func (h *AdminHandler) DownloadWireGuardQRCode(w http.ResponseWriter, r *http.Request) {
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

	_, conf, err := h.buildWireGuardConfig(r.Context(), backend, r.PathValue("account"), r.PathValue("device"), r.URL.Query().Get("private_key"), r.URL.Query().Get("mode"))
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "private_key") || strings.Contains(err.Error(), "missing account/device") {
			status = http.StatusBadRequest
		}
		if strings.Contains(err.Error(), "device not found") {
			status = http.StatusNotFound
		}
		if strings.Contains(err.Error(), "no active gateway") {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}

	png, err := qrcode.Encode(conf, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, "failed to render qr code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (h *AdminHandler) GenerateAndSyncWireGuardKey(w http.ResponseWriter, r *http.Request) {
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

	accountNumber := strings.TrimSpace(r.PathValue("account"))
	deviceID := strings.TrimSpace(r.PathValue("device"))
	if accountNumber == "" || deviceID == "" {
		http.Error(w, "missing account/device path params", http.StatusBadRequest)
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

	device, err := backend.AdminReplaceDeviceKeyByAccountNumber(r.Context(), accountNumber, deviceID, publicKey, presharedKey)
	if err != nil {
		http.Error(w, "failed to sync device key", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"account_number": device.AccountNumber,
		"device_id":      device.ID,
		"public_key":     publicKey,
		"private_key":    privateKey,
		"preshared_key":  presharedKey,
	})
}

func (h *AdminHandler) buildWireGuardConfig(ctx context.Context, backend adminReadableStore, accountPathValue, devicePathValue, privateKeyRaw, modeRaw string) (domain.AdminDeviceSummary, string, error) {
	accountNumber := strings.TrimSpace(accountPathValue)
	deviceID := strings.TrimSpace(devicePathValue)
	privateKey := strings.TrimSpace(privateKeyRaw)
	if accountNumber == "" || deviceID == "" {
		return domain.AdminDeviceSummary{}, "", fmt.Errorf("missing account/device path params")
	}
	if !looksLikeWireGuardPrivateKey(privateKey) {
		return domain.AdminDeviceSummary{}, "", fmt.Errorf("missing or invalid private_key query parameter")
	}

	device, err := backend.AdminGetDeviceByAccountNumber(ctx, accountNumber, deviceID)
	if err != nil {
		return domain.AdminDeviceSummary{}, "", fmt.Errorf("device not found")
	}
	derivedPublicKey, err := wireGuardPublicKeyFromPrivate(privateKey)
	if err != nil {
		return domain.AdminDeviceSummary{}, "", fmt.Errorf("invalid private_key")
	}
	if strings.TrimSpace(device.PubKey) != "" && strings.TrimSpace(device.PubKey) != derivedPublicKey {
		return domain.AdminDeviceSummary{}, "", fmt.Errorf("private_key does not match device public key; click Generate key to sync")
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

	endpointIP := stripNetworkMask(strings.TrimSpace(selected.PublicIPv4))
	listenPort := selected.WGPort
	if listenPort <= 0 {
		listenPort = 51820
	}
	allowedIPs := "0.0.0.0/0, ::/0"
	switch strings.ToLower(strings.TrimSpace(modeRaw)) {
	case "split":
		allowedIPs = "10.64.0.0/24, fd00::/64"
	}

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

func stripNetworkMask(value string) string {
	if idx := strings.Index(value, "/"); idx > 0 {
		return strings.TrimSpace(value[:idx])
	}
	return strings.TrimSpace(value)
}

func sanitizeFilename(value string) string {
	if value == "" {
		return "config"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	return b.String()
}

func wireGuardFilename(accountNumber, deviceID string) string {
	account := strings.ToLower(strings.TrimSpace(sanitizeFilename(accountNumber)))
	if account == "" || account == "config" {
		account = "account"
	}
	if len(account) > 16 {
		account = account[:16]
	}

	device := strings.ToLower(strings.TrimSpace(sanitizeFilename(deviceID)))
	if len(device) > 8 {
		device = device[:8]
	}
	if device == "" || device == "config" {
		return fmt.Sprintf("wg-%s.conf", account)
	}

	return fmt.Sprintf("wg-%s-%s.conf", account, device)
}

func looksLikeWireGuardPrivateKey(value string) bool {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return len(decoded) == 32
}

func generateWireGuardKeypair() (string, string, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", err
	}
	// Clamp as required by X25519 private keys.
	priv[0] &= 248
	priv[31] = (priv[31] & 127) | 64

	pubBytes, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}

	privateKey := base64.StdEncoding.EncodeToString(priv[:])
	publicKey := base64.StdEncoding.EncodeToString(pubBytes)
	return privateKey, publicKey, nil
}

func generateWireGuardPresharedKey() (string, error) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key[:]), nil
}

func wireGuardPublicKeyFromPrivate(privateKey string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKey))
	if err != nil {
		return "", err
	}
	if len(decoded) != 32 {
		return "", fmt.Errorf("invalid private key length")
	}

	pubBytes, err := curve25519.X25519(decoded, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pubBytes), nil
}

func toAdminAccountRows(items []domain.AdminAccountSummary) []adminAccountRow {
	out := make([]adminAccountRow, 0, len(items))
	for _, item := range items {
		periodEnd := "n/a"
		if item.CurrentPeriodEnd != nil {
			periodEnd = formatTS(*item.CurrentPeriodEnd)
		}
		lastSeen := "never"
		if item.LastSeenAt != nil {
			lastSeen = relativeTime(*item.LastSeenAt)
		}
		out = append(out, adminAccountRow{
			AccountNumber:    item.AccountNumber,
			SupabaseUserID:   item.SupabaseUserID,
			Status:           item.Status,
			PaymentStatus:    item.PaymentStatus,
			Plan:             item.Plan,
			Expiry:           formatTS(item.Expiry),
			CurrentPeriodEnd: periodEnd,
			DeviceCount:      item.DeviceCount,
			LastSeen:         lastSeen,
			BandwidthUsed:    formatBytes(item.RxBytesTotal + item.TxBytesTotal),
			UpdatedAt:        formatTS(item.UpdatedAt),
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
		lastApply := strings.TrimSpace(item.LastApply)
		if lastApply == "" {
			lastApply = "n/a"
		}
		if item.LastApplyAt != nil {
			lastApply = lastApply + " @ " + formatTS(*item.LastApplyAt)
		}
		statusClass := "status-unknown"
		status := strings.ToLower(strings.TrimSpace(item.LastStatus))
		if status == "healthy" || status == "ok" {
			statusClass = "status-ok"
		}
		if status == "failed" || status == "error" || status == "degraded" {
			statusClass = "status-bad"
		}
		heartbeatRelative := "never"
		if item.LastHeartbeat != nil {
			heartbeatRelative = relativeTime(*item.LastHeartbeat)
		}
		load := "n/a"
		if item.ConfiguredPeers > 0 {
			percent := int((float64(item.ConnectedPeers) / float64(item.ConfiguredPeers)) * 100)
			load = fmt.Sprintf("%d/%d (%d%%)", item.ConnectedPeers, item.ConfiguredPeers, percent)
		} else if item.ConnectedPeers > 0 {
			load = fmt.Sprintf("%d connected", item.ConnectedPeers)
		}

		out = append(out, adminGatewayRow{
			Hostname:         item.Hostname,
			Region:           item.Region,
			Provider:         item.Provider,
			WGPort:           item.WGPort,
			Active:           fmt.Sprintf("%t", item.Active),
			PublicIPv4:       item.PublicIPv4,
			PublicIPv6:       item.PublicIPv6,
			LastStatus:       item.LastStatus,
			LastStatusClass:  statusClass,
			LastHeartbeat:    heartbeat,
			LastHeartbeatRel: heartbeatRelative,
			LastApply:        lastApply,
			Load:             load,
		})
	}
	return out
}

func toAdminDeviceRows(items []domain.AdminDeviceSummary) []adminDeviceRow {
	out := make([]adminDeviceRow, 0, len(items))
	for _, item := range items {
		lastSeen := "never"
		if item.LastSeenAt != nil {
			lastSeen = relativeTime(*item.LastSeenAt)
		}
		connectionText := "offline"
		connectionClass := "status-bad"
		if item.Connected {
			connectionText = "connected"
			connectionClass = "status-ok"
		}
		relay := strings.TrimSpace(item.RelayHostname)
		if relay == "" {
			relay = "n/a"
		}
		out = append(out, adminDeviceRow{
			AccountNumber:   item.AccountNumber,
			DeviceID:        item.ID,
			DeviceName:      item.Name,
			IPv4Address:     item.IPv4Address,
			RelayHostname:   relay,
			LastSeen:        lastSeen,
			BandwidthUsed:   formatBytes(item.RxBytes + item.TxBytes),
			ConnectionText:  connectionText,
			ConnectionClass: connectionClass,
			CreatedAt:       formatTS(item.CreatedAt),
			DownloadURL:     fmt.Sprintf("/admin/wireguard-config/%s/%s", item.AccountNumber, item.ID),
			QRURL:           fmt.Sprintf("/admin/wireguard-qr/%s/%s", item.AccountNumber, item.ID),
		})
	}
	return out
}

func countHealthyGateways(items []domain.AdminGatewaySummary) int {
	total := 0
	for _, item := range items {
		if !item.Active {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(item.LastStatus))
		if status == "healthy" || status == "ok" {
			total++
		}
	}
	return total
}

func formatTS(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	return t.UTC().Format(time.RFC3339)
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "n/a"
	}
	delta := time.Since(t.UTC())
	if delta < 0 {
		delta = -delta
	}
	if delta < time.Minute {
		return "just now"
	}
	if delta < time.Hour {
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	}
	if delta < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
}

func formatBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(n)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value = value / 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d %s", int64(value), units[unit])
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}

func countConnectedDevices(items []domain.AdminDeviceSummary) int {
	total := 0
	for _, item := range items {
		if item.Connected {
			total++
		}
	}
	return total
}

func countPaidAccounts(items []domain.AdminAccountSummary) int {
	total := 0
	for _, item := range items {
		status := strings.ToLower(strings.TrimSpace(item.PaymentStatus))
		if status == "active" || status == "trialing" || status == "past_due" {
			total++
		}
	}
	return total
}

func totalTrafficBytes(items []domain.AdminAccountSummary) int64 {
	var total int64
	for _, item := range items {
		total += item.RxBytesTotal + item.TxBytesTotal
	}
	return total
}

func summarizeGatewayUtilization(items []domain.AdminGatewaySummary) string {
	var configured int64
	var connected int64
	for _, item := range items {
		configured += item.ConfiguredPeers
		connected += item.ConnectedPeers
	}
	if configured <= 0 {
		if connected <= 0 {
			return "n/a"
		}
		return fmt.Sprintf("%d connected", connected)
	}
	percent := int((float64(connected) / float64(configured)) * 100)
	return fmt.Sprintf("%d/%d (%d%%)", connected, configured, percent)
}
