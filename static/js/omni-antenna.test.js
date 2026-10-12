/**
 * Unit tests for omni-antenna.js — gain model of fixed antennas and the two
 * canvas views of the Antenna tab (lobe and gain over the pass).
 *
 * Run: node static/js/omni-antenna.test.js
 */
'use strict';

const assert = require('assert');

global.window = {};

const {
    gainDb, isOmniKind, skyPathPass, passGainCurve,
    OmniLobeView, OmniGainView, GAIN_FLOOR_DB,
} = require('./omni-antenna.js');

let passed = 0;
let failed = 0;
function test(name, fn) {
    try { fn(); passed++; console.log('PASS: ' + name); } catch (e) { failed++; console.error('FAIL: ' + name + '\n  ' + e.message); }
}

function near(actual, expected, tol, msg) {
    assert.ok(Math.abs(actual - expected) <= tol, (msg || '') + ` got ${actual}, want ${expected}±${tol}`);
}

/** Canvas stub: records draw calls, never fails on unknown context methods. */
function fakeCanvas(w, h) {
    const calls = [];
    const ctx = new Proxy({}, {
        get(target, prop) {
            if (prop in target) { return target[prop]; }
            if (prop === 'measureText') { return () => ({ width: 10 }); }
            if (prop === 'createLinearGradient') { return () => ({ addColorStop() {} }); }
            return (...args) => { calls.push([prop, ...args]); };
        },
        set(target, prop, value) { target[prop] = value; return true; },
    });
    return {
        width: w, height: h, calls,
        getContext: () => ctx,
        getBoundingClientRect: () => ({ width: w, height: h }),
    };
}

console.log('\n— gain model —');

test('QFH is strongest at the zenith and only a few dB down at the horizon', () => {
    near(gainDb('qfh', 0, 90), 0, 1e-9);
    near(gainDb('qfh', 0, 0), 10 * Math.log10(0.35), 1e-9);
    assert.ok(gainDb('qfh', 0, 30) < gainDb('qfh', 0, 60));
});

test('whip is strongest at the horizon and deaf at the zenith', () => {
    near(gainDb('whip', 0, 0), 0, 1e-9);
    assert.strictEqual(gainDb('whip', 0, 90), GAIN_FLOOR_DB);
    assert.ok(gainDb('whip', 0, 60) < gainDb('whip', 0, 30));
});

test('fixed yagi looks at the zenith with a ~50 degree beam', () => {
    near(gainDb('yagi', 0, 90), 0, 1e-9);
    near(gainDb('yagi', 0, 65), -3, 0.3, '25 degrees off the axis is the half-power edge');
    assert.strictEqual(gainDb('yagi', 0, 0), GAIN_FLOOR_DB);
});

test('horizontal dipole on the N-S axis is deaf along the axis, full broadside', () => {
    near(gainDb('dipole', 123, 90), 0, 1e-9);
    assert.strictEqual(gainDb('dipole', 0, 0), GAIN_FLOOR_DB, 'north horizon is along the wire');
    assert.strictEqual(gainDb('dipole', 180, 0), GAIN_FLOOR_DB);
    near(gainDb('dipole', 90, 0), 0, 1e-9, 'east horizon is broadside');
});

test('below the horizon every antenna is at the floor', () => {
    ['qfh', 'yagi', 'dipole', 'whip'].forEach((k) => {
        assert.strictEqual(gainDb(k, 90, -5), GAIN_FLOOR_DB, k);
    });
});

test('unknown kind has no pattern', () => {
    assert.strictEqual(gainDb('helix', 0, 45), null);
    assert.strictEqual(isOmniKind('helix'), false);
    assert.strictEqual(isOmniKind(null), false);
    ['qfh', 'yagi', 'dipole', 'whip'].forEach((k) => assert.strictEqual(isOmniKind(k), true, k));
});

console.log('\n— pass —');

const MIN = 60000;

/** Group entry as SSE sends it: AOS/LOS and the sky path of the pass. */
function groupSat(azOf) {
    const skyPath = [];
    for (let i = 0; i <= 10; i++) {
        skyPath.push({ time: (i - 1) * MIN, az: azOf(i), el: i === 0 || i === 10 ? 0 : 60 * Math.sin(Math.PI * i / 10) });
    }
    return { norad_id: 25544, aos: 0, los: 9 * MIN, sky_path: skyPath };
}
const PASS = skyPathPass(groupSat(() => 90));

