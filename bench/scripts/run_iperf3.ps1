param(
    [string]$Server = "127.0.0.1",
    [int]$DurationSeconds = 30
)

Write-Host "Running iperf3 benchmark against $Server for $DurationSeconds seconds"
Write-Host "Command: iperf3 -c $Server -t $DurationSeconds -P 4"
Write-Host "TODO: Install iperf3 on client and server before running this script."
