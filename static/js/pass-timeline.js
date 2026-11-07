// pass-timeline.js — график пролётов под панорамой Ручного режима.
//
// Ось X — время, ось Y — угол места от 0° до потолка по самому высокому пролёту в окне. Каждый пролёт — полупрозрачная
// горка от AOS до LOS с вершиной maxEl в TCA. Горки перекрываются, как у
// эталона «Passes for the next N hours».
//
// Расписание — из satellite_group_update (passesFromGroup): пролёты на час вперёд
// из timeline, окно графика не короче часа. Без данных график пуст.

'use strict';

(function() {
    const PAD_L = 24; // ось угла места
    const PAD_R = 4;
    const PAD_T = 12; // подпись NOW над линией
    const PAD_B = 12; // шкала времени
    const DEFAULT_WINDOW_MIN = 240; // пока группы нет
    const PAST_MIN = 3;
    // Горизонт вправо по умолчанию; колесо крутит по AHEAD_STEPS
    const MIN_AHEAD_MIN = 60;
    const AHEAD_STEPS_MIN = [15, 20, 30, 45, 60, 90, 120, 180, 240, 360];
    // Как DefaultWindowForward на бэке: горка цветная, пока пролёт в скользящем окне сеанса
    const SESSION_FORWARD_MIN = 1;
    const TICK_STEPS_MIN = [1, 2, 5, 10, 15, 20, 30, 60, 120];
    const MAX_TICKS = 8;
    // Потолки оси угла места и деления для каждого: низкие пролёты не жмутся к полу
    const EL_SCALES = [
        { max: 20, ticks: [0, 10, 20] },
        { max: 30, ticks: [0, 10, 20, 30] },
        { max: 45, ticks: [0, 15, 30, 45] },
        { max: 60, ticks: [0, 20, 40, 60] },
        { max: 90, ticks: [0, 30, 60, 90] },
    ];
    // Запас над вершиной: подпись КА помещается под верхней кромкой
    const EL_HEADROOM = 1.15;
    const SAMPLES = 28;
    const LABEL_MAX_PX = 90;
    const FALLBACK_H = 100;

    function cssVar(name, fallback) {
        if (typeof window !== 'undefined' && typeof window.cssVar === 'function') {
            return window.cssVar(name, fallback);
        }
        return fallback || '';
    }

    /**
     * Расписание и окно времени из события satellite_group_update.
     * @param {?Object} group
     * @returns {{passes: Array, window: ?{startMs: number, endMs: number}}}
     */
    function passesFromGroup(group) {
        const out = { passes: [], window: null };
        if (!group) { return out; }
        // timeline — пролёты на час вперёд; satellites — только окно Плана сеансов
        const list = Array.isArray(group.timeline) && group.timeline.length ? group.timeline : group.satellites;
        if (!Array.isArray(list)) { return out; }
        for (let i = 0; i < list.length; i++) {
            const s = list[i];
            if (!(s.aos > 0) || !(s.los > s.aos)) { continue; }
            out.passes.push({
                noradId: s.norad_id,
                name: s.sat_name || s.sat_alias || ('NORAD ' + s.norad_id),
                aosMs: s.aos,
                losMs: s.los,
                tcaMs: s.tca,
                maxEl: s.tca_el,
            });
        }
        const w = group.time_window;
        if (w && w.start > 0 && w.end > w.start) {
            out.window = { startMs: w.start, endMs: w.end };
        }
        return out;
    }

    /** Шкала угла места под самый высокий пролёт; без пролётов — всё небо. */
    function elScale(maxEl) {
        if (!(maxEl > 0)) { return EL_SCALES[EL_SCALES.length - 1]; }
        for (let i = 0; i < EL_SCALES.length; i++) {
            if (maxEl * EL_HEADROOM <= EL_SCALES[i].max) { return EL_SCALES[i]; }
        }
        return EL_SCALES[EL_SCALES.length - 1];
    }

    /** Потолок оси угла места, градусы. */
    function elCeil(maxEl) {
        return elScale(maxEl).max;
    }

    /** Пролёт в скользящем окне сеанса: AOS < now+1мин и LOS > now. */
    function inSession(pass, now) {
        if (!pass) { return false; }
        return pass.losMs > now && pass.aosMs < now + SESSION_FORWARD_MIN * 60000;
    }

    /**
     * Вершина для автомасштаба: сначала КА в окне сеанса «сейчас»,
     * иначе все видимые в окне графика.
     * @param {Array<{noradId:number,maxEl:number,aosMs:number,losMs:number}>} visible
     * @param {number} now
     */
    function peakEl(visible, now) {
        let all = 0;
        let session = 0;
        for (let i = 0; i < visible.length; i++) {
            const p = visible[i];
            const el = p.maxEl || 0;
            if (el > all) { all = el; }
            if (inSession(p, now) && el > session) { session = el; }
        }
        return session > 0 ? session : all;
    }

    /** Шаг подписей времени, мин: не больше MAX_TICKS подписей на окно. */
    function tickStepMin(spanMin) {
        for (let i = 0; i < TICK_STEPS_MIN.length; i++) {
            if (spanMin / TICK_STEPS_MIN[i] <= MAX_TICKS) { return TICK_STEPS_MIN[i]; }
        }
        return TICK_STEPS_MIN[TICK_STEPS_MIN.length - 1];
    }

    /**
     * Угол места в момент tMs: четверть синуса от AOS до TCA и от TCA до LOS —
     * типичная форма LEO-пролёта. TCA вне пролёта — вершина посередине.
     * @param {{aosMs:number, losMs:number, tcaMs?:number, maxEl:number}} pass
     * @param {number} tMs
     * @returns {number}
     */
    function elevationAt(pass, tMs) {
        if (!pass || tMs <= pass.aosMs || tMs >= pass.losMs) { return 0; }
        const span = pass.losMs - pass.aosMs;
        if (span <= 0) { return 0; }
        const tca = pass.tcaMs > pass.aosMs && pass.tcaMs < pass.losMs ? pass.tcaMs : pass.aosMs + span / 2;
        const u = tMs <= tca
            ? (tMs - pass.aosMs) / (tca - pass.aosMs)
            : (pass.losMs - tMs) / (pass.losMs - tca);
        return pass.maxEl * Math.sin(Math.PI / 2 * u);
    }

    /** Момент вершины горки: TCA или середина пролёта. */
    function peakMs(pass) {
        return pass.tcaMs > pass.aosMs && pass.tcaMs < pass.losMs ? pass.tcaMs : (pass.aosMs + pass.losMs) / 2;
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
        this._aheadMin = opts.aheadMin > 0 ? opts.aheadMin : MIN_AHEAD_MIN;
        this._passes = opts.passes || [];
        this._window = null;
        this._trackingId = null;
        this._selectedId = null;
        // Палитра всех КА ленты; цвет на горке — только пока пролёт в окне сеанса
        this._satColors = opts.satColors || opts.sessionColors || null;
        this._elTicks = EL_SCALES[EL_SCALES.length - 1].ticks;
        this._onWheel = null;
        if (canvas && typeof canvas.addEventListener === 'function') {
            const self = this;
            this._onWheel = function(e) {
                if (e && typeof e.preventDefault === 'function') { e.preventDefault(); }
                self.zoomWheel(e.deltaY || 0);
            };
            canvas.addEventListener('wheel', this._onWheel, { passive: false });
        }
    }

    /**
     * Палитра КА (NORAD → hex). Раскраска горок — по inSession(now) при каждой отрисовке.
     * @param {?Object<number,string>} map
     */
    PassTimelineView.prototype.setSatColors = function(map) {
        this._satColors = map && typeof map === 'object' ? map : null;
        this.draw();
    };

    /** @deprecated имя для совместимости с прежним вызовом */
    PassTimelineView.prototype.setSessionColors = function(map) {
        this.setSatColors(map);
    };

    /**
     * Ось времени: левый край «сейчас − PAST_MIN», вправо — _aheadMin (колесо мыши).
     * @param {number} now
     * @returns {{t0: number, t1: number}}
     */
    PassTimelineView.prototype._timeSpan = function(now) {
        const ahead = this._aheadMin > 0 ? this._aheadMin : MIN_AHEAD_MIN;
        return {
            t0: now - PAST_MIN * 60000,
            t1: now + ahead * 60000,
        };
    };

    /**
     * Масштаб по горизонтали: левый край на месте, вправо сжимаем/растягиваем горизонт.
     * deltaY &lt; 0 (колесо вверх) — крупнее (меньше минут вперёд), вниз — мельче.
     * @param {number} deltaY
     */
    PassTimelineView.prototype.zoomWheel = function(deltaY) {
        if (!(deltaY > 0 || deltaY < 0)) { return; }
        const steps = AHEAD_STEPS_MIN;
        let idx = 0;
        let best = Math.abs(steps[0] - this._aheadMin);
        for (let i = 1; i < steps.length; i++) {
            const d = Math.abs(steps[i] - this._aheadMin);
            if (d < best) { best = d; idx = i; }
        }
        const next = deltaY < 0 ? Math.max(0, idx - 1) : Math.min(steps.length - 1, idx + 1);
        if (steps[next] === this._aheadMin) { return; }
        this._aheadMin = steps[next];
        this.draw();
    };

    PassTimelineView.prototype.destroy = function() {
        if (this._canvas && this._onWheel && typeof this._canvas.removeEventListener === 'function') {
            this._canvas.removeEventListener('wheel', this._onWheel);
        }
        this._onWheel = null;
        this._canvas = null;
        this._ctx = null;
    };

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

    /**
     * @param {Array<{noradId:number,name:string,aosMs:number,losMs:number,tcaMs?:number,maxEl:number}>} list
     * @param {?{startMs:number, endMs:number}} [win] — окно группы; без него windowMin вперёд
     */
    PassTimelineView.prototype.setPasses = function(list, win) {
        if (!Array.isArray(list)) { return; }
        this._passes = list;
        this._window = win && win.endMs > win.startMs ? win : null;
        this.draw();
    };

    /** Подсветка горки КА под наблюдением. @param {number|null} noradId */
    PassTimelineView.prototype.setTrackingId = function(noradId) {
        this._trackingId = typeof noradId === 'number' ? noradId : null;
        this.draw();
    };

    /** Подсветка горки выбранного в Плане КА. @param {number|null} noradId */
    PassTimelineView.prototype.setSelectedId = function(noradId) {
        this._selectedId = typeof noradId === 'number' ? noradId : null;
        this.draw();
    };

    /** 2 = сопровождаемый, 1 = выбранный, 0 = остальные (для порядка отрисовки). */
    PassTimelineView.prototype._focusRank = function(noradId) {
        if (this._trackingId != null && noradId === this._trackingId) { return 2; }
        if (this._selectedId != null && noradId === this._selectedId) { return 1; }
        return 0;
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
        const span = this._timeSpan(now);
        const t0 = span.t0;
        const t1 = span.t1;
        const plotX = PAD_L;
        const plotY = PAD_T;
        const plotW = W - PAD_L - PAD_R;
        const plotH = H - PAD_T - PAD_B;
        if (plotW < 40 || plotH < 24) { return; }

        const visible = this._visiblePasses(t0, t1);
        const peak = peakEl(visible, now);
        const scale = elScale(peak);
        this._elTicks = scale.ticks;

        const xOf = (t) => plotX + (t - t0) / (t1 - t0) * plotW;
        const yOf = (el) => plotY + (1 - Math.max(0, Math.min(scale.max, el)) / scale.max) * plotH;

        this._drawBands(ctx, plotX, plotY, plotW, plotH, yOf);
        this._drawGrid(ctx, t0, t1, xOf, yOf, plotX, plotY, plotW, plotH);
        this._drawHills(ctx, t0, t1, xOf, yOf, plotX, plotW, now);
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

        const elTicks = this._elTicks;
        for (let i = 0; i < elTicks.length; i++) {
            const y = Math.round(yOf(elTicks[i])) + 0.5;
            ctx.beginPath();
            ctx.moveTo(plotX, y);
            ctx.lineTo(plotX + plotW, y);
            ctx.stroke();
        }

        const tickMs = tickStepMin((t1 - t0) / 60000) * 60000;
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
        const self = this;
        // Сначала низкие, потом высокие; выбранный/сопровождаемый — поверх всех
        out.sort(function(a, b) {
            const fa = self._focusRank(a.noradId);
            const fb = self._focusRank(b.noradId);
            if (fa !== fb) { return fa - fb; }
            return a.maxEl - b.maxEl;
        });
        return out;
    };

    PassTimelineView.prototype._drawHills = function(ctx, t0, t1, xOf, yOf, plotX, plotW, now) {
        const hill = cssVar('--accent-success', '#00d4aa');
        const plotRight = plotX + plotW;
        const y0 = yOf(0);
        const visible = this._visiblePasses(t0, t1);
        const colors = this._satColors;

        for (let i = 0; i < visible.length; i++) {
            const p = visible[i];
            const focus = this._focusRank(p.noradId);
            // Цвет палитры только пока пролёт в окне сеанса; иначе зелёный — перекраска идёт с NOW
            const stroke = (inSession(p, now) && colors && colors[p.noradId]) || hill;
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
            // Сопровождаемый / выбранный: плотнее заливка и толще контур, рисуются последними
            ctx.fillStyle = hexAlpha(stroke, focus === 2 ? 0.5 : (focus === 1 ? 0.4 : 0.22));
            ctx.fill();
            ctx.strokeStyle = stroke;
            ctx.lineWidth = focus === 2 ? 2.2 : (focus === 1 ? 1.8 : 1);
            ctx.stroke();

            this._drawHillLabel(ctx, p, t0, t1, xOf, yOf, plotX, plotRight, stroke, focus > 0);
        }
    };

    PassTimelineView.prototype._drawHillLabel = function(ctx, p, t0, t1, xOf, yOf, plotX, plotRight, color, focus) {
        const tPeak = peakMs(p);
        if (tPeak < t0 || tPeak > t1) { return; }
        const x = xOf(tPeak);
        if (x < plotX + 8 || x > plotRight - 8) { return; }
        const y = yOf(p.maxEl) - 1;
        const mono = cssVar('--font-mono', 'monospace');
        ctx.font = (focus ? 'bold 11px ' : '9px ') + mono;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'bottom';
        ctx.fillStyle = color;
        ctx.fillText(this._fitLabel(ctx, p.name, focus ? LABEL_MAX_PX + 20 : LABEL_MAX_PX), x, y);
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
        const elTicks = this._elTicks;
        for (let i = 0; i < elTicks.length; i++) {
            const el = elTicks[i];
            ctx.fillText(el + '°', plotX - 4, yOf(el));
        }

        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        const tickMs = tickStepMin((t1 - t0) / 60000) * 60000;
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

    PassTimelineView.passesFromGroup = passesFromGroup;

    if (typeof window !== 'undefined') {
        window.PassTimelineView = PassTimelineView;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { PassTimelineView, elevationAt, passesFromGroup, tickStepMin, elCeil, peakEl, inSession }; // eslint-disable-line no-undef
    }
})();
