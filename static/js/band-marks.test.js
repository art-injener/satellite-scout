/**
 * Unit tests for band-marks.js (pure helpers, no DOM).
 *
 * Run: node static/js/band-marks.test.js
 */

'use strict';

const assert = require('assert');
const { markCode, markKind, markTitle, markSummary, clusterAt, leaderAnchor, pickBandwidth, openCardSignals } = require('./band-marks.js');

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

/** Satellite beacon from ADR-009 §11.2. */
const BEACON = {
    id: 's7', center_hz: 436198200, bandwidth_hz: 18000, snr_db: 14.2, state: 'active',
    last_seen_ms: 1669299999000, norad_id: 53385,
    origin: 'satellite', emission: 'pulsed', hint: 'Люб. 70 см',
    stats: { first_seen_ms: 1669299700000, observed_s: 300, on_s: 41, bursts: 29, period_s: 10.1, drift_hz_s: -162 },
};

console.log('\n— markCode —');

test('emission only when structure is unknown; origin is never on the code', () => {
    assert.strictEqual(markCode({ origin: 'satellite', emission: 'pulsed' }), 'Имп.');
    assert.strictEqual(markCode({ origin: 'terrestrial', emission: 'continuous' }), 'Непр.');
    assert.strictEqual(markCode({ origin: 'satellite_probable', emission: 'undetermined' }), '');
    assert.strictEqual(markCode({ origin: 'unknown', emission: 'pulsed' }), 'Имп.');
    assert.strictEqual(markCode(null), '');
});

test('recognised modulation replaces emission; no origin prefix', () => {
    const fsk = { modulation: '2fsk', confidence: 0.9, params: { occupied_hz: 10000, baud: 4800, deviation_hz: 2400 } };
    assert.strictEqual(markCode({ origin: 'terrestrial', emission: 'continuous', structure: fsk }), '2FSK 4800');
    assert.strictEqual(markCode({ origin: 'unknown', emission: 'pulsed',
        structure: { modulation: 'cw', params: { wpm: 18 } } }), 'CW');
    assert.strictEqual(markCode({ origin: 'satellite', emission: 'pulsed',
        structure: { modulation: 'afsk', params: { baud: 1200 } } }), 'AFSK 1200');
    assert.strictEqual(markCode({ origin: 'unknown', emission: 'continuous',
        structure: { modulation: 'fm_voice', params: {} } }), 'ЧМ голос');
    assert.strictEqual(markCode({ origin: 'terrestrial', emission: 'continuous',
        structure: { modulation: 'wfm', service: 'broadcast', service_label: 'Эфирное радио', params: {} } }),
        'ШЧМ/Радио');
});

test('undetermined or unknown structure keeps the emission code', () => {
    assert.strictEqual(markCode({ origin: 'unknown', emission: 'pulsed',
        structure: { modulation: 'undetermined', params: {} } }), 'Имп.');
    assert.strictEqual(markCode({ origin: 'terrestrial', emission: 'continuous',
        structure: { modulation: 'unknown', params: {} } }), 'Непр.');
});

console.log('\n— markKind —');

test('maps origin to the look of the marker', () => {
    assert.strictEqual(markKind({ origin: 'satellite' }), 'sat');
    assert.strictEqual(markKind({ origin: 'satellite_probable' }), 'probable');
    assert.strictEqual(markKind({ origin: 'terrestrial' }), 'ground');
    assert.strictEqual(markKind({ origin: 'unknown' }), 'unknown');
    assert.strictEqual(markKind({}), 'unknown');
});

console.log('\n— markTitle —');

function clock(ms) {
    const d = new Date(ms);
    const p = (n) => (n < 10 ? '0' : '') + n;
    return p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
}

test('card lists the agreed air fields; emission and features stay off', () => {
    const t0 = clock(BEACON.stats.first_seen_ms);
    const t1 = clock(BEACON.last_seen_ms);
    assert.strictEqual(markTitle(BEACON, 'GEOSCAN-EDELVEIS'), [
        'Передатчик\tGEOSCAN-EDELVEIS',
        'Частота\t436.1982 МГц',
        'Полоса\t18 кГц',
        'Дрейф\t−162 Гц/с',
        'Модуляция\t—',
        'Вероятность\t—',
        'Вид передачи\t—',
        'Положение\tКосмос',
        'Демодулятор\t—',
        'Период\t10.1 с',
        'С/Ш\t14.2 дБ',
        'В эфире\t41 с',
        'Время\t' + t0 + ' → ' + t1,
    ].join('\n'));
});

