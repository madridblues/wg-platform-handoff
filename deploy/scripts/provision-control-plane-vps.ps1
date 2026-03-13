param(
  [Parameter(Mandatory = $true)][string]$VpsHost,
  [string]$VpsUser = "root",
  [int]$VpsPort = 22,
  [string]$VpsPassword = "",
  [string]$VpsSshKeyPath = "",

  [Parameter(Mandatory = $true)][string]$ControlPlaneBinUrl,
  [Parameter(Mandatory = $true)][string]$DatabaseUrl,
  [Parameter(Mandatory = $true)][string]$SupabaseJwtSecret,

  [string]$HttpAddr = ":8080",
  [string]$HttpReadTimeoutSeconds = "15",
  [string]$HttpWriteTimeoutSeconds = "15",
  [string]$AuthRateLimitPerMinute = "120",
  [string]$WebhookRateLimitPerMinute = "600",
  [string]$CompatTokenSecret = "",
  [string]$CompatTokenTtlSeconds = "3600",
  [string]$PaddleWebhookSecret = "",
  [string]$AdminMasterPassword = "",
  [string]$AdminSessionSecret = "",
  [string]$AdminSessionTtlSeconds = "43200",
  [string]$WebhookProxyToken = "",
  [string]$ControlPlaneSha256 = ""
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

if ([string]::IsNullOrWhiteSpace($CompatTokenSecret)) {
  $CompatTokenSecret = $SupabaseJwtSecret
}
if ([string]::IsNullOrWhiteSpace($AdminSessionSecret)) {
  $AdminSessionSecret = $CompatTokenSecret
}

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$installScript = Join-Path $scriptDir "install-control-plane.sh"
if (-not (Test-Path $installScript)) {
  throw "Cannot find install script: $installScript"
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("wg-platform-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
  $envFile = Join-Path $tmp "control-plane.env"
  $runFile = Join-Path $tmp "run-remote.sh"

  $pairs = @(
    @{ K = "CONTROL_PLANE_BIN_URL"; V = $ControlPlaneBinUrl },
    @{ K = "DATABASE_URL"; V = $DatabaseUrl },
    @{ K = "SUPABASE_JWT_SECRET"; V = $SupabaseJwtSecret },
    @{ K = "HTTP_ADDR"; V = $HttpAddr },
    @{ K = "HTTP_READ_TIMEOUT_SECONDS"; V = $HttpReadTimeoutSeconds },
    @{ K = "HTTP_WRITE_TIMEOUT_SECONDS"; V = $HttpWriteTimeoutSeconds },
    @{ K = "AUTH_RATE_LIMIT_PER_MINUTE"; V = $AuthRateLimitPerMinute },
    @{ K = "WEBHOOK_RATE_LIMIT_PER_MINUTE"; V = $WebhookRateLimitPerMinute },
    @{ K = "COMPAT_TOKEN_SECRET"; V = $CompatTokenSecret },
    @{ K = "COMPAT_TOKEN_TTL_SECONDS"; V = $CompatTokenTtlSeconds },
    @{ K = "PADDLE_WEBHOOK_SECRET"; V = $PaddleWebhookSecret },
    @{ K = "ADMIN_MASTER_PASSWORD"; V = $AdminMasterPassword },
    @{ K = "ADMIN_SESSION_SECRET"; V = $AdminSessionSecret },
    @{ K = "ADMIN_SESSION_TTL_SECONDS"; V = $AdminSessionTtlSeconds },
    @{ K = "WEBHOOK_PROXY_TOKEN"; V = $WebhookProxyToken },
    @{ K = "CONTROL_PLANE_SHA256"; V = $ControlPlaneSha256 }
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
'@ | Set-Content -Path $runFile -NoNewline

  $remote = "$VpsUser@$VpsHost"

  if (-not [string]::IsNullOrWhiteSpace($VpsPassword)) {
    $pscp = Get-Tool @("pscp.exe", "pscp")
    $plink = Get-Tool @("plink.exe", "plink")
    if (-not $pscp -or -not $plink) {
      throw "Password mode requires PuTTY tools (pscp/plink) in PATH."
    }

    & $pscp -P $VpsPort -pw $VpsPassword $installScript "$remote`:/tmp/install-control-plane.sh"
    & $pscp -P $VpsPort -pw $VpsPassword $envFile "$remote`:/tmp/wg-platform-control-plane.env"
    & $pscp -P $VpsPort -pw $VpsPassword $runFile "$remote`:/tmp/wg-platform-run-control-plane.sh"
    & $plink -P $VpsPort -pw $VpsPassword $remote "chmod +x /tmp/install-control-plane.sh /tmp/wg-platform-run-control-plane.sh && bash /tmp/wg-platform-run-control-plane.sh"
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

    & $scp @scpArgs $installScript "$remote`:/tmp/install-control-plane.sh"
    & $scp @scpArgs $envFile "$remote`:/tmp/wg-platform-control-plane.env"
    & $scp @scpArgs $runFile "$remote`:/tmp/wg-platform-run-control-plane.sh"
    & $ssh @sshArgs $remote "chmod +x /tmp/install-control-plane.sh /tmp/wg-platform-run-control-plane.sh && bash /tmp/wg-platform-run-control-plane.sh"
  }

  Write-Host "Control-plane provisioning completed on $VpsHost."
}
finally {
  if (Test-Path $tmp) {
    Remove-Item -Path $tmp -Recurse -Force
  }
}
