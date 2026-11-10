/**
 * Unit tests for air-table.js (DOM via minimal stubs).
 *
 * Run: node static/js/air-table.test.js
 */

'use strict';

const assert = require('assert');
const { markRows } = require('./band-marks.js');
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
        getAttribute(k) { return this.attributes[k] || null; },
        appendChild(child) { children.push(child); return child; },
        addEventListener() {},
        removeEventListener() {},
        querySelectorAll(sel) {
            if (sel !== 'tr[data-sig-id]') { return []; }
            return children.filter((c) => c.getAttribute('data-sig-id'));
        },
        closest(sel) {
            if (sel === 'tr[data-sig-id]' && this.getAttribute('data-sig-id')) { return this; }
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

test('click picks the signal and marks the row', () => {
    const tbody = el('tbody');
    let picked = null;
    const table = new AirTable(tbody, {
        onPick: (sig) => { picked = sig; },
        nameOf: () => null,
    });
    table.setSignals([SIG_A]);
    const tr = tbody.children[0];
    table._onClick({ target: tr });
    assert.strictEqual(picked, SIG_A);
    assert.ok(tr.classList.contains('is-current'));
    table.destroy();
});

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed ? 1 : 0);
