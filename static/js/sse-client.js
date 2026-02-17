// sse-client.js — FE-002: SSE Client
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
 * - Маршрутизация событий (position, track, satellite_change) в StateManager
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

        try {
            this._eventSource = new EventSource(this._url);
        } catch (err) {
            console.error('[SSEClient] failed to create EventSource:', err);
            this._scheduleReconnect();
            return;
        }

        // Приветственное событие от SSE Hub — соединение установлено.
        this._eventSource.addEventListener('connected', (e) => {
            this._onConnected(e);
        });

        // Бизнес-события: маршрутизация в StateManager.
        this._eventSource.addEventListener('position', (e) => {
            this._handleEvent('position', e);
        });

        this._eventSource.addEventListener('track', (e) => {
            this._handleEvent('track', e);
        });

        this._eventSource.addEventListener('satellite_change', (e) => {
            this._handleEvent('satellite_change', e);
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
    _onConnected(_e) {
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
        case 'position':
            this._stateManager.updatePosition(data);
            break;
        case 'track':
            this._stateManager.updateTrack(data);
            break;
        case 'satellite_change':
            if (typeof data.norad_id === 'number') {
                var orbitalParams = null;
                if (typeof data.inclination === 'number' || typeof data.period === 'number') {
                    orbitalParams = {
                        inclination: data.inclination,
                        period: data.period
                    };
                }
                this._stateManager.setActiveSatellite(data.norad_id, data.name || '', orbitalParams);
            }
            break;
        default:
            console.warn(`[SSEClient] unhandled event type: ${eventType}`);
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

// Экспорт для использования в других модулях и тестах.
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { SSEClient, SSEConnectionStatus };
}

// Экспорт для использования в браузере.
if (typeof window !== 'undefined') {
    window.SSEClient = SSEClient;
    window.SSEConnectionStatus = SSEConnectionStatus;
}
