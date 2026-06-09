// Тесты для auto-link.js: fingerprint, сортировка TX, renderStrip, getPassPacketTotal.
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
            if (!this._listeners[ev]) this._listeners[ev] = [];
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
    StateEventType: { TX_CYCLE: 'tx_cycle', SATELLITE_GROUP_UPDATE: 'satellite_group_update' },
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
} = AutoLink;

// ── Также загрузим satellite-state для тестов getPassPacketTotal ──────────────

const { SatelliteStateManager, StateEventType } = require('./satellite-state.js');

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

// ── getPassPacketTotal (satellite-state.js) ──────────────────────────────────

(function test_getPassPacketTotal_from_backend() {
    const sm = new SatelliteStateManager();
    const txCycleData = {
        ts: Date.now(),
        satellites: [{
            norad_id: 25544,
            transmitters: [
                { uuid: 'tx-a', packets: 5, power: 0.5, total_packets: 100, history: [] },
                { uuid: 'tx-b', packets: 3, power: 0.3, total_packets: 50, history: [] },
            ],
        }],
    };
    sm.updateTxCycle(txCycleData);
    const total = sm.getPassPacketTotal(25544);
    assert.strictEqual(total, 150, 'Σ total_packets = 100 + 50');
    console.log('PASS: getPassPacketTotal from backend total_packets');
})();

(function test_getPassPacketTotal_unknown_norad() {
    const sm = new SatelliteStateManager();
    sm.updateTxCycle({
        ts: Date.now(),
        satellites: [{ norad_id: 25544, transmitters: [{ uuid: 'x', total_packets: 10 }] }],
    });
    assert.strictEqual(sm.getPassPacketTotal(99999), 0, 'неизвестный NORAD → 0');
    console.log('PASS: getPassPacketTotal unknown norad');
})();

(function test_getPassPacketTotal_updates_on_new_event() {
    const sm = new SatelliteStateManager();
    sm.updateTxCycle({
        ts: 1, satellites: [{
            norad_id: 25544,
            transmitters: [{ uuid: 'a', total_packets: 10 }],
        }],
    });
    assert.strictEqual(sm.getPassPacketTotal(25544), 10);

    sm.updateTxCycle({
        ts: 2, satellites: [{
            norad_id: 25544,
            transmitters: [{ uuid: 'a', total_packets: 25 }],
        }],
    });
    assert.strictEqual(sm.getPassPacketTotal(25544), 25, 'обновляется по новому событию');
    console.log('PASS: getPassPacketTotal updates on new event');
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

console.log('\nAll auto-link tests passed.');
