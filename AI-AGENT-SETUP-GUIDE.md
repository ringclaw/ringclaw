# RingClaw 官方 Binary Setup Runbook

这份文档是给 AI agent 执行 setup 用的，不是只给人看的说明。目标是让用户通过本文完成 RingClaw setup；AI 读取本文后，应按步骤完成 RingClaw 安装、RingCentral DPW 配置、本机配置、重启和验证。

文档里不要写入真实 `Bot Token`、`Client Secret`、`JWT`。执行时 AI 可以读取或写入本机 `~/.ringclaw/config.json`，但最终汇报只能输出脱敏状态。

## 用户如何使用本文

用户只需要把这个文件路径或链接交给 AI，并要求 AI 通过本文 setup RingClaw。

推荐输入：

```text
根据 {ringclaw-repo/AI-AGENT-SETUP-GUIDE.md} setup RingClaw
```

其中 `{ringclaw-repo}` 可以是：

- 本地 RingClaw repo 路径，例如 `/Users/<you>/work/git/ringclaw`
- GitHub 文件链接
- AI 工具能读取的仓库引用

AI 收到这句话后，必须先打开并读取本文，然后直接执行 setup。不要只给计划，也不要要求用户把本文内容再复制一遍。

## AI 执行目标

AI 执行本文时必须满足：

1. 使用 RingClaw 官方 binary，不要使用源码自编译版本。
2. 按官方文档配置 Bot App 和 Private REST API App/JWT。
3. 支持群聊响应，也支持我和 bot 的私聊直接响应。
4. 必要时打开浏览器让我登录 RingCentral Developer Console；我登录后你继续配置。
5. 不要在聊天回复里打印 Bot Token、Client Secret、JWT、access token。
6. 最终必须验证：
   - ringclaw status 显示 running
   - 日志里 private app authentication successful
   - 日志里 bot DM chat resolved
   - 日志里 subscribed to post events, listening...
   - 私聊或群聊至少一次端到端响应成功

## 0. 执行规则

AI 执行时遵守这些规则：

- 先看官方文档，再操作：
  - https://ringclaw.github.io/ringclaw/zh/guide/getting-started.html
  - https://ringclaw.github.io/ringclaw/zh/guide/configuration.html
  - https://developers.ringcentral.com/guide/getting-started/create-credential
- 只使用官方 binary：`~/.local/bin/ringclaw` 或 `PATH` 里的 `ringclaw`。
- 不改 RingClaw 源码，不用源码 build 替换官方 binary。
- 所有真实凭证只写进 `~/.ringclaw/config.json`，不要写入共享文档。
- 每次展示配置时只展示布尔值、chat id、user id、agent 名称，不展示 token/secret/JWT。
- 如果浏览器需要登录，让用户完成登录、MFA/Okta push，然后继续。

## 1. 预检本机环境

执行：

```bash
which ringclaw || true
ringclaw version || true
jq --version
node --version
```

如果没有 `ringclaw`，按官方方式安装：

```bash
curl -sSL https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.sh | sh
```

安装后确认：

```bash
which ringclaw
ringclaw version
```

期望：

```text
ringclaw v0.4.x
```

说明：官方文档里 `ringclaw setup` 可以交互式收集凭据，但 AI setup 通常更适合先在 DPW 创建好凭据，再直接写 `~/.ringclaw/config.json`，这样更容易验证和排错。

## 2. 在 DPW 创建 Bot App

打开：

```text
https://developers.ringcentral.com/my-account.html#/applications
```

如果需要登录，让用户登录。

创建或确认一个 Bot App：

1. 点击 `Create App` / `Register App`。
2. 选择 `Bot Add-in`。
3. App Name 建议：`<Your Name> RingClaw Bot`。
4. Access 选择 `Private`。
5. 启用 Bot User。
6. Scopes 选择：
   - `Read Accounts`
   - `Read Messages`
   - `Team Messaging`
   - `WebSocket`
   - `WebSocket Subscriptions`
7. 创建 app。
8. 进入 Bot tab。
9. 点击 `Install`，把 bot 安装到当前 RingCentral 账号。
10. 复制 `Bot Token`，后面写入 `ringcentral.bot_token`。

Bot App 验证点：

