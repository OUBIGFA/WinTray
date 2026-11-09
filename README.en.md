<div align="center">

# WinTray

<img src="internal/branding/assets/logo.png" alt="WinTray logo" width="120" />

**A Windows tool for startup window management and background launches**

English | [简体中文](README.md)

</div>

WinTray starts programs, closes their windows, or runs background scripts when you sign in to Windows. In this guide, startup means startup after Windows sign-in.

## Download and use

Download one of these files from [Releases](../../releases).

| Version | File | How to open |
| --- | --- | --- |
| Installed | `WinTray-Setup.exe` | Run the installer, then open WinTray from the Start menu |
| Portable | `WinTray-Portable.zip` | Extract the complete archive, then run `WinTray.exe` |

1. In **Programs**, select **Add Program**, or drag program files into the list. You can add several files at once.
2. Select a program.
3. In **Startup & schedule**, turn on **Start this program at sign-in**.
4. Select a mode under **How it starts**.
5. In **Settings**, turn on **Start at sign-in**.
6. Use **Launch Now** to check the launch behavior. Check automatic startup at the next Windows sign-in.

Changes save automatically. For names, arguments, and numbers, press `Enter` or leave the field to save.

## Program management

| Action | Method |
| --- | --- |
| Edit the name or arguments | Select the program, then open **Program details** |
| Change the program file | Click the folder icon in the **Change** column, or drag one file onto that icon |
| Change the start order | Drag the icon on the left of the program row, or select an **Order** number. Press `Esc` to cancel a drag |
| Stop a program | Select the running program, then select **Stop Program** |
| Show or hide a window | Select the running program, then select **Show Window** or **Hide Window** |
| Remove a program | Select the program, select **Delete**, then select **Confirm**. The program file is kept |

Drag the divider between the list and details to change their widths. In a narrow window, the list appears above the details.

## Supported files

| File | Default mode | Requirement |
| --- | --- | --- |
| `.exe` | Close to tray | Select the file that displays the main window, not an updater |
| `.bat` / `.cmd` | Run in background | Windows `cmd.exe` |
| `.ps1` | Run in background | Windows PowerShell |
| `.py` / `.pyw` | Run in background | `python.exe` / `pythonw.exe` available through `PATH` |
| `.ahk` | Run in background | Install AutoHotkey and check that double-clicking the script runs it |

Scripts always use **Run in background**. Install the script dependencies first. If WinTray runs as administrator, use **Add Program** to select files.

## Launch modes

Select a mode in **Startup & schedule**.

| Mode | Behavior |
| --- | --- |
| Close to tray | Closes the window after launch. Graphical programs must support closing to the tray, or they can exit |
| Run in background | Hides the console window. Use for scripts and command-line tools. Graphical windows created by the program can still appear |
| Launch at boot | Uses a scheduled task at sign-in. For `.exe` files only. Prefers the original startup arguments, privileges, and delay |
| Start normally | Uses the program's original startup when available. Otherwise, WinTray starts it. The window stays unchanged |

WinTray uses the arguments in **Program details** when it starts the program directly. An original startup entry supplies its own arguments.

Turn off **Start this program at sign-in** to stop normal startup scheduling. Original startup is kept or restored. **Launch Now** then uses normal mode. An enabled **Custom timing** schedule still starts the program.

### Close to tray

**Wait before closing (sec)** accepts 0–600 seconds. The default is 0. The wait starts when the target process is created. A value of 0 closes the window immediately.

- For programs with a sign-in window, such as QQ, set the wait longer than the sign-in process. Use **Start normally** if manual sign-in is required.
- If the program disables **Close** and does not respond to `Esc`, the window stays open. A window can briefly appear or take focus during startup.
- For console programs such as `syncthing.exe` and `frpc.exe`, WinTray provides a tray icon. Left-click it to show or hide the window. Use its right-click menu to quit the program.

### Background windows

Use **Show Window** to read console output and enter commands. Closing the console window can end the program. Output is not retained after the program exits.

For `.pyw`, only a graphical window created by the script can be shown. Exit and relaunch programs started in the background by an older WinTray. Previously discarded output cannot be recovered.

### Launch at boot

WinTray reuses an available scheduled task at sign-in. The task keeps its original privileges and delay.

For a supported current-user registry entry or Startup-folder shortcut, WinTray temporarily disables the original entry to prevent duplicate launches. If no original entry exists, WinTray creates a task from the current settings. Windows requests confirmation when administrator privileges are required.

New tasks start when WinTray and the desktop are ready. If a launch delay is set, new tasks use that delay.

WinTray restores original startup when you disable this mode without a launch limit, remove the program, or disable WinTray startup. It also restores original startup when you turn off **Start this program at sign-in** without **Custom timing**.

### WeChat auto login

Select `Weixin.exe` or `WeChat.exe` to use **WeChat auto login**. It is off by default and unavailable in **Run in background** mode.

WinTray clicks the login button. Scan the QR code or confirm on your phone when required. With **Close to tray**, WinTray waits for the main window before closing it. If login is not confirmed within 30 seconds, the login window stays open.

