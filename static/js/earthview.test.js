// Тесты для EarthView — масштабирование карты, проекция, антимеридианные пороги
// и phased-анимация с «карандашной» прорисовкой контуров.
// Запуск: node static/js/earthview.test.js

'use strict';

const assert = require('assert');

// ── Минимальные DOM/CSS моки до require earthview.js ─────────────────────────
global.window = global.window || { devicePixelRatio: 1, addEventListener: function() {} };
global.document = global.document || { getElementById: function() { return null; } };
global.cssVar = function(_name, fallback) { return fallback || ''; };
global.themeRgba = function(_key, fallback) { return fallback || ''; };
global.getThemeId = function() { return 'classic'; };
global.requestAnimationFrame = function(cb) { return setTimeout(cb, 0); };
global.cancelAnimationFrame = function(id) { clearTimeout(id); };

const { EarthView, MAP_ZOOM_LEVELS } = require('./earthview.js');

// ── Утилиты тестов ───────────────────────────────────────────────────────────

let passed = 0;
let failed = 0;

function test(name, fn) {
    try {
        fn();
        passed++;
        console.log(`  \u2713 ${name}`);
    } catch (err) {
        failed++;
        console.error(`  \u2717 ${name}`);
        console.error(`    ${err.message}`);
        if (err.stack) {
            console.error('    ' + err.stack.split('\n').slice(1, 3).join('\n    '));
        }
    }
}

function approx(a, b, tol) {
    tol = tol == null ? 1e-6 : tol;
    return Math.abs(a - b) <= tol;
}

function makeCanvas(w, h) {
    const noopFn = function() {};
    const ctx = new Proxy({}, {
        get: function(_t, _prop) { return noopFn; },
        set: function() { return true; }
    });
    return {
        width: w, height: h, clientWidth: w, clientHeight: h,
        getContext: function() { return ctx; },
        addEventListener: function() {},
        parentElement: null
    };
}

/**
 * Создать EarthView с замоканной отрисовкой.
 * draw() считает число вызовов и не делает реальной работы.
 */
function makeEv(opts) {
    const canvas = makeCanvas(800, 400);
    const ev = new EarthView(canvas, opts || {});
    ev._drawCount = 0;
    ev._drawStaticCount = 0;
    ev.draw = function() { ev._drawCount++; };
    ev._drawStatic = function() { ev._drawStaticCount++; };
    return ev;
}

// ── MAP_ZOOM_LEVELS ──────────────────────────────────────────────────────────

console.log('\nEarthView: MAP_ZOOM_LEVELS');

test('MAP_ZOOM_LEVELS содержит 4 ступени [1.0, 1.5, 2.5, 4.0]', () => {
    assert.deepStrictEqual(MAP_ZOOM_LEVELS, [1.0, 1.5, 2.5, 4.0]);
});

// ── Базовые поля состояния ───────────────────────────────────────────────────

console.log('\nEarthView: state defaults');

test('После конструктора zoom=1.0, _zoomIdx=0, center=(0,0)', () => {
    const ev = makeEv();
    assert.strictEqual(ev.zoom, 1.0);
    assert.strictEqual(ev._zoomIdx, 0);
    assert.deepStrictEqual(ev.center, { lon: 0, lat: 0 });
    assert.strictEqual(ev._zoomAnim, null);
});

test('options.animStyle по умолчанию = "phased"', () => {
    const ev = makeEv();
    assert.strictEqual(ev.options.animStyle, 'phased');
});

test('options.animStyle можно переопределить через конструктор', () => {
    const ev = makeEv({ animStyle: 'instant' });
    assert.strictEqual(ev.options.animStyle, 'instant');
});

test('options.zoomAnimDurationMs по умолчанию = 1500', () => {
    const ev = makeEv();
    assert.strictEqual(ev.options.zoomAnimDurationMs, 1500);
});

test('options.zoomAnimDurationMs можно переопределить через конструктор', () => {
    const ev = makeEv({ zoomAnimDurationMs: 1200 });
    assert.strictEqual(ev.options.zoomAnimDurationMs, 1200);
});

// ── project / unproject ──────────────────────────────────────────────────────

