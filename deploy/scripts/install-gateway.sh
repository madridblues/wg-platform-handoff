#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Run as root (or: sudo bash install-gateway.sh)" >&2
  exit 1
fi

require_env() {
  local key="$1"
  if [[ -z "${!key:-}" ]]; then
    echo "Missing required env: ${key}" >&2
    exit 1
  fi
}

# Required runtime values
require_env "CONTROL_PLANE_BASE_URL"
require_env "GATEWAY_ID"
require_env "GATEWAY_REGION"
require_env "GATEWAY_TOKEN"
require_env "GATEWAY_AGENT_URL"

# Optional values / defaults
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

install_packages() {
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

  echo "Unsupported package manager. Install wireguard-tools/iproute2/curl manually." >&2
  exit 1
}

download_binary() {
  local target="$1"
  local tmp
  tmp="$(mktemp)"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${GATEWAY_AGENT_URL}" -o "${tmp}"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "${tmp}" "${GATEWAY_AGENT_URL}"
  else
    echo "curl or wget is required" >&2
    exit 1
  fi

  if [[ -n "${GATEWAY_AGENT_SHA256}" ]]; then
    local actual
    actual="$(sha256sum "${tmp}" | awk '{print $1}')"
    if [[ "${actual}" != "${GATEWAY_AGENT_SHA256}" ]]; then
      echo "Checksum mismatch for gateway-agent binary" >&2
      echo "expected: ${GATEWAY_AGENT_SHA256}" >&2
      echo "actual:   ${actual}" >&2
      exit 1
    fi
  fi

  install -m 0755 "${tmp}" "${target}"
  rm -f "${tmp}"
}

write_env_file() {
  local env_file="/etc/wg-platform/gateway-agent.env"
  mkdir -p /etc/wg-platform

  cat > "${env_file}" <<EOF
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

  chmod 0600 "${env_file}"
}

ensure_wireguard_keys() {
  local key_path="${GATEWAY_WG_PRIVATE_KEY_PATH}"
  local key_dir
  key_dir="$(dirname "${key_path}")"

  mkdir -p "${key_dir}"
  chmod 700 "${key_dir}"

  if [[ ! -f "${key_path}" ]]; then
    wg genkey > "${key_path}"
    chmod 600 "${key_path}"
  fi

  local pub_path="${key_dir}/publickey"
  wg pubkey < "${key_path}" > "${pub_path}"
  chmod 600 "${pub_path}"
}

write_systemd_unit() {
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
}

main() {
  install_packages
  download_binary "/usr/local/bin/gateway-agent"
  write_env_file
  ensure_wireguard_keys
  write_systemd_unit

  systemctl daemon-reload
  systemctl enable gateway-agent
  systemctl restart gateway-agent

  echo "Gateway node installed and started."
  echo "Service: systemctl status gateway-agent"
  echo "Logs:    journalctl -u gateway-agent -f"
}

main "$@"
