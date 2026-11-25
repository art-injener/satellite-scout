# ADR-003: Архитектура приложения (обзорный снимок)

**Дата:** 2026-05-26  
**Статус:** Accepted  
**Ревизия:** 2026-11-21 — рантайм читает реестр / `data/scout.db` (ADR-011); внешние API только наполняют реестр, не обслуживают UI напрямую

---

## 1. Стартовая последовательность

### Точка входа

Единственный `main()` — `cmd/server/main.go`; сборка модулей — `cmd/server/app.go`. Функций `init()` в проекте нет.

### Конфигурация

Источник — **единый JSON-файл** `data/config.json` (`internal/config`). Переменные окружения не описывают настройки, а только указывают путь к файлу.

| Что | Как |
|-----|-----|
| Путь к конфигу | `SCOUT_CONFIG_PATH`, по умолчанию `data/config.json` (`config.ResolveConfigPath`) |
| Уровень логов | `SCOUT_LOG_LEVEL` = `debug`/`info`/`warn`/`error` (по умолчанию `info`; `trace` = `debug`) |
| Первый запуск | `config.Bootstrap` создаёт файл с дефолтами (`internal/config/defaults.go`) |
| Правка из UI | `PUT /api/settings` → `config.Store` пишет файл атомарно (temp + rename) |
| Hot-reload | `configStore.Subscribe`: смена наблюдателя пересобирает `Observer` в `PassService`/`TrackingService` и сбрасывает кеш пролётов; смена темы уходит в SSE `theme_changed`. Остальные секции требуют перезапуска |

Секции конфига (`internal/config/types.go`): `server`, `dev_mode`, `station` (observer + `radio_paths`), `tle`, `satnogs`, `ui`, `exclude_norad_file`. Схема станции и вычисление `station_type` — ADR-004 § 2.2. Каталог КА и передатчиков — ADR-011 (реестр + `data/scout.db`).

### Порядок инициализации

По `cmd/server/main.go` → `app.NewApp()` → `app.Run()` (схема; детали в коде):

```
NewApp:
 1. setupLogger() — slog
 2. loadConfig() — config.Bootstrap(ResolveConfigPath()): чтение или создание data/config.json
 3. resolveAssets() — templates/static (диск в dev_mode или embed)
 4. openRegistry() — data/scout.db (или память, если файл недоступен)
 5. TLEStore — конструктор; sink каталога пишет в реестр
 6. SSEHub — конструктор
 7. Observer — координаты из station.observer
 8. SatelliteTrackingService — конструктор
 9. exclude.Store + passExcluder(реестр) — ручные исключения и сошедшие КА
10. PassService + TrackingService; TransmitterProvider = реестр / план частот
11. ReceiverHub — регистрация radio_path_N по station.radio_paths (lazy open)

Run:
12. signal.NotifyContext — SIGINT/SIGTERM
13. setupRoutes() + newServer()
14. net.Listen — порт до загрузки каталога: если занят, выходим сразу
15. TLEStore.Start() — каталог/орбиты (Space-Track и запасные источники; пишет в реестр)
16. go sseHub.Run(ctx)
17. setupClientState() — on-connect: client_state_restore
18. go tracking.Run(ctx) — тикеры 1 с / 2 мин / 5 с
19. startFreqPlan / догрузка пробелов (SatNOGS и др.) — фоновое наполнение реестра
20. startHotReload() — подписка на изменения конфига
21. server.Serve(ln)
```

Shutdown в обратном порядке: `ReceiverHub.CloseAll()` → `TLEStore.Stop()` → ожидание `SSEHub.Done()` (лимит 5 с) → `server.Shutdown` (лимит 30 с).

### Граф зависимостей сервисов

