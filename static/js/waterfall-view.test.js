/**
 * Waterfall history survives a 1 px layout jitter and a real resize.
 *
 * Run: node static/js/waterfall-view.test.js
 */

'use strict';

const assert = require('assert');

global.window = global;
global.cssVar = function(_name, fallback) { return fallback; };

require('./bottom-panel.js');

const queue = [];
let passed = 0;
let failed = 0;
function test(name, fn) { queue.push([name, fn]); }

/** Canvas stub: client size is the layout, width/height is the bitmap. */
function makeCanvas(w, h) {
    const ctx = {
        createImageData: function(cw, ch) {
            return { width: cw, height: ch, data: new Uint8ClampedArray(cw * ch * 4) };
        },
        putImageData: function() {},
    };
    return {
        clientWidth: w,
        clientHeight: h,
        offsetWidth: w,
        offsetHeight: h,
        width: 0,
        height: 0,
        getContext: function() { return ctx; },
    };
}

/** Pixel RGB at bitmap column x, row y (row 0 is the newest line). */
function pixel(view, x, y) {
    const img = view._imageData;
    const i = (y * img.width + x) * 4;
    return [img.data[i], img.data[i + 1], img.data[i + 2]];
}

const HOT = [245, 148, 106]; // colormap at 1

test('a 1 px layout change keeps the bitmap and the painted lines', () => {
    const canvas = makeCanvas(40, 20);
    const view = new window.WaterfallView(canvas, null, { marginLeft: 0, marginRight: 0 });
    view.pushLine(new Float32Array(16).fill(1));
    view.pushLine(new Float32Array(16).fill(1));
    canvas.clientWidth = 41;
    canvas.offsetWidth = 41;
    view.pushLine(new Float32Array(16).fill(0));
    assert.strictEqual(canvas.width, 40, 'a single pixel must not rebuild the bitmap');
    assert.deepStrictEqual(pixel(view, 4, 2), HOT, 'older lines stay after the jitter');
});

test('a real resize keeps already painted lines instead of starting blank', () => {
    const canvas = makeCanvas(40, 20);
    const view = new window.WaterfallView(canvas, null, { marginLeft: 0, marginRight: 0 });
    view.pushLine(new Float32Array(16).fill(1));
    view.pushLine(new Float32Array(16).fill(1));
    canvas.clientWidth = 70;
    canvas.offsetWidth = 70;
    view.pushLine(new Float32Array(16).fill(0));
    assert.strictEqual(canvas.width, 70);
    assert.deepStrictEqual(pixel(view, 4, 2), HOT, 'history is carried into the new width');
});

for (const [name, fn] of queue) {
    try {
        fn();
        passed++;
        console.log('  ✓ ' + name);
    } catch (err) {
        failed++;
        console.log('  ✗ ' + name);
        console.log(err && err.stack ? err.stack : err);
    }
}
console.log(passed + ' passed, ' + failed + ' failed');
if (failed) { process.exit(1); }
