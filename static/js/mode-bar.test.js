/**
 * Unit tests for the tract selector in the header and the receiver column.
 *
 * Run: node static/js/mode-bar.test.js
 */

'use strict';

const assert = require('assert');

function makeSelect() {
    const sel = {
        disabled: false,
        options: [],
        _value: '',
        _listeners: {},
        appendChild(opt) { this.options.push(opt); },
        addEventListener(type, fn) {
            if (!this._listeners[type]) { this._listeners[type] = []; }
            this._listeners[type].push(fn);
        },
        removeEventListener(type, fn) {
            const arr = this._listeners[type] || [];
            const idx = arr.indexOf(fn);
            if (idx >= 0) { arr.splice(idx, 1); }
        },
    };
    Object.defineProperty(sel, 'innerHTML', {
        set() { sel.options = []; },
        get() { return ''; },
    });
    Object.defineProperty(sel, 'value', {
        set(v) { sel._value = String(v); },
        get() { return sel._value; },
    });
    return sel;
}

function makeButton(mode) {
    return {
        dataset: { mode: mode },
        disabled: false,
        _attrs: {},
        classList: { toggle() {} },
        setAttribute(key, val) { this._attrs[key] = val; },
        addEventListener() {},
        removeEventListener() {},
    };
}

function makeManager(mode, pathId) {
    const listeners = { mode: [], path: [] };
    const paths = [
        { id: 1, name: 'VHF', has_rotator: false },
        { id: 4, name: 'RTL-SDR V3', has_rotator: true },
    ];
    const state = { mode: mode, pathId: pathId };
    return {
        isBasic() { return false; },
        availableModes() { return ['overview', 'manual']; },
        getMode() { return state.mode; },
        setMode(next) {
            if (state.mode === next) { return false; }
            state.mode = next;
            listeners.mode.forEach((fn) => fn(next));
            return true;
        },
        getRadioPaths() { return paths.slice(); },
        getRadioPathId() { return state.pathId; },
        getRadioPath() { return paths.find((p) => p.id === state.pathId) || null; },
        setRadioPath(id) {
            const n = Number(id);
            const rp = paths.find((p) => p.id === n);
            if (!rp || state.pathId === n) { return false; }
            state.pathId = n;
            listeners.path.forEach((fn) => fn(rp));
            return true;
        },
        onModeChange(fn) {
            listeners.mode.push(fn);
            return () => { listeners.mode = listeners.mode.filter((x) => x !== fn); };
        },
        onRadioPathChange(fn) {
            listeners.path.push(fn);
            return () => { listeners.path = listeners.path.filter((x) => x !== fn); };
        },
    };
}

const headerSelect = makeSelect();
const panelSelect = makeSelect();
const panoramaSelect = makeSelect();
const pathSection = { hidden: false };
const header = {
    querySelector(sel) {
        if (sel === '#app-header-path') { return headerSelect; }
        if (sel === '.app-header__path') { return pathSection; }
        return null;
    },
    querySelectorAll() { return [makeButton('overview'), makeButton('manual')]; },
};

global.document = {
    getElementById(id) {
        if (id === 'app-header') { return header; }
        if (id === 'manual-rx-path') { return panelSelect; }
        if (id === 'manual-sweep-path') { return panoramaSelect; }
        return null;
    },
    createElement() { return { value: '', textContent: '' }; },
};

const { attachModeBar } = require('./mode-bar.js');

function labels(sel) {
    return sel.options.map((opt) => opt.textContent);
}

let failed = 0;
function test(name, fn) {
    try {
        fn();
        console.log('  ✓ ' + name);
    } catch (err) {
        failed++;
        console.error('  ✗ ' + name);
        console.error('    ' + (err.stack || err.message));
    }
}

test('manual hides the header tract and both selects list the same paths', () => {
    const bar = attachModeBar(makeManager('manual', 4));
    assert.strictEqual(pathSection.hidden, true);
    assert.deepStrictEqual(labels(headerSelect), ['1. VHF', '4. RTL-SDR V3 (R)']);
    assert.deepStrictEqual(labels(panelSelect), labels(headerSelect));
    assert.deepStrictEqual(labels(panoramaSelect), labels(headerSelect));
    assert.strictEqual(headerSelect.value, '4');
    assert.strictEqual(panelSelect.value, '4');
    assert.strictEqual(panoramaSelect.value, '4');
    bar.destroy();
});

test('overview keeps the header tract hidden', () => {
    const bar = attachModeBar(makeManager('overview', 1));
    assert.strictEqual(pathSection.hidden, true);
    assert.strictEqual(headerSelect.value, '1');
    bar.destroy();
});

test('panel select change updates the header select', () => {
    const manager = makeManager('manual', 1);
    const bar = attachModeBar(manager);
    panelSelect.value = '4';
    panelSelect._listeners.change[0]({ currentTarget: panelSelect });
    assert.strictEqual(manager.getRadioPathId(), 4);
    assert.strictEqual(headerSelect.value, '4');
    assert.strictEqual(panelSelect.value, '4');
    assert.strictEqual(panoramaSelect.value, '4');
    bar.destroy();
});

test('panorama select change updates the other selects', () => {
    const manager = makeManager('manual', 1);
    const bar = attachModeBar(manager);
    panoramaSelect.value = '4';
    panoramaSelect._listeners.change[0]({ currentTarget: panoramaSelect });
    assert.strictEqual(manager.getRadioPathId(), 4);
    assert.strictEqual(headerSelect.value, '4');
    assert.strictEqual(panelSelect.value, '4');
    bar.destroy();
});

test('header tract stays hidden in both modes', () => {
    const manager = makeManager('overview', 1);
    const bar = attachModeBar(manager);
    assert.strictEqual(pathSection.hidden, true);
    manager.setMode('manual');
    assert.strictEqual(pathSection.hidden, true);
    manager.setMode('overview');
    assert.strictEqual(pathSection.hidden, true);
    bar.destroy();
});

if (failed > 0) {
    process.exit(1);
}
console.log('\nmode-bar tests passed');
