#include "bridge.h"
#include <commctrl.h>
#include <wchar.h>

// Only Explorer's tray receiver is subclassed. No application APIs, taskbar
// discovery, geometry, input, or on-disk system files are modified.
#define TRAY_SIGNATURE 0x34753423u
#define BROKER_TIMEOUT 500u
#define SUBCLASS_ID 0x57544253u

typedef struct Owner {
    struct Owner *next;
    GUID guid;
    HWND hwnd;
} Owner;

// An in-flight request lives on the receiving thread's stack. Its token is
// valid only while that exact native call is waiting; late Go work cannot
// replay an old hide/balloon over an owner's newer native update.
typedef struct Packet {
    struct Packet *previous;
    uint64_t token;
    BOOL forwarded;
    LRESULT result;
} Packet;

typedef struct Route {
    HANDLE host, stopped, ready;
    HWND broker;
    DWORD host_pid, broker_thread;
    unsigned depth;
    BOOL detached;
    BYTE *selection;
    DWORD selection_size;
    Owner *owners;
    uint64_t serial, pause_token;
    DWORD timeout_error;
    Packet *packets;
    WinTrayObserved *observed;
    DWORD observed_count, observed_capacity, observed_sent;
    BOOL observed_overflow;
} Route;

static void release_route(Route *route) {
    CloseHandle(route->host);
    CloseHandle(route->stopped);
    CloseHandle(route->ready);
    if (route->observed) HeapFree(GetProcessHeap(), 0, route->observed);
    if (route->selection) HeapFree(GetProcessHeap(), 0, route->selection);
    while (route->owners) { Owner *next = route->owners->next; HeapFree(GetProcessHeap(), 0, route->owners); route->owners = next; }
    HeapFree(GetProcessHeap(), 0, route);
}

static BOOL route_alive(Route *route) {
    DWORD pid = 0;
    DWORD thread = GetWindowThreadProcessId(route->broker, &pid);
    return WaitForSingleObject(route->host, 0) == WAIT_TIMEOUT &&
           WaitForSingleObject(route->stopped, 0) == WAIT_TIMEOUT &&
           pid == route->host_pid && thread == route->broker_thread;
}

static BOOL tray_packet(const COPYDATASTRUCT *data) {
    if (!data || (data->dwData != 1 && data->dwData != 3) ||
        data->cbData < 4 || data->cbData > 65536 || !data->lpData) return FALSE;
    return *(DWORD *)data->lpData == TRAY_SIGNATURE;
}

// Native selection is a bounded snapshot: byte length of the MULTI_SZ paths,
// owner count, hosted HWNDs, then paths. Unselected traffic never enters Go.
static BOOL selected_owner(Route *route, HWND owner) {
    if (!owner || !route->selection) return FALSE;
    const DWORD *header = (const DWORD *)route->selection;
    const DWORD *hosted = header + 2;
    for (DWORD i = 0; i < header[1]; ++i)
        if ((DWORD)(UINT_PTR)owner == hosted[i]) return TRUE;
    DWORD pid = 0;
    GetWindowThreadProcessId(owner, &pid);
    if (!pid) return FALSE;
    HANDLE process = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid);
    if (!process) return FALSE;
    wchar_t path[32768];
    DWORD length = ARRAYSIZE(path);
    BOOL found = QueryFullProcessImageNameW(process, 0, path, &length);
    CloseHandle(process);
    if (!found) return FALSE;
    const wchar_t *item = (const wchar_t *)(hosted + header[1]);
    const wchar_t *end = item + header[0] / sizeof(wchar_t);
    while (item < end && *item) {
        if (CompareStringOrdinal(item, -1, path, -1, TRUE) == CSTR_EQUAL) return TRUE;
        item += wcslen(item) + 1;
    }
    return FALSE;
}

