#include "../bridge.h"
#include <shellapi.h>
#include <stddef.h>
#include <stdio.h>
#include <wchar.h>

// Disposable cross-bitness fixture, never shipped in the portable package.
// Commands use integers only; no pointers cross the test-process boundary.
#define TEST_ADD       (WM_APP + 100)
#define TEST_MODIFY    (WM_APP + 101)
#define TEST_VERSION   (WM_APP + 102)
#define TEST_DELETE    (WM_APP + 103)
#define TEST_RECT      (WM_APP + 104)
#define TEST_STATS     (WM_APP + 105)
#define TEST_TASKBAR   (WM_APP + 106)
#define TEST_HIDE      (WM_APP + 107)
#define TEST_PROBE     (WM_APP + 108)
#define TEST_LAYOUT    (WM_APP + 109)
#define TEST_WIRE      (WM_APP + 110)
#define TEST_DELAY     (WM_APP + 111)
#define TEST_STATE     (WM_APP + 112)
#define ICON_CALLBACK  (WM_APP + 200)
#define ICON_UID       71u

static NOTIFYICONDATAW icon;
static UINT taskbar_created;
static BOOL installed;
static DWORD clicks, right_clicks, refreshes;
static BOOL menu_open;
static BOOL use_ansi;
static HWND test_shell;
static BOOL shell_fixture;
static DWORD wire_count, wire_uid, wire_delay;

static GUID fixture_guid(DWORD pid) {
    GUID guid = {pid, 0x7921, 0x45a0, {0x9c, 0x37, 0x30, 0x11, 0x01, 0x09, 0x17, 0x24}};
    return guid;
}

static BOOL notify(DWORD message) {
    if (!use_ansi)
        return Shell_NotifyIconW(message, &icon);
    NOTIFYICONDATAA ansi = {0};
    ansi.cbSize = sizeof(ansi);
    ansi.hWnd = icon.hWnd;
    ansi.uID = icon.uID;
    ansi.uFlags = icon.uFlags;
    ansi.uCallbackMessage = icon.uCallbackMessage;
    ansi.hIcon = icon.hIcon;
    ansi.uVersion = icon.uVersion;
    ansi.guidItem = icon.guidItem;
    WideCharToMultiByte(CP_ACP, 0, icon.szTip, -1, ansi.szTip, ARRAYSIZE(ansi.szTip), NULL, NULL);
    return Shell_NotifyIconA(message, &ansi);
}

static BOOL add_icon(void) {
    icon.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP | (icon.guidItem.Data1 ? NIF_GUID : 0);
    wcscpy_s(icon.szTip, ARRAYSIZE(icon.szTip), L"WinTray bridge fixture");
    installed = notify(NIM_ADD);
    if (installed) {
        icon.uVersion = NOTIFYICON_VERSION_4;
        notify(NIM_SETVERSION);
    }
    return installed;
}

static BOOL icon_rect(HWND owner, UINT uid, BOOL guid) {
    NOTIFYICONIDENTIFIER id = {0};
    id.cbSize = sizeof(id);
    id.hWnd = owner;
    id.uID = uid;
    if (guid) {
        DWORD pid = 0;
        GetWindowThreadProcessId(owner, &pid);
        id.guidItem = fixture_guid(pid);
    }
    RECT rect;
    return SUCCEEDED(Shell_NotifyIconGetRect(&id, &rect)) && rect.right > rect.left && rect.bottom > rect.top;
}

