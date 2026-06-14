// Тесты для SatelliteStateManager (FE-001).
// Запуск: node static/js/satellite-state.test.js

'use strict';

const assert = require('assert');
const {
    SatelliteStateManager,
    SatelliteState,
    StateEventType,
    getTrackColorPalette,
    pickTrackColorFromPalette,
    trackColorDistance,
    paletteMinHueSeparation,
} = require('./satellite-state.js');

// ── Вспомогательные данные ────────────────────────────────

const ISS_NORAD_ID = 25544;
const METEOR_NORAD_ID = 40069;

/** Фабрика тестовых данных позиции. */
function makePositionData(overrides = {}) {
    return {
        norad_id: ISS_NORAD_ID,
        name: 'ISS',
        lat: 47.3,
        lon: 39.8,
        alt: 418,
        az: 215.0,
        el: 42.0,
        range: 623.0,
        visibility_zone: {
            segments: [[{ lon: 20, lat: 30 }, { lon: 21, lat: 31 }]],
            radius_deg: 20.1,
            center_lat: 47.3,
            center_lon: 39.8,
            altitude_km: 418,
        },
        ts: 1738900000000,
        ...overrides,
    };
}

/** Фабрика тестовых данных трека. */
function makeTrackData(overrides = {}) {
    return {
        norad_id: ISS_NORAD_ID,
        past: [[{ lon: 38, lat: 46, ts: 1738899990000 }, { lon: 39, lat: 47, ts: 1738899995000 }]],
        future: [[{ lon: 40, lat: 48, ts: 1738900005000 }, { lon: 41, lat: 49, ts: 1738900010000 }]],
        ...overrides,
    };
}

// ── Счётчики тестов ───────────────────────────────────────

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

// ── SatelliteState ────────────────────────────────────────

console.log('\nSatelliteState');

test('constructor sets noradId and name', () => {
    const state = new SatelliteState(25544, 'ISS');
    assert.strictEqual(state.noradId, 25544);
    assert.strictEqual(state.name, 'ISS');
    assert.strictEqual(state.position, null);
    assert.strictEqual(state.track, null);
    assert.strictEqual(state.visibilityZone, null);
});

test('constructor defaults name to empty string', () => {
    const state = new SatelliteState(25544);
    assert.strictEqual(state.name, '');
});

// ── StateEventType ────────────────────────────────────────

console.log('\nStateEventType');

test('event types are frozen', () => {
    assert.strictEqual(StateEventType.POSITION, 'position');
    assert.strictEqual(StateEventType.TRACK, 'track');
    assert.strictEqual(StateEventType.SATELLITE_CHANGE, 'satellite_change');
    assert.ok(Object.isFrozen(StateEventType));
});

// ── Constructor ───────────────────────────────────────────

console.log('\nSatelliteStateManager — constructor');

test('initial state is empty', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.satelliteCount, 0);
    assert.strictEqual(m.getActiveSatelliteId(), null);
    assert.strictEqual(m.getActiveState(), null);
    assert.deepStrictEqual(m.getSatelliteIds(), []);
});

// ── Subscribe / Unsubscribe ───────────────────────────────

console.log('\nSatelliteStateManager — subscribe/unsubscribe');

test('subscribe returns true for valid event type', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.subscribe('position', () => {}), true);
    assert.strictEqual(m.subscriberCount('position'), 1);
});

test('subscribe returns false for unknown event type', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.subscribe('unknown_event', () => {}), false);
});

test('subscribe returns false for non-function callback', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.subscribe('position', 'not a function'), false);
    assert.strictEqual(m.subscriberCount('position'), 0);
});

test('unsubscribe removes callback', () => {
    const m = new SatelliteStateManager();
    const cb = () => {};
    m.subscribe('position', cb);
    assert.strictEqual(m.subscriberCount('position'), 1);
    assert.strictEqual(m.unsubscribe('position', cb), true);
    assert.strictEqual(m.subscriberCount('position'), 0);
});

test('unsubscribe returns false for unknown callback', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.unsubscribe('position', () => {}), false);
});

test('unsubscribe returns false for unknown event type', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.unsubscribe('bad_type', () => {}), false);
});

test('same callback is added only once (Set)', () => {
    const m = new SatelliteStateManager();
    const cb = () => {};
    m.subscribe('position', cb);
    m.subscribe('position', cb);
    assert.strictEqual(m.subscriberCount('position'), 1);
});

test('multiple different callbacks', () => {
    const m = new SatelliteStateManager();
    m.subscribe('position', () => {});
    m.subscribe('position', () => {});
    assert.strictEqual(m.subscriberCount('position'), 2);
});

// ── updatePosition ────────────────────────────────────────

console.log('\nSatelliteStateManager — updatePosition');

test('creates satellite state on first position update', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    assert.strictEqual(m.satelliteCount, 1);
    const state = m.getState(ISS_NORAD_ID);
    assert.ok(state);
    assert.strictEqual(state.noradId, ISS_NORAD_ID);
    assert.strictEqual(state.name, 'ISS');
    assert.strictEqual(state.position.lat, 47.3);
    assert.strictEqual(state.position.lon, 39.8);
    assert.strictEqual(state.position.alt, 418);
    assert.strictEqual(state.position.az, 215.0);
    assert.strictEqual(state.position.el, 42.0);
    assert.strictEqual(state.position.range, 623.0);
    assert.strictEqual(state.position.ts, 1738900000000);
});

