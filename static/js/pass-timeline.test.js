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
        stroke: () => ops.push(['stroke', ctx.strokeStyle, ctx.lineWidth]),
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
        addEventListener: () => {},
        removeEventListener: () => {},
    };
    ctx.canvas = canvas;
    return canvas;
}

global.window = {
    cssVar: (name, fallback) => fallback || name,
};

const { PassTimelineView, elevationAt, passesFromGroup, tickStepMin, elCeil, peakEl, inSession } = require('./pass-timeline.js');

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

test('peaks at TCA when the pass is asymmetric', () => {
    const p = { aosMs: 0, losMs: 1000, tcaMs: 300, maxEl: 60 };
    assert.ok(Math.abs(elevationAt(p, 300) - 60) < 1e-9, 'maxEl exactly at TCA');
    assert.ok(elevationAt(p, 500) < 60, 'midpoint is past the peak');
    assert.ok(elevationAt(p, 150) < elevationAt(p, 300));
    assert.ok(elevationAt(p, 650) < elevationAt(p, 300));
});

test('ignores a TCA outside the pass and falls back to the midpoint', () => {
    const p = { aosMs: 0, losMs: 1000, tcaMs: 5000, maxEl: 30 };
    assert.ok(Math.abs(elevationAt(p, 500) - 30) < 1e-9);
});

console.log('\n— passesFromGroup —');

test('maps satellite_group_update into timeline passes', () => {
    const r = passesFromGroup({
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', sat_alias: 'ZARYA', aos: 1000, los: 9000, tca: 4000, tca_el: 63.2 },
            { norad_id: 7530, sat_name: 'OSCAR 7', aos: 2000, los: 8000, tca: 5000, tca_el: 21 },
        ],
        time_window: { start: 1000, end: 9000 },
    });
    assert.deepStrictEqual(r.passes[0], {
        noradId: 25544, name: 'ISS', aosMs: 1000, losMs: 9000, tcaMs: 4000, maxEl: 63.2,
    });
    assert.strictEqual(r.passes[1].name, 'OSCAR 7');
    assert.deepStrictEqual(r.window, { startMs: 1000, endMs: 9000 });
});

test('skips satellites without a valid pass', () => {
    const r = passesFromGroup({
        satellites: [
            { norad_id: 1, sat_name: 'A', aos: 0, los: 0, tca_el: 10 },
            { norad_id: 2, sat_name: 'B', aos: 5000, los: 4000, tca_el: 10 },
            { norad_id: 3, sat_name: 'C', aos: 1000, los: 2000, tca_el: 10 },
        ],
    });
    assert.deepStrictEqual(r.passes.map((p) => p.noradId), [3]);
    assert.strictEqual(r.window, null, 'no time_window in the event');
});

test('prefers the hour-ahead timeline over the session plan satellites', () => {
    const r = passesFromGroup({
        satellites: [{ norad_id: 1, sat_name: 'NOW', aos: 1000, los: 2000, tca_el: 30 }],
        timeline: [
            { norad_id: 1, sat_name: 'NOW', aos: 1000, los: 2000, tca: 1500, tca_el: 30 },
            { norad_id: 2, sat_name: 'LATER', sat_alias: 'L', aos: 5000, los: 6000, tca: 5500, tca_el: 12 },
        ],
    });
    assert.deepStrictEqual(r.passes.map((p) => p.name), ['NOW', 'LATER'], 'sat_name over alias');
});

test('empty or missing group gives an empty schedule', () => {
    assert.deepStrictEqual(passesFromGroup(null), { passes: [], window: null });
    assert.deepStrictEqual(passesFromGroup({ satellites: [] }).passes, []);
});

console.log('\n— tickStepMin —');

test('picks a time tick step that keeps the axis readable', () => {
    assert.strictEqual(tickStepMin(15), 2);
    assert.strictEqual(tickStepMin(60), 10);
    assert.strictEqual(tickStepMin(240), 30);
});

console.log('\n— PassTimelineView: construction —');

test('constructor without canvas does not crash', () => {
    const v = new PassTimelineView(null);
    v.draw();
    assert.ok(v);
});

