.PHONY: build test lint lint-js lint-fix fmt run run-classic stop clean docker-build docker-up docker-down docker-logs

APP_NAME=satellite-scout
BUILD_DIR=./build
BINARY=$(BUILD_DIR)/$(APP_NAME)
PID_FILE=$(BUILD_DIR)/$(APP_NAME).pid

## build: Собрать приложение
build:
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BINARY) ./cmd/server
	@echo "✓ Приложение собрано: $(BINARY)"

## test: Запустить тесты с покрытием
test:
	@go test -cover ./...

## lint: Проверить код линтером
lint:
	@golangci-lint run --timeout=2m

## lint-fix: Проверить и автоматически исправить что можно
lint-fix:
	@golangci-lint run --fix --timeout=2m

## fmt: Отформатировать код (goimports + golines)
fmt:
	@golangci-lint fmt
	@echo "✓ Код отформатирован"

## lint-js: Проверить JS линтером (ESLint)
lint-js:
	@npm run lint:js

## run: Запустить приложение в фоне (THEME из окружения, иначе default)
run: build
	@if [ -f $(PID_FILE) ]; then \
		echo "Приложение уже запущено (PID: $$(cat $(PID_FILE)))"; \
	else \
		THEME="$${THEME:-default}" $(BINARY) & echo $$! > $(PID_FILE); \
		echo "✓ Приложение запущено (PID: $$(cat $(PID_FILE)), THEME=$${THEME:-default})"; \
	fi

## run-classic: То же, со старой цветовой схемой (colors-classic.css)
run-classic: build
	@if [ -f $(PID_FILE) ]; then \
		echo "Приложение уже запущено (PID: $$(cat $(PID_FILE)))"; \
	else \
		THEME=classic $(BINARY) & echo $$! > $(PID_FILE); \
		echo "✓ Запущено с THEME=classic (PID: $$(cat $(PID_FILE)))"; \
	fi

## stop: Остановить приложение
stop:
	@if [ -f $(PID_FILE) ]; then \
		kill $$(cat $(PID_FILE)) 2>/dev/null || true; \
		rm -f $(PID_FILE); \
		echo "✓ Приложение остановлено"; \
	else \
		echo "Приложение не запущено"; \
	fi

## clean: Очистить build артефакты
clean:
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "✓ Очистка завершена"

## docker-build: Собрать Docker-образ
docker-build:
	@docker compose build
	@echo "✓ Docker-образ собран"

## docker-up: Запустить через Docker Compose
docker-up:
	@docker compose up -d
	@echo "✓ Контейнер запущен"

## docker-down: Остановить контейнер
docker-down:
	@docker compose down
	@echo "✓ Контейнер остановлен"

## docker-logs: Показать логи контейнера
docker-logs:
	@docker compose logs -f
