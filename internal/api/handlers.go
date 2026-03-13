package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"wg-platform-handoff/internal/api/middleware"
	"wg-platform-handoff/internal/domain"
	"wg-platform-handoff/internal/integrations/compat"
	"wg-platform-handoff/internal/integrations/mem0"
	"wg-platform-handoff/internal/integrations/paddle"
	"wg-platform-handoff/internal/store"
)

var (
	errUnauthenticated = errors.New("missing authenticated user")
	errEntitlement     = errors.New("account not entitled")
	accountNumberRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{4,32}$`)
)

type Handler struct {
	store  store.Store
	paddle *paddle.Verifier
	mem0   *mem0.Client
	compat *compat.Manager
}

func NewHandler(store store.Store, paddleVerifier *paddle.Verifier, mem0Client *mem0.Client, compatManager *compat.Manager) *Handler {
	return &Handler{store: store, paddle: paddleVerifier, mem0: mem0Client, compat: compatManager}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) CreateAccessToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountNumber string `json:"account_number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AccountNumber == "" {
		writeCompatibilityError(w, http.StatusBadRequest, "INVALID_ACCOUNT")
		return
	}

	accountNumber := strings.ToUpper(strings.TrimSpace(req.AccountNumber))
	if !accountNumberRegex.MatchString(accountNumber) {
		writeCompatibilityError(w, http.StatusBadRequest, "INVALID_ACCOUNT")
		return
	}

	account, err := h.store.GetAccountByNumber(r.Context(), accountNumber)
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCOUNT")
		return
	}

	if h.compat == nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED")
		return
	}

	token, expiry, err := h.compat.Issue(account.Number)
	if err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "TOKEN_ISSUE_FAILED")
		return
	}
	h.recordAudit(r.Context(), domain.AuditEvent{
		ActorType:  "account",
		ActorID:    account.Number,
		Action:     "auth.token_issued",
		EntityType: "account",
		EntityID:   account.ID,
		Payload: map[string]any{
			"mode": "compat",
		},
	})

	writeJSON(w, http.StatusOK, domain.AccessTokenResponse{
		AccessToken: token,
		Expiry:      expiry,
	})
}

func (h *Handler) GetAccountMe(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"number": account.Number})
}

func (h *Handler) DeleteAccountMe(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}

	devices, err := h.store.ListDevices(r.Context(), account.ID)
	if err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "DEVICE_LIST_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

func (h *Handler) CreateDevice(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}
	if err := ensureEntitled(account); err != nil {
		writeCompatibilityError(w, http.StatusPaymentRequired, "ACCOUNT_NOT_ENTITLED")
		return
	}

	var req struct {
		PubKey    string `json:"pubkey"`
		HijackDNS bool   `json:"hijack_dns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PubKey == "" {
		writeCompatibilityError(w, http.StatusBadRequest, "INVALID_PUBKEY")
		return
	}

	d, err := h.store.CreateDevice(r.Context(), account.ID, req.PubKey, req.HijackDNS)
	if err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "DEVICE_CREATE_FAILED")
		return
	}
	h.recordAudit(r.Context(), domain.AuditEvent{
		ActorType:  "account",
		ActorID:    account.ID,
		Action:     "device.created",
		EntityType: "device",
		EntityID:   d.ID,
		Payload: map[string]any{
			"pubkey":     req.PubKey,
			"hijack_dns": req.HijackDNS,
		},
	})
	writeJSON(w, http.StatusCreated, d)
}

func (h *Handler) GetDevice(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}

	id := r.PathValue("id")
	d, err := h.store.GetDevice(r.Context(), account.ID, id)
	if err != nil {
		writeCompatibilityError(w, http.StatusNotFound, "DEVICE_NOT_FOUND")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) ReplaceDeviceKey(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}
	if err := ensureEntitled(account); err != nil {
		writeCompatibilityError(w, http.StatusPaymentRequired, "ACCOUNT_NOT_ENTITLED")
		return
	}

	id := r.PathValue("id")
	var req struct {
		PubKey string `json:"pubkey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PubKey == "" {
		writeCompatibilityError(w, http.StatusBadRequest, "INVALID_PUBKEY")
		return
	}

	d, err := h.store.ReplaceDeviceKey(r.Context(), account.ID, id, req.PubKey)
	if err != nil {
		writeCompatibilityError(w, http.StatusNotFound, "DEVICE_NOT_FOUND")
		return
	}
	h.recordAudit(r.Context(), domain.AuditEvent{
		ActorType:  "account",
		ActorID:    account.ID,
		Action:     "device.pubkey_replaced",
		EntityType: "device",
		EntityID:   d.ID,
		Payload: map[string]any{
			"pubkey": req.PubKey,
		},
	})
	writeJSON(w, http.StatusOK, map[string]string{
		"ipv4_address": d.IPv4Address,
		"ipv6_address": d.IPv6Address,
	})
}

