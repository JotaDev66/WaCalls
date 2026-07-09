COMPOSE ?= docker compose
POSTGRES_COMPOSE ?= $(COMPOSE) -f docker-compose.yml -f docker-compose.postgres.yml

.PHONY: build up up-postgres down restart logs sh clean

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
