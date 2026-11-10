/**
 * Unit tests for ChannelOverlay geometry.
 *
 * Run: node static/js/channel-overlay.test.js
 *
 * Covers:
 *   - channel box on the plot: position, clipping at plot edges, outside marker
 *   - click position to channel offset, rounding
 *   - offset clamping so the channel stays inside the receiver band
 *   - click reports the frequency under the pointer; drag pans the picture
 *   - absolute channel edges as text
 *   - doppler shift of the whole channel, SSB passband beside the carrier
 */

'use strict';

const assert = require('assert');

global.window = {};
const {
    channelBox, offsetFromX, clampOffset, edgesText,
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
    assert.strictEqual(ov._guide.hidden, false, 'the centre guide shows while the demodulator is off');
    assert.strictEqual(ov._guide.style.left, '532px');
    ov.setVisible(true);
    assert.strictEqual(ov._root.hidden, false);
    assert.strictEqual(ov._guide.hidden, true, 'the demodulator stripe is the centre');
    ov.destroy();
});

console.log('doppler and width limit');

function makeOverlay(onChange) {
    global.document = {
        createElement: () => fakeEl(),
        addEventListener() {},
        removeEventListener() {},
    };
    const host = fakeEl();
    host.getBoundingClientRect = () => ({ left: 0, top: 0, width: 1100, height: 200 });
    const plot = fakeEl();
    plot.getBoundingClientRect = () => ({ left: 32, top: 0, width: 1000, height: 80 });
    const { ChannelOverlay } = require('./channel-overlay.js');
    const ov = new ChannelOverlay(host, { plotCanvas: plot, marginLeft: 0, marginRight: 0, onChange });
    ov.setBand(BAND.centerHz, BAND.spanHz);
    return ov;
}

test('doppler moves the whole channel, the center line stays in the middle of the fill', () => {
    const ov = makeOverlay();
    ov.setChannel(0, 2_000);
    ov.setShift(5_000);
    const left = parseFloat(ov._root.style.left);
    assert.ok(Math.abs(left - 536) < 1e-9, 'fill around offset + doppler: ' + left);
    const mark = left + parseFloat(ov._center.style.left);
    assert.ok(Math.abs(mark - 537) < 1e-9, 'center line at offset + doppler: ' + mark);
    ov.destroy();
});

test('the fill follows the chosen width at once, wider too', () => {
    const ov = makeOverlay();
    ov.setChannel(0, 2_000);
    ov.setChannel(0, 15_000);
    assert.ok(Math.abs(parseFloat(ov._root.style.width) - 15) < 1e-9);
    ov.destroy();
});

test('click reports the frequency under the pointer and does not move the channel', () => {
    let tuned = null;
    const ov = makeOverlay();
    ov._opts.onTune = (hz) => { tuned = hz; };
    ov.setChannel(0, 2_000);
    ov.setShift(5_000);
    ov._drag = { mode: 'click', x0: 632, moved: false, offset0: 0, target: null };
    ov._dragEnd({ clientX: 632 });
    assert.strictEqual(ov._ch.offsetHz, 0);
    assert.strictEqual(tuned, 436_300_000);
    ov.destroy();
});

test('USB fills the right half of the column, LSB the left', () => {
    const ov = makeOverlay();
    ov.setSide('usb');
    assert.strictEqual(ov._side.hidden, false);
    assert.ok(ov._side.className.indexOf('is-usb') >= 0);
    ov.setSide('lsb');
    assert.ok(ov._side.className.indexOf('is-lsb') >= 0);
    ov.setSide('');
    assert.strictEqual(ov._side.hidden, true);
    ov.destroy();
});

test('a lower limit narrows the channel already drawn', () => {
    const ov = makeOverlay();
    ov.setChannel(0, 15_000);
    ov.setMaxWidth(1_000);
    assert.ok(Math.abs(parseFloat(ov._root.style.width) - 1) < 1e-9);
    ov.destroy();
});


