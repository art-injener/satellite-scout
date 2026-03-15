// Overlay-панель: управление видимостью (show/hide).
// Обновление данных (#ip-*) выполняет InfoPanel через StateManager.

(function() {
    'use strict';

    /**
     * @param {HTMLElement} el — элемент overlay-панели (#sat-overlay-panel)
     * @param {SatelliteStateManager} stateManager
     */
    function OverlayPanel(el, stateManager) {
        this._el = el;
        this._container = el && el.parentElement; // .earth-view-container
        // Флаг: пользователь вручную закрыл панель (не показывать до смены КА)
        this._manuallyHidden = false;

        this._initClose();
        this._initRestore();

        if (stateManager) {
            this._subscribeToState(stateManager);
        }
    }

    OverlayPanel.prototype._setClosedState = function(closed) {
        this._manuallyHidden = !!closed;
        if (closed) {
            this._el.classList.remove('overlay-panel--visible');
            if (this._container) {
                this._container.classList.add('overlay-manually-closed');
            }
        } else {
            this._el.classList.add('overlay-panel--visible');
            if (this._container) {
                this._container.classList.remove('overlay-manually-closed');
            }
        }
    };

    // Привязка кнопки «×»
    OverlayPanel.prototype._initClose = function() {
        var self = this;
        var closeBtn = document.getElementById('overlay-panel-close');
        if (closeBtn) {
            closeBtn.addEventListener('click', function() {
                self._setClosedState(true);
            });
        }
    };

    // Кнопка «КА» — восстановить overlay
    OverlayPanel.prototype._initRestore = function() {
        var self = this;
        var restoreBtn = document.getElementById('overlay-panel-restore');
        if (restoreBtn) {
            restoreBtn.addEventListener('click', function() {
                self._setClosedState(false);
            });
        }
    };

    // Подписка на смену спутника — показывать панель при смене КА
    OverlayPanel.prototype._subscribeToState = function(sm) {
        var self = this;
        var SE = window.StateEventType;
        if (!SE) { return; }

        sm.subscribe(SE.SATELLITE_CHANGE, function() {
            self._manuallyHidden = false;
            self.show();
        });
    };

    OverlayPanel.prototype.show = function() {
        this._manuallyHidden = false;
        if (this._container) {
            this._container.classList.remove('overlay-manually-closed');
        }
        this._el.classList.add('overlay-panel--visible');
    };

    OverlayPanel.prototype.hide = function() {
        this._el.classList.remove('overlay-panel--visible');
    };

    window.OverlayPanel = OverlayPanel;

})();
