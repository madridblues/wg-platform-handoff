-- 006_device_runtime_stats.sql

create table if not exists device_runtime_stats (
    device_id uuid primary key references devices(id) on delete cascade,
    relay_id uuid references relays(id) on delete set null,
    endpoint text,
    last_handshake_at timestamptz,
    rx_bytes bigint not null default 0,
    tx_bytes bigint not null default 0,
    updated_at timestamptz not null default now()
);

create index if not exists idx_device_runtime_stats_updated_at
    on device_runtime_stats(updated_at desc);

