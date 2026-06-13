// manual-panorama.js — Блок «Панорама эфира» Ручного режима (ADR-004 § 4.4).
//
// FFT canvas: склейка нескольких частотных диапазонов через разделитель «≈».
// Чипы-маркеры (рамка + пунктир/сплошная линия) над известными несущими.
// Боковой список «Передатчики» (DOM, 2-этажные строки).
// Hover-связка: строка ↔ чип на FFT.
//
// Данные: mock (имитатор PanoramaDataSource). Реальный SDR — после SDR-003.
// Источник передатчиков: пока mock-массив. Реальный — TX-DB-001.

'use strict';

(function() {

    // ── Утилиты ───────────────────────────────────────────────────────────

    function cssVar(name, fallback) {
        if (typeof window.cssVar === 'function') {
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

    /** Экранирует спецсимволы для безопасной вставки в innerHTML. */
    function escapeHtml(s) {
        return String(s)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    }

    function calcFreqScaleStep(spanMHz, widthPx, minSpacingPx) {
        minSpacingPx = minSpacingPx || 52;
        const maxTicks = Math.max(2, Math.floor(widthPx / minSpacingPx));
        const idealStep = spanMHz / maxTicks;
        const niceSteps = [0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.25, 0.5, 1, 2, 5];
        for (let i = 0; i < niceSteps.length; i++) {
            if (niceSteps[i] >= idealStep) { return niceSteps[i]; }
        }
        return niceSteps[niceSteps.length - 1];
    }

    // ── Имитатор данных панорамы (несколько диапазонов) ────────────────────

    /** Генерирует шум + пики на заданных частотах для каждого диапазона. */
    function PanoramaDataSource(opts) {
        opts = opts || {};
        this.bands = opts.bands || [
            { minMHz: 144.0, maxMHz: 148.0 },
            { minMHz: 430.0, maxMHz: 440.0 },
        ];
        this._binsPerBand = opts.binsPerBand || 256;
        this._totalBins = this._binsPerBand * this.bands.length;
        this._buf = new Float32Array(this._totalBins);
        this._transmitters = opts.transmitters || [];
        this._avgAlpha = 0.3;
    }

    PanoramaDataSource.prototype.generateLine = function() {
        const totalBins = this._totalBins;
        const binsPerBand = this._binsPerBand;
        const bands = this.bands;

        for (let i = 0; i < totalBins; i++) {
            const noiseFloor = -90 + (Math.random() - 0.5) * 6;
            const bandIdx = Math.floor(i / binsPerBand);
            const binInBand = i - bandIdx * binsPerBand;
            const band = bands[bandIdx];
            const freqMHz = band.minMHz + (binInBand / (binsPerBand - 1)) * (band.maxMHz - band.minMHz);
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

    // ── Canvas: FFT панорамы (multi-band + чипы) ──────────────────────────

    function PanoramaFFTView(canvas, opts) {
        opts = opts || {};
        this._canvas = canvas;
        this._ctx = canvas.getContext('2d');
        this.bands = opts.bands || [
            { minMHz: 144.0, maxMHz: 148.0 },
            { minMHz: 430.0, maxMHz: 440.0 },
        ];
        this._binsPerBand = opts.binsPerBand || 256;
        this._marginLeft = 32;
        this._marginRight = 8;
        this._marginTop = 48;
        this._marginBottom = 22;
        this._gapPx = 20;
        this._dbMin = -100;
        this._dbMax = 0;
        this._transmitters = [];
        this._highlightFreq = null;
        this._selectedFreq = null;
        // Хитбоксы меток: прямоугольник подписи {left, top, w, h, tx}
        this._chipHitboxes = [];
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

    /** Возвращает передатчик под пикселем (px,py) или null. */
    PanoramaFFTView.prototype.hitTest = function(px, py) {
        const boxes = this._chipHitboxes;
        for (let i = boxes.length - 1; i >= 0; i--) {
            const b = boxes[i];
            if (px >= b.left && px <= b.left + b.w &&
                py >= b.top && py <= b.top + b.h) {
                return b.tx;
            }
        }
        return null;
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

    /** X-координата частоты → пиксель (с учётом разрывов между диапазонами). */
    PanoramaFFTView.prototype._freqToX = function(freqMHz, bandIdx, plotX, bandWidths) {
        let offset = plotX;
        for (let b = 0; b < bandIdx; b++) {
            offset += bandWidths[b] + this._gapPx;
        }
        const band = this.bands[bandIdx];
        const frac = (freqMHz - band.minMHz) / (band.maxMHz - band.minMHz);
        return offset + frac * bandWidths[bandIdx];
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

        const bands = this.bands;
        const numBands = bands.length;
        const gapPx = this._gapPx;
        const totalGap = (numBands - 1) * gapPx;
        let totalSpanMHz = 0;
        for (let b = 0; b < numBands; b++) {
            totalSpanMHz += bands[b].maxMHz - bands[b].minMHz;
        }

        const bandWidths = [];
        const usableW = plotW - totalGap;
        for (let b2 = 0; b2 < numBands; b2++) {
            const spanB = bands[b2].maxMHz - bands[b2].minMHz;
            bandWidths.push((spanB / totalSpanMHz) * usableW);
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

        // ── Вертикальная сетка частот (по тикам внутри каждого диапазона) ──
        ctx.save();
        ctx.strokeStyle = gridCol;
        ctx.setLineDash([2, 5]);
        ctx.lineWidth = 1;
        for (let vgi = 0; vgi < numBands; vgi++) {
            const vgBand = bands[vgi];
            let vgBandX = ml;
            for (let vgk = 0; vgk < vgi; vgk++) {
                vgBandX += bandWidths[vgk] + gapPx;
            }
            const vgBandW = bandWidths[vgi];
            const vgSpan = vgBand.maxMHz - vgBand.minMHz;
            const vgStep = calcFreqScaleStep(vgSpan, vgBandW, 60);
            for (let vf = Math.ceil(vgBand.minMHz / vgStep) * vgStep; vf <= vgBand.maxMHz; vf += vgStep) {
                const vgx = vgBandX + ((vf - vgBand.minMHz) / vgSpan) * vgBandW;
                ctx.beginPath();
                ctx.moveTo(vgx, mt);
                ctx.lineTo(vgx, mt + plotH);
                ctx.stroke();
            }
        }
        ctx.restore();

        // ── Рисуем FFT-линию по диапазонам ──
        const binsPerBand = this._binsPerBand;
        ctx.save();
        ctx.strokeStyle = lineColor;
        ctx.lineWidth = 1.5;
        ctx.lineJoin = 'round';

        for (let bi = 0; bi < numBands; bi++) {
            const bStart = bi * binsPerBand;
            let bandX = ml;
            for (let bk = 0; bk < bi; bk++) {
                bandX += bandWidths[bk] + gapPx;
            }
            const bandW = bandWidths[bi];

            ctx.beginPath();
            for (let p = 0; p < binsPerBand; p++) {
                const xPt = bandX + (p / (binsPerBand - 1)) * bandW;
                const vv = Math.max(0, Math.min(1, buf[bStart + p]));
                const yPt = mt + plotH * (1 - vv);
                if (p === 0) { ctx.moveTo(xPt, yPt); }
                else { ctx.lineTo(xPt, yPt); }
            }
            ctx.stroke();
        }
        ctx.restore();

        // ── Разделитель «≈» между диапазонами ──
        if (numBands > 1) {
            ctx.save();
            ctx.font = '14px monospace';
            ctx.fillStyle = axisText;
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            let gapX = ml;
            for (let g = 0; g < numBands - 1; g++) {
                gapX += bandWidths[g];
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

        for (let fi = 0; fi < numBands; fi++) {
            const band = bands[fi];
            let fBandX = ml;
            for (let fk = 0; fk < fi; fk++) {
                fBandX += bandWidths[fk] + gapPx;
            }
            const fBandW = bandWidths[fi];
            const spanB2 = band.maxMHz - band.minMHz;
            const step = calcFreqScaleStep(spanB2, fBandW, 60);

            for (let f = Math.ceil(band.minMHz / step) * step; f <= band.maxMHz; f += step) {
                const xF = fBandX + ((f - band.minMHz) / spanB2) * fBandW;
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
        this._drawChips(ctx, ml, mt, plotH, bandWidths);
    };

    /**
     * Метки передатчиков: пунктирная вертикаль + подпись в рамке с заливкой.
     * Выбранный — сплошная линия и ярче рамка; клик по всей области подписи.
     */
    PanoramaFFTView.prototype._drawChips = function(ctx, plotX, plotTop, plotH, bandWidths) {
        this._chipHitboxes = [];
        const txList = this._transmitters;
        if (!txList || txList.length === 0) { return; }

        const items = [];
        for (let t = 0; t < txList.length; t++) {
            const tx = txList[t];
            const bandIdx = this._findBand(tx.freqMHz);
            if (bandIdx < 0) { continue; }
            items.push({ tx: tx, x: this._freqToX(tx.freqMHz, bandIdx, plotX, bandWidths) });
        }
        items.sort(function(a, b) { return a.x - b.x; });

        const MIN_GAP = 72;
        let prevX = -Infinity;
        let prevTier = 1;
        for (let k = 0; k < items.length; k++) {
            if (items[k].x - prevX < MIN_GAP) {
                items[k].tier = prevTier === 0 ? 1 : 0;
            } else {
                items[k].tier = 0;
            }
            prevTier = items[k].tier;
            prevX = items[k].x;
        }

        const LABEL_PAD_X = 6;
        const LABEL_H = 16;
        const TIER_STEP = 20;
        const silentColor = cssVar('--panorama-tx-muted', '#8fa8bc');
        const chipFill = cssVar('--panorama-chip-fill', 'rgba(6, 10, 18, 0.88)');
        const labelTextCol = cssVar('--panorama-chip-label-text', '#eefcff');
        const labelMutedCol = cssVar('--panorama-chip-label-text-muted', '#c8d4e0');

        ctx.save();
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        for (let i = 0; i < items.length; i++) {
            const it = items[i];
            const tx2 = it.tx;
            const x = it.x;
            const baseColor = tx2.active
                ? (tx2.chipColor || cssVar('--panorama-tx-1', '#00ffc8'))
                : silentColor;
            const color = neonColor(baseColor);
            const isHi = this._highlightFreq !== null &&
                Math.abs(this._highlightFreq - tx2.freqMHz) < 0.001;
            const isSel = this._selectedFreq !== null &&
                Math.abs(this._selectedFreq - tx2.freqMHz) < 0.001;

            // Вертикальная привязка частоты к спектру
            ctx.save();
            ctx.strokeStyle = color;
            ctx.shadowColor = color;
            ctx.shadowBlur = isSel ? 12 : (isHi ? 8 : 5);
            ctx.lineWidth = isSel ? 2 : 1.25;
            ctx.globalAlpha = isSel ? 0.92 : (isHi ? 0.78 : (tx2.active ? 0.62 : 0.48));
            if (isSel) {
                ctx.setLineDash([]);
            } else {
                ctx.setLineDash([4, 3]);
            }
            ctx.beginPath();
            ctx.moveTo(x, plotTop);
            ctx.lineTo(x, plotTop + plotH);
            ctx.stroke();
            ctx.restore();

            const labelText = (tx2.label ? tx2.label + ' ' : '') + tx2.freqMHz.toFixed(3);
            ctx.font = isSel ? 'bold 10px monospace' : '10px monospace';
            const tw = ctx.measureText(labelText).width;
            const boxW = tw + LABEL_PAD_X * 2;
            const boxH = LABEL_H;
            const boxLeft = x - boxW / 2;
            const boxTop = plotTop - 10 - boxH - it.tier * TIER_STEP;

            // Неоновая табличка: тёмная подложка + яркая рамка и свечение
            ctx.save();
            ctx.shadowColor = color;
            ctx.shadowBlur = isSel ? 14 : (isHi ? 10 : 7);
            ctx.fillStyle = isSel
                ? hexToRgba(baseColor, 0.38)
                : (isHi ? hexToRgba(baseColor, 0.28) : chipFill);
            ctx.strokeStyle = color;
            ctx.lineWidth = isSel ? 2 : 1.5;
            ctx.globalAlpha = tx2.active ? 1 : 0.82;
            roundRect(ctx, boxLeft, boxTop, boxW, boxH, 3);
            ctx.fill();
            ctx.stroke();
            ctx.restore();

            ctx.shadowBlur = 0;
            ctx.fillStyle = isSel ? labelTextCol : (tx2.active ? labelTextCol : labelMutedCol);
            ctx.globalAlpha = 1;
            ctx.fillText(labelText, x, boxTop + boxH / 2);

            this._chipHitboxes.push({
                left: boxLeft - 2,
                top: boxTop - 2,
                w: boxW + 4,
                h: boxH + 4,
                tx: tx2,
                freqMHz: tx2.freqMHz,
            });
        }

        ctx.restore();
    };

    /** Определяет, в какой диапазон попадает частота. */
    PanoramaFFTView.prototype._findBand = function(freqMHz) {
        for (let i = 0; i < this.bands.length; i++) {
            if (freqMHz >= this.bands[i].minMHz && freqMHz <= this.bands[i].maxMHz) {
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
     * @param {HTMLElement} opts.txListEl — #manual-tx-scroll (контейнер списка)
     * @param {Array} opts.bands — [{minMHz, maxMHz}, ...]
     * @param {Array} opts.transmitters — [{freqMHz, label, active, ...}, ...]
     */
    function ManualPanorama(opts) {
        opts = opts || {};
        this._active = false;
        this._timer = null;
        this._highlightFreq = null;
        this._selectedFreq = 437.800;

        const defaultBands = [
            { minMHz: 144.0, maxMHz: 148.0 },
            { minMHz: 430.0, maxMHz: 440.0 },
        ];
        const bands = opts.bands || defaultBands;

        // Mock-список передатчиков для разработки
        const defaultTx = [
            { freqMHz: 145.825, label: 'ISS-VOICE', modulation: 'FM', active: true, chipColor: '#00ffc8' },
            { freqMHz: 437.800, label: 'ISS-FSK', modulation: 'FSK 1k2', active: true, chipColor: '#d070ff' },
            { freqMHz: 435.250, label: 'AO-91 BPSK', modulation: 'BPSK31', active: true, chipColor: '#ffaa22' },
            { freqMHz: 145.920, label: 'RS-44 CW', modulation: 'CW', active: false, chipColor: '#8fa8bc' },
            { freqMHz: 437.100, label: 'NOAA 18 APT', modulation: 'APT', active: false, chipColor: '#8fa8bc' },
        ];
        this._transmitters = opts.transmitters || defaultTx;

        // Canvas FFT
        this._canvas = opts.canvas || document.getElementById('manual-panorama-fft');
        this._fftView = null;
        if (this._canvas) {
            this._fftView = new PanoramaFFTView(this._canvas, {
                bands: bands,
                binsPerBand: 256,
            });
            this._fftView.setTransmitters(this._transmitters);
            this._fftView.setSelected(this._selectedFreq);
        }

        // Источник данных
        this._dataSource = new PanoramaDataSource({
            bands: bands,
            binsPerBand: 256,
            transmitters: this._transmitters,
        });

        // Список передатчиков (DOM) — устаревший вариант; рендерится только если контейнер есть
        this._txListEl = opts.txListEl || document.getElementById('manual-tx-scroll');
        this._renderTxList();

        // Инфо-панель по клику на метку
        this._infoEl = opts.infoEl || document.getElementById('manual-panorama-info');
        this._infoBodyEl = opts.infoBodyEl || document.getElementById('manual-panorama-info-body');
        this._infoCloseEl = opts.infoCloseEl || document.getElementById('manual-panorama-info-close');

        const self = this;
        if (this._infoCloseEl) {
            this._infoCloseEl.addEventListener('click', function() { self._hideInfo(); });
        }

        // Интерактив по канвасу: клик по метке + hover-подсветка
        if (this._canvas) {
            this._onCanvasClick = function(e) {
                if (!self._fftView) { return; }
                const hit = self._fftView.hitTest(e.offsetX, e.offsetY);
                if (hit) { self._onTxClick(hit); }
            };
            this._onCanvasMove = function(e) {
                if (!self._fftView) { return; }
                const hit = self._fftView.hitTest(e.offsetX, e.offsetY);
                self._canvas.style.cursor = hit ? 'pointer' : 'default';
                self._setHighlight(hit ? hit.freqMHz : null);
            };
            this._canvas.addEventListener('click', this._onCanvasClick);
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

    // ── Список передатчиков (DOM) ─────────────────────────────────────────

    ManualPanorama.prototype._renderTxList = function() {
        const el = this._txListEl;
        if (!el) { return; }
        el.innerHTML = '';

        const txList = this._transmitters;
        if (!txList || txList.length === 0) {
            el.innerHTML = '<p class="ml-panorama__placeholder">Нет данных о передатчиках</p>';
            return;
        }

        const self = this;
        for (let i = 0; i < txList.length; i++) {
            const tx = txList[i];
            const row = document.createElement('div');
            row.className = 'ml-tx-row' + (tx.active ? '' : ' ml-tx-row--silent');
            row.setAttribute('data-freq', tx.freqMHz);

            // Этаж 1: LED + имя
            const nameLine = document.createElement('div');
            nameLine.className = 'ml-tx-row__name';

            const led = document.createElement('span');
            led.className = 'ml-tx-row__led';
            if (tx.active) {
                led.classList.add('ml-tx-row__led--' + (this._ledClass(tx.chipColor)));
            } else {
                led.classList.add('ml-tx-row__led--grey');
            }
            nameLine.appendChild(led);

            const nameText = document.createElement('span');
            nameText.textContent = tx.label || 'Unknown';
            nameLine.appendChild(nameText);
            row.appendChild(nameLine);

            // Этаж 2: частота · модуляция
            const freqLine = document.createElement('div');
            freqLine.className = 'ml-tx-row__freq';
            let freqStr = tx.freqMHz.toFixed(3) + ' · ' + (tx.modulation || '—');
            if (!tx.active) { freqStr += '  — молчит'; }
            freqLine.textContent = freqStr;
            row.appendChild(freqLine);

            // Hover-связка
            (function(freq, rowEl) {
                rowEl.addEventListener('mouseenter', function() {
                    self._setHighlight(freq);
                    rowEl.classList.add('ml-tx-row--active');
                });
                rowEl.addEventListener('mouseleave', function() {
                    self._setHighlight(null);
                    rowEl.classList.remove('ml-tx-row--active');
                });
                rowEl.addEventListener('click', function() {
                    self._onTxClick(tx);
                });
            })(tx, row);

            el.appendChild(row);
        }
        this._updateSelectedRow();
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
    };

    /** Клик по метке передатчика → перенастройка активного канала (ADR-004 § 4.6, вариант A). */
    ManualPanorama.prototype._onTxClick = function(tx) {
        if (!tx || typeof tx.freqMHz !== 'number') { return; }
        this._selectedFreq = tx.freqMHz;
        if (this._fftView) { this._fftView.setSelected(tx.freqMHz); }
        this._updateSelectedRow();
        this._showInfo(tx);
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

    /** Показывает инфо-панель с подробностями о передатчике поверх графика. */
    ManualPanorama.prototype._showInfo = function(tx) {
        if (!this._infoEl || !this._infoBodyEl) { return; }
        const rows = [
            ['Частота', tx.freqMHz.toFixed(3) + ' MHz'],
            ['Модуляция', tx.modulation || '—'],
            ['Статус', tx.active ? 'активен' : 'молчит'],
        ];
        if (tx.noradId) { rows.push(['NORAD', String(tx.noradId)]); }
        if (tx.baud) { rows.push(['Скорость', tx.baud]); }

        let html = '<div class="ml-panorama__info-name">' + escapeHtml(tx.label || 'Unknown') + '</div>';
        for (let i = 0; i < rows.length; i++) {
            html += '<div class="ml-panorama__info-row"><span>' + escapeHtml(rows[i][0]) +
                '</span><span>' + escapeHtml(rows[i][1]) + '</span></div>';
        }
        this._infoBodyEl.innerHTML = html;
        this._infoEl.hidden = false;
    };

    ManualPanorama.prototype._hideInfo = function() {
        if (this._infoEl) { this._infoEl.hidden = true; }
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
        this._fftView.draw(this._dataSource.getLine());
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
            if (this._onCanvasMove) { this._canvas.removeEventListener('mousemove', this._onCanvasMove); }
        }
        for (let i = 0; i < this._resizeObservers.length; i++) {
            this._resizeObservers[i].disconnect();
        }
        this._resizeObservers = [];
        this._fftView = null;
        this._dataSource = null;
    };

    // ── Экспорт ───────────────────────────────────────────────────────────

    if (typeof window !== 'undefined') {
        window.ManualPanorama = ManualPanorama;
        window.PanoramaFFTView = PanoramaFFTView;
        window.PanoramaDataSource = PanoramaDataSource;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { ManualPanorama, PanoramaFFTView, PanoramaDataSource }; // eslint-disable-line no-undef
    }
})();