static BOOL selected_packet(Route *route, const COPYDATASTRUCT *data) {
    if (!tray_packet(data)) return FALSE;
    const BYTE *bytes = data->lpData;
    HWND owner = NULL;
    GUID guid = {0}, empty = {0};
    if (data->dwData == 1 && data->cbData >= 964) {
        owner = (HWND)(UINT_PTR)*(const DWORD *)(bytes + 12);
        if (*(const DWORD *)(bytes + 20) & 0x20) memcpy(&guid, bytes + 944, sizeof(guid));
    } else if (data->dwData == 3 && data->cbData >= 40) {
        owner = (HWND)(UINT_PTR)*(const DWORD *)(bytes + 16);
        memcpy(&guid, bytes + 24, sizeof(guid));
    } else return FALSE;
    BOOL has_guid = memcmp(&guid, &empty, sizeof(guid)) != 0;
    if (!owner && has_guid) {
        for (Owner *item = route->owners; item; item = item->next)
            if (!memcmp(&item->guid, &guid, sizeof(guid))) { owner = item->hwnd; break; }
    }
    if (!selected_owner(route, owner)) return FALSE;
    if (data->dwData == 1 && *(const DWORD *)(bytes + 4) == 0 && has_guid) {
        Owner *item = route->owners;
        while (item && memcmp(&item->guid, &guid, sizeof(guid))) item = item->next;
        if (!item) {
            item = HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*item));
            if (!item) return FALSE;
            item->guid = guid; item->next = route->owners; route->owners = item;
        }
        item->hwnd = owner;
    }
    if (data->dwData == 1 && *(const DWORD *)(bytes + 4) == 2 && has_guid) {
        Owner **slot = &route->owners;
        while (*slot) {
            Owner *item = *slot;
            if (!memcmp(&item->guid, &guid, sizeof(guid))) {
                *slot = item->next; HeapFree(GetProcessHeap(), 0, item); break;
            }
            slot = &item->next;
        }
    }
    return TRUE;
}

static BOOL update_selection(Route *route, const COPYDATASTRUCT *data) {
    if (!data->lpData || data->cbData < 12 || data->cbData > WINTRAY_MAX_SELECTION) return FALSE;
    const DWORD *header = data->lpData;
    if (header[1] > (data->cbData - 8) / 4 || header[0] < 4 || header[0] % 2 ||
        header[0] != data->cbData - 8 - header[1] * 4) return FALSE;
    const wchar_t *paths = (const wchar_t *)(header + 2 + header[1]);
    if (paths[header[0] / 2 - 1] || paths[header[0] / 2 - 2]) return FALSE;
    BYTE *copy = HeapAlloc(GetProcessHeap(), 0, data->cbData);
    if (!copy) return FALSE;
    memcpy(copy, data->lpData, data->cbData);
    if (route->selection) HeapFree(GetProcessHeap(), 0, route->selection);
    route->selection = copy; route->selection_size = data->cbData;
    return TRUE;
}

static void observe_native(Route *route, const COPYDATASTRUCT *data) {
    if (data->dwData != 1 || data->cbData < 964) return;
    const DWORD *words = data->lpData;
    DWORD message = words[1];
    if (message > 2 && message != 4) return;
    if (message == 1 && !(words[5] & 8)) return; // image/tooltip updates need no recovery journal
    if (route->observed_count == route->observed_capacity) {
        DWORD capacity = route->observed_capacity ? route->observed_capacity * 2 : 16;
        if (capacity > 4096) { route->observed_overflow = TRUE; return; }
        void *buffer = route->observed ? HeapReAlloc(GetProcessHeap(), 0, route->observed, capacity * sizeof(WinTrayObserved)) :
                                         HeapAlloc(GetProcessHeap(), 0, capacity * sizeof(WinTrayObserved));
        if (!buffer) { route->observed_overflow = TRUE; return; }
        route->observed = buffer; route->observed_capacity = capacity;
    }
    WinTrayObserved *item = &route->observed[route->observed_count++];
    *item = (WinTrayObserved){message, words[3], words[4], words[5], words[72], words[73], words[202], {0}};
    memcpy(&item->guid, (const BYTE *)data->lpData + 944, sizeof(GUID));
}

// A selected owner's rebuild ADD can meet its still-existing registration
// while the broker is paused. Preserve the successful native registration as
// in the normal Go path; otherwise clients cache FALSE and stop rebuilding.
static LRESULT accept_duplicate(HWND hwnd, WPARAM wp, const COPYDATASTRUCT *data, LRESULT result) {
    if (!result && data->dwData == 1 && data->cbData >= 964 &&
        ((const DWORD *)data->lpData)[1] == 0) {
        BYTE *copy = HeapAlloc(GetProcessHeap(), 0, data->cbData);
        if (copy) {
            memcpy(copy, data->lpData, data->cbData);
            ((DWORD *)copy)[1] = 1;
            COPYDATASTRUCT modify = {data->dwData, data->cbData, copy};
            result = DefSubclassProc(hwnd, WM_COPYDATA, wp, (LPARAM)&modify);
            HeapFree(GetProcessHeap(), 0, copy);
        }
    }
    return result;
}

