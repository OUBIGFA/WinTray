<div align="center">

# WinTray

<img src="internal/branding/assets/logo.png" alt="WinTray logo" width="120" />

**A Windows tool for startup window management and background launches**

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Platform: Windows 10 / 11](https://img.shields.io/badge/platform-Windows%2010%2F11-lightgrey.svg)
![Go 1.27.1](https://img.shields.io/badge/Go-1.27.1-00ADD8.svg)

English | [简体中文](README.md)

</div>

## Introduction

WinTray starts selected programs when you sign in to Windows. It can close their windows or run scripts in the background. In this guide, startup means startup after Windows sign-in.

- **Close to tray**: Closes program windows. Programs that support closing to the tray keep running. WinTray provides tray icons for console programs.
- **Run in background**: Starts scripts or command-line tools without a console window.
- **Tray icon**: Moves selected programs' tray icons into the WinTray menu. This feature requires Windows 11.
- **Launch at boot**: Uses a scheduled task at sign-in, with the original startup arguments and privileges when available.
- **Custom timing**: Sets a start delay, an automatic exit time, and a limit on automatic launches.
- **Wait before closing**: Waits until the program has run for the specified time before closing its window.

![WinTray program list and program settings](image/01.png)

![WinTray settings page](image/02.png)

## Download and use

WinTray is available as a portable package. It does not need installation.

1. Download the latest `WinTray-Portable.zip` from [Releases](../../releases).
2. Extract the archive to a folder you intend to keep.
3. Run `WinTray.exe`.
4. Select **Add Program** to add programs or scripts.
5. Select a mode under **How it starts**, as described below. Scripts always use **Run in background**.
6. Make sure **Start this program at sign-in** is selected for the program.
7. Select **More Features** at the top right of the main window to open **Settings**.
8. Make sure **Run WinTray at sign-in** is enabled.

After the settings are saved successfully, use **Launch Now** to check the launch behavior. WinTray does not start another instance if the program is already running. Check automatic startup at the next Windows sign-in.

Settings and logs are stored in your user data directory. Before deleting WinTray, follow [Stop using WinTray](#stop-using-wintray) to restore the original startup entries.

## Supported program types

| Type | File extension | Default mode when added | Requirements |
| --- | --- | --- | --- |
| Executable | `.exe` | Close to tray | You can select another mode. Graphical programs must support continued operation after their window closes |
| Batch script | `.bat` / `.cmd` | Run in background | Uses Windows `cmd.exe` |
| PowerShell script | `.ps1` | Run in background | Uses `powershell.exe` |
| Python script | `.py` / `.pyw` | Run in background | `python.exe` / `pythonw.exe` must be available through `PATH` |

Scripts always use **Run in background** and cannot use tray collection. Install the interpreter and packages that each script requires.

If an application has several `.exe` files, select the file that displays its main window. A launcher or updater might not match the main window.

## Per-program settings

### How it starts

Select **Start this program at sign-in** to edit the launch mode and arguments. Select one of the four modes.

| Mode | Behavior and use |
| --- | --- |
| Close to tray | Closes the window after launch. Use for programs that support closing to the tray, or console programs that need a tray icon |
| Run in background | Starts without a console window. Use for scripts and command-line tools. Graphical windows created by the program can still appear |
| Launch at boot | Uses a scheduled task at sign-in and preserves supported original startup settings. For `.exe` files only |
| Start normally | Uses the program's original startup when available. Otherwise, WinTray starts the program. The window stays unchanged |

**Arguments** are optional. WinTray uses these arguments when it starts the program directly. When it uses an original startup entry, that entry supplies the arguments.

Clear **Start this program at sign-in** to stop WinTray from scheduling the program. The program's original startup is kept or restored. **Launch Now** then uses normal launch behavior. You can still edit **Custom timing**.

### Close to tray and wait before closing

WinTray tries to hide startup windows. It closes the window after **Wait before closing** expires. If closing fails or the operation is cancelled, WinTray tries to restore the window.

- **Range**: 0–600 seconds. The default is 0 seconds, which adds no wait.
- **Start of the wait**: The time when the target process was created. This also applies when the program starts through its own startup entry.
- **Program sign-in**: WinTray does not detect whether sign-in is complete. Set the wait longer than the actual sign-in time. Use **Start normally** if manual sign-in is required.
- **Window limits**: Some programs can briefly display a window. A graphical program that does not support closing to the tray can exit when its window closes.
- **Launchers**: WinTray can recognize a main executable with the same filename in a subdirectory of the launcher when both files have the same valid signing certificate. If this relationship cannot be verified, add the actual main `.exe` instead.

Select **Close to tray** for console programs such as `syncthing.exe` and `frpc.exe` to give them a WinTray-hosted tray icon. Left-click the icon to show or hide the window. Right-click it to show the window, hide it, stop hosting and show it, or quit the program.

### Tray collection

Select **Tray icon** in the program list to move the program's existing tray icons into WinTray's right-click menu. This feature requires Windows 11 and an `.exe` file.

- The original icons are hidden from the system tray and its `^` panel. The WinTray menu uses the program's current icon and name.
- Click a program icon in the menu to pass a click to the program. If no window appears, WinTray also sends a double-click.
- Right-click the program icon to open its own tray menu.
- Clear **Tray icon**, or exit WinTray normally, to restore the original icons.
- If collection fails, WinTray tries to restore the original icons and records the cause in the log.

This setting affects only selected programs.

### Launch at boot

WinTray selects a scheduled task according to the program's existing startup entries:

| Original startup | WinTray behavior |
| --- | --- |
| An available scheduled task at sign-in | Reuses the task with its original arguments, privileges, and delay |
| A supported current-user registry entry or Startup-folder shortcut | Creates a replacement task with the original command, silent behavior, and working directory. Backs up the enabled state and temporarily disables the original entry to prevent duplicate launches |
| No original startup entry | Creates a task with the program path and arguments configured in WinTray |

New tasks start when WinTray and the desktop are ready. They do not use the delay between programs. If **Custom timing** specifies a start delay, new tasks use that delay. A task that is reused directly keeps its original delay.

Windows requests confirmation when a new task requires administrator privileges. After the settings are saved successfully, **Launch Now** runs the same registered task so you can check its behavior.

WinTray reports an error if an original entry is disabled, removed after migration, unclear, or conflicts with another entry. It also reports an error if the wait for startup conditions times out. Follow the error message and save the settings again, or use **Start normally**. WinTray keeps a backup of startup states that it could not restore.

When this mode and the launch limit are both off, WinTray removes the replacement task and restores the original entry's state. This also happens when you disable the program's startup, remove it from the list, or disable WinTray startup.

### Custom timing

Select **Custom timing** to use these settings. All values must be whole numbers.

| Setting | Range | Default | When it applies |
| --- | --- | --- | --- |
| Start delay (**Start…min after sign-in**) | 0–1440 minutes | 0 minutes | Requires **Run WinTray at sign-in** and **Start this program at sign-in**. 0 adds no delay |
| Automatic exit (**Quit after…min**) | 0–1440 minutes | 30 minutes | Applies while WinTray is running. 0 disables automatic exit |
| Limit automatic launches | 1–1000 launches in the last 1–365 days | Off; 1 launch in 1 day when enabled | Requires **Run WinTray at sign-in** and **Start this program at sign-in** |

The start delay begins at Windows sign-in. Original startup entries that WinTray has not taken over keep their own schedule. Tasks that WinTray reuses directly keep their original delay.

The run time for automatic exit starts when the target process is created. At the configured time limit, WinTray ends the program and its child processes. This also applies to manually started programs, even when **Start this program at sign-in** is off.

The launch limit applies only to automatic launches at sign-in. Manual launches and **Launch Now** are not limited. One day means 24 hours. A restart or midnight does not reset the count.

When the limit is enabled, WinTray takes over the `.exe` program's original startup and checks the count before launch. When the limit is disabled, WinTray restores the startup arrangement for the current launch mode. If this change fails, WinTray reports an error. Windows requests confirmation when administrator privileges are required.

Scripts started by WinTray can use the limit. WinTray cannot take over a script's external startup entry to apply the limit.

Clear **Custom timing** to disable the start delay, automatic exit, and launch limit.

### Delay between programs

**Delay between programs** in **Settings** sets the minimum interval between programs that WinTray starts automatically. The default is 3 seconds. The range is 0–120 seconds; 0 means no wait.

Select a number in each program's **Order** list to change the start order. This interval does not apply to **Launch at boot**, programs started by their own startup entries, or **Launch Now**.

## Common use cases

For these examples, enable **Run WinTray at sign-in** and **Start this program at sign-in**.

| Scenario | Configuration |
| --- | --- |
| Close WeChat or DingTalk to the tray after launch | Add the main `.exe` and select **Close to tray**. Make sure the program supports closing to the tray |
| Close the new QQ to the tray after sign-in | Add `QQ.exe` and select **Close to tray**. Set **Wait before closing** longer than the actual sign-in time |
| Give console programs such as syncthing or frpc a tray icon | Add the `.exe` and select **Close to tray** |
| Run tunnel or crawler scripts in the background | Add the script and use **Run in background**. Install its dependencies first |
| Start an administrator tool through a scheduled task at sign-in | Add the `.exe` and select **Launch at boot**. Confirm task privileges when prompted |

## Keep WinTray running or exit

When **Exit WinTray when sign-in tasks finish** is off, WinTray keeps its own tray icon. **Don't show the WinTray window at sign-in** controls whether the main window appears after sign-in.

When automatic exit is enabled, WinTray continues running while any of these conditions apply:

- A program has tray collection enabled.
- A console program still uses a tray icon provided by WinTray.
- A running program is waiting for its automatic exit time.

Select **Run silently** at the bottom of **Settings** or in the tray menu to hide the main window. WinTray keeps its own tray icon when tray collection is enabled. Otherwise, it hides that icon. Hosted console icons remain available. Run `WinTray.exe` again to open the main window.

## System requirements

| Item | Requirement |
| --- | --- |
| Operating system | Windows 10 / 11; tray collection requires Windows 11 |
| WinTray runtime | No additional runtime installation |
| Script environment | The required interpreter and script dependencies |
| Build from source | Go 1.27.1 or later on Windows |

The source and release package support Windows only. Cross-platform builds are not supported.

## Data directory

| Data | Path |
| --- | --- |
| Configuration | `%LOCALAPPDATA%\WinTray\settings.json` |
| Startup recovery backup | `%LOCALAPPDATA%\WinTray\startup-migrations.json` |
| Runtime log | `%LOCALAPPDATA%\WinTray\wintray.log` |

## Stop using WinTray

To stop WinTray from starting at sign-in, disable **Run WinTray at sign-in** in **Settings**. WinTray removes its own startup entries and replacement program tasks. The programs' original startup entries are kept or restored.

Before deleting WinTray, complete these steps. **Reset… clears WinTray settings and logs.**

1. Select **More Features** in the WinTray main window to open **Settings**.
2. Under **Troubleshooting**, select **Reset…**.
3. Confirm the reset.
4. Wait for cleanup to finish and WinTray to exit.
5. Move the WinTray program folder to the Recycle Bin.

If the original startup cannot be restored, follow the error message and try again. Keep the startup recovery backup in the data directory until restoration succeeds.

## FAQ

### How do I open settings when there is no main window?

Right-click the WinTray tray icon and select **Open WinTray**. If the icon is not visible, run `WinTray.exe` again.

Select **More Features** in the main window to open **Settings**.

### What if the new QQ fails to sign in or exits?

QQ can exit if its sign-in window receives a close request too early. Set **Wait before closing** longer than the actual sign-in time, then check the result. Use **Start normally** if manual sign-in is required.

### Can original startup and WinTray start the same program twice?

When WinTray identifies an enabled original startup entry, it waits for Windows to start the program. It does not start another instance if that wait times out. If the program is already running, WinTray does not start it again.

Detection covers registry `Run` entries, including `HKCU`, `HKLM`, and 32-bit entries. It also covers Startup-folder shortcuts and supported scheduled tasks at sign-in.

### What should I check if a program is not closed to the tray?

1. Make sure **Run WinTray at sign-in** is enabled.
2. Make sure **Start this program at sign-in** is selected for the program.
3. Make sure **Close to tray** is selected.
4. Make sure the selected `.exe` displays the main window.
5. If the window appears slowly, increase **Wait for a window up to**. The default is 10 seconds; the range is 0–120 seconds.
6. If the problem continues, select **Open Logs** to check the cause.

## License

This project uses the [MIT License](LICENSE).
