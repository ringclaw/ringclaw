---
layout: home
title: RingClaw
---

<script setup>
import { useData } from 'vitepress'
const { isDark } = useData()
</script>

# RingClaw {.home-title}

:::: hero
::: info

# RingClaw

RingCentral AI Agent Bridge

Connect RingCentral Team Messaging to AI agents — Claude, Codex, Gemini, Kimi, and more.

[Get Started](/guide/getting-started){.action-btn .primary}
[GitHub](https://github.com/ringclaw/ringclaw){.action-btn .secondary}

:::
::::

## Features

<div class="features-grid">

<div class="feature">

### 🤖 Multi-Agent Support

Connect Claude, Codex, Gemini, Kimi, Copilot, Droid, and more. Switch between agents with a single command. Supports ACP, CLI, and HTTP modes.

</div>

<div class="feature">

### 🔒 Security First

Three-layer permission model: trusted-sender allowlist, chat-command authorization, and ACP session capability gating with `/full-access` two-step approval. Token-based API, DNS rebinding protection, workspace path restrictions built in.

</div>

<div class="feature">

### 💬 Chat Summarization

Summarize conversations from any chat. Resolve targets by name, fetch messages, and get AI-powered summaries delivered to your current chat.

</div>

<div class="feature">

### ⚡ AI-Driven Actions

Agents automatically create notes, tasks, events, and adaptive cards during conversation. No extra configuration needed.

</div>

<div class="feature">

### ⏰ Cron & Heartbeat

Schedule recurring tasks with cron expressions or intervals. Heartbeat mode runs periodic agent check-ins driven by a user-authored checklist.

</div>

<div class="feature">

### 🖥️ Full CLI

Complete command-line interface for messages, chats, tasks, notes, events, cards, users, and files — no bridge needed.

</div>

</div>