console.log('\nEarthView: project/unproject (zoom=1.0, center=(0,0))');

test('project(0, 0) = (W/2, H/2) при zoom=1.0, center=(0,0)', () => {
    const ev = makeEv();
    const p = ev.project(0, 0);
    assert.ok(approx(p.x, 400), 'x=' + p.x);
    assert.ok(approx(p.y, 200), 'y=' + p.y);
});

test('project(180, 90): антимеридиан попадает на край (x=0 или W) при zoom=1.0', () => {
    const ev = makeEv();
    const p = ev.project(180, 90);
    // Нормализация Δlon в [-180, 180) уводит lon=180 в dLon=-180 → x=0.
    // На сфере x=0 и x=W — это один и тот же меридиан.
    assert.ok(approx(p.x, 0) || approx(p.x, 800), 'x=' + p.x);
    assert.ok(approx(p.y, 0), 'y=' + p.y);
});

test('project(-180, -90) = (0, H) при zoom=1.0, center=(0,0)', () => {
    const ev = makeEv();
    const p = ev.project(-180, -90);
    assert.ok(approx(p.x, 0) || approx(p.x, 800), 'x=' + p.x);
    assert.ok(approx(p.y, 400), 'y=' + p.y);
});

test('round-trip project/unproject на zoom=1.0', () => {
    const ev = makeEv();
    const samples = [
        { lon: 0, lat: 0 },
        { lon: 37.62, lat: 55.75 },
        { lon: -74.01, lat: 40.71 },
        { lon: 139.69, lat: 35.69 },
        { lon: 90, lat: -45 }
    ];
    for (let i = 0; i < samples.length; i++) {
        const s = samples[i];
        const p = ev.project(s.lon, s.lat);
        const u = ev.unproject(p.x, p.y);
        assert.ok(approx(u.lon, s.lon, 1e-5), `lon ${s.lon} → ${u.lon}`);
        assert.ok(approx(u.lat, s.lat, 1e-5), `lat ${s.lat} → ${u.lat}`);
    }
});

console.log('\nEarthView: project/unproject (zoom>1, центрирование по X и Y)');

test('zoom=1: X центрируется на center.lon, Y по экватору (полный диапазон ±90)', () => {
    const ev = makeEv();
    ev.center = { lon: 37.62, lat: 55.75 };
    ev.zoom = 1.0;
    // На долготе центра X должен быть в середине канваса.
    const p0 = ev.project(37.62, 0);
    assert.ok(approx(p0.x, 400, 1e-3), `x(center.lon)=${p0.x}`);
    // На zoom=1 Y не зависит от center.lat: lat=0 → y=H/2.
    assert.ok(approx(p0.y, 200, 1e-3), `y(lat=0)=${p0.y}`);
    // Полный диапазон ±90 на полную высоту независимо от center.lat.
    assert.ok(approx(ev.project(0,  90).y,   0, 1e-3), `top y(90)=${ev.project(0, 90).y}`);
    assert.ok(approx(ev.project(0, -90).y, 400, 1e-3), `bot y(-90)=${ev.project(0,-90).y}`);
});

test('zoom>1 с center.lat=0: X центрируется на center.lon, Y масштабируется в zoom раз', () => {
    const ev = makeEv();
    ev.center = { lon: 37.62, lat: 0 };
    [1.5, 2.5, 4.0].forEach(function(z) {
        ev.zoom = z;
        const pc = ev.project(37.62, 0);
        assert.ok(approx(pc.x, 400, 1e-3), `zoom=${z} x(center.lon)=${pc.x}`);
        assert.ok(approx(pc.y, 200, 1e-3), `zoom=${z} y(center.lat=0)=${pc.y}`);
        const dy = (10 / 90) * 200 * z;
        const pUp = ev.project(37.62, 10);
        assert.ok(approx(pUp.y, 200 - dy, 1e-3),
            `zoom=${z} y(lat=10)=${pUp.y}, expected ${200 - dy}`);
    });
});

