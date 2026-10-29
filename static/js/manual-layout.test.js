/**
 * Unit tests for ManualLayout.
 *
 * Run: node static/js/manual-layout.test.js
 *
 * Covers:
 *   - constructor doesn't crash without DOM/widgets
 *   - activate/deactivate toggles _active flag and timer
 *   - StateManager subscriptions, priority tracking → selected
 *   - destroy: unsubscribes and clears resources
 */

'use strict';

const assert = require('assert');

// ── Minimal DOM tree stub: nodes with parent/child relations ──────────────

function makeNode(id, opts) {
    opts = opts || {};
    const node = {
        id: id || null,
        parentElement: null,
        nextSibling: null,
        children: [],
        clientWidth: opts.clientWidth || 200,
        clientHeight: opts.clientHeight || 200,
        getContext: () => ({}),
        getBoundingClientRect: () => ({
            width: opts.clientWidth || 200,
            height: opts.clientHeight || 200,
        }),
        closest: () => null,
        contains(child) {
            if (child === this) { return true; }
            for (const c of this.children) {
                if (c.contains && c.contains(child)) { return true; }
            }
            return false;
        },
        appendChild(child) {
            // Detach from previous parent if any
            if (child.parentElement) {
                const idx = child.parentElement.children.indexOf(child);
                if (idx >= 0) {
                    child.parentElement.children.splice(idx, 1);
                }
            }
            this.children.push(child);
            child.parentElement = this;
            child.nextSibling = null;
            return child;
        },
        insertBefore(child, ref) {
            if (child.parentElement) {
                const idx = child.parentElement.children.indexOf(child);
                if (idx >= 0) {
                    child.parentElement.children.splice(idx, 1);
                }
            }
            const idx = this.children.indexOf(ref);
            if (idx >= 0) {
                this.children.splice(idx, 0, child);
            } else {
                this.children.push(child);
            }
            child.parentElement = this;
            child.nextSibling = ref || null;
            return child;
        },
    };
    return node;
}

// Canvas-like node (extends makeNode with width/height attributes)
function makeCanvas(id) {
    const wrap = makeNode('wrap-' + id);
    const canvas = makeNode(id);
    canvas.width = 200;
    canvas.height = 200;
    wrap.appendChild(canvas);
    return canvas;
}

function makeDomElement(id, extraProps) {
    const node = {
        id: id,
        _attrs: {},
        _listeners: {},
        textContent: '',
        style: {},
        getAttribute: function(key) { return this._attrs[key] || null; },
        setAttribute: function(key, val) { this._attrs[key] = String(val); },
        addEventListener: function(type, fn) {
            if (!this._listeners[type]) { this._listeners[type] = []; }
            this._listeners[type].push(fn);
        },
        removeEventListener: function(type, fn) {
            const arr = this._listeners[type] || [];
            const idx = arr.indexOf(fn);
            if (idx >= 0) { arr.splice(idx, 1); }
        },
        classList: {
            _set: new Set(),
            toggle: function(cls, force) {
                if (force) { this._set.add(cls); } else { this._set.delete(cls); }
            },
            contains: function(cls) { return this._set.has(cls); },
        },
    };
    if (extraProps) { Object.assign(node, extraProps); }
    return node;
}

/** Синхронно вызывает обработчики, навешанные на mock-элемент. */
function fire(el, type) {
    const arr = (el && el._listeners && el._listeners[type]) || [];
    for (const fn of arr.slice()) { fn({ type: type, target: el }); }
}

const _domNodes = new Map();

const knownIds = [
    'manual-azimuth-view',
    'manual-elevation-view',
    'manual-fft',
    'manual-wf',
    'manual-spec-scale',
    'manual-ant-spectrum',
    'manual-omni-lobe',
    'manual-omni-gain',
];
for (const id of knownIds) {
    _domNodes.set(id, makeCanvas(id));
}

global.document = {
    readyState: 'complete',
    addEventListener: () => {},
    removeEventListener: () => {},
    getElementById: (id) => _domNodes.get(id) || null,
    querySelector: () => null,
    createElement: (tag) => ({ tagName: tag.toUpperCase(), value: '', text: '' }),
};
global.window = {
    addEventListener: () => {},
};
global.ResizeObserver = class {
    observe() {}
    disconnect() {}
};
global.requestAnimationFrame = (cb) => { cb(); return 0; };
global.cancelAnimationFrame = () => {};

// ── Widget mocks: each instance has its own `calls` array (not closure-shared) ──

function makeMockWidget() {
    function Widget() {
        this.calls = [];
    }
    Widget.prototype.setInfoElements = function(o) { this.calls.push(['setInfoElements', o]); };
    Widget.prototype.setSatellitePosition = function(a, b) { this.calls.push(['setSatellitePosition', a, b]); };
    Widget.prototype.setAzimuth = function(a) { this.calls.push(['setAzimuth', a]); };
    Widget.prototype.setPosition = function(a, b) { this.calls.push(['setPosition', a, b]); };
    Widget.prototype.setNoradId = function(id) { this.calls.push(['setNoradId', id]); };
    Widget.prototype.resize = function(w, h) { this.calls.push(['resize', w, h]); };
    Widget.prototype.draw = function(buf, hold) { this.calls.push(['draw', buf, hold]); };
    Widget.prototype.refreshThemeColors = function() { this.calls.push(['refreshThemeColors']); };
    Widget.prototype._resize = function() { this.calls.push(['_resize']); };
    Widget.prototype.refresh = function() { this.calls.push(['refresh']); };
    Widget.prototype.start = function() { this.calls.push(['start']); };
    Widget.prototype.clear = function() { this.calls.push(['clear']); };
    Widget.prototype.pushLine = function(buf) { this.calls.push(['pushLine', buf]); };
    return Widget;
}

global.window.AzimuthIndicator = makeMockWidget();
global.window.ElevationIndicator = makeMockWidget();
global.window.FFTSpectrumView = makeMockWidget();
global.window.WaterfallView = makeMockWidget();
// SpectrumStream mock: tests play the server through opts.onFrame / opts.onStatus
const realStream = require('./spectrum-stream.js');
function MockStream(opts) {
    this.opts = opts;
    this.started = 0;
    this.stopped = 0;
    this.running = false;
    MockStream.instances.push(this);
}
MockStream.instances = [];
MockStream.last = function() { return MockStream.instances[MockStream.instances.length - 1] || null; };
MockStream.prototype.start = function() { this.started++; this.running = true; };
MockStream.prototype.stop = function() { this.stopped++; this.running = false; };
MockStream.prototype.isRunning = function() { return this.running; };
MockStream.dbfsToUnit = realStream.dbfsToUnit;
MockStream.AutoRange = realStream.AutoRange;
MockStream.DisplaySmooth = realStream.DisplaySmooth;
MockStream.markerReadout = realStream.markerReadout;
MockStream.dbmOffset = realStream.dbmOffset;
MockStream.shiftLevels = realStream.shiftLevels;
global.window.SpectrumStream = MockStream;

// ChannelOverlay mock: geometry is tested in channel-overlay.test.js; here — wiring
function MockChannelOverlay(host, opts) {
    this.host = host;
    this.opts = opts;
    this.calls = [];
    MockChannelOverlay.last = this;
}
['setBand', 'setChannel', 'setStatus', 'setVisible', 'render', 'destroy'].forEach((name) => {
    MockChannelOverlay.prototype[name] = function(...args) { this.calls.push([name, ...args]); };
});
MockChannelOverlay.prototype.lastCall = function(name) {
    const c = this.calls.filter((x) => x[0] === name);
    return c.length ? c[c.length - 1] : null;
};
global.window.ChannelOverlay = MockChannelOverlay;

/** FFT frame as SpectrumStream hands it over. */
function fftFrame(centerHz, values, spanHz) {
    return {
        header: { dataType: 0x01, centerHz: centerHz, spanHz: spanHz || 2048000 },
        values: new Float32Array(values || [-100, -50, 0]),
    };
}

// fetch mock: records POST /api/sdr/tune, answers with fetchMock.status
const fetchMock = { calls: [], status: 200, body: { ok: true } };
global.fetch = function(url, init) {
    fetchMock.calls.push({ url: url, body: init && init.body ? JSON.parse(init.body) : null });
    const status = fetchMock.status;
    return Promise.resolve({
        ok: status >= 200 && status < 300,
        status: status,
        json: () => Promise.resolve(fetchMock.body),
    });
};
function tuneCalls() {
    return fetchMock.calls.filter((c) => c.url === '/api/sdr/tune');
}

// AccessControl mock: role is a flag the test flips
function makeAccess(canControl) {
    const ac = {
        can: canControl,
        listeners: [],
        notices: [],
        refreshed: 0,
        canControl() { return ac.can; },
        onChange(cb) {
            ac.listeners.push(cb);
            return () => { ac.listeners = ac.listeners.filter((x) => x !== cb); };
        },
        setNotice(t) { ac.notices.push(t); },
        refresh() { ac.refreshed++; return Promise.resolve(); },
        setRole(can) { ac.can = can; ac.listeners.forEach((cb) => cb()); },
    };
    return ac;
}

/** Single-tract station: tract id 1, Az/El view. */
const TRACT_1 = { hasRotator: () => true, antennaKind: () => 'yagi', getRadioPathId: () => 1 };

/** Runs fn with a mode manager and an access control on window. */
function withStation(opts, fn) {
    const prevMM = global.window._modeManager;
    const prevAC = global.window._accessControl;
    global.window._modeManager = opts.mm === undefined ? TRACT_1 : opts.mm;
    global.window._accessControl = opts.access || makeAccess(true);
    fetchMock.calls = [];
    fetchMock.status = 200;
    try {
        return fn(global.window._accessControl);
    } finally {
        global.window._modeManager = prevMM;
        global.window._accessControl = prevAC;
    }
}

// ManualPanorama mock
function MockPanorama() {
    this.calls = [];
}
MockPanorama.prototype.activate = function() { this.calls.push(['activate']); };
MockPanorama.prototype.deactivate = function() { this.calls.push(['deactivate']); };
MockPanorama.prototype.isActive = function() { return false; };
MockPanorama.prototype.getSelectedTx = function() { return this._selected || null; };
MockPanorama.prototype.refreshAfterThemeChange = function() { this.calls.push(['refreshAfterThemeChange']); };
MockPanorama.prototype.destroy = function() { this.calls.push(['destroy']); };
global.window.ManualPanorama = MockPanorama;

// PassProfileView mock
function MockPassProfile() {
    this.calls = [];
}
MockPassProfile.prototype.draw = function() { this.calls.push(['draw']); };
MockPassProfile.prototype.refreshAfterThemeChange = function() { this.calls.push(['refreshAfterThemeChange']); };
global.window.PassProfileView = MockPassProfile;
_domNodes.set('manual-pass-profile-canvas', makeCanvas('manual-pass-profile-canvas'));

// PassTimelineView mock
function MockPassTimeline() {
    this.calls = [];
}
MockPassTimeline.prototype.draw = function() { this.calls.push(['draw']); };
MockPassTimeline.prototype.setPasses = function(l) { this.calls.push(['setPasses', l]); };
MockPassTimeline.prototype.setTrackingId = function(id) { this.calls.push(['setTrackingId', id]); };
MockPassTimeline.prototype.refreshAfterThemeChange = function() { this.calls.push(['refreshAfterThemeChange']); };
global.window.PassTimelineView = MockPassTimeline;

// Omni views: real gain model, canvas views mocked
require('./omni-antenna.js');
function MockOmniView() {
    this.calls = [];
    this.kind = null;
}
MockOmniView.prototype.setKind = function(k) { this.kind = k; this.calls.push(['setKind', k]); };
MockOmniView.prototype.setSatellite = function(az, el) { this.calls.push(['setSatellite', az, el]); };
MockOmniView.prototype.setPass = function(p) { this.calls.push(['setPass', p]); };
MockOmniView.prototype.resize = function(w, h) { this.calls.push(['resize', w, h]); };
MockOmniView.prototype.draw = function() { this.calls.push(['draw']); };
MockOmniView.prototype.refreshThemeColors = function() { this.calls.push(['refreshThemeColors']); };
global.window.OmniLobeView = MockOmniView;
global.window.OmniGainView = MockOmniView;
_domNodes.set('manual-pass-timeline', makeCanvas('manual-pass-timeline'));

function makeLayoutRoot() {
    const node = makeNode('layout-manual');
    node._listeners = {};
    node.addEventListener = function(type, fn) {
        if (!this._listeners[type]) { this._listeners[type] = []; }
        this._listeners[type].push(fn);
    };
    node.removeEventListener = function(type, fn) {
        const arr = this._listeners[type] || [];
        const idx = arr.indexOf(fn);
        if (idx >= 0) { arr.splice(idx, 1); }
    };
    node.dispatchEvent = function(e) {
        const arr = this._listeners[e.type] || [];
        for (const fn of arr) { fn(e); }
        return true;
    };
    return node;
}
_domNodes.set('layout-manual', makeLayoutRoot());
_domNodes.set('manual-rx-agc', makeDomElement('manual-rx-agc'));
_domNodes.set('manual-rx-apply', makeDomElement('manual-rx-apply'));
_domNodes.set('manual-demod-apply', makeDomElement('manual-demod-apply'));
_domNodes.set('manual-rx-panel', makeDomElement('manual-rx-panel'));
_domNodes.set('manual-rx-dial', makeDomElement('manual-rx-dial'));

