@echo off
cd /d "%~dp0"
if exist "workbuddy-gateway-tray.exe" (
    start "" "%~dp0workbuddy-gateway-tray.exe"
) else (
    start "" "%~dp0workbuddy-gateway.exe"
)