test('zoom>1: на больших zoom полюса уходят за пределы канваса', () => {
    const ev = makeEv();
    ev.center = { lon: 0, lat: 0 };
    ev.zoom = 2.0;
    // На zoom=2 lat=90 → y = H/2 - (90/90)*(H/2)*2 = H/2 - H = -H/2 (за верхним краем).
    assert.ok(ev.project(0, 90).y < 0, `зум=2: y(90)=${ev.project(0,90).y} должен быть < 0`);
    assert.ok(ev.project(0, -90).y > 400, `зум=2: y(-90)=${ev.project(0,-90).y} должен быть > H`);
});

test('_effectiveCenterLat: zoom=1 → 0; zoom>1 → clamp в [-90·(1-1/z), +90·(1-1/z)]', () => {
    const ev = makeEv();
    ev.zoom = 1.0;
    ev.center = { lon: 0, lat: 80 };
    assert.strictEqual(ev._effectiveCenterLat(), 0, 'zoom=1 → cy=0');

    ev.zoom = 1.5;
    assert.ok(approx(ev._effectiveCenterLat(), 30, 1e-6),
        `zoom=1.5 cy(80)=${ev._effectiveCenterLat()} (макс 30°)`);

    ev.zoom = 2.5;
    ev.center = { lon: 0, lat: 55.75 };
    assert.ok(approx(ev._effectiveCenterLat(), 54, 1e-6),
        `zoom=2.5 cy(55.75)=${ev._effectiveCenterLat()} (макс 54°)`);

    ev.zoom = 4.0;
    ev.center = { lon: 0, lat: 55.75 };
    assert.ok(approx(ev._effectiveCenterLat(), 55.75, 1e-6),
        `zoom=4 cy(55.75)=${ev._effectiveCenterLat()} (макс 67.5°, не клампится)`);

    ev.zoom = 1.5;
    ev.center = { lon: 0, lat: -45 };
    assert.ok(approx(ev._effectiveCenterLat(), -30, 1e-6),
        `zoom=1.5 cy(-45)=${ev._effectiveCenterLat()} (мин -30°)`);
});

test('clamp: при zoom>1 карта всегда полностью покрывает canvas по высоте', () => {
    const ev = makeEv();
    [1.5, 2.5, 4.0].forEach(function(z) {
        // Намеренно «уходящий» центр в Арктику.
        ev.center = { lon: 0, lat: 89 };
        ev.zoom = z;
        const yTop = ev.project(0,  90).y;
        const yBot = ev.project(0, -90).y;
        assert.ok(yTop <= 0 + 1e-6, `zoom=${z} верх (lat=90) y=${yTop} должен быть ≤ 0`);
        assert.ok(yBot >= 400 - 1e-6, `zoom=${z} низ (lat=-90) y=${yBot} должен быть ≥ H`);
    });
});

test('round-trip project/unproject при zoom=2.5, center=Moscow', () => {
    const ev = makeEv();
    ev.center = { lon: 37.62, lat: 55.75 };
    ev.zoom = 2.5;
    const samples = [
        { lon: 37.62, lat: 55.75 },     // центр
        { lon: 30, lat: 60 },           // близко к центру
        { lon: 50, lat: 40 },
        { lon: -10, lat: 30 }
    ];
    for (let i = 0; i < samples.length; i++) {
        const s = samples[i];
        const p = ev.project(s.lon, s.lat);
        const u = ev.unproject(p.x, p.y);
        assert.ok(approx(u.lon, s.lon, 1e-4), `lon ${s.lon} → ${u.lon}`);
        assert.ok(approx(u.lat, s.lat, 1e-4), `lat ${s.lat} → ${u.lat}`);
    }
});

test('project нормализует Δlon в [-180, 180): антимеридиан слева', () => {
    const ev = makeEv();
    ev.center = { lon: 170, lat: 0 };
    ev.zoom = 1.0;
    // lon = -170 (далеко на западе на globe), но Δlon = -340 → +20 → справа от центра
    const p = ev.project(-170, 0);
    assert.ok(p.x > 400, 'expected x > 400, got ' + p.x);
});

// ── _antimeridianThreshold ───────────────────────────────────────────────────

console.log('\nEarthView: _antimeridianThreshold');

