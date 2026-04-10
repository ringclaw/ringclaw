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

FROM public.ecr.aws/docker/library/alpine:latest

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /usr/local/bin/ringclaw /usr/local/bin/ringclaw

VOLUME /root/.ringclaw
ENTRYPOINT ["ringclaw"]
CMD ["start"]
