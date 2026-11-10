// satellite-state.js — FE-001: SatelliteStateManager
// Единый источник данных для всех UI-компонентов.
// Observer pattern: компоненты подписываются на события и получают обновления.

'use strict';

/**
 * Состояние одного спутника.
 * Хранит последние данные позиции, трека и зоны видимости.
 */
class SatelliteState {
    /**
     * @param {number} noradId — NORAD ID спутника.
     * @param {string} name — название спутника.
     */
    constructor(noradId, name = '') {
        /** @type {number} */
        this.noradId = noradId;

        /** @type {string} */
        this.name = name;

        /**
         * Текущая позиция спутника.
         * @type {?{lat: number, lon: number, alt: number, az: number, el: number, range: number, ts: number}}
         */
        this.position = null;

        /**
         * Наземная трасса орбиты.
         * past/future — массивы сегментов (разрыв на антимеридиане).
         * @type {?{past: Array<Array<{lon: number, lat: number, ts: number}>>, future: Array<Array<{lon: number, lat: number, ts: number}>>}}
         */
        this.track = null;

        /**
         * Зона видимости спутника (footprint).
         * @type {?{points: Array<{lon: number, lat: number}>, radius_deg: number, center_lat: number, center_lon: number, altitude_km: number}}
         */
        this.visibilityZone = null;
    }
}

/**
 * Типы событий, поддерживаемые SatelliteStateManager.
 * @enum {string}
 */
const StateEventType = Object.freeze({
    /** Пакет позиций группы обработан — перерисовать карту со всеми актуальными координатами. */
    GROUP_POSITION: 'group_position',
    /** Обновление позиции выбранного спутника (для карты/SkyView). */
    POSITION: 'position',
    /** Обновление трека выбранного спутника. */
    TRACK: 'track',
    /** Смена выбранного спутника (selected) — клик в таблице или авто из группы. */
    SELECTED_CHANGE: 'selected_change',
    /** Смена спутника в режиме сопровождения (tracking) — только кнопка «Сопровождать». */
    TRACKING_CHANGE: 'tracking_change',
    /** Обновление группы скользящего окна. */
    SATELLITE_GROUP_UPDATE: 'satellite_group_update',
    /** @deprecated Убран вместе с детектором пакетов. Подписчики должны использовать satellite_group_update. */
    // TX_CYCLE: 'tx_cycle',
    /** Изменение набора спутников с видимыми трассами (UX-TABLE-TRACK-COL-001). */
    TRACK_VISIBILITY_CHANGE: 'track_visibility_change',
    /** Переключение режима «все трассы группы» (UX-TABLE-TRACK-GROUP-MODE-001). */
    SHOW_ALL_MODE_CHANGE: 'show_all_mode_change',
    /** @deprecated Использовать SELECTED_CHANGE / TRACKING_CHANGE. */
    SATELLITE_CHANGE: 'satellite_change',
});

/** HSL → #rrggbb (h: 0–360, s/l: 0–100). */
function hslToHex(h, s, l) {
    const sn = s / 100;
    const ln = l / 100;
    const c = (1 - Math.abs(2 * ln - 1)) * sn;
    const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
    const m = ln - c / 2;
    let r = 0;
    let g = 0;
    let b = 0;
    if (h < 60) {
        r = c; g = x;
    } else if (h < 120) {
        r = x; g = c;
    } else if (h < 180) {
        g = c; b = x;
    } else if (h < 240) {
        g = x; b = c;
    } else if (h < 300) {
        r = x; b = c;
    } else {
        r = c; b = x;
    }
    const toByte = (v) => {
        const n = Math.round((v + m) * 255);
        return Math.max(0, Math.min(255, n)).toString(16).padStart(2, '0');
    };
    return '#' + toByte(r) + toByte(g) + toByte(b);
}

/**
 * Два кольца по 12 оттенков, шаг hue 30°.
 * Второе кольцо темнее и входит в ход, только когда первое занято:
 * на тонкой трассе соседние hue сливаются, а разная светлота читается.
 * @param {readonly {l: number, s: number, hard?: boolean, hardL?: number, hardS?: number}[]} rings
 * @returns {readonly string[]}
 */
function buildTrackPalette(rings) {
    const colors = [];
    for (let r = 0; r < rings.length; r++) {
        const spec = rings[r];
        for (let i = 0; i < 12; i++) {
            const h = i * 30;
            const hard = spec.hard && h >= 30 && h <= 180;
            const l = hard ? spec.hardL : spec.l;
            const s = hard ? spec.hardS : spec.s;
            colors.push(hslToHex(h, s, l));
        }
    }
    return Object.freeze(colors);
}

/** Тёмная карта: яркое кольцо, затем более глубокое. */
const _PALETTE_DARK = buildTrackPalette([
    { l: 64, s: 68 },
    { l: 46, s: 78 },
]);

/**
 * Светлая тема: живые mid-тона; жёлто-зелёно-бирюзовый ряд темнее, чтобы не теряться на светлом холсте.
 * Второе кольцо ещё темнее.
 */
const _PALETTE_LIGHT = buildTrackPalette([
    { l: 50, s: 70, hard: true, hardL: 36, hardS: 74 },
    { l: 32, s: 72, hard: true, hardL: 24, hardS: 80 },
]);