test('threshold = width*zoom/2 для всех уровней', () => {
    const ev = makeEv();
    [1.0, 1.5, 2.5, 4.0].forEach(function(z) {
        ev.zoom = z;
        assert.strictEqual(ev._antimeridianThreshold(), 800 * z / 2);
    });
});

// ── _isInViewport ────────────────────────────────────────────────────────────

console.log('\nEarthView: _isInViewport');

test('точка внутри прямоугольника — true', () => {
    const ev = makeEv();
    assert.strictEqual(ev._isInViewport({ x: 100, y: 100 }), true);
});

test('точка вне прямоугольника — false', () => {
    const ev = makeEv();
    assert.strictEqual(ev._isInViewport({ x: -10, y: 100 }), false);
    assert.strictEqual(ev._isInViewport({ x: 900, y: 100 }), false);
    assert.strictEqual(ev._isInViewport({ x: 100, y: -10 }), false);
    assert.strictEqual(ev._isInViewport({ x: 100, y: 500 }), false);
});

test('padding учитывается', () => {
    const ev = makeEv();
    assert.strictEqual(ev._isInViewport({ x: -5, y: 100 }, 10), true);
    assert.strictEqual(ev._isInViewport({ x: -15, y: 100 }, 10), false);
});

test('null/undefined — false', () => {
    const ev = makeEv();
    assert.strictEqual(ev._isInViewport(null), false);
    assert.strictEqual(ev._isInViewport(undefined), false);
});

// ── API zoom (instant) ───────────────────────────────────────────────────────

console.log('\nEarthView: zoom API (instant mode)');

test('setZoomLevel(2): _zoomIdx=2, zoom=2.5, draw вызывается', () => {
    const ev = makeEv({ animStyle: 'instant' });
    const changed = ev.setZoomLevel(2);
    assert.strictEqual(changed, true);
    assert.strictEqual(ev._zoomIdx, 2);
    assert.strictEqual(ev.zoom, 2.5);
    assert.strictEqual(ev._drawCount, 1);
});

test('setZoomLevel(currentIdx): без изменений и без draw', () => {
    const ev = makeEv({ animStyle: 'instant' });
    ev._drawCount = 0;
    const changed = ev.setZoomLevel(0);
    assert.strictEqual(changed, false);
    assert.strictEqual(ev._drawCount, 0);
});

test('setZoomLevel(-1) и setZoomLevel(99) клампятся к [0, len-1]', () => {
    const ev = makeEv({ animStyle: 'instant' });
    ev.setZoomLevel(99);
    assert.strictEqual(ev._zoomIdx, MAP_ZOOM_LEVELS.length - 1);
    ev.setZoomLevel(-5);
    assert.strictEqual(ev._zoomIdx, 0);
});

test('zoomIn и zoomOut шагают по уровням', () => {
    const ev = makeEv({ animStyle: 'instant' });
    assert.strictEqual(ev._zoomIdx, 0);
    ev.zoomIn();
    assert.strictEqual(ev._zoomIdx, 1);
    ev.zoomIn();
    assert.strictEqual(ev._zoomIdx, 2);
    ev.zoomOut();
    assert.strictEqual(ev._zoomIdx, 1);
});

test('zoomIn на максимуме — не уходит за границу', () => {
    const ev = makeEv({ animStyle: 'instant' });
    ev.setZoomLevel(MAP_ZOOM_LEVELS.length - 1);
    const before = ev._zoomIdx;
    const changed = ev.zoomIn();
    assert.strictEqual(changed, false);
    assert.strictEqual(ev._zoomIdx, before);
});

test('zoomOut на минимуме — не уходит за границу', () => {
    const ev = makeEv({ animStyle: 'instant' });
    const changed = ev.zoomOut();
    assert.strictEqual(changed, false);
    assert.strictEqual(ev._zoomIdx, 0);
});

test('resetView возвращает zoom к 1.0 и центр на наблюдателя', () => {
    const ev = makeEv({ animStyle: 'instant' });
    ev.setObserver(37.62, 55.75, 'Москва');
    ev.setZoomLevel(3);
    assert.strictEqual(ev.zoom, 4.0);
    ev.resetView();
    assert.strictEqual(ev.zoom, 1.0);
    assert.deepStrictEqual(ev.center, { lon: 37.62, lat: 55.75 });
});

