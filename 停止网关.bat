@echo off
title 停止 WorkBuddy 网关
cd /d "%~dp0"

echo 正在停止 WorkBuddy 网关进程...
taskkill /F /IM workbuddy-gateway* >nul 2>&1

echo [成功] WorkBuddy 网关已安全停止。
ping 127.0.0.1 -n 2 >nul
exit