// FreqDial mock: keeps the onChange handed to it so tests can play the operator
function MockFreqDial(root, opts) {
    this.root = root;
    this.opts = opts || {};
    this.hz = Math.round(this.opts.hz || 0);
    this.calls = [];
    this._onChange = this.opts.onChange || null;
}
MockFreqDial.prototype.setHz = function(hz) { this.hz = Math.round(hz); this.calls.push(['setHz', hz]); };
MockFreqDial.prototype.setMHz = function(mhz) { this.hz = Math.round(mhz * 1e6); this.calls.push(['setMHz', mhz]); };
MockFreqDial.prototype.getHz = function() { return this.hz; };
MockFreqDial.prototype.getMHz = function() { return this.hz / 1e6; };
MockFreqDial.prototype.destroy = function() { this.calls.push(['destroy']); };
MockFreqDial.prototype.setReadOnly = function(on) { this.readOnly = !!on; this.calls.push(['setReadOnly', !!on]); };
MockFreqDial.prototype.isEditing = function() { return !!this.editing; };
/** Имитирует перестройку оператором: индикатор сообщает новую частоту наружу. */
MockFreqDial.prototype.emit = function(hz) {
    this.hz = Math.round(hz);
    if (this._onChange) { this._onChange(this.hz); }
};
global.window.FreqDial = MockFreqDial;
_domNodes.set('manual-rx-marker-freq', makeDomElement('manual-rx-marker-freq'));
_domNodes.set('manual-rx-marker-level', makeDomElement('manual-rx-marker-level'));
_domNodes.set('manual-rx-marker-bar', makeDomElement('manual-rx-marker-bar'));
_domNodes.set('manual-rx-marker-unit', makeDomElement('manual-rx-marker-unit', { textContent: 'dBFS' }));
_domNodes.set('manual-rx-squelch', makeDomElement('manual-rx-squelch', { value: '-90' }));
_domNodes.set('manual-rx-squelch-dec', makeDomElement('manual-rx-squelch-dec'));
_domNodes.set('manual-rx-squelch-inc', makeDomElement('manual-rx-squelch-inc'));
_domNodes.set('manual-rx-squelch-unit', makeDomElement('manual-rx-squelch-unit', { textContent: 'dBFS' }));
_domNodes.set('manual-rx-gain', makeDomElement('manual-rx-gain', { value: '32' }));
_domNodes.set('manual-rx-gain-dec', makeDomElement('manual-rx-gain-dec'));
_domNodes.set('manual-rx-gain-inc', makeDomElement('manual-rx-gain-inc'));
_domNodes.set('manual-rx-doppler-val', makeDomElement('manual-rx-doppler-val', { value: '0' }));
_domNodes.set('manual-rx-doppler-val-dec', makeDomElement('manual-rx-doppler-val-dec'));
_domNodes.set('manual-rx-doppler-val-inc', makeDomElement('manual-rx-doppler-val-inc'));
// Накопление спектра: Мгн. | Накопление | Пик, число циклов N и «Сброс» над графиком
const specModeBtns = ['current', 'avg', 'maxhold'].reduce(function(acc, mode) {
    acc[mode] = makeDomElement('spec-mode-' + mode, { _attrs: { 'data-spec-mode': mode } });
    return acc;
}, {});
_domNodes.set('manual-spec-mode', makeDomElement('manual-spec-mode', {
    querySelectorAll: () => Object.values(specModeBtns),
}));
_domNodes.set('manual-spec-reset', makeDomElement('manual-spec-reset'));
const specAvgN = makeDomElement('manual-spec-avg-n', { value: '10', hidden: false });
_domNodes.set('manual-spec-avg-n', specAvgN);
// Тот же переключатель над компактным спектром «Антенны»
const antSpecModeBtns = ['current', 'avg', 'maxhold'].reduce(function(acc, mode) {
    acc[mode] = makeDomElement('ant-spec-mode-' + mode, { _attrs: { 'data-spec-mode': mode } });
    return acc;
}, {});
_domNodes.set('manual-ant-spec-mode', makeDomElement('manual-ant-spec-mode', {
    querySelectorAll: () => Object.values(antSpecModeBtns),
}));
_domNodes.set('manual-ant-spec-reset', makeDomElement('manual-ant-spec-reset'));
const antSpecAvgN = makeDomElement('manual-ant-spec-avg-n', { value: '10', hidden: false });
_domNodes.set('manual-ant-spec-avg-n', antSpecAvgN);
// Вкладка «Демодулятор»: переключатель Таблица/Диаграмма + кнопки записи и прослушивания
const resultTabTable = makeDomElement('result-tab-table', { _attrs: { 'data-view': 'table', 'aria-selected': 'true' } });
const resultTabChart = makeDomElement('result-tab-chart', { _attrs: { 'data-view': 'chart', 'aria-selected': 'false' } });
const resultViewTable = makeDomElement('result-view-table', { _attrs: { 'data-view': 'table' } });
const resultViewChart = makeDomElement('result-view-chart', { _attrs: { 'data-view': 'chart' } });
resultViewTable.classList._set.add('is-active');

const resultCol = makeDomElement('result-col', {
    querySelectorAll: (sel) => (sel.indexOf('ml-demod__result-view') >= 0
        ? [resultViewTable, resultViewChart]
        : []),
});
_domNodes.set('manual-result-tabs', makeDomElement('manual-result-tabs', {
    closest: () => resultCol,
    querySelectorAll: () => [resultTabTable, resultTabChart],
}));
// Секция «Данные»: папка записи и транспорт ● / ■
_domNodes.set('manual-rec-path', makeDomElement('manual-rec-path', { value: '', readOnly: false }));
_domNodes.set('manual-rec-start', makeDomElement('manual-rec-start', { disabled: false }));
_domNodes.set('manual-rec-stop', makeDomElement('manual-rec-stop', { disabled: false }));

// Tabs = SDR mode: Panorama (SWEEP) | Spectrum (HOLD) | Antenna (view only)
const tabPanorama = makeDomElement('tab-panorama', { _attrs: { 'data-tab': 'panorama' } });
const tabSpectrum = makeDomElement('tab-spectrum', { _attrs: { 'data-tab': 'spectrum' } });
const tabAntenna = makeDomElement('tab-antenna', { _attrs: { 'data-tab': 'antenna' } });
const panePanorama = makeDomElement('pane-panorama', { _attrs: { 'data-pane': 'panorama' } });
const paneSpectrum = makeDomElement('pane-spectrum', { _attrs: { 'data-pane': 'spectrum' } });
const paneAntenna = makeDomElement('pane-antenna', { _attrs: { 'data-pane': 'antenna' } });
_domNodes.set('manual-tabbar', makeDomElement('manual-tabbar', {
    querySelectorAll: () => [tabPanorama, tabSpectrum, tabAntenna],
}));
(function wireLayoutRoot() {
    const root = _domNodes.get('layout-manual');
    root._attrs = {};
    root.setAttribute = function(k, v) { this._attrs[k] = String(v); };
    root.getAttribute = function(k) { return this._attrs[k] || null; };
    root.querySelectorAll = (sel) => (sel.indexOf('ml-tabpane') >= 0
        ? [panePanorama, paneSpectrum, paneAntenna]
        : []);
})();

function MockDemodPanel() {
    this.calls = [];
}
MockDemodPanel.prototype.setChannel = function(ch) { this.calls.push(['setChannel', ch]); };
MockDemodPanel.prototype.setLive = function(live) { this.calls.push(['setLive', live]); };
MockDemodPanel.prototype.applyStatus = function(st, off) { this.calls.push(['applyStatus', st, off]); };
MockDemodPanel.prototype.destroy = function() { this.calls.push(['destroy']); };
global.window.DemodPanel = MockDemodPanel;
global.window.formatDemodFreqMHz = (f) => f.toFixed(3).replace('.', ',');

// TelemetryTable mock
function MockTelemetryTable() {
    this.calls = [];
}
MockTelemetryTable.prototype.activate = function() { this.calls.push(['activate']); };
MockTelemetryTable.prototype.deactivate = function() { this.calls.push(['deactivate']); };
MockTelemetryTable.prototype.setChannel = function(label, freq) { this.calls.push(['setChannel', label, freq]); };
MockTelemetryTable.prototype.clear = function() { this.calls.push(['clear']); };
MockTelemetryTable.prototype.setLive = function(live) { this.calls.push(['setLive', live]); };
MockTelemetryTable.prototype.addTelemetry = function(ev) { this.calls.push(['addTelemetry', ev]); };
MockTelemetryTable.prototype.destroy = function() { this.calls.push(['destroy']); };
global.window.TelemetryTable = MockTelemetryTable;

// EyeConstellationView mock
function MockEyeView() {
    this.calls = [];
}
MockEyeView.prototype.activate = function() { this.calls.push(['activate']); };
MockEyeView.prototype.deactivate = function() { this.calls.push(['deactivate']); };
MockEyeView.prototype.setModulation = function(m) { this.calls.push(['setModulation', m]); };
MockEyeView.prototype.refreshAfterThemeChange = function() { this.calls.push(['refreshAfterThemeChange']); };
MockEyeView.prototype.destroy = function() { this.calls.push(['destroy']); };
global.window.EyeConstellationView = MockEyeView;
_domNodes.set('manual-eye-canvas', makeCanvas('manual-eye-canvas'));

function makeSelect(id, options, selectedIndex) {
    const opts = options.map(function(text, idx) {
        return { text: text, selected: idx === selectedIndex };
    });
    return {
        id: id,
        selectedIndex: selectedIndex || 0,
        options: opts,
    };
}
// Pipeline select с поддержкой addEventListener (для _initRxPanel)
const pipelineSel = makeSelect('manual-rx-pipeline', ['FSK 9600', 'FSK 1200', 'AFSK 1200', 'BPSK 1k2'], 0);
['fsk9600_g3ruh', 'fsk1200', 'afsk1200', 'bpsk1200'].forEach((v, i) => { pipelineSel.options[i].value = v; });
pipelineSel._listeners = {};
pipelineSel.addEventListener = function(type, fn) {
    if (!this._listeners[type]) { this._listeners[type] = []; }
    this._listeners[type].push(fn);
};
pipelineSel.removeEventListener = function(type, fn) {
    const arr = this._listeners[type] || [];
    const idx = arr.indexOf(fn);
    if (idx >= 0) { arr.splice(idx, 1); }
};
_domNodes.set('manual-rx-pipeline', pipelineSel);
const filterBwSel = makeSelect('manual-rx-filter-bw', ['2.8 кГц', '6 кГц', '15 кГц', '18 кГц'], 2);
['2800', '6000', '15000', '18000'].forEach((v, i) => { filterBwSel.options[i].value = v; });
filterBwSel._listeners = {};
filterBwSel.addEventListener = pipelineSel.addEventListener;
filterBwSel.removeEventListener = pipelineSel.removeEventListener;
_domNodes.set('manual-spectrum', makeDomElement('manual-spectrum'));
// «Полоса»: пункты перестраиваются по sample_rates статуса
const spanSel = { id: 'manual-rx-span', selectedIndex: 0, options: [] };
spanSel.add = function(opt) { this.options.push(opt); };
spanSel.remove = function(i) { this.options.splice(i, 1); };
function resetSpanOptions() {
    spanSel.options = [['2048000', '2.048 МГц'], ['1024000', '1.024 МГц'], ['512000', '512 кГц'], ['256000', '256 кГц']]
        .map(([value, text]) => ({ value: value, text: text }));
    spanSel.selectedIndex = 0;
}
resetSpanOptions();
_domNodes.set('manual-rx-span', spanSel);
function spanValue() { return spanSel.options[spanSel.selectedIndex].value; }
_domNodes.set('manual-rx-filter-bw', filterBwSel);

// StateEventType — read by the module from window
global.window.StateEventType = {
    POSITION: 'position',
    TRACKING_CHANGE: 'tracking_change',
    SELECTED_CHANGE: 'selected_change',
};

// ── Minimal StateManager for tests ────────────────────────────

function makeStateManager() {
    const subs = { position: [], tracking_change: [], selected_change: [] };
    return {
        _trackingId: null,
        _selectedId: null,
        _state: new Map(),
        getTrackingSatelliteId() { return this._trackingId; },
        getSelectedSatelliteId() { return this._selectedId; },
        getState(id) { return this._state.get(id) || null; },
        subscribe(event, cb) {
            if (!subs[event]) { subs[event] = []; }
            subs[event].push(cb);
            return () => {
                const arr = subs[event];
                const idx = arr.indexOf(cb);
                if (idx >= 0) { arr.splice(idx, 1); }
            };
        },
        emit(event, payload) {
            for (const cb of (subs[event] || [])) { cb(payload); }
        },
        listeners(event) { return subs[event] || []; },
    };
}

// ── Module under test ─────────────────────────────────────────

const { ManualLayout } = require('./manual-layout.js');

// ── Tiny test runner ──────────────────────────────────────────

let passed = 0;
let failed = 0;

function test(name, fn) {
    try {
        fn();
        passed++;
        console.log(`  ✓ ${name}`);
    } catch (err) {
        failed++;
        console.error(`  ✗ ${name}`);
        console.error(`    ${err.stack || err.message}`);
    }
}

// ── Tests ─────────────────────────────────────────────────────

console.log('\n— ManualLayout: constructor —');

test('constructor without StateManager does not crash', () => {
    const m = new ManualLayout(null);
    assert.strictEqual(m.isActive(), false);
    m.destroy();
});

test('constructor instantiates Az/El/FFT/WF widgets from window classes', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.ok(m._az, 'AzimuthIndicator must be created');
    assert.ok(m._el, 'ElevationIndicator must be created');
    assert.ok(m._fft, 'FFTSpectrumView must be created');
    assert.ok(m._wf, 'WaterfallView must be created');
    m.destroy();
});

test('constructor wires info element ids to indicators', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const azCalls = m._az.calls.filter((c) => c[0] === 'setInfoElements');
    const elCalls = m._el.calls.filter((c) => c[0] === 'setInfoElements');
    assert.strictEqual(azCalls.length, 1);
    assert.deepStrictEqual(azCalls[0][1], { ant: 'manual-az-info-ant', sat: 'manual-az-info-sat' });
    assert.strictEqual(elCalls.length, 1);
    assert.deepStrictEqual(elCalls[0][1], { ant: 'manual-el-info-ant', sat: 'manual-el-info-sat' });
    m.destroy();
});

console.log('\n— ManualLayout: activate / deactivate —');

test('activate starts on Spectrum; a panorama request does not start the sweep', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.strictEqual(m.isActive(), false);
    m.activate();
    assert.strictEqual(m.isActive(), true);
    assert.strictEqual(m._view, 'spectrum');
    assert.ok(m._spectrumTimer, 'spectrum view must run the timer');
    m._setView('panorama');
    assert.strictEqual(m._view, 'spectrum', 'panorama is off, so the sweep is not started');
    assert.ok(m._spectrumTimer, 'the spectrum timer stays while panorama is off');
    const sweep = m._panorama.calls.filter((c) => c[0] === 'activate');
    assert.strictEqual(sweep.length, 0);
    m.deactivate();
    assert.strictEqual(m.isActive(), false);
    assert.strictEqual(m._spectrumTimer, null, 'spectrum timer must be cleared');
    m.destroy();
});

test('repeated activate does not start a second timer', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    const t1 = m._spectrumTimer;
    m.activate();
    assert.strictEqual(m._spectrumTimer, t1);
    m.destroy();
});

console.log('\n— ManualLayout: subscriptions and tracking → selected priority —');

test('constructor subscribes to POSITION/TRACKING_CHANGE/SELECTED_CHANGE', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.strictEqual(sm.listeners('position').length, 1);
    assert.strictEqual(sm.listeners('tracking_change').length, 1);
    assert.strictEqual(sm.listeners('selected_change').length, 1);
    m.destroy();
});

