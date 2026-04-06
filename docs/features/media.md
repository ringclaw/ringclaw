---
title: Media & Messaging
---

# Media Messages

RingClaw supports sending images, videos, and files to RingCentral chats.

**From agent replies:** When an AI agent returns markdown with images (`![](url)`), RingClaw automatically extracts the image URLs, downloads them, and uploads them to the chat via the RingCentral file upload API.

**Markdown support:** RingCentral Team Messaging natively supports markdown, so agent responses are sent as-is without conversion.

Supported media types: images (png, jpg, gif, webp), videos (mp4, mov), files (pdf, doc, zip, etc.).

## Proactive Messaging

Send messages to RingCentral chats without waiting for incoming messages.

### CLI

```bash
# Send text (uses default chat from config)
ringclaw send --text "Hello from RingClaw"

# Send text to a specific chat
ringclaw send --to "chatId" --text "Hello"

# Send image
ringclaw send --media "https://example.com/photo.png"

# Send text + image
ringclaw send --text "Check this out" --media "https://example.com/photo.png"

# Send file
ringclaw send --media "https://example.com/report.pdf"
```

### HTTP API

The HTTP API runs on `127.0.0.1:18011` when `ringclaw start` is running:

```bash
# Send text (uses default chat)
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"text": "Hello from RingClaw"}'

# Send text to a specific chat
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"to": "chatId", "text": "Hello"}'

# Send image
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"media_url": "https://example.com/photo.png"}'

# Send text + media
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -d '{"text": "See this", "media_url": "https://example.com/photo.png"}'
```

Set `RINGCLAW_API_ADDR` to change the listen address (e.g. `0.0.0.0:18011`).

## Resource APIs

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET/POST` | `/api/tasks` | List / create tasks |
| `GET/PATCH/DELETE` | `/api/tasks/{id}` | Get / update / delete task |
| `POST` | `/api/tasks/{id}/complete` | Complete a task |
| `GET/POST` | `/api/notes` | List / create notes |
| `GET/PATCH/DELETE` | `/api/notes/{id}` | Get / update / delete note |
| `GET/POST` | `/api/events` | List / create events |
| `GET/PUT/DELETE` | `/api/events/{id}` | Get / update / delete event |
| `POST` | `/api/cards` | Create adaptive card |
| `GET/PUT/DELETE` | `/api/cards/{id}` | Get / update / delete card |
