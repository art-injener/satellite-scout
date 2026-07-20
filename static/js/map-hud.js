/**
 * MapHud — VFD-индикаторы поверх Earth View.
 * Левый верх: UTC / местное время.
 * Правый верх: точка наблюдения.
 * Низ: NORAD · AZ · EL NOW/MAX (+↑/↓) · countdown · ALT.
 */
(function() {
    'use strict';

    function pad2(n) {
        return n < 10 ? '0' + n : String(n);
    }

    /**
     * Стрелка тренда EL по фазе пролёта: до TCA — ↑, после — ↓.
     * Вне окна AOS…LOS или без TCA — пустая строка.
     * @param {number} nowMs
     * @param {number} tcaMs
     * @param {number} aos
     * @param {number} los
     * @returns {string}
     */
    function elTrendArrow(nowMs, tcaMs, aos, los) {
        if (!tcaMs || !aos || !los || los <= aos) { return ''; }
        if (nowMs < aos || nowMs > los) { return ''; }
        if (nowMs < tcaMs) { return '\u2191'; }
        if (nowMs > tcaMs) { return '\u2193'; }
        return '\u2014';
    }

    /**
     * @param {HTMLElement} root — #map-hud
     * @param {object} [stateManager] — SatelliteStateManager
     */
    function MapHud(root, stateManager) {
        this._root = root;
        this._sm = stateManager || null;
        this._serverSkewMs = 0;
        this._clockTimer = null;
        this._tickTimer = null;
        this._els = {
            utc: document.getElementById('map-hud-utc'),
            local: document.getElementById('map-hud-local'),
            city: document.getElementById('map-hud-city'),
            coords: document.getElementById('map-hud-coords'),
            pass: document.getElementById('map-hud-pass'),
            name: document.getElementById('map-hud-name'),
            norad: document.getElementById('map-hud-norad'),
            az: document.getElementById('map-hud-az'),
            el: document.getElementById('map-hud-el'),
            elDir: document.getElementById('map-hud-el-dir'),
            cdLabel: document.getElementById('map-hud-cd-label'),
            cdTime: document.getElementById('map-hud-cd-time'),
            alt: document.getElementById('map-hud-alt'),
        };

        this._updateClocks();
        this._clockTimer = setInterval(this._updateClocks.bind(this), 1000);

        if (this._sm && window.StateEventType) {
            this._subscribe();
        }
        this._refreshPass();
        this._tickTimer = setInterval(this._refreshPass.bind(this), 1000);
    }

    MapHud.prototype.setStateManager = function(stateManager) {
        this._sm = stateManager || null;
        if (this._sm && window.StateEventType) {
            this._subscribe();
        }
        this._refreshPass();
    };

    /** Обновить имя и координаты точки наблюдения (из настроек). */
    MapHud.prototype.setObserver = function(name, coordsLabel) {
        if (this._els.city && name) {
            this._els.city.textContent = name;
        }
        if (this._els.coords && coordsLabel) {
            this._els.coords.textContent = coordsLabel;
        }
    };

    MapHud.prototype._subscribe = function() {
        if (this._subscribed || !this._sm) { return; }
        const self = this;
        const Ev = window.StateEventType;
        this._sm.subscribe(Ev.POSITION, function() { self._refreshPass(); });
        this._sm.subscribe(Ev.SELECTED_CHANGE, function() { self._refreshPass(); });
        this._sm.subscribe(Ev.TRACKING_CHANGE, function() { self._refreshPass(); });
        this._sm.subscribe(Ev.SATELLITE_GROUP_UPDATE, function(data) {
            if (data && typeof data.ts === 'number') {
                self._serverSkewMs = data.ts - Date.now();
            }
            self._refreshPass();
        });
        this._subscribed = true;
    };

    MapHud.prototype._updateClocks = function() {
        const now = new Date();
        if (this._els.utc) {
            this._els.utc.textContent =
                pad2(now.getUTCHours()) + ':' +
                pad2(now.getUTCMinutes()) + ':' +
                pad2(now.getUTCSeconds());
        }
        if (this._els.local) {
            this._els.local.textContent =
                pad2(now.getHours()) + ':' +
                pad2(now.getMinutes()) + ':' +
                pad2(now.getSeconds());
        }
    };

    MapHud.prototype._serverNowMs = function() {
        return Date.now() + (this._serverSkewMs || 0);
    };

    /** Логика колонки «ЗРВ» плана сеансов (AOS: / LOS: + длительность). */
    MapHud.prototype._fmtRuDuration = function(ms) {
        if (ms < 0) { ms = 0; }
        const totalSec = Math.floor(ms / 1000);
        const h = Math.floor(totalSec / 3600);
        const m = Math.floor((totalSec % 3600) / 60);
        const s = totalSec % 60;
        if (h > 0) { return h + 'ч ' + m + 'м'; }
        if (m > 0) { return m + 'м ' + pad2(s) + 'с'; }
        return s + 'с';
    };

    MapHud.prototype._fmtSessionCols = function(aos, los, nowMs) {
        if (!aos || !los || los <= aos) {
            return { label: '', time: '—' };
        }
        if (nowMs < aos) {
            return { label: 'AOS:', time: this._fmtRuDuration(aos - nowMs) };
        }
        if (nowMs <= los) {
            return { label: 'LOS:', time: this._fmtRuDuration(los - nowMs) };
        }
        return { label: '', time: '—' };
    };

    MapHud.prototype._fmtOrbitAltKm = function(km) {
        const v = Number(km);
        if (!isFinite(v) || v <= 0) { return '—'; }
        if (v >= 10000) {
            const k = v / 1000;
            const rounded = Math.round(k * 10) / 10;
            const num = rounded % 1 === 0 ? String(Math.round(rounded)) : rounded.toFixed(1);
            return num + 'k км';
        }
        return String(Math.round(v)) + ' км';
    };

    /** Текущий выбранный КА (selected). */
    MapHud.prototype._resolveNoradId = function() {
        if (!this._sm) { return null; }
        return this._sm.getSelectedSatelliteId() || null;
    };

    MapHud.prototype._findGroupSat = function(noradId) {
        if (!this._sm || !noradId) { return null; }
        const group = this._sm.getSatelliteGroup();
        if (!group || !group.satellites) { return null; }
        for (let i = 0; i < group.satellites.length; i++) {
            if (group.satellites[i].norad_id === noradId) {
                return group.satellites[i];
            }
        }
        return null;
    };

    MapHud.prototype._setText = function(el, text) {
        if (el) { el.textContent = text; }
    };

    MapHud.prototype._clearPass = function() {
        this._setText(this._els.name, '—');
        this._setText(this._els.norad, '-----');
        this._setText(this._els.az, '—');
        this._setText(this._els.el, '— / —');
        this._setText(this._els.elDir, '');
        this._setText(this._els.cdLabel, '');
        this._setText(this._els.cdTime, '—');
        this._setText(this._els.alt, '—');
    };

    MapHud.prototype._refreshPass = function() {
        const noradId = this._resolveNoradId();
        if (!noradId) {
            this._clearPass();
            return;
        }

        const sat = this._findGroupSat(noradId);
        const state = this._sm.getState(noradId);
        const satName = (sat && sat.sat_name)
            || (state && state.name)
            || '';
        this._setText(this._els.name, satName || '—');
        this._setText(this._els.norad, String(noradId));

        const pos = state && state.position ? state.position : null;
        if (pos && typeof pos.az === 'number' && typeof pos.el === 'number') {
            this._setText(this._els.az, pos.az.toFixed(1) + '°');
        } else {
            this._setText(this._els.az, '—');
        }

        // Текущая EL / max EL + стрелка тренда (до TCA ↑, после ↓).
        const elNow = (pos && typeof pos.el === 'number') ? pos.el.toFixed(1) + '°' : '—';
        const tcaEl = sat && typeof sat.tca_el === 'number' ? sat.tca_el : NaN;
        const elMax = (isFinite(tcaEl) && tcaEl > 0) ? tcaEl.toFixed(1) + '°' : '—';
        const arrow = elTrendArrow(
            this._serverNowMs(),
            sat && sat.tca ? sat.tca : 0,
            sat ? sat.aos : 0,
            sat ? sat.los : 0
        );
        this._setText(this._els.el, elNow + ' / ' + elMax);
        this._setText(this._els.elDir, arrow);

        const cd = this._fmtSessionCols(
            sat ? sat.aos : 0,
            sat ? sat.los : 0,
            this._serverNowMs()
        );
        this._setText(this._els.cdLabel, cd.label);
        this._setText(this._els.cdTime, cd.time);

        this._setText(
            this._els.alt,
            this._fmtOrbitAltKm(sat && sat.orbit_alt_km)
        );
    };

    MapHud.prototype.destroy = function() {
        if (this._clockTimer) {
            clearInterval(this._clockTimer);
            this._clockTimer = null;
        }
        if (this._tickTimer) {
            clearInterval(this._tickTimer);
            this._tickTimer = null;
        }
    };

    window.MapHud = MapHud;
    window.MapHudElTrendArrow = elTrendArrow;
})();
