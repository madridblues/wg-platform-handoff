# 08 - Operations Runbook

## Daily Checks

- API health and latency dashboard
- Relay heartbeat freshness
- Device provisioning error rate
- Paddle webhook failure queue
- Gateway apply-result failure rate

## Incident Classes

### Auth Incident

Symptoms:
- token endpoint failures
- sudden increase in `401`/`403`

Actions:
1. Validate Supabase auth health.
2. Verify JWT signing key configuration.
3. Check entitlement resolver logs.

### Billing Incident

Symptoms:
- active subscribers blocked
- webhook queue backlog

Actions:
1. Verify Paddle webhook delivery status.
2. Replay missed events with idempotency checks.
3. Recompute entitlement snapshots for impacted accounts.

### Gateway Incident

Symptoms:
- tunnel handshake failures in one region
- stale relay heartbeats

Actions:
1. Drain affected relays from active list.
2. Restart gateway-agent and verify `wg syncconf` succeeds.
3. Validate desired config version progression.

### Fly Pilot Incident

Symptoms:
- UDP instability or low throughput

Actions:
1. Disable pilot routing weight to zero.
2. Keep VM baseline serving traffic.
3. Capture benchmark + packet diagnostics for postmortem.

## Security Controls

- Rotate service secrets regularly.
- Enforce least privilege for service-role DB credentials.
- Store audit events for all admin and gateway writes.
- Never log private keys or raw auth tokens.

## Handoff Checklist for Next Developer

1. Confirm Supabase env variables and service-role access are configured.
2. Confirm Paddle webhook secret and replay handling are wired.
3. Confirm gateway register/pull/apply flow exists in staging.
4. Confirm benchmark scripts can run in CI/staging.
5. Confirm docs and architecture decisions match current code.



