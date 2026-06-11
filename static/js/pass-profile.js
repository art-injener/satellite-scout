// pass-profile.js — Split-view график пролёта (elevation + doppler).
//
// Два под-графика на одной оси времени (AOS → TCA → LOS):
//   Верх: кривая Elevation 0°–90°, маркер с текущим значением
//   Низ: кривая Doppler shift ±3 кГц, маркер с текущим значением
// Сквозной маркер NOW (зелёная вертикальная линия).
// Шапка: имя КА + NORAD + T-AOS / T-LOS обратный отсчёт.
//
// Данные: mock (синтетическая кривая). Реальный endpoint — Phase 2+.
// Референс: docs/img/pass-profile-mockup.png, ADR-004 § 4.7.

'use strict';

(function() {

    function cssVar(name, fallback) {
        if (typeof window.cssVar === 'function') {
            return window.cssVar(name, fallback);
        }
        return fallback;
    }

    /** Синтетическая кривая элевации (параболический профиль). */
    function mockElevationCurve(maxEl, points) {
        var result = new Float32Array(points);
        for (var i = 0; i < points; i++) {
            var t = i / (points - 1);
            result[i] = maxEl * Math.sin(t * Math.PI);
        }
        return result;
    }

    /** Синтетическая кривая Допплера (линейная через 0 в TCA). */
    function mockDopplerCurve(maxHz, points) {
        var result = new Float32Array(points);
        for (var i = 0; i < points; i++) {
            var t = i / (points - 1);
            result[i] = maxHz * (1 - 2 * t);
        }
        return result;
    }

    var CURVE_POINTS = 120;

    /**
     * PassProfileView — canvas split-view.
     * @param {HTMLCanvasElement} canvas
     * @param {Object} opts
     */
    function PassProfileView(canvas, opts) {
        opts = opts || {};
        this._canvas = canvas;
        this._ctx = canvas ? canvas.getContext('2d') : null;

        this._satName = opts.satName || '—';
        this._noradId = opts.noradId || 0;

        // Время пролёта (unix ms)
        var now = Date.now();
        this._aosMs = opts.aosMs || (now - 3 * 60000);
        this._tcaMs = opts.tcaMs || (now + 2 * 60000);
        this._losMs = opts.losMs || (now + 8 * 60000);
        this._maxEl = opts.maxEl || 67;
        this._maxDopplerHz = opts.maxDopplerHz || 3000;

        // Кривые
        this._elCurve = mockElevationCurve(this._maxEl, CURVE_POINTS);
        this._dopCurve = mockDopplerCurve(this._maxDopplerHz, CURVE_POINTS);

        this._headerH = 28;
        this._splitRatio = 0.55;
    }

    PassProfileView.prototype._resize = function() {
        if (!this._canvas) { return; }
        var rect = this._canvas.getBoundingClientRect();
        var w = Math.floor(rect.width) || 300;
        var h = Math.floor(rect.height) || 200;
        if (w < 2 || h < 2) { return; }
        if (this._canvas.width !== w || this._canvas.height !== h) {
            this._canvas.width = w;
            this._canvas.height = h;
        }
    };

    /** Обновить данные пролёта. */
    PassProfileView.prototype.setPassData = function(data) {
        this._satName = data.satName || '—';
        this._noradId = data.noradId || 0;
        this._aosMs = data.aosMs || Date.now();
        this._tcaMs = data.tcaMs || Date.now();
        this._losMs = data.losMs || Date.now();
        this._maxEl = data.maxEl || 45;
        this._maxDopplerHz = data.maxDopplerHz || 3000;
        this._elCurve = mockElevationCurve(this._maxEl, CURVE_POINTS);
        this._dopCurve = mockDopplerCurve(this._maxDopplerHz, CURVE_POINTS);
    };

    PassProfileView.prototype.draw = function() {
        this._resize();
        if (!this._canvas || !this._ctx) { return; }
        var w = this._canvas.width;
        var h = this._canvas.height;
        if (w < 60 || h < 60) { return; }

        var ctx = this._ctx;
        var bg = cssVar('--spectrum-plot-bg', '#0c1420');
        var gridCol = cssVar('--spectrum-plot-grid', 'rgba(255,255,255,0.08)');
        var textCol = cssVar('--spectrum-axis-text', '#c8d0d8');
        var elColor = cssVar('--pass-profile-el', '#5bcefa');
        var dopColor = cssVar('--pass-profile-doppler', '#ffb347');
        var nowColor = cssVar('--pass-profile-now', cssVar('--accent-success', '#00d4aa'));

        ctx.fillStyle = bg;
        ctx.fillRect(0, 0, w, h);

        var hdr = this._headerH;
        var ml = 50;
        var mr = 10;
        var mb = 22;
        var plotW = w - ml - mr;
        if (plotW < 20) { return; }

        var totalPlotH = h - hdr - mb;
        var elH = Math.floor(totalPlotH * this._splitRatio);
        var dopH = totalPlotH - elH;
        var elTop = hdr;
        var dopTop = hdr + elH;

        // ── Шапка ──
        ctx.save();
        ctx.font = '11px monospace';
        ctx.fillStyle = cssVar('--panel-header-title', textCol);
        ctx.textBaseline = 'middle';
        ctx.textAlign = 'left';
        ctx.fillText('ПРОФИЛЬ СЕАНСА — ' + this._satName + '  NORAD ' + this._noradId, 8, hdr / 2);

        var now = Date.now();
        var tAos = Math.max(0, Math.round((this._aosMs - now) / 60000));
        var tLos = Math.max(0, Math.round((this._losMs - now) / 60000));
        ctx.textAlign = 'right';
        ctx.fillText('T-AOS ' + this._fmtMM(tAos) + '   T-LOS ' + this._fmtMM(tLos), w - 8, hdr / 2);
        ctx.restore();

        // ── Позиция NOW (0..1) ──
        var passDur = this._losMs - this._aosMs;
        var nowFrac = passDur > 0 ? Math.max(0, Math.min(1, (now - this._aosMs) / passDur)) : 0.5;
        var nowX = ml + nowFrac * plotW;
        var nowIdx = Math.min(CURVE_POINTS - 1, Math.max(0, Math.round(nowFrac * (CURVE_POINTS - 1))));

        // ── Elevation ──
        this._drawSubPlot(ctx, ml, elTop, plotW, elH, this._elCurve, CURVE_POINTS,
            0, 90, 30, '°', elColor, gridCol, textCol);
        // Подпись
        ctx.save();
        ctx.font = '10px monospace';
        ctx.fillStyle = elColor;
        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';
        ctx.fillText('ELEVATION', ml + 4, elTop + 2);
        ctx.restore();

        // ── Разделитель ──
        ctx.save();
        ctx.strokeStyle = cssVar('--border-color', '#2a3040');
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(ml, dopTop);
        ctx.lineTo(w - mr, dopTop);
        ctx.stroke();
        ctx.restore();

        // ── Doppler ──
        this._drawSubPlot(ctx, ml, dopTop, plotW, dopH, this._dopCurve, CURVE_POINTS,
            -this._maxDopplerHz, this._maxDopplerHz, this._maxDopplerHz, ' Hz', dopColor, gridCol, textCol);
        ctx.save();
        ctx.font = '10px monospace';
        ctx.fillStyle = dopColor;
        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';
        ctx.fillText('DOPPLER', ml + 4, dopTop + 2);
        ctx.restore();

        // ── Маркер NOW (сквозной) ──
        ctx.save();
        ctx.strokeStyle = nowColor;
        ctx.lineWidth = 1.5;
        ctx.setLineDash([]);
        ctx.beginPath();
        ctx.moveTo(nowX, elTop);
        ctx.lineTo(nowX, h - mb);
        ctx.stroke();
        // Подпись NOW
        ctx.font = '9px monospace';
        ctx.fillStyle = nowColor;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'bottom';
        ctx.fillText('NOW', nowX, elTop - 1);
        ctx.restore();

        // ── Маркер-точка на elevation ──
        var elVal = this._elCurve[nowIdx];
        var elY = elTop + elH * (1 - elVal / 90);
        this._drawMarker(ctx, nowX, elY, Math.round(elVal) + '°', elColor);

        // ── Маркер-точка на doppler ──
        var dopVal = this._dopCurve[nowIdx];
        var dopFrac = (dopVal - (-this._maxDopplerHz)) / (2 * this._maxDopplerHz);
        var dopY = dopTop + dopH * (1 - dopFrac);
        this._drawMarker(ctx, nowX, dopY, Math.round(dopVal) + ' Hz', dopColor);

        // ── Шкала X (AOS / TCA / LOS) ──
        ctx.save();
        ctx.font = '10px monospace';
        ctx.fillStyle = textCol;
        ctx.textBaseline = 'top';
        var labelY = h - mb + 4;

        ctx.textAlign = 'left';
        ctx.fillText('AOS', ml, labelY);

        var tcaFrac = passDur > 0 ? (this._tcaMs - this._aosMs) / passDur : 0.5;
        var tcaX = ml + tcaFrac * plotW;
        ctx.textAlign = 'center';
        ctx.fillText('TCA', tcaX, labelY);

        ctx.textAlign = 'right';
        ctx.fillText('LOS', w - mr, labelY);
        ctx.restore();
    };

    /** Рисует один под-график (кривая + сетка + подписи Y). */
    PassProfileView.prototype._drawSubPlot = function(ctx, x, y, w, h, curve, points,
        valMin, valMax, valStep, suffix, lineColor, gridCol, textCol) {
        var range = valMax - valMin;
        if (range === 0) { return; }

        // Сетка
        ctx.save();
        ctx.strokeStyle = gridCol;
        ctx.setLineDash([3, 3]);
        ctx.lineWidth = 1;
        for (var v = valMin; v <= valMax; v += valStep) {
            var gy = y + h * (1 - (v - valMin) / range);
            ctx.beginPath();
            ctx.moveTo(x, gy);
            ctx.lineTo(x + w, gy);
            ctx.stroke();
        }
        ctx.restore();

        // Подписи Y
        ctx.save();
        ctx.font = '9px monospace';
        ctx.fillStyle = textCol;
        ctx.textAlign = 'right';
        ctx.textBaseline = 'middle';
        for (var v2 = valMin; v2 <= valMax; v2 += valStep) {
            var ly = y + h * (1 - (v2 - valMin) / range);
            var label = v2 > 0 ? '+' + v2 + suffix : v2 + suffix;
            if (v2 === 0) { label = '0' + suffix; }
            ctx.fillText(label, x - 4, ly);
        }
        ctx.restore();

        // Линия
        ctx.save();
        ctx.strokeStyle = lineColor;
        ctx.lineWidth = 2;
        ctx.lineJoin = 'round';
        ctx.beginPath();
        for (var i = 0; i < points; i++) {
            var px = x + (i / (points - 1)) * w;
            var frac = (curve[i] - valMin) / range;
            var py = y + h * (1 - frac);
            if (i === 0) { ctx.moveTo(px, py); }
            else { ctx.lineTo(px, py); }
        }
        ctx.stroke();
        ctx.restore();
    };

    /** Маркер-точка с подписью значения. */
    PassProfileView.prototype._drawMarker = function(ctx, x, y, label, color) {
        ctx.save();
        ctx.fillStyle = color;
        ctx.strokeStyle = '#0c1420';
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(x, y, 5, 0, 2 * Math.PI);
        ctx.fill();
        ctx.stroke();

        ctx.font = '11px monospace';
        ctx.fillStyle = color;
        ctx.textAlign = 'left';
        ctx.textBaseline = 'middle';
        ctx.fillText(label, x + 8, y);
        ctx.restore();
    };

    /** Формат минут в MM:SS (или HH:MM если > 60). */
    PassProfileView.prototype._fmtMM = function(minutes) {
        if (minutes >= 60) {
            var hh = Math.floor(minutes / 60);
            var mm = minutes % 60;
            return String(hh).padStart(2, '0') + ':' + String(mm).padStart(2, '0');
        }
        return String(minutes).padStart(2, '0') + ':00';
    };

    /** Смена темы. */
    PassProfileView.prototype.refreshAfterThemeChange = function() {
        this.draw();
    };

    // ── Экспорт ───────────────────────────────────────────────────────────

    if (typeof window !== 'undefined') {
        window.PassProfileView = PassProfileView;
    }

    if (typeof module !== 'undefined' && module.exports) {
        module.exports = { PassProfileView };
    }
})();
