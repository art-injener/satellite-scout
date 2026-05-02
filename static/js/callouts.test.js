// Тесты для CalloutLayout — аллокатор размещения выносок (callout) подписей КА.
// Запуск: node static/js/callouts.test.js

'use strict';

const assert = require('assert');
const {
    CalloutLayout,
    SECTORS,
    computeGeometry,
    segmentsIntersect,
    leadersIntersect,
} = require('./callouts.js');

// ── Утилиты тестов ─────────────────────────────────────────

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
    }
}

/** Стандартный bounds канваса для тестов. */
const BOUNDS = { width: 1024, height: 512 };

/** Базовые опции, чтобы тесты были предсказуемы. */
const OPTS = {
    stemLength: 80,
    tailLength: 24,
    cardWidth: 140,
    cardHeight: 36,
    minCardGap: 6,
    boundsPadding: 8,
};

function bbox(card) {
    return { x: card.x, y: card.y, w: card.w, h: card.h };
}

function overlap(a, b) {
    return !(a.x + a.w <= b.x || b.x + b.w <= a.x ||
             a.y + a.h <= b.y || b.y + b.h <= a.y);
}

// ── SECTORS ────────────────────────────────────────────────

console.log('\nCallouts: SECTORS');

test('SECTORS contains exactly 8 entries', () => {
    assert.strictEqual(SECTORS.length, 8);
});

test('sector names are unique', () => {
    const names = SECTORS.map(s => s.name);
    const unique = new Set(names);
    assert.strictEqual(unique.size, 8);
});

test('every sector has slopeDeg in (0, 90) — keeps stem diagonal', () => {
    for (const s of SECTORS) {
        assert.ok(s.slopeDeg > 0 && s.slopeDeg < 90,
            `sector ${s.name}: slopeDeg=${s.slopeDeg} outside (0,90)`);
    }
});

// ── Single callout geometry ────────────────────────────────

console.log('\nCallouts: single-callout geometry');

test('layout returns array of same length as markers', () => {
    const layout = new CalloutLayout(OPTS);
    const markers = [
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 600, y: 300 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    assert.strictEqual(res.length, 2);
});

test('marker in result matches input', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 512, y: 256 }], [], BOUNDS);
    assert.strictEqual(res[0].marker.x, 512);
    assert.strictEqual(res[0].marker.y, 256);
});

test('id and color are propagated to result', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([
        { id: 42, x: 500, y: 250, color: '#ffeb3b' },
    ], [], BOUNDS);
    assert.strictEqual(res[0].id, 42);
    assert.strictEqual(res[0].color, '#ffeb3b');
});

test('stem is diagonal: bend offset both in X and Y from marker', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 500, y: 250 }], [], BOUNDS);
    const r = res[0];
    assert.notStrictEqual(r.bend.x, r.marker.x, 'stem must not be strictly vertical');
    assert.notStrictEqual(r.bend.y, r.marker.y, 'stem must not be strictly horizontal');
});

test('tail is horizontal: bend.y equals card vertical center', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 500, y: 250 }], [], BOUNDS);
    const r = res[0];
    const cardCenterY = r.card.y + r.card.h / 2;
    assert.ok(Math.abs(r.bend.y - cardCenterY) < 1e-6,
        `bend.y=${r.bend.y} must match card center Y=${cardCenterY}`);
});

test('card touches tail end (bend.x ± tailLength = card edge)', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 500, y: 250 }], [], BOUNDS);
    const r = res[0];
    const leftEdgeMatch = Math.abs(r.bend.x + OPTS.tailLength - r.card.x) < 1e-6;
    const rightEdgeMatch = Math.abs(r.bend.x - OPTS.tailLength - (r.card.x + r.card.w)) < 1e-6;
    assert.ok(leftEdgeMatch || rightEdgeMatch,
        'card must touch the tail end on the left or right side');
});

test('stem length equals stemLength (within 0.5 px tolerance)', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 500, y: 250 }], [], BOUNDS);
    const r = res[0];
    const dx = r.bend.x - r.marker.x;
    const dy = r.bend.y - r.marker.y;
    const len = Math.sqrt(dx * dx + dy * dy);
    assert.ok(Math.abs(len - OPTS.stemLength) < 0.5,
        `stem length ${len} != ${OPTS.stemLength}`);
});

// ── Multiple markers spread ────────────────────────────────

console.log('\nCallouts: multi-marker spread');

