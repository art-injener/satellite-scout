// План сеансов наблюдения в правой панели (компактная таблица) + кнопки управления.
// Данные приходят из SSE-события satellite_group_update,
// не из polling GET /api/passes.
// Колонки: [глаз — видимость трассы] | NORAD/имя | AOS/LOS | До AOS / До LOS (подписи в шапке).
// Логика значений колонки 3 совпадает с internal/services/session_table_ui.go (FormatSessionTableColumns).

(function() {
    'use strict';

    /**
     * SVG «глаз»: stroke/fill задаются inline-цветом из палитры трассы.
     * Функции возвращают HTML-строку с нужным цветом.
     */
    function eyeVisibleSvg(color) {
        const c = color || 'currentColor';
        return '<span class="pc-track-eye-svg-wrap" aria-hidden="true">' +
            '<svg class="pc-track-eye-svg" viewBox="0 0 24 24" width="22" height="22" focusable="false">' +
            '<path fill="none" stroke="' + c + '" stroke-width="2" stroke-linejoin="round" ' +
            'd="M1 12s4.5-7 11-7 11 7 11 7-4.5 7-11 7-11-7-11-7z"/>' +
            '<circle cx="12" cy="12" r="3.25" fill="' + c + '" fill-opacity="0.25" stroke="' + c + '" stroke-width="2"/>' +
            '</svg></span>';
    }

    function eyeHiddenSvg(color) {
        const c = color || 'currentColor';
        const opacity = color ? '0.45' : '1';
        return '<span class="pc-track-eye-svg-wrap" aria-hidden="true">' +
            '<svg class="pc-track-eye-svg" viewBox="0 0 24 24" width="22" height="22" focusable="false"' +
            (color ? ' style="opacity:' + opacity + '"' : '') + '>' +
            '<path fill="none" stroke="' + c + '" stroke-width="2" stroke-linejoin="round" ' +
            'd="M1 12s4.5-7 11-7 11 7 11 7-4.5 7-11 7-11-7-11-7z"/>' +
            '<circle cx="12" cy="12" r="3.25" fill="none" stroke="' + c + '" stroke-width="2"/>' +
            '<path fill="none" stroke="' + c + '" stroke-width="2.25" stroke-linecap="round" d="M3.5 3.5l17 17"/>' +
            '</svg></span>';
    }

    let _trackLimitToastListenerAdded = false;

    /** Всплывающее уведомление о лимите дополнительных трасс на карте/небе. */
    function showTrackLimitToast(maxCount) {
        const max = typeof maxCount === 'number' ? maxCount
            : (typeof window.MAX_VISIBLE_TRACKS === 'number' ? window.MAX_VISIBLE_TRACKS : 5);
        const existing = document.getElementById('pc-track-limit-toast');
        if (existing) { existing.remove(); }
        const el = document.createElement('div');
        el.id = 'pc-track-limit-toast';
        el.className = 'pc-limit-toast';
        el.setAttribute('role', 'alert');
        const p = document.createElement('p');
        p.className = 'pc-limit-toast__text';
        p.textContent = 'На карте и в небе одновременно можно показать не более ' + max +
            ' дополнительных трасс спутников. Отключите одну из трасс (иконка глаза), чтобы включить другую.';
        el.appendChild(p);
        const host = document.getElementById('passes-compact');
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
        this._selectedNoradId = null; // Выбранный в таблице спутник
        this._trackingNoradId = null; // Под наблюдением (red/green).
        this._countdownTimer = null;
        /** Смещение клиентских часов относительно server ts из satellite_group_update (мс). */
        this._serverSkewMs = 0;

        this._trackBtn = document.getElementById('rp-track');
        this._resetBtn = document.getElementById('rp-reset');
        this._controlsEl = document.querySelector('.right-panel__controls');

        this._bindControls();
    }

    RightPanelTable.prototype.init = function() {
        const self = this;

        this._countdownTimer = setInterval(function() { self._tickCountdowns(); }, 1000);

        if (!_trackLimitToastListenerAdded) {
            _trackLimitToastListenerAdded = true;
            window.addEventListener('satellite-scout-track-limit', function(ev) {
                const m = ev && ev.detail && typeof ev.detail.max === 'number' ? ev.detail.max : undefined;
                showTrackLimitToast(m);
            });
        }

        // Клик по заголовку столбца «Трасса» — toggle all.
        const thToggle = document.getElementById('pc-th-track-toggle');
        if (thToggle) {
            thToggle.addEventListener('click', function() {
                self._toggleAllTracks();
            });
        }

        if (window._stateManager) {
            const sm = window._stateManager;
            const group = sm.getSatelliteGroup();
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
                sm.subscribe(window.StateEventType.SHOW_ALL_MODE_CHANGE, function() {
                    self._render();
                });
                sm.subscribe(window.StateEventType.TX_CYCLE, function() {
                    self._updatePacketCells();
                });
            }
        } else {
            this._syncThTrackEye();
        }

        document.addEventListener('satellite-scout-mode-change', function(ev) {
            const mode = ev && ev.detail ? ev.detail.mode : null;
            self._applyTrackingControlsVisibility(mode);
        });
        this._applyTrackingControlsVisibility(
            window._modeManager && typeof window._modeManager.getMode === 'function'
                ? window._modeManager.getMode()
                : null
        );
        this._updateControls();
    };

    /** Master-toggle в заголовке: отражает состояние showAllMode. */
    RightPanelTable.prototype._syncThTrackEye = function() {
        const th = document.getElementById('pc-th-track-toggle');
        if (!th) { return; }
        const inner = document.getElementById('pc-th-track-toggle-inner');
        const on = Boolean(window._stateManager && window._stateManager.isShowAllMode());
        th.classList.remove('pc-th-track--on', 'pc-th-track--off');
        th.classList.add(on ? 'pc-th-track--on' : 'pc-th-track--off');
        th.title = on
            ? 'Показаны все трассы группы — нажмите, чтобы скрыть'
            : 'Скрыты все трассы — нажмите, чтобы показать все';
        const html = on ? eyeVisibleSvg(null) : eyeHiddenSvg();
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
        this._hideMenu();

        const satellites = (this._group && this._group.satellites) ? this._group.satellites : [];

        if (satellites.length === 0) {
            this._tbody.innerHTML = '<tr><td colspan="5" class="pc-empty">Нет пролётов</td></tr>';
            return;
        }

        let html = '';

        for (let i = 0; i < satellites.length; i++) {
            const sat = satellites[i];
            const isTracking = (sat.norad_id === this._trackingNoradId);
            const isSelected = (sat.norad_id === this._selectedNoradId);

            let cls = 'pc-row';
            if (isTracking) { cls += ' pc-row--tracking'; }
            if (isSelected) { cls += ' pc-row--selected'; }

            const trackVisible = window._stateManager && window._stateManager.isTrackVisible(sat.norad_id);
            if (trackVisible) { cls += ' pc-row--track-visible'; }

            const name = this._escapeHtml(sat.sat_name || String(sat.norad_id));
            const alias = sat.sat_alias ? this._escapeHtml(sat.sat_alias) : '';
            const norad = String(sat.norad_id);

            const col3 = this._renderCol3Html(sat.aos, sat.los);

            const azel = this._getAzEl(sat.norad_id);

            const markerColor = window._stateManager
                ? window._stateManager.getMarkerColor(sat.norad_id) : null;
            const trackCls = 'pc-track-cell' + (trackVisible ? ' pc-track-cell--on' : ' pc-track-cell--off');
            const trackIcon = trackVisible ? eyeVisibleSvg(markerColor) : eyeHiddenSvg(markerColor);

            // Пакеты за пролёт (tx_cycle, Σ по всем TX). Частота SatNOGS — только в подсказке ячейки.
            const hasFreq = Boolean(sat.freq_mhz);
            const satnoogsHint = hasFreq
                ? (sat.freq_mhz + ' MHz' + (sat.modulation ? ' · ' + sat.modulation : '') + ' (SatNOGS)')
                : 'Данные SatNOGS недоступны';

            const pktTotal = window._stateManager && typeof window._stateManager.getPassPacketTotal === 'function'
                ? window._stateManager.getPassPacketTotal(sat.norad_id)
                : 0;
            const pktVal = pktTotal > 0 ? String(pktTotal) : '\u2014';
            const pktHint = pktTotal > 0
                ? (pktTotal + ' пакетов за пролёт (все передатчики)')
                : 'Пакетов пока нет';
            const pktCellCls = 'pc-pkt-cell' + (pktTotal > 0 ? ' pc-pkt-cell--has-data' : '');
            const cellTitle = satnoogsHint + '. ' + pktHint;

            html += '<tr class="' + cls + '" data-norad="' + sat.norad_id + '"' +
                ' data-aos="' + sat.aos + '" data-los="' + sat.los + '" data-dur="' + sat.duration + '">' +
                '<td class="' + trackCls + '" data-track-toggle="' + sat.norad_id + '"' +
                (trackVisible ? ' title="Трасса на карте и в небе: видна"' : ' title="Трасса скрыта"') +
                '>' + trackIcon + '</td>' +
                '<td class="pc-name-cell' + (alias ? ' pc-name-cell--alias' : '') + '"' +
                    ' title="' + norad + ' — ' + name + (alias ? ' (' + alias + ')' : '') + '">' +
                    '<div class="pc-sat-name">' + name + '</div>' +
                    (alias ? '<div class="pc-sat-alias">(' + alias + ')</div>' : '') +
                    '<div class="pc-sat-norad">' + norad + '</div>' +
                '</td>' +
                '<td class="pc-azel-cell">' +
                    '<div class="pc-azel-az">' + azel.az + '</div>' +
                    '<div class="pc-azel-el">' + azel.el + '</div>' +
                '</td>' +
                '<td class="pc-col3-cell">' + col3 + '</td>' +
                '<td class="' + pktCellCls + '" title="' + this._escapeHtml(cellTitle) + '"' +
                    ' data-satnoogs-title="' + this._escapeHtml(satnoogsHint) + '">' +
                    '<div class="pc-pkt-val">' + pktVal + '</div>' +
                '</td>' +
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

    /** Текущие Az/El из StateManager (или «—»). */
    RightPanelTable.prototype._getAzEl = function(noradId) {
        const sm = window._stateManager;
        if (!sm) { return { az: '\u2014', el: '\u2014' }; }
        const state = sm.getState(noradId);
        if (!state || !state.position) { return { az: '\u2014', el: '\u2014' }; }
        return {
            az: state.position.az.toFixed(1) + '\u00b0',
            el: state.position.el.toFixed(1) + '\u00b0'
        };
    };

    /** Обновить колонку «Пакеты» без полного перерендера (по событию tx_cycle). */
    RightPanelTable.prototype._updatePacketCells = function() {
        if (!this._tbody || !window._stateManager) { return; }
        const sm = window._stateManager;
        if (typeof sm.getPassPacketTotal !== 'function') { return; }
        const rows = this._tbody.querySelectorAll('.pc-row');
        for (let i = 0; i < rows.length; i++) {
            const row = rows[i];
            const noradId = parseInt(row.getAttribute('data-norad'), 10);
            if (!noradId) { continue; }
            const pktTd = row.querySelector('.pc-pkt-cell');
            const pktEl = row.querySelector('.pc-pkt-val');
            if (!pktEl || !pktTd) { continue; }
            const total = sm.getPassPacketTotal(noradId);
            pktEl.textContent = total > 0 ? String(total) : '\u2014';
            pktTd.classList.toggle('pc-pkt-cell--has-data', total > 0);
            const baseTitle = pktTd.getAttribute('data-satnoogs-title') || '';
            const pktHint = total > 0
                ? (total + ' пакетов за пролёт (все передатчики)')
                : 'Пакетов пока нет';
            if (baseTitle) {
                pktTd.title = baseTitle + '. ' + pktHint;
            }
        }
    };

    RightPanelTable.prototype._tickCountdowns = function() {
        if (!this._tbody) { return; }
        const rows = this._tbody.querySelectorAll('.pc-row');
        for (let i = 0; i < rows.length; i++) {
            const row = rows[i];
            const aos = parseInt(row.getAttribute('data-aos'), 10);
            const los = parseInt(row.getAttribute('data-los'), 10);
            const noradId = parseInt(row.getAttribute('data-norad'), 10);

            const durEl = row.querySelector('.pc-col3-dur');
            const untilEl = row.querySelector('.pc-col3-until');
            const c = this._fmtSessionCols(aos, los, this._serverNowMs());
            if (durEl) { durEl.textContent = c.time; }
            if (untilEl) {
                untilEl.textContent = c.label;
                untilEl.classList.remove('pc-col3-label--aos', 'pc-col3-label--los');
                untilEl.classList.add(c.label === 'LOS:' ? 'pc-col3-label--los' : 'pc-col3-label--aos');
            }

            const azel = this._getAzEl(noradId);
            const azEl2 = row.querySelector('.pc-azel-az');
            const elEl2 = row.querySelector('.pc-azel-el');
            if (azEl2) { azEl2.textContent = azel.az; }
            if (elEl2) { elEl2.textContent = azel.el; }
        }
    };

    /** HEX → rgba с альфой (заливка строки под цвет трассы). */
    RightPanelTable.prototype._hexToRgba = function(hex, alpha) {
        if (!hex || hex[0] !== '#') { return 'transparent'; }
        let h = hex.slice(1);
        if (h.length === 3) {
            h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
        }
        if (h.length !== 6) { return 'transparent'; }
        const r = parseInt(h.slice(0, 2), 16);
        const g = parseInt(h.slice(2, 4), 16);
        const b = parseInt(h.slice(4, 6), 16);
        if (isNaN(r) || isNaN(g) || isNaN(b)) { return 'transparent'; }
        return 'rgba(' + r + ',' + g + ',' + b + ',' + alpha + ')';
    };

    // ── Колонка 3: верх = до AOS / «ЗРВ», низ = длит. пролёта или до LOS (FormatSessionTableColumns) ──

    RightPanelTable.prototype._fmtRuDuration = function(ms) {
        if (ms < 0) { ms = 0; }
        const totalSec = Math.floor(ms / 1000);
        const h = Math.floor(totalSec / 3600);
        const m = Math.floor((totalSec % 3600) / 60);
        const s = totalSec % 60;
        if (h > 0) { return h + 'ч ' + m + 'м'; }
        if (m > 0) { return m + 'м ' + (s < 10 ? '0' : '') + s + 'с'; }
        return s + 'с';
    };

    RightPanelTable.prototype._fmtSessionCols = function(aos, los, nowMs) {
        if (!aos || !los || los <= aos) {
            return { label: '', time: '—' };
        }
        const now = nowMs;
        if (now < aos) {
            return { label: 'AOS:', time: this._fmtRuDuration(aos - now) };
        }
        if (now <= los) {
            return { label: 'LOS:', time: this._fmtRuDuration(los - now) };
        }
        return { label: '', time: '—' };
    };

    RightPanelTable.prototype._renderCol3Html = function(aos, los) {
        const c = this._fmtSessionCols(aos, los, this._serverNowMs());
        const cls = c.label === 'LOS:' ? 'pc-col3-label--los' : 'pc-col3-label--aos';
        return '<div class="pc-col3-until ' + cls + '">' + this._escapeHtml(c.label) + '</div>' +
            '<div class="pc-col3-dur">' + this._escapeHtml(c.time) + '</div>';
    };

    // ── Привязка кликов по строкам ──

    RightPanelTable.prototype._bindRowEvents = function() {
        const self = this;
        const rows = this._tbody.querySelectorAll('.pc-row');
        for (let i = 0; i < rows.length; i++) {
            (function(row) {
                // Клик по ячейке «Трасса» — toggle видимости.
                const trackCell = row.querySelector('[data-track-toggle]');
                if (trackCell) {
                    trackCell.addEventListener('click', function(e) {
                        e.stopPropagation();
                        const id = parseInt(trackCell.getAttribute('data-track-toggle'), 10);
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
                    const id = parseInt(row.getAttribute('data-norad'), 10);
                    self._onRowClick(id);
                });
                row.addEventListener('dblclick', function() {
                    const id = parseInt(row.getAttribute('data-norad'), 10);
                    self._onRowDblClick(id);
                });
                // ПКМ по строке — контекст-меню «Скрыть спутник».
                row.addEventListener('contextmenu', function(e) {
                    e.preventDefault();
                    const id = parseInt(row.getAttribute('data-norad'), 10);
                    const sat = self._findSatInGroup(id);
                    const name = sat ? (sat.sat_name || String(id)) : String(id);
                    self._showHideMenu(e.clientX, e.clientY, id, name);
                });
            })(rows[i]);
        }
    };

    // ── Контекст-меню «Скрыть спутник» ──

    RightPanelTable.prototype._showHideMenu = function(x, y, noradId, name) {
        this._hideMenu();
        const self = this;

        const menu = document.createElement('div');
        menu.className = 'pc-context-menu';
        menu.setAttribute('role', 'menu');

        const item = document.createElement('button');
        item.type = 'button';
        item.className = 'pc-context-menu__item';
        item.textContent = 'Скрыть спутник' + (name ? ' «' + name + '»' : '');
        item.addEventListener('click', function() {
            self._hideSatellite(noradId);
            self._hideMenu();
        });
        menu.appendChild(item);
        document.body.appendChild(menu);

        // Позиционирование с учётом краёв окна.
        const rect = menu.getBoundingClientRect();
        const left = Math.max(4, Math.min(x, window.innerWidth - rect.width - 4));
        const top = Math.max(4, Math.min(y, window.innerHeight - rect.height - 4));
        menu.style.left = left + 'px';
        menu.style.top = top + 'px';
        this._contextMenu = menu;

        // Закрытие по клику вне меню и по Esc.
        this._menuDismiss = function(ev) {
            if (ev.type === 'keydown' && ev.key !== 'Escape') { return; }
            if (ev.type === 'mousedown' && menu.contains(ev.target)) { return; }
            self._hideMenu();
        };
        setTimeout(function() {
            document.addEventListener('mousedown', self._menuDismiss, true);
            document.addEventListener('keydown', self._menuDismiss, true);
        }, 0);
    };

    RightPanelTable.prototype._hideMenu = function() {
        if (this._contextMenu) {
            if (this._contextMenu.parentNode) {
                this._contextMenu.parentNode.removeChild(this._contextMenu);
            }
            this._contextMenu = null;
        }
        if (this._menuDismiss) {
            document.removeEventListener('mousedown', this._menuDismiss, true);
            document.removeEventListener('keydown', this._menuDismiss, true);
            this._menuDismiss = null;
        }
    };

    RightPanelTable.prototype._hideSatellite = function(noradId) {
        const clientId = (typeof window.getClientId === 'function') ? window.getClientId() : '';
        fetch('/api/exclusions', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Client-Id': clientId
            },
            body: JSON.stringify({ norad_id: noradId })
        }).then(function(r) {
            if (!r.ok) { console.error('[RightPanel] exclusions error:', r.status); }
        }).catch(function(err) {
            console.error('[RightPanel] exclusions fetch error:', err);
        });
    };

    RightPanelTable.prototype._onRowClick = function(noradId) {
        this._selectedNoradId = noradId;
        // Устанавливаем selected через StateManager (per-tab, без API-вызова).
        if (window._stateManager) {
            const satInfo = this._findSatInGroup(noradId);
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
        for (let i = 0; i < this._group.satellites.length; i++) {
            if (this._group.satellites[i].norad_id === noradId) { return this._group.satellites[i]; }
        }
        return null;
    };

    // ── Кнопки управления ──

    /** Ручной режим работы (не basic): доступны «Сопровождать» и «Сброс». */
    RightPanelTable.prototype._isManualWorkMode = function() {
        if (document.body.classList.contains('station-basic')) {
            return false;
        }
        const mm = window._modeManager;
        return Boolean(mm && typeof mm.getMode === 'function' && mm.getMode() === 'manual');
    };

    /** Показать блок кнопок только в mode-manual. */
    RightPanelTable.prototype._applyTrackingControlsVisibility = function(mode) {
        if (!this._controlsEl) {
            return;
        }
        if (document.body.classList.contains('station-basic')) {
            this._controlsEl.hidden = true;
            return;
        }
        const current = mode != null ? mode : (
            window._modeManager && window._modeManager.getMode
                ? window._modeManager.getMode()
                : null
        );
        this._controlsEl.hidden = current !== 'manual';
    };

    RightPanelTable.prototype._bindControls = function() {
        const self = this;

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
        if (!this._isManualWorkMode()) {
            return;
        }
        const clientId = (typeof window.getClientId === 'function') ? window.getClientId() : '';
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
        if (!this._isManualWorkMode()) {
            return;
        }
        const clientId = (typeof window.getClientId === 'function') ? window.getClientId() : '';
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
        const manual = this._isManualWorkMode();
        if (this._trackBtn) {
            this._trackBtn.disabled = !manual || !this._selectedNoradId;
        }
        if (this._resetBtn) {
            this._resetBtn.disabled = !manual || !this._trackingNoradId;
        }
    };

    // ── Master-toggle «все трассы группы» ──

    RightPanelTable.prototype._toggleAllTracks = function() {
        if (!window._stateManager) { return; }
        const sm = window._stateManager;
        sm.setShowAllMode(!sm.isShowAllMode());
    };

    // ── Форматирование ──

    // Время в локальном часовом поясе браузера: ЧЧ:ММ:СС
    RightPanelTable.prototype._fmtTime = function(ms) {
        if (!ms) { return '--:--:--'; }
        const d = new Date(ms);
        const hh = d.getHours();
        const mm = d.getMinutes();
        const ss = d.getSeconds();
        return (hh < 10 ? '0' : '') + hh + ':' +
               (mm < 10 ? '0' : '') + mm + ':' +
               (ss < 10 ? '0' : '') + ss;
    };

    RightPanelTable.prototype._escapeHtml = function(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    };

    RightPanelTable.prototype.destroy = function() {
        if (this._countdownTimer) { clearInterval(this._countdownTimer); }
        this._hideMenu();
    };

    window.RightPanelTable = RightPanelTable;

})();
