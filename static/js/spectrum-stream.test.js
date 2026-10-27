/**
 * Unit tests for SpectrumStream.
 *
 * Run: node static/js/spectrum-stream.test.js
 *
 * Covers:
 *   - SSTM header parsing (little-endian, 64-bit center/span)
 *   - frame reader: frames split across chunks, several frames in one chunk,
 *     heartbeat without payload, bad magic
 *   - dBFS to 0..1 scale for the canvas views
 *   - peak marker readout: frequency, level, noise floor, SNR
 *   - one long-lived request: frames delivered as chunks arrive, stop() aborts,
 *     end of stream and HTTP errors reconnect with growing delay
 */

'use strict';

const assert = require('assert');

global.window = {};
const {
    SpectrumStream, FrameReader, parseHeader, dbfsToUnit, AutoRange, DisplaySmooth,
    markerReadout, dbmOffset, shiftLevels,
} = require('./spectrum-stream.js');

const HEADER = 40;
const MAGIC = 0x4D545353;

/** Builds one SSTM frame the way internal/dsp.EncodeHeader does. */
function makeFrame(opts) {
    const values = opts.values || [];
    const buf = new ArrayBuffer(HEADER + values.length * 4);
    const dv = new DataView(buf);
    dv.setUint32(0, opts.magic === undefined ? MAGIC : opts.magic, true);
    dv.setUint8(4, 1);
    dv.setUint8(5, opts.type === undefined ? 0x01 : opts.type);
    dv.setUint8(6, opts.mode || 0);
    const ts = opts.ts || 0;
    dv.setUint32(8, ts % 0x100000000, true);
    dv.setUint32(12, Math.floor(ts / 0x100000000), true);
    const center = opts.centerHz || 0;
    dv.setUint32(16, center % 0x100000000, true);
    dv.setUint32(20, Math.floor(center / 0x100000000), true);
    const span = opts.spanHz || 0;
    dv.setUint32(24, span % 0x100000000, true);
    dv.setUint32(28, Math.floor(span / 0x100000000), true);
    dv.setUint32(32, values.length * 4, true);
    dv.setUint32(36, opts.seq || 0, true);
    for (let i = 0; i < values.length; i++) {
        dv.setFloat32(HEADER + i * 4, values[i], true);
    }
    return new Uint8Array(buf);
}

function concat(parts) {
    const total = parts.reduce((n, p) => n + p.length, 0);
    const out = new Uint8Array(total);
    let off = 0;
    for (const p of parts) { out.set(p, off); off += p.length; }
    return out;
}

let passed = 0;
let failed = 0;
const queue = [];
function test(name, fn) { queue.push([name, fn]); }

// ── dBm at the antenna input ──────────────────────────────────────────

test('dbmOffset is calibration minus gain', () => {
    assert.strictEqual(dbmOffset(-40, 0), -40);
    assert.strictEqual(dbmOffset(-40, 20), -60);
    assert.strictEqual(dbmOffset(0, 10.5), -10.5);
});

test('dbmOffset gives null when dBm cannot be shown: no calibration, unknown gain, AGC', () => {
    assert.strictEqual(dbmOffset(undefined, 0), null);
    assert.strictEqual(dbmOffset(null, 0), null);
    assert.strictEqual(dbmOffset(-40, undefined), null);
    assert.strictEqual(dbmOffset(-40, -1), null, 'negative gain is AGC');
    assert.strictEqual(dbmOffset(NaN, 0), null);
});

test('shiftLevels adds the offset to every bin and keeps the source intact', () => {
    const src = new Float32Array([-100, -50.5, 0]);
    const out = shiftLevels(src, -40);
    assert.deepStrictEqual(Array.from(out), [-140, -90.5, -40]);
    assert.deepStrictEqual(Array.from(src), [-100, -50.5, 0]);
});

// ── Header and frame reader ───────────────────────────────────────────

test('parseHeader reads all fields, center above 2^32 Hz', () => {
    const f = makeFrame({ type: 0x01, mode: 2, ts: 1760000000123, centerHz: 5760000000, spanHz: 2048000, seq: 7, values: [1, 2] });
    const h = parseHeader(new DataView(f.buffer), 0);
    assert.strictEqual(h.magic, MAGIC);
    assert.strictEqual(h.dataType, 0x01);
    assert.strictEqual(h.displayMode, 2);
    assert.strictEqual(h.timestampMs, 1760000000123);
    assert.strictEqual(h.centerHz, 5760000000);
    assert.strictEqual(h.spanHz, 2048000);
    assert.strictEqual(h.payloadLen, 8);
    assert.strictEqual(h.seq, 7);
});