```
ConfigStore ──→ Config ──→ TLEStore ──→ PassService ──→ TrackingService
                  │             │            │              ↑    ↑   ↑
                  │             ▼            │              │    │   │
                  │        Registry ◄────────┼──────────────┘    │   │
                  │      (scout.db)          │                   │   │
                  ├──→ Observer ─────────────┴───────────────────┘   │
                  │                                                   │
                  ├──→ exclude.Store ─────────────────────────────────┘
                  │
                  ├──→ SSEHub ◄── TxCycleMock (mock tx_cycle для Авто)
                  │
                  ├──→ ingest (Space-Track, sonik, HA, AMSAT, SatNOGS gaps)
                  │         └─ пишет в Registry; UI/SSE читают только Registry
                  │
                  └──→ ReceiverHub (radio_paths, lazy open) ──→ sdr.Receiver
```

### Файлы, читаемые при старте

| Файл | Когда |
|------|-------|
| `data/config.json` | Всегда; создаётся с дефолтами, если отсутствует |
| `data/scout.db` | Реестр КА и передатчиков (ADR-011); без файла — память |
| `templates/`, `static/` | `dev_mode = true` и папки существуют (иначе embed) |
| Файл из `exclude_norad_file` | Всегда; список исключённых NORAD ID |
| `data/tle_cache/*`, JSON-кеши источников | Сырые снимки ответов; реестр восстанавливается без сети |

### Внешние API (наполнение реестра, не UI)

| API | Роль | Блокирует старт? |
|-----|------|------------------|
| Space-Track GP / SATCAT | Каталог и орбиты → реестр | Да (загрузка каталога) |
| Celestrak | Запасной источник орбит по NORAD | По необходимости |
| sonik / HA / AMSAT / curated | Имена, живость, передатчики → реестр | Нет (фон / кеш) |
| SatNOGS DB | Догрузка пробелов по NORAD → реестр | Нет |

---

## 2. Потоки данных

### SSE — основной канал (постоянное соединение)

**Endpoint:** `GET /api/sse?client_id=<uuid>`

| Событие | Данные | Частота |
|---------|--------|---------|
| `satellite_state_update` | `positions[]` (lat, lon, alt, az, el, range, visibility_zone, map_marker) + `tracks[]` (past/future) | Позиции: **1 с**; треки: **2 мин** |
| `satellite_group_update` | `satellites[]` (norad_id, name, alias, AOS/LOS, duration, freq/modulation) + `primary_id` + `tracking_id` | **~5 с** (только при изменениях) |
| `satellite_change` | `{norad_id, name, reason}` — reason: initial/auto/manual/tracking_ended | По событию |
| `tx_cycle` | `power`, `packets`, `snr_db`, `lock`, `history[]` по передатчику — нижняя панель Авто | **5 с** (пока mock, `TxCycleMock`) |
| `theme_changed` | `{theme}` — смена темы через настройки | По событию (hot-reload конфига) |
| `client_state_restore` | `{tracking_id}` — per-client восстановление | При подключении + после POST tracking |
| `connected` | `{status: "ok"}` | Однократно |

SSEHub кеширует последние `satellite_state_update` и `satellite_group_update` — новый клиент сразу получает актуальное состояние.
Контракт `tx_cycle` — ADR-002 § I.9; события режима «Анализ» (`frame_decoded`, `field_schema`, `analysis_state`) — ADR-007 § 6.2.

### REST API

По `cmd/server/routes.go`:

| Метод | Endpoint | Назначение |
|-------|----------|------------|
| GET | `/api/health` | Healthcheck |
| GET | `/api/config` | Наблюдатель, тема, `station_type` для UI |
| GET / PUT | `/api/settings` | Чтение и запись конфига из модалки настроек |
| GET | `/api/passes?group=&hours=&min_el=` | Расписание пролётов (polling 60 с) |
| POST | `/api/tracking/current` · `/api/tracking/reset` | Взять КА на сопровождение / сбросить |
| GET / POST / DELETE | `/api/exclusions`, `/api/exclusions/{norad}` | Список исключённых NORAD ID |
| GET | `/api/sdr/devices` · POST `/api/sdr/test` | Обнаружение и проверка SDR-приёмников |
| GET | `/api/satnogs/transmitters/{norad}` | Передатчики КА из реестра (имя пути историческое; сеть не дергается ради ответа) |
| GET | `/api/sse` | SSE-поток |

