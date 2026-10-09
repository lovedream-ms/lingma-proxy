# 编译无头版（cmd/lingma-ipc-proxy）→ dist\lingma-proxy-fixed.exe
param([string]$Out = '')
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
if (-not $Out) { $Out = Join-Path $root 'dist\lingma-proxy-fixed.exe' }
$env:GOROOT = Join-Path $root 'toolchain\go'
$env:GOPATH = Join-Path $root 'toolchain\gopath'
$env:GOCACHE = Join-Path $root 'cache\gocache'
$env:GOTOOLCHAIN = 'local'
$go = Join-Path $root 'toolchain\go\bin\go.exe'
Push-Location (Join-Path $root 'src\lingma-proxy')
try {
  & $go build -trimpath -o $Out ./cmd/lingma-ipc-proxy
  if ($LASTEXITCODE -ne 0) { throw "go build failed: $LASTEXITCODE" }
  "OK -> $Out ($((Get-Item $Out).Length) bytes)"
} finally { Pop-Location }
