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
    assert.ok(approx(ev.project(0, 90).y, 0, 1e-3), `top y(90)=${ev.project(0, 90).y}`);
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
        const yTop = ev.project(0, 90).y;
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
        { lon: 37.62, lat: 55.75 }, // центр
        { lon: 30, lat: 60 }, // близко к центру
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

// ── _bridgePastFuture: защита от «палки через всю карту» ─────────────────────

console.log('\nEarthView: _bridgePastFuture');

test('bridge: нормальное соседство (Δlon~3°, Δts=30s) — past_last↔future_first склеиваются', () => {
    const ev = makeEv();
    const past = [[
        { lon: 5, lat: 30, ts: 1000000 },
        { lon: 7, lat: 31, ts: 1030000 },
    ]];
    const future = [[
        { lon: 10, lat: 32, ts: 1060000 },
        { lon: 13, lat: 33, ts: 1090000 },
    ]];
    const out = ev._bridgePastFuture(past, future);
    assert.strictEqual(out.length, 1, 'один сегмент в past');
    assert.strictEqual(out[0].length, 3, 'last past дополнен first future');
    assert.strictEqual(out[0][2].lon, 10, 'добавлена именно first future-точка');
});

test('bridge: разрыв по lon >30° — bridge НЕ делается (антипалка)', () => {
    const ev = makeEv();
    // past содержит только boundary-точку (КА вылетел за границу окна на первом шаге).
    const past = [[
        { lon: -139.99, lat: 37, ts: 1000000 },
    ]];
    const future = [[
        // nowPoint далеко от boundary — палка пошла бы через половину карты.
        { lon: 10, lat: 37, ts: 1030000 },
        { lon: 13, lat: 37, ts: 1060000 },
    ]];
    const out = ev._bridgePastFuture(past, future);
    assert.strictEqual(out.length, 1, 'past остался как был');
    assert.strictEqual(out[0].length, 1, 'last past НЕ дополнен — палка не строится');
});

test('bridge: разрыв по времени >120 с — bridge НЕ делается', () => {
    const ev = makeEv();
    const past = [[
        { lon: 5, lat: 30, ts: 1000000 },
        { lon: 7, lat: 31, ts: 1030000 },
    ]];
    const future = [[
        // gap 5 минут — например, между сегментами потеряны точки.
        { lon: 10, lat: 32, ts: 1330000 },
    ]];
    const out = ev._bridgePastFuture(past, future);
    assert.strictEqual(out[0].length, 2, 'past не дополнен при больших Δts');
});

test('bridge: переход через ±180° (Δlon учитывает антимеридиан) — короткий гэп склеивается', () => {
    const ev = makeEv();
    const past = [[
        { lon: 178, lat: 0, ts: 1000000 },
    ]];
    const future = [[
        // raw lon=-179°, физически рядом с +178° (Δlon=3° через ±180°).
        { lon: -179, lat: 0, ts: 1030000 },
    ]];
    const out = ev._bridgePastFuture(past, future);
    assert.strictEqual(out[0].length, 2, 'переход через ±180° не считается «палкой»');
});

test('bridge: пустой past возвращает входной массив без модификаций', () => {
    const ev = makeEv();
    const past = [];
    assert.strictEqual(ev._bridgePastFuture(past, [[{ lon: 0, lat: 0, ts: 0 }]]), past);
});

test('bridge: null past возвращает null', () => {
    const ev = makeEv();
    assert.strictEqual(ev._bridgePastFuture(null, [[{ lon: 0, lat: 0, ts: 0 }]]), null);
});

test('bridge: пустой future возвращает past как есть', () => {
    const ev = makeEv();
    const past = [[{ lon: 0, lat: 0, ts: 0 }]];
    assert.strictEqual(ev._bridgePastFuture(past, []), past);
});

