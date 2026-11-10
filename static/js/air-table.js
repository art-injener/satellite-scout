// air-table.js — таблица «Эфир» под панорамой: участки band_signals.
//
// Столбцы совпадают со строками карточки маркера (BandMarks.markRows).
// Щелчок по строке вызывает onPick — тот же путь, что щелчок по маркеру.

'use strict';

(function() {

    /**
     * @param {HTMLElement} tbody
     * @param {{onPick: ?function(Object), nameOf: ?function(number): ?string}} opts
     */
    function AirTable(tbody, opts) {
        this._tbody = tbody || null;
        this._onPick = opts && opts.onPick;
        this._nameOf = opts && opts.nameOf;
        this._signals = [];
        this._selectedId = null;
        this._onClick = null;
        if (this._tbody) {
            const self = this;
            this._onClick = function(ev) {
                const tr = ev.target && ev.target.closest ? ev.target.closest('tr[data-sig-id]') : null;
                if (!tr || !self._onPick) { return; }
                const id = tr.getAttribute('data-sig-id');
                for (let i = 0; i < self._signals.length; i++) {
                    if (self._signals[i].id === id) {
                        self._selectedId = id;
                        self._paintSelection();
                        self._onPick(self._signals[i]);
                        return;
                    }
                }
            };
            this._tbody.addEventListener('click', this._onClick);
        }
    }

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
        const rows = window.BandMarks && typeof window.BandMarks.markRows === 'function'
            ? window.BandMarks.markRows
            : null;
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
                td.textContent = fields[c][1];
                tr.appendChild(td);
            }
            this._tbody.appendChild(tr);
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
        if (this._tbody && this._onClick) {
            this._tbody.removeEventListener('click', this._onClick);
        }
        if (this._tbody) { this._tbody.textContent = ''; }
        this._tbody = null;
        this._onClick = null;
        this._signals = [];
    };

    if (typeof window !== 'undefined') {
        window.AirTable = AirTable;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { AirTable: AirTable }; // eslint-disable-line no-undef
    }
})();