test('frame split across chunks is assembled once complete', () => {
    const f = makeFrame({ centerHz: 436000000, spanHz: 2048000, values: [-80, -20.5, -60] });
    const r = new FrameReader();
    assert.deepStrictEqual(r.push(f.subarray(0, 10)), []);
    assert.deepStrictEqual(r.push(f.subarray(10, 45)), []);
    const frames = r.push(f.subarray(45));
    assert.strictEqual(frames.length, 1);
    assert.strictEqual(frames[0].header.centerHz, 436000000);
    assert.deepStrictEqual(Array.from(frames[0].values), [-80, -20.5, -60]);
});

test('several frames in one chunk, heartbeat in between', () => {
    const a = makeFrame({ seq: 1, values: [-1] });
    const hb = makeFrame({ type: 0xFE, seq: 2 });
    const b = makeFrame({ seq: 3, values: [-2, -3] });
    const r = new FrameReader();
    const frames = r.push(concat([a, hb, b]));
    assert.deepStrictEqual(frames.map((x) => x.header.seq), [1, 2, 3]);
    assert.strictEqual(frames[1].values, null, 'heartbeat carries no spectrum');
    assert.deepStrictEqual(Array.from(frames[2].values), [-2, -3]);
});

test('reader keeps working after a frame larger than its initial buffer', () => {
    const big = makeFrame({ seq: 1, values: new Array(20000).fill(-70) });
    const next = makeFrame({ seq: 2, values: [-5] });
    const r = new FrameReader();
    const frames = r.push(big).concat(r.push(next));
    assert.strictEqual(frames.length, 2);
    assert.strictEqual(frames[0].values.length, 20000);
    assert.deepStrictEqual(Array.from(frames[1].values), [-5]);
});

test('bad magic throws: the stream is out of sync', () => {
    const r = new FrameReader();
    assert.throws(() => r.push(makeFrame({ magic: 0x12345678, values: [1] })), /magic/);
});

test('dbfsToUnit maps dBFS onto the 0..1 view scale and clamps', () => {
    const out = dbfsToUnit(new Float32Array([-100, -50, 0, -130, 12]), -100, 0);
    assert.deepStrictEqual(Array.from(out), [0, 0.5, 1, 0, 1]);
});

// ── Auto range ────────────────────────────────────────────────────────

/** Spectrum: noise floor everywhere, optional peaks {bin: dBFS}. */
function noiseFrame(floorDb, peaks, bins) {
    const v = new Float32Array(bins || 1024).fill(floorDb);
    Object.keys(peaks || {}).forEach((k) => { v[Number(k)] = peaks[k]; });
    return v;
}

test('AutoRange: noise floor sits near the bottom, 40 dB of room above it', () => {
    const r = new AutoRange().update(noiseFrame(-69));
    assert.deepStrictEqual(r, { min: -80, max: -25 });
});

test('AutoRange: a strong peak raises the top at once', () => {
    const ar = new AutoRange();
    ar.update(noiseFrame(-69));
    assert.deepStrictEqual(ar.update(noiseFrame(-69, { 512: -11 })), { min: -80, max: -5 });
});

test('AutoRange: after the peak is gone the top comes back slowly', () => {
    const ar = new AutoRange();
    ar.update(noiseFrame(-69, { 512: -11 }));
    assert.strictEqual(ar.update(noiseFrame(-69)).max, -5, 'one quiet frame must not drop the top');
    let r;
    for (let i = 0; i < 200; i++) { r = ar.update(noiseFrame(-69)); }
    assert.deepStrictEqual(r, { min: -80, max: -25 });
});

test('AutoRange: noise jitter of a few dB keeps the scale still', () => {
    const ar = new AutoRange();
    ar.update(noiseFrame(-69));
    for (let i = 0; i < 100; i++) {
        assert.deepStrictEqual(ar.update(noiseFrame(i % 2 ? -66 : -72)), { min: -80, max: -25 });
    }
});

test('AutoRange: the scale follows a lasting change of the noise floor', () => {
    const ar = new AutoRange();
    ar.update(noiseFrame(-69));
    let r;
    for (let i = 0; i < 60; i++) { r = ar.update(noiseFrame(-49)); }
    assert.deepStrictEqual(r, { min: -60, max: -5 });
});

test('AutoRange: the top also fits the held trace, the floor stays on the live frame', () => {
    const held = noiseFrame(-40, { 512: -11 });
    assert.deepStrictEqual(new AutoRange().update(noiseFrame(-69), held), { min: -80, max: -5 },
        'a held peak above the live one lifts the top, the held noise does not lift the bottom');
});

