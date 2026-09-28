@echo off
chcp 65001 >nul
title 停止 WorkBuddy Gateway
cd /d "%~dp0"

echo 正在停止 WorkBuddy Gateway 进程...
taskkill /F /IM workbuddy-gateway* >nul 2>&1

echo [完成] WorkBuddy 网关已退出。
ping 127.0.0.1 -n 2 >nul
exit /b 0
