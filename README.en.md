<div align="center">

# WinTray

<img src="internal/branding/assets/logo.png" alt="WinTray Logo" width="120" />

**A Windows tray tool that automatically organizes your desktop at startup**

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%2F11-lightgrey.svg)]()
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8.svg)]()

English | [简体中文](README.md)

</div>

---

## Introduction

WinTray is a Windows startup organizer. It sits in the system tray and, when triggered by the auto-start flow, automatically manages the windows of specified programs (e.g., minimizing or hiding them) based on configurable rules — saving you the hassle of manually cleaning up your desktop after every boot.

![](image/01.png)

---

## Features

- **Tray Resident**: Lives in the notification area with one-click access to settings, logs, and exit
- **Managed Program List**: Maintain any number of programs, each with independent behavior configuration
- **Auto Start**: Writes to the current user's `Run` registry key for automatic launch at Windows logon
- **Auto Hide Windows**: When configured in the program list, the `--autorun` flow automatically minimizes and hides target windows
- **Retry on Window Handling**: Configurable 0–120 second retry window for slow-starting programs
- **Cleanup & Restore Defaults**: One-click cleanup of local config/logs from the main window or tray menu
- **Bilingual UI**: Built-in Simplified Chinese / English, switchable instantly
- **Single Instance Protection**: Prevents duplicate launches to avoid configuration conflicts

---

## Download & Usage

WinTray is **portable only** — no installation needed.

Go to the [Releases](../../releases) page, download the latest `WinTray-Portable-vX.Y.Z.zip`, extract it, and run `WinTray.exe` directly.

- Configuration and logs are stored in `%LOCALAPPDATA%\WinTray\` with no registry dependencies
- To remove completely, simply close the program and delete the folder

---

## Supported Program Types

WinTray supports adding the following program types to the managed list, automatically launching and handling their windows at startup:

| Type              | File Extension  | Launch Behavior                                                               |
| ----------------- | --------------- | ----------------------------------------------------------------------------- |
| Executable        | `.exe`          | Foreground launch by default; can optionally close the window to hide to tray |
| Batch script      | `.bat` / `.cmd` | Hidden background launch by default (no console window)                       |
| PowerShell script | `.ps1`          | Hidden background launch by default                                           |
| Python script     | `.py` / `.pyw`  | Hidden background launch by default, invokes `python.exe` / `pythonw.exe`       |

> Non-`.exe` scripts (`.bat` / `.cmd` / `.ps1` / `.py` / `.pyw`) automatically enable the "Launch hidden in background" option when added, and cannot simultaneously use "Close window after launch".

### Per-Program Configuration Options

- **Launch Arguments**: Pass custom command-line arguments to the program
- **Close Window After Launch**: Sends a close message (WM_CLOSE) after launch — most tray-aware apps minimize to tray rather than quitting; a destroyed or invisible window is considered successfully handled
- **Launch Hidden in Background**: Starts the program without any visible window, suitable for command-line and script programs
- **Pause Task**: Temporarily skip this program's auto-start task; it will run again on the next boot

### Window Matching

WinTray automatically combines the process ID, executable path, process name, and
window features to select a target. An action is only sent after the safety score
reaches the threshold, so no matching strategy needs to be configured.

Scoring system (an action is only taken when the total score ≥ 500):

- **+1000**: Exact PID match (process launched by WinTray)
- **+500**: Exact executable path match
- **+250**: Process name match (case-insensitive)
- **+200**: New window that appeared after launch
- **+50**: Non-empty window title
- **-80**: Tool window (auxiliary, skipped)
- **-60**: Window with an owner (child/owned window, skipped)

### Common Use Cases

| Scenario                                                     | Configuration                                            |
| ------------------------------------------------------------ | -------------------------------------------------------- |
| QQ / WeChat / DingTalk auto-start and minimize to tray       | Add `.exe`, enable "Close window after launch"           |
| Tunnel scripts (frpc / SSH) running in background at startup | Add `.bat` / `.ps1`, hidden background launch by default |
| Python crawler/service starting silently in background       | Add `.py`, hidden background launch by default           |
| Auto-start only, no window handling                          | Add program, leave "Close window after launch" unchecked |

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
| `--background`      | Start without showing the main window (for auto-start scenarios) |
| `--autorun`         | Execute managed program tasks automatically (used by auto-start) |
| `--cleanup-restore` | Only perform cleanup: clear `%LOCALAPPDATA%\WinTray\` and exit   |

For auto-start, “Exit automatically after all tasks complete” controls whether
WinTray exits after the task set finishes. When disabled, it remains in the tray;
“Minimize to tray after launch” controls whether the main window is initially shown.

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
A: Right-click the WinTray icon in the system tray and select "Open Settings".

**Q: How do I disable auto-start after it's been enabled?**
A: Uncheck "Run WinTray at logon" in the settings page; the corresponding registry entry will be cleaned up automatically.

**Q: A program in my list isn't being minimized automatically.**
A: Make sure the program has "Close window after launch" enabled, and that WinTray was triggered with the `--autorun` flag (auto-start does this automatically). If the program starts slowly, try increasing the retry seconds setting.

---

## License

This project is released under the [MIT License](LICENSE).
