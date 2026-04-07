// sse-client.js — FE-002: SSE Client (SSE-004: групповые события)
// Единственный канал получения данных с backend через Server-Sent Events.
// Маршрутизирует SSE-события в SatelliteStateManager.

'use strict';

/**
 * Статусы соединения SSE Client.
 * @enum {string}
 */
const SSEConnectionStatus = Object.freeze({
    /** Соединение установлено и активно. */
    CONNECTED: 'connected',
    /** Соединение разорвано. */
    DISCONNECTED: 'disconnected',
    /** Идёт подключение или переподключение. */
    CONNECTING: 'connecting',
});

/**
 * SSEClient — обёртка над EventSource для получения данных с SSE Hub.
 *
 * Функции:
 * - Подключение к /api/sse через EventSource
 * - Маршрутизация событий (satellite_state_update, satellite_change) в StateManager
 * - Автопереподключение с exponential backoff
 * - Индикация статуса соединения через callback
 *
 * Использование:
 *   const client = new SSEClient(stateManager);
 *   client.onStatusChange((status) => updateUI(status));
 *   client.connect();
 */
class SSEClient {
    /**
     * @param {SatelliteStateManager} stateManager — менеджер состояния спутников.
     * @param {string} [url='/api/sse'] — URL SSE endpoint.
     */
    constructor(stateManager, url = '/api/sse') {
        if (!stateManager) {
            throw new Error('stateManager is required');
        }

        /** @type {SatelliteStateManager} */
        this._stateManager = stateManager;

        /** @type {string} */
        this._url = url;

        /** @type {?EventSource} */
        this._eventSource = null;

        /** @type {string} */
        this._status = SSEConnectionStatus.DISCONNECTED;

        /** @type {Set<Function>} Подписчики на смену статуса. */
        this._statusCallbacks = new Set();

        /** @type {?number} ID таймера переподключения. */
        this._reconnectTimer = null;

        /** @type {number} Текущая задержка переподключения (мс). */
        this._reconnectDelay = SSEClient.RECONNECT_MIN_DELAY;

        /** @type {boolean} Флаг ручного отключения (не переподключаться). */
        this._manualDisconnect = false;
    }

    // ── Константы reconnect ───────────────────────────────────

    /** Минимальная задержка переподключения (мс). */
    static get RECONNECT_MIN_DELAY() { return 1000; }

    /** Максимальная задержка переподключения (мс). */
    static get RECONNECT_MAX_DELAY() { return 30000; }

    /** Множитель задержки (exponential backoff). */
    static get RECONNECT_MULTIPLIER() { return 2; }

    // ── Подключение / Отключение ──────────────────────────────

    /**
     * Установить соединение с SSE Hub.
     * Если соединение уже активно — ничего не делает.
     */
    connect() {
        if (this._eventSource) {
            return;
        }

        this._manualDisconnect = false;
        this._createEventSource();
    }

    /**
     * Закрыть соединение и остановить автопереподключение.
     */
    disconnect() {
        this._manualDisconnect = true;
        this._clearReconnectTimer();
        this._closeEventSource();
        this._setStatus(SSEConnectionStatus.DISCONNECTED);
    }

    // ── Статус соединения ─────────────────────────────────────

    /**
     * Текущий статус соединения.
     * @returns {string} — одно из значений SSEConnectionStatus.
     */
    getStatus() {
        return this._status;
    }

    /**
     * Подписка на изменение статуса соединения.
     * @param {Function} callback — вызывается с объектом { status: string }.
     * @returns {boolean} true если подписка успешна.
     */
    onStatusChange(callback) {
        if (typeof callback !== 'function') {
            return false;
        }
        this._statusCallbacks.add(callback);
        return true;
    }

    /**
     * Отписка от изменений статуса.
     * @param {Function} callback — ранее зарегистрированный callback.
     * @returns {boolean} true если отписка успешна.
     */
    offStatusChange(callback) {
        return this._statusCallbacks.delete(callback);
    }

    // ── Внутренние методы ─────────────────────────────────────

