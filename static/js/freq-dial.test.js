/**
 * Unit tests for FreqDial.
 *
 * Run: node static/js/freq-dial.test.js
 *
 * Covers:
 *   - formatHz grouping and 11-digit padding
 *   - digit stepping with carry, cursor moves, clamping to [min, max]
 *   - manual typing: digits only, cursor advances, other keys ignored
 *   - click / wheel target a digit position
 *   - edits stay on the display until SET/Enter, Esc restores the applied value
 *   - setHz/setMHz are silent, only SET notifies via onChange
 *   - destroy detaches listeners
 */

'use strict';

const assert = require('assert');

// ── Minimal DOM stub: enough for createElement/appendChild/classList ──────

function makeEl(tag) {
    return {
        tagName: String(tag).toUpperCase(),
        type: '',
        className: '',
        textContent: '',
        tabIndex: 0,
        children: [],
        parentElement: null,
        _attrs: {},
        _listeners: {},
        _focused: false,
        _classes: new Set(),
        classList: {
            _owner: null,
            add(cls) { this._owner._classes.add(cls); },
            remove(cls) { this._owner._classes.delete(cls); },
            contains(cls) { return this._owner._classes.has(cls); },
            toggle(cls, force) {
                const on = (force === undefined) ? !this._owner._classes.has(cls) : !!force;
                if (on) { this._owner._classes.add(cls); } else { this._owner._classes.delete(cls); }
                return on;
            },
        },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this._attrs, k) ? this._attrs[k] : null; },
        setAttribute(k, v) { this._attrs[k] = String(v); },
        appendChild(child) {
            this.children.push(child);
            child.parentElement = this;
            return child;
        },
        addEventListener(type, fn) {
            if (!this._listeners[type]) { this._listeners[type] = []; }
            this._listeners[type].push(fn);
        },
        removeEventListener(type, fn) {
            const arr = this._listeners[type] || [];
            const idx = arr.indexOf(fn);
            if (idx >= 0) { arr.splice(idx, 1); }
        },
        focus() { this._focused = true; },
        blur() { this._focused = false; },
    };
}

function makeElement(tag) {
    const el = makeEl(tag);
    el.classList._owner = el;
    return el;
}

global.document = {
    createElement: (tag) => makeElement(tag),
    createElementNS: (ns, tag) => makeElement(tag),
};
global.window = {};

const { FreqDial, formatHz } = require('./freq-dial.js');

// ── Helpers over the built widget tree ───────────────────────────────────

/** Все узлы поддерева в порядке обхода. */
function walk(node, out) {
    out = out || [];
    for (const c of node.children) {
        out.push(c);
        walk(c, out);
    }
    return out;
}

function strip(root) {
    return walk(root).find((n) => n.className === 'fdial__digits');
}

function digits(root) {
    return walk(root).filter((n) => n.getAttribute && n.getAttribute('data-pos') !== null);
}

function btn(root, act) {
    return walk(root).find((n) => n.getAttribute && n.getAttribute('data-act') === act);
}

function shown(root) {
    return digits(root).map((d) => d.getAttribute('data-digit')).join('');
}

/** Зажжённые сегменты разряда в порядке 'abcdefg'. */
function litSegs(cell) {
    const order = ['a', 'b', 'c', 'd', 'e', 'f', 'g'];
    let out = '';
    for (const name of order) {
        const poly = cell._segs[name];
        if (poly && poly._classes.has('is-on')) { out += name; }
    }
    return out;
}

function cursorPos(root) {
    return digits(root).findIndex((d) => d._classes.has('is-cursor'));
}

function editing(root) {
    return strip(root)._classes.has('is-editing');
}

function fire(el, type, ev) {
    const arr = (el && el._listeners && el._listeners[type]) || [];
    const event = Object.assign({ type: type, target: el, preventDefault() {} }, ev || {});
    for (const fn of arr.slice()) { fn(event); }
}

function key(root, k) {
    fire(strip(root), 'keydown', { key: k });
}

function makeDial(opts) {
    const root = makeElement('div');
    const changes = [];
    const dial = new FreqDial(root, Object.assign({
        hz: 437800000,
        onChange: (hz) => changes.push(hz),
    }, opts || {}));
    return { root, dial, changes };
}

let failures = 0;
function test(name, fn) {
    try {
        fn();
        console.log(`  ✓ ${name}`);
    } catch (e) {
        failures++;
        console.error(`  ✗ ${name}\n    ${e.message}`);
    }
}

