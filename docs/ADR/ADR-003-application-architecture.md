# ADR-003: Архитектура приложения (обзорный снимок)

**Дата:** 2026-05-26  
**Статус:** Accepted  

---

## 1. Стартовая последовательность

### Точка входа

Единственный `main()` — `cmd/server/main.go`; сборка модулей — `cmd/server/app.go`. Функций `init()` в проекте нет.

### Конфигурация

Источник — **единый JSON-файл** `data/config.json` (`internal/config`). Переменные окружения не описывают настройки, а только указывают путь к файлу.

| Что | Как |
|-----|-----|
| Путь к конфигу | `SCOUT_CONFIG_PATH`, по умолчанию `data/config.json` (`config.ResolveConfigPath`) |
| Первый запуск | `config.Bootstrap` создаёт файл с дефолтами (`internal/config/defaults.go`) |
| Правка из UI | `PUT /api/settings` → `config.Store` пишет файл атомарно (temp + rename) |
| Hot-reload | `configStore.Subscribe`: смена наблюдателя пересобирает `Observer` в `PassService`/`TrackingService` и сбрасывает кеш пролётов; смена темы уходит в SSE `theme_changed`. Остальные секции требуют перезапуска |

Секции конфига (`internal/config/types.go`): `server`, `dev_mode`, `station` (observer + `radio_paths`), `tle`, `satnogs`, `ui`, `exclude_norad_file`. Схема станции и вычисление `station_type` — ADR-004 § 2.2.

### Порядок инициализации

По `cmd/server/main.go` → `app.NewApp()` → `app.Run()`:

```
NewApp:
 1. setupLogger() — slog
 2. loadConfig() — config.Bootstrap(ResolveConfigPath()): чтение или создание data/config.json
 3. resolveAssets() — templates/static (диск в dev_mode или embed)
 4. TLEStore — конструктор (без загрузки)
 5. SSEHub — конструктор
 6. Observer — координаты из station.observer
 7. SatelliteTrackingService — конструктор
 8. exclude.Store — чтение файла исключённых NORAD ID
 9. PassService + WithExcluder; trackingService.SetPassProvider / WithExcluder
10. ReceiverHub — регистрация radio_path_N по station.radio_paths (lazy open, реальный приёмник создаётся при первом обращении)

Run:
11. signal.NotifyContext — SIGINT/SIGTERM
12. setupRoutes() + newServer()
13. net.Listen — порт занимается ДО загрузки TLE: если занят, выходим сразу
14. TLEStore.Start() — синхронная загрузка с Celestrak (блокирует ~6 с)
15. go sseHub.Run(ctx)
16. setupClientState() — on-connect callback: client_state_restore
17. go tracking.Run(ctx) — тикеры 1 с / 2 мин / 5 с
18. startSatNOGS(ctx) — если включён: client + service + adapter + go TxCycleMock.Run(ctx)
19. startHotReload() — подписка на изменения конфига
20. server.Serve(ln)
```

Shutdown в обратном порядке: `ReceiverHub.CloseAll()` → `TLEStore.Stop()` → ожидание `SSEHub.Done()` (лимит 5 с) → `server.Shutdown` (лимит 30 с).

### Граф зависимостей сервисов

```
ConfigStore ──→ Config ──→ TLEStore ──→ PassService ──→ TrackingService
                  │                          │              ↑    ↑   ↑
                  ├──→ Observer ─────────────┴──────────────┘    │   │
                  │                                              │   │
                  ├──→ exclude.Store ────────────────────────────┘   │
                  │         (PassService + TrackingService)           │
                  │                                                   │
                  ├──→ SSEHub ───────────────────────────────────────┘
                  │        ↑
                  │        └──── TxCycleMock (mock tx_cycle для Авто)
                  │
                  ├──→ SatNOGS (optional) ───TransmitterProvider──→ TrackingService
                  │
                  └──→ ReceiverHub (radio_paths, lazy open) ──→ sdr.Receiver
```

### Файлы, читаемые при старте

| Файл | Когда |
|------|-------|
| `data/config.json` | Всегда; создаётся с дефолтами, если отсутствует |
| `templates/`, `static/` | `dev_mode = true` и папки существуют (иначе embed) |
| Файл из `exclude_norad_file` | Всегда; список исключённых NORAD ID |
| `data/tle_cache/{group}.tle` | Fallback при недоступности Celestrak |
| `data/tle_cache/cache_meta.json` | Метаданные кеша |

### Внешние API при старте

| API | Метод | Блокирует старт? |
|-----|-------|------------------|
| Celestrak (`gp.php?GROUP=...&FORMAT=TLE`) | Синхронный, 3 группы с rate limit 2с | **Да** (~6+ сек) |
| SatNOGS DB | Асинхронный, по запросу из tracking | Нет |

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
| GET | `/api/satnogs/transmitters/{norad}` | Передатчики КА (только если SatNOGS включён) |
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
└─────────────────────────┼──────────────────────────────────────┘
                          │
           ┌──────────────┼──────────────┐
           ▼                             ▼
