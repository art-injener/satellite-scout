// spectrum-stream.js — приём бинарного потока спектра (ADR-002, кадры SSTM).
//
// Поток — одно долгое HTTP-соединение: сервер не закрывает ответ и дописывает
// в него кадры по мере расчёта, браузер читает их через ReadableStream.
// EventSource бинарные данные не передаёт, поэтому поток идёт мимо SSE.
//
// Заголовок кадра 40 байт, little-endian:
//   0 magic 'SSTM' | 4 версия | 5 тип данных | 6 режим отображения | 7 резерв
//   8 timestamp_ms (u64) | 16 центр, Гц (i64) | 24 полоса, Гц (i64)
//   32 длина полезной нагрузки (u32) | 36 номер кадра (u32)
// Полезная нагрузка FFT — float32 dBFS; звука (/audio) — int16, отдаётся в долях
// полной шкалы, центр — частота канала, полоса — частота отсчётов звука; у heartbeat её нет.

'use strict';

(function() {
    const HEADER_SIZE = 40;
    const MAGIC = 0x4D545353;
    const DATA_TYPE_FFT_DB = 0x01;
    const DATA_TYPE_PCM_I16 = 0x03;
    const TWO_32 = 0x100000000;
    const RECONNECT_MIN_MS = 1000;
    const RECONNECT_MAX_MS = 15000;

    /** Беззнаковое 64-битное поле; частоты до 10 ГГц точно помещаются в Number. */
    function readU64(dv, off) {
        return dv.getUint32(off, true) + dv.getUint32(off + 4, true) * TWO_32;
    }

    /**
     * @param {DataView} dv
     * @param {number} off — смещение начала кадра
     */
    function parseHeader(dv, off) {
        return {
            magic: dv.getUint32(off, true),
            version: dv.getUint8(off + 4),
            dataType: dv.getUint8(off + 5),
            displayMode: dv.getUint8(off + 6),
            timestampMs: readU64(dv, off + 8),
            centerHz: readU64(dv, off + 16),
            spanHz: readU64(dv, off + 24),
            payloadLen: dv.getUint32(off + 32, true),
            seq: dv.getUint32(off + 36, true),
        };
    }

    /**
     * Собирает кадры из кусков потока: границы кусков с кадрами не совпадают.
     */
    function FrameReader() {
        this._buf = new Uint8Array(64 * 1024);
        this._len = 0;
    }

    /**
     * @param {Uint8Array} chunk
     * @returns {Array<{header: Object, values: ?Float32Array}>} values — бины FFT или звук
     * @throws {Error} если поток рассинхронизирован (нет magic в начале кадра)
     */
    FrameReader.prototype.push = function(chunk) {
        this._append(chunk);
        const frames = [];
        const dv = new DataView(this._buf.buffer, 0, this._len);
        let off = 0;
        while (this._len - off >= HEADER_SIZE) {
            const header = parseHeader(dv, off);
            if (header.magic !== MAGIC) {
                this._len = 0;
                throw new Error('spectrum stream: bad magic 0x' + header.magic.toString(16));
            }
            const end = off + HEADER_SIZE + header.payloadLen;
            if (end > this._len) { break; }
            let values = null;
            const base = off + HEADER_SIZE;
            if (header.dataType === DATA_TYPE_FFT_DB) {
                const n = header.payloadLen >> 2;
                values = new Float32Array(n);
                for (let i = 0; i < n; i++) { values[i] = dv.getFloat32(base + i * 4, true); }
            } else if (header.dataType === DATA_TYPE_PCM_I16) {
                const n = header.payloadLen >> 1;
                values = new Float32Array(n);
                for (let i = 0; i < n; i++) { values[i] = dv.getInt16(base + i * 2, true) / 32768; }
            }
            frames.push({ header: header, values: values });
            off = end;
        }
        if (off > 0) {
            this._buf.copyWithin(0, off, this._len);
            this._len -= off;
        }
        return frames;
    };

    FrameReader.prototype._append = function(chunk) {
        const need = this._len + chunk.length;
        if (need > this._buf.length) {
            let size = this._buf.length * 2;
            while (size < need) { size *= 2; }
            const next = new Uint8Array(size);
            next.set(this._buf.subarray(0, this._len));
            this._buf = next;
        }
        this._buf.set(chunk, this._len);
        this._len = need;
    };

    /**
     * dBFS в шкалу 0…1, которую ждут FFTSpectrumView и WaterfallView.
     * @param {Float32Array} dbfs
     * @param {number} dbMin — низ шкалы
     * @param {number} dbMax — верх шкалы
     */
    function dbfsToUnit(dbfs, dbMin, dbMax) {
        const out = new Float32Array(dbfs.length);
        const range = dbMax - dbMin;
        for (let i = 0; i < dbfs.length; i++) {
            const v = (dbfs[i] - dbMin) / range;
            out[i] = v < 0 ? 0 : (v > 1 ? 1 : v);
        }
        return out;
    }

    /**
     * Сдвиг из dBFS в dBm на входе антенны: поправка приёмника минус его усиление.
     * null — показываем dBFS: нет поправки, усиление неизвестно или включена АРУ
     * (отрицательное усиление в статусе приёмника).
     * @param {*} calibrationDb — receiver.calibration_db тракта
     * @param {*} gainDb — gain_db из статуса приёмника
     * @returns {?number}
     */
    function dbmOffset(calibrationDb, gainDb) {
        if (typeof calibrationDb !== 'number' || !isFinite(calibrationDb)) { return null; }
        if (typeof gainDb !== 'number' || !isFinite(gainDb) || gainDb < 0) { return null; }
        return calibrationDb - gainDb;
    }

    /**
     * Копия кадра, сдвинутая на offset дБ.
     * @param {Float32Array} values
     * @param {number} offset
     */
    function shiftLevels(values, offset) {
        const out = new Float32Array(values.length);
        for (let i = 0; i < values.length; i++) { out[i] = values[i] + offset; }
        return out;
    }

    // Автодиапазон шкалы dB
    const RANGE_FLOOR_MARGIN_DB = 10;  // запас под уровнем шума
    const RANGE_MIN_SPAN_DB = 40;      // окно над шумом, даже когда сигналов нет
    const RANGE_PEAK_MARGIN_DB = 5;    // запас над пиком
    const RANGE_STEP_DB = 5;           // границы кратны шагу, чтобы сетка не дрожала
    const RANGE_FLOOR_ALPHA = 0.1;     // сглаживание уровня шума по кадрам
    const RANGE_PEAK_DECAY_DB = 0.5;   // спад удержания пика за кадр

    /**
     * Шкала dB по самим данным: низ чуть ниже шума, верх над недавним пиком.
     * Шум — медиана кадра: несущие, занимающие меньше половины полосы, его не поднимают.
     * Низ сдвигается, только когда шум ушёл дальше шага; верх растёт сразу,
     * а сжимается, когда запас превысил два шага: водопад не перекрашивается от дрожи шума.
     */
    function AutoRange() {
        this._floor = null;
        this._peak = null;
        this.min = null;
        this.max = null;
    }

    /**
     * @param {Float32Array} dbfs — кадр спектра
     * @param {?Float32Array} [heldDbfs] — удержание рядом с кадром: поднимает только верх,
     *        шум удержания выше живого и низ по нему не считается
     * @returns {{min: number, max: number}}
     */
    AutoRange.prototype.update = function(dbfs, heldDbfs) {
        const sorted = Float32Array.from(dbfs).sort();
        const median = sorted[sorted.length >> 1];
        let top = sorted[sorted.length - 1];
        if (heldDbfs) {
            for (let i = 0; i < heldDbfs.length; i++) {
                if (heldDbfs[i] > top) { top = heldDbfs[i]; }
            }
        }
        this._floor = this._floor === null ? median : this._floor + RANGE_FLOOR_ALPHA * (median - this._floor);
        this._peak = this._peak === null ? top : Math.max(top, this._peak - RANGE_PEAK_DECAY_DB);

        const lo = this._floor - RANGE_FLOOR_MARGIN_DB;
        const hi = Math.max(this._floor + RANGE_MIN_SPAN_DB, this._peak + RANGE_PEAK_MARGIN_DB);
        if (this.min === null || Math.abs(lo - this.min) > RANGE_STEP_DB) {
            this.min = Math.floor(lo / RANGE_STEP_DB) * RANGE_STEP_DB;
        }
        if (this.max === null || hi > this.max || hi < this.max - 2 * RANGE_STEP_DB) {
            this.max = Math.ceil(hi / RANGE_STEP_DB) * RANGE_STEP_DB;
        }
        return { min: this.min, max: this.max };
    };

    /**
     * Плавное обновление линии спектра на экране: экспоненциальное сглаживание по бинам.
     * Это не серверное «Накопление» (avg по окну FFT), а только сглаживание картинки,
     * как у анализаторов спектра / осциллографов: поток быстрее глаза, рисуем спокойнее.
     * @param {number} [alpha] — доля нового кадра (0…1); 1 = без сглаживания
     */
    function DisplaySmooth(alpha) {
        this._alpha = typeof alpha === 'number' && alpha > 0 && alpha <= 1 ? alpha : 0.35;
        this._prev = null;
    }

    /** Сброс при смене полосы, единиц или переоткрытии потока. */
    DisplaySmooth.prototype.reset = function() {
        this._prev = null;
    };

    /**
     * @param {Float32Array} dbfs — новый кадр (не меняется)
     * @returns {Float32Array} сглаженный кадр (свой буфер, не держать дольше одного кадра UI)
     */
    DisplaySmooth.prototype.update = function(dbfs) {
        if (!this._prev || this._prev.length !== dbfs.length) {
            this._prev = new Float32Array(dbfs.length);
            this._prev.set(dbfs);
            return this._prev;
        }
        const a = this._alpha;
        const out = this._prev;
        for (let i = 0; i < dbfs.length; i++) {
            out[i] += a * (dbfs[i] - out[i]);
        }
        return out;
    };

    /**
     * Измеритель под курсором. Доля frac — положение указателя на графике, 0 у левого края.
     * Частота — точка оси под указателем. Уровень — ближайший бин линии (бин i на доле i / (n - 1)).
     * @param {?Float32Array} dbfs — видимые бины, уже в единицах экрана
     * @param {number} centerHz
     * @param {number} spanHz
     * @param {number} frac
     * @returns {?{freqHz: number, dbfs: number, index: number}}
     */
    function cursorSample(dbfs, centerHz, spanHz, frac) {
        if (!dbfs || dbfs.length < 2 || !(spanHz > 0)) { return null; }
        if (typeof frac !== 'number' || !isFinite(frac) || frac < 0 || frac > 1) { return null; }
        const last = dbfs.length - 1;
        let idx = Math.round(frac * last);
        if (idx < 0) { idx = 0; }
        if (idx > last) { idx = last; }
        return {
            freqHz: centerHz - spanHz / 2 + frac * spanHz,
            dbfs: dbfs[idx],
            index: idx,
        };
    }

    /**
     * Маркер на пике кадра. Частота бина — как у перекрестья на спектре (от левого края полосы),
     * шумовая полка — медиана кадра, как у AutoRange. С/Ш — пик над полкой в одном бине БПФ.
     * @param {?Float32Array} dbfs
     * @param {number} centerHz
     * @param {number} spanHz
     * @returns {?{freqHz: number, dbfs: number, floorDbfs: number, snrDb: number}}
     */
    function markerReadout(dbfs, centerHz, spanHz) {
        if (!dbfs || dbfs.length === 0) { return null; }
        let idx = 0;
        for (let i = 1; i < dbfs.length; i++) {
            if (dbfs[i] > dbfs[idx]) { idx = i; }
        }
        const sorted = Float32Array.from(dbfs).sort();
        const floor = sorted[sorted.length >> 1];
        return {
            freqHz: centerHz - spanHz / 2 + idx * spanHz / dbfs.length,
            dbfs: dbfs[idx],
            floorDbfs: floor,
            snrDb: dbfs[idx] - floor,
        };
    }

    // Окно масштаба спектра: {offsetHz, spanHz} — центр видимой части от центра
    // приёмника и её ширина; null — видна вся полоса.

    /** Окно целиком внутри полосы приёмника. */
    function clampView(offsetHz, spanHz, fullSpanHz) {
        const limit = Math.max(0, (fullSpanHz - spanHz) / 2);
        return { offsetHz: Math.max(-limit, Math.min(limit, offsetHz)), spanHz: spanHz };
    }

    /**
     * Масштаб в factor раз (больше 1 — крупнее) вокруг частоты atHz от центра приёмника:
     * точка под указателем остаётся на месте.
     * @returns {?{offsetHz: number, spanHz: number}} null, если окно дошло до всей полосы
     */
    function zoomView(view, fullSpanHz, factor, atHz, minSpanHz) {
        const cur = view || { offsetHz: 0, spanHz: fullSpanHz };
        let span = cur.spanHz / factor;
        if (!(span < fullSpanHz)) { return null; }
        span = Math.max(span, Math.min(minSpanHz, fullSpanHz));
        const offset = atHz - (atHz - cur.offsetHz) * span / cur.spanHz;
        return clampView(offset, span, fullSpanHz);
    }

    /** Сдвиг окна на dHz; вся полоса не сдвигается. */
    function panView(view, fullSpanHz, dHz) {
        if (!view) { return null; }
        return clampView(view.offsetHz + dHz, view.spanHz, fullSpanHz);
    }

    /**
     * Бины кадра, попавшие в окно. Центр и ширина пересчитаны по сетке бинов:
     * крайние бины ложатся ровно на края графика.
     * @param {number} nBins — бинов в кадре на всю полосу
     * @returns {{start: number, count: number, offsetHz: number, spanHz: number}}
     */
    function viewSlice(nBins, fullSpanHz, view) {
        const binHz = fullSpanHz / nBins;
        const count = Math.min(nBins, Math.max(2, Math.round(view.spanHz / binHz) + 1));
        const first = Math.round((view.offsetHz - view.spanHz / 2 + fullSpanHz / 2) / binHz);
        const start = Math.max(0, Math.min(nBins - count, first));
        return {
            start: start,
            count: count,
            offsetHz: -fullSpanHz / 2 + (start + (count - 1) / 2) * binHz,
            spanHz: (count - 1) * binHz,
        };
    }

    /**
     * Чтение потока с переподключением.
     * @param {{url: string,
     *          onFrame: function({header: Object, values: ?Float32Array}): void,
     *          onStatus?: function({state: string, message?: string}): void,
     *          fetchImpl?: Function, timers?: {setTimeout: Function, clearTimeout: Function}}} opts
     * Состояния: connecting, live (пришёл первый кадр), error, stopped.
     */
    function SpectrumStream(opts) {
        this._url = opts.url;
        this._onFrame = opts.onFrame;
        this._onStatus = opts.onStatus || function() {};
        this._fetch = opts.fetchImpl || function(u, i) { return fetch(u, i); };
        this._timers = opts.timers || {
            setTimeout: function(fn, ms) { return setTimeout(fn, ms); },
            clearTimeout: function(id) { clearTimeout(id); },
        };
        this._running = false;
        this._abort = null;
        this._retryTimer = null;
        this._retryMs = RECONNECT_MIN_MS;
        // Номер подключения: результат устаревшего подключения игнорируется
        this._gen = 0;
    }

    SpectrumStream.prototype.isRunning = function() {
        return this._running;
    };

    SpectrumStream.prototype.start = function() {
        if (this._running) { return; }
        this._running = true;
        this._retryMs = RECONNECT_MIN_MS;
        this._connect();
    };

    SpectrumStream.prototype.stop = function() {
        if (!this._running) { return; }
        this._running = false;
        this._gen++;
        if (this._retryTimer !== null) {
            this._timers.clearTimeout(this._retryTimer);
            this._retryTimer = null;
        }
        if (this._abort) { this._abort.abort(); this._abort = null; }
        this._onStatus({ state: 'stopped' });
    };

    SpectrumStream.prototype._connect = function() {
        const self = this;
        const gen = ++this._gen;
        const abort = new AbortController();
        this._abort = abort;
        const reader = new FrameReader();
        let live = false;
        this._onStatus({ state: 'connecting' });

        this._fetch(this._url, { signal: abort.signal, cache: 'no-store', credentials: 'same-origin' })
            .then(function(resp) {
                if (!resp.ok) {
                    return resp.text().then(function(text) {
                        throw new Error('HTTP ' + resp.status + ': ' + String(text || '').trim());
                    });
                }
                const body = resp.body.getReader();
                const pump = function() {
                    return body.read().then(function(r) {
                        if (gen !== self._gen) { return null; }
                        if (r.done) { throw new Error('поток закрыт сервером'); }
                        const frames = reader.push(r.value);
                        for (let i = 0; i < frames.length; i++) {
                            if (!live) {
                                live = true;
                                self._retryMs = RECONNECT_MIN_MS;
                                self._onStatus({ state: 'live' });
                            }
                            self._onFrame(frames[i]);
                        }
                        return pump();
                    });
                };
                return pump();
            })
            .catch(function(err) {
                if (gen !== self._gen || !self._running) { return; }
                abort.abort();
                self._onStatus({ state: 'error', message: err && err.message ? err.message : String(err) });
                self._scheduleRetry();
            });
    };

    SpectrumStream.prototype._scheduleRetry = function() {
        const self = this;
        const delay = this._retryMs;
        this._retryMs = Math.min(this._retryMs * 2, RECONNECT_MAX_MS);
        this._retryTimer = this._timers.setTimeout(function() {
            self._retryTimer = null;
            if (self._running) { self._connect(); }
        }, delay);
    };

    SpectrumStream.FrameReader = FrameReader;
    SpectrumStream.parseHeader = parseHeader;
    SpectrumStream.dbfsToUnit = dbfsToUnit;
    SpectrumStream.AutoRange = AutoRange;
    SpectrumStream.DisplaySmooth = DisplaySmooth;
    SpectrumStream.markerReadout = markerReadout;
    SpectrumStream.cursorSample = cursorSample;
    SpectrumStream.dbmOffset = dbmOffset;
    SpectrumStream.shiftLevels = shiftLevels;
    SpectrumStream.zoomView = zoomView;
    SpectrumStream.panView = panView;
    SpectrumStream.viewSlice = viewSlice;

    if (typeof window !== 'undefined') {
        window.SpectrumStream = SpectrumStream;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            SpectrumStream: SpectrumStream,
            FrameReader: FrameReader,
            parseHeader: parseHeader,
            dbfsToUnit: dbfsToUnit,
            AutoRange: AutoRange,
            DisplaySmooth: DisplaySmooth,
            markerReadout: markerReadout,
            cursorSample: cursorSample,
            dbmOffset: dbmOffset,
            shiftLevels: shiftLevels,
            zoomView: zoomView,
            panView: panView,
            viewSlice: viewSlice,
        };
    }
})();
