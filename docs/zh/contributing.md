---
title: 贡献指南
---

# 贡献指南

## 开发

```bash
# 热重载
make dev

# 构建
go build -o ringclaw .

# 运行
./ringclaw start
```

## 发布流水线

多阶段 CI 流水线：

| 触发条件 | 通道 | Tag 格式 |
|---------|------|----------|
| 推送到 feature 分支 | Alpha | `alpha-<branch>` |
| 推送到 main | Beta | `beta-latest` |
| 推送版本 tag | Stable | `v0.1.0` |

### 创建稳定版

```bash
git tag v0.1.0
git push origin v0.1.0
```

所有通道都为 `darwin/linux/windows` × `amd64/arm64` 构建二进制并生成校验和。

## License

[MIT](https://github.com/ringclaw/ringclaw/blob/main/LICENSE)
