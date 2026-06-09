// mode-manager.js — менеджер режимов работы и выбранного радиотракта (ADR-004).
// Хранит текущий режим (overview/manual) и активный радиотракт,
// сохраняет выбор в localStorage и уведомляет подписчиков об изменениях.
// Не делает HTTP-запросов сам — получает начальное состояние конструктором.

'use strict';

/** Возможные значения mainMode (ADR-004 §2.1). */
const ModeId = Object.freeze({
    OVERVIEW: 'overview',
    MANUAL: 'manual',
});

/** Возможные значения вычисляемого типа станции (бэкенд). */
const StationType = Object.freeze({
    BASIC: 'basic',
    OBSERVATION: 'observation',
    TRACKING: 'tracking',
    HYBRID: 'hybrid',
});

/** События ModeManager. */
const ModeEvent = Object.freeze({
    MODE_CHANGE: 'mode_change',
    RADIO_PATH_CHANGE: 'radio_path_change',
});

/** Ключи localStorage. */
const STORAGE_MODE = 'ux.mainMode';
const STORAGE_RADIO_PATH = 'ux.radioPath';

/**
 * Минимальный JSDoc-тип радиотракта в форме, приходящей с бэкенда
 * через GET /api/config (см. handlers.RadioPathInfo).
 * @typedef {{id: number, name: string, has_rotator: boolean}} RadioPathInfo
 */

/**
 * ModeManager — состояние режимов и выбранного радиотракта.
 *
 * Контракт:
 *  - В basic-конфигурации (нет радиотрактов) режим всегда null, селектор скрыт.
 *  - При наличии радиотрактов восстанавливает последний выбор из localStorage,
 *    если он валиден; иначе — первый тракт, режим OVERVIEW.
 *  - Имитация — свойство радиотракта, а не отдельный режим.
 */
class ModeManager {
    /**
     * @param {string} stationType — значение из StationType (basic/observation/...).
     * @param {RadioPathInfo[]} radioPaths — список доступных трактов.
     * @param {{storage?: Storage}} [opts] — для тестов: подменяем localStorage.
     */
    constructor(stationType, radioPaths, opts = {}) {
        this._stationType = stationType || StationType.BASIC;
        this._radioPaths = Array.isArray(radioPaths) ? radioPaths.slice() : [];
        this._storage = opts.storage || (typeof localStorage !== 'undefined' ? localStorage : null);
        this._listeners = { [ModeEvent.MODE_CHANGE]: [], [ModeEvent.RADIO_PATH_CHANGE]: [] };

        this._mode = null;
        this._radioPathId = null;

        if (!this.isBasic()) {
            this._radioPathId = this._restoreRadioPath();
            this._mode = this._restoreMode();
        }
    }

    /** @returns {string} тип станции (basic/observation/tracking/hybrid). */
    getStationType() {
        return this._stationType;
    }

    /** @returns {boolean} true, если станция без оборудования. */
    isBasic() {
        return this._stationType === StationType.BASIC;
    }

    /**
     * Какие режимы доступны при текущей конфигурации.
     * В basic — пустой массив (mode-bar скрыт).
     * @returns {string[]}
     */
    availableModes() {
        if (this.isBasic()) {
            return [];
        }
        return [ModeId.OVERVIEW, ModeId.MANUAL];
    }

    /** @returns {?string} текущий режим или null для basic. */
    getMode() {
        return this._mode;
    }

    /**
     * Установить режим работы. Игнорирует попытку выставить недоступный режим
     * (например, что-либо кроме null в basic-конфигурации).
     * @param {string} mode
     * @returns {boolean} true, если режим действительно сменился.
     */
    setMode(mode) {
        const available = this.availableModes();
        if (available.length === 0) {
            return false;
        }
        if (!available.includes(mode)) {
            return false;
        }
        if (this._mode === mode) {
            return false;
        }
        this._mode = mode;
        this._persist(STORAGE_MODE, mode);
        this._emit(ModeEvent.MODE_CHANGE, mode);
        return true;
    }

