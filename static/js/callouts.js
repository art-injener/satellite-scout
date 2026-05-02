// callouts.js — выноски (callout/leader line) для подписей КА на карте.
// Геометрия: диагональный стержень + горизонтальный хвост, один излом < 90°.
// Размещение: 8 секторов «циферблата» вокруг маркера + жадный аллокатор.
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
    //   'sectors' — исторический 8-секторный «циферблат» вокруг каждого маркера;
    //   'ring'    — карточки на расширенном PCA-эллипсе кластера КА (карта
    //               остаётся читаемой при плотных группах одновременных пролётов).
    groupingMode: 'sectors',
    // Радиальный отступ карточки от bounding-эллипса (px). Подобран как ≈ 0.5·cardWidth.
    ringGap: 70,
    // Порог single-linkage кластеризации (px). По умолчанию ≈ 4·iconRadius.
    clusterDistance: 72,
    // Визуальный зазор от запретных сегментов трасс (px) при пост-проходе обхода.
    forbiddenPadding: 5,
    // Максимум угловых проб при обходе запретных сегментов (шаг 5°, итого до 360°).
    forbiddenMaxSteps: 36,
    // Шаг угловой пробы при обходе запретных сегментов (рад).
    forbiddenStepRad: 5 * Math.PI / 180,
};

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

// ─── Помощники для ring-режима (PCA-эллипс кластера) ──────────────────────

/**
 * Single-linkage кластеризация маркеров по евклидовой дистанции.
 * Возвращает массив групп (каждая — массив индексов исходного `markers`).
 *
 * Используется в ring-режиме: близкие КА получают общий bounding-эллипс,
 * далёкие — свои отдельные.
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

/**
 * Bounding-эллипс кластера маркеров через PCA (метод главных компонент).
 * Возвращает: центр (cx, cy), угол главной оси φ (cos/sin),
 * полуоси a, b такие, что все маркеры (с их iconRadius) лежат внутри эллипса.
 *
 * Защита от вырождения:
 *   - n=1 (одиночный КА): a = b = iconRadius (или 1px).
 *   - коллинеарные точки: меньшая ось ≥ 1px (нулевая ширина превращается в линию).
 *   - совпадающие точки: ковариация равна нулю → φ=0, полуоси по iconRadius.
 *
 * Полуоси берутся как масштабированный до самой удалённой точки эллипс,
 * так что a, b гарантированно покрывают все маркеры (а не только проекции на оси).
 */
function pcaEllipse(markers, indices) {
    const n = indices.length;
    let cx = 0, cy = 0;
    for (let k = 0; k < n; k++) {
        cx += markers[indices[k]].x;
        cy += markers[indices[k]].y;
    }
    cx /= n;
    cy /= n;

    let sxx = 0, syy = 0, sxy = 0;
    for (let k = 0; k < n; k++) {
        const dx = markers[indices[k]].x - cx;
        const dy = markers[indices[k]].y - cy;
        sxx += dx * dx;
        syy += dy * dy;
        sxy += dx * dy;
    }
    sxx /= n; syy /= n; sxy /= n;

    let phi = 0;
    if (Math.abs(sxy) > 1e-9 || Math.abs(sxx - syy) > 1e-9) {
        phi = 0.5 * Math.atan2(2 * sxy, sxx - syy);
    }
    const cosPhi = Math.cos(phi);
    const sinPhi = Math.sin(phi);

    // Полуоси: max |проекция на главную ось| + радиус иконки маркера.
    let aBase = 0, bBase = 0;
    for (let k = 0; k < n; k++) {
        const m = markers[indices[k]];
        const dx = m.x - cx;
        const dy = m.y - cy;
        const u = cosPhi * dx + sinPhi * dy;
        const v = -sinPhi * dx + cosPhi * dy;
        const r = (typeof m.iconRadius === 'number' && isFinite(m.iconRadius))
            ? m.iconRadius : 0;
        const au = Math.abs(u) + r;
        const bv = Math.abs(v) + r;
        if (au > aBase) { aBase = au; }
        if (bv > bBase) { bBase = bv; }
    }
    aBase = Math.max(aBase, 1);
    bBase = Math.max(bBase, 1);

    // Масштабируем эллипс так, чтобы он накрыл и угловые точки, а не только
    // проекции на оси: max sqrt((u/a)² + (v/b)²) = 1.
    let kMax = 1;
    for (let k = 0; k < n; k++) {
        const m = markers[indices[k]];
        const dx = m.x - cx;
        const dy = m.y - cy;
        const u = cosPhi * dx + sinPhi * dy;
        const v = -sinPhi * dx + cosPhi * dy;
        const ku = u / aBase;
        const kv = v / bBase;
        const norm = Math.sqrt(ku * ku + kv * kv);
        if (norm > kMax) { kMax = norm; }
    }

    return {
        cx, cy, phi, cosPhi, sinPhi,
        a: aBase * kMax,
        b: bBase * kMax,
    };
}

