$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

Write-Host "[1/3] go mod tidy"
go mod tidy

Write-Host "[2/3] go build"
New-Item -ItemType Directory -Force -Path "$root\dist" | Out-Null
go build -ldflags "-s -w" -o "$root\dist\bizanti-agent.exe" ./cmd/bizanti-agent

Write-Host "[3/3] done: dist\\bizanti-agent.exe"