func (h *Handler) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}

	id := r.PathValue("id")
	if err := h.store.DeleteDevice(r.Context(), account.ID, id); err != nil {
		writeCompatibilityError(w, http.StatusNotFound, "DEVICE_NOT_FOUND")
		return
	}
	h.recordAudit(r.Context(), domain.AuditEvent{
		ActorType:  "account",
		ActorID:    account.ID,
		Action:     "device.deleted",
		EntityType: "device",
		EntityID:   id,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetRelays(w http.ResponseWriter, r *http.Request) {
	relayList, etag, err := h.store.CurrentRelayList(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "RELAY_LIST_FAILED")
		return
	}

	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("ETag", etag)
	writeJSON(w, http.StatusOK, relayList)
}

func (h *Handler) GetAPIAddrs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []string{"203.0.113.10:443"})
}

func (h *Handler) HeadAPIAddrs(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) GetWWWAuthToken(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}
	if err := ensureEntitled(account); err != nil {
		writeCompatibilityError(w, http.StatusPaymentRequired, "ACCOUNT_NOT_ENTITLED")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"auth_token": uuid.NewString()})
}

func (h *Handler) SubmitVoucher(w http.ResponseWriter, r *http.Request) {
	account, err := h.authenticatedAccount(r.Context())
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_ACCESS_TOKEN")
		return
	}
	if err := ensureEntitled(account); err != nil {
		writeCompatibilityError(w, http.StatusPaymentRequired, "ACCOUNT_NOT_ENTITLED")
		return
	}

	writeJSON(w, http.StatusOK, domain.VoucherSubmission{
		TimeAdded: 2592000,
		NewExpiry: time.Now().UTC().Add(30 * 24 * time.Hour),
	})
}

func (h *Handler) ProblemReport(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	_ = json.NewDecoder(r.Body).Decode(&payload)
	_ = h.mem0.WriteEvent(r.Context(), map[string]any{
		"kind":    "problem_report",
		"payload": sanitize(payload),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RegisterGateway(w http.ResponseWriter, r *http.Request) {
	var req domain.GatewayRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCompatibilityError(w, http.StatusBadRequest, "INVALID_GATEWAY_REGISTER")
		return
	}
	if err := h.store.RegisterGateway(r.Context(), req); err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "GATEWAY_REGISTER_FAILED")
		return
	}
	h.recordAudit(r.Context(), domain.AuditEvent{
		ActorType:  "gateway",
		ActorID:    req.ID,
		Action:     "gateway.registered",
		EntityType: "relay",
		EntityID:   req.ID,
		Payload: map[string]any{
			"region": req.Region,
		},
	})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
}

