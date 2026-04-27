# ADR-002: Транспорт бинарных данных (SDR-стримы) backend → frontend

**Дата создания:** 2026-04-26  
**Дата исходного решения:** 2026-04-14 (HTTP Chunked Stream, см. `memory-bank/techContext.md` и `memory-bank/activeContext.md`)  
**Статус:** 🟡 **Draft** — обсуждение не завершено, см. § «Открытые вопросы»  
**Связанные задачи:** UX-OVERVIEW-BACKEND-001, SDR-001, SDR-002, SDR-003, SDR-005

---

## Описание задачи

Satellite Scout получает с SDR-приёмника большой бинарный поток (IQ-семплы), на основе которого формируются:

- **FFT-спектр** (для отображения в нижней панели «Обзор» / «Сопровождение»);
- **Waterfall** (накопительная развёртка спектра во времени);
- **Демодулированное аудио** (для прослушивания оператором);
- **Декодированная телеметрия** (через демодулятор → AX.25 → ТМИ).

Параметры SDR — переменные:

| Параметр | Диапазон |
|----------|----------|
| Полоса / Sample rate | 2 МSPS (стартовая) — 10 МSPS (типовая) — 150 МSPS (high-end) |
| Разрядность АЦП IQ | 8 бит / 16 бит |
| Частота отсчётов на UI | 15-30 кадров/сек |

При полосе 150 МSPS × 16-bit IQ сырой поток = **600 МБ/с (4.8 Гбит/с)**. Передавать такое на UI невозможно и **бессмысленно** — браузер не отобразит, глаз не различит. Требуется архитектура, разделяющая «сырьё для DSP» и «картинку для оператора».

Транспортный канал должен:

1. Доставлять **бинарные** данные (FFT, IQ, PCM) с минимальным оверхедом.
2. Быть **самодостаточным** — каждый кадр интерпретируется независимо (метаданные внутри).
3. Поддерживать **разные параметры SDR** на лету (смена частоты, полосы, коэффициента усиления) без переустановки соединения.
4. Быть **устойчивым** к разрывам сети, рестарту бэкенда, переключению вкладок браузера.
5. Быть **отлаживаемым** — поток можно записать в файл и проанализировать без знания контекста запроса.
6. Жить **рядом с SSE** (текстовые события), не пытаясь заменить его.

---

## Принятые решения (сводка)

| # | Тема | Решение | Статус |
|---|------|---------|--------|
| 1 | Транспорт | HTTP Chunked Stream (`fetch` + `ReadableStream`), отдельно от SSE | ✅ Принято 2026-04-14 |
| 2 | Endianness | **Little-endian** для всего (заголовок и payload) | ✅ Принято 2026-04-26 |
| 3 | Метаданные | **Self-describing frames** — все параметры в заголовке каждого фрейма (по принципу VITA-49 / VRT) | ✅ Принято 2026-04-26 |
| 4 | Версионирование | `magic` + `version` в заголовке + `/api/stream/v1/...` в URL | ✅ Принято 2026-04-26 |
| 5 | Heartbeat | Кадр с `data_type=0xFE`, `payload_len=0` каждые 15 сек простоя | ✅ Принято 2026-04-26 |
| 6 | Sequence number | `uint32 seq` в заголовке (per-stream, wrap-around) — для детекции потерь | ✅ Принято 2026-04-26 |
| 7 | Display Reducer | Аккумуляция/децимация на бэкенде; UI получает ≤100 КБ/с | 🟡 **Принят принцип**, детали в обсуждении |
| 8 | Демодулятор | Живёт **на бэкенде**, потребляет raw IQ напрямую; на UI — только распакованные пакеты ТМИ через SSE | 🟡 К подтверждению |

---

## Часть I. Решения и обоснования

### 1. Транспорт: HTTP Chunked Stream (a не SSE/WebSocket/gRPC)

**Решение:** два независимых канала, каждый под свой тип данных.

| Канал | Протокол | Тип данных | Когда активен |
|-------|----------|------------|---------------|
| **SSE** (`EventSource`) | text/event-stream (UTF-8, JSON) | Позиции КА, пролёты, статусы, группа, ТМИ | Всегда, пока страница открыта |
| **HTTP Chunked Stream** (`fetch` + `ReadableStream`) | application/octet-stream | FFT, IQ, PCM-аудио | По запросу, только пока активна вкладка |

**Отвергнутые альтернативы:**

| Альтернатива | Почему отвергнуто |
|--------------|-------------------|
| **SSE для всех данных** | SSE — текстовый протокол; бинарь только через base64 (+33% оверхед). При waterfall 30 FPS × 4 КБ — нагрузка на encode/decode неоправданна. |
| **WebSocket** | Постоянное соединение даже когда данные не нужны. Нет авто-реконнекта (нужно писать руками, SSE делает сам). Нужна Go-библиотека (`gorilla/websocket`). Несовместим с HTMX. |
| **gRPC / gRPC-Web** | Требует прокси (Envoy/Connect). Избыточен для однонаправленного потока. Несовместим с HTMX. |
| **Полная замена SSE на Chunked Stream** | Потеря API `EventSource` (авто-реконнект, `Last-Event-ID`, именованные события). Потеря интеграции с HTMX (`sse-connect`, `sse-swap`). Пришлось бы писать свой SSE-подобный протокол поверх. |

