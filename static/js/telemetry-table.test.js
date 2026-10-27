/**
 * Unit tests for TelemetryTable.
 *
 * Run: node static/js/telemetry-table.test.js
 */

'use strict';

const assert = require('assert');

function makeTbody() {
    const rows = [];
    const obj = {
        get innerHTML() { return ''; },
        set innerHTML(_v) { rows.length = 0; },
        get firstChild() { return rows[0] || null; },
        appendChild(tr) { rows.push(tr); },
        removeChild(tr) {
            const idx = rows.indexOf(tr);
            if (idx >= 0) { rows.splice(idx, 1); }
        },
        get childNodes() { return rows; },
    };
    return obj;
}

function makeEl(id) {
    return { id: id, textContent: '', scrollTop: 0, scrollHeight: 500 };
}

function withListeners(obj) {
    const handlers = {};
    obj.addEventListener = (type, fn) => { (handlers[type] = handlers[type] || []).push(fn); };
    obj.removeEventListener = (type, fn) => {
        handlers[type] = (handlers[type] || []).filter((h) => h !== fn);
    };
    obj.dispatch = (type, ev) => { (handlers[type] || []).forEach((fn) => fn(ev || {})); };
    return obj;
}

function makeTr() {
    const cells = [];
    return withListeners({
        className: '',
        title: '',
        appendChild(td) { cells.push(td); },
        get childNodes() { return cells; },
    });
}

function makeModal() {
    const classes = new Set(['modal--hidden']);
    return withListeners({
        classList: {
            add: (c) => classes.add(c),
            remove: (c) => classes.delete(c),
            contains: (c) => classes.has(c),
        },
    });
}

global.document = withListeners({
    getElementById: (id) => {
        if (id === 'manual-tmi-tbody') {return _tbody;}
        if (id === 'manual-tmi-scroll') {return _scrollEl;}
        if (id === 'manual-tmi-details') {return _modal;}
        if (id === 'manual-tmi-details-title') {return _modalTitle;}
        if (id === 'manual-tmi-details-tbody') {return _modalTbody;}
        return null;
    },
    createElement: (tag) => {
        if (tag === 'tr') {return makeTr();}
        return { textContent: '', className: '', appendChild() {} };
    },
});
global.window = {};
global.window.demodLabel = require('./demod-panel.js').demodLabel;

const _tbody = makeTbody();
const _scrollEl = makeEl('manual-tmi-scroll');
const _modal = makeModal();
const _modalTitle = makeEl('manual-tmi-details-title');
const _modalTbody = makeTbody();

const { TelemetryTable, telemetryPacket } = require('./telemetry-table.js');

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

console.log('\n— TelemetryTable —');

test('addPacket adds one row per packet with channel column', () => {
    _tbody.innerHTML = '';
    const t = new TelemetryTable();
    t.addPacket({
        time: '12:00:00',
        frame: 1,
        crc: 'OK',
        params: [{ key: 'темп', value: '+35°C' }],
    });
    assert.strictEqual(_tbody.childNodes.length, 1);
    const row = _tbody.childNodes[0];
    assert.strictEqual(row.childNodes.length, 6);
    assert.ok(row.childNodes[0].textContent.indexOf('ISS-FSK') >= 0);
    assert.strictEqual(row.childNodes[1].textContent, '—', 'mock packet has no demodulator');
    t.destroy();
});

test('addPacket shows the demodulator in the second column', () => {
    _tbody.innerHTML = '';
    const t = new TelemetryTable();
    t.addPacket({ crc: 'OK', demod: 'FSK 9600 GEOSCAN', params: [] });
    assert.strictEqual(_tbody.childNodes[0].childNodes[1].textContent, 'FSK 9600 GEOSCAN');
    t.destroy();
});

test('addPacket trims beyond MAX_ROWS', () => {
    _tbody.innerHTML = '';
    const t = new TelemetryTable();
    for (let i = 0; i < 205; i++) {
        t.addPacket({ frame: i, crc: 'OK', params: [{ key: 'x', value: 'y' }] });
    }
    assert.ok(_tbody.childNodes.length <= 200, 'rows must be capped at MAX_ROWS');
    t.destroy();
});