test('AutoRange: floor is the median, so a band full of carriers below half does not lift it', () => {
    const peaks = {};
    for (let i = 0; i < 400; i++) { peaks[i] = -20; }
    assert.strictEqual(new AutoRange().update(noiseFrame(-69, peaks)).min, -80);
});

// ── Display smooth (client EMA, not server avg) ───────────────────────

test('DisplaySmooth: first frame is a copy, next blends by alpha', () => {
    const s = new DisplaySmooth(0.5);
    const a = s.update(new Float32Array([-100, -20]));
    assert.deepStrictEqual(Array.from(a), [-100, -20]);
    const b = s.update(new Float32Array([-80, -40]));
    assert.deepStrictEqual(Array.from(b), [-90, -30]);
});

test('DisplaySmooth: reset starts a fresh trace; length change does too', () => {
    const s = new DisplaySmooth(0.5);
    s.update(new Float32Array([-100, -20]));
    s.reset();
    assert.deepStrictEqual(Array.from(s.update(new Float32Array([-50, -50]))), [-50, -50]);
    assert.deepStrictEqual(Array.from(s.update(new Float32Array([-10, -10, -10]))), [-10, -10, -10]);
});

// ── Peak marker ───────────────────────────────────────────────────────

test('markerReadout: peak frequency, level, noise floor and SNR', () => {
    // 1024 bins over 2.048 MHz around 436.5 MHz: bin 768 is 2 kHz * 256 above center
    const m = markerReadout(noiseFrame(-90, { 768: -49.3 }), 436500000, 2048000);
    assert.strictEqual(m.freqHz, 437012000);
    assert.ok(Math.abs(m.dbfs + 49.3) < 1e-4, String(m.dbfs));
    assert.strictEqual(m.floorDbfs, -90);
    assert.ok(Math.abs(m.snrDb - 40.7) < 1e-4, String(m.snrDb));
});

test('markerReadout: frequency matches the bin the canvas marks, from the left edge', () => {
    assert.strictEqual(markerReadout(noiseFrame(-90, { 0: -10 }), 436500000, 2048000).freqHz, 435476000);
});

test('markerReadout: no data gives null', () => {
    assert.strictEqual(markerReadout(new Float32Array(0), 436500000, 2048000), null);
    assert.strictEqual(markerReadout(null, 436500000, 2048000), null);
});

// ── Long-lived request ────────────────────────────────────────────────

/** fetch double: every call returns a body the test feeds chunk by chunk. */
function makeFetch() {
    const calls = [];
    function fetchImpl(url, init) {
        const call = { url, init, chunks: [], waiters: [], done: false, aborted: false };
        calls.push(call);
        if (init && init.signal) {
            init.signal.addEventListener('abort', () => {
                call.aborted = true;
                call.waiters.splice(0).forEach((w) => w.reject(Object.assign(new Error('aborted'), { name: 'AbortError' })));
            });
        }
        if (fetchImpl.nextStatus) {
            const status = fetchImpl.nextStatus;
            fetchImpl.nextStatus = 0;
            return Promise.resolve({ ok: false, status, text: () => Promise.resolve('receiver not available') });
        }
        const reader = {
            read() {
                if (call.chunks.length) { return Promise.resolve({ done: false, value: call.chunks.shift() }); }
                if (call.done) { return Promise.resolve({ done: true }); }
                return new Promise((resolve, reject) => call.waiters.push({ resolve, reject }));
            },
            cancel() { return Promise.resolve(); },
        };
        call.send = (chunk) => {
            const w = call.waiters.shift();
            if (w) { w.resolve({ done: false, value: chunk }); } else { call.chunks.push(chunk); }
        };
        call.end = () => {
            call.done = true;
            const w = call.waiters.shift();
            if (w) { w.resolve({ done: true }); }
        };
        return Promise.resolve({ ok: true, status: 200, body: { getReader: () => reader } });
    }
    fetchImpl.calls = calls;
    return fetchImpl;
}

/** Timer double: the test fires scheduled reconnects by hand. */
function makeTimers() {
    const pending = [];
    return {
        pending,
        setTimeout(fn, ms) { pending.push({ fn, ms }); return pending.length; },
        clearTimeout(id) { if (pending[id - 1]) { pending[id - 1].fn = null; } },
        fire() { const t = pending.shift(); if (t && t.fn) { t.fn(); } },
    };
}

const tick = () => new Promise((r) => setImmediate(r));

