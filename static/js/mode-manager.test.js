// Тесты для ModeManager.
// Запуск: node static/js/mode-manager.test.js

'use strict';

const assert = require('assert');
const { ModeManager, ModeId, StationType } = require('./mode-manager.js');

// ── In-memory localStorage для изоляции тестов ───────────────

class MemoryStorage {
    constructor() {
        this._data = new Map();
    }
    getItem(key) {
        return this._data.has(key) ? this._data.get(key) : null;
    }
    setItem(key, value) {
        this._data.set(key, String(value));
    }
    removeItem(key) {
        this._data.delete(key);
    }
    clear() {
        this._data.clear();
    }
}

// ── Фабрики тестовых данных ──────────────────────────────────

function vhfPath(id = 1) {
    return { id, name: 'VHF Observation', has_rotator: false };
}

function uhfRotatorPath(id = 2) {
    return { id, name: 'UHF Tracking', has_rotator: true };
}

// ── Микро-фреймворк ──────────────────────────────────────────

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
        console.error(`    ${err.message}`);
    }
}

// ── Группы тестов ────────────────────────────────────────────

console.log('\n— Basic station —');

test('basic: getStationType returns basic', () => {
    const mm = new ModeManager(StationType.BASIC, [], { storage: new MemoryStorage() });
    assert.strictEqual(mm.getStationType(), 'basic');
    assert.strictEqual(mm.isBasic(), true);
});

test('basic: availableModes is empty', () => {
    const mm = new ModeManager(StationType.BASIC, [], { storage: new MemoryStorage() });
    assert.deepStrictEqual(mm.availableModes(), []);
});

test('basic: getMode returns null', () => {
    const mm = new ModeManager(StationType.BASIC, [], { storage: new MemoryStorage() });
    assert.strictEqual(mm.getMode(), null);
});

test('basic: setMode is ignored', () => {
    const mm = new ModeManager(StationType.BASIC, [], { storage: new MemoryStorage() });
    assert.strictEqual(mm.setMode(ModeId.OVERVIEW), false);
    assert.strictEqual(mm.getMode(), null);
});

test('basic: setRadioPath is ignored', () => {
    const mm = new ModeManager(StationType.BASIC, [], { storage: new MemoryStorage() });
    assert.strictEqual(mm.setRadioPath(1), false);
    assert.strictEqual(mm.getRadioPath(), null);
});

console.log('\n— Observation station —');

test('observation: default mode is overview', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    assert.strictEqual(mm.getMode(), ModeId.OVERVIEW);
});

test('observation: first radio path is active by default', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(7), vhfPath(11)], {
        storage: new MemoryStorage(),
    });
    assert.strictEqual(mm.getRadioPathId(), 7);
});

test('observation: hasRotator=false for path without rotator', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    assert.strictEqual(mm.hasRotator(), false);
});

test('observation: availableModes includes overview and manual', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    assert.deepStrictEqual(mm.availableModes(), [
        ModeId.OVERVIEW,
        ModeId.MANUAL,
    ]);
});

console.log('\n— setMode —');

test('setMode notifies subscribers', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    const events = [];
    mm.onModeChange((m) => events.push(m));
    mm.setMode(ModeId.MANUAL);
    assert.deepStrictEqual(events, [ModeId.MANUAL]);
});

test('setMode does not notify when mode is unchanged', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    const events = [];
    mm.onModeChange((m) => events.push(m));
    mm.setMode(ModeId.OVERVIEW);
    assert.deepStrictEqual(events, []);
});

test('setMode rejects unknown value', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    assert.strictEqual(mm.setMode('unknown'), false);
    assert.strictEqual(mm.getMode(), ModeId.OVERVIEW);
});

console.log('\n— setRadioPath —');

test('setRadioPath switches to existing path', () => {
    const mm = new ModeManager(StationType.HYBRID, [vhfPath(1), uhfRotatorPath(2)], {
        storage: new MemoryStorage(),
    });
    assert.strictEqual(mm.setRadioPath(2), true);
    assert.strictEqual(mm.getRadioPathId(), 2);
    assert.strictEqual(mm.hasRotator(), true);
});

test('setRadioPath ignores non-existent id', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    assert.strictEqual(mm.setRadioPath(99), false);
    assert.strictEqual(mm.getRadioPathId(), 1);
});

test('setRadioPath notifies subscribers with full path object', () => {
    const mm = new ModeManager(StationType.HYBRID, [vhfPath(1), uhfRotatorPath(2)], {
        storage: new MemoryStorage(),
    });
    const received = [];
    mm.onRadioPathChange((rp) => received.push(rp));
    mm.setRadioPath(2);
    assert.strictEqual(received.length, 1);
    assert.strictEqual(received[0].id, 2);
    assert.strictEqual(received[0].has_rotator, true);
});

console.log('\n— Persistence (localStorage) —');

test('persist: setMode is saved to localStorage', () => {
    const storage = new MemoryStorage();
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], { storage });
    mm.setMode(ModeId.MANUAL);
    assert.strictEqual(storage.getItem('ux.mainMode'), ModeId.MANUAL);
});

test('persist: setRadioPath is saved to localStorage', () => {
    const storage = new MemoryStorage();
    const mm = new ModeManager(StationType.HYBRID, [vhfPath(1), uhfRotatorPath(2)], {
        storage,
    });
    mm.setRadioPath(2);
    assert.strictEqual(storage.getItem('ux.radioPath'), '2');
});

test('persist: mode is restored from localStorage on construction', () => {
    const storage = new MemoryStorage();
    storage.setItem('ux.mainMode', ModeId.MANUAL);
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], { storage });
    assert.strictEqual(mm.getMode(), ModeId.MANUAL);
});

test('persist: radio path is restored from localStorage', () => {
    const storage = new MemoryStorage();
    storage.setItem('ux.radioPath', '2');
    const mm = new ModeManager(StationType.HYBRID, [vhfPath(1), uhfRotatorPath(2)], {
        storage,
    });
    assert.strictEqual(mm.getRadioPathId(), 2);
});

test('persist: invalid stored id falls back to first path', () => {
    const storage = new MemoryStorage();
    storage.setItem('ux.radioPath', '999');
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], { storage });
    assert.strictEqual(mm.getRadioPathId(), 1);
});

test('persist: invalid stored mode falls back to overview', () => {
    const storage = new MemoryStorage();
    storage.setItem('ux.mainMode', 'bogus');
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], { storage });
    assert.strictEqual(mm.getMode(), ModeId.OVERVIEW);
});

console.log('\n— Subscriptions —');

test('subscription: unsubscribe stops notifications', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    const events = [];
    const off = mm.onModeChange((m) => events.push(m));
    mm.setMode(ModeId.MANUAL);
    off();
    mm.setMode(ModeId.OVERVIEW);
    assert.deepStrictEqual(events, [ModeId.MANUAL]);
});

test('subscription: error in one listener does not break another', () => {
    const mm = new ModeManager(StationType.OBSERVATION, [vhfPath(1)], {
        storage: new MemoryStorage(),
    });
    const ok = [];
    const origErr = console.error;
    console.error = () => {};
    try {
        mm.onModeChange(() => {
            throw new Error('boom');
        });
        mm.onModeChange((m) => ok.push(m));
        mm.setMode(ModeId.MANUAL);
    } finally {
        console.error = origErr;
    }
    assert.deepStrictEqual(ok, [ModeId.MANUAL]);
});

console.log(`\nResults: ${passed} passed, ${failed} failed`);
if (failed > 0) {
    process.exit(1);
}
