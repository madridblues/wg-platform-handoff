#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Run as root (or: sudo -E bash bootstrap-vps.sh)" >&2
  exit 1
fi

require_env() {
  local key="$1"
  if [[ -z "${!key:-}" ]]; then
    echo "Missing required env: ${key}" >&2
    exit 1
  fi
}

install_base_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y curl ca-certificates
    return
  fi

  if command -v dnf >/dev/null 2>&1; then
    dnf install -y curl ca-certificates
    return
  fi

  if command -v yum >/dev/null 2>&1; then
    yum install -y curl ca-certificates
    return
  fi

  echo "Unsupported package manager." >&2
  exit 1
}

install_gateway_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y wireguard wireguard-tools iproute2 curl jq ca-certificates
    return
  fi

  if command -v dnf >/dev/null 2>&1; then
    dnf install -y wireguard-tools iproute curl jq ca-certificates
    return
  fi

  if command -v yum >/dev/null 2>&1; then
    yum install -y wireguard-tools iproute curl jq ca-certificates
    return
  fi

  echo "Unsupported package manager for gateway install." >&2
  exit 1
}

download_binary() {
  local source_url="$1"
  local target="$2"
  local expected_sha="${3:-}"
  local tmp
  tmp="$(mktemp)"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${source_url}" -o "${tmp}"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "${tmp}" "${source_url}"
  else
    echo "curl or wget is required" >&2
    exit 1
  fi

  if [[ -n "${expected_sha}" ]]; then
    local actual
    actual="$(sha256sum "${tmp}" | awk '{print $1}')"
    if [[ "${actual}" != "${expected_sha}" ]]; then
      echo "Checksum mismatch for ${source_url}" >&2
      echo "expected: ${expected_sha}" >&2
      echo "actual:   ${actual}" >&2
      exit 1
    fi
  fi

  install -m 0755 "${tmp}" "${target}"
  rm -f "${tmp}"
}

install_control_plane() {
  require_env "CONTROL_PLANE_BIN_URL"
  require_env "DATABASE_URL"
  require_env "SUPABASE_JWT_SECRET"

  HTTP_ADDR="${HTTP_ADDR:-:8080}"
  HTTP_READ_TIMEOUT_SECONDS="${HTTP_READ_TIMEOUT_SECONDS:-15}"
  HTTP_WRITE_TIMEOUT_SECONDS="${HTTP_WRITE_TIMEOUT_SECONDS:-15}"
  AUTH_RATE_LIMIT_PER_MINUTE="${AUTH_RATE_LIMIT_PER_MINUTE:-120}"
  WEBHOOK_RATE_LIMIT_PER_MINUTE="${WEBHOOK_RATE_LIMIT_PER_MINUTE:-600}"
  COMPAT_TOKEN_SECRET="${COMPAT_TOKEN_SECRET:-${SUPABASE_JWT_SECRET}}"
  COMPAT_TOKEN_TTL_SECONDS="${COMPAT_TOKEN_TTL_SECONDS:-3600}"
  PADDLE_WEBHOOK_SECRET="${PADDLE_WEBHOOK_SECRET:-}"
  ADMIN_MASTER_PASSWORD="${ADMIN_MASTER_PASSWORD:-}"
  ADMIN_SESSION_SECRET="${ADMIN_SESSION_SECRET:-${COMPAT_TOKEN_SECRET}}"
  ADMIN_SESSION_TTL_SECONDS="${ADMIN_SESSION_TTL_SECONDS:-43200}"
  WEBHOOK_PROXY_TOKEN="${WEBHOOK_PROXY_TOKEN:-}"
  CONTROL_PLANE_SHA256="${CONTROL_PLANE_SHA256:-}"

  install_base_packages
  download_binary "${CONTROL_PLANE_BIN_URL}" "/usr/local/bin/control-plane" "${CONTROL_PLANE_SHA256}"

  mkdir -p /etc/wg-platform
  cat > /etc/wg-platform/control-plane.env <<EOF
HTTP_ADDR=${HTTP_ADDR}
HTTP_READ_TIMEOUT_SECONDS=${HTTP_READ_TIMEOUT_SECONDS}
HTTP_WRITE_TIMEOUT_SECONDS=${HTTP_WRITE_TIMEOUT_SECONDS}
AUTH_RATE_LIMIT_PER_MINUTE=${AUTH_RATE_LIMIT_PER_MINUTE}
WEBHOOK_RATE_LIMIT_PER_MINUTE=${WEBHOOK_RATE_LIMIT_PER_MINUTE}
DATABASE_URL=${DATABASE_URL}
SUPABASE_JWT_SECRET=${SUPABASE_JWT_SECRET}
COMPAT_TOKEN_SECRET=${COMPAT_TOKEN_SECRET}
COMPAT_TOKEN_TTL_SECONDS=${COMPAT_TOKEN_TTL_SECONDS}
PADDLE_WEBHOOK_SECRET=${PADDLE_WEBHOOK_SECRET}
ADMIN_MASTER_PASSWORD=${ADMIN_MASTER_PASSWORD}
ADMIN_SESSION_SECRET=${ADMIN_SESSION_SECRET}
ADMIN_SESSION_TTL_SECONDS=${ADMIN_SESSION_TTL_SECONDS}
WEBHOOK_PROXY_TOKEN=${WEBHOOK_PROXY_TOKEN}
EOF
  chmod 0600 /etc/wg-platform/control-plane.env

  cat > /etc/systemd/system/wg-control-plane.service <<'EOF'
[Unit]
Description=WG Platform Control Plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/wg-platform/control-plane.env
ExecStart=/usr/local/bin/control-plane
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable wg-control-plane
  systemctl restart wg-control-plane

  echo "Control-plane installed."
  echo "Status: systemctl status wg-control-plane"
}