test('when tracking sat is set, indicators receive its az/el', () => {
    const sm = makeStateManager();
    sm._trackingId = 25544;
    sm._selectedId = 99999; // selected must NOT win over tracking
    sm._state.set(25544, { position: { az: 162, el: 67 } });
    sm._state.set(99999, { position: { az: 0, el: 0 } });

    const m = new ManualLayout(sm);
    sm.emit('position', null);

    const azSet = m._az.calls.find((c) => c[0] === 'setSatellitePosition');
    const elSet = m._el.calls.find((c) => c[0] === 'setSatellitePosition');
    assert.deepStrictEqual(azSet, ['setSatellitePosition', 162, undefined]);
    assert.deepStrictEqual(elSet, ['setSatellitePosition', 67, 162]);

    const azNorad = m._az.calls.find((c) => c[0] === 'setNoradId');
    assert.deepStrictEqual(azNorad, ['setNoradId', 25544]);
    m.destroy();
});

test('without tracking but with selected — indicators take selected', () => {
    const sm = makeStateManager();
    sm._trackingId = null;
    sm._selectedId = 25544;
    sm._state.set(25544, { position: { az: 200, el: 30 } });

    const m = new ManualLayout(sm);
    sm.emit('selected_change', null);

    const azSet = m._az.calls.find((c) => c[0] === 'setSatellitePosition');
    assert.deepStrictEqual(azSet, ['setSatellitePosition', 200, undefined]);
    m.destroy();
});

test('without tracking and selected — indicators are cleared', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    sm.emit('position', null);

    const azClear = m._az.calls.filter((c) => c[0] === 'setSatellitePosition' && c[1] === null);
    const elClear = m._el.calls.filter((c) => c[0] === 'setSatellitePosition' && c[1] === null);
    assert.ok(azClear.length >= 1, 'Az must be cleared');
    assert.ok(elClear.length >= 1, 'El must be cleared');
    m.destroy();
});

console.log('\n— ManualLayout: destroy —');

test('destroy unsubscribes all callbacks from StateManager', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.strictEqual(sm.listeners('position').length, 1);
    m.destroy();
    assert.strictEqual(sm.listeners('position').length, 0);
    assert.strictEqual(sm.listeners('tracking_change').length, 0);
    assert.strictEqual(sm.listeners('selected_change').length, 0);
});

test('destroy after activate stops the spectrum timer', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    m._setView('spectrum');
    assert.ok(m._spectrumTimer, 'spectrum timer running in spectrum view');
    m.destroy();
    assert.strictEqual(m._spectrumTimer, null, 'destroy clears the spectrum timer');
});

console.log('\n— ManualLayout: panorama integration —');

test('constructor creates ManualPanorama instance', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.ok(m._panorama, 'panorama must be created');
    assert.ok(m._panorama instanceof MockPanorama, 'panorama must be MockPanorama');
    m.destroy();
});

test('activate does not start the panorama imitator', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    const activateCalls = m._panorama.calls.filter((c) => c[0] === 'activate');
    assert.strictEqual(activateCalls.length, 0);
    m.destroy();
});

test('deactivate calls panorama.deactivate', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    m.deactivate();
    const deactivateCalls = m._panorama.calls.filter((c) => c[0] === 'deactivate');
    assert.strictEqual(deactivateCalls.length, 2, 'spectrum entry and leaving Manual both stop the sweep');
    m.destroy();
});

test('destroy calls panorama.destroy and nullifies', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const pan = m._panorama;
    m.destroy();
    assert.strictEqual(m._panorama, null);
    const destroyCalls = pan.calls.filter((c) => c[0] === 'destroy');
    assert.strictEqual(destroyCalls.length, 1);
});

console.log('\n— ManualLayout: pass profile integration —');

test('constructor creates PassProfileView instance', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.ok(m._passProfile, 'pass profile must be created');
    assert.ok(m._passProfile instanceof MockPassProfile);
    m.destroy();
});

test('activate calls passProfile.draw', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    const drawCalls = m._passProfile.calls.filter((c) => c[0] === 'draw');
    assert.ok(drawCalls.length >= 1, 'draw must be called on activate');
    m.destroy();
});

test('destroy nullifies passProfile', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.destroy();
    assert.strictEqual(m._passProfile, null);
});

console.log('\n— ManualLayout: demod + panorama:tune —');

test('constructor creates DemodPanel and subscribes to panorama:tune', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.ok(m._demodPanel, 'DemodPanel must be created');
    assert.ok(m._onPanoramaTune, 'panorama:tune handler must be set');
    m.destroy();
});

test('panorama:tune updates demod and the dial, and tunes the receiver', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        const layoutEl = _domNodes.get('layout-manual');
        const tx = { freqMHz: 435.25, label: 'AO-91 BPSK', modulation: 'BPSK31', active: true };

        layoutEl.dispatchEvent({ type: 'panorama:tune', detail: { freqMHz: 435.25, tx: tx } });

        const setCalls = m._demodPanel.calls.filter((c) => c[0] === 'setChannel');
        assert.strictEqual(setCalls.length, 1);
        assert.strictEqual(setCalls[0][1].label, 'AO-91 BPSK');
        assert.strictEqual(m._freqDial.getHz(), 435250000);
        assert.deepStrictEqual(tuneCalls().map((c) => c.body), [{ radio_path: 'radio_path_1', center_hz: 435250000 }]);
        m.destroy();
    });
});

console.log('\n— ManualLayout: telemetry table —');

test('constructor creates TelemetryTable instance', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.ok(m._tmiTable, 'TelemetryTable must be created');
    assert.ok(m._tmiTable instanceof MockTelemetryTable);
    m.destroy();
});

test('activate calls tmiTable.activate', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    const activateCalls = m._tmiTable.calls.filter((c) => c[0] === 'activate');
    assert.strictEqual(activateCalls.length, 1);
    m.destroy();
});

test('deactivate calls tmiTable.deactivate', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    m.deactivate();
    const deactivateCalls = m._tmiTable.calls.filter((c) => c[0] === 'deactivate');
    assert.strictEqual(deactivateCalls.length, 1);
    m.destroy();
});

test('panorama:tune updates tmiTable channel without clearing', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const layoutEl = _domNodes.get('layout-manual');
    const tx = { freqMHz: 435.25, label: 'AO-91', modulation: 'BPSK31', active: true };
    layoutEl.dispatchEvent({ type: 'panorama:tune', detail: { freqMHz: 435.25, tx: tx } });

    const chCalls = m._tmiTable.calls.filter((c) => c[0] === 'setChannel');
    assert.strictEqual(chCalls.length, 1);
    assert.strictEqual(chCalls[0][1], 'AO-91');
    assert.strictEqual(chCalls[0][2], 435.25);
    const clearCalls = m._tmiTable.calls.filter((c) => c[0] === 'clear');
    assert.strictEqual(clearCalls.length, 0);
    m.destroy();
});

test('destroy calls tmiTable.destroy and nullifies', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const tmi = m._tmiTable;
    m.destroy();
    assert.strictEqual(m._tmiTable, null);
    const destroyCalls = tmi.calls.filter((c) => c[0] === 'destroy');
    assert.strictEqual(destroyCalls.length, 1);
});

console.log('\n— ManualLayout: eye/constellation —');

test('constructor creates EyeConstellationView instance', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.ok(m._eyeView, 'EyeConstellationView must be created');
    assert.ok(m._eyeView instanceof MockEyeView);
    m.destroy();
});

test('activate calls eyeView.activate', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    const calls = m._eyeView.calls.filter((c) => c[0] === 'activate');
    assert.strictEqual(calls.length, 1);
    m.destroy();
});

test('deactivate calls eyeView.deactivate', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    m.deactivate();
    const calls = m._eyeView.calls.filter((c) => c[0] === 'deactivate');
    assert.strictEqual(calls.length, 1);
    m.destroy();
});

test('panorama:tune calls eyeView.setModulation from pipeline', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const layoutEl = _domNodes.get('layout-manual');
    const tx = { freqMHz: 435.25, label: 'AO-91', modulation: 'BPSK31', active: true };
    layoutEl.dispatchEvent({ type: 'panorama:tune', detail: { freqMHz: 435.25, tx: tx } });

    const modCalls = m._eyeView.calls.filter((c) => c[0] === 'setModulation');
    assert.ok(modCalls.length >= 1);
    assert.strictEqual(modCalls[modCalls.length - 1][1], 'BPSK 1k2');
    m.destroy();
});

test('init syncs eye diagram from default pipeline select', () => {
    _domNodes.get('manual-rx-pipeline').selectedIndex = 0;
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const modCalls = m._eyeView.calls.filter((c) => c[0] === 'setModulation');
    assert.ok(modCalls.length >= 1);
    assert.strictEqual(modCalls[0][1], 'FSK 9600');
    m.destroy();
});

test('destroy calls eyeView.destroy and nullifies', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const ev = m._eyeView;
    m.destroy();
    assert.strictEqual(m._eyeView, null);
    const calls = ev.calls.filter((c) => c[0] === 'destroy');
    assert.strictEqual(calls.length, 1);
});

console.log('\n— ManualLayout: RxPanel tuning —');

test('constructor creates the freq dial seeded with the current frequency', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.ok(m._freqDial, 'FreqDial must be created');
    assert.strictEqual(m._freqDial.hz, Math.round(m._rxFreqMHz * 1e6));
    assert.strictEqual(m._freqDial.root, _domNodes.get('manual-rx-dial'));
    m.destroy();
});

test('_setRxFreqMHz pushes the frequency onto the dial', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m._setRxFreqMHz(437.80252);
    assert.strictEqual(m._freqDial.getHz(), 437802520);
    m.destroy();
});

test('_setRxFreqMHz ignores a non-finite frequency', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m._setRxFreqMHz(437.8);
    m._setRxFreqMHz(NaN);
    assert.strictEqual(m._freqDial.getHz(), 437800000);
    m.destroy();
});

test('dial SET tunes the receiver; the views wait for the stream to move', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        const centerBefore = m._fft.freqCenterMHz;
        m._freqDial.emit(437801000);
        assert.ok(Math.abs(m._rxFreqMHz - 437.801) < 1e-9, `got ${m._rxFreqMHz}`);
        assert.deepStrictEqual(tuneCalls().map((c) => c.body), [{ radio_path: 'radio_path_1', center_hz: 437801000 }]);
        assert.strictEqual(m._fft.freqCenterMHz, centerBefore, 'data on screen is still the old tuning');
        m.destroy();
    });
});

test('destroy tears the dial down', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const dial = m._freqDial;
    m.destroy();
    assert.ok(dial.calls.some((c) => c[0] === 'destroy'), 'dial.destroy must be called');
    assert.strictEqual(m._freqDial, null);
});

function markerText() {
    return ['freq', 'level'].map((k) => _domNodes.get('manual-rx-marker-' + k).textContent);
}

test('marker readout shows dashes until the first frame', () => {
    const m = new ManualLayout(makeStateManager());
    assert.deepStrictEqual(markerText(), ['—', '—']);
    assert.strictEqual(_domNodes.get('manual-rx-marker-bar').style.width, '0.0%');
    m.destroy();
});

test('level bar clamps out-of-range values to 0..100%', () => {
    const m = new ManualLayout(makeStateManager());
    m._showMarker({ freqHz: 436500000, dbfs: -200, floorDbfs: -210, snrDb: 10 });
    assert.strictEqual(_domNodes.get('manual-rx-marker-bar').style.width, '0.0%');
    m._showMarker({ freqHz: 436500000, dbfs: 20, floorDbfs: -90, snrDb: 110 });
    assert.strictEqual(_domNodes.get('manual-rx-marker-bar').style.width, '100.0%');
    m.destroy();
});

console.log('\n— ManualLayout: demod result view —');

test('switching back to Table restores the table view', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    fire(resultTabChart, 'click');
    fire(resultTabTable, 'click');
    assert.strictEqual(resultTabTable.getAttribute('aria-selected'), 'true');
    assert.strictEqual(resultViewTable.classList.contains('is-active'), true);
    assert.strictEqual(resultViewChart.classList.contains('is-active'), false);
    m.destroy();
});

test('switching to Chart activates the graph view and resizes the eye canvas', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m._eyeView._resize = function() { this.calls.push(['_resize']); };
    fire(resultTabChart, 'click');
    assert.strictEqual(resultTabChart.getAttribute('aria-selected'), 'true');
    assert.strictEqual(resultViewChart.classList.contains('is-active'), true);
    assert.strictEqual(resultViewTable.classList.contains('is-active'), false);
    // requestAnimationFrame в тестах синхронный
    const resizes = m._eyeView.calls.filter((c) => c[0] === '_resize');
    assert.strictEqual(resizes.length, 1, 'hidden canvas must be re-measured once shown');
    fire(resultTabTable, 'click');
    m.destroy();
});

test('Chart view shows the pass profile next to the diagram and redraws it', () => {
    const m = new ManualLayout(makeStateManager());
    const before = m._passProfile.calls.filter((c) => c[0] === 'draw').length;
    fire(resultTabChart, 'click');
    const after = m._passProfile.calls.filter((c) => c[0] === 'draw').length;
    assert.strictEqual(after, before + 1, 'profile canvas becomes visible together with the diagram');
    fire(resultTabTable, 'click');
    m.destroy();
});

console.log('\n— ManualLayout: pass timeline —');

test('constructor creates PassTimelineView instance', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.ok(m._passTimeline instanceof MockPassTimeline);
    m.destroy();
});

test('activate draws the pass timeline', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    const calls = m._passTimeline.calls.filter((c) => c[0] === 'draw');
    assert.strictEqual(calls.length, 1);
    m.destroy();
});

test('tracking change forwards the norad id to the timeline', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    sm._trackingId = 25544;
    sm.emit('tracking_change', {});
    const calls = m._passTimeline.calls.filter((c) => c[0] === 'setTrackingId');
    assert.strictEqual(calls.length, 1);
    assert.strictEqual(calls[0][1], 25544);
    m.destroy();
});

test('theme change refreshes the pass timeline', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.refreshAfterThemeChange();
    const calls = m._passTimeline.calls.filter((c) => c[0] === 'refreshAfterThemeChange');
    assert.strictEqual(calls.length, 1);
    m.destroy();
});

test('destroy nullifies the pass timeline', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.destroy();
    assert.strictEqual(m._passTimeline, null);
});

console.log('\n— ManualLayout: IQ recorder —');

test('recording name carries channel and UTC start time', () => {
    const at = new Date(Date.UTC(2026, 9, 7, 18, 5, 9));
    assert.strictEqual(ManualLayout.makeRecordingName('ISS-FSK', at),
        'ISS-FSK_20261007_180509Z.sigmf-data');
});

