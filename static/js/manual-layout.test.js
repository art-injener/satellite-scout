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
    getElementById: (id) => _domNodes.get(id) || null,
    querySelector: () => null,
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
    Widget.prototype.draw = function() { this.calls.push(['draw']); };
    Widget.prototype.refreshThemeColors = function() { this.calls.push(['refreshThemeColors']); };
    Widget.prototype._resize = function() { this.calls.push(['_resize']); };
    Widget.prototype.refresh = function() { this.calls.push(['refresh']); };
    Widget.prototype.start = function() { this.calls.push(['start']); };
    Widget.prototype.clear = function() { this.calls.push(['clear']); };
    Widget.prototype.pushLine = function() { this.calls.push(['pushLine']); };
    return Widget;
}

global.window.AzimuthIndicator = makeMockWidget();
global.window.ElevationIndicator = makeMockWidget();
global.window.FFTSpectrumView = makeMockWidget();
global.window.WaterfallView = makeMockWidget();
function MockDataSource(opts) {
    this.freqCenterMHz = opts.freqCenterMHz;
    this.freqSpanMHz = opts.freqSpanMHz;
    this.calls = [];
    this._buf = new Float32Array(opts.bins || 512);
}
MockDataSource.prototype.generateLine = function() { this.calls.push(['generateLine']); };
MockDataSource.prototype.getLine = function() { return this._buf; };
MockDataSource.prototype.reset = function() { this.calls.push(['reset']); };
global.window.SpectrumDataSource = MockDataSource;

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
/** Имитирует перестройку оператором: индикатор сообщает новую частоту наружу. */
MockFreqDial.prototype.emit = function(hz) {
    this.hz = Math.round(hz);
    if (this._onChange) { this._onChange(this.hz); }
};
global.window.FreqDial = MockFreqDial;
_domNodes.set('manual-rx-rf-bar', makeDomElement('manual-rx-rf-bar'));
_domNodes.set('manual-rx-rf-val', makeDomElement('manual-rx-rf-val'));
_domNodes.set('manual-rx-audio-bar', makeDomElement('manual-rx-audio-bar'));
_domNodes.set('manual-rx-audio-val', makeDomElement('manual-rx-audio-val'));

// Вкладка «Демодулятор»: переключатель Таблица/Hex + кнопки записи и прослушивания
const resultTabTable = makeDomElement('result-tab-table', { _attrs: { 'data-view': 'table', 'aria-selected': 'true' } });
const resultTabHex = makeDomElement('result-tab-hex', { _attrs: { 'data-view': 'hex', 'aria-selected': 'false' } });
const resultTabChart = makeDomElement('result-tab-chart', { _attrs: { 'data-view': 'chart', 'aria-selected': 'false' } });
const resultViewTable = makeDomElement('result-view-table', { _attrs: { 'data-view': 'table' } });
const resultViewHex = makeDomElement('result-view-hex', { _attrs: { 'data-view': 'hex' } });
const resultViewChart = makeDomElement('result-view-chart', { _attrs: { 'data-view': 'chart' } });
resultViewTable.classList._set.add('is-active');

const resultCol = makeDomElement('result-col', {
    querySelectorAll: (sel) => (sel.indexOf('ml-demod__result-view') >= 0
        ? [resultViewTable, resultViewHex, resultViewChart]
        : []),
});
_domNodes.set('manual-result-tabs', makeDomElement('manual-result-tabs', {
    closest: () => resultCol,
    querySelectorAll: () => [resultTabTable, resultTabHex, resultTabChart],
}));
_domNodes.set('manual-demod-hex', makeDomElement('manual-demod-hex'));
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
    assert.ok(m._dataSource, 'SpectrumDataSource must be created');
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

test('activate starts in panorama (SWEEP); spectrum timer runs only in spectrum view (HOLD)', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.strictEqual(m.isActive(), false);
    m.activate();
    assert.strictEqual(m.isActive(), true);
    // Панорама: спектр/водопад выключены — таймера нет
    assert.strictEqual(m._spectrumTimer, null, 'panorama view must not run spectrum timer');
    // Настройка на канал → вид «Спектр»: таймер запускается
    m._setView('spectrum');
    assert.ok(m._spectrumTimer, 'spectrum view must run the timer');
    // Возврат в «Панораму» → таймер очищается
    m._setView('panorama');
    assert.strictEqual(m._spectrumTimer, null, 'back to panorama clears the timer');
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

test('activate calls panorama.activate', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    const activateCalls = m._panorama.calls.filter((c) => c[0] === 'activate');
    assert.strictEqual(activateCalls.length, 1);
    m.destroy();
});