test('auto-sets active satellite on first update', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.getActiveSatelliteId(), null);
    m.updatePosition(makePositionData());
    assert.strictEqual(m.getActiveSatelliteId(), ISS_NORAD_ID);
});

test('does not override active satellite on subsequent updates', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.updatePosition(makePositionData({ norad_id: METEOR_NORAD_ID, name: 'METEOR' }));
    assert.strictEqual(m.getActiveSatelliteId(), ISS_NORAD_ID);
    assert.strictEqual(m.satelliteCount, 2);
});

test('updates visibility zone from position data', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    const state = m.getState(ISS_NORAD_ID);
    assert.ok(state.visibilityZone);
    assert.strictEqual(state.visibilityZone.radius_deg, 20.1);
    assert.strictEqual(state.visibilityZone.segments.length, 1);
    assert.strictEqual(state.visibilityZone.segments[0].length, 2);
});

test('position without visibility_zone does not clear existing zone', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    // Второе обновление без зоны видимости.
    const dataNoZone = makePositionData();
    delete dataNoZone.visibility_zone;
    m.updatePosition(dataNoZone);
    const state = m.getState(ISS_NORAD_ID);
    assert.ok(state.visibilityZone, 'visibility zone should be preserved');
});

test('notifies position subscribers for active satellite', () => {
    const m = new SatelliteStateManager();
    let notified = null;
    m.subscribe('position', (state) => { notified = state; });
    m.updatePosition(makePositionData());
    assert.ok(notified);
    assert.strictEqual(notified.noradId, ISS_NORAD_ID);
    assert.strictEqual(notified.position.lat, 47.3);
});

test('does not notify for non-active satellite', () => {
    const m = new SatelliteStateManager();
    m.setActiveSatellite(METEOR_NORAD_ID);
    let notified = false;
    m.subscribe('position', () => { notified = true; });
    m.updatePosition(makePositionData({ norad_id: ISS_NORAD_ID }));
    assert.strictEqual(notified, false);
});

test('rejects position with missing norad_id', () => {
    const m = new SatelliteStateManager();
    m.updatePosition({ lat: 1, lon: 2 });
    assert.strictEqual(m.satelliteCount, 0);
});

test('rejects null/undefined data', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(null);
    m.updatePosition(undefined);
    assert.strictEqual(m.satelliteCount, 0);
});

test('updates name when provided', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData({ name: 'ISS (ZARYA)' }));
    assert.strictEqual(m.getState(ISS_NORAD_ID).name, 'ISS (ZARYA)');
    m.updatePosition(makePositionData({ name: 'ISS (NAUKA)' }));
    assert.strictEqual(m.getState(ISS_NORAD_ID).name, 'ISS (NAUKA)');
});

test('range defaults to 0 if not provided', () => {
    const m = new SatelliteStateManager();
    const data = makePositionData();
    delete data.range;
    m.updatePosition(data);
    assert.strictEqual(m.getState(ISS_NORAD_ID).position.range, 0);
});

// ── updateTrack ───────────────────────────────────────────

console.log('\nSatelliteStateManager — updateTrack');

test('stores track data', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.updateTrack(makeTrackData());
    const state = m.getState(ISS_NORAD_ID);
    assert.ok(state.track);
    assert.strictEqual(state.track.past.length, 1);
    assert.strictEqual(state.track.future.length, 1);
    assert.strictEqual(state.track.past[0].length, 2);
});

test('notifies track subscribers for active satellite', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    let notified = null;
    m.subscribe('track', (state) => { notified = state; });
    m.updateTrack(makeTrackData());
    assert.ok(notified);
    assert.strictEqual(notified.track.past.length, 1);
});

test('does not notify for non-active satellite track', () => {
    const m = new SatelliteStateManager();
    m.setActiveSatellite(METEOR_NORAD_ID);
    let notified = false;
    m.subscribe('track', () => { notified = true; });
    m.updateTrack(makeTrackData({ norad_id: ISS_NORAD_ID }));
    assert.strictEqual(notified, false);
});

test('rejects track with missing norad_id', () => {
    const m = new SatelliteStateManager();
    m.updateTrack({ past: [], future: [] });
    assert.strictEqual(m.satelliteCount, 0);
});

test('defaults past/future to empty arrays', () => {
    const m = new SatelliteStateManager();
    m.updateTrack({ norad_id: ISS_NORAD_ID });
    const state = m.getState(ISS_NORAD_ID);
    assert.deepStrictEqual(state.track.past, []);
    assert.deepStrictEqual(state.track.future, []);
});

// ── setActiveSatellite ────────────────────────────────────

console.log('\nSatelliteStateManager — setActiveSatellite');

test('sets active satellite and notifies', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    let notified = null;
    m.subscribe(StateEventType.SELECTED_CHANGE, (state) => { notified = state; });
    m.setActiveSatellite(METEOR_NORAD_ID);
    assert.strictEqual(m.getActiveSatelliteId(), METEOR_NORAD_ID);
    assert.ok(notified);
    assert.strictEqual(notified.noradId, METEOR_NORAD_ID);
});

test('does not notify when setting same satellite', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    let count = 0;
    m.subscribe(StateEventType.SELECTED_CHANGE, () => { count++; });
    m.setActiveSatellite(ISS_NORAD_ID);
    assert.strictEqual(count, 0, 'should not notify for same satellite');
});

test('returns false for invalid noradId', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.setActiveSatellite(-1), false);
    assert.strictEqual(m.setActiveSatellite(0), false);
    assert.strictEqual(m.setActiveSatellite('abc'), false);
});

