# RingClaw AI Agent Setup Runbook

这份文档是给 AI agent 执行 setup 用的可执行 runbook。AI 读取本文后，应按步骤完成 RingClaw 安装、RingCentral DPW 配置、本机配置、重启和验证。

## 使用方式

用户只需把文件路径或链接交给 AI：

```text
根据 {ringclaw-repo}/docs/zh/guide/ai-agent-setup.md setup RingClaw
```

AI 收到后必须先读取本文，然后直接执行 setup。不要只给计划，也不要要求用户把内容再复制一遍。

## 执行规则与目标

AI 执行时遵守以下规则：

- 先看官方文档再操作：[快速开始](./getting-started.md)、[配置文件](./configuration.md)、[DPW 凭证创建](https://developers.ringcentral.com/guide/getting-started/create-credential)
- 只使用官方 binary（`~/.local/bin/ringclaw` 或 `PATH` 里的 `ringclaw`），不改源码，不用源码 build
- 所有真实凭证只写进 `~/.ringclaw/config.json`，不写入共享文档
- 每次展示配置时只展示布尔值、chat id、user id、agent 名称，不展示 token/secret/JWT
- 如果浏览器需要登录，让用户完成登录、MFA/Okta push，然后继续

最终必须验证：

1. `ringclaw status` 显示 running
2. 日志里 `private app authentication successful`
3. 日志里 `bot DM chat resolved`
4. 日志里 `subscribed to post events, listening...`
5. 私聊或群聊至少一次端到端响应成功

## 1. 预检本机环境

```bash
which ringclaw || true
ringclaw version || true
curl --version
jq --version
```

如果没有 `ringclaw`，按官方方式安装：

```bash
curl -sSL https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.sh | sh
```

安装后确认版本 `ringclaw v0.4.x`。

说明：官方 `ringclaw setup` 可以交互式收集凭据，但 AI setup 更适合先在 DPW 创建好凭据，再直接写 `~/.ringclaw/config.json`。

## 2. 在 DPW 创建 Bot App

打开 https://developers.ringcentral.com/my-account.html#/applications ，如需登录让用户登录。

创建或确认一个 Bot App：

1. 点击 `Create App` / `Register App`，选择 `Bot Add-in`。
2. App Name 建议：`<Your Name> RingClaw Bot`，Access 选 `Private`，启用 Bot User。
3. Scopes：`Read Accounts`、`Read Messages`、`Team Messaging`、`WebSocket`、`WebSocket Subscriptions`。
4. 创建 app，进入 Bot tab，点击 `Install` 安装到当前账号。
5. 复制 `Bot Token`，后面写入 `ringcentral.bot_token`。

验证点：App Type 是 Bot Add-in、Access 是 Private、Bot User 已启用、5 个 Scopes 齐全、已安装、有 Bot Token。

## 3. 在 DPW 创建 Private REST API App

此 app 用于 owner 身份认证、owner DM 解析、OOB approval。私聊要稳定工作必须配。

1. 点击 `Create App`，选择 `REST API App (most common)`。
2. App Name 建议：`<Your Name> RingClaw Private App`，Auth 选 `JWT auth flow`，Access 选 `Private`。
3. Scopes 同 Bot App（5 个）。
4. 创建 app，记录 `Client ID` 和 `Client Secret`。

创建 JWT：

1. 打开 `https://developers.ringcentral.com/console/my-credentials/create?client_id=<PRIVATE_APP_CLIENT_ID>`
2. Label：`RingClaw owner private chat`，选择 `Only specific apps of my choice`，确认表格里有刚创建的 Private App。
3. 点击 `Create JWT`，复制 JWT。

验证点：Auth 是 JWT、Access 是 Private、JWT 是当前 owner 创建的、JWT 授权给 Private App。

## 4. 获取 chat id

需要至少一个 chat id（群聊和/或 owner 与 bot 的私聊）。

1. 打开 RingCentral App，进入目标群聊或与 bot 的私聊。
2. 复制 conversation link，URL 中 `/messages/` 后面的数字就是 chat id。

示例：`https://app.ringcentral.com/l/messages/1234567890` → chat id 是 `1234567890`

注意：群聊里必须把 bot 加进去；私聊必须是 owner 和 bot 的 direct chat；`chat_ids` 里要同时放群聊和私聊。

## 5. 用 API 验证 chat id

把 Bot Token 放到环境变量里，不要打印：

```bash
export RC_BOT_TOKEN='<BOT_TOKEN>'
```

验证 chat：

```bash
for id in <GROUP_CHAT_ID> <OWNER_DM_CHAT_ID>; do
  out="/tmp/ringclaw-chat-${id}.json"
  code=$(curl -sS -o "$out" -w "%{http_code}" \
    -H "Authorization: Bearer ${RC_BOT_TOKEN}" \
    "https://platform.ringcentral.com/team-messaging/v1/chats/${id}")
  printf "chat=%s http=%s\n" "$id" "$code"
  jq '{id, type, name, status}' "$out"
done
```

期望：群聊返回 HTTP `200`，`type` 通常是 `Team`；私聊返回 `200`，`type` 通常是 `Direct`。

如需验证私聊 members（确认包含 owner 和 bot），使用 members 端点：

```bash
curl -sS -H "Authorization: Bearer ${RC_BOT_TOKEN}" \
  "https://platform.ringcentral.com/team-messaging/v1/chats/<OWNER_DM_CHAT_ID>/members" \
  | jq '.records[] | {id, name}'
```

> 注意：执行完成后运行 `unset RC_BOT_TOKEN` 清理环境变量，避免凭证留在 shell history 中。

## 6. 获取 owner user id

把 Private App 凭证放到环境变量里：

```bash
export RC_CLIENT_ID='<PRIVATE_APP_CLIENT_ID>'
export RC_CLIENT_SECRET='<PRIVATE_APP_CLIENT_SECRET>'
export RC_JWT_TOKEN='<OWNER_JWT>'
```

换取 access token 并获取 owner extension id：

```bash
token_json=$(curl -sS -u "$RC_CLIENT_ID:$RC_CLIENT_SECRET" \
  -X POST https://platform.ringcentral.com/restapi/oauth/token \
  -d grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer \
  -d assertion="$RC_JWT_TOKEN")

RC_PRIVATE_ACCESS_TOKEN=$(printf "%s" "$token_json" | jq -r '.access_token // empty')

if [ -z "$RC_PRIVATE_ACCESS_TOKEN" ]; then
  printf "%s\n" "$token_json" | jq '{error, error_description, message}'
  exit 1
fi

curl -sS \
  -H "Authorization: Bearer ${RC_PRIVATE_ACCESS_TOKEN}" \
  https://platform.ringcentral.com/restapi/v1.0/account/~/extension/~ \
  | jq '{id, extensionNumber, name, email}'
```

输出里的 `id` 就是 owner user id，放入 `ringcentral.source_user_ids`。

常见错误：JWT 不是当前 owner 创建的、JWT 没授权给 Private App、Client ID/Secret/JWT 填错。

> 注意：执行完成后运行 `unset RC_CLIENT_ID RC_CLIENT_SECRET RC_JWT_TOKEN RC_PRIVATE_ACCESS_TOKEN` 清理环境变量。

## 7. 写入 RingClaw 配置

最小可用模板（`~/.ringclaw/config.json`）：

```json
{
  "default_agent": "codex",
  "ringcentral": {
    "server_url": "https://platform.ringcentral.com",
    "bot_token": "<BOT_TOKEN>",
    "client_id": "<PRIVATE_APP_CLIENT_ID>",
    "client_secret": "<PRIVATE_APP_CLIENT_SECRET>",
    "jwt_token": "<OWNER_JWT>",
    "chat_ids": [
      "<GROUP_CHAT_ID>",
      "<OWNER_DM_CHAT_ID>"
    ],
    "source_user_ids": [
      "<OWNER_USER_ID>"
    ],
    "group_mention_only": true
  }
}
```

如果 AI 要自动写入，先 export 所有变量，然后用 jq 生成配置：

```bash
export RC_BOT_TOKEN='<BOT_TOKEN>'
export RC_CLIENT_ID='<PRIVATE_APP_CLIENT_ID>'
export RC_CLIENT_SECRET='<PRIVATE_APP_CLIENT_SECRET>'
export RC_JWT_TOKEN='<OWNER_JWT>'
export RINGCLAW_GROUP_CHAT_ID='<GROUP_CHAT_ID>'
export RINGCLAW_OWNER_DM_CHAT_ID='<OWNER_DM_CHAT_ID>'
export RINGCLAW_OWNER_USER_ID='<OWNER_USER_ID>'
export RINGCLAW_DEFAULT_AGENT='codex'
```

写配置（支持合并已有配置）：

```bash
mkdir -p ~/.ringclaw

new_config=$(jq -n \
  --arg bot "$RC_BOT_TOKEN" \
  --arg cid "$RC_CLIENT_ID" \
  --arg sec "$RC_CLIENT_SECRET" \
  --arg jwt "$RC_JWT_TOKEN" \
  --arg gchat "$RINGCLAW_GROUP_CHAT_ID" \
  --arg dmchat "$RINGCLAW_OWNER_DM_CHAT_ID" \
  --arg uid "$RINGCLAW_OWNER_USER_ID" \
  --arg agent "${RINGCLAW_DEFAULT_AGENT:-codex}" \
  '{
    default_agent: $agent,
    ringcentral: {
      server_url: "https://platform.ringcentral.com",
      bot_token: $bot,
      client_id: $cid,
      client_secret: $sec,
      jwt_token: $jwt,
      chat_ids: ([$gchat, $dmchat] | map(select(. != ""))),
      source_user_ids: [$uid],
      group_mention_only: true
    }
  }')

if [ -f ~/.ringclaw/config.json ]; then
  jq -s '.[0] * .[1]' ~/.ringclaw/config.json <(echo "$new_config") > ~/.ringclaw/config.json.tmp
  mv ~/.ringclaw/config.json.tmp ~/.ringclaw/config.json
else
  echo "$new_config" > ~/.ringclaw/config.json
fi

chmod 600 ~/.ringclaw/config.json
```

验证配置（不打印 secret）：

```bash
jq '{
  has_bot_token: ((.ringcentral.bot_token // "") != ""),
  has_private_app: ((.ringcentral.client_id // "") != "" and (.ringcentral.client_secret // "") != "" and (.ringcentral.jwt_token // "") != ""),
  chat_ids: .ringcentral.chat_ids,
  source_user_ids: .ringcentral.source_user_ids,
  group_mention_only: .ringcentral.group_mention_only,
  default_agent: .default_agent
}' ~/.ringclaw/config.json
```

> 注意：执行完成后运行 `unset RC_BOT_TOKEN RC_CLIENT_ID RC_CLIENT_SECRET RC_JWT_TOKEN RINGCLAW_GROUP_CHAT_ID RINGCLAW_OWNER_DM_CHAT_ID RINGCLAW_OWNER_USER_ID` 清理环境变量。

## 8. 启动或重启 RingClaw

先停止旧实例：

```bash
ringclaw stop || true
```

查是否还有旧进程：

```bash
ps -ef | grep -E 'ringclaw start -f|codex-acp|claude-agent-acp' | grep -v grep || true
```

如果有多个旧 RingClaw 实例，`kill <OLD_RINGCLAW_PID>` 清理旧的。

`ringclaw start` 默认以后台 daemon 方式运行，不会阻塞当前终端：

```bash
ringclaw start
ringclaw status
```

期望输出：`ringclaw is running (pid=<PID>)`

## 9. 验证启动日志

```bash
tail -160 ~/.ringclaw/ringclaw.log
```

必须看到这些日志：

```text
initializing bot client...
bot extension ID resolved
initializing private app client...
private app authentication successful
private app owner ID resolved ownerID=<OWNER_USER_ID>
bot DM chat resolved chatID=<OWNER_DM_CHAT_ID>
OOB approval flow active ownerDMChatID=<OWNER_DM_CHAT_ID>
starting message bridge chatIDs="[<GROUP_CHAT_ID> <OWNER_DM_CHAT_ID>]"
source_user_ids resolved
subscribed to post events, listening...
default agent ready
```

如果出现 `no private app configured` 或 `bot DM chat with owner not resolved`，不能算 setup 完成。如果出现 `address already in use`，回到第 8 步清理旧进程。

## 10. 端到端测试

### 私聊测试

让用户在 owner 与 bot 的私聊里发送 `Reply only OK`，然后看日志：

```bash
tail -120 ~/.ringclaw/ringclaw.log | grep -E 'received post|received message|sent typing placeholder|dispatching to agent|agent replied|updated post|sent reply|ERR|WRN'
```

期望看到：`received post` → `received message` → `sent typing placeholder` → `dispatching to agent` → `agent replied` → `updated post`。

### 群聊测试

如果 `group_mention_only` 是 `true`，群聊里必须 `@<Bot Name> Reply only OK`。

## 11. 最终交付标准

AI 最终回复只报告：官方 binary 版本、DPW App 状态、配置写入状态（`has_bot_token`/`has_private_app`/`chat_ids`/`source_user_ids`）、`ringclaw status` 结果、日志关键行确认、端到端测试结果。提醒用户 rotate 任何曾发到聊天里的凭证。

不要在最终回复中包含 Bot Token、Client Secret、JWT、OAuth access token 或完整配置文件。

## 12. 排错

| 症状 | 检查点 |
|------|--------|
| 私聊完全没反应 | 日志是否有 `bot DM chat resolved`；`client_id`/`client_secret`/`jwt_token` 是否齐全；JWT 是否 owner 创建并授权给 Private App；owner 与 bot 是否有 direct chat；DM chat id 是否在 `chat_ids` 中 |
| `no private app configured` | 配置缺少 `client_id`、`client_secret` 或 `jwt_token`，补全后重启 |
| `bot DM chat with owner not resolved` | 确认 `private app owner ID resolved` 日志存在；确认 owner 与 bot 私聊存在；用 Bot Token API 验证 chat type 是 `Direct` |
| 群聊 `Only the bot owner can use this command` | 发消息人 user id 不在 `source_user_ids`；或需走 authorize-mention OOB 流程 |
| 群聊不触发 | `chat_ids` 是否包含该群聊；bot 是否在群里；`group_mention_only: true` 时是否 `@bot`；WebSocket 是否 subscribed |
| 收到消息但没回复 | 日志是否有 `dispatching to agent`；检查 `default agent ready` 和 agent 可用性 |
| `address already in use` | 有旧进程占端口，`ps -ef \| grep ringclaw \| grep -v grep` 找到并 kill |

## 13. 新增群聊

不需要重新创建 DPW app。把 bot 加到新群聊，取 chat id，追加到 `chat_ids`，重启 RingClaw，`@bot Reply only OK` 测试。

## 14. 安全要求

- 真实凭证永远不要写进分享文档
- 用户如果把凭证发到聊天里，setup 后建议 rotate/recreate
- `~/.ringclaw/config.json` 权限保持 `600`
- AI 最终输出只能脱敏
- 临时文件如果包含 API 响应，确认不含 access token，否则删除