func (h *Handler) GetDesiredGatewayConfig(w http.ResponseWriter, r *http.Request) {
	gatewayID := r.PathValue("id")
	cfg, err := h.store.DesiredGatewayConfig(r.Context(), gatewayID)
	if err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "DESIRED_CONFIG_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (h *Handler) GatewayHeartbeat(w http.ResponseWriter, r *http.Request) {
	gatewayID := r.PathValue("id")
	var req domain.GatewayHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCompatibilityError(w, http.StatusBadRequest, "INVALID_HEARTBEAT")
		return
	}
	if err := h.store.HeartbeatGateway(r.Context(), gatewayID, req); err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "HEARTBEAT_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) GatewayApplyResult(w http.ResponseWriter, r *http.Request) {
	gatewayID := r.PathValue("id")
	var req domain.GatewayApplyResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCompatibilityError(w, http.StatusBadRequest, "INVALID_APPLY_RESULT")
		return
	}
	if err := h.store.RecordGatewayApplyResult(r.Context(), gatewayID, req); err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "APPLY_RESULT_FAILED")
		return
	}
	h.recordAudit(r.Context(), domain.AuditEvent{
		ActorType:  "gateway",
		ActorID:    gatewayID,
		Action:     "gateway.apply_result",
		EntityType: "gateway_apply_event",
		EntityID:   gatewayID,
		Payload: map[string]any{
			"desired_version": req.DesiredVersion,
			"result":          req.Result,
			"error_text":      req.ErrorText,
		},
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) PaddleWebhook(w http.ResponseWriter, r *http.Request) {
	event, err := h.paddle.VerifyAndNormalizeEvent(r)
	if err != nil {
		writeCompatibilityError(w, http.StatusUnauthorized, "INVALID_PADDLE_SIGNATURE")
		return
	}

	if err := h.store.ApplyBillingEvent(r.Context(), event); err != nil {
		writeCompatibilityError(w, http.StatusInternalServerError, "BILLING_EVENT_FAILED")
		return
	}

	_ = h.mem0.WriteEvent(context.Background(), map[string]any{
		"kind":       "paddle_webhook",
		"event_id":   event.EventID,
		"event_type": event.EventType,
	})
	h.recordAudit(r.Context(), domain.AuditEvent{
		ActorType:  "billing_provider",
		ActorID:    "paddle",
		Action:     "billing.webhook_processed",
		EntityType: "billing_event",
		EntityID:   event.EventID,
		Payload: map[string]any{
			"event_type": event.EventType,
		},
	})

	writeJSON(w, http.StatusOK, map[string]string{"received": "true"})
}

func (h *Handler) authenticatedAccount(ctx context.Context) (domain.Account, error) {
	userID := middleware.SubjectFromContext(ctx)
	if userID == "" {
		accountNumber := middleware.AccountNumberFromContext(ctx)
		if accountNumber == "" {
			return domain.Account{}, errUnauthenticated
		}
		account, err := h.store.GetAccountByNumber(ctx, accountNumber)
		if err != nil {
			return domain.Account{}, err
		}
		return account, nil
	}

	account, err := h.store.GetOrCreateAccountByUserID(ctx, userID)
	if err != nil {
		return domain.Account{}, err
	}

	return account, nil
}

func ensureEntitled(account domain.Account) error {
	status := strings.ToLower(strings.TrimSpace(account.Status))
	if status == "" {
		status = "active"
	}

	if status != "active" && status != "trialing" && status != "past_due" {
		return errEntitlement
	}

	if !account.Expiry.IsZero() && account.Expiry.Before(time.Now().UTC()) {
		return errEntitlement
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeCompatibilityError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	writeJSON(w, status, map[string]string{"type": code})
}

func sanitize(m map[string]any) map[string]any {
	redacted := map[string]any{}
	for k, v := range m {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key") {
			redacted[k] = "[REDACTED]"
			continue
		}
		redacted[k] = v
	}
	return redacted
}

func (h *Handler) recordAudit(ctx context.Context, event domain.AuditEvent) {
	if h == nil || h.store == nil {
		return
	}

	if event.Payload != nil {
		event.Payload = sanitize(event.Payload)
	}

	_ = h.store.RecordAuditEvent(ctx, event)
}
