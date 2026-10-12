/**
 * Unit tests for the transmitter strip of ManualPanorama.
 *
 * Run: ELECTRON_RUN_AS_NODE=1 /usr/share/cursor/cursor static/js/manual-panorama.test.js
 *
 * The strip is a single horizontal row of compact items under the tab bar:
 * sorted by frequency, no per-item buttons, click selects, double click tunes,
 * hover is linked with the panorama marks both ways, wheel and arrows scroll it.
 */

'use strict';

const assert = require('assert');

// ── Minimal DOM fake ─────────────────────────────────────────────────────

class FakeClassList {
    constructor() { this.set = new Set(); }
    add(...c) { c.forEach((x) => this.set.add(x)); }
    remove(...c) { c.forEach((x) => this.set.delete(x)); }
    contains(c) { return this.set.has(c); }
    toggle(c, force) {
        const on = force === undefined ? !this.set.has(c) : Boolean(force);
        if (on) { this.set.add(c); } else { this.set.delete(c); }
        return on;
    }
}

class FakeEl {
    constructor(tag) {
        this.tagName = String(tag || 'div').toUpperCase();
        this.children = [];
        this.attrs = {};
        this.listeners = {};
        this.classList = new FakeClassList();
        this.textContent = '';
        this.scrollLeft = 0;
        this.clientWidth = 400;
        this.intoViewCalls = [];
        this.style = {};
        this._html = '';
    }
    get className() { return [...this.classList.set].join(' '); }
    set className(v) { this.classList.set = new Set(String(v).split(/\s+/).filter(Boolean)); }
    get innerHTML() { return this._html; }
    set innerHTML(v) { this.children = []; this._html = String(v); }
    appendChild(c) { this.children.push(c); c.parentNode = this; return c; }
    setAttribute(k, v) { this.attrs[k] = String(v); }
    getAttribute(k) { return k in this.attrs ? this.attrs[k] : null; }
    addEventListener(t, f) { (this.listeners[t] = this.listeners[t] || []).push(f); }
    removeEventListener(t, f) { this.listeners[t] = (this.listeners[t] || []).filter((x) => x !== f); }
    dispatchEvent(ev) { (this.listeners[ev.type] || []).forEach((f) => f(ev)); return true; }
    fire(type, extra) {
        const ev = Object.assign({
            type,
            defaultPrevented: false,
            preventDefault() { this.defaultPrevented = true; },
            stopPropagation() {},
        }, extra || {});
        (this.listeners[type] || []).forEach((f) => f(ev));
        return ev;
    }
    querySelectorAll(sel) {
        const cls = sel.replace(/^\./, '');
        const out = [];
        const walk = (e) => e.children.forEach((c) => {
            if (c.classList.contains(cls)) { out.push(c); }
            walk(c);
        });
        walk(this);
        return out;
    }
    scrollIntoView(opts) { this.intoViewCalls.push(opts); }
    getContext() { return {}; }
}

function textOf(el) {
    return el.textContent + el.children.map(textOf).join('');
}

global.document = {
    getElementById: () => null,
    createElement: (tag) => new FakeEl(tag),
};
global.CustomEvent = class {
    constructor(type, init) { this.type = type; this.detail = init && init.detail; }
};

const { ManualPanorama } = require('./manual-panorama.js');

const TX = [
    { freqMHz: 437.800, label: 'ISS-FSK', modulation: 'FSK 1k2', active: true, chipColor: '#d070ff', noradId: 25544 },
    { freqMHz: 145.825, label: 'ISS-VOICE', modulation: 'FM', active: true, chipColor: '#00ffc8', noradId: 25544 },
    { freqMHz: 145.920, label: 'RS-44 CW', modulation: 'CW', active: false, chipColor: '#8fa8bc', noradId: 44909 },
];

function setup() {
    const canvas = new FakeEl('canvas');
    const list = new FakeEl('div');
    const prev = new FakeEl('button');
    const next = new FakeEl('button');
    const events = [];
    canvas.addEventListener('panorama:tune', (e) => events.push(['tune', e.detail.freqMHz]));
    canvas.addEventListener('panorama:follow', (e) => events.push(['follow', e.detail.freqMHz]));
    const p = new ManualPanorama({
        canvas,
        txListEl: list,
        prevBtn: prev,
        nextBtn: next,
        transmitters: TX.slice(),
    });
    return { p, canvas, list, prev, next, events };
}

function rowsOf(list) { return list.querySelectorAll('.ml-tx-row'); }
function rowByFreq(list, f) { return rowsOf(list).find((r) => parseFloat(r.getAttribute('data-freq')) === f); }

let passed = 0;
let failed = 0;
function test(name, fn) {
    try { fn(); passed++; console.log('PASS: ' + name); } catch (e) { failed++; console.error('FAIL: ' + name + '\n  ' + e.message); }
}

console.log('\n— ManualPanorama: transmitter strip —');

test('items are sorted by frequency', () => {
    const { list } = setup();
    const freqs = rowsOf(list).map((r) => parseFloat(r.getAttribute('data-freq')));
    assert.deepStrictEqual(freqs, [145.825, 145.920, 437.800]);
});

