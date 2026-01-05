.PHONY: build test lint run stop clean

APP_NAME=satwatch
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

## run: Запустить приложение в фоне
run: build
	@if [ -f $(PID_FILE) ]; then \
		echo "Приложение уже запущено (PID: $$(cat $(PID_FILE)))"; \
	else \
		$(BINARY) & echo $$! > $(PID_FILE); \
		echo "✓ Приложение запущено (PID: $$(cat $(PID_FILE)))"; \
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
