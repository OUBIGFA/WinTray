# Explorer 托盘接收端桥接

## 边界

目标是修复 issue #4 的全局假任务栏干扰，同时保留普通权限收纳管理员程序的体验。

- 不创建 `Shell_TrayWnd`，不改变任务栏父窗口、位置、样式、置顶顺序或原生布局子窗口。
- 不向 QQ、剪贴板等被收纳应用加载组件，不请求提权、不修改第三方或系统文件。
- `injector.c` 使用指定线程的 `WH_CALLWNDPROC` 加载 DLL；握手后立即移除临时 Hook。DLL 在任务栏所属线程上使用 `SetWindowSubclass`，不使用 API detour 或远程线程。
- 非托盘消息直接 `DefSubclassProc`；原生侧按路径和明确登记的托管窗口筛选，未收纳图标不进入 Go broker。
- 保留原始托盘包、扩展尾部、通知及音效字段，只修改选中图标的隐藏状态。GUID-only 更新通过原始 owner 解析。
- 目标程序的位数与权限不影响接收端：它们本来就向 Explorer 注册图标。

## 生命周期

1. 启动私有 broker 与独立恢复程序。
2. 核对桌面 Shell 的 PID、创建时间、taskbar HWND、宿主和 nonce 事件，安装休眠的 subclass。
3. 下发选择快照，提交选择，确认后置位 ready；只通知选中程序重建图标。
4. 新应用注册自然经过接收端，无需枚举/注入其 GPU、渲染或网络子进程。Explorer endpoint 变化后重新建立桥接。
5. 取消收纳先恢复原图标，再停止转交。宿主退出或 stop 事件触发后原生过程直接放行，并移除 subclass。
6. 主进程被强制结束时，恢复程序恢复仍存活图标。迟到的 broker 返回不得在取消后重新隐藏图标。

选中托盘请求同步等待 broker 的上限为 500ms，超时取消路由；未知结果不重放通知。普通任务栏输入不等待 broker。DLL 保持加载，避免同步重入的栈帧被卸载；取消后不再拦截。不可变缓存位于 `%LOCALAPPDATA%\WinTray\TrayBridge\<SHA-256>\<架构>`，不锁定便携包。新版和旧版不叠加有效 subclass。

## 构建与验证

需要 Windows、Go、Visual Studio C++ Build Tools（x86/x64 和 Windows SDK），使用静态 CRT。

```powershell
./build/native.ps1 -OutputDir dist/native -Test
$env:WINTRAY_SHELL_BRIDGE_TEST = '1'
go test ./internal/traybox -run '^TestShellBridgeIsolatedReceivingEndpoint$' -count=1 -v
Remove-Item Env:WINTRAY_SHELL_BRIDGE_TEST
```

隔离构建使用私有测试 Shell 类，验证跨进程握手、原样返回、选择、重复拒绝、broker 卡住时未选中消息与普通消息不受阻。它不创建假任务栏，也不进入发布包。

真实桌面验证先关闭 WinTray，临时程序会短暂显示窗口和菜单：

```powershell
$env:WINTRAY_TRAY_LIVE_TEST = '1'
$env:WINTRAY_RECOVERY_EXE = (Resolve-Path dist/WinTray-Recovery.exe).Path
go test ./internal/traybox -count=1 -v
Remove-Item Env:WINTRAY_TRAY_LIVE_TEST, Env:WINTRAY_RECOVERY_EXE

$env:WINTRAY_UI_TEST = '1'
go test ./internal/ui -run '^TestHostedIconsCanBeCollectedAndReleased$' -count=1
go test ./internal/app -run '^TestMainSessionHostingLifecycle$' -count=1
Remove-Item Env:WINTRAY_UI_TEST
./build/package.ps1 -OutputDir dist
```

测试覆盖 x86/x64、ANSI/Unicode、GUID/句柄标识、原生图标更新与回调、取消与重接管、外部启动、崩溃恢复，以及未受管理程序查到的任务栏身份/几何/布局不变。

鼠标拖动需另设 `WINTRAY_TRAY_DRAG_LIVE_TEST=1`；只拖临时图标，测试鼠标需保持空闲。`WINTRAY_EXISTING_OWNER_PATHS` 是明确操作现有程序的单独 opt-in，不是常规测试的一部分。具体第三方软件的视觉效果不能仅由通用测试推断为完全兼容。

包内为主程序、恢复程序及两种架构的 `WinTray-Bridge.exe`、`WinTray-ShellBridge.dll`。不再使用/打包应用侧桥接或 MinHook。
