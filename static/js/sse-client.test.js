// Тесты для SSEClient (FE-002).
// Запуск: node static/js/sse-client.test.js

'use strict';

// ── Мок EventSource ───────────────────────────────────────

// Эмуляция браузерного EventSource для тестирования в Node.js.
class MockEventSource {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 2;

    constructor(url) {
        this.url = url;
        this.readyState = MockEventSource.CONNECTING;
        this._listeners = {};
        this.onerror = null;

        // Сохраняем последний созданный экземпляр для управления из тестов.
        MockEventSource._lastInstance = this;
        MockEventSource._instanceCount++;
    }

    addEventListener(type, callback) {
        if (!this._listeners[type]) {
            this._listeners[type] = [];
        }
        this._listeners[type].push(callback);
    }

    close() {
        this.readyState = MockEventSource.CLOSED;
    }

    // Вспомогательные методы для тестов.

    /** Эмуляция получения события. */
    _emit(type, data) {
        const listeners = this._listeners[type] || [];
        const event = { data: typeof data === 'string' ? data : JSON.stringify(data) };
        for (const cb of listeners) {
            cb(event);
        }
    }

    /** Эмуляция ошибки с закрытием соединения. */
    _emitErrorClosed() {
        this.readyState = MockEventSource.CLOSED;
        if (this.onerror) this.onerror(new Event('error'));
    }

    /** Эмуляция ошибки с попыткой переподключения браузером. */
    _emitErrorConnecting() {
        this.readyState = MockEventSource.CONNECTING;
        if (this.onerror) this.onerror(new Event('error'));
    }

    /** Эмуляция успешного подключения (open + connected event). */
    _simulateOpen() {
        this.readyState = MockEventSource.OPEN;
        this._emit('connected', { status: 'ok' });
    }

    static _lastInstance = null;
    static _instanceCount = 0;

    static _reset() {
        MockEventSource._lastInstance = null;
        MockEventSource._instanceCount = 0;
    }
}

// Подмена глобального EventSource.
global.EventSource = MockEventSource;

// Подмена Event (для onerror).
if (typeof Event === 'undefined') {
    global.Event = class Event { constructor(type) { this.type = type; } };
}

// ── Загрузка модулей ──────────────────────────────────────

const { SatelliteStateManager } = require('./satellite-state.js');
const { SSEClient, SSEConnectionStatus } = require('./sse-client.js');
const assert = require('assert');

// ── Счётчики тестов ───────────────────────────────────────

let passed = 0;
let failed = 0;
const pendingTimers = [];

function test(name, fn) {
    MockEventSource._reset();
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


// ── Constructor ───────────────────────────────────────────

console.log('\nSSEClient — constructor');

test('requires stateManager', () => {
    assert.throws(() => new SSEClient(null), /stateManager is required/);
    assert.throws(() => new SSEClient(undefined), /stateManager is required/);
});

test('creates with default url', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    assert.strictEqual(client.getStatus(), SSEConnectionStatus.DISCONNECTED);
});

test('creates with custom url', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm, '/custom/sse');
    assert.strictEqual(client._url, '/custom/sse');
});

// ── SSEConnectionStatus ───────────────────────────────────

console.log('\nSSEConnectionStatus');

test('status enum is frozen', () => {
    assert.strictEqual(SSEConnectionStatus.CONNECTED, 'connected');
    assert.strictEqual(SSEConnectionStatus.DISCONNECTED, 'disconnected');
    assert.strictEqual(SSEConnectionStatus.CONNECTING, 'connecting');
    assert.ok(Object.isFrozen(SSEConnectionStatus));
});

// ── connect / disconnect ──────────────────────────────────

console.log('\nSSEClient — connect/disconnect');

test('connect creates EventSource', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    assert.ok(MockEventSource._lastInstance);
    assert.strictEqual(MockEventSource._lastInstance.url, '/api/sse');
    assert.strictEqual(client.getStatus(), SSEConnectionStatus.CONNECTING);
    client.disconnect();
});

test('connect is idempotent', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    const first = MockEventSource._lastInstance;
    client.connect(); // повторный вызов
    assert.strictEqual(MockEventSource._lastInstance, first, 'should not create new EventSource');
    client.disconnect();
});

test('disconnect closes EventSource', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    const es = MockEventSource._lastInstance;
    client.disconnect();
    assert.strictEqual(es.readyState, MockEventSource.CLOSED);
    assert.strictEqual(client.getStatus(), SSEConnectionStatus.DISCONNECTED);
});