test('starts with an empty schedule, no made-up passes', () => {
    const v = new PassTimelineView(makeCanvas(800, 148));
    assert.deepStrictEqual(v._passes, []);
    v.draw();
    const ops = v._canvas.getContext().ops;
    assert.strictEqual(ops.filter((o) => o[0] === 'closePath').length, 0);
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
    assert.strictEqual(canvas.height, 100);
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

test('elevation ceiling follows the highest pass with headroom', () => {
    assert.strictEqual(elCeil(0), 90, 'empty chart keeps the full sky');
    assert.strictEqual(elCeil(5), 20, 'a tiny pass is not blown up to the full height');
    assert.strictEqual(elCeil(25), 30);
    assert.strictEqual(elCeil(30), 45, 'a peak never touches the top edge');
    assert.strictEqual(elCeil(50), 60);
    assert.strictEqual(elCeil(70), 90);
});

test('low passes stretch the Y-axis instead of hugging the floor', () => {
    const now = Date.now();
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas);
    v.setPasses([{ noradId: 1, name: 'LOW', aosMs: now, losMs: now + 600000, tcaMs: now + 300000, maxEl: 12 }]);
    const texts = canvas.getContext().ops.filter((o) => o[0] === 'fillText');
    const ticks = texts.map((o) => o[1]);
    assert.ok(ticks.indexOf('20°') >= 0, `axis tops at 20°, got ${ticks.join('|')}`);
    assert.ok(ticks.indexOf('90°') < 0, 'no 90° tick for a 12° pass');
    const label = texts.filter((o) => o[1] === 'LOW')[0];
    const plotTop = 12;
    const plotH = 160 - 12 - 12;
    assert.ok(label[3] < plotTop + plotH / 2, `peak label y=${label[3]} sits in the upper half`);
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

test('time axis left edge stays at now − PAST; right edge follows aheadMin', () => {
    const now = Date.now();
    const v = new PassTimelineView(makeCanvas(900, 160));
    // Длинное окно группы больше не раздувает ось — масштаб задаёт пользователь
    v.setPasses([], { startMs: now - 600000, endMs: now + 7 * 3600000 });
    const span = v._timeSpan(now);
    assert.strictEqual(span.t0, now - 3 * 60000, 'left edge fixed at now − 3 min');
    assert.strictEqual(span.t1, now + 60 * 60000, 'default one hour ahead');
});

test('mouse wheel zooms the horizon with the left edge fixed', () => {
    const now = Date.now();
    const v = new PassTimelineView(makeCanvas(900, 160));
    assert.strictEqual(v._aheadMin, 60);
    v.zoomWheel(-100); // вверх — крупнее
    assert.strictEqual(v._aheadMin, 45);
    assert.strictEqual(v._timeSpan(now).t0, now - 3 * 60000);
    assert.strictEqual(v._timeSpan(now).t1, now + 45 * 60000);
    v.zoomWheel(100); // вниз — обратно
    v.zoomWheel(100);
    assert.strictEqual(v._aheadMin, 90);
    // Упоры шкалы
    v._aheadMin = 15;
    v.zoomWheel(-100);
    assert.strictEqual(v._aheadMin, 15);
    v._aheadMin = 360;
    v.zoomWheel(100);
    assert.strictEqual(v._aheadMin, 360);
});

test('hill label sits above TCA, not the pass midpoint', () => {
    const now = Date.now();
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas);
    v.setPasses([{ noradId: 1, name: 'TCA', aosMs: now, losMs: now + 600000, tcaMs: now + 120000, maxEl: 50 }],
        { startMs: now, endMs: now + 600000 });
    const span = v._timeSpan(now);
    const label = canvas.getContext().ops.filter((o) => o[0] === 'fillText' && o[1] === 'TCA')[0];
    assert.ok(label, 'label drawn');
    const plotW = 900 - 24 - 4;
    const xTca = 24 + (now + 120000 - span.t0) / (span.t1 - span.t0) * plotW;
    assert.ok(Math.abs(label[2] - xTca) < 1, `label x=${label[2]}, TCA x=${xTca}`);
});

test('setPasses ignores a non-array argument', () => {
    const v = new PassTimelineView(makeCanvas(900, 160), { passes: samplePasses() });
    v.setPasses(null);
    assert.strictEqual(v._passes.length, 3);
});

test('setTrackingId thickens the stroke of the tracked hill', () => {
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, { passes: samplePasses() });
    v.setTrackingId(1);
    const strokes = canvas.getContext().ops.filter((o) => o[0] === 'stroke');
    assert.ok(strokes.some((o) => o[2] >= 2), 'tracked hill stroke is thicker');
});

test('focused hill is drawn last so it sits in the foreground', () => {
    const now = Date.now();
    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, {
        passes: [
            { noradId: 1, name: 'LOW', aosMs: now, losMs: now + 600000, maxEl: 20 },
            { noradId: 2, name: 'FOCUS', aosMs: now, losMs: now + 600000, maxEl: 10 },
            { noradId: 3, name: 'HIGH', aosMs: now, losMs: now + 600000, maxEl: 50 },
        ],
    });
    v.setSelectedId(2);
    const labels = canvas.getContext().ops.filter((o) => o[0] === 'fillText').map((o) => o[1]);
    const focusAt = labels.lastIndexOf('FOCUS');
    const highAt = labels.lastIndexOf('HIGH');
    const lowAt = labels.lastIndexOf('LOW');
    assert.ok(focusAt > highAt && focusAt > lowAt, 'FOCUS label is painted after the others');
});

