/**
 * Unit tests for DemodPanel.
 *
 * Run: node static/js/demod-panel.test.js
 */

'use strict';

const assert = require('assert');

function makeTextEl(id) {
    return {
        id: id,
        textContent: '',
        classList: {
            _set: new Set(),
            toggle(cls, on) {
                if (on) { this._set.add(cls); } else { this._set.delete(cls); }
            },
        },
        title: '',
    };
}

const ids = [
    'manual-ch-lock', 'manual-ch-name', 'manual-ch-freq', 'manual-ch-pipeline',
    'manual-dm-lock', 'manual-dm-snr', 'manual-dm-cn0', 'manual-dm-esn0',
    'manual-dm-pipeline', 'manual-dm-ber', 'manual-dm-rssi', 'manual-dm-afc',
    'manual-dm-foff', 'manual-dm-fec', 'manual-dm-squelch',
    'manual-dm-rate', 'manual-dm-frames', 'manual-dm-sync', 'manual-dm-crc', 'manual-dm-last',
];
const nodes = new Map();
for (const id of ids) {
    nodes.set(id, makeTextEl(id));
}

global.document = {
    getElementById: (id) => nodes.get(id) || null,
};
global.window = {};

const { DemodPanel, formatFreqMHz, metricsFromTx, metricsFromStatus, demodLabel } = require('./demod-panel.js');

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

console.log('\n— DemodPanel —');

test('formatFreqMHz uses comma decimal', () => {
    assert.strictEqual(formatFreqMHz(437.8), '437,800');
});

test('metricsFromTx returns idle metrics for silent tx', () => {
    const m = metricsFromTx({ modulation: 'FSK 1k2', active: false });
    assert.strictEqual(m.lock, false);
    assert.strictEqual(m.frames, '0');
    assert.strictEqual(m.snr, '—');
});

test('setChannel updates header and metrics for active tx', () => {
    const panel = new DemodPanel();
    panel.setChannel({
        freqMHz: 435.25,
        label: 'AO-91 BPSK',
        modulation: 'BPSK31',
        active: true,
    });
    assert.strictEqual(nodes.get('manual-ch-name').textContent, 'AO-91 BPSK');
    assert.strictEqual(nodes.get('manual-ch-freq').textContent, '435,250');
    assert.strictEqual(nodes.get('manual-dm-lock').textContent, '●');
    assert.strictEqual(panel.getChannel().freqMHz, 435.25);
    panel.destroy();
});

test('setChannel shows idle state for silent tx', () => {
    const panel = new DemodPanel();
    panel.setChannel({
        freqMHz: 145.92,
        label: 'RS-44 CW',
        modulation: 'CW',
        active: false,
    });
    assert.strictEqual(nodes.get('manual-dm-lock').textContent, '○');
    assert.strictEqual(nodes.get('manual-dm-snr').textContent, '—');
    assert.strictEqual(nodes.get('manual-dm-cn0').textContent, '—');
    assert.strictEqual(nodes.get('manual-dm-squelch').textContent, 'Closed');
    panel.destroy();
});

test('setChannel populates extended metrics for active tx', () => {
    const panel = new DemodPanel();
    panel.setChannel({
        freqMHz: 437.8,
        label: 'ISS-FSK',
        modulation: 'FSK 9600',
        active: true,
    });
    assert.strictEqual(nodes.get('manual-dm-cn0').textContent, '52.3 dB·Hz');
    assert.strictEqual(nodes.get('manual-dm-esn0').textContent, '8.4 dB');
    assert.strictEqual(nodes.get('manual-dm-foff').textContent, '+1.2 kHz');
    assert.strictEqual(nodes.get('manual-dm-fec').textContent, '12 B');
    assert.ok(nodes.get('manual-dm-squelch').classList._set.has('ml-demod__value--ok'));
    assert.strictEqual(nodes.get('manual-dm-sync').textContent, '131');
    assert.strictEqual(nodes.get('manual-dm-last').textContent, '0.8 s');
    panel.destroy();
});

const status = {
    radio_path: 'radio_path_1',
    active: true,
    demod: 'afsk1200',
    supported: true,
    frequency_hz: 145825000,
    level_dbfs: -40.04,
    squelch_dbfs: -90,
    squelch_open: true,
    frames_good: 3,
    frames_bad: 1,
    last_packet_ms: 10000,
};