**Преимущества выбранного подхода:**

- Сервер на Go: стандартный `net/http` + `http.Flusher`, без библиотек.
- Клиент на JS: `fetch` + `resp.body.getReader()` + `AbortController` для отмены.
- Соединение существует **только пока нужны данные** (открыта соответствующая вкладка).
- Обе стороны используют родные API без зависимостей.

### 2. Endianness: Little-endian везде

**Решение:** все многобайтовые поля (заголовок и payload) — в **little-endian**.

**Почему:**

- Это **внутренний** протокол между «нашим бэком» и «нашим фронтом», а не публичный сетевой протокол. Конвенция «network byte order = BE» нужна для совместимости с третьими сторонами, которых тут нет.
- x86, ARM, RISC-V — все LE. Сервер пишет `Float32Array` напрямую через `unsafe.Slice` + `w.Write` без перестановки байт. Клиент читает через `new Float32Array(buf)` тоже без перестановки.
- На FFT 512 bins × 4 байта × 30 FPS = ~60 КБ/с. При BE-payload пришлось бы делать `binary.Write(w, binary.BigEndian, ...)` для каждого float32 — 512 операций × 30 раз/сек = 15 360 операций/сек. На LE — `w.Write(buf)` одной операцией.
- Внутренняя консистентность: «всё в LE, потому что наш бэк и фронт оба на LE-архитектурах».

**Реализация:**

```go
// Сервер (Go) — пишем заголовок и данные одной операцией
binary.LittleEndian.PutUint32(headerBuf[36:40], seq)
binary.LittleEndian.PutUint32(headerBuf[32:36], uint32(len(data)))
// ... остальные поля
w.Write(headerBuf[:])
w.Write(data) // float32[N] уже в памяти в LE-порядке
flusher.Flush()
```

```javascript
// Клиент (JS) — читаем заголовок и данные без перестановки
const view = new DataView(headerBuf.buffer);
const magic       = view.getUint32(0, /*littleEndian=*/ true);
const timestampMs = Number(view.getBigInt64(8, true));
const centerHz    = Number(view.getBigInt64(16, true));
const payloadLen  = view.getUint32(32, true);
const seq         = view.getUint32(36, true);
const fft = new Float32Array(buffer, headerEnd, payloadLen / 4);
```

### 3. Метаданные: self-describing frames

**Решение:** все параметры (timestamp, центральная частота, ширина полосы, режим обработки, тип данных) передаются **в заголовке каждого фрейма**. Никаких config-фреймов в начале, никаких query-параметров для метаданных, никакой синхронизации с REST.

**Почему:**

| Проблема при «параметры через query/REST» | Решение через self-describing frames |
|------------------------------------------|--------------------------------------|
| Клиент переподключился — параметры устарели | Первый же фрейм после reconnect несёт актуальные `center_hz`, `span_hz`, `timestamp_ms` |
| Бэкенд упал и перезапустился с другой настройкой SDR | Новые фреймы автоматически несут новые параметры; клиент не знает разницы |
| Оператор сменил частоту через REST `/api/sdr/tune` — нужно синхронизировать с потоком | Следующий же фрейм несёт новый `center_hz`; синхронизация не нужна |
| Запись потока в файл для анализа — теряется контекст запроса | Файл самодостаточен: каждый фрейм можно интерпретировать в hex-редакторе или скрипте |
| Тестирование требует мокать query/REST + стрим одновременно | Тест: пишем фреймы в `bytes.Buffer`, читаем оттуда — чистая I/O |

**Прецедент в индустрии:** **VITA-49 (VRT)** — стандарт IEEE для SDR-стримов (US DoD, scientific instruments). Каждая VRT-запись несёт RF-метаданные: timestamp, center_freq, sample_rate, gain. Используется в военных и научных SDR-системах именно потому, что снимает все проблемы синхронизации.

**Оверхед:**

| Тип данных | Размер payload | Заголовок 40 байт | Оверхед |
|-----------|---------------|-------------------|---------|
| FFT 512 bins (`float32`) | 2048 байт | 40 байт | 1.9% |
| FFT 1024 bins (`float32`) | 4096 байт | 40 байт | 1.0% |
| Audio PCM 24kHz, 100мс (`int16`) | 4800 байт | 40 байт | 0.8% |
| IQ 1 Msps, 100мс (`complex64`) | 800 КБ | 40 байт | 0.005% |

Накладные расходы пренебрежимо малы.

### 4. Версионирование: magic + version

**Решение:** двухуровневое:

- **`/api/stream/v1/...`** в URL — для **breaking changes** на уровне API (изменилась структура заголовка, удалён endpoint, поменялась семантика).
- **`magic` + `version` в заголовке** — для верификации формата каждого фрейма; защищает от случайного «не того» формата (например, попытка отправить фрейм через старый endpoint).