test('4 markers at the same point get 4 distinct sectors', () => {
    const layout = new CalloutLayout(OPTS);
    const markers = [
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 500, y: 250 },
        { id: 3, x: 500, y: 250 },
        { id: 4, x: 500, y: 250 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const sectors = res.map(r => r.sector);
    const unique = new Set(sectors);
    assert.strictEqual(unique.size, 4, `expected 4 unique sectors, got: ${sectors}`);
});

test('cards of 4 co-located markers do not overlap', () => {
    const layout = new CalloutLayout(OPTS);
    const markers = [
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 500, y: 250 },
        { id: 3, x: 500, y: 250 },
        { id: 4, x: 500, y: 250 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    for (let i = 0; i < res.length; i++) {
        for (let j = i + 1; j < res.length; j++) {
            assert.ok(!overlap(bbox(res[i].card), bbox(res[j].card)),
                `cards ${i} and ${j} overlap`);
        }
    }
});

test('8 markers at the same point fill all 8 sectors', () => {
    const layout = new CalloutLayout(OPTS);
    const markers = [];
    for (let i = 1; i <= 8; i++) {
        markers.push({ id: i, x: 500, y: 250 });
    }
    const res = layout.layout(markers, [], BOUNDS);
    const sectors = new Set(res.map(r => r.sector));
    assert.strictEqual(sectors.size, 8, `expected 8 unique sectors, got ${sectors.size}`);
});

// ── Sector cache by id ─────────────────────────────────────

console.log('\nCallouts: sector cache by id');

test('repeated layout with same id yields same sector (cache hit)', () => {
    const layout = new CalloutLayout(OPTS);
    const m = [{ id: 1, x: 500, y: 250 }];
    const r1 = layout.layout(m, [], BOUNDS);
    const r2 = layout.layout(m, [], BOUNDS);
    assert.strictEqual(r1[0].sector, r2[0].sector);
});

test('reset() clears cache', () => {
    const layout = new CalloutLayout(OPTS);
    layout.layout([{ id: 1, x: 100, y: 250 }], [], BOUNDS);
    layout.reset();
    assert.strictEqual(layout._cache.size, 0);
});

test('prune() removes ids missing from current list', () => {
    const layout = new CalloutLayout(OPTS);
    layout.layout([
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 500, y: 250 },
    ], [], BOUNDS);
    layout.prune([1]);
    assert.ok(layout._cache.has(1), 'id=1 must remain in cache');
    assert.ok(!layout._cache.has(2), 'id=2 must be evicted');
});

// ── Obstacles (cities / observer) ──────────────────────────

console.log('\nCallouts: obstacles');

test('obstacle covering preferred sector forces alternative pick', () => {
    const layout = new CalloutLayout(OPTS);
    const m = [{ id: 1, x: 500, y: 250 }];
    const obstacles = [
        { x: 510, y: 100, w: 300, h: 200 },
    ];
    const res = layout.layout(m, obstacles, BOUNDS);
    const r = res[0];
    assert.ok(!overlap(bbox(r.card), obstacles[0]),
        `card overlaps obstacle: card=${JSON.stringify(r.card)}`);
});

// ── Canvas bounds ──────────────────────────────────────────

console.log('\nCallouts: canvas bounds');

test('marker near top-right corner: card stays inside canvas', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 1000, y: 30 }], [], BOUNDS);
    const r = res[0];
    assert.ok(r.card.x >= OPTS.boundsPadding,
        `card.x=${r.card.x} crossed left bound`);
    assert.ok(r.card.x + r.card.w <= BOUNDS.width - OPTS.boundsPadding,
        `card.x+w=${r.card.x + r.card.w} crossed right bound`);
    assert.ok(r.card.y >= OPTS.boundsPadding,
        `card.y=${r.card.y} crossed top bound`);
    assert.ok(r.card.y + r.card.h <= BOUNDS.height - OPTS.boundsPadding,
        `card.y+h=${r.card.y + r.card.h} crossed bottom bound`);
});

test('marker near bottom-left corner: card stays inside canvas', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 20, y: 490 }], [], BOUNDS);
    const r = res[0];
    assert.ok(r.card.x >= OPTS.boundsPadding);
    assert.ok(r.card.x + r.card.w <= BOUNDS.width - OPTS.boundsPadding);
    assert.ok(r.card.y >= OPTS.boundsPadding);
    assert.ok(r.card.y + r.card.h <= BOUNDS.height - OPTS.boundsPadding);
});

// ── Geometric invariant: single bend, turn angle < 90° ────

console.log('\nCallouts: single-bend invariant (turn < 90°)');

test('turn angle at the bend is always strictly less than 90°', () => {
    const samples = [
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 100, y: 100 },
        { id: 3, x: 900, y: 400 },
        { id: 4, x: 250, y: 400 },
        { id: 5, x: 800, y: 80 },
    ];
    for (const s of samples) {
        const fresh = new CalloutLayout(OPTS);
        const r = fresh.layout([s], [], BOUNDS)[0];
        const sx = r.bend.x - r.marker.x;
        const sy = r.bend.y - r.marker.y;
        const tailSide = Math.sign(r.card.x + r.card.w / 2 - r.bend.x);
        const tx = tailSide * OPTS.tailLength;
        const ty = 0;
        const dot = sx * tx + sy * ty;
        const lenS = Math.sqrt(sx * sx + sy * sy);
        const lenT = Math.sqrt(tx * tx + ty * ty);
        const cosTurn = dot / (lenS * lenT);
        const turnDeg = Math.acos(Math.max(-1, Math.min(1, cosTurn))) * 180 / Math.PI;
        assert.ok(turnDeg < 90 - 1e-6,
            `marker (${s.x},${s.y}): turn angle ${turnDeg.toFixed(1)}° >= 90°`);
        assert.ok(turnDeg > 1e-6,
            `marker (${s.x},${s.y}): turn angle 0° (no bend)`);
    }
});

// ── Cache stability under micro-movement ──────────────────

console.log('\nCallouts: cache stability under micro-movement');

test('small marker motion keeps the same sector when no collisions appear', () => {
    const layout = new CalloutLayout(OPTS);
    const r1 = layout.layout([{ id: 1, x: 500, y: 250 }], [], BOUNDS);
    const r2 = layout.layout([{ id: 1, x: 502, y: 252 }], [], BOUNDS);
    assert.strictEqual(r1[0].sector, r2[0].sector);
});

