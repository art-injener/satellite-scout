# ADR-005: Референсные реализации для PHASE-11/12 — go-порт алгоритмов r2cloud и SatNOGS

**Дата:** 2026-05-28
**Статус:** Accepted
**Связанные задачи:** PHASE-8, PHASE-11 (SDR-001..005), PHASE-12 (DEMOD-001..006), ROTATOR-004
**Связанные ADR:** ADR-004 (Operation Modes and Station Schema) — этот ADR поставляет реализацию для интерфейсов, описанных в ADR-004 § 2.3, § 3, § 4.4, § 4.5.

---

## 1. Контекст и проблема

### 1.1. Пустые фазы PHASE-8/11/12

К моменту 2026-05-28 у нас не реализованы:
- **PHASE-8 — Управление антенной** (0/5 задач): `ROTATOR-001..004`, `ROTATOR-UI-001`.
- **PHASE-11 — Интеграция с SDR** (0/5 задач): `SDR-001..005` — интерфейс `Receiver`, RTL-SDR, FFT/Waterfall, Doppler, `ScanStrategy`.
- **PHASE-12 — Демодуляция ТМИ** (0/6 задач): `DEMOD-001..006` — FSK/AFSK, AX.25, парсер ТМИ, энергетический детектор, VFO Manager.

ADR-004 задаёт **архитектурный скелет** этих фаз (интерфейсы и контракты), но не отвечает на вопрос **«откуда брать сами алгоритмы DSP / демодуляторов / FEC / парсеров ТМИ»**.

### 1.2. Существуют две зрелые open source экосистемы

| Экосистема | Стек | Лицензия | Что закрывает |
|---|---|---|---|
| **r2cloud** (dernasherbrezon) | Java + C | Apache 2.0 | DSP, демодуляторы, FEC, де-фреймеры, ~80 декодеров КА, мультиклиентский SDR-сервер |
| **SatNOGS** (Libre Space Foundation) | Python + C++ (GNU Radio) | **GPL-3.0** (gr-satnogs), **AGPL** (helpers, client) | GNU Radio out-of-tree блоки, **Kaitai Struct декодеры телеметрии ~80 КА**, авто-планировщик наблюдений, калькулятор коллизий передатчиков |

Обе экосистемы продакшн-зрелые, активно развиваются (последние коммиты — май 2026).

### 1.3. Альтернативы и почему они отвергнуты

| Альтернатива | Почему отвергнуто |
|---|---|
| **Интеграция с jradio как sidecar-процесс** (JVM рядом с Go-бинарём) | Тащит JVM в дистрибутив, ломает идею «один Go-бинарь», усложняет docker-образ и embedded-сборки (Raspberry Pi) |
| **Интеграция с sdr-server как внешний демон** (по его бинарному TCP API) | Внешняя зависимость на C-демон, ломает «один бинарь», усложняет имитацию в тестах |
| **Полностью самостоятельная разработка DSP с нуля** | 1-2 года работы вместо 3-6 месяцев, риск некорректных алгоритмов (BER, синхронизация кадров — нетривиально) |
| **Свой Kaitai-аналог для парсеров ТМИ** | Изобретение колеса; satnogs-decoders уже покрывает ~80 КА в готовом машиночитаемом виде |

---

## 2. Принятые решения

### 2.1. Go-порт алгоритмов без интеграций

**Решение:** портируем алгоритмы r2cloud и SatNOGS на Go **внутрь нашего репозитория**. Никаких внешних демонов, никаких форков, никакой JVM. Один Go-бинарь (как сейчас).

**Не делаем:**
- jradio как sidecar (JVM-процесс рядом с Go).
- sdr-server как внешний демон.
- Форк r2cloud / gr-satnogs.

**Делаем:**
- Реализуем те же алгоритмы и архитектурные паттерны на Go.
- Где исходная экосистема даёт код — портируем код (с учётом лицензий, см. § 2.2).
- Где экосистема даёт спецификацию формата данных (`.ksy` файлы) — используем как формат через codegen.

### 2.2. Смена лицензии проекта: Apache 2.0 → GPL-3.0

**До 2026-05-28:** проект под Apache 2.0 (файл `LICENSE`); в `README.md` некорректно указан MIT — рассинхрон.

**С 2026-05-28:** проект переходит под **GPL-3.0**.

**Обоснование:**
- gr-satnogs — GPL-3.0; без GPL-лицензии у нашего проекта код gr-satnogs нельзя копировать.
- satnogs-client, autoscheduler, satnogs-collision-calc — AGPL; код этих проектов не копируем (AGPL строже GPL), берём только идеи.
- r2cloud-экосистема (Apache 2.0) → совместима с GPL «в одну сторону»: Apache-код можно включать в GPL-проект с сохранением атрибуции.
- Идеологически соответствует ценностям референсных экосистем (SatNOGS, r2cloud) — общественное достояние, защита от закрытого переиспользования.