test('disconnect is safe when not connected', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.disconnect(); // не должен падать
    assert.strictEqual(client.getStatus(), SSEConnectionStatus.DISCONNECTED);
});

// ── Status change callbacks ───────────────────────────────

console.log('\nSSEClient — status change');

test('onStatusChange fires on connect', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    const statuses = [];
    client.onStatusChange(({ status }) => statuses.push(status));

    client.connect();
    assert.deepStrictEqual(statuses, ['connecting']);

    MockEventSource._lastInstance._simulateOpen();
    assert.deepStrictEqual(statuses, ['connecting', 'connected']);

    client.disconnect();
    assert.deepStrictEqual(statuses, ['connecting', 'connected', 'disconnected']);
});

test('onStatusChange returns false for non-function', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    assert.strictEqual(client.onStatusChange('not a fn'), false);
});

test('offStatusChange removes callback', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    const cb = () => {};
    client.onStatusChange(cb);
    assert.strictEqual(client.offStatusChange(cb), true);
    assert.strictEqual(client.offStatusChange(cb), false);
});

test('status callback error does not break other callbacks', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    let secondCalled = false;

    client.onStatusChange(() => { throw new Error('boom'); });
    client.onStatusChange(() => { secondCalled = true; });

    const origError = console.error;
    console.error = () => {};
    client.connect(); // triggers 'connecting'
    console.error = origError;

    assert.strictEqual(secondCalled, true);
    client.disconnect();
});

// ── Connected event ───────────────────────────────────────

console.log('\nSSEClient — connected event');

test('connected event sets status to connected', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    assert.strictEqual(client.getStatus(), SSEConnectionStatus.CONNECTING);

    MockEventSource._lastInstance._simulateOpen();
    assert.strictEqual(client.getStatus(), SSEConnectionStatus.CONNECTED);
    client.disconnect();
});

// ── Event routing: satellite_state_update (positions) ─────

console.log('\nSSEClient — event routing: satellite_state_update (positions)');

test('satellite_state_update routes positions to stateManager.updatePosition', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emit('satellite_state_update', {
        positions: [{
            norad_id: 25544,
            name: 'ISS',
            lat: 47.3, lon: 39.8, alt: 418,
            az: 215, el: 42, range: 623,
        }],
        tracks_included: false,
        ts: 1738900000000,
    });

    const state = sm.getState(25544);
    assert.ok(state, 'state should exist');
    assert.strictEqual(state.position.lat, 47.3);
    assert.strictEqual(state.position.lon, 39.8);
    assert.strictEqual(state.position.ts, 1738900000000, 'ts from update should be set');
    assert.strictEqual(state.name, 'ISS');
    client.disconnect();
});

test('satellite_state_update with visibility_zone', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emit('satellite_state_update', {
        positions: [{
            norad_id: 25544, name: 'ISS',
            lat: 47.3, lon: 39.8, alt: 418,
            az: 215, el: 42,
            visibility_zone: { points: [{ lon: 20, lat: 30 }], radius_deg: 20.1 },
        }],
        tracks_included: false,
        ts: 1738900000000,
    });

    const state = sm.getState(25544);
    assert.ok(state.visibilityZone);
    assert.strictEqual(state.visibilityZone.radius_deg, 20.1);
    client.disconnect();
});

test('satellite_state_update with multiple positions', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emit('satellite_state_update', {
        positions: [
            { norad_id: 25544, name: 'ISS', lat: 47, lon: 39, alt: 418, az: 215, el: 42 },
            { norad_id: 40069, name: 'METEOR-M2', lat: 55, lon: 37, alt: 820, az: 180, el: 20 },
        ],
        tracks_included: false,
        ts: 100,
    });

    assert.strictEqual(sm.satelliteCount, 2);
    assert.strictEqual(sm.getState(25544).position.lat, 47);
    assert.strictEqual(sm.getState(40069).position.lat, 55);
    client.disconnect();
});

test('satellite_state_update with invalid data is handled gracefully', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    const origWarn = console.warn;
    let warned = false;
    console.warn = (msg) => { if (msg.includes('invalid satellite_state_update')) warned = true; };

    MockEventSource._lastInstance._emit('satellite_state_update', { no_positions: true });

    console.warn = origWarn;
    assert.strictEqual(warned, true, 'should warn about missing positions');
    assert.strictEqual(sm.satelliteCount, 0);
    client.disconnect();
});

