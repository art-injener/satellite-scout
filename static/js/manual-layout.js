// manual-layout.js — Ручной режим: владеет независимыми canvas-инстансами
// Az/El и спектра/водопада в #layout-manual. Отдельные виджеты (не те, что
// в нижней панели), чтобы при переключении режимов рендер не «дёргался»
// и буфер водопада сохранялся в каждом из режимов независимо.
//
// Источник данных:
//  - Az/El берутся из SatelliteStateManager (приоритет: tracking → selected).
//  - Спектр/водопад — пока имитатор SpectrumDataSource (как в нижней панели).
//    Реальный SDR-стрим придёт от бэкенда (задачи SDR-001/002).
//
// Жизненный цикл: activate()/deactivate() управляют таймером отрисовки;
// при переключении режима через app.js ManualLayout приостанавливает
// генерацию данных, чтобы не нагружать CPU вне зоны видимости.

'use strict';

(function() {
    /** Период отрисовки спектра/водопада (мс). Совпадает с нижней панелью. */
    const SPECTRUM_TICK_MS = 80;

    /**
     * @param {SatelliteStateManager} stateManager
     */
    function ManualLayout(stateManager) {
        this._sm = stateManager || null;
        this._active = false;
        this._spectrumTimer = null;
        this._unsubscribers = [];
        this._resizeObservers = [];

        // Виджеты (создаются в _init*)
        this._az = null;
        this._el = null;
        this._fft = null;
        this._wf = null;
        this._dataSource = null;

        // Ссылки на canvas / контейнеры
        this._azCanvas = document.getElementById('manual-azimuth-view');
        this._elCanvas = document.getElementById('manual-elevation-view');
        this._fftCanvas = document.getElementById('manual-fft');
        this._wfCanvas = document.getElementById('manual-wf');
        this._scaleCanvas = document.getElementById('manual-spec-scale');

        this._initIndicators();
        this._initSpectrum();
        this._subscribeState();
    }

    // ── Инициализация виджетов ────────────────────────────────────────────

    ManualLayout.prototype._initIndicators = function() {
        const self = this;
        if (this._azCanvas && typeof window.AzimuthIndicator === 'function') {
            this._az = new window.AzimuthIndicator(this._azCanvas);
            this._az.setInfoElements({
                ant: 'manual-az-info-ant',
                sat: 'manual-az-info-sat',
            });
            this._observeWrap(this._azCanvas, function() {
                self._resizeIndicator(self._az, self._azCanvas);
            });
        }
        if (this._elCanvas && typeof window.ElevationIndicator === 'function') {
            this._el = new window.ElevationIndicator(this._elCanvas);
            this._el.setInfoElements({
                ant: 'manual-el-info-ant',
                sat: 'manual-el-info-sat',
            });
            this._observeWrap(this._elCanvas, function() {
                self._resizeIndicator(self._el, self._elCanvas);
            });
        }
    };

    ManualLayout.prototype._initSpectrum = function() {
        if (typeof window.SpectrumDataSource !== 'function' ||
            typeof window.FFTSpectrumView !== 'function' ||
            typeof window.WaterfallView !== 'function') {
            return;
        }

        // Узкая полоса для активного КА (см. ADR-004 § 4.4 «Спектр + водопад»).
        // Реальные значения позже придут с бэка через SSE.
        this._dataSource = new window.SpectrumDataSource({
            bins: 512,
            freqCenterMHz: 437.365,
            freqSpanMHz: 0.192,
        });

        if (this._fftCanvas) {
            this._fft = new window.FFTSpectrumView(this._fftCanvas, {
                freqCenterMHz: this._dataSource.freqCenterMHz,
                freqSpanMHz: this._dataSource.freqSpanMHz,
            });
        }

        if (this._wfCanvas) {
            const ml = this._fft ? this._fft._marginLeft : 32;
            const mr = this._fft ? this._fft._marginRight : 4;
            this._wf = new window.WaterfallView(this._wfCanvas, this._scaleCanvas, {
                freqCenterMHz: this._dataSource.freqCenterMHz,
                freqSpanMHz: this._dataSource.freqSpanMHz,
                marginLeft: ml,
                marginRight: mr,
            });
            this._wf.clear();
            this._wf.start();
        }

        // Ресайз спектра/водопада при изменении контейнера .ml-spec
        const self = this;
        const specWrap = this._fftCanvas ? this._fftCanvas.closest('.ml-spec') : null;
        if (specWrap) {
            this._observeRaw(specWrap, function() {
                if (self._fft) { self._fft._resize(); }
                if (self._wf) { self._wf.refresh(); }
            });
        }
    };

    ManualLayout.prototype._subscribeState = function() {
        if (!this._sm || !window.StateEventType) {
            return;
        }
        const self = this;
        const StateEventType = window.StateEventType;

        this._unsubscribers.push(this._sm.subscribe(StateEventType.POSITION, function() {
            self._updateIndicators();
        }));
        this._unsubscribers.push(this._sm.subscribe(StateEventType.TRACKING_CHANGE, function() {
            self._updateIndicators();
        }));
        this._unsubscribers.push(this._sm.subscribe(StateEventType.SELECTED_CHANGE, function() {
            self._updateIndicators();
        }));
    };

    // ── Az/El: обновление по приоритету tracking → selected ───────────────

    ManualLayout.prototype._updateIndicators = function() {
        if (!this._az && !this._el) { return; }
        const sm = this._sm;
        if (!sm) { return; }

        const trackingId = sm.getTrackingSatelliteId();
        const targetId = trackingId || sm.getSelectedSatelliteId();

        if (!targetId) {
            this._clearIndicators();
            return;
        }

        const state = sm.getState(targetId);
        if (!state || !state.position) { return; }

        const az = state.position.az;
        const el = state.position.el;

        if (this._az) {
            this._az.setSatellitePosition(az);
            this._az.setAzimuth(az);
            this._az.setNoradId(targetId);
        }
        if (this._el) {
            this._el.setSatellitePosition(el, az);
            this._el.setPosition(az, el);
            this._el.setNoradId(targetId);
        }
    };

    ManualLayout.prototype._clearIndicators = function() {
        if (this._az) {
            this._az.setSatellitePosition(null);
            this._az.setNoradId(null);
            this._az.draw();
        }
        if (this._el) {
            this._el.setSatellitePosition(null, null);
            this._el.setNoradId(null);
            this._el.draw();
        }
    };

    // ── Размеры (ResizeObserver) ──────────────────────────────────────────

    ManualLayout.prototype._resizeIndicator = function(indicator, canvas) {
        if (!indicator || !canvas || !canvas.parentElement) { return; }
        const w = canvas.parentElement.clientWidth;
        const h = canvas.parentElement.clientHeight;
        if (w > 0 && h > 0) {
            indicator.resize(w, h);
        }
    };

    /** ResizeObserver на родителе canvas, с первым вызовом callback. */
    ManualLayout.prototype._observeWrap = function(canvas, callback) {
        if (typeof ResizeObserver === 'undefined' || !canvas || !canvas.parentElement) {
            return;
        }
        const ro = new ResizeObserver(callback);
        ro.observe(canvas.parentElement);
        this._resizeObservers.push(ro);
        callback();
    };

    /** ResizeObserver на произвольном узле без первого вызова. */
    ManualLayout.prototype._observeRaw = function(node, callback) {
        if (typeof ResizeObserver === 'undefined' || !node) { return; }
        const ro = new ResizeObserver(callback);
        ro.observe(node);
        this._resizeObservers.push(ro);
    };

    // ── Управление активностью (двойной rAF для гарантии layout-pass) ─────

    ManualLayout.prototype.activate = function() {
        if (this._active) {
            this.refresh();
            return;
        }
        this._active = true;
        this._updateIndicators();
        const self = this;
        requestAnimationFrame(function() {
            requestAnimationFrame(function() {
                self.refresh();
            });
        });
        this._startSpectrumTimer();
    };

    ManualLayout.prototype.deactivate = function() {
        if (!this._active) { return; }
        this._active = false;
        this._stopSpectrumTimer();
    };

    /** @returns {boolean} */
    ManualLayout.prototype.isActive = function() {
        return this._active;
    };

    /** Принудительный пересчёт размеров всех виджетов. */
    ManualLayout.prototype.refresh = function() {
        if (this._fft) { this._fft._resize(); }
        if (this._wf) { this._wf.refresh(); }
        this._resizeIndicator(this._az, this._azCanvas);
        this._resizeIndicator(this._el, this._elCanvas);
    };

    /** Обработка смены темы: сброс буфера водопада + перерисовка виджетов. */
    ManualLayout.prototype.refreshAfterThemeChange = function() {
        if (this._wf) {
            this._wf._imageData = null;
            this._wf.clear();
            if (this._active) {
                this._wf.start();
            }
        }
        if (this._az && typeof this._az.refreshThemeColors === 'function') {
            this._az.refreshThemeColors();
            this._az.draw();
        }
        if (this._el && typeof this._el.refreshThemeColors === 'function') {
            this._el.refreshThemeColors();
            this._el.draw();
        }
        this.refresh();
    };

    // ── Таймер отрисовки спектра/водопада ─────────────────────────────────

    ManualLayout.prototype._startSpectrumTimer = function() {
        if (this._spectrumTimer || !this._dataSource) { return; }
        const self = this;
        this._spectrumTimer = setInterval(function() { self._spectrumTick(); }, SPECTRUM_TICK_MS);
    };

    ManualLayout.prototype._stopSpectrumTimer = function() {
        if (this._spectrumTimer) {
            clearInterval(this._spectrumTimer);
            this._spectrumTimer = null;
        }
    };

    ManualLayout.prototype._spectrumTick = function() {
        if (!this._active || !this._dataSource) { return; }
        this._dataSource.generateLine();
        const line = this._dataSource.getLine();
        if (this._fft) { this._fft.draw(line); }
        if (this._wf) { this._wf.pushLine(line); }
    };

    // ── Очистка ──────────────────────────────────────────────────────────

    ManualLayout.prototype.destroy = function() {
        this._stopSpectrumTimer();
        for (let i = 0; i < this._unsubscribers.length; i++) {
            const off = this._unsubscribers[i];
            if (typeof off === 'function') { off(); }
        }
        this._unsubscribers = [];
        for (let i = 0; i < this._resizeObservers.length; i++) {
            this._resizeObservers[i].disconnect();
        }
        this._resizeObservers = [];
        this._az = null;
        this._el = null;
        this._fft = null;
        this._wf = null;
        this._dataSource = null;
    };

    // ── Экспорт ──────────────────────────────────────────────────────────

    if (typeof window !== 'undefined') {
        window.ManualLayout = ManualLayout;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { ManualLayout }; // eslint-disable-line no-undef
    }
})();
