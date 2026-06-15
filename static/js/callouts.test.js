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
    bboxOverlap,
    placementFromCard,
    stemPiercesCard,
    accentOnCardRight,
    accentOnCardTop,
    accentOnCardBottom,
    countLeaderForeignIconHits,
    countLeaderObstacleHits,
    computeAnnealEnergy,
    annealInitialState,
    buildVirtualStacks,
    subsampleForbiddenSegments,
    clusterRingSeedState,
    buildCalloutLinesOverlaySpec,
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

console.log('\nCallouts: anneal layout (SA, groupingMode=anneal/ring)');

const RING_OPTS = Object.assign({}, OPTS, {
    groupingMode: 'anneal',
    annealSweeps: 1200,
    annealStepPx: 14,
    annealSeedRadius: 100,
    annealSeed: 42,
    annealWObstacle: 200,
    annealWLeaderCross: 400,
    annealWCardTrack: 150,
    annealWLeaderTrack: 600,
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
    let minX = Number(Infinity), maxX = -Infinity, minY = Number(Infinity), maxY = -Infinity;
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
    // Кластер прижат к правому краю canvas (расстояния > stackDistance=12)
    const markers = [
        { id: 1, x: 1000, y: 250 },
        { id: 2, x: 1020, y: 268 },
        { id: 3, x: 980, y: 235 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    for (const r of res) {
        if (!r) { continue; }
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
        { id: 1, x: 80, y: 80 },
        { id: 2, x: 940, y: 90 },
        { id: 3, x: 100, y: 420 },
        { id: 4, x: 920, y: 430 },
        { id: 5, x: 512, y: 256 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    // Карточки укладываются в bounds (поведение buildRingPlacement не сломалось).
    for (const r of res) {
        const c = r.card;
        assert.ok(c.x >= opts.boundsPadding && c.x + c.w <= BOUNDS.width - opts.boundsPadding,
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
        const distLeft = c.x - opts.boundsPadding;
        const distRight = (BOUNDS.width - opts.boundsPadding) - (c.x + c.w);
        const distTop = c.y - opts.boundsPadding;
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

test('ring: PCA does not degenerate on coincident markers (stacked into one card)', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 512, y: 256 },
        { id: 2, x: 512, y: 256 },
        { id: 3, x: 512, y: 256 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    // Совпадающие маркеры объединяются в один стек — одна карточка.
    const nonNull = res.filter(r => r !== null);
    assert.strictEqual(nonNull.length, 1, 'coincident markers must produce exactly 1 stacked card');
    const r = nonNull[0];
    assert.ok(isFinite(r.bend.x) && isFinite(r.bend.y));
    assert.ok(isFinite(r.card.x) && isFinite(r.card.y));
    assert.ok(Array.isArray(r.stacked), 'stacked card must have stacked array');
    assert.strictEqual(r.stacked.length, 3, 'stacked array must contain all 3 ids');
    // Порядок стабильный (по возрастанию NORAD ID)
    assert.deepStrictEqual(r.stacked, [1, 2, 3]);
});

test('ring: anneal layout has angled leader (stem diagonal + horizontal tail)', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    for (const r of res) {
        const tail = r.tailEnd || { x: r.bend.x, y: r.bend.y };
        assert.notStrictEqual(r.bend.x, r.marker.x,
            `id ${r.id}: stem must not be strictly vertical`);
        assert.notStrictEqual(r.bend.y, r.marker.y,
            `id ${r.id}: stem must not be strictly horizontal`);
        if (r.attach === 'horizontal') {
            assert.notStrictEqual(tail.x, r.bend.x,
                `id ${r.id}: horizontal tail must exist`);
            assert.strictEqual(tail.y, r.bend.y,
                `id ${r.id}: tail must stay horizontal`);
        }
    }
});

test('ring: stem attaches to nearest card edge (no pierce through bbox)', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    for (const r of res) {
        const pl = { bend: r.bend, card: r.card };
        assert.strictEqual(stemPiercesCard(r.marker, pl), false,
            `stem pierces card ${r.id}`);
        if (r.attach === 'horizontal') {
            const cardCenterY = r.card.y + r.card.h / 2;
            assert.ok(Math.abs(r.bend.y - cardCenterY) < 1e-6,
                `horizontal bend.y=${r.bend.y} vs center Y=${cardCenterY}`);
        } else {
            const cardCenterX = r.card.x + r.card.w / 2;
            assert.ok(Math.abs(r.bend.x - cardCenterX) < 1e-6,
                `vertical bend.x=${r.bend.x} vs center X=${cardCenterX}`);
        }
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
        w: 2 * m.iconRadius, h: 2 * m.iconRadius,
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
    const a = new CalloutLayout(RING_OPTS).layout(RING_CLUSTER_6, [], BOUNDS);
    const b = new CalloutLayout(RING_OPTS).layout(RING_CLUSTER_6, [], BOUNDS, []);
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
        optSum += countCardCrossings(res[i].card, padding, segments);
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
        { x1: 0, y1: 256, x2: 1024, y2: 256 },
    ];
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    const pad = RING_OPTS.boundsPadding;
    for (const r of res) {
        const c = r.card;
        assert.ok(c.x >= pad, `card.x=${c.x} crossed left bound`);
        assert.ok(c.y >= pad, `card.y=${c.y} crossed top bound`);
        assert.ok(c.x + c.w <= BOUNDS.width - pad, `card crossed right bound: ${JSON.stringify(c)}`);
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

// ── Ring layout: leader-линия (stem + tail) vs forbidden tracks ───────────
//
// Шаг 1: расширение проверки пересечений. Раньше `countCardCrossings` считал
// только пересечения bbox карточки с запретными трассами. На плотных кадрах
// это приводило к тому, что сама карточка стояла «чисто», но её leader-линия
// (диагональный stem + горизонтальный tail) шла сквозь жёлтую/синюю трассу.
// Теперь учитываем пересечения всей leader-линии.

console.log('\nCallouts: ring layout — leader line (stem + tail) vs tracks');

/** Извлекает конечную точку хвоста leader-линии из layout-объекта. */
function tailEndOfLayout(lt) {
    if (lt.tailEnd) { return lt.tailEnd; }
    if (lt.attach === 'vertical') {
        return { x: lt.bend.x, y: lt.bend.y };
    }
    const x = (lt.card.x > lt.bend.x) ? lt.card.x : (lt.card.x + lt.card.w);
    return { x: x, y: lt.bend.y };
}

/** Пересекает ли leader-линия (stem + tail) заданный запретный отрезок? */
function leaderHitsSegment(lt, seg) {
    const a1 = { x: seg.x1, y: seg.y1 };
    const a2 = { x: seg.x2, y: seg.y2 };
    const tail = tailEndOfLayout(lt);
    return segmentsIntersect(lt.marker, lt.bend, a1, a2) ||
           segmentsIntersect(lt.bend, tail, a1, a2);
}

/** Суммарное число пересечений (card bbox + stem + tail) с сегментами. */
function countLeaderTrackHits(lt, padding, segments) {
    let n = countCardCrossings(lt.card, padding, segments);
    for (const s of segments) {
        if (leaderHitsSegment(lt, s)) { n++; }
    }
    return n;
}

test('ring: stem does not cross a horizontal track above the cluster', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, {
        annealSweeps: 2000,
        annealWLeaderTrack: 1500,
        annealSeed: 55,
    }));
    // Маркеры «крест» вокруг центра: самый верхний — (512, 228).
    // Горизонтальная трасса y=200 проходит выше всех маркеров; карточка
    // верхнего маркера в старом алгоритме встанет на y<200 (bbox не
    // пересекает y=200), а stem от (512,228) к bend(~512,~100) пробьёт трассу.
    const markers = [
        { id: 1, x: 540, y: 256 },
        { id: 2, x: 484, y: 256 },
        { id: 3, x: 512, y: 228 },
        { id: 4, x: 512, y: 284 },
    ];
    const segments = [
        { x1: 0, y1: 200, x2: 1024, y2: 200 },
    ];
    const res = layout.layout(markers, [], BOUNDS, segments);
    for (const r of res) {
        assert.ok(!leaderHitsSegment(r, segments[0]),
            `leader ${r.id} (stem|tail) crosses horizontal track y=200: ` +
            `marker=${JSON.stringify(r.marker)} bend=${JSON.stringify(r.bend)} ` +
            `card=${JSON.stringify(r.card)}`);
    }
});

test('ring: tail does not cross a vertical track on the far side of the card', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 500, y: 256 },
        { id: 2, x: 520, y: 240 },
        { id: 3, x: 520, y: 272 },
    ];
    // Вертикальная трасса сразу за правым краем вероятной карточки.
    const segments = [
        { x1: 720, y1: 0, x2: 720, y2: 512 },
    ];
    const res = layout.layout(markers, [], BOUNDS, segments);
    for (const r of res) {
        assert.ok(!leaderHitsSegment(r, segments[0]),
            `leader ${r.id} (stem|tail) pierces vertical track x=720`);
    }
});

test('ring: combined — neither card bbox nor leader crosses horizontal track', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const segments = [
        { x1: 0, y1: 256, x2: 1024, y2: 256 },
    ];
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    const padding = RING_OPTS.forbiddenPadding != null ? RING_OPTS.forbiddenPadding : 5;
    for (const r of res) {
        assert.strictEqual(countCardCrossings(r.card, padding, segments), 0,
            `card ${r.id} bbox crosses track`);
        assert.ok(!leaderHitsSegment(r, segments[0]),
            `leader ${r.id} crosses track (stem+tail check)`);
    }
});

test('ring: leader-aware avoidance minimises total (card + stem + tail) hits vs baseline', () => {
    // «Забор» из трёх горизонтальных трасс — почти невозможно уйти.
    // Best-effort контракт: сумма пересечений leader'а ≤ сумме без обхода.
    const layout = new CalloutLayout(RING_OPTS);
    const segments = [
        { x1: 0, y1: 180, x2: 1024, y2: 180 },
        { x1: 0, y1: 260, x2: 1024, y2: 260 },
        { x1: 0, y1: 340, x2: 1024, y2: 340 },
    ];
    const optimised = layout.layout(RING_CLUSTER_6, [], BOUNDS, segments);
    const baseline = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    const padding = RING_OPTS.forbiddenPadding != null ? RING_OPTS.forbiddenPadding : 5;
    let baseSum = 0, optSum = 0;
    for (let i = 0; i < optimised.length; i++) {
        baseSum += countLeaderTrackHits(baseline[i], padding, segments);
        optSum += countLeaderTrackHits(optimised[i], padding, segments);
    }
    assert.ok(optSum <= baseSum,
        `leader-aware avoidance regressed: baseline=${baseSum}, optimised=${optSum}`);
});

// ── Ring layout: циклический порядок θ по маркерам (шаг 5) ────────────────
//
// Шаг 5: первичное размещение сохраняет циклический порядок маркеров вокруг
// PCA-эллипса. Это математически исключает пересечение leader-линий двух
// ближайших по углу соседей — самая частая причина «лапши» на плотных кадрах.

console.log('\nCallouts: ring layout — cyclic θ order preservation (step 5)');

/**
 * Циклический порядок: массив `ids` (angles) «эквивалентен» `expected`, если
 * существует такой сдвиг/разворот, что они совпадают. Возвращает true/false.
 */
function cyclicOrderMatches(order, expected) {
    const n = order.length;
    if (n !== expected.length) { return false; }
    for (let dir = 0; dir < 2; dir++) {
        const seq = (dir === 0) ? expected : expected.slice().reverse();
        for (let shift = 0; shift < n; shift++) {
            let ok = true;
            for (let i = 0; i < n; i++) {
                if (order[i] !== seq[(i + shift) % n]) { ok = false; break; }
            }
            if (ok) { return true; }
        }
    }
    return false;
}

test('ring: cyclic order of cards around ellipse matches cyclic order of markers', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // 6 маркеров равномерно по окружности радиуса 40 вокруг (512, 256).
    // Ожидаемый циклический порядок id: 1→2→3→4→5→6.
    const N = 6;
    const markers = [];
    for (let i = 0; i < N; i++) {
        const t = (2 * Math.PI * i) / N;
        markers.push({ id: i + 1, x: 512 + 40 * Math.cos(t), y: 256 + 40 * Math.sin(t) });
    }
    const res = layout.layout(markers, [], BOUNDS);
    // Центр «кольца карточек» — среднее от центров всех карточек.
    let sx = 0, sy = 0;
    for (const r of res) { sx += r.card.x + r.card.w / 2; sy += r.card.y + r.card.h / 2; }
    const cx = sx / res.length, cy = sy / res.length;
    const byAngle = res.slice().sort((A, B) => {
        const tA = Math.atan2(A.card.y + A.card.h / 2 - cy, A.card.x + A.card.w / 2 - cx);
        const tB = Math.atan2(B.card.y + B.card.h / 2 - cy, B.card.x + B.card.w / 2 - cx);
        return tA - tB;
    });
    const order = byAngle.map(r => r.id);
    assert.ok(cyclicOrderMatches(order, [1, 2, 3, 4, 5, 6]),
        `cyclic order mismatch: got ${JSON.stringify(order)}`);
});

