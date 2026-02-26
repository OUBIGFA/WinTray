# WinTray

[中文说明](README.md)

WinTray is a Windows tray utility that manages configured app windows during startup automation (for example auto-hiding/closing target windows), so your desktop is organized without manual cleanup.

## Features

- Runs in the system tray with quick actions (open settings, open logs, exit)
- Configurable managed-app list with per-app behavior
- Optional run-at-logon via current user `Run` registry key
- Background launch mode for startup scenarios
- Window handling retry control (0-120 seconds)
- Built-in Chinese/English UI localization
- Single-instance lock to prevent duplicate launches

## Requirements

- Windows 10/11
- Go 1.22+ (only needed when building from source)

## Quick Start

### Option 1: Download from Releases (recommended)

1. Open the repository `Releases` page
2. Download `WinTray.exe`
3. Run it and configure managed apps

### Option 2: Build locally

```powershell
powershell -ExecutionPolicy Bypass -File build/package.ps1 -OutputDir dist
```

Build artifacts:

- `dist/WinTray.exe`
- `dist/WinTray.exe.manifest`
- `dist/checksums.txt`
- `publish/WinTray-Portable.zip` (Portable ZIP archive)

## Usage

- After adding a managed app, you can configure:
  - whether to run that app at startup
  - whether to auto-minimize/hide its window after launch
- Managed handling is triggered by the `--autorun` startup flow
- Log path: `%LOCALAPPDATA%\WinTray\wintray.log`
- Settings path: `%LOCALAPPDATA%\WinTray\settings.json`

## CLI Flags

- `--background`: start without showing main window
- `--autorun`: execute managed startup tasks automatically

## GitHub Release Publishing

Built-in workflow: `.github/workflows/release.yml`

Create and push a version tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow publishes these assets to the GitHub Release:

- `WinTray.exe`
- `WinTray.exe.manifest`
- `checksums.txt`

## Project Layout

```text
.
├─ .github/workflows/      # CI/CD and release automation
├─ build/                  # packaging scripts and manifest
├─ cmd/wintray/            # entrypoint
└─ internal/               # core implementation
```
