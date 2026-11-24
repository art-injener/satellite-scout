/**
 * Unit tests for air-table.js (DOM via minimal stubs).
 *
 * Run: node static/js/air-table.test.js
 */

'use strict';

const assert = require('assert');
const { markRows, hearRows } = require('./band-marks.js');
const { AirTable } = require('./air-table.js');

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

function el(tag) {
    const children = [];
    const node = {
        tagName: tag.toUpperCase(),
        children: children,
        textContent: '',
        classList: {
            _set: new Set(),
            add(c) { this._set.add(c); },
            remove(c) { this._set.delete(c); },
            contains(c) { return this._set.has(c); },
        },
        attributes: {},
        setAttribute(k, v) { this.attributes[k] = String(v); },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attributes, k) ? this.attributes[k] : null; },
        removeAttribute(k) { delete this.attributes[k]; },
        appendChild(child) { children.push(child); return child; },
        addEventListener() {},
        removeEventListener() {},
        querySelector(sel) {
            if (sel && sel.indexOf('tr[data-sig-id=') === 0) {
                const m = /tr\[data-sig-id="([^"]*)"\]/.exec(sel);
                if (!m) { return null; }
                return children.find((c) => c.getAttribute('data-sig-id') === m[1]) || null;
            }
            return null;
        },
        querySelectorAll(sel) {
            if (sel !== 'tr[data-sig-id]') { return []; }
            return children.filter((c) => c.getAttribute('data-sig-id'));
        },
        closest(sel) {
            if (sel === 'tr[data-sig-id]' && this.getAttribute('data-sig-id')) { return this; }
            if (sel === '[data-air-payload]' && this.getAttribute('data-air-payload')) { return this; }
            return null;
        },
    };
    Object.defineProperty(node, 'textContent', {
        get() { return this._text || ''; },
        set(v) {
            this._text = v;
            if (v === '') { children.length = 0; }
        },
    });
    return node;
}

global.document = {
    createElement(tag) { return el(tag); },
    createDocumentFragment() {
        const f = el('fragment');
        f.appendChild = function(child) { this.children.push(child); return child; };
        return f;
    },
};

global.window = { BandMarks: { markRows: markRows } };

const SIG_A = {
    id: 's1', center_hz: 436200000, bandwidth_hz: 12000, snr_db: 10,
    origin: 'terrestrial', emission: 'continuous',
    stats: { on_s: 5, first_seen_ms: 1000 }, last_seen_ms: 2000,
};
const SIG_B = {
    id: 's2', center_hz: 435100000, bandwidth_hz: 9000, snr_db: 14, norad_id: 53385,
    origin: 'satellite', emission: 'pulsed',
    stats: { on_s: 12, first_seen_ms: 3000, period_s: 10 }, last_seen_ms: 4000,
};

console.log('\n— AirTable —');

test('rows follow markRows and sort by frequency', () => {
    const tbody = el('tbody');
    const table = new AirTable(tbody, { nameOf: (id) => (id === 53385 ? 'GEOSCAN' : null) });
    table.setSignals([SIG_A, SIG_B]);
    assert.strictEqual(tbody.children.length, 2);
    assert.strictEqual(tbody.children[0].getAttribute('data-sig-id'), 's2');
    assert.strictEqual(tbody.children[1].getAttribute('data-sig-id'), 's1');
    const cells = tbody.children[0].children.map((td) => td.textContent);
    const expect = markRows(SIG_B, 'GEOSCAN').map((r) => r[1]);
    assert.deepStrictEqual(cells, expect);
    table.destroy();
});

test('rowsFn uses hearRows for the spectrum table', () => {
    const tbody = el('tbody');
    const table = new AirTable(tbody, { rowsFn: hearRows, nameOf: () => null });
    table.setSignals([SIG_A]);
    const cells = tbody.children[0].children.map((td) => td.textContent);
    const expect = hearRows(SIG_A, null).map((r) => {
        if (!r[1] || typeof r[1] !== 'object') { return r[1]; }
        return r[1].label || r[1].text;
    });
    assert.deepStrictEqual(cells, expect);
    assert.strictEqual(cells.length, 10);
    table.destroy();
});

test('Signal and Demod tip cells expose full title on hover', () => {
    const tbody = el('tbody');
    const table = new AirTable(tbody, { rowsFn: hearRows, nameOf: () => null });
    const rich = Object.assign({}, SIG_A, {
        structure: {
            modulation: '2fsk', confidence: 0.8, demod: 'fsk9600_g3ruh',
            demod_alt: ['fsk9600_geoscan'],
            service: 'sat_tx', service_label: 'ТМИ', params: { baud: 9600 },
        },
    });
    table.setSignals([rich]);
    const signalTd = tbody.children[0].children[3];
    const demodTd = tbody.children[0].children[5];
    assert.strictEqual(signalTd.className, 'ml-air__tip');
    assert.ok(signalTd.getAttribute('title').indexOf('·') > 0, signalTd.getAttribute('title'));
    assert.strictEqual(demodTd.className, 'ml-air__tip');
    assert.ok(demodTd.getAttribute('title').indexOf('или') > 0, demodTd.getAttribute('title'));
    table.destroy();
});

test('payload data cell shows kind and opens modal on pointerdown', () => {
    const tbody = el('tbody');
    let opened = null;
    const withData = Object.assign({}, SIG_A, {
        hear_kind: 'Маяк ТМИ',
        hear_details: [{ key: 'тип', value: 'Маяк ТМИ' }],
    });
    const table = new AirTable(tbody, {
        rowsFn: hearRows,
        onOpenPayload: (sig) => { opened = sig; },
        nameOf: () => null,
    });
    table.setSignals([withData]);
    const dataTd = tbody.children[0].children[8];
    assert.strictEqual(dataTd.className, 'ml-air__data');
    assert.strictEqual(dataTd.children[0].textContent, 'Маяк ТМИ');
    const btn = dataTd.children[1];
    assert.strictEqual(btn.getAttribute('data-air-payload'), 's1');
    btn.closest = function(sel) {
        return sel === '[data-air-payload]' || sel === 'button[data-air-payload]' ? btn : null;
    };
    table._onPointerDown({
        button: 0,
        target: btn,
        preventDefault() {},
        stopPropagation() {},
    });
    assert.strictEqual(opened, withData);
    table.destroy();
});

test('click picks the signal and marks the row', () => {
    const tbody = el('tbody');
    let picked = null;
    const table = new AirTable(tbody, {
        onPick: (sig) => { picked = sig; },
        nameOf: () => null,
    });
    table.setSignals([SIG_A]);
    const tr = tbody.children[0];
    tr.closest = function(sel) {
        return sel === 'tr[data-sig-id]' ? tr : null;
    };
    table._onClick({ target: tr });
    assert.strictEqual(picked, SIG_A);
    assert.ok(tr.classList.contains('is-current'));
    table.destroy();
});

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed ? 1 : 0);