test('ring: 8-marker ring — zero leader crossings (cyclic order guarantees it)', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const N = 8;
    const markers = [];
    for (let i = 0; i < N; i++) {
        const t = (2 * Math.PI * i) / N;
        markers.push({ id: i + 1, x: 512 + 50 * Math.cos(t), y: 256 + 50 * Math.sin(t) });
    }
    const res = layout.layout(markers, [], BOUNDS);
    assert.strictEqual(countCrossings(res), 0,
        'ring with 8 markers must have 0 leader crossings after cyclic θ ordering');
});

test('ring: asymmetric cluster (markers lumped on one side) — still zero leader crossings', () => {
    // Все маркеры на правой половине кадра — без упорядочивания θ могли бы
    // дать стыки, когда distributeAnglesAround распихал их в пределах узкого
    // сектора; циклический порядок гарантирует 0 пересечений.
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 600, y: 210 },
        { id: 2, x: 640, y: 240 },
        { id: 3, x: 660, y: 280 },
        { id: 4, x: 630, y: 310 },
        { id: 5, x: 580, y: 290 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    assert.strictEqual(countCrossings(res), 0,
        'asymmetric ring cluster: expected 0 leader crossings');
});

test('ring: 10 near-collinear markers (horizontal line, small y jitter) — zero leader crossings', () => {
    // Реалистичный патологический: маркеры сильно вытянуты по одной оси,
    // но имеют небольшой разброс по y (как проекции орбитальных КА вблизи
    // наклонной трассы). PCA не вырождается → циклический порядок θ
    // определён однозначно → переназначка спиц убирает все пересечения.
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [];
    for (let i = 0; i < 10; i++) {
        markers.push({ id: i + 1, x: 340 + i * 30, y: 256 + ((i % 2) ? 1 : -1) * 4 });
    }
    const res = layout.layout(markers, [], BOUNDS);
    assert.strictEqual(countCrossings(res), 0,
        'near-collinear cluster: expected 0 leader crossings (cyclic ordering must apply)');
});

