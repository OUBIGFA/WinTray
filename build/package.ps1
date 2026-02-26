param(
  [string]$OutputDir = "dist"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root $OutputDir
if (!(Test-Path $out)) {
  New-Item -ItemType Directory -Path $out | Out-Null
}

$exe = Join-Path $out "WinTray.exe"
$manifestSource = Join-Path $PSScriptRoot "WinTray.exe.manifest"
$manifestTarget = "$exe.manifest"

go build -trimpath -ldflags "-s -w -H=windowsgui" -o $exe ./cmd/wintray
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

# Create a portable zip package
$zipTarget = Join-Path $publishDir "WinTray-Portable.zip"
if (Test-Path $zipTarget) {
  Remove-Item -Path $zipTarget -Force
}

$filesToZip = @($exe, $checksumsPath)
if (Test-Path $manifestTarget) {
  $filesToZip += $manifestTarget
}

Compress-Archive -Path $filesToZip -DestinationPath $zipTarget -Force
Write-Host "Packaged portable version: $zipTarget"