test('creates state for unknown satellite when set active', () => {
    const m = new SatelliteStateManager();
    m.setActiveSatellite(99999);
    assert.strictEqual(m.satelliteCount, 1);
    assert.strictEqual(m.getActiveSatelliteId(), 99999);
});

// ── getActiveState / getState ─────────────────────────────

console.log('\nSatelliteStateManager — getActiveState/getState');

test('getActiveState returns active satellite state', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    const state = m.getActiveState();
    assert.ok(state);
    assert.strictEqual(state.noradId, ISS_NORAD_ID);
    assert.strictEqual(state.position.lat, 47.3);
});

test('getActiveState returns null when no active satellite', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.getActiveState(), null);
});

test('getState returns null for unknown satellite', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.getState(99999), null);
});

// ── removeSatellite / clear ───────────────────────────────

console.log('\nSatelliteStateManager — removeSatellite/clear');

test('removeSatellite removes state', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    assert.strictEqual(m.removeSatellite(ISS_NORAD_ID), true);
    assert.strictEqual(m.satelliteCount, 0);
    assert.strictEqual(m.getState(ISS_NORAD_ID), null);
});

test('removeSatellite resets active if removed', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.removeSatellite(ISS_NORAD_ID);
    assert.strictEqual(m.getActiveSatelliteId(), null);
});

test('removeSatellite returns false for unknown', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.removeSatellite(99999), false);
});

test('clear resets everything', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.updatePosition(makePositionData({ norad_id: METEOR_NORAD_ID }));
    m.clear();
    assert.strictEqual(m.satelliteCount, 0);
    assert.strictEqual(m.getActiveSatelliteId(), null);
    assert.deepStrictEqual(m.getSatelliteIds(), []);
});

// ── getSatelliteIds ───────────────────────────────────────

console.log('\nSatelliteStateManager — getSatelliteIds');

test('returns list of known satellite IDs', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.updatePosition(makePositionData({ norad_id: METEOR_NORAD_ID }));
    const ids = m.getSatelliteIds();
    assert.strictEqual(ids.length, 2);
    assert.ok(ids.includes(ISS_NORAD_ID));
    assert.ok(ids.includes(METEOR_NORAD_ID));
});

// ── Subscriber error handling ─────────────────────────────

console.log('\nSatelliteStateManager — subscriber error handling');

test('subscriber error does not break other subscribers', () => {
    const m = new SatelliteStateManager();
    let secondCalled = false;

    m.subscribe('position', () => { throw new Error('boom'); });
    m.subscribe('position', () => { secondCalled = true; });

    // Подавляем console.error для чистоты вывода.
    const origError = console.error;
    console.error = () => {};
    m.updatePosition(makePositionData());
    console.error = origError;

    assert.strictEqual(secondCalled, true, 'second subscriber should be called');
});

// ── Множественные спутники ────────────────────────────────

console.log('\nSatelliteStateManager — multiple satellites');

test('switching active satellite redirects notifications', () => {
    const m = new SatelliteStateManager();
    const received = [];

    m.subscribe('position', (state) => { received.push(state.noradId); });

    // ISS — автоактивный.
    m.updatePosition(makePositionData());
    assert.deepStrictEqual(received, [ISS_NORAD_ID]);

    // Meteor — не активный, не нотифицирует.
    m.updatePosition(makePositionData({ norad_id: METEOR_NORAD_ID, name: 'METEOR' }));
    assert.deepStrictEqual(received, [ISS_NORAD_ID]);

    // Переключаемся на Meteor — сразу нотифицируем POSITION для нового selected.
    m.setActiveSatellite(METEOR_NORAD_ID);

    // Ещё один position для Meteor — вторая нотификация по position.
    m.updatePosition(makePositionData({ norad_id: METEOR_NORAD_ID, name: 'METEOR', lat: 55.0 }));
    assert.deepStrictEqual(received, [ISS_NORAD_ID, METEOR_NORAD_ID, METEOR_NORAD_ID]);

    // ISS не selected — не нотифицирует.
    m.updatePosition(makePositionData({ lat: 50.0 }));
    assert.deepStrictEqual(received, [ISS_NORAD_ID, METEOR_NORAD_ID, METEOR_NORAD_ID]);
});

// ── setSelectedSatellite ──────────────────────────────────

console.log('\nSatelliteStateManager — setSelectedSatellite');

test('setSelectedSatellite sets selected and notifies SELECTED_CHANGE', () => {
    const m = new SatelliteStateManager();
    let notified = null;
    m.subscribe(StateEventType.SELECTED_CHANGE, (state) => { notified = state; });
    m.setSelectedSatellite(25544, 'ISS');
    assert.strictEqual(m.getSelectedSatelliteId(), 25544);
    assert.ok(notified);
    assert.strictEqual(notified.noradId, 25544);
});

test('setSelectedSatellite does not notify when same ID without forceNotify', () => {
    const m = new SatelliteStateManager();
    m.setSelectedSatellite(25544, 'ISS');
    let count = 0;
    m.subscribe(StateEventType.SELECTED_CHANGE, () => { count++; });
    m.setSelectedSatellite(25544, 'ISS', false, false);
    assert.strictEqual(count, 0, 'should not notify for same NORAD without forceNotify');
});

test('setSelectedSatellite notifies when same ID with forceNotify=true', () => {
    const m = new SatelliteStateManager();
    m.setSelectedSatellite(25544, 'ISS');
    let count = 0;
    m.subscribe(StateEventType.SELECTED_CHANGE, () => { count++; });
    m.setSelectedSatellite(25544, 'ISS', false, true);
    assert.strictEqual(count, 1, 'must notify with forceNotify=true even for same NORAD');
});

