/**
 * InfoPanel — отображение данных спутника в 5 карточках.
 *
 * Подписывается на StateManager (POSITION, SATELLITE_CHANGE),
 * загружает данные пролёта из /api/passes при смене спутника.
 * Управляет кнопкой наблюдения и модальным окном подтверждения.
 */
(function() {
    'use strict';

    const MODAL_ID = 'track-end-session-modal';

    /**
     * @param {HTMLElement} containerEl — контейнер info-panel.
     * @param {SatelliteStateManager} stateManager — менеджер состояния спутников.
     */
    function InfoPanel(containerEl, stateManager) {
        this.container = containerEl;
        this.trackBtn = containerEl.querySelector('#btn-track');
        this.modal = document.getElementById(MODAL_ID);
        this._stateManager = stateManager || null;

        // Кэш элементов DOM по id (ip-norad, ip-name и т.д.)
        this._els = {};
        const ids = [
            'ip-norad', 'ip-name', 'ip-group',
            'ip-lon', 'ip-lat', 'ip-az', 'ip-el', 'ip-alt',
            'ip-orbit', 'ip-period', 'ip-incl', 'ip-status',
            'ip-aos', 'ip-tca', 'ip-los', 'ip-dur',
            'ip-tmi', 'ip-uplink', 'ip-downlink', 'ip-mod'
        ];
        for (let i = 0; i < ids.length; i++) {
            this._els[ids[i]] = document.getElementById(ids[i]);
        }

        this._activeNoradId = null;
        this._currentPass = null;

        this._bindEvents();
        this._subscribeToState();
        this._initFromCurrentState();
    }

    // ── Вспомогательные методы ────────────────────────────────

    InfoPanel.prototype._setEl = function(id, text) {
        const el = this._els[id];
        if (el) {el.textContent = text;}
    };

    /**
     * Форматирование Unix ms → "HH:MM:SS UTC".
     */
    InfoPanel.prototype._fmtTime = function(ms) {
        if (!ms) {return '--:--:--';}
        const d = new Date(ms);
        const hh = d.getUTCHours();
        const mm = d.getUTCMinutes();
        const ss = d.getUTCSeconds();
        return (hh < 10 ? '0' : '') + hh + ':' +
               (mm < 10 ? '0' : '') + mm + ':' +
               (ss < 10 ? '0' : '') + ss + ' UTC';
    };

    /**
     * Форматирование длительности (секунды) → "Xm Ys".
     */
    InfoPanel.prototype._fmtDuration = function(sec) {
        if (!sec || sec <= 0) {return '--:--';}
        const m = Math.floor(sec / 60);
        const s = Math.round(sec % 60);
        return m + 'm ' + (s < 10 ? '0' : '') + s + 's';
    };

    /**
     * Форматирование орбитального периода (минуты) → "Xh Ym" или "X.Y min".
     */
    InfoPanel.prototype._fmtPeriod = function(minutes) {
        if (typeof minutes !== 'number') {return '---';}
        if (minutes >= 60) {
            const h = Math.floor(minutes / 60);
            const m = Math.round(minutes % 60);
            return h + 'h ' + m + 'm';
        }
        return minutes.toFixed(1) + ' min';
    };

    // ── Привязка UI-событий ───────────────────────────────────

    InfoPanel.prototype._bindEvents = function() {
        if (this.trackBtn) {
            this.trackBtn.addEventListener('click', this._onTrackClick.bind(this));
        }
        if (!this.modal) {return;}

        const backdrop = this.modal.querySelector('#track-end-session-backdrop');
        const btnNo = document.getElementById('track-end-session-no');
        const btnYes = document.getElementById('track-end-session-yes');

        if (backdrop) {backdrop.addEventListener('click', this._closeModal.bind(this));}
        if (btnNo) {btnNo.addEventListener('click', this._closeModal.bind(this));}
        if (btnYes) {btnYes.addEventListener('click', this._onConfirmEndSession.bind(this));}
    };

    InfoPanel.prototype._onTrackClick = function() {
        const noradText = this._els['ip-norad'] ? this._els['ip-norad'].textContent.trim() : '';
        if (!noradText || noradText === '---' || noradText === '--') {return;}
        this._openModal();
    };

    InfoPanel.prototype._openModal = function() {
        if (this.modal) {this.modal.classList.remove('modal--hidden');}
    };

    InfoPanel.prototype._closeModal = function() {
        if (this.modal) {this.modal.classList.add('modal--hidden');}
    };

    InfoPanel.prototype._onConfirmEndSession = function() {
        this._closeModal();
        // TODO: завершение сеанса, POST /api/...
    };

    // ── Подписки на StateManager ──────────────────────────────

    InfoPanel.prototype._subscribeToState = function() {
        if (!this._stateManager) {return;}

        const self = this;
        const SE = window.StateEventType;

        // Позиция обновляется для selected, но overlay показывает tracking.
        // Поэтому InfoPanel читает позицию tracking-спутника из кеша.
        this._stateManager.subscribe(SE.POSITION, function() {
            const trackId = self._stateManager.getTrackingSatelliteId();
            if (!trackId) { return; }
            const trkState = self._stateManager.getState(trackId);
            if (trkState) { self._updateFromPosition(trkState); }
        });

        // При смене наблюдения — обновляем данные.
        this._stateManager.subscribe(SE.TRACKING_CHANGE, function(state) {
            if (state && state.noradId) {
                self._onSatelliteChange(state);
            } else {
                self._clearAll();
            }
        });
    };

    /**
     * Подтягивание данных, уже имеющихся в StateManager на момент создания InfoPanel.
     */
    InfoPanel.prototype._initFromCurrentState = function() {
        if (!this._stateManager) {return;}
        // Инициализация из tracking (если уже есть).
        const trackId = this._stateManager.getTrackingSatelliteId();
        if (!trackId) { return; }
        const state = this._stateManager.getState(trackId);
        if (!state) { return; }

        if (state.position) {
            this._updateFromPosition(state);
        }
        if (state.noradId) {
            this._onSatelliteChange(state);
        }
    };

    InfoPanel.prototype._clearAll = function() {
        this._activeNoradId = null;
        this._currentPass = null;
        for (const id in this._els) {
            if (this._els[id]) { this._els[id].textContent = '---'; }
        }
    };

    // ── Обработчики событий StateManager ──────────────────────

    /**
     * Обновление полей из позиции (1 Гц).
     */
    InfoPanel.prototype._updateFromPosition = function(state) {
        const pos = state.position;
        if (!pos) {return;}

        // Столбик «Спутник»
        this._setEl('ip-norad', state.noradId || '---');
        this._setEl('ip-name', state.name || '---');

        // Столбик «Геоданные»
        const latDir = pos.lat >= 0 ? 'N' : 'S';
        const lonDir = pos.lon >= 0 ? 'E' : 'W';
        this._setEl('ip-lat', Math.abs(pos.lat).toFixed(2) + '°' + latDir);
        this._setEl('ip-lon', Math.abs(pos.lon).toFixed(2) + '°' + lonDir);
        this._setEl('ip-az', pos.az.toFixed(1) + '°');
        this._setEl('ip-el', pos.el.toFixed(1) + '°');
        this._setEl('ip-alt', (pos.alt || 0).toFixed(0) + ' km');

        // Статус видимости
        if (typeof pos.el === 'number') {
            this._setEl('ip-status', pos.el > 0 ? 'VISIBLE' : 'BELOW HOR');
        }
    };

    /**
     * При смене спутника: обновить орбитальные параметры и загрузить данные пролёта.
     */
    InfoPanel.prototype._onSatelliteChange = function(state) {
        const noradId = state.noradId;

        // Орбитальные параметры из SSE satellite_change
        this._setEl('ip-incl', typeof state.inclination === 'number'
            ? state.inclination.toFixed(2) + '°' : '---°');
        this._setEl('ip-period', this._fmtPeriod(state.period));

        // Сбрасываем данные пролёта при смене спутника
        this._currentPass = null;
        this._resetPassFields();

        if (noradId && noradId !== this._activeNoradId) {
            this._activeNoradId = noradId;
            this._fetchPassData(noradId);
        }
    };

    /**
     * Сброс полей пролёта в дефолтные значения.
     */
    InfoPanel.prototype._resetPassFields = function() {
        this._setEl('ip-group', '---');
        this._setEl('ip-orbit', '#---');
        this._setEl('ip-aos', '--:--:--');
        this._setEl('ip-tca', '--:--:--');
        this._setEl('ip-los', '--:--:--');
        this._setEl('ip-dur', '--:--');
    };

    /**
     * Загрузка данных ближайшего пролёта для спутника.
     */
    InfoPanel.prototype._fetchPassData = function(noradId) {
        const self = this;

        fetch('/api/passes?hours=24')
            .then(function(resp) { return resp.json(); })
            .then(function(data) {
                if (!data.passes || data.passes.length === 0) {return;}
                if (self._activeNoradId !== noradId) {return;}

                const now = Date.now();
                let pass = null;

                for (let i = 0; i < data.passes.length; i++) {
                    const p = data.passes[i];
                    if (p.norad_id === noradId) {
                        if ((p.aos <= now && now <= p.los) || now < p.aos) {
                            pass = p;
                            break;
                        }
                    }
                }

                if (pass) {
                    self._currentPass = pass;
                    self._updatePassFields(pass);
                }
            })
            .catch(function(err) {
                console.error('[InfoPanel] Ошибка загрузки данных пролёта:', err);
            });
    };

    /**
     * Заполнение полей пролёта из данных pass.
     */
    InfoPanel.prototype._updatePassFields = function(pass) {
        this._setEl('ip-group', pass.group || '---');
        this._setEl('ip-orbit', '#' + (pass.orbit_number || '---'));
        this._setEl('ip-aos', this._fmtTime(pass.aos));
        this._setEl('ip-tca', this._fmtTime(pass.tca));
        this._setEl('ip-los', this._fmtTime(pass.los));
        this._setEl('ip-dur', this._fmtDuration(pass.duration));
    };

    window.InfoPanel = InfoPanel;
})();