test('pass is taken from the group entry, clipped to AOS..LOS', () => {
    assert.strictEqual(PASS.aosMs, 0);
    assert.strictEqual(PASS.losMs, 9 * MIN);
    assert.strictEqual(PASS.points[0].tMs, 0);
    assert.strictEqual(PASS.points[PASS.points.length - 1].tMs, 9 * MIN);
    assert.ok(PASS.points.every((p) => typeof p.az === 'number' && typeof p.el === 'number'));
});

test('no pass without AOS/LOS or a sky path', () => {
    assert.strictEqual(skyPathPass(null), null);
    assert.strictEqual(skyPathPass({ norad_id: 1, aos: 0, los: 0, sky_path: [] }), null);
    assert.strictEqual(skyPathPass({ norad_id: 1, aos: 0, los: MIN }), null);
});

test('gain curve follows the pattern along the sky path', () => {
    const qfh = passGainCurve('qfh', PASS);
    assert.strictEqual(qfh.length, PASS.points.length);
    const top = qfh.reduce((a, b) => (b.el > a.el ? b : a));
    assert.ok(top.db > qfh[1].db, 'QFH hears best at TCA');
    const whip = passGainCurve('whip', PASS);
    const whipTop = whip.reduce((a, b) => (b.el > a.el ? b : a));
    assert.ok(whipTop.db < whip[1].db, 'whip loses the satellite at TCA');
});

test('dipole curve depends on where the pass goes', () => {
    const broadside = passGainCurve('dipole', skyPathPass(groupSat(() => 90)));
    const alongWire = passGainCurve('dipole', skyPathPass(groupSat(() => 0)));
    assert.ok(alongWire[1].db < broadside[1].db, 'low over the north the dipole is deaf');
});

test('gain curve is empty without a pass or a known kind', () => {
    assert.deepStrictEqual(passGainCurve('qfh', null), []);
    assert.deepStrictEqual(passGainCurve('helix', PASS), []);
});

console.log('\n— views —');

test('lobe view draws the wireframe and the ray to the satellite', () => {
    const c = fakeCanvas(240, 240);
    const v = new OmniLobeView(c);
    v.setKind('qfh');
    v.setSatellite(83, 74);
    v.draw();
    assert.ok(c.calls.some((x) => x[0] === 'stroke'), 'wireframe must be stroked');
    const texts = c.calls.filter((x) => x[0] === 'fillText').map((x) => String(x[1]));
    assert.ok(texts.some((t) => /дБ/.test(t)), 'gain toward the satellite is labelled');
});

test('lobe view without a satellite draws the lobe and no gain label', () => {
    const c = fakeCanvas(240, 240);
    const v = new OmniLobeView(c);
    v.setKind('whip');
    v.setSatellite(null, null);
    v.draw();
    const texts = c.calls.filter((x) => x[0] === 'fillText').map((x) => String(x[1]));
    assert.ok(!texts.some((t) => /дБ/.test(t)));
});

test('gain view marks "now" inside the pass', () => {
    const c = fakeCanvas(240, 240);
    const v = new OmniGainView(c);
    v.setKind('qfh');
    v.setPass(PASS);
    v.setSatellite(90, 60);
    v.draw(4 * MIN);
    const texts = c.calls.filter((x) => x[0] === 'fillText').map((x) => String(x[1]));
    assert.ok(texts.some((t) => /сейчас/.test(t)));
});

test('gain view without a pass says so', () => {
    const c = fakeCanvas(240, 240);
    const v = new OmniGainView(c);
    v.setKind('qfh');
    v.setPass(null);
    v.draw(0);
    const texts = c.calls.filter((x) => x[0] === 'fillText').map((x) => String(x[1]));
    assert.ok(texts.some((t) => /нет пролёта/i.test(t)));
});

test('views resize their canvas and skip drawing when hidden', () => {
    const c = fakeCanvas(0, 0);
    const v = new OmniLobeView(c);
    v.setKind('qfh');
    v.resize(0, 0);
    v.draw();
    assert.strictEqual(c.calls.length, 0);
    v.resize(200, 180);
    assert.strictEqual(c.width, 200);
    assert.strictEqual(c.height, 180);
});

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) { process.exit(1); }