test('bridge: разрыв по широте >15° — bridge НЕ делается (антидиагональ через карту)', () => {
    const ev = makeEv();
    // past_last на низкой широте, future_first — высоко (после потерянных
    // точек прохождения полюса). Δlon мал, Δts мал → старая защита пропускала.
    const past = [[
        { lon: 5, lat: 20, ts: 1000000 },
    ]];
    const future = [[
        { lon: 10, lat: 80, ts: 1030000 },
    ]];
    const out = ev._bridgePastFuture(past, future);
    assert.strictEqual(out[0].length, 1, 'past не дополнен при Δlat>15°');
});

// ── _drawTrackSegment: разрыв линии при |Δlon|>30° внутри сегмента ───────────

console.log('\nEarthView: _drawTrackSegment lon-jump break');

/**
 * Записываем moveTo/lineTo, чтобы посчитать число sub-path'ов.
 * Для линии без разрыва lineTo вызывается N-1 раз, moveTo — 1 раз.
 * При разрыве на каком-то шаге появляется лишний moveTo.
 */
function makeRecordingTrackEv() {
    const calls = [];
    const handler = {
        get: function(_t, prop) {
            if (prop === 'beginPath' || prop === 'stroke' || prop === 'fill' ||
                prop === 'closePath' || prop === 'save' || prop === 'restore' ||
                prop === 'arc' || prop === 'fillRect' ||
                prop === 'strokeRect' || prop === 'clip') {
                return function() { calls.push({ op: prop, args: [] }); };
            }
            if (prop === 'setLineDash') {
                return function(pattern) { calls.push({ op: prop, args: [pattern] }); };
            }
            if (prop === 'moveTo' || prop === 'lineTo') {
                return function(x, y) { calls.push({ op: prop, args: [x, y] }); };
            }
            if (prop === 'rect') {
                return function(x, y, w, h) { calls.push({ op: prop, args: [x, y, w, h] }); };
            }
            if (prop === 'measureText') {
                return function() { return { width: 0 }; };
            }
            return undefined;
        },
        set: function(_t, prop, value) {
            if (prop === 'strokeStyle' || prop === 'fillStyle') {
                calls.push({ op: prop, args: [value] });
            }
            return true;
        }
    };
    const ctx = new Proxy({}, handler);
    const canvas = {
        width: 1024, height: 512, clientWidth: 1024, clientHeight: 512,
        getContext: function() { return ctx; },
        addEventListener: function() {}, parentElement: null
    };
    const ev = new EarthView(canvas, { animStyle: 'instant' });
    ev._drawCount = 0;
    ev._mapTrackLineWidth = 1;
    return { ev: ev, calls: calls };
}

test('drawTrackSegment: соседние точки с Δlon=150° → разрыв (новый moveTo)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Сегмент с «дырой»: две точки разделены Δlon=150° на одной широте.
    // Без новой защиты: |Δx|≈427 < threshold(512), разрыва нет → палка через
    // полкарты. С защитой по Δlon — два moveTo.
    const seg = [
        { lon: -120, lat: 50, ts: 1000000 },
        { lon: 30, lat: 50, ts: 1030000 },
    ];
    ev._drawTrackSegment(seg, '#ff0');
    const moveTos = calls.filter(c => c.op === 'moveTo');
    assert.strictEqual(moveTos.length, 2, 'должно быть 2 moveTo (разрыв)');
});

test('drawTrackSegment: нормальный шаг Δlon=4° → без разрыва (один moveTo)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    const seg = [
        { lon: 10, lat: 50, ts: 1000000 },
        { lon: 14, lat: 51, ts: 1030000 },
        { lon: 18, lat: 52, ts: 1060000 },
    ];
    ev._drawTrackSegment(seg, '#ff0');
    const moveTos = calls.filter(c => c.op === 'moveTo');
    assert.strictEqual(moveTos.length, 1, 'один moveTo на сегмент');
});

