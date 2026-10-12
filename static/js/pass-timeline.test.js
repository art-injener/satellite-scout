/**
 * Unit tests for PassTimelineView.
 *
 * Run: node static/js/pass-timeline.test.js
 *
 * Covers the Elevation×time hill chart (overlapping filled curves),
 * not a Gantt row-per-satellite strip.
 */

'use strict';

const assert = require('assert');

function makeCtx() {
    const ops = [];
    const ctx = {
        ops,
        canvas: null,
        globalAlpha: 1,
        lineWidth: 1,
        font: '',
        textAlign: '',
        textBaseline: '',
        fillStyle: '',
        strokeStyle: '',
        clearRect: (...a) => ops.push(['clearRect', ...a]),
        fillRect: (...a) => ops.push(['fillRect', ...a, ctx.fillStyle]),
        strokeRect: (...a) => ops.push(['strokeRect', ...a, ctx.strokeStyle]),
        fillText: (t, x, y) => ops.push(['fillText', t, x, y, ctx.fillStyle]),
        beginPath: () => ops.push(['beginPath']),
        moveTo: (...a) => ops.push(['moveTo', ...a]),
        lineTo: (...a) => ops.push(['lineTo', ...a]),
        closePath: () => ops.push(['closePath']),
        fill: () => ops.push(['fill', ctx.fillStyle, ctx.globalAlpha]),
        stroke: () => ops.push(['stroke', ctx.strokeStyle]),
        save: () => ops.push(['save']),
        restore: () => ops.push(['restore']),
        measureText: (t) => ({ width: String(t).length * 6 }),
    };
    return ctx;
}

function makeCanvas(w, h) {
    const ctx = makeCtx();
    const canvas = {
        width: 0,
        height: 0,
        getContext: () => ctx,
        getBoundingClientRect: () => ({ width: w, height: h }),
    };
    ctx.canvas = canvas;
    return canvas;
}

global.window = {
    cssVar: (name, fallback) => fallback || name,
};

const { PassTimelineView, elevationAt } = require('./pass-timeline.js');

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

function samplePasses() {
    const now = Date.now();
    return [
        { noradId: 1, name: 'ALPHA', aosMs: now + 60000, losMs: now + 600000, maxEl: 70 },
        { noradId: 2, name: 'BETA', aosMs: now + 300000, losMs: now + 900000, maxEl: 40 },
        { noradId: 3, name: 'GAMMA', aosMs: now + 1800000, losMs: now + 2400000, maxEl: 10 },
    ];
}

console.log('\n— elevationAt —');

test('is zero at AOS and LOS, maxEl at the midpoint', () => {
    const p = { aosMs: 0, losMs: 1000, maxEl: 80 };
    assert.strictEqual(elevationAt(p, 0), 0);
    assert.strictEqual(elevationAt(p, 1000), 0);
    assert.ok(Math.abs(elevationAt(p, 500) - 80) < 1e-9);
});

test('is zero outside the pass window', () => {
    const p = { aosMs: 100, losMs: 200, maxEl: 45 };
    assert.strictEqual(elevationAt(p, 99), 0);
    assert.strictEqual(elevationAt(p, 200), 0);
});

console.log('\n— PassTimelineView: construction —');

test('constructor without canvas does not crash', () => {
    const v = new PassTimelineView(null);
    v.draw();
    assert.ok(v);
});

test('uses mock passes when none supplied', () => {
    const v = new PassTimelineView(makeCanvas(800, 148));
    assert.ok(Array.isArray(v._passes));
    assert.ok(v._passes.length >= 6, 'mock schedule must be dense enough to overlap');
    for (const p of v._passes) {
        assert.strictEqual(typeof p.name, 'string');
        assert.ok(p.losMs > p.aosMs, `${p.name}: LOS must follow AOS`);
        assert.ok(p.maxEl > 0 && p.maxEl <= 90);
    }
});

test('window defaults to four hours', () => {
    const v = new PassTimelineView(makeCanvas(800, 148));
    assert.strictEqual(v._windowMin, 240);
});

test('accepts passes through options', () => {
    const list = samplePasses();
    const v = new PassTimelineView(makeCanvas(800, 148), { passes: list });
    assert.strictEqual(v._passes, list);
});

console.log('\n— PassTimelineView: sizing —');

test('draw sizes the canvas from its bounding box', () => {
    const canvas = makeCanvas(640, 148);
    const v = new PassTimelineView(canvas);
    v.draw();
    assert.strictEqual(canvas.width, 640);
    assert.strictEqual(canvas.height, 148);
});

test('collapsed layout box falls back to default dimensions', () => {
    const canvas = makeCanvas(0, 0);
    const v = new PassTimelineView(canvas);
    v.draw();
    assert.strictEqual(canvas.width, 400);
    assert.strictEqual(canvas.height, 88);
});

test('draw bails out when the plot area is too narrow', () => {
    const canvas = makeCanvas(50, 148);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    v.draw();
    const fills = canvas.getContext().ops.filter((o) => o[0] === 'fill');
    assert.strictEqual(fills.length, 0, 'no hills fit into a 50px wide widget');
});

console.log('\n— PassTimelineView: hill chart —');

