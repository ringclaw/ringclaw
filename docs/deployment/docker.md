---
title: Docker
---

# Docker

## Build

```bash
docker build -t ringclaw .
```

## Run with Bot Token

```bash
docker run -d --name ringclaw \
  -v ~/.ringclaw:/root/.ringclaw \
  -e RC_BOT_TOKEN=xxx \
  ringclaw
```

## Run with Private App and HTTP Agent

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

## View Logs

```bash
docker logs -f ringclaw
```

## Quick Start with GHCR

```bash
docker run -it -v ~/.ringclaw:/root/.ringclaw \
  -e RC_BOT_TOKEN=xxx \
  ghcr.io/ringclaw/ringclaw start
```

::: info Agent Binaries
ACP and CLI agents require the agent binary inside the container. The Docker image ships only RingClaw itself. For ACP/CLI agents, mount the binary or build a custom image. HTTP agents work out of the box.
:::