test('drawTrackSegment: соседние точки с Δlat=40° → разрыв (антидиагональ через полюс)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Δlon=2°, Δlat=45° — pixel-check проходит, lon-check проходит, но
    // физически нереально (LEO ~6.5°/мин). lat-check должен разорвать.
    const seg = [
        { lon: 30, lat: 40, ts: 1000000 },
        { lon: 32, lat: 85, ts: 1030000 },
    ];
    ev._drawTrackSegment(seg, '#ff0');
    const moveTos = calls.filter(c => c.op === 'moveTo');
    assert.strictEqual(moveTos.length, 2, 'разрыв по Δlat>15°');
});

test('drawTrackSegment: нормальный шаг Δlat=2° → без разрыва', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    const seg = [
        { lon: 30, lat: 50, ts: 1000000 },
        { lon: 32, lat: 52, ts: 1030000 },
        { lon: 34, lat: 54, ts: 1060000 },
    ];
    ev._drawTrackSegment(seg, '#ff0');
    const moveTos = calls.filter(c => c.op === 'moveTo');
    assert.strictEqual(moveTos.length, 1, 'нормальный Δlat — без разрыва');
});

test('drawZoneRing: кольцо целиком в окне → fill (минимум одна заливка, видимая в canvas)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Кольцо вокруг Москвы, целиком внутри окна.
    const ring = [
        { lon: 40, lat: 60 }, { lon: 50, lat: 55 }, { lon: 55, lat: 47 },
        { lon: 50, lat: 39 }, { lon: 40, lat: 34 }, { lon: 30, lat: 39 },
        { lon: 25, lat: 47 }, { lon: 30, lat: 55 },
    ];
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const fills = calls.filter(c => c.op === 'fill');
    assert.ok(fills.length >= 1, 'кольцо в окне должно иметь заливку');
    // Bbox видимой копии не должен охватывать половину canvas.
    const lineTos = calls.filter(c => c.op === 'lineTo');
    const visibleXs = lineTos
        .map(c => c.args[0])
        .filter(x => x >= 0 && x <= ev.width);
    if (visibleXs.length > 0) {
        const span = Math.max.apply(null, visibleXs) - Math.min.apply(null, visibleXs);
        assert.ok(span < ev.width * 0.5, 'span видимой копии меньше половины canvas, реально=' + span);
    }
});

test('drawZoneRing: кольцо в «обёрнутой» части окна (observerLon=+40, центр lon=-160°) → fill есть и компактный (duplicate-shift)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Кольцо вокруг lon=-160°: dLonRaw = -200..-220 (за левым краем canvas).
    // drawOnce(0) viewport-фильтром пропускается, drawOnce(+wz) рисует
    // компактный полигон в правой части canvas — заливка должна быть.
    const ring = [
        { lon: -150, lat: 0 }, { lon: -155, lat: 5 }, { lon: -160, lat: 7 },
        { lon: -165, lat: 5 }, { lon: -170, lat: 0 }, { lon: -165, lat: -5 },
        { lon: -160, lat: -7 }, { lon: -155, lat: -5 },
    ];
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const fills = calls.filter(c => c.op === 'fill');
    assert.ok(fills.length >= 1, 'fill для кольца в обёрнутой части окна');
    const lineTos = calls.filter(c => c.op === 'lineTo');
    const visibleXs = lineTos
        .map(c => c.args[0])
        .filter(x => x >= 0 && x <= ev.width);
    assert.ok(visibleXs.length >= 3, 'есть видимые точки');
    const span = Math.max.apply(null, visibleXs) - Math.min.apply(null, visibleXs);
    assert.ok(span < ev.width * 0.4, 'компактная видимая копия, span=' + span);
});