install_gateway() {
  require_env "CONTROL_PLANE_BASE_URL"
  require_env "GATEWAY_ID"
  require_env "GATEWAY_REGION"
  require_env "GATEWAY_TOKEN"
  require_env "GATEWAY_AGENT_URL"

  GATEWAY_PROVIDER="${GATEWAY_PROVIDER:-self}"
  GATEWAY_PUBLIC_IPV4="${GATEWAY_PUBLIC_IPV4:-}"
  GATEWAY_PUBLIC_IPV6="${GATEWAY_PUBLIC_IPV6:-}"
  GATEWAY_WG_INTERFACE="${GATEWAY_WG_INTERFACE:-wg0}"
  GATEWAY_WG_PRIVATE_KEY_PATH="${GATEWAY_WG_PRIVATE_KEY_PATH:-/etc/wireguard/privatekey}"
  GATEWAY_WG_ADDRESS_IPV4="${GATEWAY_WG_ADDRESS_IPV4:-10.64.0.1/24}"
  GATEWAY_WG_ADDRESS_IPV6="${GATEWAY_WG_ADDRESS_IPV6:-fd00::1/64}"
  GATEWAY_WG_LISTEN_PORT="${GATEWAY_WG_LISTEN_PORT:-51820}"
  GATEWAY_WG_APPLY_ENABLED="${GATEWAY_WG_APPLY_ENABLED:-true}"
  GATEWAY_WG_CONFIG_DIR="${GATEWAY_WG_CONFIG_DIR:-/run/wg-platform}"
  GATEWAY_HEARTBEAT_SECONDS="${GATEWAY_HEARTBEAT_SECONDS:-10}"
  GATEWAY_AGENT_SHA256="${GATEWAY_AGENT_SHA256:-}"

  if [[ -z "${GATEWAY_PUBLIC_IPV4}" ]]; then
    GATEWAY_PUBLIC_IPV4="$(curl -fsS --max-time 5 https://api.ipify.org || true)"
  fi

  install_gateway_packages
  download_binary "${GATEWAY_AGENT_URL}" "/usr/local/bin/gateway-agent" "${GATEWAY_AGENT_SHA256}"

  mkdir -p /etc/wg-platform
  cat > /etc/wg-platform/gateway-agent.env <<EOF
CONTROL_PLANE_BASE_URL=${CONTROL_PLANE_BASE_URL}
GATEWAY_ID=${GATEWAY_ID}
GATEWAY_REGION=${GATEWAY_REGION}
GATEWAY_PROVIDER=${GATEWAY_PROVIDER}
GATEWAY_TOKEN=${GATEWAY_TOKEN}
GATEWAY_PUBLIC_IPV4=${GATEWAY_PUBLIC_IPV4}
GATEWAY_PUBLIC_IPV6=${GATEWAY_PUBLIC_IPV6}
GATEWAY_WG_INTERFACE=${GATEWAY_WG_INTERFACE}
GATEWAY_WG_PRIVATE_KEY_PATH=${GATEWAY_WG_PRIVATE_KEY_PATH}
GATEWAY_WG_ADDRESS_IPV4=${GATEWAY_WG_ADDRESS_IPV4}
GATEWAY_WG_ADDRESS_IPV6=${GATEWAY_WG_ADDRESS_IPV6}
GATEWAY_WG_LISTEN_PORT=${GATEWAY_WG_LISTEN_PORT}
GATEWAY_WG_APPLY_ENABLED=${GATEWAY_WG_APPLY_ENABLED}
GATEWAY_WG_CONFIG_DIR=${GATEWAY_WG_CONFIG_DIR}
GATEWAY_HEARTBEAT_SECONDS=${GATEWAY_HEARTBEAT_SECONDS}
EOF
  chmod 0600 /etc/wg-platform/gateway-agent.env

  mkdir -p "$(dirname "${GATEWAY_WG_PRIVATE_KEY_PATH}")"
  chmod 700 "$(dirname "${GATEWAY_WG_PRIVATE_KEY_PATH}")"
  if [[ ! -f "${GATEWAY_WG_PRIVATE_KEY_PATH}" ]]; then
    wg genkey > "${GATEWAY_WG_PRIVATE_KEY_PATH}"
    chmod 600 "${GATEWAY_WG_PRIVATE_KEY_PATH}"
  fi
  wg pubkey < "${GATEWAY_WG_PRIVATE_KEY_PATH}" > "$(dirname "${GATEWAY_WG_PRIVATE_KEY_PATH}")/publickey"
  chmod 600 "$(dirname "${GATEWAY_WG_PRIVATE_KEY_PATH}")/publickey"

  cat > /etc/systemd/system/gateway-agent.service <<'EOF'
[Unit]
Description=WG Platform Gateway Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/wg-platform/gateway-agent.env
ExecStart=/usr/local/bin/gateway-agent
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable gateway-agent
  systemctl restart gateway-agent

  echo "Gateway installed."
  echo "Status: systemctl status gateway-agent"
}

ROLE="${ROLE:-${1:-}}"
ROLE="$(echo "${ROLE}" | tr '[:upper:]' '[:lower:]' | xargs)"

case "${ROLE}" in
  control-plane|controlplane|cp)
    install_control_plane
    ;;
  gateway|gw)
    install_gateway
    ;;
  *)
    echo "Set ROLE=control-plane or ROLE=gateway." >&2
    exit 1
    ;;
esac
