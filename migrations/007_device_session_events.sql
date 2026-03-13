-- 007_device_session_events.sql

create table if not exists device_session_events (
    id uuid primary key default gen_random_uuid(),
    device_id uuid not null references devices(id) on delete cascade,
    relay_id uuid references relays(id) on delete set null,
    endpoint text,
    handshake_at timestamptz not null,
    rx_bytes_snapshot bigint not null default 0,
    tx_bytes_snapshot bigint not null default 0,
    created_at timestamptz not null default now(),
    unique(device_id, handshake_at)
);

create index if not exists idx_device_session_events_device_created
    on device_session_events(device_id, created_at desc);

