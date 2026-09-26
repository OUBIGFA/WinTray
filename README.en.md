<div align="center">

# WinTray

<img src="internal/branding/assets/logo.png" alt="WinTray Logo" width="120" />

**A lightweight Windows startup organizer for silent launch and window management**

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%2F11-lightgrey.svg)]()
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8.svg)]()

English | [简体中文](README.md)

</div>

---

## Introduction

**WinTray** is a lightweight Windows startup organizer designed to eliminate desktop clutter from startup popups and flashing console windows, giving you a clean desktop after every boot.

Core use cases:

- **Auto-Hide Windows**: Automatically closes the main window of applications lacking a native "start minimized to tray" option, keeping them running quietly in the tray.
- **Silent Script Launch**: Runs `.bat`, `.cmd`, `.ps1`, `.py`, and `.pyw` scripts completely hidden in the background without intrusive console windows.

![](image/01.png)

![](image/02.png)

---

## Features

- **Tray Resident**: Lives in the notification area with one-click access to settings and exit
- **Managed Program List**: Maintain any number of programs, each with independent behavior configuration
- **Auto Start**: Registers a per-user logon task that starts WinTray right after sign-in; no administrator rights needed
- **Close to Tray**: When configured in the program list, the sign-in task closes the target window while the program keeps running in the tray
- **Wait for a Window**: 0–120 seconds, for slow-starting programs whose window shows up late
- **Wait Before Closing**: Each program can run for a set number of seconds before its window is closed, to skip login dialogs and other pre-launch popups (such as the new QQ)
- **Staggered Startup**: Launch programs in list order, 3 seconds apart by default; adjust the interval from 0–120 seconds in Global Settings to reduce competing startup workloads
- **Cleanup & Restore Defaults**: One-click cleanup of local config/logs from the main window
- **Bilingual UI**: Built-in Simplified Chinese / English, switchable instantly
- **Single Instance Protection**: Prevents duplicate launches to avoid configuration conflicts

---

## Download & Usage

WinTray is **portable only** — no installation needed.

Go to the [Releases](../../releases) page, download the latest `WinTray-Portable.zip`, extract it, and run `WinTray.exe` directly.

