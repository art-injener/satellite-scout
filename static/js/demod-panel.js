// demod-panel.js — блок «Демодулятор»: текущий канал + сетка метрик.
//
// MVP: один канал. Обновление шапки — по событию panorama:tune или вызову setChannel()
// из ManualLayout. Метрики живого режима — из SSE `demod_status`; mock — пока у станции
// нет радиотракта.

'use strict';

(function() {

    /** Частота в MHz для отображения (десятичная запятая). */
    function formatFreqMHz(freqMHz) {
        if (typeof freqMHz !== 'number' || !isFinite(freqMHz)) {
            return '—';
        }
        return freqMHz.toFixed(3).replace('.', ',');
    }

    /** Подбор mock-метрик по строке модуляции. */
    function metricsFromTx(tx) {
        const active = tx.active !== false;
        const mod = (tx.modulation || '').toLowerCase();
        let pipeline = tx.modulation || '—';
        let rate = '—';

        if (mod.indexOf('fsk') >= 0 && (mod.indexOf('1k2') >= 0 || mod.indexOf('1200') >= 0)) {
            pipeline = 'FSK 1200';
            rate = '1.2 ksym/s';
        } else if (mod.indexOf('fsk') >= 0 && mod.indexOf('9600') >= 0) {
            pipeline = 'FSK 9600';
            rate = '9.6 ksym/s';
        } else if (mod.indexOf('afsk') >= 0) {
            pipeline = 'AFSK 1200';
            rate = '1.2 ksym/s';
        } else if (mod.indexOf('bpsk') >= 0) {
            pipeline = 'BPSK 1200';
            rate = '1.2 ksym/s';
        } else if (mod.indexOf('fm') >= 0) {
            pipeline = 'FM';
            rate = '—';
        }

        if (!active) {
            return {
                lock: false,
                pipeline: pipeline,
                snr: '—',
                cn0: '—',
                esn0: '—',
                ber: '—',
                rssi: '—',
                afc: '—',
                foff: '—',
                fec: '—',
                squelch: 'Closed',
                squelchClosed: true,
                rate: rate,
                frames: '0',
                sync: '0',
                crc: '—',
                last: '—',
            };
        }

        return {
            lock: true,
            pipeline: pipeline,
            snr: '12.4 dB',
            cn0: '52.3 dB·Hz',
            esn0: '8.4 dB',
            ber: '3.2e-4',
            rssi: '−42 dBm',
            afc: '−18 Hz',
            foff: '+1.2 kHz',
            fec: '12 B',
            squelch: 'Open / −85 dB',
            squelchClosed: false,
            rate: rate,
            frames: '128',
            sync: '131',
            crc: 'OK',
            last: '0.8 s',
        };
    }

    // Подписи демодуляторов сервера (internal/sdr.Demods) и скорость символов.
    const DEMOD_LABELS = {
        afsk1200: { pipeline: 'AFSK 1200', rate: '1.2 ksym/s' },
        fsk9600_g3ruh: { pipeline: 'FSK 9600 G3RUH', rate: '9.6 ksym/s' },
        fsk9600_geoscan: { pipeline: 'FSK 9600 GEOSCAN', rate: '9.6 ksym/s' },
    };

    /** Подпись демодулятора по идентификатору сервера; неизвестный — как есть. */
    function demodLabel(id) {
        const known = DEMOD_LABELS[id];
        return known ? known.pipeline : id;
    }

    /** Уровень в dBm по поправке тракта, без неё — в dBFS; минус типографский. */
    function formatLevel(dbfs, levelOffset) {
        const text = typeof levelOffset === 'number'
            ? (dbfs + levelOffset).toFixed(0) + ' dBm'
            : dbfs.toFixed(1) + ' dBFS';
        return text.replace('-', '−');
    }

    function formatAge(ms) {
        const s = Math.max(0, ms) / 1000;
        return s < 10 ? s.toFixed(1) + ' s' : Math.round(s) + ' s';
    }

    /**
     * Метрики панели из SSE `demod_status`. Чего демодулятор пока не измеряет — «—».
     * CRC последнего кадра считает DemodPanel.applyStatus по приросту счётчиков.
     * @param {Object} st — событие сервера (internal/demod.StatusEvent)
     * @param {?number} levelOffset — сдвиг dBFS в dBm, null — без поправки
     * @param {number} nowMs — текущее время, мс эпохи
     */
    function metricsFromStatus(st, levelOffset, nowMs) {
        const known = DEMOD_LABELS[st.demod];
        const open = st.active && st.squelch_open;
        return {
            lock: st.active && st.supported,
            pipeline: st.supported && known ? known.pipeline : st.demod + ' — не реализован',
            snr: '—',
            cn0: '—',
            esn0: '—',
            ber: '—',
            rssi: formatLevel(st.level_dbfs, levelOffset),
            afc: '—',
            foff: '—',
            fec: '—',
            squelch: (open ? 'Open' : 'Closed') + ' / ' + formatLevel(st.squelch_dbfs, levelOffset),
            squelchClosed: !open,
            rate: known ? known.rate : '—',
            frames: String(st.frames_good),
            sync: String(st.frames_good + st.frames_bad),
            crc: '—',
            last: st.last_packet_ms ? formatAge(nowMs - st.last_packet_ms) : '—',
        };
    }

    /** Метрики живого режима до первого состояния с сервера. */
    function idleLiveMetrics(pipeline) {
        return {
            lock: false, pipeline: pipeline || '—', snr: '—', cn0: '—', esn0: '—', ber: '—', rssi: '—',
            afc: '—', foff: '—', fec: '—', squelch: '—', squelchClosed: true, rate: '—',
            frames: '—', sync: '—', crc: '—', last: '—',
        };
    }

    /**
     * DemodPanel — шапка текущего канала и сетка метрик.
     * @param {Object} [opts]
     * @param {HTMLElement} [opts.root] — #manual-demod-panel
     */
    function DemodPanel(opts) {
        opts = opts || {};
        this._root = opts.root || document.getElementById('manual-demod-panel');
        this._channel = null;
        this._metrics = null;
        this._mockTimer = null;

        this._els = {
            lockHead: document.getElementById('manual-ch-lock'),
            name: document.getElementById('manual-ch-name'),
            freq: document.getElementById('manual-ch-freq'),
            pipeline: document.getElementById('manual-ch-pipeline'),
            dmLock: document.getElementById('manual-dm-lock'),
            snr: document.getElementById('manual-dm-snr'),
            cn0: document.getElementById('manual-dm-cn0'),
            esn0: document.getElementById('manual-dm-esn0'),
            pipelineVal: document.getElementById('manual-dm-pipeline'),
            ber: document.getElementById('manual-dm-ber'),
            rssi: document.getElementById('manual-dm-rssi'),
            afc: document.getElementById('manual-dm-afc'),
            foff: document.getElementById('manual-dm-foff'),
            fec: document.getElementById('manual-dm-fec'),
            squelch: document.getElementById('manual-dm-squelch'),
            rate: document.getElementById('manual-dm-rate'),
            frames: document.getElementById('manual-dm-frames'),
            sync: document.getElementById('manual-dm-sync'),
            crc: document.getElementById('manual-dm-crc'),
            last: document.getElementById('manual-dm-last'),
        };
        this._frames = 0;
        this._syncCount = 0;
        this._lastFrameAt = 0;
        this._live = false;
        this._liveCounts = { good: 0, bad: 0, crc: '—' };
    }

    /**
     * Живой режим — у станции есть радиотракт: метрики только из applyStatus, mock выключен.
     * @param {boolean} live
     */
    DemodPanel.prototype.setLive = function(live) {
        if (this._live === live) { return; }
        this._live = live;
        if (live) {
            this._stopMockDrift();
            this._liveCounts = { good: 0, bad: 0, crc: '—' };
            this._applyMetrics(idleLiveMetrics());
        } else if (this._channel) {
            this.setChannel(this._channel);
        }
    };

    /**
     * Состояние демодулятора из SSE `demod_status`.
     * @param {Object} st
     * @param {?number} levelOffset — сдвиг dBFS в dBm, null — без поправки
     */
    DemodPanel.prototype.applyStatus = function(st, levelOffset) {
        if (!this._live || !st) { return; }
        const c = this._liveCounts;
        if (st.frames_good < c.good || st.frames_bad < c.bad) {
            c.crc = '—'; // новый сеанс потока: счётчики начались заново
        } else if (st.frames_good > c.good) {
            c.crc = 'OK';
        } else if (st.frames_bad > c.bad) {
            c.crc = 'FAIL';
        }
        c.good = st.frames_good;
        c.bad = st.frames_bad;
        const m = metricsFromStatus(st, levelOffset, Date.now());
        m.crc = c.crc;
        this._applyMetrics(m);
    };

    DemodPanel.prototype.setChannel = function(ch) {
        if (!ch || typeof ch.freqMHz !== 'number') {
            return;
        }
        this._channel = ch;
        this._renderHeader(ch);
        if (this._live) {
            return;
        }
        this._applyMetrics(metricsFromTx(ch));
        if (ch.active !== false) {
            this._startMockDrift();
        } else {
            this._stopMockDrift();
        }
    };

    DemodPanel.prototype.applyMetrics = function(metrics) {
        this._applyMetrics(metrics || {});
    };

    DemodPanel.prototype.getChannel = function() {
        return this._channel;
    };

    DemodPanel.prototype._renderHeader = function(ch) {
        const els = this._els;
        const locked = ch.active !== false;

        if (els.lockHead) {
            els.lockHead.classList.toggle('ml-demod__channel-led--lock', locked);
            els.lockHead.classList.toggle('ml-demod__channel-led--idle', !locked);
            els.lockHead.title = locked ? 'Lock' : 'Нет сигнала';
        }
        if (els.name) {
            els.name.textContent = ch.label || 'Unknown';
        }
        if (els.freq) {
            els.freq.textContent = formatFreqMHz(ch.freqMHz);
        }
        if (els.pipeline) {
            els.pipeline.textContent = ch.modulation || '—';
        }
    };

    DemodPanel.prototype._applyMetrics = function(metrics) {
        this._metrics = metrics;
        const els = this._els;
        const locked = metrics.lock === true;

        if (els.dmLock) {
            els.dmLock.textContent = locked ? '●' : '○';
            els.dmLock.classList.toggle('ml-demod__value--ok', locked);
            els.dmLock.classList.toggle('ml-demod__value--idle', !locked);
        }
        if (els.snr) {
            this._setText(els.snr, metrics.snr, locked);
        }
        if (els.cn0) {
            this._setText(els.cn0, metrics.cn0, locked);
        }
        if (els.esn0) {
            this._setText(els.esn0, metrics.esn0, locked);
        }
        if (els.pipelineVal) {
            this._setText(els.pipelineVal, metrics.pipeline, locked);
        }
        if (els.ber) {
            this._setText(els.ber, metrics.ber, locked);
        }
        if (els.rssi) {
            this._setText(els.rssi, metrics.rssi, locked);
        }
        if (els.afc) {
            this._setText(els.afc, metrics.afc, locked);
        }
        if (els.foff) {
            this._setText(els.foff, metrics.foff, locked);
        }
        if (els.fec) {
            this._setText(els.fec, metrics.fec, locked);
        }
        if (els.squelch) {
            const squelchOpen = !metrics.squelchClosed && locked;
            els.squelch.textContent = metrics.squelch != null ? String(metrics.squelch) : '—';
            els.squelch.classList.toggle('ml-demod__value--ok', squelchOpen);
            els.squelch.classList.toggle('ml-demod__value--idle', !locked || metrics.squelch === '—');
        }
        if (els.rate) {
            this._setText(els.rate, metrics.rate, locked);
        }
        if (els.frames) {
            this._setText(els.frames, metrics.frames, locked);
        }
        if (els.sync) {
            this._setText(els.sync, metrics.sync, locked);
        }
        if (els.crc) {
            const crcOk = metrics.crc === 'OK';
            els.crc.textContent = metrics.crc != null ? String(metrics.crc) : '—';
            els.crc.classList.toggle('ml-demod__value--ok', crcOk);
            els.crc.classList.toggle('ml-demod__value--idle', !locked || metrics.crc === '—');
            els.crc.classList.toggle('ml-demod__value--bad', metrics.crc === 'FAIL');
        }
        if (els.last) {
            this._setText(els.last, metrics.last, locked);
        }
        this._frames = parseInt(metrics.frames, 10) || 0;
        this._syncCount = parseInt(metrics.sync, 10) || 0;
        this._lastFrameAt = locked ? Date.now() : 0;
    };

    DemodPanel.prototype._setText = function(el, value, locked) {
        el.textContent = value != null ? String(value) : '—';
        el.classList.toggle('ml-demod__value--idle', !locked || value === '—');
    };

    /** Имитация дрейфа метрик при активном канале. */
    DemodPanel.prototype._startMockDrift = function() {
        this._stopMockDrift();
        const self = this;
        this._mockTimer = setInterval(function() {
            if (!self._metrics || self._metrics.lock !== true) {
                return;
            }
            const els = self._els;

            const snrBase = 12.4 + (Math.random() - 0.5) * 1.2;
            const afcBase = -18 + Math.round((Math.random() - 0.5) * 8);
            const foffBase = 1.2 + (Math.random() - 0.5) * 0.4;
            const cn0Base = 52.3 + (Math.random() - 0.5) * 1.5;
            const esn0Base = snrBase - 4.0 + (Math.random() - 0.5) * 0.3;
            const rssiBase = -42 + Math.round((Math.random() - 0.5) * 4);

            if (els.snr) { els.snr.textContent = snrBase.toFixed(1) + ' dB'; }
            if (els.cn0) { els.cn0.textContent = cn0Base.toFixed(1) + ' dB·Hz'; }
            if (els.esn0) { els.esn0.textContent = esn0Base.toFixed(1) + ' dB'; }
            if (els.rssi) { els.rssi.textContent = rssiBase + ' dBm'; }
            if (els.afc) {
                const afcSign = afcBase >= 0 ? '+' : '';
                els.afc.textContent = afcSign + afcBase + ' Hz';
            }
            if (els.foff) {
                const foffSign = foffBase >= 0 ? '+' : '';
                els.foff.textContent = foffSign + foffBase.toFixed(2) + ' kHz';
            }

            if (Math.random() < 0.55) {
                self._frames += 1;
                self._syncCount += 1;
                self._lastFrameAt = Date.now();
                if (els.frames) { els.frames.textContent = String(self._frames); }
                if (els.sync) { els.sync.textContent = String(self._syncCount); }
                if (els.fec) {
                    const fecBytes = Math.floor(Math.random() * 18);
                    els.fec.textContent = fecBytes + ' B';
                }
            } else if (Math.random() < 0.2) {
                self._syncCount += 1;
                if (els.sync) { els.sync.textContent = String(self._syncCount); }
            }

            if (els.last && self._lastFrameAt > 0) {
                const dt = (Date.now() - self._lastFrameAt) / 1000;
                els.last.textContent = dt < 10 ? dt.toFixed(1) + ' s' : Math.round(dt) + ' s';
            }
        }, 1200);
    };

    DemodPanel.prototype._stopMockDrift = function() {
        if (this._mockTimer) {
            clearInterval(this._mockTimer);
            this._mockTimer = null;
        }
    };

    DemodPanel.prototype.destroy = function() {
        this._stopMockDrift();
        this._channel = null;
        this._metrics = null;
    };

    if (typeof window !== 'undefined') {
        window.DemodPanel = DemodPanel;
        window.formatDemodFreqMHz = formatFreqMHz;
        window.demodLabel = demodLabel;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            DemodPanel: DemodPanel,
            formatFreqMHz: formatFreqMHz,
            metricsFromTx: metricsFromTx,
            metricsFromStatus: metricsFromStatus,
            demodLabel: demodLabel,
        };
    }
})();
