param([Parameter(Mandatory=$true)][string]$ProcessName)
$ErrorActionPreference = 'Stop'
# Inspect Explorer's actual UI rather than treating DeleteTab/SW_HIDE success
# as evidence that the taskbar no longer shows a button. Only fixture names
# are reported; no user's window titles leave this query.
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$primary = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ClassNameProperty, 'Shell_TrayWnd')
$secondary = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ClassNameProperty, 'Shell_SecondaryTrayWnd')
$condition = New-Object System.Windows.Automation.OrCondition($primary, $secondary)
$taskbars = [System.Windows.Automation.AutomationElement]::RootElement.FindAll([System.Windows.Automation.TreeScope]::Children, $condition)
$count = 0
foreach ($taskbar in $taskbars) {
  $elements = $taskbar.FindAll([System.Windows.Automation.TreeScope]::Descendants, [System.Windows.Automation.Condition]::TrueCondition)
  foreach ($element in $elements) {
    $state = $element.Current
    if (!$state.IsOffscreen -and $state.BoundingRectangle.Width -gt 0 -and
        ($state.Name.StartsWith($ProcessName, [StringComparison]::OrdinalIgnoreCase) -or $state.Name.StartsWith('Fixture UI'))) {
      $count++
    }
  }
}
$count