test('setChannel affects only new packets', () => {
    _tbody.innerHTML = '';
    const t = new TelemetryTable();
    t.addPacket({ crc: 'OK', params: [{ key: 'a', value: 'b' }] });
    t.setChannel('AO-91', 435.250);
    t.addPacket({ crc: 'OK', params: [{ key: 'c', value: 'd' }] });
    assert.ok(_tbody.childNodes[0].childNodes[0].textContent.indexOf('ISS-FSK') >= 0);
    assert.ok(_tbody.childNodes[1].childNodes[0].textContent.indexOf('AO-91') >= 0);
    t.destroy();
});

test('clear resets rows and frame seq', () => {
    _tbody.innerHTML = '';
    const t = new TelemetryTable();
    t.addPacket({ crc: 'OK', params: [{ key: 'a', value: 'b' }] });
    t.clear();
    assert.strictEqual(_tbody.childNodes.length, 0);
    t.destroy();
});

test('CRC FAIL row gets fail class', () => {
    _tbody.innerHTML = '';
    const t = new TelemetryTable();
    t.addPacket({ crc: 'FAIL', params: [{ key: 'x', value: 'y' }] });
    const lastRow = _tbody.childNodes[_tbody.childNodes.length - 1];
    assert.strictEqual(lastRow.className, 'ml-tmi__row--fail');
    t.destroy();
});

test('activate starts mock timer, deactivate stops it', () => {
    const t = new TelemetryTable();
    t.activate();
    assert.ok(t._mockTimer, 'timer must be set');
    t.deactivate();
    assert.strictEqual(t._mockTimer, null);
    t.destroy();
});

const beacon = {
    radio_path: 'radio_path_1',
    timestamp_ms: new Date(2026, 9, 14, 11, 10, 15).getTime(),
    frequency_hz: 145825000,
    demod: 'afsk1200',
    src: 'SCOUT',
    dest: 'CQ',
    info: 'Satellite Scout imitator beacon',
    info_hex: '5361',
};

test('telemetryPacket maps a server frame to a table row', () => {
    const pkt = telemetryPacket(beacon);
    assert.strictEqual(pkt.time, '11:10:15');
    assert.strictEqual(pkt.crc, 'OK');
    assert.strictEqual(pkt.channelLabel, 'SCOUT');
    assert.strictEqual(pkt.channelFreqMHz, 145.825);
    assert.strictEqual(pkt.demod, 'AFSK 1200');
    assert.deepStrictEqual(pkt.params, [
        { key: 'кому', value: 'CQ' },
        { key: 'данные', value: 'Satellite Scout imitator beacon' },
    ]);
});

test('telemetryPacket labels the demodulator like the receiver panel, unknown ones by id', () => {
    assert.strictEqual(telemetryPacket(Object.assign({}, beacon, { demod: 'fsk9600_geoscan' })).demod, 'FSK 9600 GEOSCAN');
    assert.strictEqual(telemetryPacket(Object.assign({}, beacon, { demod: 'bpsk1200' })).demod, 'bpsk1200');
});

test('telemetryPacket shows hex when the payload is not text', () => {
    const pkt = telemetryPacket(Object.assign({}, beacon, { info: '' }));
    assert.deepStrictEqual(pkt.params[1], { key: 'hex', value: '5361' });
});

test('live mode stops the mock and keeps it off on activate', () => {
    const t = new TelemetryTable();
    t.activate();
    t.setLive(true);
    assert.strictEqual(t._mockTimer, null, 'live mode must stop the mock');
    t.deactivate();
    t.activate();
    assert.strictEqual(t._mockTimer, null, 'activate in live mode must not start the mock');
    t.setLive(false);
    assert.ok(t._mockTimer, 'leaving live mode while active resumes the mock');
    t.destroy();
});

test('addTelemetry appends a live row', () => {
    _tbody.innerHTML = '';
    const t = new TelemetryTable();
    t.addTelemetry(beacon);
    const row = _tbody.childNodes[0];
    assert.strictEqual(row.childNodes[0].textContent, 'SCOUT · 145,825 MHz');
    assert.strictEqual(row.childNodes[1].textContent, 'AFSK 1200');
    assert.strictEqual(row.childNodes[2].textContent, '11:10:15');
    t.destroy();
});