/** @returns {{ h: number, s: number, l: number }} h в [0, 360), s/l в [0, 100]. */
function hexToHsl(hex) {
    const raw = String(hex || '').replace('#', '');
    const norm = raw.length === 3
        ? raw.split('').map((c) => c + c).join('')
        : raw;
    const n = parseInt(norm, 16);
    // RGB из hex; дальше только читаем.
    const r = ((n >> 16) & 255) / 255;
    const g = ((n >> 8) & 255) / 255;
    const b = (n & 255) / 255;
    const max = Math.max(r, g, b);
    const min = Math.min(r, g, b);
    let h = 0;
    let s = 0;
    const l = (max + min) / 2;
    if (max !== min) {
        const d = max - min;
        s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
        if (max === r) {
            h = ((g - b) / d + (g < b ? 6 : 0)) / 6;
        } else if (max === g) {
            h = ((b - r) / d + 2) / 6;
        } else {
            h = ((r - g) / d + 4) / 6;
        }
    }
    return { h: h * 360, s: s * 100, l: l * 100 };
}

/** Кратчайшая разница двух hue в градусах, [0, 180]. */
function hueDiffDeg(h1, h2) {
    let d = Math.abs(h1 - h2) % 360;
    if (d > 180) { d = 360 - d; }
    return d;
}

/**
 * Перцептальная дистанция двух hex-цветов для подбора трасс.
 * Hue доминирует — на карте пунктиром оттенки внутри одного семейства неразличимы.
 */
function trackColorDistance(hex1, hex2) {
    const a = hexToHsl(hex1);
    const b = hexToHsl(hex2);
    const dh = hueDiffDeg(a.h, b.h) / 180;
    const ds = Math.abs(a.s - b.s) / 100;
    const dl = Math.abs(a.l - b.l) / 100;
    return dh * 3.0 + ds * 0.5 + dl * 0.5;
}

/** Минимальная hue-дистанция (°) между любыми двумя цветами палитры. */
function paletteMinHueSeparation(palette) {
    let minHue = 360;
    for (let i = 0; i < palette.length; i++) {
        const hi = hexToHsl(palette[i]).h;
        for (let j = i + 1; j < palette.length; j++) {
            const hj = hexToHsl(palette[j]).h;
            const d = hueDiffDeg(hi, hj);
            if (d < minHue) { minHue = d; }
        }
    }
    return minHue;
}

/**
 * Минимальная дистанция кандидата до уже выданных цветов.
 * Совпадение с самим собой не считается: иначе при повторе у всех дистанция 0
 * и побеждает первый красный.
 * @param {string} candidate
 * @param {readonly string[]} used
 * @returns {number}
 */
function minDistanceToUsed(candidate, used) {
    let minDist = Infinity;
    for (let i = 0; i < used.length; i++) {
        if (used[i] === candidate) { continue; }
        const d = trackColorDistance(candidate, used[i]);
        if (d < minDist) { minDist = d; }
    }
    return minDist;
}

/**
 * Цвет трассы из палитры.
 * Кольца по очереди: следующее, только когда предыдущее занято.
 * Внутри кольца берётся самый редкий оттенок, далёкий от уже выданных
 * и от последнего. Повтор не схлопывается в один красный.
 * @param {Set<string>|Iterable<string>} usedColors
 * @param {readonly string[]} palette
 * @returns {string}
 */
function pickTrackColorFromPalette(usedColors, palette) {
    const used = [];
    if (usedColors) {
        for (const c of usedColors) { used.push(c); }
    }
    const counts = new Map();
    for (let i = 0; i < palette.length; i++) { counts.set(palette[i], 0); }
    for (let i = 0; i < used.length; i++) {
        counts.set(used[i], (counts.get(used[i]) || 0) + 1);
    }
    let minCount = Infinity;
    for (let i = 0; i < palette.length; i++) {
        const n = counts.get(palette[i]) || 0;
        if (n < minCount) { minCount = n; }
    }
    // Кольца по 12. Следующее кольцо, только когда предыдущее уже занято на этом круге.
    // Иначе более тёмный «противоположный» оттенок обгоняет яркий из-за разницы светлоты.
    const ringSize = 12;
    let poolStart = 0;
    for (let ring = 0; ring < palette.length; ring += ringSize) {
        let has = false;
        const end = Math.min(ring + ringSize, palette.length);
        for (let i = ring; i < end; i++) {
            if ((counts.get(palette[i]) || 0) === minCount) { has = true; break; }
        }
        if (has) { poolStart = ring; break; }
    }
    const pool = [];
    const poolEnd = Math.min(poolStart + ringSize, palette.length);
    for (let i = poolStart; i < poolEnd; i++) {
        if ((counts.get(palette[i]) || 0) === minCount) { pool.push(palette[i]); }
    }
    if (pool.length === 0) { return palette[0]; }
    if (used.length === 0) { return pool[0]; }

    let best = pool[0];
    let bestDist = -1;
    let bestRecent = -1;
    const last = used[used.length - 1];
    for (let i = 0; i < pool.length; i++) {
        const candidate = pool[i];
        let minDist = minDistanceToUsed(candidate, used);
        if (minDist === Infinity) { minDist = 1000; }
        const recent = last === candidate ? 0 : trackColorDistance(candidate, last);
        const betterDist = minDist > bestDist + 1e-9;
        const tieRecent = Math.abs(minDist - bestDist) <= 1e-9 && recent > bestRecent + 1e-9;
        if (betterDist || tieRecent) {
            bestDist = minDist;
            bestRecent = recent;
            best = candidate;
        }
    }
    return best;
}

/** Палитра трасс группы: зависит от темы — вызывать при назначении цветов (в т.ч. после смены темы). */
function getTrackColorPalette() {
    if (typeof getThemeId === 'function' && getThemeId() === 'light') {
        return _PALETTE_LIGHT;
    }
    return _PALETTE_DARK;
}

/**
 * Лимит одновременно видимых дополнительных трасс (не считая tracking и selected).
 * 0 = без ограничений. Значение > 0 = лимит (будет задаваться из настроек).
 */
const MAX_VISIBLE_TRACKS = 0;

/** Ключ sessionStorage для сохранения selected КА (per-tab). */
const SELECTED_SAT_STORAGE_KEY = 'satellite-scout-selected-id';

