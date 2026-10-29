/**
 * Unit tests for ChannelOverlay geometry.
 *
 * Run: node static/js/channel-overlay.test.js
 *
 * Covers:
 *   - channel box on the plot: position, clipping at plot edges, outside marker
 *   - click position to channel offset, rounding
 *   - offset clamping so the channel stays inside the receiver band
 *   - edge drag to bandwidth, snapping to the offered widths
 *   - absolute channel edges as text
 */

'use strict';

const assert = require('assert');

global.window = {};
const {
    channelBox, offsetFromX, clampOffset, widthFromX, snapBandwidth, edgesText,
} = require('./channel-overlay.js');

let passed = 0;
let failed = 0;

function test(name, fn) {
    try {
        fn();
        passed++;
        console.log('  ok  ' + name);
    } catch (e) {
        failed++;
        console.log('  FAIL ' + name + '\n       ' + e.message);
    }
}

const BAND = { centerHz: 436_200_000, spanHz: 1_000_000 };
const PLOT = { left: 32, width: 1000 };

console.log('channelBox');

test('centered channel sits in the middle of the plot', () => {
    const b = channelBox(BAND, { offsetHz: 0, bandwidthHz: 20_000 }, PLOT);
    assert.strictEqual(b.centerX, 532);
    assert.strictEqual(b.left, 522);
    assert.strictEqual(b.width, 20);
    assert.strictEqual(b.outside, null);
});

test('offset moves the box by span fraction', () => {
    const b = channelBox(BAND, { offsetHz: 250_000, bandwidthHz: 10_000 }, PLOT);
    assert.strictEqual(b.centerX, 782);
    assert.strictEqual(b.left, 777);
    assert.strictEqual(b.width, 10);
});

test('box is clipped at the right plot edge', () => {
    const b = channelBox(BAND, { offsetHz: 495_000, bandwidthHz: 20_000 }, PLOT);
    assert.strictEqual(b.left, 1017);
    assert.strictEqual(b.width, 15, 'right part beyond the plot is cut');
    assert.strictEqual(b.outside, null);
});

test('channel beyond the band is reported as outside', () => {
    const right = channelBox(BAND, { offsetHz: 700_000, bandwidthHz: 20_000 }, PLOT);
    assert.strictEqual(right.outside, 'right');
    assert.strictEqual(right.width, 0);
    const left = channelBox(BAND, { offsetHz: -700_000, bandwidthHz: 20_000 }, PLOT);
    assert.strictEqual(left.outside, 'left');
});

test('no band yet gives no box', () => {
    assert.strictEqual(channelBox({ centerHz: 0, spanHz: 0 }, { offsetHz: 0, bandwidthHz: 1 }, PLOT), null);
    assert.strictEqual(channelBox(BAND, { offsetHz: 0, bandwidthHz: 20_000 }, { left: 0, width: 0 }), null);
});

console.log('offsetFromX');

test('plot center is zero offset', () => {
    assert.strictEqual(offsetFromX(532, BAND, PLOT), 0);
});

test('click offset is rounded to 10 Hz', () => {
    // 1 px = 1 kHz; 0.4567 px = 456.7 Hz
    assert.strictEqual(offsetFromX(532.4567, BAND, PLOT), 460);
    assert.strictEqual(offsetFromX(282, BAND, PLOT), -250_000);
});

console.log('clampOffset');

test('offset inside the band is kept', () => {
    assert.strictEqual(clampOffset(100_000, 20_000, 1_000_000), 100_000);
});

test('channel edge is held at the band edge', () => {
    assert.strictEqual(clampOffset(499_000, 20_000, 1_000_000), 490_000);
    assert.strictEqual(clampOffset(-600_000, 20_000, 1_000_000), -490_000);
});

console.log('widthFromX / snapBandwidth');

test('edge drag gives symmetric width', () => {
    // handle 15 px right of center at 1 kHz/px: half 15 kHz, width 30 kHz
    assert.strictEqual(widthFromX(547, 532, BAND, PLOT), 30_000);
    assert.strictEqual(widthFromX(517, 532, BAND, PLOT), 30_000, 'left handle works the same');
});

test('width snaps to the nearest offered value', () => {
    const opts = [2800, 6000, 15000, 30000, 50000];
    assert.strictEqual(snapBandwidth(26_000, opts), 30_000);
    assert.strictEqual(snapBandwidth(1, opts), 2800);
    assert.strictEqual(snapBandwidth(90_000, opts), 50_000);
});

test('no offered values keeps the width', () => {
    assert.strictEqual(snapBandwidth(12_345, []), 12_345);
});

console.log('edgesText');

test('edges in MHz around receiver plus offset', () => {
    assert.strictEqual(edgesText(436_200_000, -12_000, 20_000), '436.1780 – 436.1980 МГц');
});

test('unknown receiver frequency gives a dash', () => {
    assert.strictEqual(edgesText(0, 0, 20_000), '—');
});

console.log('visibility');

function fakeEl() {
    return {
        className: '',
        hidden: false,
        style: {},
        textContent: '',
        classList: { toggle() {} },
        addEventListener() {},
        appendChild() {},
    };
}

test('hidden channel mark removes the lines and the label', () => {
    global.document = {
        createElement: () => fakeEl(),
        addEventListener() {},
        removeEventListener() {},
    };
    const host = fakeEl();
    host.getBoundingClientRect = () => ({ left: 0, top: 0, width: 1000, height: 200 });
    const plot = fakeEl();
    plot.getBoundingClientRect = () => ({ left: 32, top: 0, width: 1000, height: 80 });
    const { ChannelOverlay } = require('./channel-overlay.js');
    const ov = new ChannelOverlay(host, { plotCanvas: plot, marginLeft: 0, marginRight: 0 });
    ov.setBand(436_500_000, 2_048_000);
    ov.setChannel(0, 15_000);
    assert.strictEqual(ov._root.hidden, false);
    ov.setVisible(false);
    assert.strictEqual(ov._root.hidden, true, 'lines and the label are hidden');
    assert.strictEqual(ov._arrow.hidden, true);
    ov.setVisible(true);
    assert.strictEqual(ov._root.hidden, false);
    ov.destroy();
});

console.log('\n' + passed + ' passed, ' + failed + ' failed');
if (failed > 0) { process.exit(1); }
