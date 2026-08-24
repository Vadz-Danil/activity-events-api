.DEFAULT_GOAL := help

COMPOSE     := docker compose
COMPOSE_DEV := docker compose -f docker-compose.yaml -f docker-compose.dev.yaml

.PHONY: help up infra dev down clean logs ps psql build test tidy

help:
	@echo "  up      підняти весь стек"
	@echo "  infra   лише база та моніторинг"
	@echo "  dev     гарячий релоад: air + vite"
	@echo "  down    зупинити стек"
	@echo "  clean   зупинити і видалити томи з даними"
	@echo "  logs    дивитися логи"
	@echo "  ps      стан контейнерів"
	@echo "  psql    psql у контейнері бази"
	@echo "  build   зібрати бінарник локально"
	@echo "  test    прогнати тести"
	@echo "  tidy    упорядкувати залежності"

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

build:
	go build -o bin/app ./cmd/app

test:
	go test ./... -race -cover

tidy:
	go mod tidy
