# Keller POC · SOUL Templates

## 设计原则

每个 bot 由三层组成：

```
SOUL          ← 这里定义：身份、声音、硬规则、记忆配置
  +
SKILLS        ← 可配置的通用工作流（dispatch-confirm / daily-digest / complaint-handling）
  +
RC ABILITIES  ← 默认全开：SMS · Fax · Phone · Task · Note · Event
               Card · Calendar · Directory · Presence · MMS · AI · Video
```

**SOUL 只负责"是谁"和"怎么做人"，不负责"怎么用 API"。**
RC 能力不在 SOUL 里声明——它们是基础设施，始终可用。
Skills 带来自己的工作流提示词——SOUL 里只需列出"激活哪些"。

---

## Bot 类型

| 类型 | 含义 | Keller 例子 |
|------|------|-------------|
| `personal` | 一个员工一个 bot，SOUL 写这个人的名字、习惯、偏好 | Sarah-bot、Tom-bot、Karen-bot |
| `role` | 共用 bot，多个人可以互动，SOUL 用角色描述而非具名 | hr-bot（全体员工可 DM） |

---

## 模板索引

| 文件 | Bot 类型 | 激活 Skills | 座位数（Keller） |
|------|---------|------------|-----------------|
| `01-csr.md` | personal | dispatch-confirm · complaint-handling | ~150 |
| `02-store-mgr.md` | personal | daily-digest · complaint-handling | 33 |
| `03-crew-lead.md` | personal | 无 | ~70 |
| `04-lowes-liaison.md` | personal | daily-digest · dispatch-confirm | 1-2 |
| `05-regional-coordinator.md` | personal | daily-digest | ~4 |
| `06-exec.md` | personal | daily-digest | 1-2 |
| `07-hr.md` | **role** | dispatch-confirm | 1-3 |

---

## 部署命令

```bash
# personal bot（Sarah 专用）
ringclaw onboard \
  --owner sarah.cooper@keller.com \
  --soul-template csr \
  --chat-id <atlanta-orders> \
  --chat-id <atlanta-ops>

# role bot（hr-bot，Linda 管理，全员可 DM）
ringclaw onboard \
  --owner linda.wu@keller.com \
  --soul-template hr \
  --bot-type role \
  --allow-dm-from all
```

部署后 SOUL.md 拷贝到 `~/.ringclaw/personas/<owner>/SOUL.md`，可自由编辑，版本升级不会覆盖。

---

## POC 起步五人组

| 姓名 | 角色 | 模板 | Bot 名 |
|------|------|------|--------|
| Sarah Cooper | Atlanta CSR | `csr` | sarah-bot |
| Tom Rivera | Atlanta 门店经理 | `store-mgr` | tom-bot |
| Mike Reyes | Atlanta 施工队长 | `crew-lead` | mike-bot |
| Karen Yates | Lowe's 联络人 | `lowes-liaison` | karen-bot |
| Beth Owens | Chief of Staff | `exec` | beth-bot |

Week 5-6 按需扩展：HR + 区域协调员。
