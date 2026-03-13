-- 003_relays_public_key.sql

alter table relays
    add column if not exists wg_public_key text;

create index if not exists idx_relays_active_public_key
    on relays(active, wg_public_key);

