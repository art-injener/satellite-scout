// telemetry-table.js — блок «Телеметрия» Ручного режима (ADR-004 § 4.7, блок 4).
//
// MVP: одна таблица без вкладок каналов. Принятые кадры приходят по SSE `telemetry`;
// mock-генератор работает, только пока у станции нет радиотракта.

'use strict';

(function() {

    const MAX_ROWS = 200;
    const MOCK_TICK_MS = 2500;

    // Каталог параметров для mock: подпись + единицы + диапазон.
    const MOCK_PARAMS = [
        { key: 'темп', unit: '°C', min: -15, max: 45, fmt: 1, sign: true },
        { key: 'V сети', unit: 'V', min: 11.4,max: 12.6,fmt: 1, sign: true },
        { key: 'I сети', unit: 'mA', min: 120, max: 480, fmt: 0 },
        { key: 'V бат', unit: 'V', min: 3.6, max: 4.2, fmt: 2 },
        { key: 'V солн', unit: 'V', min: 0, max: 5.5, fmt: 2 },
        { key: 'темп бат', unit: '°C', min: -10, max: 38, fmt: 1, sign: true },
        { key: 'RSSI', unit: 'dBm', min: -105,max: -60, fmt: 0, sign: true },
        { key: 'uptime', unit: 's', min: 0, max: 86400, fmt: 0 },
        { key: 'reboots', unit: '', min: 0, max: 12, fmt: 0 },
    ];

    function pad2(n) { return n < 10 ? '0' + n : String(n); }

    function timeOf(d) {
        return pad2(d.getHours()) + ':' + pad2(d.getMinutes()) + ':' + pad2(d.getSeconds());
    }

    function timeNow() {
        return timeOf(new Date());
    }

    /** Подпись демодулятора из панели приёмника (demod-panel.js), без неё — идентификатор. */
    function demodLabel(id) {
        const label = typeof window !== 'undefined' && window.demodLabel;
        return label ? label(id) : id;
    }

    /** Параметр телеметрии сервера в виде «подпись: значение единица». */
    function paramEntry(p) {
        return { key: p.name, value: p.unit ? p.value + ' ' + p.unit : p.value };
    }

    /**
     * Строка таблицы из кадра SSE `telemetry`: отправитель в колонке канала, демодулятор.
     * Разобранная телеметрия: в строке ключевые поля, все поля с получателем и hex — в details.
     * Без разбора: получатель и данные параметрами; не текстовые данные показываются в hex.
     * @param {Object} ev — событие сервера (internal/demod.TelemetryEvent)
     */
    function telemetryPacket(ev) {
        const pkt = {
            time: timeOf(new Date(ev.timestamp_ms)),
            crc: 'OK',
            channelLabel: ev.src,
            channelFreqMHz: ev.frequency_hz / 1e6,
            demod: demodLabel(ev.demod),
        };
        const parsed = ev.params || [];
        if (parsed.length > 0) {
            pkt.params = parsed.filter(function(p) { return p.key; }).map(paramEntry);
            pkt.details = parsed.map(paramEntry).concat([
                { key: 'кому', value: ev.dest },
                { key: 'hex', value: ev.info_hex || '' },
            ]);
            return pkt;
        }
        const data = ev.info ? { key: 'данные', value: ev.info } : { key: 'hex', value: ev.info_hex || '' };
        pkt.params = [{ key: 'кому', value: ev.dest }, data];
        return pkt;
    }

    function formatFreqMHz(freqMHz) {
        if (typeof freqMHz !== 'number' || !isFinite(freqMHz)) {
            return '—';
        }
        return freqMHz.toFixed(3).replace('.', ',');
    }

    /** Форматирует одно значение параметра с возможным знаком и единицей. */
    function formatParamValue(p) {
        const val = p.min + Math.random() * (p.max - p.min);
        let str = val.toFixed(p.fmt);
        if (p.sign && val >= 0) { str = '+' + str; }
        if (p.unit) { str += p.unit; }
        return str;
    }

    /** Случайный набор параметров (3–6 штук) для одного пакета. */
    function randomParams() {
        const pool = MOCK_PARAMS.slice();
        const count = 3 + Math.floor(Math.random() * 4);
        const out = [];
        for (let i = 0; i < count && pool.length > 0; i++) {
            const idx = Math.floor(Math.random() * pool.length);
            const p = pool.splice(idx, 1)[0];
            out.push({ key: p.key, value: formatParamValue(p) });
        }
        return out;
    }

    /**
     * TelemetryTable — контроллер блока «Телеметрия».
     * @param {Object} [opts]
     * @param {HTMLElement} [opts.tbody]    — #manual-tmi-tbody
     * @param {HTMLElement} [opts.scrollEl] — #manual-tmi-scroll
     */
    function TelemetryTable(opts) {
        opts = opts || {};
        this._tbody = opts.tbody || document.getElementById('manual-tmi-tbody');
        this._scrollEl = opts.scrollEl || document.getElementById('manual-tmi-scroll');
        this._modal = document.getElementById('manual-tmi-details');
        this._modalTitle = document.getElementById('manual-tmi-details-title');
        this._modalTbody = document.getElementById('manual-tmi-details-tbody');
        const self = this;
        this._onModalClick = function(e) {
            if (e.target && e.target.dataset && e.target.dataset.action === 'close') { self._hideDetails(); }
        };
        this._onKeydown = function(e) {
            if (e.key === 'Escape') { self._hideDetails(); }
        };
        if (this._modal) {
            this._modal.addEventListener('click', this._onModalClick);
            document.addEventListener('keydown', this._onKeydown);
        }
        this._count = 0;
        this._frameSeq = 1;
        this._autoScroll = true;
        this._mockTimer = null;
        this._active = false;
        this._live = false;
        this._channelLabel = 'ISS-FSK';
        this._channelFreq = 437.800;
    }

    TelemetryTable.prototype._channelText = function(pkt) {
        const label = pkt && pkt.channelLabel ? pkt.channelLabel : this._channelLabel;
        const freqMHz = pkt && typeof pkt.channelFreqMHz === 'number' && isFinite(pkt.channelFreqMHz)
            ? pkt.channelFreqMHz
            : this._channelFreq;
        return label + ' · ' + formatFreqMHz(freqMHz) + ' MHz';
    };

    /**
     * Добавить одну строку = один пакет (реальный или mock).
     * @param {Object} pkt
     * @param {string} [pkt.time]
     * @param {number} [pkt.frame]
     * @param {string} [pkt.crc] — 'OK' | 'FAIL'
     * @param {string} [pkt.channelLabel]
     * @param {number} [pkt.channelFreqMHz]
     * @param {string} [pkt.demod] — подпись демодулятора, принявшего кадр
     * @param {Array<{key:string,value:string}>} [pkt.params] — параметры пакета
     * @param {Array<{key:string,value:string}>} [pkt.details] — все параметры: строка открывает их по клику
     */
    TelemetryTable.prototype.addPacket = function(pkt) {
        if (!this._tbody) { return; }
        pkt = pkt || {};

        const tr = document.createElement('tr');
        const crcOk = pkt.crc !== 'FAIL';
        const classes = [];
        if (!crcOk) { classes.push('ml-tmi__row--fail'); }
        if (pkt.details) { classes.push('ml-tmi__row--details'); }
        if (classes.length > 0) {
            tr.className = classes.join(' ');
        }

        const channel = this._channelText(pkt);
        const tdChan = document.createElement('td');
        tdChan.className = 'ml-tmi__chan';
        tdChan.textContent = channel;
        tr.appendChild(tdChan);

        const tdDemod = document.createElement('td');
        tdDemod.textContent = pkt.demod || '—';
        tr.appendChild(tdDemod);

        const time = pkt.time || timeNow();
        const tdTime = document.createElement('td');
        tdTime.textContent = time;
        tr.appendChild(tdTime);

        if (pkt.details) {
            const self = this;
            const title = [channel, time, pkt.demod].filter(Boolean).join(' · ');
            tr.title = 'Все параметры кадра';
            tr.addEventListener('click', function() { self._showDetails(title, pkt.details); });
        }

        const tdFrame = document.createElement('td');
        tdFrame.textContent = String(pkt.frame != null ? pkt.frame : this._frameSeq);
        tr.appendChild(tdFrame);

        const tdCrc = document.createElement('td');
        tdCrc.textContent = pkt.crc || 'OK';
        tdCrc.className = crcOk ? 'ml-tmi__crc--ok' : 'ml-tmi__crc--fail';
        tr.appendChild(tdCrc);

        const tdParams = document.createElement('td');
        tdParams.className = 'ml-tmi__params';
        const params = pkt.params || [];
        for (let i = 0; i < params.length; i++) {
            if (i > 0) {
                const comma = document.createElement('span');
                comma.textContent = ', ';
                tdParams.appendChild(comma);
            }
            const keyEl = document.createElement('span');
            keyEl.className = 'ml-tmi__param-key';
            keyEl.textContent = params[i].key + ':';
            tdParams.appendChild(keyEl);
            const valEl = document.createElement('span');
            valEl.className = 'ml-tmi__param-val';
            valEl.textContent = params[i].value;
            tdParams.appendChild(valEl);
        }
        tr.appendChild(tdParams);

        this._tbody.appendChild(tr);
        this._count++;
        this._frameSeq++;

        if (this._count > MAX_ROWS && this._tbody.firstChild) {
            this._tbody.removeChild(this._tbody.firstChild);
            this._count--;
        }

        if (this._autoScroll && this._scrollEl) {
            this._scrollEl.scrollTop = this._scrollEl.scrollHeight;
        }
    };

    /** Текущий канал для новых пакетов (история строк не меняется). */
    TelemetryTable.prototype.setChannel = function(label, freqMHz) {
        this._channelLabel = label || 'Unknown';
        this._channelFreq = freqMHz || 0;
    };

    /** Окно со всеми параметрами кадра; повторное открытие заменяет строки. */
    TelemetryTable.prototype._showDetails = function(title, details) {
        if (!this._modal || !this._modalTbody) { return; }
        if (this._modalTitle) { this._modalTitle.textContent = title; }
        this._modalTbody.innerHTML = '';
        for (let i = 0; i < details.length; i++) {
            const tr = document.createElement('tr');
            const tdKey = document.createElement('td');
            tdKey.className = 'ml-tmi-details__key';
            tdKey.textContent = details[i].key;
            tr.appendChild(tdKey);
            const tdVal = document.createElement('td');
            tdVal.className = 'ml-tmi-details__val';
            tdVal.textContent = details[i].value;
            tr.appendChild(tdVal);
            this._modalTbody.appendChild(tr);
        }
        this._modal.classList.remove('modal--hidden');
    };

    TelemetryTable.prototype._hideDetails = function() {
        if (this._modal) { this._modal.classList.add('modal--hidden'); }
    };

    TelemetryTable.prototype.clear = function() {
        if (this._tbody) { this._tbody.innerHTML = ''; }
        this._count = 0;
        this._frameSeq = 1;
    };

    // ── Mock-генератор ────────────────────────────────────────────────────

    TelemetryTable.prototype.startMock = function() {
        this.stopMock();
        this._active = true;
        const self = this;
        this._mockTimer = setInterval(function() {
            self._mockTick();
        }, MOCK_TICK_MS);
    };

    TelemetryTable.prototype.stopMock = function() {
        if (this._mockTimer) {
            clearInterval(this._mockTimer);
            this._mockTimer = null;
        }
        this._active = false;
    };

    TelemetryTable.prototype._mockTick = function() {
        this.addPacket({
            time: timeNow(),
            frame: this._frameSeq,
            crc: Math.random() < 0.07 ? 'FAIL' : 'OK',
            params: randomParams(),
        });
    };

    /** Принятый кадр из SSE `telemetry`. */
    TelemetryTable.prototype.addTelemetry = function(ev) {
        this.addPacket(telemetryPacket(ev));
    };

    // ── Жизненный цикл ───────────────────────────────────────────────────

    /**
     * Живой режим — у станции есть радиотракт: строки дают только принятые кадры, mock выключен.
     * @param {boolean} live
     */
    TelemetryTable.prototype.setLive = function(live) {
        if (this._live === live) { return; }
        this._live = live;
        if (live) {
            this.stopMock();
            this._active = true;
        } else if (this._active) {
            this.startMock();
        }
    };

    TelemetryTable.prototype.activate = function() {
        if (this._active) { return; }
        if (this._live) {
            this._active = true;
            return;
        }
        this.startMock();
    };

    TelemetryTable.prototype.deactivate = function() {
        this.stopMock();
    };

    TelemetryTable.prototype.destroy = function() {
        this.stopMock();
        if (this._modal) {
            this._modal.removeEventListener('click', this._onModalClick);
            document.removeEventListener('keydown', this._onKeydown);
            this._modal = null;
        }
        this._tbody = null;
        this._scrollEl = null;
    };

    // ── Экспорт ──────────────────────────────────────────────────────────

    if (typeof window !== 'undefined') {
        window.TelemetryTable = TelemetryTable;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { TelemetryTable: TelemetryTable, telemetryPacket: telemetryPacket }; // eslint-disable-line no-undef
    }
})();
