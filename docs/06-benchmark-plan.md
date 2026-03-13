# 06 - Benchmark Plan

## Purpose

Provide measurable acceptance for API performance, tunnel dataplane quality, failover behavior, and Fly pilot promotion.

## Control Plane API Benchmarks

### Tooling

- `k6`

### Target Routes

- `POST /auth/v1/token`
- `POST /accounts/v1/devices`
- `GET /accounts/v1/devices`
- `GET /app/v1/relays`

### Metrics

- p50/p95/p99 latency
- request error rate
- saturation point by virtual users

### Success Gate

- p95 under agreed threshold at target concurrency
- error rate below agreed threshold

## Gateway Dataplane Benchmarks

### Tooling

- `iperf3`
- tunnel connect/disconnect scripts

### Metrics

- throughput (single stream and multi-stream)
- jitter
- packet loss
- tunnel handshake success rate
- reconnect time

## Failure and Recovery Benchmarks

- Kill one active relay and measure reroute/recovery time
- Pause control-plane API and confirm gateway behavior degrades safely
- Force stale desired-config version and validate idempotent replay

## VM vs Fly Pilot

Run identical scenarios on VM baseline and Fly pilot.

Fly can move from pilot to candidate only after meeting VM SLO envelope on:

- handshake reliability
- throughput floor
- reconnect time
- packet loss ceiling
- 24h stability soak