test('deactivate calls panorama.deactivate', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m.activate();
    m.deactivate();
    const deactivateCalls = m._panorama.calls.filter((c) => c[0] === 'deactivate');
    assert.strictEqual(deactivateCalls.length, 1);
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

test('panorama:tune updates demod, spectrum center and the freq dial', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const layoutEl = _domNodes.get('layout-manual');
    const tx = { freqMHz: 435.25, label: 'AO-91 BPSK', modulation: 'BPSK31', active: true };

    layoutEl.dispatchEvent({ type: 'panorama:tune', detail: { freqMHz: 435.25, tx: tx } });

    const setCalls = m._demodPanel.calls.filter((c) => c[0] === 'setChannel');
    assert.strictEqual(setCalls.length, 1);
    assert.strictEqual(setCalls[0][1].label, 'AO-91 BPSK');
    assert.strictEqual(m._dataSource.freqCenterMHz, 435.25);
    assert.strictEqual(m._fft.freqCenterMHz, 435.25);
    assert.strictEqual(m._freqDial.getHz(), 435250000);
    const resetCalls = m._dataSource.calls.filter((c) => c[0] === 'reset');
    assert.strictEqual(resetCalls.length, 1);
    m.destroy();
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

test('dial retune updates the stored frequency and the spectrum center', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m._freqDial.emit(437801000);
    assert.ok(Math.abs(m._rxFreqMHz - 437.801) < 1e-9, `got ${m._rxFreqMHz}`);
    assert.ok(Math.abs(m._dataSource.freqCenterMHz - 437.801) < 1e-9);
    assert.ok(Math.abs(m._fft.freqCenterMHz - 437.801) < 1e-9);
    assert.ok(Math.abs(m._wf._freqCenterMHz - 437.801) < 1e-9);
    m.destroy();
});

test('destroy tears the dial down', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const dial = m._freqDial;
    m.destroy();
    assert.ok(dial.calls.some((c) => c[0] === 'destroy'), 'dial.destroy must be called');
    assert.strictEqual(m._freqDial, null);
});

test('level meters render bar width and formatted value', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m._updateRxMeters(-60, -30);
    assert.strictEqual(_domNodes.get('manual-rx-rf-bar').style.width, '50.0%');
    assert.strictEqual(_domNodes.get('manual-rx-rf-val').textContent, '-60.0 dBFS');
    assert.strictEqual(_domNodes.get('manual-rx-audio-bar').style.width, '75.0%');
    assert.strictEqual(_domNodes.get('manual-rx-audio-val').textContent, '-30.0 dB');
    m.destroy();
});

test('level meters clamp out-of-range values to 0..100%', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m._updateRxMeters(-200, 20);
    assert.strictEqual(_domNodes.get('manual-rx-rf-bar').style.width, '0.0%');
    assert.strictEqual(_domNodes.get('manual-rx-audio-bar').style.width, '100.0%');
    m.destroy();
});

console.log('\n— ManualLayout: demod result view —');

test('hex dump is rendered on init with offset, hex and ASCII columns', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const lines = _domNodes.get('manual-demod-hex').textContent.split('\n');
    assert.ok(lines.length >= 2, 'dump must span several lines');
    assert.ok(/^0000 {2}([0-9a-f]{2} ){8}/.test(lines[0]), `bad first line: ${lines[0]}`);
    assert.ok(lines[0].indexOf('|') >= 0, 'ASCII column must be present');
    assert.strictEqual(lines[1].slice(0, 4), '0010');
    m.destroy();
});

test('hex dump renders supplied bytes', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    m._renderHexDump([0x41, 0x42, 0x00]);
    const line = _domNodes.get('manual-demod-hex').textContent;
    assert.ok(line.indexOf('41 42 00') >= 0, `bad hex: ${line}`);
    assert.ok(line.indexOf('|AB.|') >= 0, `bad ascii: ${line}`);
    m.destroy();
});

test('switching to Hex marks its tab and view active', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    fire(resultTabHex, 'click');
    assert.strictEqual(resultTabHex.getAttribute('aria-selected'), 'true');
    assert.strictEqual(resultTabTable.getAttribute('aria-selected'), 'false');
    assert.strictEqual(resultViewHex.classList.contains('is-active'), true);
    assert.strictEqual(resultViewTable.classList.contains('is-active'), false);
    m.destroy();
});

test('switching back to Table restores the table view', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    fire(resultTabHex, 'click');
    fire(resultTabTable, 'click');
    assert.strictEqual(resultTabTable.getAttribute('aria-selected'), 'true');
    assert.strictEqual(resultViewTable.classList.contains('is-active'), true);
    assert.strictEqual(resultViewHex.classList.contains('is-active'), false);
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

test('entering Manual opens the Panorama tab in SWEEP', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    assert.strictEqual(activeTabOf(m), 'panorama');
    assert.strictEqual(m._view, 'panorama');
    assert.strictEqual(m._spectrumTimer, null);
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

test('Panorama tab returns the SDR to SWEEP', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabSpectrum, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(activeTabOf(m), 'panorama');
    assert.strictEqual(m._view, 'panorama');
    assert.strictEqual(m._spectrumTimer, null);
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
    assert.ok(Math.abs(m._dataSource.freqCenterMHz - 435.26) < 1e-9);
    m.destroy();
});

test('Antenna from Panorama tunes to the transmitter selected there and stays on Antenna', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    m._panorama._selected = AO91;
    fire(tabAntenna, 'click');
    assert.strictEqual(activeTabOf(m), 'antenna');
    assert.strictEqual(m._view, 'spectrum', 'one SDR: Antenna shows the channel, so it holds');
    assert.ok(m._spectrumTimer);
    assert.strictEqual(m._freqDial.getHz(), 435250000);
    assert.strictEqual(m._dataSource.freqCenterMHz, 435.25);
    assert.strictEqual(m._chanFreqMHz, 435.25);
    m.destroy();
});