test('ring: 12 markers in tight grid — zero leader crossings', () => {
    // Плотная прямоугольная группа 4×3 с шагом 20 px. При таком плотном
    // расположении Phase A без циклической раскладки двигает слоты
    // независимо и нередко создаёт X-образные пересечения.
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [];
    let id = 1;
    for (let r = 0; r < 3; r++) {
        for (let c = 0; c < 4; c++) {
            markers.push({ id: id++, x: 470 + c * 20, y: 230 + r * 20 });
        }
    }
    const res = layout.layout(markers, [], BOUNDS);
    assert.strictEqual(countCrossings(res), 0,
        '4x3 tight grid: expected 0 leader crossings');
});

// ── Ring layout: анти-cross пост-проход свапом θ (шаг 2) ──────────────────
//
// Шаг 2: в ring-режиме `_resolveCrossings` работает через обмен углами θ
// (аналог sector-swap в greedy-режиме). Применяется, если шаги 5+обход трасс
// оставили leader-пересечения из-за клампинга в bounds.

console.log('\nCallouts: ring layout — θ-swap anti-crossing post-pass (step 2)');

test('ring: post-pass removes leader crossings after forbidden-track avoidance', () => {
    // Патологический: плотный кластер + трасса через центр. После обхода
    // трассы Phase A/B двигают слоты, и порядок по θ может нарушиться —
    // leader-линии двух смежных слотов пересекаются. Пост-проход свапом
    // θ должен их развести.
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 470, y: 240 },
        { id: 2, x: 510, y: 240 },
        { id: 3, x: 550, y: 240 },
        { id: 4, x: 470, y: 272 },
        { id: 5, x: 510, y: 272 },
        { id: 6, x: 550, y: 272 },
    ];
    // Диагональная трасса через центр — «режет» кольцо на два коридора.
    const segments = [
        { x1: 100, y1: 100, x2: 900, y2: 400 },
    ];
    const res = layout.layout(markers, [], BOUNDS, segments);
    assert.strictEqual(countCrossings(res), 0,
        'ring θ-swap post-pass must remove all leader crossings even after track avoidance');
});

test('ring: resolveCrossings=false (ring) allows pre-swap crossings on pathological input', () => {
    // Чистый сигнал SANITY: если выключить пост-проход, на патологическом
    // кадре остаются пересечения. Включённый — убирает.
    const mk = [
        { id: 1, x: 470, y: 240 },
        { id: 2, x: 510, y: 240 },
        { id: 3, x: 550, y: 240 },
        { id: 4, x: 470, y: 272 },
        { id: 5, x: 510, y: 272 },
        { id: 6, x: 550, y: 272 },
    ];
    const segments = [{ x1: 100, y1: 100, x2: 900, y2: 400 }];
    const optsOn = Object.assign({}, RING_OPTS, { resolveCrossings: true });
    const optsOff = Object.assign({}, RING_OPTS, { resolveCrossings: false });
    const on = new CalloutLayout(optsOn ).layout(mk, [], BOUNDS, segments);
    const off = new CalloutLayout(optsOff).layout(mk, [], BOUNDS, segments);
    const cOn = countCrossings(on);
    const cOff = countCrossings(off);
    assert.ok(cOn <= cOff,
        `θ-swap post-pass must not increase crossings (on=${cOn}, off=${cOff})`);
});

