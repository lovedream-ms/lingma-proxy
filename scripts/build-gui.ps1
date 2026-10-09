# 编译 GUI 版（desktop/，Wails）→ dist\LingmaProxy-gui-fixed.exe
# 说明：GOTOOLCHAIN 必须是 go1.23.6，否则 wails CLI 报 archive/zip without types
param([string]$Out = '')
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$src  = Join-Path $root 'src\lingma-proxy'
if (-not $Out) { $Out = Join-Path $root 'dist\LingmaProxy-gui-fixed.exe' }
$env:GOROOT = Join-Path $root 'toolchain\go'
$env:GOPATH = Join-Path $root 'toolchain\gopath'
$env:GOCACHE = Join-Path $root 'cache\gocache'
$env:GOTOOLCHAIN = 'go1.23.6'
$nodeDir = Join-Path $root 'toolchain\node20\node-v20.20.2-win-x64'
$env:PATH = (@((Join-Path $root 'toolchain\bin'), (Join-Path $root 'toolchain\go\bin'), $nodeDir, $env:PATH) -join ';')
$frontend = Join-Path $src 'desktop\frontend'
if (-not (Test-Path (Join-Path $frontend 'dist\index.html'))) {
  Push-Location $frontend
  try { & npm.cmd ci; & npm.cmd run build } finally { Pop-Location }
}
Push-Location (Join-Path $src 'desktop')
try {
  & wails.exe build -platform windows/amd64 -clean -devtools -skipbindings -skipembedcreate -m -s -ldflags "-X main.devtoolsBuild=true"
  if ($LASTEXITCODE -ne 0) { throw "wails build failed: $LASTEXITCODE" }
} finally { Pop-Location }
Copy-Item (Join-Path $src 'desktop\build\bin\LingmaProxy.exe') $Out -Force
"OK -> $Out ($((Get-Item $Out).Length) bytes)"