test('draw paints overlapping filled hills, not Gantt bars', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    v.draw();
    const ops = canvas.getContext().ops;

    assert.ok(ops.some((o) => o[0] === 'clearRect'), 'canvas must be cleared');
    assert.strictEqual(ops.filter((o) => o[0] === 'strokeRect').length, 0,
        'Gantt strokeRect bars must not be used');
    const hills = ops.filter((o) => o[0] === 'closePath');
    assert.strictEqual(hills.length, 3, 'one closed path per pass');
    const fills = ops.filter((o) => o[0] === 'fill');
    assert.ok(fills.length >= 3, 'each hill is filled');
});

test('Y-axis is labelled in elevation degrees', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    v.draw();
    const texts = canvas.getContext().ops
        .filter((o) => o[0] === 'fillText')
        .map((o) => o[1]);
    assert.ok(texts.indexOf('0°') >= 0, `missing 0°, got ${texts.join('|')}`);
    assert.ok(texts.indexOf('90°') >= 0, `missing 90°, got ${texts.join('|')}`);
});

test('satellite names are drawn on the plot, not in a left column', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    v.draw();
    const labels = canvas.getContext().ops.filter((o) => o[0] === 'fillText');
    const alpha = labels.filter((o) => o[1] === 'ALPHA')[0];
    assert.ok(alpha, 'ALPHA label must be drawn');
    // Левая колонка имён была ~4px; подпись на горке лежит правее оси El (~28px)
    assert.ok(alpha[2] > 40, `label x=${alpha[2]} should sit on the plot, not in a gutter`);
});

test('time axis is labelled in HH:MM', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    v.draw();
    const texts = canvas.getContext().ops
        .filter((o) => o[0] === 'fillText')
        .map((o) => o[1]);
    assert.ok(texts.some((t) => /^\d{2}:\d{2}$/.test(t)), 'at least one HH:MM tick');
});

test('NOW marker is drawn', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    v.draw();
    const texts = canvas.getContext().ops
        .filter((o) => o[0] === 'fillText')
        .map((o) => o[1]);
    assert.ok(texts.indexOf('NOW') >= 0, 'NOW label must be present');
});

test('passes outside the time window are not drawn', () => {
    const now = Date.now();
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, {
        windowMin: 60,
        passes: [
            { noradId: 1, name: 'IN', aosMs: now + 60000, losMs: now + 300000, maxEl: 40 },
            { noradId: 2, name: 'OUT', aosMs: now + 5 * 3600000, losMs: now + 5 * 3600000 + 300000, maxEl: 80 },
        ],
    });
    v.draw();
    const texts = canvas.getContext().ops
        .filter((o) => o[0] === 'fillText')
        .map((o) => o[1]);
    assert.ok(texts.indexOf('IN') >= 0);
    assert.ok(texts.indexOf('OUT') < 0);
    assert.strictEqual(canvas.getContext().ops.filter((o) => o[0] === 'closePath').length, 1);
});

test('long satellite names on a hill are truncated', () => {
    const canvas = makeCanvas(900, 160);
    const now = Date.now();
    const v = new PassTimelineView(canvas, {
        passes: [{ noradId: 9, name: 'VERY-LONG-SATELLITE-NAME', aosMs: now, losMs: now + 300000, maxEl: 50 }],
    });
    v.draw();
    const drawn = canvas.getContext().ops
        .filter((o) => o[0] === 'fillText')
        .map((o) => o[1]);
    assert.ok(drawn.some((t) => t.indexOf('…') >= 0), `expected truncation, got ${drawn.join('|')}`);
});

console.log('\n— PassTimelineView: state —');

test('setPasses replaces the schedule and redraws', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    canvas.getContext().ops.length = 0;
    const next = [{ noradId: 7, name: 'DELTA', aosMs: Date.now(), losMs: Date.now() + 300000, maxEl: 50 }];
    v.setPasses(next);
    assert.strictEqual(v._passes, next);
    const texts = canvas.getContext().ops.filter((o) => o[0] === 'fillText').map((o) => o[1]);
    assert.ok(texts.indexOf('DELTA') >= 0, 'redraw must use the new schedule');
});

test('setPasses ignores a non-array argument', () => {
    const v = new PassTimelineView(makeCanvas(900, 160), { passes: samplePasses() });
    v.setPasses(null);
    assert.strictEqual(v._passes.length, 3);
});

test('setTrackingId uses a distinct stroke for the tracked hill', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    v.draw();
    const before = canvas.getContext().ops.filter((o) => o[0] === 'stroke').map((o) => o[1]);
    canvas.getContext().ops.length = 0;
    v.setTrackingId(1);
    const after = canvas.getContext().ops.filter((o) => o[0] === 'stroke').map((o) => o[1]);
    assert.strictEqual(v._trackingId, 1);
    assert.notDeepStrictEqual(after, before, 'tracked pass must change at least one stroke colour');
});

test('refreshAfterThemeChange redraws', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    canvas.getContext().ops.length = 0;
    v.refreshAfterThemeChange();
    assert.ok(canvas.getContext().ops.length > 0);
});

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) {
    process.exit(1);
}
