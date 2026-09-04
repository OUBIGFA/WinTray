param(
  [string]$OutputDir = "dist",
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

if (Test-Path $manifestSource) {
  Copy-Item -Path $manifestSource -Destination $manifestTarget -Force
}

$hash = (Get-FileHash $exe -Algorithm SHA256).Hash
$checksumsPath = Join-Path $out "checksums.txt"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($checksumsPath, "WinTray.exe  $hash`n", $utf8NoBom)
Write-Host "Built: $exe"

$publishDir = Join-Path $root "publish"
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
Copy-Item -Path $checksumsPath -Destination $portableChecksums -Force
if (Test-Path $manifestTarget) {
  Copy-Item -Path $manifestTarget -Destination $portableManifest -Force
}

$zipName = if ($Version) { "WinTray-Portable-$Version.zip" } else { "WinTray-Portable.zip" }
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
