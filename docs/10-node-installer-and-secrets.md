# 10 - One-Command Node Install + Secrets Checklist

## 1) Host The Installer Scripts

- `deploy/scripts/install-gateway.sh`
- `deploy/scripts/install-control-plane.sh`

Recommended hosting:

- GitHub raw URL pinned to a tag/commit
- S3/R2 static object

## 2) Control Plane Prerequisites

Set up and provide:

- Supabase Postgres (`DATABASE_URL`)
- Supabase JWT secret (`SUPABASE_JWT_SECRET`)
- Compatibility token secret (`COMPAT_TOKEN_SECRET`)
- Paddle webhook secret (`PADDLE_WEBHOOK_SECRET`)
- Auth rate limit (`AUTH_RATE_LIMIT_PER_MINUTE`, optional default)
- Webhook rate limit (`WEBHOOK_RATE_LIMIT_PER_MINUTE`, optional default)
- Admin dashboard master password (`ADMIN_MASTER_PASSWORD`)
- Admin session secret (`ADMIN_SESSION_SECRET`, recommended)
- Optional edge webhook token (`WEBHOOK_PROXY_TOKEN`)

## 2b) Vultr Provisioning Prerequisites (Terraform)

- `vultr_api_key` (Vultr API key)
- `gateway_token` (shared internal token between control-plane and gateway agent)
- `control_plane_base_url`
- `gateway_agent_url`

Recommended with Terraform Cloud:

- workspace working directory: `deploy/terraform/environments/vultr`
- mark `vultr_api_key` and `gateway_token` as sensitive workspace variables

Generate `gateway_token` (PowerShell):

```powershell
[Convert]::ToBase64String((1..48 | ForEach-Object { Get-Random -Max 256 }))
```

Run migrations in order:

- `001_init.sql`
- `002_billing_provider_and_entitlements.sql`
- `003_relays_public_key.sql`
- `004_webhook_idempotency_and_ip_pool.sql`

## 3) Optional One-Command Control Plane Install

```bash
wget -qO /tmp/install-control-plane.sh https://raw.githubusercontent.com/<org>/<repo>/<tag>/deploy/scripts/install-control-plane.sh
chmod +x /tmp/install-control-plane.sh
sudo CONTROL_PLANE_BIN_URL="https://artifacts.example.com/control-plane-linux-amd64" \
  DATABASE_URL="<supabase-postgres-url>" \
  SUPABASE_JWT_SECRET="<supabase-jwt-secret>" \
  COMPAT_TOKEN_SECRET="<compat-token-secret>" \
  PADDLE_WEBHOOK_SECRET="<paddle-secret>" \
  AUTH_RATE_LIMIT_PER_MINUTE="120" \
  WEBHOOK_RATE_LIMIT_PER_MINUTE="600" \
  ADMIN_MASTER_PASSWORD="<strong-master-password>" \
  ADMIN_SESSION_SECRET="<admin-session-secret>" \
  WEBHOOK_PROXY_TOKEN="<optional-edge-token>" \
  /tmp/install-control-plane.sh
```

## 4) Optional One-Command Gateway Install

```bash
wget -qO /tmp/install-gateway.sh https://raw.githubusercontent.com/<org>/<repo>/<tag>/deploy/scripts/install-gateway.sh
chmod +x /tmp/install-gateway.sh
sudo CONTROL_PLANE_BASE_URL="https://api.example.com" \
  GATEWAY_ID="gw-lon-1" \
  GATEWAY_REGION="eu-west" \
  GATEWAY_TOKEN="<internal-token>" \
  GATEWAY_AGENT_URL="https://artifacts.example.com/gateway-agent-linux-amd64" \
  GATEWAY_AGENT_SHA256="<sha256>" \
  /tmp/install-gateway.sh
```

## 5) Required Env Vars

Control-plane:

- `DATABASE_URL`
- `SUPABASE_JWT_SECRET`
- `COMPAT_TOKEN_SECRET`
- `PADDLE_WEBHOOK_SECRET`
- `AUTH_RATE_LIMIT_PER_MINUTE` (optional, default `120`)
- `WEBHOOK_RATE_LIMIT_PER_MINUTE` (optional, default `600`)
- `ADMIN_MASTER_PASSWORD` (required to enable admin dashboard)
- `ADMIN_SESSION_SECRET` (optional, recommended)

Gateway:

- `CONTROL_PLANE_BASE_URL`
- `GATEWAY_ID`
- `GATEWAY_REGION`
- `GATEWAY_TOKEN`

## 6) What The Gateway Installer Does

- Installs WireGuard tools and system deps.
- Downloads `gateway-agent`.
- Generates WireGuard keypair if missing.
- Writes `/etc/wg-platform/gateway-agent.env`.
- Creates/restarts `gateway-agent` systemd unit.

## 7) Existing VPS (IP + Root Credentials) Option

If you do not want provider API provisioning, use SSH provisioning scripts:

- `deploy/scripts/provision-control-plane-vps.sh`
- `deploy/scripts/provision-gateway-vps.sh`
- `deploy/scripts/provision-control-plane-vps.ps1`
- `deploy/scripts/provision-gateway-vps.ps1`

They copy the corresponding install script to your server and execute it remotely.

Required local tools:

- `ssh` + `scp`
- If using password auth: `sshpass`
- On Windows password mode: `plink` + `pscp` (PuTTY)

Example (control-plane over root password):

```bash
VPS_HOST="203.0.113.10" \
VPS_USER="root" \
VPS_PASSWORD="<root-password>" \
CONTROL_PLANE_BIN_URL="https://artifacts.example.com/control-plane-linux-amd64" \
DATABASE_URL="<supabase-url>" \
SUPABASE_JWT_SECRET="<jwt-secret>" \
COMPAT_TOKEN_SECRET="<compat-secret>" \
PADDLE_WEBHOOK_SECRET="<paddle-secret>" \
ADMIN_MASTER_PASSWORD="<admin-password>" \
bash deploy/scripts/provision-control-plane-vps.sh
```

Example (gateway over root password):

```bash
VPS_HOST="203.0.113.20" \
VPS_USER="root" \
VPS_PASSWORD="<root-password>" \
CONTROL_PLANE_BASE_URL="https://api.example.com" \
GATEWAY_ID="gw-lon-1" \
GATEWAY_REGION="eu-west" \
GATEWAY_TOKEN="<shared-gateway-token>" \
GATEWAY_AGENT_URL="https://artifacts.example.com/gateway-agent-linux-amd64" \
bash deploy/scripts/provision-gateway-vps.sh
```

Example (Windows PowerShell, gateway over root password):

```powershell
.\deploy\scripts\provision-gateway-vps.ps1 `
  -VpsHost "203.0.113.20" `
  -VpsUser "root" `
  -VpsPassword "<root-password>" `
  -ControlPlaneBaseUrl "https://api.example.com" `
  -GatewayId "gw-lon-1" `
  -GatewayRegion "eu-west" `
  -GatewayToken "<shared-gateway-token>" `
  -GatewayAgentUrl "https://artifacts.example.com/gateway-agent-linux-amd64"
```