    /**
     * Создание EventSource и подписка на события.
     * @private
     */
    _createEventSource() {
        this._setStatus(SSEConnectionStatus.CONNECTING);

        // Добавляем client_id в URL для per-client state (TRACK-STATE-003).
        let url = this._url;
        const clientId = getClientId();
        if (clientId) {
            const sep = url.indexOf('?') >= 0 ? '&' : '?';
            url = url + sep + 'client_id=' + encodeURIComponent(clientId);
        }

        try {
            this._eventSource = new EventSource(url);
        } catch (err) {
            console.error('[SSEClient] failed to create EventSource:', err);
            this._scheduleReconnect();
            return;
        }

        this._eventSource.addEventListener('connected', (e) => {
            this._onConnected(e);
        });

        this._eventSource.addEventListener('satellite_state_update', (e) => {
            this._handleEvent('satellite_state_update', e);
        });

        this._eventSource.addEventListener('satellite_change', (e) => {
            this._handleEvent('satellite_change', e);
        });

        this._eventSource.addEventListener('satellite_group_update', (e) => {
            this._handleEvent('satellite_group_update', e);
        });

        // Per-client восстановление слежения при подключении (TRACK-STATE-003).
        this._eventSource.addEventListener('client_state_restore', (e) => {
            this._handleEvent('client_state_restore', e);
        });

        // Обработка ошибок (потеря соединения и т.д.).
        this._eventSource.onerror = () => {
            this._onError();
        };
    }

    /**
     * Обработка успешного подключения (event: connected).
     * @param {MessageEvent} _e — событие EventSource.
     * @private
     */
    _onConnected() {
        this._reconnectDelay = SSEClient.RECONNECT_MIN_DELAY;
        this._setStatus(SSEConnectionStatus.CONNECTED);
        console.log('[SSEClient] connected to', this._url);
    }

    /**
     * Обработка ошибки EventSource.
     * @private
     */
    _onError() {
        // EventSource.CLOSED === 2 — соединение потеряно без автовосстановления.
        if (this._eventSource && this._eventSource.readyState === EventSource.CLOSED) {
            console.warn('[SSEClient] connection lost');
            this._closeEventSource();
            this._setStatus(SSEConnectionStatus.DISCONNECTED);

            if (!this._manualDisconnect) {
                this._scheduleReconnect();
            }
        }
        // EventSource.CONNECTING === 0 — браузер пытается переподключиться сам.
        // В этом случае мы просто обновляем статус.
        else if (this._eventSource && this._eventSource.readyState === EventSource.CONNECTING) {
            this._setStatus(SSEConnectionStatus.CONNECTING);
        }
    }

    /**
     * Маршрутизация SSE-события в StateManager.
     * @param {string} eventType — тип события.
     * @param {MessageEvent} event — событие EventSource.
     * @private
     */
    _handleEvent(eventType, event) {
        let data;
        try {
            data = JSON.parse(event.data);
        } catch (err) {
            console.error(`[SSEClient] failed to parse '${eventType}' event:`, err);
            return;
        }

        switch (eventType) {
            case 'satellite_state_update':
                this._handleStateUpdate(data);
                break;
            case 'satellite_change':
                this._handleSatelliteChange(data);
                break;
            case 'satellite_group_update':
                this._stateManager.setSatelliteGroup(data);
                break;
            case 'client_state_restore':
                this._handleClientStateRestore(data);
                break;
            default:
                console.warn(`[SSEClient] unhandled event type: ${eventType}`);
        }
    }

    /**
     * Обработка satellite_change: маршрутизация по reason.
     *   - "manual"          → setTrackingSatellite (подтверждение бэкенда)
     *   - "tracking_ended"  → clearTrackingSatellite
     *   - "auto"/"initial"  → setSelectedSatellite (если нет ручного выбора в таблице)
     * @param {Object} data
     * @private
     */
    _handleSatelliteChange(data) {
        if (typeof data.norad_id !== 'number') { return; }

        const reason = data.reason || '';

        // satellite_change(manual) не обрабатываем — tracking устанавливается
        // через client_state_restore (TRACK-STATE-003: per-client).
        if (reason === 'manual') {
            return;
        }

        if (reason === 'tracking_ended') {
            this._stateManager.clearTrackingSatellite();
            // forceNotify: даже при совпадении NORAD обновить таблицу и сбросить слой selected/трек.
            // tracking_ended всегда сбрасывает ручной выбор — сеанс закончился, контекст неактуален.
            this._stateManager.setSelectedSatellite(data.norad_id, data.name || '', false, true);
        } else {
            // "auto", "initial" — обновляем selected (не tracking).
            // Если оператор сделал ручной выбор в таблице — не перезатираем его.
            // satellite_group_update уже корректно проверяет _manualTableSelection;
            // satellite_change не должен обходить эту защиту.
            if (this._stateManager.isManualTableSelection()) {
                return;
            }
            this._stateManager.setSelectedSatellite(data.norad_id, data.name || '', false, true);
        }
    }

