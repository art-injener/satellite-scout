/**
 * Unit tests for the TLE freshness indicator in the header.
 *
 * Run: node static/js/tle-status.test.js
 */

'use strict';

const assert = require('assert');
const { describeTLEStatus, formatTLEAge } = require('./tle-status.js');

let passed = 0;
function test(name, fn) {
    fn();
    passed++;
    console.log('  ok ' + name);
}

test('formatTLEAge shows hours under a day', () => {
    assert.strictEqual(formatTLEAge(5.4), '5 ч');
});

test('formatTLEAge shows days with comma from a day', () => {
    assert.strictEqual(formatTLEAge(41), '1,7 сут');
});

test('formatTLEAge returns dash for missing value', () => {
    assert.strictEqual(formatTLEAge(undefined), '—');
});

test('ok status shows median age and source', () => {
    const view = describeTLEStatus({
        level: 'ok', reasons: [], count: 196, stale_count: 0,
        median_age_h: 20, source: 'celestrak', fetched_at: '2026-11-12T08:00:00Z',
    });
    assert.strictEqual(view.level, 'ok');
    assert.strictEqual(view.text, 'TLE 20 ч');
    assert.ok(view.title.includes('Celestrak'), view.title);
    assert.ok(view.title.includes('196'), view.title);
});

test('warn status explains reasons and ISS age', () => {
    const view = describeTLEStatus({
        level: 'warn', reasons: ['iss_age', 'network'], count: 196, stale_count: 2,
        median_age_h: 41, iss_age_h: 38, source: 'cache', last_error: 'timeout',
    });
    assert.strictEqual(view.level, 'warn');
    assert.strictEqual(view.text, 'TLE 1,7 сут');
    assert.ok(view.title.includes('МКС: 1,6 сут'), view.title);
    assert.ok(view.title.includes('кеш'), view.title);
    assert.ok(view.title.includes('timeout'), view.title);
    assert.ok(view.title.includes('Орбита МКС устарела'), view.title);
    assert.ok(view.title.includes('из сети'), view.title);
});

test('empty catalog is stale without age', () => {
    const view = describeTLEStatus({ level: 'stale', reasons: ['empty'], count: 0, median_age_h: 0 });
    assert.strictEqual(view.level, 'stale');
    assert.strictEqual(view.text, 'TLE нет');
});

test('pass check shows drift, ISS drift and pair count', () => {
    const view = describeTLEStatus({
        level: 'warn', reasons: ['pass_drift'], count: 196, median_age_h: 20,
        pass_check: { source: 'heavens-above', matched: 14, median_drift_s: 95, iss_drift_s: 8 },
    });
    assert.ok(view.title.includes('Пролёты расходятся с Heavens-Above'), view.title);
    assert.ok(view.title.includes('Сверка с Heavens-Above: сдвиг 1,6 мин, МКС 8 с (14 пролётов)'), view.title);
});

test('missing pass check says it was not done', () => {
    const view = describeTLEStatus({ level: 'ok', reasons: [], count: 196, median_age_h: 20 });
    assert.ok(view.title.includes('Сверка с Heavens-Above не проводилась'), view.title);
});

test('unknown level falls back to unknown', () => {
    const view = describeTLEStatus(null);
    assert.strictEqual(view.level, 'unknown');
    assert.strictEqual(view.text, 'TLE …');
});

test('loading status shows batch progress over empty catalog', () => {
    const view = describeTLEStatus({
        level: 'stale', reasons: ['empty'], count: 0, loading: true,
        load_phase: 'gp', load_done: 3, load_total: 12,
    });
    assert.strictEqual(view.level, 'loading');
    assert.strictEqual(view.text, 'TLE 3/12…');
    assert.ok(view.title.includes('орбиты (GP)'), view.title);
    assert.ok(view.title.includes('3 из 12'), view.title);
});

test('loading satcat phase uses SATCAT label', () => {
    const view = describeTLEStatus({
        level: 'ok', count: 100, loading: true,
        load_phase: 'satcat', load_done: 1, load_total: 4,
    });
    assert.strictEqual(view.text, 'TLE 1/4…');
    assert.ok(view.title.includes('SATCAT'), view.title);
});

console.log(`tle-status: ${passed} passed`);
