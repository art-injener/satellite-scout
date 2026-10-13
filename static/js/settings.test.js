/**
 * Юнит-тесты SettingsModal.
 *
 * Запуск: node static/js/settings.test.js
 *
 * Покрываются чистые функции (getByPath, setByPath) и поведение SettingsModal
 * через DOM-стаб (минимальный мок document/window без подключения jsdom).
 */
'use strict';

const assert = require('assert');

global.document = {
    readyState: 'complete',
    addEventListener: () => {},
    getElementById: () => null,
    querySelector: () => null
};
global.window = {
    location: { search: '', href: 'http://test/' },
    history: { replaceState: () => {} },
    addEventListener: () => {}
};

const settings = require('./settings.js');

console.log('TEST: getByPath returns nested values');
{
    const obj = { a: { b: { c: 42 } } };
    assert.strictEqual(settings.getByPath(obj, 'a.b.c'), 42);
    assert.strictEqual(settings.getByPath(obj, 'a.b'), obj.a.b);
    assert.strictEqual(settings.getByPath(obj, 'missing.path'), undefined);
}
console.log('  OK');

console.log('TEST: setByPath creates intermediate objects');
{
    const obj = {};
    settings.setByPath(obj, 'station.observer.lat', 47.31);
    assert.strictEqual(obj.station.observer.lat, 47.31);
    settings.setByPath(obj, 'station.observer.lon', 39.78);
    assert.strictEqual(obj.station.observer.lon, 39.78);
    assert.strictEqual(obj.station.observer.lat, 47.31, 'lat must persist after second set');
}
console.log('  OK');

console.log('TEST: antenna kind is mandatory without a rotator');
{
    const values = settings.antennaKindOptions('stationary').map((o) => o[0]);
    assert.deepStrictEqual(values, ['qfh', 'yagi', 'dipole', 'whip']);
}
console.log('  OK');

console.log('TEST: antenna kind may stay empty on a rotator');
{
    const values = settings.antennaKindOptions('rotatable').map((o) => o[0]);
    assert.deepStrictEqual(values, ['', 'qfh', 'yagi', 'dipole', 'whip']);
    settings.antennaKindOptions('rotatable').forEach((o) => assert.ok(o[1], 'every option has a label'));
}
console.log('  OK');

function receiverModal(receiver) {
    const modal = Object.create(settings.SettingsModal.prototype);
    modal._workingConfig = { station: { radio_paths: [{ id: 1, receiver: receiver }] } };
    modal._sdrDevices = [{ driver: 'rtlsdr', serial: '0002', device_path: '', label: 'RTL-SDR v4' }];
    modal._setDirty = () => {};
    return modal;
}

console.log('TEST: receiver selection keeps sample rate for the same driver');
{
    const modal = receiverModal({ driver: 'rtlsdr', serial: '0001', sample_rate_hz: 1024000 });
    modal._applyReceiverSelection(0, 'rtlsdr|0002|');
    const rx = modal._workingConfig.station.radio_paths[0].receiver;
    assert.strictEqual(rx.serial, '0002');
    assert.strictEqual(rx.sample_rate_hz, 1024000);
}
console.log('  OK');

console.log('TEST: receiver selection drops sample rate when driver changes');
{
    const modal = receiverModal({ driver: 'simulated', sample_rate_hz: 48000 });
    modal._applyReceiverSelection(0, 'rtlsdr|0002|');
    const rx = modal._workingConfig.station.radio_paths[0].receiver;
    assert.strictEqual(rx.driver, 'rtlsdr');
    assert.strictEqual(rx.sample_rate_hz, undefined);
}
console.log('  OK');

console.log('TEST: receiver selection keeps calibration for the same driver');
{
    const modal = receiverModal({ driver: 'rtlsdr', serial: '0001', calibration_db: -23.5 });
    modal._applyReceiverSelection(0, 'rtlsdr|0002|');
    assert.strictEqual(modal._workingConfig.station.radio_paths[0].receiver.calibration_db, -23.5);
}
console.log('  OK');

console.log('TEST: receiver selection drops calibration when driver changes');
{
    const modal = receiverModal({ driver: 'simulated', calibration_db: -40 });
    modal._applyReceiverSelection(0, 'rtlsdr|0002|');
    assert.strictEqual(modal._workingConfig.station.radio_paths[0].receiver.calibration_db, undefined);
}
console.log('  OK');

console.log('TEST: reselecting the imitator keeps its signals section');
{
    const imitator = { noise_dbfs: -60, signals: [{ kind: 'carrier', freq_hz: 436500000, level_dbfs: -40 }] };
    const modal = receiverModal({ driver: 'simulated', imitator: imitator });
    modal._applyReceiverSelection(0, 'simulated');
    assert.deepStrictEqual(modal._workingConfig.station.radio_paths[0].receiver.imitator, imitator);
}
console.log('  OK');

console.log('TEST: switching from the imitator to hardware drops its signals section');
{
    const modal = receiverModal({ driver: 'simulated', imitator: { noise_dbfs: -60 } });
    modal._applyReceiverSelection(0, 'rtlsdr|0002|');
    assert.strictEqual(modal._workingConfig.station.radio_paths[0].receiver.imitator, undefined);
}
console.log('  OK');

console.log('TEST: save refused with 403 explains that only the master may change settings');
global.fetch = () => Promise.resolve({
    ok: false,
    status: 403,
    text: () => Promise.resolve('forbidden: master only'),
    json: () => Promise.resolve({ error: 'forbidden' }),
});
const modal403 = Object.create(settings.SettingsModal.prototype);
modal403._workingConfig = { station: {} };
modal403._clearFieldErrors = () => {};
modal403._setDirty = () => { throw new Error('must stay dirty: nothing was saved'); };
const statuses403 = [];
modal403._setStatus = (text, kind) => statuses403.push([text, kind]);
modal403._save().then(() => {
    assert.strictEqual(statuses403.length, 1);
    assert.strictEqual(statuses403[0][1], 'error');
    assert.ok(/паролю станции/.test(statuses403[0][0]), statuses403[0][0]);
    assert.ok(!/мастер/i.test(statuses403[0][0]), 'role names stay out of the UI');
    assert.ok(!/HTTP 403/.test(statuses403[0][0]), 'no raw HTTP code for a known case');
    console.log('  OK');
    console.log('\nAll tests passed.');
}).catch((err) => {
    console.error(err);
    process.exit(1);
});