static LRESULT CALLBACK shell_proc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp,
                                   UINT_PTR id, DWORD_PTR ref) {
    Route *route = (Route *)ref;
    ++route->depth;
    // Ordinary taskbar input/layout messages take the original path without
    // process queries, IPC, or waiting for the WinTray broker.
    if (msg != WM_COPYDATA && msg != WM_NCDESTROY && !(msg == WM_TIMER && wp == (UINT_PTR)route)) {
        LRESULT original = DefSubclassProc(hwnd, msg, wp, lp);
        if (--route->depth == 0 && route->detached) release_route(route);
        return original;
    }
    BOOL alive = route_alive(route);
    LRESULT result = 0;
    if (msg == WM_NCDESTROY || (msg == WM_TIMER && wp == (UINT_PTR)route && !alive)) {
        RemoveWindowSubclass(hwnd, shell_proc, id);
        RemovePropW(hwnd, WINTRAY_ROUTE_PROPERTY);
        KillTimer(hwnd, (UINT_PTR)route);
        route->detached = TRUE;
    }
    if (msg == WM_TIMER && wp == (UINT_PTR)route) {
        if (alive && route->pause_token) {
            PostMessageW(route->broker, WINTRAY_BROKER_RESUME, (WPARAM)route->pause_token, route->timeout_error);
        }
        goto done;
    }
    if (msg == WM_COPYDATA && lp) {
        const COPYDATASTRUCT *data = (COPYDATASTRUCT *)lp;
        if (data->dwData == WINTRAY_SELECT_DATA && wp == (WPARAM)route->broker && alive) {
            result = update_selection(route, data);
            goto done;
        }
        if (data->dwData == WINTRAY_RESUME_DATA && wp == (WPARAM)route->broker && alive &&
            data->cbData == sizeof(uint64_t) && data->lpData) {
            uint64_t token; memcpy(&token, data->lpData, sizeof(token));
            if (!route->pause_token) { result = 2; goto done; }
            // A different active pause is not a completed resume. Reporting
            // 2 here makes Go discard the real resume notification as stale.
            if (route->pause_token != token) goto done;
            if (!route->packets) {
                if (route->observed_overflow) { SetEvent(route->stopped); goto done; }
                // Acknowledged batches remain ordered and retryable. The Go
                // receiver deduplicates (pause token, offset), so a timeout in
                // this handshake cannot replay stale state after resumption.
                DWORD count = route->observed_count - route->observed_sent;
                if (count) {
                    DWORD size = 8 + count * sizeof(WinTrayObserved);
                    BYTE *wire = HeapAlloc(GetProcessHeap(), 0, size);
                    if (!wire) goto done;
                    ((DWORD *)wire)[0] = (DWORD)token;
                    ((DWORD *)wire)[1] = route->observed_sent;
                    memcpy(wire + 8, route->observed + route->observed_sent, size - 8);
                    COPYDATASTRUCT observed = {WINTRAY_OBSERVED_DATA, size, wire};
                    DWORD_PTR ack = 0;
                    LRESULT delivered = SendMessageTimeoutW(route->broker, WM_COPYDATA, (WPARAM)hwnd,
                                            (LPARAM)&observed, SMTO_ABORTIFHUNG, BROKER_TIMEOUT, &ack);
                    HeapFree(GetProcessHeap(), 0, wire);
                    if (!delivered || !ack) goto done;
                    route->observed_sent += count;
                }
                if (route->observed_sent == route->observed_count) {
                    route->observed_count = route->observed_sent = 0;
                    route->pause_token = 0; result = 1;
                }
            }
            goto done;
        }
        // A broker-forwarded packet must reach the original shell procedure,
        // not reenter the broker. WM_COPYDATA marshals both headers and bytes;
        // never transmit pointers between processes.
        if (data->dwData == WINTRAY_FORWARD_DATA && wp == (WPARAM)route->broker &&
            data->cbData >= 24 && data->cbData <= 65560 && data->lpData) {
            const uint64_t *header = (const uint64_t *)data->lpData;
            COPYDATASTRUCT original = {(ULONG_PTR)header[0], data->cbData - 24, (BYTE *)data->lpData + 24};
            if (!tray_packet(&original)) goto done;
            Packet *packet = route->packets;
            if (header[2]) {
                while (packet && packet->token != header[2]) packet = packet->previous;
                if (!packet || route->pause_token) { result = WINTRAY_EXPIRED_REPLY; goto done; }
            }
            if (route->pause_token && original.dwData == 1) { result = WINTRAY_EXPIRED_REPLY; goto done; }
            // Once cancelled, a delayed broker must not hide an icon after
            // recovery. Only state-only visibility restoration/probes remain
            // valid on the return leg; never replay a timed-out notification.
            if (!alive && original.dwData == 1) {
                const DWORD *words = original.lpData;
                if (original.cbData < 964 || words[1] != 1 || (words[5] & ~0x28u) ||
                    !(words[5] & 8) || (words[72] & 1)) goto done;
            }
            result = DefSubclassProc(hwnd, msg, (WPARAM)header[1], (LPARAM)&original);
            if (header[2] && packet) { packet->forwarded = TRUE; packet->result = result; }
            goto done;
        }
        if (alive && route->pause_token && selected_packet(route, data)) {
            result = accept_duplicate(hwnd, wp, data, DefSubclassProc(hwnd, msg, wp, lp));
            if (result) observe_native(route, data);
            goto done;
        }
        if (alive && !route->pause_token && WaitForSingleObject(route->ready, 0) == WAIT_OBJECT_0 && selected_packet(route, data)) {
            BYTE *wire = HeapAlloc(GetProcessHeap(), 0, 24 + data->cbData);
            if (!wire) { result = DefSubclassProc(hwnd, msg, wp, lp); goto done; }
            // Stay within a 32-bit message word for the resume notification.
            route->serial = (route->serial % 0xffffffffu) + 1;
            Packet packet = {route->packets, route->serial, FALSE, 0};
            route->packets = &packet;
            uint64_t header[3] = {data->dwData, (uint64_t)wp, packet.token};
            memcpy(wire, header, sizeof(header)); memcpy(wire + 24, data->lpData, data->cbData);
            COPYDATASTRUCT request = {WINTRAY_PACKET_DATA, 24 + data->cbData, wire};
            DWORD_PTR reply = 0;
            SetLastError(ERROR_SUCCESS);
            LRESULT sent = SendMessageTimeoutW(route->broker, msg, (WPARAM)hwnd, (LPARAM)&request,
                                                SMTO_ABORTIFHUNG, BROKER_TIMEOUT, &reply);
            DWORD error = GetLastError();
            route->packets = packet.previous;
            HeapFree(GetProcessHeap(), 0, wire);
            if (sent && !route->pause_token) { result = (LRESULT)reply; goto done; }
            // SendMessageTimeout pumps nested native calls. If an inner call
            // paused the route, Go can answer the outer call without having
            // forwarded it. Finish that native request exactly once as well.
            if (!sent) {
                // A late outer timeout must not move the pause generation
                // backwards to its older packet token. All nested timeouts
                // share the newest issued token until the stack has unwound.
                route->pause_token = route->serial;
                route->timeout_error = error ? error : ERROR_TIMEOUT;
            }
            result = accept_duplicate(hwnd, wp, data, packet.forwarded ? packet.result : DefSubclassProc(hwnd, msg, wp, lp));
            if (!packet.forwarded && result) observe_native(route, data);
            PostMessageW(route->broker, WINTRAY_BROKER_RESUME, (WPARAM)route->pause_token, route->timeout_error);
            goto done;
        }
    }
    result = DefSubclassProc(hwnd, msg, wp, lp);
done:
    if (--route->depth == 0 && route->detached) release_route(route);
    return result;
}

