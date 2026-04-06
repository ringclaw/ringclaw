---
title: Chat Commands
---

# Chat Commands

Send these as messages in your RingCentral chat:

| Command | Description |
|---------|-------------|
| `hello` | Send to default agent |
| `/codex write a function` | Send to a specific agent |
| `/cc explain this code` | Send to agent by alias |
| `/cc /cx explain this` | Broadcast to multiple agents in parallel |
| `/claude` | Switch default agent to Claude |
| `/new` or `/clear` | Reset current agent session |
| `/cwd /path/to/project` | Switch workspace directory for all agents |
| `/task list\|create\|get\|update\|delete\|complete` | Manage tasks |
| `/note list\|create\|get\|update\|delete\|lock\|unlock` | Manage notes |
| `/event list [chatId]\|create\|get\|update\|delete` | Manage calendar events |
| `/card get\|delete` | Manage adaptive cards |
| `/chatinfo [chatId]` | Show chat details (name, type, members) |
| `summarize my chat with John` | Summarize a conversation |
| `/cron list\|add\|delete\|enable\|disable` | Manage scheduled tasks |
| `/info` | Show current agent info (alias: `/status`) |
| `/help` | Show help message |

Unknown `/commands` (e.g. `/status`, `/compact`) are forwarded to the default agent, so agent-specific slash commands work transparently.

## Aliases

| Alias | Agent |
|-------|-------|
| `/cc` | claude |
| `/cx` | codex |
| `/cs` | cursor |
| `/km` | kimi |
| `/gm` | gemini |
| `/ocd` | opencode |
| `/oc` | openclaw |
| `/pi` | pi |
| `/cp` | copilot |
| `/dr` | droid |
| `/if` | iflow |
| `/kr` | kiro |
| `/qw` | qwen |

Switching default agent is persisted to config — survives restarts.

## Multi-Agent Broadcast

Send the same message to multiple agents in parallel:

```
/cc /cx review this function     # broadcast to Claude and Codex in parallel
```

Each agent replies in a separate message prefixed with `[agent-name]`.

## Session Management

| Command | Description |
|---------|-------------|
| `/new`   | Reset the default agent's session and start fresh |
| `/clear` | Same as `/new` |

::: info Dify Note
For Dify agents, `/new` and `/clear` also call `DELETE /v1/conversations/{id}` on the Dify server to wipe history on both sides. Use HTTPS endpoints to avoid nginx 301 redirect issues.
:::

## Dynamic Workspace

```bash
/cwd ~/projects/my-app    # switch all agents to this directory
/cwd                       # show current workspace info
```

Tilde (`~`) is expanded to the home directory. The new working directory applies to all running agents immediately.

## Tasks, Notes & Calendar Events

Full CRUD for RingCentral Team Messaging resources directly from chat:

```
/task create Fix login bug         # create a task
/task list                         # list tasks in this chat
/task complete <id>                # mark task done
/note create Meeting Notes | body  # create a note (auto-published)
/event list                        # list calendar events
```

Each command supports: `list`, `create`, `get`, `update`, `delete`. Tasks also support `complete`.

## Adaptive Cards

AI agents can generate [Adaptive Cards](https://adaptivecards.io/) for rich structured display (progress reports, dashboards, forms, etc.). When the agent includes an `ACTION:CARD` block in its response, RingClaw automatically posts the card to the chat:

```
ACTION:CARD
{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Sprint Status","weight":"bolder"},{"type":"FactSet","facts":[{"title":"Completed","value":"12"},{"title":"Remaining","value":"3"}]}]}
END_ACTION
```

Manage cards via chat commands:

```
/card get <id>       # view card details
/card delete <id>    # delete a card
```

## CLI Command Map

RingClaw provides a full CLI for interacting with RingCentral Team Messaging without the bridge running. All commands support `--json` for machine-readable output.

```
ringclaw
├── start [-f] [--api-addr]           # start bridge
├── stop                               # stop background process
├── restart                            # restart
├── status                             # check if running
├── setup                              # interactive credential wizard
├── update [--channel beta|alpha] [--branch X]  # self-update
├── upgrade / version                  # aliases
│
├── message                            # message operations
│   ├── send <chatId> <text>           # send a message
│   ├── get  <chatId> <postId>        # fetch a single message
│   ├── list <chatId> [--count N]     # list recent messages
│   ├── edit <chatId> <postId> <text> # edit a message
│   └── delete <chatId> <postId>      # delete a message
│
├── chat                               # chat operations
│   ├── list [--type X] [--recent]    # list chats
│   └── get <chatId>                  # get chat details
│
├── task                               # task operations
│   ├── list <chatId>                 # list tasks
│   ├── create <chatId> <subject>     # create task
│   ├── get <taskId>                  # get task details
│   ├── update <taskId> <key=value>   # update task
│   ├── complete <taskId>             # mark complete
│   └── delete <taskId>              # delete task
│
├── note                               # note operations
│   ├── list <chatId>                 # list notes
│   ├── create <chatId> <title> [body] # create + auto-publish
│   ├── get <noteId>                  # get note
│   ├── update <noteId> <key=value>   # update note
│   ├── lock <noteId>                 # lock for editing
│   ├── unlock <noteId>              # unlock
│   └── delete <noteId>              # delete
│
├── event                              # event operations
│   ├── list [chatId]                 # list events
│   ├── create <title> <start> <end>  # create event
│   ├── get <eventId>                 # get event
│   ├── update <eventId> <key=value>  # update event
│   └── delete <eventId>             # delete event
│
├── card                               # adaptive card operations
│   ├── create <chatId> <json-file>   # create from JSON file
│   ├── get <cardId>                  # get card
│   └── delete <cardId>              # delete card
│
├── user                               # user operations
│   ├── search <query>                # search company directory
│   └── get <personId>               # get person info
│
└── file                               # file operations
    └── upload <chatId> <file-path>   # upload local file
```

**Examples:**

```bash
# List chats sorted by recent activity
ringclaw chat list --recent --json

# Send a message
ringclaw message send 123456 "Hello from CLI"

# List tasks in a chat
ringclaw task list 123456

# Search company directory
ringclaw user search "Alice"

# Upload a file
ringclaw file upload 123456 ./report.pdf
```