**`magic = "SSTM"`** — 4 байта `0x4D 0x54 0x53 0x53` (в LE = `0x4D54_5353`). Расшифровка: **S**atellite **S**cout **T**ele**M**etry. Видны глазами в hex-дампе:

```text
$ xxd recording.bin | head -1
00000000: 5353 544d 0101 0000 ...
          |---| ||  ||
          SSTM v1  data_type=FFT_DB
```

При несовпадении magic — клиент закрывает соединение с ошибкой «protocol mismatch». Это даёт fail-fast при багах.

### 5. Heartbeat: фрейм с `data_type=0xFE`, `payload_len=0`

**Решение:** при отсутствии данных более 15 секунд сервер отправляет «пустой» фрейм с `data_type = 0xFE` (HEARTBEAT) и `payload_len = 0`. Клиент пропускает его (не пушит в waterfall), но соединение остаётся живым.

**Почему 15 сек:** типичные idle-таймауты обратных прокси (nginx, Cloudflare, AWS ALB) — 30-60 сек. Период 15 сек даёт двукратный запас.

**Зачем:** сценарий «ждём AOS, спутник появится через 5 минут, сигнала нет» — без heartbeat прокси убьёт TCP, клиент должен будет реконнектиться. С heartbeat — соединение остаётся.

**Альтернатива (отвергнута):** TCP keepalive. Не работает через прокси, которые часто отключают/игнорируют TCP keepalive. Прикладной heartbeat надёжнее.

### 6. Sequence number: `uint32 seq` per-stream

**Решение:** каждый фрейм нумеруется монотонно растущим `uint32 seq`, начиная с 0 при старте handler-а на сервере. При reconnect нумерация начинается заново с 0.

**Почему нужен:**

TCP гарантирует доставку байт в порядке, но **в нашем протоколе фреймы могут теряться**:

| Сценарий | Где теряются | Без счётчика | Со счётчиком |
|----------|--------------|--------------|--------------|
| Backpressure: канал клиента переполнен | Сервер дропает старый фрейм (см. § B1) | Молча | `seq=42, 43, 45` → пропустил 1 |
| Reconnect | Между разрывом и восстановлением | Молча | `seq=0` (новая сессия) |
| Бэк не успевает считать FFT | DSP-конвейер | Только jitter заметен | Конкретное число пропусков |
| Прокси выкинул кусок (теоретически) | Сетевой стек | Тихий desync | `seq` рассинхронизирован → re-sync по `magic` |

**Размер: `uint32`** — 2³² фреймов. При 30 FPS wrap наступит через **4.5 года** непрерывной работы. Wrap обрабатывается одной строкой `((next - prev) >>> 0)` — стандартный паттерн для unsigned int в JS.

**Семантика:**

- **Per-stream:** каждое открытие потока (`/spectrum`, `/iq`, `/audio`) имеет свой счётчик с нуля.
- **Heartbeat-фреймы тоже инкрементят счётчик** — это «честные» фреймы потока.
- **Сброс при reconnect:** клиент видит `seq=0` после ожидаемого `seq=N+1` → детектирует сброс сессии.

**Алгоритм клиента (псевдокод):**

```javascript
class BinaryStreamReader {
    constructor() {
        this._expectedSeq = null;
        this._stats = { framesReceived: 0, framesDropped: 0, sessions: 0 };
    }

    _onFrame(header, payload) {
        const got = header.seq;

        if (this._expectedSeq === null) {
            this._stats.sessions++;
            this._expectedSeq = (got + 1) >>> 0;
            return this._dispatch(header, payload);
        }

        const expected = this._expectedSeq;

        if (got === expected) {
            // Норма
            this._expectedSeq = (expected + 1) >>> 0;
        } else if (got === 0 && expected > 1000) {
            // Сервер начал новую сессию (reconnect, рестарт бэка)
            this._stats.sessions++;
            this._expectedSeq = 1;
        } else {
            const lost = ((got - expected) >>> 0);
            if (lost < 0x80000000) {
                // Прямой пропуск
                this._stats.framesDropped += lost;
                console.warn(`[stream] dropped ${lost} frames (expected ${expected}, got ${got})`);
                this._emit('framesDropped', { count: lost });
            } else {
                // Out-of-order (не должно случиться в TCP, но обработаем)
                console.warn(`[stream] out-of-order frame, ignoring`);
                return;
            }
            this._expectedSeq = (got + 1) >>> 0;
        }

        this._stats.framesReceived++;
        this._dispatch(header, payload);
    }
}
```

### 7. Display Reducer: обработка на бэкенде, UI — «картинка для глаз»

**Решение:** между SDR и UI на бэкенде встраивается **DSP-конвейер**, который превращает full-rate сырьё в обработанный поток ≤ 100 КБ/с.

```text
                                 ┌──► Демодулятор (на бэке, full IQ)
                                 │    Декодирует AX.25/FSK/etc → ТМИ
SDR ───► IQ Ring Buffer ─────────┤    Результат через SSE.
(2-150 МSPS, 8/16-bit IQ)        │
                                 └──► FFT Engine ──► Display Reducer ──► UI Stream
                                      (1000+ FFT/s)  (avg/maxhold/...)    (15-30 FPS)
                                                     (downsample bins)
                                                     (dB scale, нормал.)
```