static DWORD install(HWND hwnd, const WinTrayBridgeArgs *args) {
    if (args->size != sizeof(*args) || args->protocol != WINTRAY_BRIDGE_PROTOCOL ||
        args->taskbar != (uint64_t)(UINT_PTR)hwnd || args->target_pid != GetCurrentProcessId() ||
        args->target_created != wintray_process_created(GetCurrentProcess()) ||
        !args->host_created || !wintray_is_taskbar(hwnd) ||
        args->stop_event[127] || args->ready_event[127]) return ERROR_INVALID_PARAMETER;
    DWORD pid = 0;
    wchar_t cls[64];
    HWND broker = (HWND)(UINT_PTR)args->broker;
    DWORD broker_thread = GetWindowThreadProcessId(broker, &pid);
    if (pid != args->host_pid || !broker_thread ||
        !GetClassNameW(broker, cls, ARRAYSIZE(cls)) || lstrcmpW(cls, WINTRAY_BROKER_CLASS))
        return ERROR_INVALID_WINDOW_HANDLE;
    DWORD_PTR ref = 0;
    if (GetWindowSubclass(hwnd, shell_proc, SUBCLASS_ID, &ref)) {
        Route *old = (Route *)ref;
        if (route_alive(old) || old->depth) return ERROR_ALREADY_EXISTS;
        RemoveWindowSubclass(hwnd, shell_proc, SUBCLASS_ID);
        RemovePropW(hwnd, WINTRAY_ROUTE_PROPERTY);
        KillTimer(hwnd, (UINT_PTR)old);
        release_route(old);
    }
    // The property is shared across DLL builds; never stack different bridge
    // versions over a live or not-yet-detached subclass.
    if (GetPropW(hwnd, WINTRAY_ROUTE_PROPERTY)) return ERROR_ALREADY_EXISTS;
    Route *route = HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*route));
    if (!route) return ERROR_NOT_ENOUGH_MEMORY;
    route->broker = broker;
    route->host_pid = args->host_pid;
    route->broker_thread = broker_thread;
    route->host = OpenProcess(SYNCHRONIZE | PROCESS_QUERY_LIMITED_INFORMATION, FALSE, args->host_pid);
    route->stopped = OpenEventW(SYNCHRONIZE | EVENT_MODIFY_STATE, FALSE, args->stop_event);
    route->ready = OpenEventW(SYNCHRONIZE, FALSE, args->ready_event);
    DWORD error = GetLastError();
    if (!route->host || !route->stopped || !route->ready ||
        wintray_process_created(route->host) != args->host_created || !route_alive(route)) {
        release_route(route);
        return error ? error : ERROR_CANCELLED;
    }
    HMODULE pinned;
    if (!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_PIN,
                           (LPCWSTR)&shell_proc, &pinned)) {
        error = GetLastError(); release_route(route); return error;
    }
    if (!SetPropW(hwnd, WINTRAY_ROUTE_PROPERTY, (HANDLE)broker)) {
        error = GetLastError(); release_route(route); return error;
    }
    if (!SetWindowSubclass(hwnd, shell_proc, SUBCLASS_ID, (DWORD_PTR)route)) {
        RemovePropW(hwnd, WINTRAY_ROUTE_PROPERTY); release_route(route); return ERROR_INVALID_FUNCTION;
    }
    if (!SetTimer(hwnd, (UINT_PTR)route, 250, NULL)) {
        RemoveWindowSubclass(hwnd, shell_proc, SUBCLASS_ID);
        RemovePropW(hwnd, WINTRAY_ROUTE_PROPERTY); release_route(route); return ERROR_NOT_ENOUGH_MEMORY;
    }
    // Sent before activation/new packets, in the same sender-thread order.
    // Explorer recreation starts a fresh packet sequence in the same broker.
    DWORD_PTR acknowledged = 0;
    if (!SendMessageTimeoutW(broker, WINTRAY_BROKER_EPOCH, 0, 0, SMTO_ABORTIFHUNG, BROKER_TIMEOUT, &acknowledged)) {
        RemoveWindowSubclass(hwnd, shell_proc, SUBCLASS_ID);
        RemovePropW(hwnd, WINTRAY_ROUTE_PROPERTY);
        KillTimer(hwnd, (UINT_PTR)route);
        release_route(route);
        return ERROR_TIMEOUT;
    }
    return ERROR_SUCCESS;
}