// ── Ring layout: стекинг co-located маркеров ──────────────────────────────
//
// Co-located КА (distance ≤ stackDistance) объединяются в один «стек»:
// одна карточка с увеличенным cardHeight и несколькими строками имён.
// Ключевые гарантии: стабильный порядок строк (по возрастанию NORAD ID),
// height = N × stackLineHeight + padding, поглощённые маркеры → null в result.

console.log('\nCallouts: ring layout — stacking co-located markers');

test('ring: 4 co-located markers → 1 stacked card + 3 nulls', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 25544, x: 500, y: 250 },
        { id: 43205, x: 502, y: 252 },
        { id: 43206, x: 501, y: 249 },
        { id: 43207, x: 503, y: 251 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const nonNull = res.filter(r => r !== null);
    assert.strictEqual(nonNull.length, 1, 'co-located markers must produce 1 card');
    const card = nonNull[0];
    assert.ok(Array.isArray(card.stacked), 'must have stacked array');
    assert.strictEqual(card.stacked.length, 4);
    // Порядок по возрастанию NORAD ID
    assert.deepStrictEqual(card.stacked, [25544, 43205, 43206, 43207]);
});

test('ring: stacked card height = N × stackLineHeight + 4', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 503, y: 252 },
        { id: 3, x: 501, y: 249 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const card = res.filter(r => r !== null)[0];
    // stackLineHeight=14 (DEFAULTS), N=3 → 3×14+4 = 46
    const expectedH = 3 * 14 + 4;
    assert.strictEqual(card.card.h, expectedH,
        `stacked card height: expected ${expectedH}, got ${card.card.h}`);
});

test('ring: large stack (>maxVisible) has capped card height', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // 8 маркеров в одной точке — стек из 8 при maxVisible=4 → высота = (4+1)×18+4 = 94
    const markers = [];
    for (let i = 0; i < 8; i++) {
        markers.push({ id: i + 1, x: 500 + i, y: 250 + i });
    }
    const res = layout.layout(markers, [], BOUNDS);
    const card = res.filter(r => r !== null)[0];
    const maxVis = 4;
    const expectedH = (maxVis + 1) * 14 + 4; // 4 строки + 1 строка "...+N"
    assert.strictEqual(card.card.h, expectedH,
        `large stack height: expected ${expectedH}, got ${card.card.h}`);
    assert.strictEqual(card.stacked.length, 8);
});

test('ring: markers farther than stackDistance are NOT stacked', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Расстояние между маркерами > 12 (stackDistance)
    const markers = [
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 520, y: 270 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const nonNull = res.filter(r => r !== null);
    assert.strictEqual(nonNull.length, 2, 'distant markers must NOT be stacked');
    for (const r of nonNull) {
        assert.ok(!r.stacked || r.stacked.length <= 1,
            'distant markers must not have stacked array');
    }
});

test('ring: mixed — some co-located, some distant → separate stacks', () => {
    const layout = new CalloutLayout(RING_OPTS);
    // Два подкластера: (500,250)±3px и (600,300)±3px
    const markers = [
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 502, y: 252 },
        { id: 3, x: 600, y: 300 },
        { id: 4, x: 601, y: 302 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const nonNull = res.filter(r => r !== null);
    assert.strictEqual(nonNull.length, 2, 'two stacks expected');
    // Каждый стек имеет 2 id
    for (const r of nonNull) {
        assert.ok(Array.isArray(r.stacked), 'each group must be stacked');
        assert.strictEqual(r.stacked.length, 2);
    }
});

test('ring: stacked card id is the smallest NORAD in group', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 99999, x: 500, y: 250 },
        { id: 11111, x: 502, y: 252 },
        { id: 55555, x: 501, y: 249 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const card = res.filter(r => r !== null)[0];
    assert.strictEqual(card.id, 11111, 'stacked card id must be smallest NORAD');
    assert.deepStrictEqual(card.stacked, [11111, 55555, 99999]);
});

// ── Ring layout: избегание препятствий-иконок (шаг 1) ─────────────────────
//
// Шаг 1 фикса callout-разметки: пост-проход `_avoidForbiddenSegments` теперь
// учитывает не только запретные трассы, но и obstacle-bbox (иконки чужих КА,
// подписи городов, observer). Лекс-стоимость: [iconHits, cardHits, leaderHits].
// Карточка может не уйти от иконки только если эллипс/bounds не оставляют
// угла без перекрытия (best-effort).

console.log('\nCallouts: ring layout — obstacle (icon) avoidance (step 1)');

test('ring: card avoids a foreign obstacle in its default sector', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, { annealSeed: 77 }));
    const markers = [
        { id: 1, x: 400, y: 256 },
        { id: 2, x: 560, y: 256 },
    ];
    // Препятствие на пути радиального seed для правого маркера.
    const obstacles = [
        { x: 620, y: 240, w: 120, h: 36 },
    ];
    const res = layout.layout(markers, obstacles, BOUNDS);
    for (let i = 0; i < res.length; i++) {
        const c = res[i].card;
        for (let k = 0; k < obstacles.length; k++) {
            const ob = obstacles[k];
            const sepX = (c.x + c.w <= ob.x) || (ob.x + ob.w <= c.x);
            const sepY = (c.y + c.h <= ob.y) || (ob.y + ob.h <= c.y);
            assert.ok(sepX || sepY,
                `card[${i}] overlaps obstacle[${k}]: ` +
                `card=${JSON.stringify(c)} ob=${JSON.stringify(ob)}`);
        }
    }
});