// ── Event routing: satellite_state_update (tracks) ────────

console.log('\nSSEClient — event routing: satellite_state_update (tracks)');

test('satellite_state_update with tracks routes to stateManager.updateTrack', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emit('satellite_state_update', {
        positions: [{
            norad_id: 25544, name: 'ISS',
            lat: 47.3, lon: 39.8, alt: 418,
            az: 215, el: 42,
        }],
        tracks: [{
            norad_id: 25544,
            past: [[{ lon: 38, lat: 46, ts: 100 }]],
            future: [[{ lon: 40, lat: 48, ts: 200 }]],
        }],
        tracks_included: true,
        ts: 1738900000000,
    });

    const state = sm.getState(25544);
    assert.ok(state, 'state should exist');
    assert.ok(state.track, 'track should be set');
    assert.strictEqual(state.track.past.length, 1);
    assert.strictEqual(state.track.future.length, 1);
    assert.ok(state.position, 'position should also be set');
    client.disconnect();
});

test('satellite_state_update without tracks_included does not set tracks', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emit('satellite_state_update', {
        positions: [{
            norad_id: 25544, name: 'ISS',
            lat: 47.3, lon: 39.8, alt: 418, az: 215, el: 42,
        }],
        tracks_included: false,
        ts: 100,
    });

    const state = sm.getState(25544);
    assert.ok(state.position, 'position should be set');
    assert.strictEqual(state.track, null, 'track should remain null');
    client.disconnect();
});

// ── Event routing: satellite_change ───────────────────────

console.log('\nSSEClient — event routing: satellite_change');

test('satellite_change event sets active satellite', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emit('satellite_change', { norad_id: 40069 });

    assert.strictEqual(sm.getActiveSatelliteId(), 40069);
    client.disconnect();
});

test('satellite_change with invalid data is ignored', () => {
    const sm = new SatelliteStateManager();
    sm.setActiveSatellite(25544);
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emit('satellite_change', { norad_id: 'invalid' });

    assert.strictEqual(sm.getActiveSatelliteId(), 25544, 'should not change');
    client.disconnect();
});

// ── Invalid JSON handling ─────────────────────────────────

console.log('\nSSEClient — invalid JSON');

test('invalid JSON in event is handled gracefully', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    const origError = console.error;
    let errorLogged = false;
    console.error = (msg) => { if (msg.includes('failed to parse')) errorLogged = true; };

    // Отправляем сырую невалидную строку.
    const listeners = MockEventSource._lastInstance._listeners['satellite_state_update'] || [];
    for (const cb of listeners) {
        cb({ data: '{invalid json' });
    }

    console.error = origError;

    assert.strictEqual(errorLogged, true, 'should log parse error');
    assert.strictEqual(sm.satelliteCount, 0, 'state should not be updated');
    client.disconnect();
});

// ── Error handling and reconnect ──────────────────────────

console.log('\nSSEClient — error handling');

test('error with CLOSED state sets disconnected and schedules reconnect', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emitErrorClosed();

    assert.strictEqual(client.getStatus(), SSEConnectionStatus.DISCONNECTED);
    assert.ok(client._reconnectTimer, 'reconnect should be scheduled');

    // Очистка.
    client.disconnect();
});

test('error with CONNECTING state sets connecting status', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emitErrorConnecting();

    assert.strictEqual(client.getStatus(), SSEConnectionStatus.CONNECTING);
    client.disconnect();
});

test('manual disconnect prevents reconnect', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    client.disconnect();

    assert.strictEqual(client._reconnectTimer, null, 'no reconnect timer');
    assert.strictEqual(client.getStatus(), SSEConnectionStatus.DISCONNECTED);
});

// ── Reconnect with backoff ────────────────────────────────

console.log('\nSSEClient — reconnect backoff');

