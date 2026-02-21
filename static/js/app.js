// SatWatch - Main Application Script

(function() {
    'use strict';

    // Индикатор статуса SSE: три состояния (подключено / подключение / отключено)
    const connectionStatusEl = document.getElementById('connection-status');
    const connectionStatusLabel = document.getElementById('connection-status-label');

    function setConnectionStatus(status) {
        if (!connectionStatusEl) { return; }

        connectionStatusEl.classList.remove('sse-status--connected', 'sse-status--connecting', 'sse-status--disconnected');

        var label = 'Отключено';
        var title = 'Поток данных: отключено';

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

        // Initialize canvas placeholders
        initCanvasPlaceholders();

        // Инициализация InfoPanel (вкладки спутников)
        var infoPanelEl = document.getElementById('info-panel');
        if (infoPanelEl && typeof window.InfoPanel === 'function') {
            window._infoPanel = new window.InfoPanel(infoPanelEl);
        }

        // Часы в station-footer: UTC и местное время (обновление каждую секунду)
        var sfUtc = document.getElementById('sf-utc');
        var sfLocal = document.getElementById('sf-local');
        if (sfUtc || sfLocal) {
            var pad2 = function(n) { return n < 10 ? '0' + n : '' + n; };
            var updateFooterClocks = function() {
                var now = new Date();
                if (sfUtc) {
                    sfUtc.textContent = 'UTC ' + pad2(now.getUTCHours()) + ':' + pad2(now.getUTCMinutes()) + ':' + pad2(now.getUTCSeconds());
                }
                if (sfLocal) {
                    var tz = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
                    var tzShort = tz.split('/').pop().replace(/_/g, ' ');
                    sfLocal.textContent = tzShort + ' ' + pad2(now.getHours()) + ':' + pad2(now.getMinutes()) + ':' + pad2(now.getSeconds());
                }
            };
            updateFooterClocks();
            setInterval(updateFooterClocks, 1000);
        }

        // Инициализация таблицы пролётов, если мы на вкладке /passes
        if (typeof initPassesTable === 'function') {
            var passesContainer = document.getElementById('passes-table-container');
            if (passesContainer) {
                initPassesTable();
            }
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
            var s = evt.status;
            if (s === window.SSEConnectionStatus.CONNECTED) {
                setConnectionStatus('connected');
            } else if (s === window.SSEConnectionStatus.CONNECTING) {
                setConnectionStatus('connecting');
            } else {
                setConnectionStatus('disconnected');
            }
        });
        
        console.log('[app.js] Подключение к SSE...');
        window._sseClient.connect();

        var sm = window._stateManager;
        var StateEventType = window.StateEventType;

        // Запоминаем для какого спутника уже загружен sky path
        var loadedSkyPathForNoradId = null;
        
        sm.subscribe(StateEventType.POSITION, function(state) {
            var pos = state.position;
            if (!pos) { 
                return; 
            }

            // Обновление EarthView
            if (window.earthView) {
                window.earthView.setSatellitePosition(pos.lon, pos.lat, pos.alt);
                window.earthView.setSatelliteInfo(state.name || '', state.noradId);
                if (state.visibilityZone && state.visibilityZone.segments) {
                    window.earthView.setVisibilityZone(state.visibilityZone.segments);
                }
                window.earthView.draw();
                window.earthView.updateInfoPanel(pos.ts || Date.now());
            }
            
            // Обновление SkyView
            if (window.skyView) {
                window.skyView.setSatelliteInfo(state.name || '');
                window.skyView.setSatellitePosition(pos.az, pos.el);
                
                // Загружаем sky path если ещё не загружен для этого спутника
                if (state.noradId && loadedSkyPathForNoradId !== state.noradId) {
                    loadedSkyPathForNoradId = state.noradId;
                    loadSkyPathForSatellite(state.noradId);
                }
            }
            
            // Обновление индикаторов: позиция спутника + перерисовка
            if (window.azimuthIndicator) {
                window.azimuthIndicator.setSatellitePosition(pos.az);
                window.azimuthIndicator.setAzimuth(pos.az);
            }
            if (window.elevationIndicator) {
                window.elevationIndicator.setSatellitePosition(pos.el, pos.az);
                window.elevationIndicator.setPosition(pos.az, pos.el);
            }

            // Обновление DOM инфо-панелей Az/El
            updateAzElInfoPanels(pos);
            
            // Обновление info panel
            updateInfoPanel(state);
        });

        sm.subscribe(StateEventType.TRACK, function(state) {
            if (window.earthView && state.track) {
                window.earthView.setGroundTrack(state.track);
                window.earthView.draw();
            }
        });
        
        // При смене спутника: оверлей + загрузка sky path + орбитальные параметры
        sm.subscribe(StateEventType.SATELLITE_CHANGE, function(state) {
            console.log('[app.js] Смена спутника:', state.noradId, state.name);
            loadedSkyPathForNoradId = state.noradId;
            loadSkyPathForSatellite(state.noradId);
            showTrackingOverlay(state.noradId, state.name || '');
            updateOrbitalParams(state);
        });
    }
    
    // Показ/скрытие оверлея при смене спутника
    var overlayTimer = null;
    function showTrackingOverlay(noradId, name) {
        var overlay = document.getElementById('tracking-overlay');
        if (!overlay) return;
        
        // Отменяем предыдущий таймер
        if (overlayTimer) {
            clearTimeout(overlayTimer);
            overlayTimer = null;
        }
        
        // Заполняем данные
        var noradEl = document.getElementById('tracking-overlay-norad');
        var nameEl = document.getElementById('tracking-overlay-name');
        if (noradEl) noradEl.textContent = noradId || '---';
        if (nameEl) nameEl.textContent = name || '---';
        
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
    
    // Обновление DOM инфо-панелей под графиками Az/El
    function updateAzElInfoPanels(pos) {
        var azSat = document.getElementById('az-satellite');
        var elSat = document.getElementById('el-satellite');
        var azRot = document.getElementById('az-rotator');
        var elRot = document.getElementById('el-rotator');
        
        if (azSat) { azSat.textContent = pos.az.toFixed(1) + '°'; }
        if (elSat) { elSat.textContent = pos.el.toFixed(1) + '°'; }
        
        // TODO: Временно дублируем значения спутника для антенны.
        // После реализации ROTATOR-003 (SSE rotator_position) заменить на реальные данные антенны.
        if (azRot) { azRot.textContent = pos.az.toFixed(1) + '°'; }
        if (elRot) { elRot.textContent = pos.el.toFixed(1) + '°'; }
    }

    // Обновление InfoPanel данными спутника (id с префиксом ip-)
    function updateInfoPanel(state) {
        var pos = state.position;
        if (!pos) return;
        
        var el = function(id) { return document.getElementById(id); };
        
        if (el('ip-norad')) el('ip-norad').textContent = state.noradId || '--';
        if (el('ip-name')) el('ip-name').textContent = state.name || '--';
        
        if (el('ip-lat')) el('ip-lat').textContent = pos.lat ? pos.lat.toFixed(2) + '°' : '---.--°';
        if (el('ip-lon')) el('ip-lon').textContent = pos.lon ? pos.lon.toFixed(2) + '°' : '---.--°';
        if (el('ip-alt')) el('ip-alt').textContent = pos.alt ? pos.alt.toFixed(0) + ' km' : '--- km';
    }
    
    // Обновление орбитальных параметров
    function updateOrbitalParams(state) {
        var el = function(id) { return document.getElementById(id); };
        
        if (el('ip-incl') && typeof state.inclination === 'number') {
            el('ip-incl').textContent = state.inclination.toFixed(2) + '°';
        }
        
        if (el('ip-period') && typeof state.period === 'number') {
            var period = state.period;
            if (period >= 60) {
                var hours = Math.floor(period / 60);
                var mins = Math.round(period % 60);
                el('ip-period').textContent = hours + 'h ' + mins + 'm';
            } else {
                el('ip-period').textContent = period.toFixed(1) + ' min';
            }
        }
    }
    
    // Загрузка sky path для SkyView при смене спутника
    function loadSkyPathForSatellite(noradId) {
        if (!noradId || !window.skyView) return;
        
        // Запрашиваем пролёты и ищем ближайший для этого спутника
        fetch('/api/passes?hours=24')
            .then(function(resp) { return resp.json(); })
            .then(function(data) {
                if (!data.passes || data.passes.length === 0) return;
                
                var now = Date.now();
                // Ищем активный или ближайший пролёт для этого спутника
                var pass = null;
                for (var i = 0; i < data.passes.length; i++) {
                    var p = data.passes[i];
                    if (p.norad_id === noradId) {
                        // Активный пролёт (сейчас виден) или ближайший предстоящий
                        if ((p.aos <= now && now <= p.los) || now < p.aos) {
                            pass = p;
                            break;
                        }
                    }
                }
                
                if (pass && pass.sky_path && pass.sky_path.length > 0) {
                    // API возвращает точки с готовыми az/el/time
                    var track = pass.sky_path.map(function(point) {
                        return {
                            az: point.az,
                            el: point.el,
                            time: point.time  // Используем время из API
                        };
                    });
                    
                    window.skyView.setTrack(track);
                    window.skyView.setPassTimes(pass.aos, pass.los);
                    console.log('[app.js] SkyView track для', noradId, ':', track.length, 'точек');
                    console.log('[app.js] Первая точка:', track[0]);
                    console.log('[app.js] Последняя точка:', track[track.length-1]);
                    console.log('[app.js] Pass AOS:', new Date(pass.aos).toISOString(), 'LOS:', new Date(pass.los).toISOString());
                } else {
                    console.log('[app.js] Нет sky_path для пролёта', noradId, pass);
                }
            })
            .catch(function(err) {
                console.error('[app.js] Ошибка загрузки sky path:', err);
            });
    }

    // Initialize canvas elements with placeholder content
    function initCanvasPlaceholders() {
        ensureSSEAndSubscriptions();

        // Earth View — карта мира, данные с SSE
        var earthCanvas = document.getElementById('earth-view');
        if (earthCanvas && window.EarthView) {
            window.earthView = new window.EarthView(earthCanvas);
            window.earthView.init().then(function() {
                // Загрузка координат наблюдателя из конфигурации сервера
                return fetch('/api/config').then(function(resp) { return resp.json(); });
            }).then(function(cfg) {
                if (cfg && cfg.observer) {
                    window.earthView.setObserver(cfg.observer.lon, cfg.observer.lat, 'Ростов-на-Дону');
                }
                // Подтягиваем накопленные данные из StateManager (track/position могли прийти до init)
                if (window._stateManager) {
                    var state = window._stateManager.getActiveState();
                    if (state) {
                        if (state.track) {
                            window.earthView.setGroundTrack(state.track);
                        }
                        if (state.position) {
                            window.earthView.setSatellitePosition(state.position.lon, state.position.lat, state.position.alt);
                            window.earthView.setSatelliteInfo(state.name || '', state.noradId);
                        }
                        if (state.visibilityZone && state.visibilityZone.segments) {
                            window.earthView.setVisibilityZone(state.visibilityZone.segments);
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

        // Sky View — азимутальная проекция неба, данные с SSE
        var skyCanvas = document.getElementById('sky-view');
        if (skyCanvas && window.SkyView) {
            window.skyView = new window.SkyView(skyCanvas);
            window.skyView.draw();
            
            // Запуск цикла анимации для SkyView (плавная отрисовка)
            startSkyViewAnimation();
            
            // Загружаем sky path для текущего спутника, если он уже известен
            if (window._stateManager) {
                var state = window._stateManager.getActiveState();
                if (state && state.noradId) {
                    loadSkyPathForSatellite(state.noradId);
                }
            }
        } else if (skyCanvas) {
            drawPlaceholder(skyCanvas, '', 'Небесная сфера');
        }

        // Azimuth indicator — данные с SSE
        var azCanvas = document.getElementById('azimuth-view');
        if (azCanvas && window.AzimuthIndicator) {
            window.azimuthIndicator = new window.AzimuthIndicator(azCanvas);
            window.azimuthIndicator.draw();
        }

        // Elevation indicator — данные с SSE
        var elCanvas = document.getElementById('elevation-view');
        if (elCanvas && window.ElevationIndicator) {
            window.elevationIndicator = new window.ElevationIndicator(elCanvas);
            window.elevationIndicator.draw();
        }

        // Waterfall placeholder
        var wfCanvas = document.getElementById('waterfall');
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
        if (typeof initPassesTable === 'function') {
            var passesContainer = document.getElementById('passes-table-container');
            if (passesContainer) {
                initPassesTable();
            }
        }
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
    var skyViewAnimationId = null;
    
    function startSkyViewAnimation() {
        if (skyViewAnimationId) {
            cancelAnimationFrame(skyViewAnimationId);
        }
        
        function animate() {
            if (window.skyView) {
                window.skyView.draw();
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