test('ring: own marker icon does not fight its own card (no self-block)', () => {
    // Свой маркер находится В ТОЧКЕ (m.x, m.y); собственная иконка
    // включается в obstacles (bbox содержит точку маркера). Алгоритм должен
    // её игнорировать — иначе карточка не может встать рядом со своим КА.
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 480, y: 256 },
        { id: 2, x: 544, y: 256 },
    ];
    const r = 18;
    const obstacles = markerObstacles(markers, r);
    const res = layout.layout(markers, obstacles, BOUNDS);
    assert.strictEqual(res.length, markers.length);
    for (let i = 0; i < res.length; i++) {
        const m = markers[i];
        const c = res[i].card;
        // Чужие иконки не пересекаются:
        for (let k = 0; k < obstacles.length; k++) {
            if (k === i) { continue; }
            const ob = obstacles[k];
            const sepX = (c.x + c.w <= ob.x) || (ob.x + ob.w <= c.x);
            const sepY = (c.y + c.h <= ob.y) || (ob.y + ob.h <= c.y);
            assert.ok(sepX || sepY,
                `card[${i}] overlaps foreign icon[${k}]`);
        }
        // Bend/card находятся в разумной близости от своего маркера
        // (карточка не «ускакала» из-за того что её собственная иконка
        // считается препятствием).
        const dx = (c.x + c.w / 2) - m.x;
        const dy = (c.y + c.h / 2) - m.y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        assert.ok(dist < 300,
            `card[${i}] too far from its marker (dist=${dist.toFixed(1)})`);
    }
});

test('ring: cluster of 6 in dense scene with city obstacles — minimises icon hits vs baseline', () => {
    // Плотный кластер + 2 препятствия-«города» рядом. Best-effort: число
    // карточек, перекрывающих obstacle, не должно вырасти относительно
    // baseline без obstacle-обхода.
    const layout = new CalloutLayout(RING_OPTS);
    const obstacles = [
        { x: 360, y: 200, w: 90, h: 16 }, // «город» слева-сверху от кластера
        { x: 600, y: 290, w: 90, h: 16 }, // «город» справа-снизу
    ];
    const baseline = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    const res = layout.layout(RING_CLUSTER_6, obstacles, BOUNDS);
    let baseHits = 0, optHits = 0;
    for (const r of baseline) {
        for (const ob of obstacles) {
            if (overlap(bbox(r.card), ob)) { baseHits++; }
        }
    }
    for (const r of res) {
        for (const ob of obstacles) {
            if (overlap(bbox(r.card), ob)) { optHits++; }
        }
    }
    assert.ok(optHits <= baseHits,
        `obstacle-aware avoidance regressed: baseline=${baseHits}, opt=${optHits}`);
});

// ── Ring layout: θ-swap для пересечений leader-линий и card-to-card overlaps ─

console.log('\nCallouts: ring layout — θ-swap (leader crossings + card overlaps)');

test('ring: θ-swap resolves leader-line crossing for opposing markers', () => {
    // Два маркера расположены так, что при наивном распределении θ
    // (θ на противоположной стороне от другого маркера) их leader'ы пересекаются.
    // Маркер A внизу-слева, маркер B вверху-справа — карточки по умолчанию
    // ставятся по θ маркера, и stem'ы X-образно перекрещиваются.
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 480, y: 280 },
        { id: 2, x: 540, y: 230 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const valid = res.filter(r => r !== null);
    // После swap leader'ы не должны пересекаться.
    for (let i = 0; i < valid.length; i++) {
        for (let j = i + 1; j < valid.length; j++) {
            const li = { marker: valid[i].marker, bend: valid[i].bend, card: valid[i].card };
            const lj = { marker: valid[j].marker, bend: valid[j].bend, card: valid[j].card };
            assert.ok(!leadersIntersect(li, lj),
                `leaders ${valid[i].id} & ${valid[j].id} still cross after swap`);
        }
    }
});

test('ring: θ-swap resolves card-to-card overlap when simple exchange fixes it', () => {
    // Специальный сценарий: 4 маркера создают ситуацию где свап позиций
    // двух карточек устраняет перекрытие bbox'ов.
    const layout = new CalloutLayout(RING_OPTS);
    const markers = [
        { id: 1, x: 500, y: 250 },
        { id: 2, x: 510, y: 260 },
        { id: 3, x: 520, y: 250 },
        { id: 4, x: 530, y: 260 },
    ];
    const res = layout.layout(markers, [], BOUNDS);
    const valid = res.filter(r => r !== null);
    const gap = RING_OPTS.minCardGap || 4;
    let overlaps = 0;
    for (let i = 0; i < valid.length; i++) {
        for (let j = i + 1; j < valid.length; j++) {
            if (bboxOverlap(valid[i].card, valid[j].card, gap)) { overlaps++; }
        }
    }
    // Свап должен минимизировать/устранить перекрытия (best-effort)
    assert.strictEqual(overlaps, 0, `card-to-card overlaps remain: ${overlaps}`);
});

test('anneal: SA reduces energy vs radial seed on cluster of 6', () => {
    const layout = new CalloutLayout(RING_OPTS);
    const { virtualMarkers } = buildVirtualStacks(RING_CLUSTER_6, RING_OPTS);
    const seed = annealInitialState(virtualMarkers, BOUNDS, RING_OPTS, new Map());
    const eSeed = computeAnnealEnergy(seed, virtualMarkers, [], [], BOUNDS, RING_OPTS);
    const res = layout.layout(RING_CLUSTER_6, [], BOUNDS);
    const placements = res.filter((r) => r !== null).map((r) => ({
        cardX: r.card.x,
        cardY: r.card.y,
    }));
    const eFinal = computeAnnealEnergy(placements, virtualMarkers, [], [], BOUNDS, RING_OPTS);
    assert.ok(eFinal <= eSeed,
        `SA regressed energy: seed=${eSeed}, final=${eFinal}`);
});

