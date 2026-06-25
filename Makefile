.PHONY: help up down logs build test fmt env

help:
	@echo "Команды:"
	@echo "  make env    - создать .env из .env.example (если нет)"
	@echo "  make up     - поднять весь стек (docker compose up --build)"
	@echo "  make down   - остановить стек"
	@echo "  make logs   - логи всех сервисов"
	@echo "  make build  - собрать образы"
	@echo "  make test   - прогнать тесты всех Go-сервисов локально (go test)"

env:
	@test -f .env || cp .env.example .env && echo ".env готов"

up: env
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f

build:
	docker compose build

# Прогон тестов всех Go-компонентов без docker
test:
	@for d in services/* mocks/*; do \
		if [ -f $$d/go.mod ]; then \
			echo "== tests: $$d =="; \
			(cd $$d && go test ./... || exit 1); \
		fi; \
	done
