// Нижняя панель: вкладки «Обзор» / «Сопровождение».
// Компоненты: SpectrumDataSource, WaterfallView, FFTSpectrumView, BottomPanel.

(function() {
    'use strict';

    // ════════════════════════════════════════════════════════════════════════
    // Общие утилиты
    // ════════════════════════════════════════════════════════════════════════

    /**
     * Единая приглушённая палитра водопада (UX-COLORS-INSTRUMENT-001).
     * От тёмного слейта (#242c34, согласован с --waterfall-cold-bg) через
     * глубокий синий → зелёный → янтарный → персиковый.
     * Применяется во всех темах для единообразия и комфорта глаз.
     */
    function hotColorMuted(v) {
        v = Math.max(0, Math.min(1, v));
        let r; let g; let b;
        if (v < 0.22) {
            const t = v / 0.22;
            r = Math.round(36 + t * 28);
            g = Math.round(44 + t * 40);
            b = Math.round(52 + t * 95);
        } else if (v < 0.45) {
            const t = (v - 0.22) / 0.23;
            r = Math.round(64 + t * 55);
            g = Math.round(84 + t * 125);
            b = Math.round(147 - t * 75);
        } else if (v < 0.68) {
            const t = (v - 0.45) / 0.23;
            r = Math.round(119 + t * 80);
            g = Math.round(209 - t * 55);
            b = Math.round(72 - t * 32);
        } else if (v < 0.88) {
            const t = (v - 0.68) / 0.2;
            r = Math.round(199 + t * 28);
            g = Math.round(154 - t * 48);
            b = Math.round(40 + t * 28);
        } else {
            const t = (v - 0.88) / 0.12;
            r = Math.round(227 + t * 18);
            g = Math.round(106 + t * 42);
            b = Math.round(68 + t * 38);
        }
        return [r, g, b];
    }

    function hotColor(v) {
        return hotColorMuted(v);
    }

    /** RGB из CSS-переменной вида #rrggbb (водопад, поля canvas). */
    function cssVarRgbHex(name, fallbackHex) {
        const raw = typeof window.cssVar === 'function' ? window.cssVar(name, fallbackHex) : fallbackHex;
        const s = String(raw || fallbackHex).trim();
        if (s.charAt(0) === '#') {
            let h = s.slice(1);
            if (h.length === 3) {
                h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
            }
            if (h.length === 6) {
                return [
                    parseInt(h.slice(0, 2), 16),
                    parseInt(h.slice(2, 4), 16),
                    parseInt(h.slice(4, 6), 16)
                ];
            }
        }
        return [0, 0, 0];
    }

    /**
     * Подбор «красивого» шага для шкалы частот.
     * @param {number} spanMHz — ширина полосы (МГц)
     * @param {number} widthPx — ширина области (пиксели)
     * @param {number} [minSpacingPx=52] — минимальное расстояние между метками
     * @returns {number} шаг (МГц)
     */
    function calcFreqScaleStep(spanMHz, widthPx, minSpacingPx) {
        minSpacingPx = minSpacingPx || 52;
        const maxTicks = Math.max(2, Math.floor(widthPx / minSpacingPx));
        const idealStep = spanMHz / maxTicks;
        const niceSteps = [0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.25, 0.5, 1, 2];
        for (let i = 0; i < niceSteps.length; i++) {
            if (niceSteps[i] >= idealStep) { return niceSteps[i]; }
        }
        return niceSteps[niceSteps.length - 1];
    }

    // cssVar() определена глобально в css-vars.js

    // ════════════════════════════════════════════════════════════════════════
    // SpectrumDataSource — генератор спектральных данных (шум + сигнал)
    // ════════════════════════════════════════════════════════════════════════

    function SpectrumDataSource(opts) {
        opts = opts || {};
        this._bins = opts.bins || 512;
        this._buf = new Float32Array(this._bins);
        this.freqCenterMHz = opts.freqCenterMHz || 437.365;
        this.freqSpanMHz = opts.freqSpanMHz || 0.192;

        // Имитация доплеровского сдвига LEO-пролёта
        this._passDurationTicks = opts.passDurationTicks || 600;
        this._gapTicks = opts.gapTicks || 150;
        this._tick = 0;
        this._inPass = true;

        // Макс. доплеровский сдвиг ±maxShift (в долях полосы, ~±8 кГц при 192 кГц)
        this._maxShift = opts.maxShift || 0.042;
        // Крутизна S-кривой: чем больше, тем резче переход у TCA
        this._slopeK = opts.slopeK || 6.0;
    }

    // Доплеровская S-кривая: f(t) = -maxShift * (2/π) * atan(k * (t - 0.5))
    // t ∈ [0, 1] — прогресс пролёта (0 = AOS, 0.5 = TCA, 1 = LOS)
    SpectrumDataSource.prototype._dopplerOffset = function(t) {
        return -this._maxShift * (2 / Math.PI) * Math.atan(this._slopeK * (t - 0.5));
    };

    // Амплитуда сигнала по колоколу: максимум в TCA, затухание к AOS/LOS
    SpectrumDataSource.prototype._signalAmplitude = function(t) {
        const d = (t - 0.5) / 0.35;
        return 0.75 * Math.exp(-0.5 * d * d);
    };

    // Заполняет буфер: шумовой пол + гауссов пик с доплеровским сдвигом
    SpectrumDataSource.prototype.generateLine = function() {
        const bins = this._bins;
        const buf = this._buf;

        if (!this._inPass) {
            for (let n = 0; n < bins; n++) {
                buf[n] = 0.04 + Math.random() * 0.06;
            }
            this._tick++;
            if (this._tick >= this._gapTicks) {
                this._tick = 0;
                this._inPass = true;
            }
            return;
        }

        const t = this._tick / this._passDurationTicks;
        const signalCenter = 0.5 + this._dopplerOffset(t);
        const amplitude = this._signalAmplitude(t);
        const signalWidth = 0.025 + 0.008 * (1 - amplitude);

        for (let i = 0; i < bins; i++) {
            const fx = i / bins;
            const noise = 0.04 + Math.random() * 0.06;
            const dist = (fx - signalCenter) / signalWidth;
            const signal = amplitude * Math.exp(-0.5 * dist * dist) * (0.85 + Math.random() * 0.3);
            buf[i] = noise + signal;
        }

        this._tick++;
        if (this._tick >= this._passDurationTicks) {
            this._tick = 0;
            this._inPass = false;
        }
    };

    // Сброс имитации пролёта (при переключении спутника)
    SpectrumDataSource.prototype.reset = function() {
        this._tick = 0;
        this._inPass = true;
    };

    // Полная строка (все bins)
    SpectrumDataSource.prototype.getLine = function() {
        return this._buf;
    };

    // Срез вокруг centerBin шириной widthBins (узкополосный водопад вкладки «Сопровождение»)
    SpectrumDataSource.prototype.getSlice = function(centerBin, widthBins) {
        const half = Math.floor(widthBins / 2);
        const start = centerBin - half;
        const out = new Float32Array(widthBins);
        for (let i = 0; i < widthBins; i++) {
            const srcIdx = start + i;
            if (srcIdx >= 0 && srcIdx < this._bins) {
                out[i] = this._buf[srcIdx];
            } else {
                out[i] = 0.04 + Math.random() * 0.03;
            }
        }
        return out;
    };

    // ════════════════════════════════════════════════════════════════════════
    // WaterfallView — отрисовка водопада (ImageData, scroll, hot colormap)
    // ════════════════════════════════════════════════════════════════════════

    function WaterfallView(canvas, scaleCanvas, opts) {
        opts = opts || {};
        this._canvas = canvas;
        this._scaleCanvas = scaleCanvas || null;
        this._ctx = canvas.getContext('2d');
        this._imageData = null;
        this._running = false;
        this._freqCenterMHz = opts.freqCenterMHz || 437.365;
        this._freqSpanMHz = opts.freqSpanMHz || 0.192;
        this._headerFreqId = opts.headerFreqId || null;
        this._headerResId = opts.headerResId || null;
        this._marginLeft = opts.marginLeft || 0;
        this._marginRight = opts.marginRight || 0;
    }

    // Подгонка размеров canvas под контейнер
    WaterfallView.prototype._resize = function() {
        const rect = this._canvas.getBoundingClientRect();
        let w = Math.floor(rect.width);
        let h = Math.floor(rect.height);
        if (w < 10) { w = this._canvas.offsetWidth || 300; }
        if (h < 10) { h = this._canvas.offsetHeight || 150; }
        if (w >= 10) { this._drawFreqScale(w); }
        if (w < 2 || h < 2) { return; }
        if (this._canvas.width !== w || this._canvas.height !== h) {
            this._canvas.width = w;
            this._canvas.height = h;
            this._imageData = null;
        }
    };

    // Шкала частот на отдельном canvas (над водопадом «Сопровождение»)
    WaterfallView.prototype._drawFreqScale = function(width) {
        const sc = this._scaleCanvas;
        if (!sc || !width) { return; }
        const ctx = sc.getContext('2d');
        let ml = this._marginLeft;
        let mr = this._marginRight;
        let plotW = width - ml - mr;
        if (plotW < 10) { plotW = width; ml = 0; mr = 0; }
        const freqMin = this._freqCenterMHz - this._freqSpanMHz / 2;
        const freqMax = this._freqCenterMHz + this._freqSpanMHz / 2;
        const stepMHz = calcFreqScaleStep(this._freqSpanMHz, plotW, 52);
        if (sc.width !== width || sc.height !== 18) {
            sc.width = width;
            sc.height = 18;
        }
        const bg = cssVar('--bg-tertiary', '#243848');
        const fg = cssVar('--text-muted', '#c8d0d8');
        ctx.fillStyle = bg;
        ctx.fillRect(0, 0, width, 18);
        ctx.strokeStyle = fg;
        ctx.fillStyle = fg;
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        const decimals = stepMHz >= 1 ? 1 : (stepMHz >= 0.1 ? 2 : 3);
        for (let f = Math.ceil(freqMin / stepMHz) * stepMHz; f <= freqMax; f += stepMHz) {
            const x = ml + ((f - freqMin) / (freqMax - freqMin)) * plotW;
            ctx.beginPath();
            ctx.moveTo(x, 0);
            ctx.lineTo(x, 6);
            ctx.stroke();
            ctx.fillText(f.toFixed(decimals), x, 8);
        }
        if (this._headerFreqId) {
            const el = document.getElementById(this._headerFreqId);
            if (el) { el.textContent = freqMin.toFixed(3) + ' \u2013 ' + freqMax.toFixed(3) + ' MHz'; }
        }
        if (this._headerResId && width > 0) {
            const resEl = document.getElementById(this._headerResId);
            if (resEl) {
                const resHz = Math.round(this._freqSpanMHz * 1e6 / width);
                resEl.textContent = resHz + ' Hz/pix';
            }
        }
    };

    // Добавить строку спектра (Float32Array) в верх водопада со сдвигом вниз
    WaterfallView.prototype.pushLine = function(buf) {
        this._resize();
        const w = this._canvas.width;
        const h = this._canvas.height;
        if (w < 2 || h < 2) { return; }

        if (!this._imageData || this._imageData.width !== w || this._imageData.height !== h) {
            this._imageData = this._ctx.createImageData(w, h);
            const d = this._imageData.data;
            const cold0 = cssVarRgbHex('--waterfall-cold-bg', '#000000');
            for (let j = 0; j < d.length; j += 4) {
                d[j] = cold0[0];
                d[j + 1] = cold0[1];
                d[j + 2] = cold0[2];
                d[j + 3] = 255;
            }
        }

        const data = this._imageData.data;
        // Прокрутка вниз на 1 строку
        data.copyWithin(w * 4, 0, (h - 1) * w * 4);

        let ml = this._marginLeft;
        let mr = this._marginRight;
        let plotW = w - ml - mr;
        if (plotW < 2) { plotW = w; ml = 0; mr = 0; }

        // Область отступов — чёрный фон (rgba 0,0,0,255)
        const marginRgb = cssVarRgbHex('--waterfall-margin-bg', '#000000');
        const bins = buf.length;
        for (let x = 0; x < w; x++) {
            const idx = x * 4;
            if (x < ml || x >= w - mr) {
                data[idx] = marginRgb[0];
                data[idx + 1] = marginRgb[1];
                data[idx + 2] = marginRgb[2];
                data[idx + 3] = 255;
            } else {
                const srcIdx = Math.min(Math.floor(((x - ml) / plotW) * bins), bins - 1);
                const col = hotColor(buf[srcIdx]);
                data[idx] = col[0];
                data[idx + 1] = col[1];
                data[idx + 2] = col[2];
                data[idx + 3] = 255;
            }
        }

        this._ctx.putImageData(this._imageData, 0, 0);
        if (ml > 10) { this._drawTimeAxis(); }
    };

    // Шкала времени слева от водопада: верх = 0s (сейчас), низ = самая старая строка
    WaterfallView.prototype._drawTimeAxis = function() {
        const ctx = this._ctx;
        const h = this._canvas.height;
        const ml = this._marginLeft;
        const tickMs = this._tickMs || 80;
        const totalSec = (h * tickMs) / 1000;

        // Фон боковой шкалы времени — как поле водопада/отступы, не --bg-primary (в light иначе серая полоса)
        const axisBg = cssVar('--waterfall-margin-bg', cssVar('--spectrum-instrument-bg', '#2a3440'));
        ctx.fillStyle = axisBg;
        ctx.fillRect(0, 0, ml, h);

        // Подбор шага (2, 5 или 10 секунд)
        const stepSec = totalSec <= 10 ? 2 : (totalSec <= 30 ? 5 : 10);
        const fg = cssVar('--spectrum-axis-text', cssVar('--text-muted', '#e4e8ec'));
        ctx.font = '9px monospace';
        ctx.fillStyle = fg;
        ctx.strokeStyle = fg;
        ctx.lineWidth = 1;
        ctx.textAlign = 'right';
        ctx.textBaseline = 'middle';

        for (let t = 0; t <= totalSec; t += stepSec) {
            let y = (t / totalSec) * h;
            if (y < 6) { y = 6; }
            if (y > h - 4) { y = h - 4; }
            ctx.beginPath();
            ctx.moveTo(ml - 2, y);
            ctx.lineTo(ml, y);
            ctx.stroke();
            ctx.fillText(t + 's', ml - 4, y);
        }
    };

    WaterfallView.prototype.start = function() { this._running = true; };
    WaterfallView.prototype.stop = function() { this._running = false; };

    // Очистка: чёрный экран
    WaterfallView.prototype.clear = function() {
        this._running = false;
        this._resize();
        const w = this._canvas.width;
        const h = this._canvas.height;
        if (w < 2 || h < 2) { return; }
        this._imageData = this._ctx.createImageData(w, h);
        const d = this._imageData.data;
        const cold = cssVarRgbHex('--waterfall-cold-bg', '#000000');
        for (let i = 0; i < d.length; i += 4) {
            d[i] = cold[0];
            d[i + 1] = cold[1];
            d[i + 2] = cold[2];
            d[i + 3] = 255;
        }
        this._ctx.putImageData(this._imageData, 0, 0);
    };

    // Принудительное обновление размеров и перерисовка буфера
    WaterfallView.prototype.refresh = function() {
        this._resize();
        if (this._running && this._imageData) {
            this._ctx.putImageData(this._imageData, 0, 0);
        }
    };

    // ════════════════════════════════════════════════════════════════════════
    // FFTSpectrumView — график спектра (вкладка «Обзор», canvas #fft-spectrum)
    // ════════════════════════════════════════════════════════════════════════

    function FFTSpectrumView(canvas, opts) {
        opts = opts || {};
        this._canvas = canvas;
        this._ctx = canvas.getContext('2d');
        this.freqCenterMHz = opts.freqCenterMHz || 437.365;
        this.freqSpanMHz = opts.freqSpanMHz || 0.192;
        this._marginLeft = 32;
        this._marginBottom = 0;
        this._marginTop = 12;

        this._marginRight = 4;
        this._dbMin = -100;
        this._dbMax = 0;
    }

    // Подгонка canvas под контейнер
    FFTSpectrumView.prototype._resize = function() {
        const rect = this._canvas.getBoundingClientRect();
        const w = Math.floor(rect.width) || this._canvas.offsetWidth || 300;
        const h = Math.floor(rect.height) || this._canvas.offsetHeight || 150;
        if (w < 2 || h < 2) { return; }
        if (this._canvas.width !== w || this._canvas.height !== h) {
            this._canvas.width = w;
            this._canvas.height = h;
        }
    };

    // Полная перерисовка: сетка dB, ось Y, линия спектра, перекрестье на пике
    FFTSpectrumView.prototype.draw = function(buf) {
        this._resize();
        const w = this._canvas.width;
        const h = this._canvas.height;
        if (w < 20 || h < 20) { return; }

        const ctx = this._ctx;
        const ml = this._marginLeft;
        const mr = this._marginRight;
        const mt = this._marginTop;
        const mb = this._marginBottom;
        const plotW = w - ml - mr;
        const plotH = h - mt - mb;
        if (plotW < 4 || plotH < 4) { return; }

        const plotBg = cssVar('--spectrum-plot-bg', cssVar('--spectrum-bg', '#0c1420'));
        const gridCol = cssVar('--spectrum-plot-grid', 'rgba(255,255,255,0.08)');
        const axisText = cssVar('--spectrum-axis-text', cssVar('--text-muted', '#c8d0d8'));
        const dbRange = this._dbMax - this._dbMin;
        const dbStep = dbRange <= 60 ? 10 : 20;

        ctx.fillStyle = plotBg;
        ctx.fillRect(0, 0, w, h);

        // ── Сетка (пунктирные линии) ──
        ctx.save();
        ctx.strokeStyle = gridCol;
        ctx.globalAlpha = 1;
        ctx.setLineDash([4, 4]);
        ctx.lineWidth = 1;

        // Горизонтальные линии (dB) — на всю ширину canvas
        for (let db = this._dbMin + dbStep; db < this._dbMax; db += dbStep) {
            const yGrid = mt + plotH * (1 - (db - this._dbMin) / dbRange);
            ctx.beginPath();
            ctx.moveTo(0, yGrid);
            ctx.lineTo(w, yGrid);
            ctx.stroke();
        }

        // Вертикальные линии (частота) — на всю высоту canvas
        const freqMin = this.freqCenterMHz - this.freqSpanMHz / 2;
        const freqMax = this.freqCenterMHz + this.freqSpanMHz / 2;
        const stepMHz = calcFreqScaleStep(this.freqSpanMHz, plotW, 52);
        for (let f = Math.ceil(freqMin / stepMHz) * stepMHz; f <= freqMax; f += stepMHz) {
            const xGrid = ml + ((f - freqMin) / (freqMax - freqMin)) * plotW;
            ctx.beginPath();
            ctx.moveTo(xGrid, 0);
            ctx.lineTo(xGrid, h);
            ctx.stroke();
        }
        ctx.restore();

        // ── Подписи оси Y (dB) ──
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

        // ── Линия спектра ──
        const bins = buf.length;
        let peakIdx = 0;
        let peakVal = buf[0];
        for (let bi = 1; bi < bins; bi++) {
            if (buf[bi] > peakVal) { peakVal = buf[bi]; peakIdx = bi; }
        }

        ctx.save();
        ctx.beginPath();
        for (let bp = 0; bp < bins; bp++) {
            const xPt = ml + (bp / (bins - 1)) * plotW;
            const vv = Math.max(0, Math.min(1, buf[bp]));
            const yPt = mt + plotH * (1 - vv);
            if (bp === 0) { ctx.moveTo(xPt, yPt); } else { ctx.lineTo(xPt, yPt); }
        }
        ctx.strokeStyle = cssVar('--spectrum-trace', '#00ff80');
        ctx.lineWidth = 1.5;
        ctx.stroke();
        ctx.restore();

        // ── Перекрестье на пике ──
        const peakX = ml + (peakIdx / (bins - 1)) * plotW;
        const peakV = Math.max(0, Math.min(1, peakVal));
        const peakY = mt + plotH * (1 - peakV);
        const peakDb = this._dbMin + peakV * dbRange;
        const peakFreq = freqMin + (peakIdx / bins) * (freqMax - freqMin);

        ctx.save();
        ctx.setLineDash([4, 4]);
        ctx.strokeStyle = cssVar('--spectrum-crosshair', '#ffd700');
        ctx.lineWidth = 1;
        // Вертикаль через пик — от верха до низа canvas
        ctx.beginPath();
        ctx.moveTo(peakX, 0);
        ctx.lineTo(peakX, h);
        ctx.stroke();
        // Горизонталь через пик — от левого до правого края canvas
        ctx.beginPath();
        ctx.moveTo(0, peakY);
        ctx.lineTo(w, peakY);
        ctx.stroke();
        ctx.restore();

        // Красная точка на пике
        ctx.save();
        ctx.beginPath();
        ctx.arc(peakX, peakY, 3, 0, Math.PI * 2);
        ctx.fillStyle = cssVar('--spectrum-peak-dot', '#ff3333');
        ctx.fill();
        ctx.restore();

        // Подписи: частота внизу вертикали, dB слева горизонтали
        ctx.save();
        ctx.font = '9px monospace';
        ctx.fillStyle = cssVar('--spectrum-peak-label', '#ffd700');
        ctx.textAlign = 'center';
        ctx.textBaseline = 'bottom';
        ctx.fillText(peakFreq.toFixed(3), peakX, mt + plotH - 2);
        ctx.textAlign = 'left';
        ctx.textBaseline = 'bottom';
        ctx.fillText(peakDb.toFixed(1) + ' dB', ml + 2, peakY - 3);
        ctx.restore();
    };

    // ════════════════════════════════════════════════════════════════════════
    // Общая шкала частот (между FFT и водопадом на вкладке «Обзор»)
    // ════════════════════════════════════════════════════════════════════════

    // Шкала с засечками вверх (к FFT) и вниз (к водопаду), метки по центру
    function drawOverviewFreqScale(canvas, freqCenterMHz, freqSpanMHz, marginLeft) {
        if (!canvas) { return; }
        const parent = canvas.parentElement;
        let totalW = parent ? Math.floor(parent.getBoundingClientRect().width) : 0;
        if (totalW < 10) { totalW = canvas.offsetWidth || 0; }
        if (totalW < 10) { return; }
        if (canvas.width !== totalW || canvas.height !== 18) {
            canvas.width = totalW;
            canvas.height = 18;
        }
        const ctx = canvas.getContext('2d');
        const bg = cssVar('--bg-tertiary', '#243848');
        const fg = cssVar('--text-muted', '#c8d0d8');
        ctx.fillStyle = bg;
        ctx.fillRect(0, 0, totalW, 18);

        const ml = marginLeft || 0;
        const mr = 4;
        const plotW = totalW - ml - mr;
        if (plotW < 10) { return; }

        const freqMin = freqCenterMHz - freqSpanMHz / 2;
        const freqMax = freqCenterMHz + freqSpanMHz / 2;
        const stepMHz = calcFreqScaleStep(freqSpanMHz, plotW, 52);
        const decimals = stepMHz >= 1 ? 1 : (stepMHz >= 0.1 ? 2 : 3);

        ctx.strokeStyle = fg;
        ctx.fillStyle = fg;
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        for (let f = Math.ceil(freqMin / stepMHz) * stepMHz; f <= freqMax; f += stepMHz) {
            const x = ml + ((f - freqMin) / (freqMax - freqMin)) * plotW;
            // Засечка вверх (к FFT)
            ctx.beginPath();
            ctx.moveTo(x, 0);
            ctx.lineTo(x, 4);
            ctx.stroke();
            // Засечка вниз (к водопаду)
            ctx.beginPath();
            ctx.moveTo(x, 14);
            ctx.lineTo(x, 18);
            ctx.stroke();
            // Подпись по центру
            ctx.fillText(f.toFixed(decimals), x, 9);
        }
    }

    // ════════════════════════════════════════════════════════════════════════
    // BottomPanel — переключение вкладок, управление компонентами спектра
    // ════════════════════════════════════════════════════════════════════════

    // Миграция старых ключей вкладок (UX-BOTTOM-RENAME-001)
    const LEGACY_BOTTOM_TAB = { spectrum: 'overview', antenna: 'follow', tmi: 'overview' };

    const VALID_BOTTOM_TABS = ['overview', 'follow', 'plan'];

    function migrateBottomTabFromStorage() {
        const raw = localStorage.getItem('ux.bottomTab');
        if (!raw) { return 'overview'; }
        const next = LEGACY_BOTTOM_TAB[raw] || raw;
        if (VALID_BOTTOM_TABS.indexOf(next) < 0) {
            try { localStorage.setItem('ux.bottomTab', 'overview'); } catch (e) { /* ignore */ }
            return 'overview';
        }
        if (next !== raw) {
            try { localStorage.setItem('ux.bottomTab', next); } catch (e) { /* ignore */ }
        }
        return next;
    }

    const TAB_LABELS = { follow: 'Сопровождение', overview: 'Обзор', plan: 'План сеансов' };
    /** Вертикальная колонка в полоске РТН (по одному знаку; follow — сокращение «Сопровожд.») */
    const TAB_LABELS_SIDE = { follow: 'Сопр-ние', overview: 'ОБЗОР', plan: 'ПЛАН' };

    // Центральный bin и ширина среза для узкополосного водопада вкладки «Сопровождение»
    const FOLLOW_CENTER_BIN = 256;
    const FOLLOW_NARROW_BINS = 128;

    function BottomPanel() {
        this._panes = {};
        this._currentTab = migrateBottomTabFromStorage();
        this._resizeBound = null;
        this._spectrumTimer = null;
        this._followRunning = false;

        // Флаг свёрнутости панели — при true спектр/водопад не рисуются.
        this._collapsed = false;

        // Компоненты визуализации (создаются в _initSpectrum)
        this._dataSource = null;
        this._followWF = null;
        this._overviewWF = null;
        this._overviewFFT = null;
        this._overviewScaleCanvas = null;
        this._resizeObserver = null;

        this._collectPanes();
        if (!this._panes[this._currentTab]) {
            this._currentTab = 'overview';
            try { localStorage.setItem('ux.bottomTab', 'overview'); } catch (e) { /* ignore */ }
        }
        this._initTabs();
        this._initSDRForm();
        this._initSpectrum();
        this._switchTab(this._currentTab, false);
        this._startSpectrumTimer();
        this._bindResize();
    }

    // Собираем ссылки на pane-контейнеры
    BottomPanel.prototype._collectPanes = function() {
        const els = document.querySelectorAll('.bp-pane');
        for (let i = 0; i < els.length; i++) {
            const id = els[i].id.replace('bp-pane-', '');
            this._panes[id] = els[i];
        }
    };

    // Кнопки режима РТН: полоска слева от тела нижней панели
    const BOTTOM_TAB_BTN_SELECTOR = '.bottom-panel__tab-btn';

    // Привязываем клики (Обзор / Сопровождение)
    BottomPanel.prototype._initTabs = function() {
        const self = this;
        const tabs = document.querySelectorAll(BOTTOM_TAB_BTN_SELECTOR);
        for (let i = 0; i < tabs.length; i++) {
            (function(tab) {
                tab.addEventListener('click', function() {
                    self._switchTab(tab.getAttribute('data-tab'), true);
                });
            })(tabs[i]);
        }
    };

    // Переключение активной вкладки
    BottomPanel.prototype._switchTab = function(name, save) {
        const tabs = document.querySelectorAll(BOTTOM_TAB_BTN_SELECTOR);
        for (let i = 0; i < tabs.length; i++) {
            const on = tabs[i].getAttribute('data-tab') === name;
            tabs[i].classList.toggle('active', on);
            if (tabs[i].getAttribute('role') === 'tab') {
                tabs[i].setAttribute('aria-selected', on ? 'true' : 'false');
            }
        }
        for (const key in this._panes) {
            this._panes[key].classList.toggle('bp-pane--hidden', key !== name);
        }
        // Подпись в полоске — столбиком (по одной букве); только короткий вариант
        const modeEl = document.getElementById('bottom-panel-mode');
        if (modeEl) {
            const full = TAB_LABELS[name] || name;
            modeEl.setAttribute('title', full);
            const label = TAB_LABELS_SIDE[name] || full;
            modeEl.innerHTML = label.split('').map(function(c) {
                return '<span>' + (c === ' ' ? '\u00A0' : c) + '</span>';
            }).join('');
        }
        this._currentTab = name;
        this._overviewScaleDrawn = false;
        if (save) {
            try { localStorage.setItem('ux.bottomTab', name); } catch (e) { /* ignore */ }
        }
        if (name === 'plan' && typeof window.ensurePassesTableBottom === 'function') {
            window.ensurePassesTableBottom();
        }
        // Отложенное обновление размеров видимых компонентов
        const self = this;
        requestAnimationFrame(function() {
            requestAnimationFrame(function() {
                self._refreshCurrentTab();
            });
        });
    };

    // Обновление видимых компонентов текущей вкладки
    BottomPanel.prototype._refreshCurrentTab = function() {
        if (this._currentTab === 'overview') {
            if (this._overviewFFT) { this._overviewFFT._resize(); }
            if (this._overviewWF) { this._overviewWF.refresh(); }
            this._drawOverviewScale();
        } else if (this._currentTab === 'follow') {
            if (this._followWF) { this._followWF.refresh(); }
        }
        // plan — только таблица в скролле, спектр не трогаем
    };

    // Инициализация всех компонентов спектра
    BottomPanel.prototype._initSpectrum = function() {
        this._dataSource = new SpectrumDataSource({
            bins: 512,
            freqCenterMHz: 437.365,
            freqSpanMHz: 0.192
        });

        const ds = this._dataSource;

        // Водопад «Сопровождение»
        const followCanvas = document.getElementById('waterfall-compact');
        const followScale = document.getElementById('waterfall-freq-scale');
        if (followCanvas) {
            this._followWF = new WaterfallView(followCanvas, followScale, {
                freqCenterMHz: ds.freqCenterMHz,
                freqSpanMHz: ds.freqSpanMHz,
                headerFreqId: 'wf-freq',
                headerResId: 'wf-res',
                marginLeft: 32
            });
            this._followWF.clear();
        }

        // FFT спектр «Обзор»
        const fftCanvas = document.getElementById('fft-spectrum');
        if (fftCanvas) {
            this._overviewFFT = new FFTSpectrumView(fftCanvas, {
                freqCenterMHz: ds.freqCenterMHz,
                freqSpanMHz: ds.freqSpanMHz
            });
        }

        // Водопад «Обзор» — marginLeft совпадает с отступом FFT для выравнивания
        const overviewWFCanvas = document.getElementById('spectrum-waterfall');
        const fftML = this._overviewFFT ? this._overviewFFT._marginLeft : 40;
        const fftMR = this._overviewFFT ? this._overviewFFT._marginRight : 4;
        if (overviewWFCanvas) {
            this._overviewWF = new WaterfallView(overviewWFCanvas, null, {
                freqCenterMHz: ds.freqCenterMHz,
                freqSpanMHz: ds.freqSpanMHz,
                marginLeft: fftML,
                marginRight: fftMR
            });
            this._overviewWF.start();
        }

        this._overviewScaleCanvas = document.getElementById('overview-freq-scale') || null;

        // ResizeObserver на контейнеры
        if (typeof ResizeObserver !== 'undefined') {
            const self = this;
            this._resizeObserver = new ResizeObserver(function() {
                self._refreshCurrentTab();
            });
            if (followCanvas && followCanvas.parentElement) {
                this._resizeObserver.observe(followCanvas.parentElement);
            }
            const overviewCharts = document.querySelector('.bp-overview-charts');
            if (overviewCharts) {
                this._resizeObserver.observe(overviewCharts);
            }
        }
    };

    // Отрисовка общей шкалы частот «Обзор»
    BottomPanel.prototype._drawOverviewScale = function() {
        if (!this._overviewScaleCanvas || !this._dataSource) { return; }
        const ml = this._overviewFFT ? this._overviewFFT._marginLeft : 40;
        drawOverviewFreqScale(
            this._overviewScaleCanvas,
            this._dataSource.freqCenterMHz,
            this._dataSource.freqSpanMHz,
            ml
        );
    };

    // Запуск единого таймера спектра (80 мс)
    BottomPanel.prototype._startSpectrumTimer = function() {
        const self = this;
        this._spectrumTimer = setInterval(function() { self._spectrumTick(); }, 80);
    };

    // Один такт: генерация данных + отрисовка для видимой вкладки
    BottomPanel.prototype._spectrumTick = function() {
        if (!this._dataSource || this._collapsed) { return; }
        this._dataSource.generateLine();

        if (this._currentTab === 'overview') {
            const line = this._dataSource.getLine();
            if (this._overviewFFT) { this._overviewFFT.draw(line); }
            if (this._overviewWF) { this._overviewWF.pushLine(line); }
            if (!this._overviewScaleDrawn) {
                this._drawOverviewScale();
                this._overviewScaleDrawn = true;
            }
        }

        if (this._currentTab === 'follow' && this._followRunning && this._followWF) {
            const slice = this._dataSource.getSlice(FOLLOW_CENTER_BIN, FOLLOW_NARROW_BINS);
            this._followWF.pushLine(slice);
        }
    };

    // ── Публичный API ──

    /**
     * Переключить вкладку программно.
     * @param {string} name — 'overview' | 'follow' | 'plan'
     * @param {boolean} [persist=false] — true при клике пользователя (localStorage)
     */
    BottomPanel.prototype.showTab = function(name, persist) {
        if (!this._panes[name]) { return; }
        this._switchTab(name, persist === true);
    };

    // Сброс имитации Доплера + очистка водопадов (при смене спутника)
    BottomPanel.prototype.resetSimulation = function() {
        if (this._dataSource) { this._dataSource.reset(); }
        if (this._followWF) { this._followWF.clear(); }
        if (this._overviewWF) { this._overviewWF.clear(); }
    };

    // Запуск водопада «Сопровождение» (кнопка «Сопровождать» в правой панели)
    BottomPanel.prototype.startWaterfall = function() {
        this.resetSimulation();
        this._followRunning = true;
        if (this._followWF) { this._followWF.start(); }
    };

    // Остановка и очистка водопада «Сопровождение» (кнопка «Сброс»)
    BottomPanel.prototype.stopWaterfallAndClear = function() {
        this._followRunning = false;
        if (this._followWF) { this._followWF.clear(); }
    };

    // Принудительное обновление видимых водопадов (после resize / разворота)
    BottomPanel.prototype.refreshWaterfall = function() {
        this._refreshCurrentTab();
    };

    /** После смены colors-*.css: сбросить буфер водопада (фон «холодных» пикселей) и перерисовать шкалы. */
    BottomPanel.prototype.refreshAfterThemeChange = function() {
        this._overviewScaleDrawn = false;
        if (this._overviewWF) {
            this._overviewWF._imageData = null;
            if (!this._overviewWF._running) {
                this._overviewWF.clear();
            }
        }
        if (this._followWF) {
            this._followWF._imageData = null;
            if (!this._followWF._running) {
                this._followWF.clear();
            }
        }
        this._refreshCurrentTab();
    };

    /**
     * Установить состояние свёрнутости панели.
     * При collapsed=true генерация данных и отрисовка спектра/водопада приостанавливается.
     * @param {boolean} collapsed
     */
    BottomPanel.prototype.setCollapsed = function(collapsed) {
        this._collapsed = Boolean(collapsed);
    };

    /** @returns {boolean} true если панель свёрнута. */
    BottomPanel.prototype.isCollapsed = function() {
        return this._collapsed;
    };

    // ── Заглушки формы SDR ──

    BottomPanel.prototype._initSDRForm = function() {
        const gainSlider = document.getElementById('sdr-gain');
        const gainVal = document.getElementById('sdr-gain-val');
        if (gainSlider && gainVal) {
            gainSlider.addEventListener('input', function() {
                gainVal.textContent = gainSlider.value;
            });
        }

        const deviceSel = document.getElementById('sdr-device');
        const deviceRefresh = document.getElementById('sdr-device-refresh');
        if (deviceRefresh) {
            deviceRefresh.addEventListener('click', function() {
                console.log('[BottomPanel] TODO: GET /api/sdr/devices — обновление списка устройств', {
                    current: deviceSel && deviceSel.value,
                });
            });
        }

        // Привязка футер-индикатора `#sf-sdr` к текущему выбору устройства.
        // Live-биндинг: меняется сразу при выборе в dropdown (не ждём «Установить»).
        const syncFooterSDR = function() {
            const footer = document.getElementById('sf-sdr');
            if (!footer || !deviceSel) { return; }
            const opt = deviceSel.options[deviceSel.selectedIndex];
            const txt = opt && opt.textContent ? opt.textContent.trim() : '---';
            footer.textContent = 'SDR: ' + txt;
        };
        if (deviceSel) {
            syncFooterSDR();
            deviceSel.addEventListener('change', syncFooterSDR);
        }

        const startBtn = document.getElementById('sdr-start');
        if (startBtn) {
            startBtn.addEventListener('click', function() {
                console.log('[BottomPanel] TODO: POST /api/sdr/start', {
                    device: deviceSel && deviceSel.value,
                    freq: document.getElementById('sdr-freq') && document.getElementById('sdr-freq').value,
                    gain: gainSlider && gainSlider.value,
                    bw:   document.getElementById('sdr-bw') && document.getElementById('sdr-bw').value,
                    mod:  document.getElementById('sdr-mod') && document.getElementById('sdr-mod').value,
                    baud: document.getElementById('sdr-baud') && document.getElementById('sdr-baud').value
                });
            });
        }
    };

    // Обработка resize окна
    BottomPanel.prototype._bindResize = function() {
        const self = this;
        this._resizeBound = function() { self._refreshCurrentTab(); };
        window.addEventListener('resize', this._resizeBound);
    };

    // Очистка ресурсов
    BottomPanel.prototype.destroy = function() {
        if (this._resizeBound) {
            window.removeEventListener('resize', this._resizeBound);
            this._resizeBound = null;
        }
        if (this._spectrumTimer) {
            clearInterval(this._spectrumTimer);
            this._spectrumTimer = null;
        }
        if (this._resizeObserver) {
            this._resizeObserver.disconnect();
            this._resizeObserver = null;
        }
        this._followWF = null;
        this._overviewWF = null;
        this._overviewFFT = null;
        this._dataSource = null;
    };

    // Экспорт
    window.BottomPanel = BottomPanel;
    window.WaterfallView = WaterfallView;

})();