test('recording name keeps only file-safe characters', () => {
    const at = new Date(Date.UTC(2026, 0, 2, 3, 4, 5));
    assert.strictEqual(ManualLayout.makeRecordingName(' RS30 · FSK ', at),
        'RS30_FSK_20260102_030405Z.sigmf-data');
    assert.strictEqual(ManualLayout.makeRecordingName('', at), 'iq_20260102_030405Z.sigmf-data');
});

/** Свежий экземпляр с секцией записи в исходном состоянии. */
function makeRecorder() {
    const path = _domNodes.get('manual-rec-path');
    const start = _domNodes.get('manual-rec-start');
    const stop = _domNodes.get('manual-rec-stop');
    path.value = '';
    path.readOnly = false;
    start.disabled = false;
    stop.disabled = false;
    const m = new ManualLayout(makeStateManager());
    m._chanLabel = 'ISS-FSK';
    return { m, path, start, stop };
}

test('recording is named after the transmitter the receiver is tuned to', () => {
    const { m, path, start } = makeRecorder();
    m._tuneChannel({ freqMHz: 435.25, tx: { label: 'AO-91 BPSK', modulation: 'BPSK31' } });
    fire(start, 'click');
    assert.ok(/^data\/recordings\/AO-91_BPSK_\d{8}_\d{6}Z\.sigmf-data$/.test(path.value), path.value);
    m.destroy();
});

test('idle recorder shows the default folder and only REC is enabled', () => {
    const { m, path, start, stop } = makeRecorder();
    assert.strictEqual(path.value, 'data/recordings/');
    assert.strictEqual(path.readOnly, false);
    assert.strictEqual(start.disabled, false);
    assert.strictEqual(stop.disabled, true);
    m.destroy();
});

test('REC shows the generated file path and locks the folder', () => {
    const { m, path, start, stop } = makeRecorder();
    fire(start, 'click');
    assert.ok(/^data\/recordings\/ISS-FSK_\d{8}_\d{6}Z\.sigmf-data$/.test(path.value), path.value);
    assert.strictEqual(path.readOnly, true);
    assert.strictEqual(start.disabled, true);
    assert.strictEqual(start.getAttribute('aria-pressed'), 'true');
    assert.strictEqual(stop.disabled, false);
    m.destroy();
});

test('STOP returns the recorder to idle with the folder', () => {
    const { m, path, start, stop } = makeRecorder();
    fire(start, 'click');
    fire(stop, 'click');
    assert.strictEqual(path.value, 'data/recordings/');
    assert.strictEqual(path.readOnly, false);
    assert.strictEqual(start.disabled, false);
    assert.strictEqual(start.getAttribute('aria-pressed'), 'false');
    assert.strictEqual(stop.disabled, true);
    m.destroy();
});

test('operator folder is used for the next recording', () => {
    const { m, path, start } = makeRecorder();
    path.value = '/mnt/iq';
    fire(path, 'change');
    fire(start, 'click');
    assert.ok(path.value.startsWith('/mnt/iq/ISS-FSK_'), path.value);
    m.destroy();
});

test('empty folder falls back to the default', () => {
    const { m, path } = makeRecorder();
    path.value = '   ';
    fire(path, 'change');
    assert.strictEqual(path.value, 'data/recordings/');
    m.destroy();
});

console.log('\n— ManualLayout: tabs drive the SDR mode —');

function activeTabOf(m) {
    const tabs = [tabPanorama, tabSpectrum, tabAntenna]
        .filter((t) => t.getAttribute('aria-selected') === 'true')
        .map((t) => t.getAttribute('data-tab'));
    const panes = [panePanorama, paneSpectrum, paneAntenna]
        .filter((p) => p.classList.contains('is-active'))
        .map((p) => p.getAttribute('data-pane'));
    assert.deepStrictEqual(tabs, panes, 'selected tab and visible pane must match');
    assert.strictEqual(tabs.length, 1, 'exactly one tab must be selected');
    assert.strictEqual(_domNodes.get('layout-manual').getAttribute('data-tab'), tabs[0]);
    assert.strictEqual(m._activeTab, tabs[0]);
    return tabs[0];
}

test('entering Manual opens the Spectrum tab and does not sweep', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    assert.strictEqual(activeTabOf(m), 'spectrum');
    assert.strictEqual(m._view, 'spectrum');
    assert.ok(m._spectrumTimer);
    m.destroy();
});

test('Spectrum tab switches the SDR to HOLD', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabSpectrum, 'click');
    assert.strictEqual(activeTabOf(m), 'spectrum');
    assert.strictEqual(m._view, 'spectrum');
    assert.ok(m._spectrumTimer, 'HOLD must run the spectrum timer');
    m.destroy();
});

test('hidden Panorama tab does not leave Spectrum', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabSpectrum, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(activeTabOf(m), 'spectrum');
    assert.strictEqual(m._view, 'spectrum');
    assert.ok(m._spectrumTimer);
    m.destroy();
});

const AO91 = { freqMHz: 435.25, label: 'AO-91', modulation: 'BPSK', noradId: 43017 };
const ISS_FSK = { freqMHz: 437.8, label: 'ISS-FSK', modulation: 'FSK 1k2', noradId: 25544 };

test('Antenna from Spectrum keeps the channel and the dial retune', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    tuneTo(AO91.freqMHz, AO91);
    m._freqDial.emit(435260000);
    m._panorama._selected = ISS_FSK;
    fire(tabAntenna, 'click');
    assert.strictEqual(activeTabOf(m), 'antenna');
    assert.strictEqual(m._view, 'spectrum', 'reception must go on');
    assert.ok(m._spectrumTimer);
    assert.strictEqual(m._freqDial.getHz(), 435260000, 'Spectrum frequency wins over the panorama selection');
    m.destroy();
});

test('Antenna does not tune from a panorama selection while the tab is hidden', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    m._panorama._selected = AO91;
    fire(tabAntenna, 'click');
    assert.strictEqual(activeTabOf(m), 'antenna');
    assert.strictEqual(m._view, 'spectrum');
    assert.ok(m._spectrumTimer);
    assert.strictEqual(m._freqDial.getHz(), 436000000, 'sweep selection is not applied');
    m.destroy();
});

test('Antenna from Panorama without a selection resumes the last channel', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    tuneTo(AO91.freqMHz, AO91);
    m._freqDial.emit(435260000);
    fire(tabPanorama, 'click');
    assert.strictEqual(m._view, 'spectrum', 'hidden panorama tab is ignored');
    fire(tabAntenna, 'click');
    assert.strictEqual(m._view, 'spectrum');
    assert.strictEqual(m._freqDial.getHz(), 435260000, 'dial retune is kept');
    m.destroy();
});

test('selection equal to the last channel does not undo the dial retune', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    tuneTo(AO91.freqMHz, AO91);
    m._freqDial.emit(435260000);
    fire(tabPanorama, 'click');
    m._panorama._selected = AO91;
    fire(tabAntenna, 'click');
    assert.strictEqual(m._freqDial.getHz(), 435260000);
    m.destroy();
});

test('with nothing chosen Antenna holds without retuning the shared receiver', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabAntenna, 'click');
        assert.strictEqual(activeTabOf(m), 'antenna');
        assert.strictEqual(m._view, 'spectrum');
        assert.strictEqual(tuneCalls().length, 0, 'other viewers keep their picture');
        assert.strictEqual(m._expectedFreqMHz(), null, 'no transmitter, no expected frequency');
        m.destroy();
    });
});

test('hidden Panorama tab does not leave Antenna', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabAntenna, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(activeTabOf(m), 'antenna');
    assert.strictEqual(m._view, 'spectrum');
    assert.ok(m._spectrumTimer);
    m.destroy();
});

test('a panorama request while on Antenna stays on Antenna', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabAntenna, 'click');
    m._setView('panorama');
    assert.strictEqual(activeTabOf(m), 'antenna');
    assert.strictEqual(m._view, 'spectrum');
    m.destroy();
});

test('tuning to a transmitter opens the Spectrum tab', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    _domNodes.get('layout-manual').dispatchEvent({
        type: 'panorama:tune',
        detail: { freqMHz: 437.8, tx: { label: 'ISS-FSK', modulation: 'FSK 9600' } },
    });
    assert.strictEqual(activeTabOf(m), 'spectrum');
    assert.strictEqual(m._view, 'spectrum');
    m.destroy();
});

test('re-entering Manual starts on Spectrum', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabAntenna, 'click');
    m.deactivate();
    m.activate();
    assert.strictEqual(activeTabOf(m), 'spectrum');
    assert.strictEqual(m._view, 'spectrum');
    m.destroy();
});

console.log('\n— ManualLayout: leaving HOLD while recording —');

test('hidden Panorama tab does not interrupt a recording', () => {
    const { m, start } = makeRecorder();
    let asked = 0;
    m._confirm = () => { asked++; return false; };
    m.activate();
    fire(tabSpectrum, 'click');
    fire(start, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(asked, 0);
    assert.strictEqual(activeTabOf(m), 'spectrum');
    assert.strictEqual(m._view, 'spectrum');
    assert.ok(m._rec.file, 'recording must go on');
    m.destroy();
});

test('confirming a hidden Panorama click does not stop the recording', () => {
    const { m, start, stop } = makeRecorder();
    m._confirm = () => true;
    m.activate();
    fire(tabSpectrum, 'click');
    fire(start, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(activeTabOf(m), 'spectrum');
    assert.ok(m._rec.file, 'hidden tab does not leave the channel');
    assert.strictEqual(stop.disabled, false);
    m.destroy();
});

test('Panorama without a recording does not ask', () => {
    const { m } = makeRecorder();
    let asked = 0;
    m._confirm = () => { asked++; return true; };
    m.activate();
    fire(tabSpectrum, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(asked, 0);
    m.destroy();
});

test('Antenna while recording does not ask and keeps recording', () => {
    const { m, start } = makeRecorder();
    let asked = 0;
    m._confirm = () => { asked++; return false; };
    m.activate();
    fire(tabSpectrum, 'click');
    fire(start, 'click');
    fire(tabAntenna, 'click');
    assert.strictEqual(asked, 0);
    assert.ok(m._rec.file);
    m.destroy();
});

test('hidden Panorama tab from Antenna does not ask while recording', () => {
    const { m, start } = makeRecorder();
    let asked = 0;
    m._confirm = () => { asked++; return false; };
    m.activate();
    fire(tabSpectrum, 'click');
    fire(start, 'click');
    fire(tabAntenna, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(asked, 0);
    assert.strictEqual(activeTabOf(m), 'antenna');
    assert.ok(m._rec.file, 'recording must go on');
    m.destroy();
});

console.log('\n— ManualLayout: slew controls availability —');

function withRotControls(m) {
    m._rotSlewEl = makeDomElement('manual-rot-slew', { title: '' });
    m._rotGoBtn = { disabled: false };
    m._rotNudges = [{ disabled: false }];
    m._rotStepButtons = [{ disabled: false }];
    return m;
}

test('without a rotator the slew controls stay visible but locked', () => {
    const m = withRotControls(new ManualLayout(makeStateManager()));
    m._rotHasRotator = false;
    m._applyRotAvailability();
    assert.strictEqual(m._rotGoBtn.disabled, true);
    assert.strictEqual(m._rotNudges[0].disabled, true);
    assert.strictEqual(m._rotSlewEl.title, 'Неподвижная');
    m.destroy();
});

test('rotator in Auto locks the slew controls', () => {
    const m = withRotControls(new ManualLayout(makeStateManager()));
    m._rotHasRotator = true;
    m._rotMode = 'auto';
    m._applyRotAvailability();
    assert.strictEqual(m._rotGoBtn.disabled, true);
    assert.strictEqual(m._rotSlewEl.title, 'Авто');
    m.destroy();
});

test('rotator in Manual unlocks the slew controls', () => {
    const m = withRotControls(new ManualLayout(makeStateManager()));
    m._rotHasRotator = true;
    m._rotMode = 'manual';
    m._applyRotAvailability();
    assert.strictEqual(m._rotGoBtn.disabled, false);
    assert.strictEqual(m._rotSlewEl.title, '');
    m.destroy();
});

console.log('\n— ManualLayout: compact spectrum on the Antenna tab —');

function antDraws(m) {
    return m._antFft.calls.filter((c) => c[0] === 'draw').length;
}

function tuneTo(freqMHz, tx) {
    _domNodes.get('layout-manual').dispatchEvent({
        type: 'panorama:tune',
        detail: { freqMHz: freqMHz, tx: tx },
    });
}

test('compact spectrum draws stream frames only while Antenna is open', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        assert.ok(m._antFft, 'compact spectrum view must be created');
        m.activate();
        fire(tabSpectrum, 'click');
        MockStream.last().opts.onFrame(fftFrame(435250000));
        assert.strictEqual(antDraws(m), 0, 'hidden on the Spectrum tab');
        fire(tabAntenna, 'click');
        MockStream.last().opts.onFrame(fftFrame(435250000));
        assert.strictEqual(antDraws(m), 1);
        m.destroy();
    });
});

test('compact spectrum follows the receiver frequency from the stream', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        MockStream.last().opts.onFrame(fftFrame(435250000));
        assert.strictEqual(m._antFft.freqCenterMHz, 435.25);
        MockStream.last().opts.onFrame(fftFrame(435260000));
        assert.strictEqual(m._antFft.freqCenterMHz, 435.26);
        m.destroy();
    });
});

test('expected frequency is the tuned transmitter shifted by Doppler', () => {
    const sm = makeStateManager();
    sm._state.set(43017, { position: { az: 10, el: 20, range_rate: 6000 } });
    const m = new ManualLayout(sm);
    tuneTo(435.25, { label: 'AO-91', modulation: 'BPSK', noradId: 43017 });
    const receding = 435.25 * (1 - 6000 / 299792458);
    assert.ok(Math.abs(m._expectedFreqMHz() - receding) < 1e-9, 'receding satellite is heard lower');
    sm._state.set(43017, { position: { az: 10, el: 20, range_rate: -6000 } });
    assert.ok(m._expectedFreqMHz() > 435.25, 'approaching satellite is heard higher');
    m.destroy();
});

test('dial retune does not move the expected frequency off the transmitter', () => {
    const sm = makeStateManager();
    sm._state.set(43017, { position: { az: 10, el: 20, range_rate: 0 } });
    const m = new ManualLayout(sm);
    tuneTo(435.25, { label: 'AO-91', modulation: 'BPSK', noradId: 43017 });
    m._freqDial.emit(435260000);
    assert.strictEqual(m._expectedFreqMHz(), 435.25);
    m.destroy();
});

test('no expected frequency without a tuned transmitter or range rate', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.strictEqual(m._expectedFreqMHz(), null, 'nothing tuned yet');
    tuneTo(435.25, { label: 'AO-91', modulation: 'BPSK', noradId: 43017 });
    assert.strictEqual(m._expectedFreqMHz(), null, 'no position for the satellite');
    m.destroy();
});

