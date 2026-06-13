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
    forbiddenMaxSteps: 72,
    // Шаг угловой пробы при обходе запретных сегментов (рад).
    forbiddenStepRad: 5 * Math.PI / 180,
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
    const cardH = (typeof marker.cardHeight === 'number' && isFinite(marker.cardHeight))
        ? marker.cardHeight : opts.cardHeight;
    const uOut = a2 * Math.cos(theta);
    const vOut = b2 * Math.sin(theta);
    const ax = ellipse.cx + ellipse.cosPhi * uOut - ellipse.sinPhi * vOut;
    const ay = ellipse.cy + ellipse.sinPhi * uOut + ellipse.cosPhi * vOut;

    let tailSign = (ax > marker.x) ? +1 : -1;
    if (Math.abs(ax - marker.x) < 1e-6) {
        tailSign = (Math.cos(theta) >= 0) ? +1 : -1;
    }
    let cardX = (tailSign > 0) ? ax + opts.tailLength : ax - opts.tailLength - cardW;
    let cardY = ay - cardH / 2;

    const pad = opts.boundsPadding;
    const maxX = bounds.width - cardW - pad;
    const maxY = bounds.height - cardH - pad;
    if (cardX < pad) { cardX = pad; }
    else if (cardX > maxX) { cardX = maxX; }
    if (cardY < pad) { cardY = pad; }
    else if (cardY > maxY) { cardY = maxY; }

    return {
        bend: { x: ax, y: cardY + cardH / 2 },
        card: { x: cardX, y: cardY, w: cardW, h: cardH },
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
 * Возвращает 2 отрезка leader-линии placement'а: stem (marker → bend) и
 * tail (bend → tailEnd). tailEnd — «дальняя» от bend сторона карточки
 * (та, к которой примыкает хвост).
 *
 * Используется проверками пересечений leader vs запретные трассы и leader
 * vs чужие линии выносок.
 */
function leaderSegmentsOf(marker, placement) {
    const card = placement.card;
    const bend = placement.bend;
    const tailX = (card.x > bend.x) ? card.x : (card.x + card.w);
    return [
        { x1: marker.x, y1: marker.y, x2: bend.x, y2: bend.y },
        { x1: bend.x, y1: bend.y, x2: tailX, y2: bend.y },
    ];
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
 * Лексикографическая «стоимость» размещения относительно запретных трасс:
 * `[cardHits, leaderHits]`. Phase A/B сравнивают кандидатов пары —
 * сначала по cardHits, затем по leaderHits. Это гарантирует, что:
 *   1. Если есть угол, где карточка не перекрывает трассу вообще,
 *      алгоритм выберет именно его (не «сдвинет» карточку под трассу ради
 *      устранения leader-пересечения).
 *   2. Среди углов с равным числом card-пересечений выбирается тот, где
 *      leader-линия пересекает меньше трасс.
 */
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
function countCardObstacleHits(card, obstacles, marker) {
    if (!obstacles || obstacles.length === 0) { return 0; }
    let n = 0;
    for (let i = 0; i < obstacles.length; i++) {
        const ob = obstacles[i];
        if (marker &&
            marker.x >= ob.x && marker.x <= ob.x + ob.w &&
            marker.y >= ob.y && marker.y <= ob.y + ob.h) {
            continue;
        }
        if (bboxOverlap(card, ob, 0)) { n++; }
    }
    return n;
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
 * Лексикографическая стоимость кандидата размещения карточки в ring-режиме.
 * Возвращает `[cardIconHits, leaderIconHits, cardTrackHits, leaderTrackHits]`:
 *
 *   1. cardIconHits   — card-bbox перекрывает иконку/подпись. Наивысший приоритет.
 *   2. leaderIconHits — leader-линия (stem+tail) пересекает bbox иконки/подписи.
 *   3. cardTrackHits  — card-bbox перекрывает запретную трассу.
 *   4. leaderTrackHits — leader-линия пересекает запретную трассу.
 *
 * Порядок: иконки > трассы; card-bbox > leader.
 */
function leaderLexCost(marker, placement, padding, segments, obstacles) {
    return [
        countCardObstacleHits(placement.card, obstacles, marker),
        countLeaderObstacleHits(marker, placement, obstacles),
        countCardCrossings(placement.card, padding, segments),
        countLeaderOnlyCrossings(marker, placement, segments),
    ];
}

/**
 * Сравнение двух lex-стоимостей произвольной длины. Возвращает −1, 0, +1.
 * При разной длине лишние компоненты считаются равными нулю.
 */
function lexCompare(a, b) {
    const n = (a.length > b.length) ? a.length : b.length;
    for (let i = 0; i < n; i++) {
        const ai = (i < a.length) ? a[i] : 0;
        const bi = (i < b.length) ? b[i] : 0;
        if (ai !== bi) { return ai < bi ? -1 : +1; }
    }
    return 0;
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
        const stackDist = opts.stackDistance;
        const stackLineH = opts.stackLineHeight;

        const groups = clusterMarkers(markers, opts.clusterDistance);
        for (let gi = 0; gi < groups.length; gi++) {
            const indices = groups[gi];

            // ── Стекинг co-located маркеров ──────────────────────────────
            // Маркеры ближе stackDistance px объединяются в один слот-стопку.
            // Одна карточка с несколькими строками вместо N скачущих карточек.
            const stacks = clusterMarkers(markers, stackDist);
            // Оставляем только стеки, целиком принадлежащие текущему PCA-кластеру.
            const indicesSet = new Set(indices);
            const stacksInGroup = [];
            for (let si = 0; si < stacks.length; si++) {
                const st = stacks[si];
                if (st.length === 0) { continue; }
                if (!indicesSet.has(st[0])) { continue; }
                let allIn = true;
                for (let sj = 0; sj < st.length; sj++) {
                    if (!indicesSet.has(st[sj])) { allIn = false; break; }
                }
                if (allIn) { stacksInGroup.push(st); }
            }

            // Для каждого стека формируем «виртуальный маркер» (centroid, max width,
            // увеличенный cardHeight). Порядок id — по возрастанию NORAD (стабильный).
            const virtualMarkers = [];
            const stackMeta = []; // параллельный массив: info о стеке
            for (let si = 0; si < stacksInGroup.length; si++) {
                const memberIndices = stacksInGroup[si];
                // Стабильная сортировка по NORAD ID
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
                    _stackIds: ids,
                    _memberIndices: memberIndices,
                });
                stackMeta.push({ ids: ids, memberIndices: memberIndices, h: h });
            }

            const ellipse = pcaEllipse(markers, indices);
            let a2 = Math.max(ellipse.a, 1) + opts.ringGap;
            let b2 = Math.max(ellipse.b, 1) + opts.ringGap;

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

            // Углы виртуальных маркеров (по одному на стек).
            const slots = [];
            for (let k = 0; k < virtualMarkers.length; k++) {
                const vm = virtualMarkers[k];
                const dx = vm.x - ellipse.cx;
                const dy = vm.y - ellipse.cy;
                const u = ellipse.cosPhi * dx + ellipse.sinPhi * dy;
                const v = -ellipse.sinPhi * dx + ellipse.cosPhi * dy;
                let theta = Math.atan2(v, u);
                if (!isFinite(theta)) { theta = 0; }
                slots.push({ idx: k, theta });
            }

            if (slots.length > 1) {
                const cardWSlot = maxCardWidth(virtualMarkers, slots.map(s => s.idx), opts.cardWidth);
                const Reff = Math.max((a2 + b2) / 2, 1);
                const maxH = virtualMarkers.reduce((m, v) => Math.max(m, v.cardHeight), opts.cardHeight);
                const angleByH = (maxH + opts.minCardGap) / Reff;
                const angleByW = cardWSlot / (2 * Reff);
                const minStep = Math.min(
                    2 * Math.PI / slots.length,
                    Math.max(angleByH, angleByW)
                );
                slots.sort((p, q) => p.theta - q.theta);
                distributeAnglesAround(slots, minStep);
            }

            const placements = new Array(slots.length);
            for (let k = 0; k < slots.length; k++) {
                placements[k] = buildRingPlacement(
                    slots[k].theta, ellipse, a2, b2,
                    virtualMarkers[slots[k].idx], opts, bounds
                );
            }

            if (segments.length > 0 || obstaclesArr.length > 0) {
                this._avoidForbiddenSegments(
                    slots, placements, ellipse, a2, b2,
                    virtualMarkers, bounds, segments, obstaclesArr
                );
            }

            // Пост-проход: θ-swap для устранения пересечений leader-линий.
            // Phase A/B может нарушить исходный циклический порядок θ (из-за
            // обхода трасс/иконок) — две соседние карточки «перехлёстывают» stem'ы.
            // Свап θ между ними убирает X-образное пересечение.
            // Приоритеты: leaders > icons > tracks (track допустимо ухудшить ради leaders).
            if (this.opts.resolveCrossings !== false && slots.length > 1) {
                const swapPasses = 4;
                for (let pass = 0; pass < swapPasses; pass++) {
                    let swapped = false;
                    for (let a = 0; a < slots.length; a++) {
                        for (let b = a + 1; b < slots.length; b++) {
                            const ma = virtualMarkers[slots[a].idx];
                            const mb = virtualMarkers[slots[b].idx];
                            const pa = placements[a];
                            const pb = placements[b];
                            if (!leadersIntersect(
                                { marker: ma, bend: pa.bend, card: pa.card },
                                { marker: mb, bend: pb.bend, card: pb.card }
                            )) { continue; }
                            // Попробовать свап θ
                            const tA = slots[a].theta;
                            const tB = slots[b].theta;
                            const candA = buildRingPlacement(tB, ellipse, a2, b2, ma, opts, bounds);
                            const candB = buildRingPlacement(tA, ellipse, a2, b2, mb, opts, bounds);
                            // Проверка: свап не создаёт новые X-пересечения
                            const ltA = { marker: ma, bend: candA.bend, card: candA.card };
                            const ltB = { marker: mb, bend: candB.bend, card: candB.card };
                            if (leadersIntersect(ltA, ltB)) { continue; }
                            // Проверка: не создаёт пересечения с другими leader'ами
                            let newXCount = 0;
                            let oldXCount = 0;
                            for (let c = 0; c < slots.length; c++) {
                                if (c === a || c === b) { continue; }
                                const mc = virtualMarkers[slots[c].idx];
                                const pc = placements[c];
                                const ltC = { marker: mc, bend: pc.bend, card: pc.card };
                                const ltOldA = { marker: ma, bend: pa.bend, card: pa.card };
                                const ltOldB = { marker: mb, bend: pb.bend, card: pb.card };
                                if (leadersIntersect(ltOldA, ltC)) { oldXCount++; }
                                if (leadersIntersect(ltOldB, ltC)) { oldXCount++; }
                                if (leadersIntersect(ltA, ltC)) { newXCount++; }
                                if (leadersIntersect(ltB, ltC)) { newXCount++; }
                            }
                            // Суммарно пересечений должно стать не больше
                            // (пара a-b: 1→0, остальные: net gain ≤ 0)
                            if (newXCount > oldXCount) { continue; }
                            // Проверка: свап не ухудшает пересечение с иконками
                            const oldIcons = countCardObstacleHits(pa.card, obstaclesArr, ma)
                                           + countCardObstacleHits(pb.card, obstaclesArr, mb);
                            const newIcons = countCardObstacleHits(candA.card, obstaclesArr, ma)
                                           + countCardObstacleHits(candB.card, obstaclesArr, mb);
                            if (newIcons > oldIcons) { continue; }
                            // Track crossings: не создавать новые (из 0→>0 запрещено)
                            const swPad = opts.forbiddenPadding != null ? opts.forbiddenPadding : 5;
                            const oldTrk = countCardCrossings(pa.card, swPad, segments)
                                         + countCardCrossings(pb.card, swPad, segments);
                            const newTrk = countCardCrossings(candA.card, swPad, segments)
                                         + countCardCrossings(candB.card, swPad, segments);
                            if (oldTrk === 0 && newTrk > 0) { continue; }
                            // Свап принят (leader-crossings уменьшаются)
                            slots[a].theta = tB;
                            slots[b].theta = tA;
                            placements[a] = candA;
                            placements[b] = candB;
                            swapped = true;
                        }
                    }
                    if (!swapped) { break; }
                }
            }

            // Пост-проход 2: swap для card-to-card overlaps.
            // Если две карточки перекрываются bbox'ами и обмен θ устраняет
            // или уменьшает перекрытие (не ухудшая tracks/icons) — принять.
            if (slots.length > 1) {
                const gap = opts.minCardGap || 4;
                for (let pass = 0; pass < 4; pass++) {
                    let didSwap = false;
                    for (let a = 0; a < slots.length; a++) {
                        for (let b = a + 1; b < slots.length; b++) {
                            const pa = placements[a];
                            const pb = placements[b];
                            if (!bboxOverlap(pa.card, pb.card, gap)) { continue; }
                            const ma = virtualMarkers[slots[a].idx];
                            const mb = virtualMarkers[slots[b].idx];
                            const tA = slots[a].theta;
                            const tB = slots[b].theta;
                            const candA = buildRingPlacement(tB, ellipse, a2, b2, ma, opts, bounds);
                            const candB = buildRingPlacement(tA, ellipse, a2, b2, mb, opts, bounds);
                            if (bboxOverlap(candA.card, candB.card, gap)) { continue; }
                            // Не ухудшает tracks
                            const swPad2 = opts.forbiddenPadding != null ? opts.forbiddenPadding : 5;
                            const oT = countCardCrossings(pa.card, swPad2, segments)
                                     + countCardCrossings(pb.card, swPad2, segments);
                            const nT = countCardCrossings(candA.card, swPad2, segments)
                                     + countCardCrossings(candB.card, swPad2, segments);
                            if (nT > oT) { continue; }
                            // Не ухудшает icons
                            const oI = countCardObstacleHits(pa.card, obstaclesArr, ma)
                                     + countCardObstacleHits(pb.card, obstaclesArr, mb);
                            const nI = countCardObstacleHits(candA.card, obstaclesArr, ma)
                                     + countCardObstacleHits(candB.card, obstaclesArr, mb);
                            if (nI > oI) { continue; }
                            // Не создаёт новые leader-crossings
                            if (leadersIntersect(
                                { marker: ma, bend: candA.bend, card: candA.card },
                                { marker: mb, bend: candB.bend, card: candB.card }
                            )) { continue; }
                            slots[a].theta = tB;
                            slots[b].theta = tA;
                            placements[a] = candA;
                            placements[b] = candB;
                            didSwap = true;
                        }
                    }
                    if (!didSwap) { break; }
                }
            }

            // Записываем результат: для первого id стека — полный layout,
            // для остальных — null (renderer будет пропускать).
            for (let k = 0; k < slots.length; k++) {
                const si = slots[k].idx;
                const vm = virtualMarkers[si];
                const placement = placements[k];
                const meta = stackMeta[si];

                const markerOut = { x: vm.x, y: vm.y };
                if (typeof vm.cardWidth === 'number' && isFinite(vm.cardWidth)) {
                    markerOut.cardWidth = vm.cardWidth;
                }
                // Записываем layout по индексу первого маркера в стеке.
                const primaryIdx = meta.memberIndices[0];
                result[primaryIdx] = {
                    id: vm.id,
                    color: vm.color,
                    marker: markerOut,
                    bend: placement.bend,
                    card: placement.card,
                    sector: 'ring',
                    stacked: (meta.ids.length > 1) ? meta.ids : undefined,
                };
                // Поглощённые маркеры → null (не рисуются отдельно).
                for (let mi = 1; mi < meta.memberIndices.length; mi++) {
                    result[meta.memberIndices[mi]] = null;
                }
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
    _avoidForbiddenSegments(slots, placements, ellipse, a2, b2, markers, bounds, segments, obstacles) {
        const opts = this.opts;
        const padding = opts.forbiddenPadding;
        const stepRad = opts.forbiddenStepRad;
        const maxSteps = opts.forbiddenMaxSteps;
        const gap = opts.minCardGap;
        const obs = obstacles || [];

        // Phase A — поиск идеальных углов: для каждого слота независимо
        // (без оглядки на чужие карточки) находим θ с минимальной лекси-
        // стоимостью `[iconHits, cardHits, leaderHits]`. Приоритеты:
        //   1) iconHits  — карточка не должна перекрывать иконку/подпись
        //                  (самый раздражающий артефакт UI);
        //   2) cardHits  — bbox карточки не должен скрывать трассу;
        //   3) leaderHits — leader-линия не должна пересекать трассу.
        const targetCost = new Array(slots.length);
        for (let i = 0; i < slots.length; i++) {
            const m = markers[slots[i].idx];
            let bestTheta = slots[i].theta;
            let bestCost = leaderLexCost(m, placements[i], padding, segments, obs);
            const isClean = function(c) {
                for (let ci = 0; ci < c.length; ci++) { if (c[ci] !== 0) { return false; } }
                return true;
            };
            if (!isClean(bestCost)) {
                for (let k = 1; k <= maxSteps; k++) {
                    let foundClean = false;
                    for (let s = 0; s < 2; s++) {
                        const sign = (s === 0) ? +1 : -1;
                        const t = slots[i].theta + sign * k * stepRad;
                        const cand = buildRingPlacement(t, ellipse, a2, b2, m, opts, bounds);
                        const cost = leaderLexCost(m, cand, padding, segments, obs);
                        if (lexCompare(cost, bestCost) < 0) {
                            bestTheta = t;
                            bestCost = cost;
                            if (isClean(cost)) { foundClean = true; break; }
                        }
                    }
                    if (foundClean) { break; }
                }
            }
            slots[i].theta = bestTheta;
            placements[i] = buildRingPlacement(
                bestTheta, ellipse, a2, b2, m, opts, bounds
            );
            targetCost[i] = bestCost;
        }

        // Phase B — жадный развод коллизий. Для каждой пары накладывающихся
        // карточек ищем ближайший θ для одной из них:
        //   — без коллизий с другими карточками;
        //   — лекси-стоимость относительно трасс/иконок не превышает
        //     targetCost[i] поэлементно с tolerance на leaderHits.
        // С каждым проходом tolerance растёт, что гарантирует сходимость
        // даже при «тесном коридоре».
        const maxResolvePasses = 12;
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
                        // Lex-budget: cardIconHits и leaderIconHits НЕ ухудшаем
                        // (карточка/leader не должны налезать на иконку ради
                        // развода коллизий); cardTrackHits — тоже строго;
                        // leaderTrackHits — с tolerance по проходам.
                        const cost = leaderLexCost(m, cand, padding, segments, obs);
                        const t0 = targetCost[i];
                        const withinBudget =
                            cost[0] <= t0[0] &&
                            cost[1] <= t0[1] &&
                            cost[2] <= t0[2] &&
                            cost[3] <= t0[3] + tolerance;
                        if (withinBudget) {
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
        const accentLeft = lt.card.x > lt.bend.x;
        entry.el.classList.toggle('map-sat-callout--accent-right', !accentLeft);

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
    };
}

if (typeof window !== 'undefined') {
    window.CalloutLayout = CalloutLayout;
    window.CalloutRenderer = CalloutRenderer;
    window.CalloutSectors = SECTORS;
}
