---
title: AI-Driven Actions
---

# AI-Driven Actions

AI agents can automatically create notes, tasks, events, and adaptive cards during conversation. When a user's request implies creating these resources, the agent appends ACTION blocks to its response and RingClaw executes them via the RC API.

## How It Works

When an agent's response contains ACTION blocks, RingClaw parses them, sends the text reply, then executes each action against the RingCentral API:

```mermaid
sequenceDiagram
    participant AI as AI Agent
    participant R as RingClaw
    participant O as Owner DM
    participant RC as RingCentral

    AI-->>R: Reply (with ACTION blocks)
    R->>R: ParseAgentActions()
    R->>R: Separate text reply and ACTIONs
    R->>RC: Send text reply
    loop Each ACTION
        R->>R: Parse type, params, chatid
        alt chatid set & sender NOT on trusted allowlist & OOB configured
            R->>O: Rich challenge prompt<br/>action / requester / origin / target<br/>+ optional Title/Subject/Assignee<br/>+ body preview (200 chars)<br/>+ host approve/deny commands
            Note over O,R: Owner runs `ringclaw approval id` on host
            R->>R: Await terminal approval (async goroutine)
            alt Approved
                R->>RC: Execute action in target chat
                R->>RC: Notify origin chat
            else Denied / expired
                R->>RC: Notify origin chat
            end
        else chatid set & sender NOT on trusted allowlist & OOB not configured
            R->>R: Silently drop chatid, force origin chat (WARN log)
        end
        alt owner cross-chat (target ≠ origin & ≠ owner DM)
            R->>O: Synchronous audit notice<br/>[notice] TYPE by requester at ts: origin=... target=...
            O-->>R: ACK (within 5s) or timeout/error
            alt notice delivery failed
                R->>R: Refuse action (Refused cross-chat TYPE: ...)
            end
        end
        alt NOTE
            R->>RC: CreateNote + PublishNote
        else TASK
            R->>RC: CreateTask (optionally resolve assignee via Private App)
        else EVENT
            R->>RC: CreateEvent
        else CARD
            R->>RC: CreateAdaptiveCard
        else MESSAGE
            R->>R: Resolve chatid / person name
            R->>RC: SendPost
        end
    end
    R->>RC: Send ACTION execution results summary
```

## ACTION Block Format

```
ACTION:NOTE title=Meeting Summary
Key decisions from today's standup...
END_ACTION

ACTION:TASK subject=Update deployment scripts
END_ACTION

ACTION:EVENT title=Sprint Review start=2026-04-01T14:00:00Z end=2026-04-01T15:00:00Z
END_ACTION
```

Actions may target a different chat via the `chatid=<id>` parameter.

- **Non-owner senders**: when OOB is configured (Private App + owner DM resolved), a context-rich challenge prompt is posted to the owner DM (action type, requester label with email, origin / target chat names, optional `Title:` / `Subject:` / `Assignee:` lines, a body preview capped at 200 characters, effect description, and host approve / deny commands). The owner must run `ringclaw approval <id>` on the host machine to approve. On approval the action executes asynchronously in the target chat. Falls back to silent drop (forced to origin chat) when OOB is not configured. Example owner DM prompt:

  ```text
  Pending approval (challenge `def67890`).
  Action: Cross-chat NOTE
  Requester: Alice Cross <alice@example.com> (id=user-7)
  Origin chat: Engineering (id=origin-1)
  Target chat: Customer Support (id=target-9)
  Title: Quarterly review notes
  Body: Highlights for the next quarter ...

  Effect: bot will write a NOTE into the target chat on the requester's behalf.

  Run on the host:
    ringclaw approval def67890        (approve)
    ringclaw approval deny def67890   (deny)

  Expires in 5m.
  ```

- **Owner-initiated cross-chat**: passes a **synchronous fail-closed audit notice** through the owner DM before dispatching — see [Security › Layer 2](../security/index.md#layer-2-ai-driven-action-dispatch) for the full gating rules.

No configuration needed — the action prompt is injected automatically.

## Task Commands

```
/task create Fix login bug         # create a task
/task list                         # list tasks in this chat
/task complete <id>                # mark task done
/task get <id>                     # get task details
/task update <id> <key=value>      # update a task
/task delete <id>                  # delete a task
```

## Note Commands

```
/note create Meeting Notes | body  # create a note (auto-published)
/note list                         # list notes in this chat
/note get <id>                     # get note details
/note update <id> <key=value>      # update a note
/note lock <id>                    # lock for editing
/note unlock <id>                  # unlock
/note delete <id>                  # delete a note
```

## Event Commands

```
/event list                        # list calendar events
/event list <chatId>               # list events in a specific chat
/event create <title> <start> <end>  # create event
/event get <id>                    # get event details
/event update <id> <key=value>     # update an event
/event delete <id>                 # delete an event
```

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
