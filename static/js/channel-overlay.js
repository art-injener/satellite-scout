// channel-overlay.js — канал демодулятора поверх спектра и водопада.
//
// Окно приёмника (центр и «Полоса») и канал демодулятора — разные вещи:
// канал задаётся сдвигом от центра приёмника и шириной. Заливка с чертой
// центра и ручками краёв лежит над графиками; щелчок ставит центр канала,
// перетаскивание заливки двигает канал, ручки меняют ширину симметрично.
// Приёмник при этом не перестраивается. Канал за краем окна — стрелка у края.

'use strict';

(function() {
    /** Сдвиг меньше стольких пикселей считается щелчком, а не перетаскиванием. */
    const CLICK_SLOP_PX = 3;

    /**
     * Положение канала на графике в пикселях хоста.
     * @param {{centerHz: number, spanHz: number}} band — окно приёмника
     * @param {{offsetHz: number, bandwidthHz: number}} ch
     * @param {{left: number, width: number}} plot — область графика без полей
     * @returns {?{left: number, width: number, centerX: number, outside: ?string}}
     *   outside — 'left' / 'right', если канал целиком за краем окна; null — нет окна
     */
    function channelBox(band, ch, plot) {
        if (!band || !(band.spanHz > 0) || !plot || !(plot.width > 0)) { return null; }
        const pxPerHz = plot.width / band.spanHz;
        const centerX = plot.left + plot.width / 2 + ch.offsetHz * pxPerHz;
        const half = ch.bandwidthHz / 2 * pxPerHz;
        const lo = Math.max(plot.left, centerX - half);
        const hi = Math.min(plot.left + plot.width, centerX + half);
        let outside = null;
        if (hi <= lo) { outside = centerX < plot.left ? 'left' : 'right'; }
        return {
            left: outside ? 0 : lo,
            width: outside ? 0 : hi - lo,
            centerX: centerX,
            outside: outside,
        };
    }

    /** Сдвиг канала от центра приёмника для точки графика, с точностью до 10 Гц. */
    function offsetFromX(x, band, plot) {
        const hz = (x - plot.left - plot.width / 2) * band.spanHz / plot.width;
        return Math.round(hz / 10) * 10 || 0;
    }

    /** Сдвиг, при котором канал целиком остаётся в окне приёмника (так требует сервер). */
    function clampOffset(offsetHz, bandwidthHz, spanHz) {
        const limit = Math.max(0, spanHz / 2 - bandwidthHz / 2);
        return Math.max(-limit, Math.min(limit, offsetHz));
    }

    /** Ширина канала по положению ручки: края симметричны относительно центра. */
    function widthFromX(x, centerX, band, plot) {
        return Math.round(2 * Math.abs(x - centerX) * band.spanHz / plot.width);
    }

    /** Ближайшая ширина из предложенных в панели; без списка — как есть. */
    function snapBandwidth(hz, options) {
        if (!options || !options.length) { return hz; }
        let best = options[0];
        for (let i = 1; i < options.length; i++) {
            if (Math.abs(options[i] - hz) < Math.abs(best - hz)) { best = options[i]; }
        }
        return best;
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
     * @param {function(): number[]} [opts.bandwidthOptions] — допустимые ширины, Гц
     * @param {function(number, number)} opts.onChange — (сдвиг, ширина) после жеста
     */
    function ChannelOverlay(host, opts) {
        this._host = host;
        this._opts = opts || {};
        this._band = null;
        this._ch = { offsetHz: 0, bandwidthHz: 15000 };
        this._shown = true;
        this._drag = null;
        if (!host || typeof document === 'undefined') { return; }
        this._root = makeEl('ml-chan', host);
        this._fill = makeEl('ml-chan__fill', this._root);
        this._center = makeEl('ml-chan__center', this._root);
        this._handleL = makeEl('ml-chan__handle ml-chan__handle--l', this._root);
        this._handleR = makeEl('ml-chan__handle ml-chan__handle--r', this._root);
        this._label = makeEl('ml-chan__label', this._root);
        this._arrow = makeEl('ml-chan-arrow', host);
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

    /** Канал из полей панели или статуса сервера; во время жеста не трогаем. */
    ChannelOverlay.prototype.setChannel = function(offsetHz, bandwidthHz) {
        if (this._drag) { return; }
        if (!isFinite(offsetHz) || !(bandwidthHz > 0)) { return; }
        this._ch = { offsetHz: offsetHz, bandwidthHz: bandwidthHz };
        this.render();
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

    ChannelOverlay.prototype.render = function() {
        if (!this._root) { return; }
        if (!this._shown) {
            this._root.hidden = true;
            this._arrow.hidden = true;
            return;
        }
        const box = channelBox(this._band, this._ch, this._plot());
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
                if (!self._shown) { return; }
                if (e.button !== undefined && e.button !== 0) { return; }
                const plot = self._plot();
                if (!plot || !self._band) { return; }
                e.preventDefault();
                e.stopPropagation();
                self._drag = {
                    mode: mode,
                    x0: e.clientX,
                    moved: false,
                    offset0: self._ch.offsetHz,
                    target: e.currentTarget,
                };
                if (e.currentTarget.setPointerCapture) { e.currentTarget.setPointerCapture(e.pointerId); }
            };
        };
        this._fill.addEventListener('pointerdown', start('move'));
        this._center.addEventListener('pointerdown', start('move'));
        this._handleL.addEventListener('pointerdown', start('width'));
        this._handleR.addEventListener('pointerdown', start('width'));
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
        if (!d.moved || d.mode === 'click') { return; }
        const plot = this._plot();
        const band = this._band;
        const x = e.clientX - plot.hostLeft;
        if (d.mode === 'move') {
            const dHz = (e.clientX - d.x0) * band.spanHz / plot.width;
            const off = Math.round((d.offset0 + dHz) / 10) * 10;
            this._ch.offsetHz = clampOffset(off, this._ch.bandwidthHz, band.spanHz);
        } else {
            const centerX = plot.left + plot.width / 2 + this._ch.offsetHz * plot.width / band.spanHz;
            this._ch.bandwidthHz = Math.max(100, widthFromX(x, centerX, band, plot));
        }
        this.render();
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
        if (!d.moved && d.mode !== 'width') {
            // Щелчок по графику или по заливке ставит центр канала в эту точку
            const off = offsetFromX(e.clientX - plot.hostLeft, band, plot);
            this._ch.offsetHz = clampOffset(off, this._ch.bandwidthHz, band.spanHz);
        } else if (d.mode === 'width') {
            const opts = this._opts.bandwidthOptions ? this._opts.bandwidthOptions() : [];
            this._ch.bandwidthHz = snapBandwidth(this._ch.bandwidthHz, opts);
            this._ch.offsetHz = clampOffset(this._ch.offsetHz, this._ch.bandwidthHz, band.spanHz);
        } else if (!d.moved) {
            return;
        }
        this.render();
        if (this._opts.onChange) { this._opts.onChange(this._ch.offsetHz, this._ch.bandwidthHz); }
    };

    ChannelOverlay.prototype.destroy = function() {
        if (typeof document !== 'undefined' && this._onMove) {
            document.removeEventListener('pointermove', this._onMove);
            document.removeEventListener('pointerup', this._onUp);
        }
        if (this._root && this._root.parentNode) { this._root.parentNode.removeChild(this._root); }
        if (this._arrow && this._arrow.parentNode) { this._arrow.parentNode.removeChild(this._arrow); }
    };

    ChannelOverlay.channelBox = channelBox;
    ChannelOverlay.offsetFromX = offsetFromX;
    ChannelOverlay.clampOffset = clampOffset;
    ChannelOverlay.widthFromX = widthFromX;
    ChannelOverlay.snapBandwidth = snapBandwidth;
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
            widthFromX: widthFromX,
            snapBandwidth: snapBandwidth,
            edgesText: edgesText,
        };
    }
})();
