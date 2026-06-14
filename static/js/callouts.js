// callouts.js — выноски (callout/leader line) для подписей КА на карте.
// Геометрия: stem (маркер → bend) + горизонтальный хвост → карточка.
// Размещение: 8 секторов (sectors) или simulated annealing (anneal/ring).
//
// Контракт:
//   layout(markers, obstacles, bounds) → массив layout-объектов;
//   marker:    { id:number, x:number, y:number, color?:string }
//   obstacle:  { x, y, w, h }     // bbox области, в которую нельзя ставить карточку
//   bounds:    { width, height }  // размер canvas
//   layout-объект: { id, color, marker:{x,y}, bend:{x,y}, card:{x,y,w,h}, sector:string }

'use strict';

/**
 * 8 секторов «циферблата» вокруг маркера.
 * Имена: пара QUADRANT-SLOPE, где
 *   QUADRANT ∈ {RT, RB, LB, LT} — Right-Top / Right-Bottom / Left-Bottom / Left-Top;
 *   SLOPE    ∈ {steep, shallow} — крутой стержень (≈80°) или пологий (≈20°).
 *
 * Параметры:
 *   dxSign — направление стержня по X (+1 вправо / −1 влево).
 *   dySign — направление стержня по Y (+1 вниз  / −1 вверх) — Y растёт вниз в canvas.
 *   slopeDeg — наклон стержня к горизонту, строго в (0, 90) — исключает вертикаль/горизонталь.
 *   tailSign — направление горизонтального хвоста (+1 вправо / −1 влево).
 *
 * Угол поворота на изломе равен slopeDeg, поэтому 0 < slopeDeg < 90
 * гарантирует «один излом, угол < 90°».
 *
 * Шаг между крутым (80°) и пологим (20°) подобран так, чтобы в одной квадранте
 * две карточки не пересекались по Y при cardHeight=36 + minCardGap=6 и stemLength=80.
 */
const SECTORS = [
    { name: 'RT-steep', dxSign: +1, dySign: -1, slopeDeg: 80, tailSign: +1 },
    { name: 'RT-shallow', dxSign: +1, dySign: -1, slopeDeg: 20, tailSign: +1 },
    { name: 'RB-shallow', dxSign: +1, dySign: +1, slopeDeg: 20, tailSign: +1 },
    { name: 'RB-steep', dxSign: +1, dySign: +1, slopeDeg: 80, tailSign: +1 },
    { name: 'LB-steep', dxSign: -1, dySign: +1, slopeDeg: 80, tailSign: -1 },
    { name: 'LB-shallow', dxSign: -1, dySign: +1, slopeDeg: 20, tailSign: -1 },
    { name: 'LT-shallow', dxSign: -1, dySign: -1, slopeDeg: 20, tailSign: -1 },
    { name: 'LT-steep', dxSign: -1, dySign: -1, slopeDeg: 80, tailSign: -1 },
];

const DEFAULTS = {
    // Длина диагонального стержня (px) — обеспечивает разнесение steep/shallow.
    // Подобрана под уменьшенную карточку (cardHeight=30) так, чтобы крутой
    // (~80°) и пологий (~20°) стержни не давали наложения по Y.
    stemLength: 64,
    // Длина горизонтального хвоста (px)
    tailLength: 18,
    cardWidth: 110,
    cardHeight: 28,
    // Минимальный зазор между карточками
    minCardGap: 6,
    // Отступ от края canvas
    boundsPadding: 8,
    // Пост-проход «развести пересекающиеся линии свапом секторов»
    resolveCrossings: true,
    // Максимум итераций пост-прохода
    crossingsMaxPasses: 4,
    // Способ группировки карточек:
    //   'sectors' — 8-секторный «циферблат» вокруг каждого маркера;
    //   'anneal' / 'ring' — simulated annealing (глобальная оптимизация позиций).
    groupingMode: 'sectors',
    // Визуальный зазор от иконок КА и других препятствий (px).
    iconObstacleGap: 0,
    // Визуальный зазор от запретных сегментов трасс (px).
    forbiddenPadding: 5,
    // Шаг округления cardWidth для стабильного ключа layout (px).
    cardWidthBucket: 8,
    // SA: число Monte-Carlo sweeps.
    annealSweeps: 400,
    annealTempStart: 1.0,
    annealTempEnd: 0.001,
    annealStepPx: 12,
    // Радиус радиального seed от centroid кластера (px). 0 = авто из stem/tail/card.
    annealSeedRadius: 0,
    // Если маркер сдвинулся меньше этого (px) — старт с кэшированной позиции карточки.
    annealCacheThreshold: 8,
    // Веса energy-функции SA.
    annealWCardOverlap: 120,
    annealWObstacle: 90,
    annealWBounds: 60,
    annealWLeaderCross: 250,
    annealWCardTrack: 220,
    annealWLeaderTrack: 200,
    annealWStemCard: 180,
    annealWDistance: 0.015,
    annealWOwnIcon: 320,
    annealWLeaderIcon: 520,
    annealWCardLeader: 400,
    // Зазор карточки от чужой leader-линии (px).
    leaderCardPadding: 4,
    // Фиксированный seed PRNG для воспроизводимости (тесты). null = Math.random.
    annealSeed: null,
    // Лимит сегментов трасс в SA (полный набор может быть тысячи — блокирует UI).
    annealMaxSegments: 120,
    // Порог стекинга co-located КА (px). Маркеры ближе этого расстояния
    // объединяются в одну «стопку» — одна карточка с несколькими строками.
    // Отдельно от clusterDistance (PCA-эллипс): стек — это «одна точка»,
    // кластер — «группа близких точек, но различимых на экране».
    stackDistance: 15,
    // Высота одной строки в стековой карточке (px). Между строками нет gap —
    // они разделены визуально только фоновым цветом / цветным маркером строки.
    stackLineHeight: 18,
    // Максимум видимых строк в свёрнутом стеке. Остальные скрыты за "...+N ещё".
    stackMaxVisible: 4,
};

/** Округление ширины карточки до стабильного «ведра» (меньше перезапусков SA). */
function bucketCardWidth(w, opts) {
    const bucket = opts.cardWidthBucket || 8;
    if (!bucket || bucket <= 0) { return Math.round(w); }
    return Math.round(w / bucket) * bucket;
}

/**
 * Геометрия одного callout для заданного сектора.
 * Чистая функция без побочных эффектов — удобно для тестов.
 */
function computeGeometry(marker, sector, opts) {
    // Per-marker ширина побеждает, если задана и валидна, иначе — глобальный default.
    const cardW = (marker && typeof marker.cardWidth === 'number' && isFinite(marker.cardWidth))
        ? marker.cardWidth
        : opts.cardWidth;
    const a = sector.slopeDeg * Math.PI / 180;
    const stemDx = sector.dxSign * opts.stemLength * Math.cos(a);
    const stemDy = sector.dySign * opts.stemLength * Math.sin(a);
    const bendX = marker.x + stemDx;
    const bendY = marker.y + stemDy;
    const tailEndX = bendX + sector.tailSign * opts.tailLength;

    let cardX;
    if (sector.tailSign > 0) {
        cardX = tailEndX;
    } else {
        cardX = tailEndX - cardW;
    }
    const cardY = bendY - opts.cardHeight / 2;

    return {
        bend: { x: bendX, y: bendY },
        card: { x: cardX, y: cardY, w: cardW, h: opts.cardHeight },
    };
}

/** Геометрия callout укладывается в bounds канваса с заданным паддингом. */
function fitsBounds(geom, bounds, opts) {
    const pad = opts.boundsPadding;
    const c = geom.card;
    return c.x >= pad &&
        c.y >= pad &&
        (c.x + c.w) <= (bounds.width - pad) &&
        (c.y + c.h) <= (bounds.height - pad);
}

/** Пересечение двух bbox (с учётом minCardGap). */
function bboxOverlap(a, b, gap) {
    return !(a.x + a.w + gap <= b.x ||
             b.x + b.w + gap <= a.x ||
             a.y + a.h + gap <= b.y ||
             b.y + b.h + gap <= a.y);
}

/** Карточка пересекается с уже размещёнными или с препятствиями. */
function collides(geom, placedCards, obstacles, opts) {
    const c = geom.card;
    for (let i = 0; i < placedCards.length; i++) {
        if (bboxOverlap(c, placedCards[i], opts.minCardGap)) {
            return true;
        }
    }
    for (let j = 0; j < obstacles.length; j++) {
        if (bboxOverlap(c, obstacles[j], 0)) {
            return true;
        }
    }
    return false;
}

// ─── Кластеризация и SA-размещение ────────────────────────────────────────

/**
 * Single-linkage кластеризация маркеров по евклидовой дистанции.
 * Используется для стекинга co-located КА (stackDistance).
 */
function clusterMarkers(markers, threshold) {
    const n = markers.length;
    const parent = new Array(n);
    for (let i = 0; i < n; i++) { parent[i] = i; }
    const find = (i) => {
        while (parent[i] !== i) {
            parent[i] = parent[parent[i]];
            i = parent[i];
        }
        return i;
    };
    const union = (i, j) => {
        const ri = find(i);
        const rj = find(j);
        if (ri !== rj) { parent[ri] = rj; }
    };
    const t2 = threshold * threshold;
    for (let i = 0; i < n; i++) {
        for (let j = i + 1; j < n; j++) {
            const dx = markers[i].x - markers[j].x;
            const dy = markers[i].y - markers[j].y;
            if (dx * dx + dy * dy <= t2) { union(i, j); }
        }
    }
    const groups = new Map();
    for (let i = 0; i < n; i++) {
        const r = find(i);
        if (!groups.has(r)) { groups.set(r, []); }
        groups.get(r).push(i);
    }
    return Array.from(groups.values());
}

/** Размеры карточки виртуального маркера. */
function cardDims(marker, opts) {
    const w = (marker && typeof marker.cardWidth === 'number' && isFinite(marker.cardWidth))
        ? marker.cardWidth : opts.cardWidth;
    const h = (marker && typeof marker.cardHeight === 'number' && isFinite(marker.cardHeight))
        ? marker.cardHeight : opts.cardHeight;
    return { w, h };
}

/** Кламп позиции карточки в bounds canvas. */
function clampCardInBounds(cardX, cardY, cardW, cardH, bounds, pad) {
    const maxX = bounds.width - cardW - pad;
    const maxY = bounds.height - cardH - pad;
    let x = cardX;
    let y = cardY;
    if (x < pad) { x = pad; }
    else if (x > maxX) { x = maxX; }
    if (y < pad) { y = pad; }
    else if (y > maxY) { y = maxY; }
    return { x, y };
}

/**
 * Геометрия выноски из позиции карточки.
 * Крепление на ближайшую к маркеру грань: горизонтальная (лево/право) или
 * вертикальная (верх/низ), чтобы stem не проходил через bbox карточки.
 */
function placementFromCard(marker, cardX, cardY, cardW, cardH) {
    const cy = cardY + cardH / 2;
    const cx = cardX + cardW / 2;
    const dx = cx - marker.x;
    const dy = cy - marker.y;
    const card = { x: cardX, y: cardY, w: cardW, h: cardH };

    if (Math.abs(dy) > Math.abs(dx)) {
        if (dy > 0) {
            const bend = { x: cx, y: cardY };
            return { bend, card, tailEnd: { x: cx, y: cardY }, attach: 'vertical' };
        }
        const bend = { x: cx, y: cardY + cardH };
        return { bend, card, tailEnd: { x: cx, y: cardY + cardH }, attach: 'vertical' };
    }

    const bendX = (dx >= 0) ? cardX : (cardX + cardW);
    const bend = { x: bendX, y: cy };
    return { bend, card, tailEnd: { x: bendX, y: cy }, attach: 'horizontal' };
}

/** Акцентная полоска на стороне примыкания хвоста (не на противоположной). */
function accentOnCardRight(lt) {
    if (lt.attach !== 'horizontal') { return false; }
    const tail = lt.tailEnd || tailEndOf(lt);
    const right = lt.card.x + lt.card.w;
    return Math.abs(tail.x - right) < Math.abs(tail.x - lt.card.x);
}

function accentOnCardTop(lt) {
    if (lt.attach !== 'vertical') { return false; }
    const tail = lt.tailEnd || tailEndOf(lt);
    return Math.abs(tail.y - lt.card.y) < Math.abs(tail.y - (lt.card.y + lt.card.h));
}

