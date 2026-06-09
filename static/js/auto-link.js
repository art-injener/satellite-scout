/**
 * Auto-link — связка нижней панели Авто-режима.
 *
 * Единый список передатчиков всех КА группы (UI-заголовок «Передатчики»):
 * на каждой строке — частота, модуляция, лента «визитов сканера» (квадратики
 * с цифрой пакетов и цветом по уровню сигнала), Σ за пролёт и последний dB.
 * Название панели — столбиком в .bottom-panel__side («Передатчики»).
 *
 * Источники данных:
 *  - Список передатчиков — REST `GET /api/satnogs/transmitters/{norad}`.
 *  - Поток циклов — SSE-событие `tx_cycle` через StateEventType.TX_CYCLE
 *    (рассылается TxCycleMock, см. ADR-004 §3).
 *
 * Один tx_cycle = один «визит сканера» на этом TX. Без оси «секунды»: при
 * переменном dwell time секунды лгут, поэтому ось X — это эпизоды визитов.
 */

(function () {
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

    /** Короткий обратный отсчёт M:SS (для подписи LOS). */
    function fmtCountdownShort(deltaMs) {
        if (deltaMs <= 0) { return '0:00'; }
        const sec = Math.floor(deltaMs / 1000);
        const m = Math.floor(sec / 60);
        const s = sec % 60;
        return `${m}:${s < 10 ? '0' : ''}${s}`;
    }

    /**
     * Подпись до конца сеанса: «-2:14 LOS» или «-5:30 AOS».
     * @param {number} aos — Unix ms
     * @param {number} los — Unix ms
     * @param {number} [nowMs]
     * @returns {{ text: string, kind: string }}
     */
    function fmtPassUntilLabel(aos, los, nowMs) {
        const now = nowMs != null ? nowMs : Date.now();
        if (!aos || !los || los <= aos) {
            return { text: '—', kind: 'none' };
        }
        if (now < aos) {
            return { text: `-${fmtCountdownShort(aos - now)} AOS`, kind: 'aos' };
        }
        if (now <= los) {
            return { text: `-${fmtCountdownShort(los - now)} LOS`, kind: 'los' };
        }
        return { text: '—', kind: 'none' };
    }

    /**
     * Подпись угла места: «El 45° ↑».
     * @param {number|null} el — градусы
     * @param {number|null} prevEl — предыдущее значение для стрелки тренда
     */
    function fmtElevationLabel(el, prevEl) {
        if (el == null || Number.isNaN(el)) {
            return { text: 'El —', level: 'none', trend: '' };
        }
        const v = Math.round(el);
        let trend = '';
        if (prevEl != null && !Number.isNaN(prevEl)) {
            const d = el - prevEl;
            if (d > 0.15) { trend = ' ↑'; }
            else if (d < -0.15) { trend = ' ↓'; }
        }
        const level = v >= 25 ? 'high' : (v >= 10 ? 'mid' : 'low');
        return { text: `El ${v}°${trend}`, level, trend };
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
            this.vertical = !!(opts && opts.vertical);
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
            const hot = window.hotColor;

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

            this._onGroupUpdateBound = (data) => this._onGroupUpdate(data);
            this._sm.subscribe(window.StateEventType.SATELLITE_GROUP_UPDATE, this._onGroupUpdateBound);

            this._onTxCycleBound = (data) => this._onTxCycle(data);
            this._sm.subscribe(window.StateEventType.TX_CYCLE, this._onTxCycleBound);

            this._onSelectedChangeBound = (state) => this._onSelectedChange(state);
            this._sm.subscribe(window.StateEventType.SELECTED_CHANGE, this._onSelectedChangeBound);

            this._onClickBound = (e) => this._onClick(e);
            this._listEl.addEventListener('click', this._onClickBound);

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
                if (this._onTxCycleBound) {
                    sm.unsubscribe(window.StateEventType.TX_CYCLE, this._onTxCycleBound);
                }
                if (this._onSelectedChangeBound) {
                    sm.unsubscribe(window.StateEventType.SELECTED_CHANGE, this._onSelectedChangeBound);
                }
            }
            this._onGroupUpdateBound = null;
            this._onTxCycleBound = null;
            this._onSelectedChangeBound = null;
            if (this._onClickBound) {
                this._listEl.removeEventListener('click', this._onClickBound);
                this._onClickBound = null;
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
            const forceFull = !!(opts && opts.forceFull);
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
            this._applyHighlight(selectedId);
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
            this._applyHighlight(selectedId);
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

            const elLine = document.createElement('div');
            elLine.className = 'auto-link__v-el';
            elLine.textContent = 'El —';

            const pktLine = document.createElement('div');
            pktLine.className = 'auto-link__v-pkt';
            pktLine.textContent = '—';

            const untilLine = document.createElement('div');
            untilLine.className = 'auto-link__v-until';
            untilLine.textContent = '—';

            footer.appendChild(elLine);
            footer.appendChild(pktLine);
            footer.appendChild(untilLine);
            col.appendChild(footer);

            tx.el = col;
            tx.stripEl = null;
            tx.wfEl = wf;
            tx.wfCell = new WaterfallCell(wf, {
                vertical: true,
                bandFrac: modulationBandFrac(tx.mode),
            });
            tx.labelEl = { el: elLine, pkt: pktLine, until: untilLine };
            tx.satAos = sat && sat.aos ? Number(sat.aos) : 0;
            tx.satLos = sat && sat.los ? Number(sat.los) : 0;
            tx._prevEl = null;
            tx.totalEl = null;
            tx.dbEl = null;
            tx.history = [];
            tx.totalPackets = 0;

            return col;
        }

        /** Обновить подписи футера одной вертикальной колонки. */
        _updateColLabels(row) {
            if (!row.labelEl) { return; }

            const pkt = row.totalPackets > 0 ? `${row.totalPackets} пак.` : '—';
            if (row.labelEl.pkt.textContent !== pkt) {
                row.labelEl.pkt.textContent = pkt;
            }
            row.labelEl.pkt.classList.toggle('auto-link__v-pkt--active', row.totalPackets > 0);

            const st = this._sm.getState && this._sm.getState(row.satNoradId);
            const elVal = st && st.position ? st.position.el : null;
            const elFmt = fmtElevationLabel(elVal, row._prevEl);
            if (elVal != null && !Number.isNaN(elVal)) {
                row._prevEl = elVal;
            }
            if (row.labelEl.el.textContent !== elFmt.text) {
                row.labelEl.el.textContent = elFmt.text;
            }
            row.labelEl.el.className = 'auto-link__v-el';
            if (elFmt.level === 'high') {
                row.labelEl.el.classList.add('auto-link__v-el--high');
            } else if (elFmt.level === 'mid') {
                row.labelEl.el.classList.add('auto-link__v-el--mid');
            } else if (elFmt.level === 'low') {
                row.labelEl.el.classList.add('auto-link__v-el--low');
            }

            const until = fmtPassUntilLabel(row.satAos, row.satLos);
            if (row.labelEl.until.textContent !== until.text) {
                row.labelEl.until.textContent = until.text;
            }
            row.labelEl.until.className = 'auto-link__v-until';
            if (until.kind === 'los') {
                row.labelEl.until.classList.add('auto-link__v-until--los');
                const left = row.satLos - Date.now();
                if (left > 0 && left < 120000) {
                    row.labelEl.until.classList.add('auto-link__v-until--urgent');
                }
            } else if (until.kind === 'aos') {
                row.labelEl.until.classList.add('auto-link__v-until--aos');
            }
        }

        /** Обновить подписи всех вертикальных колонок. */
        _updateAllColLabels() {
            if (this._layout !== 'v') { return; }
            for (const row of this._rows) {
                this._updateColLabels(row);
            }
        }

        /** Построить DOM-строку передатчика и привязать буфер истории. */
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

            const strip = document.createElement('div');
            strip.className = 'auto-link__strip';
            row.appendChild(strip);

            const wf = document.createElement('canvas');
            wf.className = 'auto-link__wf';
            row.appendChild(wf);

            tx.el = row;
            tx.stripEl = strip;
            tx.wfEl = wf;
            tx.wfCell = new WaterfallCell(wf, {
                vertical: false,
                bandFrac: modulationBandFrac(tx.mode),
            });
            tx.totalEl = null;
            tx.dbEl = null;
            tx.history = [];
            tx.totalPackets = 0;

            renderStrip(strip, tx.history, STRIP_CAPACITY);

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

        /**
         * Принять очередной цикл сканирования: взять history[] и total_packets
         * из бэкендового события, перерисовать ленту и обновить кумулятив/dB.
         */
        _onTxCycle(data) {
            if (!this._rows.length) { return; }

            const txDataByRowId = new Map();
            const sats = data && Array.isArray(data.satellites) ? data.satellites : [];
            for (const sat of sats) {
                const norad = Number(sat && sat.norad_id) || 0;
                if (!norad) { continue; }
                const txs = Array.isArray(sat.transmitters) ? sat.transmitters : [];
                for (const tx of txs) {
                    if (!tx || !tx.uuid) { continue; }
                    const rowId = `tx-${norad}-${tx.uuid}`;
                    txDataByRowId.set(rowId, {
                        packets: Math.max(0, Number(tx.packets) || 0),
                        power: Math.max(0, Math.min(1, Number(tx.power) || 0)),
                        totalPackets: Math.max(0, Number(tx.total_packets) || 0),
                        history: Array.isArray(tx.history) ? tx.history : [],
                    });
                }
            }

            for (const row of this._rows) {
                const txd = txDataByRowId.get(row.id) || {
                    packets: 0, power: 0, totalPackets: 0, history: [],
                };
                row.history = txd.history;
                row.totalPackets = txd.totalPackets;
                this._renderRowCycle(row, txd);
            }
            this._updateAllColLabels();
        }

        _renderRowCycle(row, cell) {
            // Яркость полосы водопада задаётся последним замером мощности;
            // саму текстуру непрерывно рисует анимационный цикл (_wfFrame).
            if (row.wfCell) {
                row.wfCell.setPower(cell ? cell.power : 0);
            }
            // Детектор пакетов остаётся только в горизонтальной раскладке.
            if (this._layout !== 'v' && row.stripEl) {
                renderStrip(row.stripEl, row.history, STRIP_CAPACITY);
            }
        }

        // ----- Взаимная подсветка -----

        _applyHighlight(noradId) {
            const id = (typeof noradId === 'number' && noradId > 0) ? noradId : null;
            const groups = this._listEl.querySelectorAll('.auto-link__group');
            for (const grp of groups) {
                const matches = id !== null && Number(grp.dataset.norad) === id;
                grp.classList.toggle('auto-link__group--highlighted', matches);
                const txs = grp.querySelectorAll('.auto-link__tx');
                for (const tx of txs) {
                    tx.classList.toggle('auto-link__tx--highlighted', matches);
                }
                const vcols = grp.querySelectorAll('.auto-link__v-col');
                for (const vc of vcols) {
                    vc.classList.toggle('auto-link__v-col--highlighted', matches);
                }
            }
        }

        _onSelectedChange(state) {
            const id = state && typeof state.noradId === 'number' ? state.noradId : null;
            this._applyHighlight(id);
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
    };
    window.OverviewLink = OverviewLink;

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = window.AutoLink; // eslint-disable-line no-undef
    }
})();