Страницы: `GET /` (индекс) и `GET /tracking`. Legacy-маршруты `/receiver`, `/simulation` и `/settings` — редиректы на `/tracking` (`/settings` → `/tracking?settings=open`). Маршрут `/analysis` (ADR-007) ещё не зарегистрирован — задача `MODE-ANALYSIS-001`.

Планируемые REST управления SDR (`/api/sdr/tune`, `/api/sdr/status`) и chunked-стримы `/api/stream/v1/*` — ADR-002 часть III, в коде их пока нет.

### HTMX

HTMX подключён (`htmx.min.js` + `htmx-sse.js`), но **фактически не используется** для обмена данными. Реальная динамика — SSE + vanilla JS.

### HTTP Chunked Stream (планируется)

Для бинарных SDR-данных (FFT, IQ, audio). Протокол: 4-байтовый заголовок длины + данные. См. ADR-002. Пока спектр/водопад генерируются локально на фронтенде (`SpectrumDataSource`).

### Схема потоков

```
┌─────────────────────────────────────────────────────────────────┐
│                          BROWSER                                │
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐  ┌──────────────┐  │
│  │ EarthView│  │ SkyView  │  │BottomPanel│  │ RightPanel   │  │
│  │ (Canvas) │  │ (Canvas) │  │ (Canvas)  │  │ (DOM/Table)  │  │
│  └────┬─────┘  └────┬─────┘  └─────┬─────┘  └──────┬───────┘  │
│       └──────────────┴──────────────┴───────────────┘          │
│                             │                                   │
│                   ┌─────────┴──────────┐                       │
│                   │  StateManager      │                       │
│                   │  selectedId,       │                       │
│                   │  trackingId        │                       │
│                   └─────────┬──────────┘                       │
│                             │                                   │
│              ┌──────────────┼──────────────┐                   │
│              │ EventSource  │  fetch()     │                   │
│              │ /api/sse     │  /api/*      │                   │
└──────────────┼──────────────┼──────────────┼───────────────────┘
               │              │              │
               ▼              ▼              ▼
┌─────────────────────────────────────────────────────────────────┐
│                   Go HTTP Server (:8088)                        │
│                                                                 │
│  ┌──────────┐  ┌──────────────────┐  ┌─────────────────────┐  │
│  │ SSE Hub  │  │ REST Handlers    │  │ Template Renderer   │  │
│  │ /api/sse │  │ /api/passes      │  │ Go templates + HTMX │  │
│  │          │  │ /api/tracking/*  │  │                     │  │
│  └────┬─────┘  └───────┬──────────┘  └─────────────────────┘  │
│       │                │                                       │
│  ┌────┴────────────────┴────────────────────────────────┐     │
│  │          SatelliteTrackingService                     │     │
│  │  ┌──────────────┐ ┌────────────┐ ┌────────────────┐  │     │
│  │  │ GroupManager  │ │PassService │ │ClientStateStore│  │     │
│  │  └──────┬───────┘ └─────┬──────┘ └────────────────┘  │     │
│  └─────────┼───────────────┼────────────────────────────┘     │
│            │               │                                   │
│  ┌─────────┴───────────────┴─────────────────┐                │
│  │              TLEStore                      │                │
│  │  ┌───────────────┐  ┌─────────────────┐   │                │
│  │  │SGP4Propagator │  │ PassPredictor   │   │                │
│  │  └───────────────┘  └─────────────────┘   │                │
│  └─────────────────────┬─────────────────────┘                │
│                        │                                      │
│  ┌─────────────────────┴─────────────────────┐                │
│  │         Registry (память + scout.db)      │                │
│  │  КА, орбиты, передатчики, метки           │                │
│  └─────────────────────┬─────────────────────┘                │
└─────────────────────────┼──────────────────────────────────────┘
                          │ ingest (фон)
           ┌──────────────┼──────────────┐
           ▼              ▼              ▼
┌──────────────┐  ┌─────────────┐  ┌──────────────────┐
│ Space-Track  │  │ sonik / HA  │  │ SatNOGS (пробелы)│
│ GP + SATCAT  │  │ AMSAT / …   │  │ → только в реестр│
└──────────────┘  └─────────────┘  └──────────────────┘
```

