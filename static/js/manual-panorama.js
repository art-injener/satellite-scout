// manual-panorama.js — Блок «Панорама эфира» Ручного режима (ADR-004 § 4.4).
//
// Окна обхода лежат на одной оси: смежные склеены в полосу, между полосами
// узкий разрыв «≈». В перекрытии спектры сшиваются плавно; край окна виден,
// пока нет снимка соседа. Основной вид — спектр и водопад видимой
// части оси в полном разрешении. По умолчанию вид едет за текущим окном обхода и плавно переезжает
// при перестройке; колесо меняет масштаб, перетаскивание сдвигает.
// Над ним обзорная полоса всей оси с рамкой видимой части.
// Текущее окно: линия — последний кадр, тень — максимум за стоянку.
// Прошлые окна: линия — среднее за их стоянку, тень — максимум, приглушённо.
// Водопад: строка на кадр текущего окна, столбцы остальных окон пустые.
// Лента «Передатчики» (DOM): одна строка по возрастанию частоты, прокрутка
// колесом, стрелками и ползунком. Клик — выбор, двойной клик или Enter — настройка.
// Наведение связывает элемент ленты и метку на спектре в обе стороны.
//
// Данные: снимки окон обхода из /api/stream/v1/panorama.

'use strict';

(function() {

    // ── Утилиты ───────────────────────────────────────────────────────────

    function cssVar(name, fallback) {
        if (typeof window !== 'undefined' && typeof window.cssVar === 'function') {
            return window.cssVar(name, fallback);
        }
        return fallback;
    }

    /** Усиливает цвет для неоновых меток на тёмном фоне спектра. */
    function neonColor(hex) {
        if (!hex || hex.charAt(0) !== '#') {
            return '#00ffc8';
        }
        let h = hex.slice(1);
        if (h.length === 3) {
            h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
        }
        let r = parseInt(h.slice(0, 2), 16);
        let g = parseInt(h.slice(2, 4), 16);
        let b = parseInt(h.slice(4, 6), 16);
        const boost = 0.42;
        r = Math.min(255, Math.round(r + (255 - r) * boost));
        g = Math.min(255, Math.round(g + (255 - g) * boost));
        b = Math.min(255, Math.round(b + (255 - b) * boost));
        return 'rgb(' + r + ',' + g + ',' + b + ')';
    }

    /** Преобразует #rrggbb в rgba для полупрозрачной заливки метки. */
    function hexToRgba(hex, alpha) {
        if (!hex || hex.charAt(0) !== '#') {
            return 'rgba(0, 255, 200, ' + alpha + ')';
        }
        let h = hex.slice(1);
        if (h.length === 3) {
            h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
        }
        const r = parseInt(h.slice(0, 2), 16);
        const g = parseInt(h.slice(2, 4), 16);
        const b = parseInt(h.slice(4, 6), 16);
        return 'rgba(' + r + ',' + g + ',' + b + ',' + alpha + ')';
    }

    /** #rrggbb в [r, g, b] — для заливки пикселей водопада. */
    function rgbFromHex(hex) {
        const s = String(hex || '').trim();
        if (s.charAt(0) !== '#') { return [0, 0, 0]; }
        let h = s.slice(1);
        if (h.length === 3) {
            h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
        }
        return [parseInt(h.slice(0, 2), 16) || 0, parseInt(h.slice(2, 4), 16) || 0, parseInt(h.slice(4, 6), 16) || 0];
    }

    /** Скруглённый прямоугольник (метка передатчика). */
    function roundRect(ctx, x, y, w, h, r) {
        r = Math.min(r, w / 2, h / 2);
        ctx.beginPath();
        ctx.moveTo(x + r, y);
        ctx.lineTo(x + w - r, y);
        ctx.quadraticCurveTo(x + w, y, x + w, y + r);
        ctx.lineTo(x + w, y + h - r);
        ctx.quadraticCurveTo(x + w, y + h, x + w - r, y + h);
        ctx.lineTo(x + r, y + h);
        ctx.quadraticCurveTo(x, y + h, x, y + h - r);
        ctx.lineTo(x, y + r);
        ctx.quadraticCurveTo(x, y, x + r, y);
        ctx.closePath();
    }

    function calcFreqScaleStep(widthMHz, widthPx, minSpacingPx) {
        minSpacingPx = minSpacingPx || 52;
        const maxTicks = Math.max(2, Math.floor(widthPx / minSpacingPx));
        const idealStep = widthMHz / maxTicks;
        const niceSteps = [0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.25, 0.5, 1, 2, 5];
        for (let i = 0; i < niceSteps.length; i++) {
            if (niceSteps[i] >= idealStep) { return niceSteps[i]; }
        }
        return niceSteps[niceSteps.length - 1];
    }

    /** Знаков после запятой у подписи шкалы с шагом step, МГц. */
    function freqDecimals(step) {
        if (step >= 1) { return 0; }
        if (step === 0.25) { return 2; }
        if (step >= 0.1) { return 1; }
        return step >= 0.01 ? 2 : 3;
    }

    /** «435,476 МГц» */
    function mhzText(freqMHz) {
        return freqMHz.toFixed(3).replace('.', ',') + ' МГц';
    }

    // ── Ось и вид ─────────────────────────────────────────────────────────

    /** Окна, между которыми меньше 1 кГц, рисуются одной полосой без «≈». */
    const ADJACENT_MHZ = 0.001;
    /** Разрыв между полосами на оси, МГц оси. */
    const PANORAMA_GAP_MHZ = 0.25;
    /** Самый крупный масштаб: 50 кГц на ширину графика. */
    const MIN_VIEW_MHZ = 0.05;
    /** Переезд вида к новому окну, мс. */
    const VIEW_ANIM_MS = 450;
    /** Поля графика слева (шкала dB, время) и справа — общие у спектра, водопада и обзора. */
    const PLOT_ML = 32;
    const PLOT_MR = 8;

    /**
     * Ось панорамы: полосы подряд, между ними разрыв gapMHz.
     * Координата u — МГц от левого края первой полосы с учётом разрывов.
     * @param {Array<{minMHz:number, maxMHz:number}>} spans — по возрастанию частоты
     * @param {number} [gapMHz]
     */
    function PanoramaAxis(spans, gapMHz) {
        this.spans = spans.map(function(s) { return { minMHz: s.minMHz, maxMHz: s.maxMHz }; });
        this.gapMHz = gapMHz >= 0 ? gapMHz : PANORAMA_GAP_MHZ;
        this.starts = [];
        let u = 0;
        for (let i = 0; i < this.spans.length; i++) {
            if (i > 0) { u += this.gapMHz; }
            this.starts.push(u);
            u += this.spans[i].maxMHz - this.spans[i].minMHz;
        }
        this.total = u;
    }

    /** Номер полосы с частотой или -1. */
    PanoramaAxis.prototype.spanIndexOf = function(freqMHz) {
        for (let i = 0; i < this.spans.length; i++) {
            if (freqMHz >= this.spans[i].minMHz && freqMHz <= this.spans[i].maxMHz) { return i; }
        }
        return -1;
    };

    /** u частоты; null — частота вне полос. */
    PanoramaAxis.prototype.toU = function(freqMHz) {
        const s = this.spanIndexOf(freqMHz);
        return s < 0 ? null : this.starts[s] + (freqMHz - this.spans[s].minMHz);
    };

    /** Частота точки оси; null — точка в разрыве или за краями. */
    PanoramaAxis.prototype.fromU = function(u) {
        for (let i = 0; i < this.spans.length; i++) {
            const w = this.spans[i].maxMHz - this.spans[i].minMHz;
            if (u >= this.starts[i] && u <= this.starts[i] + w) {
                return this.spans[i].minMHz + (u - this.starts[i]);
            }
        }
        return null;
    };

    // Вид — видимая часть оси {u0, u1}.

    /** Вид целиком на оси, ширина не меньше minWidth и не больше всей оси. */
    function clampView(view, total, minWidth) {
        let w = view.u1 - view.u0;
        w = Math.max(Math.min(minWidth, total), Math.min(total, w));
        const u0 = Math.max(0, Math.min(total - w, view.u0));
        return { u0: u0, u1: u0 + w };
    }

    /** Масштаб в factor раз (больше 1 — крупнее), точка anchorU остаётся на месте. */
    function zoomView(view, total, factor, anchorU, minWidth) {
        const w0 = view.u1 - view.u0;
        const w = Math.max(Math.min(minWidth, total), Math.min(total, w0 / factor));
        const u0 = anchorU - (anchorU - view.u0) * (w / w0);
        return clampView({ u0: u0, u1: u0 + w }, total, minWidth);
    }

    /** Сдвиг вида на du вдоль оси. */
    function panView(view, total, du) {
        return clampView({ u0: view.u0 + du, u1: view.u1 + du }, total, 0);
    }

    /** Промежуточный вид переезда: t от 0 до 1, к концу движение замедляется. */
    function easeView(from, to, t) {
        const k = 1 - Math.pow(1 - Math.max(0, Math.min(1, t)), 3);
        return { u0: from.u0 + (to.u0 - from.u0) * k, u1: from.u1 + (to.u1 - from.u1) * k };
    }

    /**
     * Половина окна для подписи и отметок: в перекрытии текст стоит ближе
     * к своему центру. Линия спектра и водопад этим не режутся.
     */
    function shownSpan(win) {
        return {
            minMHz: win.showMinMHz !== undefined ? win.showMinMHz : win.minMHz,
            maxMHz: win.showMaxMHz !== undefined ? win.showMaxMHz : win.maxMHz,
        };
    }

    /**
     * Столбцы графика, на которые ложится окно {minMHz, maxMHz} при виде view.
     * @returns {?{x0:number, count:number, startMHz:number, pxMHz:number}}
     *   startMHz — частота левого края столбца x0, pxMHz — ширина столбца; null — окна не видно
     */
    function windowColumns(axis, view, win, ml, plotW) {
        const s = axis.spanIndexOf((win.minMHz + win.maxMHz) / 2);
        const du = view.u1 - view.u0;
        if (s < 0 || !(du > 0) || !(plotW > 0)) { return null; }
        const span = axis.spans[s];
        const lo = Math.max(win.minMHz, span.minMHz);
        const hi = Math.min(win.maxMHz, span.maxMHz);
        const ua = axis.starts[s] + (lo - span.minMHz);
        const pxU = du / plotW;
        const xa = ml + (ua - view.u0) / pxU;
        const xb = ml + (ua + (hi - lo) - view.u0) / pxU;
        const x0 = Math.max(ml, Math.ceil(xa - 1e-6));
        const x1 = Math.min(ml + plotW, Math.ceil(xb - 1e-6));
        if (x1 <= x0) { return null; }
        return { x0: x0, count: x1 - x0, startMHz: lo + (view.u0 + (x0 - ml) * pxU - ua), pxMHz: pxU };
    }

    /**
     * Значения кадра по столбцам: в столбец идёт максимум попавших бинов,
     * узкая несущая при сжатии не теряется; при крупном масштабе бин повторяется.
     * @param {Float32Array} values — бины от loMHz до hiMHz
     * @returns {Float32Array} NaN — столбец вне кадра
     */
    function resampleMax(values, loMHz, hiMHz, startMHz, pxMHz, count) {
        const out = new Float32Array(count).fill(NaN);
        const n = values.length;
        const binMHz = (hiMHz - loMHz) / n;
        if (!(binMHz > 0)) { return out; }
        for (let c = 0; c < count; c++) {
            const fa = startMHz + c * pxMHz;
            const fb = fa + pxMHz;
            if (fb <= loMHz || fa >= hiMHz) { continue; }
            const i0 = Math.max(0, Math.floor((fa - loMHz) / binMHz + 1e-9));
            if (i0 > n - 1) { continue; }
            const i1 = Math.min(n - 1, Math.max(i0, Math.ceil((fb - loMHz) / binMHz - 1e-9) - 1));
            let m = values[i0];
            for (let i = i0 + 1; i <= i1; i++) { if (values[i] > m) { m = values[i]; } }
            out[c] = m;
        }
        return out;
    }

    /**
     * Доля окна self на частоте freqMHz. В перекрытии края плавно меняются
     * местами. Если соседнего окна с данными нет, доля равна 1 до самого края.
     * @param {{centerMHz:number, loMHz:number, hiMHz:number}} self
     * @param {Array<{centerMHz:number, loMHz:number, hiMHz:number}>} covering
     */
    function stitchWeight(self, covering, freqMHz) {
        if (!(freqMHz >= self.loMHz && freqMHz <= self.hiMHz)) { return 0; }
        const hit = [];
        for (let i = 0; i < covering.length; i++) {
            const w = covering[i];
            if (freqMHz >= w.loMHz && freqMHz <= w.hiMHz) { hit.push(w); }
        }
        if (hit.length <= 1) { return 1; }
        let left = hit[0];
        let right = hit[0];
        for (let i = 1; i < hit.length; i++) {
            if (hit[i].centerMHz < left.centerMHz) { left = hit[i]; }
            if (hit[i].centerMHz > right.centerMHz) { right = hit[i]; }
        }
        const span = left.hiMHz - right.loMHz;
        if (!(span > 0)) { return self === left ? 1 : 0; }
        let t = (freqMHz - right.loMHz) / span;
        t = Math.max(0, Math.min(1, t));
        const towardRight = t * t * (3 - 2 * t);
        if (self === left) { return 1 - towardRight; }
        if (self === right) { return towardRight; }
        return 0;
    }

    /**
     * Одна линия и тень на видимую часть оси. В перекрытии значения складываются
     * по stitchWeight, поэтому шов не обрывается. Столбец без данных — NaN.
     * @returns {{x0:number, line:Float32Array, max:Float32Array, live:Array<boolean>}}
     */
    function blendTraces(axis, view, layers, ml, plotW) {
        const line = new Float32Array(plotW);
        const maxv = new Float32Array(plotW);
        const lineW = new Float32Array(plotW);
        const maxW = new Float32Array(plotW);
        const liveW = new Float32Array(plotW);
        const prepared = [];
        const covering = [];
        for (let i = 0; i < layers.length; i++) {
            const l = layers[i];
            if (!l.line || !(l.hiMHz > l.loMHz)) { continue; }
            const cols = windowColumns(axis, view, { minMHz: l.loMHz, maxMHz: l.hiMHz }, ml, plotW);
            if (!cols) { continue; }
            prepared.push({
                layer: l,
                cols: cols,
                line: resampleMax(l.line, l.loMHz, l.hiMHz, cols.startMHz, cols.pxMHz, cols.count),
                max: l.max ? resampleMax(l.max, l.loMHz, l.hiMHz, cols.startMHz, cols.pxMHz, cols.count) : null,
            });
            covering.push({ centerMHz: l.centerHz / 1e6, loMHz: l.loMHz, hiMHz: l.hiMHz });
        }
        for (let p = 0; p < prepared.length; p++) {
            const item = prepared[p];
            const self = covering[p];
            for (let c = 0; c < item.cols.count; c++) {
                const freq = item.cols.startMHz + (c + 0.5) * item.cols.pxMHz;
                const w = stitchWeight(self, covering, freq);
                const x = item.cols.x0 - ml + c;
                if (!(w > 0) || x < 0 || x >= plotW) { continue; }
                const lv = item.line[c];
                if (lv === lv) {
                    line[x] += lv * w;
                    lineW[x] += w;
                    if (item.layer.live) { liveW[x] += w; }
                }
                if (item.max) {
                    const mv = item.max[c];
                    if (mv === mv) {
                        maxv[x] += mv * w;
                        maxW[x] += w;
                    }
                }
            }
        }
        const outLine = new Float32Array(plotW);
        const outMax = new Float32Array(plotW);
        const live = new Array(plotW);
        for (let x = 0; x < plotW; x++) {
            outLine[x] = lineW[x] > 0 ? line[x] / lineW[x] : NaN;
            outMax[x] = maxW[x] > 0 ? maxv[x] / maxW[x] : NaN;
            live[x] = lineW[x] > 0 && liveW[x] * 2 >= lineW[x];
        }
        return { x0: ml, line: outLine, max: outMax, live: live };
    }

    // ── Снимки окон обхода ───────────────────────────────────────────────

    /**
     * Окна обхода, их данные и отметки сигналов.
     * Окно — {center_hz, span_hz} из sweep_state. Кадры окна, на котором стоит
     * обход, копятся: последний, сумма и максимум. Конец стоянки превращает
     * накопленное в снимок окна: среднее и максимум за стоянку.
     */
    function PanoramaStitcher() {
        this.windows = [];
        this.spans = [];
        this._snaps = new Map();
        this._live = null;
        this._signals = new Map();
    }

    /** @param {Array<{center_hz:number, span_hz:number}>} list */
    PanoramaStitcher.prototype.setWindows = function(list) {
        const wins = (Array.isArray(list) ? list : [])
            .filter(function(w) { return w && w.center_hz > 0 && w.span_hz > 0; })
            .map(function(w) {
                return {
                    centerHz: w.center_hz,
                    minMHz: (w.center_hz - w.span_hz / 2) / 1e6,
                    maxMHz: (w.center_hz + w.span_hz / 2) / 1e6,
                };
            })
            .sort(function(a, b) { return a.minMHz - b.minMHz; });
        const spans = [];
        for (let i = 0; i < wins.length; i++) {
            const last = spans[spans.length - 1];
            if (last && wins[i].minMHz - last.maxMHz <= ADJACENT_MHZ) {
                last.maxMHz = Math.max(last.maxMHz, wins[i].maxMHz);
                last.windows.push(wins[i]);
            } else {
                spans.push({ minMHz: wins[i].minMHz, maxMHz: wins[i].maxMHz, windows: [wins[i]] });
            }
        }
        for (let i = 0; i < wins.length; i++) {
            let left = wins[i].minMHz;
            let right = wins[i].maxMHz;
            if (i > 0) {
                left = Math.max(left, (wins[i - 1].centerHz + wins[i].centerHz) / 2e6);
            }
            if (i + 1 < wins.length) {
                right = Math.min(right, (wins[i].centerHz + wins[i + 1].centerHz) / 2e6);
            }
            wins[i].showMinMHz = left;
            wins[i].showMaxMHz = right;
        }
        this.windows = wins;
        this.spans = spans;
    };

    /** Окно с центром centerHz или null. */
    PanoramaStitcher.prototype.windowOf = function(centerHz) {
        for (let i = 0; i < this.windows.length; i++) {
            if (this.windows[i].centerHz === centerHz) { return this.windows[i]; }
        }
        return null;
    };

    /**
     * Кадр окна: значения dBFS по возрастанию частоты на полосе spanHz.
     * Кадр другого окна закрывает стоянку предыдущего.
     * @param {number} centerHz
     * @param {number} spanHz
     * @param {number} timeMs — время кадра (UTC, мс)
     * @param {Float32Array} values
     */
    PanoramaStitcher.prototype.putFrame = function(centerHz, spanHz, timeMs, values) {
        if (!(centerHz > 0) || !(spanHz > 0) || !values || !values.length) { return; }
        if (!this.windowOf(centerHz)) { return; }
        let live = this._live;
        if (live && (live.centerHz !== centerHz || live.max.length !== values.length)) {
            this.endDwell();
            live = null;
        }
        if (!live) {
            live = this._live = {
                centerHz: centerHz,
                n: 0,
                sum: new Float32Array(values.length),
                max: new Float32Array(values.length).fill(-Infinity),
            };
        }
        for (let i = 0; i < values.length; i++) {
            live.sum[i] += values[i];
            if (values[i] > live.max[i]) { live.max[i] = values[i]; }
        }
        live.n++;
        live.last = values;
        live.spanHz = spanHz;
        live.timeMs = timeMs;
    };

    /** Конец стоянки: накопленное становится снимком окна. */
    PanoramaStitcher.prototype.endDwell = function() {
        const live = this._live;
        if (!live) { return; }
        const avg = new Float32Array(live.sum.length);
        for (let i = 0; i < avg.length; i++) { avg[i] = live.sum[i] / live.n; }
        this._snaps.set(live.centerHz, {
            spanHz: live.spanHz,
            timeMs: live.timeMs,
            line: avg,
            max: live.n > 1 ? live.max : null,
        });
        this._live = null;
    };

    /** Давность данных окна, мс; null — данных ещё нет. */
    PanoramaStitcher.prototype.ageMs = function(centerHz, nowMs) {
        const live = this._live;
        const s = live && live.centerHz === centerHz ? live : this._snaps.get(centerHz);
        return s ? Math.max(0, nowMs - s.timeMs) : null;
    };

    /**
     * Данные окон для отрисовки, в порядке windows.
     * line и max — dBFS от loMHz до hiMHz; line null — у окна данных нет.
     */
    PanoramaStitcher.prototype.layers = function() {
        const live = this._live;
        const snaps = this._snaps;
        return this.windows.map(function(w) {
            const isLive = !!live && live.centerHz === w.centerHz;
            const src = isLive ? live : snaps.get(w.centerHz);
            const half = src ? src.spanHz / 2e6 : 0;
            return {
                centerHz: w.centerHz,
                minMHz: w.minMHz,
                maxMHz: w.maxMHz,
                loMHz: w.centerHz / 1e6 - half,
                hiMHz: w.centerHz / 1e6 + half,
                showMinMHz: w.showMinMHz,
                showMaxMHz: w.showMaxMHz,
                live: isLive,
                line: src ? (isLive ? live.last : src.line) : null,
                max: src ? src.max : null,
            };
        });
    };

    /**
     * Сигналы band_signals текущего окна. Сервер помнит участок час,
     * отметки остальных окон между их обходами хранит панорама.
     * @param {number} index — номер окна в sweep_state.windows
     * @param {Array} signals
     */
    PanoramaStitcher.prototype.setSignals = function(index, signals) {
        // Окна в sweep_state идут по возрастанию частоты, как и this.windows
        const win = this.windows[index];
        if (!win) { return; }
        const inside = (Array.isArray(signals) ? signals : []).filter(function(sig) {
            const f = sig.center_hz / 1e6;
            return f >= win.showMinMHz && f < win.showMaxMHz;
        });
        this._signals.set(win.centerHz, inside);
    };

    /** Отметки всех окон по возрастанию частоты. */
    PanoramaStitcher.prototype.marks = function() {
        const out = [];
        this._signals.forEach(function(list) { out.push.apply(out, list); });
        return out.sort(function(a, b) { return a.center_hz - b.center_hz; });
    };

    // ── Общее для холстов ─────────────────────────────────────────────────

    /** Размер буфера холста по его месту на странице; false — холст скрыт. */
    function fitCanvas(canvas) {
        const rect = canvas.getBoundingClientRect();
        const w = Math.floor(rect.width);
        const h = Math.floor(rect.height);
        if (w < 2 || h < 2) { return false; }
        if (canvas.width !== w || canvas.height !== h) {
            canvas.width = w;
            canvas.height = h;
        }
        return true;
    }

    /** Пиксель x точки оси u при виде view. */
    function xOfU(u, view, ml, plotW) {
        return ml + (u - view.u0) / (view.u1 - view.u0) * plotW;
    }

    /** Точка оси под пикселем x. */
    function uOfX(x, view, ml, plotW) {
        return view.u0 + (x - ml) / plotW * (view.u1 - view.u0);
    }

    /**
     * Ломаная по столбцам; NaN рвёт линию. close — замкнуть вниз до base (тень).
     */
    function traceColumns(ctx, cols, vals, yOf, base, close) {
        let pen = false;
        let firstX = 0;
        let lastX = 0;
        ctx.beginPath();
        for (let c = 0; c <= vals.length; c++) {
            const v = c < vals.length ? vals[c] : NaN;
            const x = cols.x0 + c + 0.5;
            if (v === v) {
                const y = yOf(v);
                if (!pen) {
                    firstX = x;
                    if (close) { ctx.moveTo(x, base); ctx.lineTo(x, y); } else { ctx.moveTo(x, y); }
                    pen = true;
                } else {
                    ctx.lineTo(x, y);
                }
                lastX = x;
            } else if (pen) {
                if (close) { ctx.lineTo(lastX, base); ctx.lineTo(firstX, base); }
                pen = false;
            }
        }
    }

    // ── Canvas: спектр видимой части оси ──────────────────────────────────

    function PanoramaFFTView(canvas) {
        this._canvas = canvas;
        this._ctx = canvas.getContext('2d');
        this._marginLeft = PLOT_ML;
        this._marginRight = PLOT_MR;
        // Сверху — треугольные маркеры передатчиков и подпись выбранной метки
        this._marginTop = 24;
        this._marginBottom = 22;
        this._transmitters = [];
        this._highlightFreq = null;
        this._selectedFreq = null;
        // Позиции маркеров: {x, tx, freqMHz}. Попадание считаем по близости к
        // вертикали метки — целиться в маленький треугольник неудобно.
        this._txMarks = [];
        this._plotTop = this._marginTop;
        // Что рисовать: ось, вид, данные окон, шкала dB, отметки
        this._scene = null;
    }

    /**
     * @param {{axis:?PanoramaAxis, view:?Object, layers:Array, currentIdx:number,
     *          range:{min:number,max:number}, ages:Array<string>, marks:Array, emptyText:string}} scene
     */
    PanoramaFFTView.prototype.setScene = function(scene) {
        this._scene = scene;
    };

    PanoramaFFTView.prototype.setTransmitters = function(txList) {
        this._transmitters = txList || [];
    };

    PanoramaFFTView.prototype.setHighlight = function(freqMHz) {
        this._highlightFreq = freqMHz;
    };

    PanoramaFFTView.prototype.setSelected = function(freqMHz) {
        this._selectedFreq = freqMHz;
    };

    /** Ширина области графика, px. */
    PanoramaFFTView.prototype.plotWidth = function() {
        return this._canvas.width - this._marginLeft - this._marginRight;
    };

    /**
     * Возвращает передатчик под пикселем (px,py) или null.
     * Ловим по горизонтальной близости к вертикали метки на всей высоте графика.
     * @param {number} px
     * @param {number} py
     */
    PanoramaFFTView.prototype.hitTest = function(px, py) {
        const marks = this._txMarks;
        if (!marks.length) { return null; }
        if (py < 0 || py > this._canvas.height - this._marginBottom) { return null; }
        const TOLERANCE_PX = 14;
        let best = null;
        let bestDist = TOLERANCE_PX + 1;
        for (let i = 0; i < marks.length; i++) {
            const d = Math.abs(px - marks[i].x);
            if (d <= TOLERANCE_PX && d < bestDist) {
                bestDist = d;
                best = marks[i].tx;
            }
        }
        return best;
    };

    /**
     * Участок band_signals под пикселем: треугольник и подпись у нижнего края.
     * @param {number} px
     * @param {number} py
     * @returns {?Object}
     */
    PanoramaFFTView.prototype.hitSignal = function(px, py) {
        const hits = this._sigHits || [];
        let best = null;
        let bestDist = 1e9;
        for (let i = 0; i < hits.length; i++) {
            const h = hits[i];
            if (py < h.top || py > h.bottom) { continue; }
            const d = Math.abs(px - h.x);
            const tol = Math.max(h.half || 0, 10);
            if (d <= tol && d < bestDist) {
                bestDist = d;
                best = h.sig;
            }
        }
        return best;
    };

    /** Пиксель частоты при текущем виде; null — частоты не видно. */
    PanoramaFFTView.prototype._xOfFreq = function(freqMHz) {
        const sc = this._scene;
        const u = sc.axis.toU(freqMHz);
        if (u === null || u < sc.view.u0 || u > sc.view.u1) { return null; }
        return xOfU(u, sc.view, this._marginLeft, this.plotWidth());
    };

    PanoramaFFTView.prototype.draw = function() {
        if (!fitCanvas(this._canvas)) { return; }
        const w = this._canvas.width;
        const h = this._canvas.height;
        const sc = this._scene;
        if (w < 40 || h < 40) { return; }
        if (!sc || !sc.axis || !sc.axis.spans.length || !sc.view) {
            this._drawEmpty(w, h, sc ? sc.emptyText : '');
            return;
        }

        const ctx = this._ctx;
        const ml = this._marginLeft;
        const mt = this._marginTop;
        const mb = this._marginBottom;
        const plotW = this.plotWidth();
        const plotH = h - mt - mb;
        if (plotW < 10 || plotH < 10) { return; }
        this._plotTop = mt;

        const axisText = cssVar('--spectrum-axis-text', cssVar('--text-muted', '#c8d0d8'));
        const gridCol = cssVar('--spectrum-plot-grid', 'rgba(255,255,255,0.08)');
        ctx.fillStyle = cssVar('--spectrum-plot-bg', cssVar('--spectrum-bg', '#0c1420'));
        ctx.fillRect(0, 0, w, h);

        // ── Шкала dB ──
        const dbMin = sc.range.min;
        const dbMax = sc.range.max;
        const dbRange = dbMax - dbMin;
        const dbStep = dbRange <= 60 ? 10 : 20;
        const yOf = function(db) {
            return mt + plotH * (1 - Math.max(0, Math.min(1, (db - dbMin) / dbRange)));
        };
        ctx.save();
        ctx.strokeStyle = gridCol;
        ctx.setLineDash([4, 4]);
        ctx.lineWidth = 1;
        ctx.font = '9px monospace';
        ctx.fillStyle = axisText;
        ctx.textAlign = 'right';
        ctx.textBaseline = 'middle';
        for (let db = Math.ceil(dbMin / dbStep) * dbStep; db <= dbMax; db += dbStep) {
            const y = yOf(db);
            ctx.beginPath();
            ctx.moveTo(ml, y);
            ctx.lineTo(ml + plotW, y);
            ctx.stroke();
            ctx.fillText(String(db), ml - 4, y);
        }
        ctx.restore();

        this._drawFreqAxis(ctx, plotW, plotH, axisText, gridCol);
        this._drawGaps(ctx, plotW, plotH, axisText);
        this._drawLayers(ctx, plotW, plotH, yOf);
        this._drawWindowBar(ctx);
        this._drawSignalMarks(ctx, mt, plotH);
        this._drawChips(ctx, mt, plotH);
    };

    /** Шкала частот снизу и вертикальная сетка по каждой видимой полосе. */
    PanoramaFFTView.prototype._drawFreqAxis = function(ctx, plotW, plotH, axisText, gridCol) {
        const sc = this._scene;
        const ml = this._marginLeft;
        const mt = this._marginTop;
        const view = sc.view;
        ctx.save();
        ctx.font = '10px monospace';
        ctx.fillStyle = axisText;
        ctx.strokeStyle = gridCol;
        ctx.setLineDash([2, 5]);
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        // Правый край последней подписи: соседние полосы не печатают поверх друг друга
        let labelEnd = ml;
        for (let s = 0; s < sc.axis.spans.length; s++) {
            const span = sc.axis.spans[s];
            const su0 = Math.max(view.u0, sc.axis.starts[s]);
            const su1 = Math.min(view.u1, sc.axis.starts[s] + (span.maxMHz - span.minMHz));
            if (su1 <= su0) { continue; }
            const f0 = span.minMHz + (su0 - sc.axis.starts[s]);
            const f1 = span.minMHz + (su1 - sc.axis.starts[s]);
            const pxW = (su1 - su0) / (view.u1 - view.u0) * plotW;
            const step = calcFreqScaleStep(f1 - f0, pxW, 70);
            const dec = freqDecimals(step);
            for (let k = Math.ceil(f0 / step - 1e-9); k * step <= f1 + 1e-9; k++) {
                const f = k * step;
                const x = xOfU(sc.axis.starts[s] + (f - span.minMHz), view, ml, plotW);
                ctx.beginPath();
                ctx.moveTo(x, mt);
                ctx.lineTo(x, mt + plotH);
                ctx.stroke();
                const text = f.toFixed(dec);
                const half = ctx.measureText(text).width / 2;
                if (x - half < labelEnd + 6 || x + half > ml + plotW) { continue; }
                ctx.fillText(text, x, mt + plotH + 4);
                labelEnd = x + half;
            }
        }
        // Единицы — в левом поле под шкалой dB, подписи частот их не закрывают
        ctx.textAlign = 'left';
        ctx.font = '9px monospace';
        ctx.fillText('МГц', 2, mt + plotH + 5);
        ctx.restore();
    };

    /** Разрывы между полосами: фон поля и «≈». */
    PanoramaFFTView.prototype._drawGaps = function(ctx, plotW, plotH, axisText) {
        const sc = this._scene;
        const ml = this._marginLeft;
        const mt = this._marginTop;
        ctx.save();
        for (let s = 0; s + 1 < sc.axis.spans.length; s++) {
            const ga = sc.axis.starts[s] + (sc.axis.spans[s].maxMHz - sc.axis.spans[s].minMHz);
            const gb = sc.axis.starts[s + 1];
            const xa = Math.max(ml, xOfU(ga, sc.view, ml, plotW));
            const xb = Math.min(ml + plotW, xOfU(gb, sc.view, ml, plotW));
            if (xb <= xa) { continue; }
            ctx.fillStyle = cssVar('--waterfall-margin-bg', '#000000');
            ctx.fillRect(xa, mt, xb - xa, plotH);
            if (xb - xa >= 10) {
                ctx.font = '14px monospace';
                ctx.fillStyle = axisText;
                ctx.textAlign = 'center';
                ctx.textBaseline = 'middle';
                ctx.fillText('≈', (xa + xb) / 2, mt + plotH / 2);
            }
        }
        ctx.restore();
    };

    /** Тень максимума и линия. Перекрытие сшито, живая половина ярче снимка. */
    PanoramaFFTView.prototype._drawLayers = function(ctx, plotW, plotH, yOf) {
        const sc = this._scene;
        const ml = this._marginLeft;
        const base = this._marginTop + plotH;
        const blended = blendTraces(sc.axis, sc.view, sc.layers, ml, plotW);
        const cols = { x0: blended.x0 };
        const lineColor = cssVar('--spectrum-trace', '#5ee878');
        const holdColor = cssVar('--spectrum-hold-trace', '#5ec8ff');
        ctx.save();
        ctx.lineJoin = 'round';
        ctx.fillStyle = holdColor;
        this._drawSplit(ctx, cols, blended.max, blended.live, yOf, base, true, 0.3, 0.15);
        ctx.strokeStyle = lineColor;
        ctx.lineWidth = 1.5;
        this._drawSplit(ctx, cols, blended.line, blended.live, yOf, base, false, 1, 0.55);
        ctx.restore();
    };

    /** Две ломаные: столбцы, где живое окно главнее, и столбцы снимка. */
    PanoramaFFTView.prototype._drawSplit = function(ctx, cols, vals, live, yOf, base, close, alphaLive, alphaOld) {
        const fresh = new Float32Array(vals.length);
        const aged = new Float32Array(vals.length);
        for (let i = 0; i < vals.length; i++) {
            if (live[i]) { fresh[i] = vals[i]; aged[i] = NaN; }
            else { fresh[i] = NaN; aged[i] = vals[i]; }
        }
        ctx.globalAlpha = alphaLive;
        traceColumns(ctx, cols, fresh, yOf, base, close);
        if (close) { ctx.fill(); } else { ctx.stroke(); }
        ctx.globalAlpha = alphaOld;
        traceColumns(ctx, cols, aged, yOf, base, close);
        if (close) { ctx.fill(); } else { ctx.stroke(); }
    };

    /** Текущее окно — полоска сверху, без подписи. */
    PanoramaFFTView.prototype._drawWindowBar = function(ctx) {
        const sc = this._scene;
        if (sc.currentIdx < 0) { return; }
        const l = sc.layers[sc.currentIdx];
        if (!l) { return; }
        const cols = windowColumns(sc.axis, sc.view, shownSpan(l), this._marginLeft, this.plotWidth());
        if (!cols) { return; }
        ctx.save();
        ctx.fillStyle = cssVar('--text-accent', '#5ec8ff');
        ctx.fillRect(cols.x0, this._marginTop - 3, cols.count, 3);
        ctx.restore();
    };

    /** Панорама без окон: фон и пояснение по центру. */
    PanoramaFFTView.prototype._drawEmpty = function(w, h, text) {
        const ctx = this._ctx;
        this._txMarks = [];
        this._sigHits = [];
        ctx.fillStyle = cssVar('--spectrum-plot-bg', cssVar('--spectrum-bg', '#0c1420'));
        ctx.fillRect(0, 0, w, h);
        if (!text) { return; }
        ctx.save();
        ctx.font = '12px sans-serif';
        ctx.fillStyle = cssVar('--text-muted', '#c8d0d8');
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText(text, w / 2, h / 2);
        ctx.restore();
    };

    /**
     * Отметки сигналов band_signals у нижнего края графика: черта по ширине
     * участка и короткий код типа, тот же, что на «Спектре».
     */
    PanoramaFFTView.prototype._drawSignalMarks = function(ctx, plotTop, plotH) {
        this._sigHits = [];
        const marks = this._scene.marks;
        if (!marks.length) { return; }
        const colors = {
            sat: cssVar('--accent-primary', '#00ffc8'),
            probable: cssVar('--accent-primary', '#00ffc8'),
            ground: cssVar('--text-label', '#8fa8bc'),
            unknown: cssVar('--accent-warning', '#ffb000'),
        };
        const y = plotTop + plotH - 4;
        const placed = [];
        ctx.save();
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'bottom';
        for (let i = 0; i < marks.length; i++) {
            const m = marks[i];
            const x = this._xOfFreq(m.freqMHz);
            if (x === null) { continue; }
            const halfMHz = (m.bwMHz || 0) / 2;
            const xl = this._xOfFreq(m.freqMHz - halfMHz);
            const xr = this._xOfFreq(m.freqMHz + halfMHz);
            const half = Math.max(1, ((xr === null ? x : xr) - (xl === null ? x : xl)) / 2);
            ctx.globalAlpha = m.kind === 'probable' ? 0.6 : 1;
            ctx.fillStyle = colors[m.kind] || colors.unknown;
            ctx.fillRect(x - half, y, half * 2, 3);
            const hit = { x: x, top: y - 28, bottom: y + 8, half: Math.max(half, 10), sig: m.sig };
            this._sigHits.push(hit);
            ctx.beginPath();
            ctx.moveTo(x, y - 7);
            ctx.lineTo(x - 4, y - 1);
            ctx.lineTo(x + 4, y - 1);
            ctx.closePath();
            ctx.fill();
            if (!m.code) { continue; }
            const textW = ctx.measureText(m.code).width;
            const left = x - textW / 2;
            const right = x + textW / 2;
            let yText = y - 10;
            let row = 0;
            while (row < 3 && placed.some(function(p) {
                return p.y === yText && !(right < p.left - 4 || left > p.right + 4);
            })) {
                row++;
                yText -= 12;
            }
            if (row >= 3) { continue; }
            placed.push({ left: left, right: right, y: yText });
            ctx.fillText(m.code, x, yText);
            if (hit.top > yText - 12) { hit.top = yText - 12; }
        }
        ctx.restore();
    };

    /**
     * Метки передатчиков: пунктирная вертикаль + треугольник сверху.
     * Подпись рисуется только для выбранной или подсвеченной метки — имена и
     * частоты всех передатчиков и так перечислены в ленте.
     */
    PanoramaFFTView.prototype._drawChips = function(ctx, plotTop, plotH) {
        this._txMarks = [];
        const txList = this._transmitters;
        if (!txList || txList.length === 0) { return; }

        const silentColor = cssVar('--panorama-tx-muted', '#8fa8bc');
        const chipFill = cssVar('--panorama-chip-fill', 'rgba(6, 10, 18, 0.88)');
        const labelTextCol = cssVar('--panorama-chip-label-text', '#eefcff');
        const MARK_W = 5;
        const MARK_H = 5;

        ctx.save();
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        let labelled = null;

        for (let t = 0; t < txList.length; t++) {
            const tx = txList[t];
            const x = this._xOfFreq(tx.freqMHz);
            if (x === null) { continue; }
            this._txMarks.push({ x: x, tx: tx, freqMHz: tx.freqMHz });

            const baseColor = tx.active
                ? (tx.chipColor || cssVar('--panorama-tx-1', '#00ffc8'))
                : silentColor;
            const color = neonColor(baseColor);
            const isHi = this._highlightFreq !== null &&
                Math.abs(this._highlightFreq - tx.freqMHz) < 0.001;
            const isSel = this._selectedFreq !== null &&
                Math.abs(this._selectedFreq - tx.freqMHz) < 0.001;
            if (isSel || (isHi && !labelled)) {
                labelled = { tx: tx, x: x, color: color, baseColor: baseColor, isSel: isSel };
            }

            // Вертикальная привязка частоты к спектру
            ctx.save();
            ctx.strokeStyle = color;
            ctx.shadowColor = color;
            ctx.shadowBlur = isSel ? 12 : (isHi ? 8 : 5);
            ctx.lineWidth = isSel ? 2 : 1.25;
            ctx.globalAlpha = isSel ? 0.92 : (isHi ? 0.78 : (tx.active ? 0.5 : 0.34));
            ctx.setLineDash(isSel ? [] : [4, 3]);
            ctx.beginPath();
            ctx.moveTo(x, plotTop);
            ctx.lineTo(x, plotTop + plotH);
            ctx.stroke();
            ctx.restore();

            // Треугольник над графиком: цвет = передатчик, размер один для всех
            ctx.save();
            ctx.fillStyle = color;
            ctx.shadowColor = color;
            ctx.shadowBlur = isSel ? 10 : (isHi ? 7 : 4);
            ctx.globalAlpha = isSel ? 1 : (isHi ? 0.95 : (tx.active ? 0.85 : 0.6));
            ctx.beginPath();
            ctx.moveTo(x - MARK_W, plotTop - MARK_H - 2);
            ctx.lineTo(x + MARK_W, plotTop - MARK_H - 2);
            ctx.lineTo(x, plotTop - 1);
            ctx.closePath();
            ctx.fill();
            ctx.restore();
        }

        // Подпись — одна, для выбранной (или подсвеченной) метки
        if (labelled) {
            const LABEL_PAD_X = 6;
            const LABEL_H = 15;
            const tx3 = labelled.tx;
            const text = (tx3.label ? tx3.label + '  ' : '') + tx3.freqMHz.toFixed(3) +
                (tx3.active ? '' : ' · молчит');
            ctx.font = labelled.isSel ? 'bold 10px monospace' : '10px monospace';
            const boxW = ctx.measureText(text).width + LABEL_PAD_X * 2;
            const boxH = LABEL_H;
            const maxLeft = this._canvas.width - this._marginRight - boxW;
            const boxLeft = Math.max(this._marginLeft, Math.min(labelled.x - boxW / 2, maxLeft));
            const boxTop = Math.max(0, plotTop - MARK_H - 4 - boxH);

            ctx.save();
            ctx.shadowColor = labelled.color;
            ctx.shadowBlur = labelled.isSel ? 12 : 8;
            ctx.fillStyle = labelled.isSel
                ? hexToRgba(labelled.baseColor, 0.38)
                : chipFill;
            ctx.strokeStyle = labelled.color;
            ctx.lineWidth = 1.5;
            roundRect(ctx, boxLeft, boxTop, boxW, boxH, 3);
            ctx.fill();
            ctx.stroke();
            ctx.restore();

            ctx.shadowBlur = 0;
            ctx.fillStyle = labelTextCol;
            ctx.fillText(text, boxLeft + boxW / 2, boxTop + boxH / 2);
        }

        ctx.restore();
    };

    // ── Canvas: обзорная полоса всей оси ──────────────────────────────────

    function PanoramaOverviewView(canvas) {
        this._canvas = canvas;
        this._ctx = canvas.getContext('2d');
        this._marginLeft = PLOT_ML;
        this._marginRight = PLOT_MR;
    }

    PanoramaOverviewView.prototype.plotWidth = function() {
        return this._canvas.width - this._marginLeft - this._marginRight;
    };

    /** Вся ось: линии окон, текущее окно, рамка видимой части основного вида. */
    PanoramaOverviewView.prototype.draw = function(sc) {
        if (!fitCanvas(this._canvas)) { return; }
        const ctx = this._ctx;
        const w = this._canvas.width;
        const h = this._canvas.height;
        const ml = this._marginLeft;
        const plotW = this.plotWidth();
        ctx.fillStyle = cssVar('--spectrum-plot-bg', cssVar('--spectrum-bg', '#0c1420'));
        ctx.fillRect(0, 0, w, h);
        if (!sc || !sc.axis || !sc.axis.spans.length || plotW < 10) { return; }
        const all = { u0: 0, u1: sc.axis.total };
        const top = 3;
        const plotH = h - 6;
        const dbRange = sc.range.max - sc.range.min;
        const yOf = function(db) {
            return top + plotH * (1 - Math.max(0, Math.min(1, (db - sc.range.min) / dbRange)));
        };
        const axisText = cssVar('--spectrum-axis-text', cssVar('--text-muted', '#c8d0d8'));
        const accent = cssVar('--text-accent', '#5ec8ff');

        ctx.save();
        ctx.fillStyle = cssVar('--waterfall-margin-bg', '#000000');
        for (let s = 0; s + 1 < sc.axis.spans.length; s++) {
            const ga = sc.axis.starts[s] + (sc.axis.spans[s].maxMHz - sc.axis.spans[s].minMHz);
            const xa = xOfU(ga, all, ml, plotW);
            ctx.fillRect(xa, 0, xOfU(sc.axis.starts[s + 1], all, ml, plotW) - xa, h);
        }
        const cur = sc.layers[sc.currentIdx];
        const curCols = cur ? windowColumns(sc.axis, all, { minMHz: cur.minMHz, maxMHz: cur.maxMHz }, ml, plotW) : null;
        if (curCols) {
            ctx.globalAlpha = 0.2;
            ctx.fillStyle = accent;
            ctx.fillRect(curCols.x0, 0, curCols.count, h);
        }
        const blended = blendTraces(sc.axis, all, sc.layers, ml, plotW);
        ctx.globalAlpha = 1;
        ctx.strokeStyle = cssVar('--spectrum-trace', '#5ee878');
        ctx.lineWidth = 1;
        traceColumns(ctx, { x0: blended.x0 }, blended.line, yOf, top + plotH, false);
        ctx.stroke();
        // Подпись полосы — у её левого края
        ctx.globalAlpha = 0.8;
        ctx.font = '9px monospace';
        ctx.fillStyle = axisText;
        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';
        for (let s = 0; s < sc.axis.spans.length; s++) {
            ctx.fillText(sc.axis.spans[s].minMHz.toFixed(1), xOfU(sc.axis.starts[s], all, ml, plotW) + 2, 2);
        }
        // Рамка видимой части
        ctx.globalAlpha = 1;
        const vx0 = xOfU(sc.view.u0, all, ml, plotW);
        const vx1 = xOfU(sc.view.u1, all, ml, plotW);
        ctx.strokeStyle = accent;
        ctx.lineWidth = 1.5;
        ctx.strokeRect(vx0 + 0.75, 0.75, Math.max(2, vx1 - vx0 - 1.5), h - 1.5);
        ctx.restore();
    };

    // ── Водопад видимой части оси ─────────────────────────────────────────

    /** Период кадров обхода на сервере, мс: одна строка водопада. */
    const FRAME_MS = 250;
    /** Строк в памяти водопада: с запасом на высокий экран. */
    const WF_MAX_ROWS = 1200;

    /**
     * Водопад: строка на кадр текущего окна, свежая сверху. Строки хранятся
     * с частотами окна, поэтому смена вида перерисовывает их без потерь;
     * столбцы, где окна строки нет, пустые.
     */
    function PanoramaWaterfallView(canvas, opts) {
        opts = opts || {};
        this._canvas = canvas;
        this._ctx = canvas.getContext('2d');
        this._marginLeft = PLOT_ML;
        this._marginRight = PLOT_MR;
        this._tickMs = opts.tickMs || FRAME_MS;
        this._colormap = opts.colormap || null;
        this._marginRgb = opts.marginRgb || null;
        this._coldRgb = opts.coldRgb || null;
        this._axis = null;
        this._rows = [];
        this._pending = 0;
        this._img = null;
        this._drawn = null;
        this._scratch = [0, 0, 0];
    }

    /** Новая ось: старые строки легли бы не на свои частоты. */
    PanoramaWaterfallView.prototype.setAxis = function(axis) {
        this._axis = axis;
        this._rows = [];
        this._pending = 0;
        this._drawn = null;
    };

    /** @param {{minMHz:number, maxMHz:number, values:Float32Array}} row — кадр окна, dBFS */
    PanoramaWaterfallView.prototype.pushRow = function(row) {
        this._rows.unshift(row);
        if (this._rows.length > WF_MAX_ROWS) { this._rows.length = WF_MAX_ROWS; }
        this._pending++;
    };

    PanoramaWaterfallView.prototype.rowCount = function() {
        return this._rows.length;
    };

    /** Следующая отрисовка — заново целиком (тема, размер). */
    PanoramaWaterfallView.prototype.invalidate = function() {
        this._drawn = null;
    };

    PanoramaWaterfallView.prototype._cmap = function() {
        if (this._colormap) { return this._colormap; }
        if (typeof window !== 'undefined' && typeof window.waterfallColormap === 'function') {
            return window.waterfallColormap();
        }
        return function(v) { const c = Math.round(v * 255); return [c, c, c]; };
    };

    /**
     * Перерисовка под вид и шкалу. Если они не менялись — только сдвиг
     * на новые строки.
     * @param {{u0:number, u1:number}} view
     * @param {{min:number, max:number}} range — dBFS для цветовой шкалы
     */
    PanoramaWaterfallView.prototype.render = function(view, range) {
        if (!this._axis || !view || !fitCanvas(this._canvas)) { return; }
        const w = this._canvas.width;
        const h = this._canvas.height;
        if (!this._img || this._img.width !== w || this._img.height !== h) {
            this._img = this._ctx.createImageData(w, h);
            this._drawn = null;
        }
        const d = this._drawn;
        const same = d && d.u0 === view.u0 && d.u1 === view.u1 && d.min === range.min && d.max === range.max;
        if (same && this._pending === 0) { return; }
        const base = this._baseRow(view, w);
        const data = this._img.data;
        let rowsToDraw = h;
        if (same && this._pending < h) {
            rowsToDraw = this._pending;
            data.copyWithin(rowsToDraw * w * 4, 0, (h - rowsToDraw) * w * 4);
        }
        const plotW = w - this._marginLeft - this._marginRight;
        const cmap = this._cmap();
        for (let y = 0; y < rowsToDraw; y++) {
            this._drawRow(data, y, w, base, this._rows[y] || null, view, range, plotW, cmap);
        }
        this._pending = 0;
        this._drawn = { u0: view.u0, u1: view.u1, min: range.min, max: range.max };
        this._ctx.putImageData(this._img, 0, 0);
        this._drawTimeAxis();
    };

    /** Строка без данных: поля и разрывы цветом поля, полосы — фоном водопада. */
    PanoramaWaterfallView.prototype._baseRow = function(view, w) {
        const margin = this._marginRgb || rgbFromHex(cssVar('--waterfall-margin-bg', '#000000'));
        const cold = this._coldRgb || rgbFromHex(cssVar('--waterfall-cold-bg', '#000000'));
        const ml = this._marginLeft;
        const plotW = w - ml - this._marginRight;
        const out = new Uint8ClampedArray(w * 4);
        for (let x = 0; x < w; x++) {
            const inPlot = x >= ml && x < ml + plotW;
            const c = inPlot && this._axis.fromU(uOfX(x + 0.5, view, ml, plotW)) !== null ? cold : margin;
            out[x * 4] = c[0];
            out[x * 4 + 1] = c[1];
            out[x * 4 + 2] = c[2];
            out[x * 4 + 3] = 255;
        }
        return out;
    };

    PanoramaWaterfallView.prototype._drawRow = function(data, y, w, base, row, view, range, plotW, cmap) {
        const off = y * w * 4;
        data.set(base, off);
        if (!row) { return; }
        const cols = windowColumns(this._axis, view, { minMHz: row.minMHz, maxMHz: row.maxMHz }, this._marginLeft, plotW);
        if (!cols) { return; }
        const vals = resampleMax(row.values, row.minMHz, row.maxMHz, cols.startMHz, cols.pxMHz, cols.count);
        const span = range.max - range.min;
        for (let c = 0; c < cols.count; c++) {
            const v = vals[c];
            if (v !== v) { continue; }
            const col = cmap(Math.max(0, Math.min(1, (v - range.min) / span)), this._scratch);
            const i = off + (cols.x0 + c) * 4;
            data[i] = col[0];
            data[i + 1] = col[1];
            data[i + 2] = col[2];
        }
    };

    /** Шкала времени в левом поле: сверху 0 с, ниже — давность кадра. */
    PanoramaWaterfallView.prototype._drawTimeAxis = function() {
        const ctx = this._ctx;
        const h = this._canvas.height;
        const totalSec = (h * this._tickMs) / 1000;
        const stepSec = timeAxisStepSec(this._tickMs, TIME_LABEL_MIN_PX);
        ctx.font = '9px monospace';
        ctx.fillStyle = cssVar('--spectrum-axis-text', cssVar('--text-muted', '#c8d0d8'));
        ctx.textAlign = 'right';
        ctx.textBaseline = 'top';
        for (let sec = 0; sec < totalSec; sec += stepSec) {
            const y = Math.round((sec * 1000) / this._tickMs);
            ctx.fillText(formatAgo(sec), this._marginLeft - 4, Math.min(y, h - 10));
        }
    };

    /** Минимальное расстояние между подписями шкалы времени, px. */
    const TIME_LABEL_MIN_PX = 20;
    const TIME_STEPS_SEC = [5, 10, 30, 60, 120, 300, 600, 1200, 1800, 3600, 7200, 10800, 21600];

    /** Шаг подписей времени: строка водопада — один пиксель и tickMs. */
    function timeAxisStepSec(tickMs, minPx) {
        for (let i = 0; i < TIME_STEPS_SEC.length; i++) {
            if (TIME_STEPS_SEC[i] * 1000 / tickMs >= minPx) { return TIME_STEPS_SEC[i]; }
        }
        return TIME_STEPS_SEC[TIME_STEPS_SEC.length - 1];
    }

    /** Давность для узкого поля шкалы: «30с», «10м», «2ч». */
    function formatAgo(sec) {
        if (sec < 60) { return sec + 'с'; }
        if (sec < 3600) { return Math.round(sec / 60) + 'м'; }
        const m = Math.round((sec % 3600) / 60);
        return Math.floor(sec / 3600) + 'ч' + (m ? m + 'м' : '');
    }

    // ── Контроллер Панорамы (связывает canvas, список TX, данные) ──────────

    /** Тип кадра потока: спектр в dBFS. */
    const DATA_TYPE_FFT_DB = 0x01;
    /** Шкала dB, пока не пришло ни одного кадра. */
    const DEFAULT_RANGE = { min: -100, max: 0 };
    /** Шаг колеса мыши по масштабу. */
    const WHEEL_ZOOM = 1.25;
    /** Сдвиг мыши, после которого нажатие считается перетаскиванием, px. */
    const DRAG_PX = 3;

    /**
     * ManualPanorama — контроллер блока «Панорама эфира».
     * @param {Object} opts
     * @param {HTMLCanvasElement} opts.canvas — #manual-panorama-fft
     * @param {HTMLCanvasElement} opts.wfCanvas — #manual-panorama-wf
     * @param {HTMLCanvasElement} opts.overviewCanvas — #manual-panorama-overview
     * @param {HTMLElement} opts.followBtn — #manual-pan-follow («за обходом»)
     * @param {HTMLElement} opts.txListEl — #manual-tx-scroll (контейнер ленты)
     * @param {HTMLElement} opts.prevBtn — #manual-tx-prev (прокрутка ленты влево)
     * @param {HTMLElement} opts.nextBtn — #manual-tx-next (прокрутка ленты вправо)
     * @param {Array} opts.transmitters — [{freqMHz, label, active, ...}, ...]
     */
    function ManualPanorama(opts) {
        opts = opts || {};
        const byId = function(id) {
            return typeof document !== 'undefined' && document.getElementById ? document.getElementById(id) : null;
        };
        this._active = false;
        this._timer = null;
        this._frameReq = null;
        this._highlightFreq = null;
        this._selectedFreq = null;

        // Окна приходят из sweep_state; до него панорама пуста
        this._stitcher = new PanoramaStitcher();
        this._state = null;
        this._emptyText = '';
        this._axis = null;
        this._view = null;
        this._anim = null;
        this._follow = true;
        this._drag = null;
        this._dragMoved = false;
        this._range = DEFAULT_RANGE;
        const AR = typeof window !== 'undefined' && window.SpectrumStream && window.SpectrumStream.AutoRange;
        this._autoRange = AR ? new AR() : null;

        // Список приходит из группы КА (setTransmitters); до неё лента пуста
        this._transmitters = opts.transmitters || [];
        this._onSignal = typeof opts.onSignal === 'function' ? opts.onSignal : null;
        this._onTransmitter = typeof opts.onTransmitter === 'function' ? opts.onTransmitter : null;

        this._canvas = opts.canvas || byId('manual-panorama-fft');
        this._fftView = null;
        if (this._canvas) {
            this._fftView = new PanoramaFFTView(this._canvas);
            this._fftView.setTransmitters(this._transmitters);
            this._fftView.setSelected(this._selectedFreq);
        }
        const wfCanvas = opts.wfCanvas || byId('manual-panorama-wf');
        this._wfCanvas = wfCanvas;
        this._wfView = wfCanvas ? new PanoramaWaterfallView(wfCanvas) : null;
        this._ovCanvas = opts.overviewCanvas || byId('manual-panorama-overview');
        this._ovView = this._ovCanvas ? new PanoramaOverviewView(this._ovCanvas) : null;
        this._followBtn = opts.followBtn || byId('manual-pan-follow');

        // Лента передатчиков — подписи к панораме и таймлайну.
        this._txListEl = opts.txListEl || byId('manual-tx-scroll');
        this._prevBtn = opts.prevBtn || byId('manual-tx-prev');
        this._nextBtn = opts.nextBtn || byId('manual-tx-next');
        // Частота передатчика, на который настроен приёмник: его элемент в рамке.
        this._followedFreq = null;

        const self = this;
        this._listeners = [];
        const on = function(el, type, fn, o) {
            if (!el) { return; }
            el.addEventListener(type, fn, o);
            self._listeners.push([el, type, fn, o]);
        };
        if (this._txListEl) {
            // Обычное колесо крутит ленту вбок; горизонтальную прокрутку
            // тачпада браузер обрабатывает сам
            on(this._txListEl, 'wheel', function(e) {
                if (!e.deltaY || Math.abs(e.deltaX || 0) >= Math.abs(e.deltaY)) { return; }
                e.preventDefault();
                self._txListEl.scrollLeft += e.deltaY;
            }, { passive: false });
        }
        on(this._prevBtn, 'click', function() { self._scrollTxList(-1); });
        on(this._nextBtn, 'click', function() { self._scrollTxList(1); });
        on(this._followBtn, 'click', function() { self.setFollow(true); });
        this._renderFollowBtn();
        this._renderTxList();

        // Спектр: клик — выбор метки, двойной клик — настройка приёмника,
        // колесо — масштаб, перетаскивание — сдвиг
        on(this._canvas, 'click', function(e) {
            if (self._dragMoved) { self._dragMoved = false; return; }
            const sig = self._fftView && typeof self._fftView.hitSignal === 'function'
                ? self._fftView.hitSignal(e.offsetX, e.offsetY) : null;
            if (sig) {
                if (self._onSignal) { self._onSignal(sig, e.clientX || 0, e.clientY || 0); }
                return;
            }
            const hit = self._fftView && self._fftView.hitTest(e.offsetX, e.offsetY);
            if (!hit) { return; }
            self._onTxClick(hit);
            if (self._onTransmitter) { self._onTransmitter(hit, e.clientX || 0, e.clientY || 0); }
        });
        on(this._canvas, 'dblclick', function(e) {
            const hit = self._fftView && self._fftView.hitTest(e.offsetX, e.offsetY);
            if (hit) { self._onTxTune(hit); }
        });
        on(this._canvas, 'mousemove', function(e) {
            if (!self._fftView || self._drag) { return; }
            const hit = self._fftView.hitTest(e.offsetX, e.offsetY);
            self._canvas.style.cursor = hit ? 'pointer' : 'grab';
            self._setHighlight(hit ? hit.freqMHz : null);
        });
        [this._canvas, wfCanvas].forEach(function(c) {
            on(c, 'wheel', function(e) {
                if (!self._view || !e.deltaY) { return; }
                e.preventDefault();
                self.zoomAtU(self._uAt(e.offsetX), e.deltaY < 0 ? WHEEL_ZOOM : 1 / WHEEL_ZOOM);
            }, { passive: false });
            on(c, 'mousedown', function(e) { self._startDrag(e, false); });
        });
        on(this._ovCanvas, 'mousedown', function(e) { self._startDrag(e, true); });

        this._resizeObservers = [];
        if (typeof ResizeObserver !== 'undefined') {
            const ro = new ResizeObserver(function() {
                if (self._wfView) { self._wfView.invalidate(); }
                self._kick();
            });
            [this._canvas, wfCanvas, this._ovCanvas].forEach(function(c) {
                if (c) { ro.observe(c.parentElement || c); }
            });
            this._resizeObservers.push(ro);
        }
    }

    /**
     * Заменить список передатчиков (привязка к живой группе КА).
     * @param {Array} list
     */
    ManualPanorama.prototype.setTransmitters = function(list) {
        this._transmitters = Array.isArray(list) ? list : [];
        if (this._fftView) { this._fftView.setTransmitters(this._transmitters); }
        this._renderTxList();
        this._kick();
    };

    // ── Обход: состояние, кадры, отметки ──────────────────────────────────

    /**
     * Состояние обхода (GET /api/sdr/sweep или SSE sweep_state).
     * Уход с окна или остановка закрывают стоянку; вид, если следит,
     * переезжает к новому окну.
     * @param {{running:boolean, dwell_s:number, windows:Array, index:number, cycle:number}} st
     */
    ManualPanorama.prototype.setSweepState = function(st) {
        if (!st) { return; }
        const prev = this._state;
        if (prev && prev.running && (!st.running || prev.index !== st.index || prev.cycle !== st.cycle)) {
            this._stitcher.endDwell();
        }
        this._state = st;
        this._stitcher.setWindows(st.windows);
        const spans = this._stitcher.spans.map(function(s) { return { minMHz: s.minMHz, maxMHz: s.maxMHz }; });
        if (!this._axis || JSON.stringify(spans) !== JSON.stringify(this._axis.spans)) {
            this._axis = spans.length ? new PanoramaAxis(spans) : null;
            this._view = null;
            this._anim = null;
            if (this._wfView) { this._wfView.setAxis(this._axis); }
        }
        if (this._follow || !this._view) {
            this._goTo(this._followTarget(), this._follow && !!this._view);
        }
        this._kick();
    };

    /** Окно, на котором стоит обход, или null. */
    ManualPanorama.prototype._currentWindow = function() {
        const st = this._state;
        const w = st && st.running && st.windows ? st.windows[st.index] : null;
        return w ? this._stitcher.windowOf(w.center_hz) : null;
    };

    /** Кадр потока панорамы; heartbeat и прочие типы пропускаются. */
    ManualPanorama.prototype.putFrame = function(header, values) {
        if (!header || header.dataType !== DATA_TYPE_FFT_DB || !values) { return; }
        this._stitcher.putFrame(header.centerHz, header.spanHz, header.timestampMs, values);
        if (this._autoRange) { this._range = this._autoRange.update(values); }
        const cur = this._currentWindow();
        if (this._wfView && cur && cur.centerHz === header.centerHz) {
            const c = header.centerHz / 1e6;
            const half = header.spanHz / 2e6;
            this._wfView.pushRow({ minMHz: c - half, maxMHz: c + half, values: values });
        }
        this._kick();
    };

    /** Сигналы band_signals тракта: относятся к окну, на котором стоит обход. */
    ManualPanorama.prototype.setSignals = function(signals) {
        if (!this._state || !this._state.running) { return; }
        this._stitcher.setSignals(this._state.index, signals);
        this._kick();
    };

    /** Накопленные отметки всех окон обхода (для таблицы «Эфир»). */
    ManualPanorama.prototype.marks = function() {
        return this._stitcher ? this._stitcher.marks() : [];
    };

    /** Пояснение вместо графика, пока окон нет («Обход не настроен»). */
    ManualPanorama.prototype.setEmptyText = function(text) {
        this._emptyText = text || '';
        this._kick();
    };

    // ── Вид: слежение, масштаб, сдвиг, переезд ────────────────────────────

    /** Куда смотреть при слежении: текущее окно, без обхода — вся ось. */
    ManualPanorama.prototype._followTarget = function() {
        const axis = this._axis;
        if (!axis) { return null; }
        const cur = this._currentWindow();
        if (!cur) { return { u0: 0, u1: axis.total }; }
        const s = axis.spanIndexOf((cur.minMHz + cur.maxMHz) / 2);
        const u0 = axis.starts[s] + (cur.minMHz - axis.spans[s].minMHz);
        return { u0: u0, u1: u0 + (cur.maxMHz - cur.minMHz) };
    };

    ManualPanorama.prototype._goTo = function(target, animate) {
        if (!target) {
            this._view = null;
            this._anim = null;
            return;
        }
        if (!animate || !this._view) {
            this._view = target;
            this._anim = null;
            return;
        }
        this._anim = { from: this._view, to: target, t0: Date.now(), dur: VIEW_ANIM_MS };
    };

    /** Вид на момент nowMs: идёт переезд — промежуточный, по окончании — целевой. */
    ManualPanorama.prototype._viewAt = function(nowMs) {
        const a = this._anim;
        if (a) {
            const t = (nowMs - a.t0) / a.dur;
            if (t >= 1) {
                this._view = a.to;
                this._anim = null;
            } else {
                this._view = easeView(a.from, a.to, t);
            }
        }
        return this._view;
    };

    /** Слежение за окном обхода: включение сразу переезжает к нему. */
    ManualPanorama.prototype.setFollow = function(on) {
        this._follow = !!on;
        this._renderFollowBtn();
        if (this._follow) { this._goTo(this._followTarget(), true); }
        this._kick();
    };

    ManualPanorama.prototype._renderFollowBtn = function() {
        if (this._followBtn) { this._followBtn.setAttribute('aria-pressed', this._follow ? 'true' : 'false'); }
    };

    /** Вид меняет оператор: слежение выключается, переезд прерывается. */
    ManualPanorama.prototype._takeView = function(view) {
        this._anim = null;
        this._view = view;
        if (this._follow) {
            this._follow = false;
            this._renderFollowBtn();
        }
        this._kick();
    };

    /** Масштаб колесом вокруг точки оси u. */
    ManualPanorama.prototype.zoomAtU = function(u, factor) {
        if (!this._axis || !this._view) { return; }
        this._takeView(zoomView(this._viewAt(Date.now()), this._axis.total, factor, u, MIN_VIEW_MHZ));
    };

    /** Точка оси под пикселем x основного вида. */
    ManualPanorama.prototype._uAt = function(x) {
        const plotW = this._fftView ? this._fftView.plotWidth() : 0;
        return plotW > 0 ? uOfX(x, this._view, PLOT_ML, plotW) : (this._view.u0 + this._view.u1) / 2;
    };

    /**
     * Нажатие на спектре или водопаде тащит вид; на обзорной полосе — ставит
     * рамку видимой части под указатель.
     */
    ManualPanorama.prototype._startDrag = function(e, overview) {
        if (e.button || !this._axis || !this._view || typeof window === 'undefined') { return; }
        const self = this;
        const canvas = overview ? this._ovCanvas : this._canvas;
        const plotW = canvas.width - PLOT_ML - PLOT_MR;
        if (!(plotW > 0)) { return; }
        this._drag = { x: e.clientX, view: this._view };
        this._dragMoved = false;
        const centerAt = function(offsetX) {
            const u = uOfX(offsetX, { u0: 0, u1: self._axis.total }, PLOT_ML, plotW);
            const half = (self._view.u1 - self._view.u0) / 2;
            self._takeView(clampView({ u0: u - half, u1: u + half }, self._axis.total, MIN_VIEW_MHZ));
        };
        if (overview) { centerAt(e.offsetX); }
        const move = function(ev) {
            const dx = ev.clientX - self._drag.x;
            if (overview) {
                centerAt(e.offsetX + dx);
                return;
            }
            if (!self._dragMoved && Math.abs(dx) < DRAG_PX) { return; }
            self._dragMoved = true;
            const v = self._drag.view;
            self._takeView(panView(v, self._axis.total, -dx / plotW * (v.u1 - v.u0)));
        };
        const up = function() {
            window.removeEventListener('mousemove', move);
            window.removeEventListener('mouseup', up);
            self._drag = null;
        };
        window.addEventListener('mousemove', move);
        window.addEventListener('mouseup', up);
    };

    // ── Отрисовка ─────────────────────────────────────────────────────────

    /** Перерисовать к следующему кадру экрана. */
    ManualPanorama.prototype._kick = function() {
        if (!this._active || this._frameReq) { return; }
        const self = this;
        const run = function() {
            self._frameReq = null;
            self._tick();
        };
        this._frameReq = typeof requestAnimationFrame === 'function'
            ? requestAnimationFrame(run) : setTimeout(run, 16);
    };

    /** Что рисовать на момент nowMs. */
    ManualPanorama.prototype._scene = function(nowMs) {
        const stitcher = this._stitcher;
        const cur = this._currentWindow();
        const bandMarks = typeof window !== 'undefined' && window.BandMarks;
        const kindOf = bandMarks && bandMarks.markKind;
        const codeOf = bandMarks && bandMarks.markCode;
        return {
            axis: this._axis,
            view: this._viewAt(nowMs),
            layers: stitcher.layers(),
            currentIdx: cur ? stitcher.windows.indexOf(cur) : -1,
            range: this._range,
            marks: stitcher.marks().map(function(sig) {
                return {
                    freqMHz: sig.center_hz / 1e6,
                    bwMHz: (sig.bandwidth_hz || 0) / 1e6,
                    kind: kindOf ? kindOf(sig) : 'unknown',
                    code: codeOf ? codeOf(sig) : '',
                    sig: sig,
                };
            }),
            emptyText: this._emptyText,
        };
    };

    ManualPanorama.prototype._tick = function() {
        if (!this._active) { return; }
        const sc = this._scene(Date.now());
        if (this._fftView) {
            this._fftView.setScene(sc);
            this._fftView.draw();
        }
        if (this._ovView) { this._ovView.draw(sc); }
        if (this._wfView && sc.view) { this._wfView.render(sc.view, sc.range); }
        if (this._anim) { this._kick(); }
    };

    /**
     * Показывать ли follow-иконки (ротатор в «Авто»).
     * @param {boolean} on
     */
    ManualPanorama.prototype.setFollowEnabled = function() { /* иконка видна всегда */ };

    /**
     * Частота активного (ведомого) передатчика — подсветить его строку.
     * @param {?number} freqMHz
     */
    ManualPanorama.prototype.setFollowedFreq = function(freqMHz) {
        const f = (typeof freqMHz === 'number' && isFinite(freqMHz)) ? freqMHz : null;
        if (f === this._followedFreq) { return; }
        this._followedFreq = f;
        this._renderTxList();
    };

    // ── Список передатчиков (DOM) ─────────────────────────────────────────

    ManualPanorama.prototype._renderTxList = function() {
        const el = this._txListEl;
        if (!el) { return; }
        el.innerHTML = '';

        if (!this._transmitters || this._transmitters.length === 0) {
            el.innerHTML = '<p class="ml-txlist__placeholder">Нет данных о передатчиках</p>';
            return;
        }

        // Порядок ленты совпадает с порядком меток на панораме
        const txList = this._transmitters.slice().sort(function(a, b) { return a.freqMHz - b.freqMHz; });

        const self = this;
        for (let i = 0; i < txList.length; i++) {
            const tx = txList[i];
            const row = document.createElement('div');
            row.className = 'ml-tx-row' + (tx.active ? '' : ' ml-tx-row--silent');
            row.setAttribute('data-freq', tx.freqMHz);
            row.setAttribute('role', 'listitem');
            row.setAttribute('tabindex', '0');

            const isFollowed = this._isFollowed(tx.freqMHz);
            row.className += isFollowed ? ' ml-tx-row--followed' : '';

            const hintParts = [];
            if (!tx.active) { hintParts.push('Молчит'); }
            else if (isFollowed) { hintParts.push('Настроен'); }
            else { hintParts.push('Настроить'); }
            if (tx.description) { hintParts.push(tx.description); }
            row.setAttribute('title', hintParts.join('\n'));

            // В одну строку: LED, имя, частота, модуляция
            const led = document.createElement('span');
            led.className = 'ml-tx-row__led';
            if (!tx.active) {
                led.classList.add('ml-tx-row__led--grey');
            } else if (tx.chipColor) {
                // Палитра КА — произвольный hex; фиксированные классы cyan/purple не покрывают
                led.style.background = tx.chipColor;
            } else {
                led.classList.add('ml-tx-row__led--cyan');
            }
            row.appendChild(led);
            const name = document.createElement('span');
            name.className = 'ml-tx-row__name';
            name.textContent = tx.label || 'Unknown';
            row.appendChild(name);
            const freq = document.createElement('span');
            freq.className = 'ml-tx-row__freq';
            freq.textContent = tx.freqMHz.toFixed(3);
            row.appendChild(freq);
            const mod = document.createElement('span');
            mod.className = 'ml-tx-row__mod';
            mod.textContent = tx.modulation || '—';
            row.appendChild(mod);

            (function(txItem, rowEl) {
                rowEl.addEventListener('click', function() { self._onTxClick(txItem); });
                rowEl.addEventListener('dblclick', function() { self._onTxActivate(txItem); });
                rowEl.addEventListener('keydown', function(e) {
                    if (e.key !== 'Enter') { return; }
                    if (typeof e.preventDefault === 'function') { e.preventDefault(); }
                    self._onTxActivate(txItem);
                });
                rowEl.addEventListener('mouseenter', function() { self._setHighlight(txItem.freqMHz); });
                rowEl.addEventListener('mouseleave', function() { self._setHighlight(null); });
            })(tx, row);

            el.appendChild(row);
        }
        this._updateSelectedRow();
        this._updateHighlightedRow();
    };

    ManualPanorama.prototype._isFollowed = function(freqMHz) {
        return this._followedFreq !== null && Math.abs(freqMHz - this._followedFreq) < 1e-6;
    };

    /**
     * Двойной клик или Enter по элементу ленты: настроить приёмник.
     * Уже настроенный передатчик не сбрасывается, а перестраивается заново.
     */
    ManualPanorama.prototype._onTxActivate = function(tx) {
        if (!tx || typeof tx.freqMHz !== 'number') { return; }
        if (this._isFollowed(tx.freqMHz)) {
            this._onTxTune(tx);
        } else {
            this._onTxFollow(tx);
        }
    };

    /** Сдвиг ленты на большую часть видимой ширины: dir = -1 влево, 1 вправо. */
    ManualPanorama.prototype._scrollTxList = function(dir) {
        const el = this._txListEl;
        if (!el) { return; }
        const step = Math.max(120, Math.round((el.clientWidth || 0) * 0.8));
        el.scrollLeft = Math.max(0, el.scrollLeft + dir * step);
    };

    /** Настройка с сопровождением КА: наружу событие panorama:follow. */
    ManualPanorama.prototype._onTxFollow = function(tx) {
        if (!tx || typeof tx.freqMHz !== 'number') { return; }
        this._onTxClick(tx);
        if (typeof CustomEvent === 'function' && this._canvas) {
            this._canvas.dispatchEvent(new CustomEvent('panorama:follow', {
                bubbles: true,
                detail: { noradId: tx.noradId || null, freqMHz: tx.freqMHz, tx: tx },
            }));
        }
    };

    ManualPanorama.prototype._setHighlight = function(freqMHz) {
        this._highlightFreq = freqMHz;
        if (this._fftView) {
            this._fftView.setHighlight(freqMHz);
        }
        this._updateHighlightedRow();
        this._kick();
    };

    /** Подсвечивает элемент ленты, над меткой которого стоит курсор. */
    ManualPanorama.prototype._updateHighlightedRow = function() {
        const el = this._txListEl;
        if (!el) { return; }
        const rows = el.querySelectorAll('.ml-tx-row');
        for (let i = 0; i < rows.length; i++) {
            const freq = parseFloat(rows[i].getAttribute('data-freq'));
            rows[i].classList.toggle('ml-tx-row--active',
                this._highlightFreq !== null && freq === this._highlightFreq);
        }
    };

    /** Одиночный клик по метке — выделение (параметры видны в строке ленты). */
    ManualPanorama.prototype._onTxClick = function(tx) {
        if (!tx || typeof tx.freqMHz !== 'number') { return; }
        this._selectedFreq = tx.freqMHz;
        if (this._fftView) { this._fftView.setSelected(tx.freqMHz); }
        this._updateSelectedRow();
        this._scrollRowIntoView(tx.freqMHz);
        this._kick();
    };

    /**
     * Передатчик, выделенный одиночным кликом, если он ещё в списке.
     * @returns {Object|null}
     */
    ManualPanorama.prototype.getSelectedTx = function() {
        if (this._selectedFreq === null) { return null; }
        for (let i = 0; i < this._transmitters.length; i++) {
            if (this._transmitters[i].freqMHz === this._selectedFreq) { return this._transmitters[i]; }
        }
        return null;
    };

    /** Прокручивает ленту к выбранной метке — при клике по панораме. */
    ManualPanorama.prototype._scrollRowIntoView = function(freqMHz) {
        const el = this._txListEl;
        if (!el) { return; }
        const rows = el.querySelectorAll('.ml-tx-row');
        for (let i = 0; i < rows.length; i++) {
            if (parseFloat(rows[i].getAttribute('data-freq')) === freqMHz) {
                if (typeof rows[i].scrollIntoView === 'function') {
                    rows[i].scrollIntoView({ block: 'nearest', inline: 'nearest' });
                }
                return;
            }
        }
    };

    /** Двойной клик по метке — перенастройка активного канала (ADR-004 § 4.6, вариант A). */
    ManualPanorama.prototype._onTxTune = function(tx) {
        if (!tx || typeof tx.freqMHz !== 'number') { return; }
        this._onTxClick(tx);
        if (typeof CustomEvent === 'function' && this._canvas) {
            this._canvas.dispatchEvent(new CustomEvent('panorama:tune', {
                bubbles: true,
                detail: {
                    freqMHz: tx.freqMHz,
                    tx: tx,
                },
            }));
        }
    };

    ManualPanorama.prototype._updateSelectedRow = function() {
        const el = this._txListEl;
        if (!el) { return; }
        const rows = el.querySelectorAll('.ml-tx-row');
        for (let i = 0; i < rows.length; i++) {
            const row = rows[i];
            const freq = parseFloat(row.getAttribute('data-freq'));
            row.classList.toggle('ml-tx-row--selected', freq === this._selectedFreq);
        }
    };

    // ── Активация / деактивация ───────────────────────────────────────────

    ManualPanorama.prototype.activate = function() {
        if (this._active) { return; }
        this._active = true;
        const self = this;
        // Давность снимков на подписях окон обновляется раз в секунду
        this._timer = setInterval(function() { self._kick(); }, 1000);
        if (this._wfView) { this._wfView.invalidate(); }
        this._kick();
    };

    ManualPanorama.prototype.deactivate = function() {
        if (!this._active) { return; }
        this._active = false;
        if (this._timer) {
            clearInterval(this._timer);
            this._timer = null;
        }
    };

    ManualPanorama.prototype.isActive = function() {
        return this._active;
    };

    /** Обновление при смене темы. */
    ManualPanorama.prototype.refreshAfterThemeChange = function() {
        if (this._wfView) { this._wfView.invalidate(); }
        this._kick();
    };

    ManualPanorama.prototype.destroy = function() {
        this.deactivate();
        for (let i = 0; i < this._listeners.length; i++) {
            const l = this._listeners[i];
            l[0].removeEventListener(l[1], l[2], l[3]);
        }
        this._listeners = [];
        for (let i = 0; i < this._resizeObservers.length; i++) {
            this._resizeObservers[i].disconnect();
        }
        this._resizeObservers = [];
        this._fftView = null;
        this._wfView = null;
        this._ovView = null;
    };

    // ── Экспорт ───────────────────────────────────────────────────────────

    if (typeof window !== 'undefined') {
        window.ManualPanorama = ManualPanorama;
        window.PanoramaFFTView = PanoramaFFTView;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            ManualPanorama, PanoramaFFTView, PanoramaStitcher, PanoramaWaterfallView, PanoramaAxis,
            zoomView, panView, easeView, windowColumns, resampleMax, stitchWeight, blendTraces,
            timeAxisStepSec, formatAgo,
        };
    }
})();
