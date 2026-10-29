/**
 * Unit tests for DemodPanel.
 *
 * Run: node static/js/demod-panel.test.js
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
        classList: {
            _set: new Set(),
            toggle(cls, on) {
                if (on) { this._set.add(cls); } else { this._set.delete(cls); }
            },
            add(cls) { this._set.add(cls); },
            remove(cls) { this._set.delete(cls); },
            contains(cls) { return this._set.has(cls); },
        },
        appendChild(child) { this.children.push(child); return child; },
        replaceChildren() { this.children = []; },
        style: {},
    };
}

const STAGES = ['signal', 'carrier', 'clock', 'sync', 'frames'];
const ids = ['manual-ch-lock', 'manual-ch-name', 'manual-ch-freq', 'manual-ch-pipeline',
    'manual-dm-diag', 'manual-dm-strip', 'manual-dm-strip-box'];
for (const k of STAGES) {
    ids.push('manual-st-' + k, 'manual-st-' + k + '-name', 'manual-st-' + k + '-val', 'manual-st-' + k + '-sub');
}
const nodes = new Map();
for (const id of ids) {
    nodes.set(id, makeEl(id));
}

global.document = {
    getElementById: (id) => nodes.get(id) || null,
    createElement: (tag) => makeEl(tag),
};
global.window = {};

const {
    DemodPanel, formatFreqMHz, formatHz, demodLabel, stagesFromStatus, diagnose, FrameStrip,
} = require('./demod-panel.js');

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

function stageState(key) {
    const cls = nodes.get('manual-st-' + key).classList._set;
    return ['ok', 'warn', 'bad', 'idle'].find((s) => cls.has('ml-stage--' + s));
}

console.log('\n— DemodPanel —');

test('formatFreqMHz uses comma decimal', () => {
    assert.strictEqual(formatFreqMHz(437.8), '437,800');
});

test('formatHz switches to kHz and keeps the sign', () => {
    assert.strictEqual(formatHz(-1800, true), '−1,8 кГц');
    assert.strictEqual(formatHz(250, true), '+250 Гц');
    assert.strictEqual(formatHz(0, true), '0 Гц');
    assert.strictEqual(formatHz(2400, false), '2,4 кГц');
});

test('demodLabel names known demodulators and returns the id for others', () => {
    assert.strictEqual(demodLabel('afsk1200'), 'AFSK 1200');
    assert.strictEqual(demodLabel('fsk9600_geoscan'), 'FSK 9600 GEOSCAN');
    assert.strictEqual(demodLabel('gmsk9600'), 'gmsk9600');
});

const status = {
    radio_path: 'radio_path_1',
    active: true,
    demod: 'fsk9600_geoscan',
    supported: true,
    frequency_hz: 436200000,
    level_dbfs: -52.3,
    squelch_dbfs: -90,
    squelch_open: true,
    frames_good: 181,
    frames_bad: 37,
    last_packet_ms: 10000,
    bandwidth_hz: 20000,
    reception: {
        carrier_hz: -1800,
        deviation_hz: 2400,
        clock_jitter: 0.04,
        sync: { words: 218, bit_errors: 133 },
    },
};

function withStatus(patch) {
    return Object.assign({}, status, patch);
}

test('stagesFromStatus fills every stage of a good reception', () => {
    const s = stagesFromStatus(status, null);
    assert.deepStrictEqual(s.signal, { state: 'ok', value: '−52.3 dBFS', sub: 'порог −90.0 dBFS' });
    assert.deepStrictEqual(s.carrier, { state: 'ok', value: '−1,8 кГц', sub: 'девиация 2,4 кГц' });
    assert.deepStrictEqual(s.clock, { state: 'ok', value: 'захват', sub: 'джиттер 4 %' });
    // 133 bad bits in 218 sync words of 32 bits.
    assert.deepStrictEqual(s.sync, { state: 'ok', value: '218', sub: 'BER 1,9e-2' });
    assert.deepStrictEqual(s.frames, { state: 'ok', value: '181 / 37', sub: 'CRC верна / нет' });
});

test('stagesFromStatus shows dBm with calibration', () => {
    const s = stagesFromStatus(status, -32);
    assert.strictEqual(s.signal.value, '−84 dBm');
    assert.strictEqual(s.signal.sub, 'порог −122 dBm');
});

test('carrier beyond a quarter of the channel warns', () => {
    const s = stagesFromStatus(withStatus({ reception: Object.assign({}, status.reception, { carrier_hz: 5200 }) }), null);
    assert.strictEqual(s.carrier.state, 'warn');
    assert.strictEqual(s.carrier.value, '+5,2 кГц');
});

test('clock without lock is bad', () => {
    const s = stagesFromStatus(withStatus({ reception: Object.assign({}, status.reception, { clock_jitter: 0.24 }) }), null);
    assert.deepStrictEqual(s.clock, { state: 'bad', value: 'нет захвата', sub: 'джиттер 24 %' });
});

test('HDLC profile has no sync word stage and no deviation', () => {
    const s = stagesFromStatus(withStatus({
        demod: 'afsk1200', reception: { carrier_hz: 120, clock_jitter: 0.02 },
    }), null);
    assert.deepStrictEqual(s.sync, { state: 'idle', value: 'HDLC', sub: 'флаги, без синхрослова' });
    assert.strictEqual(s.carrier.sub, '');
});

test('before any signal the measured stages are idle', () => {
    const s = stagesFromStatus(withStatus({
        squelch_open: false, frames_good: 0, frames_bad: 0, last_packet_ms: 0, reception: undefined,
    }), null);
    assert.strictEqual(s.signal.state, 'idle');
    for (const k of ['carrier', 'clock', 'sync', 'frames']) {
        assert.strictEqual(s[k].state, 'idle', k);
        assert.strictEqual(s[k].value, '—', k);
    }
});

test('closed squelch keeps the last measures dimmed', () => {
    const s = stagesFromStatus(withStatus({ squelch_open: false }), null);
    assert.strictEqual(s.carrier.state, 'idle');
    assert.strictEqual(s.carrier.value, '−1,8 кГц');
    assert.strictEqual(s.frames.state, 'ok', 'received frames stay counted');
});

test('demod off clears the reception stages', () => {
    const s = stagesFromStatus(withStatus({ demod_off: true, squelch_open: true }), null);
    assert.strictEqual(s.signal.state, 'idle');
    assert.strictEqual(s.signal.value, '—');
    assert.strictEqual(s.frames.state, 'idle');
});

test('frames without a good CRC are bad', () => {
    const s = stagesFromStatus(withStatus({ frames_good: 0, frames_bad: 4 }), null);
    assert.strictEqual(s.frames.state, 'bad');
});

test('diagnose names the first stage that fails', () => {
    const d = (patch) => diagnose(withStatus(patch), stagesFromStatus(withStatus(patch), null), 12000);
    assert.deepStrictEqual(d({ active: false }), { state: 'idle', text: 'Поток приёмника остановлен' });
    assert.deepStrictEqual(d({ demod_off: true, squelch_open: true }),
        { state: 'idle', text: 'Демодулятор выключен' });
    assert.deepStrictEqual(d({ supported: false, demod: 'gmsk9600' }),
        { state: 'bad', text: 'Демодулятор gmsk9600 не реализован' });
    assert.strictEqual(d({ squelch_open: false, frames_good: 0, frames_bad: 0, reception: undefined }).text,
        'Нет сигнала выше порога');
    assert.strictEqual(d({ reception: Object.assign({}, status.reception, { carrier_hz: 5200 }) }).text,
        'Несущая ушла на +5,2 кГц от центра канала: поправьте сдвиг');
    assert.strictEqual(d({ reception: Object.assign({}, status.reception, { clock_jitter: 0.3 }) }).text,
        'Нет захвата тактов: проверьте профиль и полосу');
    assert.strictEqual(d({ frames_good: 0, frames_bad: 0,
        reception: Object.assign({}, status.reception, { sync: { words: 0, bit_errors: 0 } }) }).text,
    'Такты есть, синхрослово не найдено');
    assert.deepStrictEqual(d({ frames_good: 0, frames_bad: 5 }),
        { state: 'bad', text: 'Синхрослова есть, CRC не сходится' });
    assert.deepStrictEqual(d({}), { state: 'ok', text: 'Приём идёт: 181 кадр, последний 2 с назад' });
    assert.strictEqual(d({ squelch_open: false }).text, 'Сигнала нет; кадров принято: 181, последний 2 с назад');
    assert.strictEqual(d({ demod: 'afsk1200', frames_good: 0, frames_bad: 2,
        reception: { carrier_hz: 0, clock_jitter: 0.02 } }).text, 'Флаги HDLC есть, CRC не сходится');
    assert.strictEqual(d({ frames_good: 0, frames_bad: 0, reception: undefined }).text, 'Сигнал есть, ждём кадр');
});

test('FrameStrip keeps 60 s of frames and places them on the time axis', () => {
    const strip = new FrameStrip(60000);
    strip.add('good', 1000, 2);
    strip.add('bad', 31000, 1);
    strip.add('good', 61000, 1);
    const pts = strip.points(61000);
    assert.deepStrictEqual(pts.map((p) => p.kind), ['good', 'good', 'bad', 'good']);
    assert.deepStrictEqual(pts.map((p) => p.x), [0, 0, 0.5, 1]);
    assert.strictEqual(strip.points(70000).length, 2, 'frames older than 60 s drop out');
    strip.clear();
    assert.strictEqual(strip.points(70000).length, 0);
});

test('live panel renders stages, diagnosis and the frame strip', () => {
    const panel = new DemodPanel({ now: () => 12000 });
    panel.setLive(true);
    assert.strictEqual(nodes.get('manual-dm-diag').textContent, 'Ждём состояние приёмника');

    panel.applyStatus(withStatus({ frames_good: 0, frames_bad: 0, last_packet_ms: 0 }), null);
    panel.applyStatus(withStatus({ frames_good: 2, frames_bad: 1 }), null);
    assert.strictEqual(nodes.get('manual-st-carrier-val').textContent, '−1,8 кГц');
    assert.strictEqual(nodes.get('manual-st-carrier-sub').textContent, 'девиация 2,4 кГц');
    assert.strictEqual(stageState('clock'), 'ok');
    assert.strictEqual(nodes.get('manual-dm-diag').textContent, 'Приём идёт: 2 кадра, последний 2 с назад');
    assert.ok(nodes.get('manual-dm-diag').classList._set.has('ml-diag--ok'));

    const dots = nodes.get('manual-dm-strip').children;
    assert.deepStrictEqual(dots.map((d) => d.className),
        ['ml-strip__dot ml-strip__dot--good', 'ml-strip__dot ml-strip__dot--good', 'ml-strip__dot ml-strip__dot--bad']);
    assert.strictEqual(dots[0].style.left, '100%');

    // A new stream session restarts the counters and the strip.
    panel.applyStatus(withStatus({ frames_good: 0, frames_bad: 0 }), null);
    assert.strictEqual(nodes.get('manual-dm-strip').children.length, 0);
    panel.destroy();
});

test('mock panel without a radio path shows a simulated reception', () => {
    const panel = new DemodPanel();
    panel.setChannel({ freqMHz: 435.25, label: 'AO-91 BPSK', modulation: 'BPSK31', active: true });
    assert.strictEqual(nodes.get('manual-ch-name').textContent, 'AO-91 BPSK');
    assert.strictEqual(nodes.get('manual-ch-freq').textContent, '435,250');
    assert.strictEqual(stageState('clock'), 'ok');
    assert.strictEqual(nodes.get('manual-dm-diag').textContent, 'Имитация: у станции нет радиотракта');
    assert.strictEqual(panel.getChannel().freqMHz, 435.25);

    panel.setChannel({ freqMHz: 145.92, label: 'RS-44 CW', modulation: 'CW', active: false });
    assert.strictEqual(stageState('signal'), 'idle');
    assert.strictEqual(nodes.get('manual-dm-diag').textContent, 'Нет сигнала выше порога');
    panel.destroy();
});

test('live mode keeps live stages on channel change', () => {
    const panel = new DemodPanel({ now: () => 12000 });
    panel.setLive(true);
    panel.applyStatus(status, null);
    panel.setChannel({ freqMHz: 145.825, label: 'SCOUT', modulation: 'AFSK 1200', active: true });
    assert.strictEqual(nodes.get('manual-ch-name').textContent, 'SCOUT');
    assert.strictEqual(nodes.get('manual-st-signal-val').textContent, '−52.3 dBFS', 'channel change keeps live stages');
    panel.destroy();
});

test('applyStatus is ignored outside live mode', () => {
    const panel = new DemodPanel();
    panel.setChannel({ freqMHz: 435.25, label: 'X', modulation: 'FSK', active: true });
    panel.applyStatus(withStatus({ squelch_open: false }), null);
    assert.strictEqual(nodes.get('manual-dm-diag').textContent, 'Имитация: у станции нет радиотракта');
    panel.destroy();
});

const sstvStatus = {
    radio_path: 'radio_path_0', active: true, demod: 'sstv', supported: true,
    level_dbfs: -60, squelch_dbfs: -90, squelch_open: true, frames_good: 1, frames_bad: 0,
    bandwidth_hz: 15000, reception: { carrier_hz: 120, clock_jitter: 0 },
    picture: { mode: 'robot36', mode_name: 'Robot 36', lines: 120, height: 240 },
};

test('SSTV stages show the VIS mode and lines instead of clock and sync, frames are hidden', () => {
    const s = stagesFromStatus(sstvStatus, null);
    assert.deepStrictEqual(s.clock, { state: 'ok', value: 'Robot 36', sub: 'заголовок принят', name: 'VIS' });
    assert.deepStrictEqual(s.sync, { state: 'ok', value: '120 / 240', sub: 'строк', name: 'Строки' });
    assert.strictEqual(s.frames.hidden, true);
    assert.strictEqual(s.carrier.state, 'ok');
});

test('SSTV without a picture waits for VIS', () => {
    const s = stagesFromStatus(Object.assign({}, sstvStatus, { picture: undefined, frames_good: 0 }), null);
    assert.deepStrictEqual(s.clock, { state: 'idle', value: 'ждём', sub: 'заголовок VIS', name: 'VIS' });
    assert.deepStrictEqual(s.sync, { state: 'idle', value: '—', sub: 'строк', name: 'Строки' });
    assert.strictEqual(s.frames.hidden, true);
});

test('SSTV diagnosis follows the picture, also under a closed squelch', () => {
    const d = (patch) => {
        const st = Object.assign({}, sstvStatus, patch);
        return diagnose(st, stagesFromStatus(st, null), 0);
    };
    const started = { mode: 'robot36', mode_name: 'Robot 36', lines: 0, height: 240 };
    assert.deepStrictEqual(d({ picture: started }), { state: 'ok', text: 'Обнаружен сигнал SSTV: Robot 36' });
    assert.deepStrictEqual(d({}), { state: 'ok', text: 'Идёт обработка: Robot 36, 120 из 240 строк' });
    assert.deepStrictEqual(d({ squelch_open: false }), { state: 'ok', text: 'Идёт обработка: Robot 36, 120 из 240 строк' });
    assert.deepStrictEqual(d({ picture: undefined, frames_good: 0 }),
        { state: 'idle', text: 'Сигнал есть, ждём заголовок VIS' });
    assert.deepStrictEqual(d({ picture: undefined, frames_good: 0, squelch_open: false }),
        { state: 'idle', text: 'Нет сигнала выше порога' });
    assert.deepStrictEqual(d({ picture: undefined, frames_good: 2 }),
        { state: 'ok', text: 'Приём завершён; принято картинок: 2' });
});

test('stage names, frames row and strip switch with the profile', () => {
    const panel = new DemodPanel();
    panel.setLive(true);
    panel.applyStatus(sstvStatus, null);
    assert.strictEqual(nodes.get('manual-st-clock-name').textContent, 'VIS');
    assert.strictEqual(nodes.get('manual-st-frames').hidden, true);
    assert.strictEqual(nodes.get('manual-dm-strip-box').hidden, true);
    panel.applyStatus(status, null);
    assert.strictEqual(nodes.get('manual-st-clock-name').textContent, 'Такты');
    assert.strictEqual(nodes.get('manual-st-frames-name').textContent, 'Кадры');
    assert.strictEqual(nodes.get('manual-st-frames').hidden, false);
    assert.strictEqual(nodes.get('manual-dm-strip-box').hidden, false);
    panel.destroy();
});

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed > 0 ? 1 : 0);