/**
 * SatelliteStateManager — центральное хранилище данных спутников.
 *
 * Принимает данные от SSE Client, хранит состояние по noradId,
 * уведомляет подписчиков (UI-компоненты) через Observer pattern.
 *
 * Использование:
 *   const manager = new SatelliteStateManager();
 *   manager.subscribe('position', (state) => { ... });
 *   manager.updatePosition(data);  // от SSE
 */
class SatelliteStateManager {
    constructor() {
        /** Хранилище состояний спутников: noradId → SatelliteState. */
        this._satellites = new Map();

        /**
         * Устанавливается : клик по строке таблицы или авто из primary_id группы.
         * @type {?number}
         */
        this._selectedSatelliteId = null;

        /**
         * Спутник под наблюдением (красный/зелёный + overlay + az/el).
         * Устанавливается: только кнопка «Сопровождать» → API → SSE.
         * @type {?number}
         */
        this._trackingSatelliteId = null;

        /**
         * Флаг ручного выбора строки в таблице (чтобы авто-selected
         * из satellite_group_update не перебивал пользовательский выбор).
         */
        this._manualTableSelection = false;

        /** Текущая группа спутников из SSE-события satellite_group_update. */
        this._satelliteGroup = null;

        /**
         * Набор NORAD ID с включённой видимостью трассы (ручной toggle и режим «все трассы»).
         * isTrackVisible() также даёт true для текущих selected и tracking, если строка не в hiddenInShowAll.
         * @type {Set<number>}
         */
        this._visibleTrackIds = new Set();

        /**
         * Режим «все трассы группы» (UX-TABLE-TRACK-GROUP-MODE-001).
         * По умолчанию false — на карте/в небе видны только трассы selected
         * и tracking; оператор включает master-toggle в заголовке таблицы,
         * чтобы посмотреть трассы всех КА группы. Выбор сделан в пользу
         * «тихого» дефолта: при больших группах визуальный шум от десятков
         * пунктирных трасс перевешивает пользу от их одновременного показа.
         * @type {boolean}
         */
        this._showAllMode = false;

        /**
         * NORAD ID, вручную скрытые оператором в режиме showAll.
         * _applyShowAll() пропускает эти ID, чтобы ручные отключения
         * переживали периодические satellite_group_update.
         * Очищается при переключении master-toggle (setShowAllMode).
         * @type {Set<number>}
         */
        this._hiddenInShowAll = new Set();

        /**
         * Карта NORAD ID → цвет из палитры. Назначается случайно при формировании группы.
         * Не персистится — при смене группы или перезагрузке пересоздаётся.
         * @type {Map<number, string>}
         */
        this._colorMap = new Map();

        /**
         * Флаг: получен ли уже хотя бы один group_update.
         * Используется для корректной инициализации при подключении.
         */
        this._firstGroupUpdateReceived = false;

        /**
         * Ключ (primary_id:aos:los:is_visible) последнего group_update для обнаружения
         * смены данных пролёта при том же primary_id (переход видимости, новый виток).
         * @type {?string}
         */
        this._lastPrimaryPassKey = null;

        /** Подписчики: eventType → Set<callback>. */
        this._subscribers = new Map();
        for (const eventType of Object.values(StateEventType)) {
            this._subscribers.set(eventType, new Set());
        }

        /** @deprecated Совместимость — alias для _selectedSatelliteId */
        this._activeSatelliteId = null;
    }

    // ── Observer pattern ──────────────────────────────────────

    /**
     * Подписка на событие.
     * @param {string} eventType — тип события (см. StateEventType).
     * @param {Function} callback — функция-обработчик.
     * @returns {boolean} true если подписка успешна.
     */
    subscribe(eventType, callback) {
        const subs = this._subscribers.get(eventType);
        if (!subs) {
            console.warn(`[StateManager] unknown event type: ${eventType}`);
            return false;
        }
        if (typeof callback !== 'function') {
            console.warn('[StateManager] callback must be a function');
            return false;
        }
        subs.add(callback);
        return true;
    }

    /**
     * Отписка от события.
     * @param {string} eventType — тип события.
     * @param {Function} callback — функция-обработчик для удаления.
     * @returns {boolean} true если отписка успешна.
     */
    unsubscribe(eventType, callback) {
        const subs = this._subscribers.get(eventType);
        if (!subs) {
            return false;
        }
        return subs.delete(callback);
    }

    /**
     * Уведомление подписчиков о событии.
     * @param {string} eventType — тип события.
     * @param {SatelliteState} state — состояние спутника.
     * @private
     */
    _notify(eventType, state) {
        const subs = this._subscribers.get(eventType);
        if (!subs) {
            return;
        }
        for (const callback of subs) {
            try {
                callback(state);
            } catch (err) {
                console.error(`[StateManager] subscriber error on '${eventType}':`, err);
            }
        }
    }

    /**
     * Количество подписчиков для указанного типа событий.
     * @param {string} eventType — тип события.
     * @returns {number}
     */
    subscriberCount(eventType) {
        const subs = this._subscribers.get(eventType);
        return subs ? subs.size : 0;
    }

    // ── Обновление данных (вызывается из SSE Client) ─────────

