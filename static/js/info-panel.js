/**
 * InfoPanel — управление вкладками спутников и кнопкой «Сопровождение».
 * Пока без привязки к данным (SSE), только визуальная логика.
 */
(function() {
    'use strict';

    function InfoPanel(containerEl) {
        this.container = containerEl;
        this.tabsContainer = containerEl.querySelector('.info-panel__tabs');
        this.trackBtn = containerEl.querySelector('#btn-track');
        this.activeNorad = null;

        this._init();
    }

    InfoPanel.prototype._init = function() {
        if (!this.tabsContainer) return;

        this.tabsContainer.addEventListener('click', function(e) {
            var tab = e.target.closest('.info-panel__tab');
            if (tab) {
                this._activateTab(tab);
                return;
            }
            if (e.target.closest('#btn-track')) {
                this._onTrackClick();
            }
        }.bind(this));

        var activeTab = this.tabsContainer.querySelector('.info-panel__tab.active');
        if (activeTab) {
            this.activeNorad = activeTab.getAttribute('data-norad');
        }
    };

    InfoPanel.prototype._activateTab = function(tab) {
        var tabs = this.tabsContainer.querySelectorAll('.info-panel__tab');
        for (var i = 0; i < tabs.length; i++) {
            tabs[i].classList.remove('active');
        }
        tab.classList.add('active');
        this.activeNorad = tab.getAttribute('data-norad');
        // TODO: обновить контент секций данными выбранного спутника
    };

    InfoPanel.prototype._onTrackClick = function() {
        if (!this.activeNorad) return;
        console.log('[InfoPanel] Сопровождение:', this.activeNorad);
        // TODO: POST /api/tracking/current → смена primary спутника
    };

    /** Обновить данные спутника по NORAD ID */
    InfoPanel.prototype.updateSatelliteData = function(noradId, data) {
        // TODO: заполнить секции данными если noradId === activeNorad
    };

    /** Добавить вкладку спутника */
    InfoPanel.prototype.addTab = function(noradId) {
        if (this.tabsContainer.querySelector('[data-norad="' + noradId + '"]')) return;

        var btn = document.createElement('button');
        btn.className = 'info-panel__tab';
        btn.setAttribute('data-norad', noradId);
        btn.textContent = noradId;

        var spacer = this.tabsContainer.querySelector('.info-panel__tabs-spacer');
        if (spacer) {
            this.tabsContainer.insertBefore(btn, spacer);
        }
    };

    /** Удалить вкладку спутника */
    InfoPanel.prototype.removeTab = function(noradId) {
        var tab = this.tabsContainer.querySelector('[data-norad="' + noradId + '"]');
        if (tab) {
            var wasActive = tab.classList.contains('active');
            tab.remove();
            if (wasActive) {
                var first = this.tabsContainer.querySelector('.info-panel__tab');
                if (first) this._activateTab(first);
            }
        }
    };

    window.InfoPanel = InfoPanel;
})();