test('item has no buttons and shows name, frequency and modulation in one line', () => {
    const { list } = setup();
    const row = rowByFreq(list, 437.800);
    assert.strictEqual(row.querySelectorAll('.ml-tx-row__btn').length, 0);
    const all = [];
    const walk = (e) => e.children.forEach((c) => { all.push(c); walk(c); });
    walk(row);
    assert.ok(!all.some((c) => c.tagName === 'BUTTON'), 'no <button> inside an item');
    const text = textOf(row);
    assert.ok(text.includes('ISS-FSK'), text);
    assert.ok(text.includes('437.800'), text);
    assert.ok(text.includes('FSK 1k2'), text);
    assert.strictEqual(row.getAttribute('role'), 'listitem');
});

test('silent transmitter is marked and explained in the tooltip', () => {
    const { list } = setup();
    const row = rowByFreq(list, 145.920);
    assert.ok(row.classList.contains('ml-tx-row--silent'));
    assert.strictEqual(row.getAttribute('title'), 'Молчит');
});

test('click selects the transmitter without tuning', () => {
    const { p, list, events } = setup();
    rowByFreq(list, 145.825).fire('click');
    assert.strictEqual(p._selectedFreq, 145.825);
    assert.ok(rowByFreq(list, 145.825).classList.contains('ml-tx-row--selected'));
    assert.deepStrictEqual(events, []);
});

test('double click tunes: emits follow for a new transmitter', () => {
    const { list, events } = setup();
    rowByFreq(list, 145.825).fire('dblclick');
    assert.deepStrictEqual(events, [['follow', 145.825]]);
});

test('double click on the already tuned transmitter re-tunes instead of dropping it', () => {
    const { p, list, events } = setup();
    p.setFollowedFreq(145.825);
    rowByFreq(list, 145.825).fire('dblclick');
    assert.deepStrictEqual(events, [['tune', 145.825]]);
});

test('Enter on a focused item tunes like double click', () => {
    const { list, events } = setup();
    const row = rowByFreq(list, 437.800);
    assert.strictEqual(row.getAttribute('tabindex'), '0');
    row.fire('keydown', { key: 'Enter' });
    assert.deepStrictEqual(events, [['follow', 437.800]]);
});

test('tuned transmitter is marked in the strip', () => {
    const { p, list } = setup();
    p.setFollowedFreq(437.800);
    assert.ok(rowByFreq(list, 437.800).classList.contains('ml-tx-row--followed'));
    assert.ok(!rowByFreq(list, 145.825).classList.contains('ml-tx-row--followed'));
});

test('hover over an item highlights its panorama mark', () => {
    const { p, list } = setup();
    rowByFreq(list, 145.825).fire('mouseenter');
    assert.strictEqual(p._fftView._highlightFreq, 145.825);
    rowByFreq(list, 145.825).fire('mouseleave');
    assert.strictEqual(p._fftView._highlightFreq, null);
});

test('hover over a panorama mark highlights the strip item', () => {
    const { p, canvas, list } = setup();
    p._fftView.hitTest = () => TX[0];
    canvas.fire('mousemove', { offsetX: 10, offsetY: 10 });
    assert.ok(rowByFreq(list, 437.800).classList.contains('ml-tx-row--active'));
    p._fftView.hitTest = () => null;
    canvas.fire('mousemove', { offsetX: 10, offsetY: 10 });
    assert.ok(!rowByFreq(list, 437.800).classList.contains('ml-tx-row--active'));
});

test('click on a panorama mark scrolls the strip horizontally to the item', () => {
    const { p, canvas, list } = setup();
    p._fftView.hitTest = () => TX[0];
    canvas.fire('click', { offsetX: 10, offsetY: 10 });
    const calls = rowByFreq(list, 437.800).intoViewCalls;
    assert.strictEqual(calls.length, 1);
    assert.strictEqual(calls[0].inline, 'nearest');
});

test('vertical wheel scrolls the strip horizontally', () => {
    const { list } = setup();
    const ev = list.fire('wheel', { deltaY: 120, deltaX: 0 });
    assert.strictEqual(list.scrollLeft, 120);
    assert.ok(ev.defaultPrevented, 'page must not scroll');
});

test('horizontal wheel is left to the browser', () => {
    const { list } = setup();
    const ev = list.fire('wheel', { deltaY: 0, deltaX: 40 });
    assert.strictEqual(list.scrollLeft, 0);
    assert.ok(!ev.defaultPrevented);
});

test('arrow buttons scroll the strip by most of its width', () => {
    const { list, prev, next } = setup();
    next.fire('click');
    assert.strictEqual(list.scrollLeft, 320);
    prev.fire('click');
    assert.strictEqual(list.scrollLeft, 0);
});

test('strip lists every transmitter, silent ones included', () => {
    const { list } = setup();
    assert.strictEqual(rowsOf(list).length, TX.length);
});

// ── Panorama waterfall ───────────────────────────────────────────────────