    /**
     * Обновление позиции спутника.
     * Формат data соответствует SSE event:position с бэкенда.
     *
     * @param {Object} data — данные позиции.
     * @param {number} data.norad_id — NORAD ID.
     * @param {string} [data.name] — название спутника.
     * @param {number} data.lat — широта (градусы).
     * @param {number} data.lon — долгота (градусы).
     * @param {number} data.alt — высота (км).
     * @param {number} data.az — азимут (градусы).
     * @param {number} data.el — элевация (градусы).
     * @param {number} [data.range] — дальность (км).
     * @param {Object} [data.visibility_zone] — зона видимости.
     * @param {number} data.ts — timestamp (Unix ms).
     * @param {number} [data.map_marker_rot_deg] — запасной угол маркера (градусы).
     * @param {number} [data.map_marker_fwd_lon] — долгота вторая точка трека (для угла через project).
     * @param {number} [data.map_marker_fwd_lat] — широта вторая точка трека.
     */
    updatePosition(data) {
        if (!data || typeof data.norad_id !== 'number') {
            console.warn('[StateManager] updatePosition: invalid data (missing norad_id)');
            return;
        }

        const noradId = data.norad_id;
        const state = this._getOrCreateState(noradId, data.name);

        // Обновление позиции.
        const pos = {
            lat: data.lat,
            lon: data.lon,
            alt: data.alt,
            az: data.az,
            el: data.el,
            range: data.range || 0,
            range_rate: typeof data.range_rate === 'number' ? data.range_rate : 0,
            ts: data.ts,
        };
        if (typeof data.map_marker_rot_deg === 'number' && !Number.isNaN(data.map_marker_rot_deg)) {
            pos.map_marker_rot_deg = data.map_marker_rot_deg;
        }
        if (typeof data.map_marker_fwd_lon === 'number' && !Number.isNaN(data.map_marker_fwd_lon) &&
            typeof data.map_marker_fwd_lat === 'number' && !Number.isNaN(data.map_marker_fwd_lat)) {
            pos.map_marker_fwd_lon = data.map_marker_fwd_lon;
            pos.map_marker_fwd_lat = data.map_marker_fwd_lat;
        }
        state.position = pos;

        // Обновление имени, если передано.
        if (data.name) {
            state.name = data.name;
        }

        // Обновление зоны видимости (приходит вместе с позицией).
        if (data.visibility_zone) {
            state.visibilityZone = data.visibility_zone;
        }

        // Автоустановка selected при первом обновлении.
        if (this._selectedSatelliteId === null) {
            this._selectedSatelliteId = noradId;
            this._activeSatelliteId = noradId;
        }

        // POSITION стреляет для selected-спутника (основной вид карты).
        if (noradId === this._selectedSatelliteId) {
            this._notify(StateEventType.POSITION, state);
        }
    }

    /**
     * Обновление наземной трассы спутника.
     * Формат data соответствует SSE event:track с бэкенда.
     *
     * @param {Object} data — данные трека.
     * @param {number} data.norad_id — NORAD ID.
     * @param {Array} data.past — пройденные сегменты.
     * @param {Array} data.future — предстоящие сегменты.
     */
    updateTrack(data) {
        if (!data || typeof data.norad_id !== 'number') {
            console.warn('[StateManager] updateTrack: invalid data (missing norad_id)');
            return;
        }

        const noradId = data.norad_id;
        const state = this._getOrCreateState(noradId);

        const newPast = data.past || [];
        const newFuture = data.future || [];

        // Пропускаем, если трек не изменился.
        // Треки пересчитываются на бэкенде каждые 2 минуты, между обновлениями
        // приходят кешированные данные — дублировать перерисовку не нужно.
        // Сравниваем fingerprint: количество сегментов + timestamps крайних точек.
        const oldTrack = state.track;
        if (oldTrack) {
            const oldFp = SatelliteStateManager._trackFingerprint(oldTrack.past, oldTrack.future);
            const newFp = SatelliteStateManager._trackFingerprint(newPast, newFuture);
            if (oldFp === newFp) {
                return;
            }
        }

        state.track = {
            past: newPast,
            future: newFuture,
        };

        // Уведомление TRACK для выбранного спутника (подписчики обновляют карту).
        // forceTrackRefresh() в app.js дополнительно вызывается после batch-обработки.
        if (noradId === this._selectedSatelliteId) {
            this._notify(StateEventType.TRACK, state);
        }
    }

    /**
     * Fingerprint трека: количество сегментов + timestamps крайних точек.
     * При пересчёте на бэкенде (каждые 2 мин) сдвигается окно `now`,
     * поэтому timestamps первых/последних точек гарантированно меняются.
     * Между пересчётами (кешированные данные) fingerprint идентичен.
     * @param {Array} past — массив past-сегментов.
     * @param {Array} future — массив future-сегментов.
     * @returns {string} строковый fingerprint.
     * @static
     */
    static _trackFingerprint(past, future) {
        let fp = '';
        fp += (past ? past.length : 0) + ':';
        fp += (future ? future.length : 0);
        if (past && past.length > 0) {
            const first = past[0];
            if (first && first.length > 0) {
                fp += ':p0=' + (first[0].ts || first[0].time || 0);
            }
            const last = past[past.length - 1];
            if (last && last.length > 0) {
                fp += ':pN=' + (last[last.length - 1].ts || last[last.length - 1].time || 0);
            }
        }
        if (future && future.length > 0) {
            const first = future[0];
            if (first && first.length > 0) {
                fp += ':f0=' + (first[0].ts || first[0].time || 0);
            }
            const last = future[future.length - 1];
            if (last && last.length > 0) {
                fp += ':fN=' + (last[last.length - 1].ts || last[last.length - 1].time || 0);
            }
        }
        return fp;
    }

    /**
     * Принудительное уведомление TRACK для активного спутника.
     * Вызывается после batch-обработки всех треков из satellite_state_update,
     * гарантируя что треки вторичных спутников уже сохранены в кеше.
     */
    forceTrackRefresh() {
        if (this._selectedSatelliteId === null) { return; }
        const state = this._satellites.get(this._selectedSatelliteId);
        if (!state) { return; }
        this._notify(StateEventType.TRACK, state);
    }

