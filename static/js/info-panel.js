/**
 * InfoPanel — кнопка «Сопровождение» и модальное окно подтверждения.
 */
(function() {
    'use strict';

    var MODAL_ID = 'track-end-session-modal';

    function InfoPanel(containerEl) {
        this.container = containerEl;
        this.trackBtn = containerEl.querySelector('#btn-track');
        this.modal = document.getElementById(MODAL_ID);

        this._bindEvents();
    }

    InfoPanel.prototype._bindEvents = function() {
        if (this.trackBtn) {
            this.trackBtn.addEventListener('click', this._onTrackClick.bind(this));
        }
        if (!this.modal) return;

        var backdrop = this.modal.querySelector('#track-end-session-backdrop');
        var btnNo = document.getElementById('track-end-session-no');
        var btnYes = document.getElementById('track-end-session-yes');

        if (backdrop) backdrop.addEventListener('click', this._closeModal.bind(this));
        if (btnNo) btnNo.addEventListener('click', this._closeModal.bind(this));
        if (btnYes) btnYes.addEventListener('click', this._onConfirmEndSession.bind(this));
    };

    InfoPanel.prototype._onTrackClick = function() {
        var noradEl = document.getElementById('ip-norad');
        var noradId = noradEl ? noradEl.textContent.trim() : null;
        if (!noradId || noradId === '---' || noradId === '--') return;

        this._openModal();
    };

    InfoPanel.prototype._openModal = function() {
        if (this.modal) this.modal.classList.remove('modal--hidden');
    };

    InfoPanel.prototype._closeModal = function() {
        if (this.modal) this.modal.classList.add('modal--hidden');
    };

    /** Пока без обработчика — только закрытие модалки */
    InfoPanel.prototype._onConfirmEndSession = function() {
        this._closeModal();
        // TODO: завершение сеанса, POST /api/...
    };

    window.InfoPanel = InfoPanel;
})();
