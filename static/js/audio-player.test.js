/**
 * Unit tests for AudioPlayer.
 *
 * Run: node static/js/audio-player.test.js
 *
 * Covers:
 *   - scheduling: blocks play back to back, a late block restarts after the lead,
 *     a block that would play too far behind the air is dropped
 *   - start() opens the audio stream of the radio path, stop() closes stream and context
 *   - only PCM frames are played, at the rate from the frame header
 *   - volume goes to the gain node
 */

'use strict';

const assert = require('assert');

global.window = {};
const { AudioPlayer, nextStart, LEAD_S, MAX_LAG_S } = require('./audio-player.js');

let passed = 0;
let failed = 0;
function test(name, fn) {
    try {
        fn();
        passed++;
        console.log('  ✓ ' + name);
    } catch (e) {
        failed++;
        console.log('  ✗ ' + name + '\n    ' + e.message);
    }
}

/** Minimal Web Audio context that records scheduled buffers. */
function fakeContext() {
    const ctx = {
        currentTime: 10,
        closed: false,
        resumed: false,
        started: [],
        destination: {},
        gain: { gain: { value: 1 }, connect() {} },
        createGain() { return ctx.gain; },
        createBuffer(channels, length, rate) {
            const data = new Float32Array(length);
            return { length, sampleRate: rate, duration: length / rate, getChannelData: () => data };
        },
        createBufferSource() {
            const src = { connect() {}, start(t) { ctx.started.push({ t, buffer: src.buffer }); } };
            return src;
        },
        resume() { ctx.resumed = true; return Promise.resolve(); },
        close() { ctx.closed = true; return Promise.resolve(); },
    };
    return ctx;
}

function fakeStreamFactory() {
    const made = [];
    const factory = (opts) => {
        const s = { opts, running: false, start() { s.running = true; }, stop() { s.running = false; } };
        made.push(s);
        return s;
    };
    factory.made = made;
    return factory;
}

function pcmFrame(n, rate) {
    return { header: { dataType: 0x03, spanHz: rate, centerHz: 436550000 }, values: new Float32Array(n).fill(0.25) };
}

console.log('AudioPlayer');

test('nextStart keeps blocks back to back while the queue is within the lag', () => {
    assert.strictEqual(nextStart(10, 10.3, LEAD_S, MAX_LAG_S), 10.3);
});

test('nextStart restarts after the lead when the queue ran dry', () => {
    assert.strictEqual(nextStart(10, 9.5, LEAD_S, MAX_LAG_S), 10 + LEAD_S);
    assert.strictEqual(nextStart(10, 0, LEAD_S, MAX_LAG_S), 10 + LEAD_S);
});

test('nextStart drops a block that would play too far behind the air', () => {
    assert.strictEqual(nextStart(10, 10 + MAX_LAG_S + 0.01, LEAD_S, MAX_LAG_S), -1);
});

test('start opens the audio stream of the radio path and resumes the context', () => {
    const ctx = fakeContext();
    const streams = fakeStreamFactory();
    const p = new AudioPlayer({ makeContext: () => ctx, makeStream: streams });
    p.start('radio path 1');
    assert.strictEqual(streams.made.length, 1);
    assert.strictEqual(streams.made[0].opts.url, '/api/stream/v1/audio?radio_path=radio%20path%201');
    assert.ok(streams.made[0].running);
    assert.ok(ctx.resumed);
    assert.ok(p.isPlaying());
});

test('start on the same path keeps one stream; another path reopens it', () => {
    const streams = fakeStreamFactory();
    const p = new AudioPlayer({ makeContext: fakeContext, makeStream: streams });
    p.start('a');
    p.start('a');
    assert.strictEqual(streams.made.length, 1);
    p.start('b');
    assert.strictEqual(streams.made.length, 2);
    assert.ok(!streams.made[0].running, 'stream of the previous path stopped');
});

test('frames play back to back at the rate from the header', () => {
    const ctx = fakeContext();
    const streams = fakeStreamFactory();
    const p = new AudioPlayer({ makeContext: () => ctx, makeStream: streams });
    p.start('a');
    const onFrame = streams.made[0].opts.onFrame;
    onFrame(pcmFrame(1200, 12000));
    onFrame(pcmFrame(1200, 12000));
    assert.strictEqual(ctx.started.length, 2);
    assert.strictEqual(ctx.started[0].t, 10 + LEAD_S);
    assert.ok(Math.abs(ctx.started[1].t - (10 + LEAD_S + 0.1)) < 1e-9);
    assert.strictEqual(ctx.started[0].buffer.sampleRate, 12000);
    assert.strictEqual(ctx.started[0].buffer.getChannelData(0)[5], 0.25);
});

test('a burst after a network stall is cut at the lag, nothing overlaps', () => {
    const ctx = fakeContext();
    const streams = fakeStreamFactory();
    const p = new AudioPlayer({ makeContext: () => ctx, makeStream: streams });
    p.start('a');
    for (let i = 0; i < 30; i++) { streams.made[0].opts.onFrame(pcmFrame(1200, 12000)); }
    const last = ctx.started[ctx.started.length - 1];
    assert.ok(last.t - ctx.currentTime <= MAX_LAG_S, 'last block starts within the lag');
    for (let i = 1; i < ctx.started.length; i++) {
        assert.ok(ctx.started[i].t >= ctx.started[i - 1].t + 0.1 - 1e-9, 'blocks do not overlap');
    }
});

test('heartbeat and spectrum frames are not played', () => {
    const ctx = fakeContext();
    const streams = fakeStreamFactory();
    const p = new AudioPlayer({ makeContext: () => ctx, makeStream: streams });
    p.start('a');
    streams.made[0].opts.onFrame({ header: { dataType: 0xFE, spanHz: 12000 }, values: null });
    streams.made[0].opts.onFrame({ header: { dataType: 0x01, spanHz: 2048000 }, values: new Float32Array(4) });
    assert.strictEqual(ctx.started.length, 0);
});

test('stop closes the stream and the context', () => {
    const ctx = fakeContext();
    const streams = fakeStreamFactory();
    const p = new AudioPlayer({ makeContext: () => ctx, makeStream: streams });
    p.start('a');
    p.stop();
    assert.ok(!streams.made[0].running);
    assert.ok(ctx.closed);
    assert.ok(!p.isPlaying());
    streams.made[0].opts.onFrame(pcmFrame(100, 12000));
    assert.strictEqual(ctx.started.length, 0, 'a frame after stop is dropped');
});

test('volume goes to the gain node, before and after start', () => {
    const ctx = fakeContext();
    const p = new AudioPlayer({ makeContext: () => ctx, makeStream: fakeStreamFactory() });
    p.setVolume(0.3);
    p.start('a');
    assert.strictEqual(ctx.gain.gain.value, 0.3);
    p.setVolume(2);
    assert.strictEqual(ctx.gain.gain.value, 1, 'volume is clamped to 0..1');
});

console.log('\n' + passed + ' passed, ' + failed + ' failed');
if (failed > 0) { process.exit(1); }
