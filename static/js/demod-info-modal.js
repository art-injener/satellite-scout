// demod-info-modal.js — модалка «Информация» по демодулятору: текущие метрики канала,
// диагноз «почему нет приёма», графики метрик за 60 с (уровень, SNR с порогами QSSTV,
// несущая, такты, кадры) и диаграмма качества демодуляции (eye/constellation), перенесённая
// из вкладки «Диаграмма». Источник данных — DemodPanel: applyStatus копит MetricHistory,
// diagnose даёт текст. Цвета графиков — через CSS-переменные.

'use strict';

(function() {

    /** Частота в Гц или кГц с десятичной запятой. */
    function formatInfoHz(hz) {
        const abs = Math.abs(hz);
        if (abs >= 1000) {
            const v = (abs / 1000);
            const text = (v >= 10 ? v.toFixed(0) : v.toFixed(1)).replace('.', ',');
            return text + ' кГц';
        }
        return Math.round(abs) + ' Гц';
    }

    /** Частота со знаком; минус типографский. */
    function formatInfoSignedHz(hz) {
        const text = formatInfoHz(hz);
        if (Math.round(hz) === 0) { return text; }
        return (hz < 0 ? '−' : '+') + text;
    }

    /** Частота в МГц с десятичной запятой. */
    function formatInfoMHz(hz) {
        return (hz / 1e6).toFixed(3).replace('.', ',') + ' МГц';
    }

    /** Уровень в dBm по поправке тракта, без неё — в dBFS; минус типографский. */
    function formatInfoLevel(dbfs, levelOffset) {
        const text = typeof levelOffset === 'number'
            ? (dbfs + levelOffset).toFixed(0) + ' dBm'
            : dbfs.toFixed(1) + ' dBFS';
        return text.replace('-', '−');
    }

    function cssVar(name, fallback) {
        if (typeof window.cssVar === 'function') {
            return window.cssVar(name, fallback);
        }
        return fallback;
    }

    // Пороги SSTV по QSSTV: VIS ловится до −3 дБ SNR, sync-pulse — до −6 дБ.
    const SNR_VIS_THRESHOLD = -3;
    const SNR_SYNC_THRESHOLD = -6;
    // Порог захвата тактов — доля символа, как в demod-panel.js (CLOCK_LOCK_JITTER).
    const CLOCK_LOCK_JITTER = 0.12;

    const CHART_KEYS = ['level', 'snr', 'carrier', 'jitter', 'frames'];

    /**
     * Рисует один график метрики на canvas. Ось X — время (60 с), ось Y — значение.
     * @param {Object} cfg
     * @param {HTMLCanvasElement} cfg.canvas
     * @param {Array} cfg.points — точки MetricHistory
     * @param {number} cfg.nowMs
     * @param {function} cfg.value — (pt) => число|null
     * @param {Object} cfg.range — {min, max} оси Y
     * @param {Array} cfg.thresholds — [{value, label, color}] горизонтальные пороги
     * @param {string} cfg.color — цвет линии
     */
    function drawChart(cfg) {
        const canvas = cfg.canvas;
        if (!canvas) { return; }
        const ctx = canvas.getContext('2d');
        if (!ctx) { return; }
        const w = canvas.width = canvas.clientWidth || 300;
        const h = canvas.height = canvas.clientHeight || 60;
        ctx.clearRect(0, 0, w, h);

        const bg = cssVar('--decode-instrument-bg', '#2a3440');
        ctx.fillStyle = bg;
        ctx.fillRect(0, 0, w, h);

        const range = cfg.range;
        const span = (range.max - range.min) || 1;
        const fmt = cfg.tick || function(v) { return String(Math.round(v)); };
        const ticks = cfg.ticks || [range.min, (range.min + range.max) / 2, range.max];
        const labels = ticks.map(function(t) { return fmt(t).replace('-', '−'); });
        // Ширина поля подписей по самой длинной из них.
        ctx.font = '10px monospace';
        let gutter = 16;
        for (const l of labels) {
            const tw = ctx.measureText ? ctx.measureText(l).width : l.length * 6;
            if (tw > gutter) { gutter = tw; }
        }
        gutter = Math.ceil(gutter) + 8;
        const padT = 9;
        const padB = 9;
        const plotH = h - padT - padB;
        const plotW = w - gutter - 4;
        const windowMs = 60000;
        const start = cfg.nowMs - windowMs;
        const yOf = function(v) { return padT + plotH - (v - range.min) / span * plotH; };

        // Шкала: деления и тусклая сетка.
        ctx.textAlign = 'right';
        for (let i = 0; i < ticks.length; i++) {
            const y = yOf(ticks[i]);
            ctx.strokeStyle = 'rgba(255,255,255,0.10)';
            ctx.setLineDash([]);
            ctx.beginPath();
            ctx.moveTo(gutter, y);
            ctx.lineTo(w - 2, y);
            ctx.stroke();
            ctx.fillStyle = cssVar('--text-muted', '#8a96a4');
            ctx.textBaseline = y < padT + 6 ? 'top' : (y > h - padB - 6 ? 'bottom' : 'middle');
            ctx.fillText(labels[i], gutter - 4, y);
        }

        // Пороговые линии. Подписи справа и сдвигаются вниз, если пороги близко.
        if (cfg.thresholds) {
            const marks = [];
            for (const th of cfg.thresholds) {
                if (th.value < range.min || th.value > range.max) { continue; }
                const y = yOf(th.value);
                ctx.strokeStyle = th.color || 'rgba(255,255,255,0.35)';
                ctx.setLineDash([3, 3]);
                ctx.beginPath();
                ctx.moveTo(gutter, y);
                ctx.lineTo(w - 2, y);
                ctx.stroke();
                ctx.setLineDash([]);
                if (th.label) { marks.push({ y: y, label: th.label }); }
            }
            marks.sort(function(a, b) { return a.y - b.y; });
            ctx.font = '10px monospace';
            ctx.textAlign = 'right';
            ctx.textBaseline = 'bottom';
            ctx.fillStyle = cssVar('--text-color', '#d7dee6');
            let prevBottom = -1e9;
            for (const m of marks) {
                let ly = m.y - 1;
                if (ly < prevBottom + 12) { ly = prevBottom + 12; }
                ly = Math.max(padT + 11, Math.min(h - 2, ly));
                ctx.fillText(m.label, w - 4, ly);
                prevBottom = ly;
            }
        }

        // Линии метрик. cfg.series — несколько линий на одной шкале (кадры: верные и с ошибкой).
        const series = cfg.series || [{ value: cfg.value, color: cfg.color }];
        for (const s of series) {
            ctx.strokeStyle = s.color || cssVar('--accent-color', '#5fbcd3');
            ctx.lineWidth = 1.5;
            ctx.beginPath();
            let started = false;
            for (const p of cfg.points) {
                const v = s.value(p);
                // Разрыв линии, где значения нет (демодулятор был выключен).
                if (v === null || v === undefined || !isFinite(v)) { started = false; continue; }
                const x = gutter + Math.max(0, Math.min(plotW, (p.t - start) / windowMs * plotW));
                if (!started) { ctx.moveTo(x, yOf(v)); started = true; } else { ctx.lineTo(x, yOf(v)); }
            }
            ctx.stroke();
        }

        // Цифра текущего значения в правом верхнем углу, поверх линии.
        const readout = [];
        for (const s of series) {
            let last = null;
            for (let i = cfg.points.length - 1; i >= 0; i--) {
                const v = s.value(cfg.points[i]);
                if (v !== null && v !== undefined && isFinite(v)) { last = v; break; }
            }
            if (last !== null) { readout.push(fmt(last).replace('-', '−')); }
        }
        if (readout.length) {
            const text = readout.join(' / ');
            ctx.font = 'bold 11px monospace';
            const tw = ctx.measureText ? ctx.measureText(text).width : text.length * 6;
            const rx = w - tw - 8;
            ctx.fillStyle = bg;
            ctx.fillRect(rx - 3, 2, tw + 7, 14);
            ctx.textAlign = 'left';
            ctx.textBaseline = 'top';
            ctx.fillStyle = cssVar('--text-color', '#d7dee6');
            ctx.fillText(text, rx, 3);
        }
    }

    /**
     * DemodInfoModal — модалка диагностики демодулятора.
     * @param {Object} [opts]
     * @param {function(): number} [opts.now] — часы, мс эпохи
     */
    function DemodInfoModal(opts) {
        opts = opts || {};
        this._now = opts.now || Date.now;
        this._root = document.getElementById('demod-info-modal');
        this._eyeView = null;
        this._raf = null;
        this._open = false;

        this._els = {
            diag: document.getElementById('demod-info-diag'),
            demod: document.getElementById('demod-info-demod'),
            freq: document.getElementById('demod-info-freq'),
            bw: document.getElementById('demod-info-bw'),
            gain: document.getElementById('demod-info-gain'),
            doppler: document.getElementById('demod-info-doppler'),
            squelch: document.getElementById('demod-info-squelch'),
            level: document.getElementById('demod-info-level'),
            snr: document.getElementById('demod-info-snr'),
            carrier: document.getElementById('demod-info-carrier'),
            clock: document.getElementById('demod-info-clock'),
            sync: document.getElementById('demod-info-sync'),
            frames: document.getElementById('demod-info-frames'),
            charts: {
                level: document.getElementById('demod-info-chart-level'),
                snr: document.getElementById('demod-info-chart-snr'),
                carrier: document.getElementById('demod-info-chart-carrier'),
                jitter: document.getElementById('demod-info-chart-jitter'),
                frames: document.getElementById('demod-info-chart-frames'),
            },
        };

        this._guideOpen = false;
        this._bindClose();
        this._bindHelp();
        this._initEye();
    }

    /** Кнопка «Как читать» раскрывает памятку по шкалам. При открытии модалки она свёрнута. */
    DemodInfoModal.prototype._bindHelp = function() {
        this._helpBtn = document.getElementById('demod-info-help');
        this._guide = document.getElementById('demod-info-guide');
        if (!this._helpBtn || !this._guide) { return; }
        const self = this;
        this._helpBtn.addEventListener('click', function() { self._setGuide(!self._guideOpen); });
        this._setGuide(false);
    };

    DemodInfoModal.prototype._setGuide = function(open) {
        this._guideOpen = !!open;
        if (this._guide) { this._guide.hidden = !this._guideOpen; }
        if (this._helpBtn) {
            this._helpBtn.setAttribute('aria-expanded', this._guideOpen ? 'true' : 'false');
        }
    };

    DemodInfoModal.prototype._bindClose = function() {
        if (!this._root) { return; }
        const self = this;
        this._root.querySelectorAll('[data-action="close-demod-info"]').forEach(function(el) {
            el.addEventListener('click', function() { self.close(); });
        });
        document.addEventListener('keydown', function(e) {
            if (self._open && e.key === 'Escape') { self.close(); e.preventDefault(); }
        });
    };

    DemodInfoModal.prototype._initEye = function() {
        if (typeof window.EyeConstellationView !== 'function') { return; }
        this._eyeView = new window.EyeConstellationView({
            canvas: document.getElementById('demod-info-eye-canvas'),
            tabsContainer: document.getElementById('demod-info-eye-tabs'),
            autoEl: document.getElementById('demod-info-eye-auto'),
        });
    };

    DemodInfoModal.prototype.isOpen = function() {
        return this._open;
    };

    DemodInfoModal.prototype.open = function() {
        if (!this._root || this._open) { return; }
        this._open = true;
        this._root.hidden = false;
        this._setGuide(false);
        // Диаграмму не запускаем здесь: она рисует схему, а не измерение.
        // update решает, есть ли сигнал, и только тогда её включает.
        this._blankEye();
        this._loop();
    };

    DemodInfoModal.prototype.close = function() {
        if (!this._root || !this._open) { return; }
        this._open = false;
        this._root.hidden = true;
        if (this._raf) { cancelAnimationFrame(this._raf); this._raf = null; }
        if (this._eyeView) { this._eyeView.deactivate(); }
    };

    /** Текущие метрики канала из demod_status. levelOffset — сдвиг dBFS в dBm, null — без поправки. */
    DemodInfoModal.prototype.applyMetrics = function(st, levelOffset) {
        if (!st) { return; }
        const els = this._els;
        if (els.demod) { els.demod.textContent = (window.demodLabel ? window.demodLabel(st.demod) : st.demod) || '—'; }
        if (els.freq) { els.freq.textContent = st.frequency_hz ? formatInfoMHz(st.frequency_hz) : '—'; }
        if (els.bw) { els.bw.textContent = st.bandwidth_hz ? formatInfoHz(st.bandwidth_hz) : '—'; }
        if (els.gain) {
            els.gain.textContent = (typeof st.gain_db === 'number')
                ? Math.round(st.gain_db) + ' дБ'
                : '—';
        }
        if (els.doppler) {
            const hz = (st.doppler_hz !== undefined && st.doppler_hz !== null) ? st.doppler_hz : 0;
            const mode = st.doppler_auto ? 'авто' : 'ручн.';
            els.doppler.textContent = (hz === 0 && !st.doppler_auto)
                ? 'ручн., 0 Гц'
                : mode + ', ' + formatInfoSignedHz(hz);
        }
        if (els.squelch) { els.squelch.textContent = formatInfoLevel(st.squelch_dbfs, levelOffset); }
        if (els.level) { els.level.textContent = formatInfoLevel(st.level_dbfs, levelOffset); }

        const rx = st.reception;
        if (els.snr) { els.snr.textContent = (rx && rx.snr_db) ? Math.round(rx.snr_db) + ' дБ' : '—'; }
        if (els.carrier) {
            els.carrier.textContent = (rx && (rx.carrier_hz || rx.carrier_hz === 0)) ? formatInfoSignedHz(rx.carrier_hz) : '—';
        }
        if (els.clock) {
            els.clock.textContent = (rx && (rx.clock_jitter || rx.clock_jitter === 0))
                ? 'джиттер ' + Math.round(rx.clock_jitter * 100) + ' %'
                : '—';
        }
        if (els.sync) {
            if (rx && rx.sync && rx.sync.words > 0) {
                const ber = rx.sync.bit_errors / (32 * rx.sync.words);
                els.sync.textContent = rx.sync.words + ' сл., BER ' + ber.toExponential(1).replace('.', ',');
            } else {
                els.sync.textContent = '—';
            }
        }
        if (els.frames) {
            els.frames.textContent = (st.frames_good || st.frames_bad)
                ? st.frames_good + ' / ' + st.frames_bad
                : '—';
        }
    };

    /** Диагноз «почему нет приёма» — {state, text} из DemodPanel.diagnose. */
    DemodInfoModal.prototype.applyDiagnosis = function(diag) {
        const el = this._els.diag;
        if (!el || !diag) { return; }
        el.textContent = diag.text || '—';
        el.classList.remove('demod-info__diag--ok', 'demod-info__diag--warn', 'demod-info__diag--bad', 'demod-info__diag--idle');
        el.classList.add('demod-info__diag--' + (diag.state || 'idle'));
    };

    /** Синхронизирует eye/constellation со строкой модуляции (как во вкладке раньше). */
    DemodInfoModal.prototype.setEyeModulation = function(modStr) {
        if (this._eyeView && modStr) { this._eyeView.setModulation(modStr); }
    };

    /** Перерисовка графиков из MetricHistory. history — DemodPanel.getHistory(). */
    DemodInfoModal.prototype.drawCharts = function(history) {
        if (!history) { return; }
        const now = this._now();
        const pts = history.points(now);
        const accent = cssVar('--accent-color', '#5fbcd3');
        const warn = cssVar('--warn-color', '#e0a040');
        const good = cssVar('--ok-color', '#5fb05f');
        const bad = cssVar('--bad-color', '#e05f5f');
        const muted = 'rgba(255,255,255,0.25)';

        // Точки с выключенным демодулятором не рисуем: статус там обнулён.
        const measured = function(p, v) { return p.off ? null : v; };
        const num = function(v) { return String(Math.round(v)); };
        drawChart({
            canvas: this._els.charts.level, points: pts, nowMs: now, color: accent,
            value: function(p) { return measured(p, p.level); },
            range: { min: -120, max: 0 }, ticks: [-120, -60, 0], tick: num,
            thresholds: [{ value: pts.length ? pts[pts.length - 1].squelch : -90, color: muted, label: 'порог' }],
        });
        drawChart({
            canvas: this._els.charts.snr, points: pts, nowMs: now, color: accent,
            value: function(p) { return measured(p, p.snr || null); },
            range: { min: -10, max: 30 }, ticks: [-10, 0, 30], tick: num,
            thresholds: [
                { value: SNR_VIS_THRESHOLD, color: good, label: 'VIS −3' },
                { value: SNR_SYNC_THRESHOLD, color: warn, label: 'sync −6' },
            ],
        });
        drawChart({
            canvas: this._els.charts.carrier, points: pts, nowMs: now, color: accent,
            value: function(p) { return measured(p, p.carrier || null); },
            range: { min: -5000, max: 5000 }, ticks: [-5000, 0, 5000],
            tick: function(v) { return v === 0 ? '0' : formatInfoSignedHz(v); },
            thresholds: [{ value: 0, color: muted }],
        });
        drawChart({
            canvas: this._els.charts.jitter, points: pts, nowMs: now, color: accent,
            value: function(p) { return measured(p, p.jitter ? p.jitter * 100 : null); },
            range: { min: 0, max: 50 }, ticks: [0, 25, 50], tick: num,
            thresholds: [{ value: CLOCK_LOCK_JITTER * 100, color: warn, label: 'захват 12' }],
        });
        const framesMax = Math.max(1, pts.reduce(function(m, p) {
            return p.off ? m : Math.max(m, p.good, p.bad);
        }, 0));
        drawChart({
            canvas: this._els.charts.frames, points: pts, nowMs: now,
            range: { min: 0, max: framesMax }, ticks: [0, framesMax], tick: num,
            thresholds: [],
            series: [
                { value: function(p) { return measured(p, p.good); }, color: good },
                { value: function(p) { return measured(p, p.bad); }, color: bad },
            ],
        });
    };

    /** Цикл перерисовки, пока модалка открыта. update() вызывается наружу на demod_status. */
    DemodInfoModal.prototype._loop = function() {
        if (!this._open) { return; }
        const self = this;
        this._raf = requestAnimationFrame(function() { self._loop(); });
    };

    /**
     * Диаграмма (eye, созвездие, гистограмма) — схема типичной формы для профиля,
     * она не измеряет принятый сигнал. Показывать её при выключенном демодуляторе
     * или без сигнала нельзя: линии приняли бы за приём. Поэтому она идёт только
     * при открытом пороге, иначе холст пуст.
     */
    DemodInfoModal.prototype._syncEye = function(st) {
        const eye = this._eyeView;
        if (!eye) { return; }
        const live = !!(st && st.active && !st.demod_off && st.squelch_open);
        const active = typeof eye.isActive === 'function' && eye.isActive();
        if (live) {
            if (!active && typeof eye.activate === 'function') { eye.activate(); }
            return;
        }
        if (active && typeof eye.deactivate === 'function') { eye.deactivate(); }
        this._blankEye();
    };

    /** Очищает холст диаграммы до фона прибора. */
    DemodInfoModal.prototype._blankEye = function() {
        const canvas = document.getElementById('demod-info-eye-canvas');
        if (!canvas) { return; }
        const ctx = canvas.getContext('2d');
        if (!ctx) { return; }
        const w = canvas.width = canvas.clientWidth || canvas.width || 300;
        const h = canvas.height = canvas.clientHeight || canvas.height || 160;
        ctx.fillStyle = cssVar('--decode-instrument-bg', '#2a3440');
        ctx.fillRect(0, 0, w, h);
    };

    /** Обновление извне: метрики, диагноз, графики, eye. history и diag — от DemodPanel. */
    DemodInfoModal.prototype.update = function(st, levelOffset, history, diag, modStr) {
        if (!this._open) { return; }
        this.applyMetrics(st, levelOffset);
        this.applyDiagnosis(diag);
        this.drawCharts(history);
        if (modStr) { this.setEyeModulation(modStr); }
        this._syncEye(st);
    };

    DemodInfoModal.prototype.refreshAfterThemeChange = function() {
        // Графики перерисуются на следующем update; eye обновляет сам.
    };

    DemodInfoModal.prototype.destroy = function() {
        this.close();
        if (this._eyeView) { this._eyeView.destroy(); this._eyeView = null; }
    };

    if (typeof window !== 'undefined') {
        window.DemodInfoModal = DemodInfoModal;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            DemodInfoModal: DemodInfoModal,
            formatInfoHz: formatInfoHz,
            formatInfoSignedHz: formatInfoSignedHz,
        };
    }
})();
