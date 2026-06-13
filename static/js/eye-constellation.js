// eye-constellation.js — блок диаграмм демодулятора (ADR-004 § 4.7, блок 3 левая колонка).
//
// Mock-визуализация под разные классы модуляций:
//   • Eye diagram        — NRZ/FSK/GMSK/AFSK (post-baseband, до slicer-а).
//   • Constellation      — BPSK/QPSK/PSK (I/Q после когерентного детектора).
//   • Envelope vs time   — CW/OOK (амплитудная огибающая, on/off keying).
//   • Audio spectrum     — FM (1D-спектр демодулированного аудио).
//   • Amplitude histogram — гистограмма baseband-семплов (моды high/low).
//
// Выбор основной диаграммы — автоматически по строке pipeline («Демод.»).
// Пользователь может вручную переключиться между доступными для данной
// категории диаграммами; кнопка «auto» возвращает к выбору по pipeline.
//
// Реальные IQ/baseband — после DEMOD-001 на бэке.

'use strict';

(function() {

    function cssVar(name, fallback) {
        if (typeof window.cssVar === 'function') {
            return window.cssVar(name, fallback);
        }
        return fallback;
    }

    // ── Mode / категории модуляций ──────────────────────────────────────

    const MODE_EYE = 'eye';
    const MODE_CONST = 'constellation';
    const MODE_ENVELOPE = 'envelope';
    const MODE_AUDIO = 'audio';
    const MODE_HISTOGRAM = 'histogram';

    const CAT_NRZ = 'nrz';
    const CAT_PSK = 'psk';
    const CAT_CW = 'cw';
    const CAT_FM = 'fm';
    const CAT_OOK = 'ook';

    // Доступные диаграммы по категории; первая в списке — по умолчанию.
    const CATEGORY_TABS = {};
    CATEGORY_TABS[CAT_NRZ] = [MODE_EYE, MODE_HISTOGRAM];
    CATEGORY_TABS[CAT_PSK] = [MODE_CONST, MODE_EYE];
    CATEGORY_TABS[CAT_CW] = [MODE_ENVELOPE];
    CATEGORY_TABS[CAT_FM] = [MODE_AUDIO];
    CATEGORY_TABS[CAT_OOK] = [MODE_ENVELOPE, MODE_HISTOGRAM];

    const TAB_LABELS = {};
    TAB_LABELS[MODE_EYE] = 'Eye';
    TAB_LABELS[MODE_CONST] = 'Constellation';
    TAB_LABELS[MODE_ENVELOPE] = 'Envelope';
    TAB_LABELS[MODE_AUDIO] = 'Audio';
    TAB_LABELS[MODE_HISTOGRAM] = 'Histogram';

    const TICK_MS = 80;

    /** Резолвер строки модуляции в категорию. */
    function categoryOf(modStr) {
        const m = String(modStr || '').toLowerCase();
        if (!m) { return CAT_NRZ; }
        // Порядок важен: GMSK содержит «sk», поэтому проверяем раньше PSK.
        if (m.indexOf('gmsk') >= 0) { return CAT_NRZ; }
        if (m.indexOf('afsk') >= 0) { return CAT_NRZ; }
        if (m.indexOf('fsk') >= 0) { return CAT_NRZ; }
        if (m.indexOf('qpsk') >= 0 || m.indexOf('bpsk') >= 0 || m.indexOf('psk') >= 0) {
            return CAT_PSK;
        }
        if (m.indexOf('cw') >= 0 || m.indexOf('morse') >= 0) { return CAT_CW; }
        if (m.indexOf('ook') >= 0 || m.indexOf('ask') >= 0) { return CAT_OOK; }
        if (m.indexOf('fm') >= 0) { return CAT_FM; }
        return CAT_NRZ;
    }

    // ── Конструктор ────────────────────────────────────────────────────

    /**
     * EyeConstellationView — canvas-виджет диаграмм демодулятора.
     * @param {Object} opts
     * @param {HTMLCanvasElement} [opts.canvas]   — #manual-eye-canvas
     * @param {HTMLElement}       [opts.tabsContainer] — #manual-eye-tabs (контейнер кнопок)
     * @param {HTMLElement}       [opts.autoEl]  — бейдж «auto»
     */
    function EyeConstellationView(opts) {
        opts = opts || {};
        this._canvas = opts.canvas || document.getElementById('manual-eye-canvas');
        this._tabsContainer = opts.tabsContainer || document.getElementById('manual-eye-tabs');
        this._autoEl = opts.autoEl || document.getElementById('manual-eye-auto');

        this._category = CAT_NRZ;
        this._availableModes = CATEGORY_TABS[CAT_NRZ].slice();
        this._mode = this._availableModes[0];
        this._tabButtons = [];

        this._autoMode = true;
        this._modulationStr = '';
        this._active = false;
        this._timer = null;
        this._phase = 0;

        this._eyeTraces = [];
        this._constPoints = [];
        this._envelopeBuf = [];
        this._envelopeIdx = 0;
        this._audioSpec = null;
        this._histogramBins = null;

        this._renderTabs();
        this._bindAuto();
        this._initResize();
        this._updateAutoLabel();
    }

    // ── Tabs (динамический рендер) ─────────────────────────────────────

    EyeConstellationView.prototype._renderTabs = function() {
        const container = this._tabsContainer;
        if (!container) { return; }

        while (container.firstChild) {
            container.removeChild(container.firstChild);
        }
        this._tabButtons = [];

        const self = this;
        for (let i = 0; i < this._availableModes.length; i++) {
            const modeId = this._availableModes[i];
            const btn = document.createElement('button');
            btn.type = 'button';
            btn.textContent = TAB_LABELS[modeId] || modeId;
            btn.dataset.mode = modeId;
            btn.classList.add('ml-eye__tab');
            if (modeId === this._mode) {
                btn.classList.add('ml-eye__tab--on');
            }
            (function(mode, button) {
                button.addEventListener('click', function() {
                    self._autoMode = false;
                    self.setMode(mode);
                    self._updateAutoLabel();
                });
            })(modeId, btn);
            container.appendChild(btn);
            this._tabButtons.push(btn);
        }
    };

    EyeConstellationView.prototype._highlightActiveTab = function() {
        for (let i = 0; i < this._tabButtons.length; i++) {
            const btn = this._tabButtons[i];
            const on = btn.dataset && btn.dataset.mode === this._mode;
            btn.classList.toggle('ml-eye__tab--on', on);
        }
    };

    EyeConstellationView.prototype._bindAuto = function() {
        if (!this._autoEl) { return; }
        const self = this;
        this._autoEl.addEventListener('click', function() {
            self._autoMode = true;
            self._updateAutoLabel();
            if (typeof self._onAutoRestore === 'function') {
                self._onAutoRestore();
            } else if (self._modulationStr) {
                self.setModulation(self._modulationStr);
            } else {
                self.setMode(self._availableModes[0]);
            }
        });
    };

    EyeConstellationView.prototype.setMode = function(mode) {
        if (this._availableModes.indexOf(mode) < 0) {
            return;
        }
        // Любая смена mode наружу — ручная (auto сбрасывается там, где это уместно).
        if (this._mode !== mode) {
            this._autoMode = false;
        }
        this._mode = mode;
        this._highlightActiveTab();
        this._resetBuffers();
        this._updateAutoLabel();
        if (this._active) { this._draw(); }
    };

    EyeConstellationView.prototype.getMode = function() {
        return this._mode;
    };

    EyeConstellationView.prototype.getCategory = function() {
        return this._category;
    };

    EyeConstellationView.prototype.getAvailableModes = function() {
        return this._availableModes.slice();
    };

    /** Авто-выбор категории/диаграмм по строке pipeline («Демод.»). */
    EyeConstellationView.prototype.setModulation = function(modStr) {
        this._modulationStr = modStr || '';
        if (!modStr) { return; }

        const cat = categoryOf(modStr);
        const modes = CATEGORY_TABS[cat] ? CATEGORY_TABS[cat].slice() : [MODE_EYE];

        const categoryChanged = cat !== this._category;
        this._category = cat;
        this._availableModes = modes;

        // В auto-режиме всегда возвращаемся к дефолту категории.
        // В ручном режиме сохраняем текущий mode, если он доступен.
        let nextMode;
        if (this._autoMode) {
            nextMode = modes[0];
        } else if (modes.indexOf(this._mode) >= 0) {
            nextMode = this._mode;
        } else {
            nextMode = modes[0];
        }

        if (categoryChanged || this._tabButtons.length !== modes.length) {
            this._mode = nextMode;
            this._renderTabs();
        } else {
            this._mode = nextMode;
            this._highlightActiveTab();
        }

        this._resetBuffers();
        if (this._active) { this._draw(); }
    };

    EyeConstellationView.prototype.isAutoMode = function() {
        return this._autoMode;
    };

    EyeConstellationView.prototype.setAutoMode = function(on) {
        this._autoMode = Boolean(on);
        this._updateAutoLabel();
    };

    EyeConstellationView.prototype._updateAutoLabel = function() {
        if (this._autoEl) {
            this._autoEl.classList.toggle('ml-eye__auto--on', this._autoMode);
            this._autoEl.setAttribute('aria-pressed', this._autoMode ? 'true' : 'false');
        }
    };

    EyeConstellationView.prototype._resetBuffers = function() {
        this._eyeTraces = [];
        this._constPoints = [];
        this._envelopeBuf = [];
        this._envelopeIdx = 0;
        this._audioSpec = null;
        this._histogramBins = null;
    };

    // ── Resize ─────────────────────────────────────────────────────────

    EyeConstellationView.prototype._initResize = function() {
        if (!this._canvas || typeof ResizeObserver === 'undefined') { return; }
        const self = this;
        this._ro = new ResizeObserver(function() { self._resize(); });
        this._ro.observe(this._canvas.parentElement || this._canvas);
    };

    EyeConstellationView.prototype._resize = function() {
        if (!this._canvas || !this._canvas.parentElement) { return; }
        const w = this._canvas.parentElement.clientWidth;
        const h = this._canvas.parentElement.clientHeight;
        if (w > 0 && h > 0 && (this._canvas.width !== w || this._canvas.height !== h)) {
            this._canvas.width = w;
            this._canvas.height = h;
            this._resetBuffers();
            if (this._active) { this._draw(); }
        }
    };

    // ── Общие цвета ────────────────────────────────────────────────────

    function plotBg() {
        return cssVar('--decode-instrument-bg', cssVar('--spectrum-plot-bg', '#2a3440'));
    }

    function axisColor() {
        return cssVar('--decode-axis-color', 'rgba(255,255,255,0.45)');
    }

    function gridColor() {
        return cssVar('--decode-grid-color', 'rgba(255,255,255,0.22)');
    }

    function traceColor() {
        return cssVar('--spectrum-trace', '#5ee878');
    }

    function accentColor() {
        return cssVar('--accent-primary', '#00bcd4');
    }

    function successColor() {
        return cssVar('--accent-success', '#00d4aa');
    }

    function mutedColor() {
        return cssVar('--text-muted', '#c8d0d8');
    }

    // ── Eye diagram ────────────────────────────────────────────────────

    EyeConstellationView.prototype._drawEye = function(ctx, w, h) {
        ctx.fillStyle = plotBg();
        ctx.fillRect(0, 0, w, h);

        ctx.strokeStyle = axisColor();
        ctx.lineWidth = 1.5;
        ctx.beginPath();
        ctx.moveTo(0, h / 2);
        ctx.lineTo(w, h / 2);
        ctx.moveTo(w / 2, 0);
        ctx.lineTo(w / 2, h);
        ctx.stroke();

        ctx.strokeStyle = gridColor();
        ctx.lineWidth = 1;
        ctx.setLineDash([3, 4]);
        ctx.beginPath();
        ctx.moveTo(0, h * 0.2);
        ctx.lineTo(w, h * 0.2);
        ctx.moveTo(0, h * 0.8);
        ctx.lineTo(w, h * 0.8);
        ctx.moveTo(w * 0.25, 0);
        ctx.lineTo(w * 0.25, h);
        ctx.moveTo(w * 0.75, 0);
        ctx.lineTo(w * 0.75, h);
        ctx.stroke();
        ctx.setLineDash([]);

        const MAX_TRACES = 48;
        if (this._eyeTraces.length > MAX_TRACES) {
            this._eyeTraces = this._eyeTraces.slice(-MAX_TRACES);
        }

        const color = traceColor();
        for (let t = 0; t < this._eyeTraces.length; t++) {
            const trace = this._eyeTraces[t];
            const age = (this._eyeTraces.length - t) / this._eyeTraces.length;
            ctx.globalAlpha = 0.12 + 0.55 * (1 - age);
            ctx.strokeStyle = color;
            ctx.lineWidth = 1;
            ctx.beginPath();
            for (let i = 0; i < trace.length; i++) {
                const x = (i / (trace.length - 1)) * w;
                const y = h / 2 - trace[i] * (h * 0.38);
                if (i === 0) { ctx.moveTo(x, y); } else { ctx.lineTo(x, y); }
            }
            ctx.stroke();
        }
        ctx.globalAlpha = 1;
    };

    function eyeLevelStep(from, to, t, edgeWidth) {
        if (t <= 0) { return from; }
        if (t >= edgeWidth) { return to; }
        const x = 0.5 - 0.5 * Math.cos(Math.PI * t / edgeWidth);
        return from + (to - from) * x;
    }

    EyeConstellationView.prototype._generateEyeTrace = function() {
        const pts = 128;
        const trace = new Array(pts);
        const sPrev = Math.random() > 0.5 ? 1 : -1;
        const sMid = Math.random() > 0.5 ? 1 : -1;
        const sNext = Math.random() > 0.5 ? 1 : -1;
        const noise = 0.05;
        const edge = 0.18;

        for (let i = 0; i < pts; i++) {
            const ui = (i / (pts - 1)) * 2;
            let y;
            if (ui < 1) {
                if (ui < edge) {
                    y = eyeLevelStep(sPrev, sMid, ui, edge);
                } else {
                    y = sMid;
                }
            } else {
                const ui2 = ui - 1;
                if (ui2 < edge) {
                    y = eyeLevelStep(sMid, sNext, ui2, edge);
                } else {
                    y = sNext;
                }
            }
            trace[i] = y + (Math.random() - 0.5) * noise;
        }
        return trace;
    };

    // ── Constellation ──────────────────────────────────────────────────

    EyeConstellationView.prototype._drawConstellation = function(ctx, w, h) {
        ctx.fillStyle = plotBg();
        ctx.fillRect(0, 0, w, h);

        const ax = axisColor();
        const grid = gridColor();

        const cx = w / 2;
        const overlayPad = 28;
        const cy = h / 2 + Math.min(overlayPad * 0.35, h * 0.04);
        const r = Math.min(w, h - overlayPad) * 0.44;

        ctx.strokeStyle = ax;
        ctx.lineWidth = 1.5;
        ctx.beginPath();
        ctx.moveTo(0, cy);
        ctx.lineTo(w, cy);
        ctx.moveTo(cx, 0);
        ctx.lineTo(cx, h);
        ctx.stroke();

        ctx.strokeStyle = grid;
        ctx.lineWidth = 1.25;
        ctx.setLineDash([4, 4]);
        ctx.beginPath();
        ctx.arc(cx, cy, r, 0, Math.PI * 2);
        ctx.stroke();
        ctx.setLineDash([]);

        ctx.strokeStyle = ax;
        ctx.lineWidth = 1;
        const tick = 5;
        ctx.beginPath();
        ctx.moveTo(cx + r, cy - tick); ctx.lineTo(cx + r, cy + tick);
        ctx.moveTo(cx - r, cy - tick); ctx.lineTo(cx - r, cy + tick);
        ctx.moveTo(cx - tick, cy - r); ctx.lineTo(cx + tick, cy - r);
        ctx.moveTo(cx - tick, cy + r); ctx.lineTo(cx + tick, cy + r);
        ctx.stroke();

        const MAX_PTS = 300;
        if (this._constPoints.length > MAX_PTS) {
            this._constPoints = this._constPoints.slice(-MAX_PTS);
        }

        const ptColor = accentColor();
        for (let i = 0; i < this._constPoints.length; i++) {
            const p = this._constPoints[i];
            const age = (this._constPoints.length - i) / this._constPoints.length;
            ctx.globalAlpha = 0.15 + 0.65 * (1 - age);
            ctx.fillStyle = ptColor;
            ctx.beginPath();
            ctx.arc(cx + p[0] * r, cy - p[1] * r, 1.5, 0, Math.PI * 2);
            ctx.fill();
        }
        ctx.globalAlpha = 1;

        const idealColor = successColor();
        ctx.fillStyle = idealColor;
        ctx.globalAlpha = 0.45;
        const ideals = this._constellationIdeals();
        for (let k = 0; k < ideals.length; k++) {
            ctx.beginPath();
            ctx.arc(cx + ideals[k][0] * r, cy - ideals[k][1] * r, 3, 0, Math.PI * 2);
            ctx.fill();
        }
        ctx.globalAlpha = 1;

        ctx.fillStyle = mutedColor();
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';
        ctx.fillText('I', w - 12, cy - 4);
        ctx.fillText('Q', cx + 4, 12);
    };

    EyeConstellationView.prototype._constellationIdeals = function() {
        const m = (this._modulationStr || '').toLowerCase();
        if (m.indexOf('qpsk') >= 0) {
            return [[1, 0], [-1, 0], [0, 1], [0, -1]];
        }
        return [[1, 0], [-1, 0]];
    };

    EyeConstellationView.prototype._generateConstPoints = function() {
        const noise = 0.26;
        const ideals = this._constellationIdeals();
        const batch = [];
        const count = 6 + Math.floor(Math.random() * 8);
        for (let i = 0; i < count; i++) {
            const ideal = ideals[Math.floor(Math.random() * ideals.length)];
            batch.push([
                ideal[0] + (Math.random() - 0.5) * noise,
                ideal[1] + (Math.random() - 0.5) * noise,
            ]);
        }
        return batch;
    };

    // ── Envelope (CW / OOK) ────────────────────────────────────────────

    /** Морзянка: точка — 1 ui, тире — 3 ui, разделитель внутри буквы — 1 ui, между буквами — 3 ui. */
    const MORSE_ALPHABET = {
        A: '.-', B: '-...', C: '-.-.', D: '-..', E: '.', F: '..-.', G: '--.', H: '....',
        I: '..', J: '.---', K: '-.-', L: '.-..', M: '--', N: '-.', O: '---', P: '.--.',
        Q: '--.-', R: '.-.', S: '...', T: '-', U: '..-', V: '...-', W: '.--', X: '-..-',
        Y: '-.--', Z: '--..'
    };

    function morseSequence(text) {
        const seq = [];
        for (let i = 0; i < text.length; i++) {
            const ch = text.charAt(i).toUpperCase();
            if (ch === ' ') {
                seq.push({ on: false, ui: 7 });
                continue;
            }
            const code = MORSE_ALPHABET[ch];
            if (!code) { continue; }
            for (let j = 0; j < code.length; j++) {
                seq.push({ on: true, ui: code.charAt(j) === '-' ? 3 : 1 });
                if (j < code.length - 1) {
                    seq.push({ on: false, ui: 1 });
                }
            }
            if (i < text.length - 1) {
                seq.push({ on: false, ui: 3 });
            }
        }
        return seq;
    }

    EyeConstellationView.prototype._drawEnvelope = function(ctx, w, h) {
        ctx.fillStyle = plotBg();
        ctx.fillRect(0, 0, w, h);

        const ax = axisColor();
        const grid = gridColor();

        ctx.strokeStyle = grid;
        ctx.lineWidth = 1;
        ctx.setLineDash([3, 4]);
        ctx.beginPath();
        ctx.moveTo(0, h * 0.18);
        ctx.lineTo(w, h * 0.18);
        ctx.moveTo(0, h * 0.82);
        ctx.lineTo(w, h * 0.82);
        ctx.stroke();
        ctx.setLineDash([]);

        ctx.strokeStyle = ax;
        ctx.lineWidth = 1.25;
        ctx.beginPath();
        ctx.moveTo(0, h * 0.82);
        ctx.lineTo(w, h * 0.82);
        ctx.stroke();

        const buf = this._envelopeBuf;
        if (!buf || buf.length === 0) { return; }

        ctx.strokeStyle = traceColor();
        ctx.fillStyle = traceColor();
        ctx.globalAlpha = 0.18;

        const baseY = h * 0.82;
        const topY = h * 0.18;
        const span = baseY - topY;

        ctx.beginPath();
        ctx.moveTo(0, baseY);
        for (let i = 0; i < buf.length; i++) {
            const x = (i / (buf.length - 1)) * w;
            const y = baseY - buf[i] * span;
            ctx.lineTo(x, y);
        }
        ctx.lineTo(w, baseY);
        ctx.closePath();
        ctx.fill();

        ctx.globalAlpha = 1;
        ctx.beginPath();
        for (let k = 0; k < buf.length; k++) {
            const xx = (k / (buf.length - 1)) * w;
            const yy = baseY - buf[k] * span;
            if (k === 0) { ctx.moveTo(xx, yy); } else { ctx.lineTo(xx, yy); }
        }
        ctx.stroke();

        ctx.fillStyle = mutedColor();
        ctx.font = '10px monospace';
        ctx.textAlign = 'left';
        ctx.fillText(this._category === CAT_CW ? 'CW key' : 'OOK', 6, 12);
    };

    /** Поток сэмплов огибающей: для CW — морзянка, для OOK — случайные импульсы. */
    EyeConstellationView.prototype._generateEnvelopeChunk = function() {
        const samplesPerUI = 16;
        const chunkLen = 96;
        const noise = 0.04;
        const edge = 0.20;

        if (this._category === CAT_CW) {
            if (!this._morseSeq || this._morseIdx >= this._morseSeq.length) {
                this._morseSeq = morseSequence('CQ CQ DE R3M ');
                this._morseIdx = 0;
                this._morseSubIdx = 0;
            }
        } else {
            if (!this._ookSeq || this._ookIdx >= this._ookSeq.length) {
                this._ookSeq = this._randomOokSeq();
                this._ookIdx = 0;
                this._ookSubIdx = 0;
            }
        }

        const chunk = new Array(chunkLen);
        let produced = 0;
        while (produced < chunkLen) {
            let seq, idxRef;
            if (this._category === CAT_CW) {
                seq = this._morseSeq;
                idxRef = '_morseIdx';
            } else {
                seq = this._ookSeq;
                idxRef = '_ookIdx';
            }
            if (this[idxRef] >= seq.length) {
                if (this._category === CAT_CW) {
                    this._morseSeq = morseSequence('CQ CQ DE R3M ');
                    this._morseIdx = 0;
                    this._morseSubIdx = 0;
                } else {
                    this._ookSeq = this._randomOokSeq();
                    this._ookIdx = 0;
                    this._ookSubIdx = 0;
                }
                continue;
            }
            const element = seq[this[idxRef]];
            const subIdxRef = (this._category === CAT_CW) ? '_morseSubIdx' : '_ookSubIdx';
            const totalSubSamples = element.ui * samplesPerUI;
            const target = element.on ? 1 : 0;
            const prevTarget = this._envLastTarget != null ? this._envLastTarget : 0;
            while (this[subIdxRef] < totalSubSamples && produced < chunkLen) {
                const localT = this[subIdxRef] / samplesPerUI;
                let y;
                if (localT < edge) {
                    y = eyeLevelStep(prevTarget, target, localT, edge);
                } else {
                    y = target;
                }
                chunk[produced] = Math.max(0, Math.min(1, y + (Math.random() - 0.5) * noise));
                produced++;
                this[subIdxRef]++;
            }
            if (this[subIdxRef] >= totalSubSamples) {
                this[idxRef]++;
                this[subIdxRef] = 0;
                this._envLastTarget = target;
            }
        }
        return chunk;
    };

    /** Случайная OOK-последовательность: 6–14 импульсов с шириной 1–4 ui. */
    EyeConstellationView.prototype._randomOokSeq = function() {
        const n = 6 + Math.floor(Math.random() * 9);
        const seq = [];
        for (let i = 0; i < n; i++) {
            seq.push({ on: true, ui: 1 + Math.floor(Math.random() * 4) });
            seq.push({ on: false, ui: 1 + Math.floor(Math.random() * 3) });
        }
        return seq;
    };

    // ── Audio spectrum (FM) ────────────────────────────────────────────

    EyeConstellationView.prototype._drawAudioSpectrum = function(ctx, w, h) {
        ctx.fillStyle = plotBg();
        ctx.fillRect(0, 0, w, h);

        const ax = axisColor();
        const grid = gridColor();

        ctx.strokeStyle = grid;
        ctx.lineWidth = 1;
        ctx.setLineDash([3, 4]);
        ctx.beginPath();
        for (let g = 1; g < 5; g++) {
            const y = (h * g) / 5;
            ctx.moveTo(0, y);
            ctx.lineTo(w, y);
        }
        ctx.stroke();
        ctx.setLineDash([]);

        ctx.strokeStyle = ax;
        ctx.lineWidth = 1.25;
        ctx.beginPath();
        ctx.moveTo(0, h * 0.92);
        ctx.lineTo(w, h * 0.92);
        ctx.stroke();

        const spec = this._audioSpec;
        if (!spec || spec.length === 0) { return; }

        const baseY = h * 0.92;
        const topY = h * 0.10;
        const span = baseY - topY;

        ctx.fillStyle = accentColor();
        ctx.globalAlpha = 0.22;
        ctx.beginPath();
        ctx.moveTo(0, baseY);
        for (let i = 0; i < spec.length; i++) {
            const x = (i / (spec.length - 1)) * w;
            const yy = baseY - spec[i] * span;
            ctx.lineTo(x, yy);
        }
        ctx.lineTo(w, baseY);
        ctx.closePath();
        ctx.fill();

        ctx.globalAlpha = 1;
        ctx.strokeStyle = accentColor();
        ctx.lineWidth = 1.25;
        ctx.beginPath();
        for (let k = 0; k < spec.length; k++) {
            const xx = (k / (spec.length - 1)) * w;
            const yyy = baseY - spec[k] * span;
            if (k === 0) { ctx.moveTo(xx, yyy); } else { ctx.lineTo(xx, yyy); }
        }
        ctx.stroke();

        ctx.fillStyle = mutedColor();
        ctx.font = '10px monospace';
        ctx.textAlign = 'left';
        ctx.fillText('Audio FFT', 6, 12);
        ctx.textAlign = 'right';
        ctx.fillText('0     →     Гц     →     fs/2', w - 6, h - 6);
    };

    /** Обновление аудио-спектра: шум + один-два узких пика (тоны). */
    EyeConstellationView.prototype._updateAudioSpec = function() {
        const bins = 192;
        if (!this._audioSpec || this._audioSpec.length !== bins) {
            this._audioSpec = new Array(bins);
            for (let i = 0; i < bins; i++) { this._audioSpec[i] = 0.1; }
        }
        const spec = this._audioSpec;

        const tonePos = Math.floor(bins * (0.18 + 0.04 * Math.sin(this._phase * 0.05)));
        const sub = Math.floor(bins * (0.36 + 0.02 * Math.cos(this._phase * 0.03)));

        for (let k = 0; k < bins; k++) {
            const prev = spec[k];
            const noise = 0.05 + (Math.random() * 0.10);
            let peak = 0;
            const d1 = k - tonePos;
            peak += 0.85 * Math.exp(-(d1 * d1) / 8);
            const d2 = k - sub;
            peak += 0.35 * Math.exp(-(d2 * d2) / 14);
            const target = Math.min(1, noise + peak);
            spec[k] = prev * 0.5 + target * 0.5;
        }
    };

    // ── Histogram (amplitude) ──────────────────────────────────────────

    EyeConstellationView.prototype._drawHistogram = function(ctx, w, h) {
        ctx.fillStyle = plotBg();
        ctx.fillRect(0, 0, w, h);

        ctx.strokeStyle = axisColor();
        ctx.lineWidth = 1.25;
        ctx.beginPath();
        ctx.moveTo(0, h * 0.88);
        ctx.lineTo(w, h * 0.88);
        ctx.stroke();

        const bins = this._histogramBins;
        if (!bins || bins.length === 0) { return; }

        let max = 1;
        for (let i = 0; i < bins.length; i++) {
            if (bins[i] > max) { max = bins[i]; }
        }

        const baseY = h * 0.88;
        const topPad = h * 0.10;
        const span = baseY - topPad;
        const barW = w / bins.length;

        ctx.fillStyle = traceColor();
        ctx.globalAlpha = 0.85;
        for (let b = 0; b < bins.length; b++) {
            const bh = (bins[b] / max) * span;
            ctx.fillRect(b * barW, baseY - bh, Math.max(1, barW - 1), bh);
        }
        ctx.globalAlpha = 1;

        ctx.fillStyle = mutedColor();
        ctx.font = '10px monospace';
        ctx.textAlign = 'left';
        ctx.fillText('Amplitude histogram', 6, 12);
        ctx.textAlign = 'center';
        ctx.fillText(this._category === CAT_OOK ? '0' : '−1', 4, h - 4);
        ctx.fillText('0', w / 2, h - 4);
        ctx.fillText('+1', w - 8, h - 4);
    };

    /** Накопление гистограммы по сэмплам baseband (Eye trace или envelope). */
    EyeConstellationView.prototype._updateHistogramFromTrace = function(samples) {
        const binsN = 48;
        if (!this._histogramBins || this._histogramBins.length !== binsN) {
            this._histogramBins = new Array(binsN);
            for (let i = 0; i < binsN; i++) { this._histogramBins[i] = 0; }
        }
        let lo, hi;
        if (this._category === CAT_OOK) {
            lo = 0; hi = 1;
        } else {
            lo = -1.4; hi = 1.4;
        }
        const span = hi - lo;
        // Лёгкое затухание, чтобы гистограмма «дышала», а не накапливалась бесконечно.
        for (let k = 0; k < binsN; k++) {
            this._histogramBins[k] *= 0.92;
        }
        for (let s = 0; s < samples.length; s++) {
            const v = samples[s];
            const t = (v - lo) / span;
            if (t < 0 || t >= 1) { continue; }
            let bin = Math.floor(t * binsN);
            if (bin >= binsN) { bin = binsN - 1; }
            this._histogramBins[bin] += 1;
        }
    };

    // ── Цикл рисования / тик ───────────────────────────────────────────

    EyeConstellationView.prototype._draw = function() {
        if (!this._canvas) { return; }
        const ctx = this._canvas.getContext('2d');
        if (!ctx) { return; }
        const w = this._canvas.width;
        const h = this._canvas.height;
        if (w <= 0 || h <= 0) { return; }

        switch (this._mode) {
            case MODE_EYE: this._drawEye(ctx, w, h); break;
            case MODE_CONST: this._drawConstellation(ctx, w, h); break;
            case MODE_ENVELOPE: this._drawEnvelope(ctx, w, h); break;
            case MODE_AUDIO: this._drawAudioSpectrum(ctx, w, h); break;
            case MODE_HISTOGRAM: this._drawHistogram(ctx, w, h); break;
            default: this._drawEye(ctx, w, h);
        }
    };

    EyeConstellationView.prototype._tick = function() {
        if (!this._active) { return; }

        switch (this._mode) {
            case MODE_EYE: {
                this._eyeTraces.push(this._generateEyeTrace());
                break;
            }
            case MODE_CONST: {
                const pts = this._generateConstPoints();
                for (let i = 0; i < pts.length; i++) {
                    this._constPoints.push(pts[i]);
                }
                break;
            }
            case MODE_ENVELOPE: {
                const chunk = this._generateEnvelopeChunk();
                if (!this._envelopeBuf || this._envelopeBuf.length !== chunk.length * 4) {
                    this._envelopeBuf = new Array(chunk.length * 4);
                    for (let z = 0; z < this._envelopeBuf.length; z++) { this._envelopeBuf[z] = 0; }
                    this._envelopeIdx = 0;
                }
                for (let c = 0; c < chunk.length; c++) {
                    this._envelopeBuf[this._envelopeIdx] = chunk[c];
                    this._envelopeIdx = (this._envelopeIdx + 1) % this._envelopeBuf.length;
                }
                // Развернём буфер для вывода так, чтобы новые сэмплы были справа.
                this._envelopeBuf = this._envelopeBuf
                    .slice(this._envelopeIdx)
                    .concat(this._envelopeBuf.slice(0, this._envelopeIdx));
                this._envelopeIdx = 0;
                break;
            }
            case MODE_AUDIO: {
                this._updateAudioSpec();
                break;
            }
            case MODE_HISTOGRAM: {
                // Для NRZ — сэмплируем eye trace; для OOK — envelope.
                let samples;
                if (this._category === CAT_OOK) {
                    samples = this._generateEnvelopeChunk();
                } else {
                    samples = this._generateEyeTrace();
                }
                this._updateHistogramFromTrace(samples);
                break;
            }
            default: break;
        }

        this._draw();
        this._phase++;
    };

    // ── Жизненный цикл ────────────────────────────────────────────────

    EyeConstellationView.prototype.activate = function() {
        if (this._active) { return; }
        this._active = true;
        this._resize();
        const self = this;
        this._timer = setInterval(function() { self._tick(); }, TICK_MS);
    };

    EyeConstellationView.prototype.deactivate = function() {
        if (!this._active) { return; }
        this._active = false;
        if (this._timer) {
            clearInterval(this._timer);
            this._timer = null;
        }
    };

    EyeConstellationView.prototype.isActive = function() {
        return this._active;
    };

    EyeConstellationView.prototype.refreshAfterThemeChange = function() {
        if (this._active) { this._draw(); }
    };

    EyeConstellationView.prototype.destroy = function() {
        this.deactivate();
        if (this._ro) {
            this._ro.disconnect();
            this._ro = null;
        }
    };

    // ── Экспорт ───────────────────────────────────────────────────────

    if (typeof window !== 'undefined') {
        window.EyeConstellationView = EyeConstellationView;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            EyeConstellationView: EyeConstellationView,
            categoryOf: categoryOf,
        };
    }
})();
