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

    // Initialize when DOM is ready
    document.addEventListener('DOMContentLoaded', function() {
        // eslint-disable-next-line no-console
        console.log('SatWatch initialized');

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

        // Overlay-панель: сохраняем ссылку ДО инициализации SSE,
        // чтобы ensureSSEAndSubscriptions мог создать InfoPanel и OverlayPanel.
        const infoPanelEl = document.getElementById('sat-overlay-panel');
        if (infoPanelEl) {
            window._infoPanelEl = infoPanelEl;
        }

        // Initialize canvas placeholders
        initCanvasPlaceholders();

        // Часы в station-footer: UTC и местное время (обновление каждую секунду)
        const sfUtc = document.getElementById('sf-utc');
        const sfLocal = document.getElementById('sf-local');
        if (sfUtc || sfLocal) {
            const pad2 = function(n) { return n < 10 ? '0' + n : String(n); };
            const updateFooterClocks = function() {
                const now = new Date();
                if (sfUtc) {
                    sfUtc.textContent = 'UTC ' + pad2(now.getUTCHours()) + ':' + pad2(now.getUTCMinutes()) + ':' + pad2(now.getUTCSeconds());
                }
                if (sfLocal) {
                    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
                    const tzShort = tz.split('/').pop().replace(/_/g, ' ');
                    sfLocal.textContent = tzShort + ' ' + pad2(now.getHours()) + ':' + pad2(now.getMinutes()) + ':' + pad2(now.getSeconds());
                }
            };
            updateFooterClocks();
            setInterval(updateFooterClocks, 1000);
        }

        // Инициализация таблицы пролётов, если мы на вкладке /passes
        if (typeof window.initPassesTable === 'function') {
            const passesContainer = document.getElementById('passes-table-container');
            if (passesContainer) {
                window.initPassesTable();
            }
        }

        // Компактная таблица пролётов в правой панели (/tracking)
        initRightPanel();

        // Нижняя панель: переключение вкладок + водопад
        initBottomPanel();

        // ── Нижняя панель: сворачивание (класс на main-wrapper уменьшает высоту строки 2 grid) ──
        const mainWrapper = document.querySelector('.main-wrapper');
        const bottomPanel = document.getElementById('bottom-panel');
        const bottomToggle = document.getElementById('bottom-panel-toggle');
        if (mainWrapper && bottomPanel && bottomToggle) {
            const LS_BOTTOM = 'ux.bottomCollapsed';
            if (localStorage.getItem(LS_BOTTOM) === '1') {
                bottomPanel.classList.add('bottom-panel--collapsed');
                mainWrapper.classList.add('bottom-panel-collapsed');
                bottomToggle.textContent = '▲';
                bottomToggle.setAttribute('title', 'Развернуть');
            } else {
                bottomToggle.setAttribute('title', 'Свернуть');
            }
            bottomToggle.addEventListener('click', function() {
                const collapsed = bottomPanel.classList.toggle('bottom-panel--collapsed');
                mainWrapper.classList.toggle('bottom-panel-collapsed', collapsed);
                bottomToggle.textContent = collapsed ? '▲' : '▼';
                bottomToggle.setAttribute('title', collapsed ? 'Развернуть' : 'Свернуть');
                localStorage.setItem(LS_BOTTOM, collapsed ? '1' : '0');
                // После разворота панели — принудительное обновление водопада и шкалы
                if (!collapsed && window._bottomPanel && typeof window._bottomPanel.refreshWaterfall === 'function') {
                    requestAnimationFrame(function() {
                        requestAnimationFrame(function() {
                            window._bottomPanel.refreshWaterfall();
                        });
                    });
                }
            });
        }

        // ── Правая панель: минимизация по ширине (30px), класс на main-wrapper ──
        const rightPanelWrapper = document.querySelector('.main-wrapper.tracking-page');
        const rightToggle = document.getElementById('right-panel-toggle');
        if (rightPanelWrapper && rightToggle) {
            const LS_RIGHT = 'ux.rightCollapsed';
            if (localStorage.getItem(LS_RIGHT) === '1') {
                rightPanelWrapper.classList.add('right-collapsed');
                rightToggle.textContent = '▶';
                rightToggle.setAttribute('title', 'Развернуть');
            } else {
                rightToggle.setAttribute('title', 'Свернуть');
            }
            rightToggle.addEventListener('click', function() {
                const collapsed = rightPanelWrapper.classList.toggle('right-collapsed');
                rightToggle.textContent = collapsed ? '▶' : '◀';
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
        window._sseClient = new window.SSEClient(window._stateManager);

        window._sseClient.onStatusChange(function(evt) {
            console.log('[app.js] Статус SSE изменился:', evt.status);
            const s = evt.status;
            if (s === window.SSEConnectionStatus.CONNECTED) {
                setConnectionStatus('connected');
            } else if (s === window.SSEConnectionStatus.CONNECTING) {
                setConnectionStatus('connecting');
            } else {
                setConnectionStatus('disconnected');
            }
        });

        // InfoPanel — обновляет поля #ip-* в overlay-панели через StateManager.
        if (window._infoPanelEl && typeof window.InfoPanel === 'function') {
            window._infoPanel = new window.InfoPanel(window._infoPanelEl, window._stateManager);
        }

        // OverlayPanel — управляет видимостью overlay-панели (show/hide).
        if (window._infoPanelEl && typeof window.OverlayPanel === 'function') {
            window._overlayPanel = new window.OverlayPanel(window._infoPanelEl, window._stateManager);
        }

        console.log('[app.js] Подключение к SSE...');
        window._sseClient.connect();

        const sm = window._stateManager;
        const StateEventType = window.StateEventType;

        // Загруженные sky path по noradId (чтобы не перезапрашивать).
        let loadedSkyPathSelected = null;
        let loadedSkyPathTracking = null;

        // ── POSITION: обновление позиций selected + tracking + secondary ──
        sm.subscribe(StateEventType.POSITION, function(state) {
            const pos = state.position;
            if (!pos) { return; }

            const selectedId = sm.getSelectedSatelliteId();
            const trackingId = sm.getTrackingSatelliteId();

            // EarthView: selected satellite (оранжевый + иконка + зона видимости).
            if (window.earthView) {
                if (state.noradId === selectedId) {
                    window.earthView.setSelectedSatellitePosition(pos.lon, pos.lat, pos.alt);
                    window.earthView.setSelectedSatelliteInfo(state.name || '', state.noradId);
                    if (state.visibilityZone && state.visibilityZone.segments) {
                        window.earthView.setSelectedVisibilityZone(state.visibilityZone.segments);
                    }
                }
                // Слой «на сопровождении»: данные только с бэка (tracking_id в group_update, позиции/треки в state_update).
                if (trackingId) {
                    const trkState = sm.getState(trackingId);
                    if (trkState && trkState.position) {
                        window.earthView.setSatellitePosition(trkState.position.lon, trkState.position.lat, trkState.position.alt);
                        window.earthView.setSatelliteInfo(trkState.name || '', trackingId);
                        if (trkState.visibilityZone && trkState.visibilityZone.segments) {
                            window.earthView.setVisibilityZone(trkState.visibilityZone.segments);
                        }
                    }
                } else {
                    // Сопровождения нет — полностью очищаем слой tracking, чтобы не рисовать красный/зелёный.
                    window.earthView.clearTrackingLayer();
                }
                _updateSecondaryPositions();
                _updateSecondaryTracks();
                window.earthView.draw();
            }

            // SkyView: selected (оранжевый).
            if (window.skyView) {
                if (state.noradId === selectedId) {
                    window.skyView.setSelectedSatellitePosition(pos.az, pos.el);
                    window.skyView.setSelectedSatelliteInfo(state.name || '', state.noradId);
                    // Загрузка sky path для selected.
                    if (state.noradId && loadedSkyPathSelected !== state.noradId) {
                        loadedSkyPathSelected = state.noradId;
                        _loadSkyPath(state.noradId, 'selected');
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

            // Az/El индикаторы — только для tracking.
            if (trackingId) {
                const trkState = sm.getState(trackingId);
                if (trkState && trkState.position) {
                    var tp = trkState.position;
                    if (window.azimuthIndicator) {
                        window.azimuthIndicator.setSatellitePosition(tp.az);
                        window.azimuthIndicator.setAzimuth(tp.az);
                        window.azimuthIndicator.setNoradId(trackingId);
                    }
                    if (window.elevationIndicator) {
                        window.elevationIndicator.setSatellitePosition(tp.el, tp.az);
                        window.elevationIndicator.setPosition(tp.az, tp.el);
                        window.elevationIndicator.setNoradId(trackingId);
                    }
                }
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
                    window.earthView.setSelectedSatellitePosition(state.position.lon, state.position.lat, state.position.alt);
                }
                if (state.visibilityZone && state.visibilityZone.segments) {
                    window.earthView.setSelectedVisibilityZone(state.visibilityZone.segments);
                }
            }
            if (window.skyView) {
                window.skyView.setSelectedSatelliteInfo(state.name || '', state.noradId);
                window.skyView.setSelectedTrack([]);
                loadedSkyPathSelected = state.noradId;
                _loadSkyPath(state.noradId, 'selected');
            }
        });

        // ── TRACKING_CHANGE: смена/сброс сопровождения ──
        sm.subscribe(StateEventType.TRACKING_CHANGE, function(state) {
            if (state) {
                console.log('[app.js] Tracking ON:', state.noradId, state.name);
                showTrackingOverlay(state.noradId, state.name || '');

                // Запуск отрисовки водопада при включении сопровождения
                if (window._bottomPanel && typeof window._bottomPanel.startWaterfall === 'function') {
                    window._bottomPanel.startWaterfall();
                }
                // Вкладка «Антенна» (азимут/угол места/водопад) при сопровождении
                if (window._bottomPanel && typeof window._bottomPanel.showTab === 'function') {
                    window._bottomPanel.showTab('antenna', false);
                }

                // Трек tracking на EarthView.
                if (window.earthView) {
                    window.earthView.setSatelliteInfo(state.name || '', state.noradId);
                    window.earthView.setGroundTrack(state.track || null);
                    if (state.position) {
                        window.earthView.setSatellitePosition(state.position.lon, state.position.lat, state.position.alt);
                    }
                    if (state.visibilityZone && state.visibilityZone.segments) {
                        window.earthView.setVisibilityZone(state.visibilityZone.segments);
                    }
                }
                // Sky path для tracking на SkyView.
                if (window.skyView) {
                    window.skyView.setSatelliteInfo(state.name || '', state.noradId);
                    loadedSkyPathTracking = state.noradId;
                    _loadSkyPath(state.noradId, 'tracking');
                }
            } else {
                console.log('[app.js] Tracking OFF');
                // Остановка водопада и очистка окна при сбросе сопровождения
                if (window._bottomPanel && typeof window._bottomPanel.stopWaterfallAndClear === 'function') {
                    window._bottomPanel.stopWaterfallAndClear();
                }
                if (window._bottomPanel && typeof window._bottomPanel.showTab === 'function') {
                    window._bottomPanel.showTab('spectrum', false);
                }
                if (window.earthView) {
                    window.earthView.clearTrackingLayer();
                    window.earthView.draw();
                }
                if (window.skyView) {
                    window.skyView.setSatelliteInfo('', null);
                    window.skyView.setTrack([]);
                    window.skyView.setSatellitePosition(NaN, NaN);
                }
                // Сброс графиков азимута и угла места — очищаем данные и перерисовываем.
                if (window.azimuthIndicator) {
                    window.azimuthIndicator.setSatellitePosition(null);
                    window.azimuthIndicator.setNoradId(null);
                    window.azimuthIndicator.draw();
                }
                if (window.elevationIndicator) {
                    window.elevationIndicator.setSatellitePosition(null, null);
                    window.elevationIndicator.setNoradId(null);
                    window.elevationIndicator.draw();
                }
                loadedSkyPathTracking = null;
            }
        });

        // ── SATELLITE_GROUP_UPDATE: обновление вторичных ──
        sm.subscribe(StateEventType.SATELLITE_GROUP_UPDATE, function(group) {
            if (!group || !group.satellites) { return; }
            _updateSecondaryPositions();
            _updateSecondaryTracks();
        });

        // ── TRACK_VISIBILITY_CHANGE: toggle видимости трасс в таблице ──
        sm.subscribe(StateEventType.TRACK_VISIBILITY_CHANGE, function() {
            _updateSecondaryPositions();
            _updateSecondaryTracks();
            // Перезагружаем sky_path для вторичных, чтобы трассы с включённой видимостью появились на SkyView.
            _refreshSecondarySkyTracks();
            if (window.earthView) { window.earthView.draw(); }
            if (window.skyView) { window.skyView.draw(); }
        });

        // ── Backward compat: SATELLITE_CHANGE (legacy) — ничего не делаем ──
        // Логика перенесена в SELECTED_CHANGE / TRACKING_CHANGE.
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

        for (var i = 0; i < group.satellites.length; i++) {
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

        for (var i = 0; i < group.satellites.length; i++) {
            const sat = group.satellites[i];
            if (sat.norad_id === selectedId || sat.norad_id === trackingId) { continue; }
            const satState = sm.getState(sat.norad_id);
            if (satState && satState.track) {
                if (window.earthView) { window.earthView.setSecondaryTrack(sat.norad_id, satState.track); }
            }
        }
    }

    /** Обновить трассы вторичных спутников в SkyView из массива пролётов (sky_path → az/el). */
    function applySecondarySkyTracks(passes) {
        if (!passes || !window.skyView) { return; }
        const sm = window._stateManager;
        const group = sm && sm.getSatelliteGroup();
        if (!group || !group.satellites) { return; }

        const now = Date.now();
        const selectedId = sm.getSelectedSatelliteId();
        const trackingId = sm.getTrackingSatelliteId();

        for (var i = 0; i < group.satellites.length; i++) {
            const sat = group.satellites[i];
            if (sat.norad_id === selectedId || sat.norad_id === trackingId) { continue; }
            // SkyView: трассы только для спутников с isTrackVisible (PASS-MAP-001).
            if (!sm.isTrackVisible(sat.norad_id)) { continue; }
            var pass = null;
            for (var j = 0; j < passes.length; j++) {
                var p = passes[j];
                if (p.norad_id === sat.norad_id && ((p.aos <= now && now <= p.los) || now < p.aos)) {
                    pass = p;
                    break;
                }
            }
            if (pass && pass.sky_path && pass.sky_path.length > 0) {
                var track = pass.sky_path.map(function(pt) {
                    return { az: pt.az, el: pt.el, time: pt.time };
                });
                window.skyView.setSecondaryTrack(sat.norad_id, track);
            }
        }
    }

    /** Загрузить пролёты и применить трассы вторичных спутников на SkyView (при смене видимости трасс). */
    function _refreshSecondarySkyTracks() {
        fetch('/api/passes?hours=24')
            .then(function(resp) { return resp.json(); })
            .then(function(data) {
                if (data.passes && data.passes.length > 0) {
                    applySecondarySkyTracks(data.passes);
                    if (window.skyView) { window.skyView.draw(); }
                }
            })
            .catch(function() {});
    }

    // Загрузка sky path для SkyView (selected или tracking).
    function _loadSkyPath(noradId, target) {
        if (!noradId || !window.skyView) { return; }
        loadSkyPathForSatellite(noradId, target);
    }

    // Показ/скрытие оверлея при смене спутника
    let overlayTimer = null;
    function showTrackingOverlay(noradId, name) {
        const overlay = document.getElementById('tracking-overlay');
        if (!overlay) {return;}

        // Отменяем предыдущий таймер
        if (overlayTimer) {
            clearTimeout(overlayTimer);
            overlayTimer = null;
        }

        // Заполняем данные
        const noradEl = document.getElementById('tracking-overlay-norad');
        const nameEl = document.getElementById('tracking-overlay-name');
        if (noradEl) {noradEl.textContent = noradId || '---';}
        if (nameEl) {nameEl.textContent = name || '---';}

        // Показываем
        overlay.classList.remove('hidden', 'fade-out');

        // Скрываем через 3 секунды с плавным fade-out
        overlayTimer = setTimeout(function() {
            overlay.classList.add('fade-out');
            // После завершения анимации скрываем полностью
            setTimeout(function() {
                overlay.classList.add('hidden');
                overlay.classList.remove('fade-out');
            }, 600); // длительность transition в CSS
        }, 3000);
    }


    /**
     * Загрузка sky path для SkyView.
     * @param {number} noradId
     * @param {string} target — 'selected' или 'tracking'
     */
    function loadSkyPathForSatellite(noradId, target) {
        if (!noradId || !window.skyView) { return; }

        fetch('/api/passes?hours=24')
            .then(function(resp) { return resp.json(); })
            .then(function(data) {
                if (!data.passes || data.passes.length === 0) { return; }

                const now = Date.now();
                let pass = null;
                for (let i = 0; i < data.passes.length; i++) {
                    const p = data.passes[i];
                    if (p.norad_id === noradId) {
                        if ((p.aos <= now && now <= p.los) || now < p.aos) {
                            pass = p;
                            break;
                        }
                    }
                }

                if (pass && pass.sky_path && pass.sky_path.length > 0) {
                    const track = pass.sky_path.map(function(point) {
                        return { az: point.az, el: point.el, time: point.time };
                    });

                    if (target === 'tracking') {
                        window.skyView.setTrack(track);
                        window.skyView.setPassTimes(pass.aos, pass.los);
                    } else {
                        window.skyView.setSelectedTrack(track);
                        window.skyView.setSelectedPassTimes(pass.aos, pass.los);
                    }
                    console.log('[app.js] SkyView', target, 'track для', noradId, ':', track.length, 'точек');
                }

                // Обновляем трассы вторичных спутников в SkyView из того же ответа
                applySecondarySkyTracks(data.passes);
            })
            .catch(function(err) {
                console.error('[app.js] Ошибка загрузки sky path:', err);
            });
    }

    // Инициализация нижней панели: вкладки + водопад
    function initBottomPanel() {
        if (window._bottomPanel) {
            window._bottomPanel.destroy();
            window._bottomPanel = null;
        }
        if (document.getElementById('bottom-panel-body') && typeof window.BottomPanel === 'function') {
            window._bottomPanel = new window.BottomPanel();
            // Синхронизация водопада с текущим сопровождением (например после HTMX-навигации)
            if (window._stateManager && window._stateManager.getTrackingSatelliteId() && typeof window._bottomPanel.startWaterfall === 'function') {
                window._bottomPanel.startWaterfall();
            }
        }
    }

    // Инициализация компактной таблицы пролётов в правой панели
    function initRightPanel() {
        if (window._rightPanelTable) {
            window._rightPanelTable.destroy();
            window._rightPanelTable = null;
        }
        var tbody = document.getElementById('passes-compact-body');
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
            window.earthView.init().then(function() {
                // Загрузка координат наблюдателя из конфигурации сервера
                return fetch('/api/config').then(function(resp) { return resp.json(); });
            }).then(function(cfg) {
                if (cfg && cfg.observer) {
                    window.earthView.setObserver(cfg.observer.lon, cfg.observer.lat, 'Ростов-на-Дону');
                }
                // Подтягиваем накопленные данные из StateManager (track/position могли прийти до init).
                // Используем selected-слой: tracking-слой устанавливается только когда пользователь
                // нажимает «Сопровождение» и бэкенд присылает satellite_group_update с tracking_id.
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
                            window.earthView.setSelectedSatellitePosition(selState.position.lon, selState.position.lat, selState.position.alt);
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
                            if (trkState.track)     { window.earthView.setGroundTrack(trkState.track); }
                            if (trkState.position)  {
                                window.earthView.setSatellitePosition(trkState.position.lon, trkState.position.lat, trkState.position.alt);
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

            // Панель информации под графиком: AOS, LOS, Длит., Осталось 
            window.skyView.setInfoElements({
                aos: 'skyview-info-aos',
                los: 'skyview-info-los',
                dur: 'skyview-info-dur',
                remaining: 'skyview-info-remaining'
            });

            // Canvas SkyView всегда 300×300 px — квадратный буфер, окружность без искажений
            var skySize = 300;
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

        // Azimuth indicator — данные с SSE; панель информации отдельно под canvas
        const azCanvas = document.getElementById('azimuth-view');
        if (azCanvas && window.AzimuthIndicator) {
            window.azimuthIndicator = new window.AzimuthIndicator(azCanvas);
            window.azimuthIndicator.setInfoElements({ ant: 'az-info-ant', sat: 'az-info-sat' });
            const azWrap = azCanvas.parentElement;
            if (azWrap && typeof ResizeObserver !== 'undefined') {
                const syncAzSize = function() {
                    const w = azWrap.clientWidth;
                    const h = azWrap.clientHeight;
                    if (w > 0 && h > 0 && window.azimuthIndicator) {
                        window.azimuthIndicator.resize(w, h);
                    }
                };
                const azRo = new ResizeObserver(syncAzSize);
                azRo.observe(azWrap);
                syncAzSize();
            } else {
                window.azimuthIndicator.draw();
            }
        }

        // Elevation indicator — данные с SSE; панель информации отдельно под canvas
        const elCanvas = document.getElementById('elevation-view');
        if (elCanvas && window.ElevationIndicator) {
            window.elevationIndicator = new window.ElevationIndicator(elCanvas);
            window.elevationIndicator.setInfoElements({ ant: 'el-info-ant', sat: 'el-info-sat' });
            const elWrap = elCanvas.parentElement;
            if (elWrap && typeof ResizeObserver !== 'undefined') {
                const syncElSize = function() {
                    const w = elWrap.clientWidth;
                    const h = elWrap.clientHeight;
                    if (w > 0 && h > 0 && window.elevationIndicator) {
                        window.elevationIndicator.resize(w, h);
                    }
                };
                const elRo = new ResizeObserver(syncElSize);
                elRo.observe(elWrap);
                syncElSize();
            } else {
                window.elevationIndicator.draw();
            }
        }

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

        // Инициализация таблицы пролётов, если мы на вкладке /passes
        if (typeof window.initPassesTable === 'function') {
            const passesContainer = document.getElementById('passes-table-container');
            if (passesContainer) {
                window.initPassesTable();
            }
        }

        initRightPanel();
        initBottomPanel();
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

        var frameCount = 0;
        function animate() {
            if (window.skyView && window._stateManager) {
                var sm = window._stateManager;
                var trackingId = sm.getTrackingSatelliteId();
                var selectedId = sm.getSelectedSatelliteId();
                // Позиция сопровождаемого — всегда из состояния сопровождаемого (не из active/selected)
                if (trackingId) {
                    var trkState = sm.getState(trackingId);
                    if (trkState && trkState.position && trkState.position.az != null && trkState.position.el != null) {
                        window.skyView.setSatellitePosition(trkState.position.az, trkState.position.el);
                    }
                }
                // Позиция текущего (выбранного) — из состояния выбранного спутника
                if (selectedId) {
                    var selState = sm.getState(selectedId);
                    if (selState && selState.position && selState.position.az != null && selState.position.el != null) {
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
