/**
 * Unit tests for tx-markers.js (pure helpers, no DOM).
 *
 * Run: node static/js/tx-markers.test.js
 */

'use strict';

const assert = require('assert');
const { markerX, labelRows, markersWithLabels, satNames } = require('./tx-markers.js');

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

const PLOT = { left: 32, width: 1000 };
const BAND = { centerHz: 436500000, spanHz: 1000000 };

console.log('\n— markerX —');

test('places the receiver centre in the middle of the plot', () => {
    assert.strictEqual(markerX(436500000, BAND, null, PLOT), 532);
});

test('maps frequency linearly across the receiver window', () => {
    assert.strictEqual(markerX(436250000, BAND, null, PLOT), 282);
    assert.strictEqual(markerX(437000000, BAND, null, PLOT), 1032);
});

test('follows the zoom window', () => {
    // 100 kHz visible around +200 kHz from the centre
    const view = { offsetHz: 200000, spanHz: 100000 };
    assert.strictEqual(markerX(436700000, BAND, view, PLOT), 532);
    assert.strictEqual(markerX(436725000, BAND, view, PLOT), 782);
});

test('is null outside the visible part', () => {
    const view = { offsetHz: 200000, spanHz: 100000 };
    assert.strictEqual(markerX(436500000, BAND, view, PLOT), null);
    assert.strictEqual(markerX(438000000, BAND, null, PLOT), null);
});

test('is null without a band or plot', () => {
    assert.strictEqual(markerX(436500000, null, null, PLOT), null);
    assert.strictEqual(markerX(436500000, BAND, null, null), null);
});

console.log('\n— labelRows —');

test('labels far apart share the first row', () => {
    assert.deepStrictEqual(labelRows([100, 300, 500], 120, 3), [0, 0, 0]);
});

test('close labels step down to the next free row', () => {
    assert.deepStrictEqual(labelRows([100, 150, 200, 400], 120, 3), [0, 1, 2, 0]);
});

test('rows are limited, extra labels wrap to the last row', () => {
    assert.deepStrictEqual(labelRows([100, 110, 120], 120, 2), [0, 1, 1]);
});

console.log('\n— markersWithLabels —');

test('labels markers from the ribbon by uuid, satellite · modulation', () => {
    const ribbon = [
        { uuid: 'tx-iss', noradId: 25544, label: 'ISS · Voice', modulation: 'FM' },
    ];
    const group = { satellites: [
        { norad_id: 25544, sat_name: 'ISS', sat_alias: 'ZARYA' },
        { norad_id: 7530, sat_name: 'OSCAR 7' },
    ] };
    const out = markersWithLabels([
        { norad_id: 25544, uuid: 'tx-iss', freq_hz: 436601000, doppler_hz: 1000 },
        { norad_id: 7530, uuid: 'tx-unknown', freq_hz: 436400000, doppler_hz: -500 },
    ], ribbon, group);
    assert.deepStrictEqual(out[0], {
        noradId: 25544, uuid: 'tx-iss', freqHz: 436601000, nominalHz: 436600000,
        label: 'ISS · FM', tx: ribbon[0],
    });
    assert.strictEqual(out[1].label, 'OSCAR 7', 'no ribbon entry: satellite name only');
    assert.strictEqual(out[1].nominalHz, 436400500);
    assert.strictEqual(out[1].tx, null);
});

console.log('\n— satNames —');

test('names by NORAD from satellites and timeline, satellites first, alias when no name', () => {
    const names = satNames({
        satellites: [{ norad_id: 25544, sat_name: 'ISS' }, { norad_id: 43017, sat_alias: 'AO-91' }],
        timeline: [{ norad_id: 25544, sat_name: 'ZARYA' }, { norad_id: 7530, sat_name: 'OSCAR 7' }],
    });
    assert.deepStrictEqual(names, { 25544: 'ISS', 43017: 'AO-91', 7530: 'OSCAR 7' });
    assert.deepStrictEqual(satNames(null), {});
});

test('ignores malformed markers', () => {
    assert.deepStrictEqual(markersWithLabels(null, [], null), []);
    assert.deepStrictEqual(markersWithLabels([{ uuid: 'x' }], [], null), []);
});

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) {
    process.exit(1);
}