console.log('\n— ManualLayout: Antenna tab of a tract without a rotator —');

function withModeManager(mm, fn) {
    global.window._modeManager = mm;
    try { fn(); } finally { delete global.window._modeManager; }
}
const OMNI_QFH = { hasRotator: () => false, antennaKind: () => 'qfh' };
const DIR_ROT = { hasRotator: () => true, antennaKind: () => 'yagi' };

test('tract without a rotator and with a known kind shows the omni views', () => {
    withModeManager(OMNI_QFH, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        assert.strictEqual(paneAntenna.getAttribute('data-ant-view'), 'omni');
        assert.ok(m._omniLobe && m._omniGain, 'omni views must be created');
        assert.strictEqual(m._omniLobe.kind, 'qfh');
        assert.strictEqual(m._omniGain.kind, 'qfh');
        m.destroy();
    });
});

test('tract with a rotator keeps the Az/El indicators', () => {
    withModeManager(DIR_ROT, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        assert.strictEqual(paneAntenna.getAttribute('data-ant-view'), 'dir');
        assert.strictEqual(m._omniKind, null);
        m.destroy();
    });
});

test('config arriving after construction switches the view on activate', () => {
    const m = new ManualLayout(makeStateManager());
    assert.strictEqual(m._omniKind, null, 'no station config yet');
    withModeManager(OMNI_QFH, () => {
        m.activate();
        assert.strictEqual(m._omniKind, 'qfh');
        assert.strictEqual(paneAntenna.getAttribute('data-ant-view'), 'omni');
    });
    m.destroy();
});

test('position feeds the omni views with the satellite and its pass', () => {
    withModeManager(OMNI_QFH, () => {
        const sm = makeStateManager();
        sm._trackingId = 25544;
        sm._state.set(25544, { position: { az: 83, el: 74 } });
        const m = new ManualLayout(sm);
        const skyPath = [{ time: 0, az: 10, el: 0 }, { time: 1000, az: 80, el: 74 }, { time: 2000, az: 170, el: 0 }];
        sm.getSatelliteGroup = () => ({ satellites: [
            { norad_id: 1, aos: 0, los: 500, sky_path: skyPath },
            { norad_id: 25544, aos: 0, los: 2000, sky_path: skyPath },
        ] });
        m.activate();
        m._omniLobe.calls = [];
        m._omniGain.calls = [];
        m._updateIndicators();
        assert.deepStrictEqual(m._omniLobe.calls.find((c) => c[0] === 'setSatellite'), ['setSatellite', 83, 74]);
        assert.ok(m._omniLobe.calls.some((c) => c[0] === 'draw'));
        const pass = m._omniGain.calls.find((c) => c[0] === 'setPass')[1];
        assert.strictEqual(pass.losMs, 2000, 'pass of the tracked satellite from the group');
        assert.strictEqual(pass.points.length, 3);
        assert.ok(m._omniGain.calls.some((c) => c[0] === 'draw'));
        m.destroy();
    });
});

test('without a target the omni views are cleared', () => {
    withModeManager(OMNI_QFH, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        m._omniLobe.calls = [];
        m._omniGain.calls = [];
        m._updateIndicators();
        assert.deepStrictEqual(m._omniLobe.calls.find((c) => c[0] === 'setSatellite'), ['setSatellite', null, null]);
        assert.deepStrictEqual(m._omniGain.calls.find((c) => c[0] === 'setPass'), ['setPass', null]);
        m.destroy();
    });
});

/** Hybrid station: QFH without a rotator and a Yagi on a rotator, switchable. */
function hybridModeManager() {
    const paths = { 1: { rot: false, kind: 'qfh' }, 2: { rot: true, kind: 'yagi' } };
    const mm = {
        id: 1,
        listeners: [],
        hasRotator: () => paths[mm.id].rot,
        antennaKind: () => paths[mm.id].kind,
        onRadioPathChange: (cb) => {
            mm.listeners.push(cb);
            return () => { mm.listeners = mm.listeners.filter((x) => x !== cb); };
        },
        setRadioPath: (id) => { mm.id = id; mm.listeners.forEach((cb) => cb({ id: id })); },
    };
    return mm;
}

test('switching the tract in Manual updates the Antenna view and the rotator', () => {
    const mm = hybridModeManager();
    withModeManager(mm, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        assert.strictEqual(paneAntenna.getAttribute('data-ant-view'), 'omni');
        assert.strictEqual(m._rotHasRotator, false);
        mm.setRadioPath(2);
        assert.strictEqual(paneAntenna.getAttribute('data-ant-view'), 'dir', 'rotator tract shows Az/El');
        assert.strictEqual(m._rotHasRotator, true);
        mm.setRadioPath(1);
        assert.strictEqual(paneAntenna.getAttribute('data-ant-view'), 'omni');
        assert.strictEqual(m._rotHasRotator, false);
        m.destroy();
    });
});

test('tract subscription is made once and dropped on destroy', () => {
    const mm = hybridModeManager();
    withModeManager(mm, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        m.deactivate();
        m.activate();
        assert.strictEqual(mm.listeners.length, 1);
        m.destroy();
        assert.strictEqual(mm.listeners.length, 0);
    });
});

console.log('\n— ManualLayout: live spectrum stream —');

test('Spectrum opens one stream for the selected tract and leaving Manual stops it', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        const s = MockStream.last();
        assert.ok(/^\/api\/stream\/v1\/spectrum\?/.test(s.opts.url), s.opts.url);
        assert.ok(s.opts.url.indexOf('radio_path=radio_path_1') >= 0, s.opts.url);
        assert.strictEqual(s.started, 1);
        fire(tabAntenna, 'click');
        assert.strictEqual(MockStream.last(), s, 'Antenna keeps the same stream');
        fire(tabPanorama, 'click');
        assert.strictEqual(s.stopped, 0, 'hidden panorama does not stop the stream');
        m.deactivate();
        assert.strictEqual(s.stopped, 1, 'leaving Manual closes the stream');
        m.destroy();
    });
});

test('without a tract there is no stream', () => {
    withStation({ mm: null }, () => {
        const before = MockStream.instances.length;
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        assert.strictEqual(MockStream.instances.length, before);
        m.destroy();
    });
});

console.log('\n— ManualLayout: spectrum accumulation mode —');

/** Query parameter of the stream URL. */
function urlParam(url, name) {
    const m = new RegExp('[?&]' + name + '=([^&]*)').exec(url);
    return m ? decodeURIComponent(m[1]) : null;
}

/** Runs fn with an in-memory localStorage, then removes it. */
function withStorage(initial, fn) {
    const data = Object.assign({}, initial);
    global.localStorage = {
        getItem: (k) => (Object.prototype.hasOwnProperty.call(data, k) ? data[k] : null),
        setItem: (k, v) => { data[k] = String(v); },
    };
    try { fn(data); } finally { delete global.localStorage; }
}

test('stream asks for the instantaneous spectrum by default', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        const url = MockStream.last().opts.url;
        assert.strictEqual(urlParam(url, 'mode'), 'current', url);
        assert.strictEqual(urlParam(url, 'window_ms'), null, 'no averaging window: ' + url);
        assert.ok(specModeBtns.current.classList.contains('is-on'));
        assert.strictEqual(specModeBtns.current.getAttribute('aria-pressed'), 'true');
        assert.strictEqual(specAvgN.hidden, true, 'N is shown only for accumulation');
        m.destroy();
    });
});

test('every mode asks for the same 4096 bins whatever the canvas width', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        assert.strictEqual(urlParam(MockStream.last().opts.url, 'bins'), '4096');
        m._fftCanvas.clientWidth = 1900;
        fire(specModeBtns.avg, 'click');
        assert.strictEqual(urlParam(MockStream.last().opts.url, 'bins'), '4096');
        fire(specModeBtns.maxhold, 'click');
        assert.strictEqual(urlParam(MockStream.last().opts.url, 'bins'), '4096');
        m.destroy();
    });
});

test('accumulation averages over N cycles: window_ms is N stream frames', () => {
    withStorage({}, (data) => {
        withStation({}, () => {
            const m = new ManualLayout(makeStateManager());
            m.activate();
            fire(tabSpectrum, 'click');
            fire(specModeBtns.avg, 'click');
            const s1 = MockStream.last();
            assert.strictEqual(urlParam(s1.opts.url, 'mode'), 'avg');
            assert.strictEqual(urlParam(s1.opts.url, 'window_ms'), '500', 'default N = 10 frames of 50 ms');
            assert.strictEqual(specAvgN.hidden, false);
            specAvgN.value = '20';
            fire(specAvgN, 'change');
            const s2 = MockStream.last();
            assert.notStrictEqual(s2, s1, 'new N reopens the stream');
            assert.strictEqual(s1.stopped, 1);
            assert.strictEqual(urlParam(s2.opts.url, 'window_ms'), '1000');
            assert.strictEqual(data['ux.manual.specAvgN'], '20');
            fire(specModeBtns.current, 'click');
            assert.strictEqual(urlParam(MockStream.last().opts.url, 'window_ms'), null);
            assert.strictEqual(specAvgN.hidden, true);
            m.destroy();
        });
    });
});

test('saved N is used, an unknown one falls back to 10; N outside accumulation is only remembered', () => {
    withStorage({ 'ux.manual.specMode': 'avg', 'ux.manual.specAvgN': '50' }, (data) => {
        withStation({}, () => {
            const m = new ManualLayout(makeStateManager());
            m.activate();
            fire(tabSpectrum, 'click');
            assert.strictEqual(urlParam(MockStream.last().opts.url, 'window_ms'), '2500');
            assert.strictEqual(specAvgN.value, '50');
            fire(specModeBtns.maxhold, 'click');
            const s = MockStream.last();
            specAvgN.value = '4';
            fire(specAvgN, 'change');
            assert.strictEqual(MockStream.last(), s, 'peak hold does not depend on N');
            assert.strictEqual(data['ux.manual.specAvgN'], '4');
            m.destroy();
        });
    });
    withStorage({ 'ux.manual.specMode': 'avg', 'ux.manual.specAvgN': '7' }, () => {
        withStation({}, () => {
            const m = new ManualLayout(makeStateManager());
            m.activate();
            fire(tabSpectrum, 'click');
            assert.strictEqual(urlParam(MockStream.last().opts.url, 'window_ms'), '500');
            m.destroy();
        });
    });
});

test('mode button reopens the stream with the new mode and remembers it', () => {
    withStorage({}, (data) => {
        withStation({}, () => {
            const m = new ManualLayout(makeStateManager());
            m.activate();
            fire(tabSpectrum, 'click');
            const s1 = MockStream.last();
            fire(specModeBtns.maxhold, 'click');
            const s2 = MockStream.last();
            assert.notStrictEqual(s2, s1, 'a new stream');
            assert.strictEqual(s1.stopped, 1, 'the old stream is closed');
            assert.strictEqual(urlParam(s2.opts.url, 'mode'), 'maxhold');
            assert.strictEqual(s2.started, 1);
            assert.ok(specModeBtns.maxhold.classList.contains('is-on'));
            assert.ok(!specModeBtns.current.classList.contains('is-on'));
            assert.strictEqual(data['ux.manual.specMode'], 'maxhold');
            fire(specModeBtns.maxhold, 'click');
            assert.strictEqual(MockStream.last(), s2, 'the same mode does not reopen the stream');
            m.destroy();
        });
    });
});

test('saved mode is used for the first stream, a removed or unknown one gives the instantaneous', () => {
    withStorage({ 'ux.manual.specMode': 'maxhold' }, () => {
        withStation({}, () => {
            const m = new ManualLayout(makeStateManager());
            m.activate();
            fire(tabSpectrum, 'click');
            assert.strictEqual(urlParam(MockStream.last().opts.url, 'mode'), 'maxhold');
            m.destroy();
        });
    });
    ['minhold', 'bogus'].forEach((saved) => {
        withStorage({ 'ux.manual.specMode': saved }, () => {
            withStation({}, () => {
                const m = new ManualLayout(makeStateManager());
                m.activate();
                fire(tabSpectrum, 'click');
                assert.strictEqual(urlParam(MockStream.last().opts.url, 'mode'), 'current', saved);
                m.destroy();
            });
        });
    });
});

test('reset reopens the stream with the same mode: the server starts the hold anew', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        fire(specModeBtns.maxhold, 'click');
        const s1 = MockStream.last();
        fire(_domNodes.get('manual-spec-reset'), 'click');
        const s2 = MockStream.last();
        assert.notStrictEqual(s2, s1);
        assert.strictEqual(s1.stopped, 1);
        assert.strictEqual(urlParam(s2.opts.url, 'mode'), 'maxhold');
        m.destroy();
    });
});

test('without an open stream the mode is only remembered, reset does nothing', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        m._stopStream();
        const before = MockStream.instances.length;
        fire(specModeBtns.maxhold, 'click');
        fire(_domNodes.get('manual-spec-reset'), 'click');
        assert.strictEqual(MockStream.instances.length, before, 'no stream to reopen');
        m._setView('spectrum');
        assert.strictEqual(urlParam(MockStream.last().opts.url, 'mode'), 'maxhold');
        fire(specModeBtns.current, 'click');
        m.destroy();
    });
});

/** Stream frame of the given display mode (0 current, 1 avg, 2 maxhold). */
function modeFrame(displayMode, values) {
    const f = fftFrame(436500000, values, 2048000);
    f.header.displayMode = displayMode;
    return f;
}

test('peak hold draws the held trace under the live one; waterfall and marker take the live', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        fire(specModeBtns.maxhold, 'click');
        const s = MockStream.last();
        m._fft.calls = [];
        m._wf.calls = [];
        const held = [-100, -20, -50];
        const live = [-100, -50, -10];
        s.opts.onFrame(modeFrame(0x02, held));
        assert.strictEqual(m._fft.calls.filter((c) => c[0] === 'draw').length, 0, 'the hold waits for its live frame');
        s.opts.onFrame(modeFrame(0x00, live));
        const draw = m._fft.calls.find((c) => c[0] === 'draw');
        const range = { min: m._fft._dbMin, max: m._fft._dbMax };
        const unit = (v) => Array.from(realStream.dbfsToUnit(new Float32Array(v), range.min, range.max));
        assert.deepStrictEqual(Array.from(draw[1]), unit(live), 'main trace is live');
        assert.deepStrictEqual(Array.from(draw[2]), unit(held), 'second trace is the hold');
        const line = m._wf.calls.find((c) => c[0] === 'pushLine');
        assert.deepStrictEqual(Array.from(line[1]), unit(live), 'waterfall shows the live spectrum');
        assert.strictEqual(m._wf.calls.filter((c) => c[0] === 'pushLine').length, 1);
        assert.strictEqual(markerText()[1], '−10.0', 'marker reads the live peak');
        m.destroy();
    });
});