- App Type 是 `Bot Add-in`
- Access 是 `Private`
- Bot User 已启用
- Scopes 包含上面 5 个
- App 已安装到账号
- 能拿到 Bot Token

## 3. 在 DPW 创建 Private REST API App

这个 app 用于 owner 身份认证、owner DM 解析、OOB approval 和高级功能。私聊 owner DM 要稳定工作，建议必须配。

在 Developer Console 创建 REST API App：

1. 点击 `Create App` / `Register App`。
2. 选择 `REST API App (most common)`。
3. App Name 建议：`<Your Name> RingClaw Private App`。
4. Auth 选择 `JWT auth flow`。
5. Access 选择 `Private`。
6. Scopes 选择：
   - `Read Accounts`
   - `Read Messages`
   - `Team Messaging`
   - `WebSocket`
   - `WebSocket Subscriptions`
7. 创建 app。
8. 在 app dashboard 记录：
   - `Client ID`
   - `Client Secret`

创建 JWT：

1. 打开下面这个 URL，把 `<PRIVATE_APP_CLIENT_ID>` 换成刚创建的 Client ID：

   ```text
   https://developers.ringcentral.com/console/my-credentials/create?client_id=<PRIVATE_APP_CLIENT_ID>
   ```

2. Label 填：`RingClaw owner private chat`。
3. `What apps are permitted to use this credential?` 选择：

   ```text
   Only specific apps of my choice
   ```

4. 确认表格里出现刚创建的 Private App。
5. 点击 `Create JWT`。
6. 复制 JWT，后面写入 `ringcentral.jwt_token`。

Private App 验证点：

- Auth 是 `JWT auth flow`
- Access 是 `Private`
- JWT credential 是当前 owner 用户创建的
- JWT 授权给刚创建的 Private App
- 有 `Client ID`、`Client Secret`、`JWT`

## 4. 获取 chat id

需要至少一个 chat id：

- 群聊 chat id
- owner 与 bot 的私聊 chat id

获取方式：

1. 打开 RingCentral App。
2. 进入目标群聊或与 bot 的私聊。
3. 复制 conversation link。
4. URL 中 `/messages/` 后面的数字就是 chat id。

示例：

```text
https://app.ringcentral.com/l/messages/1234567890
```

chat id 是：

```text
1234567890
```

注意：

- 群聊里必须把 bot 加进去。
- 私聊必须是 owner 和 bot 的 direct chat。
- `chat_ids` 里要同时放群聊和私聊。

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
  jq '{id, type, name, status, member_count: ((.members // []) | length), members: ((.members // []) | map(.id))}' "$out"
done
```

期望：

- 群聊返回 HTTP `200`，`type` 通常是 `Team`。
- 私聊返回 HTTP `200`，`type` 通常是 `Direct`。
- 私聊 members 里应该包含 owner user id 和 bot user id。

## 6. 获取 owner user id

把 Private App 凭证放到环境变量里：

```bash
export RC_CLIENT_ID='<PRIVATE_APP_CLIENT_ID>'
export RC_CLIENT_SECRET='<PRIVATE_APP_CLIENT_SECRET>'
export RC_JWT_TOKEN='<OWNER_JWT>'
```

换取 access token，并获取 owner extension id：

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

常见错误：

- JWT 不是当前 owner 创建的。
- JWT 没有授权给 Private App。
- Private App 不是 production/private 状态。
- Client ID/Secret/JWT 填错。

## 7. 写入 RingClaw 配置

建议直接生成或更新：

```text
~/.ringclaw/config.json
```

最小可用模板：

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

如果 AI 要自动写入，可以使用这个 Node 脚本。执行前先 export 所有变量：

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

写配置：

```bash
mkdir -p ~/.ringclaw
node <<'NODE'
const fs = require('fs')
const os = require('os')
const path = require('path')

const configPath = path.join(os.homedir(), '.ringclaw', 'config.json')
const required = [
  'RC_BOT_TOKEN',
  'RC_CLIENT_ID',
  'RC_CLIENT_SECRET',
  'RC_JWT_TOKEN',
  'RINGCLAW_OWNER_DM_CHAT_ID',
  'RINGCLAW_OWNER_USER_ID'
]

const missing = required.filter((key) => !process.env[key])
if (missing.length) {
  throw new Error(`Missing env vars: ${missing.join(', ')}`)
}

