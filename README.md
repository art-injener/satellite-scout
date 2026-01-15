# SatWatch

Кроссплатформенное приложение для отслеживания спутников CubeSat и SDR приёма/демодуляции сигналов.

![SatWatch Interface](docs/images/main.png)

## Возможности

- **Earth View** — интерактивная карта мира с отображением орбит спутников в стиле STSPLUS
- **Индикаторы азимута и угла места** — визуализация положения спутника относительно наблюдателя
- **Sky View** — азимутальная проекция неба
- **Info-панель** — координаты спутника, параметры орбиты, время UTC

## Технологии

**Backend:**
- Go
- REST API + SSE (Server-Sent Events)
- Go templates

**Frontend:**
- HTMX — динамические UI компоненты
- Vanilla JS + Canvas — визуализации (карта, индикаторы)
- CSS Grid — адаптивная вёрстка

## Запуск

```bash
# Сборка
make build

# Запуск
make run

# Открыть в браузере
# http://localhost:8080
```

## Структура проекта

```
├── cmd/server/          # Приложение
├── internal/
│   ├── config/          # Конфигурация
│   └── handlers/        # HTTP handlers
├── static/
│   ├── css/             # Стили
│   ├── js/              # JavaScript (earthview, azimuth, elevation)
│   └── data/            # GeoJSON данные береговых линий
└── templates/           # HTML шаблоны
```

## Лицензия

MIT
