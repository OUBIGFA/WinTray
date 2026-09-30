param([long]$Taskbar, [long]$Owner, [ValidateSet('Pin','Swap')][string]$Action = 'Swap')
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -ReferencedAssemblies System.Drawing @'
using System;
using System.Runtime.InteropServices;
public static class TrayDragInput {
    [StructLayout(LayoutKind.Sequential)] public struct Point { public int X, Y; }
    [DllImport("user32.dll")] public static extern bool GetCursorPos(out Point p);
    [DllImport("user32.dll")] public static extern void mouse_event(uint flags, uint x, uint y, uint data, UIntPtr extra);
    [DllImport("user32.dll")] public static extern void keybd_event(byte key, byte scan, uint flags, UIntPtr extra);
    [DllImport("user32.dll")] public static extern int GetSystemMetrics(int metric);
    public static void Move(int x, int y) {
        int left = GetSystemMetrics(76), top = GetSystemMetrics(77);
        uint dx = (uint)(((long)x - left) * 65536 / GetSystemMetrics(78));
        uint dy = (uint)(((long)y - top) * 65536 / GetSystemMetrics(79));
        // Generate input, not just SetCursorPos: XAML dragging needs mouse movement events.
        mouse_event(0xC001, dx, dy, 0, UIntPtr.Zero);
    }
    public static void CancelDrag() {
        keybd_event(0x1B, 0, 0, UIntPtr.Zero);
        keybd_event(0x1B, 0, 2, UIntPtr.Zero);
    }
    [StructLayout(LayoutKind.Sequential)] public struct Identifier { public uint Size; public IntPtr Hwnd; public uint Uid; public Guid Guid; }
    [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left, Top, Right, Bottom; }
    [DllImport("shell32.dll")] public static extern int Shell_NotifyIconGetRect(ref Identifier id, out Rect rect);
    public static System.Drawing.Rectangle IconRect(long owner, uint uid) {
        var id = new Identifier { Size = (uint)Marshal.SizeOf(typeof(Identifier)), Hwnd = (IntPtr)owner, Uid = uid };
        Rect rect;
        int hr = Shell_NotifyIconGetRect(ref id, out rect);
        if (hr != 0) throw new Exception("Shell_NotifyIconGetRect failed: " + hr.ToString("X"));
        return new System.Drawing.Rectangle(rect.Left, rect.Top, rect.Right - rect.Left, rect.Bottom - rect.Top);
    }
}
'@
$root = [System.Windows.Automation.AutomationElement]::RootElement
$tray = [System.Windows.Automation.AutomationElement]::FromHandle([IntPtr]$Taskbar)
$buttonCondition = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::AutomationIdProperty, 'SystemTrayIcon')
$toggle = $tray.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $buttonCondition)
$overflowCondition = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::ClassNameProperty, 'TopLevelWindowForOverflowXamlIsland')
function Find-Icon($parent, [string]$name) {
    $condition = [System.Windows.Automation.AndCondition]::new(
        [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::NameProperty, $name),
        [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::ClassNameProperty, 'SystemTray.NormalButton'))
    return $parent.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
}
function Close-Overflow {
    $panel = $root.FindFirst([System.Windows.Automation.TreeScope]::Children, $overflowCondition)
    if ($panel -and !$panel.Current.IsOffscreen) {
        $toggle.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
        Start-Sleep -Milliseconds 300
    }
}
function Drag-Icon($from, $to) {
    $x = [int]($from.X + $from.Width / 2)
    $y = [int]($from.Y + $from.Height / 2)
    $destX = [int]($to.X + $to.Width / 2)
    $destY = [int]($to.Y + $to.Height / 2)
    [TrayDragInput]::Move($x, $y)
    Start-Sleep -Milliseconds 150
    [TrayDragInput]::mouse_event(0x2, 0, 0, 0, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 150
    for ($i = 1; $i -le 25; $i++) {
        [TrayDragInput]::Move([int]($x + ($destX - $x) * $i / 25), [int]($y + ($destY - $y) * $i / 25))
        Start-Sleep -Milliseconds 30
    }
    Start-Sleep -Milliseconds 300
    [TrayDragInput]::mouse_event(0x4, 0, 0, 0, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 1200
}
$old = [TrayDragInput+Point]::new()
if (![TrayDragInput]::GetCursorPos([ref]$old)) { throw 'GetCursorPos failed' }
try {
    if ($Action -eq 'Pin') {
        foreach ($name in @('WinTray drag test A', 'WinTray drag test B')) {
            if (Find-Icon $tray $name) { continue }
            if (!$toggle) { throw 'Overflow button not found' }
            $toggle.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
            Start-Sleep -Milliseconds 1600
            $deadline = [DateTime]::UtcNow.AddSeconds(5)
            do {
                $icon = Find-Icon $root $name
                if ($icon -and !$icon.Current.IsOffscreen) { break }
                Start-Sleep -Milliseconds 100
            } while ([DateTime]::UtcNow -lt $deadline)
            if (!$icon -or $icon.Current.IsOffscreen) { throw "Test icon not visible: $name" }
            $to = [System.Windows.Rect]::new($toggle.Current.BoundingRectangle.Right, $tray.Current.BoundingRectangle.Y, 32, $tray.Current.BoundingRectangle.Height)
            Drag-Icon $icon.Current.BoundingRectangle $to
            Close-Overflow
            if (!(Find-Icon $tray $name)) { throw "Could not pin test icon: $name" }
        }
        Write-Output '{"Pinned":true}'
    } else {
        Close-Overflow
        $a = Find-Icon $tray 'WinTray drag test A'
        $b = Find-Icon $tray 'WinTray drag test B'
        if (!$a -or !$b) { throw 'Both test icons must be on the taskbar before swapping' }
        $beforeA = [TrayDragInput]::IconRect($Owner, 801)
        $beforeB = [TrayDragInput]::IconRect($Owner, 802)
        if ($beforeA.Y -ne $beforeB.Y -or $beforeA.Width -le 0 -or $beforeB.Width -le 0 -or
            [Math]::Abs($beforeA.X - $beforeB.X) -gt [Math]::Max($beforeA.Width, $beforeB.Width)) {
            throw 'Test icons are not adjacent; refusing to move other icons'
        }
        # Use the same direction every time; dropping at the right-hand icon's center need not change order.
        if ($beforeA.X -gt $beforeB.X) { Drag-Icon $beforeA $beforeB }
        else { Drag-Icon $beforeB $beforeA }
        [TrayDragInput]::Move($old.X, $old.Y)
        $afterA = [TrayDragInput]::IconRect($Owner, 801)
        $afterB = [TrayDragInput]::IconRect($Owner, 802)
        $swapped = [Math]::Sign($beforeA.X - $beforeB.X) -eq -[Math]::Sign($afterA.X - $afterB.X)
        [pscustomobject]@{BeforeA=$beforeA.X;BeforeB=$beforeB.X;AfterA=$afterA.X;AfterB=$afterB.X;Swapped=$swapped} | ConvertTo-Json -Compress
    }
} finally {
    [TrayDragInput]::mouse_event(0x4, 0, 0, 0, [UIntPtr]::Zero)
    [TrayDragInput]::CancelDrag()
    Close-Overflow
    [TrayDragInput]::Move($old.X, $old.Y)
}
