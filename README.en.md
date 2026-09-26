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
- **Auto Start**: Registers a per-user logon task that starts WinTray right after sign-in, ahead of the `Run` entries Explorer launches one by one; no administrator rights needed. Falls back to the current user's `Run` registry key if the task cannot be registered
- **Close to Tray**: When configured in the program list, the sign-in task closes the target window while the program keeps running in the tray
- **Wait for a Window**: 0–120 seconds, for slow-starting programs whose window shows up late
- **Wait Before Closing**: Each program can run for a set number of seconds before its window is closed, to skip login dialogs and other pre-launch popups (such as the new QQ); also covers instances started by the program's own auto-start
- **Staggered Startup**: Launch programs in list order, 3 seconds apart by default; adjust the interval from 0–120 seconds in Global Settings to reduce competing startup workloads
- **Cleanup & Restore Defaults**: One-click cleanup of local config/logs from the main window
- **Bilingual UI**: Built-in Simplified Chinese / English, switchable instantly
- **Single Instance Protection**: Prevents duplicate launches to avoid configuration conflicts

---

## Download & Usage

WinTray is **portable only** — no installation needed.

Go to the [Releases](../../releases) page, download the latest `WinTray-Portable.zip`, extract it, and run `WinTray.exe` directly.

- Configuration and logs are stored in `%LOCALAPPDATA%\WinTray\` with no registry dependencies
- Exit WinTray before deleting the folder: `WinTray.exe` remains in use while it runs, and removing the folder afterwards is all it takes to clean up

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

> **Important:** Some applications contain multiple `.exe` files, such as launchers, updaters, helper processes, and the actual main program. When adding an application, select the executable that owns the main visible window. Selecting the wrong `.exe` may allow the program to start, but WinTray may not find its window, so "Close to tray" will not take effect.

### Per-Program Configuration Options

- **How it starts**: **Start normally** (just starts the program, window untouched) / **Close to tray** (closes the main window after launch, the program keeps running in the tray) / **Run in background** (no window at all, best for scripts and command-line tools)
- **Wait before closing**: Available with "Close to tray", 0–600 seconds. WinTray only looks for the window and closes it once the program's process has been running that long, which skips a sign-in window — about 10–15 seconds for the new QQ. The delay counts from process creation, so instances started by the program's own auto-start are covered too, and that auto-start can stay enabled
- **Arguments**: Command-line arguments passed to the program
- **Start this program at sign-in**: Uncheck it to pause the program, which is then skipped at sign-in; "Launch Now" remains available

> Console programs without a tray icon of their own (such as `syncthing.exe` or `frpc.exe`) exit on a close message, so "Close to tray" hides their console window instead and WinTray hosts a tray icon for them: left-click shows/hides the window, and the context menu can show, hide, stop hosting or quit the program. All icons are owned by the main WinTray process, with no extra processes.

### Staggered Startup

The global **Delay between programs** sets the minimum gap between launches by WinTray: **3 seconds** by default, configurable from **0–120 seconds** (0 means no wait) and worth raising when many heavy programs are involved. Launches follow list order, with no extra wait before the first one or after the last one; paused, already-running and failed entries add no gap. "Launch Now" is unaffected.

The delay only paces programs that WinTray launches itself: enabled Windows `Run` entries remain scheduled by Windows, and WinTray only waits for those programs and handles their windows without changing other programs' startup settings.

### Common Use Cases

| Scenario                                                     | Configuration                                            |
| ------------------------------------------------------------ | -------------------------------------------------------- |
| WeChat / DingTalk auto-start and minimize to tray            | Add `.exe`, set "How it starts" to "Close to tray"        |
| New QQ (NT-based) auto-start, minimize to tray after login   | Add `QQ.exe`, set "Close to tray" and "Wait before closing" to 10–15 s to skip the login window; QQ's own auto-start can stay enabled |
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

**Exit WinTray when sign-in tasks finish** (optional, in Settings): with no hosted icons WinTray exits once the tasks are done; with hosted icons still present it hides the settings window and its own icon, leaving only the programs' icons, and exits after the last hosted program ends. Running WinTray again opens the existing instance's settings, and it never auto-exits while settings are open or a manual launch is in progress. Choosing "Exit WinTray" manually cancels pending tasks, restores hidden windows and then exits.

**Run silently** (next to "Exit WinTray" at the bottom of the settings window, and in the tray menu; available at any time): hides the settings window and WinTray's own icon while hidden program windows stay hidden and hosted icons for console programs such as syncthing and frpc remain available; runs in progress finish, then WinTray exits when the last hosted program ends, or immediately if nothing is hosted. Running WinTray again brings it back.

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
A: Right-click WinTray's tray icon and select "Open Settings". If automatic-exit mode leaves only the hosted programs' icons, run `WinTray.exe` again to open settings in the existing process.

**Q: How do I disable auto-start after it's been enabled?**
A: Uncheck "Run WinTray at logon" in the settings page; the logon task and registry entry are removed automatically. You can also use "Remove sign-in task" under More Features → Troubleshooting, which deletes both and turns running at sign-in off after confirmation.

**Q: The new QQ doesn't minimize to the tray; instead the login fails or QQ quits.**
A: The new QQ shows a login window first and quits when that window receives a close message. Set "Wait before closing" for it so the delay covers the whole login (auto-login usually takes 15–30 seconds; allow more for manual login). Whether WinTray or QQ's own auto-start launched it, WinTray waits until QQ has been running for the delay and its main window is up before closing it.

**Q: Can QQ's own auto-start and WinTray launch two instances?**
A: WinTray checks enabled `HKCU/HKLM Run` entries (including 32-bit entries) that directly launch the configured executable, respecting Task Manager's disabled state. If one exists, WinTray only waits for Windows to start the program, for up to 300 seconds, then applies the configured "Wait before closing" before handling its window. A timeout is logged explicitly, with **no fallback launch**, so a later Windows launch does not create a duplicate. "Launch now" can still start an absent program manually. Detection does not cover scheduled tasks, Startup-folder items, or indirect launches through third-party launchers, and does not change other programs' startup settings.

**Q: A program in my list isn't being minimized to the tray.**
A: Make sure the program is set to "Close to tray", and that WinTray was triggered with the `--autorun` flag (auto-start does this automatically). If the program starts slowly, increase "Wait for a window up to".

---

## License

This project is released under the [MIT License](LICENSE).