test('metricsFromStatus formats live demodulator state in dBFS', () => {
    const m = metricsFromStatus(status, null, 12500);
    assert.strictEqual(m.lock, true);
    assert.strictEqual(m.pipeline, 'AFSK 1200');
    assert.strictEqual(m.rate, '1.2 ksym/s');
    assert.strictEqual(m.rssi, '−40.0 dBFS');
    assert.strictEqual(m.squelch, 'Open / −90.0 dBFS');
    assert.strictEqual(m.squelchClosed, false);
    assert.strictEqual(m.frames, '3');
    assert.strictEqual(m.sync, '4');
    assert.strictEqual(m.last, '2.5 s');
    assert.strictEqual(m.snr, '—');
});

test('metricsFromStatus uses dBm with calibration and idles when stopped', () => {
    const m = metricsFromStatus(Object.assign({}, status, { active: false, squelch_open: false }), -32, 12500);
    assert.strictEqual(m.lock, false);
    assert.strictEqual(m.rssi, '−72 dBm');
    assert.strictEqual(m.squelch, 'Closed / −122 dBm');
    assert.strictEqual(m.squelchClosed, true);
});

test('metricsFromStatus labels FSK 9600 G3RUH', () => {
    const m = metricsFromStatus(Object.assign({}, status, { demod: 'fsk9600_g3ruh' }), null, 0);
    assert.strictEqual(m.pipeline, 'FSK 9600 G3RUH');
    assert.strictEqual(m.rate, '9.6 ksym/s');
});

test('metricsFromStatus labels FSK 9600 GEOSCAN', () => {
    const m = metricsFromStatus(Object.assign({}, status, { demod: 'fsk9600_geoscan' }), null, 0);
    assert.strictEqual(m.pipeline, 'FSK 9600 GEOSCAN');
    assert.strictEqual(m.rate, '9.6 ksym/s');
});

test('demodLabel names known demodulators and returns the id for others', () => {
    assert.strictEqual(demodLabel('afsk1200'), 'AFSK 1200');
    assert.strictEqual(demodLabel('fsk9600_geoscan'), 'FSK 9600 GEOSCAN');
    assert.strictEqual(demodLabel('gmsk9600'), 'gmsk9600');
});

test('metricsFromStatus marks a demodulator that is not implemented', () => {
    const m = metricsFromStatus(Object.assign({}, status, { demod: 'gmsk9600', supported: false }), null, 0);
    assert.strictEqual(m.lock, false);
    assert.strictEqual(m.pipeline, 'gmsk9600 — не реализован');
});

test('applyStatus shows CRC of the latest frame from counter growth', () => {
    const panel = new DemodPanel();
    panel.setLive(true);
    panel.applyStatus(Object.assign({}, status, { frames_good: 0, frames_bad: 0, last_packet_ms: 0 }), null);
    assert.strictEqual(nodes.get('manual-dm-crc').textContent, '—');
    panel.applyStatus(Object.assign({}, status, { frames_good: 1, frames_bad: 0 }), null);
    assert.strictEqual(nodes.get('manual-dm-crc').textContent, 'OK');
    panel.applyStatus(Object.assign({}, status, { frames_good: 1, frames_bad: 1 }), null);
    assert.strictEqual(nodes.get('manual-dm-crc').textContent, 'FAIL');
    panel.applyStatus(Object.assign({}, status, { frames_good: 1, frames_bad: 1 }), null);
    assert.strictEqual(nodes.get('manual-dm-crc').textContent, 'FAIL', 'unchanged counters keep the last result');
    // A new stream session restarts the counters.
    panel.applyStatus(Object.assign({}, status, { frames_good: 0, frames_bad: 0 }), null);
    assert.strictEqual(nodes.get('manual-dm-crc').textContent, '—');
    panel.destroy();
});

test('live mode turns off the mock drift and keeps live metrics on channel change', () => {
    const panel = new DemodPanel();
    panel.setChannel({ freqMHz: 437.8, label: 'ISS-FSK', modulation: 'FSK 9600', active: true });
    assert.ok(panel._mockTimer, 'mock drift runs without a radio path');
    panel.setLive(true);
    assert.strictEqual(panel._mockTimer, null);
    assert.strictEqual(nodes.get('manual-dm-snr').textContent, '—', 'live mode drops mock values');

    panel.applyStatus(status, null);
    panel.setChannel({ freqMHz: 145.825, label: 'SCOUT', modulation: 'AFSK 1200', active: true });
    assert.strictEqual(panel._mockTimer, null);
    assert.strictEqual(nodes.get('manual-ch-name').textContent, 'SCOUT');
    assert.strictEqual(nodes.get('manual-dm-rssi').textContent, '−40.0 dBFS', 'channel change keeps live metrics');
    panel.destroy();
});

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed > 0 ? 1 : 0);
