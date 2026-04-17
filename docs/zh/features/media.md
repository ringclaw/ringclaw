---
title: 媒体与推送
---

# 富媒体消息

RingClaw 支持向 RingCentral 聊天发送图片、视频和文件。

**Agent 回复自动处理：** 当 AI Agent 返回包含图片的 markdown（`![](url)`）时，RingClaw 会自动提取图片 URL，下载文件，通过 RingCentral 文件上传 API 发送到聊天。

**Markdown 支持：** RingCentral Team Messaging 原生支持 Markdown，Agent 的回复无需转换直接发送。

支持的媒体类型：图片（png、jpg、gif、webp）、视频（mp4、mov）、文件（pdf、doc、zip 等）。

## 主动推送消息

无需等待用户发消息，主动向 RingCentral 聊天推送消息。

### 命令行

```bash
# 发送文本（使用配置中的默认 Chat）
ringclaw send --text "你好，来自 RingClaw"

# 发送文本到指定 Chat
ringclaw send --to "chatId" --text "你好"

# 发送图片
ringclaw send --media "https://example.com/photo.png"

# 发送文本 + 图片
ringclaw send --text "看看这个" --media "https://example.com/photo.png"

# 发送文件
ringclaw send --media "https://example.com/report.pdf"
```

### HTTP API

`ringclaw start` 运行时，默认监听 `127.0.0.1:18011`：

```bash
# 发送文本（使用默认 Chat）
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"text": "你好，来自 RingClaw"}'

# 发送文本到指定 Chat
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"to": "chatId", "text": "你好"}'

# 发送图片
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"media_url": "https://example.com/photo.png"}'

# 发送文本 + 媒体
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"text": "看看这个", "media_url": "https://example.com/photo.png"}'
```

在 `~/.ringclaw/config.json` 中设置 `api_addr` 可更改监听地址（如 `0.0.0.0:18011`）。

## 资源 API

| 方法 | 端点 | 说明 |
|------|------|------|
| `GET/POST` | `/api/tasks` | 列出 / 创建任务 |
| `GET/PATCH/DELETE` | `/api/tasks/{id}` | 查看 / 更新 / 删除任务 |
| `POST` | `/api/tasks/{id}/complete` | 完成任务 |
| `GET/POST` | `/api/notes` | 列出 / 创建笔记 |
| `GET/PATCH/DELETE` | `/api/notes/{id}` | 查看 / 更新 / 删除笔记 |
| `GET/POST` | `/api/events` | 列出 / 创建事件 |
| `GET/PUT/DELETE` | `/api/events/{id}` | 查看 / 更新 / 删除事件 |
| `POST` | `/api/cards` | 创建 Adaptive Card |
| `GET/PUT/DELETE` | `/api/cards/{id}` | 查看 / 更新 / 删除卡片 |