// ── isManualTableSelection ────────────────────────────────

console.log('\nSatelliteStateManager — isManualTableSelection');

test('isManualTableSelection returns false by default', () => {
    const m = new SatelliteStateManager();
    assert.strictEqual(m.isManualTableSelection(), false);
});

test('isManualTableSelection returns true after manual setSelectedSatellite', () => {
    const m = new SatelliteStateManager();
    m.setSelectedSatellite(25544, 'ISS', true);
    assert.strictEqual(m.isManualTableSelection(), true);
});

test('isManualTableSelection reset to false by auto setSelectedSatellite', () => {
    const m = new SatelliteStateManager();
    m.setSelectedSatellite(25544, 'ISS', true);
    m.setSelectedSatellite(40069, 'METEOR', false);
    assert.strictEqual(m.isManualTableSelection(), false);
});

// ── setSatelliteGroup [BUG-E] ─────────────────────────────

console.log('\nSatelliteStateManager — setSatelliteGroup [BUG-E]');

test('setSatelliteGroup sets selected from primary_id', () => {
    const m = new SatelliteStateManager();
    let notified = null;
    m.subscribe(StateEventType.SELECTED_CHANGE, (state) => { notified = state; });

    m.setSatelliteGroup({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true, is_active: true },
        ],
    });

    assert.strictEqual(m.getSelectedSatelliteId(), 25544);
    assert.ok(notified, 'should notify SELECTED_CHANGE on first group update');
});

test('setSatelliteGroup notifies SELECTED_CHANGE when primary_id changes', () => {
    const m = new SatelliteStateManager();
    m.setSatelliteGroup({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true, is_active: true },
        ],
    });

    let notified = null;
    m.subscribe(StateEventType.SELECTED_CHANGE, (state) => { notified = state; });

    m.setSatelliteGroup({
        primary_id: 40069,
        satellites: [
            { norad_id: 40069, sat_name: 'METEOR', aos: 3000, los: 4000, is_visible: true, is_active: true },
        ],
    });

    assert.ok(notified, 'should notify when primary_id changes');
    assert.strictEqual(notified.noradId, 40069);
});

test('setSatelliteGroup notifies when pass data changes for same primary_id', () => {
    const m = new SatelliteStateManager();

    // Первый group_update: primary 25544, AOS=1000, LOS=2000, visible.
    m.setSatelliteGroup({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true, is_active: true },
        ],
    });

    let notifiedCount = 0;
    m.subscribe(StateEventType.SELECTED_CHANGE, () => { notifiedCount++; });

    // Второй group_update: тот же primary, но LOS другой (видимость изменилась).
    m.setSatelliteGroup({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: false, is_active: true },
        ],
    });

    assert.strictEqual(notifiedCount, 1,
        'must notify SELECTED_CHANGE when pass data (is_visible) changes for same primary_id');
});

test('setSatelliteGroup notifies when AOS/LOS changes (new orbit)', () => {
    const m = new SatelliteStateManager();

    m.setSatelliteGroup({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true, is_active: true },
        ],
    });

    let notifiedCount = 0;
    m.subscribe(StateEventType.SELECTED_CHANGE, () => { notifiedCount++; });

    // Новый виток: другие AOS/LOS.
    m.setSatelliteGroup({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 100000, los: 200000, is_visible: false, is_active: true },
        ],
    });

    assert.strictEqual(notifiedCount, 1,
        'must notify when AOS/LOS changes (next orbit pass)');
});

test('setSatelliteGroup does NOT notify when same primary and same pass data', () => {
    const m = new SatelliteStateManager();

    m.setSatelliteGroup({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true, is_active: true },
        ],
    });

    let notifiedCount = 0;
    m.subscribe(StateEventType.SELECTED_CHANGE, () => { notifiedCount++; });

    // Полностью идентичный group_update.
    m.setSatelliteGroup({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true, is_active: true },
        ],
    });

    assert.strictEqual(notifiedCount, 0,
        'should NOT notify when primary and pass data are identical');
});

test('setSatelliteGroup does not override manual table selection when satellite still in group', () => {
    const m = new SatelliteStateManager();
    m.setSelectedSatellite(25544, 'ISS', true); // manual selection

    let notified = false;
    m.subscribe(StateEventType.SELECTED_CHANGE, () => { notified = true; });

    m.setSatelliteGroup({
        primary_id: 40069,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true, is_active: false },
            { norad_id: 40069, sat_name: 'METEOR', aos: 3000, los: 4000, is_visible: true, is_active: true },
        ],
    });

    assert.strictEqual(m.getSelectedSatelliteId(), 25544,
        'manual selection should not be overridden by group_update when satellite is in group');
    assert.strictEqual(notified, false, 'should not notify');
});

test('setSatelliteGroup resets manual selection when satellite leaves group', () => {
    const m = new SatelliteStateManager();
    m.setSelectedSatellite(25544, 'ISS', true);

    m.setSatelliteGroup({
        primary_id: 40069,
        satellites: [
            { norad_id: 40069, sat_name: 'METEOR', aos: 3000, los: 4000, is_visible: true, is_active: true },
        ],
    });

    assert.strictEqual(m.getSelectedSatelliteId(), 40069,
        'should auto-select primary when manually selected satellite leaves group');
});

// ── _hasPrimaryPassChanged ────────────────────────────────

