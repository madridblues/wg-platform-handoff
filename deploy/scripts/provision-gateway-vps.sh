#!/usr/bin/env bash
set -euo pipefail

require_env() {
  local key="$1"
  if [[ -z "${!key:-}" ]]; then
    echo "Missing required env: ${key}" >&2
    exit 1
  fi
}

require_any_auth() {
  if [[ -n "${VPS_PASSWORD:-}" ]]; then
    if ! command -v sshpass >/dev/null 2>&1; then
      echo "VPS_PASSWORD was provided but sshpass is not installed." >&2
      echo "Install sshpass or use VPS_SSH_KEY_PATH instead." >&2
      exit 1
    fi
    return
  fi

  if [[ -z "${VPS_SSH_KEY_PATH:-}" ]]; then
    echo "Provide either VPS_PASSWORD or VPS_SSH_KEY_PATH." >&2
    exit 1
  fi
}

shell_quote() {
  printf "'%s'" "${1//\'/\'\"\'\"\'}"
}

write_env_line() {
  local file="$1"
  local key="$2"
  local value="$3"
  printf "%s=%s\n" "${key}" "$(shell_quote "${value}")" >> "${file}"
}

require_env "VPS_HOST"
require_env "CONTROL_PLANE_BASE_URL"
require_env "GATEWAY_ID"
require_env "GATEWAY_REGION"
require_env "GATEWAY_TOKEN"
require_env "GATEWAY_AGENT_URL"

VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
require_any_auth

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_SCRIPT="${SCRIPT_DIR}/install-gateway.sh"
if [[ ! -f "${INSTALL_SCRIPT}" ]]; then
  echo "Cannot find install script: ${INSTALL_SCRIPT}" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

remote_env="${tmpdir}/gateway.env"
remote_run="${tmpdir}/run-remote.sh"

write_env_line "${remote_env}" "CONTROL_PLANE_BASE_URL" "${CONTROL_PLANE_BASE_URL}"
write_env_line "${remote_env}" "GATEWAY_ID" "${GATEWAY_ID}"
write_env_line "${remote_env}" "GATEWAY_REGION" "${GATEWAY_REGION}"
write_env_line "${remote_env}" "GATEWAY_TOKEN" "${GATEWAY_TOKEN}"
write_env_line "${remote_env}" "GATEWAY_AGENT_URL" "${GATEWAY_AGENT_URL}"
write_env_line "${remote_env}" "GATEWAY_PROVIDER" "${GATEWAY_PROVIDER:-self}"
write_env_line "${remote_env}" "GATEWAY_PUBLIC_IPV4" "${GATEWAY_PUBLIC_IPV4:-}"
write_env_line "${remote_env}" "GATEWAY_PUBLIC_IPV6" "${GATEWAY_PUBLIC_IPV6:-}"
write_env_line "${remote_env}" "GATEWAY_WG_INTERFACE" "${GATEWAY_WG_INTERFACE:-wg0}"
write_env_line "${remote_env}" "GATEWAY_WG_PRIVATE_KEY_PATH" "${GATEWAY_WG_PRIVATE_KEY_PATH:-/etc/wireguard/privatekey}"
write_env_line "${remote_env}" "GATEWAY_WG_ADDRESS_IPV4" "${GATEWAY_WG_ADDRESS_IPV4:-10.64.0.1/24}"
write_env_line "${remote_env}" "GATEWAY_WG_ADDRESS_IPV6" "${GATEWAY_WG_ADDRESS_IPV6:-fd00::1/64}"
write_env_line "${remote_env}" "GATEWAY_WG_LISTEN_PORT" "${GATEWAY_WG_LISTEN_PORT:-51820}"
write_env_line "${remote_env}" "GATEWAY_WG_APPLY_ENABLED" "${GATEWAY_WG_APPLY_ENABLED:-true}"
write_env_line "${remote_env}" "GATEWAY_WG_CONFIG_DIR" "${GATEWAY_WG_CONFIG_DIR:-/run/wg-platform}"
write_env_line "${remote_env}" "GATEWAY_HEARTBEAT_SECONDS" "${GATEWAY_HEARTBEAT_SECONDS:-10}"
write_env_line "${remote_env}" "GATEWAY_AGENT_SHA256" "${GATEWAY_AGENT_SHA256:-}"

cat > "${remote_run}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
set -a
. /tmp/wg-platform-gateway.env
set +a

if [[ "$(id -u)" -ne 0 ]]; then
  if ! command -v sudo >/dev/null 2>&1; then
    echo "Remote user is non-root and sudo is unavailable." >&2
    exit 1
  fi
  sudo -E bash /tmp/install-gateway.sh
else
  bash /tmp/install-gateway.sh
fi
EOF

chmod +x "${remote_run}"

ssh_opts=(-o StrictHostKeyChecking=accept-new -p "${VPS_PORT}")
scp_opts=(-o StrictHostKeyChecking=accept-new -P "${VPS_PORT}")
if [[ -n "${VPS_SSH_KEY_PATH:-}" ]]; then
  ssh_opts+=(-i "${VPS_SSH_KEY_PATH}")
  scp_opts+=(-i "${VPS_SSH_KEY_PATH}")
fi

ssh_prefix=()
if [[ -n "${VPS_PASSWORD:-}" ]]; then
  ssh_prefix=(sshpass -p "${VPS_PASSWORD}")
fi

remote="${VPS_USER}@${VPS_HOST}"

"${ssh_prefix[@]}" scp "${scp_opts[@]}" "${INSTALL_SCRIPT}" "${remote}:/tmp/install-gateway.sh"
"${ssh_prefix[@]}" scp "${scp_opts[@]}" "${remote_env}" "${remote}:/tmp/wg-platform-gateway.env"
"${ssh_prefix[@]}" scp "${scp_opts[@]}" "${remote_run}" "${remote}:/tmp/wg-platform-run-gateway.sh"
"${ssh_prefix[@]}" ssh "${ssh_opts[@]}" "${remote}" "chmod +x /tmp/install-gateway.sh /tmp/wg-platform-run-gateway.sh && bash /tmp/wg-platform-run-gateway.sh"

echo "Gateway provisioning completed on ${VPS_HOST}."
