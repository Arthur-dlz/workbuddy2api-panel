@echo off
chcp 65001 >nul
title WorkBuddy Gateway (Console Mode)
echo =======================================================
echo   WorkBuddy 多账号池与自定义模型网关 (控制台模式)
echo   服务地址: http://127.0.0.1:9527/
echo   控制台面板: http://127.0.0.1:9527/panel/
echo   API 端口: 9527 (不与本地 WB 抢端口)
echo =======================================================
echo 正在启动...
workbuddy-gateway.exe
pause