**Почему обработка на бэке, а не на UI:**

- На 150 МSPS сырой поток = 4.8 Гбит/с. Браузер не прокачает.
- Глаз не различает > 30 FPS. UI 1000 FFT/s бесполезен.
- Canvas waterfall шириной 1200 пикселей физически не отобразит 65536 бинов FFT.
- Демодулятор живёт **на бэке** в Go-коде. Делать его в браузере на JS — отдельная библиотека, отдельные баги, дублирование математики.

**Что считается на бэке (Display Reducer):**

| Стадия | Что делает | Параметр |
|--------|-----------|----------|
| 1. Накопление в окне | Собирает N FFT-кадров за время `window_ms` | `window_ms = 1000/fps` |
| 2. Аккумуляция | Применяет `mode`: avg / maxhold / minhold / persistence | `mode` |
| 3. Bin downsampling | Сжимает full-FFT (например, 65536 бинов) до целевого числа (например, 1024) через `max()` для каждой группы | `bins` |
| 4. dB-конверсия | Переводит линейную мощность в dB | (всегда) |
| 5. Нормализация | Опц. вычитание шумового пола | (опц.) |

**Прецедент:** так работают все профессиональные спектроанализаторы (Agilent N9020 MXA, R&S FSW, Tektronix RSA). FFT гонит со скоростью 1000+ FFT/sec, отображение — 30 кадров/сек.

**Bin downsampling через `max()` (а не среднее):** сохраняет узкополосные сигналы. CW-маяк шириной 1 кГц при разрешении 18 кГц/UI-бин **остаётся виден** в виде пика (если бы было среднее — он бы «утонул»).

```go
// Downsampling FFT с сохранением пиков (Trace Detector: Peak в Agilent)
func downsampleMax(full []float32, targetBins int) []float32 {
    factor := len(full) / targetBins
    out := make([]float32, targetBins)
    for i := 0; i < targetBins; i++ {
        m := full[i*factor]
        for j := 1; j < factor; j++ {
            if full[i*factor+j] > m {
                m = full[i*factor+j]
            }
        }
        out[i] = m
    }
    return out
}
```

### 8. Демодулятор: на бэкенде

**Решение:** демодулятор (FSK / AFSK / GMSK / SDR-демод) живёт **на бэке** в Go, потребляет raw IQ напрямую из ring buffer SDR. На UI идут **только декодированные пакеты ТМИ через SSE**, не сырьё.

**Почему:**

- Демодулятор — это DSP, который ест мегабиты IQ в секунду. На UI это передавать нереально.
- Логика декодирования (clock recovery, bit stuffing, CRC, AX.25) — сложная математика, которой не место в браузере.
- Один источник истины: декодированный пакет получается один раз на сервере, рассылается всем подключённым клиентам через SSE.

Связь: SDR-001..004 (бэкенд DSP) → DEMOD-001..004 (демодулятор) → SSE-событие `tmi_packet`.

---

## Часть II. Структура фрейма (формат wire protocol v1)

### Общий вид

