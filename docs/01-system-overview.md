# 01 - System Overview

## Target Architecture

The platform has five logical domains:

1. Identity and Entitlement
2. Account and Device Management
3. Relay and Gateway Orchestration
4. Session and Audit Tracking
5. Operations and Support Memory

## Core Services

- `identity-entitlement` (Go)
  - Validates Supabase auth context
  - Maps customer subscription state to VPN entitlements
  - Issues short-lived access tokens for compatibility endpoints

- `device-service` (Go)
  - Manages account-device lifecycle
  - Allocates WireGuard addresses
  - Rotates and validates WireGuard keys

- `relay-service` (Go)
  - Publishes relay list in Mullvad-compatible schema
  - Manages ETag versioning for relay payloads
  - Tracks relay health and availability

- `session-audit-service` (Go)
  - Stores account actions, gateway apply events, and security events
  - Provides immutable audit rows for incident and abuse review

- `gateway-agent` (Go, runs on gateways)
  - Registers to control plane
  - Fetches desired config
  - Applies WG peer changes
  - Sends heartbeat and apply result telemetry

## High-Level Data Flow

1. User signs in through Supabase Auth.
2. API receives JWT, resolves entitlement from DB.
3. Device creation/rotation updates peer records and config state.
4. Relay list endpoint serves current healthy gateways.
5. Gateway agents pull desired config and apply via local WG tooling.
6. Paddle webhooks update entitlement state and trigger access changes.
7. Mem0 receives sanitized support context from events and workflows.

## Canonical Data Ownership

- Canonical state: Supabase Postgres
- Source of auth truth: Supabase Auth
- Source of billing truth: Paddle + webhook event ledger in Postgres
- Source of support memory: Mem0 (advisory only, not canonical)

## Security Boundaries

- Public API only accepts authenticated requests except explicit public routes.
- Internal gateway API uses service-to-service auth (signed machine tokens).
- Paddle webhooks require signature validation and idempotency guard.
- Mem0 ingestion strips secrets/tokens before write.


