# 05 - Auth, Billing, and Mem0

## Supabase Auth Integration

## Auth Model

- End users authenticate with Supabase Auth.
- Backend validates JWT and resolves account binding via `sub`.
- Compatibility token endpoint issues short-lived access token used by Mullvad-like routes.

## Auth Flows Required

- Signup and email verification
- Login + refresh
- Password reset
- MFA where enabled

## Billing Integration (Paddle)

### Source of Billing Truth

- Paddle subscription events + webhook signatures.
- Persist normalized entitlement state in Postgres (`subscriptions` + `accounts`).

### Webhook Endpoint

- `POST /webhooks/paddle`

### Cloudflare Functions Edge Ingress

- Optional edge ingress is provided under `deploy/cloudflare`.
- Receives provider webhooks and forwards to control-plane endpoints.
- Preserves provider signature headers.

### Required Paddle Webhooks

- `transaction.completed`
- `subscription.created`
- `subscription.updated`
- `subscription.canceled`
- `subscription.paused`
- `subscription.resumed`

### Entitlement Logic

- `active/trialing/past_due` -> account allowed for device/tunnel operations.
- Any non-active terminal status -> account suspended.
- `expiry_at` is updated from billing period end when available.

## Mem0 Integration

Mem0 is optional contextual memory for support/operations workflows.

### Allowed Use Cases

- Store customer troubleshooting timeline summaries
- Store runbook decisions and incident context
- Improve support assistant context continuity

### Disallowed Use Cases

- Never store private keys, access tokens, Paddle secrets, or raw JWTs
- Never use Mem0 as canonical entitlement/account state

### Data Hygiene Rules

- Redact secrets before ingestion
- Attach retention policy and deletion path
- Log write/read calls for auditability