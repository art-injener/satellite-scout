// Тесты для auto-link.js: fingerprint, сортировка TX, renderStrip, cellLabel.
// Запуск: node static/js/auto-link.test.js

'use strict';

const assert = require('assert');

// ── Шимы для Node.js (auto-link.js рассчитан на браузер) ─────────────────

class MemoryStorage {
    constructor() { this._data = new Map(); }
    getItem(key) { return this._data.has(key) ? this._data.get(key) : null; }
    setItem(key, value) { this._data.set(key, String(value)); }
    removeItem(key) { this._data.delete(key); }
}

global.localStorage = new MemoryStorage();

function makeElement(tag) {
    const el = {
        tagName: (tag || 'div').toUpperCase(),
        className: '',
        textContent: '',
        title: '',
        style: {},
        children: [],
        _listeners: {},
        _attrs: {},
        appendChild(child) { this.children.push(child); return child; },
        removeChild(child) {
            const i = this.children.indexOf(child);
            if (i >= 0) { this.children.splice(i, 1); }
        },
        setAttribute(k, v) { this._attrs[k] = v; },
        getAttribute(k) { return this._attrs[k] || null; },
        addEventListener(ev, fn) {
            if (!this._listeners[ev]) {this._listeners[ev] = [];}
            this._listeners[ev].push(fn);
        },
        querySelector() { return null; },
        querySelectorAll() { return []; },
        contains() { return false; },
        classList: {
            _set: new Set(),
            add(c) { this._set.add(c); },
            remove(c) { this._set.delete(c); },
            toggle(c, force) { force ? this._set.add(c) : this._set.delete(c); },
            contains(c) { return this._set.has(c); },
        },
        get lastChild() { return this.children[this.children.length - 1] || null; },
        get clientHeight() { return 0; },
    };
    return el;
}