test('setShift with bad values keeps the channel unshifted', () => {
    const ov = makeOverlay();
    ov.setChannel(0, 2_000);
    ov.setShift(undefined, NaN);
    assert.ok(Math.abs(parseFloat(ov._root.style.left) - 531) < 1e-9);
    ov.destroy();
});

console.log('zoomed window');

// 100 kHz window centered 50 kHz above the receiver
const WIN = { spanHz: 100_000, offsetHz: 50_000 };

test('channel at the window center sits in the middle of the plot', () => {
    const box = channelBox(WIN, { offsetHz: 50_000, bandwidthHz: 10_000 }, PLOT);
    assert.strictEqual(box.left, 482);
    assert.strictEqual(box.width, 100);
    assert.strictEqual(box.centerX, 532);
});

test('channel outside the zoomed window is reported as outside', () => {
    const box = channelBox(WIN, { offsetHz: -20_000, bandwidthHz: 10_000 }, PLOT);
    assert.strictEqual(box.outside, 'left');
});

test('click offset counts from the window center', () => {
    assert.strictEqual(offsetFromX(PLOT.left + PLOT.width, WIN, PLOT), 100_000);
});

test('setView zooms the channel box', () => {
    const ov = makeOverlay();
    ov.setView(200_000, 100_000);
    ov.setChannel(200_000, 10_000);
    assert.strictEqual(parseFloat(ov._root.style.left), 482);
    assert.strictEqual(parseFloat(ov._root.style.width), 100);
    ov.destroy();
});

test('drag on the plot pans the window and does not move the channel', () => {
    let changed = 0;
    const pans = [];
    const ov = makeOverlay(() => { changed++; });
    ov._opts.onPan = (dHz) => { pans.push(dHz); };
    ov.setView(0, 100_000);
    ov.setChannel(0, 2_000);
    ov._drag = { mode: 'click', x0: 500, xLast: 500, moved: false, offset0: 0 };
    ov._dragMove({ clientX: 600 });
    ov._dragMove({ clientX: 650 });
    ov._dragEnd({ clientX: 650 });
    // content follows the pointer: dragging right shows lower frequencies
    assert.deepStrictEqual(pans, [-10_000, -5_000]);
    assert.strictEqual(changed, 0, 'no channel settings sent');
    ov.destroy();
});

test('hidden channel: drag on the plot still pans, click does not move the channel', () => {
    let changed = 0;
    const pans = [];
    const listeners = {};
    global.document = {
        createElement: () => fakeEl(),
        addEventListener() {},
        removeEventListener() {},
    };
    const host = fakeEl();
    host.getBoundingClientRect = () => ({ left: 0, top: 0, width: 1100, height: 200 });
    const plot = fakeEl();
    plot.getBoundingClientRect = () => ({ left: 32, top: 0, width: 1000, height: 80 });
    plot.addEventListener = (type, fn) => { listeners[type] = fn; };
    const { ChannelOverlay } = require('./channel-overlay.js');
    const ov = new ChannelOverlay(host, {
        plotCanvas: plot, clickTargets: [plot], marginLeft: 0, marginRight: 0,
        onChange: () => { changed++; }, onPan: (dHz) => { pans.push(dHz); },
    });
    ov.setBand(BAND.centerHz, BAND.spanHz);
    ov.setView(0, 100_000);
    ov.setVisible(false);
    const ev = (x) => ({ clientX: x, button: 0, preventDefault() {}, stopPropagation() {}, currentTarget: plot });
    listeners.pointerdown(ev(500));
    ov._dragMove(ev(520));
    ov._dragEnd(ev(520));
    assert.deepStrictEqual(pans, [-2_000]);
    listeners.pointerdown(ev(500));
    ov._dragEnd(ev(500));
    assert.strictEqual(changed, 0);
    ov.destroy();
});

console.log('\n' + passed + ' passed, ' + failed + ' failed');
if (failed > 0) { process.exit(1); }
