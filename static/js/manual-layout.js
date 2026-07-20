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
        this._panorama = null;
        this._passProfile = null;
        this._demodPanel = null;
        this._tmiTable = null;
        this._eyeView = null;
        this._rxBar = null;
        this._onPanoramaTune = null;
        this._onRxPipelineChange = null;
        this._onRxApply = null;

        // Ссылки на canvas / контейнеры
        this._layoutRoot = document.getElementById('layout-manual');
        this._azCanvas = document.getElementById('manual-azimuth-view');
        this._elCanvas = document.getElementById('manual-elevation-view');
        this._fftCanvas = document.getElementById('manual-fft');
        this._wfCanvas = document.getElementById('manual-wf');
        this._scaleCanvas = document.getElementById('manual-spec-scale');

        this._initIndicators();
        this._initSpectrum();
        this._initPanorama();
        this._initPassProfile();
        this._initDemodPanel();
        this._initTelemetryTable();
        this._initEyeConstellation();
        this._initRxBar();
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

        // Ресайз спектра/водопада при изменении контейнера .ml-spectrum
        const self = this;
        const specWrap = this._fftCanvas ? this._fftCanvas.closest('.ml-spectrum') : null;
        if (specWrap) {
            this._observeRaw(specWrap, function() {
                if (self._fft) { self._fft._resize(); }
                if (self._wf) { self._wf.refresh(); }
            });
        }
    };

    ManualLayout.prototype._initPanorama = function() {
        if (typeof window.ManualPanorama !== 'function') { return; }
        this._panorama = new window.ManualPanorama();
    };

    ManualLayout.prototype._initPassProfile = function() {
        if (typeof window.PassProfileView !== 'function') { return; }
        const canvas = document.getElementById('manual-pass-profile-canvas');
        if (!canvas) { return; }
        this._passProfile = new window.PassProfileView(canvas, {
            satName: 'ISS',
            noradId: 25544,
            maxEl: 67,
            maxDopplerHz: 3000,
        });
        const self = this;
        if (typeof ResizeObserver !== 'undefined') {
            const ro = new ResizeObserver(function() {
                if (self._passProfile && self._active) { self._passProfile.draw(); }
            });
            ro.observe(canvas.parentElement || canvas);
            this._resizeObservers.push(ro);
        }
    };

    ManualLayout.prototype._initDemodPanel = function() {
        if (typeof window.DemodPanel !== 'function') { return; }
        this._demodPanel = new window.DemodPanel();
        const root = this._layoutRoot || document.getElementById('layout-manual');
        if (!root) { return; }
        const self = this;
        this._onPanoramaTune = function(e) {
            self._tuneChannel(e.detail || {});
        };
        root.addEventListener('panorama:tune', this._onPanoramaTune);
    };

    ManualLayout.prototype._initTelemetryTable = function() {
        if (typeof window.TelemetryTable !== 'function') { return; }
        this._tmiTable = new window.TelemetryTable();
    };

    ManualLayout.prototype._initEyeConstellation = function() {
        if (typeof window.EyeConstellationView !== 'function') { return; }
        const self = this;
        this._eyeView = new window.EyeConstellationView();
        this._eyeView._onAutoRestore = function() {
            self._syncEyeFromPipeline();
        };
    };

    ManualLayout.prototype._initRxBar = function() {
        if (typeof window.ManualRxBar !== 'function') { return; }
        this._rxBar = new window.ManualRxBar();
        this._bindRxBarEvents();
        this._syncEyeFromPipeline();
    };

    /** Текст pipeline из select «Демод.» или напрямую из DOM. */
    ManualLayout.prototype._getPipelineLabel = function() {
        if (this._rxBar && typeof this._rxBar.getPipeline === 'function') {
            return this._rxBar.getPipeline();
        }
        const sel = document.getElementById('manual-rx-pipeline');
        if (sel && sel.options && sel.selectedIndex >= 0) {
            const opt = sel.options[sel.selectedIndex];
            return opt && opt.text ? String(opt.text).trim() : '';
        }
        return '';
    };

    /** Диаграмма Eye/Const следует за полем «Демод.», пока не закреплена вручную. */
    ManualLayout.prototype._syncEyeFromPipeline = function() {
        if (!this._eyeView) { return; }
        const pipeline = this._getPipelineLabel();
        if (pipeline) {
            this._eyeView.setModulation(pipeline);
        }
    };

    ManualLayout.prototype._bindRxBarEvents = function() {
        const root = document.getElementById('manual-rx-bar');
        if (!root) { return; }
        const self = this;
        this._onRxPipelineChange = function() {
            self._syncEyeFromPipeline();
        };
        this._onRxApply = function() {
            self._syncEyeFromPipeline();
        };
        root.addEventListener('rx:pipeline-change', this._onRxPipelineChange);
        root.addEventListener('rx:apply', this._onRxApply);
    };

    /**
     * Перенастройка активного канала: демодулятор + Спектр (§ 4.6 вариант A).
     * @param {{ freqMHz?: number, tx?: Object }} detail
     */
    ManualLayout.prototype._tuneChannel = function(detail) {
        const freqMHz = detail.freqMHz;
        const tx = detail.tx || null;
        if (typeof freqMHz !== 'number' || !isFinite(freqMHz)) { return; }

        if (this._demodPanel && tx) {
            this._demodPanel.setChannel(tx);
        } else if (this._demodPanel) {
            this._demodPanel.setChannel({
                freqMHz: freqMHz,
                label: 'Unknown',
                modulation: '—',
                active: true,
            });
        }

        if (this._dataSource) {
            this._dataSource.freqCenterMHz = freqMHz;
            if (typeof this._dataSource.reset === 'function') {
                this._dataSource.reset();
            }
        }
        if (this._fft) {
            this._fft.freqCenterMHz = freqMHz;
        }
        if (this._wf) {
            this._wf._freqCenterMHz = freqMHz;
            this._wf.clear();
            if (this._active) {
                this._wf.start();
            }
        }

        if (this._rxBar) {
            this._rxBar.setFreqMHz(freqMHz);
        } else {
            const freqInput = document.getElementById('manual-rx-freq');
            if (freqInput && typeof window.formatDemodFreqMHz === 'function') {
                freqInput.value = window.formatDemodFreqMHz(freqMHz);
            }
        }

        if (tx && tx.modulation) {
            if (this._rxBar && typeof this._rxBar.setPipeline === 'function') {
                this._rxBar.setPipeline(tx.modulation);
            } else {
                const pipelineSelect = document.getElementById('manual-rx-pipeline');
                if (pipelineSelect && pipelineSelect.options) {
                    const mod = tx.modulation.toLowerCase();
                    for (let i = 0; i < pipelineSelect.options.length; i++) {
                        const opt = pipelineSelect.options[i];
                        if (opt.text && mod.indexOf(opt.text.toLowerCase().replace(/\s+/g, '')) >= 0) {
                            pipelineSelect.selectedIndex = i;
                            break;
                        }
                    }
                }
            }
        }

        if (this._tmiTable) {
            const label = tx ? (tx.label || 'Unknown') : 'Unknown';
            this._tmiTable.setChannel(label, freqMHz);
        }

        this._syncEyeFromPipeline();
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
        if (this._panorama) { this._panorama.activate(); }
        if (this._passProfile) { this._passProfile.draw(); }
        if (this._tmiTable) { this._tmiTable.activate(); }
        if (this._eyeView) { this._eyeView.activate(); }
    };

    ManualLayout.prototype.deactivate = function() {
        if (!this._active) { return; }
        this._active = false;
        this._stopSpectrumTimer();
        if (this._panorama) { this._panorama.deactivate(); }
        if (this._tmiTable) { this._tmiTable.deactivate(); }
        if (this._eyeView) { this._eyeView.deactivate(); }
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
        if (this._panorama) { this._panorama.refreshAfterThemeChange(); }
        if (this._passProfile) { this._passProfile.refreshAfterThemeChange(); }
        if (this._eyeView) { this._eyeView.refreshAfterThemeChange(); }
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

        // Pass profile ~1с (каждые ~12 тиков по 80мс)
        this._ppTickCount = (this._ppTickCount || 0) + 1;
        if (this._passProfile && this._ppTickCount >= 12) {
            this._ppTickCount = 0;
            this._passProfile.draw();
        }
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
        if (this._panorama) {
            this._panorama.destroy();
            this._panorama = null;
        }
        if (this._demodPanel) {
            this._demodPanel.destroy();
            this._demodPanel = null;
        }
        const root = this._layoutRoot || document.getElementById('layout-manual');
        if (root && this._onPanoramaTune) {
            root.removeEventListener('panorama:tune', this._onPanoramaTune);
        }
        this._onPanoramaTune = null;
        this._passProfile = null;
        if (this._tmiTable) {
            this._tmiTable.destroy();
            this._tmiTable = null;
        }
        if (this._eyeView) {
            this._eyeView.destroy();
            this._eyeView = null;
        }
        if (this._rxBar) {
            this._rxBar.destroy();
            this._rxBar = null;
        }
        const rxRoot = document.getElementById('manual-rx-bar');
        if (rxRoot && this._onRxPipelineChange) {
            rxRoot.removeEventListener('rx:pipeline-change', this._onRxPipelineChange);
        }
        if (rxRoot && this._onRxApply) {
            rxRoot.removeEventListener('rx:apply', this._onRxApply);
        }
        this._onRxPipelineChange = null;
        this._onRxApply = null;
    };

    // ── Экспорт ──────────────────────────────────────────────────────────

    if (typeof window !== 'undefined') {
        window.ManualLayout = ManualLayout;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { ManualLayout }; // eslint-disable-line no-undef
    }
})();
