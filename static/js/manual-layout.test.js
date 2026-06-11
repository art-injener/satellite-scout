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

const _domNodes = new Map();

const knownIds = [
    'manual-azimuth-view',
    'manual-elevation-view',
    'manual-fft',
    'manual-wf',
    'manual-spec-scale',
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
_domNodes.set('manual-rx-freq', { id: 'manual-rx-freq', value: '437,800' });

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
_domNodes.set('manual-rx-pipeline', makeSelect('manual-rx-pipeline', ['FSK 9600', 'FSK 1200', 'AFSK 1200', 'BPSK 1k2'], 0));
const _rxBarRoot = makeLayoutRoot();
_rxBarRoot.id = 'manual-rx-bar';
_domNodes.set('manual-rx-bar', _rxBarRoot);

function MockManualRxBar() {
    this.calls = [];
    this._pipeline = 'FSK 9600';
}
MockManualRxBar.prototype.getPipeline = function() { return this._pipeline; };
MockManualRxBar.prototype.setPipeline = function(p) {
    this.calls.push(['setPipeline', p]);
    var options = ['FSK 9600', 'FSK 1200', 'AFSK 1200', 'BPSK 1k2'];
    var pl = String(p).toLowerCase().replace(/\s+/g, '');
    for (var i = 0; i < options.length; i++) {
        var optKey = options[i].toLowerCase().replace(/\s+/g, '');
        if (pl.indexOf(optKey) >= 0 || optKey.indexOf(pl) >= 0) {
            this._pipeline = options[i];
            return;
        }
        if (pl.indexOf('bpsk') >= 0 && optKey.indexOf('bpsk') >= 0) {
            this._pipeline = options[i];
            return;
        }
        if (pl.indexOf('afsk') >= 0 && optKey.indexOf('afsk') >= 0) {
            this._pipeline = options[i];
            return;
        }
        if (pl.indexOf('fsk') >= 0 && optKey.indexOf('fsk') >= 0) {
            this._pipeline = options[i];
            return;
        }
    }
    this._pipeline = p;
};
MockManualRxBar.prototype.setFreqMHz = function(mhz) {
    var el = _domNodes.get('manual-rx-freq');
    if (el && global.window.formatDemodFreqMHz) {
        el.value = global.window.formatDemodFreqMHz(mhz);
    }
};
MockManualRxBar.prototype.destroy = function() { this.calls.push(['destroy']); };
global.window.ManualRxBar = MockManualRxBar;

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

test('activate switches to active and starts spectrum timer; deactivate reverses', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    assert.strictEqual(m.isActive(), false);
    m.activate();
    assert.strictEqual(m.isActive(), true);
    assert.ok(m._spectrumTimer, 'spectrum timer must be running');
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
    assert.ok(m._spectrumTimer);
    m.destroy();
    assert.strictEqual(m._spectrumTimer, null);
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

test('panorama:tune updates demod, loupe center and freq field', () => {
    const sm = makeStateManager();
    const m = new ManualLayout(sm);
    const layoutEl = _domNodes.get('layout-manual');
    const freqInput = _domNodes.get('manual-rx-freq');
    const tx = { freqMHz: 435.25, label: 'AO-91 BPSK', modulation: 'BPSK31', active: true };

    layoutEl.dispatchEvent({ type: 'panorama:tune', detail: { freqMHz: 435.25, tx: tx } });

    const setCalls = m._demodPanel.calls.filter((c) => c[0] === 'setChannel');
    assert.strictEqual(setCalls.length, 1);
    assert.strictEqual(setCalls[0][1].label, 'AO-91 BPSK');
    assert.strictEqual(m._dataSource.freqCenterMHz, 435.25);
    assert.strictEqual(m._fft.freqCenterMHz, 435.25);
    assert.strictEqual(freqInput.value, '435,250');
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

// ── Summary ───────────────────────────────────────────────────

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) {
    process.exit(1);
}
