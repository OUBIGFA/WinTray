#include "bridge.h"
#include <stdio.h>
#include <wchar.h>
#include <errno.h>

// Install only on the real taskbar's thread, through the documented Windows
// hook loader. No OpenProcess write/thread rights and no elevation are needed.
static DWORD attach(const wchar_t *dll, const WinTrayBridgeArgs *args) {
    HWND taskbar = (HWND)(UINT_PTR)args->taskbar;
    DWORD pid = 0;
    DWORD thread = GetWindowThreadProcessId(taskbar, &pid);
    if (!thread || pid != args->target_pid || !wintray_is_taskbar(taskbar)) return ERROR_INVALID_WINDOW_HANDLE;
    HANDLE process = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
    if (!process) return GetLastError();
    BOOL valid = wintray_process_created(process) == args->target_created;
    CloseHandle(process);
    if (!valid) return ERROR_INVALID_PARAMETER;
    HMODULE module = LoadLibraryW(dll);
    if (!module) return GetLastError();
    HOOKPROC proc = (HOOKPROC)GetProcAddress(module, WINTRAY_HOOK_EXPORT);
    if (!proc) { DWORD error = GetLastError(); FreeLibrary(module); return error; }
    wchar_t name[140];
    swprintf_s(name, ARRAYSIZE(name), L"%s.result", args->stop_event);
    HANDLE mapping = CreateFileMappingW(INVALID_HANDLE_VALUE, NULL, PAGE_READWRITE, 0, sizeof(WinTrayBridgeReply), name);
    DWORD error = GetLastError();
    if (!mapping || error == ERROR_ALREADY_EXISTS) {
        if (mapping) CloseHandle(mapping);
        FreeLibrary(module); return error;
    }
    WinTrayBridgeReply *reply = MapViewOfFile(mapping, FILE_MAP_ALL_ACCESS, 0, 0, sizeof(*reply));
    if (!reply) { error = GetLastError(); CloseHandle(mapping); FreeLibrary(module); return error; }
    reply->result = ERROR_IO_PENDING;
    HHOOK hook = SetWindowsHookExW(WH_CALLWNDPROC, proc, module, thread);
    if (!hook) { error = GetLastError(); goto done; }
    COPYDATASTRUCT data = {WINTRAY_INSTALL_DATA, sizeof(*args), (void *)args};
    DWORD_PTR result = 0;
    if (!SendMessageTimeoutW(taskbar, WM_COPYDATA, (WPARAM)args->broker, (LPARAM)&data,
                             SMTO_ABORTIFHUNG, 10000, &result)) {
        error = GetLastError();
        if (!error) error = ERROR_TIMEOUT;
    } else {
        error = (DWORD)InterlockedCompareExchange(&reply->result, 0, 0);
    }
    if (!UnhookWindowsHookEx(hook) && !error) error = GetLastError();
done:
    UnmapViewOfFile(reply);
    CloseHandle(mapping);
    FreeLibrary(module);
    return error;
}

static BOOL number(const wchar_t *text, uint64_t *value) {
    if (!text[0] || text[0] == L'-' || text[0] == L'+') return FALSE;
    wchar_t *end;
    errno = 0;
    *value = _wcstoui64(text, &end, 10);
    return !errno && !*end;
}

int wmain(int argc, wchar_t **argv) {
    WinTrayBridgeArgs args = {0};
    uint64_t pid, host;
    if (argc != 11 || wcscmp(argv[1], L"--attach") ||
        !number(argv[2], &pid) || !pid || pid > MAXDWORD ||
        !number(argv[3], &args.target_created) || !args.target_created ||
        !number(argv[4], &host) || !host || host > MAXDWORD ||
        !number(argv[5], &args.host_created) || !args.host_created ||
        !number(argv[6], &args.broker) || !args.broker ||
        !number(argv[7], &args.taskbar) || !args.taskbar ||
        !argv[8][0] || wcslen(argv[8]) >= ARRAYSIZE(args.stop_event) ||
        !argv[9][0] || wcslen(argv[9]) >= ARRAYSIZE(args.ready_event)) {
        fprintf(stderr, "Invalid private shell-bridge arguments\n"); return ERROR_INVALID_PARAMETER;
    }
    args.size = sizeof(args);
    args.protocol = WINTRAY_BRIDGE_PROTOCOL;
    args.target_pid = (DWORD)pid;
    args.host_pid = (DWORD)host;
    wcscpy_s(args.stop_event, ARRAYSIZE(args.stop_event), argv[8]);
    wcscpy_s(args.ready_event, ARRAYSIZE(args.ready_event), argv[9]);
    DWORD error = attach(argv[10], &args);
    if (error) fprintf(stderr, "Shell tray bridge failed: pid=%lu error=0x%08lX (%lu)\n", args.target_pid, error, error);
    return (int)error;
}
