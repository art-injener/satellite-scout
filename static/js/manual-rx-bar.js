// manual-rx-bar.js — панель управления приёмником в Ручном режиме.
// Конвертация единиц частоты, toggle «Панорама» с сохранением в localStorage.

'use strict';

(function() {
    var STORAGE_PANORAMA = 'ux.manualRxPanorama';
    var STORAGE_AGC = 'ux.manualRxAgc';

    /** Множитель: значение поля × factor = MHz. */
    var UNIT_TO_MHZ = {
        mhz: 1,
        khz: 0.001,
        hz: 0.000001,
    };

    function parseNumber(raw) {
        if (raw == null || raw === '') {
            return NaN;
        }
        return parseFloat(String(raw).replace(',', '.'));
    }

    /** Число для <input type="number"> в выбранных единицах. */
    function formatFreqForUnit(freqMHz, unit) {
        if (typeof freqMHz !== 'number' || !isFinite(freqMHz)) {
            return '';
        }
        var factor = UNIT_TO_MHZ[unit] || 1;
        var value = freqMHz / factor;
        if (unit === 'hz') {
            return String(Math.round(value));
        }
        return value.toFixed(3);
    }

    function stepForUnit(unit) {
        if (unit === 'hz') {
            return '1';
        }
        return '0.001';
    }

    function readBool(key, defaultValue) {
        try {
            var raw = localStorage.getItem(key);
            if (raw === null) {
                return defaultValue;
            }
            return raw === '1' || raw === 'true';
        } catch (_e) {
            return defaultValue;
        }
    }

    function writeBool(key, value) {
        try {
            localStorage.setItem(key, value ? '1' : '0');
        } catch (_e) {
            /* localStorage недоступен — только UI */
        }
    }

    /**
     * @param {Object} [opts]
     * @param {HTMLElement} [opts.root]
     */
    function ManualRxBar(opts) {
        opts = opts || {};
        this._root = opts.root || document.getElementById('manual-rx-bar');
        this._freqMHz = 435.641694;
        this._freqInput = document.getElementById('manual-rx-freq');
        this._freqUnit = document.getElementById('manual-rx-freq-unit');
        this._agcBtn = document.getElementById('manual-rx-agc');
        this._panBtn = document.getElementById('manual-rx-panorama');
        this._pipelineSelect = document.getElementById('manual-rx-pipeline');
        this._applyBtn = document.getElementById('manual-rx-apply');
        this._handlers = [];
        this._init();
    }

    ManualRxBar.prototype._on = function(el, type, fn) {
        if (!el) {
            return;
        }
        el.addEventListener(type, fn);
        this._handlers.push([el, type, fn]);
    };

    ManualRxBar.prototype._init = function() {
        var self = this;

        if (this._freqInput) {
            this._freqMHz = parseInputToMHz(this._freqInput.value, this._getUnit()) || this._freqMHz;
            this._renderFreqField();
        }

        this._on(this._freqUnit, 'change', function() {
            self._renderFreqField();
        });

        this._on(this._freqInput, 'change', function() {
            var mhz = self.getFreqMHz();
            if (mhz != null) {
                self._freqMHz = mhz;
            }
        });

        this._initToggle(this._agcBtn, STORAGE_AGC, false);
        this._initToggle(this._panBtn, STORAGE_PANORAMA, false);

        this._on(this._pipelineSelect, 'change', function() {
            self._emitPipelineChange();
        });

        this._on(this._applyBtn, 'click', function() {
            self._emitApply();
        });
    };

    ManualRxBar.prototype._emitPipelineChange = function() {
        if (!this._root) {
            return;
        }
        this._root.dispatchEvent(new CustomEvent('rx:pipeline-change', {
            bubbles: true,
            detail: { pipeline: this.getPipeline() },
        }));
    };

    ManualRxBar.prototype._emitApply = function() {
        if (!this._root) {
            return;
        }
        this._root.dispatchEvent(new CustomEvent('rx:apply', {
            bubbles: true,
            detail: {
                freqMHz: this.getFreqMHz(),
                pipeline: this.getPipeline(),
            },
        }));
    };

    /** @returns {string} выбранный pipeline из «Демод.» */
    ManualRxBar.prototype.getPipeline = function() {
        if (!this._pipelineSelect || !this._pipelineSelect.options) {
            return '';
        }
        var opt = this._pipelineSelect.options[this._pipelineSelect.selectedIndex];
        return opt && opt.text ? String(opt.text).trim() : '';
    };

    /** @param {string} pipeline — текст option или строка модуляции из передатчика */
    ManualRxBar.prototype.setPipeline = function(pipeline) {
        if (!this._pipelineSelect || !pipeline) {
            return;
        }
        var target = String(pipeline).trim();
        var targetLower = target.toLowerCase();
        var targetKey = targetLower.replace(/\s+/g, '');

        for (var i = 0; i < this._pipelineSelect.options.length; i++) {
            var opt = this._pipelineSelect.options[i];
            if (opt.text && opt.text.toLowerCase() === targetLower) {
                this._pipelineSelect.selectedIndex = i;
                return;
            }
        }

        for (var j = 0; j < this._pipelineSelect.options.length; j++) {
            var o = this._pipelineSelect.options[j];
            if (!o.text) { continue; }
            var optKey = o.text.toLowerCase().replace(/\s+/g, '');
            if (targetKey.indexOf(optKey) >= 0 || optKey.indexOf(targetKey) >= 0) {
                this._pipelineSelect.selectedIndex = j;
                return;
            }
            if (targetKey.indexOf('bpsk') >= 0 && optKey.indexOf('bpsk') >= 0) {
                this._pipelineSelect.selectedIndex = j;
                return;
            }
            if (targetKey.indexOf('afsk') >= 0 && optKey.indexOf('afsk') >= 0) {
                this._pipelineSelect.selectedIndex = j;
                return;
            }
            if (targetKey.indexOf('fsk') >= 0 && optKey.indexOf('fsk') >= 0) {
                this._pipelineSelect.selectedIndex = j;
                return;
            }
        }
    };

    ManualRxBar.prototype._getUnit = function() {
        if (!this._freqUnit) {
            return 'mhz';
        }
        var unit = this._freqUnit.value;
        return UNIT_TO_MHZ[unit] ? unit : 'mhz';
    };

    ManualRxBar.prototype._renderFreqField = function() {
        if (!this._freqInput) {
            return;
        }
        var unit = this._getUnit();
        this._freqInput.step = stepForUnit(unit);
        this._freqInput.value = formatFreqForUnit(this._freqMHz, unit);
    };

    /** @returns {number|null} частота в MHz */
    ManualRxBar.prototype.getFreqMHz = function() {
        if (!this._freqInput) {
            return this._freqMHz;
        }
        return parseInputToMHz(this._freqInput.value, this._getUnit());
    };

    /** @param {number} freqMHz */
    ManualRxBar.prototype.setFreqMHz = function(freqMHz) {
        if (typeof freqMHz !== 'number' || !isFinite(freqMHz)) {
            return;
        }
        this._freqMHz = freqMHz;
        this._renderFreqField();
    };

    ManualRxBar.prototype._initToggle = function(btn, storageKey, defaultOn) {
        if (!btn) {
            return;
        }
        var self = this;
        var on = readBool(storageKey, defaultOn);
        this._reflectToggle(btn, on);

        this._on(btn, 'click', function() {
            var next = btn.getAttribute('aria-pressed') !== 'true';
            self._reflectToggle(btn, next);
            writeBool(storageKey, next);
            if (btn === self._panBtn) {
                self._root && self._root.dispatchEvent(new CustomEvent('rx:panorama-toggle', {
                    bubbles: true,
                    detail: { enabled: next },
                }));
            }
        });
    };

    ManualRxBar.prototype._reflectToggle = function(btn, on) {
        btn.classList.toggle('ml-rx-bar__pill-toggle--on', on);
        btn.setAttribute('aria-pressed', on ? 'true' : 'false');
    };

    ManualRxBar.prototype.isPanoramaEnabled = function() {
        return this._panBtn && this._panBtn.getAttribute('aria-pressed') === 'true';
    };

    ManualRxBar.prototype.destroy = function() {
        for (var i = 0; i < this._handlers.length; i++) {
            var h = this._handlers[i];
            h[0].removeEventListener(h[1], h[2]);
        }
        this._handlers = [];
    };

    function parseInputToMHz(raw, unit) {
        var n = parseNumber(raw);
        if (!isFinite(n)) {
            return null;
        }
        var factor = UNIT_TO_MHZ[unit] || 1;
        return n * factor;
    }

    if (typeof window !== 'undefined') {
        window.ManualRxBar = ManualRxBar;
        window.formatRxFreqForUnit = formatFreqForUnit;
        window.parseRxFreqToMHz = parseInputToMHz;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            ManualRxBar: ManualRxBar,
            formatFreqForUnit: formatFreqForUnit,
            parseInputToMHz: parseInputToMHz,
        };
    }
})();