test('drawZoneRing: кольцо ровно на шве окна (observerLon=+40, центр lon=-140°) → fill есть, без палки через ВСЮ карту', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Кольцо ровно на антимеридиане окна — визуально разрывается на две дуги
    // у противоположных краёв canvas. Нам важно: (а) fill вызывается;
    // (б) НИ В ОДНОЙ из copy полигон не строится через всю карту —
    // т.е. в каждом sub-path (между beginPath/closePath) span x ≤ 40% canvas.
    const ring = [
        { lon: -130, lat: 0 }, { lon: -135, lat: 5 }, { lon: -140, lat: 7 },
        { lon: -145, lat: 5 }, { lon: -150, lat: 0 }, { lon: -145, lat: -5 },
        { lon: -140, lat: -7 }, { lon: -135, lat: -5 },
    ];
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const fills = calls.filter(c => c.op === 'fill');
    assert.ok(fills.length >= 1, 'fill вызван');
    // Разбиваем calls на sub-path между beginPath. Считаем span x в каждом.
    const subSpans = [];
    let curXs = null;
    for (const c of calls) {
        if (c.op === 'beginPath') { curXs = []; continue; }
        if (curXs && (c.op === 'moveTo' || c.op === 'lineTo')) {
            curXs.push(c.args[0]);
        }
        if (c.op === 'closePath' || c.op === 'stroke' || c.op === 'fill') {
            if (curXs && curXs.length >= 2) {
                subSpans.push(Math.max.apply(null, curXs) - Math.min.apply(null, curXs));
            }
            curXs = [];
        }
    }
    for (const s of subSpans) {
        assert.ok(s < ev.width * 0.5, 'sub-path не растянут через полкарты, span=' + s);
    }
});

test('drawZoneRing: open-arc после backend split — fill через дальний край canvas, без диагонали', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Имитируем сегмент после splitZoneAtAntimeridian: открытая дуга,
    // first.lon=+178°, last.lon=-178°. После seam-fix continuous-проекция
    // даёт большой |closeDx| > wz/2 — старый код тянул диагональ закрытия
    // не на одной высоте, теперь должно замыкаться через дальний край canvas.
    const ring = [
        { lon: 178, lat: 60 },
        { lon: 179, lat: 65 },
        { lon: 180, lat: 70 },
        { lon: -179, lat: 75 },
        { lon: -178, lat: 80 },
    ];
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const fills = calls.filter(c => c.op === 'fill');
    assert.ok(fills.length >= 1, 'fill для open-arc');
    // Проверка: ни один sub-path не «растягивается» через всю карту по x
    // (как раз случай прежней диагональной линии замыкания).
    const subSpans = [];
    let curXs = null;
    for (const c of calls) {
        if (c.op === 'beginPath') { curXs = []; continue; }
        if (curXs && (c.op === 'moveTo' || c.op === 'lineTo')) {
            const x = c.args[0];
            if (x >= 0 && x <= ev.width) { curXs.push(x); }
        }
        if (c.op === 'closePath' || c.op === 'stroke' || c.op === 'fill') {
            if (curXs && curXs.length >= 2) {
                subSpans.push(Math.max.apply(null, curXs) - Math.min.apply(null, curXs));
            }
            curXs = [];
        }
    }
    for (const s of subSpans) {
        assert.ok(s < ev.width * 0.6, 'видимая часть sub-path < 60% canvas, span=' + s);
    }
});

test('drawZoneRing: footprint, пересекающий полюс — заливка через верхний край canvas, без densify полюса', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Footprint вокруг полюса: точки от bearing 0..360, верхушка — на полюсе
    // (lat>85). Нижняя дуга идёт через средние широты. Алгоритм должен:
    // 1) заменить полярный блок на пары точек у y<0 (за верх canvas);
    // 2) оставить нижнюю дугу как есть; 3) полигон замыкается через clip.
    const ring = [
        { lon: 30, lat: 88 }, { lon: 60, lat: 88 }, { lon: 90, lat: 87 },
        { lon: 120, lat: 75 }, { lon: 150, lat: 70 }, { lon: 180, lat: 65 },
        { lon: -150, lat: 65 }, { lon: -120, lat: 65 }, { lon: -90, lat: 70 },
        { lon: -60, lat: 75 }, { lon: -30, lat: 87 }, { lon: 0, lat: 88 },
    ];
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const fills = calls.filter(c => c.op === 'fill');
    assert.ok(fills.length >= 1, 'fill должен быть');
    // Ищем хотя бы одну точку с y<0 (заменённая полярная точка) — это
    // подтверждает, что верхушка ушла «за верх canvas».
    const lineTos = calls.filter(c => c.op === 'lineTo' || c.op === 'moveTo');
    const hasAboveTop = lineTos.some(c => c.args[1] < 0);
    assert.ok(hasAboveTop, 'есть точка с y<0 — полярный блок заменён на верхний край');
});