// ── Segment intersection utility ───────────────────────────

console.log('\nCallouts: segmentsIntersect');

test('classic crossing X returns true', () => {
    const hit = segmentsIntersect(
        { x: 0, y: 0 }, { x: 10, y: 10 },
        { x: 0, y: 10 }, { x: 10, y: 0 },
    );
    assert.strictEqual(hit, true);
});

test('parallel non-collinear segments return false', () => {
    const hit = segmentsIntersect(
        { x: 0, y: 0 }, { x: 10, y: 0 },
        { x: 0, y: 5 }, { x: 10, y: 5 },
    );
    assert.strictEqual(hit, false);
});

test('shared endpoint (T-junction) is not strict intersection', () => {
    const hit = segmentsIntersect(
        { x: 0, y: 0 }, { x: 10, y: 0 },
        { x: 10, y: 0 }, { x: 10, y: 10 },
    );
    assert.strictEqual(hit, false);
});

test('disjoint segments return false', () => {
    const hit = segmentsIntersect(
        { x: 0, y: 0 }, { x: 5, y: 5 },
        { x: 10, y: 10 }, { x: 15, y: 15 },
    );
    assert.strictEqual(hit, false);
});

// ── leadersIntersect on raw geometries ─────────────────────

console.log('\nCallouts: leadersIntersect');

test('two opposing diagonal stems get detected as crossing', () => {
    const RB_sh = SECTORS.find(s => s.name === 'RB-shallow');
    const RT_sh = SECTORS.find(s => s.name === 'RT-shallow');
    const gA = computeGeometry({ id: 1, x: 400, y: 200 }, RB_sh, OPTS);
    const gB = computeGeometry({ id: 2, x: 400, y: 250 }, RT_sh, OPTS);
    const ltA = { id: 1, marker: { x: 400, y: 200 }, bend: gA.bend, card: gA.card };
    const ltB = { id: 2, marker: { x: 400, y: 250 }, bend: gB.bend, card: gB.card };
    assert.strictEqual(leadersIntersect(ltA, ltB), true);
});

test('two stems pointing into opposite quadrants do not cross', () => {
    const RT = SECTORS.find(s => s.name === 'RT-steep');
    const LB = SECTORS.find(s => s.name === 'LB-steep');
    const gA = computeGeometry({ id: 1, x: 400, y: 250 }, RT, OPTS);
    const gB = computeGeometry({ id: 2, x: 400, y: 250 }, LB, OPTS);
    const ltA = { id: 1, marker: { x: 400, y: 250 }, bend: gA.bend, card: gA.card };
    const ltB = { id: 2, marker: { x: 400, y: 250 }, bend: gB.bend, card: gB.card };
    assert.strictEqual(leadersIntersect(ltA, ltB), false);
});

// ── Anti-crossing post-pass in CalloutLayout ──────────────

console.log('\nCallouts: anti-crossing post-pass');

const CLUSTER_4 = [
    { id: 1, x: 480, y: 240 },
    { id: 2, x: 520, y: 240 },
    { id: 3, x: 480, y: 280 },
    { id: 4, x: 520, y: 280 },
];

function countCrossings(layouts) {
    let count = 0;
    for (let i = 0; i < layouts.length; i++) {
        for (let j = i + 1; j < layouts.length; j++) {
            if (leadersIntersect(layouts[i], layouts[j])) { count++; }
        }
    }
    return count;
}

test('cluster of 4 markers: no two leaders intersect (default mode)', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout(CLUSTER_4, [], BOUNDS);
    assert.strictEqual(countCrossings(res), 0,
        'default mode must remove all leader crossings');
});

test('with resolveCrossings=false the same cluster keeps crossings', () => {
    const opts = Object.assign({}, OPTS, { resolveCrossings: false });
    const layout = new CalloutLayout(opts);
    const res = layout.layout(CLUSTER_4, [], BOUNDS);
    assert.ok(countCrossings(res) > 0,
        'sanity: greedy phase alone must produce at least one crossing on this cluster');
});

test('post-pass changes the sector assignment vs raw greedy', () => {
    const optsRaw = Object.assign({}, OPTS, { resolveCrossings: false });
    const raw = new CalloutLayout(optsRaw).layout(CLUSTER_4, [], BOUNDS);
    const fixed = new CalloutLayout(OPTS).layout(CLUSTER_4, [], BOUNDS);
    let differs = false;
    for (let i = 0; i < raw.length; i++) {
        if (raw[i].sector !== fixed[i].sector) { differs = true; break; }
    }
    assert.ok(differs,
        'expected at least one sector to change after anti-crossing pass');
});

test('post-pass writes swapped sectors back into _cache', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout(CLUSTER_4, [], BOUNDS);
    for (let i = 0; i < res.length; i++) {
        const cachedIdx = layout._cache.get(res[i].id);
        assert.strictEqual(SECTORS[cachedIdx].name, res[i].sector,
            `cache for id=${res[i].id} must match final sector ${res[i].sector}`);
    }
});

test('post-pass keeps cards inside bounds', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout(CLUSTER_4, [], BOUNDS);
    for (const r of res) {
        assert.ok(r.card.x >= OPTS.boundsPadding);
        assert.ok(r.card.x + r.card.w <= BOUNDS.width - OPTS.boundsPadding);
        assert.ok(r.card.y >= OPTS.boundsPadding);
        assert.ok(r.card.y + r.card.h <= BOUNDS.height - OPTS.boundsPadding);
    }
});