┌───────────────────┐         ┌───────────────────┐
│    Celestrak       │         │   SatNOGS DB      │
│ TLE (6h refresh)   │         │ Transmitters      │
│ gp.php?GROUP=...   │         │ (24h TTL cache)   │
└───────────────────┘         └───────────────────┘
```

---

## 3. Хранение данных

### Backend — всё в памяти

| Компонент | Структура | TTL / обновление |
|-----------|-----------|------------------|
| `TLEStore` | `map[int]*TLE` + индексы по группам и именам | Перезагрузка каждые 6 ч |
| `PassService` | `map[string]*cacheEntry` (ключ: `group:hours:minEl`) | TTL **5 мин** |
| `SatelliteTrackingService` | `map[int]*trackedSatellite` + `lastTracks` | Реальное время (1с/5с/30с) |
| `ClientStateStore` | `map[string]*clientState` (client_id → tracking_id) | TTL 24ч (Cleanup не вызывается) |
| `SatNOGSService` | `map[int]*cacheEntry` (norad → transmitters) | TTL **24 ч**, retry **5 мин** → exp backoff до 2ч |
| `SSEHub` | `map[string]SSEEvent` (кеш последних событий) | Перезапись при каждом broadcast |

**Файлы на диске (вся персистентность):**

| Файл | Назначение |
|------|------------|
| `data/config.json` | Конфигурация станции; пишется атомарно при `PUT /api/settings` |
| Файл из `exclude_norad_file` | Исключённые NORAD ID (правится из UI) |
| `data/tle_cache/{group}.tle` | TLE по группам — fallback при недоступности Celestrak |
| `data/tle_cache/cache_meta.json` | Метаданные: `updated_at`, `count` |

**БД нет.** При рестарте теряются: pass cache, client state, SatNOGS cache, propagators. TLE восстанавливается с Celestrak или из файлового кеша. Решение перейти на SQLite принято (§ 7), но **не реализовано** — задача `PERSIST-001`.

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

### Celestrak (TLE)

| Параметр | Значение |
|----------|----------|
| URL | `https://celestrak.org/NORAD/elements/gp.php?GROUP={group}&FORMAT=TLE` |
| Группы по умолчанию | `stations`, `amateur`, `cubesat` |
| Частота | При старте + каждые **6 часов** |
| Rate limit | **2 с** между запросами |
| Таймаут HTTP | **30 с** |
| Retry | До **3** доп. попыток, exponential backoff (1с, 2с, 4с) |
| Fallback | Файловый кеш `data/tle_cache/{group}.tle` |
| Обработка ошибок | 404 — не ретраится; 429/5xx — ретраи; при полном отказе — работа со старыми данными |

### SatNOGS DB (передатчики)

| Параметр | Значение |
|----------|----------|
| URL | `https://db.satnogs.org/api/transmitters/?satellite__norad_cat_id={id}&format=json` |
| Частота | On-demand при `satellite_group_update` (~каждые 5 с проверка) |
| TTL кеша | **24 часа** |
| Rate limit | **1 с** между запросами |
| Retry | **5 мин** base → exp backoff до **2 ч** |
| Fallback | Stale data из кеша (UI не мерцает) |
| Отключение | `satnogs.enabled = false` в `data/config.json` — сервис не создаётся (вместе с ним не запускается `TxCycleMock`) |

---

## 5. Тикеры и интервалы (сводная)

| Компонент | Интервал | Уровень |
|-----------|----------|---------|
| Позиции SSE | **1 с** | Backend → все клиенты |
| Наземные трассы | **2 мин** | Backend → все клиенты |
| Проверка группы | **5 с** | Backend (broadcast при изменениях) |
| `tx_cycle` (mock) | **5 с** | Backend → нижняя панель Авто |
| Pass cache | **5 мин** TTL | Backend |
| TLE обновление | **6 ч** | Backend → Celestrak |
| SatNOGS cache | **24 ч** TTL | Backend |
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

> **Статус реализации: не реализовано.** В коде нет ни `modernc.org/sqlite`, ни слоя repository — всё состояние в памяти (§ 3). Задача `PERSIST-001`.

**Выбор:** SQLite через `modernc.org/sqlite` (pure Go, без CGO).

**Обоснование:**
- SQL-совместимость — проще миграция на другую БД в будущем (PostgreSQL, MySQL)
- Реляционная модель подходит для телеметрии, истории сеансов, статистики
- ACID-транзакции — надёжность при крэшах
- WAL-режим — конкурентное чтение без блокировок
- Один файл `data/satellite-scout.db` — легко бэкапить
- Pure Go — кросс-компиляция без CGO-зависимостей

**Что будет храниться:**

| Таблица | Данные | Источник |
|---------|--------|----------|
| `client_state` | Состояние клиентов (группа, selected, tracking) | `ClientStateStore` |
| `satnogs_cache` | Кеш SatNOGS transmitters | `SatNOGS Service` |
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
