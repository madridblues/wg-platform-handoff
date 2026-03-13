package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wg-platform-handoff/internal/config"
	"wg-platform-handoff/internal/domain"
	"wg-platform-handoff/internal/integrations/compat"
	"wg-platform-handoff/internal/integrations/mem0"
	"wg-platform-handoff/internal/integrations/paddle"
	"wg-platform-handoff/internal/integrations/supabase"
)

type fakeStore struct {
	account domain.Account
	devices []domain.Device
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		account: domain.Account{
			ID:     "acc-1",
			Number: "ACC0001",
			Status: "active",
			Expiry: time.Now().UTC().Add(30 * 24 * time.Hour),
		},
		devices: []domain.Device{},
	}
}

func (s *fakeStore) GetOrCreateAccountByUserID(_ context.Context, _ string) (domain.Account, error) {
	return s.account, nil
}

func (s *fakeStore) GetAccountByNumber(_ context.Context, accountNumber string) (domain.Account, error) {
	if strings.EqualFold(strings.TrimSpace(accountNumber), s.account.Number) {
		return s.account, nil
	}
	return domain.Account{}, errors.New("not found")
}

func (s *fakeStore) ListDevices(_ context.Context, _ string) ([]domain.Device, error) {
	out := make([]domain.Device, len(s.devices))
	copy(out, s.devices)
	return out, nil
}

func (s *fakeStore) CreateDevice(_ context.Context, _ string, pubkey string, hijackDNS bool) (domain.Device, error) {
	d := domain.Device{
		ID:          "dev-1",
		Name:        "device-1",
		PubKey:      pubkey,
		HijackDNS:   hijackDNS,
		Created:     time.Now().UTC(),
		IPv4Address: "10.64.0.2/32",
		IPv6Address: "fd00::2/128",
	}
	s.devices = append(s.devices, d)
	return d, nil
}

func (s *fakeStore) GetDevice(_ context.Context, _ string, deviceID string) (domain.Device, error) {
	for _, d := range s.devices {
		if d.ID == deviceID {
			return d, nil
		}
	}
	return domain.Device{}, context.Canceled
}

func (s *fakeStore) ReplaceDeviceKey(_ context.Context, _ string, deviceID, pubkey string) (domain.Device, error) {
	for i, d := range s.devices {
		if d.ID == deviceID {
			d.PubKey = pubkey
			s.devices[i] = d
			return d, nil
		}
	}
	return domain.Device{}, context.Canceled
}

func (s *fakeStore) DeleteDevice(_ context.Context, _ string, deviceID string) error {
	for i, d := range s.devices {
		if d.ID == deviceID {
			s.devices = append(s.devices[:i], s.devices[i+1:]...)
			return nil
		}
	}
	return context.Canceled
}

func (s *fakeStore) CurrentRelayList(_ context.Context) (domain.RelayListResponse, string, error) {
	return domain.RelayListResponse{
		Locations: map[string]domain.RelayLocation{"uk-lon": {City: "London", Country: "UK"}},
		Wireguard: domain.WireguardBlock{Relays: []domain.WireguardRelayEntry{{Hostname: "gw-lon-1", Active: true, Location: "uk-lon", PublicKey: "pub"}}},
		Bridge:    domain.BridgeBlock{},
	}, `W/"relay-v1"`, nil
}

func (s *fakeStore) RegisterGateway(_ context.Context, _ domain.GatewayRegisterRequest) error {
	return nil
}

func (s *fakeStore) DesiredGatewayConfig(_ context.Context, gatewayID string) (domain.GatewayDesiredConfigResponse, error) {
	return domain.GatewayDesiredConfigResponse{Version: 1, Relay: map[string]string{"gateway_id": gatewayID}}, nil
}

func (s *fakeStore) HeartbeatGateway(_ context.Context, _ string, _ domain.GatewayHeartbeatRequest) error {
	return nil
}

func (s *fakeStore) RecordGatewayApplyResult(_ context.Context, _ string, _ domain.GatewayApplyResultRequest) error {
	return nil
}