test('RDS station fills transmitter; wfm suggests FM broadcast demod', () => {
    const title = markTitle({
        center_hz: 105700000, bandwidth_hz: 200000, snr_db: 22,
        origin: 'terrestrial', emission: 'continuous',
        structure: {
            modulation: 'wfm', service: 'broadcast', service_label: 'Эфирное радио: EUROPA+',
            station: 'EUROPA+', demod: 'wfm', params: {},
        },
        stats: { on_s: 10, first_seen_ms: 1000 }, last_seen_ms: 2000,
    }, null);
    assert.ok(title.indexOf('Передатчик\tEUROPA+') === 0, title);
    assert.ok(title.indexOf('Демодулятор\tFM эфир') > 0, title);
    assert.ok(title.indexOf('Модуляция\tширокая ЧМ') > 0, title);
    assert.ok(title.indexOf('Вероятность\t—') > 0, title);
    assert.ok(title.indexOf('Вид передачи\tЭфирное радио: EUROPA+') > 0, title);
    assert.ok(title.indexOf('Положение\tЗемля') > 0, title);
});

test('transmitter falls back to broadcast service_label when station is empty', () => {
    const title = markTitle({
        center_hz: 104100000, bandwidth_hz: 180000, emission: 'continuous',
        structure: {
            modulation: 'wfm', service: 'broadcast', service_label: 'Эфирное радио: DFM',
            demod: 'wfm', params: {},
        },
        stats: {},
    }, null);
    assert.ok(title.indexOf('Передатчик\tDFM') === 0, title);
});

test('time column merges first and last; removed rows stay gone', () => {
    const ground = markTitle({
        center_hz: 433920000, bandwidth_hz: 2500, snr_db: 8, origin: 'terrestrial', emission: 'continuous',
        stats: { on_s: 3700, first_seen_ms: 1000 }, last_seen_ms: 2000,
    }, null);
    assert.ok(ground.indexOf('Передатчик\t—') === 0 || ground.indexOf('Передатчик\t—\n') >= 0);
    assert.ok(ground.indexOf('С/Ш\t8 дБ') > 0);
    assert.ok(ground.indexOf('Время\t' + clock(1000) + ' → ' + clock(2000)) > 0);
    assert.ok(ground.indexOf('В эфире\t1 ч 1 мин') > 0);
    assert.ok(ground.indexOf('Период\t—') > 0);
    assert.ok(ground.indexOf('Положение\tЗемля') > 0);
    assert.ok(ground.indexOf('Расположение') < 0);
    assert.ok(ground.indexOf('Первый') < 0);
    assert.ok(ground.indexOf('Последний') < 0);
    assert.ok(ground.indexOf('Вид источника') < 0);
    assert.ok(ground.indexOf('Излучение') < 0);
    assert.ok(ground.indexOf('Признаки') < 0);

    assert.ok(markTitle({
        center_hz: 1e8, bandwidth_hz: 800, norad_id: 25544, stats: {}, last_seen_ms: 0,
    }, null).indexOf('Передатчик\tNORAD 25544') === 0);

    const same = markTitle({
        center_hz: 1e8, bandwidth_hz: 1e3, snr_db: 3,
        stats: { on_s: 1, first_seen_ms: 5000 }, last_seen_ms: 5000,
    }, null);
    assert.ok(same.endsWith('Время\t' + clock(5000)), same);

    const empty = markTitle({
        center_hz: 1e8, bandwidth_hz: 1e4, origin: 'unknown', emission: 'pulsed',
        stats: { on_s: 2, first_seen_ms: 0 }, last_seen_ms: 0,
    }, null);
    assert.ok(empty.indexOf('Время\t—') > 0);
    assert.ok(empty.indexOf('Положение\t—') > 0);
    assert.ok(markTitle({
        center_hz: 1e8, bandwidth_hz: 1e3, origin: 'satellite_probable', stats: {},
    }, null).indexOf('Положение\tКосмос?') > 0);
    assert.ok(empty.indexOf('С/Ш\t—') > 0);
});

