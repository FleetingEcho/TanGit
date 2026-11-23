!include "WinMessages.nsh"

; Run the PATH edit from the Windows-only helper shipped beside the app.
; Keeping PowerShell out of a nested NSIS command string avoids changing its
; variables or quotes while the installer is compiled.
!macro runLazygitGoPathScript action
  System::Call 'Kernel32::SetEnvironmentVariable(t "LAZYGITGO_INSTALL_DIR", t "$INSTDIR")'
  nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\lazygitgo-path.ps1" -Action ${action}'
  Pop $0
  Pop $1
  System::Call 'Kernel32::SetEnvironmentVariable(t "LAZYGITGO_INSTALL_DIR", t "")'
  DetailPrint "Updated the user PATH for lazygitGo (${action}, PowerShell exit code $0)"
  SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
!macroend

; The terminal command is the installed lazygitGo.exe itself. Keep its folder on
; the current user's PATH without requiring an elevated, machine-wide install.
!macro customInstall
  !insertmacro runLazygitGoPathScript Add
  Delete "$SMPROGRAMS\TanGit.lnk"
  IfFileExists "$DESKTOP\TanGit.lnk" 0 +3
  Delete "$DESKTOP\TanGit.lnk"
  Call CreateDesktopShortcut
!macroend

; Leave the rest of the user's PATH untouched. This also handles an uninstall
; after the app was installed into a custom directory.
!macro customUnInstall
  !insertmacro runLazygitGoPathScript Remove
!macroend
