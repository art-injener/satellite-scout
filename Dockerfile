# --- Этап 1: сборка ---
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Зависимости кешируются отдельно от исходников.
COPY go.mod go.sum ./
RUN go mod download

# Исходники + шаблоны/статика (нужны для go:embed).
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /satellite-scout ./cmd/server

# --- Этап 2: минимальный образ ---
FROM alpine:3.21

# CA-сертификаты для HTTPS-запросов к Celestrak.
RUN apk add --no-cache ca-certificates

COPY --from=builder /satellite-scout /usr/local/bin/satellite-scout

# Единое место для пользовательских данных: config.json, кеш TLE, список исключений.
# Содержимое монтируется через volume в docker-compose.yml.
WORKDIR /app
RUN mkdir -p /app/data/tle_cache

# Только две переменные окружения, признанных архитектурой долгосрочно. Все
# прочие настройки живут в /app/data/config.json и редактируются через UI.
ENV DEV_MODE=false
ENV SS_CONFIG=/app/data/config.json

EXPOSE 8080

ENTRYPOINT ["satellite-scout"]
