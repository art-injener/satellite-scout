// SatWatch - Main Application Script

(function() {
    'use strict';

    // Connection status management
    const connectionStatus = document.getElementById('connection-status');

    function setConnected(connected) {
        if (connectionStatus) {
            connectionStatus.textContent = connected ? '● Подключено' : '● Отключено';
            connectionStatus.classList.toggle('connected', connected);
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

    // Initialize canvas elements with placeholder content
    function initCanvasPlaceholders() {
        // Earth View placeholder
        const earthCanvas = document.getElementById('earth-view');
        if (earthCanvas) {
            drawPlaceholder(earthCanvas, 'Earth View', 'Карта мира появится здесь');
        }

        // Sky View placeholder
        const skyCanvas = document.getElementById('sky-view');
        if (skyCanvas) {
            drawPlaceholder(skyCanvas, 'Sky View', 'Небесная сфера');
        }

        // Azimuth indicator с антенной
        const azCanvas = document.getElementById('azimuth-view');
        if (azCanvas && window.AzimuthIndicator) {
            window.azimuthIndicator = new window.AzimuthIndicator(azCanvas);
            window.azimuthIndicator.draw();
            window.azimuthIndicator.enableMouseControl();
            window.azimuthIndicator.startDemo(0.3);
        }

        // Elevation indicator с антенной
        const elCanvas = document.getElementById('elevation-view');
        if (elCanvas && window.ElevationIndicator) {
            window.elevationIndicator = new window.ElevationIndicator(elCanvas);
            window.elevationIndicator.draw();
            window.elevationIndicator.enableMouseControl();
            window.elevationIndicator.startDemo(0.5);
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
        setConnected: setConnected
    };

})();
