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

        // Логические размеры canvas (из HTML-атрибутов, фиксированные)
        this.logicalWidth = parseInt(canvas.getAttribute('width'), 10);
        this.logicalHeight = parseInt(canvas.getAttribute('height'), 10);

        if (window.CanvasUtils) {
            this.ctx = window.CanvasUtils.setupHiDPICanvas(canvas, this.logicalWidth, this.logicalHeight);
            canvas.style.width = '';
            canvas.style.height = '';
        } else {
            this.ctx = canvas.getContext('2d');
        }

        // Высота инфо-панели внизу canvas (как в SkyView)
        this.infoPanelHeight = 30;

        this.centerX = this.logicalWidth / 2;
        this.centerY = (this.logicalHeight - this.infoPanelHeight) / 2;
        this.radius = Math.min(this.logicalWidth, this.logicalHeight - this.infoPanelHeight) / 2 - 25;
        this.currentAzimuth = 0;

        // Позиция спутника (null = нет данных)
        this.satelliteAzimuth = null;
        // NORAD ID спутника
        this.noradId = null;
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
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;

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

        for (let deg = 0; deg < 360; deg += 15) {
            const rad = this.degToRad(deg - 90);
            const isMain = deg % 30 === 0;
            const innerR = isMain ? r - 15 : r - 10;
            const outerR = r - 2;

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
                const labelR = r + 14;
                let label = deg.toString();

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

        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;
        const rad = this.degToRad(this.satelliteAzimuth - 90);
        // var endX = cx + Math.cos(rad) * (r - 20);
        // var endY = cy + Math.sin(rad) * (r - 20);

        // === НАСТРОЙКИ СТРЕЛКИ СПУТНИКА ===
        // var lineWidth = 2;            // Толщина пунктирной линии
        // var dashPattern = [6, 4];     // Пунктир
        const markerRadius = 6; // Радиус маркера на лимбе
        const markerLineWidth = 2; // Толщина контура маркера
        // ================================

        // ctx.save();
        // ctx.beginPath();
        // ctx.moveTo(cx, cy);
        // ctx.lineTo(endX, endY);
        // ctx.strokeStyle = this.colors.satelliteLine;
        // ctx.lineWidth = lineWidth;
        // ctx.setLineDash(dashPattern);
        // ctx.stroke();
        // ctx.setLineDash([]);
        // ctx.restore();

        // Маркер-кольцо на лимбе (без заливки)
        const markerR = r - 9;
        const mx = cx + Math.cos(rad) * markerR;
        const my = cy + Math.sin(rad) * markerR;

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
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;

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
            this.radius - 18,
            'azimuth'
        );
    };

    /**
     * Информационная панель внизу canvas — 3 колонки в одну строку
     * Колонка 1: NORAD ID
     * Колонка 2: Az ант.
     * Колонка 3: Az КА
     */
    AzimuthIndicator.prototype._drawInfo = function() {
        const ctx = this.ctx;
        const w = this.logicalWidth;
        const h = this.logicalHeight;
        const panelHeight = this.infoPanelHeight;
        const panelY = h - panelHeight;

        const panelPadding = 6;
        const cornerRadius = 6;

        ctx.beginPath();
        if (ctx.roundRect) {
            ctx.roundRect(panelPadding, panelY + 2, w - panelPadding * 2, panelHeight - 4, cornerRadius);
        } else {
            ctx.rect(panelPadding, panelY + 2, w - panelPadding * 2, panelHeight - 4);
        }
        ctx.fillStyle = 'rgba(20, 30, 45, 0.9)';
        ctx.fill();
        ctx.strokeStyle = '#006666';
        ctx.lineWidth = 1;
        ctx.stroke();

        const rowY = panelY + panelHeight / 2;

        // 3 колонки с фиксированными позициями
        const col1X = panelPadding + 10; // NORAD слева
        const col2X = col1X + 70; // Az ант. (отступ 70px от NORAD, было 90px)
        const col3X = col2X + 110; // Az КА (отступ 110px от Az ант., было 115px)

        ctx.font = 'bold 11px monospace';
        ctx.textBaseline = 'middle';

        // Колонка 1: NORAD ID (слева)
        ctx.textAlign = 'left';
        ctx.fillStyle = '#ffffff';
        ctx.fillText('ID:', col1X, rowY);
        ctx.fillStyle = '#00d4aa';
        const noradText = this.noradId ? String(this.noradId) : '-----';
        ctx.fillText(noradText, col1X + ctx.measureText('ID:').width + 3, rowY); // +3px минимальный отступ

        // Колонка 2: Az ант. (центр-слева)
        ctx.textAlign = 'left';
        ctx.fillStyle = '#ffffff';
        ctx.fillText('Az ант.: ', col2X, rowY);
        ctx.fillStyle = '#00d4aa';
        const azAntVal = this.currentAzimuth !== null ? this.currentAzimuth.toFixed(1) + '°' : '---';
        ctx.fillText(azAntVal, col2X + ctx.measureText('Az ант.: ').width, rowY);

        // Колонка 3: Az КА (с отступом от col2)
        const azSatVal = this.satelliteAzimuth !== null ? this.satelliteAzimuth.toFixed(1) + '°' : '---';
        ctx.textAlign = 'left';
        ctx.fillStyle = '#ffffff';
        ctx.fillText('Az КА: ', col3X, rowY);
        ctx.fillStyle = '#00d4aa';
        ctx.fillText(azSatVal, col3X + ctx.measureText('Az КА: ').width, rowY);
    };

    /**
     * Главная функция отрисовки
     */
    AzimuthIndicator.prototype.draw = function() {
        const ctx = this.ctx;

        ctx.fillStyle = this.colors.bgPrimary;
        ctx.fillRect(0, 0, this.logicalWidth, this.logicalHeight);

        this.drawLimb();

        // Стрелка спутника на заднем плане (под антенной)
        this.drawSatellitePointer();

        this.drawPlatformBase();
        this.drawAntenna(this.currentAzimuth);

        if (!this.isVisible) {
            this._drawOutOfViewMessage();
        }

        this._drawInfo();
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
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const cfg = this.platformBaseConfig;

        ctx.strokeStyle = this.colors.accent;
        ctx.lineWidth = cfg.lineWidth;

        if (cfg.useDash) {
            ctx.setLineDash(cfg.dashPattern);
        }

        ctx.beginPath();
        for (let i = 0; i < 6; i++) {
            const angle = (i * 60 + 30) * Math.PI / 180;
            const x = cx + Math.cos(angle) * cfg.radius;
            const y = cy + Math.sin(angle) * cfg.radius;
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

    /**
     * Установка NORAD ID спутника
     * @param {number|string|null} noradId - NORAD ID
     */
    AzimuthIndicator.prototype.setNoradId = function(noradId) {
        this.noradId = noradId;
    };

    AzimuthIndicator.prototype.getAzimuth = function() {
        return this.currentAzimuth;
    };

    window.AzimuthIndicator = AzimuthIndicator;

})();