func (s *fakeStore) ApplyBillingEvent(_ context.Context, _ domain.BillingEvent) error {
	return nil
}

func (s *fakeStore) RecordAuditEvent(_ context.Context, _ domain.AuditEvent) error {
	return nil
}

func (s *fakeStore) AdminListAccounts(_ context.Context, limit int) ([]domain.AdminAccountSummary, error) {
	rows := []domain.AdminAccountSummary{
		{
			ID:             s.account.ID,
			AccountNumber:  s.account.Number,
			SupabaseUserID: "dev-user",
			Status:         s.account.Status,
			Expiry:         s.account.Expiry,
			UpdatedAt:      time.Now().UTC(),
			DeviceCount:    int64(len(s.devices)),
		},
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (s *fakeStore) AdminListGateways(_ context.Context, limit int) ([]domain.AdminGatewaySummary, error) {
	now := time.Now().UTC()
	rows := []domain.AdminGatewaySummary{
		{
			ID:            "relay-1",
			Hostname:      "gw-lon-1",
			Region:        "uk-lon",
			Provider:      "self",
			Active:        true,
			PublicIPv4:    "203.0.113.10",
			WGPublicKey:   "pub",
			LastStatus:    "healthy",
			LastHeartbeat: &now,
		},
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (s *fakeStore) AdminListDevices(_ context.Context, limit int) ([]domain.AdminDeviceSummary, error) {
	rows := make([]domain.AdminDeviceSummary, 0, len(s.devices))
	for _, d := range s.devices {
		rows = append(rows, domain.AdminDeviceSummary{
			ID:            d.ID,
			AccountID:     s.account.ID,
			AccountNumber: s.account.Number,
			Name:          d.Name,
			PubKey:        d.PubKey,
			HijackDNS:     d.HijackDNS,
			CreatedAt:     d.Created,
			IPv4Address:   d.IPv4Address,
			IPv6Address:   d.IPv6Address,
		})
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (s *fakeStore) AdminGetDeviceByAccountNumber(_ context.Context, accountNumber, deviceID string) (domain.AdminDeviceSummary, error) {
	if !strings.EqualFold(strings.TrimSpace(accountNumber), s.account.Number) {
		return domain.AdminDeviceSummary{}, errors.New("not found")
	}
	for _, d := range s.devices {
		if d.ID == deviceID {
			return domain.AdminDeviceSummary{
				ID:            d.ID,
				AccountID:     s.account.ID,
				AccountNumber: s.account.Number,
				Name:          d.Name,
				PubKey:        d.PubKey,
				HijackDNS:     d.HijackDNS,
				CreatedAt:     d.Created,
				IPv4Address:   d.IPv4Address,
				IPv6Address:   d.IPv6Address,
			}, nil
		}
	}
	return domain.AdminDeviceSummary{}, errors.New("not found")
}

func buildTestRouter(gatewayToken string) http.Handler {
	cfg := config.Config{
		GatewayToken:      gatewayToken,
		CompatTokenSecret: "test-compat-secret",
		CompatTokenTTL:    1 * time.Hour,
	}
	return NewRouterWithDeps(
		cfg,
		newFakeStore(),
		supabase.NewVerifier(""),
		compat.NewManager(cfg.CompatTokenSecret, cfg.CompatTokenTTL),
		paddle.NewVerifier(""),
		mem0.NewClient(""),
	)
}

func issueAccessToken(t *testing.T, router http.Handler) string {
	t.Helper()

	body := map[string]any{"account_number": "ACC0001"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/v1/token", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 on /auth/v1/token, got %d: %s", res.Code, res.Body.String())
	}

	var payload domain.AccessTokenResponse
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode token payload: %v", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		t.Fatalf("expected non-empty access token")
	}

	return payload.AccessToken
}

func TestHealthEndpoint(t *testing.T) {
	router := buildTestRouter("internal-secret")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
}

func TestAccountsMeRequiresBearerToken(t *testing.T) {
	router := buildTestRouter("internal-secret")
	req := httptest.NewRequest(http.MethodGet, "/accounts/v1/accounts/me", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}
}

func TestCreateAndListDevicesFlow(t *testing.T) {
	router := buildTestRouter("internal-secret")
	token := issueAccessToken(t, router)

	createBody := map[string]any{"pubkey": "test-pubkey", "hijack_dns": false}
	b, _ := json.Marshal(createBody)

	createReq := httptest.NewRequest(http.MethodPost, "/accounts/v1/devices", bytes.NewReader(b))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRes := httptest.NewRecorder()
	router.ServeHTTP(createRes, createReq)

	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create, got %d: %s", createRes.Code, createRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/accounts/v1/devices", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listRes := httptest.NewRecorder()
	router.ServeHTTP(listRes, listReq)

	if listRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d: %s", listRes.Code, listRes.Body.String())
	}
	if !strings.Contains(listRes.Body.String(), "test-pubkey") {
		t.Fatalf("expected list response to contain created pubkey, got %s", listRes.Body.String())
	}
}

func TestReplaceDeviceKeyWithCompatRoute(t *testing.T) {
	router := buildTestRouter("internal-secret")
	token := issueAccessToken(t, router)

	createBody := map[string]any{"pubkey": "initial-pubkey", "hijack_dns": false}
	b, _ := json.Marshal(createBody)

	createReq := httptest.NewRequest(http.MethodPost, "/accounts/v1/devices", bytes.NewReader(b))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRes := httptest.NewRecorder()
	router.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("expected 201 on create, got %d: %s", createRes.Code, createRes.Body.String())
	}

	replaceBody := map[string]any{"pubkey": "rotated-pubkey"}
	rb, _ := json.Marshal(replaceBody)
	replaceReq := httptest.NewRequest(http.MethodPut, "/accounts/v1/devices/dev-1", bytes.NewReader(rb))
	replaceReq.Header.Set("Authorization", "Bearer "+token)
	replaceRes := httptest.NewRecorder()
	router.ServeHTTP(replaceRes, replaceReq)

	if replaceRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on key replace, got %d: %s", replaceRes.Code, replaceRes.Body.String())
	}
}

func TestCreateAccessTokenRejectsUnknownAccount(t *testing.T) {
	router := buildTestRouter("internal-secret")

	body := map[string]any{"account_number": "UNKNOWN999"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/v1/token", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown account, got %d: %s", res.Code, res.Body.String())
	}
}

