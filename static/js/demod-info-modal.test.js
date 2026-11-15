/**
 * Unit tests for DemodInfoModal.
 *
 * Run: node static/js/demod-info-modal.test.js
 */

'use strict';

const assert = require('assert');

function makeEl(id) {
    return {
        id: id,
        textContent: '',
        title: '',
        children: [],
        dataset: {},
        hidden: false,
        classList: {
            _set: new Set(),
            toggle(cls, on) { if (on) { this._set.add(cls); } else { this._set.delete(cls); } },
            add(cls) { this._set.add(cls); },
            remove(cls) { this._set.delete(cls); },
            contains(cls) { return this._set.has(cls); },
        },
        appendChild(child) { this.children.push(child); return child; },
        replaceChildren() { this.children = []; },
        remove() {},
        attrs: {},
        setAttribute(name, value) { this.attrs[name] = String(value); },
        getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null; },
        _listeners: [],
        addEventListener(type, fn) { this._listeners.push({ type: type, fn: fn }); },
        removeEventListener() {},
        getContext() { return null; },
        style: {},
        parentElement: null,
    };
}

const ids = ['demod-info-modal', 'demod-info-metrics', 'demod-info-diag',
    'demod-info-demod', 'demod-info-freq', 'demod-info-bw', 'demod-info-gain',
    'demod-info-doppler', 'demod-info-squelch', 'demod-info-level', 'demod-info-snr',
    'demod-info-carrier', 'demod-info-clock', 'demod-info-sync', 'demod-info-frames',
    'demod-info-chart-level', 'demod-info-chart-snr', 'demod-info-chart-carrier',
    'demod-info-chart-jitter', 'demod-info-chart-frames',
    'demod-info-eye-canvas', 'demod-info-eye-tabs', 'demod-info-eye-auto',
    'demod-info-help', 'demod-info-guide'];
const nodes = new Map();
for (const id of ids) {
    nodes.set(id, makeEl(id));
}
// Close buttons inside the modal share data-action.
const closeA = makeEl('demod-info-close-a');
closeA.dataset.action = 'close-demod-info';
const closeB = makeEl('demod-info-close-b');
closeB.dataset.action = 'close-demod-info';
nodes.set('demod-info-close-a', closeA);
nodes.set('demod-info-close-b', closeB);
// Корень модалки возвращает обе кнопки закрытия по data-action.
const rootEl = nodes.get('demod-info-modal');
rootEl.querySelectorAll = (sel) => sel === '[data-action="close-demod-info"]' ? [closeA, closeB] : [];

global.document = {
    getElementById: (id) => nodes.get(id) || null,
    createElement: (tag) => makeEl(tag),
    querySelectorAll: () => [],
    addEventListener() {},
};
global.window = {
    EyeConstellationView: function() { this.activate = () => {}; this.deactivate = () => {}; this.destroy = () => {}; this.setModulation = () => {}; },
    demodLabel: (id) => ({ fsk9600_geoscan: 'FSK 9600 GEOSCAN', afsk1200: 'AFSK 1200' }[id] || id),
};
global.requestAnimationFrame = () => 0;
global.cancelAnimationFrame = () => {};

const { DemodInfoModal, formatInfoHz, formatInfoSignedHz } = require('./demod-info-modal.js');

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

console.log('\n— DemodInfoModal —');

test('formatInfoHz switches to kHz with comma decimal', () => {
    assert.strictEqual(formatInfoHz(0), '0 Гц');
    assert.strictEqual(formatInfoHz(800), '800 Гц');
    assert.strictEqual(formatInfoHz(2400), '2,4 кГц');
    assert.strictEqual(formatInfoHz(18000), '18 кГц');
});

test('formatInfoSignedHz keeps the sign and uses a typographic minus', () => {
    assert.strictEqual(formatInfoSignedHz(0), '0 Гц');
    assert.strictEqual(formatInfoSignedHz(250), '+250 Гц');
    assert.strictEqual(formatInfoSignedHz(-1800), '−1,8 кГц');
});

test('open shows the modal and closes the backdrop', () => {
    const modal = nodes.get('demod-info-modal');
    modal.hidden = true;
    const m = new DemodInfoModal({ now: () => 12000 });
    m.open();
    assert.strictEqual(modal.hidden, false);
    m.close();
    assert.strictEqual(modal.hidden, true);
    m.destroy();
});