test('drawZoneRing: малый polar footprint (lat=60..70°, lonSpread≈360°) — детектится как polar и заливается через top edge', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 0, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Footprint, охватывающий полюс с малым radius: на equirectangular
    // точки распределены по всей долготе с lat=60..70° (отражение через
    // полюс). НИ ОДНОЙ точки lat>85° нет, но lonSpread≈360° — это
    // признак polar-pass и нужна ветка envelope+top-edge.
    const ring = [];
    for (let lon = -180; lon < 180; lon += 15) {
        const lat = 65 + 5 * Math.cos((lon + 180) * Math.PI / 180);
        ring.push({ lon: lon, lat: lat });
    }
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const lineTos = calls.filter(c => c.op === 'lineTo' || c.op === 'moveTo');
    const hasAboveTop = lineTos.some(c => c.args[1] < 0);
    assert.ok(hasAboveTop, 'малый polar footprint должен использовать top-edge replacement (есть точка y<0)');
});

test('drawZoneRing: polar footprint — stroke рисуется ТОЛЬКО по envelope (без замыкающих диагоналей к y<0)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 0, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Polar footprint: lat=60..70°, lonSpread=360°.
    const ring = [];
    for (let lon = -180; lon < 180; lon += 15) {
        const lat = 65 + 5 * Math.cos((lon + 180) * Math.PI / 180);
        ring.push({ lon: lon, lat: lat });
    }
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    // Разбиваем calls на sub-path'ы (между beginPath). Хотя бы один sub-path
    // должен быть «stroke-only»: ВСЕ его lineTo/moveTo иметь y>=0 (нижняя
    // огибающая) и завершаться вызовом stroke. Это гарантирует, что stroke
    // НЕ рисует диагонали к (corner_x, edgeY=-10).
    let strokeOnlyCleanPath = false;
    let curPath = null;
    for (const c of calls) {
        if (c.op === 'beginPath') { curPath = { ys: [], hasStroke: false, hasFill: false }; continue; }
        if (curPath && (c.op === 'moveTo' || c.op === 'lineTo')) {
            curPath.ys.push(c.args[1]);
        }
        if (c.op === 'stroke' && curPath) { curPath.hasStroke = true; }
        if (c.op === 'fill' && curPath) { curPath.hasFill = true; }
        if ((c.op === 'stroke' || c.op === 'fill') && curPath) {
            // Проверяем path при каждом fill/stroke — мы на pre-restore стадии.
            if (curPath.hasStroke && !curPath.hasFill) {
                const allPositive = curPath.ys.every(y => y >= 0);
                if (allPositive && curPath.ys.length >= 2) { strokeOnlyCleanPath = true; }
            }
        }
    }
    assert.ok(strokeOnlyCleanPath, 'должен быть stroke-only sub-path с y>=0 (envelope без замыкающих диагоналей)');
});

test('drawZoneRing: узкая дуга через ±180° (lon=178..-178, малый Δlon) — НЕ polar, обычная ветка', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Узкая footprint-дуга ровно через антимеридиан, lonSpread по wrap = ~4°.
    // Без circular-spread детект мог бы ложно определить как polar
    // (max-min=359°). Должна попасть в обычную ветку: НЕТ y<0 точек.
    const ring = [
        { lon: 178, lat: 60 }, { lon: 179, lat: 65 }, { lon: 180, lat: 70 },
        { lon: -179, lat: 75 }, { lon: -178, lat: 80 },
    ];
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const lineTos = calls.filter(c => c.op === 'lineTo' || c.op === 'moveTo');
    const hasAboveTop = lineTos.some(c => c.args[1] < 0);
    assert.ok(!hasAboveTop, 'узкая дуга НЕ должна попадать в polar-ветку (нет точек y<0)');
});

