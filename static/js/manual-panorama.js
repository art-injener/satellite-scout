// manual-panorama.js — Блок «Панорама эфира» Ручного режима (ADR-004 § 4.4).
//
// FFT canvas: склейка нескольких span (полос обзора) через разделитель «≈».
// Чипы-маркеры (рамка + пунктир/сплошная линия) над известными несущими.
// Под графиком водопад обходов с той же раскладкой полос по X.
// Лента «Передатчики» (DOM): одна строка по возрастанию частоты, прокрутка
// колесом, стрелками и ползунком. Клик — выбор, двойной клик или Enter — настройка.
// Наведение связывает элемент ленты и метку на FFT в обе стороны.
//
// Данные: mock (имитатор PanoramaDataSource). Реальный SDR — после DWELL-001.
// Источник передатчиков: пока mock-массив. Реальный — TX-DB-001.

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

    // ── Имитатор данных панорамы (несколько span) ────────────────────

    /** Генерирует шум + пики на заданных частотах для каждого span. */
    function PanoramaDataSource(opts) {
        opts = opts || {};
        this.spans = opts.spans || [
            { minMHz: 144.0, maxMHz: 148.0 },
            { minMHz: 430.0, maxMHz: 440.0 },
        ];
        this._binsPerSpan = opts.binsPerSpan || 256;
        this._totalBins = this._binsPerSpan * this.spans.length;
        this._buf = new Float32Array(this._totalBins);
        this._transmitters = opts.transmitters || [];
        this._avgAlpha = 0.3;
    }

    PanoramaDataSource.prototype.generateLine = function() {
        const totalBins = this._totalBins;
        const binsPerSpan = this._binsPerSpan;
        const spans = this.spans;

        for (let i = 0; i < totalBins; i++) {
            const noiseFloor = -90 + (Math.random() - 0.5) * 6;
            const spanIdx = Math.floor(i / binsPerSpan);
            const binInSpan = i - spanIdx * binsPerSpan;
            const span = spans[spanIdx];
            const freqMHz = span.minMHz + (binInSpan / (binsPerSpan - 1)) * (span.maxMHz - span.minMHz);
            let signal = noiseFloor;

            for (let t = 0; t < this._transmitters.length; t++) {
                const tx = this._transmitters[t];
                if (!tx.active) { continue; }
                const dist = Math.abs(freqMHz - tx.freqMHz);
                const bw = tx.bandwidthMHz || 0.025;
                if (dist < bw * 3) {
                    const snr = tx.snrDb || 25;
                    signal = Math.max(signal, noiseFloor + snr * Math.exp(-(dist * dist) / (2 * bw * bw * 0.18)));
                }
            }

            const norm = Math.max(0, Math.min(1, (signal + 100) / 100));
            this._buf[i] = this._buf[i] * (1 - this._avgAlpha) + norm * this._avgAlpha;
        }
    };

    PanoramaDataSource.prototype.getLine = function() {
        return this._buf;
    };

    // ── Canvas: FFT панорамы (multi-span + чипы) ──────────────────────────

    function PanoramaFFTView(canvas, opts) {
        opts = opts || {};
        this._canvas = canvas;
        this._ctx = canvas.getContext('2d');
        this.spans = opts.spans || [
            { minMHz: 144.0, maxMHz: 148.0 },
            { minMHz: 430.0, maxMHz: 440.0 },
        ];
        this._binsPerSpan = opts.binsPerSpan || 256;
        this._marginLeft = 32;
        this._marginRight = 8;
        // Сверху — только треугольные маркеры и подпись выбранной метки.
        // Имена и частоты всех передатчиков читаются в легенде средней колонки,
        // поэтому графику отдана почти вся высота панели.
        this._marginTop = 24;
        this._marginBottom = 22;
        this._gapPx = 20;
        this._dbMin = -100;
        this._dbMax = 0;
        this._transmitters = [];
        this._highlightFreq = null;
        this._selectedFreq = null;
        // Позиции маркеров: {x, tx, freqMHz}. Попадание считаем по близости к
        // вертикали метки — целиться в маленький треугольник неудобно.
        this._txMarks = [];
        this._plotTop = this._marginTop;
    }

    PanoramaFFTView.prototype.setTransmitters = function(txList) {
        this._transmitters = txList || [];
    };

    PanoramaFFTView.prototype.setHighlight = function(freqMHz) {
        this._highlightFreq = freqMHz;
    };

    PanoramaFFTView.prototype.setSelected = function(freqMHz) {
        this._selectedFreq = freqMHz;
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
        const TOLERANCE_PX = 7;
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

    PanoramaFFTView.prototype._resize = function() {
        const rect = this._canvas.getBoundingClientRect();
        const w = Math.floor(rect.width) || this._canvas.offsetWidth || 400;
        const h = Math.floor(rect.height) || this._canvas.offsetHeight || 200;
        if (w < 2 || h < 2) { return; }
        if (this._canvas.width !== w || this._canvas.height !== h) {
            this._canvas.width = w;
            this._canvas.height = h;
        }
    };

    /** X-координата частоты → пиксель (с учётом разрывов между span). */
    PanoramaFFTView.prototype._freqToX = function(freqMHz, spanIdx, plotX, spanWidths) {
        let offset = plotX;
        for (let b = 0; b < spanIdx; b++) {
            offset += spanWidths[b] + this._gapPx;
        }
        const span = this.spans[spanIdx];
        const frac = (freqMHz - span.minMHz) / (span.maxMHz - span.minMHz);
        return offset + frac * spanWidths[spanIdx];
    };

    PanoramaFFTView.prototype.draw = function(buf) {
        this._resize();
        const w = this._canvas.width;
        const h = this._canvas.height;
        if (w < 40 || h < 40) { return; }

        const ctx = this._ctx;
        const ml = this._marginLeft;
        const mr = this._marginRight;
        const mt = this._marginTop;
        const mb = this._marginBottom;
        const plotW = w - ml - mr;
        const plotH = h - mt - mb;
        if (plotW < 10 || plotH < 10) { return; }

        const spans = this.spans;
        const numSpans = spans.length;
        const gapPx = this._gapPx;
        const totalGap = (numSpans - 1) * gapPx;
        let totalWidthMHz = 0;
        for (let b = 0; b < numSpans; b++) {
            totalWidthMHz += spans[b].maxMHz - spans[b].minMHz;
        }

        const spanWidths = [];
        const usableW = plotW - totalGap;
        for (let b2 = 0; b2 < numSpans; b2++) {
            const widthMHz = spans[b2].maxMHz - spans[b2].minMHz;
            spanWidths.push((widthMHz / totalWidthMHz) * usableW);
        }

        const plotBg = cssVar('--spectrum-plot-bg', cssVar('--spectrum-bg', '#0c1420'));
        const gridCol = cssVar('--spectrum-plot-grid', 'rgba(255,255,255,0.08)');
        const axisText = cssVar('--spectrum-axis-text', cssVar('--text-muted', '#c8d0d8'));
        const lineColor = cssVar('--spectrum-trace', '#5ee878');
        const dbRange = this._dbMax - this._dbMin;
        const dbStep = dbRange <= 60 ? 10 : 20;

        ctx.fillStyle = plotBg;
        ctx.fillRect(0, 0, w, h);

        // ── Сетка dB ──
        ctx.save();
        ctx.strokeStyle = gridCol;
        ctx.setLineDash([4, 4]);
        ctx.lineWidth = 1;
        for (let db = this._dbMin + dbStep; db < this._dbMax; db += dbStep) {
            const yGrid = mt + plotH * (1 - (db - this._dbMin) / dbRange);
            ctx.beginPath();
            ctx.moveTo(ml, yGrid);
            ctx.lineTo(w - mr, yGrid);
            ctx.stroke();
        }
        ctx.restore();

        // Подписи dB
        ctx.save();
        ctx.font = '9px monospace';
        ctx.fillStyle = axisText;
        ctx.textAlign = 'right';
        ctx.textBaseline = 'middle';
        for (let dbL = this._dbMin; dbL <= this._dbMax; dbL += dbStep) {
            const yLabel = mt + plotH * (1 - (dbL - this._dbMin) / dbRange);
            ctx.fillText(String(dbL), ml - 4, yLabel);
        }
        ctx.restore();

        // Сохраняем метрики для хит-теста меток
        this._plotTop = mt;

        // ── Вертикальная сетка частот (по тикам внутри каждого span) ──
        ctx.save();
        ctx.strokeStyle = gridCol;
        ctx.setLineDash([2, 5]);
        ctx.lineWidth = 1;
        for (let vgi = 0; vgi < numSpans; vgi++) {
            const vgSpan = spans[vgi];
            let vgSpanX = ml;
            for (let vgk = 0; vgk < vgi; vgk++) {
                vgSpanX += spanWidths[vgk] + gapPx;
            }
            const vgSpanW = spanWidths[vgi];
            const vgWidthMHz = vgSpan.maxMHz - vgSpan.minMHz;
            const vgStep = calcFreqScaleStep(vgWidthMHz, vgSpanW, 60);
            for (let vf = Math.ceil(vgSpan.minMHz / vgStep) * vgStep; vf <= vgSpan.maxMHz; vf += vgStep) {
                const vgx = vgSpanX + ((vf - vgSpan.minMHz) / vgWidthMHz) * vgSpanW;
                ctx.beginPath();
                ctx.moveTo(vgx, mt);
                ctx.lineTo(vgx, mt + plotH);
                ctx.stroke();
            }
        }
        ctx.restore();

        // ── Рисуем FFT-линию по span ──
        const binsPerSpan = this._binsPerSpan;
        ctx.save();
        ctx.strokeStyle = lineColor;
        ctx.lineWidth = 1.5;
        ctx.lineJoin = 'round';

        for (let bi = 0; bi < numSpans; bi++) {
            const bStart = bi * binsPerSpan;
            let spanX = ml;
            for (let bk = 0; bk < bi; bk++) {
                spanX += spanWidths[bk] + gapPx;
            }
            const spanW = spanWidths[bi];

            ctx.beginPath();
            for (let p = 0; p < binsPerSpan; p++) {
                const xPt = spanX + (p / (binsPerSpan - 1)) * spanW;
                const vv = Math.max(0, Math.min(1, buf[bStart + p]));
                const yPt = mt + plotH * (1 - vv);
                if (p === 0) { ctx.moveTo(xPt, yPt); }
                else { ctx.lineTo(xPt, yPt); }
            }
            ctx.stroke();
        }
        ctx.restore();

        // ── Разделитель «≈» между span ──
        if (numSpans > 1) {
            ctx.save();
            ctx.font = '14px monospace';
            ctx.fillStyle = axisText;
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            let gapX = ml;
            for (let g = 0; g < numSpans - 1; g++) {
                gapX += spanWidths[g];
                const cx = gapX + gapPx / 2;
                ctx.fillText('≈', cx, mt + plotH / 2);
                gapX += gapPx;
            }
            ctx.restore();
        }

        // ── Шкала частот (внизу) ──
        ctx.save();
        ctx.font = '10px monospace';
        ctx.fillStyle = axisText;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';

        for (let fi = 0; fi < numSpans; fi++) {
            const span = spans[fi];
            let fSpanX = ml;
            for (let fk = 0; fk < fi; fk++) {
                fSpanX += spanWidths[fk] + gapPx;
            }
            const fSpanW = spanWidths[fi];
            const widthMHz = span.maxMHz - span.minMHz;
            const step = calcFreqScaleStep(widthMHz, fSpanW, 60);

            for (let f = Math.ceil(span.minMHz / step) * step; f <= span.maxMHz; f += step) {
                const xF = fSpanX + ((f - span.minMHz) / widthMHz) * fSpanW;
                ctx.fillText(f.toFixed(1), xF, h - mb + 4);
            }
        }
        ctx.restore();

        // Подпись «MHz» справа
        ctx.save();
        ctx.font = '9px monospace';
        ctx.fillStyle = axisText;
        ctx.textAlign = 'right';
        ctx.textBaseline = 'top';
        ctx.fillText('MHz', w - 2, h - mb + 4);
        ctx.restore();

        // ── Чипы-маркеры передатчиков ──
        this._drawChips(ctx, ml, mt, plotH, spanWidths);
    };

    /**
     * Метки передатчиков: пунктирная вертикаль + треугольник сверху.
     * Подпись рисуется только для выбранной или подсвеченной метки — имена и
     * частоты всех передатчиков и так перечислены в легенде, а постоянные
     * таблички отнимали у спектра больше высоты, чем сам сигнал.
     */
    PanoramaFFTView.prototype._drawChips = function(ctx, plotX, plotTop, plotH, spanWidths) {
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
            const spanIdx = this._findSpan(tx.freqMHz);
            if (spanIdx < 0) { continue; }
            const x = this._freqToX(tx.freqMHz, spanIdx, plotX, spanWidths);
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

    // ── Водопад панорамы ──────────────────────────────────────────────────

    /**
     * Какой бин строки панорамы показывать в каждом столбце холста.
     * Раскладка по X та же, что у графика: поля слева и справа, полосы
     * пропорционально ширине в МГц, между ними разрыв. Поля и разрывы — -1.
     * @returns {Int32Array}
     */
    function panoramaColumnBins(width, marginLeft, marginRight, gapPx, spans, binsPerSpan) {
        const cols = new Int32Array(Math.max(0, width)).fill(-1);
        const plotW = width - marginLeft - marginRight;
        const usableW = plotW - (spans.length - 1) * gapPx;
        if (usableW <= 0) { return cols; }
        let totalMHz = 0;
        for (let i = 0; i < spans.length; i++) { totalMHz += spans[i].maxMHz - spans[i].minMHz; }
        let spanX = marginLeft;
        for (let s = 0; s < spans.length; s++) {
            const spanW = ((spans[s].maxMHz - spans[s].minMHz) / totalMHz) * usableW;
            const x0 = Math.ceil(spanX);
            const x1 = Math.min(width, Math.ceil(spanX + spanW));
            for (let x = x0; x < x1; x++) {
                const frac = Math.max(0, Math.min(1, (x - spanX) / spanW));
                cols[x] = s * binsPerSpan + Math.round(frac * (binsPerSpan - 1));
            }
            spanX += spanW + gapPx;
        }
        return cols;
    }

    /**
     * Водопад обходов: строка на цикл обхода, свежая сверху. Столбцы совпадают
     * с графиком панорамы, поэтому несущая на графике и её след в водопаде
     * стоят на одной вертикали.
     */
    function PanoramaWaterfallView(canvas, opts) {
        opts = opts || {};
        this._canvas = canvas;
        this._ctx = canvas.getContext('2d');
        this.spans = opts.spans;
        this._binsPerSpan = opts.binsPerSpan || 256;
        this._marginLeft = 32;
        this._marginRight = 8;
        this._gapPx = 20;
        this._tickMs = opts.tickMs || PANORAMA_TICK_MS;
        this._colormap = opts.colormap || null;
        this._marginRgb = opts.marginRgb || null;
        this._imageData = null;
        this._cols = null;
    }

    PanoramaWaterfallView.prototype._resize = function() {
        const rect = this._canvas.getBoundingClientRect();
        const w = Math.floor(rect.width);
        const h = Math.floor(rect.height);
        if (w < 2 || h < 2) { return false; }
        if (this._canvas.width !== w || this._canvas.height !== h || !this._imageData) {
            this._canvas.width = w;
            this._canvas.height = h;
            this._imageData = this._ctx.createImageData(w, h);
            this._cols = panoramaColumnBins(w, this._marginLeft, this._marginRight,
                this._gapPx, this.spans, this._binsPerSpan);
            const bg = this._marginColor();
            const d = this._imageData.data;
            for (let j = 0; j < d.length; j += 4) {
                d[j] = bg[0]; d[j + 1] = bg[1]; d[j + 2] = bg[2]; d[j + 3] = 255;
            }
        }
        return true;
    };

    PanoramaWaterfallView.prototype._marginColor = function() {
        if (this._marginRgb) { return this._marginRgb; }
        return rgbFromHex(cssVar('--waterfall-margin-bg', '#000000'));
    };

    /** Добавляет строку обхода сверху, старые строки сдвигаются вниз. */
    PanoramaWaterfallView.prototype.pushLine = function(buf) {
        if (!this._resize()) { return; }
        const w = this._canvas.width;
        const h = this._canvas.height;
        const data = this._imageData.data;
        data.copyWithin(w * 4, 0, (h - 1) * w * 4);

        const cmap = this._colormap ||
            (typeof window !== 'undefined' && typeof window.waterfallColormap === 'function'
                ? window.waterfallColormap()
                : function(v) { const c = Math.round(v * 255); return [c, c, c]; });
        const bg = this._marginColor();
        const cols = this._cols;
        for (let x = 0; x < w; x++) {
            const i = x * 4;
            const bin = cols[x];
            const c = bin < 0 ? bg : cmap(Math.max(0, Math.min(1, buf[bin])));
            data[i] = c[0];
            data[i + 1] = c[1];
            data[i + 2] = c[2];
            data[i + 3] = 255;
        }
        this._ctx.putImageData(this._imageData, 0, 0);
        this._drawTimeAxis();
    };

    /** Шкала времени в левом поле: сверху 0 с, ниже — давность обхода. */
    PanoramaWaterfallView.prototype._drawTimeAxis = function() {
        const ctx = this._ctx;
        const h = this._canvas.height;
        const totalSec = (h * this._tickMs) / 1000;
        const stepSec = totalSec <= 20 ? 5 : (totalSec <= 60 ? 10 : 30);
        ctx.font = '9px monospace';
        ctx.fillStyle = cssVar('--spectrum-axis-text', cssVar('--text-muted', '#c8d0d8'));
        ctx.textAlign = 'right';
        ctx.textBaseline = 'top';
        for (let sec = 0; sec < totalSec; sec += stepSec) {
            const y = Math.round((sec * 1000) / this._tickMs);
            ctx.fillText(sec + 's', this._marginLeft - 4, Math.min(y, h - 10));
        }
    };

    /** Определяет, в какой span попадает частота. */
    PanoramaFFTView.prototype._findSpan = function(freqMHz) {
        for (let i = 0; i < this.spans.length; i++) {
            if (freqMHz >= this.spans[i].minMHz && freqMHz <= this.spans[i].maxMHz) {
                return i;
            }
        }
        return -1;
    };

    // ── Контроллер Панорамы (связывает canvas, список TX, данные) ──────────

    /** Период обновления панорамы (мс). */
    const PANORAMA_TICK_MS = 200;

    /**
     * ManualPanorama — контроллер блока «Панорама эфира».
     * @param {Object} opts
     * @param {HTMLCanvasElement} opts.canvas — #manual-panorama-fft
     * @param {HTMLElement} opts.txListEl — #manual-tx-scroll (контейнер ленты)
     * @param {HTMLElement} opts.prevBtn — #manual-tx-prev (прокрутка ленты влево)
     * @param {HTMLElement} opts.nextBtn — #manual-tx-next (прокрутка ленты вправо)
     * @param {Array} opts.spans — [{minMHz, maxMHz}, ...]
     * @param {Array} opts.transmitters — [{freqMHz, label, active, ...}, ...]
     */
    function ManualPanorama(opts) {
        opts = opts || {};
        this._active = false;
        this._timer = null;
        this._highlightFreq = null;
        this._selectedFreq = null;

        const defaultSpans = [
            { minMHz: 144.0, maxMHz: 148.0 },
            { minMHz: 430.0, maxMHz: 440.0 },
        ];
        const spans = opts.spans || defaultSpans;

        // Mock-список передатчиков для разработки
        const defaultTx = [
            { freqMHz: 145.825, label: 'ISS-VOICE', modulation: 'FM', active: true, chipColor: '#00ffc8', noradId: 25544 },
            { freqMHz: 437.800, label: 'ISS-FSK', modulation: 'FSK 1k2', active: true, chipColor: '#d070ff', noradId: 25544 },
            { freqMHz: 435.250, label: 'AO-91 BPSK', modulation: 'BPSK31', active: true, chipColor: '#ffaa22', noradId: 43017 },
            { freqMHz: 145.920, label: 'RS-44 CW', modulation: 'CW', active: false, chipColor: '#8fa8bc', noradId: 44909 },
            { freqMHz: 437.100, label: 'NOAA 18 APT', modulation: 'APT', active: false, chipColor: '#8fa8bc', noradId: 28654 },
        ];
        this._transmitters = opts.transmitters || defaultTx;

        // Canvas FFT
        this._canvas = opts.canvas || document.getElementById('manual-panorama-fft');
        this._fftView = null;
        if (this._canvas) {
            this._fftView = new PanoramaFFTView(this._canvas, {
                spans: spans,
                binsPerSpan: 256,
            });
            this._fftView.setTransmitters(this._transmitters);
            this._fftView.setSelected(this._selectedFreq);
        }

        // Водопад обходов под графиком
        const wfCanvas = opts.wfCanvas || document.getElementById('manual-panorama-wf');
        this._wfView = wfCanvas
            ? new PanoramaWaterfallView(wfCanvas, { spans: spans, binsPerSpan: 256 })
            : null;

        // Источник данных
        this._dataSource = new PanoramaDataSource({
            spans: spans,
            binsPerSpan: 256,
            transmitters: this._transmitters,
        });

        // Лента передатчиков — подписи к панораме и таймлайну.
        // Заменяет всплывающую справку: параметры метки читаются в элементе ленты.
        this._txListEl = opts.txListEl || document.getElementById('manual-tx-scroll');
        this._prevBtn = opts.prevBtn || document.getElementById('manual-tx-prev');
        this._nextBtn = opts.nextBtn || document.getElementById('manual-tx-next');
        // Частота передатчика, на который настроен приёмник: его элемент в рамке.
        this._followedFreq = null;

        const self = this;
        if (this._txListEl) {
            // Обычное колесо крутит ленту вбок; горизонтальную прокрутку
            // тачпада браузер обрабатывает сам
            this._onTxWheel = function(e) {
                if (!e.deltaY || Math.abs(e.deltaX || 0) >= Math.abs(e.deltaY)) { return; }
                e.preventDefault();
                self._txListEl.scrollLeft += e.deltaY;
            };
            this._txListEl.addEventListener('wheel', this._onTxWheel, { passive: false });
        }
        this._onTxPrev = function() { self._scrollTxList(-1); };
        this._onTxNext = function() { self._scrollTxList(1); };
        if (this._prevBtn) { this._prevBtn.addEventListener('click', this._onTxPrev); }
        if (this._nextBtn) { this._nextBtn.addEventListener('click', this._onTxNext); }
        this._renderTxList();

        // Интерактив по канвасу: клик — выбор метки, двойной клик — настройка приёмника
        if (this._canvas) {
            this._onCanvasClick = function(e) {
                if (!self._fftView) { return; }
                const hit = self._fftView.hitTest(e.offsetX, e.offsetY);
                if (hit) { self._onTxClick(hit); }
            };
            this._onCanvasDblClick = function(e) {
                if (!self._fftView) { return; }
                const hit = self._fftView.hitTest(e.offsetX, e.offsetY);
                if (hit) { self._onTxTune(hit); }
            };
            this._onCanvasMove = function(e) {
                if (!self._fftView) { return; }
                const hit = self._fftView.hitTest(e.offsetX, e.offsetY);
                self._canvas.style.cursor = hit ? 'pointer' : 'default';
                self._setHighlight(hit ? hit.freqMHz : null);
            };
            this._canvas.addEventListener('click', this._onCanvasClick);
            this._canvas.addEventListener('dblclick', this._onCanvasDblClick);
            this._canvas.addEventListener('mousemove', this._onCanvasMove);
        }

        // ResizeObserver
        this._resizeObservers = [];
        if (this._canvas && typeof ResizeObserver !== 'undefined') {
            const ro = new ResizeObserver(function() {
                if (self._fftView) { self._fftView._resize(); }
            });
            ro.observe(this._canvas.parentElement || this._canvas);
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
        if (this._dataSource) { this._dataSource._transmitters = this._transmitters; }
        this._renderTxList();
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

            const hint = !tx.active ? 'Молчит' : (isFollowed ? 'Настроен' : 'Настроить');
            row.setAttribute('title', hint);

            // В одну строку: LED, имя, частота, модуляция
            const led = document.createElement('span');
            led.className = 'ml-tx-row__led';
            led.classList.add(tx.active ? 'ml-tx-row__led--' + this._ledClass(tx.chipColor) : 'ml-tx-row__led--grey');
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

    /** Определяет CSS-класс LED по цвету чипа. */
    ManualPanorama.prototype._ledClass = function(chipColor) {
        if (!chipColor) { return 'cyan'; }
        const c = String(chipColor).toLowerCase();
        if (c.indexOf('d070') >= 0 || c.indexOf('a855') >= 0 || c.indexOf('55f7') >= 0) { return 'purple'; }
        if (c.indexOf('ffaa') >= 0 || c.indexOf('ffb3') >= 0 || c.indexOf('b347') >= 0) { return 'amber'; }
        if (c.indexOf('00ff') >= 0 || c.indexOf('00d4') >= 0 || c.indexOf('ffc8') >= 0) { return 'cyan'; }
        return 'grey';
    };

    ManualPanorama.prototype._setHighlight = function(freqMHz) {
        this._highlightFreq = freqMHz;
        if (this._fftView) {
            this._fftView.setHighlight(freqMHz);
        }
        this._updateHighlightedRow();
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

    /** Одиночный клик по метке → выделение (параметры видны в строке легенды). */
    ManualPanorama.prototype._onTxClick = function(tx) {
        if (!tx || typeof tx.freqMHz !== 'number') { return; }
        this._selectedFreq = tx.freqMHz;
        if (this._fftView) { this._fftView.setSelected(tx.freqMHz); }
        this._updateSelectedRow();
        this._scrollRowIntoView(tx.freqMHz);
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

    /** Двойной клик по метке → перенастройка активного канала (ADR-004 § 4.6, вариант A). */
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
        this._timer = setInterval(function() { self._tick(); }, PANORAMA_TICK_MS);
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

    ManualPanorama.prototype._tick = function() {
        if (!this._active || !this._dataSource || !this._fftView) { return; }
        this._dataSource.generateLine();
        const line = this._dataSource.getLine();
        this._fftView.draw(line);
        // В имитаторе такт и есть цикл обхода всех полос
        if (this._wfView) { this._wfView.pushLine(line); }
    };

    /** Обновление при смене темы. */
    ManualPanorama.prototype.refreshAfterThemeChange = function() {
        if (this._active && this._dataSource && this._fftView) {
            this._dataSource.generateLine();
            this._fftView.draw(this._dataSource.getLine());
        }
    };

    ManualPanorama.prototype.destroy = function() {
        this.deactivate();
        if (this._canvas) {
            if (this._onCanvasClick) { this._canvas.removeEventListener('click', this._onCanvasClick); }
            if (this._onCanvasDblClick) { this._canvas.removeEventListener('dblclick', this._onCanvasDblClick); }
            if (this._onCanvasMove) { this._canvas.removeEventListener('mousemove', this._onCanvasMove); }
        }
        if (this._txListEl && this._onTxWheel) {
            this._txListEl.removeEventListener('wheel', this._onTxWheel);
        }
        if (this._prevBtn) { this._prevBtn.removeEventListener('click', this._onTxPrev); }
        if (this._nextBtn) { this._nextBtn.removeEventListener('click', this._onTxNext); }
        for (let i = 0; i < this._resizeObservers.length; i++) {
            this._resizeObservers[i].disconnect();
        }
        this._resizeObservers = [];
        this._fftView = null;
        this._wfView = null;
        this._dataSource = null;
    };

    // ── Экспорт ───────────────────────────────────────────────────────────

    if (typeof window !== 'undefined') {
        window.ManualPanorama = ManualPanorama;
        window.PanoramaFFTView = PanoramaFFTView;
        window.PanoramaDataSource = PanoramaDataSource;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            ManualPanorama, PanoramaFFTView, PanoramaDataSource, PanoramaWaterfallView, panoramaColumnBins,
        };
    }
})();