static LRESULT CALLBACK window_proc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
    if (msg == taskbar_created) {
        ++refreshes;
        // Model clients which stop refreshing after a failed duplicate ADD.
        if (installed)
            add_icon();
        return 0;
    }
    switch (msg) {
    case WM_COPYDATA:
        if (shell_fixture) {
            COPYDATASTRUCT *data = (COPYDATASTRUCT *)lp;
            if (data && data->dwData == 1 && data->cbData >= 20) {
                ++wire_count; wire_uid = ((DWORD *)data->lpData)[4];
                DWORD delay = wire_delay; wire_delay = 0; if (delay) Sleep(delay);
                return 73;
            }
            if (data && data->dwData == 3) return 73;
        }
        break;
    case TEST_WIRE: {
        DWORD bytes[241] = {0x34753423, 0, 956};
        bytes[3] = (DWORD)(UINT_PTR)hwnd;
        bytes[4] = (DWORD)wp;
        COPYDATASTRUCT data = {1, sizeof(bytes), bytes};
        return SendMessageW(test_shell, WM_COPYDATA, (WPARAM)hwnd, (LPARAM)&data);
    }
    case TEST_DELAY:
        wire_delay = (DWORD)wp;
        return 1;
    case TEST_STATE:
        icon.uFlags = NIF_STATE | (icon.guidItem.Data1 ? NIF_GUID : 0);
        icon.dwState = wp ? NIS_HIDDEN : 0;
        icon.dwStateMask = NIS_HIDDEN;
        return notify(NIM_MODIFY);
    case TEST_ADD:
        return add_icon();
    case TEST_MODIFY:
        icon.uFlags = NIF_ICON | NIF_TIP | (icon.guidItem.Data1 ? NIF_GUID : 0);
        icon.hIcon = LoadIconW(NULL, MAKEINTRESOURCEW(wp & 1 ? 32515 : 32516));
        swprintf_s(icon.szTip, ARRAYSIZE(icon.szTip), L"native update %llu", (unsigned long long)wp);
        return notify(NIM_MODIFY);
    case TEST_VERSION:
        icon.uVersion = (UINT)wp;
        icon.uFlags = icon.guidItem.Data1 ? NIF_GUID : 0;
        return notify(NIM_SETVERSION);
    case TEST_DELETE:
        installed = FALSE;
        icon.uFlags = icon.guidItem.Data1 ? NIF_GUID : 0;
        return notify(NIM_DELETE);
    case TEST_RECT:
        return icon_rect(hwnd, ICON_UID, icon.guidItem.Data1 != 0);
    case TEST_STATS:
        switch (wp) {
        case 0: return clicks;
        case 1: return right_clicks;
        case 2: return refreshes;
        case 3: return installed;
        case 4: return menu_open;
        case 5: return wire_count;
        case 6: return wire_uid;
        }
        return 0;
    case TEST_TASKBAR:
        return (LRESULT)FindWindowW(L"Shell_TrayWnd", NULL);
    case TEST_LAYOUT:
        return (LRESULT)FindWindowExW(FindWindowW(L"Shell_TrayWnd", NULL), NULL,
                                      L"Windows.UI.Composition.DesktopWindowContentBridge", NULL);
    case TEST_HIDE:
        ShowWindow(hwnd, SW_HIDE);
        return 1;
    case TEST_PROBE:
        return icon_rect((HWND)wp, (UINT)lp & 0x7fffffff, ((UINT)lp & 0x80000000) != 0);
    case ICON_CALLBACK:
        if (LOWORD(lp) == WM_LBUTTONUP) {
            ++clicks;
            // Frameworks can ignore clicks if a hidden icon has no anchor.
            if (icon_rect(hwnd, ICON_UID, icon.guidItem.Data1 != 0))
                ShowWindow(hwnd, IsWindowVisible(hwnd) ? SW_HIDE : SW_SHOWNOACTIVATE);
        } else if (LOWORD(lp) == WM_RBUTTONUP && !menu_open) {
            ++right_clicks;
            HMENU menu = CreatePopupMenu();
            AppendMenuW(menu, MF_STRING, 1, L"Native fixture action");
            POINT cursor;
            GetCursorPos(&cursor);
            SetForegroundWindow(hwnd);
            menu_open = TRUE;
            TrackPopupMenu(menu, TPM_RIGHTBUTTON | TPM_RETURNCMD | TPM_NONOTIFY,
                           cursor.x, cursor.y, 0, hwnd, NULL);
            menu_open = FALSE;
            DestroyMenu(menu);
        }
        return 0;
    case WM_CLOSE:
        if (installed) {
            icon.uFlags = icon.guidItem.Data1 ? NIF_GUID : 0;
            notify(NIM_DELETE);
        }
        DestroyWindow(hwnd);
        return 0;
    case WM_DESTROY:
        PostQuitMessage(0);
        return 0;
    }
    return DefWindowProcW(hwnd, msg, wp, lp);
}