func TestRelaysETag304(t *testing.T) {
	router := buildTestRouter("internal-secret")

	firstReq := httptest.NewRequest(http.MethodGet, "/app/v1/relays", nil)
	firstRes := httptest.NewRecorder()
	router.ServeHTTP(firstRes, firstReq)

	if firstRes.Code != http.StatusOK {
		t.Fatalf("expected first relays call 200, got %d", firstRes.Code)
	}

	eTag := firstRes.Header().Get("ETag")
	if eTag == "" {
		t.Fatalf("expected ETag header on first call")
	}

	secondReq := httptest.NewRequest(http.MethodGet, "/app/v1/relays", nil)
	secondReq.Header.Set("If-None-Match", eTag)
	secondRes := httptest.NewRecorder()
	router.ServeHTTP(secondRes, secondReq)

	if secondRes.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", secondRes.Code)
	}
}

func TestInternalGatewayAuth(t *testing.T) {
	router := buildTestRouter("internal-secret")

	req := httptest.NewRequest(http.MethodPost, "/internal/gateways/register", strings.NewReader(`{"id":"gw1"}`))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without internal token, got %d", res.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/internal/gateways/register", strings.NewReader(`{"id":"gw1"}`))
	req2.Header.Set("X-Gateway-Token", "internal-secret")
	res2 := httptest.NewRecorder()
	router.ServeHTTP(res2, req2)

	if res2.Code != http.StatusCreated {
		t.Fatalf("expected 201 with internal token, got %d", res2.Code)
	}
}
