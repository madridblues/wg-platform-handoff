-- 001_init.sql
-- Supabase Postgres schema scaffold for VPN control plane.

create extension if not exists "pgcrypto";

create table if not exists accounts (
    id uuid primary key default gen_random_uuid(),
    account_number text not null unique,
    supabase_user_id uuid not null unique,
    status text not null default 'active',
    expiry_at timestamptz not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists subscriptions (
    id uuid primary key default gen_random_uuid(),
    account_id uuid not null references accounts(id) on delete cascade,
    provider text not null default 'paddle',
    external_customer_id text not null,
    external_subscription_id text not null,
    status text not null,
    current_period_end timestamptz,
    last_webhook_event_id text unique,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique(provider, external_subscription_id)
);

create index if not exists idx_subscriptions_customer on subscriptions(provider, external_customer_id);
create index if not exists idx_subscriptions_subscription on subscriptions(provider, external_subscription_id);

create table if not exists devices (
    id uuid primary key default gen_random_uuid(),
    account_id uuid not null references accounts(id) on delete cascade,
    name text not null,
    pubkey text not null unique,
    hijack_dns boolean not null default false,
    ipv4_address cidr not null,
    ipv6_address cidr not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique(account_id, name)
);

create table if not exists relays (
    id uuid primary key default gen_random_uuid(),
    region text not null,
    hostname text not null unique,
    public_ipv4 inet not null,
    public_ipv6 inet,
    wg_port int not null default 51820,
    wg_public_key text,
    active boolean not null default true,
    weight int not null default 100,
    provider text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists relay_heartbeats (
    relay_id uuid not null references relays(id) on delete cascade,
    received_at timestamptz not null default now(),
    status text not null,
    metrics jsonb not null default '{}'
);

create index if not exists idx_relay_heartbeats_received on relay_heartbeats(received_at desc);

create table if not exists gateway_apply_events (
    id uuid primary key default gen_random_uuid(),
    relay_id uuid references relays(id) on delete set null,
    desired_version bigint not null,
    result text not null,
    error_text text,
    created_at timestamptz not null default now(),
    unique(relay_id, desired_version)
);

create table if not exists audit_events (
    id uuid primary key default gen_random_uuid(),
    actor_type text not null,
    actor_id text not null,
    action text not null,
    entity_type text not null,
    entity_id text,
    payload jsonb not null default '{}',
    created_at timestamptz not null default now()
);

create index if not exists idx_audit_events_created on audit_events(created_at desc);

-- Row level security defaults. Fine-grained policies should be added by service-specific migrations.
alter table accounts enable row level security;
alter table subscriptions enable row level security;
alter table devices enable row level security;

