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
- **Run in background**: Starts with the console hidden. Use **Show Window** to view output and enter commands.
- **Tray icon**: Moves selected programs' tray icons into the WinTray menu. This feature requires Windows 11.
- **Tray icon recovery**: If WinTray is force-closed, restores the original icons of programs that are still running.
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
4. Select **Add Program**, or drag programs or scripts from File Explorer into the program list on the left; you can add several at once.
5. Select a mode under **How it starts**, as described below. Scripts always use **Run in background**.
6. Make sure **Start this program at sign-in** is selected for the program.
7. Select **More Features** at the top right of the main window to open **Settings**.
8. Make sure **Run WinTray at sign-in** is enabled.

After the settings are saved, use **Launch Now** to check the launch behavior. WinTray does not start another instance if the program is already running. Check automatic startup at the next Windows sign-in.

Click the delete icon at the end of a program's row, then select **Confirm** to remove it from the list. The program file is kept.

Settings and logs are stored in your user data directory. Before deleting WinTray, follow [Stop using WinTray](#stop-using-wintray) to restore the original startup entries.

## Supported program types

| Type | File extension | Default mode when added | Requirements |
| --- | --- | --- | --- |
| Executable | `.exe` | Close to tray | You can select another mode. Graphical programs must support continued operation after their window closes |
| Batch script | `.bat` / `.cmd` | Run in background | Uses Windows `cmd.exe` |
| PowerShell script | `.ps1` | Run in background | Uses `powershell.exe` |
| Python script | `.py` / `.pyw` | Run in background | `python.exe` / `pythonw.exe` must be available through `PATH` |

Scripts always use **Run in background** and cannot use tray collection. Install the interpreter and packages that each script requires.

You can drag only the file types in the table into the list. If WinTray runs as administrator, use **Add Program**. To drag files, run WinTray without administrator rights.

If an application has several `.exe` files, select the file that displays its main window. A launcher or updater might not match the main window.

## Per-program settings

### How it starts

Select **Start this program at sign-in** to edit the launch mode and arguments.

| Mode | Behavior and use |
| --- | --- |
| Close to tray | Closes the window after launch. Use for programs that support closing to the tray, or console programs that need a tray icon |
| Run in background | Starts with the console hidden. Use **Show Window** to open it. For scripts and command-line tools. Graphical windows created by the program can still appear |
| Launch at boot | Uses a scheduled task at sign-in and preserves supported original startup settings. For `.exe` files only |
| Start normally | Uses the program's original startup when available. Otherwise, WinTray starts the program. The window stays unchanged |

**Arguments** are optional. WinTray uses these arguments when it starts the program directly. When it uses an original startup entry, that entry supplies the arguments.

Clear **Start this program at sign-in** to stop WinTray from scheduling the program. The program's original startup is kept or restored. **Launch Now** then uses normal launch behavior. You can still edit **Custom timing**.

### Show or hide a window

1. Select a running program in the list on the left.
2. Select **Show Window** beside the program name on the right.
3. Select **Hide Window** in the same position to hide it again.

Use the native console to view output, scroll through its history, and enter commands. Showing or hiding the window does not change the startup mode. Closing the console window can end the program.

The program must still be running and have a native window. For `.pyw`, only a graphical window created by the script can be shown. Console output is not retained after the program exits.

Exit programs started in the background by an older WinTray, then launch them with the new version. Previously discarded output cannot be recovered.

### Close to tray and wait before closing

WinTray closes the program window after **Wait before closing** expires. Use the program's tray icon to open the window again.

- **Range**: 0–600 seconds. The default is 0 seconds, which closes the window immediately.
- **Start of the wait**: The time when the target process was created. This also applies when the program starts through its own startup entry.
- **Program sign-in**: WinTray does not detect whether sign-in is complete. Set the wait longer than the actual sign-in time. Use **Start normally** if manual sign-in is required.
- **Window limits**: A window can briefly appear or take focus during startup. Pinned taskbar shortcuts remain visible. Graphical programs that do not support closing to the tray can exit.

Select **Close to tray** for console programs such as `syncthing.exe` and `frpc.exe` to give them a WinTray-hosted tray icon. Left-click the icon to show or hide the window. Right-click it to show the window, hide it, stop hosting and show it, or quit the program.

### Tray collection

Select **Tray icon** in the program list to move the program's existing tray icons into WinTray's right-click menu. This feature requires Windows 11 and an `.exe` file.

- Collected icons no longer appear in the system tray or its `^` panel.
- Click a program icon in the WinTray menu to open the program window.
- Hover over or click the expand icon at the right of that program to open its own tray menu.
- Clear **Tray icon**, or exit WinTray normally, to restore the original icons.

### Launch at boot

WinTray selects a scheduled task according to the program's existing startup entries:

| Original startup | WinTray behavior |
| --- | --- |
| An available scheduled task at sign-in | Reuses the task with its original arguments, privileges, and delay |
| A supported current-user registry entry or Startup-folder shortcut | Keeps the original startup settings and temporarily disables the original entry to prevent duplicate launches |
| No original startup entry | Creates a task with the program path and arguments configured in WinTray |

New tasks start when WinTray and the desktop are ready and do not use the delay between programs. If **Custom timing** specifies a start delay, new tasks use that delay.

Windows requests confirmation when a new task requires administrator privileges. After the settings are saved successfully, **Launch Now** runs the same registered task so you can check its behavior.

If setup fails, follow the error message and save the settings again, or use **Start normally**.

When this mode and the launch limit are both off, WinTray removes the replacement task and restores the original entry's state. This also happens when you disable the program's startup, remove it from the list, or disable WinTray startup.

### Custom timing

Select **Custom timing** to use these settings.

| Setting | Range | Default | When it applies |
| --- | --- | --- | --- |
| Start delay (**Start…min after sign-in**) | 0–1440 minutes | 0 minutes | Requires **Run WinTray at sign-in** and **Start this program at sign-in**. 0 adds no delay |
| Automatic exit (**Quit after…min**) | 0–1440 minutes | 30 minutes | Applies while WinTray is running. 0 disables automatic exit |
| Limit automatic launches | 1–1000 launches in the last 1–365 calendar days, including today | Off; 1 launch in 1 day when enabled | Requires **Run WinTray at sign-in** and **Start this program at sign-in** |

The start delay begins at Windows sign-in. Original startup entries that WinTray has not taken over keep their own schedule. Tasks that WinTray reuses directly keep their original delay.

The run time for automatic exit starts when the target process is created. At the configured time limit, WinTray ends the program and its child processes. This also applies to manually started programs, even when **Start this program at sign-in** is off.

The launch limit applies only to automatic launches at sign-in. Manual launches and **Launch Now** are not limited. Days follow the computer's system time zone and include today: 1 day starts at today's local midnight, 2 days at yesterday's local midnight, and so on.

Restarting does not clear the history; records from earlier versions are also counted by calendar day. Start delays and automatic exit still use elapsed minutes.

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
- A program still uses a background window.
- A console program still uses a tray icon provided by WinTray.
- A running program is waiting for its automatic exit time.

Select **Run silently** at the bottom of **Settings** or in the tray menu to hide the main window. WinTray keeps its own tray icon while tray collection or background windows are in use. Otherwise, it hides that icon. Hosted console icons remain available. Run `WinTray.exe` again to open the main window.

Selecting **Exit WinTray** restores the hidden windows of programs that are still running.

## System requirements

| Item | Requirement |
| --- | --- |
| Operating system | Windows 10 / 11; tray collection requires Windows 11 |
| WinTray runtime | No additional runtime installation |
| Script environment | The required interpreter and script dependencies |
| Build from source | Windows and Go 1.27.1 or later; packaging also requires Visual Studio C++ Build Tools (x86/x64 tools and Windows SDK) |

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

Left-click the WinTray tray icon. If the icon is not visible, run `WinTray.exe` again.

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