// Temporary thread-specific hook installs the subclass on its owner thread.
// The helper owns a nonce-named result mapping and removes this hook on return.
__declspec(dllexport) LRESULT CALLBACK WinTrayShellHook(int code, WPARAM wp, LPARAM lp) {
    if (code >= 0 && lp) {
        const CWPSTRUCT *message = (CWPSTRUCT *)lp;
        if (message->message == WM_COPYDATA && message->lParam && wintray_is_taskbar(message->hwnd)) {
            const COPYDATASTRUCT *data = (COPYDATASTRUCT *)message->lParam;
            if (data->dwData == WINTRAY_INSTALL_DATA && data->cbData == sizeof(WinTrayBridgeArgs) && data->lpData) {
                const WinTrayBridgeArgs *args = (WinTrayBridgeArgs *)data->lpData;
                if (!args->stop_event[127]) {
                    wchar_t name[140];
                    if (swprintf_s(name, ARRAYSIZE(name), L"%s.result", args->stop_event) > 0) {
                        HANDLE mapping = OpenFileMappingW(FILE_MAP_WRITE, FALSE, name);
                        if (mapping) {
                            WinTrayBridgeReply *reply = MapViewOfFile(mapping, FILE_MAP_WRITE, 0, 0, sizeof(*reply));
                            if (reply) {
                                InterlockedExchange(&reply->result, (LONG)install(message->hwnd, args));
                                UnmapViewOfFile(reply);
                            }
                            CloseHandle(mapping);
                        }
                    }
                }
            }
        }
    }
    return CallNextHookEx(NULL, code, wp, lp);
}

BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
    (void)reserved;
    if (reason == DLL_PROCESS_ATTACH) DisableThreadLibraryCalls(instance);
    return TRUE;
}
