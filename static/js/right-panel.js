// Компактная таблица пролётов в правой панели + кнопки управления.
// Загружает данные из GET /api/passes, отображает имя, AOS, обратный отсчёт.

(function() {
    'use strict';

    var API_URL = '/api/passes';
    var REFRESH_MS = 60000;

    function RightPanelTable() {
        this._tbody = document.getElementById('passes-compact-body');
        this._passes = [];
        this._selectedNoradId = null;
        this._currentNoradId = null;
        this._countdownTimer = null;
        this._refreshTimer = null;

        // Кнопки
        this._autoCheckbox = document.getElementById('rp-auto');
        this._trackBtn = document.getElementById('rp-track');
        this._resetBtn = document.getElementById('rp-reset');

        this._bindControls();
    }

    RightPanelTable.prototype.init = function() {
        var self = this;
        this._loadPasses();
        this._countdownTimer = setInterval(function() { self._tickCountdowns(); }, 1000);
        this._refreshTimer = setInterval(function() { self._loadPasses(); }, REFRESH_MS);

        if (window._stateManager) {
            var activeState = window._stateManager.getActiveState();
            if (activeState && activeState.noradId) {
                this._currentNoradId = activeState.noradId;
            }
            if (window.StateEventType) {
                window._stateManager.subscribe(window.StateEventType.SATELLITE_CHANGE, function(state) {
                    self._currentNoradId = state.noradId || null;
                    self._render();
                    self._updateControls();
                });
            }
        }

        this._updateControls();
    };

    // ── Загрузка ──

    RightPanelTable.prototype._loadPasses = function() {
        var self = this;
        fetch(API_URL)
            .then(function(r) { return r.json(); })
            .then(function(data) {
                self._passes = data.passes || [];
                self._render();
            })
            .catch(function(err) {
                console.error('[RightPanelTable] Ошибка загрузки:', err);
            });
    };

    // ── Рендер ──

    RightPanelTable.prototype._render = function() {
        if (!this._tbody) { return; }

        if (this._passes.length === 0) {
            this._tbody.innerHTML = '<tr><td colspan="4" class="pc-empty">Нет пролётов</td></tr>';
            return;
        }

        var now = Date.now();
        var html = '';

        for (var i = 0; i < this._passes.length; i++) {
            var p = this._passes[i];
            var isCurrent = (p.norad_id === this._currentNoradId);
            var isSelected = (p.norad_id === this._selectedNoradId);

            var cls = 'pc-row';
            if (isCurrent) { cls += ' pc-row--active'; }
            if (isSelected) { cls += ' pc-row--selected'; }

            var aosStr = this._fmtTimeShort(p.aos);
            var durationStr = this._fmtDuration(p.duration);
            var countdown = this._fmtCountdown(p.aos - now);

            var name = this._escapeHtml(p.sat_name || String(p.norad_id));
            if (name.length > 16) { name = name.substring(0, 15) + '…'; }

            html += '<tr class="' + cls + '" data-norad="' + p.norad_id + '">' +
                '<td class="pc-name">' + name + '</td>' +
                '<td>' + aosStr + '</td>' +
                '<td class="pc-duration">' + durationStr + '</td>' +
                '<td class="pc-countdown-cell" data-aos="' + p.aos + '">' + countdown + '</td>' +
                '</tr>';
        }

        this._tbody.innerHTML = html;
        this._bindRowEvents();
    };

    // ── Привязка кликов по строкам ──

    RightPanelTable.prototype._bindRowEvents = function() {
        var self = this;
        var rows = this._tbody.querySelectorAll('.pc-row');
        for (var i = 0; i < rows.length; i++) {
            (function(row) {
                row.addEventListener('click', function() {
                    var id = parseInt(row.getAttribute('data-norad'), 10);
                    self._onRowClick(id);
                });
                row.addEventListener('dblclick', function() {
                    var id = parseInt(row.getAttribute('data-norad'), 10);
                    self._onRowDblClick(id);
                });
            })(rows[i]);
        }
    };

    RightPanelTable.prototype._onRowClick = function(noradId) {
        this._selectedNoradId = noradId;
        this._render();
        this._updateControls();
    };

    RightPanelTable.prototype._onRowDblClick = function(noradId) {
        console.log('[RightPanelTable] TODO: POST /api/tracking/current', noradId);
    };

    // ── Кнопки управления ──

    RightPanelTable.prototype._bindControls = function() {
        var self = this;

        if (this._autoCheckbox) {
            this._autoCheckbox.addEventListener('change', function() {
                console.log('[RightPanelTable] TODO: auto-track toggle', self._autoCheckbox.checked);
            });
        }
        if (this._trackBtn) {
            this._trackBtn.addEventListener('click', function() {
                if (self._selectedNoradId) {
                    console.log('[RightPanelTable] TODO: POST /api/tracking/current', self._selectedNoradId);
                }
            });
        }
        if (this._resetBtn) {
            this._resetBtn.addEventListener('click', function() {
                console.log('[RightPanelTable] TODO: POST /api/tracking/reset');
            });
        }
    };

    RightPanelTable.prototype._updateControls = function() {
        if (this._trackBtn) {
            this._trackBtn.disabled = !this._selectedNoradId;
        }
        if (this._resetBtn) {
            this._resetBtn.disabled = !this._currentNoradId;
        }
    };

    // ── Обратный отсчёт ──

    RightPanelTable.prototype._tickCountdowns = function() {
        if (!this._tbody) { return; }
        var now = Date.now();
        var cells = this._tbody.querySelectorAll('.pc-countdown-cell');
        for (var i = 0; i < cells.length; i++) {
            var aos = parseInt(cells[i].getAttribute('data-aos'), 10);
            cells[i].innerHTML = this._fmtCountdown(aos - now);
        }
    };

    // ── Форматирование ──

    RightPanelTable.prototype._fmtTimeShort = function(ms) {
        if (!ms) { return '--:--'; }
        var d = new Date(ms);
        var hh = d.getUTCHours();
        var mm = d.getUTCMinutes();
        return (hh < 10 ? '0' : '') + hh + ':' + (mm < 10 ? '0' : '') + mm;
    };

    // Длительность пролёта в секундах → "М:СС"
    RightPanelTable.prototype._fmtDuration = function(seconds) {
        if (seconds == null || isNaN(seconds)) { return '--:--'; }
        var s = Math.floor(Number(seconds));
        var m = Math.floor(s / 60);
        s = s % 60;
        return m + ':' + (s < 10 ? '0' : '') + s;
    };

    RightPanelTable.prototype._fmtCountdown = function(ms) {
        if (ms <= 0) {
            return '<span class="pc-countdown pc-countdown--now">СЕЙЧАС</span>';
        }
        var totalSec = Math.floor(ms / 1000);
        var h = Math.floor(totalSec / 3600);
        var m = Math.floor((totalSec % 3600) / 60);
        var s = totalSec % 60;

        var cls = 'pc-countdown';
        if (totalSec < 300) { cls += ' pc-countdown--soon'; }

        if (h > 0) {
            return '<span class="' + cls + '">' + h + 'ч ' + m + 'м</span>';
        }
        if (m > 0) {
            return '<span class="' + cls + '">' + m + 'м ' + s + 'с</span>';
        }
        return '<span class="' + cls + '">' + s + 'с</span>';
    };

    RightPanelTable.prototype._escapeHtml = function(str) {
        var div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    };

    RightPanelTable.prototype.destroy = function() {
        if (this._countdownTimer) { clearInterval(this._countdownTimer); }
        if (this._refreshTimer) { clearInterval(this._refreshTimer); }
    };

    window.RightPanelTable = RightPanelTable;

})();
