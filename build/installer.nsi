Unicode true
RequestExecutionLevel user
ManifestSupportedOS win10
SetCompressor /SOLID lzma
!include "MUI2.nsh"
!include "LogicLib.nsh"

!ifndef WINTRAY_PAYLOAD_DIR
  !error "WINTRAY_PAYLOAD_DIR is required"
!endif
!ifndef WINTRAY_OUTPUT
  !error "WINTRAY_OUTPUT is required"
!endif
!ifndef WINTRAY_REGISTRY_KEY
  !define WINTRAY_REGISTRY_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\WinTray"
!endif

Name "WinTray"
OutFile "${WINTRAY_OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\WinTray"
InstallDirRegKey HKCU "${WINTRAY_REGISTRY_KEY}" "InstallLocation"
ShowInstDetails show
ShowUninstDetails show
VIProductVersion "${WINTRAY_VERSION}.0"
VIAddVersionKey ProductName "WinTray"
VIAddVersionKey FileDescription "WinTray Setup"
VIAddVersionKey FileVersion "${WINTRAY_VERSION}"
VIAddVersionKey LegalCopyright "Copyright (c) OUBIGFA"
!define MUI_ICON "..\internal\branding\assets\logo.ico"
!define MUI_UNICON "..\internal\branding\assets\logo.ico"
!define MUI_ABORTWARNING
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\WinTray.exe"
!define MUI_FINISHPAGE_RUN_NOTCHECKED
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

LangString ExitFirst ${LANG_SIMPCHINESE} "请先从 WinTray 中选择“退出 WinTray”，然后重试。"
LangString ExitFirst ${LANG_ENGLISH} "Select Exit WinTray in WinTray, then retry."
LangString RestoreFailed ${LANG_SIMPCHINESE} "无法还原开机启动设置。文件已保留。请打开 WinTray 的“设置”，检查开机启动选项，然后重试卸载。"
LangString RestoreFailed ${LANG_ENGLISH} "Startup settings could not be restored. Files were kept. Open Settings in WinTray, check the sign-in options, then retry uninstalling."
LangString ProfileLocation ${LANG_SIMPCHINESE} "请选择独立的程序文件夹。用户数据目录不能作为安装位置。"
LangString ProfileLocation ${LANG_ENGLISH} "Choose a separate program folder. The user data folder cannot be used for installation."
LangString RemoveFailed ${LANG_SIMPCHINESE} "部分程序文件无法删除。请关闭占用这些文件的程序，然后重试卸载。"
LangString RemoveFailed ${LANG_ENGLISH} "Some program files could not be removed. Close programs using these files, then retry uninstalling."

; Test builds redirect only OS integration. The copy, upgrade and uninstall
; file lists remain identical and never touch the user's installed copy.
!macro CheckRunning PREFIX
Function ${PREFIX}CheckRunning
!ifndef WINTRAY_TEST_ROOT
  System::Call 'kernel32::OpenMutexW(i 0x100000, i 0, w "WinTray_SingleInstance") p.r0'
  ${If} $0 != 0
    System::Call 'kernel32::CloseHandle(p r0)'
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(ExitFirst)" /SD IDOK
    SetErrorLevel 2
    Abort
  ${EndIf}
!endif
FunctionEnd
!macroend
!insertmacro CheckRunning ""
!insertmacro CheckRunning "un."

Function .onInit
  SetShellVarContext current
  Call CheckRunning
FunctionEnd
Function un.onInit
  SetShellVarContext current
  Call un.CheckRunning
FunctionEnd
Function .onVerifyInstDir
  ${If} $INSTDIR == "$LOCALAPPDATA\WinTray"
    Abort
  ${EndIf}
FunctionEnd