function accentOnCardBottom(lt) {
    if (lt.attach !== 'vertical') { return false; }
    const tail = lt.tailEnd || tailEndOf(lt);
    const bottom = lt.card.y + lt.card.h;
    return Math.abs(tail.y - bottom) < Math.abs(tail.y - lt.card.y);
}

/** Stem (маркер → bend) проходит через интерьер bbox карточки, а не только касается грани. */
function stemPiercesCard(marker, placement) {
    const card = placement.card;
    const bend = placement.bend;
    const pad = 2;
    const mx = (marker.x + bend.x) / 2;
    const my = (marker.y + bend.y) / 2;
    if (mx <= card.x + pad || mx >= card.x + card.w - pad ||
        my <= card.y + pad || my >= card.y + card.h - pad) {
        return false;
    }
    const mInside = marker.x > card.x && marker.x < card.x + card.w &&
        marker.y > card.y && marker.y < card.y + card.h;
    return !mInside;
}

/** Грубое ведро числа сегментов трасс для ключа структуры (без координат). */
function segmentCountBucket(count) {
    if (!count || count <= 0) { return 0; }
    if (count <= 50) { return 1; }
    if (count <= 200) { return 2; }
    return 3;
}

/**
 * Равномерная подвыборка запретных сегментов для SA.
 * Полный массив в screen space меняется каждый кадр — в energy достаточно репрезентативной выборки.
 */
function subsampleForbiddenSegments(segments, maxCount) {
    if (!segments || segments.length === 0) { return []; }
    const max = maxCount || 120;
    if (segments.length <= max) { return segments; }
    const out = [];
    const step = segments.length / max;
    for (let i = 0; i < max; i++) {
        out.push(segments[Math.floor(i * step)]);
    }
    return out;
}

/** Ключ структуры layout: id КА, ведро ширины, ведро числа трасс (без координат). */
function computeAnnealStructureKey(virtualMarkers, segmentCount, opts) {
    const parts = ['v3'];
    for (let i = 0; i < virtualMarkers.length; i++) {
        const vm = virtualMarkers[i];
        const dims = cardDims(vm, opts);
        parts.push(String(vm.id), String(bucketCardWidth(dims.w, opts)),
            String(Math.round(dims.h)));
    }
    parts.push('seg', String(segmentCountBucket(segmentCount)));
    return parts.join('|');
}

/** Bbox-ы иконок маркеров для проверки зазора карточек. */
function markerIconObstacles(markers) {
    const out = [];
    for (let i = 0; i < markers.length; i++) {
        const m = markers[i];
        const r = m.iconRadius;
        if (typeof r !== 'number' || !isFinite(r) || r <= 0) { continue; }
        out.push({ x: m.x - r, y: m.y - r, w: 2 * r, h: 2 * r });
    }
    return out;
}

/** Сводный «штраф» нарушений layout (для sticky-режима: рост → пересчёт SA). */
function annealViolationScore(state, virtualMarkers, obstacles, segments, bounds, opts) {
    const placements = placementsFromState(state, virtualMarkers, opts);
    const gap = opts.minCardGap || 0;
    const pad = opts.forbiddenPadding != null ? opts.forbiddenPadding : 5;
    const iconGap = opts.iconObstacleGap || 0;
    const boundsPad = opts.boundsPadding;
    let score = 0;
    const n = placements.length;

    for (let i = 0; i < n; i++) {
        const m = virtualMarkers[i];
        const pl = placements[i];
        const card = pl.card;
        score += outOfBoundsPenalty(card, bounds, boundsPad);
        score += countCardObstacleHits(card, obstacles, m, iconGap) * 120;
        score += countCardCrossings(card, pad, segments) * 80;
        score += countLeaderOnlyCrossings(m, pl, segments) * 60;
        if (stemPiercesCard(m, pl)) { score += 200; }
        for (let j = i + 1; j < n; j++) {
            if (bboxOverlap(card, placements[j].card, gap)) { score += 150; }
        }
    }
    return score;
}

/** Есть ли нарушения после сдвига карточек вслед за маркерами (нужен пересчёт SA). */
function annealLayoutHasViolations(state, virtualMarkers, obstacles, segments, bounds, opts) {
    return annealViolationScore(state, virtualMarkers, obstacles, segments, bounds, opts) > 0;
}

function allVirtualMarkersCached(virtualMarkers, cacheMap) {
    if (virtualMarkers.length === 0) { return false; }
    for (let i = 0; i < virtualMarkers.length; i++) {
        const c = cacheMap.get(virtualMarkers[i].id);
        if (!c || typeof c.cardX !== 'number' || typeof c.cardY !== 'number') {
            return false;
        }
    }
    return true;
}

/** Позиции карточек из кэша + сдвиг маркера (без повторного SA). */
function stateFromCacheRelative(virtualMarkers, bounds, opts, cacheMap) {
    const pad = opts.boundsPadding;
    const state = [];
    for (let i = 0; i < virtualMarkers.length; i++) {
        const m = virtualMarkers[i];
        const dims = cardDims(m, opts);
        const c = cacheMap.get(m.id);
        const mx = (typeof c.mx === 'number') ? c.mx : m.x;
        const my = (typeof c.my === 'number') ? c.my : m.y;
        const pos = clampCardInBounds(
            c.cardX + (m.x - mx),
            c.cardY + (m.y - my),
            dims.w, dims.h, bounds, pad
        );
        state.push({ cardX: pos.x, cardY: pos.y });
    }
    return state;
}

/**
 * Позиции карточек из polar-кэша: фиксированный угол/дистанция от маркера.
 * При движении КА вдоль трассы карточка остаётся «сбоку», а не в фиксированном screen-offset.
 */
function stateFromCachePolar(virtualMarkers, bounds, opts, cacheMap) {
    const pad = opts.boundsPadding;
    const state = [];
    for (let i = 0; i < virtualMarkers.length; i++) {
        const m = virtualMarkers[i];
        const dims = cardDims(m, opts);
        const c = cacheMap.get(m.id);
        if (c && typeof c.angle === 'number' && typeof c.dist === 'number' &&
            isFinite(c.angle) && isFinite(c.dist) && c.dist > 0) {
            const ccx = m.x + Math.cos(c.angle) * c.dist;
            const ccy = m.y + Math.sin(c.angle) * c.dist;
            const pos = clampCardInBounds(
                ccx - dims.w / 2, ccy - dims.h / 2,
                dims.w, dims.h, bounds, pad
            );
            state.push({ cardX: pos.x, cardY: pos.y });
            continue;
        }
        if (c && typeof c.cardX === 'number' && typeof c.cardY === 'number') {
            const mx = (typeof c.mx === 'number') ? c.mx : m.x;
            const my = (typeof c.my === 'number') ? c.my : m.y;
            const pos = clampCardInBounds(
                c.cardX + (m.x - mx), c.cardY + (m.y - my),
                dims.w, dims.h, bounds, pad
            );
            state.push({ cardX: pos.x, cardY: pos.y });
            continue;
        }
        state.push({ cardX: pad, cardY: pad });
    }
    return state;
}

/** Polar-параметры карточки относительно маркера (для sticky при движении КА). */
function polarFromCardState(vm, cardX, cardY, cardW, cardH) {
    const ccx = cardX + cardW / 2;
    const ccy = cardY + cardH / 2;
    const dx = ccx - vm.x;
    const dy = ccy - vm.y;
    const dist = Math.hypot(dx, dy);
    const angle = Math.atan2(dy, dx);
    return { angle, dist };
}
function bboxOverlapArea(a, b, gap) {
    if (!bboxOverlap(a, b, gap)) { return 0; }
    const ix = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x);
    const iy = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y);
    return Math.max(0, ix) * Math.max(0, iy);
}

/** Штраф за выход карточки за bounds (квадрат вылета). */
function outOfBoundsPenalty(card, bounds, pad) {
    let p = 0;
    if (card.x < pad) { p += (pad - card.x) * (pad - card.x); }
    if (card.y < pad) { p += (pad - card.y) * (pad - card.y); }
    const right = card.x + card.w;
    const bottom = card.y + card.h;
    const maxRight = bounds.width - pad;
    const maxBottom = bounds.height - pad;
    if (right > maxRight) { p += (right - maxRight) * (right - maxRight); }
    if (bottom > maxBottom) { p += (bottom - maxBottom) * (bottom - maxBottom); }
    return p;
}

/**
 * Стекинг co-located маркеров → виртуальные маркеры (centroid + размеры карточки).
 */
function buildVirtualStacks(markers, opts) {
    const stackDist = opts.stackDistance;
    const stackLineH = opts.stackLineHeight;
    const stacks = clusterMarkers(markers, stackDist);
    const virtualMarkers = [];
    const stackMeta = [];
    for (let si = 0; si < stacks.length; si++) {
        const memberIndices = stacks[si];
        memberIndices.sort((a, b) => {
            const idA = markers[a].id;
            const idB = markers[b].id;
            return (idA < idB) ? -1 : (idA > idB) ? 1 : 0;
        });
        let cx = 0, cy = 0, maxW = 0, maxR = 0;
        const ids = [];
        for (let mi = 0; mi < memberIndices.length; mi++) {
            const m = markers[memberIndices[mi]];
            cx += m.x;
            cy += m.y;
            ids.push(m.id);
            const w = (typeof m.cardWidth === 'number' && isFinite(m.cardWidth))
                ? m.cardWidth : opts.cardWidth;
            if (w > maxW) { maxW = w; }
            const r = (typeof m.iconRadius === 'number') ? m.iconRadius : 0;
            if (r > maxR) { maxR = r; }
        }
        cx /= memberIndices.length;
        cy /= memberIndices.length;
        const n = memberIndices.length;
        const maxVis = opts.stackMaxVisible || 4;
        const visRows = (n > maxVis) ? (maxVis + 1) : n;
        const h = (n > 1) ? (visRows * stackLineH + 4) : opts.cardHeight;
        virtualMarkers.push({
            id: ids[0],
            x: cx,
            y: cy,
            cardWidth: maxW,
            cardHeight: h,
            iconRadius: maxR,
            color: markers[memberIndices[0]].color,
            _memberIndices: memberIndices,
        });
        stackMeta.push({ ids: ids, memberIndices: memberIndices });
    }
    return { virtualMarkers, stackMeta };
}

