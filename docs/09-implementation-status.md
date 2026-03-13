# 09 - Implementation Status

## Implemented

- Go module and entrypoints for control-plane and gateway-agent.
- Public compatibility routes and internal gateway routes are wired.
- Auth supports:
  - Supabase JWT bearer validation (`sub` required).
  - Compatibility bearer tokens issued by `POST /auth/v1/token`.
- Account resolution works for both auth modes:
  - Supabase user binding (`supabase_user_id`).
  - Account-number binding (compat token claim).
- SQL-backed Postgres store for:
  - account upsert/lookup
  - device CRUD
  - relay list retrieval + ETag
  - gateway register/heartbeat/apply-result
  - entitlement updates from billing events
- Paddle webhook verification + broader payload normalization for common event variants.
- Billing webhook replay/idempotency tracking via `billing_webhook_events`.
- Device IP allocation upgraded to slot-based allocation with global uniqueness indexes.
- In-memory rate limiting for token issuance and webhook endpoints.
- Audit event persistence for key actions (token issuance, device changes, gateway events, billing webhook processing).
- Admin dashboard with master-password login and gateway/account visibility (`/admin`).
- Mem0 client now performs best-effort HTTP ingestion when `MEM0_API_KEY` is set.
- Gateway-agent performs runtime apply flow (`ip` + `wg syncconf`) with apply-result posting.
- Migration set includes:
  - `001_init.sql`
  - `002_billing_provider_and_entitlements.sql` (legacy-safe backfill logic)
  - `003_relays_public_key.sql`
  - `004_webhook_idempotency_and_ip_pool.sql`

## Remaining Work

- Add RLS policies and service-role hardening for Supabase tables.
- Add production secret manager integration (instead of env-only secrets).
- Add full contract test matrix against real Mullvad client behavior.
- Add distributed rate limiting (Redis/edge) and richer audit payload coverage.
- Expand Terraform to additional providers (Vultr and Hetzner environments are implemented; AWS/DO still pending).
- Add CI pipeline for lint, test, build, and migration checks.

## Verification Snapshot

- `go build ./...` passes.
- `go test ./...` passes for current repository tests.
- API behavior is functional for the implemented MVP flow, but not yet production-complete.
