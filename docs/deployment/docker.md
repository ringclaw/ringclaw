---
title: Docker
---

# Docker

All RingClaw configuration lives in `~/.ringclaw/config.json` — mount the
directory into the container and the file is shared with the host. The
container uses `HOME=/home/ringclaw`, so the in-container config path is
`/home/ringclaw/.ringclaw/config.json`. The
previously supported `-e RC_*` / `-e OPENCLAW_GATEWAY_*` environment
variables are no longer honored.

## Build

```bash
docker build -t ringclaw .
```

## Prepare Config

On the host, run `ringclaw setup` (or edit manually) so that
`~/.ringclaw/config.json` exists. See
[Configuration](../guide/configuration.md) for the full schema.

## Run

```bash
docker run -d --name ringclaw \
  -v ~/.ringclaw:/home/ringclaw/.ringclaw \
  ringclaw
```

The container reads `/home/ringclaw/.ringclaw/config.json` on startup.

## View Logs

```bash
docker logs -f ringclaw
```

## Quick Start with GHCR

```bash
docker run -it -v ~/.ringclaw:/home/ringclaw/.ringclaw \
  ghcr.io/ringclaw/ringclaw start
```

For Kubernetes, prefer a pod-local writable volume such as `emptyDir` mounted
at `/home/ringclaw/.ringclaw` instead of sharing one writable directory across
multiple pods.

::: info Agent Binaries
ACP and CLI agents require the agent binary inside the container. The Docker image ships only RingClaw itself. For ACP/CLI agents, mount the binary or build a custom image. HTTP agents work out of the box.
:::