test('applyMetrics fills text fields from demod_status', () => {
    const m = new DemodInfoModal({ now: () => 12000 });
    m.applyMetrics({
        demod: 'fsk9600_geoscan', frequency_hz: 436200000, bandwidth_hz: 15000,
        gain_db: 32, squelch_dbfs: -90, level_dbfs: -52.3, squelch_open: true,
        doppler_auto: true, doppler_hz: 1200,
        reception: { snr_db: 18, carrier_hz: -1800, clock_jitter: 0.04, sync: { words: 218, bit_errors: 133 } },
        frames_good: 181, frames_bad: 37,
    }, -32);
    assert.strictEqual(nodes.get('demod-info-demod').textContent, 'FSK 9600 GEOSCAN');
    assert.strictEqual(nodes.get('demod-info-freq').textContent, '436,200 МГц');
    assert.strictEqual(nodes.get('demod-info-bw').textContent, '15 кГц');
    assert.strictEqual(nodes.get('demod-info-gain').textContent, '32 дБ');
    assert.strictEqual(nodes.get('demod-info-snr').textContent, '18 дБ');
    assert.strictEqual(nodes.get('demod-info-frames').textContent, '181 / 37');
    m.destroy();
});

test('applyMetrics handles missing reception and gain', () => {
    const m = new DemodInfoModal({ now: () => 12000 });
    m.applyMetrics({ demod: 'afsk1200', frequency_hz: 145800000, bandwidth_hz: 15000,
        squelch_dbfs: -90, level_dbfs: -60, squelch_open: false, frames_good: 0, frames_bad: 0 }, null);
    assert.strictEqual(nodes.get('demod-info-snr').textContent, '—');
    assert.strictEqual(nodes.get('demod-info-gain').textContent, '—');
    assert.strictEqual(nodes.get('demod-info-carrier').textContent, '—');
    m.destroy();
});

test('update keeps the eye diagram idle while the demodulator is off', () => {
    const calls = [];
    const saved = global.window.EyeConstellationView;
    global.window.EyeConstellationView = function() {
        this.activate = () => calls.push('activate');
        this.deactivate = () => calls.push('deactivate');
        this.isActive = () => false;
        this.setModulation = () => {};
        this.destroy = () => {};
    };
    const m = new DemodInfoModal({ now: () => 12000 });
    m.open();
    m.update({
        demod: 'afsk1200', demod_off: true, active: true, squelch_open: false,
        level_dbfs: 0, squelch_dbfs: -90, frames_good: 0, frames_bad: 0,
    }, null, null, { state: 'idle', text: 'Демодулятор выключен' }, 'afsk1200');
    assert.ok(!calls.includes('activate'), 'eye must not draw while the demodulator is off');
    m.destroy();
    global.window.EyeConstellationView = saved;
});

test('help button toggles the reading guide and opening the modal collapses it', () => {
    const btn = nodes.get('demod-info-help');
    const guide = nodes.get('demod-info-guide');
    const m = new DemodInfoModal({ now: () => 12000 });
    assert.strictEqual(guide.hidden, true);
    assert.strictEqual(btn.getAttribute('aria-expanded'), 'false');
    const clicks = btn._listeners.filter(function(l) { return l.type === 'click'; });
    clicks[clicks.length - 1].fn();
    assert.strictEqual(guide.hidden, false);
    assert.strictEqual(btn.getAttribute('aria-expanded'), 'true');
    m.open();
    assert.strictEqual(guide.hidden, true);
    assert.strictEqual(btn.getAttribute('aria-expanded'), 'false');
    m.destroy();
});

test('applyDiagnosis sets the diagnosis text and state class', () => {
    const m = new DemodInfoModal({ now: () => 12000 });
    m.applyDiagnosis({ state: 'bad', text: 'Нет захвата тактов' });
    assert.strictEqual(nodes.get('demod-info-diag').textContent, 'Нет захвата тактов');
    assert.ok(nodes.get('demod-info-diag').classList._set.has('demod-info__diag--bad'));
    m.destroy();
});

console.log(`\n${passed} ${failed > 0 ? 'failed' : 'passed'}\n`);
process.exit(failed > 0 ? 1 : 0);