/**
 * Раздвинуть углы соседних слотов так, чтобы между ними было ≥ minStep.
 * Слоты должны быть отсортированы по slot.theta перед вызовом.
 * Учитывается wrap-around на 2π (последний и первый — соседи через границу).
 * Гарантированный лимит итераций обеспечивает завершимость.
 */
function distributeAnglesAround(slots, minStep) {
    const n = slots.length;
    if (n < 2) { return; }
    const maxPasses = 16;
    for (let pass = 0; pass < maxPasses; pass++) {
        let moved = false;
        for (let i = 0; i < n; i++) {
            const j = (i + 1) % n;
            let dt = slots[j].theta - slots[i].theta;
            if (j === 0) { dt += 2 * Math.PI; }
            if (dt < minStep - 1e-9) {
                const need = minStep - dt;
                slots[i].theta -= need / 2;
                slots[j].theta += need / 2;
                moved = true;
            }
        }
        if (!moved) { break; }
    }
}

/**
 * Геометрия одной ring-карточки для заданного угла θ.
 * Чистая функция: используется и при первичном размещении, и при поиске
 * обходных углов в `_avoidForbiddenSegments`.
 *
 * Алгоритм:
 *   1. Якорная точка `(ax, ay)` — на расширенном эллипсе под углом θ.
 *   2. Хвост карточки тянется наружу по X относительно маркера
 *      (sign выбирается по стороне якоря; в вырожденном случае — по cos θ).
 *   3. cardX/cardY клампятся в bounds (карточка должна быть видна целиком),
 *      bend.x = ax (якорь), bend.y = центр карточки по вертикали.
 */