    /**
     * Принудительное уведомление после batch satellite_state_update (позиции).
     * Все norad_id уже в кеше; подписчики синхронизируют EarthView/SkyView и рисуют карту.
     */
    forcePositionRefresh() {
        this._notify(StateEventType.GROUP_POSITION, null);
    }

    // ── Выбранный спутник (selected) ───────────────────────────

    /**
     * Установка выбранного спутника
     * Вызывается: клик по строке таблицы или авто из группы.
     *
     * @param {number} noradId — NORAD ID.
     * @param {string} [name] — имя.
     * @param {boolean} [manual=false] — ручной выбор в таблице.
     * @param {boolean} [forceNotify=false] — если true, снова шлём SELECTED_CHANGE/TRACK при том же NORAD
     *   (нужно после tracking_ended: сняли с наблюдения, тот же primary — перерисовать таблицу и трассы).
     * @returns {boolean}
     */
    setSelectedSatellite(noradId, name, manual = false, forceNotify = false) {
        if (typeof noradId !== 'number' || noradId <= 0) { return false; }

        const prevId = this._selectedSatelliteId;
        this._selectedSatelliteId = noradId;
        this._activeSatelliteId = noradId; // backward compat
        this._manualTableSelection = manual;

        // Сохраняем в sessionStorage для восстановления при reload (per-tab).
        this._persistSelectedId(noradId);

        const state = this._getOrCreateState(noradId);
        if (name) { state.name = name; }

        if (noradId !== prevId || forceNotify) {
            this._notify(StateEventType.SELECTED_CHANGE, state);
            if (state.position) {
                this._notify(StateEventType.POSITION, state);
            }
            this._notify(StateEventType.TRACK, state);
        }
        return true;
    }

    /** NORAD ID выбранного спутника. */
    getSelectedSatelliteId() {
        return this._selectedSatelliteId;
    }

    /** true если оператор вручную выбрал строку в таблице (клик). */
    isManualTableSelection() {
        return this._manualTableSelection;
    }

    // ── Спутник под наблюдением (tracking) ──────────────────

    /**
     * Постановка спутника под наблюдение.
     * Вызывается из SSE-события (reason "manual") после подтверждения бэкендом.
     *
     * @param {number} noradId — NORAD ID.
     * @param {string} [name] — имя.
     * @param {Object} [orbitalParams] — {inclination, period}.
     */
    setTrackingSatellite(noradId, name, orbitalParams) {
        if (typeof noradId !== 'number' || noradId <= 0) { return; }

        const prevId = this._trackingSatelliteId;
        this._trackingSatelliteId = noradId;

        const state = this._getOrCreateState(noradId);
        if (name) { state.name = name; }
        if (orbitalParams) {
            if (typeof orbitalParams.inclination === 'number') { state.inclination = orbitalParams.inclination; }
            if (typeof orbitalParams.period === 'number') { state.period = orbitalParams.period; }
        }

        if (noradId !== prevId) {
            this._notify(StateEventType.TRACKING_CHANGE, state);
        }
    }

    /**
     * Сброс наблюдения (tracking_ended или ручной сброс).
     */
    clearTrackingSatellite() {
        if (this._trackingSatelliteId === null) { return; }
        this._trackingSatelliteId = null;
        this._notify(StateEventType.TRACKING_CHANGE, null);
    }

    /** NORAD ID спутника под наблюдением (null = нет). */
    getTrackingSatelliteId() {
        return this._trackingSatelliteId;
    }

    // ── Backward compat ──────────────────────────────────────

    /**
     * @deprecated Использовать setSelectedSatellite / setTrackingSatellite.
     */
    setActiveSatellite(noradId, name) {
        return this.setSelectedSatellite(noradId, name, false);
    }

    /** @deprecated Использовать getSelectedSatelliteId. */
    getActiveSatelliteId() {
        return this._selectedSatelliteId;
    }

    // ── Чтение состояния ──────────────────────────────────────

    /**
     * Получить состояние спутника по NORAD ID.
     * @param {number} noradId — NORAD ID.
     * @returns {?SatelliteState} состояние или null.
     */
    getState(noradId) {
        return this._satellites.get(noradId) || null;
    }

    /**
     * Получить состояние активного спутника (shortcut).
     * @returns {?SatelliteState}
     */
    getActiveState() {
        if (this._activeSatelliteId === null) {
            return null;
        }
        return this._satellites.get(this._activeSatelliteId) || null;
    }

    /**
     * Список всех известных NORAD ID.
     * @returns {number[]}
     */
    getSatelliteIds() {
        return Array.from(this._satellites.keys());
    }

    /**
     * Количество известных спутников.
     * @returns {number}
     */
    get satelliteCount() {
        return this._satellites.size;
    }