test('post-pass preserves card non-overlap (with minCardGap)', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout(CLUSTER_4, [], BOUNDS);
    for (let i = 0; i < res.length; i++) {
        for (let j = i + 1; j < res.length; j++) {
            const a = res[i].card;
            const b = res[j].card;
            const sepX = (a.x + a.w + OPTS.minCardGap <= b.x) ||
                         (b.x + b.w + OPTS.minCardGap <= a.x);
            const sepY = (a.y + a.h + OPTS.minCardGap <= b.y) ||
                         (b.y + b.h + OPTS.minCardGap <= a.y);
            assert.ok(sepX || sepY,
                `cards ${i} and ${j} overlap after swap`);
        }
    }
});

// ── Per-marker cardWidth (auto-width by text) ──────────────

console.log('\nCallouts: per-marker cardWidth');

test('marker without cardWidth falls back to opts.cardWidth', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 500, y: 250 }], [], BOUNDS);
    assert.strictEqual(res[0].card.w, OPTS.cardWidth);
});

test('marker.cardWidth overrides opts.cardWidth', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([{ id: 1, x: 500, y: 250, cardWidth: 90 }], [], BOUNDS);
    assert.strictEqual(res[0].card.w, 90);
});

test('computeGeometry uses marker.cardWidth when provided', () => {
    const sector = SECTORS[0];
    const g = computeGeometry({ id: 1, x: 500, y: 250, cardWidth: 100 }, sector, OPTS);
    assert.strictEqual(g.card.w, 100);
});

test('cluster with mixed widths: each card keeps its own width after swaps', () => {
    const layout = new CalloutLayout(OPTS);
    const res = layout.layout([
        { id: 1, x: 480, y: 240, cardWidth: 80 },
        { id: 2, x: 520, y: 240, cardWidth: 120 },
        { id: 3, x: 480, y: 280, cardWidth: 80 },
        { id: 4, x: 520, y: 280, cardWidth: 120 },
    ], [], BOUNDS);
    const widthById = {};
    for (const r of res) { widthById[r.id] = r.card.w; }
    assert.strictEqual(widthById[1], 80);
    assert.strictEqual(widthById[2], 120);
    assert.strictEqual(widthById[3], 80);
    assert.strictEqual(widthById[4], 120);
});

// ── Marker bboxes as obstacles (FE-007 collision with sat icons) ──

console.log('\nCallouts: marker icons as obstacles');

test('card never overlaps a foreign marker bbox passed as obstacle', () => {
    const layout = new CalloutLayout(OPTS);
    // Маркер #1 — наш callout, маркер #2 — препятствие (чужой спутник рядом).
    const r2 = 18;
    const m2x = 540;
    const m2y = 220;
    const obstacles = [{ x: m2x - r2, y: m2y - r2, w: 2 * r2, h: 2 * r2 }];
    const res = layout.layout([{ id: 1, x: 500, y: 250 }], obstacles, BOUNDS);
    const c = res[0].card;
    const ob = obstacles[0];
    const sepX = (c.x + c.w <= ob.x) || (ob.x + ob.w <= c.x);
    const sepY = (c.y + c.h <= ob.y) || (ob.y + ob.h <= c.y);
    assert.ok(sepX || sepY,
        `card {x:${c.x},y:${c.y},w:${c.w},h:${c.h}} overlaps obstacle ` +
        `{x:${ob.x},y:${ob.y},w:${ob.w},h:${ob.h}}`);
});

test('cluster of 4 markers: each card avoids all 3 foreign marker bboxes', () => {
    // Эмулируем то, что делает EarthView._collectCalloutObstacles:
    // каждый маркер получает bbox ±r и они все добавляются в obstacles.
    const layout = new CalloutLayout(OPTS);
    const r = 18;
    const markersIn = CLUSTER_4;
    const obstacles = markersIn.map(m => ({
        x: m.x - r, y: m.y - r, w: 2 * r, h: 2 * r,
    }));
    const res = layout.layout(markersIn, obstacles, BOUNDS);
    for (let i = 0; i < res.length; i++) {
        const c = res[i].card;
        for (let k = 0; k < obstacles.length; k++) {
            // Свой маркер — это obstacles[i] для маркера с тем же индексом.
            // Геометрия выноски гарантирует разнос карточки и своего маркера,
            // поэтому проверяем коллизию именно с чужими bbox-ами.
            if (k === i) { continue; }
            const ob = obstacles[k];
            const sepX = (c.x + c.w <= ob.x) || (ob.x + ob.w <= c.x);
            const sepY = (c.y + c.h <= ob.y) || (ob.y + ob.h <= c.y);
            assert.ok(sepX || sepY,
                `card[${i}] overlaps foreign marker bbox[${k}]`);
        }
    }
});

test('own marker bbox does not block own callout (geometry-guaranteed gap)', () => {
    // Нагрузочный кейс: маркер посередине, его собственная bbox в obstacles.
    // Аллокатор обязан найти сектор и не уйти в fallback с коллизией.
    const layout = new CalloutLayout(OPTS);
    const r = 18;
    const m = { id: 99, x: 512, y: 256 };
    const obstacles = [{ x: m.x - r, y: m.y - r, w: 2 * r, h: 2 * r }];
    const res = layout.layout([m], obstacles, BOUNDS);
    const c = res[0].card;
    const ob = obstacles[0];
    const sepX = (c.x + c.w <= ob.x) || (ob.x + ob.w <= c.x);
    const sepY = (c.y + c.h <= ob.y) || (ob.y + ob.h <= c.y);
    assert.ok(sepX || sepY,
        'own callout card unexpectedly overlaps own marker bbox');
});