global.document = {
    createElement: makeElement,
    getElementById() { return null; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
};

global.window = {
    addEventListener() {},
    _stateManager: null,
    StateEventType: { SATELLITE_GROUP_UPDATE: 'satellite_group_update' },
};

// ── Загрузка модуля ──────────────────────────────────────────────────────────

const AutoLink = require('./auto-link.js');

const {
    groupFingerprint,
    txListFingerprint,
    txFromSatnogs,
    renderStrip,
    powerLevelClass,
    cellLabel,
    STRIP_CAPACITY,
    dopplerHz,
    formatDopplerKhz,
    snrLevel,
    lockLedClass,
    powerToDbm,
    resolveLinkHighlight,
    indexTxCycleUpdates,
    applyTxCycleUpdate,
} = AutoLink;

// ── groupFingerprint ─────────────────────────────────────────────────────────

(function test_groupFingerprint_basic() {
    const data = {
        satellites: [
            { norad_id: 25544 },
            { norad_id: 40069 },
        ],
        primary_id: 25544,
        tracking_id: 40069,
    };
    const fp = groupFingerprint(data);
    assert.ok(fp.length > 0, 'fingerprint не пустой');
    assert.ok(fp.includes('25544'), 'содержит NORAD 25544');
    assert.ok(fp.includes('40069'), 'содержит NORAD 40069');
    console.log('PASS: groupFingerprint basic');
})();

(function test_groupFingerprint_order_independent() {
    const a = {
        satellites: [{ norad_id: 25544 }, { norad_id: 40069 }],
        primary_id: 25544, tracking_id: 0,
    };
    const b = {
        satellites: [{ norad_id: 40069 }, { norad_id: 25544 }],
        primary_id: 25544, tracking_id: 0,
    };
    assert.strictEqual(groupFingerprint(a), groupFingerprint(b), 'порядок не влияет');
    console.log('PASS: groupFingerprint order independent');
})();

(function test_groupFingerprint_different_groups() {
    const a = {
        satellites: [{ norad_id: 25544 }],
        primary_id: 25544, tracking_id: 0,
    };
    const b = {
        satellites: [{ norad_id: 40069 }],
        primary_id: 40069, tracking_id: 0,
    };
    assert.notStrictEqual(groupFingerprint(a), groupFingerprint(b));
    console.log('PASS: groupFingerprint different groups');
})();

(function test_groupFingerprint_primary_change_same_composition() {
    const a = {
        satellites: [{ norad_id: 25544 }, { norad_id: 40069 }],
        primary_id: 25544,
        tracking_id: 0,
    };
    const b = {
        satellites: [{ norad_id: 25544 }, { norad_id: 40069 }],
        primary_id: 40069,
        tracking_id: 25544,
    };
    assert.strictEqual(groupFingerprint(a), groupFingerprint(b), 'primary/tracking не влияют на fingerprint');
    console.log('PASS: groupFingerprint primary change same composition');
})();

(function test_txListFingerprint() {
    const rows = [{ id: 'tx-1-a' }, { id: 'tx-1-b' }];
    assert.strictEqual(txListFingerprint(rows), 'tx-1-a,tx-1-b');
    assert.strictEqual(txListFingerprint([]), '');
    console.log('PASS: txListFingerprint');
})();

// ── txFromSatnogs + sort ─────────────────────────────────────────────────────

(function test_txFromSatnogs_basic() {
    const sat = { norad_id: 25544, sat_name: 'ISS' };
    const t = {
        uuid: 'abc-123',
        alive: true,
        status: 'active',
        downlink_low: 145825000,
        mode: 'FM',
        baud: 1200,
        description: 'APRS',
    };
    const row = txFromSatnogs(sat, t);
    assert.ok(row, 'не null для активного передатчика');
    assert.strictEqual(row.freqMHz, '145.825');
    assert.strictEqual(row.freqHz, 145825000);
    assert.strictEqual(row.satNoradId, 25544);
    console.log('PASS: txFromSatnogs basic');
})();

(function test_txFromSatnogs_inactive() {
    const sat = { norad_id: 25544 };
    assert.strictEqual(txFromSatnogs(sat, { uuid: 'x', alive: false, status: 'active', downlink_low: 100e6 }), null);
    assert.strictEqual(txFromSatnogs(sat, { uuid: 'x', alive: true, status: 'inactive', downlink_low: 100e6 }), null);
    assert.strictEqual(txFromSatnogs(sat, { uuid: 'x', alive: true, status: 'active', downlink_low: 0 }), null);
    console.log('PASS: txFromSatnogs inactive filtered');
})();

(function test_tx_sort_by_frequency() {
    const sat = { norad_id: 25544, sat_name: 'ISS' };
    const txList = [
        { uuid: 'c', alive: true, status: 'active', downlink_low: 437800000, mode: 'GMSK' },
        { uuid: 'a', alive: true, status: 'active', downlink_low: 145825000, mode: 'FM' },
        { uuid: 'b', alive: true, status: 'active', downlink_low: 436750000, mode: 'CW' },
    ];
    const rows = txList.map(t => txFromSatnogs(sat, t)).filter(Boolean);
    rows.sort((a, b) => a.freqHz - b.freqHz);

    assert.strictEqual(rows[0].freqMHz, '145.825');
    assert.strictEqual(rows[1].freqMHz, '436.750');
    assert.strictEqual(rows[2].freqMHz, '437.800');
    console.log('PASS: TX sort by frequency');
})();

// ── renderStrip ──────────────────────────────────────────────────────────────

(function test_renderStrip_empty() {
    const el = makeElement('div');
    renderStrip(el, [], 5);
    assert.strictEqual(el.children.length, 5, '5 слотов создано');
    for (const c of el.children) {
        assert.ok(c.className.includes('silent'), 'пустые слоты — silent');
    }
    console.log('PASS: renderStrip empty');
})();

(function test_renderStrip_with_data() {
    const el = makeElement('div');
    const history = [
        { packets: 10, power: 0.9 },
        { packets: 0, power: 0 },
        { packets: 3, power: 0.5 },
    ];
    renderStrip(el, history, 5);
    assert.strictEqual(el.children.length, 5);
    assert.ok(!el.children[0].className.includes('silent'), 'первый слот — не silent');
    assert.ok(el.children[1].className.includes('silent'), 'второй слот — silent (0 пакетов)');
    assert.ok(el.children[3].className.includes('silent'), 'четвёртый — пустой слот');
    console.log('PASS: renderStrip with data');
})();

// ── STRIP_CAPACITY ───────────────────────────────────────────────────────────

(function test_strip_capacity() {
    assert.strictEqual(STRIP_CAPACITY, 10, 'совпадает с бэкендом');
    console.log('PASS: STRIP_CAPACITY = 10');
})();

// ── cellLabel ────────────────────────────────────────────────────────────────

(function test_cellLabel() {
    assert.strictEqual(cellLabel({ packets: 0, power: 0 }), '·');
    assert.strictEqual(cellLabel({ packets: 5, power: 0.5 }), '5');
    assert.strictEqual(cellLabel({ packets: 100, power: 1 }), '99+');
    assert.strictEqual(cellLabel(null), '·');
    console.log('PASS: cellLabel');
})();

// ── powerLevelClass ──────────────────────────────────────────────────────────

(function test_powerLevelClass() {
    assert.ok(powerLevelClass(0, 0).includes('silent'));
    assert.ok(powerLevelClass(0.1, 5).includes('lvl1'));
    assert.ok(powerLevelClass(0.9, 5).includes('lvl5'));
    console.log('PASS: powerLevelClass');
})();

// ── dopplerHz ────────────────────────────────────────────────────────────────

(function test_dopplerHz_zero_rate() {
    assert.strictEqual(dopplerHz(435e6, 0), 0);
    console.log('PASS: dopplerHz zero rate');
})();

(function test_dopplerHz_sign_convention() {
    // range_rate > 0 (удаляется) → доплер отрицательный (ниже несущей)
    const df1 = dopplerHz(435e6, 7000);
    assert.ok(df1 < 0, `df=${df1} ожидался отрицательный при удалении`);
    // range_rate < 0 (приближается) → доплер положительный
    const df2 = dopplerHz(435e6, -7000);
    assert.ok(df2 > 0, `df=${df2} ожидался положительный при приближении`);
    // Симметрия: |df| примерно равны
    assert.ok(Math.abs(df1 + df2) < 1e-6, 'симметрия знаков');
    console.log('PASS: dopplerHz sign convention');
})();

(function test_dopplerHz_magnitude() {
    // Типичный LEO UHF: 435 МГц, range_rate ≈ 7 км/с → |df| ≈ 10 кГц
    const df = dopplerHz(435e6, 7000);
    assert.ok(Math.abs(df) > 9000 && Math.abs(df) < 11000,
        `|df|=${Math.abs(df).toFixed(1)} Гц вне ожидаемого диапазона 9–11 кГц`);
    console.log('PASS: dopplerHz magnitude (UHF/LEO)');
})();

(function test_dopplerHz_invalid_inputs() {
    assert.strictEqual(dopplerHz(0, 1000), 0);
    assert.strictEqual(dopplerHz(NaN, 1000), 0);
    assert.strictEqual(dopplerHz(435e6, NaN), 0);
    console.log('PASS: dopplerHz invalid inputs');
})();

// ── formatDopplerKhz ─────────────────────────────────────────────────────────

(function test_formatDopplerKhz_format() {
    assert.strictEqual(formatDopplerKhz(0), '0.00 кГц');
    assert.strictEqual(formatDopplerKhz(1320), '+1.32 кГц');
    assert.strictEqual(formatDopplerKhz(-850), '\u2212' + '0.85 кГц');
    console.log('PASS: formatDopplerKhz');
})();

// ── snrLevel ─────────────────────────────────────────────────────────────────

(function test_snrLevel() {
    assert.strictEqual(snrLevel(0), 'silent');
    assert.strictEqual(snrLevel(-1), 'silent');
    assert.strictEqual(snrLevel(5), 'low');
    assert.strictEqual(snrLevel(10), 'mid');
    assert.strictEqual(snrLevel(20), 'high');
    console.log('PASS: snrLevel');
})();

// ── lockLedClass ─────────────────────────────────────────────────────────────

(function test_lockLedClass() {
    assert.ok(lockLedClass('OK').endsWith('--ok'));
    assert.ok(lockLedClass('SEARCH').endsWith('--search'));
    assert.ok(lockLedClass('LOST').endsWith('--lost'));
    assert.ok(lockLedClass('').endsWith('--lost'));
    assert.ok(lockLedClass(null).endsWith('--lost'));
    console.log('PASS: lockLedClass');
})();

// ── powerToDbm ───────────────────────────────────────────────────────────────

(function test_powerToDbm() {
    assert.strictEqual(powerToDbm(0), null);
    assert.strictEqual(powerToDbm(0.5), -65);
    assert.strictEqual(powerToDbm(1), -30);
    console.log('PASS: powerToDbm');
})();

// ── resolveLinkHighlight ─────────────────────────────────────────────────────

(function test_resolveLinkHighlight_plan_hover() {
    const r = resolveLinkHighlight(25544, null, null, 25544, 'tx-25544-a');
    assert.strictEqual(r.group, true);
    assert.strictEqual(r.tx, true);
    const other = resolveLinkHighlight(25544, null, null, 40069, 'tx-40069-a');
    assert.strictEqual(other.group, false);
    assert.strictEqual(other.tx, false);
    console.log('PASS: resolveLinkHighlight plan hover');
})();

(function test_resolveLinkHighlight_tx_hover() {
    const hit = resolveLinkHighlight(25544, 'tx-25544-b', null, 25544, 'tx-25544-b');
    assert.strictEqual(hit.group, false);
    assert.strictEqual(hit.tx, true);
    const miss = resolveLinkHighlight(25544, 'tx-25544-b', null, 25544, 'tx-25544-a');
    assert.strictEqual(miss.tx, false);
    console.log('PASS: resolveLinkHighlight tx hover');
})();

(function test_resolveLinkHighlight_selected_fallback() {
    const r = resolveLinkHighlight(null, null, 25544, 25544, 'tx-1');
    assert.strictEqual(r.group, true);
    assert.strictEqual(r.tx, true);
    console.log('PASS: resolveLinkHighlight selected fallback');
})();

// ── tx_cycle helpers ─────────────────────────────────────────────────────────

(function test_indexTxCycleUpdates() {
    const payload = {
        satellites: [{
            norad_id: 25544,
            transmitters: [
                { uuid: 'abc', packets: 3, power: 0.5 },
                { uuid: 'def', packets: 0, power: 0 },
            ],
        }],
    };
    const map = indexTxCycleUpdates(payload);
    assert.strictEqual(map.size, 2);
    assert.strictEqual(map.get('abc').packets, 3);
    console.log('PASS: indexTxCycleUpdates');
})();

(function test_applyTxCycleUpdate() {
    const row = { power: 0, lock: 'LOST', totalPackets: 0 };
    applyTxCycleUpdate(row, {
        power: 0.8,
        lock: 'OK',
        snr_db: 12.5,
        total_packets: 42,
        total_failed: 2,
        packets_failed: 1,
        history: [{ packets: 3, power: 0.5 }],
    });
    assert.strictEqual(row.lock, 'OK');
    assert.strictEqual(row.totalPackets, 42);
    assert.strictEqual(row.history.length, 1);
    console.log('PASS: applyTxCycleUpdate');
})();

console.log('\nAll auto-link tests passed.');
