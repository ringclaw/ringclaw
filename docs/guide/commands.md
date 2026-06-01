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
| `/video list\|create\|get\|delete` | Create and manage RingCentral Video bridges |
| `/phone ringout\|status\|cancel\|calllog\|missed` | Phone actions and call logs; RingOut is owner-only |
| `/chatinfo [chatId]` | Show chat details (name, type, members) |
| `summarize my chat with John` | Summarize a conversation |
| `/cron list\|add\|delete\|enable\|disable` | Manage scheduled tasks |
| `/mem add [user\|chat\|global] <text>` | Append to persona memory (default: current chat). See [Configuration › persona](./configuration.md#persona). |
| `/mem show [user\|chat\|global]` | Show stored memory for a scope |
| `/mem del [scope]` / `/mem del [scope] confirm` | Clear a memory scope (needs the second `confirm` step) |
| `/persona` | Show the current SOUL.md and where to edit it |
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
| `/ag` | augment |

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
/video create Design Review        # create a Video bridge and return join URL
/phone calllog direction=Outbound  # list recent extension call logs (owner only)
/phone missed                      # list missed inbound calls (owner only)
```

Each command supports: `list`, `create`, `get`, `update`, `delete`. Tasks also support `complete`.

The same Video and Phone capabilities are available through natural language when a default agent is configured. Example prompts:

```text
Tell me what important meetings I have today.
Show my recent meeting list.
Create a video meeting for release planning.

Check today's calls and tell me if I have missing calls. Summarize next actions.
Show today's call summary and next actions.
Call +12123753080.
```

These route through intent classification and agent ACTION blocks (`ACTION:VIDEO`, `ACTION:VIDEO_LIST`, `ACTION:PHONE_CALL`, `ACTION:PHONE_CALLLOG`) rather than command-only shortcuts. Legacy `ACTION:RINGOUT` is accepted as a compatibility alias for the FIJI client make-call bridge.

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
├── app-url                            # pre-filled Developer Console app URLs
├── onboard                            # API-assisted non-interactive credential/K8S onboarding
├── runtime start                      # claim AVA Control Plane config and run managed bot runtime
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
├── video                              # RingCentral Video operations
│   ├── list                           # list Video bridges
│   ├── create <title> [--type X]     # create a bridge
│   ├── get <bridgeId>                # get bridge details
│   └── delete <bridgeId>             # delete bridge
│
├── phone                              # RingCentral Phone operations
│   ├── ringout <to> [--from X]       # start two-legged RingOut
│   ├── status <ringOutId>            # get RingOut status
│   ├── cancel <ringOutId>            # cancel connecting RingOut
│   ├── calllog [--limit N]           # list extension call logs
│   └── missed [limit=N]              # list missed inbound calls
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

# Create a RingCentral Video bridge
ringclaw video create "Design Review" --type Scheduled

# Record Video/Phone capability requirements during onboarding
ringclaw onboard --from-env --capability video --capability phone

# Render long-lived Kubernetes pods from a multi-bot manifest
ringclaw onboard --manifest bots.json --output-dir ./rendered-bots --skip-validate \
  --k8s --k8s-namespace personal-ava
kubectl apply -f ./rendered-bots/personal-ava-summer/k8s.yaml

# Start RingOut (requires RingOut scope and owner permissions)
ringclaw phone ringout +14155550199 --caller-id +14155550100

# Search company directory
ringclaw user search "Alice"

# Upload a file
ringclaw file upload 123456 ./report.pdf
```
