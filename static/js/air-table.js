// air-table.js — таблица «Эфир»: участки band_signals.
//
// Столбцы — BandMarks.markRows (панорама) или hearRows (спектр), задаётся rowsFn.
// Щелчок по строке вызывает onPick, если задан (панорама). На Спектре таблица без onPick — только чтение.

'use strict';

(function() {

    /**
     * @param {HTMLElement} tbody
     * @param {{onPick: ?function(Object), onOpenPayload: ?function(Object), nameOf: ?function(number): ?string, rowsFn: ?function(Object, ?string): Array}} opts
     */
    function AirTable(tbody, opts) {
        this._tbody = tbody || null;
        this._onPick = opts && opts.onPick;
        this._onOpenPayload = opts && opts.onOpenPayload;
        this._nameOf = opts && opts.nameOf;
        this._rowsFn = opts && opts.rowsFn;
        this._signals = [];
        this._selectedId = null;
        this._onClick = null;
        this._onPointerDown = null;
        if (this._tbody) {
            const self = this;
            /** Открытие модалки: pointerdown, чтобы не терять клик при перерисовке строки. */
            this._onPointerDown = function(ev) {
                if (ev.button != null && ev.button !== 0) { return; }
                const hit = ev.target && ev.target.closest
                    ? ev.target.closest('[data-air-payload]') : null;
                if (!hit) { return; }
                ev.preventDefault();
                ev.stopPropagation();
                const id = hit.getAttribute('data-air-payload');
                const sig = self._signalById(id);
                if (sig && self._onOpenPayload) { self._onOpenPayload(sig); }
            };
            this._onClick = function(ev) {
                // Модалку уже открыли на pointerdown — не дублируем.
                if (ev.target && ev.target.closest
                    && ev.target.closest('[data-air-payload]')) {
                    ev.preventDefault();
                    ev.stopPropagation();
                    return;
                }
                const tr = ev.target && ev.target.closest ? ev.target.closest('tr[data-sig-id]') : null;
                if (!tr || !self._onPick) { return; }
                const id = tr.getAttribute('data-sig-id');
                const sig = self._signalById(id);
                if (!sig) { return; }
                self._selectedId = id;
                self._paintSelection();
                self._onPick(sig);
            };
            this._tbody.addEventListener('pointerdown', this._onPointerDown);
            this._tbody.addEventListener('click', this._onClick);
        }
    }

    /** @param {?string} id @returns {?Object} */
    AirTable.prototype._signalById = function(id) {
        if (!id) { return null; }
        for (let i = 0; i < this._signals.length; i++) {
            if (this._signals[i].id === id) { return this._signals[i]; }
        }
        return null;
    };

    /**
     * @param {Array} signals — band_signals.signals или накопленные отметки панорамы
     */
    AirTable.prototype.setSignals = function(signals) {
        this._signals = Array.isArray(signals) ? signals.slice() : [];
        this._signals.sort(function(a, b) { return (a.center_hz || 0) - (b.center_hz || 0); });
        this.render();
    };

    AirTable.prototype.render = function() {
        if (!this._tbody) { return; }
        const rows = this._rowsFn
            || (window.BandMarks && typeof window.BandMarks.markRows === 'function'
                ? window.BandMarks.markRows : null);
        if (!rows) { return; }
        const nameOf = this._nameOf;
        const sample = rows({ center_hz: 0, stats: {} }, null);
        this._syncHead(sample.map(function(field) { return field[0]; }));
        this._tbody.textContent = '';
        for (let i = 0; i < this._signals.length; i++) {
            const sig = this._signals[i];
            const satName = nameOf && sig.norad_id ? nameOf(sig.norad_id) : null;
            const fields = rows(sig, satName);
            const tr = document.createElement('tr');
            if (sig.id) { tr.setAttribute('data-sig-id', sig.id); }
            if (sig.id && sig.id === this._selectedId) { tr.classList.add('is-current'); }
            for (let c = 0; c < fields.length; c++) {
                const td = document.createElement('td');
                this._fillCell(td, fields[c][1], sig);
                tr.appendChild(td);
            }
            this._tbody.appendChild(tr);
        }
    };

    /**
     * Строка или ячейка «Данные» с типом и кнопкой модалки.
     * @param {HTMLElement} td
     * @param {string|Object} value
     * @param {Object} sig
     */
    AirTable.prototype._fillCell = function(td, value, sig) {
        if (value && typeof value === 'object' && value.type === 'payload') {
            td.className = 'ml-air__data';
            td.textContent = '';
            const label = document.createElement('span');
            label.className = 'ml-air__data-kind';
            label.textContent = value.label || '—';
            td.appendChild(label);
            if (this._onOpenPayload && sig && sig.id) {
                td.setAttribute('data-air-payload', sig.id);
                td.setAttribute('title', 'Подробности пакета');
                const btn = document.createElement('button');
                btn.type = 'button';
                btn.className = 'ml-air__data-btn';
                btn.setAttribute('data-air-payload', sig.id);
                btn.setAttribute('aria-label', 'Подробности данных');
                btn.setAttribute('title', 'Подробности');
                btn.textContent = 'ⓘ';
                td.appendChild(btn);
            }
            return;
        }
        if (value && typeof value === 'object' && value.type === 'tip') {
            td.removeAttribute('data-air-payload');
            td.className = 'ml-air__tip';
            td.textContent = value.text == null ? '' : String(value.text);
            const tip = value.title || value.text;
            if (tip) { td.setAttribute('title', String(tip)); }
            else { td.removeAttribute('title'); }
            return;
        }
        td.removeAttribute('data-air-payload');
        td.className = '';
        const text = value == null ? '' : String(value);
        td.textContent = text;
        // Узкие колонки с ellipsis: полный текст в подсказке при наведении.
        if (text && text !== '—') { td.setAttribute('title', text); }
        else { td.removeAttribute('title'); }
    };

    /**
     * Обновить Кадры/Данные одной строки без полной перерисовки (клик ⓘ не теряется).
     * @param {Object} sig
     */
    AirTable.prototype.patchHear = function(sig) {
        if (!this._tbody || !sig || !sig.id) { return; }
        const idx = this._signals.findIndex(function(s) { return s.id === sig.id; });
        if (idx >= 0) { this._signals[idx] = sig; }
        else { this._signals.push(sig); }
        const safeId = String(sig.id).replace(/\\/g, '\\\\').replace(/"/g, '\\"');
        const tr = this._tbody.querySelector
            ? this._tbody.querySelector('tr[data-sig-id="' + safeId + '"]')
            : null;
        if (!tr) {
            // В тестах без querySelector — полный render; в браузере строка появится на band_signals.
            this.render();
            return;
        }
        const rows = this._rowsFn
            || (window.BandMarks && typeof window.BandMarks.hearRows === 'function'
                ? window.BandMarks.hearRows : null);
        if (!rows) { return; }
        const nameOf = this._nameOf;
        const satName = nameOf && sig.norad_id ? nameOf(sig.norad_id) : null;
        const fields = rows(sig, satName);
        // hearRows: [7]=Кадры, [8]=Данные, [9]=Обновлён
        for (const col of [7, 8, 9]) {
            if (!fields[col] || !tr.children[col]) { continue; }
            this._fillCell(tr.children[col], fields[col][1], sig);
        }
    };

    /** Подписи столбцов берутся из тех же строк, что ячейки. */
    AirTable.prototype._syncHead = function(labels) {
        const table = this._tbody && typeof this._tbody.closest === 'function'
            ? this._tbody.closest('table') : null;
        const head = table && typeof table.querySelector === 'function'
            ? table.querySelector('thead tr') : null;
        if (!head) { return; }
        while (head.children.length < labels.length) {
            head.appendChild(document.createElement('th'));
        }
        while (head.children.length > labels.length) {
            head.removeChild(head.lastChild);
        }
        for (let i = 0; i < labels.length; i++) {
            if (head.children[i].textContent !== labels[i]) {
                head.children[i].textContent = labels[i];
            }
        }
    };

    AirTable.prototype._paintSelection = function() {
        if (!this._tbody) { return; }
        const list = this._tbody.querySelectorAll('tr[data-sig-id]');
        for (let i = 0; i < list.length; i++) {
            const tr = list[i];
            if (tr.getAttribute('data-sig-id') === this._selectedId) {
                tr.classList.add('is-current');
            } else {
                tr.classList.remove('is-current');
            }
        }
    };

    AirTable.prototype.destroy = function() {
        if (this._tbody && this._onPointerDown) {
            this._tbody.removeEventListener('pointerdown', this._onPointerDown);
        }
        if (this._tbody && this._onClick) {
            this._tbody.removeEventListener('click', this._onClick);
        }
        if (this._tbody) { this._tbody.textContent = ''; }
        this._tbody = null;
        this._onClick = null;
        this._onPointerDown = null;
        this._signals = [];
    };

    if (typeof window !== 'undefined') {
        window.AirTable = AirTable;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { AirTable: AirTable }; // eslint-disable-line no-undef
    }
})();
