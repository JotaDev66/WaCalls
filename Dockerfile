# syntax=docker/dockerfile:1

FROM node:22-bookworm-slim AS client
WORKDIR /src/client
COPY client/package.json client/package-lock.json ./
RUN npm ci
COPY client/ ./
RUN npm run build

FROM golang:1.26-bookworm AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=client /src/client/dist ./client/dist
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags "-s -w" -o /out/wacalls ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates \
	&& rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=server /out/wacalls ./wacalls
COPY --from=client /src/client/dist ./client/dist
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/app/wacalls"]
CMD ["-addr", ":8080", "-db", "/data/wacalls.db", "-static", "client/dist"]
