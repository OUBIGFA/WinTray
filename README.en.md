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
- **Launch at Boot**: Changes only the sign-in trigger, preserving original startup arguments, silent behavior and privileges; reuses existing logon tasks instead of launching twice
- **Wait for a window up to**: 0–120 seconds, for slow-starting programs whose window shows up late
- **Wait Before Closing**: Each program can run for a set number of seconds before its window is closed, to skip login dialogs and other pre-launch popups (such as the new QQ)
- **Staggered Startup**: Launch programs in list order (drag them in the list to reorder), 3 seconds apart by default; adjust "Delay between programs" in Settings from 0–120 seconds to reduce competing startup workloads
- **Reset all data**: One-click cleanup of local config/logs in Settings
- **Bilingual UI**: Built-in Simplified Chinese / English, switchable instantly
- **Single Instance Protection**: Prevents duplicate launches to avoid configuration conflicts

---

## Download & Usage

WinTray is **portable only** — no installation needed.

Go to the [Releases](../../releases) page, download the latest `WinTray-Portable.zip`, extract it, and run `WinTray.exe` directly.

- Configuration, logs and original-startup recovery backups are stored in `%LOCALAPPDATA%\WinTray\`
- Before removing WinTray, use **Reset…** in Settings to restore migrated startup states and remove WinTray tasks, then delete the program folder; do not delete a data directory that still contains recovery backups

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

- **How it starts** (choose one): **Close to tray** — closes the main window after launch, the program keeps running in the tray / **Run in background** — no window at all, best for scripts and command-line tools / **Launch at boot** — changes only the original startup trigger / **Start normally** — just starts the program, window untouched
- **Wait before closing**: Works with "Close to tray", 0–600 seconds; for programs with a sign-in window (10–15 seconds for the new QQ) it skips the login process
- **Arguments**: Optional, passed to the program in normal/background/close-to-tray launches
- **Start this program at sign-in**: Unchecking stops WinTray's launch scheduling; the program's own startup remains or is restored

> Console programs without a tray icon of their own (such as `syncthing.exe` or `frpc.exe`) get a WinTray-hosted tray icon with "Close to tray": left-click shows/hides the window, and the context menu can show, hide, stop hosting or quit the program.

### Close-to-Tray Startup

WinTray tries to hide startup windows, then closes them once the program has run for the configured delay. The native tray remains available. If closing fails or is cancelled, the windows become visible again.

**Some programs may still briefly show a window.** Complete manual sign-in first or choose “Start normally”. Tray-icon collection must be enabled separately.

### Launch at Boot: Preserve Original Startup Settings

Only the startup trigger is replaced; the program's own startup configuration is left untouched.

- **Reuse the original entry**: Existing logon tasks, registry entries and Startup-folder shortcuts are all reused; only a recoverable enable state is toggled to prevent double launches. Duplicate tasks left by older WinTray versions are removed.
- **Arguments and privileges unchanged**: Preserve original arguments, silent startup, privileges and working directory. New administrator tasks require one confirmation.
- **Startup timing**: New tasks start as soon as WinTray and the desktop are ready, without the staggering interval. Existing tasks retain their delays, and custom timing still applies. A readiness timeout reports an error.
- **Clean undo**: Switching modes, pausing, removing an entry or turning off WinTray startup deletes the replacement task and restores the enable states; the program's own startup is never touched.
- **Errors instead of guesses**: Missing, disabled or unresolvable entries report an error rather than guessed flags or a bare-executable fallback. Use "Start normally" for such programs.

### Tray Icons

Check **Tray icon** in the program list to move the program's existing tray icons into WinTray's right-click menu. Requires Windows 11 and an `.exe` program.

- Icons disappear from the taskbar and the `^` flyout; menu entries use the program's current icon and name
- Left-click acts as clicking the original icon; right-click opens the program's own tray menu
- Unchecking the option or exiting WinTray normally restores the icons; unselected programs keep their native tray icons
- Failed collection attempts restore the original icon where possible and log the cause

### Custom Timing

When checked, configure:

- **Start delay**: **0–1440 minutes** after sign-in, default **0**.
- **Auto-quit**: quit after **0–1440 minutes**, including child processes; default **30**, **0** means never quit. Still applies when “Start this program at sign-in” is off.
- **Limit automatic launches**: at most **1–1000 launches** within the last **1–365 days**. Off by default; starts at **1 launch per day** when enabled. **1 day = 24 hours**; restarting or crossing midnight does not reset the count.

The limit applies only to automatic sign-in launches. Manual launches and “Launch Now” are unrestricted. Enabling it takes over original sign-in startup; disabling it restores the previous startup arrangement. Takeover failures show an error; administrator tasks require one confirmation.

Unchecking “Custom timing” disables all three settings.

### Staggered Startup

The **Delay between programs** in Settings sets the minimum gap between launches — **3 seconds** by default, adjustable from **0–120 seconds**. Programs start in list order (drag to reorder); "Launch Now" is unaffected.

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

**Exit WinTray when sign-in tasks finish** (optional, in Settings): exits once the tasks are done; it never exits while any program uses tray collection, and while tray icons are still in use it waits, and exits after the last one ends.

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
A: Make sure the program is set to "Close to tray" and "Run WinTray at sign-in" is on. If the program starts slowly, increase "Wait for a window up to".

---

## License

This project is released under the [MIT License](LICENSE).
