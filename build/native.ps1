param(
  [string]$OutputDir = 'dist/native',
  [switch]$Test
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$out = if ([IO.Path]::IsPathRooted($OutputDir)) { $OutputDir } else { Join-Path $root $OutputDir }
$vswhere = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio/Installer/vswhere.exe'
if (!(Test-Path -LiteralPath $vswhere)) {
  throw 'The native tray bridge requires Visual Studio C++ Build Tools (x86/x64 and Windows SDK).'
}
$vs = & $vswhere -latest -prerelease -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
if (!$vs) { throw 'Visual Studio C++ x86/x64 tools were not found.' }
$vcvars = Join-Path $vs 'VC/Auxiliary/Build/vcvarsall.bat'
$src = Join-Path $root 'native/traybridge'

foreach ($arch in @('x64', 'x86')) {
  $dest = Join-Path $out $arch
  $work = Join-Path $root "_temp/native-build/$arch"
  New-Item -ItemType Directory -Path $dest, $work -Force | Out-Null
  $target = if ($arch -eq 'x64') { 'amd64' } else { 'amd64_x86' }
  $export = if ($arch -eq 'x86') { '/EXPORT:WinTrayShellHook=_WinTrayShellHook@12' } else { '' }
  $dll = Join-Path $dest 'WinTray-ShellBridge.dll'
  $exe = Join-Path $dest 'WinTray-Bridge.exe'
  $testCommands = ''
  if ($Test) {
    $testDir = Join-Path $root "_temp/native-tests/$arch"
    New-Item -ItemType Directory -Path $testDir -Force | Out-Null
    $client = Join-Path $testDir 'WinTray-TrayClient.exe'
    $testCommands = @"
cl /nologo /O2 /MT /W4 /WX /std:c11 /DUNICODE /D_UNICODE "$src/tests/client.c" /Fe:"$client" /link /INCREMENTAL:NO /DYNAMICBASE /NXCOMPAT /SUBSYSTEM:CONSOLE user32.lib shell32.lib
if errorlevel 1 exit /b 1
"$client" --abi "$dll"
if errorlevel 1 exit /b 1
cl /nologo /O2 /MT /W4 /WX /std:c11 /DUNICODE /D_UNICODE /DWINTRAY_BRIDGE_TEST /LD "$src/bridge.c" /Fe:"$testDir/WinTray-ShellBridge.dll" /link /INCREMENTAL:NO /IMPLIB:"$work/test-bridge.lib" $export user32.lib comctl32.lib
if errorlevel 1 exit /b 1
cl /nologo /O2 /MT /W4 /WX /std:c11 /DUNICODE /D_UNICODE /DWINTRAY_BRIDGE_TEST "$src/injector.c" /Fe:"$testDir/WinTray-Bridge.exe" /link /INCREMENTAL:NO user32.lib
if errorlevel 1 exit /b 1
"@
  }
  # Compiler intermediates and the private shell fixture stay outside the
  # portable directory. Only the receiving thread uses a native subclass.
  $commands = @"
@echo off
set "PATH=$((Split-Path -Parent $vswhere));%PATH%"
call "$vcvars" $target >nul
if errorlevel 1 exit /b 1
cd /d "$work"
cl /nologo /O2 /MT /W4 /WX /std:c11 /DUNICODE /D_UNICODE /LD "$src/bridge.c" /Fe:"$dll" /link /INCREMENTAL:NO /DYNAMICBASE /NXCOMPAT /Brepro /IMPLIB:"$work/bridge.lib" $export user32.lib comctl32.lib
if errorlevel 1 exit /b 1
cl /nologo /O2 /MT /W4 /WX /std:c11 /DUNICODE /D_UNICODE "$src/injector.c" /Fe:"$exe" /link /INCREMENTAL:NO /DYNAMICBASE /NXCOMPAT /Brepro /SUBSYSTEM:CONSOLE user32.lib
if errorlevel 1 exit /b 1
$testCommands
"@
  $script = Join-Path $work 'build.cmd'
  # cmd reads its script using the active ANSI code page; all paths remain
  # quoted, including Visual Studio installations and workdirs with spaces.
  [IO.File]::WriteAllText($script, $commands, [Text.Encoding]::Default)
  & $env:ComSpec /d /c "`"$script`""
  if ($LASTEXITCODE -ne 0) { throw "Native tray bridge build failed ($arch)." }
  Write-Host "Built native tray bridge: $dest"
}