    /**
     * Обновление группы спутников из SSE-события satellite_group_update.
     * Уведомляет подписчиков на SATELLITE_GROUP_UPDATE.
     *
     * @param {Object} data — данные события.
     * @param {Array}  data.satellites — список спутников в группе.
     * @param {number} data.primary_id — NORAD ID primary спутника.
     * @param {Object} data.time_window — временное окно {start, end} (Unix ms).
     * @param {number} data.ts — timestamp события.
     */
    setSatelliteGroup(data) {
        if (!data || !Array.isArray(data.satellites)) {
            console.warn('[StateManager] setSatelliteGroup: invalid data');
            return;
        }
        this._satelliteGroup = data;

        // При первом group_update пытаемся восстановить selected из sessionStorage
        // (оператор обновил страницу — ручной выбор сохранён per-tab).
        if (!this._firstGroupUpdateReceived) {
            const restoredId = this._restoreSelectedId(data);
            if (restoredId > 0) {
                const satInfo = data.satellites.find(s => s.norad_id === restoredId);
                this._selectedSatelliteId = restoredId;
                this._activeSatelliteId = restoredId;
                this._manualTableSelection = true;
                const state = this._getOrCreateState(restoredId, satInfo ? satInfo.sat_name : '');
                this._notify(StateEventType.SELECTED_CHANGE, state);
                if (state.position) { this._notify(StateEventType.POSITION, state); }
                this._notify(StateEventType.TRACK, state);
            }
        }

        // Если оператор выбирал строку вручную, но этого спутника больше нет в группе —
        // сбрасываем ручной выбор, чтобы авто-выбор primary_id от бэкенда сработал.
        if (this._manualTableSelection && this._selectedSatelliteId) {
            const stillInGroup = data.satellites.some(s => s.norad_id === this._selectedSatelliteId);
            if (!stillInGroup) {
                this._manualTableSelection = false;
            }
        }

        // Авто-выбор selected из primary_id (если нет ручного выбора в таблице).
        if (!this._manualTableSelection && typeof data.primary_id === 'number' && data.primary_id > 0) {
            const prevSel = this._selectedSatelliteId;
            this._selectedSatelliteId = data.primary_id;
            this._activeSatelliteId = data.primary_id;

            const primaryPassChanged = this._hasPrimaryPassChanged(data);
            if (data.primary_id !== prevSel || primaryPassChanged) {
                const satInfo = data.satellites.find(s => s.norad_id === data.primary_id);
                const state = this._getOrCreateState(data.primary_id, satInfo ? satInfo.sat_name : '');
                this._notify(StateEventType.SELECTED_CHANGE, state);
                if (state.position) { this._notify(StateEventType.POSITION, state); }
                this._notify(StateEventType.TRACK, state);
            }
        }

        this._firstGroupUpdateReceived = true;

        // Назначить цвета и автовключить трассы для новых КА группы.
        this._syncGroupTracks(data);

        // Очистка stale-записей из _satellites: удаляем КА, ушедших из группы
        // (кроме selected и tracking — их данные ещё актуальны).
        this._cleanupStaleStates(data);

        this._notify(StateEventType.SATELLITE_GROUP_UPDATE, data);
    }


    /**
     * Получить текущую группу спутников.
     * @returns {?Object} группа или null.
     */
    getSatelliteGroup() {
        return this._satelliteGroup;
    }

    /**
     * Получить primary (активный) спутник из группы.
     * @returns {?Object} PassInfo или null.
     */
    getPrimarySatellite() {
        if (!this._satelliteGroup) { return null; }
        const id = this._satelliteGroup.primary_id;
        return this._satelliteGroup.satellites.find(s => s.norad_id === id) || null;
    }

    /**
     * Получить secondary (не primary) спутники из группы.
     * @returns {Array} массив PassInfo (может быть пустым).
     */
    getSecondarySatellites() {
        if (!this._satelliteGroup) { return []; }
        const id = this._satelliteGroup.primary_id;
        return this._satelliteGroup.satellites.filter(s => s.norad_id !== id);
    }

    // ── Видимость трасс (UX-TABLE-TRACK-GROUP-MODE-001) ──────────────

    /**
     * Toggle видимости трассы для спутника.
     * В режиме showAll=true — скрывает выбранную трассу.
     * В режиме showAll=false — добавляет выбранную трассу.
     * Не сбрасывает master-toggle.
     * @param {number} noradId
     * @returns {boolean} true если трасса теперь видима.
     */
    toggleTrackVisibility(noradId) {
        if (typeof noradId !== 'number' || noradId <= 0) { return false; }
        if (this._visibleTrackIds.has(noradId)) {
            this._visibleTrackIds.delete(noradId);
            if (this._showAllMode) { this._hiddenInShowAll.add(noradId); }
            this._notify(StateEventType.TRACK_VISIBILITY_CHANGE, this.getVisibleTrackIds());
            return false;
        }
        if (MAX_VISIBLE_TRACKS > 0 && this._visibleTrackIds.size >= MAX_VISIBLE_TRACKS) {
            console.warn(`[StateManager] лимит трасс (${MAX_VISIBLE_TRACKS}) достигнут`);
            if (typeof window !== 'undefined' && typeof window.dispatchEvent === 'function') {
                try {
                    window.dispatchEvent(new CustomEvent('satellite-scout-track-limit', {
                        detail: { max: MAX_VISIBLE_TRACKS },
                    }));
                } catch (_e) { /* ignore */ }
            }
            return false;
        }
        this._hiddenInShowAll.delete(noradId);
        this._visibleTrackIds.add(noradId);
        this._notify(StateEventType.TRACK_VISIBILITY_CHANGE, this.getVisibleTrackIds());
        return true;
    }

    /** Очистить все дополнительные трассы. */
    clearAllTracks() {
        if (this._visibleTrackIds.size === 0) { return; }
        this._visibleTrackIds.clear();
        this._notify(StateEventType.TRACK_VISIBILITY_CHANGE, this.getVisibleTrackIds());
    }

    /**
     * Множество _visibleTrackIds (явные включения; в showAll — все группы минус скрытые глазом).
     * @returns {number[]}
     */
    getVisibleTrackIds() {
        return Array.from(this._visibleTrackIds);
    }

    /**
     * Проверка: нужно ли рисовать трассу для данного спутника.
     * true если noradId в visibleTrackIds ИЛИ tracking ИЛИ selected.
     * @param {number} noradId
     * @returns {boolean}
     */
    isTrackVisible(noradId) {
        // В режиме «все трассы» ручное скрытие глазом важнее «всегда видим» для selected/tracking.
        if (this._showAllMode && this._hiddenInShowAll.has(noradId)) {
            return false;
        }
        return noradId === this._trackingSatelliteId ||
               noradId === this._selectedSatelliteId ||
               this._visibleTrackIds.has(noradId);
    }