console.log('\nSatelliteStateManager — _hasPrimaryPassChanged');

test('_hasPrimaryPassChanged returns false on first call (no previous data)', () => {
    const m = new SatelliteStateManager();
    const result = m._hasPrimaryPassChanged({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true },
        ],
    });
    assert.strictEqual(result, false, 'first call should return false (no baseline)');
});

test('_hasPrimaryPassChanged returns true when visibility changes', () => {
    const m = new SatelliteStateManager();
    m._hasPrimaryPassChanged({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true },
        ],
    });
    const result = m._hasPrimaryPassChanged({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: false },
        ],
    });
    assert.strictEqual(result, true, 'visibility change must be detected');
});

test('_hasPrimaryPassChanged returns true when AOS/LOS changes', () => {
    const m = new SatelliteStateManager();
    m._hasPrimaryPassChanged({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true },
        ],
    });
    const result = m._hasPrimaryPassChanged({
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 100000, los: 200000, is_visible: false },
        ],
    });
    assert.strictEqual(result, true, 'AOS/LOS change must be detected');
});

test('_hasPrimaryPassChanged returns false when nothing changes', () => {
    const m = new SatelliteStateManager();
    const data = {
        primary_id: 25544,
        satellites: [
            { norad_id: 25544, sat_name: 'ISS', aos: 1000, los: 2000, is_visible: true },
        ],
    };
    m._hasPrimaryPassChanged(data);
    const result = m._hasPrimaryPassChanged(data);
    assert.strictEqual(result, false, 'identical data should return false');
});

// ── setTrackingSatellite / clearTrackingSatellite ──────────

console.log('\nSatelliteStateManager — tracking satellite');

test('setTrackingSatellite and getTrackingSatelliteId', () => {
    const m = new SatelliteStateManager();
    m.setTrackingSatellite(25544, 'ISS');
    assert.strictEqual(m.getTrackingSatelliteId(), 25544);
});

test('clearTrackingSatellite resets tracking', () => {
    const m = new SatelliteStateManager();
    m.setTrackingSatellite(25544, 'ISS');
    m.clearTrackingSatellite();
    assert.strictEqual(m.getTrackingSatelliteId(), null);
});

test('setTrackingSatellite notifies TRACKING_CHANGE', () => {
    const m = new SatelliteStateManager();
    let notified = null;
    m.subscribe(StateEventType.TRACKING_CHANGE, (state) => { notified = state; });
    m.setTrackingSatellite(25544, 'ISS');
    assert.ok(notified, 'should notify TRACKING_CHANGE');
    assert.strictEqual(notified.noradId, 25544);
});

test('clearTrackingSatellite notifies TRACKING_CHANGE with null', () => {
    const m = new SatelliteStateManager();
    m.setTrackingSatellite(25544, 'ISS');
    let notifiedState = 'not_called';
    m.subscribe(StateEventType.TRACKING_CHANGE, (state) => { notifiedState = state; });
    m.clearTrackingSatellite();
    assert.strictEqual(notifiedState, null, 'should notify with null state when tracking cleared');
});

// ── updateTrack: дедупликация по fingerprint (FIX-TRACK-DEDUP) ──

console.log('\nSatelliteStateManager — updateTrack deduplication (FIX-TRACK-DEDUP)');

test('updateTrack stores track on first call', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.updateTrack(makeTrackData());
    const state = m.getState(ISS_NORAD_ID);
    assert.ok(state.track, 'track should be stored');
    assert.ok(state.track.past.length > 0, 'past segments should exist');
});

test('updateTrack skips identical track data (cached)', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.setSelectedSatellite(ISS_NORAD_ID);
    m.updateTrack(makeTrackData());
    let notified = false;
    m.subscribe('track', () => { notified = true; });
    m.updateTrack(makeTrackData());
    assert.strictEqual(notified, false, 'identical track should not notify');
});

test('updateTrack applies track when timestamps change (track recalculated)', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.setSelectedSatellite(ISS_NORAD_ID);
    m.updateTrack(makeTrackData());
    let notified = false;
    m.subscribe('track', () => { notified = true; });
    const shifted = makeTrackData({
        past: [[{ lon: 38.5, lat: 46.5, ts: 1738900020000 }, { lon: 39.5, lat: 47.5, ts: 1738900025000 }]],
        future: [[{ lon: 40.5, lat: 48.5, ts: 1738900035000 }, { lon: 41.5, lat: 49.5, ts: 1738900040000 }]],
    });
    m.updateTrack(shifted);
    assert.strictEqual(notified, true, 'track with shifted timestamps must trigger notify');
});

test('updateTrack detects change when segment count stays same but data differs', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.setSelectedSatellite(ISS_NORAD_ID);
    m.updateTrack(makeTrackData());
    let notified = false;
    m.subscribe('track', () => { notified = true; });
    const newTrack = {
        norad_id: ISS_NORAD_ID,
        past: [[{ lon: 38, lat: 46, ts: 1738900050000 }, { lon: 39, lat: 47, ts: 1738900055000 }]],
        future: [[{ lon: 40, lat: 48, ts: 1738900065000 }, { lon: 41, lat: 49, ts: 1738900070000 }]],
    };
    m.updateTrack(newTrack);
    assert.strictEqual(notified, true, 'same segment count but different timestamps must trigger notify');
});

