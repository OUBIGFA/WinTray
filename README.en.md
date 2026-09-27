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
- **System Start**: Changes only the sign-in trigger, preserving original startup arguments, silent behavior and privileges; reuses existing logon tasks instead of launching twice
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

- Configuration, logs and original-startup recovery backups are stored in `%LOCALAPPDATA%\WinTray\`
- Before removing WinTray, use **Reset** in Settings to restore migrated startup states and remove WinTray tasks, then delete the program folder; do not delete a data directory that still contains recovery backups

---

## Supported Program Types

WinTray supports adding the following program types to the managed list, automatically launching and handling their windows at startup:

| Type              | File Extension  | Launch Behavior                                                               |
| ----------------- | --------------- | ----------------------------------------------------------------------------- |
| Executable        | `.exe`          | Starts normally by default; can be set to "Close to tray"; console programs get a WinTray-hosted tray icon |
| Batch script      | `.bat` / `.cmd` | Runs in the background by default (no console window)                         |
| PowerShell script | `.ps1`          | Runs in the background by default                                             |
| Python script     | `.py` / `.pyw`  | Runs in the background by default, invokes `python.exe` / `pythonw.exe`        |

> Non-`.exe` scripts (`.bat` / `.cmd` / `.ps1` / `.py` / `.pyw`) default to "Run in background" when added and cannot use "Close to tray" or "Launch at boot".

> **Note:** Some applications contain multiple `.exe` files (launchers, updaters, etc.); pick the one that owns the main window, or "Close to tray" won't take effect.

### Per-Program Configuration Options

- **How it starts**: **Launch at boot** (changes only the original sign-in trigger, preserving arguments, silent behavior and privileges; reuses existing tasks, and asks once when creating an elevated task) / **Start normally** (just starts the program, window untouched) / **Close to tray** (closes the main window after launch, the program keeps running in the tray) / **Run in background** (no window at all, best for scripts and command-line tools)
- **Wait before closing**: Available with "Close to tray", 0–600 seconds — the window is closed only after the program has been running that long, skipping sign-in windows (10–15 seconds for the new QQ). Covers instances started by the program's own auto-start too
- **Arguments**: Arguments for normal/background/close-to-tray launches; launch at boot uses the original entry's arguments instead of overriding them with this field
- **Start this program at sign-in**: Unchecking stops WinTray's launch scheduling; the program's own startup remains or is restored

> Console programs without a tray icon of their own (such as `syncthing.exe` or `frpc.exe`) get a WinTray-hosted tray icon instead: left-click shows/hides the window, and the context menu can show, hide, stop hosting or quit the program.

### System Start: Preserve Original Startup Settings

Only the startup trigger is replaced; the program's own startup configuration is left untouched.

- **Reuse existing tasks**: The original logon task keeps its actions, arguments, working directory, privileges, delay and conditions — no second task is created (e.g. Karing's `Karing Autorun`). Duplicate tasks left by older WinTray versions are removed.
- **User-level startup entries**: `HKCU\Run` and `.lnk` files in the Startup folder are supported. Original registry values and shortcuts stay intact; only a recoverable enable state is toggled to prevent double launches, so the program's own startup option can stay on.
- **Timing**: New tasks bypass Explorer's startup queue, waiting 10 seconds after sign-in plus the list position times the global interval. Existing tasks keep their own delay.
- **Privileges**: Ordinary tasks need no elevation; creating an elevated task asks once for confirmation.
- **Errors instead of guesses**: Missing, disabled, ambiguous or machine-wide shared entries report an error rather than guessed silent flags, a bare-executable fallback or blanket elevation. Use "Start normally" for such programs.
- **Undo**: Switching modes, pausing, removing an entry or turning off WinTray startup deletes the replacement task and restores only the enable states WinTray changed; the program's own task is never touched. "Launch Now" runs the same registered task (after a successful save) and does nothing if the program is already running.
- **Rollback**: `startup-migrations.json` is saved before migration; on failure WinTray rolls back, keeps the backup and reports the cause, without overwriting later user changes.

### Collect Tray Icons

Check **Tray icon** in the program list to move the program's existing tray icons into WinTray's right-click menu. Requires Windows 11 and an `.exe` program.

- The original icons disappear from the taskbar and the `^` hidden-icons flyout
- Entries use the program's current icon and tooltip; status changes appear the next time you open the menu
- Left-click acts as clicking the original icon; right-click opens the program's own tray menu (behavior set by that program)
- Unchecking the option or exiting WinTray normally restores the icons; WinTray stays running while collecting so they remain reachable

### Custom Timing

Check **Custom timing** to set two values: a **0–1440 minute** delay after sign-in (default **0**) and an auto-exit **0–1440 minutes** after launch that also ends child processes (default **30**; **0** disables it). Both are ignored while unchecked.

### Staggered Startup

The global **Delay between programs** sets the minimum gap between launches — **3 seconds** by default, adjustable from **0–120 seconds**. Programs start in list order; "Launch Now" is unaffected.

### Common Use Cases

| Scenario                                                     | Configuration                                            |
| ------------------------------------------------------------ | -------------------------------------------------------- |
| WeChat / DingTalk auto-start and minimize to tray            | Add `.exe`, set "How it starts" to "Close to tray"        |
| New QQ auto-start, minimize to tray after login | Add `QQ.exe`, set "Close to tray" and "Wait before closing" to 10–15 s |
| syncthing / frpc console programs running in the background, reachable from the tray | Add `.exe`, set "Close to tray"; WinTray hosts the tray icon |
| Tunnel scripts (frpc / SSH) running in background at startup | Add `.bat` / `.ps1`, set "Run in background"              |
| Python crawler/service starting silently in background       | Add `.py`, set "Run in background"                        |
| Auto-start only, no window handling                          | Add program, keep "Start normally"                        |
| Administrator tools starting fast at sign-in                 | Add the `.exe`, set "How it starts" to "Launch at boot"     |

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
| Startup recovery | `%LOCALAPPDATA%\WinTray\startup-migrations.json` |
| Logs          | `%LOCALAPPDATA%\WinTray\wintray.log`   |

---

## Command-Line Arguments

| Argument            | Description                                                      |
| ------------------- | ---------------------------------------------------------------- |
| `--background`      | Start without showing the main window (used for auto-start)      |
| `--autorun`         | Run the startup programs list (used by auto-start)               |
| `--cleanup-restore` | Restore original startup and remove WinTray tasks/data; prefer the Settings action |
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
A: Turn off "Run WinTray at sign-in" in Settings. WinTray's task/startup entry and replacement program tasks are removed; applications' original startup remains or is restored, not disabled together with WinTray.

**Q: The new QQ doesn't minimize to the tray; instead the login fails or QQ quits.**
A: The new QQ shows a login window first and quits if it is closed too early. Set "Wait before closing" longer than the whole login (auto-login usually takes 15–30 seconds).

**Q: Can QQ's own auto-start and WinTray launch two instances?**
A: No. When the program's own `Run` entry is enabled, WinTray just waits for Windows to start it and **never launches it again**, so there's no duplicate.

**Q: A program in my list isn't being minimized to the tray.**
A: Make sure the program is set to "Close to tray" and WinTray's auto-start is enabled. If the program starts slowly, increase "Wait for a window up to".

---

## License

This project is released under the [MIT License](LICENSE).

