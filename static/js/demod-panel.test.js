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

const { DemodPanel, formatFreqMHz, metricsFromTx } = require('./demod-panel.js');

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

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed > 0 ? 1 : 0);
