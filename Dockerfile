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

ARG NPM_REGISTRY=https://registry.npmjs.org/
ENV DISABLE_AUTOUPDATER=1
ENV NPM_CONFIG_REGISTRY=${NPM_REGISTRY}
ENV HOME=/home/ringclaw
ENV RINGCLAW_HOME=/home/ringclaw/.ringclaw
WORKDIR /home/ringclaw

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata curl python3 \
    && npm install -g \
      @zed-industries/codex-acp \
      @openai/codex \
      @anthropic-ai/claude-code \
      @agentclientprotocol/claude-agent-acp \
    && mkdir -p /home/ringclaw/.ringclaw/workspace /home/ringclaw/.ringclaw/memory \
    && npm cache clean --force \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/ringclaw /usr/local/bin/ringclaw
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod 755 /usr/local/bin/docker-entrypoint.sh

VOLUME /home/ringclaw/.ringclaw
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
