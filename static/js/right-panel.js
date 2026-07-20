// План сеансов наблюдения в правой панели (компактная таблица) + кнопки управления.
// Данные приходят из SSE-события satellite_group_update,
// не из polling GET /api/passes.
// Колонки: [глаз — видимость трассы] | NORAD/имя | FREQ/MOD | AZ/EL | ЗРВ/AOS/LOS.
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
        /** Временная подсветка связки с auto-link (hover). */
        this._linkHoverNorad = null;

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

        this._onLinkHoverBound = function(ev) {
            const d = ev && ev.detail ? ev.detail : null;
            if (!d || d.source === 'plan') { return; }
            self._linkHoverNorad = (typeof d.noradId === 'number' && d.noradId > 0) ? d.noradId : null;
            self._applyLinkHoverClasses();
        };
        document.addEventListener('satellite-scout-link-hover', this._onLinkHoverBound);

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
        this._hideTxMenu();

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

            const txHtml = this._renderTxCellHtml(sat);

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
                '<td class="pc-tx-cell' + txHtml.cellClass + '"' + txHtml.titleAttr + '>' + txHtml.body + '</td>' +
                '<td class="pc-azel-cell">' +
                    '<div class="pc-azel-az">' + azel.az + '</div>' +
                    '<div class="pc-azel-el">' + azel.el + '</div>' +
                '</td>' +
                '<td class="pc-col3-cell">' + col3 + '</td>' +
                '</tr>';
        }

        this._tbody.innerHTML = html;
        this._bindRowEvents();
        this._applyLinkHoverClasses();
        // Убрать фокус с ячейки после перерисовки — иначе в некоторых браузерах мигает текстовая каретка.
        if (this._tbody && document.activeElement && this._tbody.contains(document.activeElement)) {
            document.activeElement.blur();
        }
        this._syncThTrackEye();
        this._fitTxModFonts();
        // Выпадашку ставим только после проверки реального списка SatNOGS
        // (SSE tx_count часто завышен — ложные ▾ при одном TX).
        this._syncTxDropdowns();
    };

    /**
     * Подгоняет размер шрифта модуляции под ширину ячейки (12px → мин. 9px).
     */
    RightPanelTable.prototype._fitTxModFonts = function() {
        if (!this._tbody) { return; }
        const mods = this._tbody.querySelectorAll('.pc-tx-mod');
        const maxPx = 12;
        const minPx = 9;
        for (let i = 0; i < mods.length; i++) {
            const el = mods[i];
            const text = (el.textContent || '').trim();
            if (!text || text === '\u2014' || text === '-/-') {
                el.style.fontSize = '';
                continue;
            }
            if (el.clientWidth < 4) {
                el.style.fontSize = '';
                continue;
            }
            let size = maxPx;
            el.style.fontSize = size + 'px';
            while (size > minPx && el.scrollWidth > el.clientWidth + 0.5) {
                size -= 0.5;
                el.style.fontSize = size + 'px';
            }
        }
    };

    /**
     * Частота для ячейки: до 1–2 знаков после запятой (без лишних нулей).
     * 435.700 → «435.7», 145.825 → «145.83».
     */
    RightPanelTable.prototype._fmtFreqMHz = function(raw) {
        const v = parseFloat(raw);
        if (!isFinite(v) || v <= 0) { return ''; }
        const t = (Math.round(v * 100) / 100).toFixed(2);
        return t.replace(/\.?0+$/, '');
    };

    /**
     * Ячейка частоты/модуляции primary-передатчика.
     * Выпадашку не рисуем здесь — см. _syncTxDropdowns (по факту из API).
     * @returns {{ body: string, titleAttr: string, cellClass: string }}
     */
    RightPanelTable.prototype._renderTxCellHtml = function(sat) {
        const freqRaw = sat && sat.freq_mhz ? String(sat.freq_mhz).trim() : '';
        const freq = this._fmtFreqMHz(freqRaw);
        const mod = sat && sat.modulation ? String(sat.modulation).trim() : '';
        if (!freq && !mod) {
            return {
                body: '<div class="pc-tx-freq">-/-</div>',
                titleAttr: ' title="Нет данных о передатчике"',
                cellClass: '',
            };
        }
        // Единица «МГц» — в заголовке колонки; в ячейке только число.
        const freqLine = freq || '\u2014';
        const modLine = mod || '\u2014';
        const hint = (freq ? freq + ' МГц' : 'частота н/д') +
            (mod ? '; ' + mod : '');
        return {
            body: '<div class="pc-tx-main">' +
                '<div class="pc-tx-freq">' + this._escapeHtml(freqLine) + '</div>' +
                '<div class="pc-tx-mod">' + this._escapeHtml(modLine) + '</div>' +
                '</div>',
            titleAttr: hint ? ' title="' + this._escapeHtml(hint) + '"' : '',
            cellClass: '',
        };
    };

    /** Число различных downlink-частот (дубликаты SatNOGS не считаем). */
    RightPanelTable.prototype._countDistinctTxFreqs = function(list) {
        const seen = Object.create(null);
        let n = 0;
        for (let i = 0; i < (list || []).length; i++) {
            const key = String(list[i].freqMHz || '');
            if (!key || seen[key]) { continue; }
            seen[key] = true;
            n++;
        }
        return n;
    };

    /**
     * Для ячеек с частотой: fetch SatNOGS → ▾ только если разных частот > 1.
     */
    RightPanelTable.prototype._syncTxDropdowns = function() {
        if (!this._tbody) { return; }
        const self = this;
        const token = (this._txSyncToken = (this._txSyncToken || 0) + 1);
        const rows = this._tbody.querySelectorAll('.pc-row');
        for (let i = 0; i < rows.length; i++) {
            (function(row) {
                const noradId = parseInt(row.getAttribute('data-norad'), 10);
                const cell = row.querySelector('.pc-tx-cell');
                if (!noradId || !cell) { return; }
                const freqEl = cell.querySelector('.pc-tx-freq');
                if (!freqEl || freqEl.textContent === '-/-') { return; }

                self._fetchTransmitters(noradId).then(function(list) {
                    if (token !== self._txSyncToken || !cell.parentNode) { return; }
                    const distinct = self._countDistinctTxFreqs(list);
                    const btn = cell.querySelector('.pc-tx-more');
                    if (distinct <= 1) {
                        if (btn) { btn.parentNode.removeChild(btn); }
                        cell.classList.remove('pc-tx-cell--multi');
                        return;
                    }
                    if (btn) {
                        btn.setAttribute('data-tx-count', String(distinct));
                        btn.title = 'Список передатчиков (' + distinct + ')';
                        return;
                    }
                    cell.classList.add('pc-tx-cell--multi');
                    const more = document.createElement('button');
                    more.type = 'button';
                    more.className = 'pc-tx-more';
                    more.setAttribute('data-tx-menu', String(noradId));
                    more.setAttribute('data-tx-count', String(distinct));
                    more.setAttribute('aria-haspopup', 'listbox');
                    more.setAttribute('aria-label', 'Список из ' + distinct + ' передатчиков');
                    more.title = 'Список передатчиков (' + distinct + ')';
                    more.textContent = '\u25BE';
                    more.addEventListener('click', function(e) {
                        e.stopPropagation();
                        e.preventDefault();
                        self._showTxMenu(more, noradId);
                    });
                    cell.appendChild(more);
                });
            })(rows[i]);
        }
    };

    /** Нормализация строки передатчика из ответа SatNOGS API. */
    RightPanelTable.prototype._txRowFromApi = function(t) {
        if (!t || !t.alive) { return null; }
        if (t.status && String(t.status).toLowerCase() !== 'active') { return null; }
        const dl = Number(t.downlink_low) || 0;
        if (dl <= 0) { return null; }
        const mhz = this._fmtFreqMHz(dl / 1e6);
        const mode = t.mode || '';
        let baud = '';
        if (typeof t.baud === 'number' && isFinite(t.baud) && t.baud > 0) {
            baud = t.baud >= 1000
                ? (Math.round(t.baud / 100) / 10) + 'k'
                : String(Math.round(t.baud));
        }
        const mod = baud ? (mode + ' ' + baud).trim() : mode;
        return {
            freqMHz: mhz,
            modulation: mod || '\u2014',
            description: t.description || '',
        };
    };

    RightPanelTable.prototype._fetchTransmitters = function(noradId) {
        if (!this._txCache) { this._txCache = {}; }
        if (this._txCache[noradId]) { return this._txCache[noradId]; }
        const self = this;
        const p = fetch('/api/satnogs/transmitters/' + noradId, {
            headers: { 'Accept': 'application/json' },
            credentials: 'same-origin',
        }).then(function(resp) {
            if (!resp.ok) { return []; }
            return resp.json();
        }).then(function(data) {
            if (!data || !Array.isArray(data.transmitters)) {
                delete self._txCache[noradId];
                return [];
            }
            const out = [];
            for (let i = 0; i < data.transmitters.length; i++) {
                const row = self._txRowFromApi(data.transmitters[i]);
                if (row) { out.push(row); }
            }
            // Пустой ответ не кешируем — ждём прогрев SatNOGS.
            if (out.length === 0) {
                delete self._txCache[noradId];
            }
            return out;
        }).catch(function() {
            delete self._txCache[noradId];
            return [];
        });
        this._txCache[noradId] = p;
        return p;
    };

    RightPanelTable.prototype._showTxMenu = function(anchorEl, noradId) {
        const self = this;
        this._hideTxMenu();
        const menu = document.createElement('div');
        menu.className = 'pc-tx-menu';
        menu.setAttribute('role', 'menu');
        menu.innerHTML = '<div class="pc-tx-menu__loading">Загрузка\u2026</div>';
        document.body.appendChild(menu);
        this._txMenu = menu;

        const place = function() {
            if (!menu.parentNode || !anchorEl.getBoundingClientRect) { return; }
            const r = anchorEl.getBoundingClientRect();
            const mr = menu.getBoundingClientRect();
            let left = r.right - mr.width;
            let top = r.bottom + 4;
            if (left < 4) { left = 4; }
            if (top + mr.height > window.innerHeight - 4) {
                top = Math.max(4, r.top - mr.height - 4);
            }
            menu.style.left = left + 'px';
            menu.style.top = top + 'px';
        };
        place();

        this._txMenuDismiss = function(ev) {
            if (ev.type === 'keydown' && ev.key !== 'Escape') { return; }
            if (ev.type === 'mousedown' && menu.contains(ev.target)) { return; }
            if (ev.type === 'mousedown' && anchorEl.contains && anchorEl.contains(ev.target)) { return; }
            self._hideTxMenu();
        };
        setTimeout(function() {
            document.addEventListener('mousedown', self._txMenuDismiss, true);
            document.addEventListener('keydown', self._txMenuDismiss, true);
        }, 0);

        this._fetchTransmitters(noradId).then(function(list) {
            if (!self._txMenu) { return; }
            // Одна downlink-частота — выпадашка не нужна.
            if (self._countDistinctTxFreqs(list) <= 1) {
                self._hideTxMenu();
                if (anchorEl && anchorEl.parentNode) {
                    const cell = anchorEl.parentNode;
                    cell.removeChild(anchorEl);
                    cell.classList.remove('pc-tx-cell--multi');
                }
                return;
            }
            let html = '';
            for (let i = 0; i < list.length; i++) {
                const t = list[i];
                const desc = t.description
                    ? '<div class="pc-tx-menu__desc">' + self._escapeHtml(t.description) + '</div>'
                    : '';
                html += '<div class="pc-tx-menu__item" role="menuitem">' +
                    '<div class="pc-tx-menu__freq">' + self._escapeHtml(t.freqMHz) + ' МГц' +
                    ' <span class="pc-tx-menu__mod">' + self._escapeHtml(t.modulation) +
                    '</span></div>' + desc + '</div>';
            }
            menu.innerHTML = html;
            if (anchorEl) {
                anchorEl.setAttribute('data-tx-count', String(list.length));
                anchorEl.title = 'Список передатчиков (' + list.length + ')';
            }
            place();
        });
    };

    RightPanelTable.prototype._hideTxMenu = function() {
        if (this._txMenuDismiss) {
            document.removeEventListener('mousedown', this._txMenuDismiss, true);
            document.removeEventListener('keydown', this._txMenuDismiss, true);
            this._txMenuDismiss = null;
        }
        if (this._txMenu && this._txMenu.parentNode) {
            this._txMenu.parentNode.removeChild(this._txMenu);
        }
        this._txMenu = null;
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
        // Знак градуса — в заголовке колонки (AZ ° / EL °).
        return {
            az: state.position.az.toFixed(1),
            el: state.position.el.toFixed(1)
        };
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

    // ── Связка hover с auto-link (План ↔ TX) ──

    RightPanelTable.prototype._applyLinkHoverClasses = function() {
        if (!this._tbody) { return; }
        const hoverId = this._linkHoverNorad;
        const rows = this._tbody.querySelectorAll('.pc-row');
        for (let i = 0; i < rows.length; i++) {
            const row = rows[i];
            const norad = parseInt(row.getAttribute('data-norad'), 10);
            const on = hoverId && norad === hoverId;
            row.classList.toggle('pc-row--link-hover', Boolean(on));
        }
    };

    RightPanelTable.prototype._emitPlanLinkHover = function(noradId) {
        document.dispatchEvent(new CustomEvent('satellite-scout-link-hover', {
            detail: {
                noradId: noradId || null,
                txRowId: null,
                source: 'plan',
            },
        }));
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
                // Список передатчиков (▾) — не выбирает строку.
                const txBtn = row.querySelector('[data-tx-menu]');
                if (txBtn) {
                    txBtn.addEventListener('click', function(e) {
                        e.stopPropagation();
                        e.preventDefault();
                        const id = parseInt(txBtn.getAttribute('data-tx-menu'), 10);
                        if (id) { self._showTxMenu(txBtn, id); }
                    });
                }
                // Блокируем выделение/каретку в ячейках (кроме «глаза» и ▾ TX).
                row.addEventListener('mousedown', function(e) {
                    if (e.target && e.target.closest &&
                        (e.target.closest('[data-track-toggle]') || e.target.closest('[data-tx-menu]'))) {
                        return;
                    }
                    e.preventDefault();
                });
                row.addEventListener('mouseenter', function() {
                    const id = parseInt(row.getAttribute('data-norad'), 10);
                    if (!id) { return; }
                    self._linkHoverNorad = id;
                    self._applyLinkHoverClasses();
                    self._emitPlanLinkHover(id);
                });
                row.addEventListener('mouseleave', function() {
                    self._linkHoverNorad = null;
                    self._applyLinkHoverClasses();
                    self._emitPlanLinkHover(null);
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
