// channel-overlay.js — канал демодулятора поверх спектра и водопада.
//
// Столбец всегда по центру частоты, которую слушает демодулятор: частота приёмника
// плюс Доплер. Ширину задаёт «Полоса канала». У USB и LSB внутри столбца залита
// правая или левая половина: там работает демодулятор.
// Перетаскивание по графику сообщает сдвиг в герцах: панель на эту величину
// крутит приёмник, и на всей полосе, и в увеличении. Щелчок частоту не меняет.

'use strict';

(function() {
    /** Сдвиг меньше стольких пикселей считается щелчком, а не перетаскиванием. */
    const CLICK_SLOP_PX = 3;

    /**
     * Положение канала на графике в пикселях хоста.
     * @param {{spanHz: number, offsetHz?: number}} band — что видно на графике: ширина
     *   и центр от центра приёмника (без offsetHz — окно приёмника целиком)
     * @param {{offsetHz: number, bandwidthHz: number}} ch — заливка вокруг offsetHz
     * @param {{left: number, width: number}} plot — область графика без полей
     * @returns {?{left: number, width: number, centerX: number, outside: ?string}}
     *   centerX — черта центра; outside — 'left' / 'right', если канал целиком за краем окна;
     *   null — нет окна
     */
    function channelBox(band, ch, plot) {
        if (!band || !(band.spanHz > 0) || !plot || !(plot.width > 0)) { return null; }
        const pxPerHz = plot.width / band.spanHz;
        const zeroX = plot.left + plot.width / 2 - (band.offsetHz || 0) * pxPerHz;
        const fillX = zeroX + ch.offsetHz * pxPerHz;
        const half = ch.bandwidthHz / 2 * pxPerHz;
        const lo = Math.max(plot.left, fillX - half);
        const hi = Math.min(plot.left + plot.width, fillX + half);
        let outside = null;
        if (hi <= lo) { outside = fillX < plot.left ? 'left' : 'right'; }
        return {
            left: outside ? 0 : lo,
            width: outside ? 0 : hi - lo,
            centerX: fillX,
            outside: outside,
        };
    }

    /** Сдвиг канала от центра приёмника для точки графика, с точностью до 10 Гц. */
    function offsetFromX(x, band, plot) {
        const hz = (band.offsetHz || 0) + (x - plot.left - plot.width / 2) * band.spanHz / plot.width;
        return Math.round(hz / 10) * 10 || 0;
    }

    /** Сдвиг, при котором канал целиком остаётся в окне приёмника (так требует сервер). */
    function clampOffset(offsetHz, bandwidthHz, spanHz) {
        const limit = Math.max(0, spanHz / 2 - bandwidthHz / 2);
        return Math.max(-limit, Math.min(limit, offsetHz));
    }

    /** Абсолютные края канала текстом: «436.1780 – 436.1980 МГц». */
    function edgesText(rxHz, offsetHz, bandwidthHz) {
        if (!(rxHz > 0) || !(bandwidthHz > 0)) { return '—'; }
        const c = rxHz + offsetHz;
        const lo = (c - bandwidthHz / 2) / 1e6;
        const hi = (c + bandwidthHz / 2) / 1e6;
        return lo.toFixed(4) + ' – ' + hi.toFixed(4) + ' МГц';
    }

    function makeEl(cls, parent) {
        const el = document.createElement('div');
        el.className = cls;
        if (parent) { parent.appendChild(el); }
        return el;
    }

    /**
     * @param {HTMLElement} host — общий контейнер спектра и водопада (position: relative)
     * @param {Object} opts
     * @param {HTMLCanvasElement} opts.plotCanvas — по нему считается область графика
     * @param {HTMLCanvasElement[]} [opts.clickTargets] — графики, щелчок по которым ставит центр
     * @param {number} opts.marginLeft
     * @param {number} opts.marginRight
     * @param {function(number)} [opts.onTune] — щелчок: абсолютная частота точки, Гц
     * @param {function(number)} [opts.onPan] — перетаскивание по графику: на сколько Гц
     *   сдвинуть видимую часть
     */
    function ChannelOverlay(host, opts) {
        this._host = host;
        this._opts = opts || {};
        this._band = null;
        this._view = null;
        this._ch = { offsetHz: 0, bandwidthHz: 15000 };
        // Сдвиг Доплера от сервера поверх настройки
        this._shift = { dopplerHz: 0 };
        // Предел ширины профиля, Гц; 0 — без предела
        this._maxWidthHz = 0;
        this._shown = true;
        this._drag = null;
        if (!host || typeof document === 'undefined') { return; }
        this._root = makeEl('ml-chan', host);
        this._fill = makeEl('ml-chan__fill', this._root);
        this._side = makeEl('ml-chan__side', this._root);
        this._side.hidden = true;
        this._center = makeEl('ml-chan__center', this._root);
        this._label = makeEl('ml-chan__label', this._root);
        this._arrow = makeEl('ml-chan-arrow', host);
        // Красный пунктир середины. Виден, только пока демодулятор выключен:
        // включённый сам показывает центр чертой своей полосы.
        this._guide = makeEl('ml-center-guide', host);
        this._guide.hidden = true;
        this._root.hidden = true;
        this._arrow.hidden = true;
        this._bind();
    }

    ChannelOverlay.prototype._plot = function() {
        const c = this._opts.plotCanvas;
        if (!c || !this._host || typeof c.getBoundingClientRect !== 'function') { return null; }
        const cr = c.getBoundingClientRect();
        const hr = this._host.getBoundingClientRect();
        const ml = this._opts.marginLeft || 0;
        const mr = this._opts.marginRight || 0;
        return { left: cr.left - hr.left + ml, width: cr.width - ml - mr, hostLeft: hr.left };
    };

    /** Окно приёмника из кадра потока. */
    ChannelOverlay.prototype.setBand = function(centerHz, spanHz) {
        if (!(spanHz > 0)) { return; }
        if (this._band && this._band.centerHz === centerHz && this._band.spanHz === spanHz) { return; }
        this._band = { centerHz: centerHz, spanHz: spanHz };
        this.render();
    };

    /** Видимая часть окна приёмника: центр от центра приёмника и ширина; без ширины — всё окно. */
    ChannelOverlay.prototype.setView = function(offsetHz, spanHz) {
        const v = spanHz > 0 ? { offsetHz: Number(offsetHz) || 0, spanHz: spanHz } : null;
        const same = v && this._view
            ? v.offsetHz === this._view.offsetHz && v.spanHz === this._view.spanHz
            : v === this._view;
        if (same) { return; }
        this._view = v;
        this.render();
    };

    /** Что видно на графике, для перевода пикселей в герцы. */
    ChannelOverlay.prototype._window = function() {
        if (!this._band) { return null; }
        return this._view || { offsetHz: 0, spanHz: this._band.spanHz };
    };

    /** Канал из полей панели или статуса сервера; во время жеста не трогаем. */
    ChannelOverlay.prototype.setChannel = function(offsetHz, bandwidthHz) {
        if (this._drag) { return; }
        if (!isFinite(offsetHz) || !(bandwidthHz > 0)) { return; }
        this._ch = { offsetHz: offsetHz, bandwidthHz: this._limitWidth(bandwidthHz) };
        this.render();
    };

    /** Сдвиг Доплера из статуса демодулятора, Гц. */
    ChannelOverlay.prototype.setShift = function(dopplerHz) {
        const d = isFinite(dopplerHz) ? Number(dopplerHz) : 0;
        if (d === this._shift.dopplerHz) { return; }
        this._shift = { dopplerHz: d };
        this.render();
    };

    /**
     * Половина столбца, где слушает SSB: 'usb' — правая, 'lsb' — левая, иначе весь столбец.
     * @param {string} side
     */
    ChannelOverlay.prototype.setSide = function(side) {
        if (!this._side) { return; }
        const s = side === 'usb' || side === 'lsb' ? side : '';
        this._side.hidden = s === '';
        this._side.className = 'ml-chan__side' + (s ? ' is-' + s : '');
    };

    /** Самая широкая полоса, которую слышно у профиля, Гц; 0 — без предела. */
    ChannelOverlay.prototype.setMaxWidth = function(hz) {
        this._maxWidthHz = hz > 0 ? Number(hz) : 0;
        this._ch.bandwidthHz = this._limitWidth(this._ch.bandwidthHz);
        this.render();
    };

    ChannelOverlay.prototype._limitWidth = function(hz) {
        return this._maxWidthHz > 0 ? Math.min(hz, this._maxWidthHz) : hz;
    };

    /** Канал для рисования: по центру сдвиг с Доплером. */
    ChannelOverlay.prototype._drawnChannel = function() {
        return { offsetHz: this._ch.offsetHz + this._shift.dopplerHz, bandwidthHz: this._ch.bandwidthHz };
    };

    /**
     * Линии, заливка и подпись профиля на спектре. Выключено — канал не рисуется
     * и щелчок по графику его не двигает.
     */
    ChannelOverlay.prototype.setVisible = function(on) {
        this._shown = on !== false;
        if (!this._shown) { this._drag = null; }
        this.render();
    };

    /** Подпись над каналом и состояние: 'open' — порог открыт, иначе приглушён. */
    ChannelOverlay.prototype.setStatus = function(text, state) {
        if (!this._root) { return; }
        this._label.textContent = text || '';
        this._root.classList.toggle('is-open', state === 'open');
    };

    /** Пунктир середины графика: есть, только пока столбец демодулятора скрыт. */
    ChannelOverlay.prototype._placeGuide = function() {
        if (!this._guide) { return; }
        if (this._shown) {
            this._guide.hidden = true;
            return;
        }
        const plot = this._plot();
        if (!plot || !(plot.width > 0)) {
            this._guide.hidden = true;
            return;
        }
        this._guide.hidden = false;
        this._guide.style.left = (plot.left + plot.width / 2) + 'px';
    };

    ChannelOverlay.prototype.render = function() {
        if (!this._root) { return; }
        this._placeGuide();
        if (!this._shown) {
            this._root.hidden = true;
            this._arrow.hidden = true;
            return;
        }
        const box = channelBox(this._window(), this._drawnChannel(), this._plot());
        if (!box) {
            this._root.hidden = true;
            this._arrow.hidden = true;
            return;
        }
        this._arrow.hidden = !box.outside;
        this._root.hidden = !!box.outside;
        if (box.outside) {
            const plot = this._plot();
            this._arrow.textContent = box.outside === 'left' ? '◀ канал' : 'канал ▶';
            this._arrow.classList.toggle('is-right', box.outside === 'right');
            this._arrow.style.left = box.outside === 'left' ? plot.left + 'px' : '';
            this._arrow.style.right = box.outside === 'right'
                ? (this._host.clientWidth - plot.left - plot.width) + 'px' : '';
            return;
        }
        this._root.style.left = box.left + 'px';
        this._root.style.width = Math.max(1, box.width) + 'px';
        this._center.style.left = (box.centerX - box.left) + 'px';
    };

    ChannelOverlay.prototype._bind = function() {
        const self = this;
        const start = function(mode) {
            return function(e) {
                // Скрытый канал не двигается, но график по-прежнему сдвигается
                if (!self._shown && mode !== 'click') { return; }
                if (e.button !== undefined && e.button !== 0) { return; }
                const plot = self._plot();
                if (!plot || !self._band) { return; }
                e.preventDefault();
                e.stopPropagation();
                self._drag = {
                    mode: mode,
                    x0: e.clientX,
                    xLast: e.clientX,
                    moved: false,
                    offset0: self._ch.offsetHz,
                    target: e.currentTarget,
                };
                if (e.currentTarget.setPointerCapture) { e.currentTarget.setPointerCapture(e.pointerId); }
            };
        };
        (this._opts.clickTargets || []).forEach(function(c) {
            if (c) { c.addEventListener('pointerdown', start('click')); }
        });

        this._onMove = function(e) { self._dragMove(e); };
        this._onUp = function(e) { self._dragEnd(e); };
        document.addEventListener('pointermove', this._onMove);
        document.addEventListener('pointerup', this._onUp);
    };

    ChannelOverlay.prototype._dragMove = function(e) {
        const d = this._drag;
        if (!d) { return; }
        if (Math.abs(e.clientX - d.x0) >= CLICK_SLOP_PX) { d.moved = true; }
        if (!d.moved) { return; }
        const plot = this._plot();
        const win = this._window();
        if (d.mode !== 'click' || !plot || !win) { return; }
        // Содержимое графика идёт за указателем. Панель решает, сдвигать ли окно.
        const dx = e.clientX - d.xLast;
        d.xLast = e.clientX;
        if (dx && this._opts.onPan) { this._opts.onPan(-dx * win.spanHz / plot.width); }
    };

    ChannelOverlay.prototype._dragEnd = function(e) {
        const d = this._drag;
        if (!d) { return; }
        this._drag = null;
        if (d.target && d.target.releasePointerCapture) {
            try { d.target.releasePointerCapture(e.pointerId); } catch (err) { /* захват уже снят */ }
        }
        const band = this._band;
        const plot = this._plot();
        if (!band || !plot) { return; }
        // Щелчок частоту не меняет: приёмник крутят перетаскиванием.
        if (d.mode !== 'click' || d.moved || !this._opts.onTune || !this._shown) { return; }
        const rel = offsetFromX(e.clientX - plot.hostLeft, this._window(), plot);
        this._opts.onTune(band.centerHz + rel);
    };

    ChannelOverlay.prototype.destroy = function() {
        if (typeof document !== 'undefined' && this._onMove) {
            document.removeEventListener('pointermove', this._onMove);
            document.removeEventListener('pointerup', this._onUp);
        }
        if (this._root && this._root.parentNode) { this._root.parentNode.removeChild(this._root); }
        if (this._arrow && this._arrow.parentNode) { this._arrow.parentNode.removeChild(this._arrow); }
        if (this._guide && this._guide.parentNode) { this._guide.parentNode.removeChild(this._guide); }
    };

    ChannelOverlay.channelBox = channelBox;
    ChannelOverlay.offsetFromX = offsetFromX;
    ChannelOverlay.clampOffset = clampOffset;
    ChannelOverlay.edgesText = edgesText;

    if (typeof window !== 'undefined') {
        window.ChannelOverlay = ChannelOverlay;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            ChannelOverlay: ChannelOverlay,
            channelBox: channelBox,
            offsetFromX: offsetFromX,
            clampOffset: clampOffset,
            edgesText: edgesText,
        };
    }
})();