test('updateTrack notifies TRACK subscriber when data changes', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.updateTrack(makeTrackData());
    let notifyCount = 0;
    m.subscribe('track', () => { notifyCount++; });
    // Кешированный трек — не должен уведомлять.
    m.updateTrack(makeTrackData());
    assert.strictEqual(notifyCount, 0, 'cached track should not notify');
    // Новый трек — должен уведомить.
    m.updateTrack(makeTrackData({
        past: [[{ lon: 38, lat: 46, ts: 9999990000 }, { lon: 39, lat: 47, ts: 9999995000 }]],
    }));
    assert.strictEqual(notifyCount, 1, 'changed track should notify');
});

test('updateTrack accepts track when segment count changes', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData());
    m.setSelectedSatellite(ISS_NORAD_ID);
    m.updateTrack(makeTrackData());
    let notified = false;
    m.subscribe('track', () => { notified = true; });
    const twoSegments = makeTrackData({
        future: [
            [{ lon: 40, lat: 48, ts: 1738900005000 }],
            [{ lon: 170, lat: 50, ts: 1738900015000 }],
        ],
    });
    m.updateTrack(twoSegments);
    assert.strictEqual(notified, true, 'different segment count must trigger notify');
});

// ── _trackFingerprint ─────────────────────────────────────

console.log('\nSatelliteStateManager — _trackFingerprint');

test('fingerprint for empty segments', () => {
    const fp = SatelliteStateManager._trackFingerprint([], []);
    assert.strictEqual(fp, '0:0');
});

test('fingerprint includes segment count and timestamps', () => {
    const past = [[{ lon: 0, lat: 0, ts: 100 }, { lon: 1, lat: 1, ts: 200 }]];
    const future = [[{ lon: 2, lat: 2, ts: 300 }, { lon: 3, lat: 3, ts: 400 }]];
    const fp = SatelliteStateManager._trackFingerprint(past, future);
    assert.ok(fp.includes('1:1'), 'should include segment counts');
    assert.ok(fp.includes('100'), 'should include first past ts');
    assert.ok(fp.includes('200'), 'should include last past ts');
    assert.ok(fp.includes('300'), 'should include first future ts');
    assert.ok(fp.includes('400'), 'should include last future ts');
});

test('fingerprint differs when timestamps shift', () => {
    const past1 = [[{ lon: 0, lat: 0, ts: 100 }, { lon: 1, lat: 1, ts: 200 }]];
    const future1 = [[{ lon: 2, lat: 2, ts: 300 }]];
    const past2 = [[{ lon: 0, lat: 0, ts: 130 }, { lon: 1, lat: 1, ts: 230 }]];
    const future2 = [[{ lon: 2, lat: 2, ts: 330 }]];
    const fp1 = SatelliteStateManager._trackFingerprint(past1, future1);
    const fp2 = SatelliteStateManager._trackFingerprint(past2, future2);
    assert.notStrictEqual(fp1, fp2, 'shifted timestamps must produce different fingerprints');
});

test('fingerprint same for identical data', () => {
    const past = [[{ lon: 0, lat: 0, ts: 100 }]];
    const future = [[{ lon: 2, lat: 2, ts: 300 }]];
    const fp1 = SatelliteStateManager._trackFingerprint(past, future);
    const fp2 = SatelliteStateManager._trackFingerprint(past, future);
    assert.strictEqual(fp1, fp2, 'same data must produce same fingerprint');
});

test('fingerprint handles null/undefined segments', () => {
    const fp = SatelliteStateManager._trackFingerprint(null, undefined);
    assert.strictEqual(fp, '0:0');
});

// ── _cleanupStaleStates ───────────────────────────────────

console.log('\nSatelliteStateManager — _cleanupStaleStates (memory leak fix)');

test('removes satellites not in current group', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData({ norad_id: 11111, name: 'OLD-SAT' }));
    m.updatePosition(makePositionData({ norad_id: 22222, name: 'OLD-SAT-2' }));
    m.updatePosition(makePositionData({ norad_id: 33333, name: 'NEW-SAT' }));
    assert.strictEqual(m.satelliteCount, 3);

    m.setSatelliteGroup({
        primary_id: 33333,
        satellites: [
            { norad_id: 33333, sat_name: 'NEW-SAT', aos: 1000, los: 2000, is_visible: true },
        ],
    });

    assert.strictEqual(m.getState(11111), null, 'OLD-SAT should be cleaned up');
    assert.strictEqual(m.getState(22222), null, 'OLD-SAT-2 should be cleaned up');
    assert.ok(m.getState(33333), 'NEW-SAT should remain');
});

test('preserves selected satellite that IS in group', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData({ norad_id: 11111, name: 'SELECTED' }));
    m.updatePosition(makePositionData({ norad_id: 22222, name: 'IN-GROUP' }));
    m.updatePosition(makePositionData({ norad_id: 33333, name: 'EXTRA' }));
    m.setSelectedSatellite(11111, 'SELECTED', true);

    m.setSatelliteGroup({
        primary_id: 22222,
        satellites: [
            { norad_id: 11111, sat_name: 'SELECTED', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 22222, sat_name: 'IN-GROUP', aos: 1000, los: 2000, is_visible: true },
        ],
    });

    assert.ok(m.getState(11111), 'selected satellite in group should be preserved');
    assert.strictEqual(m.getState(33333), null, 'EXTRA not in group should be cleaned');
});

