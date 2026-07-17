<div align="center">

# Onrix AI

**One AI gateway for every model — aggregate providers, resell access, manage keys, quota & billing.**

一个 AI API 网关与管理面板:把多家上游模型聚合到统一的兼容 API 之后,发放密钥、限额、计费。

![License](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)
![Container](https://img.shields.io/badge/ghcr.io-applabs--dev%2Fonrix-2496ED?logo=docker&logoColor=white)
![Arch](https://img.shields.io/badge/arch-amd64%20%7C%20arm64-555)
![Backend](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)
![Frontend](https://img.shields.io/badge/React%2019-20232A?logo=react&logoColor=61DAFB)
![DB](https://img.shields.io/badge/DB-SQLite%20%2F%20MySQL%20%2F%20PostgreSQL-003B57?logo=sqlite&logoColor=white)

</div>

---

## Overview · 简介

**Onrix AI** is a self-hosted **AI API gateway and management panel**. It puts many upstream AI
providers behind a single **OpenAI‑, Anthropic‑, and Gemini‑compatible API**, and adds everything
you need to run it as a service: users, API keys, quota, rate limiting, billing/top‑up, usage logs
and a modern web dashboard.

Point any OpenAI / Anthropic / Gemini SDK at Onrix AI's `/v1` and it routes the request to whichever
**channel** you configure — a direct provider API key, a relay endpoint, or
**[apiforge](https://github.com/DevEloLin/apiforge)**, which turns your existing AI *subscriptions*
into standard APIs (see [Turn subscriptions into API](#-turn-subscriptions-into-api--apiforge)).

一句话:**客户端只对接一个 `/v1`,Onrix AI 负责路由、发 key、限额、计费;上游可以是官方 API key、中转站,或用 apiforge 把订阅额度变成标准 API。**

## ✨ Highlights

- **One endpoint, every model** — a unified OpenAI/Anthropic/Gemini‑compatible API in front of many providers.
- **Turn subscriptions into API** — pair with **apiforge** to reuse Codex / Claude / Copilot / Cursor / Qwen … CLI logins as standard APIs.
- **Full commercial layer** — users, API keys, quotas, per‑key rate limits, usage logs, and top‑up via **Stripe / Creem / epay / Lemon Squeezy** + redemption codes.
- **Modern admin panel** — React 19 dashboard, dark mode, i18n; login‑gated and `noindex`.
- **Self‑hosted & light** — single container, **SQLite** by default (MySQL / PostgreSQL supported), Redis optional; **multi‑arch (amd64 / arm64)**.

## 🧩 Features

| Area | What you get |
|------|--------------|
| **Gateway / relay** | OpenAI `/v1/chat/completions`, `/v1/responses`; Anthropic `/v1/messages`; Gemini `/v1beta/*`; SSE streaming; image generation |
| **Channels** | Many providers; **multi‑account channel pools** with automatic failover & load balancing; model mapping |
| **Commerce** | Client API keys/tokens · quota & usage tracking · top‑up (Stripe / Creem / epay / Lemon Squeezy) · redemption codes · per‑key RPM limits |
| **Accounts & auth** | Username/password, email verification, OAuth (GitHub / Discord / OIDC / LinuxDo / Telegram / WeChat), Passkey/WebAuthn, 2FA/TOTP |
| **Ops** | Admin dashboard · audit logs · multi‑node ready (Redis) · i18n (en / zh / …) |
| **Data** | SQLite · MySQL ≥ 5.7.8 · PostgreSQL ≥ 9.6 |

## 🏗️ Architecture

```
              your customers / apps  (OpenAI · Anthropic · Gemini SDK)
                              │   https://api.<your-domain>/v1
                              ▼
              ┌───────────────────────────────────────┐
              │               Onrix AI                 │
              │  compatible API · panel · keys ·       │
              │  quota · rate limit · billing          │
              └───────────────────┬───────────────────┘
                                  │  channels
        ┌─────────────────────────┼─────────────────────────┐
        ▼                         ▼                          ▼
   apiforge                 direct provider            relay / 中转站
 (subscription CLI          API keys (OpenAI,          endpoints
  logins → std API)         DeepSeek, GLM, Qwen …)
```

## 🔑 Turn subscriptions into API · apiforge

Many AI products (Codex/OpenAI, Claude, GitHub Copilot, Cursor, Qwen …) are sold as
**subscriptions with a CLI login**, not as a metered API key. **apiforge** reuses those local
CLI logins and exposes them as **official‑compatible APIs** (real endpoints, model lists, key
auth), with a built‑in **multi‑account pool + automatic/manual switching**. It can also mix in
API‑key vendors (DeepSeek, Kimi, GLM, Qwen, MiniMax, Doubao, Hunyuan …).

Add apiforge as a **channel** inside Onrix AI (base URL `http://<apiforge-host>:8899`) → your
subscription quota becomes a standard API that Onrix AI can key, meter, rate‑limit and bill.

- **apiforge source · 源码:** https://github.com/DevEloLin/apiforge
- Ships as a Docker image; mount your host's CLI credentials and `docker compose up`.

> ⚠️ **Compliance / 合规提示:** reusing a subscription CLI's OAuth token against official APIs is a
> gray area under each vendor's Terms of Service; vendors run server‑side checks and may rate‑limit
> or ban. Use it only to reuse **your own** logins, at your own risk.

## 🚀 Quick start (Docker)

```bash
docker run -d --name onrix -p 3000:3000 \
  -v "$PWD/data:/data" \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/applabs-dev/onrix:latest
```

Open `http://localhost:3000`, finish the setup wizard (create the admin), then add a channel.
Client API base URL: `http://<host>:3000/v1`.

Multi‑arch images are published to **`ghcr.io/applabs-dev/onrix`** (amd64 / arm64).

## ⚙️ Configuration

Runtime config comes from environment variables + the in‑app **System Settings** (channels,
payments, OAuth, SMTP are configured in the panel and stored in the DB).

| Env | Purpose |
|-----|---------|
| `SESSION_SECRET` | Session signing secret — **required** for stable login / multi‑node |
| `CRYPTO_SECRET` | General encryption key (must match across instances) |
| `SQL_DSN` | Database DSN; **empty = SQLite** (`/data`) |
| `REDIS_CONN_STRING` | Redis (optional; needed for multi‑instance shared cache/rate‑limit) |
| `TZ` | Time zone, e.g. `Asia/Shanghai` |

> Set the panel's **Server Address** to your public URL (e.g. `https://api.<your-domain>`) — OAuth
> callbacks and verification emails are derived from it.

## 🆚 What's different from upstream

Onrix AI is built on top of **New API** with:

- Rebrand to **Onrix AI** and the modern React 19 frontend as the **default** theme.
- **Lemon Squeezy** top‑up channel added (alongside Stripe / Creem / epay).
- **Multi‑arch GHCR CI** and a Docker + **Cloudflare Tunnel** deployment path.
- Upstream attribution preserved — see [License](#-license--attribution).

## 📦 Deployment

Onrix AI is a single Go binary that serves **both** the panel (`/`) and the API (`/v1`) on one
port (`3000`). Run the container, then front it with a reverse proxy or **Cloudflare** (a
**Cloudflare Tunnel** is recommended for home/NAT/Raspberry‑Pi hosting — no port forwarding, TLS at
the edge). Keep any public marketing/landing site on a separate host; the panel itself is `noindex`.

## 📄 License & attribution

Onrix AI is a **modified version of [New API](https://github.com/QuantumNous/new-api)** by
**QuantumNous and contributors**, distributed under the **GNU AGPL‑3.0** license. Frontend design and
development by New API contributors.

- Upstream project · 上游: https://github.com/QuantumNous/new-api
- Corresponding source of this modified version (AGPLv3 §13): https://github.com/applabs-dev/onrix
- Full terms in [`LICENSE`](./LICENSE) and [`NOTICE`](./NOTICE).
