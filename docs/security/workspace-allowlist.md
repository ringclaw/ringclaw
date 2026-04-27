---
title: Workspace Allowlist
---

# Workspace Allowlist

`/cwd` and the underlying `Agent.SetCwd` are pinned to an
**allowlist of directory roots**. Any attempt to switch the working
directory to a path outside every configured root is denied with an
error like:

```text
Denied: path "/etc" escapes configured workspace allowlist [/home/alice/code /home/alice/.ringclaw/workspace]
```

The allowlist constrains where the **starting working directory**
of an agent can be set. It does **not** sandbox file access from
inside an ACP session — see
[ACP Full-Access › Layer 3 invariants](./full-access#layer-3-invariants-worth-highlighting)
for the broader picture.

## Effective allowlist

The effective allowlist is the union of (deduplicated,
symlink-resolved):

1. Every entry in `agent_allow_workspace_list` from `config.json`.
2. The legacy `agent_workspace` (continues to be the default cwd).
3. `~/.ringclaw/workspace` — always implicitly trusted so the
   built-in default cwd is never rejected.

A **denylist** is kept as a defense-in-depth secondary check: even
when the allowlist would admit a path, `/cwd` still refuses any of
the sensitive directories `.ssh`, `.gnupg`, `.ringclaw`, `.aws`,
`.kube`, `.config/gcloud`. Both checks run regardless of
full-access state.

## Example config

```jsonc
{
  // Default cwd (initial directory the agent starts in).
  "agent_workspace": "/home/alice/projects/main",

  // Additional directories the agent may chdir into via /cwd.
  "agent_allow_workspace_list": [
    "/home/alice/projects/secondary",
    "/home/alice/scratch"
  ]
}
```

## What it does NOT do

- It does **not** sandbox file reads or writes from inside an ACP
  session. An ACP agent in default mode can `fs/read_text_file` any
  path it has OS permission to touch; if `allow_write: true`, it
  can also `fs/write_text_file` any such path.
- It does **not** prevent shell commands run via `terminal/create`
  from operating on paths outside the allowlist. The allowlist only
  pins the starting cwd.
- It does **not** auto-restrict tools the ACP agent uses internally
  (e.g. Claude's grep / read tools). Whatever the agent chooses to
  use is bounded by OS permissions, not by RingClaw.

For the broader file-permission picture see
[ACP Full-Access](./full-access). For the `/cwd` command's Layer 1
gate (owner-only outside DM), see
[Command Authorization](./command-authorization).
