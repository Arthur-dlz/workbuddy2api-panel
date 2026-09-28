<p align="center">
  <img src="https://raw.githubusercontent.com/DGZSbot/ai-icon/refs/heads/main/WorkBuddy.png" alt="WorkBuddy Gateway" width="120">
</p>

<h1 align="center">WorkBuddy Gateway</h1>

<p align="center">
  <b>Multi-account OpenAI-compatible API gateway for Tencent CodeBuddy accounts · Featuring Web Dashboard & Windows System Tray</b>
</p>

<p align="center">
  <a href="README.md">简体中文</a> | <a href="README_EN.md">English</a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22.5-00ADD8?logo=go&logoColor=white&style=flat-square">
  <img alt="API" src="https://img.shields.io/badge/API-OpenAI_Compatible-412991?style=flat-square">
  <img alt="Deploy" src="https://img.shields.io/badge/Deploy-Single_Binary%20%7C%20Docker-2496ED?style=flat-square">
  <img alt="Transport" src="https://img.shields.io/badge/Transport-SSE%20%2F%20Streaming-0DBD8B?style=flat-square">
  <img alt="License" src="https://img.shields.io/badge/License-MIT-blue?style=flat-square">
</p>

---

> **Note**: This project is an enhanced distribution based on [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api).
> It extends the upstream gateway with a modern Web management dashboard, Windows system tray integration, automated task schedulers, and detailed token analytics.

## 📖 Overview

**WorkBuddy Gateway** is a self-hosted **OpenAI-compatible reverse proxy gateway** that wraps Tencent CodeBuddy accounts (`copilot.tencent.com`) into standard `/v1/chat/completions` endpoints.

- **Drop-in Replacement**: Seamlessly integrate with NextChat, LibreChat, Cherry Studio, Cursor, Claude Code, and any OpenAI SDK without code modification.
- **Multi-Account Pooling**: Automatic account rotation (Earliest-Expiration-First, weighted selection), exponential backoff circuit breakers, and sticky sessions for multi-turn conversations.
- **Silent Background Execution**: Runs quietly in the Windows system tray with zero disturbing console windows.

---

## ✨ Key Features

| Feature | Description |
|---|---|
| 🖥️ **Web Dashboard** | Built-in graphite dark/light theme dashboard for real-time traffic inspection, hot-reloading configurations, and visual account management. |
| 🪟 **Native Windows Tray** | Run silently in the background with a system tray icon. Right-click to open panel, copy endpoints, or grab API keys. |
| 🤖 **Automated Quest Manager** | Automatic pet expeditions (adopt, travel, claim loot), daily active streak checks, and background token keep-alives. |
| 📊 **Token & Cost Analytics** | Dual-dimension heatmap & calendar breakdown for token consumption, aligned with official credit calculations. |
| 🛡️ **Fault Tolerance & Circuit Breaking** | Model-level 429 adaptive throttling, exponential backoff cooling, automatic retry on alternative accounts. |
| 🧲 **Sticky Sessions** | Keeps the same conversation (`conversation_id`) bound to a single upstream account to prevent cross-account context corruption. |
| 💬 **Prompt & System Defense** | Cleanses suspicious system telemetry and replaces outbound fingerprints to ensure high delivery reliability. |

---

## 🚀 Quick Start

### Windows (Pre-built Binaries)

1. Download the latest release package.
2. Double-click **`start.bat`**:
   - The gateway will automatically start in the background (Windows System Tray).
   - Your browser will open the Web Dashboard at `http://127.0.0.1:9527/panel/`.
   - On the first run, a recommended `config.json` with a random API key will be generated automatically.
3. For debugging with live console output, double-click **`debug.bat`**.
4. To gracefully stop the gateway, double-click **`stop.bat`**.

### Build from Source

Requirements: **Go 1.22+**

```bash
# Clone the repository
git clone https://github.com/Arthur-dlz/workbuddy2api-panel.git
cd workbuddy2api-panel

# Run tests
go test ./...

# Build standard CLI binary
go build -ldflags="-s -w" -o workbuddy-gateway.exe main.go

# Build silent Windows tray binary
go build -ldflags="-s -w -H windowsgui" -o workbuddy-gateway-tray.exe main.go
```

---

## ⚙️ Configuration

Configuration is managed via `config.json` (see `config.example.json` for reference). Key settings can be modified live through the Web Panel:

```json
{
  "port": 9527,
  "listen": ":9527",
  "api_key": "sk-your-custom-gateway-key",
  "models": {
    "deepseek-chat": "deepseek-v4-pro",
    "deepseek-reasoner": "deepseek-v4-pro"
  }
}
```

---

## ⚖️ Credits & License

This project is licensed under the [MIT License](LICENSE).

- Upstream gateway protocols and reverse engineering by [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api).
- Dashboard, Windows tray architecture, automated schedulers, and analytics engine developed and maintained by [Arthur-dlz](https://github.com/Arthur-dlz/workbuddy2api-panel).

### Disclaimer
This project is for educational and research purposes only. Users must comply with Tencent CodeBuddy's Terms of Service and assume all operational risks.
