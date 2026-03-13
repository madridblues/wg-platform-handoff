-- 004_webhook_idempotency_and_ip_pool.sql

create table if not exists billing_webhook_events (
    id bigint generated always as identity primary key,
    provider text not null,
    event_id text not null,
    event_type text not null,
    payload jsonb not null default '{}',
    received_at timestamptz not null default now(),
    unique(provider, event_id)
);

create sequence if not exists device_ip_slot_seq start 1;

do $$
begin
    if exists (
        select 1
        from information_schema.tables
        where table_schema = 'public'
          and table_name = 'devices'
    ) then
        execute '
            select setval(
                ''device_ip_slot_seq'',
                greatest(
                    coalesce((
                        select max(
                            (split_part(host(ipv4_address), ''.'', 3)::bigint * 253) +
                            (split_part(host(ipv4_address), ''.'', 4)::bigint - 1)
                        )
                        from devices
                    ), 0),
                    1
                ),
                true
            )
        ';
    end if;
end
$$;

create unique index if not exists idx_devices_ipv4_unique on devices (ipv4_address);
create unique index if not exists idx_devices_ipv6_unique on devices (ipv6_address);
