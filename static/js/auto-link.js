/**
 * Auto-link — связка нижней панели Авто-режима.
 *
 * Единый список передатчиков всех КА группы (UI-заголовок «Передатчики»):
 * на каждой строке — частота, модуляция, водопад, доплер.
 *
 * Источники данных:
 *  - Список передатчиков — REST `GET /api/satnogs/transmitters/{norad}`.
 *  - Состав группы — SSE-событие `satellite_group_update`.
 */

(function() {
    'use strict';

    /**
     * Сколько слотов в ленте визитов (совпадает с stripCapacity на бэкенде).
     * Бэкенд хранит кольцевой буфер такого же размера и отдаёт history[].
     */
    const STRIP_CAPACITY = 10;

    /**
     * Fingerprint состава группы: только набор NORAD (без primary/tracking/AOS/LOS).
     * Смена primary не должна сносить водопады и пересобирать DOM.
     * @param {Object} data — объект satellite_group_update.
     * @returns {string}
     */
    function groupFingerprint(data) {
        if (!data || !Array.isArray(data.satellites)) { return ''; }
        return data.satellites
            .map((s) => Number(s.norad_id) || 0)
            .filter((n) => n > 0)
            .sort((a, b) => a - b)
            .join(',');
    }

    function txListFingerprint(rows) {
        if (!rows || !rows.length) { return ''; }
        return rows.map((r) => r.id).filter(Boolean).join(',');
    }

    /** Ключ localStorage для режима раскладки ('h' | 'v'). */
    const LS_LAYOUT_KEY = 'ux.autoLinkLayout';

    /** Текущая раскладка: 'h' — горизонтальная (по умолчанию), 'v' — вертикальная. */
    function getLayoutMode() {
        return localStorage.getItem(LS_LAYOUT_KEY) === 'v' ? 'v' : 'h';
    }
    function setLayoutMode(mode) {
        localStorage.setItem(LS_LAYOUT_KEY, mode === 'v' ? 'v' : 'h');
    }


    /**
     * Форматирование скорости передачи (бод). 1200 → "1200", 19200 → "19k2".
     */
    function formatBaud(b) {
        const v = Number(b) || 0;
        if (v <= 0) { return ''; }
        if (v >= 10000) {
            const k = v / 1000;
            const intK = Math.trunc(k);
            const rem = Math.round((k - intK) * 10);
            return rem === 0 ? `${intK}k` : `${intK}k${rem}`;
        }
        return String(Math.round(v));
    }

    /** Преобразование downlink_low (Hz) → строка «145.825». */
    function freqMHz(hz) {
        const v = Number(hz) || 0;
        if (v <= 0) { return ''; }
        return (v / 1e6).toFixed(3);
    }

    /** Преобразование SatNOGS-передатчика в строку UI; null для неактивных. */
    function txFromSatnogs(sat, t) {
        if (!t || !t.alive) { return null; }
        if (t.status && String(t.status).toLowerCase() !== 'active') { return null; }
        const dl = Number(t.downlink_low) || 0;
        if (dl <= 0) { return null; }
        const norad = Number(sat.norad_id) || 0;
        const uuid = t.uuid || '';
        const f = freqMHz(dl);
        const mode = t.mode || '';
        const baud = formatBaud(t.baud);
        const modulation = baud ? `${mode} ${baud}`.trim() : mode;
        return {
            id: `tx-${norad}-${uuid || Math.random().toString(36).slice(2, 8)}`,
            uuid: uuid || '',
            satNoradId: norad,
            satLabel: sat.sat_name || `NORAD ${norad}`,
            freqHz: dl,
            freqMHz: f,
            mode: modulation,
            description: t.description || '',
        };
    }

    /**
     * Уровень-категория квадратика по power (0..1) — соответствует CSS-классу
     * .auto-link__cell--lvlN. Шкала фиксированная, чтобы цвета между строками
     * читались одинаково.
     */
    function powerLevelClass(power, packets) {
        if (!packets || packets <= 0) { return 'auto-link__cell--silent'; }
        const p = Math.max(0, Math.min(1, Number(power) || 0));
        if (p < 0.20) { return 'auto-link__cell--lvl1'; }
        if (p < 0.40) { return 'auto-link__cell--lvl2'; }
        if (p < 0.60) { return 'auto-link__cell--lvl3'; }
        if (p < 0.80) { return 'auto-link__cell--lvl4'; }
        return 'auto-link__cell--lvl5';
    }

    /** Подпись внутри квадратика: число пакетов, точка для тишины, «99+». */
    function cellLabel(cell) {
        const n = cell && cell.packets > 0 ? cell.packets : 0;
        if (n <= 0) { return '·'; }
        if (n > 99) { return '99+'; }
        return String(n);
    }

    /**
     * Преобразование нормированной мощности 0..1 в условный уровень dBm.
     * 0 → −100 dBm, 1 → −30 dBm. null — если сигнала нет.
     */
    function powerToDbm(power) {
        const v = Math.max(0, Math.min(1, Number(power) || 0));
        if (v <= 0) { return null; }
        return Math.round(-100 + v * 70);
    }

    /** Скорость света (м/с) — для расчёта доплера. */
    const C_LIGHT = 299792458;

    /**
     * Доплеровский сдвиг частоты приёма (Гц) для нисходящей линии.
     * Положительное значение range_rate (м/с) = спутник удаляется → df < 0 (ниже несущей).
     */
    function dopplerHz(freqHz, rangeRateMps) {
        const f = Number(freqHz) || 0;
        const rr = Number(rangeRateMps) || 0;
        if (f <= 0 || rr === 0) { return 0; }
        return -f * rr / C_LIGHT;
    }

    /** Формат доплера: "+1.32 кГц" / "−0.85 кГц"; знак Unicode «−». */
    function formatDopplerKhz(hz) {
        const k = (Number(hz) || 0) / 1000;
        if (!Number.isFinite(k) || Math.abs(k) < 0.005) {
            return '0.00 кГц';
        }
        const abs = Math.abs(k).toFixed(2);
        return (k > 0 ? '+' : '\u2212') + abs + ' кГц';
    }

    /** Цветовой класс RSSI-бара по нормированной мощности (0..1). */
    function rssiBarColor(power) {
        const p = Math.max(0, Math.min(1, Number(power) || 0));
        if (p < 0.20) { return 'var(--accent-danger, #ff5722)'; }
        if (p < 0.40) { return 'var(--accent-warning, #ffb300)'; }
        if (p < 0.60) { return '#cddc39'; }
        return 'var(--accent-success, #4caf50)';
    }

    /** Уровень SNR: low / mid / high — управляет цветом текста. */
    function snrLevel(snrDb) {
        const v = Number(snrDb) || 0;
        if (v <= 0) { return 'silent'; }
        if (v < 8) { return 'low'; }
        if (v < 14) { return 'mid'; }
        return 'high';
    }

    /** CSS-класс LED для Lock (горизонтальная раскладка). */
    function lockLedClass(lock) {
        const v = String(lock || '').toUpperCase();
        if (v === 'OK') { return 'auto-link__tx-lock__led--ok'; }
        if (v === 'SEARCH') { return 'auto-link__tx-lock__led--search'; }
        return 'auto-link__tx-lock__led--lost';
    }

    /** CSS-класс LED для Lock (вертикальная раскладка — мельче, отдельный неймспейс). */
    function vLockLedClass(lock) {
        const v = String(lock || '').toUpperCase();
        if (v === 'OK') { return 'auto-link__v-rssi__led--ok'; }
        if (v === 'SEARCH') { return 'auto-link__v-rssi__led--search'; }
        return 'auto-link__v-rssi__led--lost';
    }

    /**
     * Решение подсветки строки/группы TX для связки План ↔ auto-link.
     * @returns {{ group: boolean, tx: boolean }}
     */
    function resolveLinkHighlight(hNorad, hTxRowId, selectedNorad, rowNorad, rowTxId) {
        const norad = Number(rowNorad) || 0;
        const txId = rowTxId || '';
        if (hTxRowId) {
            return { group: false, tx: txId === hTxRowId };
        }
        if (hNorad && norad === hNorad) {
            return { group: true, tx: true };
        }
        if (!hNorad && !hTxRowId && selectedNorad && norad === selectedNorad) {
            return { group: true, tx: true };
        }
        return { group: false, tx: false };
    }

    /** Индекс обновлений tx_cycle по UUID передатчика. */
    function indexTxCycleUpdates(payload) {
        const map = new Map();
        if (!payload || !Array.isArray(payload.satellites)) { return map; }
        for (const sat of payload.satellites) {
            if (!sat || !Array.isArray(sat.transmitters)) { continue; }
            for (const tx of sat.transmitters) {
                if (tx && tx.uuid) {
                    map.set(tx.uuid, tx);
                }
            }
        }
        return map;
    }

    /** Применить снимок tx_cycle к объекту строки передатчика. */
    function applyTxCycleUpdate(row, upd) {
        if (!row || !upd) { return; }
        row.power = Number(upd.power) || 0;
        row.lock = upd.lock || 'LOST';
        row.snrDb = Number(upd.snr_db) || 0;
        row.totalPackets = Number(upd.total_packets) || 0;
        row.totalFailed = Number(upd.total_failed) || 0;
        row.packetsFailed = Number(upd.packets_failed) || 0;
        row.history = Array.isArray(upd.history) ? upd.history.slice() : [];
    }

    const LINK_HOVER_EVENT = 'satellite-scout-link-hover';
    const TX_CYCLE_EVENT = 'satellite-scout-tx-cycle';

    function dispatchLinkHover(detail) {
        document.dispatchEvent(new CustomEvent(LINK_HOVER_EVENT, { detail: detail || {} }));
    }

    /** Заранее создать фиксированный ряд пустых квадратов в ленте. */
    function ensureStripPool(stripEl, capacity) {
        while (stripEl.children.length < capacity) {
            const c = document.createElement('div');
            c.className = 'auto-link__cell auto-link__cell--silent';
            stripEl.appendChild(c);
        }
        while (stripEl.children.length > capacity) {
            stripEl.removeChild(stripEl.lastChild);
        }
    }

    /**
     * Перерисовать ленту: слева новейший визит, дальше вправо — более старые,
     * справа — зарезервированные пустые слоты (outline), пока история не заполнит ряд.
     * @param {HTMLElement} stripEl
     * @param {Array} items — массив {packets, power} от новейшего к старейшему (с бэка).
     * @param {number} capacity
     */
    function renderStrip(stripEl, items, capacity) {
        ensureStripPool(stripEl, capacity);
        const n = items.length;

        for (let i = 0; i < capacity; i++) {
            const cellEl = stripEl.children[i];
            if (i >= n) {
                if (cellEl.className !== 'auto-link__cell auto-link__cell--silent') {
                    cellEl.className = 'auto-link__cell auto-link__cell--silent';
                }
                if (cellEl.textContent !== '') { cellEl.textContent = ''; }
                continue;
            }
            const item = items[i];
            const cls = `auto-link__cell ${powerLevelClass(item.power, item.packets)}`;
            if (cellEl.className !== cls) { cellEl.className = cls; }
            const lbl = cellLabel(item);
            if (cellEl.textContent !== lbl) { cellEl.textContent = lbl; }
        }

        // Новый визит всегда у левого края видимой области.
        stripEl.scrollLeft = 0;
    }

    /**
     * Толщина яркой полосы сигнала в водопаде как доля высоты строки (0..1).
     * Кодирует тип модуляции: CW — тонкая линия, GMSK/FSK — широкая полоса.
     */
    function modulationBandFrac(mode) {
        const m = String(mode || '').toUpperCase();
        if (m.indexOf('CW') !== -1) { return 0.12; }
        if (m.indexOf('GMSK') !== -1 || m.indexOf('GFSK') !== -1) { return 0.42; }
        if (m.indexOf('FSK') !== -1) { return 0.34; }
        if (m.indexOf('PSK') !== -1) { return 0.28; }
        if (m.indexOf('AFSK') !== -1) { return 0.20; }
        if (m.indexOf('FM') !== -1) { return 0.5; }
        return 0.24;
    }

    /** Холодный фон водопада как [r,g,b]. */
    function wfColdRgb() {
        if (typeof window.cssVarRgbHex === 'function') {
            return window.cssVarRgbHex('--waterfall-cold-bg', '#06101a');
        }
        return [6, 16, 26];
    }

    /**
     * WaterfallCell — живой спектрограф одного передатчика на отдельном canvas.
     *
     * Ось времени бежит непрерывно (каждый кадр — новая линия спектра):
     *   вертикаль  — новое сверху, старое уезжает вниз;
     *   горизонталь — новое слева, старое уезжает вправо.
     * Поперечная ось — частота: шумовая подложка с зерном + гауссова полоса
     * сигнала по центру. Ширина полосы = модуляция (bandFrac), яркость = power.
     * Прокрутка — drawImage canvas «сам на себя» со сдвигом на 1px.
     */
    class WaterfallCell {
        constructor(canvas, opts) {
            this.canvas = canvas;
            this.ctx = canvas.getContext('2d');
            this.vertical = Boolean(opts && opts.vertical);
            this.bandFrac = (opts && opts.bandFrac) || 0.24;
            this.power = 0;
            this._w = 0;
            this._h = 0;
            this._line = null;
            /** Последний зафиксированный CSS-размер — защита от дрожания 1px. */
            this._cssW = 0;
            this._cssH = 0;
        }

        setPower(p) {
            this.power = Math.max(0, Math.min(1, Number(p) || 0));
        }

        /** Подогнать размер под CSS; сброс буфера только при заметном изменении размера. */
        _ensure() {
            const rect = this.canvas.getBoundingClientRect();
            const w = Math.floor(rect.width);
            const h = Math.floor(rect.height);
            if (w < 2 || h < 2) { return false; }
            const dw = Math.abs(w - this._cssW);
            const dh = Math.abs(h - this._cssH);
            if (this._cssW > 0 && this._cssH > 0 && dw < 2 && dh < 2) {
                return true;
            }
            this.canvas.width = w;
            this.canvas.height = h;
            this._w = w;
            this._h = h;
            this._cssW = w;
            this._cssH = h;
            this._line = null;
            const cold = wfColdRgb();
            this.ctx.fillStyle = `rgb(${cold[0]}, ${cold[1]}, ${cold[2]})`;
            this.ctx.fillRect(0, 0, w, h);
            return true;
        }

        /** Сгенерировать одну линию спектра вдоль поперечной (частотной) оси. */
        _genLine() {
            const cross = this.vertical ? this._w : this._h;
            if (!this._line || this._line.lineLen !== cross) {
                // Вертикаль: линия w×1; горизонталь: линия 1×h. Индекс пикселя = c*4.
                const img = this.vertical
                    ? this.ctx.createImageData(this._w, 1)
                    : this.ctx.createImageData(1, this._h);
                img.lineLen = cross;
                this._line = img;
            }
            const data = this._line.data;
            const center = cross / 2;
            const sigma = Math.max(1.2, this.bandFrac * cross * 0.3);
            const p = this.power;
            const hot = window.waterfallColormap ? window.waterfallColormap() : window.hotColor;

            for (let c = 0; c < cross; c++) {
                const noise = 0.05 + Math.random() * 0.07;
                let val = noise;
                if (p > 0) {
                    const d = (c - center) / sigma;
                    const g = Math.exp(-0.5 * d * d);
                    val += p * g * (0.78 + Math.random() * 0.4);
                }
                if (val > 1) { val = 1; }
                let r, gg, b;
                if (hot) {
                    const col = hot(val);
                    r = col[0]; gg = col[1]; b = col[2];
                } else {
                    r = 0; gg = Math.round(val * 255); b = 0;
                }
                const idx = c * 4;
                data[idx] = r;
                data[idx + 1] = gg;
                data[idx + 2] = b;
                data[idx + 3] = 255;
            }
            return this._line;
        }

        /** Один кадр: сдвиг на 1px + новая линия у активного края. */
        tick() {
            if (!this._ensure()) { return; }
            const ctx = this.ctx;
            if (this.vertical) {
                ctx.drawImage(this.canvas, 0, 1);
                ctx.putImageData(this._genLine(), 0, 0);
            } else {
                ctx.drawImage(this.canvas, 1, 0);
                ctx.putImageData(this._genLine(), 0, 0);
            }
        }
    }

    /**
     * OverviewLink — контроллер связки auto-link.
     *
     * @param {Object} stateManager — экземпляр SatelliteStateManager.
     * @param {Object} opts
     * @param {HTMLElement} opts.txListEl — контейнер списка передатчиков.
     */
    class OverviewLink {
        constructor(stateManager, opts) {
            if (!stateManager) {
                throw new Error('OverviewLink: stateManager is required');
            }
            if (!opts || !opts.txListEl) {
                throw new Error('OverviewLink: txListEl обязателен');
            }
            this._sm = stateManager;
            this._listEl = opts.txListEl;
            this._rootEl = this._listEl.closest('.auto-link');

            /** Текущая раскладка: 'h' или 'v'. */
            this._layout = getLayoutMode();
            this._applyLayoutClass();

            /** Текущие строки/колонки передатчиков. */
            this._rows = [];
            /** Кеш ответов SatNOGS: norad → Promise<rows>. */
            this._txCache = new Map();
            /** Версия группы — защита от гонки async-запросов. */
            this._groupVersion = 0;
            /** Последние данные групп (для перерисовки при смене layout). */
            this._lastGroups = null;
            /** Fingerprint последней обработанной группы — для пропуска ненужных rebuild. */
            /** Fingerprint состава группы (NORAD) — полный rebuild только при смене. */
            this._lastCompFP = '';
            /** Сохранённые строки TX (id → объект с wfCell) для инкрементального DOM. */
            this._rowRegistry = new Map();
            /** Hover-связка с Планом сеансов (временная, без смены selected). */
            this._linkHoverNorad = null;
            this._linkHoverTxRowId = null;
            /** Пауза анимации водопадов в Ручном режиме. */
            this._paused = false;

            this._onGroupUpdateBound = (data) => this._onGroupUpdate(data);
            this._sm.subscribe(window.StateEventType.SATELLITE_GROUP_UPDATE, this._onGroupUpdateBound);

            this._onSelectedChangeBound = (state) => this._onSelectedChange(state);
            this._sm.subscribe(window.StateEventType.SELECTED_CHANGE, this._onSelectedChangeBound);

            this._onClickBound = (e) => this._onClick(e);
            this._listEl.addEventListener('click', this._onClickBound);

            this._onListPointerBound = (e) => this._onListPointer(e);
            this._listEl.addEventListener('mouseover', this._onListPointerBound);
            this._listEl.addEventListener('mouseout', this._onListPointerBound);

            this._onLinkHoverBound = (ev) => this._onLinkHover(ev);
            document.addEventListener(LINK_HOVER_EVENT, this._onLinkHoverBound);

            this._onTxCycleBound = (ev) => this._onTxCycle(ev);
            document.addEventListener(TX_CYCLE_EVENT, this._onTxCycleBound);

            // Если группа уже была получена до инициализации — отрисовать сразу.
            const existing = this._sm.getSatelliteGroup && this._sm.getSatelliteGroup();
            if (existing) { this._onGroupUpdate(existing); }

            // Непрерывная анимация водопадов всех передатчиков.
            this._wfRaf = 0;
            this._startWaterfall();

            // Подписи футера колонок (El, пакеты, LOS) — раз в секунду.
            this._labelTimer = setInterval(() => this._updateAllColLabels(), 1000);
        }

        /** Запустить анимационный цикл водопадов (~18 кадров/с). */
        _startWaterfall() {
            const self = this;
            const intervalMs = 1000 / 18;
            let last = 0;
            function frame(ts) {
                self._wfRaf = requestAnimationFrame(frame);
                if (self._paused) { return; }
                if (ts - last < intervalMs) { return; }
                last = ts;
                for (const row of self._rows) {
                    if (row.wfCell) { row.wfCell.tick(); }
                }
            }
            this._wfRaf = requestAnimationFrame(frame);
        }

        /** Применить CSS-класс раскладки на корневой элемент. */
        _applyLayoutClass() {
            if (!this._rootEl) { return; }
            this._rootEl.classList.toggle('auto-link--vertical', this._layout === 'v');
        }

        /** Переключить раскладку; вызывается кнопкой-флагом. */
        toggleLayout() {
            this._layout = this._layout === 'v' ? 'h' : 'v';
            setLayoutMode(this._layout);
            this._applyLayoutClass();
            this._rowRegistry.clear();
            if (this._lastGroups) {
                this._applyGroups(this._lastGroups, { forceFull: true });
            }
        }

        getLayout() { return this._layout; }

        /** Приостановить анимацию водопадов (Ручной режим). */
        pause() {
            this._paused = true;
        }

        /** Возобновить анимацию водопадов (Авто). */
        resume() {
            this._paused = false;
        }

        /** Освободить подписки. */
        destroy() {
            if (this._wfRaf) {
                cancelAnimationFrame(this._wfRaf);
                this._wfRaf = 0;
            }
            if (this._labelTimer) {
                clearInterval(this._labelTimer);
                this._labelTimer = null;
            }
            const sm = this._sm;
            if (sm && typeof sm.unsubscribe === 'function') {
                if (this._onGroupUpdateBound) {
                    sm.unsubscribe(window.StateEventType.SATELLITE_GROUP_UPDATE, this._onGroupUpdateBound);
                }
                if (this._onSelectedChangeBound) {
                    sm.unsubscribe(window.StateEventType.SELECTED_CHANGE, this._onSelectedChangeBound);
                }
            }
            this._onGroupUpdateBound = null;
            this._onSelectedChangeBound = null;
            if (this._onClickBound) {
                this._listEl.removeEventListener('click', this._onClickBound);
                this._onClickBound = null;
            }
            if (this._onListPointerBound) {
                this._listEl.removeEventListener('mouseover', this._onListPointerBound);
                this._listEl.removeEventListener('mouseout', this._onListPointerBound);
                this._onListPointerBound = null;
            }
            if (this._onLinkHoverBound) {
                document.removeEventListener(LINK_HOVER_EVENT, this._onLinkHoverBound);
                this._onLinkHoverBound = null;
            }
            if (this._onTxCycleBound) {
                document.removeEventListener(TX_CYCLE_EVENT, this._onTxCycleBound);
                this._onTxCycleBound = null;
            }
            this._txCache.clear();
            this._rowRegistry.clear();
        }

        // ----- Обновление группы -----

        _onGroupUpdate(data) {
            if (!data || !Array.isArray(data.satellites)) { return; }
            const compFp = groupFingerprint(data);
            if (compFp && compFp === this._lastCompFP) {
                this._softGroupUpdate(data);
                return;
            }
            this._lastCompFP = compFp;
            this._rebuildFromGroup(data.satellites);
        }

        /**
         * Группа та же (те же NORAD), но обновились AOS/LOS/видимость — без пересборки DOM.
         * Иначе водопады обнуляются при каждом satellite_group_update с бэка.
         */
        _softGroupUpdate(data) {
            const sats = data.satellites || [];
            const byNorad = new Map();
            for (let i = 0; i < sats.length; i++) {
                const s = sats[i];
                if (s && s.norad_id) { byNorad.set(s.norad_id, s); }
            }
            for (const row of this._rows) {
                const sat = byNorad.get(row.satNoradId);
                if (!sat) { continue; }
                row.satAos = sat.aos ? Number(sat.aos) : 0;
                row.satLos = sat.los ? Number(sat.los) : 0;
            }
            if (this._layout === 'v') {
                this._updateAllColLabels();
            }
        }

        _rebuildFromGroup(satellites) {
            this._groupVersion++;
            const myVersion = this._groupVersion;

            const tasks = satellites.map((s) => this._fetchTxForSat(s).then((rows) => ({ sat: s, rows })));
            Promise.all(tasks).then((results) => {
                if (myVersion !== this._groupVersion) { return; }
                this._applyGroups(results);
            }).catch((err) => {
                console.warn('[OverviewLink] rebuildFromGroup failed:', err);
            });
        }

        // ----- Рендер списка -----

        _renderSkeleton(satellites) {
            const el = this._listEl;
            el.textContent = '';
            if (satellites.length === 0) {
                const ph = document.createElement('p');
                ph.className = 'auto-link__placeholder';
                ph.textContent = 'Группа пуста — ждём ближайшие пролёты';
                el.appendChild(ph);
                return;
            }
            const frag = document.createDocumentFragment();
            for (const sat of satellites) {
                frag.appendChild(this._buildGroupSkeleton(sat));
            }
            el.appendChild(frag);
        }

        /** Оболочка группы одного КА. */
        _buildGroupShell(sat) {
            const grp = document.createElement('div');
            grp.className = 'auto-link__group';
            grp.dataset.norad = String(sat.norad_id);
            const grid = document.createElement('div');
            grid.className = 'auto-link__group-grid';
            grp.appendChild(grid);
            return { grp, grid };
        }

        /** Ячейка КА: имя, затем NORAD (как в плане сеансов), объединяет N строк TX. */
        _buildSatCell(sat, rowSpan) {
            const cell = document.createElement('div');
            cell.className = 'auto-link__sat-cell';
            cell.style.gridRow = `1 / span ${Math.max(1, rowSpan)}`;

            const satName = sat.sat_name || `NORAD ${sat.norad_id}`;
            const noradStr = String(sat.norad_id);

            const nameEl = document.createElement('span');
            nameEl.className = 'auto-link__sat-name';
            nameEl.textContent = satName;

            const noradEl = document.createElement('span');
            noradEl.className = 'auto-link__sat-norad';
            noradEl.textContent = noradStr;

            cell.title = `${satName} — ${noradStr}`;
            cell.appendChild(nameEl);
            cell.appendChild(noradEl);
            return cell;
        }

        _buildGroupSkeleton(sat) {
            const { grp, grid } = this._buildGroupShell(sat);
            grid.appendChild(this._buildSatCell(sat, 1));
            const loading = document.createElement('div');
            loading.className = 'auto-link__tx auto-link__tx--empty';
            loading.style.gridRow = '1';
            loading.textContent = 'Загрузка…';
            grid.appendChild(loading);
            return grp;
        }

        _applyGroups(groups, opts) {
            this._lastGroups = groups;
            const forceFull = Boolean(opts && opts.forceFull);
            if (this._layout === 'v') {
                this._applyGroupsVertical(groups, forceFull);
            } else {
                this._applyGroupsHorizontal(groups, forceFull);
            }
        }

        /** Удалить из реестра все строки ушедшего КА. */
        _purgeRegistryNorad(norad) {
            this._purgeRegistryNoradExcept(norad, new Set());
        }

        /** Удалить из реестра TX КА, которых нет в keepIds. */
        _purgeRegistryNoradExcept(norad, keepIds) {
            for (const [id, row] of this._rowRegistry) {
                if (row.satNoradId === norad && !keepIds.has(id)) {
                    this._rowRegistry.delete(id);
                }
            }
        }

        /** Синхронизировать метаданные SatNOGS и пролёта без пересоздания DOM. */
        _syncTxRowMeta(row, fresh, sat) {
            row.freqMHz = fresh.freqMHz;
            row.freqHz = fresh.freqHz;
            row.mode = fresh.mode;
            row.description = fresh.description;
            row.satLabel = fresh.satLabel;
            row.satNoradId = fresh.satNoradId;
            row.satAos = sat && sat.aos ? Number(sat.aos) : 0;
            row.satLos = sat && sat.los ? Number(sat.los) : 0;

            const tooltip = row.description
                ? `${row.satLabel} · ${row.freqMHz} МГц · ${row.mode}\n${row.description}`
                : `${row.satLabel} · ${row.freqMHz} МГц · ${row.mode}`;

            if (row._layout === 'h' && row.el) {
                const freqEl = row.el.querySelector('.auto-link__tx-freq');
                const modeEl = row.el.querySelector('.auto-link__tx-mode');
                if (freqEl) { freqEl.textContent = `${row.freqMHz} МГц`; }
                if (modeEl) { modeEl.textContent = row.mode || ''; }
                row.el.title = tooltip;
            } else if (row._layout === 'v' && row.el) {
                const freqEl = row.el.querySelector('.auto-link__v-freq');
                const modeEl = row.el.querySelector('.auto-link__v-mode');
                if (freqEl) { freqEl.textContent = `${row.freqMHz} МГц`; }
                if (modeEl) { modeEl.textContent = row.mode || '—'; }
                row.el.title = tooltip;
            }
            if (row.wfCell) {
                row.wfCell.bandFrac = modulationBandFrac(row.mode);
            }
        }

        /** Взять или создать строку TX (горизонталь), сохраняя wfCell и историю. */
        _acquireHorizontalRow(freshTx, sat, gridRow) {
            let row = this._rowRegistry.get(freshTx.id);
            if (row && row._layout === 'h' && row.el) {
                this._syncTxRowMeta(row, freshTx, sat);
                row.el.style.gridRow = String(gridRow);
                return row;
            }
            row = freshTx;
            this._buildTxRow(row, gridRow);
            row._layout = 'h';
            this._rowRegistry.set(row.id, row);
            return row;
        }

        /** Взять или создать колонку TX (вертикаль), сохраняя wfCell и историю. */
        _acquireVerticalCol(freshTx, sat) {
            let row = this._rowRegistry.get(freshTx.id);
            if (row && row._layout === 'v' && row.el) {
                this._syncTxRowMeta(row, freshTx, sat);
                return row;
            }
            row = freshTx;
            this._buildVerticalCol(row, sat);
            row._layout = 'v';
            this._rowRegistry.set(row.id, row);
            return row;
        }

        /** Обновить ячейку КА и метаданные строк без перестройки DOM. */
        _patchHorizontalGroupLight(grpEl, sat, rows) {
            const grid = grpEl.querySelector('.auto-link__group-grid');
            if (!grid) { return; }
            const satCell = grid.querySelector('.auto-link__sat-cell');
            const satName = sat.sat_name || `NORAD ${sat.norad_id}`;
            const noradStr = String(sat.norad_id);
            const rowCount = Math.max(1, rows.length);
            if (satCell) {
                satCell.style.gridRow = `1 / span ${rowCount}`;
                satCell.title = `${satName} — ${noradStr}`;
                const nameEl = satCell.querySelector('.auto-link__sat-name');
                const noradEl = satCell.querySelector('.auto-link__sat-norad');
                if (nameEl) { nameEl.textContent = satName; }
                if (noradEl) { noradEl.textContent = noradStr; }
            }
            let rowIndex = 1;
            for (const tx of rows) {
                const row = this._acquireHorizontalRow(tx, sat, rowIndex);
                row.el.style.gridRow = String(rowIndex);
                rowIndex += 1;
            }
        }

        /** Обновить ячейку КА и порядок строк (сменился состав TX). */
        _patchHorizontalGroup(grpEl, sat, rows) {
            const grid = grpEl.querySelector('.auto-link__group-grid');
            if (!grid) { return; }
            const satCell = grid.querySelector('.auto-link__sat-cell');
            const satName = sat.sat_name || `NORAD ${sat.norad_id}`;
            const noradStr = String(sat.norad_id);
            const rowCount = Math.max(1, rows.length);
            if (satCell) {
                satCell.style.gridRow = `1 / span ${rowCount}`;
                satCell.title = `${satName} — ${noradStr}`;
                const nameEl = satCell.querySelector('.auto-link__sat-name');
                const noradEl = satCell.querySelector('.auto-link__sat-norad');
                if (nameEl) { nameEl.textContent = satName; }
                if (noradEl) { noradEl.textContent = noradStr; }
            }
            grid.querySelectorAll('.auto-link__tx--empty').forEach((n) => n.remove());
            Array.from(grid.children).forEach((child) => {
                if (!child.classList.contains('auto-link__sat-cell')) {
                    child.remove();
                }
            });
            if (rows.length === 0) {
                const empty = document.createElement('div');
                empty.className = 'auto-link__tx auto-link__tx--empty';
                empty.style.gridRow = '1';
                empty.textContent = 'Активные передатчики не найдены';
                grid.appendChild(empty);
                return;
            }
            let rowIndex = 1;
            for (const tx of rows) {
                const row = this._acquireHorizontalRow(tx, sat, rowIndex);
                grid.appendChild(row.el);
                rowIndex += 1;
            }
        }

        _mountHorizontalGroup(sat, rows) {
            const { grp, grid } = this._buildGroupShell(sat);
            const rowCount = Math.max(1, rows.length);
            grid.appendChild(this._buildSatCell(sat, rowCount));

            if (rows.length === 0) {
                const empty = document.createElement('div');
                empty.className = 'auto-link__tx auto-link__tx--empty';
                empty.style.gridRow = '1';
                empty.textContent = 'Активные передатчики не найдены';
                grid.appendChild(empty);
            } else {
                let rowIndex = 1;
                for (const tx of rows) {
                    const row = this._acquireHorizontalRow(tx, sat, rowIndex);
                    grid.appendChild(row.el);
                    rowIndex += 1;
                }
            }
            grp.dataset.txFp = txListFingerprint(rows);
            return grp;
        }

        _updateVerticalGroupFlex(grp, rowCount) {
            const txCount = Math.max(1, rowCount);
            grp.style.flex = `${txCount} 0 ${txCount * 80}px`;
            grp.style.minWidth = `${txCount * 80}px`;
        }

        _patchVerticalGroupHeader(grpEl, sat, rowCount) {
            this._updateVerticalGroupFlex(grpEl, rowCount);
            const satName = sat.sat_name || `NORAD ${sat.norad_id}`;
            const noradStr = String(sat.norad_id);
            const header = grpEl.querySelector('.auto-link__v-header');
            if (header) {
                header.title = `${satName} — [${noradStr}]`;
                const nameEl = header.querySelector('.auto-link__v-header__name');
                const noradEl = header.querySelector('.auto-link__v-header__norad');
                if (nameEl) { nameEl.textContent = satName; }
                if (noradEl) { noradEl.textContent = `[${noradStr}]`; }
            }
        }

        _patchVerticalGroupLight(grpEl, sat, rows) {
            this._patchVerticalGroupHeader(grpEl, sat, rows.length);
            for (const tx of rows) {
                this._acquireVerticalCol(tx, sat);
            }
        }

        _patchVerticalGroup(grpEl, sat, rows) {
            this._patchVerticalGroupHeader(grpEl, sat, rows.length);
            const cols = grpEl.querySelector('.auto-link__v-cols');
            if (!cols) { return; }
            cols.textContent = '';
            if (rows.length === 0) {
                const empty = document.createElement('div');
                empty.className = 'auto-link__v-empty';
                empty.textContent = 'Нет TX';
                cols.appendChild(empty);
                return;
            }
            for (const tx of rows) {
                const row = this._acquireVerticalCol(tx, sat);
                cols.appendChild(row.el);
            }
        }

        _mountVerticalGroup(sat, rows) {
            const grp = document.createElement('div');
            grp.className = 'auto-link__group';
            grp.dataset.norad = String(sat.norad_id);
            this._updateVerticalGroupFlex(grp, rows.length);

            const satName = sat.sat_name || `NORAD ${sat.norad_id}`;
            const noradStr = String(sat.norad_id);
            const header = document.createElement('div');
            header.className = 'auto-link__v-header';
            const hStack = document.createElement('span');
            hStack.className = 'auto-link__v-header__stack';
            const hName = document.createElement('span');
            hName.className = 'auto-link__v-header__name';
            hName.textContent = satName;
            const hNorad = document.createElement('span');
            hNorad.className = 'auto-link__v-header__norad';
            hNorad.textContent = `[${noradStr}]`;
            hStack.appendChild(hName);
            hStack.appendChild(hNorad);
            header.appendChild(hStack);
            header.title = `${satName} — [${noradStr}]`;
            grp.appendChild(header);

            const cols = document.createElement('div');
            cols.className = 'auto-link__v-cols';
            if (rows.length === 0) {
                const empty = document.createElement('div');
                empty.className = 'auto-link__v-empty';
                empty.textContent = 'Нет TX';
                cols.appendChild(empty);
            } else {
                for (const tx of rows) {
                    const row = this._acquireVerticalCol(tx, sat);
                    cols.appendChild(row.el);
                }
            }
            grp.appendChild(cols);
            grp.dataset.txFp = txListFingerprint(rows);
            return grp;
        }

        /** Удалить группы КА, которых больше нет в составе. */
        _removeObsoleteGroups(newNorads) {
            const el = this._listEl;
            el.querySelectorAll('.auto-link__group').forEach((grp) => {
                const norad = Number(grp.dataset.norad);
                if (!newNorads.has(norad)) {
                    grp.remove();
                    this._purgeRegistryNorad(norad);
                }
            });
            el.querySelector('.auto-link__placeholder')?.remove();
        }

        // ──────── Горизонтальная раскладка (строки = TX) ────────

        _applyGroupsHorizontal(groups, forceFull) {
            const el = this._listEl;
            if (forceFull) {
                el.textContent = '';
                this._rowRegistry.clear();
            }

            const newNorads = new Set(groups.map((g) => g.sat.norad_id));
            this._removeObsoleteGroups(newNorads);

            if (groups.length === 0) {
                if (!el.querySelector('.auto-link__group')) {
                    const ph = document.createElement('p');
                    ph.className = 'auto-link__placeholder';
                    ph.textContent = 'Группа пуста — ждём ближайшие пролёты';
                    el.appendChild(ph);
                }
                this._rows = [];
                return;
            }

            const newRows = [];
            const orderNodes = [];

            for (const { sat, rows } of groups) {
                const norad = sat.norad_id;
                const fp = txListFingerprint(rows);
                let grpEl = el.querySelector(`.auto-link__group[data-norad="${norad}"]`);

                if (grpEl && !forceFull && grpEl.dataset.txFp === fp) {
                    this._patchHorizontalGroupLight(grpEl, sat, rows);
                } else if (grpEl && !forceFull) {
                    this._patchHorizontalGroup(grpEl, sat, rows);
                    grpEl.dataset.txFp = fp;
                } else {
                    if (grpEl) {
                        const keepIds = new Set(rows.map((r) => r.id));
                        this._purgeRegistryNoradExcept(norad, keepIds);
                        grpEl.remove();
                    }
                    grpEl = this._mountHorizontalGroup(sat, rows);
                }
                orderNodes.push(grpEl);

                if (rows.length > 0) {
                    let rowIndex = 1;
                    for (const tx of rows) {
                        newRows.push(this._acquireHorizontalRow(tx, sat, rowIndex));
                        rowIndex += 1;
                    }
                }
            }

            for (const node of orderNodes) {
                el.appendChild(node);
            }

            this._rows = newRows;

            const selectedId = this._sm.getSelectedSatelliteId
                ? this._sm.getSelectedSatelliteId()
                : null;
            this._refreshHighlight(selectedId);
        }

        // ──────── Вертикальная раскладка (столбцы = TX) ────────

        _applyGroupsVertical(groups, forceFull) {
            const el = this._listEl;
            if (forceFull) {
                el.textContent = '';
                this._rowRegistry.clear();
            }

            const newNorads = new Set(groups.map((g) => g.sat.norad_id));
            this._removeObsoleteGroups(newNorads);

            if (groups.length === 0) {
                if (!el.querySelector('.auto-link__group')) {
                    const ph = document.createElement('p');
                    ph.className = 'auto-link__placeholder';
                    ph.textContent = 'Группа пуста — ждём ближайшие пролёты';
                    el.appendChild(ph);
                }
                this._rows = [];
                return;
            }

            const sorted = groups.slice().sort((a, b) => {
                const aHas = a.rows.length > 0 ? 0 : 1;
                const bHas = b.rows.length > 0 ? 0 : 1;
                return aHas - bHas;
            });

            const newRows = [];
            const orderNodes = [];

            for (const { sat, rows } of sorted) {
                const norad = sat.norad_id;
                const fp = txListFingerprint(rows);
                let grpEl = el.querySelector(`.auto-link__group[data-norad="${norad}"]`);

                if (grpEl && !forceFull && grpEl.dataset.txFp === fp) {
                    this._patchVerticalGroupLight(grpEl, sat, rows);
                } else if (grpEl && !forceFull) {
                    this._patchVerticalGroup(grpEl, sat, rows);
                    grpEl.dataset.txFp = fp;
                } else {
                    if (grpEl) {
                        const keepIds = new Set(rows.map((r) => r.id));
                        this._purgeRegistryNoradExcept(norad, keepIds);
                        grpEl.remove();
                    }
                    grpEl = this._mountVerticalGroup(sat, rows);
                }
                orderNodes.push(grpEl);

                for (const tx of rows) {
                    newRows.push(this._acquireVerticalCol(tx, sat));
                }
            }

            for (const node of orderNodes) {
                el.appendChild(node);
            }

            this._rows = newRows;
            this._updateAllColLabels();

            const selectedId = this._sm.getSelectedSatelliteId
                ? this._sm.getSelectedSatelliteId()
                : null;
            this._refreshHighlight(selectedId);
        }

        /** Столбец одного передатчика (вертикальный режим). */
        _buildVerticalCol(tx, sat) {
            const col = document.createElement('div');
            col.className = 'auto-link__v-col';
            col.dataset.rowId = tx.id;
            col.dataset.norad = String(tx.satNoradId);
            col.title = tx.description
                ? `${tx.satLabel} · ${tx.freqMHz} МГц · ${tx.mode}\n${tx.description}`
                : `${tx.satLabel} · ${tx.freqMHz} МГц · ${tx.mode}`;

            const freq = document.createElement('div');
            freq.className = 'auto-link__v-freq';
            freq.textContent = `${tx.freqMHz} МГц`;
            col.appendChild(freq);

            const mode = document.createElement('div');
            mode.className = 'auto-link__v-mode';
            mode.textContent = tx.mode || '—';
            col.appendChild(mode);

            const wf = document.createElement('canvas');
            wf.className = 'auto-link__v-wf';
            col.appendChild(wf);

            const footer = document.createElement('div');
            footer.className = 'auto-link__v-footer';

            // Строка 1: LED статуса Lock + RSSI в dBm.
            const rssiLine = document.createElement('div');
            rssiLine.className = 'auto-link__v-rssi auto-link__v-rssi--silent';
            const rssiLed = document.createElement('span');
            rssiLed.className = 'auto-link__v-rssi__led auto-link__v-rssi__led--lost';
            const rssiVal = document.createElement('span');
            rssiVal.className = 'auto-link__v-rssi__val';
            rssiVal.textContent = '—';
            rssiLine.appendChild(rssiLed);
            rssiLine.appendChild(rssiVal);

            // Строка 2: SNR в дБ.
            const snrLine = document.createElement('div');
            snrLine.className = 'auto-link__v-snr auto-link__v-snr--silent';
            snrLine.textContent = 'SNR —';

            // Строка 3: декодированные / битые пакеты за пролёт.
            const pktLine = document.createElement('div');
            pktLine.className = 'auto-link__v-pkt';
            const pktOk = document.createElement('span');
            pktOk.className = 'auto-link__v-pkt__ok';
            pktOk.textContent = '0';
            const pktSep = document.createElement('span');
            pktSep.className = 'auto-link__v-pkt__sep';
            pktSep.textContent = '/';
            const pktErr = document.createElement('span');
            pktErr.className = 'auto-link__v-pkt__err';
            pktErr.textContent = '0';
            pktLine.appendChild(pktOk);
            pktLine.appendChild(pktSep);
            pktLine.appendChild(pktErr);

            footer.appendChild(rssiLine);
            footer.appendChild(snrLine);
            footer.appendChild(pktLine);
            col.appendChild(footer);

            tx.el = col;
            tx.stripEl = null;
            tx.wfEl = wf;
            tx.wfCell = new WaterfallCell(wf, {
                vertical: true,
                bandFrac: modulationBandFrac(tx.mode),
            });
            tx.labelEl = {
                rssi: rssiLine, rssiLed, rssiVal,
                snr: snrLine,
                pkt: pktLine, pktOk, pktErr,
            };
            // AOS/LOS оставлены для tooltip и совместимости (см. _softGroupUpdate).
            tx.satAos = sat && sat.aos ? Number(sat.aos) : 0;
            tx.satLos = sat && sat.los ? Number(sat.los) : 0;
            tx.history = [];
            tx.totalPackets = 0;
            tx.totalFailed = 0;
            tx.power = 0;
            tx.snrDb = 0;
            tx.lock = 'LOST';

            return col;
        }

        /** Обновить подписи футера одной вертикальной колонки:
         *  RSSI+Lock LED, SNR, декодированные/битые. */
        _updateColLabels(row) {
            const lbl = row.labelEl;
            if (!lbl) { return; }

            // ── RSSI + Lock LED ──
            const dbm = powerToDbm(row.power);
            const lock = row.lock || 'LOST';
            lbl.rssiLed.className = `auto-link__v-rssi__led ${vLockLedClass(lock)}`;
            if (dbm == null) {
                lbl.rssi.className = 'auto-link__v-rssi auto-link__v-rssi--silent';
                lbl.rssiVal.textContent = '—';
            } else {
                lbl.rssi.className = 'auto-link__v-rssi';
                lbl.rssiVal.textContent = `${dbm} dBm`;
            }
            lbl.rssi.title = `Lock: ${lock}`;

            // ── SNR ──
            const lvl = snrLevel(row.snrDb);
            lbl.snr.className = `auto-link__v-snr auto-link__v-snr--${lvl}`;
            lbl.snr.textContent = lvl === 'silent'
                ? 'SNR —'
                : `SNR ${row.snrDb.toFixed(1)} dB`;

            // ── Декодированные / битые ──
            const ok = row.totalPackets || 0;
            const err = row.totalFailed || 0;
            lbl.pktOk.textContent = String(ok);
            lbl.pktErr.textContent = String(err);
            lbl.pktErr.className = err > 0
                ? 'auto-link__v-pkt__err auto-link__v-pkt__err--bad'
                : 'auto-link__v-pkt__err';
        }

        /** Обновить динамические подписи всех строк: для вертикального режима —
         *  El/пакеты/until, для горизонтального — доплер (range_rate из state). */
        _updateAllColLabels() {
            if (this._layout === 'v') {
                for (const row of this._rows) {
                    this._updateColLabels(row);
                }
                return;
            }
            for (const row of this._rows) {
                this._renderRowDoppler(row);
            }
        }

        /** Построить DOM-строку передатчика. Колонки: частота | модуляция |
         *  RSSI | декодированные/битые | SNR | Доплер | Lock | Водопад. */
        _buildTxRow(tx, gridRow) {
            const row = document.createElement('div');
            row.className = 'auto-link__tx';
            row.style.gridRow = String(gridRow);
            row.dataset.rowId = tx.id;
            row.dataset.norad = String(tx.satNoradId);
            row.setAttribute('role', 'listitem');
            const tooltip = tx.description
                ? `${tx.satLabel} · ${tx.freqMHz} МГц · ${tx.mode}\n${tx.description}`
                : `${tx.satLabel} · ${tx.freqMHz} МГц · ${tx.mode}`;
            row.setAttribute('title', tooltip);

            const freq = document.createElement('span');
            freq.className = 'auto-link__tx-freq';
            freq.textContent = `${tx.freqMHz} МГц`;
            row.appendChild(freq);

            const mode = document.createElement('span');
            mode.className = 'auto-link__tx-mode';
            mode.textContent = tx.mode || '';
            row.appendChild(mode);

            // ── Сигнал (RSSI + узкая полоса) ─────────────────────────────
            const rssi = document.createElement('div');
            rssi.className = 'auto-link__tx-rssi auto-link__tx-rssi--silent';
            const rssiBar = document.createElement('span');
            rssiBar.className = 'auto-link__tx-rssi__bar';
            const rssiVal = document.createElement('span');
            rssiVal.className = 'auto-link__tx-rssi__val';
            rssiVal.textContent = '—';
            rssi.appendChild(rssiBar);
            rssi.appendChild(rssiVal);
            row.appendChild(rssi);

            // ── Декодированные / битые ───────────────────────────────────
            const pkt = document.createElement('div');
            pkt.className = 'auto-link__tx-pkt';
            const pktOk = document.createElement('span');
            pktOk.className = 'auto-link__tx-pkt__ok';
            pktOk.textContent = '0';
            const pktSep = document.createElement('span');
            pktSep.className = 'auto-link__tx-pkt__sep';
            pktSep.textContent = '/';
            const pktErr = document.createElement('span');
            pktErr.className = 'auto-link__tx-pkt__err';
            pktErr.textContent = '0';
            pkt.appendChild(pktOk);
            pkt.appendChild(pktSep);
            pkt.appendChild(pktErr);
            row.appendChild(pkt);

            // ── SNR ──────────────────────────────────────────────────────
            const snr = document.createElement('div');
            snr.className = 'auto-link__tx-snr auto-link__tx-snr--silent';
            snr.textContent = '—';
            row.appendChild(snr);

            // ── Доплер ───────────────────────────────────────────────────
            const doppler = document.createElement('div');
            doppler.className = 'auto-link__tx-doppler auto-link__tx-doppler--silent';
            doppler.textContent = '—';
            row.appendChild(doppler);

            // ── Lock LED ─────────────────────────────────────────────────
            const lock = document.createElement('div');
            lock.className = 'auto-link__tx-lock';
            const lockLed = document.createElement('span');
            lockLed.className = 'auto-link__tx-lock__led auto-link__tx-lock__led--lost';
            lock.appendChild(lockLed);
            row.appendChild(lock);

            const wf = document.createElement('canvas');
            wf.className = 'auto-link__wf';
            row.appendChild(wf);

            tx.el = row;
            tx.stripEl = null;
            tx.wfEl = wf;
            tx.wfCell = new WaterfallCell(wf, {
                vertical: false,
                bandFrac: modulationBandFrac(tx.mode),
            });
            tx.cells = {
                rssi, rssiBar, rssiVal,
                pkt, pktOk, pktErr,
                snr, doppler,
                lock, lockLed,
            };
            tx.history = [];
            tx.totalPackets = 0;
            tx.totalFailed = 0;
            tx.snrDb = 0;
            tx.lock = 'LOST';
            tx.packetsFailed = 0;

            return row;
        }

        // ----- SatNOGS API -----

        _fetchTxForSat(sat) {
            const norad = Number(sat.norad_id) || 0;
            if (!norad) { return Promise.resolve([]); }
            if (this._txCache.has(norad)) {
                return this._txCache.get(norad);
            }
            const p = fetch(`/api/satnogs/transmitters/${norad}`, {
                headers: { 'Accept': 'application/json' },
                credentials: 'same-origin',
            }).then((resp) => {
                if (!resp.ok) {
                    if (resp.status !== 404) {
                        console.warn(`[OverviewLink] SatNOGS ${norad}: HTTP ${resp.status}`);
                    }
                    return [];
                }
                return resp.json();
            }).then((data) => {
                if (!data || !Array.isArray(data.transmitters)) { return []; }
                const out = [];
                for (const t of data.transmitters) {
                    const row = txFromSatnogs(sat, t);
                    if (row) { out.push(row); }
                }
                out.sort((a, b) => a.freqHz - b.freqHz);
                return out;
            }).catch((err) => {
                console.warn(`[OverviewLink] SatNOGS ${norad} fetch failed:`, err);
                this._txCache.delete(norad);
                return [];
            });
            this._txCache.set(norad, p);
            return p;
        }

        // ----- Поток tx_cycle -----

        _onTxCycle(ev) {
            const payload = ev && ev.detail ? ev.detail : null;
            if (!payload) { return; }
            const updates = indexTxCycleUpdates(payload);
            if (updates.size === 0) { return; }
            for (const row of this._rows) {
                const uuid = row.uuid || '';
                if (!uuid) { continue; }
                const upd = updates.get(uuid);
                if (!upd) { continue; }
                applyTxCycleUpdate(row, upd);
                if (row.wfCell) {
                    row.wfCell.setPower(row.power);
                }
                if (this._layout === 'v') {
                    this._updateColLabels(row);
                } else {
                    this._renderHorizontalMetrics(row);
                }
            }
        }

        /** Обновить метрики одной горизонтальной строки TX из tx_cycle / state. */
        _renderHorizontalMetrics(row) {
            if (!row || !row.cells) { return; }
            const c = row.cells;
            const power = row.power || 0;
            const lock = row.lock || 'LOST';
            const lockActive = lock === 'OK' || lock === 'SEARCH';
            const dbm = powerToDbm(power);

            if (!lockActive || dbm == null) {
                c.rssi.className = 'auto-link__tx-rssi auto-link__tx-rssi--silent';
                c.rssiVal.textContent = '—';
                c.rssiBar.style.setProperty('--auto-link-rssi-fill', '0%');
            } else {
                c.rssi.className = 'auto-link__tx-rssi';
                c.rssiVal.textContent = `${dbm} dBm`;
                const pct = Math.round(Math.max(0, Math.min(1, power)) * 100);
                c.rssiBar.style.setProperty('--auto-link-rssi-fill', `${pct}%`);
                c.rssiBar.style.setProperty('--auto-link-rssi-color', rssiBarColor(power));
            }

            const snrLvl = snrLevel(row.snrDb);
            c.snr.className = `auto-link__tx-snr auto-link__tx-snr--${snrLvl}`;
            c.snr.textContent = snrLvl === 'silent' ? '—' : `${row.snrDb.toFixed(1)} dB`;

            c.pktOk.textContent = String(row.totalPackets || 0);
            c.pktErr.textContent = String(row.totalFailed || 0);
            c.pktErr.className = (row.totalFailed || 0) > 0
                ? 'auto-link__tx-pkt__err auto-link__tx-pkt__err--bad'
                : 'auto-link__tx-pkt__err';

            c.lockLed.className = `auto-link__tx-lock__led ${lockLedClass(lock)}`;
            c.lock.title = `Lock: ${lock}`;

            this._renderRowDoppler(row);
        }

        /** Обновить ячейку доплера в одной строке (секундный таймер). */
        _renderRowDoppler(row) {
            if (!row || !row.cells || !row.cells.doppler) { return; }
            const cell = row.cells.doppler;
            const st = this._sm.getState && this._sm.getState(row.satNoradId);
            const pos = st && st.position;
            const rangeRate = pos && typeof pos.range_rate === 'number' ? pos.range_rate : null;
            // Доплер показываем только когда есть и сигнал, и range_rate.
            const lockActive = row.lock === 'OK' || row.lock === 'SEARCH';
            if (!lockActive || rangeRate == null) {
                cell.className = 'auto-link__tx-doppler auto-link__tx-doppler--silent';
                cell.textContent = '—';
                return;
            }
            const hz = dopplerHz(row.freqHz, rangeRate);
            cell.textContent = formatDopplerKhz(hz);
            if (hz > 5) {
                cell.className = 'auto-link__tx-doppler auto-link__tx-doppler--pos';
            } else if (hz < -5) {
                cell.className = 'auto-link__tx-doppler auto-link__tx-doppler--neg';
            } else {
                cell.className = 'auto-link__tx-doppler';
            }
        }

        // ----- Взаимная подсветка План ↔ TX -----

        _refreshHighlight(selectedNorad) {
            const sel = (typeof selectedNorad === 'number' && selectedNorad > 0)
                ? selectedNorad
                : (this._sm.getSelectedSatelliteId ? this._sm.getSelectedSatelliteId() : null);
            const hNorad = this._linkHoverNorad;
            const hTx = this._linkHoverTxRowId;
            const groups = this._listEl.querySelectorAll('.auto-link__group');

            for (const grp of groups) {
                const norad = Number(grp.dataset.norad) || 0;
                const grpHi = resolveLinkHighlight(hNorad, hTx, sel, norad, '').group;
                grp.classList.toggle('auto-link__group--highlighted', grpHi);

                const txs = grp.querySelectorAll('.auto-link__tx');
                for (const tx of txs) {
                    const rowId = tx.dataset.rowId || '';
                    const txHi = resolveLinkHighlight(hNorad, hTx, sel, norad, rowId).tx;
                    tx.classList.toggle('auto-link__tx--highlighted', txHi);
                }

                const vcols = grp.querySelectorAll('.auto-link__v-col');
                for (const vc of vcols) {
                    const rowId = vc.dataset.rowId || '';
                    const colHi = resolveLinkHighlight(hNorad, hTx, sel, norad, rowId).tx;
                    vc.classList.toggle('auto-link__v-col--highlighted', colHi);
                }
            }
        }

        _clearLinkHover() {
            this._linkHoverNorad = null;
            this._linkHoverTxRowId = null;
        }

        _emitLinkHover(noradId, txRowId) {
            dispatchLinkHover({
                noradId: noradId || null,
                txRowId: txRowId || null,
                source: 'auto-link',
            });
        }

        _onLinkHover(ev) {
            const d = ev && ev.detail ? ev.detail : null;
            if (!d || d.source === 'auto-link') { return; }
            this._linkHoverNorad = (typeof d.noradId === 'number' && d.noradId > 0) ? d.noradId : null;
            this._linkHoverTxRowId = d.txRowId || null;
            this._refreshHighlight();
        }

        /** Делегирование hover по строкам TX / v-col. */
        _onListPointer(e) {
            const type = e.type;
            const txEl = e.target && e.target.closest
                ? e.target.closest('.auto-link__tx, .auto-link__v-col')
                : null;
            if (type === 'mouseover') {
                if (!txEl || txEl.classList.contains('auto-link__tx--empty')) { return; }
                const norad = Number(txEl.dataset.norad) || 0;
                const rowId = txEl.dataset.rowId || null;
                if (!norad) { return; }
                this._linkHoverNorad = norad;
                this._linkHoverTxRowId = rowId;
                this._refreshHighlight();
                this._emitLinkHover(norad, rowId);
                return;
            }
            if (type === 'mouseout') {
                if (!txEl) { return; }
                const related = e.relatedTarget;
                if (related && txEl.contains(related)) { return; }
                const stillInside = related && this._listEl.contains(related)
                    && related.closest('.auto-link__tx, .auto-link__v-col');
                if (stillInside) { return; }
                this._clearLinkHover();
                this._refreshHighlight();
                this._emitLinkHover(null, null);
            }
        }

        _onSelectedChange(state) {
            const id = state && typeof state.noradId === 'number' ? state.noradId : null;
            this._refreshHighlight(id);
        }

        _onClick(e) {
            const target = e.target;
            if (!target || !target.closest) { return; }
            const groupEl = target.closest('.auto-link__group');
            if (!groupEl) { return; }
            const norad = Number(groupEl.dataset.norad) || 0;
            if (!norad) { return; }

            const group = this._sm.getSatelliteGroup
                ? this._sm.getSatelliteGroup()
                : null;
            const sats = group && Array.isArray(group.satellites) ? group.satellites : [];
            const info = sats.find((s) => s && s.norad_id === norad);
            const name = info ? (info.sat_name || '') : '';

            // manual=true — фиксируем выбор как ручной (как клик в правой таблице).
            if (typeof this._sm.setSelectedSatellite === 'function') {
                this._sm.setSelectedSatellite(norad, name, true);
            }
        }
    }

    // Экспорт служебных функций — пригодятся внешним модулям, если понадобится.
    window.AutoLink = {
        OverviewLink,
        renderStrip,
        powerLevelClass,
        cellLabel,
        STRIP_CAPACITY,
        groupFingerprint,
        txListFingerprint,
        txFromSatnogs,
        getLayoutMode,
        setLayoutMode,
        dopplerHz,
        formatDopplerKhz,
        snrLevel,
        lockLedClass,
        rssiBarColor,
        powerToDbm,
        resolveLinkHighlight,
        indexTxCycleUpdates,
        applyTxCycleUpdate,
        LINK_HOVER_EVENT,
        TX_CYCLE_EVENT,
    };
    window.OverviewLink = OverviewLink;

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = window.AutoLink; // eslint-disable-line no-undef
    }
})();
