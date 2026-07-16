# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /web
COPY client/package.json client/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY client/ ./
RUN npm run build

FROM golang:1.26-alpine AS libopusmlow
# opus_mlow (libopus 1.4 fork with SMPL/MLow) pinned by commit for reproducible
# native-encoder builds. This whole stage is one cached layer keyed by the pin.
ARG OPUS_MLOW_SHA=93e91a74c0a2af610d8313a85e2c811081a73f93
RUN apk add --no-cache git cmake samurai gcc musl-dev \
    && git clone https://github.com/edgardmessias/opus_mlow.git /opus_mlow \
    && cd /opus_mlow && git checkout "${OPUS_MLOW_SHA}" \
    && cmake -B build -G Ninja -DCMAKE_BUILD_TYPE=Release \
    && cmake --build build \
    && mkdir -p /opus/lib && cp build/libopus.a /opus/lib/ && cp -r include /opus/include

FROM golang:1.26-alpine AS build
# NATIVE=1 links the opus_mlow SMPL encoder (nativemlow build tag, CGO + static);
# NATIVE=0 is the default pure-Go build.
ARG NATIVE=0
RUN if [ "$NATIVE" = "1" ]; then apk add --no-cache gcc musl-dev; fi
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web /web/dist ./internal/app/webui/dist
COPY --from=libopusmlow /opus /opus
ARG VERSION=docker
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ "$NATIVE" = "1" ]; then \
        CGO_ENABLED=1 CGO_CFLAGS="-I/opus/include" CGO_LDFLAGS="-L/opus/lib" \
        go build -trimpath -tags nativemlow \
            -ldflags "-s -w -X main.version=${VERSION} -extldflags '-static'" \
            -o /out/wacalls ./cmd/server; \
    else \
        CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/wacalls ./cmd/server; \
    fi

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
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1
ENTRYPOINT ["wacalls"]
CMD ["-addr=:8080", "-db=/data/wacalls.db"]
