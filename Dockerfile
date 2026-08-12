# syntax=docker/dockerfile:1

# ---------------------------------------------------------------- base
# Shared by dev and builder so dependency downloads are cached once.
FROM golang:1.25-alpine AS base

WORKDIR /src

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# ---------------------------------------------------------------- dev
# Source is bind-mounted by compose; air rebuilds on change.
FROM base AS dev

RUN go install github.com/air-verse/air@v1.63.0

COPY . .

EXPOSE 3000

CMD ["air", "-c", ".air.web.toml"]

# ---------------------------------------------------------------- builder
FROM base AS builder

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/web ./cmd/web && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# ---------------------------------------------------------------- prod
FROM alpine:3.21 AS prod

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 mailpulse

WORKDIR /app

COPY --from=builder /out/web /app/web
COPY --from=builder /out/worker /app/worker
# no config file: the binaries read the environment, with defaults compiled
# into internal/config/viper.go

USER mailpulse

EXPOSE 3000

# compose overrides this with /app/worker for the worker service
ENTRYPOINT ["/app/web"]
