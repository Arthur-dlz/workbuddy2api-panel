@echo off
title WorkBuddy Gateway (控制台调试模式)
cd /d "%~dp0"

echo ========================================================
echo   WorkBuddy 账号池网关 - 控制台调试模式
echo   服务地址: http://127.0.0.1:9527/
echo   管理面板: http://127.0.0.1:9527/panel/
echo   API 端口: 9527
echo ========================================================
echo.
echo 正在启动控制台模式，实时日志如下：
echo [提示] 若需后台静默常驻，请双击运行 [启动网关.bat]
echo.

if exist "workbuddy-gateway.exe" (
    "%~dp0workbuddy-gateway.exe" -no-tray
) else (
    go run . -no-tray
)

pause