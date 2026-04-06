---
title: Contributing
---

# Contributing

## Development

```bash
# Hot reload
make dev

# Build
go build -o ringclaw .

# Run
./ringclaw start
```

## Release Pipeline

Multi-stage CI pipeline:

| Trigger | Channel | Tag format |
|---------|---------|------------|
| Push to feature branch | Alpha | `alpha-<branch>` |
| Push to main | Beta | `beta-latest` |
| Push version tag | Stable | `v0.1.0` |

### Creating a Stable Release

```bash
git tag v0.1.0
git push origin v0.1.0
```

All channels build binaries for `darwin/linux/windows` × `amd64/arm64` with checksums.

## License

[MIT](https://github.com/ringclaw/ringclaw/blob/main/LICENSE)