test('reconnect delay doubles after each error', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    // Начальная задержка.
    assert.strictEqual(client._reconnectDelay, 1000);

    // Первая ошибка: delay увеличивается с 1000 до 2000.
    MockEventSource._lastInstance._emitErrorClosed();
    assert.strictEqual(client._reconnectDelay, 2000, 'delay should double to 2000');
    assert.ok(client._reconnectTimer, 'reconnect timer should be set');

    // Имитируем: таймер сработал — очищаем и вызываем reconnect вручную.
    clearTimeout(client._reconnectTimer);
    client._reconnectTimer = null;
    client._createEventSource();

    // Вторая ошибка: delay увеличивается с 2000 до 4000.
    MockEventSource._lastInstance._emitErrorClosed();
    assert.strictEqual(client._reconnectDelay, 4000, 'delay should double to 4000');

    // Третья ошибка: delay увеличивается с 4000 до 8000.
    clearTimeout(client._reconnectTimer);
    client._reconnectTimer = null;
    client._createEventSource();
    MockEventSource._lastInstance._emitErrorClosed();
    assert.strictEqual(client._reconnectDelay, 8000, 'delay should double to 8000');

    client.disconnect();
});

test('successful reconnect resets delay', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    // Ошибка — увеличиваем backoff.
    MockEventSource._lastInstance._emitErrorClosed();
    assert.strictEqual(client._reconnectDelay, 2000);

    // Имитируем reconnect.
    clearTimeout(client._reconnectTimer);
    client._reconnectTimer = null;
    client._createEventSource();

    // Успешное подключение — delay сбрасывается.
    MockEventSource._lastInstance._simulateOpen();
    assert.strictEqual(client._reconnectDelay, 1000, 'delay should reset to min');
    assert.strictEqual(client.getStatus(), SSEConnectionStatus.CONNECTED);

    client.disconnect();
});

test('max delay is capped at RECONNECT_MAX_DELAY', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);

    // Искусственно выставляем большую задержку.
    client._reconnectDelay = 20000;
    client.connect();
    MockEventSource._lastInstance._emitErrorClosed();

    assert.ok(client._reconnectDelay <= SSEClient.RECONNECT_MAX_DELAY,
        `delay ${client._reconnectDelay} should be <= ${SSEClient.RECONNECT_MAX_DELAY}`);

    client.disconnect();
});

test('scheduleReconnect is idempotent (no double timers)', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emitErrorClosed();
    const firstTimer = client._reconnectTimer;
    assert.ok(firstTimer, 'reconnect timer should be set');

    // Попытка повторного reconnect (не должна перезаписать таймер).
    client._scheduleReconnect();
    assert.strictEqual(client._reconnectTimer, firstTimer, 'timer should not change');

    client.disconnect();
});

// ── Multiple events in sequence ───────────────────────────

console.log('\nSSEClient — sequence of events');

test('full data flow: positions + tracks in one event', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    // Групповое событие с позициями и треками.
    MockEventSource._lastInstance._emit('satellite_state_update', {
        positions: [{
            norad_id: 25544, name: 'ISS',
            lat: 47.3, lon: 39.8, alt: 418,
            az: 215, el: 42,
        }],
        tracks: [{
            norad_id: 25544,
            past: [[{ lon: 38, lat: 46, ts: 90 }]],
            future: [[{ lon: 40, lat: 48, ts: 110 }]],
        }],
        tracks_included: true,
        ts: 100,
    });

    const state = sm.getState(25544);
    assert.ok(state.position);
    assert.ok(state.track);
    assert.strictEqual(state.position.lat, 47.3);
    assert.strictEqual(state.position.ts, 100, 'ts from group event');
    assert.strictEqual(state.track.past[0][0].lon, 38);
    assert.strictEqual(sm.getActiveSatelliteId(), 25544);

    client.disconnect();
});

test('multiple satellites in one satellite_state_update', () => {
    const sm = new SatelliteStateManager();
    const client = new SSEClient(sm);
    client.connect();
    MockEventSource._lastInstance._simulateOpen();

    MockEventSource._lastInstance._emit('satellite_state_update', {
        positions: [
            { norad_id: 25544, name: 'ISS', lat: 47, lon: 39, alt: 418, az: 215, el: 42 },
            { norad_id: 40069, name: 'METEOR-M2', lat: 55, lon: 37, alt: 820, az: 180, el: 20 },
        ],
        tracks_included: false,
        ts: 100,
    });

    assert.strictEqual(sm.satelliteCount, 2);
    assert.strictEqual(sm.getActiveSatelliteId(), 25544, 'first satellite is active');
    assert.strictEqual(sm.getState(40069).name, 'METEOR-M2');

    client.disconnect();
});

// ── Итоги ─────────────────────────────────────────────────

console.log(`\n${'═'.repeat(50)}`);
console.log(`Results: ${passed} passed, ${failed} failed, ${passed + failed} total`);
console.log('═'.repeat(50));

if (failed > 0) {
    process.exit(1);
}