Section "WinTray"
  Call CheckRunning
  ${If} $INSTDIR == "$LOCALAPPDATA\WinTray"
    MessageBox MB_OK|MB_ICONEXCLAMATION "$(ProfileLocation)" /SD IDOK
    SetErrorLevel 3
    Abort
  ${EndIf}
  SetOutPath "$INSTDIR"
  ; Explicit inputs exclude logs, profiles and stale staging files.
  File "${WINTRAY_PAYLOAD_DIR}\WinTray.exe"
  File "${WINTRAY_PAYLOAD_DIR}\WinTray-Recovery.exe"
  File "${WINTRAY_PAYLOAD_DIR}\WinTray.exe.manifest"
  File "${WINTRAY_PAYLOAD_DIR}\checksums.txt"
  File "${WINTRAY_PAYLOAD_DIR}\ThirdPartyNotices.txt"
  File "${WINTRAY_PAYLOAD_DIR}\LICENSE.txt"
  SetOutPath "$INSTDIR\native\x64"
  File "${WINTRAY_PAYLOAD_DIR}\native\x64\WinTray-Bridge.exe"
  File "${WINTRAY_PAYLOAD_DIR}\native\x64\WinTray-ShellBridge.dll"
  SetOutPath "$INSTDIR\native\x86"
  File "${WINTRAY_PAYLOAD_DIR}\native\x86\WinTray-Bridge.exe"
  File "${WINTRAY_PAYLOAD_DIR}\native\x86\WinTray-ShellBridge.dll"
  SetOutPath "$INSTDIR"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
!ifdef WINTRAY_TEST_ROOT
  CreateShortCut "${WINTRAY_TEST_ROOT}\WinTray.lnk" "$INSTDIR\WinTray.exe"
!else
  CreateDirectory "$SMPROGRAMS\WinTray"
  CreateShortCut "$SMPROGRAMS\WinTray\WinTray.lnk" "$INSTDIR\WinTray.exe"
  CreateShortCut "$SMPROGRAMS\WinTray\Uninstall.lnk" "$INSTDIR\Uninstall.exe"
!endif
  WriteRegStr HKCU "${WINTRAY_REGISTRY_KEY}" "DisplayName" "WinTray"
  WriteRegStr HKCU "${WINTRAY_REGISTRY_KEY}" "DisplayVersion" "${WINTRAY_VERSION}"
  WriteRegStr HKCU "${WINTRAY_REGISTRY_KEY}" "Publisher" "OUBIGFA"
  WriteRegStr HKCU "${WINTRAY_REGISTRY_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${WINTRAY_REGISTRY_KEY}" "DisplayIcon" "$INSTDIR\WinTray.exe"
  WriteRegStr HKCU "${WINTRAY_REGISTRY_KEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${WINTRAY_REGISTRY_KEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "${WINTRAY_REGISTRY_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${WINTRAY_REGISTRY_KEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  Call un.CheckRunning
!ifndef WINTRAY_TEST_ROOT
  ClearErrors
  ExecWait '"$INSTDIR\WinTray.exe" --prepare-uninstall' $0
  ${If} ${Errors}
  ${OrIf} $0 != 0
    MessageBox MB_OK|MB_ICONSTOP "$(RestoreFailed)" /SD IDOK
    SetErrorLevel 4
    Abort
  ${EndIf}
!endif
  ; Only installed files are removed; never recursively delete a chosen folder.
  ; Keep WinTray and the uninstaller until dependencies are removed so a
  ; blocked uninstall can be retried, including startup restoration.
  ClearErrors
  Delete "$INSTDIR\WinTray-Recovery.exe"
  Delete "$INSTDIR\WinTray.exe.manifest"
  Delete "$INSTDIR\checksums.txt"
  Delete "$INSTDIR\ThirdPartyNotices.txt"
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\native\x64\WinTray-Bridge.exe"
  Delete "$INSTDIR\native\x64\WinTray-ShellBridge.dll"
  Delete "$INSTDIR\native\x86\WinTray-Bridge.exe"
  Delete "$INSTDIR\native\x86\WinTray-ShellBridge.dll"
  ${If} ${Errors}
    MessageBox MB_OK|MB_ICONSTOP "$(RemoveFailed)" /SD IDOK
    SetErrorLevel 5
    Abort
  ${EndIf}
  Delete "$INSTDIR\WinTray.exe"
  ${If} ${Errors}
    MessageBox MB_OK|MB_ICONSTOP "$(RemoveFailed)" /SD IDOK
    SetErrorLevel 5
    Abort
  ${EndIf}
  RMDir "$INSTDIR\native\x64"
  RMDir "$INSTDIR\native\x86"
  RMDir "$INSTDIR\native"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
!ifdef WINTRAY_TEST_ROOT
  Delete "${WINTRAY_TEST_ROOT}\WinTray.lnk"
!else
  Delete "$SMPROGRAMS\WinTray\WinTray.lnk"
  Delete "$SMPROGRAMS\WinTray\Uninstall.lnk"
  RMDir "$SMPROGRAMS\WinTray"
!endif
  DeleteRegKey HKCU "${WINTRAY_REGISTRY_KEY}"
SectionEnd
