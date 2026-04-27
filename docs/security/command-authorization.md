---
title: Command Authorization
---

# Command Authorization (Layer 1)

Layer 1 controls who may trigger each slash command in each chat
shape. It runs **after** the [Sender Allowlist](./sender-allowlist)
admits the message — so every cell in the table below already
assumes the sender is on Layer 0.

See [Permission Matrix](./index#permission-matrix) for how Layer 1
relates to the other layers.

## Per-command authorization table

Column legend: ✅ allowed; ❌ blocked (bot replies with an explicit
refusal or silently drops); ⚠️ allowed with an extra check.

"Owner" means the Private App owner when one is configured (the
true machine operator). The "Bot DM (other trusted sender)" column
only applies when `ringcentral.source_user_ids` lists more than one
person; see the "DM is the trust boundary" warning on the
[security overview](./index#permission-matrix).

| Command / Message Shape | Bot DM (owner) | Bot DM (other trusted sender, Private App configured) | Bot Group (owner) | Bot Group (others) | Gate |
|---|---|---|---|---|---|
| Plain text with no `/` prefix (→ default agent) | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/help` | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/info` / `/status` | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/chatinfo [id]` | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/task` / `/note` / `/event` / `/card` | ✅ | ✅ | ✅ | ✅ | `actions_commands.go` |
| `/<agent> <msg>` (send / broadcast) | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/<agent>` (switch default agent) | ✅ | ✅ | ✅ | ❌ | `handler.go` |
| `/new` / `/clear` | ✅ | ❌ | ✅ | ❌ | `handler.go` + `handler_commands.go` |
| `/cwd [path]` | ✅ ⚠️ | ❌ | ✅ ⚠️ | ❌ | `handler_commands.go` (allowlist + denylist) |
| `/cron add\|list\|delete` | ✅ | ❌ | ✅ | ❌ | `handler.go` + `handler_commands.go` |
| `/reload` | ✅ | ❌ | ✅ | ❌ | `handler.go` + `handler_commands.go` |
| Summarize (NL trigger, e.g. "总结", "summarize") | ✅ (needs Private App) | ❌ | ⚠️ configured group only | ❌ | `handler_summarize.go` + `handler_commands.go` |
| Summarize without Private App | ❌ disabled | n/a | ❌ disabled | ❌ disabled | `handler_summarize.go` |
| `/full-access status\|grant\|revoke` | ✅ ⚠️ | ❌ (owner-only, DM-only) | ❌ (DM-only) | ❌ (DM-only) | `handler_fullaccess.go` |
| `/approval <id>` / `/approval deny <id>` | ✅ consumed; redirected to terminal (`ringclaw approval <id>`) | ✅ consumed; redirected to terminal | ❌ refused with explanatory message | ❌ refused with explanatory message | `handler.go` + `oob/authorize.go` |
| `/mem add [user\|chat\|global] <text>` | ✅ | ❌ | ✅ | ❌ | `handler_persona.go` + `handler_commands.go` |
| `/mem del [scope] [confirm]` | ✅ | ❌ | ✅ | ❌ | same as `/mem add`; two-phase confirmation |
| `/mem show [scope]` | ✅ | ✅ | ✅ | ✅ | read-only, unprivileged |
| `/persona` | ✅ | ✅ | ✅ | ✅ | read-only, unprivileged |

## Extra checks

- **`/cwd`** — the absolute path must land inside
  `agent_allow_workspace_list ∪ agent_workspace ∪ ~/.ringclaw/workspace`
  AND must not contain any of the denylisted directories (`.ssh`,
  `.gnupg`, `.ringclaw`, `.aws`, `.kube`, `.config/gcloud`). Both
  checks run regardless of full-access state. See
  [Workspace Allowlist](./workspace-allowlist).
- **`/full-access grant [duration]`** — only activates after the
  owner replies `/approval <id>` from the terminal. Challenge TTL 5
  min, default grant 24 h, max 30 d. See
  [ACP Full-Access](./full-access).
- **`/approval`** — any `/approval ...` message in the bot DM is
  consumed and redirected to the terminal CLI with instructions
  (`oob/authorize.go`). Approval requires running
  `ringclaw approval <id>` on the host machine. A `/approval ...`
  shape posted outside the bot DM is intercepted with an explicit
  refusal so the syntax never leaks into a default-agent prompt.
  See [Approval CLI](./approval-cli).
- **Summarize in group** — only the group whose ID matches
  `ringcentral.group_summary_group_id`; cross-group / cross-person
  summarize refused (`handler_summarize.go`).
- **`/mem add` and `/mem del`** — Layer 1 privileged (same gate as
  `/cron`). All memory file writes land strictly under
  `persona.memory_dir`; hostile chat/user IDs cannot escape the
  tree because IDs go through `SanitizeID` before being used as
  filenames. See
  [Configuration › persona](../guide/configuration#persona) for the
  scope layout.
- **`/mem del`** without the trailing `confirm` token never clears
  memory; the first call prints the resolved file path, current
  size, and a tail preview so the operator can verify they are
  targeting the right scope before re-sending with `confirm`.
  `/mem del confirm` does **not** reset agent sessions — the
  persona banner is rebuilt from disk on the next message, but
  in-flight sessions still hold the old memory in their context.
  Run `/new` after a clear if you want the live agent to drop the
  old context too.
- **Cron / Heartbeat / HTTP API do NOT inject the persona banner.**
  These non-interactive entry points have no real chat or user
  context; the banner is only prepended to WebSocket user messages
  (`dispatchToAgent` and `broadcastToAgents`).
