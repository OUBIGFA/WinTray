param(
  [Parameter(Mandatory=$true)][string]$NsisExe,
  [string]$PayloadDir = 'publish/WinTray-Portable',
  [string]$Version = '3.5.0'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$payload = [IO.Path]::GetFullPath((Join-Path $root $PayloadDir))
$testRoot = Join-Path $root ('_temp/installer-test-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($testRoot) | Out-Null
$key = 'Software\WinTrayInstallerTests\' + [Guid]::NewGuid().ToString('N')
$exe = Join-Path $testRoot 'WinTray-TestInstaller.exe'
$installation = Join-Path $testRoot 'installed'
$profile = Join-Path $testRoot 'profile/WinTray/settings.json'
[IO.Directory]::CreateDirectory((Split-Path -Parent $profile)) | Out-Null
[IO.File]::WriteAllText($profile, '{"language":"zh-CN","managedApps":[{"id":"portable-existing"}]}', [Text.UTF8Encoding]::new($false))
$profileHash = (Get-FileHash -LiteralPath $profile).Hash

function Invoke-TestProcess([string]$Path, [string]$Arguments, [int]$ExpectedExitCode = 0) {
  $process = Start-Process -FilePath $Path -ArgumentList $Arguments -WindowStyle Hidden -PassThru
  if (-not $process.WaitForExit(60000)) { throw "Installer test timed out: $Path" }
  if ($process.ExitCode -ne $ExpectedExitCode) { throw "Installer test failed ($($process.ExitCode), expected $ExpectedExitCode): $Path" }
}

try {
  & $NsisExe /V2 /INPUTCHARSET UTF8 "/DWINTRAY_PAYLOAD_DIR=$payload" "/DWINTRAY_OUTPUT=$exe" "/DWINTRAY_VERSION=$Version" "/DWINTRAY_REGISTRY_KEY=$key" "/DWINTRAY_TEST_ROOT=$testRoot" (Join-Path $PSScriptRoot 'installer.nsi')
  if ($LASTEXITCODE -ne 0) { throw 'Test installer compilation failed' }
  Invoke-TestProcess $exe ('/S /D=' + $installation)
  $files = @('WinTray.exe','WinTray-Recovery.exe','WinTray.exe.manifest','checksums.txt','ThirdPartyNotices.txt','LICENSE.txt',
    'native/x64/WinTray-Bridge.exe','native/x64/WinTray-ShellBridge.dll','native/x86/WinTray-Bridge.exe','native/x86/WinTray-ShellBridge.dll')
  foreach ($file in $files) {
    if ((Get-FileHash -LiteralPath (Join-Path $installation $file)).Hash -ne (Get-FileHash -LiteralPath (Join-Path $payload $file)).Hash) { throw "Installed payload differs: $file" }
  }
  if (-not (Test-Path -LiteralPath (Join-Path $testRoot 'WinTray.lnk'))) { throw 'Start shortcut missing' }
  $registration = Get-ItemProperty -LiteralPath ('HKCU:\' + $key)
  if ($registration.DisplayVersion -ne $Version -or $registration.InstallLocation -ne $installation) { throw 'Uninstall registration differs' }
  $extra = Join-Path $installation 'user-file.txt'
  [IO.File]::WriteAllText($extra, 'Keep this user file', [Text.UTF8Encoding]::new($false))
  Invoke-TestProcess $exe ('/S /D=' + $installation)
  if ([IO.File]::ReadAllText($extra) -ne 'Keep this user file') { throw 'Upgrade changed a user file' }
  # Run a copy so Windows does not lock the installed uninstaller itself.
  $uninstaller = Join-Path $testRoot 'Uninstall-test.exe'
  Copy-Item -LiteralPath (Join-Path $installation 'Uninstall.exe') -Destination $uninstaller
  $lockedFile = [IO.File]::Open((Join-Path $installation 'native/x64/WinTray-ShellBridge.dll'), 'Open', 'Read', 'None')
  try {
    Invoke-TestProcess $uninstaller ('/S _?=' + $installation) 5
    if (-not (Test-Path -LiteralPath (Join-Path $installation 'WinTray.exe')) -or -not (Test-Path -LiteralPath ('HKCU:\' + $key))) { throw 'Failed uninstall removed its retry entry points' }
  } finally { $lockedFile.Dispose() }
  Invoke-TestProcess $uninstaller ('/S _?=' + $installation)
  foreach ($file in $files + @('Uninstall.exe')) {
    if (Test-Path -LiteralPath (Join-Path $installation $file)) { throw "Uninstall left an installed file: $file" }
  }
  if (Test-Path -LiteralPath ('HKCU:\' + $key)) { throw 'Uninstall registration remained' }
  if (Test-Path -LiteralPath (Join-Path $testRoot 'WinTray.lnk')) { throw 'Start shortcut remained' }
  if ((Get-FileHash -LiteralPath $profile).Hash -ne $profileHash -or [IO.File]::ReadAllText($extra) -ne 'Keep this user file') { throw 'Uninstall changed user data' }
  Write-Host 'Installer lifecycle passed: install, payload hashes, overwrite, locked-file failure, uninstall retry, retained profile and extra files.'
} finally {
  # Only the isolated test key and this invocation's directory are owned here.
  if (Test-Path -LiteralPath ('HKCU:\' + $key)) { Remove-Item -LiteralPath ('HKCU:\' + $key) -Recurse }
  Add-Type -AssemblyName Microsoft.VisualBasic
  [Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory($testRoot, 'OnlyErrorDialogs', 'SendToRecycleBin')
}
