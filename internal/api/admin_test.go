package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