test('cleans selected satellite that left the group (selection resets to primary)', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData({ norad_id: 11111, name: 'LEFT' }));
    m.updatePosition(makePositionData({ norad_id: 22222, name: 'IN-GROUP' }));
    m.setSelectedSatellite(11111, 'LEFT', true);

    m.setSatelliteGroup({
        primary_id: 22222,
        satellites: [
            { norad_id: 22222, sat_name: 'IN-GROUP', aos: 1000, los: 2000, is_visible: true },
        ],
    });

    // 11111 ушёл из группы → manual selection сброшен → selected = primary (22222).
    // Запись 11111 больше не нужна и должна быть удалена.
    assert.strictEqual(m.getSelectedSatelliteId(), 22222, 'selected should reset to primary');
    assert.strictEqual(m.getState(11111), null, 'left satellite should be cleaned up');
});

test('preserves tracking satellite even if not in group', () => {
    const m = new SatelliteStateManager();
    m.updatePosition(makePositionData({ norad_id: 11111, name: 'TRACKING' }));
    m.updatePosition(makePositionData({ norad_id: 22222, name: 'IN-GROUP' }));
    m.setTrackingSatellite(11111, 'TRACKING');

    m.setSatelliteGroup({
        primary_id: 22222,
        satellites: [
            { norad_id: 22222, sat_name: 'IN-GROUP', aos: 1000, los: 2000, is_visible: true },
        ],
    });

    assert.ok(m.getState(11111), 'tracking satellite state should be preserved');
});

test('cleanup happens on every group update', () => {
    const m = new SatelliteStateManager();
    // Группа 1: три КА.
    m.setSatelliteGroup({
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 300, sat_name: 'C', aos: 1000, los: 2000, is_visible: true },
        ],
    });
    m.updatePosition(makePositionData({ norad_id: 100, name: 'A' }));
    m.updatePosition(makePositionData({ norad_id: 200, name: 'B' }));
    m.updatePosition(makePositionData({ norad_id: 300, name: 'C' }));
    assert.strictEqual(m.satelliteCount, 3);

    // Группа 2: только один КА. Два ушли.
    m.setSatelliteGroup({
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 3000, los: 4000, is_visible: true },
        ],
    });
    assert.strictEqual(m.getState(200), null, 'B should be cleaned');
    assert.strictEqual(m.getState(300), null, 'C should be cleaned');
    assert.ok(m.getState(100), 'A should remain');
});

// ── showAll + ручное отключение трасс (BUG-SHOWALL-RESET) ──

test('setShowAllMode(true) adds all group satellites to visibleTrackIds', () => {
    const m = new SatelliteStateManager();
    m.setSatelliteGroup({
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 300, sat_name: 'C', aos: 1000, los: 2000, is_visible: true },
        ],
    });
    m.setShowAllMode(true);
    const vis = m.getVisibleTrackIds();
    assert.ok(vis.includes(100), 'primary A в наборе — чтобы при смене selected трасса не пропадала');
    assert.ok(vis.includes(200), 'B visible');
    assert.ok(vis.includes(300), 'C visible');
});

test('showAll + смена selected: предыдущий КА остаётся с видимой трассой', () => {
    const m = new SatelliteStateManager();
    m.setSatelliteGroup({
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 1000, los: 2000, is_visible: true },
        ],
    });
    m.setShowAllMode(true);
    m.setSelectedSatellite(100, 'A', true);
    m.setSelectedSatellite(200, 'B', true);
    assert.ok(m.isTrackVisible(100), 'бывший selected A остаётся видимым при включённых всех трассах');
    assert.ok(m.isTrackVisible(200), 'новый selected B видим');
});

test('toggleTrackVisibility OFF in showAll mode: track stays hidden after group update', () => {
    const m = new SatelliteStateManager();
    const group = {
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 300, sat_name: 'C', aos: 1000, los: 2000, is_visible: true },
        ],
    };
    m.setSatelliteGroup(group);
    m.setShowAllMode(true);

    // Оператор вручную скрывает трассу B.
    const result = m.toggleTrackVisibility(200);
    assert.strictEqual(result, false, 'toggle returns false (hidden)');
    assert.ok(!m.getVisibleTrackIds().includes(200), 'B hidden after toggle');

    // Приходит новый group update (каждые 5-10 сек) — B должна остаться скрытой.
    m.setSatelliteGroup(group);
    assert.ok(!m.getVisibleTrackIds().includes(200), 'B still hidden after group update');
    assert.ok(m.getVisibleTrackIds().includes(300), 'C still visible');
});

test('toggleTrackVisibility ON in showAll mode: removes from hidden set', () => {
    const m = new SatelliteStateManager();
    const group = {
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 1000, los: 2000, is_visible: true },
        ],
    };
    m.setSatelliteGroup(group);
    m.setShowAllMode(true);

    // Скрываем, потом снова показываем.
    m.toggleTrackVisibility(200);
    assert.ok(!m.getVisibleTrackIds().includes(200), 'B hidden');
    m.toggleTrackVisibility(200);
    assert.ok(m.getVisibleTrackIds().includes(200), 'B visible again');

    // group update — B должна остаться видимой.
    m.setSatelliteGroup(group);
    assert.ok(m.getVisibleTrackIds().includes(200), 'B still visible after group update');
});

test('setShowAllMode toggles clear hiddenInShowAll', () => {
    const m = new SatelliteStateManager();
    const group = {
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 300, sat_name: 'C', aos: 1000, los: 2000, is_visible: true },
        ],
    };
    m.setSatelliteGroup(group);
    m.setShowAllMode(true);
    m.toggleTrackVisibility(200); // скрыли B

    // Выключаем и заново включаем showAll — скрытый список должен сброситься.
    m.setShowAllMode(false);
    m.setShowAllMode(true);
    assert.ok(m.getVisibleTrackIds().includes(200), 'B visible after re-enable showAll');
    assert.ok(m.getVisibleTrackIds().includes(300), 'C visible');
});

