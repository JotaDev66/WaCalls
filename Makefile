COMPOSE ?= docker compose
POSTGRES_COMPOSE ?= $(COMPOSE) -f docker-compose.yml -f docker-compose.postgres.yml

# Load .env into recipe environments for the local dev targets (ignored if absent).
ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: build up up-postgres down restart logs sh clean dev server client test lint tidy

build:
	$(COMPOSE) build

up:
	$(COMPOSE) up -d

up-postgres:
	$(POSTGRES_COMPOSE) up -d

down:
	$(COMPOSE) down

restart:
	$(COMPOSE) up -d --force-recreate

logs:
	$(COMPOSE) logs -f wacalls

sh:
	$(COMPOSE) exec wacalls sh

clean:
	$(COMPOSE) down -v

# --- Local development (hot reload) ---
# `make dev` runs the Go server under air (rebuild+restart on .go changes) on :3001
# and the Vite client on :5173 (which proxies /api -> :3001). Open http://localhost:5173.
dev:
	$(MAKE) -j2 server client

server:
	air

client:
	cd client && npm run dev

test:
	go test ./... -count=1

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
