# Single entry point for desktop releases and optional console builds.
param(
  [ValidateSet('All', 'Windows', 'Linux', 'Headless')][string]$Target = 'All',
  [switch]$SkipFrontend,
  [switch]$CleanBeforeBuild,
  [string]$Distro = 'Ubuntu-24.04',
  [string]$Source = '',
  [string]$BuildRoot = '/var/tmp/lingma-proxy-ubuntu2004-20261019',
  [string]$AppIndicatorDeb = '',
  [string]$Out = ''
)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
if (-not $Source) { $Source = $projectRoot }
$Source = (Resolve-Path -LiteralPath $Source).Path
if (-not (Test-Path -LiteralPath (Join-Path $Source 'go.mod'))) { throw "Missing source checkout: $Source" }
if ($Out -and $Target -ne 'Headless') { throw '-Out is only supported with -Target Headless.' }
$version = (Get-Content (Join-Path $Source 'VERSION') -Raw).Trim()
$desktopVersion = (Get-Content (Join-Path $Source 'desktop\wails.json') -Raw | ConvertFrom-Json).info.productVersion
if ($version -notmatch '^\d+\.\d+\.\d+$' -or $desktopVersion -ne $version) { throw 'VERSION and desktop/wails.json must contain the same major.minor.patch version.' }
if ($CleanBeforeBuild) {
  $targetPath = [IO.Path]::GetFullPath((Join-Path $projectRoot 'cache\gocache'))
  $boundary = [IO.Path]::GetFullPath($projectRoot).TrimEnd('\') + '\'
  if (-not $targetPath.StartsWith($boundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'Cache target outside workspace' }
  if (Test-Path -LiteralPath $targetPath) { Remove-Item -LiteralPath $targetPath -Recurse -Force }
}
$toolRoot = Join-Path $projectRoot 'cache\toolchain'
New-Item -ItemType Directory -Path $toolRoot -Force | Out-Null
function Get-VerifiedArchive([string]$Url, [string]$Path, [string]$ExpectedHash) {
  if (-not (Test-Path -LiteralPath $Path)) { Invoke-WebRequest -Uri $Url -OutFile $Path }
  if ((Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash -ne $ExpectedHash.Trim()) {
    throw "SHA256 mismatch: $Path; remove the invalid download and retry."
  }
}
$goZip = Join-Path $toolRoot 'go1.23.6.windows-amd64.zip'
$goExe = Join-Path $toolRoot 'go\bin\go.exe'
if (-not (Test-Path -LiteralPath $goExe)) {
  $releases = Invoke-RestMethod 'https://go.dev/dl/?mode=json&include=all'
  $hash = (($releases | Where-Object version -eq 'go1.23.6').files | Where-Object filename -eq 'go1.23.6.windows-amd64.zip').sha256
  if (-not $hash) { throw 'Go archive checksum missing' }
  Get-VerifiedArchive 'https://go.dev/dl/go1.23.6.windows-amd64.zip' $goZip $hash
  Expand-Archive -LiteralPath $goZip -DestinationPath $toolRoot -Force
}
$env:GOROOT = Join-Path $toolRoot 'go'
$env:GOPATH = Join-Path $projectRoot 'cache\gopath'
$env:GOCACHE = Join-Path $projectRoot 'cache\gocache'
$env:GOTOOLCHAIN = 'local'
$env:PATH = (@((Join-Path $env:GOPATH 'bin'), (Join-Path $env:GOROOT 'bin'), $env:PATH) -join ';')
if ($Target -ne 'Headless') {
  $nodeName = 'node-v20.20.2-win-x64'
  $nodeDir = Join-Path $toolRoot $nodeName
  if (-not (Test-Path -LiteralPath (Join-Path $nodeDir 'npm.cmd'))) {
    $sums = Invoke-RestMethod 'https://nodejs.org/dist/v20.20.2/SHASUMS256.txt'
    $line = ($sums -split "`n" | Where-Object { $_ -match "  $nodeName.zip$" })
    if (-not $line) { throw 'Node archive checksum missing' }
    $archive = Join-Path $toolRoot "$nodeName.zip"
    Get-VerifiedArchive "https://nodejs.org/dist/v20.20.2/$nodeName.zip" $archive ($line -split ' ')[0]
    Expand-Archive -LiteralPath $archive -DestinationPath $toolRoot -Force
  }
  $env:PATH = "$nodeDir;$env:PATH"
}

function Build-Frontend {
  $frontend = Join-Path $Source 'desktop\frontend'
  if (-not $SkipFrontend) {
    Push-Location $frontend
    try {
      & npm.cmd install --package-lock-only --ignore-scripts
      if ($LASTEXITCODE -ne 0) { throw 'npm lock generation failed' }
      & npm.cmd ci
      if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
      & npm.cmd run build
      if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }
    } finally { Pop-Location }
  }
  if (-not (Test-Path -LiteralPath (Join-Path $frontend 'dist\index.html'))) { throw 'Frontend assets missing; rerun without -SkipFrontend.' }
}

function Build-Windows([string]$Destination, [switch]$Headless) {
  $Destination = [IO.Path]::GetFullPath($Destination)
  New-Item -ItemType Directory -Path (Split-Path -Parent $Destination) -Force | Out-Null
  if ($Headless) {
    & (Join-Path $env:GOROOT 'bin\go.exe') -C $Source build -trimpath -o $Destination ./cmd/lingma-ipc-proxy
    if ($LASTEXITCODE -ne 0) { throw 'Headless Go build failed' }
  } else {
    if (-not (Get-Command wails.exe -ErrorAction SilentlyContinue)) {
      & $goExe install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
      if ($LASTEXITCODE -ne 0) { throw 'Wails CLI installation failed' }
    }
    Push-Location (Join-Path $Source 'desktop')
    try {
      & wails.exe build -platform windows/amd64 -clean -devtools -skipbindings -skipembedcreate -m -s -ldflags '-X main.devtoolsBuild=true'
      if ($LASTEXITCODE -ne 0) { throw 'Windows Wails build failed' }
    } finally { Pop-Location }
    Copy-Item -LiteralPath (Join-Path $Source 'desktop\build\bin\LingmaProxy.exe') -Destination $Destination -Force
  }
  Write-Host "OK -> $Destination"
}

function Convert-ToWslPath([string]$WindowsPath) {
  $converted = & wsl.exe -d $Distro -u root --exec wslpath -a -u ($WindowsPath.Replace('\', '/'))
  if ($LASTEXITCODE -ne 0) { throw "Cannot access WSL distribution: $Distro" }
  return ($converted -join "`n").Trim()
}

function Build-Linux([string]$Destination) {
  $cacheDir = Join-Path $projectRoot 'cache\ubuntu2004-build'
  New-Item -ItemType Directory -Path $cacheDir -Force | Out-Null
  $dependencyName = 'libappindicator3-1_12.10.1+20.04.20200408.1-0ubuntu1_amd64.deb'
  $dependencyPath = $AppIndicatorDeb
  if (-not $dependencyPath) { $dependencyPath = Join-Path $cacheDir $dependencyName }
  if (-not (Test-Path -LiteralPath $dependencyPath)) {
    Invoke-WebRequest -Uri "https://archive.ubuntu.com/ubuntu/pool/universe/liba/libappindicator/$dependencyName" -OutFile $dependencyPath
  }
  $dependencyPath = (Resolve-Path -LiteralPath $dependencyPath).Path
  $archive = Join-Path $cacheDir ('source-' + [guid]::NewGuid().ToString('N') + '.tar.gz')
  $archiveWsl = Convert-ToWslPath $archive
  $sourceWsl = Convert-ToWslPath $Source
  & wsl.exe -d $Distro -u root --exec tar -czf $archiveWsl --exclude=desktop/frontend/node_modules --exclude=desktop/build/bin --exclude=desktop/build/windows --exclude=desktop/build/darwin --exclude=_tests_for_patch --exclude=tests_for_patch '--exclude=*.exe' -C $sourceWsl VERSION go.mod go.sum cmd internal desktop scripts LICENSE
  if ($LASTEXITCODE -ne 0) { throw 'Source archive creation failed' }
  $modules = Join-Path $env:GOPATH 'pkg\mod'
  New-Item -ItemType Directory -Path $modules -Force | Out-Null
  $driver = Convert-ToWslPath (Join-Path $PSScriptRoot 'build-ubuntu2004-wsl.sh')
  $recipe = Convert-ToWslPath (Join-Path $PSScriptRoot 'build-ubuntu2004-gui.sh')
  $outWsl = Convert-ToWslPath $Destination
  $cacheWsl = Convert-ToWslPath $cacheDir
  $modulesWsl = Convert-ToWslPath $modules
  $dependencyWsl = Convert-ToWslPath $dependencyPath
  & wsl.exe -d $Distro -u root --exec bash $driver $archiveWsl $outWsl $recipe $cacheWsl $BuildRoot $modulesWsl $dependencyWsl
  if ($LASTEXITCODE -ne 0) { throw "Ubuntu build failed: $LASTEXITCODE" }
}

if ($Target -eq 'Headless') {
  if (-not $Out) { $Out = Join-Path $projectRoot 'cache\build\lingma-proxy.exe' }
  Build-Windows -Destination $Out -Headless
  return
}
Build-Frontend
$releaseId = [guid]::NewGuid().ToString('N')
$stagingDir = Join-Path $projectRoot "cache\release-builds\$releaseId"
New-Item -ItemType Directory -Path $stagingDir -Force | Out-Null
$windowsName = "lingma-proxy-desktop-windows_${version}_amd64.exe"
$linuxName = "lingma-proxy-desktop-ubuntu20.04_${version}_amd64.deb"
$publishNames = @()
if ($Target -in @('All', 'Windows')) {
  Build-Windows -Destination (Join-Path $stagingDir $windowsName)
  $publishNames += $windowsName
}
if ($Target -in @('All', 'Linux')) {
  Build-Linux -Destination $stagingDir
  $publishNames += $linuxName
}
foreach ($name in $publishNames) {
  if (-not (Test-Path -LiteralPath (Join-Path $stagingDir $name) -PathType Leaf)) { throw "Build output missing: $name" }
}
$outputDir = Join-Path $projectRoot 'dist'
New-Item -ItemType Directory -Path $outputDir -Force | Out-Null
$archiveDir = [IO.Path]::GetFullPath((Join-Path $projectRoot "cache\release-archive\$releaseId"))
$workspaceBoundary = [IO.Path]::GetFullPath($projectRoot).TrimEnd('\') + '\'
$distBoundary = [IO.Path]::GetFullPath($outputDir).TrimEnd('\') + '\'
if (-not $archiveDir.StartsWith($workspaceBoundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'Archive target outside workspace' }
foreach ($entry in @(Get-ChildItem -LiteralPath $outputDir -Force)) {
  # A single-platform build preserves the other platform's current release.
  if (-not $entry.PSIsContainer -and $entry.Name -in @($windowsName, $linuxName) -and $entry.Name -notin $publishNames) { continue }
  $entryPath = [IO.Path]::GetFullPath($entry.FullName)
  if (-not $entryPath.StartsWith($distBoundary, [StringComparison]::OrdinalIgnoreCase)) { throw 'Archive source outside dist' }
  New-Item -ItemType Directory -Path $archiveDir -Force | Out-Null
  Move-Item -LiteralPath $entryPath -Destination $archiveDir
}
foreach ($name in $publishNames) { Copy-Item -LiteralPath (Join-Path $stagingDir $name) -Destination (Join-Path $outputDir $name) }
$published = @(Get-ChildItem -LiteralPath $outputDir -Force)
if (@($published | Where-Object { $_.PSIsContainer -or $_.Name -notin @($windowsName, $linuxName) }).Count) { throw 'Unexpected output under dist' }
if ($Target -eq 'All' -and $published.Count -ne 2) { throw 'dist must contain exactly two files' }
Write-Host "Desktop release $version built successfully:"
$published | ForEach-Object { Write-Host "  $($_.Name)" }
