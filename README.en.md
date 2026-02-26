<div align="center">

# WinTray

**A Windows tray utility that auto-organizes your desktop at startup**

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%2F11-lightgrey.svg)]()
[![Go 1.22](https://img.shields.io/badge/Go-1.22-00ADD8.svg)]()

[中文说明](README.md) | English

</div>

---

## Overview

WinTray is a Windows startup organizer that lives in the system tray. When triggered at logon, it automatically handles configured application windows — such as hiding or minimizing them — so your desktop is always clean without any manual effort.

---

## Features

- **Tray resident**: Sits in the notification area with quick actions — open settings, view logs, exit
- **Managed app list**: Add any number of programs and configure per-app behavior
- **Run at logon**: Writes to the current user `Run` registry key to start with Windows
- **Auto-hide window**: When configured, the `--autorun` flow automatically minimizes and hides target windows
- **Window retry control**: Configurable 0–120 s retry wait to handle slow-starting programs
- **Bilingual UI**: Built-in Simplified Chinese / English, switchable at any time
- **Single-instance lock**: Prevents duplicate launches and configuration conflicts

---

## Requirements

| Item | Requirement |
|---|---|
| OS | Windows 10 / 11 |
| Runtime | None — standalone executable |
| Build from source | Go 1.22+ |

---

## Download & Usage

WinTray is **portable only** — no installer required.

Go to the [Releases](../../releases) page, download `WinTray-Portable.zip`, extract it, and run `WinTray.exe` directly.

- Configuration and logs are written to `%LOCALAPPDATA%\WinTray\` — no registry installation entries
- To remove completely, just close the program and delete the folder

---

## Data Directory

| Type | Path |
|---|---|
| Settings | `%LOCALAPPDATA%\WinTray\settings.json` |
| Log file | `%LOCALAPPDATA%\WinTray\wintray.log` |

---

## CLI Flags

| Flag | Description |
|---|---|
| `--background` | Start without showing the main window (for logon startup) |
| `--autorun` | Execute managed startup tasks automatically |

---

## Build Locally

```powershell
powershell -ExecutionPolicy Bypass -File build/package.ps1 -OutputDir dist
```

Build artifacts:

| File | Description |
|---|---|
| `dist/WinTray.exe` | Main executable |
| `dist/WinTray.exe.manifest` | Application manifest (DPI awareness, etc.) |
| `dist/checksums.txt` | SHA256 checksum file |
| `publish/WinTray-Portable.zip` | Portable ZIP archive |

---

## Auto-Release (GitHub Actions)

Push a `v*` tag to trigger the CI workflow, which builds and uploads Release assets automatically:

```bash
git tag v0.1.0
git push origin v0.1.0
```

Release assets: `WinTray.exe`, `WinTray.exe.manifest`, `checksums.txt`

---

## Project Layout

```text
.
├─ .github/workflows/      # CI/CD and release automation
├─ build/                  # packaging script (package.ps1) and manifest
├─ cmd/wintray/            # entrypoint
└─ internal/               # core implementation
```

---

## FAQ

**Q: There's no main window after launch — how do I open settings?**
A: Right-click the WinTray icon in the system tray and choose "Open Settings".

**Q: How do I disable run-at-logon?**
A: Uncheck "Run at logon" on the settings page. WinTray removes the registry entry automatically.

**Q: A program in my list isn't being hidden at startup.**
A: Make sure the program has "Auto-hide window" enabled, and that WinTray was launched with the `--autorun` flag (passed automatically by Windows when using run-at-logon). If the program starts slowly, try increasing the retry wait time.

---

## License

This project is released under the [MIT License](LICENSE).