const existing = fs.existsSync(configPath)
  ? JSON.parse(fs.readFileSync(configPath, 'utf8'))
  : {}

const chatIds = [
  process.env.RINGCLAW_GROUP_CHAT_ID,
  process.env.RINGCLAW_OWNER_DM_CHAT_ID
].filter(Boolean)

const uniqueChatIds = [...new Set([
  ...((existing.ringcentral && existing.ringcentral.chat_ids) || []),
  ...chatIds
])]

const next = {
  ...existing,
  default_agent: process.env.RINGCLAW_DEFAULT_AGENT || existing.default_agent || 'codex',
  ringcentral: {
    ...(existing.ringcentral || {}),
    server_url: 'https://platform.ringcentral.com',
    bot_token: process.env.RC_BOT_TOKEN,
    client_id: process.env.RC_CLIENT_ID,
    client_secret: process.env.RC_CLIENT_SECRET,
    jwt_token: process.env.RC_JWT_TOKEN,
    chat_ids: uniqueChatIds,
    source_user_ids: [...new Set([
      ...(((existing.ringcentral || {}).source_user_ids) || []),
      process.env.RINGCLAW_OWNER_USER_ID
    ])],
    group_mention_only: true
  }
}

fs.writeFileSync(configPath, JSON.stringify(next, null, 2) + '\n', { mode: 0o600 })
fs.chmodSync(configPath, 0o600)
console.log(JSON.stringify({
  updated: true,
  path: configPath,
  has_bot_token: Boolean(next.ringcentral.bot_token),
  has_private_app: Boolean(next.ringcentral.client_id && next.ringcentral.client_secret && next.ringcentral.jwt_token),
  chat_ids: next.ringcentral.chat_ids,
  source_user_ids: next.ringcentral.source_user_ids,
  default_agent: next.default_agent
}, null, 2))
NODE
```

验证配置，不要打印 secret：

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

设置权限：

```bash
chmod 600 ~/.ringclaw/config.json
```

## 8. 启动或重启 RingClaw

先停止旧实例：

```bash
ringclaw stop || true
```

查是否还有旧进程：

```bash
ps -ef | rg 'ringclaw start -f|codex-acp|claude-agent-acp' || true
```

如果有多个旧 RingClaw 实例，清理旧的，只保留后续新启动的一个：

```bash
kill <OLD_RINGCLAW_PID>
```

启动：

```bash
ringclaw start
ringclaw status
```

期望：

```text
ringclaw is running (pid=<PID>)
```

## 9. 验证启动日志

执行：

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

如果出现下面日志，不能算 setup 完成：

```text
no private app configured
bot DM chat with owner not resolved
```

如果出现：

```text
listen tcp 127.0.0.1:<PORT>: bind: address already in use
```

说明有旧进程占端口。回到第 8 步清理旧 RingClaw 进程。

## 10. 端到端测试

### 私聊测试

最简单方式：让用户在 owner 与 bot 的私聊里发送：

```text
Reply only OK
```

然后看日志：

```bash
tail -120 ~/.ringclaw/ringclaw.log | rg 'received post|received message|sent typing placeholder|dispatching to agent|agent replied|updated post|sent reply|ERR|WRN'
```

期望：

```text
received post chatID=<OWNER_DM_CHAT_ID>
received message chatID=<OWNER_DM_CHAT_ID>
sent typing placeholder chatID=<OWNER_DM_CHAT_ID>
dispatching to agent conversationID=rc:dm:<OWNER_DM_CHAT_ID>:<OWNER_USER_ID>
agent replied
updated post chatID=<OWNER_DM_CHAT_ID>
```

### API 发送私聊测试

如果用户允许 AI 主动发送一条测试消息，可以用 Private App access token 发：

```bash
body=$(jq -n --arg text "Reply only OK" '{text: $text}')
curl -sS -X POST \
  "https://platform.ringcentral.com/team-messaging/v1/chats/${RINGCLAW_OWNER_DM_CHAT_ID}/posts" \
  -H "Authorization: Bearer ${RC_PRIVATE_ACCESS_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$body" \
  | jq '{id, chatId, creatorId, text}'