- Configuration and logs are stored in `%LOCALAPPDATA%\WinTray\` with no registry dependencies
- Exit WinTray first, then delete the folder to remove it completely

---

## Supported Program Types

WinTray supports adding the following program types to the managed list, automatically launching and handling their windows at startup:

| Type              | File Extension  | Launch Behavior                                                               |
| ----------------- | --------------- | ----------------------------------------------------------------------------- |
| Executable        | `.exe`          | Starts normally by default; can be set to "Close to tray"; console programs get a WinTray-hosted tray icon |
| Batch script      | `.bat` / `.cmd` | Runs in the background by default (no console window)                         |
| PowerShell script | `.ps1`          | Runs in the background by default                                             |
| Python script     | `.py` / `.pyw`  | Runs in the background by default, invokes `python.exe` / `pythonw.exe`        |

> Non-`.exe` scripts (`.bat` / `.cmd` / `.ps1` / `.py` / `.pyw`) default to "Run in background" when added and cannot use "Close to tray".

> **Note:** Some applications contain multiple `.exe` files (launchers, updaters, etc.); pick the one that owns the main window, or "Close to tray" won't take effect.

### Per-Program Configuration Options

- **How it starts**: **Start normally** (just starts the program, window untouched) / **Close to tray** (closes the main window after launch, the program keeps running in the tray) / **Run in background** (no window at all, best for scripts and command-line tools)
- **Wait before closing**: Available with "Close to tray", 0–600 seconds — the window is closed only after the program has been running that long, skipping sign-in windows (10–15 seconds for the new QQ). Covers instances started by the program's own auto-start too
- **Arguments**: Command-line arguments passed to the program
- **Start this program at sign-in**: Uncheck it to pause the program, which is then skipped at sign-in; "Launch Now" remains available

> Console programs without a tray icon of their own (such as `syncthing.exe` or `frpc.exe`) get a WinTray-hosted tray icon instead: left-click shows/hides the window, and the context menu can show, hide, stop hosting or quit the program.

### Collect Tray Icons

Check "Collect tray icon" in a program's editor, below "How it starts", and its tray icon moves into WinTray's right-click menu, shown with its own icon and name; click the entry to open the program. Uncheck to restore. Requires Windows 11.

### Staggered Startup

The global **Delay between programs** sets the minimum gap between launches: **3 seconds** by default, configurable from **0–120 seconds**. Launches follow list order, and "Launch Now" is unaffected.

### Common Use Cases

| Scenario                                                     | Configuration                                            |
| ------------------------------------------------------------ | -------------------------------------------------------- |
| WeChat / DingTalk auto-start and minimize to tray            | Add `.exe`, set "How it starts" to "Close to tray"        |
| New QQ auto-start, minimize to tray after login | Add `QQ.exe`, set "Close to tray" and "Wait before closing" to 10–15 s |
| syncthing / frpc console programs running in the background, reachable from the tray | Add `.exe`, set "Close to tray"; WinTray hosts the tray icon |
| Tunnel scripts (frpc / SSH) running in background at startup | Add `.bat` / `.ps1`, set "Run in background"              |
| Python crawler/service starting silently in background       | Add `.py`, set "Run in background"                        |
| Auto-start only, no window handling                          | Add program, keep "Start normally"                        |

---

## System Requirements

The source and release package support Windows only; cross-platform builds are not supported.

| Item              | Requirement                                        |
| ----------------- | -------------------------------------------------- |
| OS                | Windows 10 / 11                                    |
| Runtime           | No additional dependencies (standalone executable) |
| Build from source | Go 1.25+                                           |

---

## Data Directory

| Type          | Path                                   |
| ------------- | -------------------------------------- |
| Configuration | `%LOCALAPPDATA%\WinTray\settings.json` |
| Logs          | `%LOCALAPPDATA%\WinTray\wintray.log`   |

---

## Command-Line Arguments

| Argument            | Description                                                      |
| ------------------- | ---------------------------------------------------------------- |
| `--background`      | Start without showing the main window (used for auto-start)      |
| `--autorun`         | Run the startup programs list (used by auto-start)               |
| `--cleanup-restore` | Clear `%LOCALAPPDATA%\WinTray\` and exit                         |
| `--host`            | Compatibility with older callers only; not needed by new launches |

**Exit WinTray when sign-in tasks finish** (optional, in Settings): exits once the tasks are done; while tray icons are still in use it waits, and exits after the last one ends.

**Run silently** (in the settings window footer and tray menu): hides the window and runs in the background; run WinTray again to restore it.

With "Exit WinTray when sign-in tasks finish" unchecked, WinTray keeps its own tray icon, and "Don't show the WinTray window at sign-in" controls whether settings are shown in that mode.

---

## Project Structure

```text
.
├─ .github/workflows/      # CI/CD and Release automation
├─ build/                  # Build scripts (package.ps1) and app manifest
├─ cmd/wintray/            # Program entry point
└─ internal/               # Core business logic
```

---

## FAQ

**Q: I don't see a main window after launch — how do I access settings?**
A: Right-click WinTray's tray icon and select "Open WinTray", or run `WinTray.exe` again.

**Q: How do I disable auto-start after it's been enabled?**
A: Turn off "Run WinTray at sign-in" in the settings page; the logon task and registry entry are removed automatically.

**Q: The new QQ doesn't minimize to the tray; instead the login fails or QQ quits.**
A: The new QQ shows a login window first and quits if it is closed too early. Set "Wait before closing" longer than the whole login (auto-login usually takes 15–30 seconds).

**Q: Can QQ's own auto-start and WinTray launch two instances?**
A: No. When the program's own `Run` entry is enabled, WinTray just waits for Windows to start it and **never launches it again**, so there's no duplicate.

**Q: A program in my list isn't being minimized to the tray.**
A: Make sure the program is set to "Close to tray" and WinTray's auto-start is enabled. If the program starts slowly, increase "Wait for a window up to".

---

## License

This project is released under the [MIT License](LICENSE).