console.log('— FreqDial: formatHz —');

test('groups 11 digits as 2.3.3.3', () => {
    assert.strictEqual(formatHz(437800000), '00.437.800.000');
});

test('pads short values with leading zeros', () => {
    assert.strictEqual(formatHz(1), '00.000.000.001');
});

test('renders the 10 GHz ceiling', () => {
    assert.strictEqual(formatHz(10000000000), '10.000.000.000');
});

test('non-numeric input renders as all zeros', () => {
    assert.strictEqual(formatHz(undefined), '00.000.000.000');
});

console.log('\n— FreqDial: build —');

test('builds 11 digit cells and 5 buttons with SET in the middle', () => {
    const { root } = makeDial();
    assert.strictEqual(digits(root).length, 11);
    const acts = walk(root)
        .filter((n) => n.getAttribute && n.getAttribute('data-act') !== null)
        .map((n) => n.getAttribute('data-act'));
    assert.deepStrictEqual(acts, ['dec', 'left', 'set', 'right', 'inc']);
    assert.strictEqual(btn(root, 'set').textContent, 'SET');
});

test('dial starts out of edit mode', () => {
    const { root } = makeDial();
    assert.ok(!editing(root));
});

test('each digit is an svg of 7 segments', () => {
    const { root } = makeDial();
    for (const cell of digits(root)) {
        // У SVG-узлов класс задаётся атрибутом, а не свойством className
        const svg = cell.children.find((c) => c.getAttribute('class') === 'fdial__seg7');
        assert.ok(svg, 'digit must hold an svg');
        assert.strictEqual(svg.children.length, 7);
        for (const poly of svg.children) {
            assert.strictEqual(poly.getAttribute('class'), 'fdial__seg');
            assert.ok(poly.getAttribute('points'), 'segment needs points');
        }
    }
});

test('segments lit per digit match the 7-segment table', () => {
    const { root, dial } = makeDial({ hz: 1234567890 });
    // 01.234.567.890 → разряды слева направо
    const expected = ['bc', 'abdeg', 'abcdg', 'bcfg', 'acdfg', 'acdefg', 'abc',
        'abcdefg', 'abcdfg', 'abcdef'];
    assert.strictEqual(dial.getText(), '01.234.567.890');
    const cells = digits(root);
    assert.strictEqual(litSegs(cells[0]), 'abcdef', 'leading 0');
    for (let i = 0; i < expected.length; i++) {
        assert.strictEqual(litSegs(cells[i + 1]), expected[i], `digit at ${i + 1}`);
    }
});

test('changing a digit relights segments without rebuilding them', () => {
    const { root, dial } = makeDial({ hz: 0 });
    const cell = digits(root)[10];
    const svgBefore = cell.children[0];
    const polyBefore = cell._segs.a;
    dial.setHz(7);
    assert.strictEqual(litSegs(cell), 'abc', 'digit 7');
    assert.strictEqual(cell.children[0], svgBefore, 'svg node must be reused');
    assert.strictEqual(cell._segs.a, polyBefore, 'polygons must be reused');
});

test('no visible unit label unless one is asked for', () => {
    const { root } = makeDial();
    assert.ok(!walk(root).some((n) => n.className === 'fdial__unit'));
    const withUnit = makeDial({ unitLabel: 'Гц' });
    assert.ok(walk(withUnit.root).some((n) => n.className === 'fdial__unit' && n.textContent === 'Гц'));
});

test('digit strip is a focusable spinbutton with aria value', () => {
    const { root } = makeDial();
    const s = strip(root);
    assert.strictEqual(s.tabIndex, 0);
    assert.strictEqual(s.getAttribute('role'), 'spinbutton');
    assert.strictEqual(s.getAttribute('aria-valuenow'), '437800000');
    assert.strictEqual(s.getAttribute('aria-valuetext'), '00.437.800.000 Гц');
    assert.strictEqual(s.getAttribute('aria-valuemax'), '10000000000');
});

test('initial value is shown and cursor defaults to the kHz digit', () => {
    const { root, dial } = makeDial();
    assert.strictEqual(shown(root), '00437800000');
    assert.strictEqual(cursorPos(root), 7);
    assert.strictEqual(dial.getText(), '00.437.800.000');
});

