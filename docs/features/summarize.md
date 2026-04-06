---
title: Chat Summarization
---

# Chat Summarization

Summarize conversations from any chat:

```
summarize my chat with John           # summarize today's chat with John
summarize my chat with Raye from Monday  # summarize since Monday
```

RingClaw resolves the target chat by name, fetches messages using the private app client, and sends the summary to the current chat via an AI agent.

## How It Works

1. RingClaw parses the target chat name and optional time range from your message
2. Uses the Private App client to search the company directory and find the matching chat
3. Fetches messages from the target chat within the specified time range
4. Sends the messages to the default AI agent for summarization
5. Posts the summary back to your current chat

## Group Summarize

By default, summarization is blocked in group chats (since the summary would be visible to all group members). You can enable it for a specific group:

```json
{
  "ringcentral": {
    "group_summary_group_id": "1234567",
    "group_summary_message_limit": 200
  }
}
```

| Field | Default | Description |
|-------|---------|-------------|
| `group_summary_group_id` | — | Only this exact group ID is allowed to use in-group summarize |
| `group_summary_message_limit` | `200` | When group summarize is enabled, pull this many recent messages before time filtering |

If `group_summary_group_id` is set, in-group summarize is enabled automatically. It is only allowed when the current group ID exactly matches that configured value.

## Security

::: warning
Summarization is blocked in group chats when using a bot client, since the summary would be visible to all group members. Use it in a direct message with the bot instead.
:::

- **Bot DM**: Summarize works — Private App reads target chat, Bot replies with summary
- **Group chat**: **Blocked** to prevent data leaks
- **Without Private App**: Summarize is **disabled** entirely — the bot cannot access other users' chats

## Requirements

Summarization requires a **Private App** to be configured. The Private App needs `ReadAccounts` permission to:

- Search the company directory to resolve chat names
- Read messages from other chats that the bot cannot access directly
