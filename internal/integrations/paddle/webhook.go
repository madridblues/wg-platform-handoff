package paddle

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"wg-platform-handoff/internal/domain"
)

type Verifier struct {
	secret string
}

func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: strings.TrimSpace(secret)}
}

func (v *Verifier) VerifyAndNormalizeEvent(r *http.Request) (domain.BillingEvent, error) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		return domain.BillingEvent{}, err
	}

	if v.secret == "" {
		// Dev fallback.
		return domain.BillingEvent{
			Provider:  "paddle",
			EventID:   "dev-" + uuid.NewString(),
			EventType: "transaction.completed",
			Status:    "active",
			Raw:       map[string]any{"raw": string(payload)},
		}, nil
	}

	signature := r.Header.Get("Paddle-Signature")
	if signature == "" {
		return domain.BillingEvent{}, errors.New("missing paddle signature")
	}

	if !verifySignature(payload, signature, v.secret) {
		return domain.BillingEvent{}, errors.New("invalid paddle signature")
	}

	return normalizePayload(payload)
}

func verifySignature(payload []byte, signatureHeader, secret string) bool {
	parts := map[string]string{}
	tokens := strings.FieldsFunc(signatureHeader, func(r rune) bool {
		return r == ';' || r == ','
	})
	for _, token := range tokens {
		item := strings.SplitN(strings.TrimSpace(token), "=", 2)
		if len(item) != 2 {
			continue
		}
		parts[strings.TrimSpace(item[0])] = strings.TrimSpace(item[1])
	}

	ts := parts["ts"]
	h1 := strings.ToLower(parts["h1"])
	if ts == "" || h1 == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(ts + ":" + string(payload)))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(h1))
}

func normalizePayload(payload []byte) (domain.BillingEvent, error) {
	envelope := map[string]any{}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return domain.BillingEvent{}, err
	}

	eventType := firstNonEmpty(
		asString(envelope["event_type"]),
		asString(envelope["alert_name"]),
		asString(envelope["type"]),
	)
	eventID := firstNonEmpty(
		asString(envelope["event_id"]),
		asString(envelope["notification_id"]),
		asString(envelope["id"]),
	)

	data := mapFromAny(envelope["data"])
	subscription := mapFromAny(data["subscription"])
	transaction := mapFromAny(data["transaction"])
	customer := mapFromAny(data["customer"])

	customData := mergedMaps(
		mapFromAny(envelope["custom_data"]),
		mapFromAny(data["custom_data"]),
		mapFromAny(subscription["custom_data"]),
		mapFromAny(transaction["custom_data"]),
		parsePassthrough(data["passthrough"]),
		parsePassthrough(subscription["passthrough"]),
		parsePassthrough(transaction["passthrough"]),
	)

	statusRaw := firstNonEmpty(
		asString(data["status"]),
		asString(subscription["status"]),
		asString(transaction["status"]),
	)

	normalized := domain.BillingEvent{
		Provider: "paddle",
		EventID:  eventID,
		EventType: firstNonEmpty(
			eventType,
			"unknown",
		),
		AccountNumber: firstNonEmpty(
			asString(customData["account_number"]),
			asString(customData["account"]),
			asString(customData["mullvad_account"]),
		),
		CustomerID: firstNonEmpty(
			asString(data["customer_id"]),
			asString(customer["id"]),
			asString(transaction["customer_id"]),
			asString(subscription["customer_id"]),
		),
		SubscriptionID: firstNonEmpty(
			asString(data["subscription_id"]),
			asString(subscription["id"]),
			asString(transaction["subscription_id"]),
			asString(transaction["id"]),
			asString(data["id"]),
		),
		Status: normalizeStatus(eventType, statusRaw),
		Raw:    envelope,
	}

	periodCandidates := []string{
		asString(pathLookup(data, "current_billing_period", "ends_at")),
		asString(pathLookup(subscription, "current_billing_period", "ends_at")),
		asString(pathLookup(data, "billing_period", "ends_at")),
		asString(data["next_billed_at"]),
		asString(subscription["next_billed_at"]),
	}
	if periodEnd, ok := firstRFC3339(periodCandidates...); ok {
		normalized.CurrentPeriodEnd = &periodEnd
	}

	return normalized, nil
}

func normalizeStatus(eventType, status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if normalized != "" {
		return normalized
	}

	event := strings.ToLower(strings.TrimSpace(eventType))
	switch {
	case strings.Contains(event, "past_due"), strings.Contains(event, "payment_failed"):
		return "past_due"
	case strings.Contains(event, "paused"):
		return "paused"
	case strings.Contains(event, "resumed"), strings.Contains(event, "transaction.completed"), strings.Contains(event, "subscription.created"), strings.Contains(event, "subscription.updated"):
		return "active"
	case strings.Contains(event, "canceled"), strings.Contains(event, "cancelled"), strings.Contains(event, "deleted"), strings.Contains(event, "expired"):
		return "canceled"
	default:
		return "unknown"
	}
}

func parsePassthrough(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		decoded := map[string]any{}
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			return decoded
		}
	}
	return nil
}

func mergedMaps(parts ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, part := range parts {
		for key, value := range part {
			out[key] = value
		}
	}
	return out
}

func pathLookup(root map[string]any, keys ...string) any {
	current := any(root)
	for _, key := range keys {
		typed, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = typed[key]
	}
	return current
}

func mapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func firstRFC3339(values ...string) (time.Time, bool) {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, trimmed)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func asString(value any) string {
	if v, ok := value.(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
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
