---
title: Docker
---

# Docker

所有 RingClaw 配置都存放在 `~/.ringclaw/config.json` 中——将该目录挂载到容器内即可
与宿主机共享同一份配置文件。容器内固定使用 `HOME=/home/ringclaw`，因此配置路径是
`/home/ringclaw/.ringclaw/config.json`。此前使用的 `-e RC_*` /
`-e OPENCLAW_GATEWAY_*` 等环境变量已被废弃，容器内不再读取。

## 构建

```bash
docker build -t ringclaw .
```

## 准备配置

在宿主机上运行 `ringclaw setup`（或直接编辑文件）确保 `~/.ringclaw/config.json`
已存在。完整字段清单见[配置](../guide/configuration.md)。

## 启动

```bash
docker run -d --name ringclaw \
  -v ~/.ringclaw:/home/ringclaw/.ringclaw \
  ringclaw
```

容器启动时会读取 `/home/ringclaw/.ringclaw/config.json`。

## 查看日志

```bash
docker logs -f ringclaw
```

## 快速启动（GHCR）

```bash
docker run -it -v ~/.ringclaw:/home/ringclaw/.ringclaw \
  ghcr.io/ringclaw/ringclaw start
```

在 Kubernetes 里，建议把 `/home/ringclaw/.ringclaw` 挂到每个 Pod 独享的可写卷
（例如 `emptyDir`），而不是让多个 Pod 共享同一个可写目录。

::: info Agent 二进制文件
ACP 和 CLI 模式需要容器内有对应的 Agent 二进制文件。默认镜像只包含 RingClaw 本体。如需使用 ACP/CLI Agent，请挂载二进制文件或构建自定义镜像。HTTP 模式开箱即用。
:::