test('frames are delivered as chunks arrive over one request', async () => {
    const fetchImpl = makeFetch();
    const got = [];
    const statuses = [];
    const s = new SpectrumStream({
        url: '/api/stream/v1/spectrum?radio_path=radio_path_1',
        fetchImpl,
        timers: makeTimers(),
        onFrame: (f) => got.push(f.header.seq),
        onStatus: (st) => statuses.push(st.state),
    });
    s.start();
    await tick();
    const call = fetchImpl.calls[0];
    call.send(makeFrame({ seq: 1, values: [-50] }));
    await tick();
    call.send(makeFrame({ seq: 2, values: [-50] }));
    await tick();
    assert.deepStrictEqual(got, [1, 2]);
    assert.strictEqual(fetchImpl.calls.length, 1, 'one request for the whole session');
    assert.deepStrictEqual(statuses, ['connecting', 'live']);
    s.stop();
});

test('stop() aborts the request and does not reconnect', async () => {
    const fetchImpl = makeFetch();
    const timers = makeTimers();
    const statuses = [];
    const s = new SpectrumStream({ url: '/x', fetchImpl, timers, onFrame: () => {}, onStatus: (st) => statuses.push(st.state) });
    s.start();
    await tick();
    s.stop();
    await tick();
    assert.strictEqual(fetchImpl.calls[0].aborted, true);
    assert.strictEqual(timers.pending.length, 0, 'no reconnect after stop');
    assert.strictEqual(statuses[statuses.length - 1], 'stopped');
    assert.strictEqual(s.isRunning(), false);
});

test('end of stream reconnects with a growing delay, a frame resets it', async () => {
    const fetchImpl = makeFetch();
    const timers = makeTimers();
    const s = new SpectrumStream({ url: '/x', fetchImpl, timers, onFrame: () => {}, onStatus: () => {} });
    s.start();
    await tick();
    fetchImpl.calls[0].end();
    await tick();
    assert.strictEqual(timers.pending.length, 1);
    assert.strictEqual(timers.pending[0].ms, 1000);
    timers.fire();
    await tick();
    fetchImpl.calls[1].end();
    await tick();
    assert.strictEqual(timers.pending[0].ms, 2000, 'delay doubles');
    timers.fire();
    await tick();
    fetchImpl.calls[2].send(makeFrame({ values: [-40] }));
    await tick();
    fetchImpl.calls[2].end();
    await tick();
    assert.strictEqual(timers.pending[0].ms, 1000, 'a frame proves the link, delay resets');
    s.stop();
});

test('HTTP error is reported with the server text and retried', async () => {
    const fetchImpl = makeFetch();
    fetchImpl.nextStatus = 400;
    const timers = makeTimers();
    const statuses = [];
    const s = new SpectrumStream({ url: '/x', fetchImpl, timers, onFrame: () => {}, onStatus: (st) => statuses.push(st) });
    s.start();
    await tick();
    await tick();
    const err = statuses.find((st) => st.state === 'error');
    assert.ok(err, 'error status expected');
    assert.ok(/400/.test(err.message) && /receiver not available/.test(err.message), err.message);
    assert.strictEqual(timers.pending.length, 1, 'retry scheduled');
    s.stop();
});

test('out-of-sync stream drops the connection and reconnects', async () => {
    const fetchImpl = makeFetch();
    const timers = makeTimers();
    const statuses = [];
    const s = new SpectrumStream({ url: '/x', fetchImpl, timers, onFrame: () => {}, onStatus: (st) => statuses.push(st.state) });
    s.start();
    await tick();
    fetchImpl.calls[0].send(makeFrame({ magic: 1, values: [1] }));
    await tick();
    await tick();
    assert.strictEqual(fetchImpl.calls[0].aborted, true);
    assert.ok(statuses.includes('error'));
    assert.strictEqual(timers.pending.length, 1);
    s.stop();
});

test('start() twice keeps one request', async () => {
    const fetchImpl = makeFetch();
    const s = new SpectrumStream({ url: '/x', fetchImpl, timers: makeTimers(), onFrame: () => {}, onStatus: () => {} });
    s.start();
    s.start();
    await tick();
    assert.strictEqual(fetchImpl.calls.length, 1);
    s.stop();
});

(async () => {
    for (const [name, fn] of queue) {
        try {
            await fn();
            passed++;
            console.log('  ✓ ' + name);
        } catch (err) {
            failed++;
            console.log('  ✗ ' + name);
            console.log('    ' + (err && err.stack ? err.stack : err));
        }
    }
    console.log(`\n${passed} passed, ${failed} failed`);
    if (failed > 0) { process.exit(1); }
})();
