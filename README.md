<div align="center">

# Onrix AI

**多厂商 AI API 网关与管理面板** · Multi-provider AI API gateway & admin panel

将多家上游 AI 服务聚合到一个 OpenAI 兼容 API 之后,统一发放密钥、限额、计费,并提供现代化管理后台。

</div>

---

## 简介 · Overview

Onrix AI 是一个自托管网关:把 OpenAI、Claude、Gemini 等多家上游模型统一到一个 **OpenAI / Anthropic / Gemini 兼容 API** 后面,附带用户体系、API Key、限流、额度与计费,以及一个现代化 Web 管理台。

Onrix AI is a self-hosted gateway that unifies multiple AI providers behind a single OpenAI-/Anthropic-/Gemini-compatible API, with user management, API keys, rate limiting, quota/billing, and a modern admin dashboard.

- **一个端点,多家上游** — 客户端只对接 Onrix AI 的 `/v1`,由它路由到你配置的渠道。
- **密钥 / 额度 / 计费** — 发放客户 API Key,统计用量,充值(Stripe / Creem / epay / Lemon Squeezy)。
- **现代化后台** — React 管理面板(登录保护)。
- **自托管** — 单容器 + SQLite(或 MySQL/PostgreSQL)+ 可选 Redis。

## 快速开始 · Quick start (Docker)

```bash
docker run -d --name onrix -p 3000:3000 -v "$PWD/data:/data" \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/applabs-dev/onrix:latest
```

打开 `http://localhost:3000` 走安装向导创建管理员,然后在后台新增渠道。客户端 API base URL:`http://<host>:3000/v1`。

多架构镜像(amd64 / arm64)发布于 `ghcr.io/applabs-dev/onrix`。

## 配置 · Configuration

运行时配置来自环境变量 + 后台「系统设置」。关键环境变量:

| 变量 | 用途 |
|------|------|
| `SESSION_SECRET` | 会话签名密钥(稳定登录 / 多实例必填) |
| `CRYPTO_SECRET` | 通用加密密钥(多实例需一致) |
| `SQL_DSN` | 数据库 DSN;留空 = SQLite |
| `REDIS_CONN_STRING` | Redis(可选;多实例共享缓存/限流时需要) |

## 许可与署名 · License & attribution

Onrix AI 是 **[New API](https://github.com/QuantumNous/new-api)**(作者 **QuantumNous and contributors**)的修改版,依 **GNU AGPL-3.0** 许可分发。Frontend design and development by New API contributors.

- 上游项目 / Upstream: https://github.com/QuantumNous/new-api
- 本修改版对应源码(AGPLv3 §13):https://github.com/applabs-dev/onrix
- 完整条款见 `LICENSE` 与 `NOTICE`。