test('resetView без наблюдателя — центр (0,0)', () => {
    const ev = makeEv({ animStyle: 'instant' });
    ev.setZoomLevel(2);
    ev.resetView();
    assert.deepStrictEqual(ev.center, { lon: 0, lat: 0 });
});

test('setObserver обновляет center', () => {
    const ev = makeEv({ animStyle: 'instant' });
    ev.setObserver(-74.01, 40.71, 'NY');
    assert.deepStrictEqual(ev.center, { lon: -74.01, lat: 40.71 });
});

test('onZoomChange колбэк получает (zoom, idx, total)', () => {
    const ev = makeEv({ animStyle: 'instant' });
    const calls = [];
    ev.onZoomChange = function(z, idx, total) { calls.push([z, idx, total]); };
    ev.setZoomLevel(2);
    assert.strictEqual(calls.length, 1);
    assert.deepStrictEqual(calls[0], [2.5, 2, 4]);
});

test('getZoom / getZoomLevel возвращают актуальные значения', () => {
    const ev = makeEv({ animStyle: 'instant' });
    ev.setZoomLevel(1);
    assert.strictEqual(ev.getZoom(), 1.5);
    assert.strictEqual(ev.getZoomLevel(), 1);
});

// ── Phased animation ─────────────────────────────────────────────────────────

console.log('\nEarthView: phased animation');

test('setZoomLevel в animStyle:"phased" запускает _zoomAnim', () => {
    const ev = makeEv({ animStyle: 'phased' });
    // Подменяем _startZoomAnim, чтобы не запускать цикл rAF.
    ev._startZoomAnim = function(fromIdx, toIdx) {
        ev._zoomAnim = { startTs: 0, fromIdx: fromIdx, toIdx: toIdx, raf: null };
    };
    ev.setZoomLevel(2);
    assert.notStrictEqual(ev._zoomAnim, null);
    assert.strictEqual(ev._zoomAnim.fromIdx, 0);
    assert.strictEqual(ev._zoomAnim.toIdx, 2);
});

test('_drawPhased посередине длительности не сбрасывает _zoomAnim', () => {
    const ev = makeEv({ animStyle: 'phased', zoomAnimDurationMs: 2000 });
    ev._zoomAnim = { startTs: 1000, fromIdx: 0, toIdx: 2, raf: null };
    ev._now = function() { return 1000 + 1000; }; // 50% пути
    ev._drawPhased(ev._now());
    assert.notStrictEqual(ev._zoomAnim, null);
});

test('_drawPhased по достижении полной длительности сбрасывает _zoomAnim и зовёт _drawStatic', () => {
    const ev = makeEv({ animStyle: 'phased', zoomAnimDurationMs: 2000 });
    ev._zoomAnim = { startTs: 1000, fromIdx: 0, toIdx: 2, raf: null };
    ev._drawStaticCount = 0;
    ev._now = function() { return 1000 + 2050; }; // > zoomAnimDurationMs
    ev._drawPhased(ev._now());
    assert.strictEqual(ev._zoomAnim, null);
    assert.ok(ev._drawStaticCount >= 1, '_drawStatic должен был быть вызван');
});

test('_drawPhased уважает кастомный zoomAnimDurationMs', () => {
    const ev = makeEv({ animStyle: 'phased', zoomAnimDurationMs: 500 });
    ev._zoomAnim = { startTs: 0, fromIdx: 0, toIdx: 2, raf: null };
    // На 300 мс при duration=500 ещё не финал.
    ev._drawPhased(300);
    assert.notStrictEqual(ev._zoomAnim, null);
    // На 600 мс при duration=500 анимация уже завершена.
    ev._zoomAnim = { startTs: 0, fromIdx: 0, toIdx: 2, raf: null };
    ev._drawStaticCount = 0;
    ev._drawPhased(600);
    assert.strictEqual(ev._zoomAnim, null);
    assert.ok(ev._drawStaticCount >= 1, '_drawStatic должен был быть вызван');
});

