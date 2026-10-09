param(
  [string]$OutputDir = "dist",
  [string]$PackageDir = "publish",
  [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {

$localConfig = Join-Path $PSScriptRoot "package.local.ps1"
if (Test-Path $localConfig) {
  . $localConfig
}

$goCommand = "go"
if ($GoExe) {
  if (!(Test-Path $GoExe)) {
    throw "Configured Go executable was not found: $GoExe"
  }
  $goCommand = $GoExe
}

$out = Join-Path $root $OutputDir
if (!(Test-Path $out)) {
  New-Item -ItemType Directory -Path $out | Out-Null
}

$exe = Join-Path $out "WinTray.exe"
$manifestSource = Join-Path $PSScriptRoot "WinTray.exe.manifest"
$manifestTarget = "$exe.manifest"

$ldflags = "-s -w -H=windowsgui"
if ($Version) {
  $ldflags += " -X wintray/internal/version.Number=$($Version.TrimStart('v'))"
}

& $goCommand build -trimpath -ldflags $ldflags -o $exe ./cmd/wintray
if ($LASTEXITCODE -ne 0) {
  throw "go build failed"
}

# Keep crash recovery in a separate process image so ending WinTray.exe by
# name does not also end the component that restores collected tray icons.
$recoveryExe = Join-Path $out "WinTray-Recovery.exe"
& $goCommand build -trimpath -ldflags "-s -w -H=windowsgui" -o $recoveryExe ./cmd/tray-recovery
if ($LASTEXITCODE -ne 0) { throw "tray recovery build failed" }

if (Test-Path $manifestSource) {
  Copy-Item -Path $manifestSource -Destination $manifestTarget -Force
}

$hash = (Get-FileHash $exe -Algorithm SHA256).Hash
$checksumsPath = Join-Path $out "checksums.txt"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
$recoveryHash = (Get-FileHash $recoveryExe -Algorithm SHA256).Hash
[System.IO.File]::WriteAllText($checksumsPath, "WinTray.exe  $hash`nWinTray-Recovery.exe  $recoveryHash`n", $utf8NoBom)
Write-Host "Built: $exe"

$publishDir = Join-Path $root $PackageDir
if (!(Test-Path $publishDir)) {
  New-Item -ItemType Directory -Path $publishDir | Out-Null
}

$portableDir = Join-Path $publishDir "WinTray-Portable"
if (!(Test-Path $portableDir)) {
  New-Item -ItemType Directory -Path $portableDir | Out-Null
}

$portableExe = Join-Path $portableDir "WinTray.exe"
$portableManifest = Join-Path $portableDir "WinTray.exe.manifest"
$portableChecksums = Join-Path $portableDir "checksums.txt"

Copy-Item -Path $exe -Destination $portableExe -Force
Copy-Item -LiteralPath $recoveryExe -Destination (Join-Path $portableDir "WinTray-Recovery.exe") -Force
Copy-Item -Path $checksumsPath -Destination $portableChecksums -Force
if (Test-Path $manifestTarget) {
  Copy-Item -Path $manifestTarget -Destination $portableManifest -Force
}

# Ship notices for the modules actually linked into this executable, using
# the same resolved versions as the build. Cache paths never enter the file.
$dependencyRows = & $goCommand list -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}' ./cmd/wintray
if ($LASTEXITCODE -ne 0) { throw "Could not list executable dependencies" }
$noticeParts = [System.Collections.Generic.List[string]]::new()
$goRoot = & $goCommand env GOROOT
if ($LASTEXITCODE -ne 0) { throw "Could not resolve Go license" }
$noticeParts.Add("Go" + [Environment]::NewLine + [IO.File]::ReadAllText((Join-Path $goRoot 'LICENSE')))
foreach ($dependency in ($dependencyRows | Where-Object { $_ } | Sort-Object -Unique)) {
  $parts = $dependency -split '\|', 3
  if ($parts[0] -eq 'wintray') { continue }
  $licensePath = @('LICENSE', 'LICENSE.txt', 'LICENSE.md', 'COPYING') |
    ForEach-Object { Join-Path $parts[2] $_ } |
    Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
    Select-Object -First 1
  if (!$licensePath) { throw "Missing license for $($parts[0])" }
  $noticeParts.Add($parts[0] + ' ' + $parts[1] + [Environment]::NewLine + [IO.File]::ReadAllText($licensePath))
}
[IO.File]::WriteAllText((Join-Path $portableDir 'ThirdPartyNotices.txt'), ($noticeParts -join ([Environment]::NewLine + [Environment]::NewLine)), $utf8NoBom)
Copy-Item -LiteralPath (Join-Path $root 'LICENSE') -Destination (Join-Path $portableDir 'LICENSE.txt') -Force

$zipName = "WinTray-Portable.zip"
$zipTarget = Join-Path $publishDir $zipName
$tempZipTarget = Join-Path $publishDir "WinTray-Portable.tmp.zip"
if (Test-Path $tempZipTarget) {
  $timestamp = Get-Date -Format "yyyyMMddHHmmss"
  Move-Item -Path $tempZipTarget -Destination (Join-Path $publishDir "WinTray-Portable.tmp.$timestamp.zip") -Force
}

Compress-Archive -Path (Join-Path $portableDir "*") -DestinationPath $tempZipTarget -Force
Move-Item -Path $tempZipTarget -Destination $zipTarget -Force
$archiveHash = (Get-FileHash $zipTarget -Algorithm SHA256).Hash
$archiveChecksumsPath = Join-Path $publishDir (([System.IO.Path]::GetFileNameWithoutExtension($zipTarget)) + ".sha256")
[System.IO.File]::WriteAllText($archiveChecksumsPath, "$archiveHash  $(Split-Path $zipTarget -Leaf)" + [Environment]::NewLine, $utf8NoBom)
Write-Host "Packaged portable version: $portableDir"
Write-Host "Packaged portable archive: $zipTarget"

} finally {
  Pop-Location
}
