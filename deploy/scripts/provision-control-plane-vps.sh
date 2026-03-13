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
require_env "CONTROL_PLANE_BIN_URL"
require_env "DATABASE_URL"
require_env "SUPABASE_JWT_SECRET"

VPS_USER="${VPS_USER:-root}"
VPS_PORT="${VPS_PORT:-22}"
require_any_auth

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_SCRIPT="${SCRIPT_DIR}/install-control-plane.sh"
if [[ ! -f "${INSTALL_SCRIPT}" ]]; then
  echo "Cannot find install script: ${INSTALL_SCRIPT}" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

remote_env="${tmpdir}/control-plane.env"
remote_run="${tmpdir}/run-remote.sh"

write_env_line "${remote_env}" "CONTROL_PLANE_BIN_URL" "${CONTROL_PLANE_BIN_URL}"
write_env_line "${remote_env}" "DATABASE_URL" "${DATABASE_URL}"
write_env_line "${remote_env}" "SUPABASE_JWT_SECRET" "${SUPABASE_JWT_SECRET}"
write_env_line "${remote_env}" "HTTP_ADDR" "${HTTP_ADDR:-:8080}"
write_env_line "${remote_env}" "HTTP_READ_TIMEOUT_SECONDS" "${HTTP_READ_TIMEOUT_SECONDS:-15}"
write_env_line "${remote_env}" "HTTP_WRITE_TIMEOUT_SECONDS" "${HTTP_WRITE_TIMEOUT_SECONDS:-15}"
write_env_line "${remote_env}" "AUTH_RATE_LIMIT_PER_MINUTE" "${AUTH_RATE_LIMIT_PER_MINUTE:-120}"
write_env_line "${remote_env}" "WEBHOOK_RATE_LIMIT_PER_MINUTE" "${WEBHOOK_RATE_LIMIT_PER_MINUTE:-600}"
write_env_line "${remote_env}" "COMPAT_TOKEN_SECRET" "${COMPAT_TOKEN_SECRET:-${SUPABASE_JWT_SECRET}}"
write_env_line "${remote_env}" "COMPAT_TOKEN_TTL_SECONDS" "${COMPAT_TOKEN_TTL_SECONDS:-3600}"
write_env_line "${remote_env}" "PADDLE_WEBHOOK_SECRET" "${PADDLE_WEBHOOK_SECRET:-}"
write_env_line "${remote_env}" "ADMIN_MASTER_PASSWORD" "${ADMIN_MASTER_PASSWORD:-}"
write_env_line "${remote_env}" "ADMIN_SESSION_SECRET" "${ADMIN_SESSION_SECRET:-${COMPAT_TOKEN_SECRET:-${SUPABASE_JWT_SECRET}}}"
write_env_line "${remote_env}" "ADMIN_SESSION_TTL_SECONDS" "${ADMIN_SESSION_TTL_SECONDS:-43200}"
write_env_line "${remote_env}" "WEBHOOK_PROXY_TOKEN" "${WEBHOOK_PROXY_TOKEN:-}"
write_env_line "${remote_env}" "CONTROL_PLANE_SHA256" "${CONTROL_PLANE_SHA256:-}"

cat > "${remote_run}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
set -a
. /tmp/wg-platform-control-plane.env
set +a

if [[ "$(id -u)" -ne 0 ]]; then
  if ! command -v sudo >/dev/null 2>&1; then
    echo "Remote user is non-root and sudo is unavailable." >&2
    exit 1
  fi
  sudo -E bash /tmp/install-control-plane.sh
else
  bash /tmp/install-control-plane.sh
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

"${ssh_prefix[@]}" scp "${scp_opts[@]}" "${INSTALL_SCRIPT}" "${remote}:/tmp/install-control-plane.sh"
"${ssh_prefix[@]}" scp "${scp_opts[@]}" "${remote_env}" "${remote}:/tmp/wg-platform-control-plane.env"
"${ssh_prefix[@]}" scp "${scp_opts[@]}" "${remote_run}" "${remote}:/tmp/wg-platform-run-control-plane.sh"
"${ssh_prefix[@]}" ssh "${ssh_opts[@]}" "${remote}" "chmod +x /tmp/install-control-plane.sh /tmp/wg-platform-run-control-plane.sh && bash /tmp/wg-platform-run-control-plane.sh"

echo "Control-plane provisioning completed on ${VPS_HOST}."
