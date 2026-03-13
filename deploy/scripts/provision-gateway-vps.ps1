param(
  [Parameter(Mandatory = $true)][string]$VpsHost,
  [string]$VpsUser = "root",
  [int]$VpsPort = 22,
  [string]$VpsPassword = "",
  [string]$VpsSshKeyPath = "",

  [Parameter(Mandatory = $true)][string]$ControlPlaneBaseUrl,
  [Parameter(Mandatory = $true)][string]$GatewayId,
  [Parameter(Mandatory = $true)][string]$GatewayRegion,
  [Parameter(Mandatory = $true)][string]$GatewayToken,
  [Parameter(Mandatory = $true)][string]$GatewayAgentUrl,

  [string]$GatewayProvider = "self",
  [string]$GatewayPublicIPv4 = "",
  [string]$GatewayPublicIPv6 = "",
  [string]$GatewayWgInterface = "wg0",
  [string]$GatewayWgPrivateKeyPath = "/etc/wireguard/privatekey",
  [string]$GatewayWgAddressIPv4 = "10.64.0.1/24",
  [string]$GatewayWgAddressIPv6 = "fd00::1/64",
  [string]$GatewayWgListenPort = "51820",
  [string]$GatewayWgApplyEnabled = "true",
  [string]$GatewayWgConfigDir = "/run/wg-platform",
  [string]$GatewayHeartbeatSeconds = "10",
  [string]$GatewayAgentSha256 = ""
)

$ErrorActionPreference = "Stop"

function Quote-BashValue {
  param([string]$Value)
  return "'" + ($Value -replace "'", "'""'""'") + "'"
}

function Get-Tool {
  param([string[]]$Candidates)
  foreach ($candidate in $Candidates) {
    $cmd = Get-Command $candidate -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
  }
  return $null
}

if ([string]::IsNullOrWhiteSpace($VpsPassword) -and [string]::IsNullOrWhiteSpace($VpsSshKeyPath)) {
  throw "Provide either -VpsPassword or -VpsSshKeyPath."
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$installScript = Join-Path $scriptDir "install-gateway.sh"
if (-not (Test-Path $installScript)) {
  throw "Cannot find install script: $installScript"
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("wg-platform-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
  $envFile = Join-Path $tmp "gateway.env"
  $runFile = Join-Path $tmp "run-remote.sh"

  $pairs = @(
    @{ K = "CONTROL_PLANE_BASE_URL"; V = $ControlPlaneBaseUrl },
    @{ K = "GATEWAY_ID"; V = $GatewayId },
    @{ K = "GATEWAY_REGION"; V = $GatewayRegion },
    @{ K = "GATEWAY_TOKEN"; V = $GatewayToken },
    @{ K = "GATEWAY_AGENT_URL"; V = $GatewayAgentUrl },
    @{ K = "GATEWAY_PROVIDER"; V = $GatewayProvider },
    @{ K = "GATEWAY_PUBLIC_IPV4"; V = $GatewayPublicIPv4 },
    @{ K = "GATEWAY_PUBLIC_IPV6"; V = $GatewayPublicIPv6 },
    @{ K = "GATEWAY_WG_INTERFACE"; V = $GatewayWgInterface },
    @{ K = "GATEWAY_WG_PRIVATE_KEY_PATH"; V = $GatewayWgPrivateKeyPath },
    @{ K = "GATEWAY_WG_ADDRESS_IPV4"; V = $GatewayWgAddressIPv4 },
    @{ K = "GATEWAY_WG_ADDRESS_IPV6"; V = $GatewayWgAddressIPv6 },
    @{ K = "GATEWAY_WG_LISTEN_PORT"; V = $GatewayWgListenPort },
    @{ K = "GATEWAY_WG_APPLY_ENABLED"; V = $GatewayWgApplyEnabled },
    @{ K = "GATEWAY_WG_CONFIG_DIR"; V = $GatewayWgConfigDir },
    @{ K = "GATEWAY_HEARTBEAT_SECONDS"; V = $GatewayHeartbeatSeconds },
    @{ K = "GATEWAY_AGENT_SHA256"; V = $GatewayAgentSha256 }
  )

  $lines = @()
  foreach ($pair in $pairs) {
    $lines += "$($pair.K)=$(Quote-BashValue ([string]$pair.V))"
  }
  Set-Content -Path $envFile -Value ($lines -join "`n") -NoNewline

  @'
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
'@ | Set-Content -Path $runFile -NoNewline

  $remote = "$VpsUser@$VpsHost"

  if (-not [string]::IsNullOrWhiteSpace($VpsPassword)) {
    $pscp = Get-Tool @("pscp.exe", "pscp")
    $plink = Get-Tool @("plink.exe", "plink")
    if (-not $pscp -or -not $plink) {
      throw "Password mode requires PuTTY tools (pscp/plink) in PATH."
    }

    & $pscp -P $VpsPort -pw $VpsPassword $installScript "$remote`:/tmp/install-gateway.sh"
    & $pscp -P $VpsPort -pw $VpsPassword $envFile "$remote`:/tmp/wg-platform-gateway.env"
    & $pscp -P $VpsPort -pw $VpsPassword $runFile "$remote`:/tmp/wg-platform-run-gateway.sh"
    & $plink -P $VpsPort -pw $VpsPassword $remote "chmod +x /tmp/install-gateway.sh /tmp/wg-platform-run-gateway.sh && bash /tmp/wg-platform-run-gateway.sh"
  }
  else {
    $scp = Get-Tool @("scp.exe", "scp")
    $ssh = Get-Tool @("ssh.exe", "ssh")
    if (-not $scp -or -not $ssh) {
      throw "Key mode requires ssh/scp in PATH."
    }

    $scpArgs = @("-P", "$VpsPort")
    $sshArgs = @("-p", "$VpsPort")
    if (-not [string]::IsNullOrWhiteSpace($VpsSshKeyPath)) {
      $scpArgs += @("-i", $VpsSshKeyPath)
      $sshArgs += @("-i", $VpsSshKeyPath)
    }

    & $scp @scpArgs $installScript "$remote`:/tmp/install-gateway.sh"
    & $scp @scpArgs $envFile "$remote`:/tmp/wg-platform-gateway.env"
    & $scp @scpArgs $runFile "$remote`:/tmp/wg-platform-run-gateway.sh"
    & $ssh @sshArgs $remote "chmod +x /tmp/install-gateway.sh /tmp/wg-platform-run-gateway.sh && bash /tmp/wg-platform-run-gateway.sh"
  }

  Write-Host "Gateway provisioning completed on $VpsHost."
}
finally {
  if (Test-Path $tmp) {
    Remove-Item -Path $tmp -Recurse -Force
  }
}
