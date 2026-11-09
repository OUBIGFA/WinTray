param(
  [string]$OutputDir = "dist",
  [string]$PackageDir = "publish",
  [string]$Version = "",
  [string]$NsisExe = ""
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {

$localConfig = Join-Path $PSScriptRoot "package.local.ps1"
if (Test-Path $localConfig) {
  . $localConfig
}

if (-not $Version) {
  $versionSource = [IO.File]::ReadAllText((Join-Path $root 'internal/version/version.go'))
  $match = [regex]::Match($versionSource, 'var Number = "(\d+\.\d+\.\d+)"')
  if (-not $match.Success) { throw 'Could not read the application version' }
  $Version = $match.Groups[1].Value
}
$Version = $Version.TrimStart('v')
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw 'Version must be X.Y.Z or vX.Y.Z' }
if (-not $NsisExe) {
  $candidate = Get-Command makensis.exe -ErrorAction SilentlyContinue
  if ($candidate) { $NsisExe = $candidate.Source }
}
if (-not $NsisExe) {
  $candidate = Join-Path ${env:ProgramFiles(x86)} 'NSIS/makensis.exe'
  if (Test-Path -LiteralPath $candidate) { $NsisExe = $candidate }
}
if (-not $NsisExe -or -not (Test-Path -LiteralPath $NsisExe -PathType Leaf)) {
  throw 'NSIS compiler not found. Pass -NsisExe or install NSIS.'
}
$scratch = Join-Path $root ('_temp/package-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($scratch) | Out-Null

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

# The private broker uses bitness-matched native bridges instead of creating
# another Shell_TrayWnd. Both architectures are required by the portable app.
& (Join-Path $PSScriptRoot 'native.ps1') -OutputDir (Join-Path $out 'native')

if (Test-Path $manifestSource) {
  Copy-Item -Path $manifestSource -Destination $manifestTarget -Force
}

$checksumsPath = Join-Path $out "checksums.txt"
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
$runtimeFiles = @('WinTray.exe', 'WinTray-Recovery.exe')
foreach ($arch in @('x64', 'x86')) {
  $runtimeFiles += "native/$arch/WinTray-Bridge.exe", "native/$arch/WinTray-ShellBridge.dll"
}
$checksums = foreach ($file in $runtimeFiles) {
  "$file  $((Get-FileHash (Join-Path $out $file) -Algorithm SHA256).Hash)"
}
[System.IO.File]::WriteAllText($checksumsPath, ($checksums -join "`n") + "`n", $utf8NoBom)
Write-Host "Built: $exe"

$publishDir = Join-Path $root $PackageDir
if (!(Test-Path $publishDir)) {
  New-Item -ItemType Directory -Path $publishDir | Out-Null
}

$portableDir = Join-Path $scratch "WinTray-Portable"
if (!(Test-Path $portableDir)) {
  New-Item -ItemType Directory -Path $portableDir | Out-Null
}

# A fresh staging directory prevents stale files or a local user profile from
# entering either artifact. Both formats are built from exactly these bytes.
$portableExe = Join-Path $portableDir "WinTray.exe"
$portableManifest = Join-Path $portableDir "WinTray.exe.manifest"
$portableChecksums = Join-Path $portableDir "checksums.txt"

foreach ($file in $runtimeFiles) {
  $target = Join-Path $portableDir $file
  New-Item -ItemType Directory -Path (Split-Path -Parent $target) -Force | Out-Null
  Copy-Item -LiteralPath (Join-Path $out $file) -Destination $target -Force
}
Copy-Item -Path $checksumsPath -Destination $portableChecksums -Force
if (Test-Path $manifestTarget) {
  Copy-Item -Path $manifestTarget -Destination $portableManifest -Force
}

# Ship notices for the modules actually linked into this executable, using
# the same resolved versions as the build. Cache paths never enter the file.
$dependencyRows = & $goCommand list -deps -f '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}' ./cmd/wintray
if ($LASTEXITCODE -ne 0) { throw "Could not list executable dependencies" }
$noticeParts = [System.Collections.Generic.List[string]]::new()
$noticeParts.Add([IO.File]::ReadAllText((Join-Path $root 'internal/ui/icons/LICENSE.lucide')))
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
$tempZipTarget = Join-Path $scratch "WinTray-Portable.zip"

Compress-Archive -Path (Join-Path $portableDir "*") -DestinationPath $tempZipTarget -Force

# The installer uses the same staged files as the portable archive. It writes
# only the program directory and shortcuts; WinTray's configuration stays in
# the user data directory and is therefore preserved during uninstall.
$setupTarget = Join-Path $publishDir "WinTray-Setup.exe"
$setupTemp = Join-Path $scratch "WinTray-Setup.exe"
& $NsisExe /V2 /INPUTCHARSET UTF8 "/DWINTRAY_PAYLOAD_DIR=$portableDir" "/DWINTRAY_OUTPUT=$setupTemp" "/DWINTRAY_VERSION=$Version" (Join-Path $PSScriptRoot 'installer.nsi')
if ($LASTEXITCODE -ne 0) { throw "installer build failed" }

# Replace the fixed deliverables only after both build successfully.
Copy-Item -LiteralPath $tempZipTarget -Destination $zipTarget -Force
Copy-Item -LiteralPath $setupTemp -Destination $setupTarget -Force
$publishedPortable = Join-Path $publishDir 'WinTray-Portable'
[IO.Directory]::CreateDirectory($publishedPortable) | Out-Null
Copy-Item -Path (Join-Path $portableDir '*') -Destination $publishedPortable -Recurse -Force
$archiveHash = (Get-FileHash $zipTarget -Algorithm SHA256).Hash
[IO.File]::WriteAllText((Join-Path $publishDir 'WinTray-Portable.sha256'), "$archiveHash  WinTray-Portable.zip" + [Environment]::NewLine, $utf8NoBom)
$setupHash = (Get-FileHash $setupTarget -Algorithm SHA256).Hash
[IO.File]::WriteAllText((Join-Path $publishDir 'WinTray-Setup.sha256'), "$setupHash  WinTray-Setup.exe" + [Environment]::NewLine, $utf8NoBom)
Write-Host "Packaged portable archive: $zipTarget"
Write-Host "Packaged installer: $setupTarget"

} finally {
  Pop-Location
  if ($scratch -and (Test-Path -LiteralPath $scratch)) {
    # Only this invocation's GUID directory is owned by this build.
    Add-Type -AssemblyName Microsoft.VisualBasic
    [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory($scratch, 'OnlyErrorDialogs', 'SendToRecycleBin')
  }
}