// ── Ring layout (PCA-эллипс группы КА) ─────────────────────
//
// Новый режим `groupingMode: 'ring'`: карточки размещаются на расширенном
// PCA-эллипсе вокруг кластера маркеров. Иконки и трассы остаются
// неперекрытыми — кадр с группой КА читается без кучи карточек в центре.

console.log('\nCallouts: ring layout (PCA-ellipse, groupingMode=ring)');

const RING_OPTS = Object.assign({}, OPTS, {
    groupingMode: 'ring',
    ringGap: 70, // ≈ 0.5·cardWidth (balanced)
    clusterDistance: 4 * 18, // примерно 4·iconRadius
});

/** Тесная группа из 6 КА (радиус ≤ stemLength) — типичный кластер пролёта. */
const RING_CLUSTER_6 = [
    { id: 11, x: 512, y: 256 },
    { id: 12, x: 540, y: 250 },
    { id: 13, x: 528, y: 280 },
    { id: 14, x: 490, y: 270 },
    { id: 15, x: 500, y: 240 },
    { id: 16, x: 532, y: 232 },
];

/** Bbox-обстаклы для группы маркеров (как делает EarthView). */
function markerObstacles(markers, r) {
    return markers.map(m => ({
        x: m.x - r, y: m.y - r, w: 2 * r, h: 2 * r,
    }));
}

test('ring: layout returns array of same length as markers', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    assert.strictEqual(res.length, RING_CLUSTER_6.length);
});

test('ring: each layout has marker, bend, card and id (rendering contract)', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    for (const r of res) {
        assert.ok(r.marker && typeof r.marker.x === 'number' && typeof r.marker.y === 'number');
        assert.ok(r.bend && typeof r.bend.x === 'number' && typeof r.bend.y === 'number');
        assert.ok(r.card && typeof r.card.x === 'number' && typeof r.card.w === 'number');
        assert.ok(r.id != null);
    }
});

test('ring: cluster of 6 — no card overlaps a foreign marker bbox', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const r = 18;
    const obstacles = markerObstacles(RING_CLUSTER_6, r);
    const res = layout.layout(RING_CLUSTER_6, obstacles, BOUNDS);
    for (let i = 0; i < res.length; i++) {
        const c = res[i].card;
        for (let k = 0; k < obstacles.length; k++) {
            if (k === i) { continue; }
            const ob = obstacles[k];
            const sepX = (c.x + c.w <= ob.x) || (ob.x + ob.w <= c.x);
            const sepY = (c.y + c.h <= ob.y) || (ob.y + ob.h <= c.y);
            assert.ok(sepX || sepY,
                `card[${i}] overlaps foreign marker bbox[${k}]: ` +
                `card=${JSON.stringify(c)} ob=${JSON.stringify(ob)}`);
        }
    }
});

test('ring: cluster of 6 — no two leader lines intersect', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    assert.strictEqual(countCrossings(res), 0,
        'ring layout must place cards radially so leaders do not cross');
});

test('ring: cluster of 6 — cards do not overlap each other', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    for (let i = 0; i < res.length; i++) {
        for (let j = i + 1; j < res.length; j++) {
            assert.ok(!overlap(bbox(res[i].card), bbox(res[j].card)),
                `ring cards ${res[i].id} and ${res[j].id} overlap`);
        }
    }
});

test('ring: every card is placed outside the cluster bounding box', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    let minX = +Infinity, maxX = -Infinity, minY = +Infinity, maxY = -Infinity;
    for (const m of RING_CLUSTER_6) {
        if (m.x < minX) { minX = m.x; }
        if (m.x > maxX) { maxX = m.x; }
        if (m.y < minY) { minY = m.y; }
        if (m.y > maxY) { maxY = m.y; }
    }
    for (const r of res) {
        const c = r.card;
        const cx = c.x + c.w / 2;
        const cy = c.y + c.h / 2;
        const inside = (cx >= minX && cx <= maxX && cy >= minY && cy <= maxY);
        assert.ok(!inside,
            `card ${r.id} center (${cx},${cy}) lies inside cluster bbox ` +
            `[${minX},${minY},${maxX},${maxY}]`);
    }
});

test('ring: single marker — card placed at distance ~stemLength from icon', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const r = 18;
    const m = { id: 1, x: 512, y: 256, iconRadius: r };
    const obstacles = markerObstacles([m], r);
    const res = layout.layout([m], obstacles, BOUNDS);
    const c = res[0].card;
    const cx = c.x + c.w / 2;
    const cy = c.y + c.h / 2;
    const dist = Math.sqrt((cx - m.x) * (cx - m.x) + (cy - m.y) * (cy - m.y));
    // Кольцо: r (иконка) + ringGap (70) + ~tailLength/2 + cardW/2 — порядок ~140 px.
    assert.ok(dist > r + 30,
        `single-marker card too close to icon: dist=${dist}, expected > ${r + 30}`);
    assert.ok(dist < 260,
        `single-marker card too far: dist=${dist}, expected < 260`);
});

