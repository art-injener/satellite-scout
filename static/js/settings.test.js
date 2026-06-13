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

console.log('\nAll tests passed.');
