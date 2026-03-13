# 03 - Data Model (Supabase Postgres)

## Core Tables

### `accounts`

- `id` (uuid, pk)
- `account_number` (text, unique, indexed)
- `supabase_user_id` (uuid, unique, indexed)
- `status` (text: active, suspended, deleted)
- `expiry_at` (timestamptz)
- `created_at`, `updated_at`

### `subscriptions`

- `id` (uuid, pk)
- `account_id` (uuid, fk accounts)
- `Paddle_customer_id` (text, indexed)
- `Paddle_subscription_id` (text, indexed)
- `status` (text)
- `current_period_end` (timestamptz)
- `last_webhook_event_id` (text, unique)
- `created_at`, `updated_at`

### `devices`

- `id` (uuid, pk)
- `account_id` (uuid, fk accounts)
- `name` (text)
- `pubkey` (text, unique)
- `hijack_dns` (boolean)
- `ipv4_address` (cidr)
- `ipv6_address` (cidr)
- `created_at`, `updated_at`
- Unique composite: `(account_id, name)`

### `relays`

- `id` (uuid, pk)
- `region` (text)
- `hostname` (text, unique)
- `public_ipv4` (inet)
- `public_ipv6` (inet, nullable)
- `wg_port` (int)
- `active` (boolean)
- `weight` (int)
- `provider` (text)
- `created_at`, `updated_at`

### `relay_heartbeats`

- `relay_id` (uuid, fk relays)
- `received_at` (timestamptz, indexed)
- `status` (text)
- `metrics` (jsonb)

### `gateway_apply_events`

- `id` (uuid, pk)
- `relay_id` (uuid, fk relays)
- `desired_version` (bigint)
- `result` (text: success, partial, failed)
- `error_text` (text, nullable)
- `created_at`

### `audit_events`

- `id` (uuid, pk)
- `actor_type` (text: user, service, gateway)
- `actor_id` (text)
- `action` (text)
- `entity_type` (text)
- `entity_id` (text)
- `payload` (jsonb)
- `created_at` (timestamptz, indexed)

## Row-Level Security

- Enable RLS for user-facing tables.
- Service role bypasses RLS for backend admin writes.
- User JWT policy: account-scoped row access only.

## Idempotency

- Paddle webhooks: dedupe on `last_webhook_event_id`.
- Gateway apply events: dedupe by `(relay_id, desired_version)`.