test('PHASED_STAGES — 6 стадий, coast рисуется до land (контур → заливка)', () => {
    assert.strictEqual(EarthView.PHASED_STAGES.length, 6);
    const keys = EarthView.PHASED_STAGES.map(function(s) { return s.key; });
    assert.deepStrictEqual(keys, ['clear', 'grid', 'coast', 'land', 'observer', 'dynamic']);
    // Стадия coast должна иметь ненулевую длительность (это и есть «карандаш»).
    const coast = EarthView.PHASED_STAGES.find(function(s) { return s.key === 'coast'; });
    assert.ok(coast.endFrac > coast.startFrac, 'coast должна иметь длительность > 0');
    assert.ok(coast.endFrac - coast.startFrac >= 0.5, 'coast занимает основную часть времени');
    // Заливка континентов появляется ПОСЛЕ окончания «карандаша».
    const land = EarthView.PHASED_STAGES.find(function(s) { return s.key === 'land'; });
    assert.ok(land.startFrac >= coast.endFrac,
        `land.startFrac (${land.startFrac}) должна быть ≥ coast.endFrac (${coast.endFrac})`);
});

test('_drawPhased до стадии observer скрывает DOM-маркеры и слой выносок', () => {
    const ev = makeEv({ animStyle: 'phased', zoomAnimDurationMs: 2000 });
    // Мокаем DOM: контейнер выносок и DOM-маркеры selected/tracking.
    const elements = {
        'map-callouts':              { style: { visibility: '' } },
        'map-sat-tracking':          { style: {}, querySelector: () => null },
        'map-sat-tracking-label':    { style: {}, textContent: '' },
        'map-sat-selected':          { style: {}, querySelector: () => null },
        'map-sat-selected-label':    { style: {}, textContent: '' }
    };
    const prevGetElement = global.document.getElementById;
    global.document.getElementById = function(id) { return elements[id] || null; };
    try {
        ev._zoomAnim = { startTs: 0, fromIdx: 0, toIdx: 2, raf: null };
        // 50% пути — observer ещё не наступил (startFrac=0.85).
        ev._drawPhased(1000);
        assert.strictEqual(elements['map-callouts'].style.visibility, 'hidden',
            'слой выносок должен быть скрыт во время анимации');
        assert.strictEqual(elements['map-sat-tracking'].style.display, 'none',
            'tracking-маркер должен быть скрыт во время анимации');
        assert.strictEqual(elements['map-sat-selected'].style.display, 'none',
            'selected-маркер должен быть скрыт во время анимации');
    } finally {
        global.document.getElementById = prevGetElement;
    }
});

test('_drawPhased на стадии observer возвращает видимость слоя выносок и зовёт _drawStatic', () => {
    const ev = makeEv({ animStyle: 'phased', zoomAnimDurationMs: 2000 });
    const elements = {
        'map-callouts': { style: { visibility: 'hidden' } }
    };
    const prevGetElement = global.document.getElementById;
    global.document.getElementById = function(id) { return elements[id] || null; };
    let drewWithVisibleCallouts = false;
    ev._drawStatic = function() {
        drewWithVisibleCallouts = (elements['map-callouts'].style.visibility === '');
    };
    try {
        ev._zoomAnim = { startTs: 0, fromIdx: 0, toIdx: 2, raf: null };
        // 90% — observer уже наступил (startFrac=0.85).
        ev._drawPhased(1800);
        assert.strictEqual(elements['map-callouts'].style.visibility, '',
            'слой выносок должен быть видим на стадии observer');
        assert.ok(drewWithVisibleCallouts,
            '_drawStatic должна вызываться при уже видимом слое');
    } finally {
        global.document.getElementById = prevGetElement;
    }
});

test('animStyle:"instant" не запускает _zoomAnim', () => {
    const ev = makeEv({ animStyle: 'instant' });
    ev.setZoomLevel(2);
    assert.strictEqual(ev._zoomAnim, null);
});

// ── Pencil-режим _drawLineString ────────────────────────────────────────────

console.log('\nEarthView: _drawLineString (pencil progress)');