UI, SSE и план частот читают **реестр**, не внешние API. Подробности слияния — ADR-011.

---

## 3. Хранение данных

### Backend — память + реестр в SQLite

| Компонент | Структура | TTL / обновление |
|-----------|-----------|------------------|
| `Registry` | КА / орбиты / TX в памяти, снимок в `data/scout.db` | Ingest источников; читают все потребители |
| `TLEStore` | пропагаторы и индексы по каталогу реестра | Обновление орбит по расписанию ingest |
| `PassService` | `map[string]*cacheEntry` (ключ: `group:hours:minEl`) | TTL **5 мин** |
| `SatelliteTrackingService` | `map[int]*trackedSatellite` + `lastTracks` | Реальное время (1с/5с/30с) |
| `ClientStateStore` | `map[string]*clientState` (client_id → tracking_id) | TTL 24ч (Cleanup не вызывается) |
| `SatNOGSService` | очередь догрузки пробелов → реестр | Не источник для UI |
| `SSEHub` | `map[string]SSEEvent` (кеш последних событий) | Перезапись при каждом broadcast |

**Файлы на диске:**

| Файл | Назначение |
|------|------------|
| `data/config.json` | Конфигурация станции; пишется атомарно при `PUT /api/settings` |
| `data/scout.db` | Реестр КА и передатчиков (ADR-011); журнал визитов |
| Файл из `exclude_norad_file` | Исключённые NORAD ID (правится из UI) |
| `data/tle_cache/*`, JSON-кеши источников | Сырые снимки; реестр поднимается без сети |

При рестарте теряются: pass cache, client state, пропагаторы. Каталог и TX восстанавливаются из `scout.db` и кешей источников. Расширение персистентности (сессии, телеметрия) — § 7 / `PERSIST-001`.

### Frontend — in-memory + localStorage

| Компонент | Хранение | Данные |
|-----------|----------|--------|
| `SatelliteStateManager` | Singleton, RAM | `_satellites` (Map), `selectedId`, `trackingId`, `_colorMap`, `_visibleTrackIds` |
| EarthView/SkyView | Canvas state | Позиции маркеров, трассы, coastline/land GeoJSON |
| ManualLayout | Canvas state | FFT, waterfall, Az/El (Ручной режим) |
| `localStorage` | Браузер | `ss-ui-theme`, `ux.bottomCollapsed`, `ux.rightCollapsed` |
| `sessionStorage` | Per-tab | `satellite-scout-client-id` (UUID для SSE), `satellite-scout-selected-id` |

---

## 4. Внешние интеграции

### Каталог и орбиты (ingest → реестр)

Основной путь — Space-Track GP + SATCAT в реестр (ADR-011). Celestrak — запасной источник орбит по NORAD (не разбиение UI на группы `stations`/`amateur`/`cubesat`). При отказе сети станция работает по `scout.db` и файловым кешам.

### SatNOGS DB (догрузка пробелов → реестр)

| Параметр | Значение |
|----------|----------|
| URL | `https://db.satnogs.org/api/transmitters/?satellite__norad_cat_id={id}&format=json` |
| Роль | Только КА без передатчиков/статуса в реестре (ADR-011 §2.5); не источник для SSE/таблиц |
| Rate limit | **1 с** между запросами, новое соединение на запрос |
| Отключение | `satnogs.enabled = false` — фоновая догрузка не стартует; реестр и UI продолжают работать |