test('leading zeros are dimmed, significant digits are not', () => {
    const { root } = makeDial();
    const d = digits(root);
    assert.ok(d[0]._classes.has('is-lead'));
    assert.ok(d[1]._classes.has('is-lead'));
    assert.ok(!d[2]._classes.has('is-lead'), 'digit "4" must stay bright');
    assert.strictEqual(d[2].getAttribute('data-digit'), '4');
});

test('constructor survives a null root', () => {
    const dial = new FreqDial(null, { hz: 1000 });
    assert.strictEqual(dial.getHz(), 1000);
    dial.destroy();
});

console.log('\n— FreqDial: stepping —');

test('+ button increments the digit under the cursor', () => {
    const { root, dial, changes } = makeDial();
    fire(btn(root, 'inc'), 'click');
    assert.strictEqual(dial.getHz(), 437801000);
    assert.strictEqual(shown(root), '00437801000');
    assert.deepStrictEqual(changes, [], 'receiver waits for SET');
});

test('- button decrements the digit under the cursor', () => {
    const { dial, root } = makeDial();
    fire(btn(root, 'dec'), 'click');
    assert.strictEqual(dial.getHz(), 437799000);
});

test('increment carries into higher digits', () => {
    const { dial, root } = makeDial({ hz: 437999000 });
    fire(btn(root, 'inc'), 'click');
    assert.strictEqual(dial.getText(), '00.438.000.000');
});

test('cursor position sets the step weight', () => {
    const { dial, root } = makeDial();
    dial.setCursor(2); // сотни МГц
    fire(btn(root, 'inc'), 'click');
    assert.strictEqual(dial.getHz(), 537800000);
});

test('decrement clamps at the lower bound instead of going negative', () => {
    const { dial, root } = makeDial({ hz: 500 });
    dial.setCursor(0);
    fire(btn(root, 'dec'), 'click');
    assert.strictEqual(dial.getHz(), 0);
});

test('increment clamps at the 10 GHz ceiling', () => {
    const { dial, root } = makeDial({ hz: 9999000000 });
    dial.setCursor(0);
    fire(btn(root, 'inc'), 'click');
    assert.strictEqual(dial.getHz(), 10000000000);
});

test('no onChange when a clamped step leaves the value unchanged', () => {
    const { dial, root, changes } = makeDial({ hz: 0 });
    dial.setCursor(0);
    fire(btn(root, 'dec'), 'click');
    assert.deepStrictEqual(changes, []);
});

test('custom min/max bounds are honoured', () => {
    const { dial, root } = makeDial({ hz: 145000000, minHz: 144000000, maxHz: 146000000 });
    dial.setCursor(1); // десятки МГц
    fire(btn(root, 'inc'), 'click');
    assert.strictEqual(dial.getHz(), 146000000);
});

console.log('\n— FreqDial: cursor —');

test('› and ‹ buttons shift the cursor', () => {
    const { root } = makeDial();
    fire(btn(root, 'right'), 'click');
    assert.strictEqual(cursorPos(root), 8);
    fire(btn(root, 'left'), 'click');
    fire(btn(root, 'left'), 'click');
    assert.strictEqual(cursorPos(root), 6);
});

test('cursor stops at both edges', () => {
    const { root, dial } = makeDial();
    dial.setCursor(0);
    fire(btn(root, 'left'), 'click');
    assert.strictEqual(cursorPos(root), 0);
    dial.setCursor(10);
    fire(btn(root, 'right'), 'click');
    assert.strictEqual(cursorPos(root), 10);
});

test('moving the cursor does not emit onChange', () => {
    const { root, changes } = makeDial();
    fire(btn(root, 'right'), 'click');
    assert.deepStrictEqual(changes, []);
});

test('click on a digit moves the cursor there and focuses the strip', () => {
    const { root } = makeDial();
    fire(strip(root), 'click', { target: digits(root)[4] }); // делегирование с разряда
    assert.strictEqual(cursorPos(root), 4);
    assert.ok(strip(root)._focused, 'strip must take focus for keyboard tuning');
});

test('click on a separator leaves the cursor alone', () => {
    const { root } = makeDial();
    const sep = walk(root).find((n) => n.className === 'fdial__sep');
    fire(strip(root), 'click', { target: sep });
    assert.strictEqual(cursorPos(root), 7);
});

console.log('\n— FreqDial: keyboard —');

test('ArrowUp/ArrowDown step the cursor digit', () => {
    const { root, dial } = makeDial();
    key(root, 'ArrowUp');
    assert.strictEqual(dial.getHz(), 437801000);
    key(root, 'ArrowDown');
    assert.strictEqual(dial.getHz(), 437800000);
});