    /**
     * Цвет трассы для спутника из палитры (случайное назначение при формировании группы).
     * Для tracking и selected возвращает null (у них свои фиксированные цвета на карте).
     * @param {number} noradId
     * @returns {?string} hex-цвет или null.
     */
    getTrackColor(noradId) {
        if (noradId === this._trackingSatelliteId || noradId === this._selectedSatelliteId) {
            return null;
        }
        return this._colorMap.get(noradId) || null;
    }

    /**
     * Цвет маркера спутника из палитры. Возвращает цвет для всех КА группы,
     * включая selected/tracking (маркеры всегда цветные по палитре).
     * @param {number} noradId
     * @returns {?string} hex-цвет или null.
     */
    getMarkerColor(noradId) {
        return this._colorMap.get(noradId) || null;
    }

    // ── Режим «все трассы группы» (UX-TABLE-TRACK-GROUP-MODE-001) ──

    /** @returns {boolean} текущее состояние master-toggle. */
    isShowAllMode() {
        return this._showAllMode;
    }

    /**
     * Установить режим «все трассы группы».
     * ON → все КА группы становятся видимыми (ручные скрытия сбрасываются).
     * OFF → только selected + tracking (visibleTrackIds очищается).
     * @param {boolean} on
     */
    setShowAllMode(on) {
        this._showAllMode = Boolean(on);
        this._hiddenInShowAll.clear();
        if (this._showAllMode) {
            this._applyShowAll();
        } else {
            this._visibleTrackIds.clear();
        }
        this._notify(StateEventType.SHOW_ALL_MODE_CHANGE, this._showAllMode);
        this._notify(StateEventType.TRACK_VISIBILITY_CHANGE, this.getVisibleTrackIds());
    }

    /**
     * Синхронизация трасс и цветов при обновлении группы.
     * Назначает цвета новым КА, удаляет цвета ушедших, применяет showAll.
     * @param {Object} data — данные satellite_group_update.
     * @private
     */
    _syncGroupTracks(data) {
        if (!data || !Array.isArray(data.satellites)) { return; }
        // Цвета и для пролётов ленты на час: иначе далёкий КА без цвета на горках и рисках
        const groupIds = new Set();
        for (const sat of data.satellites) {
            groupIds.add(sat.norad_id);
        }
        if (Array.isArray(data.timeline)) {
            for (let i = 0; i < data.timeline.length; i++) {
                groupIds.add(data.timeline[i].norad_id);
            }
        }

        // Удалить ушедших из colorMap, visibleTrackIds и hiddenInShowAll.
        for (const id of this._colorMap.keys()) {
            if (!groupIds.has(id)) { this._colorMap.delete(id); }
        }
        for (const id of this._visibleTrackIds) {
            if (!groupIds.has(id)) { this._visibleTrackIds.delete(id); }
        }
        for (const id of this._hiddenInShowAll) {
            if (!groupIds.has(id)) { this._hiddenInShowAll.delete(id); }
        }

        // Назначить цвета новым КА: сначала строкам таблицы, потом ленте на час.
        const palette = getTrackColorPalette();
        for (const nid of groupIds) {
            if (!this._colorMap.has(nid)) {
                const color = pickTrackColorFromPalette(this._colorMap.values(), palette);
                this._colorMap.set(nid, color);
            }
        }
        this._separateVisibleSatelliteColors(data.satellites);

        // В режиме showAll — добавить все КА группы в visibleTrackIds.
        if (this._showAllMode) {
            this._applyShowAll();
        }

        this._notify(StateEventType.TRACK_VISIBILITY_CHANGE, this.getVisibleTrackIds());
    }

    /**
     * Переназначить цвета трасс под текущую тему (после смены light ↔ dark).
     * Сохраняет порядок КА в colorMap, подбирает новые оттенки из актуальной палитры.
     */
    reassignTrackColorsForTheme() {
        const ids = Array.from(this._colorMap.keys());
        this._colorMap.clear();
        const palette = getTrackColorPalette();
        for (let i = 0; i < ids.length; i++) {
            const color = pickTrackColorFromPalette(this._colorMap.values(), palette);
            this._colorMap.set(ids[i], color);
        }
        if (this._satelliteGroup && Array.isArray(this._satelliteGroup.satellites)) {
            this._separateVisibleSatelliteColors(this._satelliteGroup.satellites);
        }
        this._notify(StateEventType.TRACK_VISIBILITY_CHANGE, this.getVisibleTrackIds());
    }

    /**
     * КА в таблице не должны сидеть на соседних оттенках.
     * Уже выданный цвет остаётся, если он не хуже лучшего относительно других видимых.
     * Лента на час может повторять цвета: на карте важны те, кто сейчас в плане.
     * @param {readonly {norad_id: number}[]} satellites
     * @private
     */
    _separateVisibleSatelliteColors(satellites) {
        if (!Array.isArray(satellites) || satellites.length === 0) { return; }
        const visible = new Set();
        for (let i = 0; i < satellites.length; i++) {
            const id = satellites[i].norad_id;
            if (this._colorMap.has(id)) { visible.add(id); }
        }
        const ordered = [];
        for (const id of this._colorMap.keys()) {
            if (visible.has(id)) { ordered.push(id); }
        }
        const palette = getTrackColorPalette();
        for (let i = 1; i < ordered.length; i++) {
            const id = ordered[i];
            const others = [];
            for (let j = 0; j < i; j++) { others.push(this._colorMap.get(ordered[j])); }
            const current = this._colorMap.get(id);
            const picked = pickTrackColorFromPalette(others, palette);
            const curMin = minDistanceToUsed(current, others);
            const bestMin = minDistanceToUsed(picked, others);
            if (curMin + 0.02 >= bestMin) { continue; }
            this._colorMap.set(id, picked);
        }
    }

