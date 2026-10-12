// omni-antenna.js — вкладка «Антенна» для тракта без поворотки: вместо Az/El
// лепесток диаграммы в изометрии с лучом к КА и усиление за пролёт (ADR-004 §4.3.1).
//
// Антенны без поворотки считаются смотрящими в зенит: яги — осью вверх,
// диполь — горизонтальный, полотном по оси север–юг. Диаграммы упрощённые,
// без влияния земли: задача вида — показать, где антенна слышит хуже.

'use strict';

(function() {
    /** Нижняя граница шкалы усиления, дБ: глубже нуль диаграммы не различаем. */
    const GAIN_FLOOR_DB = -30;
    /** Нижний край графика усиления за пролёт, дБ. */
    const CHART_MIN_DB = -20;
    /** Показатель sin^n для яги: ширина луча по половинной мощности около 50°. */
    const YAGI_EXP = 7;
    /** Остаток мощности квадрифиляра у горизонта (около −4,6 дБ). */
    const QFH_HORIZON = 0.35;
    const D2R = Math.PI / 180;
    /** Наклон взгляда на изометрию: смотрим с юга, приподнявшись на этот угол. */
    const TILT = 28 * D2R;
    const GROUND_R = 1.15;
    /** Радиус подписей сторон света. */
    const LABEL_R = GROUND_R + 0.12;

    const KIND_LABELS = { qfh: 'Квадрифиляр', yagi: 'Яги (в зенит)', dipole: 'Диполь (N–S)', whip: 'Штырь' };

    function cssVar(name, fallback) {
        if (typeof window !== 'undefined' && typeof window.cssVar === 'function') {
            return window.cssVar(name, fallback);
        }
        return fallback;
    }

    // ── Модель ────────────────────────────────────────────────────────────

    /**
     * Нормированная мощность диаграммы 0…1 в направлении (az, el).
     * @returns {?number} null для неизвестной конструкции.
     */
    function patternPower(kind, azDeg, elDeg) {
        if (!isOmniKind(kind)) { return null; }
        if (elDeg < 0) { return 0; }
        const e = elDeg * D2R;
        switch (kind) {
        case 'qfh': return QFH_HORIZON + (1 - QFH_HORIZON) * Math.sin(e);
        case 'yagi': return Math.pow(Math.sin(e), YAGI_EXP);
        case 'whip': return Math.cos(e) * Math.cos(e);
        default: {
            // Диполь: мощность ∝ sin² угла между направлением и полотном (ось N–S)
            const c = Math.cos(e) * Math.cos(azDeg * D2R);
            return 1 - c * c;
        }
        }
    }

    /** Усиление относительно максимума диаграммы, дБ (не ниже GAIN_FLOOR_DB). */
    function gainDb(kind, azDeg, elDeg) {
        const p = patternPower(kind, azDeg, elDeg);
        if (p === null) { return null; }
        if (p <= 0) { return GAIN_FLOOR_DB; }
        return Math.max(GAIN_FLOOR_DB, 10 * Math.log10(p));
    }

    function isOmniKind(kind) {
        return Object.prototype.hasOwnProperty.call(KIND_LABELS, kind);
    }

    /**
     * Пролёт из записи группы КА (SSE): AOS/LOS и трасса по небу в их пределах.
     * @param {?{aos:number, los:number, sky_path:Array<{time:number, az:number, el:number}>}} sat
     * @returns {?{aosMs:number, losMs:number, points:Array<{tMs:number, az:number, el:number}>}}
     */
    function skyPathPass(sat) {
        if (!sat || !(sat.los > sat.aos) || !Array.isArray(sat.sky_path)) { return null; }
        const points = [];
        sat.sky_path.forEach((p) => {
            if (p.time >= sat.aos && p.time <= sat.los) { points.push({ tMs: p.time, az: p.az, el: p.el }); }
        });
        return points.length >= 2 ? { aosMs: sat.aos, losMs: sat.los, points: points } : null;
    }

    /** @returns {Array<{tMs:number, el:number, db:number}>} усиление в точках трассы пролёта. */
    function passGainCurve(kind, pass) {
        if (!pass || !isOmniKind(kind)) { return []; }
        return pass.points.map((p) => ({ tMs: p.tMs, el: p.el, db: gainDb(kind, p.az, p.el) }));
    }

    /** Ближайшая по времени точка трассы. */
    function pointAt(pass, tMs) {
        let best = pass.points[0];
        pass.points.forEach((p) => { if (Math.abs(p.tMs - tMs) < Math.abs(best.tMs - tMs)) { best = p; } });
        return best;
    }

    function fmtDb(db) {
        if (db <= GAIN_FLOOR_DB) { return '≤ −' + Math.abs(GAIN_FLOOR_DB) + ' дБ'; }
        if (db > -0.05) { return '0 дБ'; }
        return '−' + Math.abs(db).toFixed(Math.abs(db) < 10 ? 1 : 0) + ' дБ';
    }

    function hhmm(ms) {
        const d = new Date(ms);
        const h = d.getHours();
        const m = d.getMinutes();
        return (h < 10 ? '0' : '') + h + ':' + (m < 10 ? '0' : '') + m;
    }

    // ── Общее для двух видов ──────────────────────────────────────────────

    function setupCanvas(view, w, h) {
        view._w = Math.max(0, Math.floor(w));
        view._h = Math.max(0, Math.floor(h));
        if (view._w < 2 || view._h < 2) { return; }
        if (typeof window !== 'undefined' && window.CanvasUtils && typeof window.CanvasUtils.setupHiDPICanvas === 'function') {
            view._ctx = window.CanvasUtils.setupHiDPICanvas(view._canvas, view._w, view._h);
            view._canvas.style.width = '';
            view._canvas.style.height = '';
        } else {
            view._canvas.width = view._w;
            view._canvas.height = view._h;
        }
    }

    function readColors() {
        return {
            bg: cssVar('--ind-bg', '#0c1420'),
            ground: cssVar('--ind-bg-secondary', '#182838'),
            border: cssVar('--ind-border', '#3a5060'),
            lobe: cssVar('--ind-antenna', '#22a05a'),
            gain: cssVar('--accent-warning', '#f0b429'),
            label: cssVar('--ind-label-muted', '#d0d8e0'),
            muted: cssVar('--ind-text-muted', '#708898'),
            sat: cssVar('--ind-satellite-marker', '#ffffff'),
        };
    }

    function baseInit(view, canvas) {
        view._canvas = canvas;
        view._ctx = canvas ? canvas.getContext('2d') : null;
        view._w = canvas ? canvas.width : 0;
        view._h = canvas ? canvas.height : 0;
        view._kind = null;
        view._satAz = null;
        view._satEl = null;
        view._colors = readColors();
    }

    function viewMethods(Ctor) {
        Ctor.prototype.setKind = function(kind) { this._kind = isOmniKind(kind) ? kind : null; };
        Ctor.prototype.setSatellite = function(az, el) {
            const ok = typeof az === 'number' && typeof el === 'number' && isFinite(az) && isFinite(el);
            this._satAz = ok ? az : null;
            this._satEl = ok ? el : null;
        };
        Ctor.prototype.resize = function(w, h) {
            setupCanvas(this, w, h);
            this.draw();
        };
        Ctor.prototype.refreshThemeColors = function() { this._colors = readColors(); };
    }

    // ── Лепесток в изометрии ──────────────────────────────────────────────

    /** @param {HTMLCanvasElement} canvas */
    function OmniLobeView(canvas) { baseInit(this, canvas); }
    viewMethods(OmniLobeView);

    /** Точка (восток, север, вверх) в координатах экрана. */
    OmniLobeView.prototype._project = function(e, n, u) {
        return [this._cx + this._s * e, this._cy - this._s * (u * Math.cos(TILT) + n * Math.sin(TILT))];
    };

    OmniLobeView.prototype._dir = function(azDeg, elDeg, r) {
        const a = azDeg * D2R;
        const e = elDeg * D2R;
        return this._project(r * Math.cos(e) * Math.sin(a), r * Math.cos(e) * Math.cos(a), r * Math.sin(e));
    };

    OmniLobeView.prototype._polyline = function(points) {
        const ctx = this._ctx;
        ctx.beginPath();
        for (let i = 0; i < points.length; i++) {
            if (i === 0) { ctx.moveTo(points[i][0], points[i][1]); } else { ctx.lineTo(points[i][0], points[i][1]); }
        }
        ctx.stroke();
    };

    OmniLobeView.prototype.draw = function() {
        const ctx = this._ctx;
        const W = this._w;
        const H = this._h;
        if (!ctx || W < 2 || H < 2) { return; }
        const c = this._colors;
        const kind = this._kind;

        ctx.clearRect(0, 0, W, H);
        ctx.fillStyle = c.bg;
        ctx.fillRect(0, 0, W, H);

        // Масштаб: лепесток высотой 1 над плоскостью земли радиусом GROUND_R,
        // по ширине — с запасом под подписи W и E за краем земли
        this._s = Math.max(10, Math.min((W - 20) / (2 * LABEL_R), (H - 40) / (Math.cos(TILT) + GROUND_R * Math.sin(TILT))));
        this._cx = W / 2;
        this._cy = 24 + this._s * Math.cos(TILT);

        // Земля
        const ground = [];
        for (let a = 0; a <= 360; a += 6) { ground.push(this._dir(a, 0, GROUND_R)); }
        ctx.fillStyle = c.ground;
        ctx.strokeStyle = c.border;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ground.forEach((p, i) => { if (i === 0) { ctx.moveTo(p[0], p[1]); } else { ctx.lineTo(p[0], p[1]); } });
        ctx.closePath();
        ctx.fill();
        ctx.stroke();

        ctx.fillStyle = c.muted;
        ctx.font = '11px sans-serif';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        [['N', 0], ['E', 90], ['S', 180], ['W', 270]].forEach(([t, a]) => {
            const p = this._dir(a, 0, LABEL_R);
            ctx.fillText(t, p[0], p[1]);
        });

        if (kind) {
            // Каркас лепестка: меридианы через 30°, параллели через 15°
            ctx.strokeStyle = c.lobe;
            ctx.globalAlpha = 0.6;
            for (let a = 0; a < 360; a += 30) {
                const pts = [];
                for (let e = 0; e <= 90; e += 5) { pts.push(this._dir(a, e, patternPower(kind, a, e))); }
                this._polyline(pts);
            }
            for (let e = 0; e < 90; e += 15) {
                const pts = [];
                for (let a = 0; a <= 360; a += 6) { pts.push(this._dir(a, e, patternPower(kind, a, e))); }
                this._polyline(pts);
            }
            ctx.globalAlpha = 1;
        }

        // Антенна в центре
        const o = this._project(0, 0, 0);
        ctx.fillStyle = c.label;
        ctx.beginPath();
        ctx.arc(o[0], o[1], 2.5, 0, 2 * Math.PI);
        ctx.fill();

        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';
        ctx.fillStyle = c.muted;
        ctx.fillText(kind ? KIND_LABELS[kind] : 'Конструкция не указана', 6, 6);

        this._drawRay();
    };

    /** Луч к КА: сплошная часть — усиление в его направлении, дальше пунктир до метки. */
    OmniLobeView.prototype._drawRay = function() {
        const ctx = this._ctx;
        const c = this._colors;
        if (!this._kind || this._satAz === null) { return; }
        if (this._satEl < 0) {
            ctx.fillStyle = c.muted;
            ctx.textAlign = 'right';
            ctx.fillText('КА под горизонтом', this._w - 6, 6);
            return;
        }
        const az = this._satAz;
        const el = this._satEl;
        const o = this._project(0, 0, 0);
        const g = this._dir(az, el, patternPower(this._kind, az, el));
        const tip = this._dir(az, el, GROUND_R);

        ctx.strokeStyle = c.muted;
        ctx.lineWidth = 1;
        ctx.setLineDash([3, 3]);
        this._polyline([g, tip]);
        ctx.setLineDash([]);

        ctx.strokeStyle = c.gain;
        ctx.lineWidth = 2.5;
        this._polyline([o, g]);
        ctx.lineWidth = 1;
        ctx.fillStyle = c.gain;
        ctx.beginPath();
        ctx.arc(g[0], g[1], 3, 0, 2 * Math.PI);
        ctx.fill();

        ctx.fillStyle = c.sat;
        ctx.beginPath();
        ctx.arc(tip[0], tip[1], 4, 0, 2 * Math.PI);
        ctx.fill();

        const right = tip[0] < this._w * 0.7;
        ctx.textAlign = right ? 'left' : 'right';
        ctx.textBaseline = 'middle';
        ctx.font = '12px sans-serif';
        ctx.fillStyle = c.label;
        ctx.fillText('КА ' + fmtDb(gainDb(this._kind, az, el)), tip[0] + (right ? 8 : -8), tip[1]);
    };

    // ── Усиление за пролёт ────────────────────────────────────────────────

    /** @param {HTMLCanvasElement} canvas */
    function OmniGainView(canvas) {
        baseInit(this, canvas);
        this._pass = null;
    }
    viewMethods(OmniGainView);

    /** @param {?{aosMs:number, losMs:number, points:Array}} pass — см. skyPathPass. */
    OmniGainView.prototype.setPass = function(pass) {
        this._pass = pass && pass.losMs > pass.aosMs && pass.points && pass.points.length ? pass : null;
    };

    /** @param {number} [nowMs] — по умолчанию текущее время. */
    OmniGainView.prototype.draw = function(nowMs) {
        const ctx = this._ctx;
        const W = this._w;
        const H = this._h;
        if (!ctx || W < 2 || H < 2) { return; }
        const c = this._colors;
        const now = typeof nowMs === 'number' ? nowMs : Date.now();

        ctx.clearRect(0, 0, W, H);
        ctx.fillStyle = c.bg;
        ctx.fillRect(0, 0, W, H);
        ctx.font = '11px sans-serif';

        const pass = this._pass;
        if (!this._kind || !pass) {
            ctx.fillStyle = c.muted;
            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            ctx.fillText(this._kind ? 'Нет пролёта' : 'Конструкция не указана', W / 2, H / 2);
            return;
        }

        const L = 30;
        const R = W - 8;
        const T = 20;
        const B = H - 18;
        const xOf = (t) => L + (t - pass.aosMs) / (pass.losMs - pass.aosMs) * (R - L);
        const yOfDb = (db) => T + Math.min(1, Math.max(0, db / CHART_MIN_DB)) * (B - T);
        const yOfEl = (el) => B - Math.max(0, el) / 90 * (B - T);

        // Сетка и шкала дБ
        ctx.strokeStyle = c.border;
        ctx.fillStyle = c.muted;
        ctx.lineWidth = 1;
        ctx.textAlign = 'right';
        ctx.textBaseline = 'middle';
        for (let db = 0; db >= CHART_MIN_DB; db -= 5) {
            const y = Math.round(yOfDb(db)) + 0.5;
            ctx.globalAlpha = db === 0 ? 0.9 : 0.4;
            ctx.beginPath();
            ctx.moveTo(L, y);
            ctx.lineTo(R, y);
            ctx.stroke();
            ctx.globalAlpha = 1;
            if (db % 10 === 0 || B - T > 120) { ctx.fillText(db === 0 ? '0' : '−' + Math.abs(db), L - 4, y); }
        }
        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';
        ctx.fillText('дБ', 2, 2);

        const curve = passGainCurve(this._kind, pass);

        // Угол места пунктиром — чтобы связать провал усиления с формой пролёта
        ctx.strokeStyle = c.muted;
        ctx.setLineDash([2, 3]);
        ctx.beginPath();
        curve.forEach((p, i) => { if (i === 0) { ctx.moveTo(xOf(p.tMs), yOfEl(p.el)); } else { ctx.lineTo(xOf(p.tMs), yOfEl(p.el)); } });
        ctx.stroke();
        ctx.setLineDash([]);

        // Усиление: заливка до низа и линия
        ctx.beginPath();
        curve.forEach((p, i) => { if (i === 0) { ctx.moveTo(xOf(p.tMs), yOfDb(p.db)); } else { ctx.lineTo(xOf(p.tMs), yOfDb(p.db)); } });
        ctx.lineTo(R, B);
        ctx.lineTo(L, B);
        ctx.closePath();
        ctx.fillStyle = c.lobe;
        ctx.globalAlpha = 0.25;
        ctx.fill();
        ctx.globalAlpha = 1;
        ctx.strokeStyle = c.lobe;
        ctx.lineWidth = 2;
        ctx.beginPath();
        curve.forEach((p, i) => { if (i === 0) { ctx.moveTo(xOf(p.tMs), yOfDb(p.db)); } else { ctx.lineTo(xOf(p.tMs), yOfDb(p.db)); } });
        ctx.stroke();
        ctx.lineWidth = 1;

        // AOS / LOS
        ctx.fillStyle = c.muted;
        ctx.textBaseline = 'bottom';
        ctx.textAlign = 'left';
        ctx.fillText('AOS ' + hhmm(pass.aosMs), L, H - 2);
        ctx.textAlign = 'right';
        ctx.fillText('LOS ' + hhmm(pass.losMs), R, H - 2);

        this._drawNow(now, xOf, yOfDb, T, B);
    };

    OmniGainView.prototype._drawNow = function(now, xOf, yOfDb, T, B) {
        const ctx = this._ctx;
        const c = this._colors;
        const pass = this._pass;
        ctx.textBaseline = 'bottom';
        if (now < pass.aosMs) {
            ctx.fillStyle = c.muted;
            ctx.textAlign = 'right';
            ctx.fillText('до AOS ' + Math.ceil((pass.aosMs - now) / 60000) + ' мин', this._w - 8, T - 4);
            return;
        }
        if (now > pass.losMs) { return; }
        const x = Math.round(xOf(now)) + 0.5;
        const at = this._satEl !== null ? { az: this._satAz, el: this._satEl } : pointAt(pass, now);
        const db = gainDb(this._kind, at.az, at.el);
        ctx.strokeStyle = c.gain;
        ctx.beginPath();
        ctx.moveTo(x, T);
        ctx.lineTo(x, B);
        ctx.stroke();
        ctx.fillStyle = c.gain;
        ctx.beginPath();
        ctx.arc(x, yOfDb(db), 3.5, 0, 2 * Math.PI);
        ctx.fill();
        ctx.textAlign = x > this._w * 0.6 ? 'right' : 'left';
        ctx.fillText('сейчас ' + fmtDb(db), x + (x > this._w * 0.6 ? -4 : 4), T - 4);
    };

    // ── Экспорт ───────────────────────────────────────────────────────────

    const api = {
        GAIN_FLOOR_DB, gainDb, isOmniKind, skyPathPass, passGainCurve,
        OmniLobeView, OmniGainView,
    };
    if (typeof window !== 'undefined') {
        window.OmniLobeView = OmniLobeView;
        window.OmniGainView = OmniGainView;
        window.OmniAntenna = api;
    }
    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = api; // eslint-disable-line no-undef
    }
})();