test('drawZoneRing: footprint без полюса — нет точек с y<0', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 40, lat: 47, name: 'Rostov' };
    ev._syncCenterToObserver();
    // Footprint в средних широтах (max lat=60) — полярный блок отсутствует,
    // классическая отрисовка через closePath без edge-замены.
    const ring = [
        { lon: 40, lat: 60 }, { lon: 50, lat: 55 }, { lon: 55, lat: 47 },
        { lon: 50, lat: 39 }, { lon: 40, lat: 34 }, { lon: 30, lat: 39 },
        { lon: 25, lat: 47 }, { lon: 30, lat: 55 },
    ];
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const lineTos = calls.filter(c => c.op === 'lineTo' || c.op === 'moveTo');
    const hasAboveTop = lineTos.some(c => c.args[1] < 0);
    assert.ok(!hasAboveTop, 'нет точек выше canvas, polar replacement не применился');
});

test('drawZoneRing: clip-region выставлен на canvas (save/restore + clip)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 0, lat: 0, name: 'gw' };
    ev._syncCenterToObserver();
    const ring = [
        { lon: 0, lat: 5 }, { lon: 5, lat: 0 }, { lon: 0, lat: -5 }, { lon: -5, lat: 0 },
    ];
    ev._drawZoneRing(ring, '#0ff', '#0ff', 1);
    const saves = calls.filter(c => c.op === 'save');
    const restores = calls.filter(c => c.op === 'restore');
    assert.ok(saves.length >= 1 && restores.length >= 1, 'save/restore вокруг clip');
});

test('drawTrackSegment: переход через ±180° короткий (Δlon=3°) → без палки', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 0, lat: 0, name: 'gw' };
    ev._syncCenterToObserver();
    // center=0, антимеридиан окна = ±180° = край canvas. Точки физически рядом
    // (Δlon=3°), но raw lon перескакивает через ±180° → проекция даёт |Δx|≈large
    // → пиксельная защита разрывает (это корректно, антимеридианный шов canvas).
    // Защита по Δlon (3°<30°) разрыв НЕ добавляет — пиксельная справится сама.
    const seg = [
        { lon: 178, lat: 0, ts: 1000000 },
        { lon: -179, lat: 0, ts: 1030000 },
    ];
    ev._drawTrackSegment(seg, '#ff0');
    const moveTos = calls.filter(c => c.op === 'moveTo');
    assert.strictEqual(moveTos.length, 2, 'разрыв по пиксельной защите на краю canvas');
});

// ── _drawSelectedLayer: двухцветная трасса past=red / future=yellow ──────────

console.log('\nEarthView: _drawSelectedLayer two-color past/future');

test('drawSelectedLayer: past и future рисуются одним цветом selectedTrack (различаются стилем линии)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 0, lat: 0, name: 'gw' };
    ev._syncCenterToObserver();
    ev.colors.orbitPast = '#FF0000';
    ev.colors.selectedTrack = '#FFFF00';
    ev._selectedSatellite = {
        position: null, name: 'TEST', noradId: 99999,
        visibilityZone: null,
        groundTrack: {
            past: [[
                { lon: 10, lat: 30, ts: 1000000 },
                { lon: 15, lat: 32, ts: 1060000 },
                { lon: 20, lat: 34, ts: 1120000 },
            ]],
            future: [[
                { lon: 25, lat: 36, ts: 1180000 },
                { lon: 30, lat: 38, ts: 1240000 },
                { lon: 35, lat: 40, ts: 1300000 },
            ]],
        },
    };
    ev._drawSelectedLayer();

    const colors = calls.filter(c => c.op === 'strokeStyle').map(c => c.args[0]);
    assert.ok(colors.length >= 2, 'минимум 2 stroke (past и future), получено: ' + colors.length);
    assert.ok(!colors.includes('#FF0000'), 'orbitPast НЕ должен использоваться (теперь оба сегмента — selectedTrack)');
    assert.ok(colors.every(c => c === '#FFFF00'), 'все strokeStyle = selectedTrack: ' + colors.join(','));
});

