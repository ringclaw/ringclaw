---
title: REST API
---

# REST API

RingClaw 在 `ringclaw start` 运行时，默认在 `127.0.0.1:18011` 提供 HTTP API。

## 认证

除 `/health` 外，所有 API 请求必须携带 `X-RingClaw-Token` 请求头：

```bash
curl -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  http://127.0.0.1:18011/api/send \
  -d '{"text":"hello"}'
```

Token 在首次启动时自动生成，存储在 `~/.ringclaw/api_token`。

## 发送消息

### POST `/api/send`

向 RingCentral 聊天发送文本和/或媒体。

**发送文本（使用默认 Chat）：**

```bash
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  -d '{"text": "你好，来自 RingClaw"}'
```

**发送文本到指定 Chat：**

```bash
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  -d '{"to": "chatId", "text": "你好"}'
```

**发送图片：**

```bash
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  -d '{"media_url": "https://example.com/photo.png"}'
```

**发送文本 + 媒体：**

```bash
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  -d '{"text": "看看这个", "media_url": "https://example.com/photo.png"}'
```

## 资源 API 端点

### 任务

| 方法 | 端点 | 说明 |
|------|------|------|
| `GET` | `/api/tasks` | 列出任务 |
| `POST` | `/api/tasks` | 创建任务 |
| `GET` | `/api/tasks/{id}` | 获取任务 |
| `PATCH` | `/api/tasks/{id}` | 更新任务 |
| `DELETE` | `/api/tasks/{id}` | 删除任务 |
| `POST` | `/api/tasks/{id}/complete` | 完成任务 |

### 笔记

| 方法 | 端点 | 说明 |
|------|------|------|
| `GET` | `/api/notes` | 列出笔记 |
| `POST` | `/api/notes` | 创建笔记 |
| `GET` | `/api/notes/{id}` | 获取笔记 |
| `PATCH` | `/api/notes/{id}` | 更新笔记 |
| `DELETE` | `/api/notes/{id}` | 删除笔记 |

### 日历事件

| 方法 | 端点 | 说明 |
|------|------|------|
| `GET` | `/api/events` | 列出事件 |
| `POST` | `/api/events` | 创建事件 |
| `GET` | `/api/events/{id}` | 获取事件 |
| `PUT` | `/api/events/{id}` | 更新事件 |
| `DELETE` | `/api/events/{id}` | 删除事件 |

### 卡片

| 方法 | 端点 | 说明 |
|------|------|------|
| `POST` | `/api/cards` | 创建 Adaptive Card |
| `GET` | `/api/cards/{id}` | 获取卡片 |
| `PUT` | `/api/cards/{id}` | 更新卡片 |
| `DELETE` | `/api/cards/{id}` | 删除卡片 |
