// tx-markers.js — риски ожидаемых передатчиков поверх «Спектра».
//
// Сервер присылает в demod_status передатчики КА группы, попавшие в окно
// приёмника, с частотой уже на Доплере (tx_markers). Здесь они только
// рисуются: пунктирная риска на частоте и подпись «КА · модуляция».
// Щелчок по подписи отдаёт маркер наружу — канал ставит manual-layout.

'use strict';

(function() {
    /** Ширина подписи для раскладки по рядам, px. */
    const LABEL_GAP_PX = 120;
    const MAX_ROWS = 3;
    /** Подписи начинаются ниже подписи канала. */
    const LABEL_TOP_PX = 18;
    const ROW_PX = 15;

    /**
     * Координата частоты на графике, px хоста; null — вне видимой части.
     * @param {number} freqHz
     * @param {?{centerHz: number, spanHz: number}} band — окно приёмника
     * @param {?{offsetHz: number, spanHz: number}} view — видимая часть при масштабе; null — всё окно
     * @param {?{left: number, width: number}} plot
     * @returns {?number}
     */
    function markerX(freqHz, band, view, plot) {
        if (!band || !(band.spanHz > 0) || !plot || !(plot.width > 0)) { return null; }
        const win = view && view.spanHz > 0 ? view : { offsetHz: 0, spanHz: band.spanHz };
        const x = plot.left + plot.width / 2 + (freqHz - band.centerHz - win.offsetHz) * plot.width / win.spanHz;
        if (x < plot.left || x > plot.left + plot.width) { return null; }
        return x;
    }

    /**
     * Ряд подписи для каждой риски: соседние ближе gapPx уходят ниже.
     * @param {number[]} xs — координаты по возрастанию
     * @param {number} gapPx
     * @param {number} maxRows — лишние подписи ложатся в последний ряд
     * @returns {number[]}
     */
    function labelRows(xs, gapPx, maxRows) {
        const last = [];
        const rows = [];
        for (let i = 0; i < xs.length; i++) {
            let row = maxRows - 1;
            for (let r = 0; r < maxRows; r++) {
                if (last[r] === undefined || xs[i] - last[r] >= gapPx) { row = r; break; }
            }
            last[row] = xs[i];
            rows.push(row);
        }
        return rows;
    }

    /**
     * Имена КА по NORAD: сначала из списка группы, затем из ленты пролётов.
     * @param {?Object} group — satellite_group_update
     * @returns {Object<number, string>}
     */
    function satNames(group) {
        const names = {};
        const addNames = function(list) {
            if (!Array.isArray(list)) { return; }
            for (let i = 0; i < list.length; i++) {
                const s = list[i];
                if (!names[s.norad_id]) {
                    names[s.norad_id] = s.sat_name || s.sat_alias;
                }
            }
        };
        if (group) {
            addNames(group.satellites);
            addNames(group.timeline);
        }
        return names;
    }

    /**
     * Маркеры сервера с подписями: КА — из группы, модуляция — из ленты передатчиков по uuid.
     * @param {?Array} serverMarkers — tx_markers из demod_status
     * @param {Array} ribbon — передатчики ленты (transmittersFromGroup)
     * @param {?Object} group — satellite_group_update
     * @returns {Array<{noradId: number, uuid: string, freqHz: number, nominalHz: number, label: string, tx: ?Object}>}
     */
    function markersWithLabels(serverMarkers, ribbon, group) {
        const out = [];
        if (!Array.isArray(serverMarkers)) { return out; }
        const names = satNames(group);
        const byUuid = {};
        for (let i = 0; i < (ribbon || []).length; i++) {
            if (ribbon[i].uuid) { byUuid[ribbon[i].uuid] = ribbon[i]; }
        }
        for (let i = 0; i < serverMarkers.length; i++) {
            const m = serverMarkers[i];
            if (!m || !(m.freq_hz > 0) || !m.norad_id) { continue; }
            const tx = byUuid[m.uuid] || null;
            const name = names[m.norad_id] || ('NORAD ' + m.norad_id);
            out.push({
                noradId: m.norad_id,
                uuid: m.uuid,
                freqHz: m.freq_hz,
                nominalHz: m.freq_hz - (m.doppler_hz || 0),
                label: tx && tx.modulation ? name + ' · ' + tx.modulation : name,
                tx: tx,
            });
        }
        return out;
    }

    /**
     * @param {HTMLElement} host — общий контейнер спектра (position: relative)
     * @param {Object} opts
     * @param {HTMLCanvasElement} opts.plotCanvas — график БПФ: по нему область и высота рисок
     * @param {number} opts.marginLeft
     * @param {number} opts.marginRight
     * @param {function(Object)} opts.onPick — щелчок по подписи маркера
     */
    function TxMarkers(host, opts) {
        this._host = host;
        this._opts = opts || {};
        this._band = null;
        this._view = null;
        this._markers = [];
        if (!host || typeof document === 'undefined') { return; }
        this._root = document.createElement('div');
        this._root.className = 'ml-txmark';
        host.appendChild(this._root);
    }

    /** Окно приёмника из кадра потока. */
    TxMarkers.prototype.setBand = function(centerHz, spanHz) {
        if (!(spanHz > 0)) { return; }
        if (this._band && this._band.centerHz === centerHz && this._band.spanHz === spanHz) { return; }
        this._band = { centerHz: centerHz, spanHz: spanHz };
        this.render();
    };

    /** Видимая часть окна при масштабе: центр от центра приёмника и ширина; без ширины — всё окно. */
    TxMarkers.prototype.setView = function(offsetHz, spanHz) {
        this._view = spanHz > 0 ? { offsetHz: Number(offsetHz) || 0, spanHz: spanHz } : null;
        this.render();
    };

    /** @param {Array} list — результат markersWithLabels */
    TxMarkers.prototype.setMarkers = function(list) {
        this._markers = Array.isArray(list) ? list : [];
        this.render();
    };

    TxMarkers.prototype._plot = function() {
        const c = this._opts.plotCanvas;
        if (!c || !this._host || typeof c.getBoundingClientRect !== 'function') { return null; }
        const cr = c.getBoundingClientRect();
        const hr = this._host.getBoundingClientRect();
        const ml = this._opts.marginLeft || 0;
        const mr = this._opts.marginRight || 0;
        return { left: cr.left - hr.left + ml, width: cr.width - ml - mr, top: cr.top - hr.top, height: cr.height };
    };

    TxMarkers.prototype.render = function() {
        if (!this._root) { return; }
        const plot = this._plot();
        const placed = [];
        for (let i = 0; i < this._markers.length; i++) {
            const x = markerX(this._markers[i].freqHz, this._band, this._view, plot);
            if (x !== null) { placed.push({ m: this._markers[i], x: Math.round(x) }); }
        }
        placed.sort(function(a, b) { return a.x - b.x; });
        // Статус приходит раз в секунду: те же пиксели не пересобираются, щелчок не теряется
        const colorOf = typeof this._opts.colorOf === 'function' ? this._opts.colorOf : null;
        const colorFor = function(m) {
            return m.color || (colorOf ? colorOf(m.noradId) : null) || '';
        };
        const sig = (plot ? plot.top + ':' + plot.height : '') + '|' +
            placed.map(function(p) {
                return p.m.uuid + '@' + p.x + ':' + p.m.label + ':' + colorFor(p.m);
            }).join('|');
        if (sig === this._sig) { return; }
        this._sig = sig;
        this._root.textContent = '';
        const rows = labelRows(placed.map(function(p) { return p.x; }), LABEL_GAP_PX, MAX_ROWS);
        const self = this;
        for (let i = 0; i < placed.length; i++) {
            const p = placed[i];
            const color = colorFor(p.m);
            const line = document.createElement('div');
            line.className = 'ml-txmark__line';
            line.style.left = p.x + 'px';
            line.style.top = plot.top + 'px';
            line.style.height = plot.height + 'px';
            if (color) { line.style.setProperty('--ml-txmark-color', color); }
            this._root.appendChild(line);

            const label = document.createElement('button');
            label.type = 'button';
            label.className = 'ml-txmark__label';
            label.textContent = p.m.label;
            label.title = 'Поставить канал: ' + (p.m.nominalHz / 1e6).toFixed(4) + ' МГц, Доплер «Авто»';
            label.style.left = p.x + 'px';
            label.style.top = (plot.top + LABEL_TOP_PX + rows[i] * ROW_PX) + 'px';
            if (color) { label.style.setProperty('--ml-txmark-color', color); }
            label.addEventListener('click', function(e) {
                if (e && typeof e.stopPropagation === 'function') { e.stopPropagation(); }
                if (self._opts.onPick) { self._opts.onPick(p.m); }
            });
            this._root.appendChild(label);
        }
    };

    TxMarkers.prototype.destroy = function() {
        if (this._root && this._root.parentNode) { this._root.parentNode.removeChild(this._root); }
        this._root = null;
    };

    TxMarkers.markerX = markerX;
    TxMarkers.labelRows = labelRows;
    TxMarkers.markersWithLabels = markersWithLabels;
    TxMarkers.satNames = satNames;

    if (typeof window !== 'undefined') {
        window.TxMarkers = TxMarkers;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            TxMarkers: TxMarkers,
            markerX: markerX,
            labelRows: labelRows,
            markersWithLabels: markersWithLabels,
            satNames: satNames,
        };
    }
})();
