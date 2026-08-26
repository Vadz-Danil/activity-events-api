.DEFAULT_GOAL := help

COMPOSE     := docker compose
COMPOSE_DEV := docker compose -f docker-compose.yaml -f docker-compose.dev.yaml
MIGRATE     := go run ./cmd/migrate

ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help up infra dev down clean logs ps psql build test tidy \
        test-integration migrate-up migrate-down migrate-up-to migrate-down-to migrate-redo migrate-status migrate-create

help:
	@echo "  up              start the whole stack"
	@echo "  infra           database and monitoring only"
	@echo "  dev             stack with the frontend in vite dev mode (HMR)"
	@echo "  down            stop the stack"
	@echo "  clean           stop and drop data volumes"
	@echo "  logs            follow logs"
	@echo "  ps              container status"
	@echo "  psql            psql inside the database container"
	@echo ""
	@echo "  migrate-up      apply all migrations"
	@echo "  migrate-down    roll back the last migration"
	@echo "  migrate-up-to   apply up to a version: make migrate-up-to v=3"
	@echo "  migrate-down-to roll back to a version: make migrate-down-to v=1"
	@echo "  migrate-redo    re-apply the last migration"
	@echo "  migrate-status  migration status"
	@echo "  migrate-create  new migration file: make migrate-create name=add_events"
	@echo ""
	@echo "  build           build binaries locally"
	@echo "  test            run tests"
	@echo "  test-integration  repository tests against the running database"
	@echo "  tidy            tidy dependencies"

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
	@test -n "$(v)" || { echo "pass a version: make migrate-up-to v=3"; exit 1; }
	$(MIGRATE) up-to $(v)

migrate-down-to:
	@test -n "$(v)" || { echo "pass a version: make migrate-down-to v=1"; exit 1; }
	$(MIGRATE) down-to $(v)

migrate-redo:
	$(MIGRATE) redo

migrate-status:
	$(MIGRATE) status

migrate-create:
	@test -n "$(name)" || { echo "pass a name: make migrate-create name=add_events"; exit 1; }
	$(MIGRATE) create $(name) sql

build:
	go build -o bin/app ./cmd/app
	go build -o bin/migrate ./cmd/migrate

test:
	go test ./... -race -cover

test-integration:
	go test ./internal/repository/... -tags integration -count=1

tidy:
	go mod tidy
