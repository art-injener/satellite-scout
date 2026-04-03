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

RUN mkdir -p /var/cache/satellite-scout/tle_cache

ENV DEV_MODE=false
ENV PORT=8080
ENV TLE_CACHE_DIR=/var/cache/satellite-scout/tle_cache

EXPOSE 8080

ENTRYPOINT ["satellite-scout"]
