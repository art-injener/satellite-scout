// pass-timeline.js — график пролётов под панорамой Ручного режима.
//
// Ось X — время (окно от «сейчас» вперёд), ось Y — угол места 0…90°.
// Каждый пролёт — полупрозрачная горка: El(t) = maxEl · sin(π · u),
// u ∈ [0, 1] от AOS до LOS. Горки перекрываются, как у эталона
// «Passes for the next N hours».
//
// Данные пока моковые. Живое расписание подключается через setPasses().

'use strict';

(function() {
    const PAD_L = 24; // ось угла места
    const PAD_R = 4;
    const PAD_T = 12; // подпись NOW над линией
    const PAD_B = 12; // шкала времени
    const DEFAULT_WINDOW_MIN = 240; // 4 часа, как на эталонном графике
    const PAST_MIN = 3;
    const TICK_MIN = 20;
    const EL_TICKS = [0, 30, 60, 90];
    const SAMPLES = 28;
    const LABEL_MAX_PX = 90;
    const FALLBACK_H = 88;

    function cssVar(name, fallback) {
        if (typeof window !== 'undefined' && typeof window.cssVar === 'function') {
            return window.cssVar(name, fallback);
        }
        return fallback || '';
    }

    /** Демонстрационное расписание: перекрывающиеся пролёты на 4 часа. */
    function mockPasses() {
        const now = Date.now();
        const minute = 60000;
        return [
            { noradId: 25544, name: 'ISS', aosMs: now - 2 * minute, losMs: now + 8 * minute, maxEl: 64 },
            { noradId: 27844, name: 'CO-66', aosMs: now - minute, losMs: now + 7 * minute, maxEl: 22 },
            { noradId: 46495, name: 'SALSAT', aosMs: now + 18 * minute, losMs: now + 32 * minute, maxEl: 41 },
            { noradId: 7530, name: 'OSCAR 7', aosMs: now + 28 * minute, losMs: now + 48 * minute, maxEl: 55 },
            { noradId: 53109, name: 'GREENCUBE', aosMs: now + 52 * minute, losMs: now + 68 * minute, maxEl: 38 },
            { noradId: 43017, name: 'NOAA 18', aosMs: now + 70 * minute, losMs: now + 88 * minute, maxEl: 19 },
            { noradId: 24278, name: 'FO-29', aosMs: now + 95 * minute, losMs: now + 112 * minute, maxEl: 72 },
            { noradId: 39417, name: 'GOMX-1', aosMs: now + 130 * minute, losMs: now + 148 * minute, maxEl: 81 },
            { noradId: 40908, name: 'AO-91', aosMs: now + 155 * minute, losMs: now + 168 * minute, maxEl: 27 },
            { noradId: 22825, name: 'IO-26', aosMs: now + 175 * minute, losMs: now + 192 * minute, maxEl: 46 },
            { noradId: 40032, name: 'QB50P1', aosMs: now + 200 * minute, losMs: now + 214 * minute, maxEl: 33 },
        ];
    }

    /**
     * Угол места в момент tMs. Синус от AOS до LOS — типичная форма LEO-пролёта.
     * @param {{aosMs:number, losMs:number, maxEl:number}} pass
     * @param {number} tMs
     * @returns {number}
     */
    function elevationAt(pass, tMs) {
        if (!pass || tMs <= pass.aosMs || tMs >= pass.losMs) { return 0; }
        const span = pass.losMs - pass.aosMs;
        if (span <= 0) { return 0; }
        const u = (tMs - pass.aosMs) / span;
        return pass.maxEl * Math.sin(Math.PI * u);
    }

    function hhmm(ms) {
        const d = new Date(ms);
        const h = d.getHours();
        const m = d.getMinutes();
        return (h < 10 ? '0' : '') + h + ':' + (m < 10 ? '0' : '') + m;
    }

    /** #rrggbb → rgba() с заданной прозрачностью. */
    function hexAlpha(hex, alpha) {
        const m = /^#?([0-9a-f]{6})$/i.exec(String(hex || ''));
        if (!m) { return hex; }
        const n = parseInt(m[1], 16);
        const r = (n >> 16) & 255;
        const g = (n >> 8) & 255;
        const b = n & 255;
        return 'rgba(' + r + ',' + g + ',' + b + ',' + alpha + ')';
    }

    /**
     * @param {HTMLCanvasElement} canvas
     * @param {{windowMin?: number, passes?: Array}} [opts]
     */
    function PassTimelineView(canvas, opts) {
        opts = opts || {};
        this._canvas = canvas;
        this._ctx = canvas ? canvas.getContext('2d') : null;
        this._windowMin = opts.windowMin || DEFAULT_WINDOW_MIN;
        this._passes = opts.passes || mockPasses();
        this._trackingId = null;
    }

    PassTimelineView.prototype._resize = function() {
        if (!this._canvas) { return; }
        const rect = this._canvas.getBoundingClientRect();
        const w = Math.floor(rect.width) || 400;
        const h = Math.floor(rect.height) || FALLBACK_H;
        if (w < 2 || h < 2) { return; }
        if (this._canvas.width !== w || this._canvas.height !== h) {
            this._canvas.width = w;
            this._canvas.height = h;
        }
    };

    /** @param {Array<{noradId:number,name:string,aosMs:number,losMs:number,maxEl:number}>} list */
    PassTimelineView.prototype.setPasses = function(list) {
        if (!Array.isArray(list)) { return; }
        this._passes = list;
        this.draw();
    };

    /** Подсветка горки КА под наблюдением. @param {number|null} noradId */
    PassTimelineView.prototype.setTrackingId = function(noradId) {
        this._trackingId = typeof noradId === 'number' ? noradId : null;
        this.draw();
    };

    PassTimelineView.prototype.draw = function() {
        this._resize();
        const ctx = this._ctx;
        if (!ctx || !this._canvas) { return; }
        const W = this._canvas.width;
        const H = this._canvas.height;
        if (W < 2 || H < 2) { return; }

        ctx.clearRect(0, 0, W, H);
        ctx.fillStyle = cssVar('--spectrum-plot-bg', '#2a3440');
        ctx.fillRect(0, 0, W, H);

        const now = Date.now();
        const t0 = now - PAST_MIN * 60000;
        const t1 = now + this._windowMin * 60000;
        const plotX = PAD_L;
        const plotY = PAD_T;
        const plotW = W - PAD_L - PAD_R;
        const plotH = H - PAD_T - PAD_B;
        if (plotW < 40 || plotH < 24) { return; }

        const xOf = (t) => plotX + (t - t0) / (t1 - t0) * plotW;
        const yOf = (el) => plotY + (1 - Math.max(0, Math.min(90, el)) / 90) * plotH;

        this._drawBands(ctx, plotX, plotY, plotW, plotH, yOf);
        this._drawGrid(ctx, t0, t1, xOf, yOf, plotX, plotY, plotW, plotH);
        this._drawHills(ctx, t0, t1, xOf, yOf, plotX, plotW);
        this._drawNow(ctx, xOf(now), plotY, plotH);
        this._drawAxes(ctx, t0, t1, xOf, yOf, plotX, plotY, plotW, plotH);
    };

    /** Горизонтальные зоны угла места: ниже 15° — темнее. */
    PassTimelineView.prototype._drawBands = function(ctx, plotX, plotY, plotW, _plotH, yOf) {
        const y15 = yOf(15);
        const y0 = yOf(0);
        ctx.fillStyle = hexAlpha(cssVar('--accent-success', '#00d4aa'), 0.06);
        ctx.fillRect(plotX, plotY, plotW, y15 - plotY);
        ctx.fillStyle = hexAlpha(cssVar('--accent-success', '#00d4aa'), 0.12);
        ctx.fillRect(plotX, y15, plotW, y0 - y15);
    };

    PassTimelineView.prototype._drawGrid = function(ctx, t0, t1, xOf, yOf, plotX, plotY, plotW, plotH) {
        const grid = cssVar('--spectrum-plot-grid', 'rgba(255,255,255,0.12)');
        ctx.strokeStyle = grid;
        ctx.lineWidth = 1;

        for (let i = 0; i < EL_TICKS.length; i++) {
            const y = Math.round(yOf(EL_TICKS[i])) + 0.5;
            ctx.beginPath();
            ctx.moveTo(plotX, y);
            ctx.lineTo(plotX + plotW, y);
            ctx.stroke();
        }

        const tickMs = TICK_MIN * 60000;
        let t = Math.ceil(t0 / tickMs) * tickMs;
        for (; t <= t1; t += tickMs) {
            const x = Math.round(xOf(t)) + 0.5;
            ctx.beginPath();
            ctx.moveTo(x, plotY);
            ctx.lineTo(x, plotY + plotH);
            ctx.stroke();
        }
    };

    PassTimelineView.prototype._visiblePasses = function(t0, t1) {
        const out = [];
        for (let i = 0; i < this._passes.length; i++) {
            const p = this._passes[i];
            if (p.losMs <= t0 || p.aosMs >= t1) { continue; }
            out.push(p);
        }
        out.sort(function(a, b) { return a.maxEl - b.maxEl; });
        return out;
    };

    PassTimelineView.prototype._drawHills = function(ctx, t0, t1, xOf, yOf, plotX, plotW) {
        const hill = cssVar('--accent-success', '#00d4aa');
        const tracked = cssVar('--accent-primary', '#33b1ff');
        const plotRight = plotX + plotW;
        const y0 = yOf(0);
        const visible = this._visiblePasses(t0, t1);

        for (let i = 0; i < visible.length; i++) {
            const p = visible[i];
            const isTrack = this._trackingId != null && p.noradId === this._trackingId;
            const stroke = isTrack ? tracked : hill;
            const tStart = Math.max(p.aosMs, t0);
            const tEnd = Math.min(p.losMs, t1);
            if (tEnd <= tStart) { continue; }

            ctx.beginPath();
            ctx.moveTo(Math.max(plotX, xOf(tStart)), y0);
            const step = (tEnd - tStart) / SAMPLES;
            for (let s = 0; s <= SAMPLES; s++) {
                const t = tStart + s * step;
                const x = Math.max(plotX, Math.min(plotRight, xOf(t)));
                ctx.lineTo(x, yOf(elevationAt(p, t)));
            }
            ctx.lineTo(Math.min(plotRight, xOf(tEnd)), y0);
            ctx.closePath();
            ctx.fillStyle = hexAlpha(stroke, isTrack ? 0.38 : 0.22);
            ctx.fill();
            ctx.strokeStyle = stroke;
            ctx.lineWidth = isTrack ? 1.6 : 1;
            ctx.stroke();

            this._drawHillLabel(ctx, p, t0, t1, xOf, yOf, plotX, plotRight, stroke);
        }
    };

    PassTimelineView.prototype._drawHillLabel = function(ctx, p, t0, t1, xOf, yOf, plotX, plotRight, color) {
        const peakMs = (p.aosMs + p.losMs) / 2;
        if (peakMs < t0 || peakMs > t1) { return; }
        const x = xOf(peakMs);
        if (x < plotX + 8 || x > plotRight - 8) { return; }
        const y = yOf(p.maxEl) - 1;
        const mono = cssVar('--font-mono', 'monospace');
        ctx.font = '9px ' + mono;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'bottom';
        ctx.fillStyle = color;
        ctx.fillText(this._fitLabel(ctx, p.name, LABEL_MAX_PX), x, y);
    };

    PassTimelineView.prototype._drawNow = function(ctx, x, plotY, plotH) {
        const color = cssVar('--accent-danger', '#ff6b6b');
        const nx = Math.round(x) + 0.5;
        ctx.save();
        ctx.font = '9px ' + cssVar('--font-mono', 'monospace');
        const halfW = ctx.measureText('NOW').width / 2 + 3;

        // Подпись центрируем по линии, но не даём ей выйти за края канваса
        const cx = Math.max(halfW + 1, Math.min(nx, this._canvas.width - halfW - 1));
        const labelBottom = PAD_T - 1;

        // Линия идёт от подписи до низа графика — они читаются одним объектом
        ctx.strokeStyle = color;
        ctx.lineWidth = 1.5;
        ctx.beginPath();
        ctx.moveTo(nx, labelBottom);
        ctx.lineTo(nx, plotY + plotH);
        ctx.stroke();

        // Разрыв под текстом, чтобы линия не перечёркивала подпись
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        ctx.fillStyle = color;
        ctx.fillText('NOW', cx, 0);
        ctx.restore();
    };

    PassTimelineView.prototype._drawAxes = function(ctx, t0, t1, xOf, yOf, plotX, plotY, _plotW, plotH) {
        const axisText = cssVar('--spectrum-axis-text', '#8fa8bc');
        const mono = cssVar('--font-mono', 'monospace');
        ctx.fillStyle = axisText;
        ctx.font = '8px ' + mono;
        ctx.textBaseline = 'middle';
        ctx.textAlign = 'right';
        for (let i = 0; i < EL_TICKS.length; i++) {
            const el = EL_TICKS[i];
            ctx.fillText(el + '°', plotX - 4, yOf(el));
        }

        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        const tickMs = TICK_MIN * 60000;
        let t = Math.ceil(t0 / tickMs) * tickMs;
        for (; t <= t1; t += tickMs) {
            ctx.fillText(hhmm(t), xOf(t), plotY + plotH + 1);
        }
    };

    /** Обрезает подпись многоточием под доступную ширину. */
    PassTimelineView.prototype._fitLabel = function(ctx, text, maxWidth) {
        const s = String(text || '');
        if (ctx.measureText(s).width <= maxWidth) { return s; }
        let cut = s;
        while (cut.length > 1 && ctx.measureText(cut + '…').width > maxWidth) {
            cut = cut.slice(0, -1);
        }
        return cut + '…';
    };

    PassTimelineView.prototype.refreshAfterThemeChange = function() {
        this.draw();
    };

    if (typeof window !== 'undefined') {
        window.PassTimelineView = PassTimelineView;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { PassTimelineView, elevationAt }; // eslint-disable-line no-undef
    }
})();
