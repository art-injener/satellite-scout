/**
 * Unit tests for EyeConstellationView.
 *
 * Run: node static/js/eye-constellation.test.js
 */

'use strict';

const assert = require('assert');

// Кнопка-tab: создаётся динамически через document.createElement('button').
function makeButton() {
    return {
        textContent: '',
        type: '',
        title: '',
        dataset: {},
        attrs: {},
        classList: {
            _set: new Set(),
            add(cls) { this._set.add(cls); },
            remove(cls) { this._set.delete(cls); },
            toggle(cls, on) { if (on) this._set.add(cls); else this._set.delete(cls); },
            contains(cls) { return this._set.has(cls); },
        },
        listeners: {},
        addEventListener(type, fn) { this.listeners[type] = fn; },
        setAttribute(name, value) { this.attrs[name] = value; },
        getAttribute(name) { return this.attrs[name]; },
        click() { if (this.listeners.click) { this.listeners.click(); } },
    };
}

function makeTabsContainer() {
    return {
        _children: [],
        get children() { return this._children.slice(); },
        firstChild: null,
        appendChild(node) {
            this._children.push(node);
            this.firstChild = this._children[0];
            return node;
        },
        removeChild(node) {
            const i = this._children.indexOf(node);
            if (i >= 0) { this._children.splice(i, 1); }
            this.firstChild = this._children[0] || null;
            return node;
        },
        // Очистка через while (firstChild) parent.removeChild(firstChild) — стандартный приём.
    };
}

function makeCanvas() {
    return {
        width: 400,
        height: 300,
        parentElement: { clientWidth: 400, clientHeight: 300 },
        getContext: () => ({
            fillRect() {},
            fillText() {},
            beginPath() {},
            moveTo() {},
            lineTo() {},
            arc() {},
            stroke() {},
            fill() {},
            setLineDash() {},
            measureText() { return { width: 10 }; },
            set fillStyle(_) {},
            set strokeStyle(_) {},
            set lineWidth(_) {},
            set globalAlpha(_) {},
            set font(_) {},
            set textAlign(_) {},
            set textBaseline(_) {},
        }),
    };
}

const _canvas = makeCanvas();
const _tabsContainer = makeTabsContainer();
const _autoEl = {
    style: { opacity: '1' },
    classList: {
        _set: new Set(),
        toggle(cls, on) { if (on) this._set.add(cls); else this._set.delete(cls); },
    },
    setAttribute() {},
    addEventListener(type, fn) { this['_on_' + type] = fn; },
};

global.document = {
    getElementById: (id) => {
        if (id === 'manual-eye-canvas') return _canvas;
        if (id === 'manual-eye-tabs') return _tabsContainer;
        if (id === 'manual-eye-auto') return _autoEl;
        return null;
    },
    createElement: (tag) => {
        if (tag === 'button') { return makeButton(); }
        return null;
    },
};
global.window = {};
global.ResizeObserver = class { observe() {} disconnect() {} };

const { EyeConstellationView } = require('./eye-constellation.js');

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

console.log('\n— EyeConstellationView: базовое поведение —');

test('default mode is eye', () => {
    const v = new EyeConstellationView();
    assert.strictEqual(v.getMode(), 'eye');
    v.destroy();
});

test('setMode switches to constellation', () => {
    const v = new EyeConstellationView();
    v.setModulation('BPSK 1k2');
    v.setMode('eye');
    v.setMode('constellation');
    assert.strictEqual(v.getMode(), 'constellation');
    v.destroy();
});

test('activate/deactivate toggles timer', () => {
    const v = new EyeConstellationView();
    v.activate();
    assert.ok(v.isActive());
    assert.ok(v._timer);
    v.deactivate();
    assert.ok(!v.isActive());
    assert.strictEqual(v._timer, null);
    v.destroy();
});

console.log('\n— EyeConstellationView: категория по модуляции —');

test('FSK 9600 → category nrz, mode eye', () => {
    const v = new EyeConstellationView();
    v.setModulation('FSK 9600');
    assert.strictEqual(v.getCategory(), 'nrz');
    assert.strictEqual(v.getMode(), 'eye');
    v.destroy();
});

test('GMSK 9600 → category nrz, mode eye', () => {
    const v = new EyeConstellationView();
    v.setModulation('GMSK 9600');
    assert.strictEqual(v.getCategory(), 'nrz');
    assert.strictEqual(v.getMode(), 'eye');
    v.destroy();
});

test('AFSK 1200 → category nrz, mode eye', () => {
    const v = new EyeConstellationView();
    v.setModulation('AFSK 1200');
    assert.strictEqual(v.getCategory(), 'nrz');
    assert.strictEqual(v.getMode(), 'eye');
    v.destroy();
});

test('BPSK 1k2 → category psk, mode constellation', () => {
    const v = new EyeConstellationView();
    v.setModulation('BPSK 1k2');
    assert.strictEqual(v.getCategory(), 'psk');
    assert.strictEqual(v.getMode(), 'constellation');
    v.destroy();
});