test('Antenna switcher drives the same mode: both groups light up, one stream reopen', () => {
    withStorage({}, (data) => {
        withStation({}, () => {
            const m = new ManualLayout(makeStateManager());
            m.activate();
            fire(tabSpectrum, 'click');
            fire(tabAntenna, 'click');
            const s1 = MockStream.last();
            const before = MockStream.instances.length;
            fire(antSpecModeBtns.maxhold, 'click');
            const s2 = MockStream.last();
            assert.notStrictEqual(s2, s1);
            assert.strictEqual(s1.stopped, 1);
            assert.strictEqual(MockStream.instances.length, before + 1, 'exactly one new stream');
            assert.strictEqual(urlParam(s2.opts.url, 'mode'), 'maxhold');
            assert.ok(antSpecModeBtns.maxhold.classList.contains('is-on'));
            assert.ok(specModeBtns.maxhold.classList.contains('is-on'), 'Spectrum tab shows the same mode');
            assert.strictEqual(specModeBtns.maxhold.getAttribute('aria-pressed'), 'true');
            assert.strictEqual(data['ux.manual.specMode'], 'maxhold');
            fire(_domNodes.get('manual-ant-spec-reset'), 'click');
            assert.notStrictEqual(MockStream.last(), s2, 'reset from Antenna reopens the stream');
            m.destroy();
        });
    });
});

test('N chosen on Antenna is shown on Spectrum too', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        fire(antSpecModeBtns.avg, 'click');
        assert.strictEqual(antSpecAvgN.hidden, false);
        assert.strictEqual(specAvgN.hidden, false);
        antSpecAvgN.value = '50';
        fire(antSpecAvgN, 'change');
        assert.strictEqual(urlParam(MockStream.last().opts.url, 'window_ms'), '2500');
        assert.strictEqual(specAvgN.value, '50');
        fire(specModeBtns.current, 'click');
        assert.strictEqual(antSpecAvgN.hidden, true);
        m.destroy();
    });
});

test('peak hold also draws the held trace on the Antenna spectrum', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        fire(tabAntenna, 'click');
        fire(antSpecModeBtns.maxhold, 'click');
        const s = MockStream.last();
        m._antFft.calls = [];
        s.opts.onFrame(modeFrame(0x02, [-100, -20, -50]));
        s.opts.onFrame(modeFrame(0x00, [-100, -50, -10]));
        const draw = m._antFft.calls.find((c) => c[0] === 'draw');
        assert.ok(draw && draw[2], 'Antenna gets the hold as the second trace');
        const unit = Array.from(realStream.dbfsToUnit(new Float32Array([-100, -20, -50]), m._antFft._dbMin, m._antFft._dbMax));
        assert.deepStrictEqual(Array.from(draw[2]), unit);
        fire(specModeBtns.current, 'click');
        m.destroy();
    });
});

test('outside peak hold the frame is drawn alone', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        fire(specModeBtns.avg, 'click');
        m._fft.calls = [];
        MockStream.last().opts.onFrame(modeFrame(0x01, [-100, -50, -10]));
        const draw = m._fft.calls.find((c) => c[0] === 'draw');
        assert.ok(draw, 'drawn at once');
        assert.strictEqual(draw[2], undefined, 'no held trace');
        m.destroy();
    });
});

/** 1024 bins of noise at -90 dBFS with one peak. */
function peakFrame(bin, dbfs, timestampMs) {
    const values = new Array(1024).fill(-90);
    values[bin] = dbfs;
    const f = fftFrame(436500000, values, 2048000);
    f.header.timestampMs = timestampMs;
    return f;
}

test('stream frame shows the peak marker: frequency and amplitude with bar', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        MockStream.last().opts.onFrame(peakFrame(768, -49.3, 1000));
        assert.deepStrictEqual(markerText(), ['437.012', '−49.3'],
            'units live in the markup, values use the typographic minus');
        assert.strictEqual(_domNodes.get('manual-rx-marker-bar').style.width, '58.9%');
        m.destroy();
    });
});

test('marker text refreshes at most four times a second', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        const s = MockStream.last();
        s.opts.onFrame(peakFrame(768, -49.3, 1000));
        s.opts.onFrame(peakFrame(768, -60, 1100));
        assert.strictEqual(markerText()[1], '−49.3', 'a frame 100 ms later must not redraw the text');
        s.opts.onFrame(peakFrame(768, -60, 1250));
        assert.strictEqual(markerText()[1], '−60.0');
        m.destroy();
    });
});

test('closing the stream resets the marker to dashes', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        MockStream.last().opts.onFrame(peakFrame(768, -49.3, 1000));
        m.deactivate();
        assert.deepStrictEqual(markerText(), ['—', '—']);
        m.destroy();
    });
});

test('frame is drawn on the FFT and the waterfall in the auto dB scale, band from the header', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        m._fft.calls = [];
        m._wf.calls = [];
        MockStream.last().opts.onFrame(fftFrame(436500000, [-100, -50, 0], 2048000));
        // median -50 gives the bottom -60, peak 0 gives the top +5
        assert.strictEqual(m._fft._dbMin, -60);
        assert.strictEqual(m._fft._dbMax, 5);
        assert.strictEqual(m._antFft._dbMin, -60, 'Antenna spectrum shares the scale');
        const want = Array.from(realStream.dbfsToUnit(new Float32Array([-100, -50, 0]), -60, 5));
        const draw = m._fft.calls.find((c) => c[0] === 'draw');
        assert.deepStrictEqual(Array.from(draw[1]), want);
        const line = m._wf.calls.find((c) => c[0] === 'pushLine');
        assert.deepStrictEqual(Array.from(line[1]), want, 'waterfall uses the same scale');
        assert.strictEqual(m._fft.freqCenterMHz, 436.5);
        assert.strictEqual(m._fft.freqSpanMHz, 2.048);
        assert.strictEqual(m._wf._freqCenterMHz, 436.5);
        assert.strictEqual(m._wf._freqSpanMHz, 2.048);
        assert.strictEqual(m._freqDial.getHz(), 436500000, 'dial shows where the receiver really is');
        m.destroy();
    });
});

test('stream frames coalesce: many onFrame before rAF yield one draw and one waterfall line', () => {
    const queued = [];
    const prevRaf = global.requestAnimationFrame;
    const prevCancel = global.cancelAnimationFrame;
    global.requestAnimationFrame = (cb) => { queued.push(cb); return queued.length; };
    global.cancelAnimationFrame = (id) => { queued[id - 1] = null; };
    let m = null;
    try {
        withStation({}, () => {
            m = new ManualLayout(makeStateManager());
            m.activate();
            fire(tabSpectrum, 'click');
            // Слить rAF вкладки/ресайза, иначе они мешают счёту coalesce
            while (queued.length) {
                const cb = queued.shift();
                if (cb) { cb(); }
            }
            const s = MockStream.last();
            m._fft.calls = [];
            m._wf.calls = [];
            s.opts.onFrame(fftFrame(436500000, [-100, -90, -80]));
            s.opts.onFrame(fftFrame(436500000, [-100, -50, -20]));
            s.opts.onFrame(fftFrame(436500000, [-100, -40, -10]));
            assert.strictEqual(m._fft.calls.filter((c) => c[0] === 'draw').length, 0, 'no draw until rAF');
            assert.strictEqual(m._wf.calls.filter((c) => c[0] === 'pushLine').length, 0);
            assert.strictEqual(queued.filter(Boolean).length, 1, 'one rAF scheduled');
            queued[0]();
            assert.strictEqual(m._fft.calls.filter((c) => c[0] === 'draw').length, 1);
            assert.strictEqual(m._wf.calls.filter((c) => c[0] === 'pushLine').length, 1);
            // Первый кадр после coalesce: DisplaySmooth копирует последний
            const want = Array.from(realStream.dbfsToUnit(new Float32Array([-100, -40, -10]), m._fft._dbMin, m._fft._dbMax));
            assert.deepStrictEqual(Array.from(m._fft.calls[0][1]), want);
        });
    } finally {
        if (m) { m.destroy(); }
        global.requestAnimationFrame = prevRaf;
        global.cancelAnimationFrame = prevCancel;
    }
});

test('display smooth blends the second frame on the FFT line', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        const s = MockStream.last();
        s.opts.onFrame(fftFrame(436500000, [-100, -20]));
        m._fft.calls = [];
        s.opts.onFrame(fftFrame(436500000, [-80, -40]));
        const draw = m._fft.calls.find((c) => c[0] === 'draw');
        // alpha 0.35: -100+0.35*20=-93, -20+0.35*(-20)=-27
        const blended = new Float32Array([-93, -27]);
        const want = Array.from(realStream.dbfsToUnit(blended, m._fft._dbMin, m._fft._dbMax));
        assert.deepStrictEqual(Array.from(draw[1]).map((v) => Math.round(v * 1e6) / 1e6),
            want.map((v) => Math.round(v * 1e6) / 1e6));
        m.destroy();
    });
});

test('Antenna draws the stream on its compact spectrum', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        fire(tabAntenna, 'click');
        m._antFft.calls = [];
        MockStream.last().opts.onFrame(fftFrame(436500000));
        assert.ok(m._antFft.calls.some((c) => c[0] === 'draw'));
        assert.strictEqual(m._antFft.freqCenterMHz, 436.5);
        m.destroy();
    });
});

test('first frame redraws the frequency scale and does not clear the waterfall', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        m._wf.calls = [];
        MockStream.last().opts.onFrame(fftFrame(436200000, [-80, -40], 57600));
        assert.strictEqual(m._wf._freqCenterMHz, 436.2);
        assert.strictEqual(m._wf._freqSpanMHz, 0.0576);
        assert.ok(m._wf.calls.some((c) => c[0] === 'refresh'), 'scale was drawn for the placeholder band');
        assert.ok(!m._wf.calls.some((c) => c[0] === 'clear'));
        m.destroy();
    });
});

test('retune seen in the stream clears the waterfall, steady frames do not', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        const s = MockStream.last();
        s.opts.onFrame(fftFrame(436500000));
        m._wf.calls = [];
        s.opts.onFrame(fftFrame(436500000));
        assert.ok(!m._wf.calls.some((c) => c[0] === 'clear'));
        s.opts.onFrame(fftFrame(437800000));
        assert.ok(m._wf.calls.some((c) => c[0] === 'clear'), 'old lines belong to another frequency');
        assert.strictEqual(m._freqDial.getHz(), 437800000, 'another operator retuned: dial follows');
        m.destroy();
    });
});

test('heartbeat updates the band without drawing', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        m._fft.calls = [];
        MockStream.last().opts.onFrame({ header: { dataType: 0xFE, centerHz: 145800000, spanHz: 1024000 }, values: null });
        assert.ok(!m._fft.calls.some((c) => c[0] === 'draw'));
        assert.strictEqual(m._fft.freqCenterMHz, 145.8);
        m.destroy();
    });
});

test('dial is not overwritten while the operator is typing', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        m._freqDial.setHz(437000000);
        m._freqDial.editing = true;
        MockStream.last().opts.onFrame(fftFrame(436500000));
        assert.strictEqual(m._freqDial.getHz(), 437000000);
        m.destroy();
    });
});

test('switching the tract restarts the stream on the new tract', () => {
    const mm = Object.assign(hybridModeManager(), {});
    mm.getRadioPathId = () => mm.id;
    withStation({ mm: mm }, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        const s1 = MockStream.last();
        mm.setRadioPath(2);
        const s2 = MockStream.last();
        assert.notStrictEqual(s1, s2);
        assert.strictEqual(s1.stopped, 1);
        assert.ok(s2.opts.url.indexOf('radio_path=radio_path_2') >= 0, s2.opts.url);
        assert.strictEqual(s2.started, 1);
        m.destroy();
    });
});

test('destroy stops the stream', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        const s = MockStream.last();
        m.destroy();
        assert.strictEqual(s.stopped, 1);
    });
});

console.log('\n— ManualLayout: live demodulator —');

/** Runs fn with document listeners captured: fn(emit) dispatches satellite-scout-demod. */
function withDemodEvents(fn) {
    const listeners = [];
    const prevAdd = global.document.addEventListener;
    const prevRemove = global.document.removeEventListener;
    global.document.addEventListener = (type, cb) => { listeners.push([type, cb]); };
    global.document.removeEventListener = (type, cb) => {
        const i = listeners.findIndex((l) => l[0] === type && l[1] === cb);
        if (i >= 0) { listeners.splice(i, 1); }
    };
    const emit = (event, data) => {
        for (const l of listeners.slice()) {
            if (l[0] === 'satellite-scout-demod') { l[1]({ detail: { event: event, data: data } }); }
        }
    };
    try {
        fn(emit, listeners);
    } finally {
        global.document.addEventListener = prevAdd;
        global.document.removeEventListener = prevRemove;
    }
}

test('demod events reach the table and the panel for the selected tract only', () => {
    withStation({}, () => withDemodEvents((emit) => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        const tm = { radio_path: 'radio_path_1', src: 'SCOUT' };
        emit('telemetry', tm);
        emit('telemetry', { radio_path: 'radio_path_2', src: 'OTHER' });
        const st = { radio_path: 'radio_path_1', active: true };
        emit('demod_status', st);
        emit('demod_status', { radio_path: 'radio_path_2', active: true });

        assert.deepStrictEqual(m._tmiTable.calls.filter((c) => c[0] === 'addTelemetry'), [['addTelemetry', tm]]);
        assert.deepStrictEqual(m._demodPanel.calls.filter((c) => c[0] === 'applyStatus'),
            [['applyStatus', st, m._levelOffset]]);
        m.destroy();
    }));
});

test('channel changed on the server follows into the fields; repeats keep the operator edit', () => {
    withStation({}, () => withDemodEvents((emit) => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fillRx({ gain: 28, agc: false, offset: 0, bwIndex: 2, squelch: -90, demodIndex: 0 });
        const st = { radio_path: 'radio_path_1', active: true, demod: 'fsk9600_g3ruh',
            offset_hz: -2500, bandwidth_hz: 6000, squelch_dbfs: -80 };
        emit('demod_status', st);
        const bw = () => filterBwSel.options[filterBwSel.selectedIndex].value;
        assert.strictEqual(_domNodes.get('manual-rx-doppler-val').value, '-2500');
        assert.strictEqual(bw(), '6000');
        assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-80');
        assert.strictEqual(pipelineSel.options[pipelineSel.selectedIndex].value, 'fsk9600_g3ruh');

        _domNodes.get('manual-rx-doppler-val').value = '1500';
        emit('demod_status', st);
        assert.strictEqual(_domNodes.get('manual-rx-doppler-val').value, '1500', 'same settings keep the edit');

        emit('demod_status', Object.assign({}, st, { bandwidth_hz: 18000 }));
        assert.strictEqual(bw(), '18000');
        assert.strictEqual(_domNodes.get('manual-rx-doppler-val').value, '-2500');

        emit('demod_status', { radio_path: 'radio_path_2', active: true, demod: 'afsk1200',
            offset_hz: 9000, bandwidth_hz: 15000, squelch_dbfs: -90 });
        assert.strictEqual(_domNodes.get('manual-rx-doppler-val').value, '-2500', 'other tract is ignored');
        m.destroy();
    }));
});

