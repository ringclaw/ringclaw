---
title: REST API
---

# REST API

RingClaw exposes an HTTP API on `127.0.0.1:18011` when `ringclaw start` is running.

## Authentication

All API requests (except `/health`) must include the `X-RingClaw-Token` header:

```bash
curl -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  http://127.0.0.1:18011/api/send \
  -d '{"text":"hello"}'
```

The token is auto-generated on first startup and stored in `~/.ringclaw/api_token`.

## Send Messages

### POST `/api/send`

Send text and/or media to a RingCentral chat.

**Send text (uses default chat):**

```bash
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  -d '{"text": "Hello from RingClaw"}'
```

**Send text to a specific chat:**

```bash
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  -d '{"to": "chatId", "text": "Hello"}'
```

**Send image:**

```bash
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  -d '{"media_url": "https://example.com/photo.png"}'
```

**Send text + media:**

```bash
curl -X POST http://127.0.0.1:18011/api/send \
  -H "Content-Type: application/json" \
  -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  -d '{"text": "See this", "media_url": "https://example.com/photo.png"}'
```

## Resource API Endpoints

### Tasks

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/tasks` | List tasks |
| `POST` | `/api/tasks` | Create task |
| `GET` | `/api/tasks/{id}` | Get task |
| `PATCH` | `/api/tasks/{id}` | Update task |
| `DELETE` | `/api/tasks/{id}` | Delete task |
| `POST` | `/api/tasks/{id}/complete` | Complete task |

### Notes

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/notes` | List notes |
| `POST` | `/api/notes` | Create note |
| `GET` | `/api/notes/{id}` | Get note |
| `PATCH` | `/api/notes/{id}` | Update note |
| `DELETE` | `/api/notes/{id}` | Delete note |

### Events

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/events` | List events |
| `POST` | `/api/events` | Create event |
| `GET` | `/api/events/{id}` | Get event |
| `PUT` | `/api/events/{id}` | Update event |
| `DELETE` | `/api/events/{id}` | Delete event |

### Cards

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/cards` | Create adaptive card |
| `GET` | `/api/cards/{id}` | Get card |
| `PUT` | `/api/cards/{id}` | Update card |
| `DELETE` | `/api/cards/{id}` | Delete card |