function buildRingPlacement(theta, ellipse, a2, b2, marker, opts, bounds) {
    const cardW = (typeof marker.cardWidth === 'number' && isFinite(marker.cardWidth))
        ? marker.cardWidth : opts.cardWidth;
    const uOut = a2 * Math.cos(theta);
    const vOut = b2 * Math.sin(theta);
    const ax = ellipse.cx + ellipse.cosPhi * uOut - ellipse.sinPhi * vOut;
    const ay = ellipse.cy + ellipse.sinPhi * uOut + ellipse.cosPhi * vOut;

    let tailSign = (ax > marker.x) ? +1 : -1;
    if (Math.abs(ax - marker.x) < 1e-6) {
        tailSign = (Math.cos(theta) >= 0) ? +1 : -1;
    }
    let cardX = (tailSign > 0) ? ax + opts.tailLength : ax - opts.tailLength - cardW;
    let cardY = ay - opts.cardHeight / 2;

    const pad = opts.boundsPadding;
    const maxX = bounds.width - cardW - pad;
    const maxY = bounds.height - opts.cardHeight - pad;
    if (cardX < pad) { cardX = pad; }
    else if (cardX > maxX) { cardX = maxX; }
    if (cardY < pad) { cardY = pad; }
    else if (cardY > maxY) { cardY = maxY; }

    return {
        bend: { x: ax, y: cardY + opts.cardHeight / 2 },
        card: { x: cardX, y: cardY, w: cardW, h: opts.cardHeight },
    };
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
 * Проверка: bbox-карточки `card` пересекается с любой другой карточкой
 * из `placements` (кроме индекса `ownIdx`) с учётом зазора `gap`.
 */
function cardCollidesWithOthers(card, placements, ownIdx, gap) {
    for (let j = 0; j < placements.length; j++) {
        if (j === ownIdx) { continue; }
        const o = placements[j].card;
        const sepX = (card.x + card.w + gap <= o.x) || (o.x + o.w + gap <= card.x);
        const sepY = (card.y + card.h + gap <= o.y) || (o.y + o.h + gap <= card.y);
        if (!sepX && !sepY) { return true; }
    }
    return false;
}

/** Нормализация угла в диапазон [-π, π). Используется перед сортировкой слотов. */
function wrapAngle(theta) {
    const TAU = 2 * Math.PI;
    let t = theta;
    while (t < -Math.PI) { t += TAU; }
    while (t >= Math.PI) { t -= TAU; }
    return t;
}

/** Максимум ширины карточки по группе индексов (per-marker cardWidth → fallback). */
function maxCardWidth(markers, indices, defaultW) {
    let max = defaultW;
    for (let k = 0; k < indices.length; k++) {
        const m = markers[indices[k]];
        if (typeof m.cardWidth === 'number' && isFinite(m.cardWidth) && m.cardWidth > max) {
            max = m.cardWidth;
        }
    }
    return max;
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
        this._nextStartIdx = 0;
    }

    /**
     * Сбросить кеш секторов.
     */
    reset() {
        this._cache.clear();
        this._nextStartIdx = 0;
    }

    /**
     * Удалить из кеша id, отсутствующие в currentIds.
     * @param {Array<number>} currentIds
     */
    prune(currentIds) {
        const keep = new Set(currentIds);
        const ids = Array.from(this._cache.keys());
        for (let i = 0; i < ids.length; i++) {
            if (!keep.has(ids[i])) {
                this._cache.delete(ids[i]);
            }
        }
    }

    /**
     * Расположить выноски для списка маркеров.
     * @param {Array} markers
     * @param {Array} obstacles
     * @param {Object} bounds
     * @param {Array} [forbiddenSegments] — массив `{x1,y1,x2,y2}` запретных
     *   отрезков (например, наземные трассы selected/tracking). Карточки в
     *   ring-режиме после первичного размещения сдвигаются по углу так, чтобы
     *   их bbox+forbiddenPadding не пересекал ни один из этих отрезков
     *   (best-effort: при невозможности обхода — выбирается угол
     *   с минимальным числом пересечений).
     * @returns {Array}
     */
    layout(markers, obstacles, bounds, forbiddenSegments) {
        const obstaclesArr = obstacles || [];
        const forbidden = forbiddenSegments || [];
        if (this.opts.groupingMode === 'ring') {
            return this._layoutRing(markers, obstaclesArr, bounds, forbidden);
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

    // ─── Ring layout (PCA-эллипс группы КА) ───────────────────
    //
    // Размещает карточки на расширенном bounding-эллипсе каждого кластера.
    // Кластеры выделяются single-linkage по расстоянию clusterDistance:
    // плотные группы получают общий эллипс, дальние КА — свои отдельные.
    // Карточки идут радиально по углу θ маркера в локальной системе главных
    // осей; пересекающиеся раздвигаются симметрично; вылет за canvas
    // компенсируется клампингом bbox.

    /**
     * @param {Array} markers
     * @param {Array} obstaclesArr
     * @param {Object} bounds
     * @param {Array} [forbiddenSegments] — отрезки трасс, которые карточки
     *   должны обходить (best-effort).
     * @returns {Array}
     * @private
     */
    _layoutRing(markers, obstaclesArr, bounds, forbiddenSegments) {
        const opts = this.opts;
        const result = new Array(markers.length);
        if (markers.length === 0) { return result; }
        const segments = forbiddenSegments || [];

        const groups = clusterMarkers(markers, opts.clusterDistance);
        for (let gi = 0; gi < groups.length; gi++) {
            const indices = groups[gi];
            const ellipse = pcaEllipse(markers, indices);
            // Базовые полуоси кольца: PCA-эллипс + ringGap.
            let a2 = Math.max(ellipse.a, 1) + opts.ringGap;
            let b2 = Math.max(ellipse.b, 1) + opts.ringGap;

            // Клампим полуоси под размер canvas: при zoom>1 кластер маркеров
            // может расходиться шире viewport, и расширенный эллипс уходит
            // за края — buildRingPlacement тогда лепит все карточки на
            // боундари и они «прилипают к краям». Ограничиваем эллипс
            // максимально возможной зоной с учётом cardWidth/cardHeight, чтобы
            // карточки оставались компактным кольцом вокруг центра кластера.
            const cardW = maxCardWidth(markers, indices, opts.cardWidth);
            const pad = opts.boundsPadding;
            const aMaxByBounds = Math.max(
                1,
                bounds.width / 2 - cardW / 2 - opts.tailLength - pad
            );
            const bMaxByBounds = Math.max(
                1,
                bounds.height / 2 - opts.cardHeight / 2 - pad
            );
            if (a2 > aMaxByBounds) { a2 = aMaxByBounds; }
            if (b2 > bMaxByBounds) { b2 = bMaxByBounds; }

            // Углы маркеров в локальной системе (u, v) главных осей эллипса.
            const slots = [];
            for (let k = 0; k < indices.length; k++) {
                const idx = indices[k];
                const m = markers[idx];
                const dx = m.x - ellipse.cx;
                const dy = m.y - ellipse.cy;
                const u = ellipse.cosPhi * dx + ellipse.sinPhi * dy;
                const v = -ellipse.sinPhi * dx + ellipse.cosPhi * dy;
                let theta = Math.atan2(v, u);
                if (!isFinite(theta)) { theta = 0; }
                slots.push({ idx, theta });
            }

            // Минимальный угловой шаг между карточками: оценка по высоте
            // карточки относительно эффективного радиуса эллипса.
            // Для одиночной карточки в кластере шаг не нужен.
            if (slots.length > 1) {
                const cardW = maxCardWidth(markers, indices, opts.cardWidth);
                const Reff = Math.max((a2 + b2) / 2, 1);
                const angleByH = (opts.cardHeight + opts.minCardGap) / Reff;
                const angleByW = cardW / (2 * Reff);
                const minStep = Math.min(
                    2 * Math.PI / slots.length,
                    Math.max(angleByH, angleByW)
                );
                slots.sort((p, q) => p.theta - q.theta);
                distributeAnglesAround(slots, minStep);
            }

            // Первичное размещение всех слотов кластера.
            const placements = new Array(slots.length);
            for (let k = 0; k < slots.length; k++) {
                placements[k] = buildRingPlacement(
                    slots[k].theta, ellipse, a2, b2,
                    markers[slots[k].idx], opts, bounds
                );
            }

            // Пост-проход: уводим карточки от запретных отрезков (трасс).
            if (segments.length > 0) {
                this._avoidForbiddenSegments(
                    slots, placements, ellipse, a2, b2,
                    markers, bounds, segments
                );
            }

            for (let k = 0; k < slots.length; k++) {
                const slot = slots[k];
                const m = markers[slot.idx];
                const placement = placements[k];

                const markerOut = { x: m.x, y: m.y };
                if (typeof m.cardWidth === 'number' && isFinite(m.cardWidth)) {
                    markerOut.cardWidth = m.cardWidth;
                }
                result[slot.idx] = {
                    id: m.id,
                    color: (m.color !== undefined ? m.color : null),
                    marker: markerOut,
                    bend: placement.bend,
                    card: placement.card,
                    sector: 'ring',
                };
            }
        }

        return result;
    }

    /**
     * Пост-проход ring-размещения: для каждой карточки, чей bbox
     * (с учётом forbiddenPadding) пересекает хотя бы один запретный отрезок,
     * пробуем угловые сдвиги θ ± k·step.
     *
     * Стратегия: жадно по убыванию числа исходных пересечений.
     *   1. Перебираем углы близко-первыми (k=1, 2, ...; ±).
     *   2. Кандидат принимается, если он:
     *      — не выходит за bounds после клампинга (как и обычное размещение,
     *        клампинг встроен в `buildRingPlacement`);
     *      — не накладывается на bbox других уже зафиксированных карточек
     *        (с зазором minCardGap);
     *      — даёт строго меньше пересечений с трассами, чем текущий best.
     *   3. Best-effort fallback: если 0-пересечений не достижимо —
     *      берём угол с минимальным числом пересечений; при равенстве
     *      побеждает ближайший к исходному углу (естественный порядок поиска).
     *
     * @private
     */
    _avoidForbiddenSegments(slots, placements, ellipse, a2, b2, markers, bounds, segments) {
        const opts = this.opts;
        const padding = opts.forbiddenPadding;
        const stepRad = opts.forbiddenStepRad;
        const maxSteps = opts.forbiddenMaxSteps;
        const gap = opts.minCardGap;

        // Phase A — поиск идеальных углов: для каждого слота независимо
        // (без оглядки на чужие карточки) находим θ с минимумом пересечений
        // запретных отрезков. Запоминаем достигнутый «таргет» — он станет
        // якорем для Phase B (карточка не должна сильно ухудшить свой счётчик
        // при разводе коллизий).
        const targetCnt = new Array(slots.length);
        for (let i = 0; i < slots.length; i++) {
            const m = markers[slots[i].idx];
            let bestTheta = slots[i].theta;
            let bestCount = countCardCrossings(placements[i].card, padding, segments);
            if (bestCount > 0) {
                for (let k = 1; k <= maxSteps; k++) {
                    let foundClean = false;
                    for (let s = 0; s < 2; s++) {
                        const sign = (s === 0) ? +1 : -1;
                        const t = slots[i].theta + sign * k * stepRad;
                        const cand = buildRingPlacement(t, ellipse, a2, b2, m, opts, bounds);
                        const cnt = countCardCrossings(cand.card, padding, segments);
                        if (cnt < bestCount) {
                            bestTheta = t;
                            bestCount = cnt;
                            if (cnt === 0) { foundClean = true; break; }
                        }
                    }
                    if (foundClean) { break; }
                }
            }
            slots[i].theta = bestTheta;
            placements[i] = buildRingPlacement(
                bestTheta, ellipse, a2, b2, m, opts, bounds
            );
            targetCnt[i] = bestCount;
        }

        // Phase B — жадный развод коллизий. Для каждой пары накладывающихся
        // карточек ищем ближайший θ для одной из них:
        //   — без коллизий с другими карточками;
        //   — счётчик пересечений с трассами не превышает targetCnt[i] + tolerance.
        // С каждым проходом tolerance растёт (0 → 1 → 2 → …), что гарантирует
        // сходимость даже при «тесном коридоре».
        const maxResolvePasses = 8;
        for (let pass = 0; pass < maxResolvePasses; pass++) {
            const tolerance = pass;
            let moved = false;
            for (let i = 0; i < placements.length; i++) {
                if (!cardCollidesWithOthers(placements[i].card, placements, i, gap)) {
                    continue;
                }
                const m = markers[slots[i].idx];
                let resolved = false;
                for (let k = 1; k <= maxSteps && !resolved; k++) {
                    for (let s = 0; s < 2; s++) {
                        const sign = (s === 0) ? +1 : -1;
                        const t = slots[i].theta + sign * k * stepRad;
                        const cand = buildRingPlacement(t, ellipse, a2, b2, m, opts, bounds);
                        if (cardCollidesWithOthers(cand.card, placements, i, gap)) {
                            continue;
                        }
                        const cnt = countCardCrossings(cand.card, padding, segments);
                        if (cnt <= targetCnt[i] + tolerance) {
                            slots[i].theta = t;
                            placements[i] = cand;
                            resolved = true;
                            moved = true;
                            break;
                        }
                    }
                }
            }
            if (!moved) { break; }
        }
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
        this._onCardClick = (this.opts && typeof this.opts.onCardClick === 'function')
            ? this.opts.onCardClick : null;
        this._cardClickHandler = null;
        if (this._onCardClick && this.container && typeof this.container.addEventListener === 'function') {
            const self = this;
            this._cardClickHandler = function(ev) {
                // Делегирование: ищем ближайший родитель с классом map-sat-callout.
                // Это даёт O(1) при добавлении/удалении карточек (один listener на пул).
                let el = ev.target;
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
            const color = lt.color || this.opts.fallbackColor;
            ctx.strokeStyle = color;
            // Хвост заходит в ближайшую к bend сторону карточки
            const tailEndX = (lt.card.x > lt.bend.x)
                ? lt.card.x
                : (lt.card.x + lt.card.w);
            ctx.beginPath();
            ctx.moveTo(lt.marker.x, lt.marker.y);
            ctx.lineTo(lt.bend.x, lt.bend.y);
            ctx.lineTo(tailEndX, lt.bend.y);
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
            seen.add(lt.id);
            let entry = this._cards.get(lt.id);
            if (!entry) {
                entry = this._createCard();
                this.container.appendChild(entry.el);
                this._cards.set(lt.id, entry);
            }
            this._updateCard(entry, lt, canvasSize, (info && info[lt.id]) || null);
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
        // Ширина в %: и lt.card.w, и canvasSize.width — в одних и тех же единицах
        // (physical px), поэтому процент корректен на любом dpr.
        const pctW = (lt.card.w / canvasSize.width) * 100;
        // NORAD ID для делегированного click-обработчика (выбор спутника).
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
        // Цветная полоска — на стороне, в которую заходит хвост:
        // card.x > bend.x → хвост слева, полоска слева (default).
        // card.x ≤ bend.x → хвост справа, полоска справа.
        const accentLeft = lt.card.x > lt.bend.x;
        entry.el.classList.toggle('map-sat-callout--accent-right', !accentLeft);
        entry.el.classList.toggle('map-sat-callout--tracked', !!(info && info.tracked));
        if (info) {
            entry.nameEl.textContent = info.name || '';
            const aliasText = (info && info.alias) ? String(info.alias) : '';
            entry.subEl.textContent = aliasText;
            // Нет alias — скрываем вторую строку, чтобы карточка была
            // визуально одностроечной (без пустой строки под именем).
            entry.subEl.style.display = aliasText ? '' : 'none';
            entry.el.classList.toggle('map-sat-callout--single-line', !aliasText);
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
    };
}

if (typeof window !== 'undefined') {
    window.CalloutLayout = CalloutLayout;
    window.CalloutRenderer = CalloutRenderer;
    window.CalloutSectors = SECTORS;
}
