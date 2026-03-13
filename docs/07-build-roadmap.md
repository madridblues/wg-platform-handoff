# 07 - Build Roadmap

## Phase 1 - Foundation

- Set up Supabase project, baseline schema, and service config layout.
- Implement service skeletons and shared auth middleware.
- Define audit/event model and logging baseline.

Exit gate:
- Health checks green and schema migrations applied in staging.

## Phase 2 - Compatibility API MVP

- Implement auth/account/device routes.
- Implement relay list route with ETag support.
- Add compatibility error shape handling.

Exit gate:
- Contract tests pass for all MVP routes.

## Phase 3 - Gateway Orchestration

- Implement internal gateway APIs.
- Build gateway-agent register/pull/apply/heartbeat loops.
- Add relay health state and active selection logic.

Exit gate:
- Test gateway can register, apply config, and stay healthy.

## Phase 4 - Billing + Entitlement Enforcement

- Integrate Paddle webhooks.
- Enforce entitlement checks on device/tunnel operations.
- Add webhook idempotency and replay protections.

Exit gate:
- End-to-end: signup -> pay -> create device -> suspend on failed billing.

## Phase 5 - Mem0 + Support Workflow

- Add event-to-memory ingestion pipeline with redaction.
- Build support lookup read path and access controls.

Exit gate:
- Support timeline appears without secret leakage.

## Phase 6 - Benchmarks + Promotion

- Run k6, iperf3, failover suite.
- Compare VM baseline vs Fly pilot.
- Promote Fly only if all gates pass.

Exit gate:
- Benchmark report and promotion decision logged.


