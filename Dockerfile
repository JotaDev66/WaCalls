# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /web
COPY client/package.json client/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY client/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /web/dist ./internal/app/webui/dist
ARG VERSION=docker
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/wacalls ./cmd/server

FROM alpine:3.21
ARG VERSION=docker
LABEL org.opencontainers.image.source="https://github.com/JotaDev66/WaCalls" \
      org.opencontainers.image.url="https://github.com/JotaDev66/WaCalls" \
      org.opencontainers.image.title="WaCalls" \
      org.opencontainers.image.description="Native WhatsApp voice calls in pure Go, straight from the browser" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"
RUN apk add --no-cache ca-certificates \
    && addgroup -S app && adduser -S -G app -h /app app \
    && mkdir -p /data && chown app:app /data
WORKDIR /app
COPY --from=build /out/wacalls /usr/local/bin/wacalls
USER app
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/sessions >/dev/null 2>&1 || exit 1
ENTRYPOINT ["wacalls"]
CMD ["-addr=:8080", "-db=/data/wacalls.db"]
