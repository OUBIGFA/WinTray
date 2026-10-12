#ifndef WINTRAY_TRAY_BRIDGE_H
#define WINTRAY_TRAY_BRIDGE_H

#define WIN32_LEAN_AND_MEAN
#define _WIN32_WINNT 0x0601
#include <windows.h>
#include <stdint.h>
#include <stddef.h>

#define WINTRAY_BRIDGE_PROTOCOL 3
#define WINTRAY_BROKER_CLASS L"WinTray.TrayIconBroker.v1"
#define WINTRAY_BRIDGE_DLL L"WinTray-ShellBridge.dll"
#define WINTRAY_HOOK_EXPORT "WinTrayShellHook"
#define WINTRAY_INSTALL_DATA 0x57544249u
#define WINTRAY_FORWARD_DATA 0x57544246u
#define WINTRAY_SELECT_DATA 0x57544250u
#define WINTRAY_MAX_SELECTION 262144u
#define WINTRAY_ROUTE_PROPERTY L"WinTray.ShellBridge.Route.v3"

// The helper shares only fixed-width values with the receiving taskbar thread.
// No remote allocation, remote thread, or application-side injection is used.
typedef struct WinTrayBridgeArgs {
    uint32_t size, protocol, host_pid, target_pid;
    uint64_t host_created, target_created, broker, taskbar;
    wchar_t stop_event[128];
    wchar_t ready_event[128];
} WinTrayBridgeArgs;

typedef struct WinTrayBridgeReply {
    volatile LONG result;
} WinTrayBridgeReply;

_Static_assert(sizeof(WinTrayBridgeArgs) == 560, "argument ABI");
_Static_assert(offsetof(WinTrayBridgeArgs, stop_event) == 48, "stop event ABI");
_Static_assert(offsetof(WinTrayBridgeArgs, ready_event) == 304, "ready event ABI");

static uint64_t wintray_process_created(HANDLE process) {
    FILETIME created, exited, kernel, user;
    if (!GetProcessTimes(process, &created, &exited, &kernel, &user))
        return 0;
    return ((uint64_t)created.dwHighDateTime << 32) | created.dwLowDateTime;
}

// Test builds exercise exactly the same hook/subclass on a disposable shell,
// without ever registering a second Shell_TrayWnd on the desktop.
static BOOL wintray_is_taskbar(HWND hwnd) {
    wchar_t name[64];
    if (!GetClassNameW(hwnd, name, ARRAYSIZE(name))) return FALSE;
#ifdef WINTRAY_BRIDGE_TEST
    return lstrcmpW(name, L"WinTray.TestShellEndpoint") == 0;
#else
    if (lstrcmpW(name, L"Shell_TrayWnd") != 0) return FALSE;
    DWORD pid = 0;
    GetWindowThreadProcessId(GetShellWindow(), &pid);
    DWORD owner = 0;
    GetWindowThreadProcessId(hwnd, &owner);
    return pid && owner == pid;
#endif
}
#endif
