# syntax=docker/dockerfile:1
#
# Imagem do serviço WaCalls: painel React + servidor Go (pure-Go, MLow embutido).
# Build: docker build -t jussiesouto/wacalls:<tag> .
# Roda em linux/amd64 (VPS). O servidor serve o painel via -static client/dist.

# 1) Painel do operador (React/Vite)
FROM node:22-alpine AS client
WORKDIR /src/client
COPY client/package.json client/package-lock.json ./
RUN npm ci
COPY client/ ./
RUN npm run build

# 2) Servidor Go (CGO off → binário estático; codec MLow é pure-Go)
FROM golang:1.26 AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/wacalls ./cmd/server

# 3) Runtime mínimo (busybox tem wget p/ o healthcheck do stack)
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=server /out/wacalls /app/wacalls
COPY --from=client /src/client/dist /app/client/dist
EXPOSE 8080
ENTRYPOINT ["/app/wacalls"]
CMD ["-static", "client/dist", "-addr", ":8080"]