## Timing

In **Startup & schedule**, turn on **Custom timing**.

| Setting | Range | Default |
| --- | --- | --- |
| Launch delay (min) | 0–1440 minutes from Windows sign-in | 0; start immediately |
| Automatic exit (min) | 0–1440 minutes from process creation | 30; set to 0 to disable automatic exit |
| Limit automatic launches | 1–1000 launches in the last 1–365 calendar days | Off; 1 launch in 1 day when enabled |

Automatic startup requires **Start at sign-in**. **Custom timing** starts the program even when **Start this program at sign-in** is off. Turn off **Custom timing** to disable all three settings in the table.

Automatic exit ends the program and its child processes. It also applies to manually started programs. Save your work before the time limit.

The launch limit applies only to automatic startup. **Launch Now** and manual launches are not limited. Days use the computer's time zone and include today. One day starts at today's midnight. Restarting does not clear the history.

When the limit is enabled, WinTray takes over the `.exe` program's original startup and checks the count before launch. When the limit is disabled, WinTray restores startup for the current launch mode. Scripts can use the limit, but WinTray cannot take over a script's external startup.

## Tray collection

Turn on **Tray** in the program list to move the program's existing tray icons into the WinTray menu. This requires Windows 11 and an `.exe` file.

- In the WinTray tray menu, click a program icon to open its window. Right-click its row to open the program's own tray menu.
- Turn off **Tray**, or exit WinTray normally, to restore the original icons.
- If WinTray is force-closed, it restores the original icons of programs that are still running.

## Settings

| Group | Purpose |
| --- | --- |
| Sign-in | Set WinTray startup, hide its main window at sign-in, or exit after sign-in tasks finish |
| Timing | **Launch interval (seconds)**: default 3, range 0–120. **Window wait limit (seconds)**: default 10, range 0–120 |
| Troubleshooting | Open logs, remove the WinTray sign-in task, or reset all data |

The launch interval applies only to programs that WinTray starts automatically. A value of 0 means no wait. It does not apply to **Launch at boot**, original startup entries, or **Launch Now**.

WinTray keeps running while tray collection, background windows, or console tray icons are in use. It also stays running while a program waits for automatic exit.

Select **Run silently** to hide the main window. WinTray keeps its own tray icon when tray collection or background windows are in use. Otherwise, it hides that icon. Run `WinTray.exe` again to open the main window.

**Exit WinTray** attempts to restore hidden windows and waits up to 2 seconds. You may need to restart a program whose window cannot be restored.

## User data

For the same Windows user, the installed and portable versions share `%LOCALAPPDATA%\WinTray`. Both read the existing settings without migration.

| Data | File |
| --- | --- |
| Configuration | `%LOCALAPPDATA%\WinTray\settings.json` |
| Startup recovery backup | `%LOCALAPPDATA%\WinTray\startup-migrations.json` |
| Log | `%LOCALAPPDATA%\WinTray\wintray.log` |

The installer puts program files in `%LOCALAPPDATA%\Programs\WinTray` by default. Uninstalling keeps user data.

## Update and uninstall

Before updating, select **Exit WinTray**. For the installed version, run the new installer. For the portable version, extract the complete new archive into the existing program folder and replace the files.

| Action | Steps |
| --- | --- |
| Stop WinTray startup | In **Settings**, turn off **Start at sign-in**. Original program startup is kept or restored |
| Uninstall the installed version | Exit WinTray, then run **Uninstall** from the WinTray folder in the Start menu. Uninstalling restores original startup |
| Delete the portable version | Disable WinTray startup, exit WinTray, then move the program folder to the Recycle Bin |
| Clear user data | In **Settings → Troubleshooting**, select **Reset…**. Confirm, then wait for WinTray to exit. This clears settings and logs |

If original startup cannot be restored, follow the error message and retry. Keep the startup recovery backup until restoration succeeds.

## Troubleshooting

- **Main window is missing**: Left-click the WinTray tray icon, or run `WinTray.exe` again.
- **Program is not closed to the tray**: Enable WinTray startup, the program's startup, and **Close to tray**. If the window appears slowly, increase **Window wait limit (seconds)**.
- **QQ cannot sign in or exits**: Increase **Wait before closing (sec)**, or use **Start normally**.
- **Problem continues**: In **Settings → Troubleshooting**, select **Open Logs**.

When WinTray detects original startup, it waits for Windows to start the program. It does not launch another instance after a timeout or when the program is already running. Detection covers registry `Run` entries, Startup-folder shortcuts, and supported scheduled tasks at sign-in.

## System requirements

| Use | Requirement |
| --- | --- |
| Run WinTray | Windows 10 / 11; no additional runtime. Tray collection requires Windows 11 |
| Run scripts | The required interpreter and script dependencies |
| Build and package | Windows, Go 1.27.3 or later, Visual Studio C++ Build Tools (x86/x64 tools and Windows SDK), and NSIS 3 |

## License

[MIT License](LICENSE)
