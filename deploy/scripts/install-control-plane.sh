#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Run as root (or: sudo bash install-control-plane.sh)" >&2
  exit 1
fi

require_env() {
  local key="$1"
  if [[ -z "${!key:-}" ]]; then
    echo "Missing required env: ${key}" >&2
    exit 1
  fi
}

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

install_packages() {
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

  echo "Unsupported package manager. Install curl manually." >&2
  exit 1
}

download_binary() {
  local target="$1"
  local tmp
  tmp="$(mktemp)"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${CONTROL_PLANE_BIN_URL}" -o "${tmp}"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "${tmp}" "${CONTROL_PLANE_BIN_URL}"
  else
    echo "curl or wget is required" >&2
    exit 1
  fi

  if [[ -n "${CONTROL_PLANE_SHA256}" ]]; then
    local actual
    actual="$(sha256sum "${tmp}" | awk '{print $1}')"
    if [[ "${actual}" != "${CONTROL_PLANE_SHA256}" ]]; then
      echo "Checksum mismatch for control-plane binary" >&2
      echo "expected: ${CONTROL_PLANE_SHA256}" >&2
      echo "actual:   ${actual}" >&2
      exit 1
    fi
  fi

  install -m 0755 "${tmp}" "${target}"
  rm -f "${tmp}"
}

write_env_file() {
  local env_file="/etc/wg-platform/control-plane.env"
  mkdir -p /etc/wg-platform

  cat > "${env_file}" <<EOF
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

  chmod 0600 "${env_file}"
}

write_systemd_unit() {
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
}

main() {
  install_packages
  download_binary "/usr/local/bin/control-plane"
  write_env_file
  write_systemd_unit

  systemctl daemon-reload
  systemctl enable wg-control-plane
  systemctl restart wg-control-plane

  echo "Control-plane installed and started."
  echo "Service: systemctl status wg-control-plane"
  echo "Logs:    journalctl -u wg-control-plane -f"
}

main "$@"
