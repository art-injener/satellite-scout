// Компактная таблица пролётов в правой панели + кнопки управления.
// Данные приходят из SSE-события satellite_group_update,
// не из polling GET /api/passes.
// Таблица: 3 колонки — [КА+NORAD] | [AOS+LOS] | [Длит./Обратный отсчёт].

(function() {
    'use strict';

    function RightPanelTable() {
        this._tbody = document.getElementById('passes-compact-body');
        this._group = null;
        this._selectedNoradId = null;  // Выбранный в таблице спутник
        this._trackingNoradId = null;  // На сопровождении (red/green).
        this._countdownTimer = null;

        this._autoCheckbox = document.getElementById('rp-auto');
        this._trackBtn = document.getElementById('rp-track');
        this._resetBtn = document.getElementById('rp-reset');

        this._bindControls();
    }

    RightPanelTable.prototype.init = function() {
        var self = this;

        this._countdownTimer = setInterval(function() { self._tickCountdowns(); }, 1000);

        if (window._stateManager) {
            var sm = window._stateManager;
            var group = sm.getSatelliteGroup();
            if (group) { this._onGroupUpdate(group); }

            if (window.StateEventType) {
                sm.subscribe(window.StateEventType.SATELLITE_GROUP_UPDATE, function(data) {
                    self._onGroupUpdate(data);
                });
                sm.subscribe(window.StateEventType.SELECTED_CHANGE, function(state) {
                    self._selectedNoradId = state ? state.noradId : null;
                    self._render();
                    self._updateControls();
                });
                sm.subscribe(window.StateEventType.TRACKING_CHANGE, function(state) {
                    self._trackingNoradId = state ? state.noradId : null;
                    self._render();
                    self._updateControls();
                });
            }
        }

        this._updateControls();
    };

    // ── Обработка группы из SSE ──

    RightPanelTable.prototype._onGroupUpdate = function(data) {
        this._group = data;
        // Синхронизация selected из StateManager.
        if (window._stateManager) {
            this._selectedNoradId = window._stateManager.getSelectedSatelliteId();
            this._trackingNoradId = window._stateManager.getTrackingSatelliteId();
        }
        this._render();
        this._updateControls();
    };

    // ── Рендер ──

    RightPanelTable.prototype._render = function() {
        if (!this._tbody) { return; }

        var satellites = (this._group && this._group.satellites) ? this._group.satellites : [];

        if (satellites.length === 0) {
            this._tbody.innerHTML = '<tr><td colspan="3" class="pc-empty">Нет пролётов</td></tr>';
            return;
        }

        var now = Date.now();
        var html = '';

        for (var i = 0; i < satellites.length; i++) {
            var sat = satellites[i];
            var isTracking = (sat.norad_id === this._trackingNoradId);
            var isSelected = (sat.norad_id === this._selectedNoradId);

            var cls = 'pc-row';
            if (isTracking) { cls += ' pc-row--tracking'; }
            if (isSelected) { cls += ' pc-row--selected'; }

            var name = this._escapeHtml(sat.sat_name || String(sat.norad_id));
            var norad = String(sat.norad_id);

            var aosStr = this._fmtTime(sat.aos);
            var losStr = this._fmtTime(sat.los);

            // Колонка 3: обратный отсчёт до AOS или оставшееся время сеанса.
            var col3 = this._fmtCol3(sat.aos, sat.los, sat.duration, now);

            html += '<tr class="' + cls + '" data-norad="' + sat.norad_id + '"' +
                ' data-aos="' + sat.aos + '" data-los="' + sat.los + '" data-dur="' + sat.duration + '">' +
                // Колонка 1: имя + NORAD (2 строки)
                '<td class="pc-name-cell">' +
                    '<div class="pc-sat-name">' + name + '</div>' +
                    '<div class="pc-sat-norad">' + norad + '</div>' +
                '</td>' +
                // Колонка 2: AOS + LOS (2 строки)
                '<td class="pc-time-cell">' +
                    '<div class="pc-time-aos">' + aosStr + '</div>' +
                    '<div class="pc-time-los">' + losStr + '</div>' +
                '</td>' +
                // Колонка 3: длительность или обратный отсчёт
                '<td class="pc-col3-cell">' + col3 + '</td>' +
                '</tr>';
        }

        this._tbody.innerHTML = html;
        this._bindRowEvents();
    };

    // ── Тикер обратного отсчёта ──

    RightPanelTable.prototype._tickCountdowns = function() {
        if (!this._tbody) { return; }
        var now = Date.now();
        var rows = this._tbody.querySelectorAll('.pc-row');
        for (var i = 0; i < rows.length; i++) {
            var row = rows[i];
            var aos = parseInt(row.getAttribute('data-aos'), 10);
            var los = parseInt(row.getAttribute('data-los'), 10);
            var dur = parseFloat(row.getAttribute('data-dur')) || 0;
            var cell = row.querySelector('.pc-col3-cell');
            if (cell) {
                cell.innerHTML = this._fmtCol3(aos, los, dur, now);
            }
        }
    };

    // ── Форматирование колонки 3 ──

    // Логика:
    //   - now < AOS: обратный отсчёт до начала сеанса (T−)
    //   - AOS ≤ now ≤ LOS: оставшееся время сеанса (убывает)
    RightPanelTable.prototype._fmtCol3 = function(aos, los, duration, now) {
        var nowMs = now || Date.now();
        if (nowMs < aos) {
            // До начала сеанса — T−
            return this._fmtCountdown(aos - nowMs);
        }
        if (nowMs <= los) {
            // Сеанс идёт — оставшееся время
            var remaining = los - nowMs;
            return '<span class="pc-countdown pc-countdown--now">' +
                this._fmtDurationMs(remaining) + '</span>';
        }
        // Пролёт завершён (не должно попасть в таблицу, но на всякий случай)
        return '<span class="pc-countdown pc-countdown--done">—</span>';
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
        // Устанавливаем selected через StateManager (per-tab, без API-вызова).
        if (window._stateManager) {
            var satInfo = this._findSatInGroup(noradId);
            window._stateManager.setSelectedSatellite(noradId, satInfo ? satInfo.sat_name : '', true);
        }
        this._render();
        this._updateControls();
    };

    RightPanelTable.prototype._onRowDblClick = function(noradId) {
        this._setManualTracking(noradId);
    };

    RightPanelTable.prototype._findSatInGroup = function(noradId) {
        if (!this._group || !this._group.satellites) { return null; }
        for (var i = 0; i < this._group.satellites.length; i++) {
            if (this._group.satellites[i].norad_id === noradId) { return this._group.satellites[i]; }
        }
        return null;
    };

    // ── Кнопки управления ──

    RightPanelTable.prototype._bindControls = function() {
        var self = this;

        // Чекбокс «Авто» — пока деактивирован.
        if (this._autoCheckbox) {
            this._autoCheckbox.disabled = true;
            /* TODO: логика авто-режима (закомментирована)
            this._autoCheckbox.addEventListener('change', function() {
                if (self._autoCheckbox.checked) {
                    self._resetTracking();
                }
            });
            */
        }
        if (this._trackBtn) {
            this._trackBtn.addEventListener('click', function() {
                if (self._selectedNoradId) {
                    self._setManualTracking(self._selectedNoradId);
                }
            });
        }
        if (this._resetBtn) {
            this._resetBtn.addEventListener('click', function() {
                self._resetTracking();
            });
        }
    };

    RightPanelTable.prototype._setManualTracking = function(noradId) {
        // Кнопка «Сопровождение» → API → бэкенд подтвердит через SSE.
        fetch('/api/tracking/current', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ norad_id: noradId })
        }).then(function(r) {
            if (!r.ok) { console.error('[RightPanel] tracking/current error:', r.status); }
        }).catch(function(err) {
            console.error('[RightPanel] tracking/current fetch error:', err);
        });
    };

    RightPanelTable.prototype._resetTracking = function() {
        // Сброс сопровождения → API → бэкенд подтвердит через SSE.
        fetch('/api/tracking/reset', { method: 'POST' })
            .catch(function(err) {
                console.error('[RightPanel] tracking/reset fetch error:', err);
            });
    };

    RightPanelTable.prototype._updateControls = function() {
        if (this._trackBtn) {
            // «Сопровождение» активна если есть выбранный спутник.
            this._trackBtn.disabled = !this._selectedNoradId;
        }
        if (this._resetBtn) {
            // «Сброс» активна если есть спутник на сопровождении.
            this._resetBtn.disabled = !this._trackingNoradId;
        }
    };

    // ── Форматирование ──

    // Время в UTC: ЧЧ:ММ:СС
    RightPanelTable.prototype._fmtTime = function(ms) {
        if (!ms) { return '--:--:--'; }
        var d = new Date(ms);
        var hh = d.getUTCHours();
        var mm = d.getUTCMinutes();
        var ss = d.getUTCSeconds();
        return (hh < 10 ? '0' : '') + hh + ':' +
               (mm < 10 ? '0' : '') + mm + ':' +
               (ss < 10 ? '0' : '') + ss;
    };

    // Длительность в миллисекундах → "Мм СС" или "Хч Мм"
    RightPanelTable.prototype._fmtDurationMs = function(ms) {
        if (!ms || ms < 0) { return '0с'; }
        var totalSec = Math.floor(ms / 1000);
        var h = Math.floor(totalSec / 3600);
        var m = Math.floor((totalSec % 3600) / 60);
        var s = totalSec % 60;
        if (h > 0) { return h + 'ч ' + m + 'м'; }
        if (m > 0) { return m + 'м ' + (s < 10 ? '0' : '') + s + 'с'; }
        return s + 'с';
    };

    // Обратный отсчёт до AOS.
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
            return '<span class="' + cls + '">T− ' + h + 'ч ' + m + 'м</span>';
        }
        if (m > 0) {
            return '<span class="' + cls + '">T− ' + m + 'м ' + (s < 10 ? '0' : '') + s + 'с</span>';
        }
        return '<span class="' + cls + '">T− ' + s + 'с</span>';
    };

    RightPanelTable.prototype._escapeHtml = function(str) {
        var div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    };

    RightPanelTable.prototype.destroy = function() {
        if (this._countdownTimer) { clearInterval(this._countdownTimer); }
    };

    window.RightPanelTable = RightPanelTable;

})();
