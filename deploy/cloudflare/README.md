# Cloudflare Functions Webhook Forwarder

This folder contains a Cloudflare Pages Function that forwards webhook requests to your control plane.

## Route Shape

- `POST /webhooks/paddle`

Forwarded upstream endpoint:

- `/webhooks/paddle`

## Required Env Vars

- `CONTROL_PLANE_BASE_URL`

## Optional Env Vars

- `CONTROL_PLANE_PROXY_TOKEN`

If set, the function forwards it as `X-Webhook-Proxy-Token`.