test('gain changed on the server follows into the field and the dBm units; repeats keep the operator edit', () => {
    const calibrated = Object.assign({}, TRACT_1, { getRadioPath: () => ({ id: 1, calibration_db: -40 }) });
    withStation({ mm: calibrated }, () => withDemodEvents((emit) => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fillRx({ gain: 28, agc: false, offset: 0, bwIndex: 2, squelch: -90, demodIndex: 0 });
        const st = { radio_path: 'radio_path_1', active: true, demod: 'afsk1200',
            offset_hz: 0, bandwidth_hz: 15000, squelch_dbfs: -90, gain_db: 0 };
        emit('demod_status', st);
        assert.strictEqual(_domNodes.get('manual-rx-gain').value, '0');
        assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-130', '-90 dBFS - 40 dB calibration');

        emit('demod_status', Object.assign({}, st, { gain_db: 20 }));
        assert.strictEqual(_domNodes.get('manual-rx-gain').value, '20');
        assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-150', 'squelch in dBm moves with the gain');
        assert.strictEqual(_domNodes.get('manual-rx-squelch-unit').textContent, 'dBm');

        _domNodes.get('manual-rx-gain').value = '33';
        emit('demod_status', Object.assign({}, st, { gain_db: 20 }));
        assert.strictEqual(_domNodes.get('manual-rx-gain').value, '33', 'same gain keeps the edit');

        emit('demod_status', Object.assign({}, st, { gain_db: -1 }));
        assert.strictEqual(_domNodes.get('manual-rx-agc').getAttribute('aria-pressed'), 'true');
        assert.strictEqual(_domNodes.get('manual-rx-squelch-unit').textContent, 'dBFS', 'АРУ: gain unknown');
        assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-90');
        m.destroy();
    }));
});

test('live mode follows whether the station has a radio path', () => {
    const lastLive = (w) => w.calls.filter((c) => c[0] === 'setLive').map((c) => c[1]).pop();
    withStation({ mm: null }, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        assert.strictEqual(lastLive(m._tmiTable), false);
        assert.strictEqual(lastLive(m._demodPanel), false);
        m.destroy();
    });
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        assert.strictEqual(lastLive(m._tmiTable), true);
        assert.strictEqual(lastLive(m._demodPanel), true);
        m.destroy();
    });
});

test('destroy removes the demod event listener', () => {
    withStation({}, () => withDemodEvents((emit, listeners) => {
        const m = new ManualLayout(makeStateManager());
        assert.strictEqual(listeners.filter((l) => l[0] === 'satellite-scout-demod').length, 1);
        m.destroy();
        assert.strictEqual(listeners.filter((l) => l[0] === 'satellite-scout-demod').length, 0);
    }));
});

console.log('\n— ManualLayout: receiver control rights —');

test('viewer: dial is read-only and a transmitter click does not tune', () => {
    withStation({ access: makeAccess(false) }, (ac) => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        assert.strictEqual(m._freqDial.readOnly, true);
        assert.strictEqual(_domNodes.get('manual-rx-gain-inc').disabled, true, 'stepper is locked with the dial');
        tuneTo(AO91.freqMHz, AO91);
        assert.strictEqual(tuneCalls().length, 0);
        assert.ok(ac.notices.some((t) => /только в режиме «Контроль»\.$/.test(t)), ac.notices.join(' | '));
        assert.strictEqual(m._view, 'spectrum', 'viewer still watches the spectrum');
        m.destroy();
    });
});

test('role change switches the dial between read-only and editable', () => {
    withStation({ access: makeAccess(false) }, (ac) => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        assert.strictEqual(m._freqDial.readOnly, true);
        ac.setRole(true);
        assert.strictEqual(m._freqDial.readOnly, false);
        assert.strictEqual(_domNodes.get('manual-rx-gain-inc').disabled, false);
        ac.setRole(false);
        assert.strictEqual(m._freqDial.readOnly, true);
        m.destroy();
        assert.strictEqual(ac.listeners.length, 0, 'destroy drops the access subscription');
    });
});

test('access subscription is made once across activate cycles', () => {
    withStation({}, (ac) => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        m.deactivate();
        m.activate();
        assert.strictEqual(ac.listeners.length, 1);
        m.destroy();
    });
});

console.log('\n— ManualLayout: receiver settings («Установить») —');

const RX_FIELD_IDS = ['manual-rx-span', 'manual-rx-gain', 'manual-rx-agc', 'manual-rx-doppler-val',
    'manual-rx-filter-bw', 'manual-rx-squelch', 'manual-rx-pipeline', 'manual-rx-apply', 'manual-demod-apply'];

function receiverCalls() {
    return fetchMock.calls.filter((c) => c.url === '/api/sdr/receiver');
}

function channelCalls() {
    return fetchMock.calls.filter((c) => c.url === '/api/sdr/channel');
}

/** Operator's input in the receiver column; squelch in the units shown. */
function fillRx(o) {
    spanSel.selectedIndex = o.spanIndex || 0;
    _domNodes.get('manual-rx-gain').value = String(o.gain);
    _domNodes.get('manual-rx-agc').setAttribute('aria-pressed', o.agc ? 'true' : 'false');
    _domNodes.get('manual-rx-doppler-val').value = String(o.offset);
    filterBwSel.selectedIndex = o.bwIndex;
    _domNodes.get('manual-rx-squelch').value = String(o.squelch);
    pipelineSel.selectedIndex = o.demodIndex;
}

test('receiver «Установить» sends frequency, span and gain', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        m._freqDial.setHz(437125000);
        fillRx({ spanIndex: 1, gain: 28, agc: false, offset: 1500, bwIndex: 3, squelch: -75, demodIndex: 2 });
        fire(_domNodes.get('manual-rx-apply'), 'click');
        assert.strictEqual(receiverCalls().length, 1);
        assert.strictEqual(channelCalls().length, 0);
        assert.strictEqual(tuneCalls().length, 0, 'SET is the only path that tunes by itself');
        assert.deepStrictEqual(receiverCalls()[0].body, {
            radio_path: 'radio_path_1',
            center_hz: 437125000,
            sample_rate_hz: 1024000,
            gain_db: 28,
        });
        m.destroy();
    });
});

test('demodulator «Установить» sends only the channel', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fillRx({ spanIndex: 1, gain: 28, agc: false, offset: 1500, bwIndex: 3, squelch: -75, demodIndex: 2 });
        fire(_domNodes.get('manual-demod-apply'), 'click');
        assert.strictEqual(channelCalls().length, 1);
        assert.strictEqual(receiverCalls().length, 0);
        assert.deepStrictEqual(channelCalls()[0].body, {
            radio_path: 'radio_path_1',
            offset_hz: 1500,
            bandwidth_hz: 18000,
            squelch_dbfs: -75,
            demod: 'afsk1200',
            demod_off: false,
        });
        m.destroy();
    });
});

test('АРУ pill sends gain -1', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fillRx({ gain: 28, agc: false, offset: 0, bwIndex: 2, squelch: -90, demodIndex: 0 });
        fire(_domNodes.get('manual-rx-agc'), 'click');
        assert.strictEqual(_domNodes.get('manual-rx-agc').getAttribute('aria-pressed'), 'true');
        fire(_domNodes.get('manual-rx-apply'), 'click');
        assert.strictEqual(receiverCalls()[0].body.gain_db, -1);
        m.destroy();
    });
});

test('span follows a change on the server; the same span keeps the operator choice', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        const s = MockStream.last();
        spanSel.selectedIndex = 3;
        s.opts.onFrame(fftFrame(436500000, null, 1024000));
        assert.strictEqual(spanValue(), '1024000', 'first frame shows the device span');
        m._wf.calls = [];
        spanSel.selectedIndex = 3;
        s.opts.onFrame(fftFrame(436500000, null, 1024000));
        assert.strictEqual(spanValue(), '256000', 'operator choice survives until «Установить»');
        assert.ok(!m._wf.calls.some((c) => c[0] === 'clear'));
        s.opts.onFrame(fftFrame(436500000, null, 512000));
        assert.strictEqual(spanValue(), '512000', 'another operator changed the span');
        assert.ok(m._wf.calls.some((c) => c[0] === 'clear'), 'old lines belong to another span');
        m.destroy();
    });
    resetSpanOptions();
});

console.log('\n— ManualLayout: demodulator channel on the spectrum —');

test('stream frame gives the channel overlay the receiver band', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        const ov = MockChannelOverlay.last;
        assert.ok(ov, 'overlay is created over the spectrum');
        fillRx({ gain: 28, offset: -12000, bwIndex: 3, squelch: -90, demodIndex: 0 });
        fire(_domNodes.get('manual-rx-doppler-val'), 'input');
        MockStream.last().opts.onFrame(fftFrame(436200000, null, 1024000));
        assert.deepStrictEqual(ov.lastCall('setBand'), ['setBand', 436200000, 1024000]);
        assert.deepStrictEqual(ov.lastCall('setChannel'), ['setChannel', -12000, 18000]);
        m.destroy();
    });
    resetSpanOptions();
});

test('demod checkbox stops the chain, hides the lines and follows the server', () => {
    const box = makeDomElement('manual-rx-channel-show', { checked: true });
    _domNodes.set('manual-rx-channel-show', box);
    try {
        withStation({}, () => withDemodEvents((emit) => {
            const m = new ManualLayout(makeStateManager());
            m.activate();
            const ov = MockChannelOverlay.last;
            box.checked = false;
            fire(box, 'change');
            const posts = fetchMock.calls.filter((c) => c.url === '/api/sdr/demod');
            assert.strictEqual(posts.length, 1);
            assert.deepStrictEqual(posts[0].body, { radio_path: 'radio_path_1', demod_off: true });
            assert.deepStrictEqual(ov.lastCall('setVisible'), ['setVisible', false]);

            emit('demod_status', {
                radio_path: 'radio_path_1', active: true, demod: 'afsk1200',
                offset_hz: 0, bandwidth_hz: 15000, squelch_dbfs: -90, demod_off: false,
            });
            assert.strictEqual(box.checked, true, 'server switched the demodulator back on');
            assert.deepStrictEqual(ov.lastCall('setVisible'), ['setVisible', true]);
            m.destroy();
        }));
        withStation({ access: makeAccess(false) }, () => {
            const m = new ManualLayout(makeStateManager());
            m.activate();
            box.checked = false;
            fire(box, 'change');
            assert.strictEqual(box.checked, true, 'a viewer cannot stop the demodulator');
            assert.strictEqual(fetchMock.calls.filter((c) => c.url === '/api/sdr/demod').length, 0);
            m.destroy();
        });
    } finally {
        _domNodes.delete('manual-rx-channel-show');
    }
});

test('channel bandwidth select moves the overlay before «Установить»', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        const ov = MockChannelOverlay.last;
        fillRx({ gain: 28, offset: 0, bwIndex: 3, squelch: -90, demodIndex: 0 });
        fire(filterBwSel, 'change');
        assert.deepStrictEqual(ov.lastCall('setChannel'), ['setChannel', 0, 18000]);
        assert.strictEqual(receiverCalls().length, 0, 'only a preview, nothing sent');
        m.destroy();
    });
});

test('gesture on the channel fills the fields and sends the settings at once', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        fillRx({ gain: 28, offset: 0, bwIndex: 2, squelch: -75, demodIndex: 1 });
        const ov = MockChannelOverlay.last;
        assert.deepStrictEqual(ov.opts.bandwidthOptions(), [2800, 6000, 15000, 18000]);
        ov.opts.onChange(2500, 18000);
        assert.strictEqual(_domNodes.get('manual-rx-doppler-val').value, '2500');
        assert.strictEqual(filterBwSel.options[filterBwSel.selectedIndex].value, '18000');
        assert.strictEqual(channelCalls().length, 1);
        assert.strictEqual(receiverCalls().length, 0, 'a channel gesture does not retune the receiver');
        const body = channelCalls()[0].body;
        assert.strictEqual(body.offset_hz, 2500);
        assert.strictEqual(body.bandwidth_hz, 18000);
        assert.strictEqual(body.demod, 'fsk1200', 'the rest of the demodulator column goes as is');
        assert.strictEqual(body.gain_db, undefined);
        m.destroy();
    });
});

test('demod status labels the channel with profile and good frames', () => {
    withStation({}, () => withDemodEvents((emit) => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        const ov = MockChannelOverlay.last;
        emit('demod_status', {
            radio_path: 'radio_path_1', demod: 'afsk1200', squelch_open: true, frames_good: 7,
        });
        assert.deepStrictEqual(ov.lastCall('setStatus'), ['setStatus', 'AFSK 1200 · ✔ 7', 'open']);
        emit('demod_status', {
            radio_path: 'radio_path_1', demod: 'afsk1200', squelch_open: false, frames_good: 7,
        });
        assert.deepStrictEqual(ov.lastCall('setStatus'), ['setStatus', 'AFSK 1200 · ✔ 7', 'idle']);
        m.destroy();
    }));
});

test('viewer: receiver fields are disabled and «Установить» sends nothing', () => {
    withStation({ access: makeAccess(false) }, (ac) => {
        const m = new ManualLayout(makeStateManager());
        m.activate();
        for (const id of RX_FIELD_IDS) {
            assert.strictEqual(_domNodes.get(id).disabled, true, id + ' disabled for viewer');
        }
        fire(_domNodes.get('manual-rx-apply'), 'click');
        fire(_domNodes.get('manual-demod-apply'), 'click');
        assert.strictEqual(receiverCalls().length, 0);
        assert.strictEqual(channelCalls().length, 0);
        assert.ok(ac.notices.some((t) => /приёмника можно только в режиме «Контроль»/.test(t)), ac.notices.join(' | '));
        assert.ok(ac.notices.some((t) => /демодулятора можно только в режиме «Контроль»/.test(t)), ac.notices.join(' | '));
        ac.setRole(true);
        for (const id of RX_FIELD_IDS) {
            assert.strictEqual(_domNodes.get(id).disabled, false, id + ' enabled with control');
        }
        m.destroy();
    });
});