test('ring: two distant markers form two separate clusters (cards on opposite sides)', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Расстояние ≫ clusterDistance (72) → должны быть разные кластеры.
    const markers = [
        { id: 1, x: 200, y: 256 },
        { id: 2, x: 800, y: 256 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    // Каждая карточка должна быть рядом со своим маркером, а не в центре карты.
    for (const r of res) {
        const c = r.card;
        const cx = c.x + c.w / 2;
        const dx = Math.abs(cx - r.marker.x);
        assert.ok(dx < 250,
            `card for marker ${r.id} drifted too far from its marker: dx=${dx}`);
    }
});

test('ring: cluster near canvas edge — every card stays inside bounds', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Кластер прижат к правому краю canvas
    const markers = [
        { id: 1, x: 1000, y: 250 },
        { id: 2, x: 1010, y: 260 },
        { id: 3, x: 1005, y: 240 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    for (const r of res) {
        const c = r.card;
        assert.ok(c.x >= RING_OPTS.boundsPadding,
            `card.x=${c.x} crossed left bound`);
        assert.ok(c.x + c.w <= BOUNDS.width - RING_OPTS.boundsPadding,
            `card.x+w=${c.x + c.w} crossed right bound`);
        assert.ok(c.y >= RING_OPTS.boundsPadding,
            `card.y=${c.y} crossed top bound`);
        assert.ok(c.y + c.h <= BOUNDS.height - RING_OPTS.boundsPadding,
            `card.y+h=${c.y + c.h} crossed bottom bound`);
    }
});

test('ring: very wide cluster (zoom>1) — semi-axes are clamped, cards do not pin to canvas edges', () => {
    // Сценарий: zoom>1, маркеры разошлись почти по всему canvas. Без клампа
    // полуосей PCA-эллипс становится шире viewport, и buildRingPlacement
    // прижимает все карточки к краям.
    const opts = Object.assign({}, RING_OPTS, {
        clusterDistance: Number.POSITIVE_INFINITY,
    });
    const layout = new CalloutLayout(opts);
    const markers = [
        { id: 1, x: 80,  y: 80  },
        { id: 2, x: 940, y: 90  },
        { id: 3, x: 100, y: 420 },
        { id: 4, x: 920, y: 430 },
        { id: 5, x: 512, y: 256 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    // Карточки укладываются в bounds (поведение buildRingPlacement не сломалось).
    for (const r of res) {
        const c = r.card;
        assert.ok(c.x >= opts.boundsPadding && c.x + c.w <= BOUNDS.width  - opts.boundsPadding,
            `card.x out of bounds: ${JSON.stringify(c)}`);
        assert.ok(c.y >= opts.boundsPadding && c.y + c.h <= BOUNDS.height - opts.boundsPadding,
            `card.y out of bounds: ${JSON.stringify(c)}`);
    }
    // Главный признак клампа полуосей: не все карточки прижаты к четырём
    // краям canvas (как было раньше). Считаем «прижатой к краю» карточку,
    // у которой расстояние от ближайшего края ≤ boundsPadding+1.
    const eps = opts.boundsPadding + 1;
    let pinned = 0;
    for (const r of res) {
        const c = r.card;
        const distLeft   = c.x - opts.boundsPadding;
        const distRight  = (BOUNDS.width  - opts.boundsPadding) - (c.x + c.w);
        const distTop    = c.y - opts.boundsPadding;
        const distBottom = (BOUNDS.height - opts.boundsPadding) - (c.y + c.h);
        const minDist = Math.min(distLeft, distRight, distTop, distBottom);
        if (minDist <= eps) { pinned++; }
    }
    assert.ok(pinned < markers.length,
        `all ${markers.length} cards pinned to canvas edges — clamping ineffective ` +
        `(pinned=${pinned})`);
});

test('ring: PCA does not degenerate on collinear markers (n=2)', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Две точки на одной горизонтали — главная ось ровно X, вторая ось вырождена.
    const markers = [
        { id: 1, x: 480, y: 256 },
        { id: 2, x: 540, y: 256 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    for (const r of res) {
        // Все координаты конечны — алгоритм не должен падать в NaN/Infinity.
        assert.ok(isFinite(r.bend.x) && isFinite(r.bend.y),
            'bend coords must be finite');
        assert.ok(isFinite(r.card.x) && isFinite(r.card.y),
            'card coords must be finite');
    }
});

test('ring: PCA does not degenerate on coincident markers', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 512, y: 256 },
        { id: 2, x: 512, y: 256 },
        { id: 3, x: 512, y: 256 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    for (const r of res) {
        assert.ok(isFinite(r.bend.x) && isFinite(r.bend.y));
        assert.ok(isFinite(r.card.x) && isFinite(r.card.y));
    }
    // И карточки должны не накладываться (раздвинулись по углу)
    for (let i = 0; i < res.length; i++) {
        for (let j = i + 1; j < res.length; j++) {
            assert.ok(!overlap(bbox(res[i].card), bbox(res[j].card)),
                `coincident markers: cards ${i} and ${j} overlap`);
        }
    }
});

test('ring: tail is horizontal (bend.y matches card vertical center)', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    for (const r of res) {
        const cardCenterY = r.card.y + r.card.h / 2;
        assert.ok(Math.abs(r.bend.y - cardCenterY) < 1e-6,
            `bend.y=${r.bend.y} must match card center Y=${cardCenterY}`);
    }
});

test('ring: tracked icon + dense cluster — single ellipse, no card overlaps tracked icon', () => {
    // Сценарий со скриншота: tracked-иконка крупнее обычной и слегка отделена
    // от плотной группы КА. С clusterDistance=Infinity все маркеры попадают
    // в один эллипс — карточка ни одного «соседа» не должна налечь на tracked.
    const opts = Object.assign({}, RING_OPTS, {
        clusterDistance: Number.POSITIVE_INFINITY,
    });
    const layout = new CalloutLayout(opts);
    const trackedR = 24; // tracked-иконка крупнее (антенны + корпус)
    const sideR = 18;
    const tracked = { id: 1, x: 480, y: 250, iconRadius: trackedR };
    const sideCluster = [
        { id: 2, x: 540, y: 240, iconRadius: sideR },
        { id: 3, x: 555, y: 250, iconRadius: sideR },
        { id: 4, x: 545, y: 265, iconRadius: sideR },
        { id: 5, x: 530, y: 270, iconRadius: sideR },
        { id: 6, x: 565, y: 235, iconRadius: sideR },
    ];
    const markers = [tracked].concat(sideCluster);
    const obstacles = markers.map(m => ({
        x: m.x - m.iconRadius, y: m.y - m.iconRadius,
        w: 2 * m.iconRadius,    h: 2 * m.iconRadius,
    }));
    const res = layout.layout(markers, obstacles, BOUNDS);
    // Проверяем именно tracked-иконку: её bbox не должен перекрываться
    // ни одной карточкой соседей (своя карточка отнесена за эллипс).
    const trackedBbox = obstacles[0];
    for (let i = 1; i < res.length; i++) {
        const c = res[i].card;
        const sepX = (c.x + c.w <= trackedBbox.x) || (trackedBbox.x + trackedBbox.w <= c.x);
        const sepY = (c.y + c.h <= trackedBbox.y) || (trackedBbox.y + trackedBbox.h <= c.y);
        assert.ok(sepX || sepY,
            `card ${res[i].id} overlaps tracked icon bbox: ` +
            `card=${JSON.stringify(c)} tracked=${JSON.stringify(trackedBbox)}`);
    }
});

// ── Ring layout: обход запретных сегментов трасс (forbiddenSegments) ──────
//
// `layout(markers, obstacles, bounds, forbiddenSegments)` — карточки в
// ring-режиме после первичного размещения сдвигаются по углу так, чтобы
// их bbox+forbiddenPadding не пересекал ни один запретный отрезок.
// Если 0-пересечений недостижимо — best-effort: минимум пересечений.

console.log('\nCallouts: ring layout — forbidden segments (track avoidance)');

/**
 * Считает число запретных сегментов, которые пересекают bbox карточки
 * (с заданным padding с каждой стороны). Используется для проверок в тестах
 * (внутри callouts.js эта же логика реализована приватно).
 */
function bboxHitsSegment(card, padding, seg) {
    const x1 = card.x - padding;
    const y1 = card.y - padding;
    const x2 = card.x + card.w + padding;
    const y2 = card.y + card.h + padding;
    const sxMin = Math.min(seg.x1, seg.x2), sxMax = Math.max(seg.x1, seg.x2);
    const syMin = Math.min(seg.y1, seg.y2), syMax = Math.max(seg.y1, seg.y2);
    if (sxMax < x1 || sxMin > x2 || syMax < y1 || syMin > y2) { return false; }
    if ((seg.x1 >= x1 && seg.x1 <= x2 && seg.y1 >= y1 && seg.y1 <= y2) ||
        (seg.x2 >= x1 && seg.x2 <= x2 && seg.y2 >= y1 && seg.y2 <= y2)) {
        return true;
    }
    const a = { x: seg.x1, y: seg.y1 };
    const b = { x: seg.x2, y: seg.y2 };
    return segmentsIntersect(a, b, { x: x1, y: y1 }, { x: x2, y: y1 }) ||
           segmentsIntersect(a, b, { x: x2, y: y1 }, { x: x2, y: y2 }) ||
           segmentsIntersect(a, b, { x: x2, y: y2 }, { x: x1, y: y2 }) ||
           segmentsIntersect(a, b, { x: x1, y: y2 }, { x: x1, y: y1 });
}

function countCardCrossings(card, padding, segments) {
    let n = 0;
    for (const s of segments) { if (bboxHitsSegment(card, padding, s)) { n++; } }
    return n;
}

test('ring: layout accepts forbiddenSegments arg without breaking existing contract', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Пустой массив сегментов — поведение не должно отличаться от 3-арочного вызова.
    const a = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    const b = layout.layout(RING_CLUSTER_6, [], BOUNDS, []);
    assert.strictEqual(a.length, b.length);
    for (let i = 0; i < a.length; i++) {
        assert.strictEqual(a[i].card.x, b[i].card.x,
            `card[${i}].x diverged with empty forbiddenSegments`);
        assert.strictEqual(a[i].card.y, b[i].card.y,
            `card[${i}].y diverged with empty forbiddenSegments`);
    }
});

test('ring: horizontal track through cluster — no card crosses the track', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Горизонтальная трасса прямо через центр кадра (через сам кластер):
    // карточки должны уйти выше/ниже, обойдя её.
    const segments = [
        { x1: 0, y1: 256, x2: 1024, y2: 256 },
    ];
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    const padding = RING_OPTS.forbiddenPadding != null ? RING_OPTS.forbiddenPadding : 5;
    for (let i = 0; i < res.length; i++) {
        const c = res[i].card;
        assert.strictEqual(countCardCrossings(c, padding, segments), 0,
            `card ${res[i].id} still crosses horizontal track: ${JSON.stringify(c)}`);
    }
});

test('ring: card keeps forbiddenPadding gap from the nearest track', () => {
    const opts = Object.assign({}, RING_OPTS, { forbiddenPadding: 8 });
    const layout = new CalloutLayout(opts);
    // Диагональная трасса через кадр.
    const segments = [
        { x1: 100, y1: 100, x2: 900, y2: 400 },
    ];
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    // Все карточки должны быть на расстоянии ≥ padding от линии (по нашей метрике
    // bboxHitsSegment с этим же padding — 0 пересечений).
    for (let i = 0; i < res.length; i++) {
        const c = res[i].card;
        assert.strictEqual(countCardCrossings(c, 8, segments), 0,
            `card ${res[i].id} violates forbiddenPadding=8 from diagonal track`);
    }
});

test('ring: best-effort — when track covers the whole frame, layout still finishes and minimizes crossings', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Сетка из 4 трасс — почти невозможно полностью обойти.
    const segments = [
        { x1: 0, y1: 100, x2: 1024, y2: 100 },
        { x1: 0, y1: 200, x2: 1024, y2: 200 },
        { x1: 0, y1: 300, x2: 1024, y2: 300 },
        { x1: 0, y1: 400, x2: 1024, y2: 400 },
    ];
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    // Контракт: layout не падает и возвращает все карточки с конечными координатами.
    assert.strictEqual(res.length, RING_CLUSTER_6.length);
    for (const r of res) {
        assert.ok(isFinite(r.card.x) && isFinite(r.card.y));
    }
    // Сумма пересечений после обхода должна быть ≤ суммы в режиме без обхода
    // (best-effort гарантирует монотонность по слоту: alg никогда не ухудшает
    // свой собственный счётчик пересечений).
    const baseline = layout.layout(RING_CLUSTER_6, [], BOUNDS); // без forbidden
    const padding = RING_OPTS.forbiddenPadding != null ? RING_OPTS.forbiddenPadding : 5;
    let baseSum = 0, optSum = 0;
    for (let i = 0; i < res.length; i++) {
        baseSum += countCardCrossings(baseline[i].card, padding, segments);
        optSum  += countCardCrossings(res[i].card,      padding, segments);
    }
    assert.ok(optSum <= baseSum,
        `best-effort regressed: baseline=${baseSum}, optimised=${optSum}`);
});

