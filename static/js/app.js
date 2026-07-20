// SatWatch - Main Application Script

(function() {
    'use strict';

    // Индикатор статуса SSE: три состояния (подключено / подключение / отключено)
    const connectionStatusEl = document.getElementById('connection-status');
    const connectionStatusLabel = document.getElementById('connection-status-label');

    function setConnectionStatus(status) {
        if (!connectionStatusEl) { return; }

        connectionStatusEl.classList.remove('sse-status--connected', 'sse-status--connecting', 'sse-status--disconnected');

        let label = 'Отключено';
        let title = 'Поток данных: отключено';

        if (status === 'connected') {
            connectionStatusEl.classList.add('sse-status--connected');
            label = 'Подключено';
            title = 'Поток данных: подключено';
        } else if (status === 'connecting') {
            connectionStatusEl.classList.add('sse-status--connecting');
            label = 'Подключение…';
            title = 'Поток данных: подключение…';
        } else {
            connectionStatusEl.classList.add('sse-status--disconnected');
            title = 'Поток данных: отключено';
        }

        connectionStatusEl.setAttribute('title', title);
        if (connectionStatusLabel) {
            connectionStatusLabel.textContent = label;
        }
    }

    /**
     * Загрузка конфигурации станции и инициализация ModeManager + app-header.
     * Запускается параллельно с инициализацией карты (initCanvasPlaceholders
     * тоже ходит за /api/config — это два независимых потребителя одного ответа).
     */
    /**
     * Синхронизация видимости зон страницы и lifecycle виджетов при смене UI-режима.
     * @param {string|null} mode — 'overview' | 'manual' | null (basic).
     */
    function applyTrackingModeLayout(mode) {
        const isManual = mode === 'manual';
        const trackingLayout = document.getElementById('tracking-layout');
        const manualLayout = document.getElementById('layout-manual');
        if (trackingLayout) {
            trackingLayout.setAttribute('aria-hidden', isManual ? 'true' : 'false');
        }
        if (manualLayout) {
            manualLayout.setAttribute('aria-hidden', isManual ? 'false' : 'true');
        }
        if (window._overviewLink) {
            if (isManual && typeof window._overviewLink.pause === 'function') {
                window._overviewLink.pause();
            } else if (mode === 'overview' && typeof window._overviewLink.resume === 'function') {
                window._overviewLink.resume();
            }
        }
    }

    function initStationModes() {
        if (!window.ModeManager || !window.attachModeBar) {
            return;
        }
        fetch('/api/config')
            .then(function(r) { return r.json(); })
            .then(function(cfg) {
                if (!cfg) { return; }
                const stationType = cfg.station_type || 'basic';
                const radioPaths = Array.isArray(cfg.radio_paths) ? cfg.radio_paths : [];

                // CSS-классы на body — используются и mode-bar (видимость),
                // и любыми будущими виджетами для адаптации под режим/тип станции.
                document.body.classList.add('station-' + stationType);

                const manager = new window.ModeManager(stationType, radioPaths);
                window._modeManager = manager;

                function notifyModeChange(mode) {
                    document.dispatchEvent(new CustomEvent('satellite-scout-mode-change', {
                        detail: { mode: mode },
                    }));
                }

                // Слушатель — до первого notify: иначе при reload с ux.mainMode=manual
                // событие уходит в пустоту и имитация Ручного режима не стартует.
                document.addEventListener('satellite-scout-mode-change', function(ev) {
                    const mode = ev && ev.detail ? ev.detail.mode : null;
                    applyTrackingModeLayout(mode);
                    if (window._manualLayout) {
                        if (mode === 'manual') {
                            window._manualLayout.activate();
                        } else {
                            window._manualLayout.deactivate();
                        }
                    }
                });

                if (manager.getMode()) {
                    document.body.classList.add('mode-' + manager.getMode());
                    applyTrackingModeLayout(manager.getMode());
                    notifyModeChange(manager.getMode());
                }
                manager.onModeChange(function(mode) {
                    // Снимаем все предыдущие mode-*; ставим текущий.
                    const cls = document.body.classList;
                    for (let i = cls.length - 1; i >= 0; i--) {
                        if (cls[i].indexOf('mode-') === 0) {
                            cls.remove(cls[i]);
                        }
                    }
                    cls.add('mode-' + mode);
                    applyTrackingModeLayout(mode);
                    notifyModeChange(mode);
                });

                window._modeBar = window.attachModeBar(manager);

                // Если ManualLayout создан раньше, чем пришёл /api/config — догоняем activate.
                if (window._manualLayout && manager.getMode() === 'manual') {
                    window._manualLayout.activate();
                }
            })
            .catch(function(err) {
                // eslint-disable-next-line no-console
                console.warn('[app.js] Не удалось загрузить конфигурацию станции:', err);
            });
    }

    // Initialize when DOM is ready
    document.addEventListener('DOMContentLoaded', function() {
        // eslint-disable-next-line no-console
        console.log('SatWatch initialized');

        // Mode-bar: переключатель режимов работы и активного радиотракта.
        initStationModes();

        // Set default datetime for simulation
        const passTimeInput = document.getElementById('pass-time');
        if (passTimeInput && !passTimeInput.value) {
            const now = new Date();
            now.setMinutes(now.getMinutes() + 30);
            passTimeInput.value = now.toISOString().slice(0, 16);
        }

        // Gain slider value display
        const gainSlider = document.getElementById('gain');
        const gainValue = document.getElementById('gain-value');
        if (gainSlider && gainValue) {
            gainSlider.addEventListener('input', function() {
                gainValue.textContent = this.value;
            });
        }

        // Overlay «КА» временно отключён; оперативные данные — Map HUD.
        // Map HUD до initCanvasPlaceholders: ensureSSE успевает вызвать setStateManager.
        if (typeof window.MapHud === 'function') {
            const hudRoot = document.getElementById('map-hud');
            if (hudRoot) {
                window._mapHud = new window.MapHud(hudRoot, window._stateManager || null);
            }
        }

        // Initialize canvas placeholders
        initCanvasPlaceholders();

        // Если SSE уже поднял StateManager до создания HUD — догоняем подписку.
        if (window._mapHud && window._stateManager
            && typeof window._mapHud.setStateManager === 'function') {
            window._mapHud.setStateManager(window._stateManager);
        }

        // Инициализация расписания сеансов наблюдения, если мы на вкладке /passes
        if (typeof window.initPassesTable === 'function') {
            const passesContainer = document.getElementById('passes-table-container');
            if (passesContainer) {
                window.initPassesTable();
            }
        }

        // Расписание сеансов наблюдения в правой панели (/tracking)
        initRightPanel();

        // Нижняя панель: переключение вкладок + водопад
        initBottomPanel();

        // Ручной layout: Az/El + спектр/водопад в #layout-manual
        initManualLayout();

        // Нижняя панель Авто: связка Передатчики ↔ История активности
        initOverviewLink();

        // ── Нижняя панель: сворачивание (класс на main-wrapper уменьшает высоту строки 2 grid) ──
        const mainWrapper = document.querySelector('.main-wrapper');
        const bottomPanel = document.getElementById('bottom-panel');
        const bottomToggle = document.getElementById('bottom-panel-toggle');
        if (mainWrapper && bottomPanel && bottomToggle) {
            const LS_BOTTOM = 'ux.bottomCollapsed';

            const setBottomCollapsed = (collapsed) => {
                bottomPanel.classList.toggle('bottom-panel--collapsed', collapsed);
                mainWrapper.classList.toggle('bottom-panel-collapsed', collapsed);
                bottomToggle.textContent = collapsed ? '▲' : '▼';
                bottomToggle.setAttribute('title', collapsed ? 'Развернуть' : 'Свернуть');
                bottomPanel.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
                localStorage.setItem(LS_BOTTOM, collapsed ? '1' : '0');
            };

            setBottomCollapsed(localStorage.getItem(LS_BOTTOM) === '1');

            bottomToggle.addEventListener('click', function(e) {
                e.stopPropagation();
                setBottomCollapsed(!bottomPanel.classList.contains('bottom-panel--collapsed'));
            });

            // В свёрнутом виде клик по левой полоске (не по всей ширине) тоже разворачивает
            const bottomSide = bottomPanel.querySelector('.bottom-panel__side');
            if (bottomSide) {
                bottomSide.addEventListener('click', function() {
                    if (bottomPanel.classList.contains('bottom-panel--collapsed')) {
                        setBottomCollapsed(false);
                    }
                });
            }
        }

        // ── Правая панель: минимизация по ширине (30px), класс на main-wrapper ──
        const rightPanelWrapper = document.querySelector('.main-wrapper.tracking-page');
        const rightToggle = document.getElementById('right-panel-toggle');
        if (rightPanelWrapper && rightToggle) {
            const LS_RIGHT = 'ux.rightCollapsed';
            if (localStorage.getItem(LS_RIGHT) === '1') {
                rightPanelWrapper.classList.add('right-collapsed');
                rightToggle.textContent = '◀';
                rightToggle.setAttribute('title', 'Развернуть');
            } else {
                rightToggle.textContent = '▶';
                rightToggle.setAttribute('title', 'Свернуть');
            }
            rightToggle.addEventListener('click', function() {
                const collapsed = rightPanelWrapper.classList.toggle('right-collapsed');
                rightToggle.textContent = collapsed ? '◀' : '▶';
                rightToggle.setAttribute('title', collapsed ? 'Развернуть' : 'Свернуть');
                localStorage.setItem(LS_RIGHT, collapsed ? '1' : '0');
            });
        }
    });

    // Инициализация StateManager и SSE Client один раз (подписки и подключение).
    function ensureSSEAndSubscriptions() {
        if (window._stateManager) {
            console.log('[app.js] SSE уже инициализирован');
            return;
        }

        console.log('[app.js] Инициализация SSE и StateManager');
        console.log('[app.js] SatelliteStateManager доступен:', typeof window.SatelliteStateManager);
        console.log('[app.js] SSEClient доступен:', typeof window.SSEClient);

        window._stateManager = new window.SatelliteStateManager();

        // Стартовое состояние «глазика» master-toggle берётся из config (UI →
        // ShowAllTracksOnStart) и прокидывается сервером в data-атрибут <body>.
        // Применяется до первого SSE-события, чтобы исключить мерцание.
        const showAllAttr = document.body && document.body.dataset
            ? document.body.dataset.showAllTracksOnStart
            : null;
        if (showAllAttr === 'true' && typeof window._stateManager.setShowAllMode === 'function') {
            window._stateManager.setShowAllMode(true);
        }

        window._sseClient = new window.SSEClient(window._stateManager);

        window._sseClient.onStatusChange(function(evt) {
            console.log('[app.js] Статус SSE изменился:', evt.status);
            const s = evt.status;
            if (s === window.SSEConnectionStatus.CONNECTED) {
                setConnectionStatus('connected');
                // При (ре)подключении к бэкенду — повторно загрузить конфиг
                // наблюдателя: если страница была открыта до старта бэкенда,
                // карта центрирована на (0,0) и без retry так и останется.
                if (window.earthView && typeof window.earthView.setObserver === 'function') {
                    fetch('/api/config').then(function(r) { return r.json(); }).then(function(cfg) {
                        if (cfg && cfg.observer) {
                            const ev = window.earthView;
                            const cur = ev.observer;
                            if (!cur || cur.lon !== cfg.observer.lon || cur.lat !== cfg.observer.lat) {
                                ev.setObserver(cfg.observer.lon, cfg.observer.lat, cfg.observer.name || cur && cur.name || '');
                                ev.draw();
                            }
                        }
                    }).catch(function() { /* бэкенд ещё не отвечает — повторим на следующем reconnect */ });
                }
            } else if (s === window.SSEConnectionStatus.CONNECTING) {
                setConnectionStatus('connecting');
            } else {
                setConnectionStatus('disconnected');
            }
        });

        // Map HUD: подписка на StateManager (если блок есть на странице).
        if (window._mapHud && typeof window._mapHud.setStateManager === 'function') {
            window._mapHud.setStateManager(window._stateManager);
        }

        console.log('[app.js] Подключение к SSE...');
        window._sseClient.connect();

        const sm = window._stateManager;
        const StateEventType = window.StateEventType;

        // Загруженные sky path по noradId (чтобы не перезапрашивать).
        let loadedSkyPathSelected = null;

        // ── POSITION: обновление позиций selected + tracking + secondary ──
        sm.subscribe(StateEventType.POSITION, function(state) {
            const pos = state.position;
            if (!pos) { return; }

            const selectedId = sm.getSelectedSatelliteId();
            const trackingId = sm.getTrackingSatelliteId();

            // EarthView: selected + tracking + вторичные из актуального кеша.
            if (window.earthView) {
                _syncEarthViewFromState();
                window.earthView.draw();
            }

            // SkyView: selected (оранжевый).
            if (window.skyView) {
                if (state.noradId === selectedId) {
                    window.skyView.setSelectedSatellitePosition(pos.az, pos.el);
                    window.skyView.setSelectedSatelliteInfo(state.name || '', state.noradId);
                    // Загрузка sky path для selected (повтор пока не загрузится).
                    if (state.noradId && loadedSkyPathSelected !== state.noradId) {
                        if (loadSkyPathForSatellite(state.noradId, 'selected')) {
                            loadedSkyPathSelected = state.noradId;
                        }
                    }
                }
                // Tracking в SkyView.
                if (trackingId) {
                    const trkState = sm.getState(trackingId);
                    if (trkState && trkState.position) {
                        window.skyView.setSatellitePosition(trkState.position.az, trkState.position.el);
                        window.skyView.setSatelliteInfo(trkState.name || '', trackingId);
                    }
                }
                _updateSecondaryPositions();
            }
        });

        // ── TRACK: обновление треков selected + tracking + secondary ──
        sm.subscribe(StateEventType.TRACK, function(state) {
            const selectedId = sm.getSelectedSatelliteId();
            const trackingId = sm.getTrackingSatelliteId();

            if (window.earthView) {
                // Трек selected спутника.
                if (state.noradId === selectedId) {
                    window.earthView.setSelectedGroundTrack(state.track || null);
                }
                // Трек tracking спутника.
                if (trackingId) {
                    const trkState = sm.getState(trackingId);
                    window.earthView.setGroundTrack(trkState ? trkState.track : null);
                } else {
                    window.earthView.setGroundTrack(null);
                }
                _updateSecondaryTracks();
                window.earthView.draw();
            }
        });

        // ── SELECTED_CHANGE: смена выбранного спутника ──
        sm.subscribe(StateEventType.SELECTED_CHANGE, function(state) {
            if (!state) { return; }
            console.log('[app.js] Selected:', state.noradId, state.name);

            if (window.earthView) {
                window.earthView.setSelectedSatelliteInfo(state.name || '', state.noradId);
                window.earthView.setSelectedGroundTrack(state.track || null);
                if (state.position) {
                    window.earthView.setSelectedSatellitePosition(state.position.lon, state.position.lat, state.position.alt, state.position);
                }
                if (state.visibilityZone && state.visibilityZone.segments) {
                    window.earthView.setSelectedVisibilityZone(state.visibilityZone.segments);
                }
            }
            if (window.skyView) {
                window.skyView.setSelectedSatelliteInfo(state.name || '', state.noradId);
                window.skyView.setSelectedTrack([]);
                if (state.position && state.position.az != null && state.position.el != null) {
                    window.skyView.setSelectedSatellitePosition(state.position.az, state.position.el);
                }
                const loaded = loadSkyPathForSatellite(state.noradId, 'selected');
                loadedSkyPathSelected = loaded ? state.noradId : null;
                const trackingId = sm.getTrackingSatelliteId();
                if (trackingId && trackingId === state.noradId) {
                    loadSkyPathForSatellite(state.noradId, 'tracking');
                }
            }
            // Пересчёт вторичных: бывший selected возвращается в карту вторичных,
            // ему нужно восстановить sky-трек из данных группы.
            _updateSecondaryPositions();
            _refreshSecondarySkyTracks();
        });

        // ── TRACKING_CHANGE: смена/сброс наблюдения ──
        sm.subscribe(StateEventType.TRACKING_CHANGE, function(state) {
            if (state) {
                console.log('[app.js] Tracking ON:', state.noradId, state.name);
                showTrackingOverlay(state.noradId, state.name || '');

                // Трек tracking на EarthView.
                if (window.earthView) {
                    window.earthView.setSatelliteInfo(state.name || '', state.noradId);
                    window.earthView.setGroundTrack(state.track || null);
                    if (state.position) {
                        window.earthView.setSatellitePosition(state.position.lon, state.position.lat, state.position.alt, state.position);
                    }
                    if (state.visibilityZone && state.visibilityZone.segments) {
                        window.earthView.setVisibilityZone(state.visibilityZone.segments);
                    }
                }
                // Sky path для tracking на SkyView.
                if (window.skyView) {
                    window.skyView.setSatelliteInfo(state.name || '', state.noradId);
                    loadSkyPathForSatellite(state.noradId, 'tracking');
                }
            } else {
                console.log('[app.js] Tracking OFF');
                if (window.earthView) {
                    window.earthView.clearTrackingLayer();
                    window.earthView.draw();
                }
                if (window.skyView) {
                    window.skyView.setSatelliteInfo('', null);
                    window.skyView.setTrack([]);
                    window.skyView.setSatellitePosition(NaN, NaN);
                }
            }
        });

        // ── GROUP_POSITION: batch satellite_state_update завершён ──
        sm.subscribe(StateEventType.GROUP_POSITION, function() {
            if (window.earthView) {
                _syncEarthViewFromState();
                window.earthView.draw();
            }
        });

        // ── SATELLITE_GROUP_UPDATE: обновление вторичных, синхронизация часов, sky tracks ──
        sm.subscribe(StateEventType.SATELLITE_GROUP_UPDATE, function(group) {
            if (!group || !group.satellites) { return; }
            if (group.ts && window.skyView) {
                window.skyView.setServerSkew(group.ts - Date.now());
            }
            _updateSecondaryPositions();
            _updateSecondaryTracks();
            if (window.earthView) { window.earthView.draw(); }

            // Sky tracks из SSE: tracking, selected, вторичные.
            if (window.skyView) {
                const trackingId = sm.getTrackingSatelliteId();
                const selectedId = sm.getSelectedSatelliteId();
                if (trackingId) { loadSkyPathForSatellite(trackingId, 'tracking'); }
                if (selectedId) { loadSkyPathForSatellite(selectedId, 'selected'); }
                _refreshSecondarySkyTracks();
                window.skyView.draw();
            }
        });

        // ── TRACK_VISIBILITY_CHANGE: toggle видимости трасс в таблице ──
        sm.subscribe(StateEventType.TRACK_VISIBILITY_CHANGE, function() {
            _updateSecondaryPositions();
            _updateSecondaryTracks();
            _refreshSecondarySkyTracks();
            if (window.earthView) { window.earthView.draw(); }
            if (window.skyView) { window.skyView.draw(); }
        });

        // ── Backward compat: SATELLITE_CHANGE (legacy) — ничего не делаем ──
        // Логика перенесена в SELECTED_CHANGE / TRACKING_CHANGE.
    }

    // Синхронизировать selected/tracking/вторичные EarthView с кешом StateManager.
    function _syncEarthViewFromState() {
        const sm = window._stateManager;
        if (!sm || !window.earthView) { return; }

        const selectedId = sm.getSelectedSatelliteId();
        const trackingId = sm.getTrackingSatelliteId();

        if (selectedId) {
            const selState = sm.getState(selectedId);
            if (selState) {
                if (selState.position) {
                    window.earthView.setSelectedSatellitePosition(
                        selState.position.lon, selState.position.lat,
                        selState.position.alt, selState.position
                    );
                }
                window.earthView.setSelectedSatelliteInfo(selState.name || '', selectedId);
                if (selState.visibilityZone && selState.visibilityZone.segments) {
                    window.earthView.setSelectedVisibilityZone(selState.visibilityZone.segments);
                }
            }
        }
        if (trackingId) {
            const trkState = sm.getState(trackingId);
            if (trkState && trkState.position) {
                window.earthView.setSatellitePosition(
                    trkState.position.lon, trkState.position.lat,
                    trkState.position.alt, trkState.position
                );
                window.earthView.setSatelliteInfo(trkState.name || '', trackingId);
                if (trkState.visibilityZone && trkState.visibilityZone.segments) {
                    window.earthView.setVisibilityZone(trkState.visibilityZone.segments);
                }
            }
        } else {
            window.earthView.clearTrackingLayer();
        }
        _updateSecondaryPositions();
        _updateSecondaryTracks();
    }

    // Вспомогательная: обновить позиции вторичных спутников (исключая selected и tracking).
    // EarthView: маркеры ВСЕХ спутников группы. SkyView: только с включённой трассой (isTrackVisible).
    function _updateSecondaryPositions() {
        const sm = window._stateManager;
        const group = sm && sm.getSatelliteGroup();
        if (!group || !group.satellites) { return; }

        const selectedId = sm.getSelectedSatelliteId();
        const trackingId = sm.getTrackingSatelliteId();
        const earthPositions = [];
        const skyPositions = [];

        for (let i = 0; i < group.satellites.length; i++) {
            const sat = group.satellites[i];
            if (sat.norad_id === selectedId || sat.norad_id === trackingId) { continue; }
            const satState = sm.getState(sat.norad_id);
            if (satState && satState.position) {
                earthPositions.push({
                    noradId: sat.norad_id,
                    name: sat.sat_name,
                    lon: satState.position.lon,
                    lat: satState.position.lat,
                    alt: satState.position.alt
                });
                if (sm.isTrackVisible(sat.norad_id)) {
                    skyPositions.push({
                        noradId: sat.norad_id,
                        name: sat.sat_name,
                        az: satState.position.az,
                        el: satState.position.el
                    });
                }
            }
        }

        if (window.earthView) { window.earthView.setSecondaryPositions(earthPositions); }
        if (window.skyView) { window.skyView.setSecondaryPositions(skyPositions); }
    }

    // Вспомогательная: обновить треки вторичных спутников.
    // Трассы передаются в EarthView только для спутников с isTrackVisible (PASS-MAP-001).
    function _updateSecondaryTracks() {
        const sm = window._stateManager;
        const group = sm && sm.getSatelliteGroup();
        if (!group || !group.satellites) { return; }

        const selectedId = sm.getSelectedSatelliteId();
        const trackingId = sm.getTrackingSatelliteId();

        for (let i = 0; i < group.satellites.length; i++) {
            const sat = group.satellites[i];
            if (sat.norad_id === selectedId || sat.norad_id === trackingId) { continue; }
            const satState = sm.getState(sat.norad_id);
            if (satState && satState.track) {
                if (window.earthView) { window.earthView.setSecondaryTrack(sat.norad_id, satState.track); }
            }
        }
    }

    /** Обновить трассы вторичных спутников в SkyView из данных группы (sky_path в satellite_group_update). */
    function _refreshSecondarySkyTracks() {
        if (!window.skyView) { return; }
        const sm = window._stateManager;
        const group = sm && sm.getSatelliteGroup();
        if (!group || !group.satellites) { return; }

        const selectedId = sm.getSelectedSatelliteId();
        const trackingId = sm.getTrackingSatelliteId();

        for (let i = 0; i < group.satellites.length; i++) {
            const sat = group.satellites[i];
            if (sat.norad_id === selectedId || sat.norad_id === trackingId) { continue; }
            if (!sm.isTrackVisible(sat.norad_id)) { continue; }
            if (sat.sky_path && sat.sky_path.length > 0) {
                const track = sat.sky_path.map(function(pt) {
                    return { az: pt.az, el: pt.el, time: pt.time };
                });
                window.skyView.setSecondaryTrack(sat.norad_id, track);
            }
        }
    }

    // Показ/скрытие оверлея при смене спутника
    let overlayTimer = null;
    let overlayFadeTimer = null;
    let overlayShownForNorad = null;

    function showTrackingOverlay(noradId, name) {
        const overlay = document.getElementById('tracking-overlay');
        if (!overlay) { return; }

        // Повторный вызов для того же NORAD не сбрасывает таймер — иначе серия событий даёт ощущение «долго висит»
        if (overlayTimer !== null && overlayShownForNorad === noradId) {
            return;
        }

        if (overlayTimer) {
            clearTimeout(overlayTimer);
            overlayTimer = null;
        }
        if (overlayFadeTimer) {
            clearTimeout(overlayFadeTimer);
            overlayFadeTimer = null;
        }

        overlayShownForNorad = noradId;

        const noradEl = document.getElementById('tracking-overlay-norad');
        const nameEl = document.getElementById('tracking-overlay-name');
        if (noradEl) { noradEl.textContent = noradId || '---'; }
        if (nameEl) { nameEl.textContent = name || '---'; }

        overlay.classList.remove('hidden', 'fade-out');

        // Короткий показ; overlayFadeMs = transition у .tracking-overlay в main.css
        const overlayVisibleMs = 450;
        const overlayFadeMs = 250;

        overlayTimer = setTimeout(function() {
            overlayTimer = null;
            overlay.classList.add('fade-out');
            overlayFadeTimer = setTimeout(function() {
                overlayFadeTimer = null;
                overlay.classList.add('hidden');
                overlay.classList.remove('fade-out');
                overlayShownForNorad = null;
            }, overlayFadeMs);
        }, overlayVisibleMs);
    }


    /**
     * Установка sky path для SkyView из данных группы (satellite_group_update SSE).
     * @param {number} noradId
     * @param {string} target — 'selected' или 'tracking'
     * @returns {boolean} true если sky_path применён
     */
    function loadSkyPathForSatellite(noradId, target) {
        if (!noradId || !window.skyView) { return false; }
        const sm = window._stateManager;
        const group = sm && sm.getSatelliteGroup();
        if (!group || !group.satellites) {
            return false;
        }

        const sat = group.satellites.find(function(s) { return s.norad_id === noradId; });
        if (!sat) {
            return false;
        }
        if (!sat.sky_path || sat.sky_path.length === 0) {
            return false;
        }

        const track = sat.sky_path.map(function(point) {
            return { az: point.az, el: point.el, time: point.time };
        });

        if (target === 'tracking') {
            window.skyView.setTrack(track);
            window.skyView.setPassTimes(sat.aos, sat.los);
        } else {
            window.skyView.setSelectedTrack(track);
            window.skyView.setSelectedPassTimes(sat.aos, sat.los);
        }
        return true;
    }

    // Инициализация нижней панели: миграция legacy-ключа localStorage
    function initBottomPanel() {
        if (window._bottomPanel) {
            window._bottomPanel.destroy();
            window._bottomPanel = null;
        }
        if (document.getElementById('bottom-panel-body') && typeof window.BottomPanel === 'function') {
            window._bottomPanel = new window.BottomPanel();
        }
    }

    // Инициализация Ручного layout: отдельные canvas Az/El/FFT/WF в #layout-manual
    function initManualLayout() {
        if (window._manualLayout) {
            window._manualLayout.destroy();
            window._manualLayout = null;
        }
        if (!document.getElementById('layout-manual') || typeof window.ManualLayout !== 'function') {
            return;
        }
        if (!window._stateManager) {
            return;
        }
        window._manualLayout = new window.ManualLayout(window._stateManager);
        // Если текущий режим уже Ручной — стартуем рендер сразу.
        const mm = window._modeManager;
        const currentMode = mm && typeof mm.getMode === 'function' ? mm.getMode() : null;
        if (currentMode === 'manual') {
            window._manualLayout.activate();
        }
    }

    // Инициализация связки нижней панели Авто-режима:
    // список передатчиков всех КА группы (#auto-link-tx-list).
    function initOverviewLink() {
        if (window._overviewLink) {
            window._overviewLink.destroy();
            window._overviewLink = null;
        }
        const txListEl = document.getElementById('auto-link-tx-list');
        if (!txListEl) { return; }
        if (!window._stateManager || typeof window.OverviewLink !== 'function') {
            return;
        }
        window._overviewLink = new window.OverviewLink(window._stateManager, {
            txListEl: txListEl,
        });

        const mm = window._modeManager;
        const currentMode = mm && typeof mm.getMode === 'function' ? mm.getMode() : null;
        if (currentMode === 'manual' && typeof window._overviewLink.pause === 'function') {
            window._overviewLink.pause();
        }

        const layoutBtn = document.getElementById('auto-link-layout-toggle');
        if (layoutBtn) {
            layoutBtn.addEventListener('click', function() {
                if (window._overviewLink && typeof window._overviewLink.toggleLayout === 'function') {
                    window._overviewLink.toggleLayout();
                }
            });
        }
    }

    // Инициализация расписания сеансов наблюдения в правой панели
    function initRightPanel() {
        if (window._rightPanelTable) {
            window._rightPanelTable.destroy();
            window._rightPanelTable = null;
        }
        const tbody = document.getElementById('passes-compact-body');
        if (tbody && typeof window.RightPanelTable === 'function') {
            window._rightPanelTable = new window.RightPanelTable();
            window._rightPanelTable.init();
        }
    }

    // Initialize canvas elements with placeholder content
    function initCanvasPlaceholders() {
        ensureSSEAndSubscriptions();

        // Earth View — карта мира, данные с SSE
        const earthCanvas = document.getElementById('earth-view');
        if (earthCanvas && window.EarthView) {
            window.earthView = new window.EarthView(earthCanvas);
            // Клик по карточке выноски → выбор спутника текущим (selected).
            // Имя берём сначала из state-manager, иначе из текущей группы пролётов.
            // manual=true — чтобы автоматический satellite_group_update не перебивал
            // ручной выбор пользователя (тот же контракт, что у клика по таблице).
            window.earthView.onSatelliteClick = function(noradId) {
                const sm = window._stateManager;
                if (!sm || typeof sm.setSelectedSatellite !== 'function') { return; }
                const state = (typeof sm.getState === 'function') ? sm.getState(noradId) : null;
                let name = (state && state.name) ? state.name : '';
                if (!name && typeof sm.getSatelliteGroup === 'function') {
                    const grp = sm.getSatelliteGroup();
                    if (grp && Array.isArray(grp.satellites)) {
                        const s = grp.satellites.find(function(x) { return x.norad_id === noradId; });
                        if (s && s.sat_name) { name = s.sat_name; }
                    }
                }
                sm.setSelectedSatellite(noradId, name, true);
            };
            window.earthView.init().then(function() {
                // Загрузка координат наблюдателя из конфигурации сервера.
                // При отсутствии бэкенда — catch возвращает null; повторный запрос
                // произойдёт при SSE-reconnect (onStatusChange → CONNECTED).
                return fetch('/api/config')
                    .then(function(resp) { return resp.json(); })
                    .catch(function() { return null; });
            }).then(function(cfg) {
                if (cfg && cfg.observer) {
                    window.earthView.setObserver(cfg.observer.lon, cfg.observer.lat, cfg.observer.name || '');
                }
                // Подтягиваем накопленные данные из StateManager (track/position могли прийти до init).
                // Используем selected-слой: tracking-слой устанавливается только когда пользователь
                // нажимает «Сопровождать» и бэкенд присылает satellite_group_update с tracking_id.
                if (window._stateManager) {
                    const sm = window._stateManager;
                    const selectedId = sm.getSelectedSatelliteId();
                    const trackingId = sm.getTrackingSatelliteId();
                    const selState = selectedId ? sm.getState(selectedId) : sm.getActiveState();
                    if (selState) {
                        if (selState.track) {
                            window.earthView.setSelectedGroundTrack(selState.track);
                        }
                        if (selState.position) {
                            window.earthView.setSelectedSatellitePosition(selState.position.lon, selState.position.lat, selState.position.alt, selState.position);
                            window.earthView.setSelectedSatelliteInfo(selState.name || '', selState.noradId);
                        }
                        if (selState.visibilityZone && selState.visibilityZone.segments) {
                            window.earthView.setSelectedVisibilityZone(selState.visibilityZone.segments);
                        }
                    }
                    // Если tracking активен — заполняем и tracking-слой.
                    if (trackingId) {
                        const trkState = sm.getState(trackingId);
                        if (trkState) {
                            if (trkState.track) { window.earthView.setGroundTrack(trkState.track); }
                            if (trkState.position) {
                                window.earthView.setSatellitePosition(trkState.position.lon, trkState.position.lat, trkState.position.alt, trkState.position);
                                window.earthView.setSatelliteInfo(trkState.name || '', trkState.noradId);
                            }
                            if (trkState.visibilityZone && trkState.visibilityZone.segments) {
                                window.earthView.setVisibilityZone(trkState.visibilityZone.segments);
                            }
                        }
                    }
                }
                window.earthView.draw();
            }).catch(function(err) {
                // eslint-disable-next-line no-console
                console.error('EarthView init failed:', err);
                drawPlaceholder(earthCanvas, 'Earth View', 'Ошибка загрузки карты');
            });
        } else if (earthCanvas) {
            drawPlaceholder(earthCanvas, 'Earth View', 'Карта мира появится здесь');
        }

        // Sky View — азимутальная проекция неба (в правой панели), данные с SSE
        const skyCanvas = document.getElementById('sky-view');
        if (skyCanvas && window.SkyView) {
            window.skyView = new window.SkyView(skyCanvas);

            // Панель AOS/LOS под SkyView: см. SHOW_INFO_PANEL в skyview.js (сейчас выкл.).
            if (typeof window.skyView.setInfoElements === 'function') {
                window.skyView.setInfoElements({
                    aos: 'skyview-info-aos',
                    los: 'skyview-info-los',
                    dur: 'skyview-info-dur',
                    remaining: 'skyview-info-remaining'
                });
            }

            // Canvas SkyView — квадратный буфер по --skyview-size (CSS), не путать с Az/El.
            const skySizeCss = parseInt(
                getComputedStyle(document.documentElement).getPropertyValue('--skyview-size'),
                10
            );
            const skySize = (skySizeCss > 0) ? skySizeCss : 340;
            if (skyCanvas.width !== skySize || skyCanvas.height !== skySize) {
                skyCanvas.width = skySize;
                skyCanvas.height = skySize;
            }

            window.skyView.draw();

            // Запуск цикла анимации для SkyView (плавная отрисовка)
            startSkyViewAnimation();

            // Загружаем sky path для текущего спутника, если он уже известен
            if (window._stateManager) {
                const state = window._stateManager.getActiveState();
                if (state && state.noradId) {
                    loadSkyPathForSatellite(state.noradId);
                }
            }
        } else if (skyCanvas) {
            drawPlaceholder(skyCanvas, '', 'Небесная сфера');
        }

        // Azimuth/Elevation индикаторы для tracking-страницы живут в Ручном режиме —
        // их инстансы создаёт ManualLayout (см. manual-layout.js). Глобальных
        // window.azimuthIndicator/elevationIndicator больше нет (ADR-004 v2026-06-03).

        // Waterfall placeholder
        const wfCanvas = document.getElementById('waterfall');
        if (wfCanvas) {
            drawWaterfallPlaceholder(wfCanvas);
        }
    }

    // Draw a generic placeholder on canvas
    function drawPlaceholder(canvas, title, subtitle) {
        const ctx = canvas.getContext('2d');
        const w = canvas.width;
        const h = canvas.height;

        // Background
        ctx.fillStyle = '#0a0e14';
        ctx.fillRect(0, 0, w, h);

        // Grid
        ctx.strokeStyle = '#1a2030';
        ctx.lineWidth = 1;
        const gridSize = 40;
        for (let x = 0; x <= w; x += gridSize) {
            ctx.beginPath();
            ctx.moveTo(x, 0);
            ctx.lineTo(x, h);
            ctx.stroke();
        }
        for (let y = 0; y <= h; y += gridSize) {
            ctx.beginPath();
            ctx.moveTo(0, y);
            ctx.lineTo(w, y);
            ctx.stroke();
        }

        // Text
        ctx.fillStyle = '#5c6370';
        ctx.font = '14px Inter, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText(title, w / 2, h / 2 - 10);
        ctx.font = '12px Inter, sans-serif';
        ctx.fillStyle = '#3c4350';
        ctx.fillText(subtitle, w / 2, h / 2 + 10);
    }

    // Draw waterfall placeholder
    function drawWaterfallPlaceholder(canvas) {
        const ctx = canvas.getContext('2d');
        const w = canvas.width;
        const h = canvas.height;

        // Gradient background
        const gradient = ctx.createLinearGradient(0, 0, 0, h);
        gradient.addColorStop(0, '#000022');
        gradient.addColorStop(1, '#000044');
        ctx.fillStyle = gradient;
        ctx.fillRect(0, 0, w, h);

        // Noise pattern simulation
        ctx.fillStyle = '#001144';
        for (let y = 0; y < h; y += 2) {
            for (let x = 0; x < w; x += 4) {
                if (Math.random() > 0.7) {
                    ctx.fillRect(x, y, 2, 1);
                }
            }
        }

        // Center text
        ctx.fillStyle = '#5c6370';
        ctx.font = '14px Inter, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('Waterfall Display', w / 2, h / 2);
    }

    // HTMX event handlers
    document.body.addEventListener('htmx:afterSwap', function() {
        // Reinitialize canvas after HTMX swap
        initCanvasPlaceholders();

        // Инициализация расписания сеансов наблюдения, если мы на вкладке /passes
        if (typeof window.initPassesTable === 'function') {
            const passesContainer = document.getElementById('passes-table-container');
            if (passesContainer) {
                window.initPassesTable();
            }
        }

        initRightPanel();
        initBottomPanel();
        initManualLayout();
        initOverviewLink();
    });

    // Переключение активного класса на табах при клике
    document.body.addEventListener('htmx:beforeRequest', function(evt) {
        const clickedTab = evt.target.closest('.tab');
        if (clickedTab) {
            // Убираем active со всех табов
            document.querySelectorAll('.tabs .tab').forEach(function(tab) {
                tab.classList.remove('active');
            });
            // Добавляем active на кликнутый таб
            clickedTab.classList.add('active');
        }
    });

    // Цикл анимации для SkyView (использует requestAnimationFrame)
    let skyViewAnimationId = null;

    function startSkyViewAnimation() {
        if (skyViewAnimationId) {
            cancelAnimationFrame(skyViewAnimationId);
        }

        let frameCount = 0;
        function animate() {
            if (window.skyView && window._stateManager) {
                const sm = window._stateManager;
                const trackingId = sm.getTrackingSatelliteId();
                const selectedId = sm.getSelectedSatelliteId();
                // Позиция спутника под наблюдением — всегда из его состояния (не из active/selected)
                if (trackingId) {
                    const trkState = sm.getState(trackingId);
                    if (trkState && trkState.position && trkState.position.az !== null && trkState.position.el !== null) {
                        window.skyView.setSatellitePosition(trkState.position.az, trkState.position.el);
                    }
                }
                // Позиция текущего (выбранного) — из состояния выбранного спутника
                if (selectedId) {
                    const selState = sm.getState(selectedId);
                    if (selState && selState.position && selState.position.az !== null && selState.position.el !== null) {
                        window.skyView.setSelectedSatellitePosition(selState.position.az, selState.position.el);
                    }
                }
            }
            if (window.skyView) {
                window.skyView.draw();
                // Раз в секунду обновляем «Осталось» в панели SkyView
                frameCount++;
                if (frameCount >= 60) {
                    frameCount = 0;
                    window.skyView._updateInfoPanelDOM();
                }
            }
            skyViewAnimationId = requestAnimationFrame(animate);
        }

        animate();
    }

    function stopSkyViewAnimation() {
        if (skyViewAnimationId) {
            cancelAnimationFrame(skyViewAnimationId);
            skyViewAnimationId = null;
        }
    }

    // Expose for debugging
    window.SatWatch = {
        setConnectionStatus: setConnectionStatus,
        loadSkyPath: loadSkyPathForSatellite,
        stopSkyViewAnimation: stopSkyViewAnimation
    };

})();