/**
 * Записываем все вызовы canvas-API в массив, чтобы посчитать сколько lineTo
 * было сделано — это и есть «сколько сегментов нарисовано».
 */
function makeRecordingEv() {
    const calls = [];
    const handler = {
        get: function(_t, prop) {
            return function() {
                calls.push({ name: prop, args: Array.prototype.slice.call(arguments) });
            };
        },
        set: function() { return true; }
    };
    const ctx = new Proxy({}, handler);
    const canvas = {
        width: 800, height: 400, clientWidth: 800, clientHeight: 400,
        getContext: function() { return ctx; },
        addEventListener: function() {},
        parentElement: null
    };
    const ev = new EarthView(canvas, { animStyle: 'instant' });
    ev._drawCount = 0;
    ev.draw = function() { ev._drawCount++; };
    ev.__ctxCalls = calls;
    return ev;
}

test('progress=undefined рисует все сегменты (старое поведение)', () => {
    const ev = makeRecordingEv();
    const coords = [[0, 0], [10, 10], [20, 20], [30, 30], [40, 40]]; // 4 сегмента
    ev.__ctxCalls.length = 0;
    ev._drawLineString(coords);
    const lineTos = ev.__ctxCalls.filter(function(c) { return c.name === 'lineTo'; });
    assert.strictEqual(lineTos.length, 4);
});

test('progress=0.5 рисует примерно половину сегментов', () => {
    const ev = makeRecordingEv();
    const coords = [];
    for (let i = 0; i <= 100; i++) { coords.push([i * 0.5, 0]); } // 100 сегментов
    ev.__ctxCalls.length = 0;
    ev._drawLineString(coords, 0.5);
    const lineTos = ev.__ctxCalls.filter(function(c) { return c.name === 'lineTo'; });
    // 50 целых сегментов; tail при 0.5 кратном — 0 (50.0 → tail=0).
    assert.ok(lineTos.length >= 49 && lineTos.length <= 51, 'lineTo=' + lineTos.length);
});

test('progress=0 не рисует ни одного сегмента', () => {
    const ev = makeRecordingEv();
    const coords = [[0, 0], [10, 10], [20, 20]];
    ev.__ctxCalls.length = 0;
    ev._drawLineString(coords, 0);
    const lineTos = ev.__ctxCalls.filter(function(c) { return c.name === 'lineTo'; });
    assert.strictEqual(lineTos.length, 0);
});

test('progress=1 эквивалентен полной отрисовке', () => {
    const ev = makeRecordingEv();
    const coords = [[0, 0], [10, 10], [20, 20], [30, 30]];
    ev.__ctxCalls.length = 0;
    ev._drawLineString(coords, 1);
    const lineTos = ev.__ctxCalls.filter(function(c) { return c.name === 'lineTo'; });
    assert.strictEqual(lineTos.length, 3);
});

test('progress дробный → дорисовывается «кончик карандаша»', () => {
    const ev = makeRecordingEv();
    // 4 точки = 3 сегмента; progress=0.5 → exact=1.5 → 1 целый сегмент + tail=0.5
    const coords = [[0, 0], [10, 0], [20, 0], [30, 0]];
    ev.__ctxCalls.length = 0;
    ev._drawLineString(coords, 0.5);
    const lineTos = ev.__ctxCalls.filter(function(c) { return c.name === 'lineTo'; });
    // Один целый lineTo + один частичный = 2.
    assert.strictEqual(lineTos.length, 2);
    // Последний lineTo — это интерполяция к середине второго сегмента.
    const last = lineTos[lineTos.length - 1];
    // x — между project(10,0).x и project(20,0).x.
    const a = ev.project(10, 0);
    const b = ev.project(20, 0);
    const expectedX = a.x + (b.x - a.x) * 0.5;
    assert.ok(Math.abs(last.args[0] - expectedX) < 0.01, 'tail x=' + last.args[0]);
});

// ── Итоги ────────────────────────────────────────────────────────────────────

console.log(`\nResults: ${passed} passed, ${failed} failed\n`);
process.exit(failed > 0 ? 1 : 0);