---

## 5. Тикеры и интервалы (сводная)

| Компонент | Интервал | Уровень |
|-----------|----------|---------|
| Позиции SSE | **1 с** | Backend → все клиенты |
| Наземные трассы | **2 мин** | Backend → все клиенты |
| Проверка группы | **5 с** | Backend (broadcast при изменениях) |
| `tx_cycle` (mock) | **5 с** | Backend → нижняя панель Авто |
| Pass cache | **5 мин** TTL | Backend |
| Обновление орбит / каталога | по расписанию ingest | Backend → реестр |
| Догрузка пробелов SatNOGS | по очереди пробелов | Backend → реестр |
| SSE reconnect | **1–30 с** exp backoff | Frontend |
| Таблица `/passes` | **60 с** polling | Frontend → REST |
| Countdown таблица | **1 с** | Frontend (локально) |
| Спектр/водопад | **80 мс** | Frontend (симуляция) |
| Footer часы | **1 с** | Frontend (локально) |

---

## 6. Ревью архитектуры (вынесено в архив)

Разделы «Известные пробелы и неиспользуемые механизмы», «Предложения по улучшению архитектуры» и «Варианты персистентности» перенесены в `docs/archive/plans/adr-003-review-2026-05-26.md`. Это ревью кода на дату снимка, а не архитектурные решения.

---

## 7. Принятое решение: SQLite

> **Статус (2026-11-21):** реестр КА и передатчиков уже в `data/scout.db` (`internal/registry`, ADR-011 / `TX-DB-001`). Ниже — расширение персистентности (`PERSIST-001`): сессии, телеметрия, client state.

**Выбор:** SQLite через `modernc.org/sqlite` (pure Go, без CGO).

**Обоснование:**
- SQL-совместимость — проще миграция на другую БД в будущем (PostgreSQL, MySQL)
- Реляционная модель подходит для телеметрии, истории сеансов, статистики
- ACID-транзакции — надёжность при крэшах
- WAL-режим — конкурентное чтение без блокировок
- Один файл `data/scout.db` — легко бэкапить
- Pure Go — кросс-компиляция без CGO-зависимостей

**Уже в `scout.db` (реестр):** КА, орбиты, передатчики, метки, загрузки источников, визиты — ADR-011.

**Что ещё планируется хранить (`PERSIST-001`):**

| Таблица | Данные | Источник |
|---------|--------|----------|
| `client_state` | Состояние клиентов (группа, selected, tracking) | `ClientStateStore` |
| `sessions` | История сеансов приёма | Ручной режим (ADR-004) |
| `telemetry` | Декодированные пакеты телеметрии | DEMOD-003 |
| `detections` | Журнал детектированных передач | Режим Обзор (ADR-004) |
| `station_config` | Схема станции (JSON blob или normalized) | STATION-001 |

**Паттерн repository:**

```go
type Repository interface {
    // Client state
    SaveClientState(ctx context.Context, clientID string, state *ClientState) error
    LoadClientState(ctx context.Context, clientID string) (*ClientState, error)
    
    // Sessions
    CreateSession(ctx context.Context, s *Session) error
    ListSessions(ctx context.Context, filter SessionFilter) ([]*Session, error)
    
    // Telemetry
    InsertTelemetry(ctx context.Context, t *TelemetryPacket) error
    QueryTelemetry(ctx context.Context, filter TelemetryFilter) ([]*TelemetryPacket, error)
    
    // Detections
    InsertDetection(ctx context.Context, d *Detection) error
    ListDetections(ctx context.Context, since time.Time) ([]*Detection, error)
}
```

**Миграции:** `golang-migrate/migrate` или встроенный механизм (SQL-файлы в `migrations/`).

**Задача:** PERSIST-001 в PHASE-9.
