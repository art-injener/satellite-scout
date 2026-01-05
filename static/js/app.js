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

        // Azimuth placeholder
        const azCanvas = document.getElementById('azimuth-view');
        if (azCanvas) {
            drawCompassPlaceholder(azCanvas);
        }

        // Elevation placeholder
        const elCanvas = document.getElementById('elevation-view');
        if (elCanvas) {
            drawElevationPlaceholder(elCanvas);
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

    // Draw compass placeholder for azimuth
    function drawCompassPlaceholder(canvas) {
        const ctx = canvas.getContext('2d');
        const w = canvas.width;
        const h = canvas.height;
        const cx = w / 2;
        const cy = h / 2;
        const r = Math.min(w, h) / 2 - 10;

        // Background
        ctx.fillStyle = '#0a0e14';
        ctx.fillRect(0, 0, w, h);

        // Circle
        ctx.strokeStyle = '#2a3444';
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(cx, cy, r, 0, Math.PI * 2);
        ctx.stroke();

        // Cardinal directions
        ctx.fillStyle = '#5c6370';
        ctx.font = '12px Inter, sans-serif';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillText('N', cx, cy - r + 15);
        ctx.fillText('S', cx, cy + r - 15);
        ctx.fillText('E', cx + r - 15, cy);
        ctx.fillText('W', cx - r + 15, cy);

        // Cross
        ctx.strokeStyle = '#1a2030';
        ctx.beginPath();
        ctx.moveTo(cx, cy - r + 25);
        ctx.lineTo(cx, cy + r - 25);
        ctx.moveTo(cx - r + 25, cy);
        ctx.lineTo(cx + r - 25, cy);
        ctx.stroke();
    }

    // Draw elevation gauge placeholder
    function drawElevationPlaceholder(canvas) {
        const ctx = canvas.getContext('2d');
        const w = canvas.width;
        const h = canvas.height;
        const cx = w / 2;
        const cy = h - 20;
        const r = Math.min(w, h) - 30;

        // Background
        ctx.fillStyle = '#0a0e14';
        ctx.fillRect(0, 0, w, h);

        // Arc (half circle)
        ctx.strokeStyle = '#2a3444';
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(cx, cy, r / 2, Math.PI, 0);
        ctx.stroke();

        // Tick marks
        ctx.strokeStyle = '#1a2030';
        ctx.fillStyle = '#5c6370';
        ctx.font = '10px Inter, sans-serif';
        ctx.textAlign = 'center';
        
        for (let deg = 0; deg <= 90; deg += 30) {
            const angle = Math.PI - (deg * Math.PI / 180);
            const x1 = cx + (r / 2 - 5) * Math.cos(angle);
            const y1 = cy + (r / 2 - 5) * Math.sin(angle);
            const x2 = cx + (r / 2 + 5) * Math.cos(angle);
            const y2 = cy + (r / 2 + 5) * Math.sin(angle);
            
            ctx.beginPath();
            ctx.moveTo(x1, y1);
            ctx.lineTo(x2, y2);
            ctx.stroke();

            const tx = cx + (r / 2 + 15) * Math.cos(angle);
            const ty = cy + (r / 2 + 15) * Math.sin(angle);
            ctx.fillText(deg + '°', tx, ty);
        }

        // Label
        ctx.fillStyle = '#3c4350';
        ctx.font = '10px Inter, sans-serif';
        ctx.fillText('EL: --°', cx, cy + 10);
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
    document.body.addEventListener('htmx:afterSwap', function(evt) {
        // Reinitialize canvas after HTMX swap
        initCanvasPlaceholders();
    });

    // Expose for debugging
    window.SatWatch = {
        setConnected: setConnected
    };

})();