    /**
     * Удаление из _satellites записей КА, которых нет в текущей группе.
     * Selected и tracking сохраняются — их данные могут понадобиться до следующего обновления.
     * Предотвращает бесконечный рост Map при длительной работе приложения.
     * @param {Object} data — данные satellite_group_update.
     * @private
     */
    _cleanupStaleStates(data) {
        if (!data || !Array.isArray(data.satellites)) { return; }
        const groupIds = new Set();
        for (const sat of data.satellites) {
            groupIds.add(sat.norad_id);
        }
        // Не удаляем selected и tracking — их данные актуальны.
        if (this._selectedSatelliteId) { groupIds.add(this._selectedSatelliteId); }
        if (this._trackingSatelliteId) { groupIds.add(this._trackingSatelliteId); }

        for (const id of this._satellites.keys()) {
            if (!groupIds.has(id)) {
                this._satellites.delete(id);
            }
        }
    }

    /**
     * Включить видимость трасс для всех КА текущей группы (включая tracking и selected).
     * Иначе при смене строки «текущий» перестаёт попадать в _visibleTrackIds и трасса пропадает
     * (раньше selected/tracking пропускались — они считались «всегда видимыми» только пока совпадают с ролью).
     * @private
     */
    _applyShowAll() {
        this._visibleTrackIds.clear();
        if (!this._satelliteGroup || !this._satelliteGroup.satellites) { return; }
        for (const sat of this._satelliteGroup.satellites) {
            const nid = sat.norad_id;
            if (this._hiddenInShowAll.has(nid)) { continue; }
            if (MAX_VISIBLE_TRACKS > 0 && this._visibleTrackIds.size >= MAX_VISIBLE_TRACKS) { break; }
            this._visibleTrackIds.add(nid);
        }
    }

    // ── Persist selected (sessionStorage, per-tab) ──────────────

    /** @private */
    _persistSelectedId(noradId) {
        try {
            if (typeof sessionStorage !== 'undefined') {
                sessionStorage.setItem(SELECTED_SAT_STORAGE_KEY, String(noradId));
            }
        } catch (_e) { /* ignore */ }
    }

    /**
     * Восстановление selected из sessionStorage.
     * Возвращает NORAD ID если КА есть в текущей группе, иначе 0.
     * @param {Object} groupData — данные satellite_group_update.
     * @returns {number}
     */
    _restoreSelectedId(groupData) {
        try {
            if (typeof sessionStorage === 'undefined') { return 0; }
            const raw = sessionStorage.getItem(SELECTED_SAT_STORAGE_KEY);
            if (!raw) { return 0; }
            const id = parseInt(raw, 10);
            if (!id || id <= 0) { return 0; }
            if (!groupData || !Array.isArray(groupData.satellites)) { return 0; }
            const inGroup = groupData.satellites.some(function(s) { return s.norad_id === id; });
            return inGroup ? id : 0;
        } catch (_e) { return 0; }
    }

    // (localStorage-персистентность visibleTrackIds убрана — UX-TABLE-TRACK-GROUP-MODE-001)

    // ── Очистка ───────────────────────────────────────────────

    /**
     * Удаление спутника из хранилища.
     * Если удалён активный спутник — сбрасывает activeSatelliteId.
     *
     * @param {number} noradId — NORAD ID.
     * @returns {boolean} true если спутник был удалён.
     */
    removeSatellite(noradId) {
        const deleted = this._satellites.delete(noradId);
        if (deleted && this._selectedSatelliteId === noradId) {
            this._selectedSatelliteId = null;
            this._activeSatelliteId = null;
        }
        return deleted;
    }

    clear() {
        this._satellites.clear();
        this._selectedSatelliteId = null;
        this._trackingSatelliteId = null;
        this._activeSatelliteId = null;
        this._manualTableSelection = false;
        this._visibleTrackIds.clear();
        this._hiddenInShowAll.clear();
        this._colorMap.clear();
        this._showAllMode = false;
    }

    // ── Внутренние методы ─────────────────────────────────────

    /**
     * Проверка: изменились ли данные пролёта primary спутника в satellite_group_update.
     * Сравнивает (primary_id, aos, los, is_visible) с предыдущим group_update.
     * @param {Object} data — данные satellite_group_update.
     * @returns {boolean} true если данные изменились.
     * @private
     */
    _hasPrimaryPassChanged(data) {
        const satInfo = data.satellites.find(s => s.norad_id === data.primary_id);
        const key = satInfo
            ? `${data.primary_id}:${satInfo.aos}:${satInfo.los}:${satInfo.is_visible}`
            : `${data.primary_id}`;
        const changed = (this._lastPrimaryPassKey !== null && this._lastPrimaryPassKey !== key);
        this._lastPrimaryPassKey = key;
        return changed;
    }

    /**
     * Получить или создать состояние спутника.
     * @param {number} noradId — NORAD ID.
     * @param {string} [name] — название (для нового спутника).
     * @returns {SatelliteState}
     * @private
     */
    _getOrCreateState(noradId, name = '') {
        let state = this._satellites.get(noradId);
        if (!state) {
            state = new SatelliteState(noradId, name);
            this._satellites.set(noradId, state);
        }
        return state;
    }
}

// Экспорт для использования в других модулях и тестах.
if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
    module.exports = { // eslint-disable-line no-undef
        SatelliteStateManager,
        SatelliteState,
        StateEventType,
        getTrackColorPalette,
        pickTrackColorFromPalette,
        trackColorDistance,
        paletteMinHueSeparation,
    };
}

if (typeof window !== 'undefined') {
    window.SatelliteStateManager = SatelliteStateManager;
    window.SatelliteState = SatelliteState;
    window.StateEventType = StateEventType;
    window.getTrackColorPalette = getTrackColorPalette;
}