    /**
     * Обработка per-client события client_state_restore (TRACK-STATE-003).
     * Восстанавливает tracking_id конкретного клиента при подключении или смене.
     * @param {Object} data — {tracking_id, ts}.
     * @private
     */
    _handleClientStateRestore(data) {
        const trackingId = (typeof data.tracking_id === 'number' && data.tracking_id > 0) ? data.tracking_id : null;
        const currentTracking = this._stateManager.getTrackingSatelliteId();

        if (trackingId !== currentTracking) {
            if (trackingId) {
                const state = this._stateManager.getState(trackingId);
                this._stateManager.setTrackingSatellite(trackingId, state ? state.name : '');
            } else {
                this._stateManager.clearTrackingSatellite();
            }
        }
    }

    /**
     * Обработка группового события satellite_state_update.
     * @param {Object} data
     * @private
     */
    _handleStateUpdate(data) {
        if (!data || !Array.isArray(data.positions)) {
            console.warn('[SSEClient] invalid satellite_state_update: missing positions array');
            return;
        }

        const ts = data.ts || Date.now();

        for (const pos of data.positions) {
            pos.ts = ts;
            this._stateManager.updatePosition(pos);
        }

        if (data.tracks_included && Array.isArray(data.tracks)) {
            // Сначала сохраняем ВСЕ треки в кеш (в т.ч. вторичных спутников).
            // updateTrack() внутри тоже стреляет TRACK для primary если изменился,
            // но порядок треков в Go map случайный — primary может прийти раньше вторичных.
            let anyChanged = false;
            for (const track of data.tracks) {
                if (this._stateManager.updateTrack(track)) {
                    anyChanged = true;
                }
            }
            // После обработки ВСЕХ треков принудительно обновляем вторичные спутники —
            // к этому моменту их треки гарантированно в кеше.
            if (anyChanged) {
                this._stateManager.forceTrackRefresh();
            }
        }
    }

    // ── Reconnect ─────────────────────────────────────────────

    /**
     * Планирование переподключения с exponential backoff.
     * @private
     */
    _scheduleReconnect() {
        if (this._manualDisconnect || this._reconnectTimer) {
            return;
        }

        const delay = this._reconnectDelay;
        console.log(`[SSEClient] reconnecting in ${delay}ms...`);

        this._reconnectTimer = setTimeout(() => {
            this._reconnectTimer = null;
            this._createEventSource();
        }, delay);

        // Exponential backoff: увеличиваем задержку для следующей попытки.
        this._reconnectDelay = Math.min(
            this._reconnectDelay * SSEClient.RECONNECT_MULTIPLIER,
            SSEClient.RECONNECT_MAX_DELAY
        );
    }

    /**
     * Отмена запланированного переподключения.
     * @private
     */
    _clearReconnectTimer() {
        if (this._reconnectTimer) {
            clearTimeout(this._reconnectTimer);
            this._reconnectTimer = null;
        }
        this._reconnectDelay = SSEClient.RECONNECT_MIN_DELAY;
    }

    // ── Вспомогательные ───────────────────────────────────────

    /**
     * Закрытие EventSource.
     * @private
     */
    _closeEventSource() {
        if (this._eventSource) {
            this._eventSource.close();
            this._eventSource = null;
        }
    }

    /**
     * Обновление статуса и уведомление подписчиков.
     * @param {string} newStatus — новый статус (SSEConnectionStatus).
     * @private
     */
    _setStatus(newStatus) {
        if (this._status === newStatus) {
            return;
        }

        this._status = newStatus;

        for (const callback of this._statusCallbacks) {
            try {
                callback({ status: newStatus });
            } catch (err) {
                console.error('[SSEClient] status callback error:', err);
            }
        }
    }
}

/**
 * Генерация/получение уникального client_id для per-client state (TRACK-STATE-003).
 * Используется sessionStorage (per-tab): каждая вкладка — отдельный клиент.
 * @returns {string}
 */
function getClientId() {
    const key = 'satellite-scout-client-id';
    if (typeof sessionStorage !== 'undefined') {
        const existing = sessionStorage.getItem(key);
        if (existing) { return existing; }
        const id = _generateUUID();
        sessionStorage.setItem(key, id);
        return id;
    }
    return _generateUUID();
}

function _generateUUID() {
    if (typeof crypto !== 'undefined' && crypto.randomUUID) {
        return crypto.randomUUID();
    }
    return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
        const r = Math.random() * 16 | 0;
        return (c === 'x' ? r : (r & 0x3 | 0x8)).toString(16);
    });
}

// Экспорт для использования в других модулях и тестах.
if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
    module.exports = { SSEClient, SSEConnectionStatus, getClientId }; // eslint-disable-line no-undef
}

// Экспорт для использования в браузере.
if (typeof window !== 'undefined') {
    window.SSEClient = SSEClient;
    window.SSEConnectionStatus = SSEConnectionStatus;
    window.getClientId = getClientId;
}
