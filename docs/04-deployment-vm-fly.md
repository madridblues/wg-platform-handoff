# 04 - Deployment Blueprint (VM-First + Fly Pilot)

## Production Baseline: Linux VPS/VM Nodes

Primary gateway deployment should run on Linux VMs in cloud regions close to user demand.

### Gateway Node Components

- `wireguard-tools` + kernel WireGuard
- `gateway-agent` binary (`/usr/local/bin/gateway-agent`)
- systemd service for agent lifecycle
- local firewall policy (nftables or iptables)

### Required Ports

- UDP `51820` (WireGuard ingress)
- TCP `443` (control-plane/webhook/cloudflared path as needed)
- Optional SSH restricted to admin IPs

### Provisioning Strategy

- Terraform modules per provider (Vultr/Hetzner/Linode/etc.)
- cloud-init bootstrap installs dependencies and starts `gateway-agent`
- gateway self-registers via `/internal/gateways/register`
- gateway polls `/internal/gateways/{id}/desired-config`
- gateway applies peers using `wg syncconf`
- Optional direct SSH provisioning for existing VPS (IP + root creds) via:
  - `deploy/scripts/provision-control-plane-vps.sh`
  - `deploy/scripts/provision-gateway-vps.sh`

### Vultr Quick Path (Implemented)

`deploy/terraform/environments/vultr` includes a real `vultr_instance` resource and cloud-init bootstrap.

Required Terraform vars for this environment:

- `vultr_api_key`
- `gateway_token`
- `control_plane_base_url`
- `gateway_agent_url`

Optional:

- `gateway_agent_sha256`
- `gateway_id` / `gateway_region_slug`
- `vultr_region` / `vultr_plan` / `vultr_os_id`
- `ssh_key_ids`

### Terraform Cloud Workspace Usage

Your Terraform Cloud workspace can be used directly:

1. Set workspace working directory to `deploy/terraform/environments/vultr`.
2. Add Terraform variables for all required inputs above.
3. Mark `vultr_api_key` and `gateway_token` as sensitive.
4. Run plan/apply from Terraform Cloud.

### First Server Deployment (Vultr)

1. Build and host `gateway-agent` binary artifact.
2. Host `deploy/scripts/install-gateway.sh` at a pinned URL.
3. In Terraform Cloud workspace variables set:
   - `vultr_api_key` (sensitive)
   - `gateway_token` (sensitive)
   - `control_plane_base_url`
   - `gateway_agent_url`
4. Apply Terraform in `deploy/terraform/environments/vultr`.
5. Verify in admin dashboard:
   - `https://<control-plane-host>/admin`
   - gateway appears with heartbeats and status.

### Minimum Gateway Environment

- `CONTROL_PLANE_BASE_URL`
- `GATEWAY_ID`
- `GATEWAY_REGION`
- `GATEWAY_PROVIDER`
- `GATEWAY_TOKEN`
- `GATEWAY_PUBLIC_IPV4` (recommended)
- `GATEWAY_PUBLIC_IPV6` (optional)
- `GATEWAY_WG_INTERFACE` (default `wg0`)
- `GATEWAY_WG_PRIVATE_KEY_PATH` (default `/etc/wireguard/privatekey`)
- `GATEWAY_WG_ADDRESS_IPV4` (default `10.64.0.1/24`)
- `GATEWAY_WG_ADDRESS_IPV6` (default `fd00::1/64`)
- `GATEWAY_WG_LISTEN_PORT` (default `51820`)
- `GATEWAY_WG_APPLY_ENABLED` (default `true`)
- `GATEWAY_WG_CONFIG_DIR` (default `/run/wg-platform`)

### Minimal Bootstrap Sequence

1. VM is created via Terraform/provider API.
2. cloud-init installs WireGuard packages.
3. private/public keypair is generated if missing.
4. `gateway-agent` systemd service starts.
5. agent registers metadata (including WG public key if derivable).
6. agent fetches desired peers and applies interface config.
7. relay becomes active and appears in relay list when public key is registered.

## Fly.io Pilot Path

Fly.io remains pilot-only until benchmark parity is proven.

### Pilot Rules

- Use dedicated IPv4 with UDP readiness checks.
- Use userspace WG path (`wireguard-go`) only if kernel mode is unavailable.
- Run CI smoke tests for:
  - tunnel establishment
  - sustained throughput
  - reconnect behavior after machine restart

### Promotion Gate

Fly pilot can be promoted only if all of these match VM baseline SLO:

- Tunnel handshake success rate
- Throughput envelope
- Reconnect time
- Packet loss ceiling
- Operational stability over soak period

## Control Plane Deployment

- Control-plane API service deploys independently (Go binary/container)
- Supabase hosts Postgres/Auth
- Cloudflare Functions can front webhook ingress (`/webhooks/paddle`)
- webhook proxy token can be enforced with `WEBHOOK_PROXY_TOKEN`

