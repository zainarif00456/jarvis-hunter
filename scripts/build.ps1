# Build on Windows (PowerShell). Same result as build.sh: dist\jarvis-host-windows-*.exe
#   .\scripts\build.ps1            both architectures
#   $env:VERSION="1.0.0"; .\scripts\build.ps1
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "Go is not installed (https://go.dev/dl/)" }
$version = if ($env:VERSION) { $env:VERSION } else { "0.1.0-dev" }
New-Item -ItemType Directory -Force dist | Out-Null
$env:CGO_ENABLED = "0"; $env:GOOS = "windows"
foreach ($arch in "amd64", "arm64") {
    $env:GOARCH = $arch
    $out = "dist\jarvis-host-windows-$arch.exe"
    Write-Host "==> $out"
    go build -trimpath -ldflags "-s -w -X github.com/jarvis-support/host/internal/version.Version=$version" -o $out .\cmd\jarvis-host
    # SIGNING HOOK (not enabled): signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /a $out
}
Get-FileHash dist\jarvis-host-* -Algorithm SHA256 | ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" } | Set-Content dist\SHA256SUMS