const geoscanBeacon = Object.assign({}, beacon, {
    demod: 'fsk9600_geoscan',
    dest: 'BEACON',
    info: '',
    info_hex: '01ff',
    params: [
        { id: 'beacon_id', name: 'ID маяка', value: '1' },
        { id: 'eps_cell_voltage_full', name: 'Напряжение АКБ', value: '7,420', unit: 'В', key: true },
        { id: 'eps_mode', name: 'Режим СЭП', value: 'штатный', key: true },
    ],
});

test('telemetryPacket puts key beacon fields into the row', () => {
    const pkt = telemetryPacket(geoscanBeacon);
    assert.deepStrictEqual(pkt.params, [
        { key: 'Напряжение АКБ', value: '7,420 В' },
        { key: 'Режим СЭП', value: 'штатный' },
    ]);
});

test('telemetryPacket keeps every beacon field, recipient and hex for the details', () => {
    const pkt = telemetryPacket(geoscanBeacon);
    assert.deepStrictEqual(pkt.details, [
        { key: 'ID маяка', value: '1' },
        { key: 'Напряжение АКБ', value: '7,420 В' },
        { key: 'Режим СЭП', value: 'штатный' },
        { key: 'кому', value: 'BEACON' },
        { key: 'hex', value: '01ff' },
    ]);
});

test('telemetryPacket without parsed fields has no details', () => {
    assert.strictEqual(telemetryPacket(beacon).details, undefined);
});

test('clicking a row with details opens the modal with every field', () => {
    _tbody.innerHTML = '';
    _modalTbody.innerHTML = '';
    const t = new TelemetryTable();
    t.addTelemetry(geoscanBeacon);
    const row = _tbody.childNodes[0];
    assert.ok(row.className.indexOf('ml-tmi__row--details') >= 0, 'row with details is marked clickable');
    assert.ok(_modal.classList.contains('modal--hidden'));

    row.dispatch('click');
    assert.ok(!_modal.classList.contains('modal--hidden'), 'click opens the modal');
    assert.strictEqual(_modalTitle.textContent, 'SCOUT · 145,825 MHz · 11:10:15 · FSK 9600 GEOSCAN');
    const rows = _modalTbody.childNodes;
    assert.strictEqual(rows.length, 5);
    assert.strictEqual(rows[1].childNodes[0].textContent, 'Напряжение АКБ');
    assert.strictEqual(rows[1].childNodes[1].textContent, '7,420 В');

    row.dispatch('click');
    assert.strictEqual(_modalTbody.childNodes.length, 5, 'reopening replaces the rows');
    t.destroy();
});

test('details modal closes by the close control and by Escape', () => {
    _tbody.innerHTML = '';
    const t = new TelemetryTable();
    t.addTelemetry(geoscanBeacon);
    const row = _tbody.childNodes[0];

    row.dispatch('click');
    _modal.dispatch('click', { target: { dataset: { action: 'close' } } });
    assert.ok(_modal.classList.contains('modal--hidden'), 'close control hides the modal');

    row.dispatch('click');
    _modal.dispatch('click', { target: { dataset: {} } });
    assert.ok(!_modal.classList.contains('modal--hidden'), 'click inside the window keeps it open');
    document.dispatch('keydown', { key: 'Escape' });
    assert.ok(_modal.classList.contains('modal--hidden'), 'Escape hides the modal');
    t.destroy();
});

test('destroy releases the modal and document listeners', () => {
    const t = new TelemetryTable();
    t.destroy();
    _modal.classList.remove('modal--hidden');
    _modal.dispatch('click', { target: { dataset: { action: 'close' } } });
    document.dispatch('keydown', { key: 'Escape' });
    assert.ok(!_modal.classList.contains('modal--hidden'), 'destroyed table must not react');
    _modal.classList.add('modal--hidden');
});

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed > 0 ? 1 : 0);
