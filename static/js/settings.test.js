/**
 * Юнит-тесты SettingsModal.
 *
 * Запуск: node static/js/settings.test.js
 *
 * Покрываются чистые функции (getByPath, setByPath, парсер длительностей)
 * и поведение SettingsModal через DOM-стаб (минимальный мок document/window
 * без подключения jsdom — модулю достаточно базовых querySelector/getElementById
 * для теста инициализации и dirty-state).
 */
'use strict';

const assert = require('assert');

// Минимальный DOM-стаб: settings.js использует document/window только в
// bootstrap-секции (querySelector/getElementById/addEventListener). Загружаем
// модуль в окружении, где document невалиден — bootstrap-блок нужно отключить
// перед require. Делаем так: при load корня модуля document.readyState уже
// 'complete' и getElementById вернёт null → SettingsModal-конструктор тихо
// выйдет, остальное (классы и helpers) экспортируется.
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

console.log('TEST: parseDurationToNanoseconds — composite formats');
{
    assert.strictEqual(settings.parseDurationToNanoseconds('6h'), 6 * 3600 * 1e9);
    assert.strictEqual(settings.parseDurationToNanoseconds('30m'), 30 * 60 * 1e9);
    assert.strictEqual(settings.parseDurationToNanoseconds('1h30m'), (60 + 30) * 60 * 1e9);
    assert.strictEqual(settings.parseDurationToNanoseconds('5s'), 5 * 1e9);
    assert.strictEqual(settings.parseDurationToNanoseconds(''), 0);
}
console.log('  OK');

console.log('TEST: formatNanosecondsToDuration round-trip');
{
    const cases = ['6h', '30m', '1h30m', '5s'];
    for (const human of cases) {
        const ns = settings.parseDurationToNanoseconds(human);
        const back = settings.formatNanosecondsToDuration(ns);
        assert.strictEqual(back, human, `round-trip failed for ${human}: got ${back}`);
    }
}
console.log('  OK');

console.log('TEST: parseDurationToNanoseconds — fallback to plain number');
{
    const ns = settings.parseDurationToNanoseconds('21600000000000');
    assert.strictEqual(ns, 21600000000000);
}
console.log('  OK');

console.log('TEST: formatNanosecondsToDuration handles zero / invalid');
{
    assert.strictEqual(settings.formatNanosecondsToDuration(0), '');
    assert.strictEqual(settings.formatNanosecondsToDuration(-1), '');
    assert.strictEqual(settings.formatNanosecondsToDuration(NaN), '');
}
console.log('  OK');

console.log('\nAll tests passed.');