**Последствие:** проект нельзя встроить в проприетарные продукты. Это осознанный выбор.

### 2.3. Правила копирования по типу источника

| Источник | Что копируем | Как |
|---|---|---|
| **r2cloud / jradio / sdr-server / sdr-modem** (Apache 2.0) | Код алгоритмов | Прямой порт на Go. В заголовке файла — атрибуция: `// Адаптировано из <repo>/<path> (Apache-2.0), <SHA-короткий>`. Глобальный `NOTICE` в корне репо. |
| **gr-satnogs** (GPL-3.0) | Код блоков | Прямой порт. Атрибуция в заголовке файла. |
| **satnogs-decoders `.ksy` файлы** | Спецификации формата | Используем как формат данных, генерируем Go через `kaitai-struct-compiler -t go`. Сами `.ksy` копируем в `internal/demod/ksy/`. |
| **satnogs-client / autoscheduler / satnogs-collision-calc** (AGPL) | **Только идеи и архитектура** | Code-copy НЕ делаем (AGPL потащит за собой). Реализуем clean-room по описанию. |
| **Блог dernasherbrezon.com** | Идеи + методики валидации (BER, синхронизация) | Ссылка в комментариях / `techContext.md`. |
| **samoyed** (GPL-3.0, Go-порт Dire Wolf) | AX.25, AFSK/GMSK демод, G3RUH scrambler | Прямой порт / адаптация. Атрибуция в заголовке: `// Адаптировано из samoyed (GPL-3.0)`. См. § 3.5.1. |
| **GopherTrunk** (Apache 2.0) | **Протоколы чипов** (RTL2832U, AirSpy, HackRF): vendor requests, tune, IQ stream | Порт с атрибуцией в `internal/sdr/chipdrivers/*`. **USB-транспорт не копируем** — самописный `internal/usbdrivers` (§ 2.4). См. ADR-006. |
| **pzl/usb**, **kevmo314/go-usb** | USB usbfs ioctl, API дизайн | **Только референс** при разработке `internal/usbdrivers`. Code-copy по необходимости (MIT/Apache-2.0) с атрибуцией. |
| **gonum** (BSD-3) | FFT, оконные функции | Используем как **зависимость** `go get gonum.org/v1/gonum`, не копируем. |
| **gomodem, go-ax25, segdsp** | Идеи и паттерны DSP pipeline | Референс для архитектуры. Code-copy по необходимости с атрибуцией. |

### 2.4. USB-транспорт: самописный модуль `internal/usbdrivers`

> Решение согласовано: USB-слой пишем сами, опираясь на референсные проекты — для глубокого понимания системного программирования под Linux.

**Решение:** не портировать USB-код из GopherTrunk и не использовать внешние USB-библиотеки как зависимости. Реализуем **`internal/usbdrivers`** — pure-Go обёртку над Linux **usbfs** (`/dev/bus/usb/…` + ioctl).

**Два слоя SDR-драйвера (не смешивать):**

| Слой | Пакет | Что делает | Источник |
|------|-------|------------|----------|
| **USB-транспорт** | `internal/usbdrivers/` | sysfs enumeration, open device node, `USBDEVFS_CONTROL`, `CLAIMINTERFACE`, bulk read/write | **Самописный** (clean-room). Референсы: pzl/usb, kevmo314/go-usb, kernel `usbdevice_fs.h` |
| **Протокол чипа** | `internal/sdr/chipdrivers/rtlsdr/` и др. | RTL2832U: tune, gain, activate stream, конвертация u8→IQ | Порт/адаптация из GopherTrunk + osmocom rtl-sdr wiki. Атрибуция при копировании кода |

**Структура `internal/usbdrivers` (целевая):**

```text
internal/usbdrivers/
├── sysfs.go          # перечисление устройств (/sys/bus/usb/devices) — часть уже в internal/sdr/discover_linux.go
├── device.go         # Open(path), Close, ClaimInterface
├── control.go        # ControlTransfer (USBDEVFS_CONTROL)
├── bulk.go           # BulkRead / BulkWrite (USBDEVFS_BULK)
├── ioctl_linux.go    # //go:build linux — константы ioctl из linux/usbdevice_fs.h
└── *_linux_test.go   # unit-тесты sysfs; live-тесты с dongle — локально, //go:build linux
```

**Учебный путь (этапы реализации USB-слоя):**

1. sysfs enumeration — уже есть в `internal/sdr/discover_linux.go`, перенос в `usblinux`
2. `open("/dev/bus/usb/BUS/DEV")` + udev rules
3. control transfer — чтение descriptor, set configuration
4. claim interface + bulk read
5. async URB (`USBDEVFS_SUBMITURB`) — когда sync bulk не хватает на 2.4 MSPS

**Документация Linux (must-read):**

