.DEFAULT_GOAL := help

COMPOSE     := docker compose
COMPOSE_DEV := docker compose -f docker-compose.yaml -f docker-compose.dev.yaml
MIGRATE     := go run ./cmd/migrate

ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help up infra dev down clean logs ps psql build test tidy \
        migrate-up migrate-down migrate-up-to migrate-down-to migrate-redo migrate-status migrate-create

help:
	@echo "  up              підняти весь стек"
	@echo "  infra           лише база та моніторинг"
	@echo "  dev             стек із фронтом у режимі vite dev (HMR)"
	@echo "  down            зупинити стек"
	@echo "  clean           зупинити і видалити томи з даними"
	@echo "  logs            дивитися логи"
	@echo "  ps              стан контейнерів"
	@echo "  psql            psql у контейнері бази"
	@echo ""
	@echo "  migrate-up      накотити всі міграції"
	@echo "  migrate-down    відкотити останню міграцію"
	@echo "  migrate-up-to   накотити до версії: make migrate-up-to v=3"
	@echo "  migrate-down-to відкотити до версії: make migrate-down-to v=1"
	@echo "  migrate-redo    перекотити останню міграцію"
	@echo "  migrate-status  стан міграцій"
	@echo "  migrate-create  новий файл міграції: make migrate-create name=add_events"
	@echo ""
	@echo "  build           зібрати бінарники локально"
	@echo "  test            прогнати тести"
	@echo "  tidy            упорядкувати залежності"

up:
	$(COMPOSE) up -d --build

infra:
	$(COMPOSE) up -d postgres prometheus grafana

dev:
	$(COMPOSE_DEV) up --build

down:
	$(COMPOSE) down

clean:
	$(COMPOSE) down -v

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

psql:
	$(COMPOSE) exec postgres psql -U $${POSTGRES_USER:-app} -d $${POSTGRES_DB:-activity}

migrate-up:
	$(MIGRATE) up

migrate-down:
	$(MIGRATE) down

migrate-up-to:
	@test -n "$(v)" || { echo "вкажи версію: make migrate-up-to v=3"; exit 1; }
	$(MIGRATE) up-to $(v)

migrate-down-to:
	@test -n "$(v)" || { echo "вкажи версію: make migrate-down-to v=1"; exit 1; }
	$(MIGRATE) down-to $(v)

migrate-redo:
	$(MIGRATE) redo

migrate-status:
	$(MIGRATE) status

migrate-create:
	@test -n "$(name)" || { echo "вкажи назву: make migrate-create name=add_events"; exit 1; }
	$(MIGRATE) create $(name) sql

build:
	go build -o bin/app ./cmd/app
	go build -o bin/migrate ./cmd/migrate

test:
	go test ./... -race -cover

tidy:
	go mod tidy
