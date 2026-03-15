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
    /** Обновление позиции выбранного спутника (для карты/SkyView). */
    POSITION: 'position',
    /** Обновление трека выбранного спутника. */
    TRACK: 'track',
    /** Смена выбранного спутника (selected) — клик в таблице или авто из группы. */
    SELECTED_CHANGE: 'selected_change',
    /** Смена спутника на сопровождении (tracking) — только кнопка «Сопровождение». */
    TRACKING_CHANGE: 'tracking_change',
    /** Обновление группы скользящего окна. */
    SATELLITE_GROUP_UPDATE: 'satellite_group_update',
    /** @deprecated Использовать SELECTED_CHANGE / TRACKING_CHANGE. */
    SATELLITE_CHANGE: 'satellite_change',
});

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
         * Спутник на сопровождении (красный/зелёный + overlay + az/el).
         * Устанавливается: только кнопка «Сопровождение» → API → SSE.
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
         * Флаг: получен ли уже хотя бы один group_update.
         * Первый (кешированный) group_update при подключении может содержать
         * устаревший tracking_id от предыдущей сессии → не применяем tracking_id,
         * чтобы не было мелькания «сопровождения» при обновлении страницы.
         */
        this._firstGroupUpdateReceived = false;

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
     */
    updatePosition(data) {
        if (!data || typeof data.norad_id !== 'number') {
            console.warn('[StateManager] updatePosition: invalid data (missing norad_id)');
            return;
        }

        const noradId = data.norad_id;
        const state = this._getOrCreateState(noradId, data.name);

        // Обновление позиции.
        state.position = {
            lat: data.lat,
            lon: data.lon,
            alt: data.alt,
            az: data.az,
            el: data.el,
            range: data.range || 0,
            ts: data.ts,
        };

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

        // Пропускаем, если трек не изменился (тот же размер сегментов).
        // Треки пересчитываются на бэкенде каждые 30 секунд, между обновлениями
        // приходят кешированные данные — дублировать перерисовку не нужно.
        const oldTrack = state.track;
        const changed = !oldTrack ||
            oldTrack.past.length !== newPast.length ||
            oldTrack.future.length !== newFuture.length;

        if (!changed) {
            return;
        }

        state.track = {
            past: newPast,
            future: newFuture,
        };

        // Уведомление НЕ отправляем здесь — только через forceTrackRefresh()
        // после того, как batch из satellite_state_update полностью обработан.
        // Это гарантирует, что треки вторичных спутников уже в кеше к моменту
        // вызова _updateSecondaryTracks() в app.js.

        return changed;
    }

    /**
     * Принудительное уведомление TRACK для активного спутника.
     * Вызывается после batch-обработки всех треков из satellite_state_update,
     * гарантируя что треки вторичных спутников уже сохранены в кеше.
     */
    forceTrackRefresh() {
        if (this._activeSatelliteId === null) { return; }
        const state = this._satellites.get(this._activeSatelliteId);
        if (!state) { return; }
        this._notify(StateEventType.TRACK, state);
    }

    // ── Выбранный спутник (selected) ───────────────────────────

    /**
     * Установка выбранного спутника
     * Вызывается: клик по строке таблицы или авто из группы.
     *
     * @param {number} noradId — NORAD ID.
     * @param {string} [name] — имя.
     * @param {boolean} [manual=false] — ручной выбор в таблице.
     * @returns {boolean}
     */
    setSelectedSatellite(noradId, name, manual = false) {
        if (typeof noradId !== 'number' || noradId <= 0) { return false; }

        const prevId = this._selectedSatelliteId;
        this._selectedSatelliteId = noradId;
        this._activeSatelliteId = noradId; // backward compat
        this._manualTableSelection = manual;

        const state = this._getOrCreateState(noradId);
        if (name) { state.name = name; }

        if (noradId !== prevId) {
            this._notify(StateEventType.SELECTED_CHANGE, state);
            // Немедленно пушим кешированные данные для нового selected.
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

    // ── Спутник на сопровождении (tracking) ──────────────────

    /**
     * Установка спутника на сопровождение.
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
     * Сброс сопровождения (tracking_ended или ручной сброс).
     */
    clearTrackingSatellite() {
        if (this._trackingSatelliteId === null) { return; }
        this._trackingSatelliteId = null;
        this._notify(StateEventType.TRACKING_CHANGE, null);
    }

    /** NORAD ID спутника на сопровождении (null = нет). */
    getTrackingSatelliteId() {
        return this._trackingSatelliteId;
    }

    // ── Backward compat ──────────────────────────────────────

    /**
     * @deprecated Использовать setSelectedSatellite / setTrackingSatellite.
     */
    setActiveSatellite(noradId, name, orbitalParams) {
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

        // Авто-выбор selected из primary_id (если нет ручного выбора в таблице).
        if (!this._manualTableSelection && typeof data.primary_id === 'number' && data.primary_id > 0) {
            const prevSel = this._selectedSatelliteId;
            this._selectedSatelliteId = data.primary_id;
            this._activeSatelliteId = data.primary_id;
            if (data.primary_id !== prevSel) {
                const satInfo = data.satellites.find(s => s.norad_id === data.primary_id);
                const state = this._getOrCreateState(data.primary_id, satInfo ? satInfo.sat_name : '');
                this._notify(StateEventType.SELECTED_CHANGE, state);
                if (state.position) { this._notify(StateEventType.POSITION, state); }
                this._notify(StateEventType.TRACK, state);
            }
        }

        // Синхронизация tracking_id из бэкенда.
        // Первый group_update — из кеша Hub (snapshot при подключении, satellite_change НЕ кешируется).
        // Игнорируем tracking_id из него: пользователь должен явно нажать «Сопровождение» в этой сессии.
        // Последующие group_update (от живого бэкенда) — применяем tracking_id.
        const rawTrackingId = (typeof data.tracking_id === 'number' && data.tracking_id > 0) ? data.tracking_id : null;
        const newTrackingId = this._firstGroupUpdateReceived ? rawTrackingId : null;
        this._firstGroupUpdateReceived = true;

        if (newTrackingId !== this._trackingSatelliteId) {
            this._trackingSatelliteId = newTrackingId;
            if (newTrackingId) {
                const st = this._getOrCreateState(newTrackingId);
                this._notify(StateEventType.TRACKING_CHANGE, st);
            } else {
                this._notify(StateEventType.TRACKING_CHANGE, null);
            }
        }

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
    }

    // ── Внутренние методы ─────────────────────────────────────

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
    module.exports = { SatelliteStateManager, SatelliteState, StateEventType }; // eslint-disable-line no-undef
}

// Экспорт для использования в браузере.
if (typeof window !== 'undefined') {
    window.SatelliteStateManager = SatelliteStateManager;
    window.SatelliteState = SatelliteState;
    window.StateEventType = StateEventType;
}