test('placementFromCard: vertical attachment when card below marker', () => {
    const marker = { x: 200, y: 100 };
    const cardW = 110;
    const cardH = 28;
    const cardX = 155;
    const cardY = 130;
    const pl = placementFromCard(marker, cardX, cardY, cardW, cardH, OPTS);
    assert.strictEqual(pl.attach, 'vertical');
    assert.strictEqual(pl.tailEnd.x, cardX + cardW / 2);
    assert.strictEqual(pl.tailEnd.y, cardY);
    assert.strictEqual(pl.bend.x, cardX + cardW / 2);
    assert.ok(pl.bend.y <= cardY - OPTS.tailLength + 1e-6);
    assert.strictEqual(!stemPiercesCard(marker, pl), true);
});

test('placementFromCard: horizontal attachment when card beside marker', () => {
    const marker = { x: 100, y: 150 };
    const cardW = 110;
    const cardH = 28;
    const cardX = 220;
    const cardY = 136;
    const pl = placementFromCard(marker, cardX, cardY, cardW, cardH, OPTS);
    assert.strictEqual(pl.attach, 'horizontal');
    assert.strictEqual(pl.tailEnd.x, cardX);
    assert.strictEqual(pl.tailEnd.y, cardY + cardH / 2);
    assert.strictEqual(pl.bend.x, cardX - OPTS.tailLength);
    assert.strictEqual(pl.bend.y, cardY + cardH / 2);
});

test('placementFromCard: distant card attaches to nearest edge', () => {
    const marker = { x: 720, y: 120 };
    const cardW = 110;
    const cardH = 28;
    const cardX = 280;
    const cardY = 420;
    const pl = placementFromCard(marker, cardX, cardY, cardW, cardH, OPTS);
    assert.strictEqual(pl.attach, 'horizontal');
    assert.strictEqual(pl.tailEnd.x, cardX + cardW);
    assert.notStrictEqual(pl.tailEnd.x, pl.bend.x);
    assert.strictEqual(pl.tailEnd.y, pl.bend.y);
});

test('accent stripe on same edge as tail (horizontal)', () => {
    const cardW = 110;
    const cardH = 28;
    const leftPl = placementFromCard({ x: 100, y: 150 }, 220, 136, cardW, cardH);
    const leftLt = {
        marker: { x: 100, y: 150 },
        bend: leftPl.bend,
        tailEnd: leftPl.tailEnd,
        attach: leftPl.attach,
        card: leftPl.card,
    };
    assert.strictEqual(accentOnCardRight(leftLt), false, 'tail on left → accent left');

    const rightPl = placementFromCard({ x: 400, y: 150 }, 220, 136, cardW, cardH);
    const rightLt = {
        marker: { x: 400, y: 150 },
        bend: rightPl.bend,
        tailEnd: rightPl.tailEnd,
        attach: rightPl.attach,
        card: rightPl.card,
    };
    assert.strictEqual(accentOnCardRight(rightLt), true, 'tail on right → accent right');
});

test('accent stripe on same edge as tail (vertical)', () => {
    const cardW = 110;
    const cardH = 28;
    const topPl = placementFromCard({ x: 200, y: 100 }, 155, 130, cardW, cardH);
    const topLt = {
        marker: { x: 200, y: 100 },
        bend: topPl.bend,
        tailEnd: topPl.tailEnd,
        attach: topPl.attach,
        card: topPl.card,
    };
    assert.strictEqual(accentOnCardTop(topLt), true, 'tail on top → accent top');
    assert.strictEqual(accentOnCardBottom(topLt), false);

    const bottomPl = placementFromCard({ x: 200, y: 200 }, 155, 100, cardW, cardH);
    const bottomLt = {
        marker: { x: 200, y: 200 },
        bend: bottomPl.bend,
        tailEnd: bottomPl.tailEnd,
        attach: bottomPl.attach,
        card: bottomPl.card,
    };
    assert.strictEqual(accentOnCardBottom(bottomLt), true, 'tail on bottom → accent bottom');
    assert.strictEqual(accentOnCardTop(bottomLt), false);
});

function assertCardDrift(a, b, dx, dy, id) {
    const tol = 1e-4;
    assert.ok(
        Math.abs(b.card.x - (a.card.x + dx)) < tol,
        `id ${id} card.x drift: got ${b.card.x}, expected ${a.card.x + dx}`
    );
    assert.ok(
        Math.abs(b.card.y - (a.card.y + dy)) < tol,
        `id ${id} card.y drift: got ${b.card.y}, expected ${a.card.y + dy}`
    );
}

test('anneal: stable layout on marker micro-move (skip SA)', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, { annealSeed: 42 }));
    const markers1 = RING_CLUSTER_6.map((m) => Object.assign({}, m));
    const res1 = layout.layout(markers1, [], BOUNDS);
    const markers2 = markers1.map((m) => ({
        id: m.id,
        x: m.x + 2,
        y: m.y - 1,
        color: m.color,
    }));
    const res2 = layout.layout(markers2, [], BOUNDS);
    const byId1 = {};
    const byId2 = {};
    for (const r of res1) {
        if (r) { byId1[r.id] = r; }
    }
    for (const r of res2) {
        if (r) { byId2[r.id] = r; }
    }
    for (const id of Object.keys(byId1)) {
        const a = byId1[id];
        const b = byId2[id];
        assertCardDrift(a, b, 2, -1, id);
    }
});

test('anneal: own card never overlaps own tracked icon', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, {
        annealSeed: 42,
        iconObstacleGap: 10,
    }));
    const trackedR = 24;
    const m = { id: 99, x: 512, y: 256, iconRadius: trackedR };
    const iconBox = {
        x: m.x - trackedR, y: m.y - trackedR, w: 2 * trackedR, h: 2 * trackedR,
    };
    const res = layout.layout([m], [iconBox], BOUNDS);
    const c = res[0].card;
    const sepX = c.x + c.w <= iconBox.x || iconBox.x + iconBox.w <= c.x;
    const sepY = c.y + c.h <= iconBox.y || iconBox.y + iconBox.h <= c.y;
    assert.ok(sepX || sepY, 'single tracked: card overlaps own icon');
});