```text
┌─────────────────────────────────────────────────────────────────────┐
│ Header (40 bytes, fixed, all fields little-endian)                  │
├─────────────────────────────────────────────────────────────────────┤
│ Off  Type      Size  Поле           Описание                        │
├─────────────────────────────────────────────────────────────────────┤
│   0  uint32     4    magic          0x4D54_5353 ("SSTM" в LE)       │
│   4  uint8      1    version        Версия формата (сейчас 1)       │
│   5  uint8      1    data_type      Тип payload (см. таблицу)       │
│   6  uint8      1    display_mode   Режим обработки (см. таблицу)   │
│   7  uint8      1    reserved       Резерв (0)                      │
│   8  int64      8    timestamp_ms   Unix миллисекунды UTC,          │
│                                     момент съёма первого семпла     │
│  16  int64      8    center_hz      Центральная частота, Гц         │
│  24  int64      8    span_hz        Ширина полосы, Гц               │
│  32  uint32     4    payload_len    Длина payload в байтах          │
│  36  uint32     4    seq            Счётчик фреймов потока (с 0)    │
├─────────────────────────────────────────────────────────────────────┤
│ Payload (payload_len байт, формат по data_type)                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Поле `data_type`

| Значение | Имя | Формат payload |
|----------|-----|----------------|
| `0x01` | `FFT_DB` | `float32[N]` — мощность в dB; `N = payload_len / 4` |
| `0x02` | `IQ_C64` | `(float32 I, float32 Q)[N]`; `N = payload_len / 8` |
| `0x03` | `PCM_I16` | `int16[N]` — PCM аудио; `N = payload_len / 2` |
| `0x04` | `IQ_I8` | `(int8 I, int8 Q)[N]` — компактный IQ для 8-bit АЦП; `N = payload_len / 2` |
| `0x05` | `IQ_I16` | `(int16 I, int16 Q)[N]` — IQ для 16-bit АЦП; `N = payload_len / 4` |
| `0xFE` | `HEARTBEAT` | (пусто, `payload_len = 0`) |
| `0xFF` | (резерв, error frame) | TBD |

### Поле `display_mode`

Применимо только к `data_type = FFT_DB` (для других типов — `0x00`):

| Значение | Имя | Описание |
|----------|-----|----------|
| `0x00` | `CURRENT` | Последний FFT в окне (без аккумуляции) |
| `0x01` | `AVG` | Линейное усреднение FFT за `window_ms` |
| `0x02` | `MAXHOLD` | Пиковый детектор за окно (показывает кратковременные сигналы) |
| `0x03` | `MINHOLD` | Минимум за окно (для оценки шумового пола, фильтр impulse interference) |
| `0x04` | `PERSISTENCE` | Гистограмма за окно (для будущего DPX-style heatmap) |
| `0x05` | `EXP_AVG` | Экспоненциальное сглаживание (`smooth = α·new + (1-α)·smooth`) |

### Примеры hex-дампа

**FFT 512 bins, MAXHOLD, центр 437.365 МГц, полоса 192 кГц:**

```text
Offset Hex                                                   Decoded
─────────────────────────────────────────────────────────────
0x0000 53 53 54 4D                                          magic = "SSTM"
0x0004 01                                                   version = 1
0x0005 01                                                   data_type = FFT_DB
0x0006 02                                                   display_mode = MAXHOLD
0x0007 00                                                   reserved
0x0008 80 7B 9B 89 95 01 00 00                              timestamp_ms = 1714065297280
0x0010 00 14 D2 1A 00 00 00 00                              center_hz = 437365000
0x0018 00 EE 02 00 00 00 00 00                              span_hz = 192000
0x0020 00 08 00 00                                          payload_len = 2048
0x0024 17 03 00 00                                          seq = 791
0x0028 ... 2048 bytes float32[512] ...                      FFT data
```

**Heartbeat:**

```text
0x0000 53 53 54 4D 01 FE 00 00                              magic, v=1, HEARTBEAT, no display_mode
0x0008 80 7B 9B 89 95 01 00 00                              timestamp_ms
0x0010 00 14 D2 1A 00 00 00 00                              center_hz (последняя известная)
0x0018 00 EE 02 00 00 00 00 00                              span_hz (последняя известная)
0x0020 00 00 00 00                                          payload_len = 0
0x0024 18 03 00 00                                          seq = 792 (всё равно инкрементится)
                                                            (payload отсутствует)
```

### Размер заголовка: 40 байт

- Все `int64` лежат на смещениях, кратных 8 (`8`, `16`, `24`) — оптимально для CPU.
- Конец заголовка тоже выровнен по 8 → payload (если это `int64`/`float64`-массив) тоже выровнен.
- Оверхед минимален (см. § I.3).

---

## Часть III. URL-маршруты и параметры запроса

### Маршруты (планируемые)

| Endpoint | Описание | Что в payload |
|----------|----------|---------------|
| `GET /api/stream/v1/spectrum` | FFT для отображения (UI-оптимизированный) | `FFT_DB` |
| `GET /api/stream/v1/iq` | Сырой IQ (для записи / диагностики; ограничен по полосе) | `IQ_I8` / `IQ_I16` / `IQ_C64` |
| `GET /api/stream/v1/audio` | Демодулированное аудио (PCM) | `PCM_I16` |

### Query-параметры (применимы для `/spectrum`)

| Параметр | Тип | Default | Описание |
|----------|-----|---------|----------|
| `bins` | int | 1024 | Целевое число бинов на UI после downsampling. Допустимо 256, 512, 1024, 2048. |
| `fps` | int | 20 | Частота кадров. Допустимо 10..30. |
| `mode` | string | `maxhold` | Режим аккумуляции: `current`, `avg`, `maxhold`, `minhold`, `persistence`, `exp_avg`. |
| `window_ms` | int | `1000/fps` | Длина окна аккумуляции. По умолчанию совпадает с периодом кадра. |
| `smoothing` | float | 0.5 | Только для `mode=exp_avg`: коэф α экспоненциального сглаживания. |

> ⚠️ Параметры `center_hz` / `span_hz` / `gain` через query **НЕ передаются**. Они задаются текущей конфигурацией SDR через REST `POST /api/sdr/tune`. В заголовке каждого фрейма UI получает фактические значения.

### Управление SDR — отдельный REST API (вне ADR)

```text
POST /api/sdr/tune       {center_hz, span_hz}
POST /api/sdr/gain       {gain_db}
GET  /api/sdr/config     ← текущая конфигурация (опционально, для UI до старта стрима)
```

Это **не часть** транспорта, а DSP-управление. Будет описано в SDR-001..005.

---

## Часть IV. Серверная и клиентская реализация

### Сервер (Go) — скелет handler'а

```go
// Handler бинарного потока FFT
func (s *Server) handleSpectrumStream(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/octet-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("X-Accel-Buffering", "no") // Отключить буферизацию nginx

    // Парсинг query
    bins := parseIntDefault(r.URL.Query().Get("bins"), 1024)
    fps := parseIntDefault(r.URL.Query().Get("fps"), 20)
    mode := parseModeDefault(r.URL.Query().Get("mode"), ModeMaxHold)

    // Подписка на DSP-конвейер
    sub := s.spectrumPipeline.Subscribe(bins, fps, mode)
    defer sub.Close()

    var seq uint32 = 0
    headerBuf := make([]byte, 40)

    for {
        select {
        case frame := <-sub.Frames():
            // Заполнение заголовка (всё в LE)
            binary.LittleEndian.PutUint32(headerBuf[0:4], 0x4D545353)   // "SSTM"
            headerBuf[4] = 1                                            // version
            headerBuf[5] = byte(frame.DataType)                         // FFT_DB
            headerBuf[6] = byte(frame.DisplayMode)
            headerBuf[7] = 0                                            // reserved
            binary.LittleEndian.PutUint64(headerBuf[8:16], uint64(frame.TimestampMs))
            binary.LittleEndian.PutUint64(headerBuf[16:24], uint64(frame.CenterHz))
            binary.LittleEndian.PutUint64(headerBuf[24:32], uint64(frame.SpanHz))
            binary.LittleEndian.PutUint32(headerBuf[32:36], uint32(len(frame.Data)))
            binary.LittleEndian.PutUint32(headerBuf[36:40], seq)

            if _, err := w.Write(headerBuf); err != nil {
                return
            }
            if _, err := w.Write(frame.Data); err != nil {
                return
            }
            flusher.Flush()
            seq++

        case <-time.After(15 * time.Second):
            // Heartbeat
            writeHeartbeatFrame(w, headerBuf, seq, sub.LastConfig())
            flusher.Flush()
            seq++

        case <-r.Context().Done():
            return // клиент отключился
        }
    }
}
```

### Клиент (JS) — `BinaryStreamReader`

```javascript
class BinaryStreamReader {
    constructor(url, onFrame) {
        this._url = url;
        this._onFrame = onFrame;
        this._ctrl = null;
        this._expectedSeq = null;
        this._buffer = new Uint8Array(0); // буфер для дозбора неполных фреймов
        this._stats = { framesReceived: 0, framesDropped: 0, sessions: 0 };
    }