test('ArrowLeft/ArrowRight move the cursor', () => {
    const { root } = makeDial();
    key(root, 'ArrowLeft');
    assert.strictEqual(cursorPos(root), 6);
    key(root, 'ArrowRight');
    key(root, 'ArrowRight');
    assert.strictEqual(cursorPos(root), 8);
});

test('Home and End jump to the edge digits', () => {
    const { root } = makeDial();
    key(root, 'Home');
    assert.strictEqual(cursorPos(root), 0);
    key(root, 'End');
    assert.strictEqual(cursorPos(root), 10);
});

test('typing a digit writes it at the cursor and advances right', () => {
    const { root, dial, changes } = makeDial();
    key(root, '9'); // курсор на единицах кГц: 800 → 809 кГц
    assert.strictEqual(dial.getText(), '00.437.809.000');
    assert.strictEqual(cursorPos(root), 8);
    assert.deepStrictEqual(changes, [], 'receiver waits for SET');
});

test('typing a full frequency from the leftmost digit', () => {
    const { root, dial } = makeDial({ hz: 0 });
    key(root, 'Home');
    for (const ch of '00145825000') { key(root, ch); }
    assert.strictEqual(dial.getHz(), 145825000);
});

test('non-digit characters are ignored', () => {
    const { root, dial, changes } = makeDial();
    for (const k of ['a', '.', ',', '-', 'F5', ' ']) { key(root, k); }
    assert.strictEqual(dial.getHz(), 437800000);
    assert.deepStrictEqual(changes, []);
});

test('handled keys call preventDefault, unhandled ones do not', () => {
    const { root } = makeDial();
    let prevented = 0;
    fire(strip(root), 'keydown', { key: 'ArrowUp', preventDefault: () => { prevented++; } });
    fire(strip(root), 'keydown', { key: 'Tab', preventDefault: () => { prevented++; } });
    assert.strictEqual(prevented, 1);
});

test('Delete zeroes the digit under the cursor', () => {
    const { root, dial } = makeDial();
    key(root, 'Delete');
    assert.strictEqual(dial.getText(), '00.437.800.000');
    key(root, 'Home');
    key(root, 'ArrowRight');
    key(root, 'ArrowRight');
    key(root, 'Delete');
    assert.strictEqual(dial.getText(), '00.037.800.000');
});

test('Backspace steps left and zeroes that digit', () => {
    const { root, dial } = makeDial();
    dial.setCursor(6);
    key(root, 'Backspace'); // сотни кГц ← обнуляем
    assert.strictEqual(cursorPos(root), 5);
    assert.strictEqual(dial.getText(), '00.437.000.000');
});

test('Backspace at the leftmost digit is a no-op', () => {
    const { root, dial } = makeDial();
    key(root, 'Home');
    key(root, 'Backspace');
    assert.strictEqual(cursorPos(root), 0);
    assert.strictEqual(dial.getHz(), 437800000);
});

console.log('\n— FreqDial: wheel —');

test('wheel up over a digit steps that digit and moves the cursor', () => {
    const { root, dial } = makeDial();
    fire(strip(root), 'wheel', { target: digits(root)[2], deltaY: -1 });
    assert.strictEqual(cursorPos(root), 2);
    assert.strictEqual(dial.getHz(), 537800000);
});

test('wheel down over the strip steps the current digit', () => {
    const { root, dial } = makeDial();
    fire(strip(root), 'wheel', { deltaY: 1 });
    assert.strictEqual(dial.getHz(), 437799000);
});

test('zero-delta wheel does nothing', () => {
    const { root, dial, changes } = makeDial();
    fire(strip(root), 'wheel', { deltaY: 0 });
    assert.strictEqual(dial.getHz(), 437800000);
    assert.deepStrictEqual(changes, []);
});

console.log('\n— FreqDial: SET / Enter / Esc —');

test('any touch of the dial enters edit mode', () => {
    for (const touch of [
        (root) => fire(btn(root, 'inc'), 'click'),
        (root) => fire(btn(root, 'right'), 'click'),
        (root) => fire(strip(root), 'click', { target: digits(root)[3] }),
        (root) => fire(strip(root), 'wheel', { deltaY: -1 }),
        (root) => key(root, 'Home'),
        (root) => key(root, '5'),
    ]) {
        const { root } = makeDial();
        touch(root);
        assert.ok(editing(root), 'cursor must blink while tuning');
    }
});