test('QPSK → category psk, mode constellation', () => {
    const v = new EyeConstellationView();
    v.setModulation('QPSK');
    assert.strictEqual(v.getCategory(), 'psk');
    assert.strictEqual(v.getMode(), 'constellation');
    v.destroy();
});

test('CW → category cw, mode envelope', () => {
    const v = new EyeConstellationView();
    v.setModulation('CW');
    assert.strictEqual(v.getCategory(), 'cw');
    assert.strictEqual(v.getMode(), 'envelope');
    v.destroy();
});

test('FM → category fm, mode audio', () => {
    const v = new EyeConstellationView();
    v.setModulation('FM');
    assert.strictEqual(v.getCategory(), 'fm');
    assert.strictEqual(v.getMode(), 'audio');
    v.destroy();
});

test('OOK → category ook, mode envelope', () => {
    const v = new EyeConstellationView();
    v.setModulation('OOK');
    assert.strictEqual(v.getCategory(), 'ook');
    assert.strictEqual(v.getMode(), 'envelope');
    v.destroy();
});

console.log('\n— EyeConstellationView: доступные tab\'ы по категории —');

test('NRZ → доступны [eye, histogram]', () => {
    const v = new EyeConstellationView();
    v.setModulation('FSK 9600');
    assert.deepStrictEqual(v.getAvailableModes(), ['eye', 'histogram']);
    v.destroy();
});

test('PSK → доступны [constellation, eye]', () => {
    const v = new EyeConstellationView();
    v.setModulation('QPSK');
    assert.deepStrictEqual(v.getAvailableModes(), ['constellation', 'eye']);
    v.destroy();
});

test('CW → доступен только [envelope]', () => {
    const v = new EyeConstellationView();
    v.setModulation('CW');
    assert.deepStrictEqual(v.getAvailableModes(), ['envelope']);
    v.destroy();
});

test('FM → доступен только [audio]', () => {
    const v = new EyeConstellationView();
    v.setModulation('FM');
    assert.deepStrictEqual(v.getAvailableModes(), ['audio']);
    v.destroy();
});

test('OOK → доступны [envelope, histogram]', () => {
    const v = new EyeConstellationView();
    v.setModulation('OOK');
    assert.deepStrictEqual(v.getAvailableModes(), ['envelope', 'histogram']);
    v.destroy();
});

console.log('\n— EyeConstellationView: динамический рендер tab\'ов —');

test('FSK 9600 → в контейнере 2 кнопки tab', () => {
    const v = new EyeConstellationView();
    v.setModulation('FSK 9600');
    assert.strictEqual(_tabsContainer.children.length, 2);
    v.destroy();
});

test('CW → в контейнере 1 кнопка tab', () => {
    const v = new EyeConstellationView();
    v.setModulation('CW');
    assert.strictEqual(_tabsContainer.children.length, 1);
    v.destroy();
});

test('активный tab помечен ml-eye__tab--on', () => {
    const v = new EyeConstellationView();
    v.setModulation('QPSK');
    const buttons = _tabsContainer.children;
    assert.ok(buttons[0].classList.contains('ml-eye__tab--on'));
    assert.ok(!buttons[1].classList.contains('ml-eye__tab--on'));
    v.destroy();
});

test('клик по tab отключает auto и меняет mode', () => {
    const v = new EyeConstellationView();
    v.setModulation('FSK 9600');
    assert.strictEqual(v.isAutoMode(), true);
    const histogramTab = _tabsContainer.children[1];
    histogramTab.click();
    assert.strictEqual(v.getMode(), 'histogram');
    assert.strictEqual(v.isAutoMode(), false);
    v.destroy();
});

test('переключение модуляции пересоздаёт tab\'ы', () => {
    const v = new EyeConstellationView();
    v.setModulation('FSK 9600');
    assert.strictEqual(_tabsContainer.children.length, 2);
    v.setModulation('CW');
    assert.strictEqual(_tabsContainer.children.length, 1);
    v.destroy();
});

console.log('\n— EyeConstellationView: auto / ручной режим —');

test('setModulation в auto-режиме применяется', () => {
    const v = new EyeConstellationView();
    v.setModulation('BPSK 1k2');
    assert.strictEqual(v.getMode(), 'constellation');
    v.destroy();
});

test('ручное переключение mode отключает auto', () => {
    const v = new EyeConstellationView();
    v.setModulation('FSK 9600');
    assert.strictEqual(v.isAutoMode(), true);
    v.setMode('histogram');
    assert.strictEqual(v.isAutoMode(), false);
    v.destroy();
});

test('auto click восстанавливает дефолтный mode для модуляции', () => {
    const v = new EyeConstellationView();
    v.setModulation('FSK 9600');
    v.setMode('histogram');
    assert.strictEqual(v.isAutoMode(), false);
    if (_autoEl._on_click) { _autoEl._on_click(); }
    assert.strictEqual(v.isAutoMode(), true);
    assert.strictEqual(v.getMode(), 'eye');
    v.destroy();
});

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed > 0 ? 1 : 0);