test('ring: avoidance does not introduce overlaps between cards', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const segments = [
        { x1: 0, y1: 256, x2: 1024, y2: 256 },
    ];
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    for (let i = 0; i < res.length; i++) {
        for (let j = i + 1; j < res.length; j++) {
            assert.ok(!overlap(bbox(res[i].card), bbox(res[j].card)),
                `cards ${res[i].id} and ${res[j].id} overlap after track avoidance`);
        }
    }
});

test('ring: avoidance does not push cards out of canvas bounds', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const segments = [
        { x1: 100, y1: 100, x2: 900, y2: 400 },
        { x1: 0,   y1: 256, x2: 1024, y2: 256 },
    ];
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    const pad = RING_OPTS.boundsPadding;
    for (const r of res) {
        const c = r.card;
        assert.ok(c.x >= pad, `card.x=${c.x} crossed left bound`);
        assert.ok(c.y >= pad, `card.y=${c.y} crossed top bound`);
        assert.ok(c.x + c.w <= BOUNDS.width  - pad, `card crossed right bound: ${JSON.stringify(c)}`);
        assert.ok(c.y + c.h <= BOUNDS.height - pad, `card crossed bottom bound: ${JSON.stringify(c)}`);
    }
});

test('ring: vertical track right of cluster — cards prefer the left side', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Вертикальная трасса справа от кластера (x=620): обходить надо влево.
    const segments = [
        { x1: 620, y1: 0, x2: 620, y2: 512 },
    ];
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    const padding = RING_OPTS.forbiddenPadding != null ? RING_OPTS.forbiddenPadding : 5;
    // Ни одна карточка не должна зацепить запретную линию.
    for (const r of res) {
        assert.strictEqual(countCardCrossings(r.card, padding, segments), 0,
            `card ${r.id} crosses vertical track at x=620: ${JSON.stringify(r.card)}`);
    }
});

test('ring: per-marker cardWidth is preserved in result', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 480, y: 240, cardWidth: 80 },
        { id: 2, x: 520, y: 240, cardWidth: 120 },
        { id: 3, x: 480, y: 280, cardWidth: 80 },
        { id: 4, x: 520, y: 280, cardWidth: 120 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const widthById = {};
    for (const r of res) { widthById[r.id] = r.card.w; }
    assert.strictEqual(widthById[1], 80);
    assert.strictEqual(widthById[2], 120);
    assert.strictEqual(widthById[3], 80);
    assert.strictEqual(widthById[4], 120);
});

// ── Summary ────────────────────────────────────────────────

console.log(`\n${'='.repeat(50)}`);
console.log(`Results: ${passed} passed, ${failed} failed, ${passed + failed} total`);
console.log('='.repeat(50));

if (failed > 0) {
    process.exit(1);
}
