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

        sm.subscribe(StateEventType.POSITION, function(state) {
            console.log('[app.js] Получено событие POSITION:', state);
            var pos = state.position;
            if (!pos) { 
                console.warn('[app.js] POSITION: нет данных позиции');
                return; 
            }

            console.log('[app.js] POSITION данные:', {
                lat: pos.lat,
                lon: pos.lon,
                alt: pos.alt,
                az: pos.az,
                el: pos.el
            });

            if (window.earthView) {
                window.earthView.setSatellitePosition(pos.lon, pos.lat, pos.alt);
                window.earthView.setSatelliteInfo(state.name || '', state.noradId);
                if (state.visibilityZone && state.visibilityZone.points) {
                    window.earthView.setVisibilityZone(state.visibilityZone.points);
                }
                window.earthView.draw();
                window.earthView.updateInfoPanel(pos.ts || Date.now());
            }
            if (window.skyView) {
                window.skyView.setSatellitePosition(pos.az, pos.el);
                window.skyView.draw();
            }
            if (window.azimuthIndicator) {
                window.azimuthIndicator.setAzimuth(pos.az);
                window.azimuthIndicator.draw();
            }
            if (window.elevationIndicator) {
                // Передаём азимут и угол места для определения полусферы (W/E)
                window.elevationIndicator.setPosition(pos.az, pos.el);
            }
        });

        sm.subscribe(StateEventType.TRACK, function(state) {
            if (window.earthView && state.track) {
                window.earthView.setGroundTrack(state.track);
                window.earthView.draw();
            }
        });
    }

    // Initialize canvas elements with placeholder content
    function initCanvasPlaceholders() {
        ensureSSEAndSubscriptions();

        // Earth View — карта мира, данные с SSE
        var earthCanvas = document.getElementById('earth-view');
        if (earthCanvas && window.EarthView) {
            if (window.earthView) {
                window.earthView.stopDemo();
            }
            window.earthView = new window.EarthView(earthCanvas);
            window.earthView.init().then(function() {
                // Загрузка координат наблюдателя из конфигурации сервера
                return fetch('/api/config').then(function(resp) { return resp.json(); });
            }).then(function(cfg) {
                if (cfg && cfg.observer) {
                    window.earthView.setObserver(cfg.observer.lon, cfg.observer.lat, 'Rostov-on-Don');
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
                        if (state.visibilityZone && state.visibilityZone.points) {
                            window.earthView.setVisibilityZone(state.visibilityZone.points);
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
            if (window.skyView) {
                window.skyView.stopDemo();
            }
            window.skyView = new window.SkyView(skyCanvas);
            window.skyView.draw();
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

    // Expose for debugging
    window.SatWatch = {
        setConnectionStatus: setConnectionStatus
    };

})();
