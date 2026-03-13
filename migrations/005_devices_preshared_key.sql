-- 005_devices_preshared_key.sql

alter table devices
    add column if not exists preshared_key text;

