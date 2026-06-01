FROM golang:1.26.2-alpine AS builder

ARG VERSION=dev
ARG COMMIT=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/ringclaw/ringclaw/cmd.Version=${VERSION} -X github.com/ringclaw/ringclaw/cmd.Commit=${COMMIT}" \
    -o /usr/local/bin/ringclaw .

FROM node:24-bookworm-slim

ARG NPM_REGISTRY=https://nexus-xmn02.int.rclabenv.com/nexus/content/groups/npm-all/
ENV DISABLE_AUTOUPDATER=1
ENV NPM_CONFIG_REGISTRY=${NPM_REGISTRY}

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && npm install -g \
      @zed-industries/codex-acp \
      @openai/codex \
      @anthropic-ai/claude-code \
      @agentclientprotocol/claude-agent-acp \
    && npm cache clean --force \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/ringclaw /usr/local/bin/ringclaw
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod 755 /usr/local/bin/docker-entrypoint.sh

VOLUME /root/.ringclaw
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
