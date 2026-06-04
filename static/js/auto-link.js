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
     * Сколько слотов в ленте визитов (в DOM всегда столько пустых квадратов).
     * Новый визит вставляется слева и сдвигает остальные вправо; самый старый
     * выпадает справа, когда слоты заняты.
     */
    const STRIP_CAPACITY = 60;

    /** Буфер визитов: индекс 0 — последний (новейший) визит слева. */
    class CycleRing {
        constructor(capacity) {
            this.capacity = Math.max(8, capacity | 0);
            this._items = [];
        }
        /** Вставка слева: предыдущие визиты сдвигаются вправо. */
        push(value) {
            this._items.unshift(value);
            if (this._items.length > this.capacity) {
                this._items.pop();
            }
        }
        clear() { this._items.length = 0; }
        get size() { return this._items.length; }
        toArray() { return this._items.slice(); }
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
     */
    function renderStrip(stripEl, ring, capacity) {
        ensureStripPool(stripEl, capacity);
        const items = ring.toArray();
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

            /** Текущие строки передатчиков. */
            this._rows = [];
            /** Кеш ответов SatNOGS: norad → Promise<rows>. */
            this._txCache = new Map();
            /** Версия группы — защита от гонки async-запросов. */
            this._groupVersion = 0;

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
        }

        /** Освободить подписки. */
        destroy() {
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
        }

        // ----- Обновление группы -----

        _onGroupUpdate(data) {
            if (!data || !Array.isArray(data.satellites)) { return; }
            this._rebuildFromGroup(data.satellites);
        }

        _rebuildFromGroup(satellites) {
            this._groupVersion++;
            const myVersion = this._groupVersion;

            this._renderSkeleton(satellites);

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

        _applyGroups(groups) {
            this._rows = [];
            const el = this._listEl;
            el.textContent = '';

            if (groups.length === 0) {
                const ph = document.createElement('p');
                ph.className = 'auto-link__placeholder';
                ph.textContent = 'Группа пуста — ждём ближайшие пролёты';
                el.appendChild(ph);
                return;
            }

            const frag = document.createDocumentFragment();
            for (const { sat, rows } of groups) {
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
                        const rowEl = this._buildTxRow(tx, rowIndex);
                        grid.appendChild(rowEl);
                        this._rows.push(tx);
                        rowIndex += 1;
                    }
                }
                frag.appendChild(grp);
            }
            el.appendChild(frag);

            // Восстановить подсветку выбранного КА после перерисовки.
            const selectedId = this._sm.getSelectedSatelliteId
                ? this._sm.getSelectedSatelliteId()
                : null;
            this._applyHighlight(selectedId);
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

            const total = document.createElement('span');
            total.className = 'auto-link__tx-total';
            total.textContent = '0';
            row.appendChild(total);

            const db = document.createElement('span');
            db.className = 'auto-link__tx-db';
            db.textContent = '— dB';
            row.appendChild(db);

            // Привязываем DOM и буферы к самому объекту — пригодятся в _onTxCycle.
            tx.el = row;
            tx.stripEl = strip;
            tx.totalEl = total;
            tx.dbEl = db;
            tx.history = new CycleRing(STRIP_CAPACITY);
            tx.totalPackets = 0;

            // Префилл silent-квадратиками — чтобы лента сразу была видна.
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
         * Принять очередной цикл сканирования: обновить буферы каждой строки,
         * перерисовать ленту визитов, обновить кумулятив и dB.
         */
        _onTxCycle(data) {
            if (!this._rows.length) { return; }

            const cellsByRowId = new Map();
            const sats = data && Array.isArray(data.satellites) ? data.satellites : [];
            for (const sat of sats) {
                const norad = Number(sat && sat.norad_id) || 0;
                if (!norad) { continue; }
                const txs = Array.isArray(sat.transmitters) ? sat.transmitters : [];
                for (const tx of txs) {
                    if (!tx || !tx.uuid) { continue; }
                    const rowId = `tx-${norad}-${tx.uuid}`;
                    cellsByRowId.set(rowId, {
                        packets: Math.max(0, Number(tx.packets) || 0),
                        power: Math.max(0, Math.min(1, Number(tx.power) || 0)),
                    });
                }
            }

            for (const row of this._rows) {
                if (!row.history) { continue; }
                const cell = cellsByRowId.get(row.id) || { packets: 0, power: 0 };
                row.history.push(cell);
                row.totalPackets += cell.packets;
                this._renderRowCycle(row, cell);
            }
        }

        _renderRowCycle(row, cell) {
            renderStrip(row.stripEl, row.history, STRIP_CAPACITY);

            // Кумулятивный счётчик пакетов за пролёт.
            row.totalEl.textContent = String(row.totalPackets);
            row.totalEl.classList.toggle(
                'auto-link__tx-total--active',
                row.totalPackets > 0,
            );

            // Последний уровень сигнала, dB. Если в текущем цикле молчит —
            // оставляем «— dB», чтобы не «застревало» старое значение.
            const dbm = powerToDbm(cell.power);
            if (dbm !== null) {
                row.dbEl.textContent = `${dbm} dB`;
                row.dbEl.classList.add('auto-link__tx-db--active');
            } else {
                row.dbEl.textContent = '— dB';
                row.dbEl.classList.remove('auto-link__tx-db--active');
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
        CycleRing,
    };
    window.OverviewLink = OverviewLink;
})();
