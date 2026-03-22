// Компактная таблица пролётов в правой панели + кнопки управления.
// Данные приходят из SSE-события satellite_group_update,
// не из polling GET /api/passes.
// Колонки: [глаз — видимость трассы] | КА | Время | длит. / до сеанса.
// Логика «длит.» / «до сеанса» совпадает с internal/services/session_table_ui.go (FormatSessionTableColumns).

(function() {
    'use strict';

    // SVG «глаз»: цвет задаётся через currentColor в CSS (тёмная тема — светлая обводка).
    var EYE_VISIBLE_SVG =
        '<span class="pc-track-eye-svg-wrap" aria-hidden="true">' +
        '<svg class="pc-track-eye-svg" viewBox="0 0 24 24" width="22" height="22" focusable="false">' +
        '<path fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" ' +
        'd="M1 12s4.5-7 11-7 11 7 11 7-4.5 7-11 7-11-7-11-7z"/>' +
        '<circle cx="12" cy="12" r="3.25" fill="none" stroke="currentColor" stroke-width="2"/>' +
        '</svg></span>';

    var EYE_HIDDEN_SVG =
        '<span class="pc-track-eye-svg-wrap" aria-hidden="true">' +
        '<svg class="pc-track-eye-svg" viewBox="0 0 24 24" width="22" height="22" focusable="false">' +
        '<path fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" ' +
        'd="M1 12s4.5-7 11-7 11 7 11 7-4.5 7-11 7-11-7-11-7z"/>' +
        '<circle cx="12" cy="12" r="3.25" fill="none" stroke="currentColor" stroke-width="2"/>' +
        '<path fill="none" stroke="currentColor" stroke-width="2.25" stroke-linecap="round" d="M3.5 3.5l17 17"/>' +
        '</svg></span>';

    var _trackLimitToastListenerAdded = false;

    /** Всплывающее уведомление о лимите дополнительных трасс на карте/небе. */
    function showTrackLimitToast(maxCount) {
        var max = typeof maxCount === 'number' ? maxCount
            : (typeof window.MAX_VISIBLE_TRACKS === 'number' ? window.MAX_VISIBLE_TRACKS : 5);
        var existing = document.getElementById('pc-track-limit-toast');
        if (existing) { existing.remove(); }
        var el = document.createElement('div');
        el.id = 'pc-track-limit-toast';
        el.className = 'pc-limit-toast';
        el.setAttribute('role', 'alert');
        var p = document.createElement('p');
        p.className = 'pc-limit-toast__text';
        p.textContent = 'На карте и в небе одновременно можно показать не более ' + max +
            ' дополнительных трасс спутников. Отключите одну из трасс (иконка глаза), чтобы включить другую.';
        el.appendChild(p);
        var host = document.getElementById('passes-compact');
        if (host) {
            host.appendChild(el);
        } else {
            document.body.appendChild(el);
        }
        clearTimeout(showTrackLimitToast._hideT);
        showTrackLimitToast._hideT = setTimeout(function() {
            if (el.parentNode) { el.parentNode.removeChild(el); }
        }, 6500);
    }

    function RightPanelTable() {
        this._tbody = document.getElementById('passes-compact-body');
        this._group = null;
        this._selectedNoradId = null;  // Выбранный в таблице спутник
        this._trackingNoradId = null;  // На сопровождении (red/green).
        this._countdownTimer = null;
        /** Смещение клиентских часов относительно server ts из satellite_group_update (мс). */
        this._serverSkewMs = 0;

        this._trackBtn = document.getElementById('rp-track');
        this._resetBtn = document.getElementById('rp-reset');

        this._bindControls();
    }

    RightPanelTable.prototype.init = function() {
        var self = this;

        this._countdownTimer = setInterval(function() { self._tickCountdowns(); }, 1000);

        if (!_trackLimitToastListenerAdded) {
            _trackLimitToastListenerAdded = true;
            window.addEventListener('satellite-scout-track-limit', function(ev) {
                var m = ev && ev.detail && typeof ev.detail.max === 'number' ? ev.detail.max : undefined;
                showTrackLimitToast(m);
            });
        }

        // Клик по заголовку столбца «Трасса» — toggle all.
        var thToggle = document.getElementById('pc-th-track-toggle');
        if (thToggle) {
            thToggle.addEventListener('click', function() {
                self._toggleAllTracks();
            });
        }

        if (window._stateManager) {
            var sm = window._stateManager;
            var group = sm.getSatelliteGroup();
            if (group) {
                this._onGroupUpdate(group);
            } else {
                this._syncThTrackEye();
            }

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
                sm.subscribe(window.StateEventType.TRACK_VISIBILITY_CHANGE, function() {
                    self._render();
                });
            }
        } else {
            this._syncThTrackEye();
        }

        this._updateControls();
    };

    /** Иконка глаза в заголовке: яркая, если есть хотя бы одна видимая доп. трасса; иначе «выкл.». */
    RightPanelTable.prototype._syncThTrackEye = function() {
        var th = document.getElementById('pc-th-track-toggle');
        if (!th) { return; }
        var inner = document.getElementById('pc-th-track-toggle-inner');
        var on = !!(window._stateManager && window._stateManager.getVisibleTrackIds().length > 0);
        th.classList.remove('pc-th-track--on', 'pc-th-track--off');
        th.classList.add(on ? 'pc-th-track--on' : 'pc-th-track--off');
        th.title = on
            ? 'Скрыть все дополнительные трассы на карте и в небе'
            : 'Показать трассы для всех КА в группе (кроме выбранного и сопровождаемого, в пределах лимита)';
        var html = on ? EYE_VISIBLE_SVG : EYE_HIDDEN_SVG;
        if (inner) {
            inner.innerHTML = html;
        } else {
            th.innerHTML = html;
        }
    };

    // ── Обработка группы из SSE ──

    RightPanelTable.prototype._onGroupUpdate = function(data) {
        this._group = data;
        if (data && typeof data.ts === 'number') {
            this._serverSkewMs = data.ts - Date.now();
        }
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
            this._tbody.innerHTML = '<tr><td colspan="4" class="pc-empty">Нет пролётов</td></tr>';
            return;
        }

        var html = '';

        for (var i = 0; i < satellites.length; i++) {
            var sat = satellites[i];
            var isTracking = (sat.norad_id === this._trackingNoradId);
            var isSelected = (sat.norad_id === this._selectedNoradId);

            var cls = 'pc-row';
            if (isTracking) { cls += ' pc-row--tracking'; }
            if (isSelected) { cls += ' pc-row--selected'; }

            var trackVisible = window._stateManager && window._stateManager.isTrackVisible(sat.norad_id);
            if (trackVisible) { cls += ' pc-row--track-visible'; }

            var name = this._escapeHtml(sat.sat_name || String(sat.norad_id));
            var norad = String(sat.norad_id);

            var aosStr = this._fmtTime(sat.aos);
            var losStr = this._fmtTime(sat.los);

            var col3 = this._renderCol3Html(sat.aos, sat.los);

            var trackCls = 'pc-track-cell' + (trackVisible ? ' pc-track-cell--on' : ' pc-track-cell--off');
            var trackIcon = trackVisible ? EYE_VISIBLE_SVG : EYE_HIDDEN_SVG;

            var rowBg = '';
            if (trackVisible && window._stateManager && !isSelected && !isTracking) {
                var tc = window._stateManager.getTrackColor(sat.norad_id);
                if (tc) { rowBg = ' style="background-color:' + this._hexToRgba(tc, 0.14) + '"'; }
            }

            html += '<tr class="' + cls + '"' + rowBg + ' data-norad="' + sat.norad_id + '"' +
                ' data-aos="' + sat.aos + '" data-los="' + sat.los + '" data-dur="' + sat.duration + '">' +
                // Колонка 0: «глаз» — видимость трассы (SVG: яркий / зачёркнутый)
                '<td class="' + trackCls + '" data-track-toggle="' + sat.norad_id + '"' +
                (trackVisible ? ' title="Трасса на карте и в небе: видна"' : ' title="Трасса скрыта"') +
                '>' + trackIcon + '</td>' +
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
        // Убрать фокус с ячейки после перерисовки — иначе в некоторых браузерах мигает текстовая каретка.
        if (this._tbody && document.activeElement && this._tbody.contains(document.activeElement)) {
            document.activeElement.blur();
        }
        this._syncThTrackEye();
    };

    // ── Тикер обратного отсчёта ──

    RightPanelTable.prototype._serverNowMs = function() {
        return Date.now() + (this._serverSkewMs || 0);
    };

    RightPanelTable.prototype._tickCountdowns = function() {
        if (!this._tbody) { return; }
        var rows = this._tbody.querySelectorAll('.pc-row');
        for (var i = 0; i < rows.length; i++) {
            var row = rows[i];
            var aos = parseInt(row.getAttribute('data-aos'), 10);
            var los = parseInt(row.getAttribute('data-los'), 10);
            var durEl = row.querySelector('.pc-col3-dur');
            var untilEl = row.querySelector('.pc-col3-until');
            var c = this._fmtSessionCols(aos, los, this._serverNowMs());
            if (durEl) { durEl.textContent = c.dur; }
            if (untilEl) { untilEl.textContent = c.until; }
        }
    };

    /** HEX → rgba с альфой (заливка строки под цвет трассы). */
    RightPanelTable.prototype._hexToRgba = function(hex, alpha) {
        if (!hex || hex[0] !== '#') { return 'transparent'; }
        var h = hex.slice(1);
        if (h.length === 3) {
            h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
        }
        if (h.length !== 6) { return 'transparent'; }
        var r = parseInt(h.slice(0, 2), 16);
        var g = parseInt(h.slice(2, 4), 16);
        var b = parseInt(h.slice(4, 6), 16);
        if (isNaN(r) || isNaN(g) || isNaN(b)) { return 'transparent'; }
        return 'rgba(' + r + ',' + g + ',' + b + ',' + alpha + ')';
    };

    // ── Столбцы «длит.» / «до сеанса» (зеркало FormatSessionTableColumns на Go) ──

    RightPanelTable.prototype._fmtRuDuration = function(ms) {
        if (ms < 0) { ms = 0; }
        var totalSec = Math.floor(ms / 1000);
        var h = Math.floor(totalSec / 3600);
        var m = Math.floor((totalSec % 3600) / 60);
        var s = totalSec % 60;
        if (h > 0) { return h + 'ч ' + m + 'м'; }
        if (m > 0) { return m + 'м ' + (s < 10 ? '0' : '') + s + 'с'; }
        return s + 'с';
    };

    RightPanelTable.prototype._fmtSessionCols = function(aos, los, nowMs) {
        if (!aos || !los || los <= aos) {
            return { dur: '—', until: '—' };
        }
        var now = nowMs;
        if (now < aos) {
            return { dur: this._fmtRuDuration(los - aos), until: this._fmtRuDuration(aos - now) };
        }
        if (now <= los) {
            return { dur: this._fmtRuDuration(los - now), until: 'сейчас' };
        }
        return { dur: '—', until: '—' };
    };

    RightPanelTable.prototype._renderCol3Html = function(aos, los) {
        var c = this._fmtSessionCols(aos, los, this._serverNowMs());
        return '<div class="pc-col3-dur">' + this._escapeHtml(c.dur) + '</div>' +
            '<div class="pc-col3-until">' + this._escapeHtml(c.until) + '</div>';
    };

    // ── Привязка кликов по строкам ──

    RightPanelTable.prototype._bindRowEvents = function() {
        var self = this;
        var rows = this._tbody.querySelectorAll('.pc-row');
        for (var i = 0; i < rows.length; i++) {
            (function(row) {
                // Клик по ячейке «Трасса» — toggle видимости.
                var trackCell = row.querySelector('[data-track-toggle]');
                if (trackCell) {
                    trackCell.addEventListener('click', function(e) {
                        e.stopPropagation();
                        var id = parseInt(trackCell.getAttribute('data-track-toggle'), 10);
                        if (window._stateManager) {
                            window._stateManager.toggleTrackVisibility(id);
                            self._render();
                        }
                    });
                }
                // Блокируем выделение/каретку в ячейках (кроме клика по «глазу»).
                row.addEventListener('mousedown', function(e) {
                    if (e.target && e.target.closest && e.target.closest('[data-track-toggle]')) {
                        return;
                    }
                    e.preventDefault();
                });
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
        var clientId = (typeof window.getClientId === 'function') ? window.getClientId() : '';
        fetch('/api/tracking/current', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Client-Id': clientId
            },
            body: JSON.stringify({ norad_id: noradId, client_id: clientId })
        }).then(function(r) {
            if (!r.ok) { console.error('[RightPanel] tracking/current error:', r.status); }
        }).catch(function(err) {
            console.error('[RightPanel] tracking/current fetch error:', err);
        });
    };

    RightPanelTable.prototype._resetTracking = function() {
        var clientId = (typeof window.getClientId === 'function') ? window.getClientId() : '';
        fetch('/api/tracking/reset', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Client-Id': clientId
            },
            body: JSON.stringify({ client_id: clientId })
        }).catch(function(err) {
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

    // ── Toggle all трасс ──

    RightPanelTable.prototype._toggleAllTracks = function() {
        if (!window._stateManager || !this._group || !this._group.satellites) { return; }
        var sm = window._stateManager;
        var extraVisible = sm.getVisibleTrackIds();
        if (extraVisible.length > 0) {
            sm.clearAllTracks();
        } else {
            var ids = [];
            var trackingId = sm.getTrackingSatelliteId();
            var selectedId = sm.getSelectedSatelliteId();
            for (var i = 0; i < this._group.satellites.length; i++) {
                var nid = this._group.satellites[i].norad_id;
                if (nid !== trackingId && nid !== selectedId) {
                    ids.push(nid);
                }
            }
            sm.setAllTracksVisible(ids);
        }
    };

    // ── Форматирование ──

    // Время в локальном часовом поясе браузера: ЧЧ:ММ:СС
    RightPanelTable.prototype._fmtTime = function(ms) {
        if (!ms) { return '--:--:--'; }
        var d = new Date(ms);
        var hh = d.getHours();
        var mm = d.getMinutes();
        var ss = d.getSeconds();
        return (hh < 10 ? '0' : '') + hh + ':' +
               (mm < 10 ? '0' : '') + mm + ':' +
               (ss < 10 ? '0' : '') + ss;
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
