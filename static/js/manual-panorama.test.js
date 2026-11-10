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
    const followBtn = new FakeEl('button');
    const events = [];
    canvas.addEventListener('panorama:tune', (e) => events.push(['tune', e.detail.freqMHz]));
    canvas.addEventListener('panorama:follow', (e) => events.push(['follow', e.detail.freqMHz]));
    const p = new ManualPanorama({
        canvas,
        txListEl: list,
        prevBtn: prev,
        nextBtn: next,
        followBtn,
        transmitters: TX.slice(),
    });
    return { p, canvas, list, prev, next, followBtn, events };
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

test('click on a transmitter mark reports it for the card', () => {
    const { p, canvas } = setup();
    const seen = [];
    p._onTransmitter = (tx, x, y) => seen.push([tx.label, x, y]);
    p._fftView.hitTest = () => TX[0];
    canvas.fire('click', { offsetX: 10, offsetY: 10, clientX: 30, clientY: 40 });
    assert.deepStrictEqual(seen, [['ISS-FSK', 30, 40]]);
    assert.strictEqual(p._selectedFreq, 437.800);
});

test('click on a detected signal opens its card and does not select a transmitter', () => {
    const { p, canvas } = setup();
    const seen = [];
    const sig = { id: 's1', center_hz: 437800000 };
    p._onSignal = (got, x, y) => seen.push([got.id, x, y]);
    p._fftView.hitSignal = () => sig;
    p._fftView.hitTest = () => TX[0];
    canvas.fire('click', { offsetX: 10, offsetY: 10, clientX: 8, clientY: 9 });
    assert.deepStrictEqual(seen, [['s1', 8, 9]]);
    assert.strictEqual(p._selectedFreq, null);
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

// ── Axis, view and resampling ────────────────────────────────────────────

const {
    PanoramaAxis, PanoramaWaterfallView, zoomView, panView, easeView, windowColumns, resampleMax,
    stitchWeight, blendTraces,
} = require('./manual-panorama.js');

console.log('\n— Panorama axis and view —');

// 144.9..146.9 and 435..439 MHz with a 0.25 MHz gap: one line 0..6.25
const SPANS = [{ minMHz: 144.9, maxMHz: 146.9 }, { minMHz: 435.0, maxMHz: 439.0 }];

const approx = (a, b, msg) => assert.ok(Math.abs(a - b) < 1e-9, (msg || '') + ' ' + a + ' vs ' + b);

test('axis puts all spans on one line with a gap between them', () => {
    const a = new PanoramaAxis(SPANS, 0.25);
    approx(a.total, 6.25);
    approx(a.toU(145.9), 1);
    approx(a.toU(435), 2.25);
    approx(a.toU(439), 6.25);
    assert.strictEqual(a.toU(300), null, 'between spans');
    assert.strictEqual(a.fromU(2.1), null, 'gap');
    approx(a.fromU(3.25), 436);
});

test('zoom keeps the point under the cursor in place', () => {
    assert.deepStrictEqual(zoomView({ u0: 0, u1: 6.25 }, 6.25, 2, 2.25, 0.05), { u0: 1.125, u1: 4.25 });
});

test('zoom out stops at the whole axis, zoom in at the minimum width, pan stays inside', () => {
    assert.deepStrictEqual(zoomView({ u0: 1, u1: 3 }, 6.25, 0.1, 2, 0.05), { u0: 0, u1: 6.25 });
    const z = zoomView({ u0: 0, u1: 1 }, 6.25, 100, 0.5, 0.05);
    assert.ok(Math.abs(z.u1 - z.u0 - 0.05) < 1e-9, JSON.stringify(z));
    assert.deepStrictEqual(panView({ u0: 5, u1: 6 }, 6.25, 1), { u0: 5.25, u1: 6.25 });
    assert.deepStrictEqual(panView({ u0: 1, u1: 2 }, 6.25, -3), { u0: 0, u1: 1 });
});

test('easing goes from the start view to the target', () => {
    const a = { u0: 0, u1: 2 };
    const b = { u0: 4, u1: 6 };
    assert.deepStrictEqual(easeView(a, b, 0), a);
    assert.deepStrictEqual(easeView(a, b, 1), b);
    const mid = easeView(a, b, 0.5);
    assert.ok(mid.u0 > 2 && mid.u0 < 4, 'ease-out passes the middle early: ' + mid.u0);
});

test('window columns follow the view and are clipped to the plot', () => {
    const a = new PanoramaAxis(SPANS, 0.25);
    const win = { minMHz: 435, maxMHz: 437 };
    const full = windowColumns(a, { u0: 2.25, u1: 4.25 }, win, 32, 200);
    assert.strictEqual(full.x0, 32);
    assert.strictEqual(full.count, 200);
    approx(full.startMHz, 435);
    approx(full.pxMHz, 0.01);
    const half = windowColumns(a, { u0: 3.25, u1: 5.25 }, win, 32, 200);
    assert.strictEqual(half.x0, 32);
    assert.strictEqual(half.count, 100);
    approx(half.startMHz, 436);
    assert.strictEqual(windowColumns(a, { u0: 4.25, u1: 6.25 }, win, 32, 200), null, 'out of view');
});

test('stitch fades across the overlap and keeps a lone window to its edge', () => {
    const left = { centerMHz: 434.862, loMHz: 433.838, hiMHz: 435.886 };
    const right = { centerMHz: 436.5, loMHz: 435.476, hiMHz: 437.524 };
    const both = [left, right];
    const mid = (right.loMHz + left.hiMHz) / 2;
    approx(stitchWeight(left, both, right.loMHz), 1);
    approx(stitchWeight(right, both, right.loMHz), 0);
    approx(stitchWeight(left, both, left.hiMHz), 0);
    approx(stitchWeight(right, both, left.hiMHz), 1);
    approx(stitchWeight(left, both, mid), 0.5);
    approx(stitchWeight(right, both, mid), 0.5);
    approx(stitchWeight(left, [left], left.hiMHz), 1, 'no neighbour: the edge stays');
});

test('blended overlap meets in the middle and a lone window fills its view', () => {
    const loneAxis = new PanoramaAxis([{ minMHz: 433.838, maxMHz: 435.886 }], 0);
    const lone = blendTraces(loneAxis, { u0: 0, u1: loneAxis.total }, [{
        centerHz: 434.862e6, loMHz: 433.838, hiMHz: 435.886, live: true,
        line: new Float32Array(8).fill(-40), max: null,
    }], 0, 100);
    assert.ok(lone.line[0] === lone.line[0] && lone.line[99] === lone.line[99], 'drawn to both edges');
    assert.ok(Math.abs(lone.line[99] + 40) < 0.01, String(lone.line[99]));

    const axis = new PanoramaAxis([{ minMHz: 433.838, maxMHz: 437.524 }], 0);
    const both = blendTraces(axis, { u0: 0, u1: axis.total }, [
        { centerHz: 434.862e6, loMHz: 433.838, hiMHz: 435.886, live: false,
            line: new Float32Array(8).fill(-10), max: new Float32Array(8).fill(-10) },
        { centerHz: 436.5e6, loMHz: 435.476, hiMHz: 437.524, live: true,
            line: new Float32Array(8).fill(-30), max: new Float32Array(8).fill(-30) },
    ], 0, 200);
    const midMHz = (435.476 + 435.886) / 2;
    const midX = Math.round((midMHz - 433.838) / (437.524 - 433.838) * 200);
    assert.ok(Math.abs(both.line[midX] + 20) < 2, 'midpoint is the mean, got ' + both.line[midX]);
    assert.ok(Math.abs(both.line[10] + 10) < 0.5, 'left of the overlap stays with the left window');
    assert.strictEqual(both.live[10], false);
    assert.strictEqual(both.live[190], true);
});

test('resampling keeps the maximum of bins in a column and repeats a bin when zoomed in', () => {
    const v = new Float32Array([-90, -20, -90, -90, -90, -90, -90, -40]);
    assert.deepStrictEqual(Array.from(resampleMax(v, 0, 8, 0, 2, 4)), [-20, -90, -90, -40]);
    assert.deepStrictEqual(Array.from(resampleMax(v, 0, 8, 1, 0.5, 4)), [-20, -20, -90, -90]);
    const edge = Array.from(resampleMax(v, 0, 8, -2, 1, 3));
    assert.ok(Number.isNaN(edge[0]) && Number.isNaN(edge[1]), 'outside the window');
    assert.strictEqual(edge[2], -90);
});

test('time axis step keeps labels apart; ages are short', () => {
    const { timeAxisStepSec, formatAgo } = require('./manual-panorama.js');
    assert.strictEqual(timeAxisStepSec(250, 20), 5, '250 ms rows: 5 s is 20 px');
    assert.strictEqual(timeAxisStepSec(60_000, 20), 1200);
    assert.strictEqual(formatAgo(30), '30с');
    assert.strictEqual(formatAgo(600), '10м');
    assert.strictEqual(formatAgo(7200), '2ч');
});

// ── Panorama waterfall ───────────────────────────────────────────────────

console.log('\n— Panorama waterfall —');

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

function makeWf(w, h) {
    const canvas = makeWfCanvas(w, h);
    const wf = new PanoramaWaterfallView(canvas, {
        colormap: (v) => [Math.round(v * 255), 0, 0], marginRgb: [1, 2, 3], coldRgb: [9, 9, 9],
    });
    wf.setAxis(new PanoramaAxis(SPANS, 0.25));
    return { canvas, wf, last: () => canvas.puts[canvas.puts.length - 1] };
}

const RANGE = { min: -100, max: 0 };

test('newest frame is the top row; columns of other windows stay blank', () => {
    // plot 200 px from x=32, view 435..439 MHz: window 435..437 is x 32..131
    const { wf, last } = makeWf(240, 10);
    const view = { u0: 2.25, u1: 6.25 };
    wf.pushRow({ minMHz: 435, maxMHz: 437, values: new Float32Array(4).fill(-80) });
    wf.render(view, RANGE);
    wf.pushRow({ minMHz: 435, maxMHz: 437, values: new Float32Array(4).fill(0) });
    wf.render(view, RANGE);
    assert.deepStrictEqual(pixel(last(), 42, 0), [255, 0, 0], 'top row = last frame');
    assert.deepStrictEqual(pixel(last(), 42, 1), [51, 0, 0], 'second row = previous frame');
    assert.deepStrictEqual(pixel(last(), 182, 0), [9, 9, 9], 'other window: blank');
    assert.deepStrictEqual(pixel(last(), 42, 5), [9, 9, 9], 'no rows yet');
    assert.deepStrictEqual(pixel(last(), 0, 0), [1, 2, 3], 'margin');
});

test('new view redraws the kept rows at their new place', () => {
    const { wf, last } = makeWf(240, 10);
    wf.pushRow({ minMHz: 435, maxMHz: 437, values: new Float32Array(4).fill(0) });
    wf.render({ u0: 2.25, u1: 6.25 }, RANGE);
    assert.deepStrictEqual(pixel(last(), 182, 0), [9, 9, 9]);
    wf.render({ u0: 2.25, u1: 4.25 }, RANGE);
    assert.deepStrictEqual(pixel(last(), 182, 0), [255, 0, 0], 'window now fills the plot');
});

test('gap between spans uses the margin colour', () => {
    const { wf, last } = makeWf(240, 10);
    wf.render({ u0: 0, u1: 6.25 }, RANGE);
    // u = 2.1 is inside the 146.9..435 gap: x = 32 + 2.1 / 6.25 * 200 = 99.2
    assert.deepStrictEqual(pixel(last(), 99, 0), [1, 2, 3]);
});

test('hidden canvas (zero size) is skipped without losing rows', () => {
    const { canvas, wf } = makeWf(0, 0);
    wf.pushRow({ minMHz: 435, maxMHz: 437, values: new Float32Array(4) });
    wf.render({ u0: 0, u1: 6.25 }, RANGE);
    assert.strictEqual(canvas.puts.length, 0);
    assert.strictEqual(wf.rowCount(), 1);
});

// ── Sweep stitching ──────────────────────────────────────────────────────

const { PanoramaStitcher } = require('./manual-panorama.js');

console.log('\n— Panorama stitcher —');

// Two adjacent UHF windows and one VHF window.
const WINDOWS = [
    { center_hz: 145_900_000, span_hz: 2_000_000 },
    { center_hz: 436_000_000, span_hz: 2_000_000 },
    { center_hz: 438_000_000, span_hz: 2_000_000 },
];

test('adjacent windows form one span, a gap starts a new one', () => {
    const s = new PanoramaStitcher();
    s.setWindows(WINDOWS);
    assert.deepStrictEqual(s.spans.map((sp) => [sp.minMHz, sp.maxMHz]), [[144.9, 146.9], [435, 439]]);
    const layers = s.layers();
    approx(layers[0].showMinMHz, 144.9);
    approx(layers[0].showMaxMHz, 146.9);
    approx(layers[1].showMaxMHz, 437);
    approx(layers[2].showMinMHz, 437);
});

test('marks in the overlap stay with the closer window', () => {
    const s = new PanoramaStitcher();
    s.setWindows([
        { center_hz: 435_700_000, span_hz: 2_000_000 },
        { center_hz: 437_300_000, span_hz: 2_000_000 },
    ]);
    const layers = s.layers();
    approx(layers[0].showMinMHz, 434.7);
    approx(layers[0].showMaxMHz, 436.5);
    approx(layers[1].showMinMHz, 436.5);
    approx(layers[1].showMaxMHz, 438.3);
    s.setSignals(0, [{ id: 'near', center_hz: 436_400_000 }, { id: 'far', center_hz: 436_600_000 }]);
    s.setSignals(1, [{ id: 'other', center_hz: 436_600_000 }]);
    assert.deepStrictEqual(s.marks().map((m) => m.id), ['near', 'other']);
});

test('without frames no window has data', () => {
    const s = new PanoramaStitcher();
    s.setWindows(WINDOWS);
    assert.strictEqual(s.layers().length, 3);
    assert.ok(s.layers().every((l) => l.line === null));
});

test('frame of an unknown window does not show up', () => {
    const s = new PanoramaStitcher();
    s.setWindows([WINDOWS[0]]);
    s.putFrame(100_000_000, 2_000_000, 1000, new Float32Array(4).fill(-10));
    assert.ok(s.layers().every((l) => l.line === null));
});

test('signals are kept per window: leaving a window keeps its marks', () => {
    const s = new PanoramaStitcher();
    s.setWindows(WINDOWS);
    s.setSignals(1, [{ id: 'a', center_hz: 436_500_000, bandwidth_hz: 12_000 }, { id: 'x', center_hz: 438_100_000 }]);
    s.setSignals(2, [{ id: 'b', center_hz: 437_500_000 }]);
    assert.deepStrictEqual(s.marks().map((m) => m.id), ['a', 'b']);
    s.setSignals(1, []);
    assert.deepStrictEqual(s.marks().map((m) => m.id), ['b']);
});

test('age of a window comes from its frame time', () => {
    const s = new PanoramaStitcher();
    s.setWindows(WINDOWS);
    s.putFrame(436_000_000, 2_000_000, 10_000, new Float32Array(4).fill(-50));
    assert.strictEqual(s.ageMs(436_000_000, 70_000), 60_000);
    assert.strictEqual(s.ageMs(438_000_000, 70_000), null);
});

const f32 = (a) => new Float32Array(a);

test('live window: the line is the latest frame, the shadow is the maximum of the dwell', () => {
    const s = new PanoramaStitcher();
    s.setWindows(WINDOWS);
    s.putFrame(436_000_000, 2_000_000, 1000, f32([-50, -60]));
    s.putFrame(436_000_000, 2_000_000, 1250, f32([-70, -40]));
    const l = s.layers()[1];
    assert.strictEqual(l.live, true);
    assert.deepStrictEqual(Array.from(l.line), [-70, -40]);
    assert.deepStrictEqual(Array.from(l.max), [-50, -40]);
    assert.strictEqual(s.layers()[0].line, null, 'no data yet');
});

test('end of dwell keeps the average as the line and the maximum as the shadow', () => {
    const s = new PanoramaStitcher();
    s.setWindows(WINDOWS);
    s.putFrame(436_000_000, 2_000_000, 1000, f32([-50, -60]));
    s.putFrame(436_000_000, 2_000_000, 1250, f32([-70, -40]));
    s.endDwell();
    const l = s.layers()[1];
    assert.strictEqual(l.live, false);
    assert.deepStrictEqual(Array.from(l.line), [-60, -50]);
    assert.deepStrictEqual(Array.from(l.max), [-50, -40]);
    assert.strictEqual(s.ageMs(436_000_000, 2250), 1000);
});

test('frame of another window closes the previous dwell', () => {
    const s = new PanoramaStitcher();
    s.setWindows(WINDOWS);
    s.putFrame(436_000_000, 2_000_000, 1000, f32([-50, -60]));
    s.putFrame(438_000_000, 2_000_000, 1250, f32([-70, -40]));
    assert.strictEqual(s.layers()[1].live, false);
    assert.strictEqual(s.layers()[2].live, true);
});

console.log('\n— ManualPanorama: sweep —');

function sweepState(index, cycle, running) {
    return { radio_path: 'radio_path_1', running: running !== false, dwell_s: 60, windows: WINDOWS, index, cycle };
}

// WINDOWS on the axis: 144.9..146.9 is u 0..2, 435..437 is 2.25..4.25, 437..439 is 4.25..6.25
const near = (v, u0, u1) => Math.abs(v.u0 - u0) < 1e-9 && Math.abs(v.u1 - u1) < 1e-9;

test('while following, the view is the current window; the next window is reached by animation', () => {
    const { p } = setup();
    p.setSweepState(sweepState(1, 1));
    assert.ok(near(p._view, 2.25, 4.25), JSON.stringify(p._view));
    p.setSweepState(sweepState(2, 1));
    assert.ok(p._anim, 'retune starts an animation');
    assert.ok(near(p._anim.to, 4.25, 6.25), JSON.stringify(p._anim.to));
    const v = p._viewAt(p._anim.t0 + 10_000);
    assert.ok(near(v, 4.25, 6.25), JSON.stringify(v));
    assert.strictEqual(p._anim, null);
});

test('stopped sweep shows the whole range', () => {
    const { p } = setup();
    p.setSweepState(sweepState(0, 1, false));
    assert.ok(near(p._view, 0, 6.25), JSON.stringify(p._view));
});

test('zoom or pan by the operator stops following; the follow button brings it back', () => {
    const { p, followBtn } = setup();
    p.setSweepState(sweepState(1, 1));
    p.zoomAtU(3, 0.5);
    assert.strictEqual(p._follow, false);
    assert.strictEqual(followBtn.getAttribute('aria-pressed'), 'false');
    const kept = Object.assign({}, p._view);
    p.setSweepState(sweepState(2, 1));
    assert.deepStrictEqual(p._view, kept, 'view stays where the operator left it');
    followBtn.fire('click');
    assert.strictEqual(p._follow, true);
    assert.strictEqual(followBtn.getAttribute('aria-pressed'), 'true');
    assert.ok(near(p._viewAt(Date.now() + 10_000), 4.25, 6.25));
});

test('waterfall gets a row for every frame of the current window only', () => {
    const { p } = setup();
    const rows = [];
    p._wfView = { pushRow: (r) => rows.push(r), setAxis() {}, render() {} };
    p.setSweepState(sweepState(1, 1));
    p.putFrame({ dataType: 0x01, centerHz: 145_900_000, spanHz: 2_000_000, timestampMs: 1 }, f32([-50]));
    assert.strictEqual(rows.length, 0, 'snapshot of another window');
    p.putFrame({ dataType: 0x01, centerHz: 436_000_000, spanHz: 2_000_000, timestampMs: 2 }, f32([-50]));
    p.putFrame({ dataType: 0x01, centerHz: 436_000_000, spanHz: 2_000_000, timestampMs: 3 }, f32([-40]));
    assert.strictEqual(rows.length, 2);
    assert.strictEqual(rows[1].minMHz, 435, 'row keeps the whole frame');
    assert.strictEqual(rows[1].maxMHz, 437);
    assert.strictEqual(rows[1].showMinMHz, undefined, 'waterfall is not cropped to the overlap');
});

test('next window ends the dwell of the previous one', () => {
    const { p } = setup();
    p.setSweepState(sweepState(1, 1));
    p.putFrame({ dataType: 0x01, centerHz: 436_000_000, spanHz: 2_000_000, timestampMs: 2 }, f32([-50]));
    p.setSweepState(sweepState(2, 1));
    assert.strictEqual(p._stitcher.layers()[1].live, false);
});

test('panorama mark carries the same source code as the spectrum', () => {
    const { BandMarks } = require('./band-marks.js');
    const prev = global.window;
    global.window = { BandMarks: BandMarks };
    try {
        const { p } = setup();
        p.setSweepState(sweepState(1, 1));
        p.setSignals([{
            id: 'a', center_hz: 436_500_000, bandwidth_hz: 12_000, origin: 'satellite',
            structure: { service: 'sstv', modulation: 'fm' },
        }]);
        assert.strictEqual(p._scene(0).marks[0].code, 'ЧМ/SSTV');
    } finally {
        if (prev === undefined) { delete global.window; } else { global.window = prev; }
    }
});

test('band signals go to the current window only while sweeping', () => {
    const { p } = setup();
    p.setSweepState(sweepState(1, 1));
    p.setSignals([{ id: 'a', center_hz: 436_500_000 }]);
    p.setSweepState(sweepState(1, 1, false));
    p.setSignals([]);
    assert.deepStrictEqual(p._stitcher.marks().map((m) => m.id), ['a']);
});

test('panorama frames are stitched, heartbeats ignored', () => {
    const { p } = setup();
    p.setSweepState(sweepState(0, 1));
    p.putFrame({ dataType: 0xFE, centerHz: 0, spanHz: 0, timestampMs: 1 }, null);
    p.putFrame({ dataType: 0x01, centerHz: 145_900_000, spanHz: 2_000_000, timestampMs: 5 },
        new Float32Array(4).fill(-50));
    assert.strictEqual(p._stitcher.ageMs(145_900_000, 5), 0);
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
