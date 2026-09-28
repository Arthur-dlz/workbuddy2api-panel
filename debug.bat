@echo off
chcp 65001 >nul
title WorkBuddy Gateway (控制台调试模式)
cd /d "%~dp0"

echo ========================================================
echo   WorkBuddy Gateway - 控制台调试模式
echo   网关接口: http://127.0.0.1:9527/
echo   管理面板: http://127.0.0.1:9527/panel/
echo   API 端口: 9527
echo ========================================================
echo.
echo 本窗口保持打开将实时输出请求与调度日志。
echo [提示] 如需静默托盘模式，请运行 start.bat
echo.

if exist "workbuddy-gateway.exe" (
    "%~dp0workbuddy-gateway.exe" -no-tray
) else (
    go run . -no-tray
)

pause
