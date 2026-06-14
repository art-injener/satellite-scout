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

/**
 * Палитра 20 цветов для трасс вторичных спутников (UX-TABLE-TRACK-GROUP-MODE-001).
 * Подобраны для читаемости на тёмной карте и в таблице.
 * Не используется для tracking (красно-зелёный) и selected (циан/жёлтый).
 * Цвет назначается случайно при формировании группы через _colorMap.
 */
const _PALETTE_DARK = Object.freeze([
    '#ff6b6b', // коралловый
    '#51cf66', // зелёный
    '#339af0', // голубой
    '#ff922b', // оранжевый
    '#cc5de8', // фиолетовый
    '#22b8cf', // бирюзовый
    '#fcc419', // жёлтый
    '#ff8787', // розово-красный
    '#20c997', // мятный
    '#748ffc', // индиго
    '#f06595', // розовый
    '#94d82d', // лаймовый
    '#e599f7', // лавандовый
    '#fd7e14', // тёмно-оранжевый
    '#66d9e8', // светло-бирюзовый
    '#ffa94d', // абрикосовый
    '#69db7c', // светло-зелёный
    '#da77f2', // пурпурный
    '#a9e34b', // салатовый
    '#74c0fc', // небесный
]);

/**
 * Насыщенная палитра для светлой темы — контрастные цвета на светлом фоне карты.
 */
const _PALETTE_LIGHT = Object.freeze([
    '#c62828',
    '#1565c0',
    '#2e7d32',
    '#d84315',
    '#7b1fa2',
    '#00838f',
    '#c2185b',
    '#283593',
    '#558b2f',
    '#6a1b9a',
    '#00796b',
    '#bf360c',
    '#0277bd',
    '#8e24aa',
    '#e65100',
    '#1b5e20',
    '#4a148c',
    '#01579b',
    '#b71c1c',
    '#004d40',
]);

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
        // Треки пересчитываются на бэкенде каждые 30 секунд, между обновлениями
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
     * При пересчёте на бэкенде (каждые 30с) сдвигается окно `now`,
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
        const groupIds = new Set();
        for (const sat of data.satellites) {
            groupIds.add(sat.norad_id);
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

        // Назначить цвета новым КА (случайный выбор из палитры).
        const usedColors = new Set(this._colorMap.values());
        const palette = getTrackColorPalette();
        const available = palette.filter(c => !usedColors.has(c));
        for (const sat of data.satellites) {
            const nid = sat.norad_id;
            if (!this._colorMap.has(nid)) {
                let color;
                if (available.length > 0) {
                    const ri = Math.floor(Math.random() * available.length);
                    color = available.splice(ri, 1)[0];
                } else {
                    color = palette[Math.floor(Math.random() * palette.length)];
                }
                this._colorMap.set(nid, color);
            }
        }

        // В режиме showAll — добавить все КА группы в visibleTrackIds.
        if (this._showAllMode) {
            this._applyShowAll();
        }

        this._notify(StateEventType.TRACK_VISIBILITY_CHANGE, this.getVisibleTrackIds());
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
    module.exports = { SatelliteStateManager, SatelliteState, StateEventType, getTrackColorPalette }; // eslint-disable-line no-undef
}

if (typeof window !== 'undefined') {
    window.SatelliteStateManager = SatelliteStateManager;
    window.SatelliteState = SatelliteState;
    window.StateEventType = StateEventType;
    window.getTrackColorPalette = getTrackColorPalette;
}