// ── Async: server answers to a tune request ───────────────────

const asyncTests = [];
function testAsync(name, fn) { asyncTests.push([name, fn]); }
const flush = () => new Promise((r) => setImmediate(r));

testAsync('tune refused with 403 reverts the dial to the receiver and re-reads the role', async () => {
    const ac = makeAccess(true);
    const m = withStation({ access: ac }, () => {
        const layout = new ManualLayout(makeStateManager());
        layout.activate();
        fire(tabSpectrum, 'click');
        MockStream.last().opts.onFrame(fftFrame(436500000));
        fetchMock.status = 403;
        layout._freqDial.emit(437000000);
        return layout;
    });
    global.window._accessControl = ac;
    await flush();
    await flush();
    assert.strictEqual(m._freqDial.getHz(), 436500000);
    assert.strictEqual(ac.refreshed, 1);
    assert.ok(ac.notices.some((t) => /Режим «Контроль» отключён/.test(t)), ac.notices.join(' | '));
    delete global.window._accessControl;
    m.destroy();
});

testAsync('stream frames do not move the dial back while a tune is in flight', async () => {
    const ac = makeAccess(true);
    let m;
    withStation({ access: ac }, () => {
        m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
        MockStream.last().opts.onFrame(fftFrame(436500000));
        m._freqDial.emit(437000000);
        MockStream.last().opts.onFrame(fftFrame(436500000));
        assert.strictEqual(m._freqDial.getHz(), 437000000, 'old-center frame must not undo the SET');
    });
    await flush();
    await flush();
    MockStream.last().opts.onFrame(fftFrame(437000000));
    assert.strictEqual(m._freqDial.getHz(), 437000000);
    m.destroy();
});

// ── Amplitude units: dBm with calibration, dBFS otherwise ──────

/** Tract 1 whose receiver has a dBm calibration. */
const CALIBRATED = Object.assign({}, TRACT_1, { getRadioPath: () => ({ id: 1, calibration_db: -40 }) });

function levelUnits() {
    return [_domNodes.get('manual-rx-marker-unit').textContent, _domNodes.get('manual-rx-squelch-unit').textContent];
}
function statusCalls() {
    return fetchMock.calls.filter((c) => c.url.indexOf('/api/sdr/status') === 0);
}

/** Opens the Spectrum tab on the given tract and waits for the receiver status. */
async function openWithStatus(mm, status) {
    const prevBody = fetchMock.body;
    fetchMock.body = status;
    let m;
    withStation({ mm: mm }, () => {
        m = new ManualLayout(makeStateManager());
        m.activate();
        fire(tabSpectrum, 'click');
    });
    await flush();
    await flush();
    fetchMock.body = prevBody;
    return m;
}

testAsync('calibrated tract shows dBm at the antenna: calibration minus receiver gain', async () => {
    _domNodes.get('manual-rx-squelch').value = '-90';
    const m = await openWithStatus(CALIBRATED, { center_hz: 436500000, gain_db: 10, running: true });
    assert.deepStrictEqual(statusCalls().map((c) => c.url), ['/api/sdr/status?radio_path=radio_path_1']);
    const frame = peakFrame(768, -49.3, 1000);
    const want = new realStream.AutoRange().update(realStream.shiftLevels(frame.values, -50));
    MockStream.last().opts.onFrame(frame);
    assert.deepStrictEqual(markerText(), ['437.012', '−99.3'], 'peak in dBm: -49.3 - 40 - 10');
    assert.deepStrictEqual(levelUnits(), ['dBm', 'dBm']);
    assert.strictEqual(_domNodes.get('manual-rx-marker-bar').style.width, '58.9%', 'bar keeps the ADC headroom');
    assert.deepStrictEqual([m._fft._dbMin, m._fft._dbMax], [want.min, want.max], 'spectrum scale in dBm');
    assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-140', 'squelch keeps its meaning in new units');
    m._stopStream();
    global.window._modeManager = TRACT_1;
    m._startStream();
    delete global.window._modeManager;
    assert.deepStrictEqual(levelUnits(), ['dBFS', 'dBFS'], 'uncalibrated tract goes back to dBFS');
    assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-90');
    m.destroy();
});

testAsync('AGC on the receiver keeps dBFS: gain is unknown', async () => {
    const m = await openWithStatus(CALIBRATED, { center_hz: 436500000, gain_db: -1, running: true });
    MockStream.last().opts.onFrame(peakFrame(768, -49.3, 1000));
    assert.deepStrictEqual(markerText(), ['437.012', '−49.3']);
    assert.deepStrictEqual(levelUnits(), ['dBFS', 'dBFS']);
    m.destroy();
});

testAsync('tract without calibration stays in dBFS with a known gain', async () => {
    const m = await openWithStatus(TRACT_1, { gain_db: 10 });
    assert.strictEqual(statusCalls().length, 1, 'status still fills the receiver fields');
    MockStream.last().opts.onFrame(peakFrame(768, -49.3, 1000));
    assert.deepStrictEqual(markerText(), ['437.012', '−49.3']);
    assert.deepStrictEqual(levelUnits(), ['dBFS', 'dBFS']);
    m.destroy();
});

// ── Receiver settings from the server status ───────────────────

const CHANNEL_FSK = { offset_hz: -2500, bandwidth_hz: 6000, squelch_dbfs: -80, demod: 'fsk9600_g3ruh' };

testAsync('receiver status fills gain, АРУ, channel and demodulator', async () => {
    fillRx({ gain: 32, agc: true, offset: 0, bwIndex: 2, squelch: -90, demodIndex: 2 });
    const m = await openWithStatus(TRACT_1, { center_hz: 436500000, gain_db: 20, channel: CHANNEL_FSK });
    assert.strictEqual(_domNodes.get('manual-rx-gain').value, '20');
    assert.strictEqual(_domNodes.get('manual-rx-agc').getAttribute('aria-pressed'), 'false');
    assert.strictEqual(_domNodes.get('manual-rx-doppler-val').value, '-2500');
    assert.strictEqual(filterBwSel.options[filterBwSel.selectedIndex].value, '6000');
    assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-80');
    assert.strictEqual(pipelineSel.options[pipelineSel.selectedIndex].value, 'fsk9600_g3ruh');
    const mods = m._eyeView.calls.filter((c) => c[0] === 'setModulation');
    assert.deepStrictEqual(mods[mods.length - 1], ['setModulation', 'FSK 9600'], 'eye follows the demodulator');
    m.destroy();
});

testAsync('receiver status builds span choices and selects the device rate', async () => {
    const m = await openWithStatus(TRACT_1, {
        gain_db: 20, sample_rate: 1200000, sample_rates: [2400000, 1200000, 300000], channel: CHANNEL_FSK,
    });
    assert.deepStrictEqual(spanSel.options.map((o) => [o.value, o.text]),
        [['2400000', '2.4 МГц'], ['1200000', '1.2 МГц'], ['300000', '300 кГц']]);
    assert.strictEqual(spanValue(), '1200000');
    m.destroy();
    resetSpanOptions();
});

testAsync('receiver status with negative gain presses АРУ', async () => {
    fillRx({ gain: 32, agc: false, offset: 0, bwIndex: 2, squelch: -90, demodIndex: 0 });
    const m = await openWithStatus(TRACT_1, { gain_db: -1, channel: CHANNEL_FSK });
    assert.strictEqual(_domNodes.get('manual-rx-agc').getAttribute('aria-pressed'), 'true');
    assert.strictEqual(_domNodes.get('manual-rx-gain').value, '32', 'manual gain is kept for when АРУ is off');
    m.destroy();
});

testAsync('demod apply converts shown dBm by the gain already applied', async () => {
    const channel = Object.assign({}, CHANNEL_FSK, { squelch_dbfs: -90 });
    const m = await openWithStatus(CALIBRATED, { center_hz: 436500000, gain_db: 0, channel: channel });
    assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-130', '-90 dBFS - 40 dB calibration');
    fillRx({ gain: 32, agc: false, offset: -2500, bwIndex: 1, squelch: -100, demodIndex: 0 });
    global.window._modeManager = CALIBRATED;
    global.window._accessControl = makeAccess(true);
    fire(_domNodes.get('manual-demod-apply'), 'click');
    assert.strictEqual(channelCalls().length, 1);
    assert.strictEqual(receiverCalls().length, 0);
    assert.strictEqual(channelCalls()[0].body.squelch_dbfs, -60, '-100 dBm at the applied gain 0: -100 - (-40)');
    assert.strictEqual(channelCalls()[0].body.gain_db, undefined, 'unsent gain is not part of the demodulator');
    delete global.window._modeManager;
    delete global.window._accessControl;
    m.destroy();
});

testAsync('receiver apply shifts the shown dBm and does not send the channel', async () => {
    const channel = Object.assign({}, CHANNEL_FSK, { squelch_dbfs: -90 });
    const m = await openWithStatus(CALIBRATED, { center_hz: 436500000, gain_db: 0, channel: channel });
    fillRx({ gain: 32, agc: false, offset: -2500, bwIndex: 1, squelch: -130, demodIndex: 0 });
    const prevBody = fetchMock.body;
    fetchMock.body = { ok: true, status: { center_hz: 436500000, gain_db: 32, sample_rate: 2048000 } };
    global.window._modeManager = CALIBRATED;
    global.window._accessControl = makeAccess(true);
    fire(_domNodes.get('manual-rx-apply'), 'click');
    assert.deepStrictEqual(receiverCalls()[0].body, {
        radio_path: 'radio_path_1', center_hz: 436000000, sample_rate_hz: 2048000, gain_db: 32,
    });
    await flush();
    await flush();
    fetchMock.body = prevBody;
    assert.strictEqual(_domNodes.get('manual-rx-squelch').value, '-162', 'same -90 dBFS, shown at gain 32');
    assert.strictEqual(_domNodes.get('manual-rx-doppler-val').value, '-2500', 'unsent channel offset stays');
    MockStream.last().opts.onFrame(peakFrame(768, -49.3, 1000));
    assert.deepStrictEqual(markerText(), ['437.012', '−121.3'], 'dBm by the applied gain: -49.3 - 40 - 32');
    delete global.window._modeManager;
    delete global.window._accessControl;
    m.destroy();
});

testAsync('refused settings: server error is shown, 403 re-reads the role', async () => {
    const ac = makeAccess(true);
    const m = withStation({ access: ac }, () => {
        const layout = new ManualLayout(makeStateManager());
        layout.activate();
        fillRx({ gain: 28, agc: false, offset: 0, bwIndex: 2, squelch: -90, demodIndex: 0 });
        fetchMock.status = 400;
        fetchMock.body = { error: 'invalid channel settings: unknown demod' };
        fire(_domNodes.get('manual-demod-apply'), 'click');
        return layout;
    });
    global.window._accessControl = ac;
    global.window._modeManager = TRACT_1;
    await flush();
    await flush();
    assert.ok(ac.notices.some((t) => /^Настройки демодулятора не применены: invalid channel settings/.test(t)), ac.notices.join(' | '));
    fetchMock.status = 403;
    fire(_domNodes.get('manual-demod-apply'), 'click');
    await flush();
    await flush();
    assert.strictEqual(ac.refreshed, 1);
    assert.ok(ac.notices.some((t) => /Режим «Контроль» отключён/.test(t)), ac.notices.join(' | '));
    fetchMock.status = 200;
    fetchMock.body = { ok: true };
    delete global.window._accessControl;
    delete global.window._modeManager;
    m.destroy();
});

console.log('\n— ManualLayout: numeric steppers —');

const GAIN_SPEC = { kind: 'gain', step: 0.1, min: 0, max: 49.6, decimals: 1 };
const OFFSET_SPEC = { kind: 'offset', step: 1, decimals: 0 };
const SQUELCH_SPEC = { kind: 'squelch', step: 1, decimals: 1 };

test('filterNumericText keeps digits and one sign or dot', () => {
    assert.strictEqual(ManualLayout.filterNumericText('32abc', 'gain'), '32');
    assert.strictEqual(ManualLayout.filterNumericText('-12.5', 'gain'), '12.5');
    assert.strictEqual(ManualLayout.filterNumericText('1.2.3', 'gain'), '1.23');
    assert.strictEqual(ManualLayout.filterNumericText('-1500.2', 'offset'), '-1500');
    assert.strictEqual(ManualLayout.filterNumericText('--90x.5', 'squelch'), '-90.5');
    assert.strictEqual(ManualLayout.filterNumericText('90-', 'squelch'), '90');
});

test('stepNumericText steps and clamps gain, offset and squelch', () => {
    assert.strictEqual(ManualLayout.stepNumericText('32', 0.1, GAIN_SPEC), '32.1');
    assert.strictEqual(ManualLayout.stepNumericText('32.1', -0.1, GAIN_SPEC), '32');
    assert.strictEqual(ManualLayout.stepNumericText('49.6', 0.1, GAIN_SPEC), '49.6');
    assert.strictEqual(ManualLayout.stepNumericText('0', -0.1, GAIN_SPEC), '0');
    assert.strictEqual(ManualLayout.stepNumericText('', -1, OFFSET_SPEC), '-1');
    assert.strictEqual(ManualLayout.stepNumericText('-90', 1, SQUELCH_SPEC), '-89');
    assert.strictEqual(ManualLayout.stepNumericText('-', 1, SQUELCH_SPEC), '1');
});

test('stepper buttons change the field and letters are stripped', () => {
    withStation({}, () => {
        const m = new ManualLayout(makeStateManager());
        const gain = _domNodes.get('manual-rx-gain');
        gain.disabled = false;
        gain.value = '32';
        fire(_domNodes.get('manual-rx-gain-inc'), 'click');
        assert.strictEqual(gain.value, '32.1');
        gain.value = '0';
        fire(_domNodes.get('manual-rx-gain-dec'), 'click');
        assert.strictEqual(gain.value, '0');
        gain.value = '12a-3.4.5';
        fire(gain, 'input');
        assert.strictEqual(gain.value, '123.45');
        const offset = _domNodes.get('manual-rx-doppler-val');
        offset.value = '-12.9x';
        fire(offset, 'input');
        assert.strictEqual(offset.value, '-12');
        m.destroy();
    });
});

// ── Summary ───────────────────────────────────────────────────

(async () => {
    for (const [name, fn] of asyncTests) {
        try {
            await fn();
            passed++;
            console.log(`  ✓ ${name}`);
        } catch (err) {
            failed++;
            console.error(`  ✗ ${name}`);
            console.error(`    ${err.stack || err.message}`);
        }
    }
    console.log(`\n${passed} passed, ${failed} failed`);
    if (failed > 0) {
        process.exit(1);
    }
})();