test('anneal: leader does not cross foreign satellite icon', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, {
        annealSeed: 42,
        iconObstacleGap: 10,
    }));
    const r = 22;
    const top = { id: 1, x: 512, y: 200, iconRadius: r };
    const middle = { id: 2, x: 512, y: 280, iconRadius: r };
    const markers = [top, middle];
    const obstacles = markers.map((m) => ({
        x: m.x - r - 10, y: m.y - r - 10,
        w: 2 * (r + 10), h: 2 * (r + 10),
    }));
    const res = layout.layout(markers, obstacles, BOUNDS);
    const topLt = res.find((x) => x && x.id === 1);
    assert.ok(topLt, 'missing top layout');
    const pl = {
        marker: { x: top.x, y: top.y },
        bend: topLt.bend,
        tailEnd: topLt.tailEnd,
        attach: topLt.attach,
        card: topLt.card,
    };
    const hits = countLeaderForeignIconHits(
        { id: 1, x: top.x, y: top.y, iconRadius: r },
        pl,
        [{ id: 1, x: top.x, y: top.y, iconRadius: r },
            { id: 2, x: middle.x, y: middle.y, iconRadius: r }],
        10
    );
    assert.strictEqual(hits, 0, 'top stem crosses middle icon');
});

test('anneal: card does not overlap foreign satellite icon', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, {
        annealSeed: 42,
        iconObstacleGap: 10,
    }));
    const r = 20;
    const left = { id: 1, x: 400, y: 300, iconRadius: r };
    const right = { id: 2, x: 430, y: 310, iconRadius: r };
    const markers = [left, right];
    const obstacles = markers.map((m) => ({
        x: m.x - r - 10, y: m.y - r - 10,
        w: 2 * (r + 10), h: 2 * (r + 10),
    }));
    const res = layout.layout(markers, obstacles, BOUNDS);
    const leftLt = res.find((x) => x && x.id === 1);
    assert.ok(leftLt, 'missing left layout');
    const iconBox = {
        x: right.x - r - 10, y: right.y - r - 10,
        w: 2 * (r + 10), h: 2 * (r + 10),
    };
    const c = leftLt.card;
    const sepX = c.x + c.w <= iconBox.x || iconBox.x + iconBox.w <= c.x;
    const sepY = c.y + c.h <= iconBox.y || iconBox.y + iconBox.h <= c.y;
    assert.ok(sepX || sepY, 'card overlaps foreign icon bbox');
});

test('anneal: clustered cards do not overlap or sit on foreign leaders', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, {
        annealSeed: 77,
        iconObstacleGap: 10,
        leaderCardPadding: 4,
    }));
    const r = 18;
    const markers = [
        { id: 1, x: 480, y: 380, iconRadius: r },
        { id: 2, x: 520, y: 395, iconRadius: r },
        { id: 3, x: 500, y: 410, iconRadius: r },
        { id: 4, x: 540, y: 385, iconRadius: r },
        { id: 5, x: 510, y: 370, iconRadius: r },
        { id: 6, x: 530, y: 405, iconRadius: r },
    ];
    const obstacles = markers.map((m) => ({
        x: m.x - r - 10, y: m.y - r - 10,
        w: 2 * (r + 10), h: 2 * (r + 10),
    }));
    const res = layout.layout(markers, obstacles, BOUNDS);
    const gap = RING_OPTS.minCardGap || 4;
    const leaderPad = 4;
    for (let i = 0; i < res.length; i++) {
        if (!res[i]) { continue; }
        for (let j = i + 1; j < res.length; j++) {
            if (!res[j]) { continue; }
            assert.ok(
                !bboxOverlap(res[i].card, res[j].card, gap),
                `cards ${res[i].id} and ${res[j].id} overlap`
            );
        }
        const cardOb = {
            x: res[i].card.x - leaderPad,
            y: res[i].card.y - leaderPad,
            w: res[i].card.w + 2 * leaderPad,
            h: res[i].card.h + 2 * leaderPad,
        };
        for (let j = 0; j < res.length; j++) {
            if (j === i || !res[j]) { continue; }
            const m = markers.find((x) => x.id === res[j].id);
            const pl = placementFromCard(
                { x: m.x, y: m.y },
                res[j].card.x, res[j].card.y, res[j].card.w, res[j].card.h
            );
            const hits = countLeaderObstacleHits(
                { x: m.x, y: m.y }, pl, [cardOb]
            );
            assert.strictEqual(
                hits, 0,
                `leader of ${res[j].id} crosses card ${res[i].id}`
            );
        }
    }
});

test('anneal: sticky layout when obstacles/segments shift with markers', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, { annealSeed: 42 }));
    const markers1 = RING_CLUSTER_6.map((m) => Object.assign({}, m));
    const segments = [{ x1: 900, y1: 0, x2: 900, y2: 512 }];
    const res1 = layout.layout(markers1, [], BOUNDS, segments);
    const markers2 = markers1.map((m) => ({
        id: m.id,
        x: m.x + 3,
        y: m.y - 2,
        color: m.color,
    }));
    const obstacles = markers2.map((m) => ({
        x: m.x - 20,
        y: m.y - 20,
        w: 40,
        h: 40,
    }));
    const res2 = layout.layout(markers2, obstacles, BOUNDS, segments);
    for (const r of res1) {
        if (!r) { continue; }
        const b = res2.find((x) => x && x.id === r.id);
        assert.ok(b, `missing id ${r.id}`);
        assertCardDrift(r, b, 3, -2, r.id);
    }
});

test('anneal: sticky with forbidden tracks on marker micro-move (no SA rerun)', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, { annealSeed: 42 }));
    const markers1 = RING_CLUSTER_6.map((m) => Object.assign({}, m));
    const segments = [{ x1: 900, y1: 0, x2: 900, y2: 512 }];
    const res1 = layout.layout(markers1, [], BOUNDS, segments);
    const markers2 = markers1.map((m) => ({
        id: m.id,
        x: m.x + 2,
        y: m.y - 1,
        color: m.color,
    }));
    const res2 = layout.layout(markers2, [], BOUNDS, segments);
    const byId1 = {};
    const byId2 = {};
    for (const r of res1) {
        if (r) { byId1[r.id] = r; }
    }
    for (const r of res2) {
        if (r) { byId2[r.id] = r; }
    }
    for (const id of Object.keys(byId1)) {
        assertCardDrift(byId1[id], byId2[id], 2, -1, id);
    }
});

