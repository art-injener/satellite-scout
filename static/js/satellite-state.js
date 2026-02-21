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
    /** Обновление позиции активного спутника. */
    POSITION: 'position',
    /** Обновление трека активного спутника. */
    TRACK: 'track',
    /** Смена активного спутника. */
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
        /**
         * Хранилище состояний спутников: noradId → SatelliteState.
         * @type {Map<number, SatelliteState>}
         */
        this._satellites = new Map();

        /**
         * NORAD ID активного (отображаемого) спутника.
         * @type {?number}
         */
        this._activeSatelliteId = null;

        /**
         * Подписчики: eventType → Set<callback>.
         * @type {Map<string, Set<Function>>}
         */
        this._subscribers = new Map();

        // Инициализация каналов подписок.
        for (const eventType of Object.values(StateEventType)) {
            this._subscribers.set(eventType, new Set());
        }
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

        // Автоустановка активного спутника при первом обновлении.
        if (this._activeSatelliteId === null) {
            this._activeSatelliteId = noradId;
        }

        // Notify только для активного спутника.
        if (noradId === this._activeSatelliteId) {
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

        // Пропускаем нотификацию, если трек не изменился (тот же размер сегментов).
        // Треки пересчитываются на бэкенде каждые 30 секунд, между обновлениями
        // приходят кешированные данные — дублировать перерисовку не нужно.
        const oldTrack = state.track;
        if (oldTrack &&
            oldTrack.past.length === newPast.length &&
            oldTrack.future.length === newFuture.length) {
            return;
        }

        state.track = {
            past: newPast,
            future: newFuture,
        };

        if (noradId === this._activeSatelliteId) {
            this._notify(StateEventType.TRACK, state);
        }
    }

    // ── Активный спутник ──────────────────────────────────────

    /**
     * Установка активного спутника.
     * Вызывает satellite_change event с состоянием нового спутника.
     *
     * @param {number} noradId — NORAD ID спутника.
     * @param {string} [name] — Имя спутника (опционально, из SSE события).
     * @param {Object} [orbitalParams] — Орбитальные параметры {inclination, period}.
     * @returns {boolean} true если спутник найден или создан.
     */
    setActiveSatellite(noradId, name, orbitalParams) {
        if (typeof noradId !== 'number' || noradId <= 0) {
            console.warn('[StateManager] setActiveSatellite: invalid noradId');
            return false;
        }

        const prevId = this._activeSatelliteId;
        this._activeSatelliteId = noradId;

        // Notify о смене спутника (даже если состояние ещё пустое).
        if (noradId !== prevId) {
            const state = this._getOrCreateState(noradId);
            // Устанавливаем имя из SSE если оно есть
            if (name && typeof name === 'string') {
                state.name = name;
            }
            // Устанавливаем орбитальные параметры из SSE
            if (orbitalParams) {
                if (typeof orbitalParams.inclination === 'number') {
                    state.inclination = orbitalParams.inclination;
                }
                if (typeof orbitalParams.period === 'number') {
                    state.period = orbitalParams.period;
                }
            }
            this._notify(StateEventType.SATELLITE_CHANGE, state);
        }

        return true;
    }

    /**
     * NORAD ID активного спутника.
     * @returns {?number}
     */
    getActiveSatelliteId() {
        return this._activeSatelliteId;
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
        if (deleted && this._activeSatelliteId === noradId) {
            this._activeSatelliteId = null;
        }
        return deleted;
    }

    /**
     * Полная очистка хранилища и сброс активного спутника.
     */
    clear() {
        this._satellites.clear();
        this._activeSatelliteId = null;
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
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { SatelliteStateManager, SatelliteState, StateEventType };
}

// Экспорт для использования в браузере.
if (typeof window !== 'undefined') {
    window.SatelliteStateManager = SatelliteStateManager;
    window.SatelliteState = SatelliteState;
    window.StateEventType = StateEventType;
}
