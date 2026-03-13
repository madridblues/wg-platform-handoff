package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wg-platform-handoff/internal/domain"
)

func TestAdminLoginAndDashboard(t *testing.T) {
	admin := NewAdminHandler(newFakeStore(), "topsecret", "session-secret", 1*time.Hour)

	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("password=topsecret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRes := httptest.NewRecorder()
	admin.LoginSubmit(loginRes, loginReq)

	if loginRes.Code != http.StatusSeeOther {
		t.Fatalf("expected login status 303, got %d", loginRes.Code)
	}

	cookies := loginRes.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == adminSessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil || strings.TrimSpace(sessionCookie.Value) == "" {
		t.Fatalf("expected admin session cookie")
	}

	dashboardReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	dashboardReq.AddCookie(sessionCookie)
	dashboardRes := httptest.NewRecorder()
	admin.Dashboard(dashboardRes, dashboardReq)

	if dashboardRes.Code != http.StatusOK {
		t.Fatalf("expected dashboard status 200, got %d", dashboardRes.Code)
	}
	if !strings.Contains(dashboardRes.Body.String(), "VPN Admin Dashboard") {
		t.Fatalf("expected dashboard page content")
	}
}

func TestAdminDashboardRedirectsWithoutSession(t *testing.T) {
	admin := NewAdminHandler(newFakeStore(), "topsecret", "session-secret", 1*time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	res := httptest.NewRecorder()
	admin.Dashboard(res, req)

	if res.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect without session, got %d", res.Code)
	}
	location := res.Header().Get("Location")
	if location != "/admin/login" {
		t.Fatalf("expected redirect to /admin/login, got %q", location)
	}
}

func TestDownloadWireGuardConfig(t *testing.T) {
	store := newFakeStore()
	store.devices = append(store.devices, domain.Device{
		ID:          "dev-1",
		Name:        "device-1",
		PubKey:      "ATOulp3th4vUrMoDc+MJ92SphcBSxmwnUQ0ChTcbVEU=",
		HijackDNS:   false,
		Created:     time.Now().UTC(),
		IPv4Address: "10.64.0.2/32",
		IPv6Address: "fd00::2/128",
	})

	admin := NewAdminHandler(store, "topsecret", "session-secret", 1*time.Hour)

	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("password=topsecret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRes := httptest.NewRecorder()
	admin.LoginSubmit(loginRes, loginReq)
	if loginRes.Code != http.StatusSeeOther {
		t.Fatalf("expected login status 303, got %d", loginRes.Code)
	}

	var sessionCookie *http.Cookie
	for _, cookie := range loginRes.Result().Cookies() {
		if cookie.Name == adminSessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected admin session cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/wireguard-config/ACC0001/dev-1?private_key=SGjouTg84AjrQtXgidUm6p7XlFi5c1rC4c%2BbSK25r10%3D", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("account", "ACC0001")
	req.SetPathValue("device", "dev-1")
	req.AddCookie(sessionCookie)

	res := httptest.NewRecorder()
	admin.DownloadWireGuardConfig(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "PrivateKey = SGjouTg84AjrQtXgidUm6p7XlFi5c1rC4c+bSK25r10=") {
		t.Fatalf("expected provided private key in config")
	}
	if !strings.Contains(res.Body.String(), "Endpoint = 203.0.113.10:51820") {
		t.Fatalf("expected gateway endpoint in config")
	}
}

func TestDownloadWireGuardQRCode(t *testing.T) {
	store := newFakeStore()
	store.devices = append(store.devices, domain.Device{
		ID:          "dev-1",
		Name:        "device-1",
		PubKey:      "ATOulp3th4vUrMoDc+MJ92SphcBSxmwnUQ0ChTcbVEU=",
		HijackDNS:   false,
		Created:     time.Now().UTC(),
		IPv4Address: "10.64.0.2/32",
		IPv6Address: "fd00::2/128",
	})

	admin := NewAdminHandler(store, "topsecret", "session-secret", 1*time.Hour)

	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("password=topsecret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRes := httptest.NewRecorder()
	admin.LoginSubmit(loginRes, loginReq)
	if loginRes.Code != http.StatusSeeOther {
		t.Fatalf("expected login status 303, got %d", loginRes.Code)
	}

	var sessionCookie *http.Cookie
	for _, cookie := range loginRes.Result().Cookies() {
		if cookie.Name == adminSessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected admin session cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/wireguard-qr/ACC0001/dev-1?private_key=SGjouTg84AjrQtXgidUm6p7XlFi5c1rC4c%2BbSK25r10%3D", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("account", "ACC0001")
	req.SetPathValue("device", "dev-1")
	req.AddCookie(sessionCookie)

	res := httptest.NewRecorder()
	admin.DownloadWireGuardQRCode(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}
	if ct := res.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("expected image/png content type, got %q", ct)
	}
	if body := res.Body.Bytes(); len(body) < 8 || string(body[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("expected PNG body")
	}
}

func TestGenerateAndSyncWireGuardKey(t *testing.T) {
	store := newFakeStore()
	store.devices = append(store.devices, domain.Device{
		ID:          "dev-1",
		Name:        "device-1",
		PubKey:      "old-pubkey",
		HijackDNS:   false,
		Created:     time.Now().UTC(),
		IPv4Address: "10.64.0.2/32",
		IPv6Address: "fd00::2/128",
	})
	admin := NewAdminHandler(store, "topsecret", "session-secret", 1*time.Hour)

	loginReq := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader("password=topsecret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRes := httptest.NewRecorder()
	admin.LoginSubmit(loginRes, loginReq)

	var sessionCookie *http.Cookie
	for _, cookie := range loginRes.Result().Cookies() {
		if cookie.Name == adminSessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected admin session cookie")
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/wireguard-key/ACC0001/dev-1/generate", nil)
	req = req.WithContext(context.Background())
	req.SetPathValue("account", "ACC0001")
	req.SetPathValue("device", "dev-1")
	req.AddCookie(sessionCookie)

	res := httptest.NewRecorder()
	admin.GenerateAndSyncWireGuardKey(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload["private_key"] == "" || payload["public_key"] == "" {
		t.Fatalf("expected generated keypair payload")
	}
	if store.devices[0].PubKey != payload["public_key"] {
		t.Fatalf("expected device pubkey to be synced")
	}
}