test('SSTV keeps its label; marker code stays short', () => {
    const sig = {
        origin: 'terrestrial', emission: 'pulsed', snr_db: 11,
        structure: {
            modulation: 'fm', service: 'sstv', service_label: 'SSTV, Robot 36',
            why: 'VIS Robot 36', params: {},
        },
        stats: {},
    };
    assert.strictEqual(markCode(sig), 'ЧМ/SSTV');
    const title = markTitle(sig, null);
    assert.ok(title.indexOf('Модуляция\tЧМ') > 0, title);
    assert.ok(title.indexOf('Вид передачи\tSSTV, Robot 36') > 0, title);
    assert.ok(title.indexOf('Вид источника') < 0);
    assert.strictEqual(markCode({
        origin: 'unknown', emission: 'continuous',
        structure: { modulation: 'fm', service: 'sstv_probable', service_label: 'SSTV?', params: {} },
    }), 'ЧМ/SSTV?');
    assert.ok(markTitle({
        origin: 'unknown', emission: 'continuous',
        structure: { modulation: 'fm', service: 'sstv_probable', service_label: 'SSTV?', params: {} },
        stats: {},
    }, null).indexOf('Вид передачи\tSSTV?') > 0);
});

test('signature and probe stages stay on the service row', () => {
    const row = (structure) => markTitle({
        center_hz: 145500000, bandwidth_hz: 12500, structure: structure, stats: {},
    }, null).split('\n').find((l) => l.startsWith('Вид передачи\t')).split('\t')[1];
    assert.strictEqual(row({
        modulation: 'fm_voice', service: 'amateur_voice', service_label: 'Радиопередатчик',
        service_basis: 'signature', params: {},
    }), 'Радиопередатчик, по признакам');
    assert.strictEqual(markCode({
        structure: {
            modulation: 'fm_voice', service: 'amateur_voice', service_label: 'Радиопередатчик',
            service_basis: 'signature', params: {},
        },
    }), 'ЧМ голос/Пер.');
    assert.strictEqual(row({
        modulation: 'ook', service: 'ism_burst', service_label: 'Датчик или брелок',
        service_basis: 'signature', params: { baud: 2000 },
    }), 'Датчик или брелок, по признакам');
    assert.strictEqual(markCode({
        structure: { modulation: 'ook', service: 'ism_burst', service_basis: 'signature', params: { baud: 2000 } },
    }), 'OOK 2000/Брел');
    assert.strictEqual(row({
        modulation: 'lfm', service: 'lora', service_label: 'LoRa', service_basis: 'probe', params: {},
    }), 'LoRa, проба сошлась');
    assert.strictEqual(markCode({
        structure: { modulation: '2fsk', service: 'sat_tx', service_label: 'Радиопередатчик', params: { baud: 9600 } },
    }), '2FSK 9600/Пер.');
    assert.strictEqual(markCode({
        structure: { modulation: '2fsk', service: 'gsm_base', service_label: 'GSM', service_basis: 'probe', params: {} },
    }), '2FSK/GSM');
});

test('recognised structure fills modulation with parameters', () => {
    const title = markTitle({
        center_hz: 405015600, bandwidth_hz: 10600, origin: 'unknown', emission: 'continuous',
        structure: {
            modulation: '2fsk', confidence: 0.87,
            params: { occupied_hz: 9800, baud: 4800, deviation_hz: 2350 },
            why: 'огибающая постоянна, 2 уровня частоты, такты 4800',
        },
        stats: { on_s: 300 },
    }, null);
    assert.ok(title.indexOf('Модуляция\t2FSK, 4800 бод, девиация ±2.4 кГц') > 0, title);
    assert.ok(title.indexOf('Вероятность\t87 %') > 0, title);
    assert.ok(title.indexOf('Признаки') < 0, title);
});

test('modulation text for each kind and for undetermined', () => {
    const row = (structure) => markTitle({ center_hz: 1e8, bandwidth_hz: 1e3, structure: structure, stats: {} }, null)
        .split('\n').find((l) => l.startsWith('Модуляция\t')).split('\t')[1];
    assert.strictEqual(row({ modulation: 'cw', params: { wpm: 18.4 } }), 'CW, 18 WPM');
    assert.strictEqual(row({ modulation: 'ook', params: { baud: 2000 } }), 'OOK, 2000 бод');
    assert.strictEqual(row({ modulation: 'ook', params: {} }), 'OOK');
    assert.strictEqual(row({ modulation: 'carrier', params: {} }), 'несущая');
    assert.strictEqual(row({ modulation: 'fm', params: {} }), 'ЧМ');
    assert.strictEqual(row({ modulation: 'wfm', params: {} }), 'широкая ЧМ');
    assert.strictEqual(row({ modulation: 'usb', params: {} }), 'USB');
    assert.strictEqual(row({ modulation: 'undetermined', params: {} }), 'не определена');
    assert.strictEqual(row({ modulation: 'unknown', params: {} }), 'неизвестна');
});

