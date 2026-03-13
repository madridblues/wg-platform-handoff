# Supabase-First WireGuard Platform Handoff

This repository contains the control-plane and gateway-agent MVP for a Mullvad-compatible WireGuard service.

## What Is Implemented

- Go project scaffold (`go.mod`, `cmd`, `internal`)
- API router with Mullvad-compatible routes
- Supabase JWT verifier with `sub` claim enforcement
- Compatibility token issuer/verifier for Mullvad-style account-number login
- SQL-backed Postgres store for accounts, devices, relays, gateway events, and billing state
- Paddle webhook normalization and entitlement updates
- Billing webhook idempotency tracking (`billing_webhook_events`)
- Gateway-agent apply path (`wg syncconf` and interface/address management hooks)
- Mem0 best-effort event client (non-canonical support context)
- Admin dashboard (`/admin`) with master-password auth for gateway/user visibility
- SQL migration starters for Supabase Postgres
- Deployment skeletons for VM-first Terraform + Fly pilot
- Vultr Terraform environment with cloud-init bootstrap (`deploy/terraform/environments/vultr`)
- Direct SSH provisioning scripts for existing VPS (`deploy/scripts/provision-*-vps.sh`)
- Benchmark templates (`k6`, `iperf3` checklist)

## Repository Layout

- `cmd/control-plane`: HTTP API service entrypoint
- `cmd/gateway-agent`: gateway worker entrypoint
- `internal/api`: route wiring and handlers
- `internal/store`: persistence interfaces and Postgres implementation
- `internal/integrations`: supabase, paddle, mem0 adapters
- `internal/gateway`: desired-config pull + WireGuard apply engine
- `migrations`: database schema migration starters
- `deploy`: terraform, cloud-init, Fly pilot
- `bench`: benchmark scripts and failover checklist
- `docs`: architecture and implementation documentation

## Quick Start (Current MVP)

1. Copy `.env.example` to `.env`.
2. Set `DATABASE_URL`, `SUPABASE_JWT_SECRET`, `COMPAT_TOKEN_SECRET`, `PADDLE_WEBHOOK_SECRET`, and gateway env vars.
3. Run migrations in order (`001`, `002`, `003`, `004`).
4. Run control plane: `go run ./cmd/control-plane`
5. Run gateway agent: `go run ./cmd/gateway-agent`
6. Hit health check: `GET /healthz`

## Documentation Map

- `docs/01-system-overview.md`
- `docs/02-api-contract.md`
- `docs/03-data-model.md`
- `docs/04-deployment-vm-fly.md`
- `docs/05-auth-billing-mem0.md`
- `docs/06-benchmark-plan.md`
- `docs/07-build-roadmap.md`
- `docs/08-runbook.md`
- `docs/09-implementation-status.md`

## Design Decisions Locked

- Protocol v1: WireGuard only (OpenVPN deferred)
- Hosting default: Supabase managed
- Billing provider: Paddle
- Gateway deployment default: Linux VM production baseline
- Fly.io: optional pilot until benchmark gate passes
- Mem0: advisory context only, never source of truth

## Go Installation

Go is installed at:

- `C:\Go\bin\go.exe`

Project cache settings used during builds:

```powershell
$env:GOCACHE='C:\new git\wg-platform-handoff\.gocache'
$env:GOMODCACHE='C:\new git\wg-platform-handoff\.gomodcache'
```