```

然后看同样的日志。

### 群聊测试

如果 `group_mention_only` 是 `true`，群聊里必须 `@bot`：

```text
@<Bot Name> Reply only OK
```

如果群聊里提示：

```text
Only the bot owner can use this command in group chats.
```

看第 12 节排错。

## 11. 最终交付标准

AI 最终回复给用户时，只报告这些：

- 使用的是官方 binary 及版本。
- DPW Bot App 已确认或创建。
- DPW Private App/JWT 已确认或创建。
- `~/.ringclaw/config.json` 已写入，且：
  - `has_bot_token: true`
  - `has_private_app: true`
  - `chat_ids` 包含群聊和 owner DM
  - `source_user_ids` 包含 owner user id
- `ringclaw status` 正常 running。
- 日志里 private app、owner DM、WebSocket、default agent 都 ready。
- 私聊或群聊端到端测试结果。
- 提醒用户 rotate 任何曾经发到聊天里的凭证。

最终回复不要包含：

- Bot Token
- Client Secret
- JWT
- OAuth access token
- 完整 `~/.ringclaw/config.json`

## 12. 排错

### 私聊完全没反应

先看日志有没有：

```text
bot DM chat resolved chatID=<OWNER_DM_CHAT_ID>
```

没有这行，重点检查：

1. `client_id`、`client_secret`、`jwt_token` 是否都配置了。
2. JWT 是否是 owner 用户创建的。
3. JWT 是否授权给 Private App。
4. owner 是否已经和 bot 有 direct chat。
5. owner DM chat id 是否加入 `ringcentral.chat_ids`。

### 日志显示 `no private app configured`

配置缺少以下任意字段：

```text
ringcentral.client_id
ringcentral.client_secret
ringcentral.jwt_token
```

重新写配置并重启。

### 日志显示 `bot DM chat with owner not resolved`

Private App 可能能登录，但 RingClaw 没找到 owner 和 bot 的 direct chat。

处理：

1. 确认日志里有 `private app owner ID resolved`。
2. 确认 owner 与 bot 的私聊存在。
3. 用 Bot Token API 验证私聊 chat id 是 `Direct`，members 里有 owner 和 bot。
4. 重启 RingClaw。

### 群聊提示 `Only the bot owner can use this command in group chats.`

这通常是权限判断，不是 DPW scope。

检查：

1. 发消息人的 RingCentral user id 是否在 `source_user_ids`。
2. Private App 是否能解析 owner id。
3. 该命令是否是特权命令。特权命令只允许 owner。
4. 如果是非 owner 用户想在群里用，需要走 RingClaw 的 authorize-mention OOB 流程，owner 在私聊批准后才行。

### 群聊不触发

检查：

1. `chat_ids` 是否包含该群聊 id。
2. bot 是否在该群里。
3. `group_mention_only: true` 时是否真的 `@bot`。
4. WebSocket 是否 subscribed。

### 收到消息但没有回复

看是否已经出现：

```text
dispatching to agent
```

如果已经 dispatch，但没有 `agent replied`：

1. 检查 `default agent ready`。
2. 检查 agent 是否可用，例如日志里 `agent available name=codex`。
3. 尝试换默认 agent 或修复 ACP agent。

### 多实例或端口占用

症状：

```text
address already in use
```

处理：

```bash
ps -ef | rg 'ringclaw start -f|codex-acp|claude-agent-acp'
kill <OLD_PID>
ringclaw stop || true
ringclaw start
ringclaw status
```

## 13. 新增群聊

新增群聊时不需要重新创建 DPW app。

步骤：

1. 把 bot 加到新群聊。
2. 复制新群聊 conversation link。
3. 取 `/messages/` 后面的 chat id。
4. 追加到 `ringcentral.chat_ids`。
5. 重启 RingClaw。
6. 在群聊里 `@bot Reply only OK` 测试。

只改这段：

```json
"chat_ids": [
  "<OLD_GROUP_CHAT_ID>",
  "<OWNER_DM_CHAT_ID>",
  "<NEW_GROUP_CHAT_ID>"
]
```

## 14. 安全要求

- 真实凭证永远不要写进分享文档。
- 用户如果把 Bot Token/JWT/Client Secret 发到聊天里，setup 后建议 rotate/recreate。
- `~/.ringclaw/config.json` 权限保持 `600`。
- AI 最终输出只能脱敏。
- 临时文件如果包含 API 响应，确认不含 access token；否则删除。