    async start() {
        this._ctrl = new AbortController();
        try {
            const resp = await fetch(this._url, { signal: this._ctrl.signal });
            if (!resp.ok) throw new Error(`HTTP ${resp.status}`);

            const reader = resp.body.getReader();
            while (true) {
                const { done, value } = await reader.read();
                if (done) break;
                this._appendAndProcess(value);
            }
        } catch (err) {
            if (err.name !== 'AbortError') {
                this._scheduleReconnect();
            }
        }
    }

    stop() {
        if (this._ctrl) this._ctrl.abort();
    }

    _appendAndProcess(chunk) {
        // Дозбор фреймов из произвольных HTTP-чанков
        const merged = new Uint8Array(this._buffer.length + chunk.length);
        merged.set(this._buffer);
        merged.set(chunk, this._buffer.length);
        this._buffer = merged;

        while (this._buffer.length >= 40) {
            const view = new DataView(this._buffer.buffer, this._buffer.byteOffset, 40);
            const magic = view.getUint32(0, true);
            if (magic !== 0x4D545353) {
                console.error('[stream] magic mismatch, resyncing');
                this._buffer = this._buffer.slice(1); // ищем magic байт за байтом
                continue;
            }
            const payloadLen = view.getUint32(32, true);
            const totalLen = 40 + payloadLen;
            if (this._buffer.length < totalLen) break; // нет полного фрейма

            const header = this._parseHeader(view);
            const payload = this._buffer.slice(40, totalLen);
            this._buffer = this._buffer.slice(totalLen);

            this._handleFrame(header, payload);
        }
    }

    _parseHeader(view) {
        return {
            version:     view.getUint8(4),
            dataType:    view.getUint8(5),
            displayMode: view.getUint8(6),
            timestampMs: Number(view.getBigInt64(8, true)),
            centerHz:    Number(view.getBigInt64(16, true)),
            spanHz:      Number(view.getBigInt64(24, true)),
            payloadLen:  view.getUint32(32, true),
            seq:         view.getUint32(36, true),
        };
    }