test('inSession matches the one-minute sliding window', () => {
    const now = 1_000_000;
    assert.ok(inSession({ aosMs: now - 1000, losMs: now + 10000 }, now));
    assert.ok(inSession({ aosMs: now + 30_000, losMs: now + 90_000 }, now), 'AOS within a minute');
    assert.ok(!inSession({ aosMs: now + 120_000, losMs: now + 200_000 }, now), 'AOS too far');
    assert.ok(!inSession({ aosMs: now - 200_000, losMs: now - 1000 }, now), 'already finished');
});

test('hills recolor when the pass enters the session window', () => {
    const now = Date.now();
    const canvas = makeCanvas(900, 160);
    const pass = { noradId: 1, name: 'SOON', aosMs: now + 90_000, losMs: now + 400_000, maxEl: 40 };
    const v = new PassTimelineView(canvas, {
        passes: [pass],
        satColors: { 1: '#ff4488' },
    });
    v.draw();
    let strokes = canvas.getContext().ops.filter((o) => o[0] === 'stroke').map((o) => o[1]);
    assert.ok(strokes.indexOf('#00d4aa') >= 0, 'still green before the window');
    assert.ok(strokes.indexOf('#ff4488') < 0);

    // Сдвигаем AOS в окно сеанса без смены палитры — только время
    pass.aosMs = now + 20_000;
    canvas.getContext().ops.length = 0;
    v.draw();
    strokes = canvas.getContext().ops.filter((o) => o[0] === 'stroke').map((o) => o[1]);
    assert.ok(strokes.indexOf('#ff4488') >= 0, 'palette color once inside the window');
});

test('Y-scale follows the session peak, not a taller pass outside the session', () => {
    const now = Date.now();
    assert.strictEqual(peakEl([
        { noradId: 1, maxEl: 12, aosMs: now - 1000, losMs: now + 60_000 },
        { noradId: 2, maxEl: 80, aosMs: now + 120_000, losMs: now + 300_000 },
    ], now), 12);
    assert.strictEqual(peakEl([
        { noradId: 2, maxEl: 80, aosMs: now + 120_000, losMs: now + 300_000 },
    ], now), 80, 'no session pass: fall back to all');

    const canvas = makeCanvas(900, 160);
    const v = new PassTimelineView(canvas, {
        passes: [
            { noradId: 1, name: 'LOW', aosMs: now - 1000, losMs: now + 600000, tcaMs: now + 300000, maxEl: 12 },
            { noradId: 2, name: 'HIGH', aosMs: now + 300000, losMs: now + 700000, tcaMs: now + 500000, maxEl: 80 },
        ],
        satColors: { 1: '#ff4488', 2: '#44aaff' },
    });
    v.draw();
    const ticks = canvas.getContext().ops.filter((o) => o[0] === 'fillText').map((o) => o[1]);
    assert.ok(ticks.indexOf('20°') >= 0, `session at 12° → ceiling 20°, got ${ticks.join('|')}`);
    assert.ok(ticks.indexOf('90°') < 0, 'tall outsider must not force the full sky');
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
