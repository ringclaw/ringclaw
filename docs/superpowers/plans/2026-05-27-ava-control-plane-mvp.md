# AVA Control Plane MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an AVA Control Plane MVP that lets an authenticated RC user create a private Personal AVA bot, allocate tokens from a shared pool, render a long-lived RingClaw runtime spec, and let the runtime pod claim config and report status.

**Architecture:** Add a focused `controlplane` package with a file-backed store that represents the future public DB, a token allocator, onboarding service, runtime claim/heartbeat service, and HTTP handlers. Add a Cobra command to run the control plane using the same Go/Cobra conventions as the existing RingClaw CLI. Keep RingClaw runtime behavior separate.

**Tech Stack:** Go stdlib HTTP, JSON file store, existing `config.Config` schema, existing RingClaw K8S runtime shape.

---

### Task 1: Control Plane Domain and Store

**Files:**
- Create: `controlplane/types.go`
- Create: `controlplane/store.go`
- Test: `controlplane/service_test.go`

- [ ] Write failing tests for creating a bot from a token pool.
- [ ] Implement file-backed store and domain types.
- [ ] Verify `go test ./controlplane -run TestCreateBotAllocatesTokens -count=1`.

### Task 2: Bot Onboarding Service

**Files:**
- Create: `controlplane/service.go`
- Test: `controlplane/service_test.go`

**Default runtime config policy:**
- Render `ringcentral.allow_unlisted_group_chats: true` for AVA-managed bots unless the request explicitly overrides it.
- Keep `ringcentral.group_mention_only: true` by default so unlisted groups still require `@bot`.

- [ ] Write failing tests for private user visibility and one active bot per user.
- [ ] Implement `CreateBot`, `ListBotsForUser`, token reservation, and runtime bootstrap token creation.
- [ ] Verify `go test ./controlplane -count=1`.

### Task 3: Runtime Claim and Heartbeat

**Files:**
- Modify: `controlplane/service.go`
- Test: `controlplane/service_test.go`

- [ ] Write failing tests for runtime claim with bootstrap token.
- [ ] Implement claim response that returns RingClaw config and marks tokens bound.
- [ ] Implement heartbeat status update.
- [ ] Verify `go test ./controlplane -count=1`.

### Task 4: HTTP API and CLI Command

**Files:**
- Create: `controlplane/server.go`
- Create: `controlplane/server_test.go`
- Create: `cmd/control_plane.go`

- [ ] Write failing HTTP tests for `POST /control/v1/bots`, `GET /control/v1/bots/me`, `POST /runtime/v1/claim`, and `POST /runtime/v1/heartbeat`.
- [ ] Implement server handlers with RC identity headers and admin token guard.
- [ ] Add `ringclaw ava-control-plane` command.
- [ ] Verify `go test ./controlplane ./cmd -count=1`.

### Task 5: Docs and End-to-End Smoke

**Files:**
- Modify: `docs/architecture/personal-ava-bot-platform.md`
- Modify: `docs/zh/guide/commands.md`

- [ ] Document the A3 Control Plane flow.
- [ ] Run `go test ./...`, `go vet ./...`, `go build ./...`, and `git diff --check`.