    /** @returns {RadioPathInfo[]} копия списка радиотрактов. */
    getRadioPaths() {
        return this._radioPaths.slice();
    }

    /** @returns {?RadioPathInfo} текущий выбранный тракт. */
    getRadioPath() {
        if (this._radioPathId == null) {
            return null;
        }
        return this._radioPaths.find((rp) => rp.id === this._radioPathId) || null;
    }

    /** @returns {?number} id текущего тракта. */
    getRadioPathId() {
        return this._radioPathId;
    }

    /**
     * Выбрать радиотракт по id.
     * @param {number} pathId
     * @returns {boolean} true, если выбор успешно изменён.
     */
    setRadioPath(pathId) {
        if (this.isBasic()) {
            return false;
        }
        const id = Number(pathId);
        if (!Number.isFinite(id)) {
            return false;
        }
        const rp = this._radioPaths.find((p) => p.id === id);
        if (!rp) {
            return false;
        }
        if (this._radioPathId === id) {
            return false;
        }
        this._radioPathId = id;
        this._persist(STORAGE_RADIO_PATH, String(id));
        this._emit(ModeEvent.RADIO_PATH_CHANGE, rp);
        return true;
    }

    /** @returns {boolean} есть ли у текущего тракта поворотная платформа. */
    hasRotator() {
        const rp = this.getRadioPath();
        return !!(rp && rp.has_rotator);
    }

    /**
     * Подписка на смену режима. callback(mode).
     * @param {(mode: string) => void} callback
     * @returns {() => void} функция отписки.
     */
    onModeChange(callback) {
        return this._subscribe(ModeEvent.MODE_CHANGE, callback);
    }

    /**
     * Подписка на смену радиотракта. callback(radioPath).
     * @param {(rp: RadioPathInfo) => void} callback
     * @returns {() => void} функция отписки.
     */
    onRadioPathChange(callback) {
        return this._subscribe(ModeEvent.RADIO_PATH_CHANGE, callback);
    }

    // ── внутреннее ─────────────────────────────────────────

    _subscribe(event, callback) {
        if (typeof callback !== 'function') {
            return () => {};
        }
        const arr = this._listeners[event];
        arr.push(callback);
        return () => {
            const idx = arr.indexOf(callback);
            if (idx >= 0) {
                arr.splice(idx, 1);
            }
        };
    }

    _emit(event, payload) {
        const arr = this._listeners[event];
        for (let i = 0; i < arr.length; i++) {
            try {
                arr[i](payload);
            } catch (err) {
                if (typeof console !== 'undefined' && console.error) {
                    console.error('ModeManager listener error:', err);
                }
            }
        }
    }

    _persist(key, value) {
        if (!this._storage) {
            return;
        }
        try {
            this._storage.setItem(key, value);
        } catch (err) {
            // localStorage может быть недоступен (приватный режим, превышение квоты) —
            // тихо игнорируем: состояние сохранится в памяти на время сессии.
        }
    }

    _readStorage(key) {
        if (!this._storage) {
            return null;
        }
        try {
            return this._storage.getItem(key);
        } catch (err) {
            return null;
        }
    }

    _restoreMode() {
        const stored = this._readStorage(STORAGE_MODE);
        if (stored && this.availableModes().includes(stored)) {
            return stored;
        }
        return ModeId.OVERVIEW;
    }

    _restoreRadioPath() {
        if (this._radioPaths.length === 0) {
            return null;
        }
        const stored = this._readStorage(STORAGE_RADIO_PATH);
        if (stored != null) {
            const id = Number(stored);
            if (Number.isFinite(id) && this._radioPaths.some((rp) => rp.id === id)) {
                return id;
            }
        }
        return this._radioPaths[0].id;
    }
}

if (typeof window !== 'undefined') {
    window.ModeManager = ModeManager;
    window.ModeId = ModeId;
    window.ModeEvent = ModeEvent;
    window.ModeStationType = StationType;
}

if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
    module.exports = { ModeManager, ModeId, ModeEvent, StationType }; // eslint-disable-line no-undef
}