test('SET applies the dialled frequency once and leaves edit mode', () => {
    const { root, dial, changes } = makeDial();
    fire(btn(root, 'inc'), 'click');
    fire(btn(root, 'inc'), 'click');
    fire(btn(root, 'set'), 'click');
    assert.deepStrictEqual(changes, [437802000]);
    assert.strictEqual(dial.getHz(), 437802000);
    assert.ok(!editing(root));
});

test('SET takes focus off the digits', () => {
    const { root } = makeDial();
    fire(strip(root), 'click', { target: digits(root)[4] });
    assert.ok(strip(root)._focused);
    fire(btn(root, 'set'), 'click');
    assert.ok(!strip(root)._focused);
});

test('SET without a new value leaves edit mode silently', () => {
    const { root, changes } = makeDial();
    fire(btn(root, 'right'), 'click');
    fire(btn(root, 'set'), 'click');
    assert.deepStrictEqual(changes, []);
    assert.ok(!editing(root));
});

test('stepping back to the applied value and pressing SET emits nothing', () => {
    const { root, changes } = makeDial();
    fire(btn(root, 'inc'), 'click');
    fire(btn(root, 'dec'), 'click');
    fire(btn(root, 'set'), 'click');
    assert.deepStrictEqual(changes, []);
});

test('Enter works as SET and is handled', () => {
    const { root, changes } = makeDial();
    key(root, 'ArrowUp');
    let prevented = 0;
    fire(strip(root), 'keydown', { key: 'Enter', preventDefault: () => { prevented++; } });
    assert.deepStrictEqual(changes, [437801000]);
    assert.ok(!editing(root));
    assert.strictEqual(prevented, 1);
});

test('Escape restores the last applied frequency without onChange', () => {
    const { root, dial, changes } = makeDial();
    key(root, 'ArrowUp');
    key(root, 'Enter');
    key(root, 'ArrowUp');
    key(root, 'ArrowUp');
    key(root, 'Escape');
    assert.strictEqual(dial.getHz(), 437801000);
    assert.deepStrictEqual(changes, [437801000]);
    assert.ok(!editing(root));
});

test('Escape keeps the cursor where the operator left it', () => {
    const { root } = makeDial();
    key(root, 'ArrowLeft');
    key(root, 'Escape');
    assert.strictEqual(cursorPos(root), 6);
});

test('setHz during editing becomes the applied value', () => {
    const { root, dial, changes } = makeDial();
    key(root, 'ArrowUp');
    dial.setHz(145825000);
    assert.ok(!editing(root), 'external retune ends editing');
    key(root, 'Escape');
    assert.strictEqual(dial.getHz(), 145825000);
    assert.deepStrictEqual(changes, []);
});

console.log('\n— FreqDial: programmatic API —');

test('setHz updates the display without emitting onChange', () => {
    const { root, dial, changes } = makeDial();
    dial.setHz(145825000);
    assert.strictEqual(shown(root), '00145825000');
    assert.deepStrictEqual(changes, []);
});

test('setMHz converts to Hz and rounds', () => {
    const { dial } = makeDial();
    dial.setMHz(437.80252);
    assert.strictEqual(dial.getText(), '00.437.802.520');
    assert.ok(Math.abs(dial.getMHz() - 437.80252) < 1e-9);
});

test('setHz clamps to bounds', () => {
    const { dial } = makeDial();
    dial.setHz(-5);
    assert.strictEqual(dial.getHz(), 0);
    dial.setHz(9e12);
    assert.strictEqual(dial.getHz(), 10000000000);
});

test('setHz ignores NaN and non-numbers', () => {
    const { dial } = makeDial();
    dial.setHz(NaN);
    dial.setHz('437');
    assert.strictEqual(dial.getHz(), 437800000);
});

console.log('\n— FreqDial: destroy —');

test('destroy detaches listeners so later events are inert', () => {
    const { root, dial, changes } = makeDial();
    const inc = btn(root, 'inc');
    const set = btn(root, 'set');
    const s = strip(root);
    dial.destroy();
    fire(inc, 'click');
    fire(s, 'keydown', { key: 'ArrowUp' });
    fire(set, 'click');
    assert.deepStrictEqual(changes, []);
});

if (failures > 0) {
    console.error(`\n${failures} test(s) failed`);
    process.exit(1);
}
console.log('\nAll FreqDial tests passed');