/** Детерминированный PRNG (mulberry32) для воспроизводимых прогонов SA в тестах. */
function makeAnnealRng(seed) {
    let s = (seed >>> 0) || 1;
    return function() {
        s += 0x6D2B79F5;
        let t = s;
        t = Math.imul(t ^ (t >>> 15), t | 1);
        t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
}

/** Радиальный seed или позиция из кэша при микродвижении маркера. */
function annealInitialState(virtualMarkers, bounds, opts, cacheMap) {
    const n = virtualMarkers.length;
    if (n === 0) { return []; }
    let cx = 0, cy = 0;
    for (let i = 0; i < n; i++) {
        cx += virtualMarkers[i].x;
        cy += virtualMarkers[i].y;
    }
    cx /= n;
    cy /= n;
    let R = opts.annealSeedRadius;
    if (!R || R <= 0) {
        R = opts.stemLength + opts.tailLength + opts.cardWidth * 0.45;
    }
    const cacheTh2 = (opts.annealCacheThreshold || 8) * (opts.annealCacheThreshold || 8);
    const pad = opts.boundsPadding;
    const state = [];
    for (let i = 0; i < n; i++) {
        const m = virtualMarkers[i];
        const dims = cardDims(m, opts);
        const cached = cacheMap.get(m.id);
        if (cached && typeof cached.cardX === 'number' && typeof cached.cardY === 'number') {
            const mx = (typeof cached.mx === 'number') ? cached.mx : m.x;
            const my = (typeof cached.my === 'number') ? cached.my : m.y;
            const dx = m.x - mx;
            const dy = m.y - my;
            if (dx * dx + dy * dy <= cacheTh2) {
                const c = clampCardInBounds(
                    cached.cardX + dx, cached.cardY + dy,
                    dims.w, dims.h, bounds, pad
                );
                state.push({ cardX: c.x, cardY: c.y });
                continue;
            }
        }
        const angle = Math.atan2(m.y - cy, m.x - cx);
        const centerX = cx + R * Math.cos(angle);
        const centerY = cy + R * Math.sin(angle);
        const c = clampCardInBounds(
            centerX - dims.w / 2, centerY - dims.h / 2,
            dims.w, dims.h, bounds, pad
        );
        state.push({ cardX: c.x, cardY: c.y });
    }
    return state;
}

/** Собрать placement-объекты из state и виртуальных маркеров. */
function placementsFromState(state, virtualMarkers, opts) {
    const out = [];
    for (let i = 0; i < virtualMarkers.length; i++) {
        const m = virtualMarkers[i];
        const dims = cardDims(m, opts);
        const s = state[i];
        out.push(placementFromCard(m, s.cardX, s.cardY, dims.w, dims.h));
    }
    return out;
}

/**
 * Energy-функция SA (порт идеи d3-labeler): overlap, obstacles, bounds,
 * leader crossings, tracks, дистанция от маркера.
 */
function computeAnnealEnergy(state, virtualMarkers, obstacles, segments, bounds, opts) {
    const placements = placementsFromState(state, virtualMarkers, opts);
    const gap = opts.minCardGap || 0;
    const pad = opts.forbiddenPadding != null ? opts.forbiddenPadding : 5;
    const wOverlap = opts.annealWCardOverlap || 120;
    const wObstacle = opts.annealWObstacle || 90;
    const wBounds = opts.annealWBounds || 60;
    const wLeader = opts.annealWLeaderCross || 250;
    const wCardTrack = opts.annealWCardTrack || 70;
    const wLeaderTrack = opts.annealWLeaderTrack || 50;
    const wStemCard = opts.annealWStemCard || 180;
    const wDist = opts.annealWDistance || 0.015;
    const wOwnIcon = opts.annealWOwnIcon || 320;
    const wLeaderIcon = opts.annealWLeaderIcon || 520;
    const wCardLeader = opts.annealWCardLeader || 400;
    const iconGap = opts.iconObstacleGap || 0;
    const leaderPad = opts.leaderCardPadding != null ? opts.leaderCardPadding : 4;

    let energy = 0;
    const n = placements.length;

    for (let i = 0; i < n; i++) {
        const m = virtualMarkers[i];
        const pl = placements[i];
        const card = pl.card;
        energy += outOfBoundsPenalty(card, bounds, opts.boundsPadding) * wBounds;
        energy += countCardObstacleHits(card, obstacles, m, iconGap) * wObstacle;
        energy += countCardCrossings(card, pad, segments) * wCardTrack;
        energy += countLeaderOnlyCrossings(m, pl, segments) * wLeaderTrack;
        energy += countLeaderObstacleHits(m, pl, obstacles) * wObstacle * 0.5;
        energy += countLeaderForeignIconHits(m, pl, virtualMarkers, iconGap) * wLeaderIcon;
        if (stemPiercesCard(m, pl)) { energy += wStemCard; }
        if (cardOverlapsOwnIcon(card, m, iconGap)) { energy += wOwnIcon; }

        const iconR = (typeof m.iconRadius === 'number' && m.iconRadius > 0) ? m.iconRadius : 0;
        const preferred = opts.stemLength + opts.tailLength + opts.cardWidth * 0.35 + iconR + iconGap;
        const ccx = card.x + card.w / 2;
        const ccy = card.y + card.h / 2;
        const dx = ccx - m.x;
        const dy = ccy - m.y;
        const dist = Math.sqrt(dx * dx + dy * dy);
        const excess = dist - preferred;
        if (excess > 0) { energy += excess * excess * wDist; }

        for (let j = i + 1; j < n; j++) {
            energy += bboxOverlapArea(card, placements[j].card, gap) * wOverlap;
            const mj = virtualMarkers[j];
            const ltI = { marker: m, bend: pl.bend, card: pl.card };
            const ltJ = { marker: mj, bend: placements[j].bend, card: placements[j].card };
            if (leadersIntersect(ltI, ltJ)) { energy += wLeader; }
            energy += countLeaderObstacleHits(
                mj, placements[j], [expandCardBbox(card, leaderPad)]
            ) * wCardLeader;
            energy += countLeaderObstacleHits(
                m, pl, [expandCardBbox(placements[j].card, leaderPad)]
            ) * wCardLeader;
        }
    }
    return energy;
}

/** Simulated annealing: оптимизация позиций карточек. */
function runSimulatedAnnealing(state, virtualMarkers, obstacles, segments, bounds, opts) {
    const sweeps = opts.annealSweeps || 400;
    if (virtualMarkers.length <= 1) { return state; }
    const tempStart = opts.annealTempStart || 1.0;
    const tempEnd = opts.annealTempEnd || 0.001;
    const step = opts.annealStepPx || 12;
    const pad = opts.boundsPadding;
    const rng = (opts.annealSeed != null && opts.annealSeed !== false)
        ? makeAnnealRng(Number(opts.annealSeed))
        : Math.random;

    let cur = state.map((s) => ({ cardX: s.cardX, cardY: s.cardY }));
    let curE = computeAnnealEnergy(cur, virtualMarkers, obstacles, segments, bounds, opts);

    for (let sweep = 0; sweep < sweeps; sweep++) {
        const t = tempStart * Math.pow(tempEnd / tempStart, sweep / sweeps);
        const i = Math.floor(rng() * virtualMarkers.length);
        const m = virtualMarkers[i];
        const dims = cardDims(m, opts);
        const dx = (rng() - 0.5) * 2 * step;
        const dy = (rng() - 0.5) * 2 * step;
        const c = clampCardInBounds(
            cur[i].cardX + dx, cur[i].cardY + dy,
            dims.w, dims.h, bounds, pad
        );
        const trial = cur.map((s, idx) => {
            if (idx === i) { return { cardX: c.x, cardY: c.y }; }
            return { cardX: s.cardX, cardY: s.cardY };
        });
        const trialE = computeAnnealEnergy(trial, virtualMarkers, obstacles, segments, bounds, opts);
        const delta = trialE - curE;
        if (delta < 0 || (t > 1e-12 && rng() < Math.exp(-delta / t))) {
            cur = trial;
            curE = trialE;
        }
    }
    return cur;
}

/**
 * Отразить карточку на противоположную сторону маркера (через точку маркера).
 * Помогает убрать пересечение stem с горизонтальной трассой над/под КА.
 */
function flipCardAcrossMarker(marker, cardX, cardY, cardW, cardH, bounds, pad) {
    const ccy = cardY + cardH / 2;
    const newCcy = marker.y - (ccy - marker.y);
    const c = clampCardInBounds(cardX, newCcy - cardH / 2, cardW, cardH, bounds, pad);
    return { cardX: c.x, cardY: c.y };
}

/**
 * Считает число запретных отрезков, которые пересекают bbox карточки
 * (расширенный на forbiddenPadding с каждой стороны).
 *
 * Сегмент считается «пересекающим», если:
 *   — хотя бы один его конец лежит внутри расширенного bbox карточки, ИЛИ
 *   — он пересекает хотя бы одну из четырёх сторон расширенного bbox.
 *
 * Реализован quick-reject по bbox-сегмента vs bbox-карточки — большая часть
 * сегментов трассы отбрасывается без обращения к segmentsIntersect.
 */
function countCardCrossings(card, padding, segments) {
    if (!segments || segments.length === 0) { return 0; }
    const x1 = card.x - padding;
    const y1 = card.y - padding;
    const x2 = card.x + card.w + padding;
    const y2 = card.y + card.h + padding;
    let count = 0;
    for (let i = 0; i < segments.length; i++) {
        const seg = segments[i];
        const sxMin = (seg.x1 < seg.x2) ? seg.x1 : seg.x2;
        const sxMax = (seg.x1 < seg.x2) ? seg.x2 : seg.x1;
        const syMin = (seg.y1 < seg.y2) ? seg.y1 : seg.y2;
        const syMax = (seg.y1 < seg.y2) ? seg.y2 : seg.y1;
        if (sxMax < x1 || sxMin > x2 || syMax < y1 || syMin > y2) { continue; }
        const p1in = (seg.x1 >= x1 && seg.x1 <= x2 && seg.y1 >= y1 && seg.y1 <= y2);
        const p2in = (seg.x2 >= x1 && seg.x2 <= x2 && seg.y2 >= y1 && seg.y2 <= y2);
        if (p1in || p2in) { count++; continue; }
        const a = { x: seg.x1, y: seg.y1 };
        const b = { x: seg.x2, y: seg.y2 };
        if (segmentsIntersect(a, b, { x: x1, y: y1 }, { x: x2, y: y1 }) ||
            segmentsIntersect(a, b, { x: x2, y: y1 }, { x: x2, y: y2 }) ||
            segmentsIntersect(a, b, { x: x2, y: y2 }, { x: x1, y: y2 }) ||
            segmentsIntersect(a, b, { x: x1, y: y2 }, { x: x1, y: y1 })) {
            count++;
        }
    }
    return count;
}

/**
 * Best-effort сдвиг карточки от осевой запретной трассы (вертикаль/горизонталь).
 */
function nudgeCardOffAxisAlignedSegment(cardX, cardY, cardW, cardH, seg, pad, marker) {
    const segDx = Math.abs(seg.x1 - seg.x2);
    const segDy = Math.abs(seg.y1 - seg.y2);
    let cx = cardX;
    let cy = cardY;
    if (segDx < 1e-3 && segDy > 1e-3) {
        const v = seg.x1;
        if (marker.x <= v) {
            cx = v - pad - cardW;
        } else {
            cx = v + pad;
        }
    } else if (segDy < 1e-3 && segDx > 1e-3) {
        const h = seg.y1;
        if (marker.y <= h) {
            cy = h - pad - cardH;
        } else {
            cy = h + pad;
        }
    }
    return { cardX: cx, cardY: cy };
}

/**
 * Best-effort сдвиг карточек из state, если bbox пересекает запретные трассы.
 * SA не всегда находит глобально чистую позицию при широких карточках.
 */
function nudgeStateOffForbiddenSegments(state, virtualMarkers, segments, bounds, opts) {
    if (!segments || segments.length === 0) { return state; }
    const pad = opts.forbiddenPadding != null ? opts.forbiddenPadding : 5;
    const boundsPad = opts.boundsPadding;
    const step = opts.annealStepPx || 12;
    const dirs = [
        { dx: -step, dy: 0 },
        { dx: step, dy: 0 },
        { dx: 0, dy: -step },
        { dx: 0, dy: step },
    ];
    const out = state.map((s) => ({ cardX: s.cardX, cardY: s.cardY }));

    for (let k = 0; k < virtualMarkers.length; k++) {
        const vm = virtualMarkers[k];
        const dims = cardDims(vm, opts);
        let cardX = out[k].cardX;
        let cardY = out[k].cardY;

        for (let round = 0; round < 10; round++) {
            let card = { x: cardX, y: cardY, w: dims.w, h: dims.h };
            const hits = countCardCrossings(card, pad, segments);
            if (hits === 0) { break; }
            for (let si = 0; si < segments.length; si++) {
                const seg = segments[si];
                if (countCardCrossings(card, pad, [seg]) === 0) { continue; }
                const n = nudgeCardOffAxisAlignedSegment(
                    cardX, cardY, dims.w, dims.h, seg, pad, vm
                );
                const c = clampCardInBounds(
                    n.cardX, n.cardY, dims.w, dims.h, bounds, boundsPad
                );
                cardX = c.x;
                cardY = c.y;
                card = { x: cardX, y: cardY, w: dims.w, h: dims.h };
            }
        }

        let card = { x: cardX, y: cardY, w: dims.w, h: dims.h };
        let hits = countCardCrossings(card, pad, segments);
        const maxPasses = 48;
        for (let pass = 0; pass < maxPasses && hits > 0; pass++) {
            let bestX = cardX;
            let bestY = cardY;
            let bestHits = hits;
            for (let d = 0; d < dirs.length; d++) {
                const c = clampCardInBounds(
                    cardX + dirs[d].dx, cardY + dirs[d].dy,
                    dims.w, dims.h, bounds, boundsPad
                );
                const trial = { x: c.x, y: c.y, w: dims.w, h: dims.h };
                const h = countCardCrossings(trial, pad, segments);
                if (h < bestHits) {
                    bestHits = h;
                    bestX = c.x;
                    bestY = c.y;
                }
            }
            if (bestHits >= hits) { break; }
            cardX = bestX;
            cardY = bestY;
            hits = bestHits;
        }
        out[k] = { cardX: cardX, cardY: cardY };
    }
    return out;
}

/**
 * Возвращает 2 отрезка leader-линии placement'а: stem (marker → bend) и
 * tail (bend → tailEnd). tailEnd — «дальняя» от bend сторона карточки
 * (та, к которой примыкает хвост).
 *
 * Используется проверками пересечений leader vs запретные трассы и leader
 * vs чужие линии выносок.
 */
function leaderSegmentsOf(marker, placement) {
    const bend = placement.bend;
    const tail = placement.tailEnd || {
        x: (placement.card.x > bend.x) ? placement.card.x : (placement.card.x + placement.card.w),
        y: bend.y,
    };
    const segs = [
        { x1: marker.x, y1: marker.y, x2: bend.x, y2: bend.y },
    ];
    if (tail.x !== bend.x || tail.y !== bend.y) {
        segs.push({ x1: bend.x, y1: bend.y, x2: tail.x, y2: tail.y });
    }
    return segs;
}

/**
 * Подсчёт пересечений ТОЛЬКО leader-линии (stem + tail) с запретными
 * сегментами. bbox карточки здесь НЕ учитывается — card-пересечения
 * считаются отдельно через `countCardCrossings`, чтобы Phase A/B мог
 * применять лексикографический порядок «сначала card, потом leader».
 */
function countLeaderOnlyCrossings(marker, placement, segments) {
    if (!segments || segments.length === 0) { return 0; }
    const leaderSegs = leaderSegmentsOf(marker, placement);
    let n = 0;
    for (let i = 0; i < leaderSegs.length; i++) {
        const L = leaderSegs[i];
        const l1 = { x: L.x1, y: L.y1 };
        const l2 = { x: L.x2, y: L.y2 };
        for (let j = 0; j < segments.length; j++) {
            const S = segments[j];
            const lxMin = (L.x1 < L.x2) ? L.x1 : L.x2;
            const lxMax = (L.x1 < L.x2) ? L.x2 : L.x1;
            const lyMin = (L.y1 < L.y2) ? L.y1 : L.y2;
            const lyMax = (L.y1 < L.y2) ? L.y2 : L.y1;
            const sxMin = (S.x1 < S.x2) ? S.x1 : S.x2;
            const sxMax = (S.x1 < S.x2) ? S.x2 : S.x1;
            const syMin = (S.y1 < S.y2) ? S.y1 : S.y2;
            const syMax = (S.y1 < S.y2) ? S.y2 : S.y1;
            if (lxMax < sxMin || sxMax < lxMin || lyMax < syMin || syMax < lyMin) { continue; }
            if (segmentsIntersect(l1, l2, { x: S.x1, y: S.y1 }, { x: S.x2, y: S.y2 })) {
                n++;
            }
        }
    }
    return n;
}

/**
 * Подсчитывает число препятствий-AABB (`obstacles`), чьи bbox-ы пересекаются
 * с карточкой `card`. «Собственная» иконка маркера, чей bbox содержит точку
 * `(marker.x, marker.y)`, исключается: иначе карточка собственного КА
 * считалась бы перекрывающей свою же иконку и алгоритм бы её «отгонял».
 *
 * `obstacles` — массив `{x, y, w, h}` (иконки чужих КА, подписи городов,
 * метка observer). Зазор между карточкой и препятствием не задаём (gap=0):
 * соприкосновение допустимо, важно лишь не накладываться.
 */
function countCardObstacleHits(card, obstacles, marker, gap) {
    if (!obstacles || obstacles.length === 0) { return 0; }
    const g = (typeof gap === 'number' && gap > 0) ? gap : 0;
    let n = 0;
    for (let i = 0; i < obstacles.length; i++) {
        const ob = obstacles[i];
        if (marker &&
            marker.x >= ob.x && marker.x <= ob.x + ob.w &&
            marker.y >= ob.y && marker.y <= ob.y + ob.h) {
            continue;
        }
        if (bboxOverlap(card, ob, g)) { n++; }
    }
    return n;
}

/** Bbox собственной иконки маркера с зазором. */
function ownIconBox(marker, gap) {
    const r = (marker && typeof marker.iconRadius === 'number') ? marker.iconRadius : 0;
    if (!r || r <= 0) { return null; }
    const g = (typeof gap === 'number' && gap > 0) ? gap : 0;
    const side = 2 * (r + g);
    return {
        x: marker.x - r - g,
        y: marker.y - r - g,
        w: side,
        h: side,
    };
}

/** Карточка перекрывает bbox собственной иконки (не допускается в UI). */
function cardOverlapsOwnIcon(card, marker, gap) {
    const box = ownIconBox(marker, gap);
    if (!box) { return false; }
    return bboxOverlap(card, box, 0);
}

/** Расширенный bbox карточки для проверки пересечения с leader-линиями. */
function expandCardBbox(card, pad) {
    const p = (typeof pad === 'number' && pad > 0) ? pad : 0;
    return { x: card.x - p, y: card.y - p, w: card.w + 2 * p, h: card.h + 2 * p };
}

/**
 * Штраф коллизий одной карточки: перекрытие соседей, чужие leader-линии, своя иконка.
 * @returns {{ cardX: number, cardY: number, score: number }}
 */
function cardCollisionScoreFor(k, cardX, cardY, state, virtualMarkers, bounds, opts, segments, scoreOpts) {
    const vm = virtualMarkers[k];
    const dims = cardDims(vm, opts);
    const boundsPad = opts.boundsPadding;
    const c = clampCardInBounds(cardX, cardY, dims.w, dims.h, bounds, boundsPad);
    const pl = placementFromCard(vm, c.x, c.y, dims.w, dims.h);
    const card = pl.card;
    const gap = opts.minCardGap || 0;
    const leaderPad = opts.leaderCardPadding != null ? opts.leaderCardPadding : 4;
    const iconGap = opts.iconObstacleGap || 0;
    const trackPad = opts.forbiddenPadding != null ? opts.forbiddenPadding : 5;
    const ignoreTracks = scoreOpts && scoreOpts.ignoreTracks;
    let score = 0;

    if (cardOverlapsOwnIcon(card, vm, iconGap)) { score += 1000; }
    score += countLeaderForeignIconHits(vm, pl, virtualMarkers, iconGap) * 90;

    if (!ignoreTracks && segments && segments.length > 0) {
        score += countCardCrossings(card, trackPad, segments) * 240;
        score += countLeaderOnlyCrossings(vm, pl, segments) * 180;
    }

    const cardOb = expandCardBbox(card, leaderPad);
    for (let j = 0; j < virtualMarkers.length; j++) {
        if (j === k) { continue; }
        const dimsJ = cardDims(virtualMarkers[j], opts);
        const sJ = state[j];
        const plJ = placementFromCard(
            virtualMarkers[j], sJ.cardX, sJ.cardY, dimsJ.w, dimsJ.h
        );
        if (bboxOverlap(card, plJ.card, gap)) { score += 280; }
        score += countLeaderObstacleHits(virtualMarkers[j], plJ, [cardOb]) * 110;
        const foreignIcon = ownIconBox(virtualMarkers[j], iconGap);
        if (foreignIcon && bboxOverlap(card, foreignIcon, 0)) { score += 650; }
    }
    score += countMarkerLeaderCrossings(k, c.x, c.y, state, virtualMarkers, opts) * 320;
    return { cardX: c.x, cardY: c.y, score };
}

/** Число пересечений leader-линии маркера k с линиями остальных. */
function countMarkerLeaderCrossings(k, cardX, cardY, state, virtualMarkers, opts) {
    const vm = virtualMarkers[k];
    const dims = cardDims(vm, opts);
    const pl = placementFromCard(vm, cardX, cardY, dims.w, dims.h);
    const ltK = {
        marker: { x: vm.x, y: vm.y },
        bend: pl.bend,
        tailEnd: pl.tailEnd,
        attach: pl.attach,
        card: pl.card,
    };
    let n = 0;
    for (let j = 0; j < virtualMarkers.length; j++) {
        if (j === k) { continue; }
        const vmJ = virtualMarkers[j];
        const dimsJ = cardDims(vmJ, opts);
        const sJ = state[j];
        const plJ = placementFromCard(vmJ, sJ.cardX, sJ.cardY, dimsJ.w, dimsJ.h);
        const ltJ = {
            marker: { x: vmJ.x, y: vmJ.y },
            bend: plJ.bend,
            tailEnd: plJ.tailEnd,
            attach: plJ.attach,
            card: plJ.card,
        };
        if (leadersIntersect(ltK, ltJ)) { n++; }
    }
    return n;
}

function totalLeaderCrossings(state, virtualMarkers, opts) {
    let count = 0;
    for (let i = 0; i < virtualMarkers.length; i++) {
        for (let j = i + 1; j < virtualMarkers.length; j++) {
            const dimsI = cardDims(virtualMarkers[i], opts);
            const dimsJ = cardDims(virtualMarkers[j], opts);
            const plI = placementFromCard(
                virtualMarkers[i], state[i].cardX, state[i].cardY, dimsI.w, dimsI.h
            );
            const plJ = placementFromCard(
                virtualMarkers[j], state[j].cardX, state[j].cardY, dimsJ.w, dimsJ.h
            );
            const ltI = {
                marker: { x: virtualMarkers[i].x, y: virtualMarkers[i].y },
                bend: plI.bend,
                tailEnd: plI.tailEnd,
                attach: plI.attach,
                card: plI.card,
            };
            const ltJ = {
                marker: { x: virtualMarkers[j].x, y: virtualMarkers[j].y },
                bend: plJ.bend,
                tailEnd: plJ.tailEnd,
                attach: plJ.attach,
                card: plJ.card,
            };
            if (leadersIntersect(ltI, ltJ)) { count++; }
        }
    }
    return count;
}

/** Свести пересечения leader-линий после nudge-проходов (не ухудшая коллизии карточек). */
function nudgeStateResolveLeaderCrossings(state, virtualMarkers, bounds, opts, segments, scoreOpts) {
    const pad = opts.boundsPadding;
    let out = state.map((s) => ({ cardX: s.cardX, cardY: s.cardY }));
    const maxPasses = opts.crossingsMaxPasses || 4;
    for (let pass = 0; pass < maxPasses; pass++) {
        let improved = false;
        for (let i = 0; i < virtualMarkers.length; i++) {
            for (let j = i + 1; j < virtualMarkers.length; j++) {
                const dimsI = cardDims(virtualMarkers[i], opts);
                const dimsJ = cardDims(virtualMarkers[j], opts);
                const plI = placementFromCard(
                    virtualMarkers[i], out[i].cardX, out[i].cardY, dimsI.w, dimsI.h
                );
                const plJ = placementFromCard(
                    virtualMarkers[j], out[j].cardX, out[j].cardY, dimsJ.w, dimsJ.h
                );
                const ltI = {
                    marker: { x: virtualMarkers[i].x, y: virtualMarkers[i].y },
                    bend: plI.bend,
                    tailEnd: plI.tailEnd,
                    attach: plI.attach,
                    card: plI.card,
                };
                const ltJ = {
                    marker: { x: virtualMarkers[j].x, y: virtualMarkers[j].y },
                    bend: plJ.bend,
                    tailEnd: plJ.tailEnd,
                    attach: plJ.attach,
                    card: plJ.card,
                };
                if (!leadersIntersect(ltI, ltJ)) { continue; }

                const beforeCross = totalLeaderCrossings(out, virtualMarkers, opts);
                const beforeScore = totalCollisionScore(
                    out, virtualMarkers, bounds, opts, segments, scoreOpts
                );
                let best = null;
                let bestCross = beforeCross;

                for (let k = 0; k < 2; k++) {
                    const idx = (k === 0) ? i : j;
                    const vm = virtualMarkers[idx];
                    const dims = cardDims(vm, opts);
                    const s = out[idx];
                    const tryState = function(newPos) {
                        const trial = out.map((x, ti) => (
                            ti === idx ? newPos : x
                        ));
                        const cross = totalLeaderCrossings(trial, virtualMarkers, opts);
                        const score = totalCollisionScore(
                            trial, virtualMarkers, bounds, opts, segments, scoreOpts
                        );
                        if (cross < bestCross && score <= beforeScore) {
                            bestCross = cross;
                            best = trial;
                        }
                    };
                    tryState(flipCardAcrossMarker(
                        vm, s.cardX, s.cardY, dims.w, dims.h, bounds, pad
                    ));
                    const radial = pickBestRadialSlot(
                        idx, out, virtualMarkers, segments, bounds, opts, scoreOpts
                    );
                    tryState({ cardX: radial.cardX, cardY: radial.cardY });
                }

                if (best && bestCross < beforeCross) {
                    out = best;
                    improved = true;
                }
            }
        }
        if (!improved) { break; }
    }
    return out;
}

/** Лёгкий проход: только отвести карточки и leader от трасс (без полного repair). */
function nudgeTracksOnly(state, virtualMarkers, segments, bounds, opts) {
    if (!segments || segments.length === 0) { return state; }
    let out = nudgeStateOffForbiddenSegments(
        state, virtualMarkers, segments, bounds, opts
    );
    out = nudgeStateClearLeaderTracks(out, virtualMarkers, segments, bounds, opts);
    return out;
}

/**
 * Пост-проходы после SA или при попытке исправить sticky без пересчёта.
 * @param {Object} [passOpts]
 * @param {boolean} [passOpts.skipTracks] — не уводить карточки от трасс
 *   (сегменты в screen space движутся каждый кадр вместе с КА; nudge на
 *   sticky-кадрах даёт «прыжки» карточек).
 */
function applyAnnealPostPasses(state, virtualMarkers, segments, bounds, opts, passOpts) {
    const skipTracks = passOpts && passOpts.skipTracks;
    let out = state.map((s) => ({ cardX: s.cardX, cardY: s.cardY }));
    if (segments.length > 0 && !skipTracks) {
        out = nudgeStateOffForbiddenSegments(out, virtualMarkers, segments, bounds, opts);
        out = nudgeStateClearLeaderTracks(out, virtualMarkers, segments, bounds, opts);
        const pad = opts.boundsPadding;
        for (let k = 0; k < virtualMarkers.length; k++) {
            const vm = virtualMarkers[k];
            const dims = cardDims(vm, opts);
            const s = out[k];
            const pl = placementFromCard(vm, s.cardX, s.cardY, dims.w, dims.h);
            const hitsBefore = countLeaderOnlyCrossings(vm, pl, segments);
            if (hitsBefore === 0) { continue; }
            const flipped = flipCardAcrossMarker(
                vm, s.cardX, s.cardY, dims.w, dims.h, bounds, pad
            );
            const pl2 = placementFromCard(
                vm, flipped.cardX, flipped.cardY, dims.w, dims.h
            );
            const hitsAfter = countLeaderOnlyCrossings(vm, pl2, segments);
            if (hitsAfter < hitsBefore) {
                out[k] = flipped;
            }
        }
    }
    out = nudgeStateOffForeignIcons(out, virtualMarkers, bounds, opts);
    out = nudgeStateOffOwnIcons(out, virtualMarkers, bounds, opts);
    const stickyScoreOpts = skipTracks ? { ignoreTracks: true } : undefined;
    out = nudgeStateResolveCollisions(
        out, virtualMarkers, bounds, opts, segments, stickyScoreOpts
    );
    out = nudgeStateResolveLeaderCrossings(
        out, virtualMarkers, bounds, opts, segments, stickyScoreOpts
    );
    return out;
}

function totalCollisionScore(state, virtualMarkers, bounds, opts, segments, scoreOpts) {
    let total = 0;
    for (let k = 0; k < virtualMarkers.length; k++) {
        const s = state[k];
        total += cardCollisionScoreFor(
            k, s.cardX, s.cardY, state, virtualMarkers, bounds, opts, segments, scoreOpts
        ).score;
    }
    return total;
}

/** Sticky-layout: карточки, иконки и leader-линии (трассы — только при первичном SA). */
function stickyLayoutValid(state, virtualMarkers, bounds, opts, segments) {
    return totalCollisionScore(
        state, virtualMarkers, bounds, opts, segments, { ignoreTracks: true }
    ) === 0;
}

/**
 * Оттолкнуть карточку от собственной иконки, если SA/sticky наложили её на маркер.
 * Собственная иконка намеренно не в obstacles — геометрия stem должна держать зазор,
 * но при плотном кластере SA может «сесть» на иконку без этого пост-прохода.
 */
function nudgeCardOffOwnIcon(marker, cardX, cardY, cardW, cardH, bounds, pad, opts) {
    const gap = opts.iconObstacleGap || 0;
    if (!marker || !marker.iconRadius || marker.iconRadius <= 0) {
        return { cardX: cardX, cardY: cardY };
    }
    let cx = cardX;
    let cy = cardY;
    const asCard = function() {
        return { x: cx, y: cy, w: cardW, h: cardH };
    };
    if (!cardOverlapsOwnIcon(asCard(), marker, gap)) {
        return { cardX: cx, cardY: cy };
    }

    let mccx = cx + cardW / 2;
    let mccy = cy + cardH / 2;
    let dx = mccx - marker.x;
    let dy = mccy - marker.y;
    let len = Math.hypot(dx, dy);
    if (len < 1e-3) {
        const push = opts.stemLength + opts.tailLength + marker.iconRadius + gap;
        cx = marker.x + push - cardW / 2;
        cy = marker.y - cardH / 2;
        mccx = cx + cardW / 2;
        mccy = cy + cardH / 2;
        dx = mccx - marker.x;
        dy = mccy - marker.y;
        len = Math.hypot(dx, dy);
    }
    if (len < 1e-3) {
        dx = 1;
        dy = 0;
        len = 1;
    }
    dx /= len;
    dy /= len;
    const step = Math.max(4, (opts.annealStepPx || 12) * 0.5);
    for (let i = 0; i < 48; i++) {
        if (!cardOverlapsOwnIcon(asCard(), marker, gap)) { break; }
        cx += dx * step;
        cy += dy * step;
    }
    let c = clampCardInBounds(cx, cy, cardW, cardH, bounds, pad);
    if (cardOverlapsOwnIcon({ x: c.x, y: c.y, w: cardW, h: cardH }, marker, gap)) {
        const flipped = flipCardAcrossMarker(marker, cardX, cardY, cardW, cardH, bounds, pad);
        cx = flipped.cardX;
        cy = flipped.cardY;
        mccx = cx + cardW / 2;
        mccy = cy + cardH / 2;
        dx = mccx - marker.x;
        dy = mccy - marker.y;
        len = Math.hypot(dx, dy);
        if (len < 1e-3) {
            dx = -1;
            dy = 0;
            len = 1;
        }
        dx /= len;
        dy /= len;
        for (let j = 0; j < 48; j++) {
            if (!cardOverlapsOwnIcon(asCard(), marker, gap)) { break; }
            cx += dx * step;
            cy += dy * step;
        }
        c = clampCardInBounds(cx, cy, cardW, cardH, bounds, pad);
    }
    return { cardX: c.x, cardY: c.y };
}

function nudgeStateOffOwnIcons(state, virtualMarkers, bounds, opts) {
    const pad = opts.boundsPadding;
    const out = state.map((s) => ({ cardX: s.cardX, cardY: s.cardY }));
    for (let i = 0; i < virtualMarkers.length; i++) {
        const m = virtualMarkers[i];
        const dims = cardDims(m, opts);
        const s = out[i];
        const nudged = nudgeCardOffOwnIcon(
            m, s.cardX, s.cardY, dims.w, dims.h, bounds, pad, opts
        );
        out[i] = nudged;
    }
    return out;
}

/** Оттолкнуть карточку от bbox иконки другого КА (карточка не должна сидеть на чужом маркере). */
function nudgeCardOffForeignIcons(vm, cardX, cardY, cardW, cardH, virtualMarkers, bounds, pad, opts) {
    const iconGap = opts.iconObstacleGap || 0;
    const step = Math.max(4, Math.round((opts.annealStepPx || 12) / 2));
    let cx = cardX;
    let cy = cardY;
    const asCard = function() {
        return { x: cx, y: cy, w: cardW, h: cardH };
    };
    const findOverlap = function() {
        for (let j = 0; j < virtualMarkers.length; j++) {
            const m = virtualMarkers[j];
            if (String(m.id) === String(vm.id)) { continue; }
            const box = ownIconBox(m, iconGap);
            if (box && bboxOverlap(asCard(), box, 0)) { return m; }
        }
        return null;
    };
    const foreign = findOverlap();
    if (!foreign) {
        return { cardX: cx, cardY: cy };
    }
    let mccx = cx + cardW / 2;
    let mccy = cy + cardH / 2;
    let dx = mccx - foreign.x;
    let dy = mccy - foreign.y;
    let len = Math.hypot(dx, dy);
    if (len < 1e-3) {
        dx = 1;
        dy = 0;
        len = 1;
    }
    dx /= len;
    dy /= len;
    for (let i = 0; i < 64; i++) {
        if (!findOverlap()) { break; }
        cx += dx * step;
        cy += dy * step;
    }
    const c = clampCardInBounds(cx, cy, cardW, cardH, bounds, pad);
    return { cardX: c.x, cardY: c.y };
}

function nudgeStateOffForeignIcons(state, virtualMarkers, bounds, opts) {
    const pad = opts.boundsPadding;
    const out = state.map((s) => ({ cardX: s.cardX, cardY: s.cardY }));
    for (let i = 0; i < virtualMarkers.length; i++) {
        const m = virtualMarkers[i];
        const dims = cardDims(m, opts);
        const s = out[i];
        const nudged = nudgeCardOffForeignIcons(
            m, s.cardX, s.cardY, dims.w, dims.h, virtualMarkers, bounds, pad, opts
        );
        out[i] = nudged;
    }
    return out;
}

/** Bbox-ы иконок других КА (для проверки stem/tail). */
function foreignIconObstaclesForMarker(marker, virtualMarkers, gap) {
    const out = [];
    if (!virtualMarkers || virtualMarkers.length === 0) { return out; }
    for (let i = 0; i < virtualMarkers.length; i++) {
        const m = virtualMarkers[i];
        if (marker && String(m.id) === String(marker.id)) { continue; }
        const box = ownIconBox(m, gap);
        if (box) { out.push(box); }
    }
    return out;
}

/** Число пересечений leader-линии с иконками других спутников. */
function countLeaderForeignIconHits(marker, placement, virtualMarkers, gap) {
    const obstacles = foreignIconObstaclesForMarker(marker, virtualMarkers, gap);
    return countLeaderObstacleHits(marker, placement, obstacles);
}

/**
 * Отразить карточку по горизонтали через точку маркера (зеркало по X).
 */
function flipCardHorizontally(marker, cardX, cardY, cardW, cardH, bounds, pad) {
    const ccx = cardX + cardW / 2;
    const newCcx = marker.x - (ccx - marker.x);
    const c = clampCardInBounds(newCcx - cardW / 2, cardY, cardW, cardH, bounds, pad);
    return { cardX: c.x, cardY: c.y };
}

/**
 * Убрать пересечения leader-линий (stem/tail) с запретными трассами.
 */
function nudgeStateClearLeaderTracks(state, virtualMarkers, segments, bounds, opts) {
    if (!segments || segments.length === 0) { return state; }
    const pad = opts.boundsPadding;
    const trackPad = opts.forbiddenPadding != null ? opts.forbiddenPadding : 5;
    const step = opts.annealStepPx || 12;
    const dirs = [
        { dx: step, dy: 0 }, { dx: -step, dy: 0 },
        { dx: 0, dy: step }, { dx: 0, dy: -step },
        { dx: step, dy: step }, { dx: -step, dy: step },
        { dx: step, dy: -step }, { dx: -step, dy: -step },
    ];
    const out = state.map((s) => ({ cardX: s.cardX, cardY: s.cardY }));

    for (let k = 0; k < virtualMarkers.length; k++) {
        const vm = virtualMarkers[k];
        const dims = cardDims(vm, opts);
        let cardX = out[k].cardX;
        let cardY = out[k].cardY;

        const trackHits = function(cx, cy) {
            const c = clampCardInBounds(cx, cy, dims.w, dims.h, bounds, pad);
            const pl = placementFromCard(vm, c.x, c.y, dims.w, dims.h);
            return countLeaderOnlyCrossings(vm, pl, segments) +
                countCardCrossings(pl.card, trackPad, segments);
        };

        let hits = trackHits(cardX, cardY);
        if (hits === 0) { continue; }

        const flippedV = flipCardAcrossMarker(vm, cardX, cardY, dims.w, dims.h, bounds, pad);
        let bestH = trackHits(flippedV.cardX, flippedV.cardY);
        if (bestH < hits) {
            cardX = flippedV.cardX;
            cardY = flippedV.cardY;
            hits = bestH;
        }
        const flippedH = flipCardHorizontally(vm, cardX, cardY, dims.w, dims.h, bounds, pad);
        bestH = trackHits(flippedH.cardX, flippedH.cardY);
        if (bestH < hits) {
            cardX = flippedH.cardX;
            cardY = flippedH.cardY;
            hits = bestH;
        }

        for (let pass = 0; pass < 40 && hits > 0; pass++) {
            let bestX = cardX;
            let bestY = cardY;
            let bestHits = hits;
            for (let d = 0; d < dirs.length; d++) {
                const h = trackHits(cardX + dirs[d].dx, cardY + dirs[d].dy);
                if (h < bestHits) {
                    bestHits = h;
                    const c = clampCardInBounds(
                        cardX + dirs[d].dx, cardY + dirs[d].dy,
                        dims.w, dims.h, bounds, pad
                    );
                    bestX = c.x;
                    bestY = c.y;
                }
            }
            if (bestHits >= hits) { break; }
            cardX = bestX;
            cardY = bestY;
            hits = bestHits;
        }
        out[k] = { cardX: cardX, cardY: cardY };
    }
    return out;
}

/** Лучший радиальный слот вокруг маркера (12 направлений × 2 радиусы). */
function pickBestRadialSlot(k, state, virtualMarkers, segments, bounds, opts, scoreOpts) {
    const vm = virtualMarkers[k];
    const dims = cardDims(vm, opts);
    const pad = opts.boundsPadding;
    const baseR = opts.stemLength + opts.tailLength + dims.w * 0.4;
    const radii = [baseR, baseR * 1.5];
    let best = cardCollisionScoreFor(
        k, state[k].cardX, state[k].cardY, state, virtualMarkers, bounds, opts, segments, scoreOpts
    );
    for (let ri = 0; ri < radii.length; ri++) {
        const r = radii[ri];
        for (let ai = 0; ai < 12; ai++) {
            const angle = ai * Math.PI * 2 / 12;
            const ccx = vm.x + Math.cos(angle) * r;
            const ccy = vm.y + Math.sin(angle) * r;
            const c = clampCardInBounds(
                ccx - dims.w / 2, ccy - dims.h / 2,
                dims.w, dims.h, bounds, pad
            );
            const cand = cardCollisionScoreFor(
                k, c.x, c.y, state, virtualMarkers, bounds, opts, segments, scoreOpts
            );
            if (cand.score < best.score) { best = cand; }
        }
    }
    return best;
}

/**
 * Развести карточки: без перекрытий, без сидания на чужие leader-линии и иконки.
 * Несколько раундов — при сдвиге одной карточки соседи тоже могут «попасть» в коллизию.
 */
function nudgeStateResolveCollisions(state, virtualMarkers, bounds, opts, segments, scoreOpts) {
    const pad = opts.boundsPadding;
    const step = opts.annealStepPx || 12;
    const dirs = [
        { dx: step, dy: 0 },
        { dx: -step, dy: 0 },
        { dx: 0, dy: step },
        { dx: 0, dy: -step },
        { dx: step, dy: step },
        { dx: -step, dy: step },
        { dx: step, dy: -step },
        { dx: -step, dy: -step },
    ];
    let out = state.map((s) => ({ cardX: s.cardX, cardY: s.cardY }));

    for (let round = 0; round < 8; round++) {
        let changed = false;
        for (let k = 0; k < virtualMarkers.length; k++) {
            const vm = virtualMarkers[k];
            const dims = cardDims(vm, opts);
            let cardX = out[k].cardX;
            let cardY = out[k].cardY;
            let cur = cardCollisionScoreFor(
                k, cardX, cardY, out, virtualMarkers, bounds, opts, segments, scoreOpts
            );
            if (cur.score === 0) { continue; }

            const flippedV = flipCardAcrossMarker(vm, cardX, cardY, dims.w, dims.h, bounds, pad);
            let cand = cardCollisionScoreFor(
                k, flippedV.cardX, flippedV.cardY, out, virtualMarkers, bounds, opts, segments, scoreOpts
            );
            if (cand.score < cur.score) {
                cardX = cand.cardX;
                cardY = cand.cardY;
                cur = cand;
            }

            const flippedH = flipCardHorizontally(vm, cardX, cardY, dims.w, dims.h, bounds, pad);
            cand = cardCollisionScoreFor(
                k, flippedH.cardX, flippedH.cardY, out, virtualMarkers, bounds, opts, segments, scoreOpts
            );
            if (cand.score < cur.score) {
                cardX = cand.cardX;
                cardY = cand.cardY;
                cur = cand;
            }

            for (let pass = 0; pass < 48 && cur.score > 0; pass++) {
                let best = cur;
                for (let d = 0; d < dirs.length; d++) {
                    cand = cardCollisionScoreFor(
                        k, cardX + dirs[d].dx, cardY + dirs[d].dy,
                        out, virtualMarkers, bounds, opts, segments, scoreOpts
                    );
                    if (cand.score < best.score) { best = cand; }
                }
                if (best.score >= cur.score) { break; }
                cardX = best.cardX;
                cardY = best.cardY;
                cur = best;
            }

            if (out[k].cardX !== cardX || out[k].cardY !== cardY) {
                changed = true;
                out[k] = { cardX: cardX, cardY: cardY };
            }
        }
        if (!changed) { break; }
    }

    // Если локальный поиск не помог — пробуем радиальный seed для «проблемных» карточек.
    for (let k = 0; k < virtualMarkers.length; k++) {
        const cur = cardCollisionScoreFor(
            k, out[k].cardX, out[k].cardY, out, virtualMarkers, bounds, opts, segments, scoreOpts
        );
        if (cur.score === 0) { continue; }
        const radial = pickBestRadialSlot(
            k, out, virtualMarkers, segments, bounds, opts, scoreOpts
        );
        if (radial.score < cur.score) {
            out[k] = { cardX: radial.cardX, cardY: radial.cardY };
        }
    }

    return out;
}

/**
 * Подсчёт числа препятствий, чьи bbox пересекает хотя бы один из отрезков
 * leader-линии (stem + tail). Собственная иконка маркера (bbox содержит
 * точку маркера) исключается — без этого leader всегда начинается из своей
 * же иконки и мы бы считали +1 для каждого размещения.
 */
function countLeaderObstacleHits(marker, placement, obstacles) {
    if (!obstacles || obstacles.length === 0) { return 0; }
    const segs = leaderSegmentsOf(marker, placement);
    let n = 0;
    for (let i = 0; i < obstacles.length; i++) {
        const ob = obstacles[i];
        if (marker &&
            marker.x >= ob.x && marker.x <= ob.x + ob.w &&
            marker.y >= ob.y && marker.y <= ob.y + ob.h) {
            continue;
        }
        const ox1 = ob.x;
        const oy1 = ob.y;
        const ox2 = ob.x + ob.w;
        const oy2 = ob.y + ob.h;
        let hit = false;
        for (let si = 0; si < segs.length && !hit; si++) {
            const L = segs[si];
            const lxMin = (L.x1 < L.x2) ? L.x1 : L.x2;
            const lxMax = (L.x1 < L.x2) ? L.x2 : L.x1;
            const lyMin = (L.y1 < L.y2) ? L.y1 : L.y2;
            const lyMax = (L.y1 < L.y2) ? L.y2 : L.y1;
            if (lxMax < ox1 || lxMin > ox2 || lyMax < oy1 || lyMin > oy2) { continue; }
            const l1 = { x: L.x1, y: L.y1 };
            const l2 = { x: L.x2, y: L.y2 };
            if (segmentsIntersect(l1, l2, { x: ox1, y: oy1 }, { x: ox2, y: oy1 }) ||
                segmentsIntersect(l1, l2, { x: ox2, y: oy1 }, { x: ox2, y: oy2 }) ||
                segmentsIntersect(l1, l2, { x: ox2, y: oy2 }, { x: ox1, y: oy2 }) ||
                segmentsIntersect(l1, l2, { x: ox1, y: oy2 }, { x: ox1, y: oy1 })) {
                hit = true;
            }
            if (!hit) {
                if (L.x1 >= ox1 && L.x1 <= ox2 && L.y1 >= oy1 && L.y1 <= oy2) { hit = true; }
                if (!hit && L.x2 >= ox1 && L.x2 <= ox2 && L.y2 >= oy1 && L.y2 <= oy2) { hit = true; }
            }
        }
        if (hit) { n++; }
    }
    return n;
}

/**
 * Аллокатор размещения карточек по 8 секторам.
 *
 * Алгоритм:
 *   1. Для каждого маркера пробуем сначала закешированный сектор (если он
 *      проходит проверки), затем перебираем 8 секторов в порядке циклического
 *      сдвига от стартового — обеспечивает разведение при кластере.
 *   2. Сектор принимается, если карточка укладывается в bounds и не пересекает
 *      ни уже размещённые карточки, ни препятствия.
 *   3. Fallback: если ни один сектор не подошёл — берём первый из перебора.
 *
 * Кеш носит выбранный сектор от вызова к вызову (стабильность при движении КА).
 */
class CalloutLayout {
    constructor(options) {
        this.opts = Object.assign({}, DEFAULTS, options || {});
        this._cache = new Map();
        this._annealStructureKey = null;
        this._tracksWereActive = false;
        this._nextStartIdx = 0;
    }

    /**
     * Сбросить кеш секторов.
     */
    reset() {
        this._cache.clear();
        this._annealStructureKey = null;
        this._tracksWereActive = false;
        this._nextStartIdx = 0;
    }

    /**
     * Удалить из кеша id, отсутствующие в currentIds.
     * @param {Array<number>} currentIds
     */
    prune(currentIds) {
        const keep = new Set(currentIds);
        const ids = Array.from(this._cache.keys());
        let removed = false;
        for (let i = 0; i < ids.length; i++) {
            if (!keep.has(ids[i])) {
                this._cache.delete(ids[i]);
                removed = true;
            }
        }
        if (removed) { this._annealStructureKey = null; }
    }

    /**
     * Расположить выноски для списка маркеров.
     * @param {Array} markers
     * @param {Array} obstacles
     * @param {Object} bounds
     * @param {Array} [forbiddenSegments] — массив `{x1,y1,x2,y2}` запретных
     *   отрезков (например, наземные трассы selected/tracking). Карточки в
     *   anneal-режиме карточки оптимизируются SA; bbox+forbiddenPadding
     *   учитываются в energy (best-effort).
     * @returns {Array}
     */
    layout(markers, obstacles, bounds, forbiddenSegments) {
        const obstaclesArr = obstacles || [];
        const forbidden = forbiddenSegments || [];
        const mode = this.opts.groupingMode;
        if (mode === 'ring' || mode === 'anneal') {
            return this._layoutAnneal(markers, obstaclesArr, bounds, forbidden);
        }
        const result = this._layoutGreedy(markers, obstaclesArr, bounds);
        if (this.opts.resolveCrossings !== false) {
            this._resolveCrossings(result, obstaclesArr, bounds);
        }
        return result;
    }

    /**
     * Жадная фаза: первый сектор, который укладывается в bounds и не пересекает
     * уже размещённые карточки/препятствия.
     */
    _layoutGreedy(markers, obstaclesArr, bounds) {
        const result = [];
        const placedCards = [];

        for (let i = 0; i < markers.length; i++) {
            const m = markers[i];
            const cachedIdx = this._cache.get(m.id);
            const order = this._sectorOrder(cachedIdx, i);

            let chosenIdx = -1;
            let chosenGeom = null;
            for (let k = 0; k < order.length; k++) {
                const sIdx = order[k];
                const geom = computeGeometry(m, SECTORS[sIdx], this.opts);
                if (!fitsBounds(geom, bounds, this.opts)) { continue; }
                if (collides(geom, placedCards, obstaclesArr, this.opts)) { continue; }
                chosenIdx = sIdx;
                chosenGeom = geom;
                break;
            }

            if (chosenIdx === -1) {
                // Fallback: берём первый сектор из перебора (даже с коллизиями)
                chosenIdx = order[0];
                chosenGeom = computeGeometry(m, SECTORS[chosenIdx], this.opts);
            }

            this._cache.set(m.id, chosenIdx);
            placedCards.push(chosenGeom.card);
            // cardWidth прокидываем внутрь layout-объекта,
            // чтобы пост-проход (_trySwap) мог переcчитать геометрию с той же шириной.
            const markerOut = { x: m.x, y: m.y };
            if (typeof m.cardWidth === 'number' && isFinite(m.cardWidth)) {
                markerOut.cardWidth = m.cardWidth;
            }
            result.push({
                id: m.id,
                color: (m.color !== undefined ? m.color : null),
                marker: markerOut,
                bend: chosenGeom.bend,
                card: chosenGeom.card,
                sector: SECTORS[chosenIdx].name,
            });
        }

        return result;
    }

    /**
     * Пост-проход: ищет пары выносок с пересекающимися линиями
     * и пробует обменять их сектора местами. Свап принимается, только если
     *   1) обе новые карточки укладываются в bounds,
     *   2) не пересекают чужие карточки и препятствия,
     *   3) их линии больше не пересекаются между собой,
     *   4) не появляется пересечений с другими выносками.
     *
     * Гарантия монотонности: суммарное число пересекающихся пар не возрастает,
     * поэтому пост-проход сходится за crossingsMaxPasses итераций.
     */
    _resolveCrossings(result, obstaclesArr, bounds) {
        const maxPasses = this.opts.crossingsMaxPasses || 1;
        for (let pass = 0; pass < maxPasses; pass++) {
            let swapped = false;
            for (let i = 0; i < result.length && !swapped; i++) {
                for (let j = i + 1; j < result.length; j++) {
                    if (!leadersIntersect(result[i], result[j])) { continue; }
                    if (this._trySwap(result, i, j, obstaclesArr, bounds)) {
                        swapped = true;
                        break;
                    }
                }
            }
            if (!swapped) { break; }
        }
    }

    /**
     * Попытка обменять сектора между result[i] и result[j].
     * Возвращает true, если свап принят и применён к result + _cache.
     */
    _trySwap(result, i, j, obstaclesArr, bounds) {
        const sIdxI = this._indexOfSector(result[i].sector);
        const sIdxJ = this._indexOfSector(result[j].sector);
        if (sIdxI < 0 || sIdxJ < 0 || sIdxI === sIdxJ) { return false; }

        const newGeomI = computeGeometry(result[i].marker, SECTORS[sIdxJ], this.opts);
        const newGeomJ = computeGeometry(result[j].marker, SECTORS[sIdxI], this.opts);

        if (!fitsBounds(newGeomI, bounds, this.opts)) { return false; }
        if (!fitsBounds(newGeomJ, bounds, this.opts)) { return false; }

        // Карточки прочих выносок (без i, j) — препятствия для свапнутой пары.
        const otherCards = [];
        for (let k = 0; k < result.length; k++) {
            if (k !== i && k !== j) { otherCards.push(result[k].card); }
        }
        if (collides(newGeomI, otherCards, obstaclesArr, this.opts)) { return false; }
        if (collides(newGeomJ, otherCards.concat([newGeomI.card]), obstaclesArr, this.opts)) { return false; }

        const candI = {
            id: result[i].id,
            color: result[i].color,
            marker: result[i].marker,
            bend: newGeomI.bend,
            card: newGeomI.card,
            sector: SECTORS[sIdxJ].name,
        };
        const candJ = {
            id: result[j].id,
            color: result[j].color,
            marker: result[j].marker,
            bend: newGeomJ.bend,
            card: newGeomJ.card,
            sector: SECTORS[sIdxI].name,
        };

        // Линии пары после свапа не должны пересекаться.
        if (leadersIntersect(candI, candJ)) { return false; }

        // Свап не должен породить новые пересечения с другими выносками.
        for (let k = 0; k < result.length; k++) {
            if (k === i || k === j) { continue; }
            if (leadersIntersect(candI, result[k])) { return false; }
            if (leadersIntersect(candJ, result[k])) { return false; }
        }

        result[i] = candI;
        result[j] = candJ;
        this._cache.set(candI.id, sIdxJ);
        this._cache.set(candJ.id, sIdxI);
        return true;
    }

    /** Индекс сектора в SECTORS по имени. */
    _indexOfSector(name) {
        for (let i = 0; i < SECTORS.length; i++) {
            if (SECTORS[i].name === name) { return i; }
        }
        return -1;
    }

    /**
     * Порядок перебора секторов:
     *   — Если есть кешированный — он первый.
     *   — Дальше: циклический сдвиг от позиции iInGroup (чтобы маркеры в группе
     *     стартовали с разных секторов и расходились).
     */
    _sectorOrder(cachedIdx, iInGroup) {
        const n = SECTORS.length;
        const start = (typeof cachedIdx === 'number') ? cachedIdx : (iInGroup % n);
        const order = [];
        for (let k = 0; k < n; k++) {
            order.push((start + k) % n);
        }
        return order;
    }


    // ─── SA layout (simulated annealing) ────────────────────────

    /**
     * Размещение карточек через simulated annealing (порт идеи d3-labeler).
     * @param {Array} markers
     * @param {Array} obstaclesArr
     * @param {Object} bounds
     * @param {Array} [forbiddenSegments]
     * @returns {Array}
     * @private
     */
    _layoutAnneal(markers, obstaclesArr, bounds, forbiddenSegments) {
        const opts = this.opts;
        const result = new Array(markers.length);
        if (markers.length === 0) { return result; }
        const segments = forbiddenSegments || [];
        const stickyScoreOpts = { ignoreTracks: true };

        const { virtualMarkers, stackMeta } = buildVirtualStacks(markers, opts);
        const structureKey = computeAnnealStructureKey(
            virtualMarkers, segments.length, opts
        );
        const hasCache = allVirtualMarkersCached(virtualMarkers, this._cache);
        const tracksNewlyVisible = segments.length > 0 && !this._tracksWereActive;

        let finalState;
        let stickyLayout = false;

        if (hasCache && !tracksNewlyVisible) {
            finalState = stateFromCachePolar(
                virtualMarkers, bounds, opts, this._cache
            );
            if (totalCollisionScore(
                finalState, virtualMarkers, bounds, opts, segments, stickyScoreOpts
            ) === 0) {
                stickyLayout = true;
            } else {
                const repaired = applyAnnealPostPasses(
                    finalState, virtualMarkers, segments, bounds, opts,
                    { skipTracks: true }
                );
                if (totalCollisionScore(
                    repaired, virtualMarkers, bounds, opts, segments, stickyScoreOpts
                ) === 0) {
                    finalState = repaired;
                    stickyLayout = true;
                }
            }
        }

        if (!stickyLayout) {
            // Структура та же — не перезапускаем SA: иначе карточки прыгают и main thread
            // блокируется на десятки–сотни ms (водопады TX в Авто замирают).
            if (hasCache && this._annealStructureKey && structureKey === this._annealStructureKey) {
                finalState = stateFromCachePolar(
                    virtualMarkers, bounds, opts, this._cache
                );
            } else {
                const layoutSegments = subsampleForbiddenSegments(
                    segments, opts.annealMaxSegments
                );
                const initial = annealInitialState(
                    virtualMarkers, bounds, opts, this._cache
                );
                finalState = runSimulatedAnnealing(
                    initial, virtualMarkers, obstaclesArr, layoutSegments, bounds, opts
                );
                finalState = applyAnnealPostPasses(
                    finalState, virtualMarkers, layoutSegments, bounds, opts
                );
                this._annealStructureKey = structureKey;
            }
        }

        this._tracksWereActive = segments.length > 0;

        for (let k = 0; k < virtualMarkers.length; k++) {
            const vm = virtualMarkers[k];
            const meta = stackMeta[k];
            const s = finalState[k];
            const dims = cardDims(vm, opts);
            const polar = polarFromCardState(vm, s.cardX, s.cardY, dims.w, dims.h);
            this._cache.set(vm.id, {
                cardX: s.cardX,
                cardY: s.cardY,
                mx: vm.x,
                my: vm.y,
                angle: polar.angle,
                dist: polar.dist,
            });
            const placement = placementFromCard(vm, s.cardX, s.cardY, dims.w, dims.h);
            const markerOut = { x: vm.x, y: vm.y };
            if (typeof vm.cardWidth === 'number' && isFinite(vm.cardWidth)) {
                markerOut.cardWidth = vm.cardWidth;
            }
            const primaryIdx = meta.memberIndices[0];
            result[primaryIdx] = {
                id: vm.id,
                color: vm.color,
                marker: markerOut,
                bend: placement.bend,
                tailEnd: placement.tailEnd,
                attach: placement.attach,
                card: placement.card,
                sector: 'anneal',
                stacked: (meta.ids.length > 1) ? meta.ids : undefined,
            };
            for (let mi = 1; mi < meta.memberIndices.length; mi++) {
                result[meta.memberIndices[mi]] = null;
            }
        }
        return result;
    }


}

// ─────────────────────────────────────────────────────────────────────────
// CalloutRenderer — отрисовка результата CalloutLayout на canvas + DOM.
// Линия (стержень + хвост) рисуется на canvas; карточка с именем КА и
// дополнительной строкой (alias / второе имя) — DOM-элемент
// `.map-sat-callout`, позиционируется в процентах от контейнера.
// ─────────────────────────────────────────────────────────────────────────

const RENDERER_DEFAULTS = {
    // В logical px (умножается на dpr внутри)
    lineWidth: 1.5,
    fallbackColor: '#ffeb3b',
};

class CalloutRenderer {
    /**
     * @param {HTMLElement} container — контейнер пула DOM-карточек
     *                                  (обычно .map-callouts внутри .earth-view-container).
     * @param {Object} [options]
     * @param {Function} [options.onCardClick] — колбэк `(noradId:number) => void`,
     *   вызывается при клике по карточке. Регистрируется как делегированный
     *   слушатель `click` на контейнере (один listener на пул карточек).
     */
    constructor(container, options) {
        this.container = container;
        this.opts = Object.assign({}, RENDERER_DEFAULTS, options || {});
        this._cards = new Map(); // id → { el, nameEl, subEl }
        this._linesSvg = null;
        this._onCardClick = (this.opts && typeof this.opts.onCardClick === 'function')
            ? this.opts.onCardClick : null;
        this._cardClickHandler = null;
        if (this._onCardClick && this.container && typeof this.container.addEventListener === 'function') {
            const self = this;
            this._cardClickHandler = function(ev) {
                // Делегирование: ищем ближайший родитель с классом map-sat-callout.
                let el = ev.target;
                // Если кликнута строка стека — берём её data-sat-id
                if (el.dataset && el.dataset.satId) {
                    const satId = parseInt(el.dataset.satId, 10);
                    if (isFinite(satId)) {
                        ev.stopPropagation();
                        try { self._onCardClick(satId); } catch (e) { /* swallow */ }
                        return;
                    }
                }
                while (el && el !== self.container) {
                    if (el.classList && el.classList.contains('map-sat-callout')) {
                        const nidStr = el.dataset && el.dataset.noradId;
                        const nid = nidStr ? parseInt(nidStr, 10) : NaN;
                        if (isFinite(nid)) {
                            ev.stopPropagation();
                            try { self._onCardClick(nid); } catch (e) { /* swallow */ }
                        }
                        return;
                    }
                    el = el.parentNode;
                }
            };
            this.container.addEventListener('click', this._cardClickHandler);
        }
        if (this.container) {
            let svg = this.container.querySelector('.map-callout-lines');
            if (!svg) {
                svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
                svg.setAttribute('class', 'map-callout-lines');
                svg.setAttribute('aria-hidden', 'true');
                svg.setAttribute('viewBox', '0 0 100 100');
                svg.setAttribute('preserveAspectRatio', 'none');
                this.container.insertBefore(svg, this.container.firstChild);
            }
            this._linesSvg = svg;
        }
    }

    /** @returns {SVGSVGElement|null} */
    getLinesLayer() {
        return this._linesSvg;
    }

    /** Координата physical px → % от размера canvas. */
    _pctCoord(px, total) {
        if (!total || total <= 0) { return 0; }
        return (px / total) * 100;
    }

    /**
     * Линии выносок в SVG-слое #map-callouts (поверх DOM-маркеров, под карточками).
     * @param {Array} layouts
     * @param {Object} canvasSize — { width, height } physical px
     */
    drawLinesOverlay(layouts, canvasSize) {
        const svg = this._linesSvg;
        if (!svg || !layouts || layouts.length === 0) {
            if (svg) { svg.innerHTML = ''; }
            return;
        }
        const w = canvasSize.width;
        const h = canvasSize.height;
        if (!w || !h) { return; }
        const ratio = (typeof window !== 'undefined' && window.devicePixelRatio)
            ? window.devicePixelRatio : 1;
        const lw = (this.opts.lineWidth || 1.5) * ratio;
        const parts = [];
        for (let i = 0; i < layouts.length; i++) {
            const lt = layouts[i];
            if (!lt) { continue; }
            const color = lt.color || this.opts.fallbackColor;
            const mx = this._pctCoord(lt.marker.x, w);
            const my = this._pctCoord(lt.marker.y, h);
            const bx = this._pctCoord(lt.bend.x, w);
            const by = this._pctCoord(lt.bend.y, h);
            const tail = lt.tailEnd || tailEndOf(lt);
            const tx = this._pctCoord(tail.x, w);
            const ty = this._pctCoord(tail.y, h);
            let pts = `${mx},${my} ${bx},${by}`;
            if (tail.x !== lt.bend.x || tail.y !== lt.bend.y) {
                pts += ` ${tx},${ty}`;
            }
            parts.push(
                '<polyline points="' + pts +
                '" fill="none" stroke="' + color +
                '" stroke-width="' + lw +
                '" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke"/>'
            );
        }
        svg.innerHTML = parts.join('');
    }

    /**
     * Отрисовать линии всех callout-объектов на canvas.
     * Координаты предполагаются в физических пикселях canvas.
     *
     * @param {CanvasRenderingContext2D} ctx
     * @param {Array} layouts — результат CalloutLayout.layout
     * @param {number} dpr — devicePixelRatio для перевода logical→physical толщин
     */
    drawLines(ctx, layouts, dpr) {
        if (!layouts || layouts.length === 0) { return; }
        const ratio = dpr || 1;
        ctx.save();
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';
        ctx.lineWidth = this.opts.lineWidth * ratio;
        for (let i = 0; i < layouts.length; i++) {
            const lt = layouts[i];
            if (!lt) { continue; }
            const color = lt.color || this.opts.fallbackColor;
            ctx.strokeStyle = color;
            const tail = lt.tailEnd || tailEndOf(lt);
            ctx.beginPath();
            ctx.moveTo(lt.marker.x, lt.marker.y);
            ctx.lineTo(lt.bend.x, lt.bend.y);
            if (tail.x !== lt.bend.x || tail.y !== lt.bend.y) {
                ctx.lineTo(tail.x, tail.y);
            }
            ctx.stroke();
        }
        ctx.restore();
    }

    /**
     * Синхронизировать DOM-карточки с layouts.
     * Создаёт новые, обновляет существующие, удаляет лишние.
     *
     * @param {Array} layouts
     * @param {Object} canvasSize — { width, height } в физических пикселях
     * @param {Object} info — id → { name, norad } (тексты карточек)
     */
    update(layouts, canvasSize, info) {
        const seen = new Set();
        for (let i = 0; i < layouts.length; i++) {
            const lt = layouts[i];
            if (!lt) { continue; }
            seen.add(lt.id);
            let entry = this._cards.get(lt.id);
            if (!entry) {
                entry = this._createCard();
                this.container.appendChild(entry.el);
                this._cards.set(lt.id, entry);
            }
            this._updateCard(entry, lt, canvasSize, info);
        }
        // Удаляем устаревшие
        const ids = Array.from(this._cards.keys());
        for (let j = 0; j < ids.length; j++) {
            const id = ids[j];
            if (!seen.has(id)) {
                const entry = this._cards.get(id);
                if (entry && entry.el && entry.el.parentNode) {
                    entry.el.parentNode.removeChild(entry.el);
                }
                this._cards.delete(id);
            }
        }
    }

    /**
     * Скрыть все карточки и удалить DOM-узлы (например, при сбросе наблюдения).
     */
    clear() {
        const ids = Array.from(this._cards.keys());
        for (let i = 0; i < ids.length; i++) {
            const entry = this._cards.get(ids[i]);
            if (entry && entry.el && entry.el.parentNode) {
                entry.el.parentNode.removeChild(entry.el);
            }
        }
        this._cards.clear();
        if (this._linesSvg) { this._linesSvg.innerHTML = ''; }
    }

    _createCard() {
        const el = document.createElement('div');
        el.className = 'map-sat-callout';
        const nameEl = document.createElement('strong');
        nameEl.className = 'map-sat-callout__name';
        // Вторая строка карточки: alias / второе имя КА. При отсутствии
        // alias DOM-узел остаётся, но скрывается через display: none —
        // карточка визуально становится одностроечной.
        const subEl = document.createElement('span');
        subEl.className = 'map-sat-callout__sub';
        el.appendChild(nameEl);
        el.appendChild(subEl);
        return { el: el, nameEl: nameEl, subEl: subEl };
    }

    _updateCard(entry, lt, canvasSize, info) {
        const pctX = (lt.card.x / canvasSize.width) * 100;
        const pctY = (lt.card.y / canvasSize.height) * 100;
        const pctW = (lt.card.w / canvasSize.width) * 100;
        // NORAD ID для делегированного click-обработчика: при стеке — первый КА.
        if (lt && lt.id != null) {
            const idStr = String(lt.id);
            if (entry.el.dataset && entry.el.dataset.noradId !== idStr) {
                entry.el.dataset.noradId = idStr;
            }
        }
        entry.el.style.left = pctX + '%';
        entry.el.style.top = pctY + '%';
        entry.el.style.width = pctW + '%';
        const accent = lt.color || this.opts.fallbackColor;
        entry.el.style.setProperty('--callout-accent', accent);
        entry.el.classList.toggle('map-sat-callout--accent-right', accentOnCardRight(lt));
        entry.el.classList.toggle('map-sat-callout--accent-top', accentOnCardTop(lt));
        entry.el.classList.toggle('map-sat-callout--accent-bottom', accentOnCardBottom(lt));

        // Стековая карточка: несколько строк (имён КА).
        const stacked = lt.stacked;
        if (stacked && stacked.length > 1 && info) {
            entry.el.classList.add('map-sat-callout--stacked');
            entry.el.classList.remove('map-sat-callout--single-line');
            // Проверяем tracked — хотя бы один КА в стеке tracked
            let anyTracked = false;
            for (let si = 0; si < stacked.length; si++) {
                const inf = info[stacked[si]];
                if (inf && inf.tracked) { anyTracked = true; break; }
            }
            entry.el.classList.toggle('map-sat-callout--tracked', anyTracked);

            const maxVis = this.opts.stackMaxVisible || 4;
            const isExpanded = Boolean(entry.expanded);
            const totalRows = stacked.length;
            const showAll = isExpanded || totalRows <= maxVis;
            const visibleCount = showAll ? totalRows : maxVis;

            // Формируем DOM-содержимое: стабильный порядок по id (уже отсортирован).
            if (!entry.extraRows) { entry.extraRows = []; }

            // Первая строка
            const info0 = info[stacked[0]];
            entry.nameEl.textContent = (info0 && info0.name) || '';
            entry.nameEl.dataset.satId = String(stacked[0]);
            entry.subEl.style.display = 'none';

            // Все дополнительные строки (создаём DOM-элементы для полного стека)
            while (entry.extraRows.length < totalRows - 1) {
                const rowEl = document.createElement('span');
                rowEl.className = 'map-sat-callout__stack-row';
                entry.el.appendChild(rowEl);
                entry.extraRows.push(rowEl);
            }
            for (let ri = 0; ri < totalRows - 1; ri++) {
                const inf = info[stacked[ri + 1]];
                entry.extraRows[ri].textContent = (inf && inf.name) || '';
                entry.extraRows[ri].dataset.satId = String(stacked[ri + 1]);
                // Видимость: показать первые (visibleCount-1), остальные скрыть
                entry.extraRows[ri].style.display = (ri < visibleCount - 1) ? '' : 'none';
            }
            // Скрываем лишние строки (если стек уменьшился)
            for (let ri = totalRows - 1; ri < entry.extraRows.length; ri++) {
                if (entry.extraRows[ri] !== entry.moreEl) {
                    entry.extraRows[ri].style.display = 'none';
                }
            }

            // Строка "...+N ещё" (или скрыть если развёрнуто/не нужно)
            if (totalRows > maxVis) {
                if (!entry.moreEl) {
                    entry.moreEl = document.createElement('span');
                    entry.moreEl.className = 'map-sat-callout__stack-more';
                    entry.moreEl.addEventListener('click', function(e) {
                        e.stopPropagation();
                        entry.expanded = !entry.expanded;
                        entry.el.classList.toggle('map-sat-callout--expanded', entry.expanded);
                        // Показать/скрыть строки inline без ожидания layout-цикла.
                        const allRows = entry.el.querySelectorAll('.map-sat-callout__stack-row');
                        const max = entry.expanded ? allRows.length : (maxVis - 1);
                        for (let r = 0; r < allRows.length; r++) {
                            allRows[r].style.display = (r < max) ? '' : 'none';
                        }
                        if (entry.expanded) {
                            entry.moreEl.textContent = '\u25B2 свернуть';
                        } else {
                            const hid = allRows.length - (maxVis - 1);
                            entry.moreEl.textContent = '...+' + hid + ' ещё';
                        }
                    });
                    entry.el.appendChild(entry.moreEl);
                }
                if (showAll) {
                    entry.moreEl.textContent = '\u25B2 свернуть';
                    entry.moreEl.style.display = '';
                } else {
                    const hidden = totalRows - maxVis;
                    entry.moreEl.textContent = '...+' + hidden + ' ещё';
                    entry.moreEl.style.display = '';
                }
            } else if (entry.moreEl) {
                entry.moreEl.style.display = 'none';
            }

            // Клик по строке → select этого спутника
            entry.el.classList.toggle('map-sat-callout--expanded', isExpanded);
        } else {
            // Одиночная карточка
            entry.el.classList.remove('map-sat-callout--stacked');
            const singleInfo = (info && lt.id != null) ? info[lt.id] : null;
            entry.el.classList.toggle('map-sat-callout--tracked', Boolean(singleInfo && singleInfo.tracked));
            if (singleInfo) {
                entry.nameEl.textContent = singleInfo.name || '';
                const aliasText = singleInfo.alias ? String(singleInfo.alias) : '';
                entry.subEl.textContent = aliasText;
                entry.subEl.style.display = aliasText ? '' : 'none';
                entry.el.classList.toggle('map-sat-callout--single-line', !aliasText);
            }
            // Скрываем экстра-строки, если были
            if (entry.extraRows) {
                for (let ri = 0; ri < entry.extraRows.length; ri++) {
                    entry.extraRows[ri].style.display = 'none';
                }
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────
// Геометрические утилиты для пересечения отрезков и линий выносок.
// Используются и в тестах, и во внутреннем пост-проходе CalloutLayout.
// ─────────────────────────────────────────────────────────────────────────

/**
 * Строгое пересечение двух открытых отрезков (без касаний на концах).
 * Возвращает true, если отрезки пересекаются «крест-накрест».
 *
 * Тест по знакам ориентаций (cross-произведений). Совпадение конечных точек
 * и коллинеарные случаи считаем НЕ пересечением: для выносок это легитимные
 * стыки «стержень → хвост» и параллельные смежные хвосты.
 */
function segmentsIntersect(a1, a2, b1, b2) {
    function ori(p, q, r) {
        return (q.x - p.x) * (r.y - p.y) - (q.y - p.y) * (r.x - p.x);
    }
    const d1 = ori(b1, b2, a1);
    const d2 = ori(b1, b2, a2);
    const d3 = ori(a1, a2, b1);
    const d4 = ori(a1, a2, b2);
    return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) &&
           ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0));
}

/** Конец хвоста (точка примыкания к карточке) для layout-объекта. */
function tailEndOf(lt) {
    if (lt.tailEnd) { return lt.tailEnd; }
    if (lt.attach === 'vertical') {
        return { x: lt.bend.x, y: lt.bend.y };
    }
    const x = (lt.card.x > lt.bend.x) ? lt.card.x : (lt.card.x + lt.card.w);
    return { x: x, y: lt.bend.y };
}

/**
 * Линии двух выносок пересекаются: проверяем все 4 пары
 * (стержень × стержень, стержень × хвост, хвост × стержень, хвост × хвост).
 */
function leadersIntersect(a, b) {
    const aTail = tailEndOf(a);
    const bTail = tailEndOf(b);
    return segmentsIntersect(a.marker, a.bend, b.marker, b.bend) ||
           segmentsIntersect(a.marker, a.bend, b.bend, bTail) ||
           segmentsIntersect(a.bend, aTail, b.marker, b.bend) ||
           segmentsIntersect(a.bend, aTail, b.bend, bTail);
}

if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
    module.exports = { // eslint-disable-line no-undef
        CalloutLayout,
        CalloutRenderer,
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
    totalCollisionScore,
        annealLayoutHasViolations,
        annealViolationScore,
        computeAnnealStructureKey,
        segmentCountBucket,
        subsampleForbiddenSegments,
        computeAnnealEnergy,
        annealInitialState,
        runSimulatedAnnealing,
        buildVirtualStacks,
    };
}

if (typeof window !== 'undefined') {
    window.CalloutLayout = CalloutLayout;
    window.CalloutRenderer = CalloutRenderer;
    window.CalloutSectors = SECTORS;
}
