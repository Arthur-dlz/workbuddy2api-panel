' WorkBuddy Gateway Silent Background Tray Launcher
Option Explicit
Dim ws, fso, scriptDir, exePath
Set ws = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")
scriptDir = fso.GetParentFolderName(WScript.ScriptFullName)
exePath = scriptDir & "\workbuddy-gateway-tray.exe"
If Not fso.FileExists(exePath) Then
    exePath = scriptDir & "\workbuddy-gateway.exe"
End If
ws.CurrentDirectory = scriptDir
ws.Run Chr(34) & exePath & Chr(34), 0, False
Set ws = Nothing
Set fso = Nothing
