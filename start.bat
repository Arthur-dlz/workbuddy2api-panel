@echo off
chcp 65001 >nul
title WorkBuddy Gateway
cd /d "%~dp0"

tasklist | findstr /I "workbuddy-gateway" >nul 2>&1
if "%ERRORLEVEL%"=="0" (
    echo [提示] WorkBuddy Gateway 已在后台运行中。
    echo 正在为您打开控制面板...
    start http://127.0.0.1:9527/panel/
    ping 127.0.0.1 -n 2 >nul
    exit /b 0
)

echo 正在后台静默启动 WorkBuddy Gateway (托盘模式)...
if exist "workbuddy-gateway-tray.exe" (
    start "" "%~dp0workbuddy-gateway-tray.exe"
) else (
    start "" "%~dp0workbuddy-gateway.exe"
)

ping 127.0.0.1 -n 2 >nul
echo 正在打开控制面板...
start http://127.0.0.1:9527/panel/
exit /b 0
