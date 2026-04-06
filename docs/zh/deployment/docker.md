---
title: Docker
---

# Docker

## 构建

```bash
docker build -t ringclaw .
```

## 使用 Bot Token 启动

```bash
docker run -d --name ringclaw \
  -v ~/.ringclaw:/root/.ringclaw \
  -e RC_BOT_TOKEN=xxx \
  ringclaw
```

## 使用 Private App 和 HTTP Agent 启动

```bash
docker run -d --name ringclaw \
  -v ~/.ringclaw:/root/.ringclaw \
  -e RC_BOT_TOKEN=xxx \
  -e RC_CLIENT_ID=xxx \
  -e RC_CLIENT_SECRET=xxx \
  -e RC_JWT_TOKEN=xxx \
  -e OPENCLAW_GATEWAY_URL=https://api.example.com \
  -e OPENCLAW_GATEWAY_TOKEN=sk-xxx \
  ringclaw
```

## 查看日志

```bash
docker logs -f ringclaw
```

## 快速启动（GHCR）

```bash
docker run -it -v ~/.ringclaw:/root/.ringclaw \
  -e RC_BOT_TOKEN=xxx \
  ghcr.io/ringclaw/ringclaw start
```

::: info Agent 二进制文件
ACP 和 CLI 模式需要容器内有对应的 Agent 二进制文件。默认镜像只包含 RingClaw 本体。如需使用 ACP/CLI Agent，请挂载二进制文件或构建自定义镜像。HTTP 模式开箱即用。
:::