console.log('\n— markSummary —');

test('one-line summary for a cluster list', () => {
    assert.strictEqual(markSummary(BEACON), '436.1982 МГц, полоса 18 кГц');
    assert.strictEqual(markSummary({ center_hz: 1e8, snr_db: 9, origin: 'unknown', emission: 'undetermined' }),
        '100.0000 МГц');
});

console.log('\n— clusterAt —');

test('signals within the gap share a tip, farther ones stay out', () => {
    const a = { id: 'a' };
    const b = { id: 'b' };
    const c = { id: 'c' };
    const placed = [{ x: 100, halfPx: 0, sig: a }, { x: 108, halfPx: 0, sig: b }, { x: 140, halfPx: 0, sig: c }];
    assert.deepStrictEqual(clusterAt(placed, 100, 12).map((s) => s.id), ['a', 'b']);
    assert.deepStrictEqual(clusterAt(placed, 140, 12).map((s) => s.id), ['c']);
    assert.deepStrictEqual(clusterAt(placed, 100, 50).map((s) => s.id), ['a', 'b', 'c']);
});

test('a wide bar pulls in a neighbour beyond the plain gap', () => {
    const a = { id: 'a' };
    const b = { id: 'b' };
    const placed = [{ x: 100, halfPx: 20, sig: a }, { x: 130, halfPx: 4, sig: b }];
    assert.deepStrictEqual(clusterAt(placed, 100, 12).map((s) => s.id), ['a', 'b']);
});

console.log('\n— openCardSignals —');

test('an open card follows the latest sample of the same signal', () => {
    const early = { id: 's1', center_hz: 405015000, stats: { on_s: 52 } };
    const later = { id: 's1', center_hz: 405016200, stats: { on_s: 53 } };
    const neighbour = { id: 's2', center_hz: 405020000 };
    const placed = [
        { x: 100, halfPx: 4, sig: later },
        { x: 110, halfPx: 2, sig: neighbour },
    ];
    const shown = openCardSignals('s1', placed, 16);
    assert.strictEqual(shown[0], later);
    assert.deepStrictEqual(shown.map((s) => s.id), ['s1', 's2']);
    assert.strictEqual(openCardSignals('s1', [{ x: 40, halfPx: 0, sig: early }], 16)[0].stats.on_s, 52);
    assert.strictEqual(openCardSignals('gone', placed, 16), null);
});

console.log('\n— leaderAnchor —');

test('the callout ends on the nearest edge of the card', () => {
    const card = { left: 100, right: 300, top: 40, bottom: 200 };
    assert.deepStrictEqual(leaderAnchor(card, 40, 120), { x: 100, y: 120 });
    assert.deepStrictEqual(leaderAnchor(card, 200, 400), { x: 200, y: 200 });
    assert.deepStrictEqual(leaderAnchor(card, 200, 100), { x: 200, y: 100 });
});

console.log('\n— pickBandwidth —');

const OPTS = [
    { value: '2800' }, { value: '6000' }, { value: '15000' }, { value: '18000' },
];

test('narrowest option not narrower than the signal', () => {
    assert.strictEqual(pickBandwidth(OPTS, 9000), '15000');
    assert.strictEqual(pickBandwidth(OPTS, 18000), '18000');
    assert.strictEqual(pickBandwidth(OPTS, 1000), '2800');
});

test('a signal wider than every option takes the widest', () => {
    assert.strictEqual(pickBandwidth(OPTS, 40000), '18000');
});

test('greyed-out widths are skipped', () => {
    const opts = OPTS.map((o) => ({ value: o.value, disabled: o.value === '15000' || o.value === '18000' }));
    assert.strictEqual(pickBandwidth(opts, 9000), '6000');
    assert.strictEqual(pickBandwidth(opts, 4000), '6000');
});

test('no options: nothing to pick', () => {
    assert.strictEqual(pickBandwidth([], 9000), null);
    assert.strictEqual(pickBandwidth(null, 9000), null);
});

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) {
    process.exit(1);
}
