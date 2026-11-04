!include "WinMessages.nsh"

; Run the PATH edit from the Windows-only helper shipped beside the app.
; Keeping PowerShell out of a nested NSIS command string avoids changing its
; variables or quotes while the installer is compiled.
!macro runTanGitPathScript action
  System::Call 'Kernel32::SetEnvironmentVariable(t "TANGIT_INSTALL_DIR", t "$INSTDIR")'
  nsExec::ExecToStack '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$INSTDIR\tangit-path.ps1" -Action ${action}'
  Pop $0
  Pop $1
  System::Call 'Kernel32::SetEnvironmentVariable(t "TANGIT_INSTALL_DIR", t "")'
  DetailPrint "Updated the user PATH for TanGit (${action}, PowerShell exit code $0)"
  SendMessage ${HWND_BROADCAST} ${WM_SETTINGCHANGE} 0 "STR:Environment" /TIMEOUT=5000
!macroend

; The terminal command is the installed TanGit.exe itself. Keep its folder on
; the current user's PATH without requiring an elevated, machine-wide install.
!macro customInstall
  !insertmacro runTanGitPathScript Add
!macroend

; Leave the rest of the user's PATH untouched. This also handles an uninstall
; after the app was installed into a custom directory.
!macro customUnInstall
  !insertmacro runTanGitPathScript Remove
!macroend