- [Kernel USB driver API](https://www.kernel.org/doc/html/latest/driver-api/usb/usb.html)
- Заголовок `linux/usbdevice_fs.h` — константы ioctl
- `lsusb -v` на подключённом RTL-SDR — реальные descriptor'ы

**Референсы для изучения (не `go get`):**

| Проект | Что смотреть |
|--------|--------------|
| [pzl/usb](https://github.com/pzl/usb) | `gusb/usbDeviceFS_ioctl.go`, `ioctl.go` — карта libusb → usbfs |
| [kevmo314/go-usb](https://github.com/kevmo314/go-usb) | API `Device`, `ControlTransfer`, `BulkTransfer` |
| GopherTrunk `internal/sdr/usb/` | Как USB-слой используется в rtlsdr (после шагов 2–4) |
| GopherTrunk `rtlsdr/` | Vendor requests, tune, read IQ (слой 2) |

**Атрибуция:** clean-room реализация usbfs — NOTICE не нужен. Атрибуция только если копируем конкретные фрагменты кода из референсов.

---

## 3. Карта «что берём → куда»

### 3.1. PHASE-11 (SDR) — источники алгоритмов

| Задача | Источник | Что берём |
|---|---|---|
| `SDR-001` (Receiver интерфейс + RTL-SDR) | `sdr-server/src/` + **`internal/usbdrivers`** (самописный) + GopherTrunk `rtlsdr/` (chip) | Lazy-start, bounded queue (`queue.png`), per-VFO goroutine. **USB:** самописный usbfs (§ 2.4). **Chip:** порт RTL2832U из GopherTrunk/osmocom. |
| `SDR-002` (FFT, спектральный анализ) | `sdr-server` + `rtlSpectrum` | **Frequency Xlating FIR Filter** (`sdr-server/src/dsp.c`) — перетаскивание подканала в baseband + децимация. Алгоритмы биннинга/нормализации waterfall — `rtlSpectrum` |
| `SDR-003` (Waterfall + Spectrum UI) | `rtlSpectrum`, `gr-satnogs/lib/waterfall_sink_impl.cc` | Цветовая карта, max-hold/avg переключение |
| `SDR-004` (Doppler автокоррекция) | `jradio` README + `sdr-modem` | Pattern `DopplerValueSource + Multiply` (умножение IQ на комплексную экспоненту) — встроен напрямую в pipeline, не отдельный модуль |
| `SDR-005` (ScanStrategy) | `r2cloud` scheduler, **`autoscheduler`** (SatNOGS, AGPL — идеи) | Hop-and-dwell pattern, приоритизация частот по плану сеансов + SatNOGS |
| `SDR-006` (Калькулятор коллизий передатчиков) | **`satnogs-collision-calc`** (AGPL — **только идея**, код не копируем) | Clean-room: пересечение интервалов `[freq − doppler − bw/2, freq + doppler + bw/2]` по time-bin'ам в окне пролёта. Используется `ScanStrategy` для приоритизации dwell и UI для предупреждений в плане сеансов |

### 3.2. PHASE-12 (Демодуляция) — источники алгоритмов

| Задача | Источник | Что берём |
|---|---|---|
| `DEMOD-001` (FSK/AFSK демодуляторы) | `jradio/src/main/java/ru/r2cloud/jradio/demod/FskDemodulator.java`, `AfskDemodulator.java` | Алгоритм soft-bits 0..255, clock recovery `ClockRecoveryMM`, RMS AGC |
| `DEMOD-001` (BPSK/QPSK) | `jradio/demod/BpskDemodulator.java`, `QpskDemodulator.java` | `CostasLoop`, `PolyphaseClockSyncComplex`, `ConstellationSoftDecoder` |
| `DEMOD-002` (AX.25 декодер) | `jradio/Ax25BeaconSource.java`, `Ax25G3ruhBeaconSource.java`, `Ax100BeaconSource.java` | Флаги 0x7E, bit unstuffing, G3RUH скремблер, заголовок AX.25 |
| `DEMOD-002` (CRC) | `jradio/crc/*` | CRC-16/CCITT, CRC-16/X-25 |
| `DEMOD-002` (FEC: Viterbi, Reed-Solomon) | `jradio/fec/Viterbi.java`, `ViterbiSoft.java`, `fec/ccsds/ReedSolomon.java` | Viterbi hard+soft, CCSDS RS, BCH, Golay |
| **`DEMOD-003`** (Парсер телеметрии — **переформулирован**) | **`satnogs-decoders/ksy/*.ksy`** | **Kaitai Struct codegen**: `.ksy` → Go-struct через `kaitai-struct-compiler -t go`. 3 эталонных декодера на MVP (CubeBel-1 / GRBAlpha / GEOSCAN), дальше — по мере появления реальных КА в плане |
| `DEMOD-004` (Энергетический детектор) | `gr-satnogs/lib/ber_calculator_impl.cc` | Threshold + длительность, метрика SNR |
| `DEMOD-005` (VFO Manager backend) | `sdr-server` + `sdr-modem` | Несколько параллельных decode-pipeline'ов на одной полосе SDR (per-VFO goroutine) |

### 3.3. PHASE-8 (Антенна)

| Задача | Источник | Что берём |
|---|---|---|
| `ROTATOR-004` (rotctld) | `r2cloud` (Hamlib интеграция) | TCP-протокол Hamlib rotctld, reconnect logic |
| `ROTATOR-001/002/003` | Свой дизайн (ADR-004 § 4.4) | — |

### 3.4. Возможные расширения (бэклог, не в текущем спринте)

| Идея | Откуда | Когда |
|---|---|---|
| Авто-планировщик наблюдений (cron-like) | `autoscheduler` (AGPL — идея) + `r2cloud` scheduler | Расширение `PassService` |
| Альтернативные источники TLE (SpaceTrack, OMM, GP) | `satnogs-orbital-data` (идея) | Если Celestrak недостаточно |
| Запись/replay IQ-сэмплов | `sdr-modem` (Apache 2.0) | Для имитатора и регресс-тестов |
| LRPT (METEOR-M) | `jradio/MeteorImageTest`, блог `Синхронизация кадров Метеор-М №2` | Если расширим за пределы кубсатов |
| CCSDS 235.1 (свежий пост 02.05.2026) | блог dernasherbrezon | По мере появления КА с этим протоколом |

### 3.5. Дополнительные Go-референсы (native Go проекты)

> Помимо Java/C/Python экосистем (r2cloud, SatNOGS), существуют зрелые Go-проекты, которые можно использовать как референсы или источники кода. Ниже — детальный обзор.

#### Сводная таблица: все найденные проекты по категориям

**SDR драйверы и USB (pure-Go)**

| Проект | Описание | Лицензия | Релевантность |
|--------|----------|----------|---------------|
| **Satellite Scout `internal/usbdrivers`** | Самописный USB usbfs (ioctl) | — | ⭐⭐⭐⭐ **Наш USB-транспорт** — clean-room, § 2.4 |
| **[kevmo314/go-usb](https://github.com/kevmo314/go-usb)** | Pure-Go USB (Linux usbfs + macOS IOKit) | Apache-2.0 | ⭐⭐⭐ Референс API и ioctl при разработке `usblinux` |
| **[pzl/usb](https://github.com/pzl/usb)** | Pure-Go USB stack (только Linux) | MIT | ⭐⭐⭐ Референс низкоуровневого usbfs ioctl |
| **[GopherTrunk](https://github.com/MattCheramie/GopherTrunk)** | Pure-Go SDR drivers | Apache-2.0 | ⭐⭐⭐ Референс **протоколов чипов** (rtlsdr, airspy), не USB-слоя |

**DSP и FFT (pure-Go)**

| Проект | Описание | Лицензия | Релевантность |
|--------|----------|----------|---------------|
| **[gonum/gonum](https://github.com/gonum/gonum)** `dsp/fourier`, `dsp/window` | FFT + оконные функции (Hamming, Hann, Blackman) | BSD-3 | ⭐⭐⭐ **Рекомендуется для FFT** — зрелая библиотека, 8k★, активно поддерживается |
| **[mjibson/go-dsp](https://github.com/mjibson/go-dsp)** | FFT, WAV-reader, оконные функции | MIT | ⭐⭐ Проще gonum, подходит для быстрого старта |
| **[racerxdl/segdsp](https://github.com/racerxdl/segdsp)** | FM/AM демод + FFT + SIMD | Apache-2.0 | ⭐⭐⭐ **Хороший референс** — pure-Go DSP с SIMD, FM/AM, waterfall через WebSocket |

**Модемы и AX.25**

| Проект | Описание | Лицензия | Релевантность |
|--------|----------|----------|---------------|
| **[doismellburning/samoyed](https://github.com/doismellburning/samoyed)** | **Go-порт Dire Wolf!** AX.25 v2.2, FX.25, IL2P, APRS, 300-9600 bps | GPL-3.0 | ⭐⭐⭐⭐ **Главная находка** — зрелый TNC на Go, AFSK/GMSK, G3RUH |
| **[akhenakh/gomodem](https://github.com/akhenakh/gomodem)** | AFSK/GFSK модем + AX.25 (gRPC) | ? | ⭐⭐⭐ Свежий (апрель 2026), чистый DSP pipeline: бандпасс → SDFT → Gardner PLL |
| **[n7get/go-ax25](https://github.com/n7get/go-ax25)** | AX.25 toolkit: KISS, router, connected-mode | ? | ⭐⭐ Хороший референс для AX.25 frame encode/decode |
| [la5nta/wl2k-go](https://github.com/la5nta/wl2k-go) | Winlink framework с AX.25 | MIT | ⭐ AX.25 transport, но ориентирован на Kenwood TNC |

**Телеметрия спутников и CCSDS**

| Проект | Описание | Лицензия | Релевантность |
|--------|----------|----------|---------------|
| **[ravisuhag/astro](https://github.com/ravisuhag/astro)** | CCSDS протоколы на Go: SPP, TM/TC frames, CADU, CLTU, time codes | Apache-2.0 | ⭐⭐⭐ **Полезен для CCSDS-совместимых КА** — готовые encode/decode |
| [cukhoaimon/ccsds-spacepacket](https://github.com/cukhoaimon/ccsds-spacepacket) | Space Packet Protocol | Apache-2.0 | ⭐⭐ Минималистичный SPP encode/decode |
| [baskiton/SatsDecoder](https://github.com/baskiton/SatsDecoder) | Декодеры GEOSCAN, SPUTNIX, CubeBel-2 и др. (Python) | GPL-3.0 | ⭐⭐ Референс для протоколов, но Python |
| **[kaitai-struct](https://github.com/kaitai-io/kaitai_struct)** + **[go_runtime](https://github.com/kaitai-io/kaitai_struct_go_runtime)** | Декларативный парсер бинарных форматов | MIT (runtime) | ⭐⭐⭐ Уже в ADR-005 для `.ksy` → Go |

**Управление антенной (PHASE-8)**

| Проект | Описание | Лицензия | Релевантность |
|--------|----------|----------|---------------|
| **[dh1tw/remoteRotator](https://github.com/dh1tw/remoteRotator)** | Rotator через сеть (Yaesu GS232A, dummy, HTTP/WS) | MIT | ⭐⭐⭐ Хороший референс для rotator API + web UI |
| [dh1tw/goHamlib](https://github.com/dh1tw/goHamlib) | Go биндинги Hamlib | ? | ⭐ CGO, но покрывает 80% Hamlib API |

---

#### 3.5.1. Модемы и AX.25

| Проект | URL | Лицензия | Статус | Что берём |
|--------|-----|----------|--------|-----------|
| **samoyed** | [github.com/doismellburning/samoyed](https://github.com/doismellburning/samoyed) | GPL-3.0 | Активный (2026-04), 11★ | **Go-порт Dire Wolf** — зрелый TNC. AFSK 1200, GMSK 9600, G3RUH scrambler, AX.25 v2.2, FX.25 FEC, IL2P, APRS, DTMF. |
| **gomodem** | [github.com/akhenakh/gomodem](https://github.com/akhenakh/gomodem) | ? | Свежий (2026-04) | gRPC-сервис модуляции/демодуляции. Чистый DSP pipeline: biquad IIR bandpass, **Sliding DFT (SDFT)** для частотной дискриминации, **Gardner PLL** для timing recovery. |
| **go-ax25** | [github.com/n7get/go-ax25](https://github.com/n7get/go-ax25) | ? | Активный | AX.25 toolkit: frame encode/decode, KISS framing, multi-port router, connected-mode sessions, AGWPE server. |
| **wl2k-go** | [github.com/la5nta/wl2k-go](https://github.com/la5nta/wl2k-go) | MIT | Стабильный, 55★ | Winlink framework с AX.25 transport (Kenwood TNC, Linux kernel stack). |

**Ключевой референс для `DEMOD-001`/`DEMOD-002`:**

**samoyed** — наиболее зрелый. Это полноценный Go-порт [Dire Wolf](https://github.com/wb2osz/direwolf) (WB2OSZ), который считается эталонным TNC в любительском радио. Покрывает:

- **Модемы:** 300 bps AFSK (HF), 1200 bps AFSK (VHF), 2400/4800 bps PSK, 9600 bps GMSK/G3RUH
- **Протоколы:** AX.25 v2.2, FX.25 (FEC), IL2P, APRS
- **DSP:** correlator-based demod, clock recovery

Файловая структура samoyed (релевантные пакеты):
```
samoyed/
├── ax25/           # AX.25 frame encode/decode, bit unstuffing
├── demod/          # Демодуляторы: afsk1200, gmsk9600, psk
├── modem/          # Модуляторы
├── dsp/            # Фильтры, correlators, AGC
├── fec/            # FX.25 Reed-Solomon
└── aprs/           # APRS encoder/decoder
```

**gomodem** — свежий проект (апрель 2026), чистая архитектура DSP-pipeline:
```
gomodem/
├── modem/
│   ├── afsk.go         # AFSK modulator (DDS)
│   ├── gfsk.go         # GFSK/G3RUH modulator
│   ├── demod.go        # Demodulator: bandpass → SDFT → Gardner PLL
│   └── filter.go       # Biquad IIR
├── protocols/
│   └── ax25/           # AX.25 encode/decode
└── cmd/atest/          # CLI декодер WAV → AX.25 (аналог Dire Wolf atest)
```

#### 3.5.2. DSP и FFT (pure-Go)

| Проект | URL | Лицензия | Что берём |
|--------|-----|----------|-----------|
| **gonum** `dsp/fourier`, `dsp/window` | [github.com/gonum/gonum](https://github.com/gonum/gonum) | BSD-3 | **FFT** (radix-2 Cooley-Tukey), **оконные функции** (Hamming, Hann, Blackman, Nuttall). Зрелая библиотека (8k★), активно поддерживается. |
| **go-dsp** | [github.com/mjibson/go-dsp](https://github.com/mjibson/go-dsp) | MIT | FFT, WAV reader, оконные функции. Проще gonum, подходит для быстрого старта. |
| **segdsp** | [github.com/racerxdl/segdsp](https://github.com/racerxdl/segdsp) | Apache-2.0 | Pure-Go DSP с **native SIMD** (ASM). FM/AM демодуляция, FFT, waterfall через WebSocket. Референс для SIMD-оптимизаций. |

**Рекомендация для `SDR-002`:**

- **FFT:** `gonum.org/v1/gonum/dsp/fourier` — стабильный, хорошо документирован.
- **Оконные функции:** `gonum.org/v1/gonum/dsp/window` — Hamming, Hann, Blackman и др.
- **SIMD-оптимизации (позже):** паттерны из `segdsp` (assembly в `dsp/` пакете).

Пример использования gonum FFT:
```go
import (
    "gonum.org/v1/gonum/dsp/fourier"
    "gonum.org/v1/gonum/dsp/window"
)

fft := fourier.NewFFT(len(samples))
windowed := window.Hamming(samples)
spectrum := fft.Coefficients(nil, windowed)
```

#### 3.5.3. CCSDS и космические протоколы

| Проект | URL | Лицензия | Что берём |
|--------|-----|----------|-----------|
| **astro** | [github.com/ravisuhag/astro](https://github.com/ravisuhag/astro) | Apache-2.0 | **CCSDS протоколы на Go:** Space Packet Protocol (SPP), TM/TC Transfer Frames, CADU, CLTU, USLP, time codes. CLI + библиотека. |
| **ccsds-spacepacket** | [github.com/cukhoaimon/ccsds-spacepacket](https://github.com/cukhoaimon/ccsds-spacepacket) | Apache-2.0 | Минималистичный SPP encode/decode по CCSDS 133.0-B-2. |

**Когда использовать:**

- Если целевой КА использует **CCSDS TM/TC фреймы** (некоторые cubesat платформы, особенно университетские проекты с CCSDS 235.1).
- `astro` предоставляет готовые Go-типы для SPP Header, TM Frame, CADU sync — не нужно писать с нуля.

Пример структуры пакета astro:
```
astro/
├── pkg/
│   ├── spp/        # Space Packet Protocol
│   ├── tmdl/       # TM Data Link Layer
│   ├── tcdl/       # TC Data Link Layer
│   ├── cadu/       # Channel Access Data Unit
│   └── time/       # CCSDS Time Codes (CUC, CDS)
└── cmd/astro/      # CLI: astro spp encode/decode, astro tm demux
```

#### 3.5.4. Управление антенной (PHASE-8)

| Проект | URL | Лицензия | Что берём |
|--------|-----|----------|-----------|
| **remoteRotator** | [github.com/dh1tw/remoteRotator](https://github.com/dh1tw/remoteRotator) | MIT | Rotator через сеть: **Yaesu GS232A/B протокол**, HTTP + WebSocket API, web UI. Поддержка EA4TX ARS, K3NG controller, dummy rotator. |
| **goHamlib** | [github.com/dh1tw/goHamlib](https://github.com/dh1tw/goHamlib) | ? | Go биндинги для Hamlib C library (CGO). Покрывает ~80% Hamlib API. |

**Рекомендация для `ROTATOR-004`:**

- **Без CGO:** реализовать TCP-клиент rotctld самостоятельно (протокол текстовый, простой).
- **Референс для web API:** `remoteRotator` — паттерны HTTP/WebSocket для управления rotator.

Протокол rotctld (текстовый TCP, порт 4533):
```
p\n              → +000.0\n+000.0\n    (get position: azimuth, elevation)
P 180.0 45.0\n  → RPRT 0\n             (set position)
S\n             → RPRT 0\n             (stop)
```

#### 3.5.5. USB: самописный `internal/usbdrivers` и референсы

> См. принятое решение § 2.4. USB-транспорт — **самописный**; GopherTrunk — только для протоколов чипов.

| Проект | URL | Лицензия | Роль |
|--------|-----|----------|------|
| **`internal/usbdrivers`** | `internal/usbdrivers/` | — | **Наш модуль:** sysfs, open, control/bulk ioctl |
| **pzl/usb** | [github.com/pzl/usb](https://github.com/pzl/usb) | MIT | Референс: `gusb/usbDeviceFS_ioctl.go`, ioctl константы |
| **go-usb** | [github.com/kevmo314/go-usb](https://github.com/kevmo314/go-usb) | Apache-2.0 | Референс: чистый API поверх usbfs |
| **GopherTrunk** `usb/` | [GopherTrunk/internal/sdr](https://github.com/MattCheramie/GopherTrunk) | Apache-2.0 | Референс: как chip-драйвер использует USB (после шагов 2–4) |
| **GopherTrunk** `rtlsdr/` | то же | Apache-2.0 | Референс/порт: vendor requests RTL2832U (слой 2) |

**Что уже есть в проекте:** `internal/sdr/discover_linux.go` — sysfs enumeration (`idVendor`, `idProduct`, `busnum`/`devnum` → `/dev/bus/usb/BUS/DEV`). Переносится в `usblinux/sysfs.go`.

**Ключевые ioctl (из `linux/usbdevice_fs.h`):**

| ioctl | Назначение |
|-------|------------|
| `USBDEVFS_CONTROL` | Control transfer (descriptor, vendor requests) |
| `USBDEVFS_CLAIMINTERFACE` / `RELEASEINTERFACE` | Захват интерфейса перед bulk |
| `USBDEVFS_BULK` | Синхронный bulk read (IQ u8) |
| `USBDEVFS_SUBMITURB` / `REAPURB` | Асинхронный режим (позже, для throughput) |

#### 3.5.6. Kaitai Struct для Go

| Компонент | URL | Назначение |
|-----------|-----|------------|
| **kaitai_struct** | [github.com/kaitai-io/kaitai_struct](https://github.com/kaitai-io/kaitai_struct) | Компилятор `.ksy` → Go/Python/Java/... |
| **kaitai_struct_go_runtime** | [github.com/kaitai-io/kaitai_struct_go_runtime](https://github.com/kaitai-io/kaitai_struct_go_runtime) | Go runtime library (MIT) |

Workflow для `DEMOD-003`:
```bash
# 1. Скачать .ksy из satnogs-decoders
cp satnogs-decoders/ksy/cubebel1.ksy internal/demod/ksy/

# 2. Сгенерировать Go-парсер
kaitai-struct-compiler -t go -d internal/demod/generated/ internal/demod/ksy/cubebel1.ksy

# 3. Использовать в коде
import "satellite-scout/internal/demod/generated"
frame, err := generated.NewCubebel1(kaitai.NewStream(rawBytes))
```

#### 3.5.7. Дополнительные Python-референсы (для спецификаций протоколов)

| Проект | URL | Что полезно |
|--------|-----|-------------|
| **SatsDecoder** | [github.com/baskiton/SatsDecoder](https://github.com/baskiton/SatsDecoder) | Декодеры **GEOSCAN**, **SPUTNIX USP**, **CubeBel-2**, D-Star ONE, Lucky-7 и др. Python, но содержит спецификации форматов телеметрии. |
| **gr-satellites** | [github.com/daniestevez/gr-satellites](https://github.com/daniestevez/gr-satellites) | GNU Radio OOT с ~100 декодерами КА. Референс для frame sync, FEC, deframing. |

> Эти проекты не портируем напрямую, но используем как источник спецификаций протоколов при написании `.ksy` или Go-декодеров.

---

## 4. Что НЕ берём (явно)

- **r2cloud-ui** — у нас собственный UI (HTMX + Vanilla JS), референс для UI — SkyRoof (см. `techContext.md`).
- **~80 декодеров jradio целиком** — портируем точечно, по мере необходимости. Для парсеров ТМИ предпочитаем Kaitai Struct (satnogs-decoders).
- **LoRa-стек** (`sx127x`) — вне скоупа Satellite Scout.
- **gr-satnogs весь** — берём только нужные блоки (frame_decoder pattern, BER calculator, metadata-формат).
- **satnogs-network серверная часть** — мы локальная станция, не сеть.
- **satnogs-rotator hardware/firmware** — у нас программная интеграция (rotctld), железо вне скоупа.

---

## 5. План работ (укрупнённо)

Этапы — детали будут в `memory-bank/tasks.md` при старте каждой задачи.

### Этап A — фундамент SDR (PHASE-11)

Актуальные этапы и состав задач SDR-001..007 — **ADR-006 §5** (эта редакция устарела: rtl_tcp понижен до опционального backend'а, приоритет — USB; имена — по коду). Референсы остаются в силе: ring buffer и Xlating FIR — `sdr-server`, Doppler — `jradio`, спектр — `rtlSpectrum`.

### Этап B — ядро демодуляции (PHASE-12)
1. `DEMOD-001` — FSK демодулятор + clock recovery (порт из `jradio`).
2. `DEMOD-002` — AX.25 декодер + CRC + bit unstuffing (порт из `jradio`).
3. `DEMOD-003` — Kaitai Struct codegen + 3 эталонных `.ksy` (CubeBel-1, GRBAlpha, GEOSCAN).
4. `DEMOD-004` — энергетический детектор (по `gr-satnogs/ber_calculator`).

### Этап C — расширение демодуляции
1. `DEMOD-005` — VFO Manager backend (несколько параллельных pipeline'ов).
2. AFSK / BPSK / QPSK демодуляторы.
3. FEC: Viterbi + Reed-Solomon (порт из `jradio/fec/`).
4. AX.25 G3RUH, AX100, CSP.

### Этап D — антенна и опциональные расширения (PHASE-8)
1. `ROTATOR-001..003` — модель + симулятор + REST/SSE.
2. `ROTATOR-004` — Hamlib rotctld клиент.
3. По запросу: LRPT, CCSDS 235.1, калькулятор коллизий.

### TDD-эталоны
- **BER vs SNR кривые** — методика из блог-постов dernasherbrezon (`Расчёт BER для FSK`, `Оптимизация FSK демодулятора`). Эталонные значения BER при заданном SNR используются как фиксированные ожидания в Go-тестах.
- **Кросс-валидация с GNU Radio** — там где возможно, тест-флоу: gnuradio-companion генерирует эталонный IQ → наш Go-демодулятор → сравнение бит-в-бит.
- **Skyfield для орбит** уже работает (TEST-REF-001) — расширим аналогичную схему на DSP.

---

## 6. Атрибуция

### 6.1. Глобальный `NOTICE` в корне репо

Создаётся при первом портированном файле. Структура:

```
Satellite Scout
Copyright 2026 <maintainer>

This product includes software derived from the following projects:

* r2cloud (Apache License 2.0)
  Copyright dernasherbrezon
  https://github.com/dernasherbrezon/r2cloud

* jradio (Apache License 2.0)
  Copyright dernasherbrezon
  https://github.com/dernasherbrezon/jradio

* sdr-server (Apache License 2.0)
  Copyright dernasherbrezon
  https://github.com/dernasherbrezon/sdr-server

* sdr-modem (Apache License 2.0)
  Copyright dernasherbrezon
  https://github.com/dernasherbrezon/sdr-modem

* gr-satnogs (GPL-3.0)
  Copyright Libre Space Foundation
  https://gitlab.com/librespacefoundation/satnogs/gr-satnogs

* satnogs-decoders (.ksy specifications)
  Copyright Libre Space Foundation and contributors
  https://gitlab.com/librespacefoundation/satnogs/satnogs-decoders

* samoyed (GPL-3.0) — Go port of Dire Wolf
  Copyright doismellburning and contributors
  https://github.com/doismellburning/samoyed
  Based on Dire Wolf by John Langner WB2OSZ
  https://github.com/wb2osz/direwolf

* GopherTrunk (Apache License 2.0) — pure-Go SDR drivers
  Copyright Matt Cheramie
  https://github.com/MattCheramie/GopherTrunk
```

### 6.2. Атрибуция в заголовке Go-файла

В шапке каждого портированного файла:

```go
// Пакет реализует FSK-демодулятор с soft-bits выходом.
//
// Алгоритм адаптирован из jradio (Apache-2.0):
//   https://github.com/dernasherbrezon/jradio
//   src/main/java/ru/r2cloud/jradio/demod/FskDemodulator.java
```

Пример для Go-референсов (§ 3.5):

```go
// Пакет реализует AX.25 декодер с G3RUH descrambler.
//
// Адаптировано из samoyed (GPL-3.0), Go-порт Dire Wolf:
//   https://github.com/doismellburning/samoyed
//   ax25/frame.go, demod/gmsk9600/
```

```go
// Пакет реализует pure-Go драйвер RTL2832U (chip protocol).
//
// Протокол чипа адаптирован из GopherTrunk (Apache-2.0):
//   https://github.com/MattCheramie/GopherTrunk
//   internal/sdr/rtlsdr/
// USB-транспорт — internal/usbdrivers (самописный, ADR-005 §2.4).
```

ID задач из трекера (`DEMOD-001` и т.п.) в заголовках не указываются (правило `techContext.md` § «Соглашения по коду»).

---

## 7. Последствия

### 7.1. Положительные
- Не зависим от JVM / внешних демонов — один Go-бинарь как сейчас.
- Контроль над DSP-кодом: можем оптимизировать под Go (SIMD через `golang.org/x/sys/cpu`, generics), без оглядки на чужую сборку.
- Прямой доступ к ~80 готовым декодерам ТМИ через `.ksy` — снимаем ~70% работы `DEMOD-003`.
- Соответствие архитектурным принципам ADR-003 (модульность, тестируемость).

### 7.2. Отрицательные / риски
- **Объём работы:** 3-6 месяцев на этапы A+B; полный объём (A+B+C+D) — до года.
- **Ответственность за корректность портированных алгоритмов:** митигируется TDD с эталонными BER-векторами и кросс-валидацией с GNU Radio.
- **Проект под GPL-3.0:** нельзя встроить в проприетарные продукты. Осознанный выбор (§ 2.2).
- **Зависимость от kaitai-struct-compiler:** требует JVM или Docker на dev-машине для генерации Go из `.ksy` (на проде уже сгенерированный Go-код, JVM не нужна).
