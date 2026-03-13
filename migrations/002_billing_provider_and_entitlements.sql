-- 002_billing_provider_and_entitlements.sql
-- Backfill/compat migration for legacy billing column names.

alter table subscriptions
    add column if not exists provider text,
    add column if not exists external_customer_id text,
    add column if not exists external_subscription_id text;

update subscriptions
set provider = coalesce(nullif(lower(provider), ''), 'paddle')
where provider is null or provider = '';

do $$
begin
    if exists (
        select 1
        from information_schema.columns
        where table_schema = 'public'
          and table_name = 'subscriptions'
          and column_name = 'stripe_customer_id'
    ) then
        execute '
            update subscriptions
            set external_customer_id = coalesce(external_customer_id, stripe_customer_id)
            where external_customer_id is null
        ';
    end if;

    if exists (
        select 1
        from information_schema.columns
        where table_schema = 'public'
          and table_name = 'subscriptions'
          and column_name = 'stripe_subscription_id'
    ) then
        execute '
            update subscriptions
            set external_subscription_id = coalesce(external_subscription_id, stripe_subscription_id)
            where external_subscription_id is null
        ';
    end if;

    if exists (
        select 1
        from information_schema.columns
        where table_schema = 'public'
          and table_name = 'subscriptions'
          and column_name = 'paddle_customer_id'
    ) then
        execute '
            update subscriptions
            set external_customer_id = coalesce(external_customer_id, paddle_customer_id)
            where external_customer_id is null
        ';
    end if;

    if exists (
        select 1
        from information_schema.columns
        where table_schema = 'public'
          and table_name = 'subscriptions'
          and column_name = 'paddle_subscription_id'
    ) then
        execute '
            update subscriptions
            set external_subscription_id = coalesce(external_subscription_id, paddle_subscription_id)
            where external_subscription_id is null
        ';
    end if;
end
$$;

alter table subscriptions
    alter column provider set default 'paddle';

create unique index if not exists idx_subscriptions_provider_external_subscription
    on subscriptions(provider, external_subscription_id)
    where external_subscription_id is not null;

create index if not exists idx_subscriptions_provider_external_customer
    on subscriptions(provider, external_customer_id)
    where external_customer_id is not null;