int wmain(int argc, wchar_t **argv) {
    if (argc == 3 && wcscmp(argv[1], L"--abi") == 0) {
        HMODULE dll = LoadLibraryW(argv[2]);
        if (!dll)
            return (int)GetLastError();
        HOOKPROC hook = (HOOKPROC)GetProcAddress(dll, WINTRAY_HOOK_EXPORT);
        if (!hook) return ERROR_PROC_NOT_FOUND;
        hook(-1, 0, 0);
        printf("%zu %zu %zu %zu %zu %zu\n", sizeof(WinTrayBridgeArgs), offsetof(WinTrayBridgeArgs, host_created),
               offsetof(WinTrayBridgeArgs, target_created), offsetof(WinTrayBridgeArgs, broker),
               offsetof(WinTrayBridgeArgs, stop_event), offsetof(WinTrayBridgeArgs, ready_event));
        FreeLibrary(dll);
        return 0;
    }
    BOOL start_icon = FALSE;
    BOOL guid = FALSE;
    for (int i = 1; i < argc; ++i) {
        if (wcscmp(argv[i], L"--icon") == 0) start_icon = TRUE;
        else if (wcscmp(argv[i], L"--ansi") == 0) use_ansi = TRUE;
        else if (wcscmp(argv[i], L"--guid") == 0) guid = TRUE;
        else if (wcscmp(argv[i], L"--shell") == 0) shell_fixture = TRUE;
        else if (wcscmp(argv[i], L"--target") == 0 && i + 1 < argc) test_shell = (HWND)(UINT_PTR)_wcstoui64(argv[++i], NULL, 10);
        else return ERROR_INVALID_PARAMETER;
    }
    HINSTANCE instance = GetModuleHandleW(NULL);
    WNDCLASSW wc = {0};
    wc.lpfnWndProc = window_proc;
    wc.hInstance = instance;
    wc.lpszClassName = shell_fixture ? L"WinTray.TestShellEndpoint" : L"WinTray.BridgeTestClient";
    wc.hCursor = LoadCursorW(NULL, MAKEINTRESOURCEW(32512));
    wc.hbrBackground = (HBRUSH)(COLOR_WINDOW + 1);
    if (!RegisterClassW(&wc))
        return (int)GetLastError();
    taskbar_created = RegisterWindowMessageW(L"TaskbarCreated");
    HWND hwnd = CreateWindowExW(WS_EX_TOOLWINDOW, wc.lpszClassName, L"WinTray native bridge fixture",
                                 WS_OVERLAPPEDWINDOW, 200, 200, 340, 140, NULL, NULL, instance, NULL);
    if (!hwnd)
        return (int)GetLastError();
    icon.cbSize = sizeof(icon);
    icon.hWnd = hwnd;
    icon.uID = ICON_UID;
    icon.hIcon = LoadIconW(NULL, MAKEINTRESOURCEW(32512));
    icon.uCallbackMessage = ICON_CALLBACK;
    if (guid)
        icon.guidItem = fixture_guid(GetCurrentProcessId());
    if (start_icon && !add_icon())
        return ERROR_INVALID_DATA;
    printf("%llu %lu\n", (unsigned long long)(UINT_PTR)hwnd, GetCurrentProcessId());
    fflush(stdout);
    MSG message;
    while (GetMessageW(&message, NULL, 0, 0) > 0) {
        TranslateMessage(&message);
        DispatchMessageW(&message);
    }
    return 0;
}
