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
global.window.SpectrumDataSource = MockDataSource;

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

// ── Summary ───────────────────────────────────────────────────

// ── Summary ───────────────────────────────────────────────────

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) {
    process.exit(1);
}