test('anneal: sticky stable when forbidden segments shift on screen (no track nudge)', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, { annealSeed: 42 }));
    const markers1 = RING_CLUSTER_6.map((m) => Object.assign({}, m));
    const segments1 = [{ x1: 350, y1: 80, x2: 350, y2: 430 }];
    const res1 = layout.layout(markers1, [], BOUNDS, segments1);
    const markers2 = markers1.map((m) => ({
        id: m.id,
        x: m.x + 2,
        y: m.y - 1,
        color: m.color,
    }));
    // Трасса сместилась на экране (как при движении КА / обновлении ground track).
    const segments2 = [{ x1: 358, y1: 85, x2: 358, y2: 435 }];
    const res2 = layout.layout(markers2, [], BOUNDS, segments2);
    for (const r of res1) {
        if (!r) { continue; }
        const b = res2.find((x) => x && x.id === r.id);
        assert.ok(b, `missing id ${r.id}`);
        assertCardDrift(r, b, 2, -1, r.id);
    }
});

test('anneal: subsampleForbiddenSegments caps segment count', () => {
    const segs = [];
    for (let i = 0; i < 500; i++) {
        segs.push({ x1: i, y1: 0, x2: i + 1, y2: 10 });
    }
    const sub = subsampleForbiddenSegments(segs, 120);
    assert.strictEqual(sub.length, 120);
    assert.strictEqual(sub[0].x1, segs[0].x1);
});

test('anneal: same structure key skips SA on sticky violation (no card jump)', () => {
    const layout = new CalloutLayout(Object.assign({}, RING_OPTS, {
        annealSeed: 42,
        annealMaxSegments: 120,
    }));
    const markers = RING_CLUSTER_6.map((m) => Object.assign({}, m));
    const segments = [{ x1: 900, y1: 0, x2: 900, y2: 512 }];
    const res1 = layout.layout(markers, [], BOUNDS, segments);
    // Сегмент сдвинулся, но bucket тот же — SA не должен перезапускаться.
    const shiftedSeg = [{ x1: 910, y1: 5, x2: 910, y2: 517 }];
    const res2 = layout.layout(
        markers.map((m) => ({ id: m.id, x: m.x + 2, y: m.y - 1, color: m.color })),
        [], BOUNDS, shiftedSeg
    );
    for (const r of res1) {
        if (!r) { continue; }
        const b = res2.find((x) => x && x.id === r.id);
        assert.ok(b, `missing id ${r.id}`);
        assertCardDrift(r, b, 2, -1, r.id);
    }
});

test('anneal: cluster ring seed places cards outside icon bbox (before SA)', () => {
    const opts = Object.assign({}, RING_OPTS, {
        annealSeed: 42,
        clusterDistance: 72,
        ringGap: 70,
    });
    const { virtualMarkers } = buildVirtualStacks(RING_CLUSTER_6, opts);
    const cache = new Map();
    const pending = virtualMarkers.map((_, i) => i);
    const seeds = clusterRingSeedState(virtualMarkers, BOUNDS, opts, cache, pending);
    assert.ok(seeds && seeds.size === virtualMarkers.length);

    let minX = Number.POSITIVE_INFINITY;
    let maxX = Number.NEGATIVE_INFINITY;
    let minY = Number.POSITIVE_INFINITY;
    let maxY = Number.NEGATIVE_INFINITY;
    for (const m of RING_CLUSTER_6) {
        if (m.x < minX) { minX = m.x; }
        if (m.x > maxX) { maxX = m.x; }
        if (m.y < minY) { minY = m.y; }
        if (m.y > maxY) { maxY = m.y; }
    }

    for (let i = 0; i < virtualMarkers.length; i++) {
        const pos = seeds.get(i);
        assert.ok(pos, `missing seed for index ${i}`);
        const dims = { w: opts.cardWidth, h: opts.cardHeight };
        const cx = pos.cardX + dims.w / 2;
        const cy = pos.cardY + dims.h / 2;
        const inside = (cx >= minX && cx <= maxX && cy >= minY && cy <= maxY);
        assert.ok(!inside,
            `seed ${i} center (${cx},${cy}) inside cluster bbox`);
    }
});

// ── SVG overlay линий (physical px viewBox) ─────────────────

test('buildCalloutLinesOverlaySpec: viewBox совпадает с размером canvas в px', () => {
    const spec = buildCalloutLinesOverlaySpec([], { width: 1600, height: 800 }, {});
    assert.strictEqual(spec.viewBox, '0 0 1600 800');
    assert.strictEqual(spec.lines.length, 0);
});

test('buildCalloutLinesOverlaySpec: вертикальный stem в physical px, не в процентах', () => {
    const layouts = [{
        id: 1,
        color: '#cf6868',
        marker: { x: 820, y: 200 },
        bend: { x: 820, y: 120 },
        tailEnd: { x: 820, y: 120 },
        attach: 'vertical',
        card: { x: 750, y: 90, w: 110, h: 28 },
    }];
    const spec = buildCalloutLinesOverlaySpec(
        layouts, { width: 1600, height: 800 }, { lineWidth: 1.5 }
    );
    assert.strictEqual(spec.lines.length, 1);
    assert.strictEqual(spec.lines[0].points, '820,200 820,120');
    assert.ok(!spec.lines[0].points.includes('51.25'), 'не должны быть проценты');
});

// ── Summary ────────────────────────────────────────────────

console.log(`\n${'='.repeat(50)}`);
console.log(`Results: ${passed} passed, ${failed} failed, ${passed + failed} total`);
console.log('='.repeat(50));

if (failed > 0) {
    process.exit(1);
}