test('Antenna from Panorama without a selection resumes the last channel', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    tuneTo(AO91.freqMHz, AO91);
    m._freqDial.emit(435260000);
    fire(tabPanorama, 'click');
    assert.strictEqual(m._view, 'panorama');
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

test('with nothing chosen Antenna opens the 435-437 MHz telemetry band', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabAntenna, 'click');
    assert.strictEqual(activeTabOf(m), 'antenna');
    assert.strictEqual(m._view, 'spectrum');
    assert.strictEqual(m._dataSource.freqCenterMHz, 436);
    assert.strictEqual(m._dataSource.freqSpanMHz, 2);
    assert.strictEqual(m._freqDial.getHz(), 436000000);
    assert.strictEqual(m._expectedFreqMHz(), null, 'no transmitter, no expected frequency');
    m.destroy();
});

test('tuning to a channel narrows the band to the channel width', () => {
    const m = new ManualLayout(makeStateManager());
    tuneTo(AO91.freqMHz, AO91);
    assert.strictEqual(m._dataSource.freqSpanMHz, 0.192);
    assert.strictEqual(m._fft.freqSpanMHz, 0.192);
    assert.strictEqual(m._antFft.freqSpanMHz, 0.192);
    assert.strictEqual(m._wf._freqSpanMHz, 0.192);
    m.destroy();
});

test('Panorama from Antenna returns the SDR to SWEEP', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabAntenna, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(activeTabOf(m), 'panorama');
    assert.strictEqual(m._view, 'panorama');
    assert.strictEqual(m._spectrumTimer, null);
    m.destroy();
});

test('SWEEP started while on Antenna brings up the Panorama tab', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabAntenna, 'click');
    m._setView('panorama');
    assert.strictEqual(activeTabOf(m), 'panorama', 'Antenna cannot show a sweep');
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

test('re-entering Manual always starts on Panorama', () => {
    const m = new ManualLayout(makeStateManager());
    m.activate();
    fire(tabSpectrum, 'click');
    m.deactivate();
    m.activate();
    assert.strictEqual(activeTabOf(m), 'panorama');
    assert.strictEqual(m._view, 'panorama');
    m.destroy();
});

console.log('\n— ManualLayout: leaving HOLD while recording —');

test('Panorama while recording asks; declining keeps HOLD and the recording', () => {
    const { m, start } = makeRecorder();
    let asked = 0;
    m._confirm = () => { asked++; return false; };
    m.activate();
    fire(tabSpectrum, 'click');
    fire(start, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(asked, 1);
    assert.strictEqual(activeTabOf(m), 'spectrum');
    assert.strictEqual(m._view, 'spectrum');
    assert.ok(m._rec.file, 'recording must go on');
    m.destroy();
});

test('confirming stops the recording and returns to SWEEP', () => {
    const { m, start, stop } = makeRecorder();
    m._confirm = () => true;
    m.activate();
    fire(tabSpectrum, 'click');
    fire(start, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(activeTabOf(m), 'panorama');
    assert.strictEqual(m._rec.file, null, 'tuner leaves the channel, recording stops');
    assert.strictEqual(stop.disabled, true);
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

test('Panorama from Antenna while recording asks too', () => {
    const { m, start } = makeRecorder();
    let asked = 0;
    m._confirm = () => { asked++; return false; };
    m.activate();
    fire(tabSpectrum, 'click');
    fire(start, 'click');
    fire(tabAntenna, 'click');
    fire(tabPanorama, 'click');
    assert.strictEqual(asked, 1);
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

test('compact spectrum shows the channel in HOLD only while Antenna is open', () => {
    const m = new ManualLayout(makeStateManager());
    assert.ok(m._antFft, 'compact spectrum view must be created');
    m.activate();
    fire(tabSpectrum, 'click');
    m._spectrumTick();
    assert.strictEqual(antDraws(m), 0, 'hidden on the Spectrum tab');
    fire(tabAntenna, 'click');
    m._spectrumTick();
    assert.strictEqual(antDraws(m), 1);
    m.destroy();
});

test('compact spectrum follows the receiver frequency', () => {
    const m = new ManualLayout(makeStateManager());
    tuneTo(435.25, { label: 'AO-91', modulation: 'BPSK', noradId: 43017 });
    assert.strictEqual(m._antFft.freqCenterMHz, 435.25);
    m._freqDial.emit(435260000);
    assert.strictEqual(m._antFft.freqCenterMHz, 435.26);
    m.destroy();
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

// ── Summary ───────────────────────────────────────────────────

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) {
    process.exit(1);
}
