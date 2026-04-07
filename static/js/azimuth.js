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

        // Круг на весь canvas (максимальный масштаб)
        this.infoPanelHeight = 0;
        this.centerX = this.logicalWidth / 2;
        this.centerY = this.logicalHeight / 2;
        this.radius = Math.min(this.logicalWidth, this.logicalHeight) / 2 - 25;
        this.currentAzimuth = 0;

        // Позиция спутника (null = нет данных)
        this.satelliteAzimuth = null;
        // NORAD ID спутника
        this.noradId = null;
        // Видимость спутника
        this.isVisible = true;

        this.colors = {
            bgPrimary:       cssVar('--ind-bg',               '#0c1420'),
            bgSecondary:     cssVar('--ind-bg-secondary',     '#182838'),
            border:          cssVar('--ind-border',           '#3a5060'),
            accent:          cssVar('--ind-accent',           '#7ab8d0'),
            antennaAccent:   cssVar('--ind-antenna', '#22a05a'),
            accentBlue:      cssVar('--ind-accent-blue',      '#86b8d4'),
            accentRed:       cssVar('--ind-accent-red',       '#d05545'),
            textPrimary:     cssVar('--ind-text',             '#c8d0d8'),
            textSecondary:   cssVar('--ind-text-secondary',   '#8a9aaa'),
            textMuted:       cssVar('--ind-text-muted',       '#708898'),
            labelMuted:      cssVar('--ind-label-muted',       '#d0d8e0'),
            satelliteLine:   themeRgba('ind-satellite-line', 'rgba(255, 255, 255, 0.5)'),
            satelliteMarker: cssVar('--ind-satellite-marker', '#ffffff'),
            outOfView:       themeRgba('ind-out-of-view',    'rgba(255, 107, 107, 0.7)')
        };

        this.antennaScale = this.radius / 100 * 0.95;

        this._infoEls = { ant: null, sat: null };
    }

    /**
     * Подстройка размера canvas под контейнер (квадрат по меньшей стороне)
     * @param {number} w - ширина
     * @param {number} h - высота
     */
    AzimuthIndicator.prototype.resize = function(w, h) {
        const size = Math.min(w, h);
        if (size <= 0) { return; }
        this.logicalWidth = size;
        this.logicalHeight = size;
        this.centerX = size / 2;
        this.centerY = size / 2;
        this.radius = size / 2 - 25;
        this.antennaScale = this.radius / 100 * 0.95;

        if (window.CanvasUtils) {
            this.ctx = window.CanvasUtils.setupHiDPICanvas(this.canvas, size, size);
        } else {
            this.canvas.width = size;
            this.canvas.height = size;
        }
        this.draw();
    };

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
        ctx.strokeStyle = this.colors.accentBlue;
        ctx.lineWidth = 1;
        ctx.stroke();

        ctx.font = '13px monospace';
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

                ctx.fillStyle = this.colors.labelMuted;
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
     * Привязка DOM-элементов панели информации (Az ант., Az КА)
     */
    AzimuthIndicator.prototype.setInfoElements = function(els) {
        const getEl = function(v) {
            if (!v) {return null;}
            return typeof v === 'string' ? document.getElementById(v) : v;
        };
        this._infoEls = { ant: getEl(els.ant), sat: getEl(els.sat) };
        this._updateInfoPanelDOM();
    };

    AzimuthIndicator.prototype._updateInfoPanelDOM = function() {
        const e = this._infoEls;
        if (!e.ant && !e.sat) {return;}
        const antStr = this.currentAzimuth !== null ? this.currentAzimuth.toFixed(1) + '°' : '---°';
        const satStr = this.satelliteAzimuth !== null ? this.satelliteAzimuth.toFixed(1) + '°' : '---°';
        if (e.ant) {e.ant.textContent = antStr;}
        if (e.sat) {e.sat.textContent = satStr;}
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

        this._updateInfoPanelDOM();
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

        ctx.strokeStyle = this.colors.antennaAccent;
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
        this._updateInfoPanelDOM();
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