    _handleFrame(header, payload) {
        // ... (логика проверки seq, см. § I.6)
        if (header.dataType === 0xFE) return; // heartbeat — пропускаем
        this._onFrame(header, payload);
    }
}
```

---

## Часть V. Сценарии работы

### Сценарий 1: Открытие вкладки «Обзор» с активным SDR

1. Оператор переключился на вкладку «Обзор» (`bottom-panel.js`).
2. JS создаёт `BinaryStreamReader('/api/stream/v1/spectrum?bins=1024&fps=20&mode=maxhold')`.
3. `fetch` устанавливает HTTP-соединение с сервером.
4. Go-handler подписывается на DSP-конвейер с параметрами `bins=1024, fps=20, mode=maxhold`.
5. Каждые 50 мс DSP выдаёт обработанный кадр → handler формирует фрейм с заголовком, отправляет через `Flush()`.
6. Клиент получает чанк, парсит заголовок, отдаёт `Float32Array` в `WaterfallView.pushLine()` и `FFTSpectrumView.draw()`.
7. UI отображает waterfall с подписанной шкалой частот (из `center_hz`/`span_hz`).

### Сценарий 2: Reconnect после обрыва сети

1. Wi-Fi пропал на 5 секунд. `fetch` бросает ошибку.
2. `BinaryStreamReader._scheduleReconnect()` запускает exponential backoff (1с → 2с → ...).
3. Сервер всё это время гнал фреймы — клиент их не получил.
4. Сеть восстановилась. Новый `fetch`. Сервер запускает новый handler. Счётчик с 0.
5. Клиент видит: ожидался `seq=N+1`, пришёл `seq=0` → детектирует «новая сессия». Сбрасывает свой `expectedSeq = 1`.
6. Waterfall на UI просто продолжает рисоваться. Между «потерянными» кадрами на UI визуально появляется промежуток (по timestamp видно).

### Сценарий 3: Оператор сменил частоту

1. Оператор кликнул «Сопровождать» по другому КА. Через REST: `POST /api/sdr/tune {center_hz: 145800000}`.
2. Бэк перенастроил SDR. Handler стрима **не закрывался** — он по-прежнему отдаёт кадры.
3. Следующий кадр имеет `center_hz = 145800000` в заголовке.
4. Клиент получает кадр, парсит заголовок, видит изменение `centerHz` → перерисовывает шкалу частот, очищает waterfall (или плавно скроллит — на усмотрение UI).
5. **Никакой явной синхронизации между REST и стримом не нужно** — кадр самодостаточен.

### Сценарий 4: Бэкенд рестарт

1. Бэк упал, systemd перезапустил.
2. Клиент видит обрыв `fetch`, идёт в reconnect. Бэк ещё не поднялся — клиент получает 503.
3. Backoff растёт. Через 30 сек бэк поднялся, клиент успешно подключился.
4. SDR на бэке мог настроиться на другую частоту (если конфиг изменился). Первый же фрейм несёт актуальные `center_hz`/`span_hz`.
5. UI адаптируется автоматически.

### Сценарий 5: Запись потока в файл для офлайн-анализа

```bash
curl http://localhost:8080/api/stream/v1/spectrum?bins=2048\&fps=10 > recording.bin
```

1. Получили файл с N фреймами, каждый по 40 + 8192 = 8232 байт.
2. Скрипт читает файл, парсит заголовки, может построить статистику: распределение по `seq` (есть ли пропуски), `timestamp_ms` (jitter), `center_hz` (менялась ли частота во время записи).
3. Каждый фрейм самодостаточен — можно начать чтение с произвольной позиции (поиск `magic` в потоке).

### Сценарий 6: Потеря кадров при перегрузке клиента

1. Клиент тормозит (heavy GC, тяжёлая JS-работа). Не успевает читать `fetch`.
2. На сервере накапливается очередь в `chan []byte`. При переполнении — `drop oldest` (см. § B1, обсуждается).
3. Клиент в итоге получает фреймы с пропусками: `seq=100, 101, 105` → детектирует пропуск 3 кадров.
4. В лог: `[stream] dropped 3 frames`. В метриках: `frames_dropped++`.
5. UI рисует waterfall с «зазором» по времени (timestamps видны).

### Сценарий 7: Долгое ожидание AOS (heartbeat)

1. Спутник ещё не появился. SDR настроен, но сигнала нет.
2. DSP-конвейер не выдаёт кадров (или выдаёт с сильным шумом — это уже зависит от настройки).
3. Каждые 15 сек handler шлёт heartbeat-фрейм (`data_type=0xFE`, `payload_len=0`).
4. nginx видит трафик → не убивает TCP.
5. Когда спутник появится и SDR начнёт давать данные — handler снова шлёт обычные кадры.

---

## Часть VI. Открытые вопросы (продолжить обсуждение)

> Эти вопросы не были полностью согласованы. Документ остаётся в статусе **Draft**, пока они не будут закрыты.

### A. Display Reducer — детали

| ID | Вопрос | Предложение |
|----|--------|-------------|
| **Q-DR-1** | Окончательное согласие на архитектуру: Reducer на бэке, параметры через query | Принять |
| **Q-DR-2** | Какой `display_mode` по умолчанию для FFT-вкладки «Обзор»? | `maxhold` (показывает все сигналы, включая короткие пакеты ТМИ) |
| **Q-DR-3** | Целевые `bins × fps` по умолчанию | `1024 × 20` → 80 КБ/с |
| **Q-DR-4** | Подтверждение: демодулятор живёт на бэке, на UI — только декодированные пакеты через SSE | Подтвердить |
| **Q-DR-5** | Алгоритмы — оператор обещал прислать примеры | Ждём примеры. Особенно интересны: persistence display, peak tracker, adaptive thresholding |

### B. Backpressure и операционная устойчивость (бэк)

| ID | Вопрос | Предложение |
|----|--------|-------------|
| **Q-B1** | Стратегия при медленном клиенте (канал переполнен) | **drop oldest** в канале сервера (`chan []byte` размером 8). При переполнении сервер выкидывает старый фрейм. Метрика `frames_dropped_server`. |
| **Q-B2** | Лимит потоков на один `client_id` | **3 потока на client_id** (по одному на тип). Повторный fetch на уже открытый тип — закрывает старый. |
| **Q-B3** | Глобальный лимит активных стримов | **`max_binary_streams = 50`** в конфиге. При превышении — HTTP 503. |
| **Q-B4** | Тайм-аут handler'а при долгом простое | **Не закрывать со стороны сервера.** Закрытие — только при отключении клиента или shutdown сервера. |

### C. Клиентская сторона

| ID | Вопрос | Предложение |
|----|--------|-------------|
| **Q-C1** | Отписка при `document.hidden` | Отменять `fetch` при `visibilitychange → hidden`, переподключаться при возврате к вкладке. Экономит трафик и CPU. |
| **Q-C2** | Auto-reconnect стратегия | Exponential backoff 1с → 2с → 4с → ... → 30с max (как у SSE). |
| **Q-C3** | Координация со SSE | Поток привязан к UI-вкладке, не к КА. Закрытие потока — только при уходе с вкладки или явном action. SSE `tracking_ended` поток не трогает. |
| **Q-C4** | Что показывать при разрыве | Последний кадр + полупрозрачная плашка «нет данных». При reconnect плашка исчезает. |

### D. Тестируемость

| ID | Вопрос | Предложение |
|----|--------|-------------|
| **Q-D1** | Тест-харнесс на бэке | `httptest.NewRecorder()` + проверка `Flushed`. Тест: пушим N фреймов в канал DSP, читаем N фреймов из `recorder.Body`, парсим заголовки. |
| **Q-D2** | Тест на фронте под jsdom / vitest | Node 18+ имеет `ReadableStream` нативно. Без полифиллов. На стадии реализации проверим версию Node. |

### E. Расширяемость (open questions для будущих версий)

| ID | Вопрос | Предложение |
|----|--------|-------------|
| **Q-E1** | Сжатие | **Не сжимать в v1.** FFT-спектр шумоподобный (low entropy gain). Для PCM-аудио — рассмотрим в будущем. |
| **Q-E2** | Multiplex (один поток несёт разные `data_type`) | **Не делать в v1.** Если понадобится — в v2 через `data_type` в заголовке (он уже есть). |
| **Q-E3** | Запись в файл (IQ recording) | **Отдельный механизм**, не часть транспорта. Команда `POST /api/sdr/record/start` → сервер пишет IQ в файл и параллельно стримит. SDR-006 (планируется). |
| **Q-E4** | `data_type=0xFF` (Error frame) | TBD: специальный фрейм с описанием ошибки (например, «SDR disconnected», «Buffer overrun») — для отображения на UI вместо тихого heartbeat. |

---

## Часть VII. Связанные файлы и задачи

### Реализационные файлы (планируемые)

| Файл | Роль |
|------|------|
| `internal/handlers/stream_handler.go` | HTTP handlers `/api/stream/v1/*` |
| `internal/dsp/frame.go` | Структура фрейма, кодирование/декодирование заголовка |
| `internal/dsp/reducer.go` | Display Reducer (avg/maxhold/minhold/persistence) |
| `internal/dsp/pipeline.go` | DSP-конвейер: SDR → FFT → Reducer → подписчики |
| `internal/sdr/source.go` | Интерфейс SDR-источника (RTL-SDR, файл, генератор) |
| `static/js/binary-stream.js` | `BinaryStreamReader` с reconnect + seq tracking |
| `static/js/bottom-panel.js` | Замена `SpectrumDataSource` на реальный поток |

### Связанные задачи

| ID | Задача | Зависимость |
|----|--------|-------------|
| **SDR-001** | Интерфейс RTL-SDR через rtl_tcp | (нет) |
| **SDR-002** | Спектральный анализ (FFT) | SDR-001 |
| **SDR-003** | Waterfall-визуализация | SDR-002 |
| **UX-OVERVIEW-BACKEND-001** | Подмена `SpectrumDataSource` на реальный поток через ADR-002 | SDR-001, SDR-002 |
| **DEMOD-001..004** | Демодуляция ТМИ (на бэке) | SDR-001, COORD-002 |

---

## История изменений

| Дата | Что | Кто |
|------|-----|-----|
| 2026-04-14 | Исходное решение по транспорту: SSE + HTTP Chunked Stream. Зафиксировано в `memory-bank/techContext.md`, `activeContext.md`. | Архитектор |
| 2026-04-26 | ADR создан как формальный документ. Расширен: self-describing frames (по принципу VITA-49), seq counter, Display Reducer, открытые вопросы зафиксированы. Статус: **Draft**. | Архитектор + AI-ассистент |
| _(будущее)_ | Закрытие открытых вопросов, переход в статус **Accepted** | TBD |
| _(будущее)_ | Пересмотр после реализации UX-OVERVIEW-BACKEND-001 | TBD |
