<p align="center">
  <img src="assets/logo/dalizi_logo.png" alt="WorkBuddy Gateway" width="130" style="border-radius: 16px;">
</p>

<h1 align="center">WorkBuddy Gateway</h1>

<p align="center">
  <b>A production-ready OpenAI-compatible API gateway for Tencent WorkBuddy (formerly CodeBuddy) · Featuring Web Dashboard & Native Windows Tray</b>
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

> **WorkBuddy Gateway** is a production-grade OpenAI-compatible reverse proxy gateway and multi-account operations hub.
> Originating from and significantly expanding the [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api) open-source ecosystem, it delivers a modern modular architecture: built-in Windows native system tray (pre-compiled & zero-dependency), an 8-view Web console, DeepSeek-style calendar usage analytics, real-time latency (TTFB) and throughput (tok/s) monitoring, resilient EOF connection self-healing, $\pi$-jittered scheduling, and pure API automation for 17 out of 18 official growth tasks.
> Fully licensed under MIT.

---

## 📖 Overview

**WorkBuddy Gateway** wraps Tencent WorkBuddy (formerly CodeBuddy, official domains `copilot.tencent.com` / `workbuddy.cn`) accounts into standard OpenAI-compatible `/v1/chat/completions` endpoints.

- **Universal Model Ecosystem**: Seamlessly access upstream models including DeepSeek-V3, DeepSeek-R1, DeepSeek-V4.1 series, GLM-4 / GLM-5.2, Kimi, and more, complete with multi-turn reasoning content streaming.
- **Drop-in Client Replacement**: Zero code modification needed for Claude Code, Cursor, NextChat, LibreChat, Cherry Studio, or any standard OpenAI SDK.
- **Out-of-the-Box Dual Deployment**: Run as a silent background Windows tray app (binaries pre-bundled in repo, double-click `start.bat`) or deploy on Linux servers via Docker / GHCR.
- **Enterprise-Grade Pooling & Routing**: Earliest-Expiration-First credit scheduling, dynamic model cost tiering, exponential backoff circuit breaking, and sticky session preservation.

---

## ✨ Key Features

| Feature | Description |
|---|---|
| 🪟 **Native Windows Tray** | Pre-bundled standalone binaries. Double-click `start.bat` to launch quietly in the system tray with no console window. Quick-access tray menu for panel, endpoints, and API keys. |
| 🖥️ **8 Dedicated Operations Views** | Full-featured Web dashboard: Account Pool, Usage Analytics, Package Breakdown, Task Center, Model Routing, API Tokens, Live Settings, and Streaming Logs. |
| ⚡ **Live Latency & Throughput** | Real-time monitoring of actual **TTFB latency** and **generation speed (tok/s)** per model directly in the usage table. |
| 📊 **DeepSeek Analytics & Cost Ledger** | Calendar slot token breakdown, prefix cache hit rate tracking, and an EMA cost ledger aligned with the official 0.014 CNY/credit subscription benchmark. |
| 🔒 **Privacy Masking & Account Aliases** | Automatic phone number masking for accounts, with custom nicknames editable directly within the dashboard. |
| 🌊 **Transient EOF Self-Healing** | Built-in transport retry and idle socket eviction specifically resolving APISIX/NGI gateway idle TCP disconnects (`list tasks: EOF`). |
| 🥧 **$\pi$-Jittered Polling** | Mathematical $\pi$-sequence pseudo-random offset dispersion for staggered background balance polling without herd effects. |
| ⏱️ **Sleep Clock Calibration** | Segmented sleep loop with wallclock checks preventing monotonicity timer freezes when Windows sleeps. |
| 🎯 **17/18 Quest Automation** | Pure API reverse-engineered automation for 17 out of 18 official growth tasks, including multi-account scanning, concurrent queue execution, and auto-claiming. |
| 🤖 **Automated Task Scheduler** | Daily check-in + streak bonus auto-exchange & lottery, pet expeditions (adopt, travel, claim), activity reporting, and token keep-alives. |
| 📋 **Check-in Done Tracking** | Visual "Done" feedback for today's check-in on the dashboard backed by persistent `lastCheckinDay` state. |
| 🛡️ **Fault Tolerance & Circuit Breaking** | Model-level 429 adaptive throttling, exponential backoff cooling, and automatic failover to alternative accounts. |
| 🧲 **Sticky Sessions** | Keeps the same conversation (`conversation_id`) bound to a single upstream account to prevent cross-account context corruption. |
| 💬 **Prompt & Telemetry Defense** | Cleanses outbound system fingerprints and offers customizable system prompt rewriting (`custom`, `append`, `passthrough`). |
| 💾 **Dual State Persistence** | Atomic local file state storage with optional asynchronous Upstash Redis mirroring. |

---

## 🚀 Quick Start

### Windows (Zero Dependencies · Recommended for Desktop)

The repository **already includes pre-compiled binaries** (`workbuddy-gateway.exe` and `workbuddy-gateway-tray.exe`). No Go compiler or Docker installation is required!

1. Clone or download this repository.
2. Double-click **`start.bat`**:
   - The gateway launches quietly in the background with a Windows taskbar tray icon.
   - Automatically opens the Web Console at `http://127.0.0.1:9527/panel/`.
   - On the first run, a recommended `config.json` with a high-entropy random API key is created automatically.
3. For live debugging with real-time log streaming, double-click **`debug.bat`**.
4. To gracefully stop the background gateway, double-click **`stop.bat`**.

---

### Docker Compose (Recommended for Linux / Server)

```bash
# 1. Clone
git clone https://github.com/linguo2625469/workbuddy2api-panel.git
cd workbuddy2api-panel

# 2. Prepare configuration
cp config.example.json config.json

# 3. Start containers
docker compose up -d --build

# 4. Access Web Console
# Open http://localhost:7863/panel/
```

---

### GHCR Container Image

```bash
mkdir -p auths data && cp config.example.json config.json

docker run -d --name workbuddy2api \
  -p 7863:7863 -e TZ=Asia/Shanghai \
  -v ./auths:/app/auths -v ./data:/app/data -v ./config.json:/app/config.json \
  ghcr.io/linguo2625469/workbuddy2api-panel:latest
```

---

### Build from Source

Requirements: **Go ≥ 1.22**

```bash
# Build standard CLI binary
go build -ldflags="-s -w" -o workbuddy-gateway.exe ./main.go

# Build silent Windows tray binary
go build -ldflags="-s -w -H windowsgui" -o workbuddy-gateway-tray.exe ./main.go
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
    "deepseek-chat": "deepseek-v4.1-pro",
    "deepseek-reasoner": "deepseek-v4.1-pro"
  }
}
```

---

## ⚖️ Credits & License

This project is licensed under the [MIT License](LICENSE).

- Upstream gateway protocols and initial reverse engineering by [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api).
- Dashboard, Windows tray architecture, automated schedulers, and analytics engine developed and maintained by [Arthur-dlz](https://github.com/Arthur-dlz/workbuddy2api-panel).

### Disclaimer
This project is for educational and research purposes only. Users must comply with Tencent WorkBuddy (formerly CodeBuddy) Terms of Service and assume all operational risks.