test('hidden satellite that leaves group is cleaned from hiddenInShowAll', () => {
    const m = new SatelliteStateManager();
    m.setSatelliteGroup({
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 300, sat_name: 'C', aos: 1000, los: 2000, is_visible: true },
        ],
    });
    m.setShowAllMode(true);
    m.toggleTrackVisibility(200); // скрыли B

    // B вышел из группы — при возвращении не должен быть скрыт.
    m.setSatelliteGroup({
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 3000, los: 4000, is_visible: true },
            { norad_id: 300, sat_name: 'C', aos: 3000, los: 4000, is_visible: true },
        ],
    });

    // B возвращается обратно.
    m.setSatelliteGroup({
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 5000, los: 6000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 5000, los: 6000, is_visible: true },
            { norad_id: 300, sat_name: 'C', aos: 5000, los: 6000, is_visible: true },
        ],
    });
    assert.ok(m.getVisibleTrackIds().includes(200), 'B visible after re-entering group');
});

test('multiple tracks hidden in showAll — all survive group update', () => {
    const m = new SatelliteStateManager();
    const group = {
        primary_id: 100,
        satellites: [
            { norad_id: 100, sat_name: 'A', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 200, sat_name: 'B', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 300, sat_name: 'C', aos: 1000, los: 2000, is_visible: true },
            { norad_id: 400, sat_name: 'D', aos: 1000, los: 2000, is_visible: true },
        ],
    };
    m.setSatelliteGroup(group);
    m.setShowAllMode(true);

    // Скрываем B и D.
    m.toggleTrackVisibility(200);
    m.toggleTrackVisibility(400);

    // group update.
    m.setSatelliteGroup(group);

    assert.ok(!m.getVisibleTrackIds().includes(200), 'B still hidden');
    assert.ok(!m.getVisibleTrackIds().includes(400), 'D still hidden');
    assert.ok(m.getVisibleTrackIds().includes(300), 'C still visible');
});

// ── Палитра и назначение цветов трасс ─────────────────────

console.log('\nSatelliteStateManager — track color palette');

test('dark palette has 12 unique hues with min separation >= 24°', () => {
    const palette = getTrackColorPalette();
    assert.strictEqual(palette.length, 12);
    const unique = new Set(palette);
    assert.strictEqual(unique.size, 12);
    assert.ok(paletteMinHueSeparation(palette) >= 24,
        `min hue separation ${paletteMinHueSeparation(palette)}°`);
});

test('pickTrackColorFromPalette returns first color when none used', () => {
    const palette = getTrackColorPalette();
    const picked = pickTrackColorFromPalette(new Set(), palette);
    assert.strictEqual(picked, palette[0]);
});

test('pickTrackColorFromPalette maximizes distance from used colors', () => {
    const palette = getTrackColorPalette();
    const used = new Set([palette[0]]);
    const picked = pickTrackColorFromPalette(used, palette);
    let pickedMin = Infinity;
    for (const c of used) {
        pickedMin = Math.min(pickedMin, trackColorDistance(picked, c));
    }
    for (let i = 0; i < palette.length; i++) {
        const cand = palette[i];
        if (used.has(cand)) { continue; }
        let candMin = Infinity;
        for (const c of used) {
            candMin = Math.min(candMin, trackColorDistance(cand, c));
        }
        assert.ok(pickedMin >= candMin - 0.01,
            `suboptimal pick ${picked} vs ${cand}`);
    }
});

test('setSatelliteGroup assigns well-separated colors to 6 satellites', () => {
    const m = new SatelliteStateManager();
    const sats = [];
    for (let i = 0; i < 6; i++) {
        sats.push({ norad_id: 50000 + i, name: `SAT-${i}` });
    }
    m.setSatelliteGroup({ satellites: sats, primary_id: 50000 });
    const colors = sats.map((s) => m.getMarkerColor(s.norad_id));
    assert.strictEqual(new Set(colors).size, 6);
    let minPair = Infinity;
    for (let i = 0; i < colors.length; i++) {
        for (let j = i + 1; j < colors.length; j++) {
            minPair = Math.min(minPair, trackColorDistance(colors[i], colors[j]));
        }
    }
    assert.ok(minPair >= 0.5, `min pairwise distance ${minPair} (ожидаем ≥ одного шага палитры 30°)`);
});

test('setSatelliteGroup keeps stable color for existing satellite', () => {
    const m = new SatelliteStateManager();
    const group = {
        satellites: [{ norad_id: 60001, name: 'A' }, { norad_id: 60002, name: 'B' }],
        primary_id: 60001,
    };
    m.setSatelliteGroup(group);
    const colorA = m.getMarkerColor(60001);
    m.setSatelliteGroup({
        satellites: [
            { norad_id: 60001, name: 'A' },
            { norad_id: 60002, name: 'B' },
            { norad_id: 60003, name: 'C' },
        ],
        primary_id: 60001,
    });
    assert.strictEqual(m.getMarkerColor(60001), colorA);
});

// ── Итоги ─────────────────────────────────────────────────

console.log(`\n${'═'.repeat(50)}`);
console.log(`Results: ${passed} passed, ${failed} failed, ${passed + failed} total`);
console.log('═'.repeat(50));

if (failed > 0) {
    process.exit(1);
}
