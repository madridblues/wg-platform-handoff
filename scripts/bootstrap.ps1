# Local bootstrap helper

Write-Host "1) Copy .env.example to .env"
Write-Host "2) Set required secrets and gateway vars (DB, auth, billing, WG paths)"
Write-Host "3) Apply migrations 001 -> 002 -> 003"
Write-Host "4) Run control-plane: go run ./cmd/control-plane"
Write-Host "5) Run gateway-agent: go run ./cmd/gateway-agent"
Write-Host "6) For real apply mode, ensure wireguard private key exists at GATEWAY_WG_PRIVATE_KEY_PATH"