test('drawSelectedLayer: только past → используется selectedTrack (без orbitPast)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 0, lat: 0, name: 'gw' };
    ev._syncCenterToObserver();
    ev.colors.orbitPast = '#FF0000';
    ev.colors.selectedTrack = '#FFFF00';
    ev._selectedSatellite = {
        position: null, name: 'TEST', noradId: 99999,
        visibilityZone: null,
        groundTrack: {
            past: [[
                { lon: 10, lat: 30, ts: 1000000 },
                { lon: 15, lat: 32, ts: 1060000 },
            ]],
            future: [],
        },
    };
    ev._drawSelectedLayer();

    const colors = calls.filter(c => c.op === 'strokeStyle').map(c => c.args[0]);
    assert.ok(colors.includes('#FFFF00'), 'past рисуется selectedTrack');
    assert.ok(!colors.includes('#FF0000'), 'orbitPast НЕ должен использоваться');
});

test('drawSelectedLayer: past рисуется точками (setLineDash непустой), future — сплошной (setLineDash пустой)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 0, lat: 0, name: 'gw' };
    ev._syncCenterToObserver();
    ev.colors.orbitPast = '#FF0000';
    ev.colors.selectedTrack = '#FFFF00';
    ev._selectedSatellite = {
        position: null, name: 'TEST', noradId: 99999, visibilityZone: null,
        groundTrack: {
            past: [[
                { lon: 10, lat: 30, ts: 1000000 },
                { lon: 15, lat: 32, ts: 1060000 },
            ]],
            future: [[
                { lon: 20, lat: 34, ts: 1120000 },
                { lon: 25, lat: 36, ts: 1180000 },
            ]],
        },
    };
    ev._drawSelectedLayer();

    // Past и future рисуются одинаковым цветом, но разным стилем линии.
    // Смотрим dash на момент каждого stroke и проверяем: первый stroke
    // (past) имеет dash непустой, второй (future) — пустой.
    let curDash = [];
    const strokeDashes = [];
    for (const c of calls) {
        if (c.op === 'setLineDash') {
            curDash = Array.isArray(c.args[0]) ? c.args[0].slice() : [];
        }
        if (c.op === 'stroke') {
            strokeDashes.push(curDash.slice());
        }
    }
    assert.ok(strokeDashes.length >= 2, 'должно быть как минимум 2 stroke (past + future)');
    assert.ok(strokeDashes[0].length > 0, 'первый stroke (past) — точки/dash: ' + JSON.stringify(strokeDashes[0]));
    assert.ok(strokeDashes[strokeDashes.length - 1].length === 0,
        'последний stroke (future) — сплошной: ' + JSON.stringify(strokeDashes[strokeDashes.length - 1]));
});

test('drawSelectedLayer: legacy-формат (массив точек) → используется selectedTrack (future-цвет)', () => {
    const { ev, calls } = makeRecordingTrackEv();
    ev.observer = { lon: 0, lat: 0, name: 'gw' };
    ev._syncCenterToObserver();
    ev.colors.orbitPast = '#FF0000';
    ev.colors.selectedTrack = '#FFFF00';
    ev._selectedSatellite = {
        position: null, name: 'TEST', noradId: 99999,
        visibilityZone: null,
        groundTrack: [
            { lon: 10, lat: 30, ts: 1000000 },
            { lon: 15, lat: 32, ts: 1060000 },
        ],
    };
    ev._drawSelectedLayer();

    const colors = calls.filter(c => c.op === 'strokeStyle').map(c => c.args[0]);
    assert.ok(colors.includes('#FFFF00'), 'legacy-формат = future-цвет');
    assert.ok(!colors.includes('#FF0000'), 'для legacy past-цвет НЕ применяется (нет past/future)');
});

// ── Итоги ────────────────────────────────────────────────────────────────────

console.log(`\nResults: ${passed} passed, ${failed} failed\n`);
process.exit(failed > 0 ? 1 : 0);