const { panoramaColumnBins, PanoramaWaterfallView } = require('./manual-panorama.js');

console.log('\n— Panorama waterfall —');

const SPANS = [{ minMHz: 144.0, maxMHz: 148.0 }, { minMHz: 430.0, maxMHz: 440.0 }];

test('column map leaves margins and the gap between spans empty', () => {
    // plot 140 px: 20 px gap, spans 4 and 10 MHz share 120 px (34.3 and 85.7)
    const cols = panoramaColumnBins(180, 32, 8, 20, SPANS, 64);
    assert.strictEqual(cols.length, 180);
    assert.strictEqual(cols[0], -1);
    assert.strictEqual(cols[31], -1);
    assert.strictEqual(cols[179], -1);
    assert.strictEqual(cols[32], 0, 'first plot column is the first bin');
    assert.strictEqual(cols[32 + 40], -1, 'gap between spans');
    assert.ok(cols[171] >= 64 && cols[171] <= 127, 'last plot column belongs to the second span');
});

test('column of a frequency points at the bin of that frequency, like the FFT x axis', () => {
    const w = 1000;
    const ml = 32;
    const mr = 8;
    const gap = 20;
    const bps = 256;
    const cols = panoramaColumnBins(w, ml, mr, gap, SPANS, bps);
    const usable = w - ml - mr - gap;
    const w0 = usable * 4 / 14;
    const w1 = usable * 10 / 14;
    const x437 = Math.floor(ml + w0 + gap + ((437.8 - 430) / 10) * w1);
    const bin437 = bps + Math.round(((437.8 - 430) / 10) * (bps - 1));
    assert.ok(Math.abs(cols[x437] - bin437) <= 1, cols[x437] + ' vs ' + bin437);
    const x146 = Math.floor(ml + ((145.825 - 144) / 4) * w0);
    const bin146 = Math.round(((145.825 - 144) / 4) * (bps - 1));
    assert.ok(Math.abs(cols[x146] - bin146) <= 1, cols[x146] + ' vs ' + bin146);
});

function makeWfCanvas(w, h) {
    const puts = [];
    const ctx = {
        createImageData: (cw, ch) => ({ width: cw, height: ch, data: new Uint8ClampedArray(cw * ch * 4) }),
        putImageData: (img) => puts.push(img),
        fillRect() {}, fillText() {}, beginPath() {}, moveTo() {}, lineTo() {}, stroke() {},
    };
    return {
        width: w, height: h, puts,
        getContext: () => ctx,
        getBoundingClientRect: () => ({ width: w, height: h }),
    };
}

function pixel(img, x, y) {
    const i = (y * img.width + x) * 4;
    return [img.data[i], img.data[i + 1], img.data[i + 2]];
}

test('newest sweep is the top row, older rows move down', () => {
    const canvas = makeWfCanvas(180, 10);
    const wf = new PanoramaWaterfallView(canvas, {
        spans: SPANS, binsPerSpan: 64, colormap: (v) => [Math.round(v * 255), 0, 0], marginRgb: [1, 2, 3],
    });
    wf.pushLine(new Float32Array(128).fill(0.2));
    wf.pushLine(new Float32Array(128).fill(1));
    const img = canvas.puts[canvas.puts.length - 1];
    assert.deepStrictEqual(pixel(img, 40, 0), [255, 0, 0], 'top row = last sweep');
    assert.deepStrictEqual(pixel(img, 40, 1), [51, 0, 0], 'second row = previous sweep');
    assert.deepStrictEqual(pixel(img, 72, 0), [1, 2, 3], 'gap column uses margin colour');
});

test('hidden canvas (zero size) is skipped without losing state', () => {
    const canvas = makeWfCanvas(0, 0);
    const wf = new PanoramaWaterfallView(canvas, { spans: SPANS, binsPerSpan: 64, colormap: () => [0, 0, 0] });
    wf.pushLine(new Float32Array(128));
    assert.strictEqual(canvas.puts.length, 0);
});

test('every panorama tick adds a waterfall row', () => {
    const { p } = setup();
    const rows = [];
    p._wfView = { pushLine: (buf) => rows.push(buf) };
    p._fftView.draw = () => {};
    p._active = true;
    p._tick();
    p._tick();
    assert.strictEqual(rows.length, 2);
    assert.strictEqual(rows[1], p._dataSource.getLine());
});

console.log('\n— ManualPanorama: selected transmitter —');

test('nothing is selected until the operator clicks a transmitter', () => {
    const { p } = setup();
    assert.strictEqual(p.getSelectedTx(), null);
});

test('getSelectedTx returns the transmitter picked by a single click', () => {
    const { p } = setup();
    p._onTxClick(TX[2]);
    assert.strictEqual(p.getSelectedTx(), TX[2]);
});

test('selection that left the transmitter list is dropped', () => {
    const { p } = setup();
    p._onTxClick(TX[2]);
    p.setTransmitters([TX[0]]);
    assert.strictEqual(p.getSelectedTx(), null);
});

console.log('\n' + passed + ' passed, ' + failed + ' failed');
if (failed > 0) { process.exit(1); }
