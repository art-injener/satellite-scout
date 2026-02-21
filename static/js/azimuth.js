// Azimuth Indicator - Индикатор азимута с антенной
// Использует AntennaDrawing для отрисовки антенны
// Стрелка спутника: тонкая пунктирная линия с точками (задний план)

(function() {
    'use strict';

    /**
     * Класс индикатора азимута
     * @param {HTMLCanvasElement} canvas - Canvas элемент для отрисовки
     */
    function AzimuthIndicator(canvas) {
        this.canvas = canvas;

        // Настройка HiDPI canvas
        var logicalWidth = parseInt(canvas.getAttribute('width'), 10);
        var logicalHeight = parseInt(canvas.getAttribute('height'), 10);

        if (window.CanvasUtils) {
            this.ctx = window.CanvasUtils.setupHiDPICanvas(canvas, logicalWidth, logicalHeight);
        } else {
            this.ctx = canvas.getContext('2d');
        }

        this.centerX = logicalWidth / 2;
        this.centerY = logicalHeight / 2;
        this.radius = Math.min(logicalWidth, logicalHeight) / 2 - 25;
        this.currentAzimuth = 0;

        // Позиция спутника (null = нет данных)
        this.satelliteAzimuth = null;
        // Видимость спутника
        this.isVisible = true;

        // Цвета
        this.colors = {
            bgPrimary: '#0a0e14',
            bgSecondary: '#12171f',
            border: '#2a3444',
            accent: '#00d4aa',
            accentBlue: '#00a8ff',
            accentRed: '#ff6b6b',
            textPrimary: '#e6e8eb',
            textSecondary: '#8b919a',
            textMuted: '#5c6370',
            satelliteLine: 'rgba(255, 255, 255, 0.5)',
            satelliteMarker: '#ffffff',
            outOfView: 'rgba(255, 107, 107, 0.7)'
        };

        this.antennaScale = this.radius / 100 * 0.95;
    }

    AzimuthIndicator.prototype.degToRad = function(deg) {
        return deg * Math.PI / 180;
    };

    /**
     * Отрисовка лимба (полный круг 360°)
     */
    AzimuthIndicator.prototype.drawLimb = function() {
        var ctx = this.ctx;
        var cx = this.centerX;
        var cy = this.centerY;
        var r = this.radius;

        ctx.beginPath();
        ctx.arc(cx, cy, r, 0, Math.PI * 2);
        ctx.strokeStyle = this.colors.accentBlue;
        ctx.lineWidth = 2;
        ctx.stroke();

        ctx.beginPath();
        ctx.arc(cx, cy, r - 18, 0, Math.PI * 2);
        ctx.strokeStyle = this.colors.border;
        ctx.lineWidth = 2;
        ctx.stroke();

        ctx.font = '11px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        for (var deg = 0; deg < 360; deg += 15) {
            var rad = this.degToRad(deg - 90);
            var isMain = deg % 30 === 0;
            var innerR = isMain ? r - 15 : r - 10;
            var outerR = r - 2;

            ctx.beginPath();
            ctx.moveTo(
                cx + Math.cos(rad) * innerR,
                cy + Math.sin(rad) * innerR
            );
            ctx.lineTo(
                cx + Math.cos(rad) * outerR,
                cy + Math.sin(rad) * outerR
            );
            ctx.strokeStyle = isMain ? this.colors.accentBlue : this.colors.border;
            ctx.lineWidth = isMain ? 2 : 1;
            ctx.stroke();

            if (isMain) {
                var labelR = r + 14;
                var label = deg.toString();

                if (deg === 0) { label = 'N'; }
                else if (deg === 90) { label = 'E'; }
                else if (deg === 180) { label = 'S'; }
                else if (deg === 270) { label = 'W'; }

                ctx.fillStyle = (deg % 90 === 0) ? this.colors.textPrimary : this.colors.textSecondary;
                ctx.fillText(
                    label,
                    cx + Math.cos(rad) * labelR,
                    cy + Math.sin(rad) * labelR
                );
            }
        }
    };

    /**
     * Стрелка спутника: тонкая пунктирная линия с точками из центра + маркер на лимбе
     * Рисуется на заднем плане (до антенны)
     */
    AzimuthIndicator.prototype.drawSatellitePointer = function() {
        if (this.satelliteAzimuth === null || !this.isVisible) { return; }

        var ctx = this.ctx;
        var cx = this.centerX;
        var cy = this.centerY;
        var r = this.radius;
        var rad = this.degToRad(this.satelliteAzimuth - 90);
        var endX = cx + Math.cos(rad) * (r - 20);
        var endY = cy + Math.sin(rad) * (r - 20);

        // === НАСТРОЙКИ СТРЕЛКИ СПУТНИКА ===
        var lineWidth = 2;          // Толщина пунктирной линии
        var dashPattern = [6, 4, 2, 4]; // Паттерн: [dash, gap, dot, gap]
        var markerRadius = 6;         // Радиус маркера на лимбе
        var markerLineWidth = 2;      // Толщина контура маркера
        // ================================

        ctx.save();
        ctx.beginPath();
        ctx.moveTo(cx, cy);
        ctx.lineTo(endX, endY);
        ctx.strokeStyle = this.colors.satelliteLine;
        ctx.lineWidth = lineWidth;
        ctx.setLineDash(dashPattern);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.restore();

        // Маркер-кольцо на лимбе (без заливки)
        var markerR = r - 9;
        var mx = cx + Math.cos(rad) * markerR;
        var my = cy + Math.sin(rad) * markerR;

        ctx.beginPath();
        ctx.arc(mx, my, markerRadius, 0, Math.PI * 2);
        ctx.strokeStyle = this.colors.satelliteMarker;
        ctx.lineWidth = markerLineWidth;
        ctx.stroke();
    };

    /**
     * Сообщение «ВНЕ ЗОНЫ НАБЛЮДЕНИЯ» по центру графика
     */
    AzimuthIndicator.prototype._drawOutOfViewMessage = function() {
        var ctx = this.ctx;
        var cx = this.centerX;
        var cy = this.centerY;

        ctx.save();
        ctx.font = 'bold 11px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillStyle = this.colors.outOfView;
        ctx.fillText('ВНЕ ЗОНЫ', cx, cy - 8);
        ctx.fillText('НАБЛЮДЕНИЯ', cx, cy + 8);
        ctx.restore();
    };

    /**
     * Отрисовка антенны с использованием общей функции
     */
    AzimuthIndicator.prototype.drawAntenna = function(azimuth) {
        window.AntennaDrawing.draw(
            this.ctx,
            this.centerX,
            this.centerY,
            azimuth,
            this.antennaScale,
            this.radius - 9,
            'azimuth'
        );
    };

    /**
     * Главная функция отрисовки
     */
    AzimuthIndicator.prototype.draw = function() {
        var ctx = this.ctx;
        var size = window.CanvasUtils ?
            window.CanvasUtils.getLogicalSize(this.canvas) :
            { width: this.canvas.width, height: this.canvas.height };

        ctx.fillStyle = this.colors.bgPrimary;
        ctx.fillRect(0, 0, size.width, size.height);

        this.drawLimb();

        // Стрелка спутника на заднем плане (под антенной)
        this.drawSatellitePointer();

        this.drawPlatformBase();
        this.drawAntenna(this.currentAzimuth);

        if (!this.isVisible) {
            this._drawOutOfViewMessage();
        }
    };

    /**
     * Отрисовка основания платформы (шестигранник)
     */
    AzimuthIndicator.prototype.platformBaseConfig = {
        radius: 40,
        lineWidth: 1,
        useDash: false,
        dashPattern: [5, 5]
    };

    AzimuthIndicator.prototype.drawPlatformBase = function() {
        var ctx = this.ctx;
        var cx = this.centerX;
        var cy = this.centerY;
        var cfg = this.platformBaseConfig;

        ctx.strokeStyle = this.colors.accent;
        ctx.lineWidth = cfg.lineWidth;

        if (cfg.useDash) {
            ctx.setLineDash(cfg.dashPattern);
        }

        ctx.beginPath();
        for (var i = 0; i < 6; i++) {
            var angle = (i * 60 + 30) * Math.PI / 180;
            var x = cx + Math.cos(angle) * cfg.radius;
            var y = cy + Math.sin(angle) * cfg.radius;
            if (i === 0) {
                ctx.moveTo(x, y);
            } else {
                ctx.lineTo(x, y);
            }
        }
        ctx.closePath();
        ctx.stroke();

        ctx.setLineDash([]);
    };

    /**
     * Установка азимута антенны и перерисовка
     */
    AzimuthIndicator.prototype.setAzimuth = function(deg) {
        this.currentAzimuth = ((deg % 360) + 360) % 360;
        this.draw();
    };

    /**
     * Установка позиции спутника
     * @param {number|null} az - азимут спутника (null = нет данных)
     */
    AzimuthIndicator.prototype.setSatellitePosition = function(az) {
        this.satelliteAzimuth = (az !== null && az !== undefined)
            ? ((az % 360) + 360) % 360
            : null;
    };

    /**
     * Установка видимости спутника
     * @param {boolean} visible
     */
    AzimuthIndicator.prototype.setVisible = function(visible) {
        this.isVisible = visible;
    };

    AzimuthIndicator.prototype.getAzimuth = function() {
        return this.currentAzimuth;
    };

    window.AzimuthIndicator = AzimuthIndicator;

})();
