// Elevation Indicator - Индикатор угла места
// Использует AntennaDrawing для отрисовки антенны
// Шкала: 0° по краям (горизонт W/E), 90° в центре (зенит)
// - Левая зона (W): западная полусфера (азимут > 180° или az == 0°)
// - Правая зона (E): восточная полусфера (0° < азимут ≤ 180°)
// Стрелка спутника: тонкая пунктирная линия с точками (задний план)

(function() {
    'use strict';

    /**
     * Класс индикатора угла места
     * @param {HTMLCanvasElement} canvas - Canvas элемент для отрисовки
     */
    function ElevationIndicator(canvas) {
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
        this.infoPanelHeight = 0;

        this.centerX = this.logicalWidth / 2;
        this.radius = this.logicalWidth / 2 - 25;

        const topPadding = 24; // чтобы верх дуги и метка 90° не обрезались
        this.centerY = this.radius + topPadding + 5;

        // Позиция антенны
        this.currentElevation = 45;
        this.currentAzimuth = 270;

        // Позиция спутника (null = нет данных)
        this.satelliteElevation = null;
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

        // Вычисляем нижнюю точку постамента для позиционирования инфо-панели
        this._recalcPedestalBottom();
    }

    /**
     * Подстройка размера canvas под контейнер (квадрат по меньшей стороне)
     * @param {number} w - ширина
     * @param {number} h - высота
     */
    ElevationIndicator.prototype.resize = function(w, h) {
        const size = Math.min(w, h);
        if (size <= 0) { return; }
        this.logicalWidth = size;
        this.logicalHeight = size;
        this.centerX = size / 2;
        this.radius = size / 2 - 25;
        const topPadding = 24;
        this.centerY = this.radius + topPadding + 5;
        this.antennaScale = this.radius / 100 * 0.95;

        this._recalcPedestalBottom();

        if (window.CanvasUtils) {
            this.ctx = window.CanvasUtils.setupHiDPICanvas(this.canvas, size, size);
        } else {
            this.canvas.width = size;
            this.canvas.height = size;
        }
        this.draw();
    };

    /**
     * Пересчёт нижней точки постамента (для позиционирования инфо-панели)
     */
    ElevationIndicator.prototype._recalcPedestalBottom = function() {
        const s = this.antennaScale;
        const mountHeight = 16 * s;
        const outerArcR = mountHeight / 2 + 6 * s + 5 * s;
        const columnH = outerArcR + 45 * s;
        const flangeH = 4 * s;
        const trapH = 28 * s;
        this.pedestalBottom = this.centerY + columnH + flangeH + trapH;
        this.infoPanelY = this.pedestalBottom + 8;
    };

    ElevationIndicator.prototype.degToRad = function(deg) {
        return deg * Math.PI / 180;
    };

    /**
     * Определение полусферы по азимуту
     * @param {number} az - азимут (если не передан, используется currentAzimuth)
     * @returns {boolean} true = западная полусфера (левая зона)
     */
    ElevationIndicator.prototype.isWesternHemisphere = function(az) {
        const a = (az !== undefined) ? az : this.currentAzimuth;
        return a > 180 || a === 0;
    };

    /**
     * Отрисовка полулимба с двумя зонами
     */
    ElevationIndicator.prototype.drawLimb = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;

        ctx.beginPath();
        ctx.arc(cx, cy, r, Math.PI, 0, false);
        ctx.strokeStyle = this.colors.accentBlue;
        ctx.lineWidth = 2;
        ctx.stroke();

        ctx.beginPath();
        ctx.arc(cx, cy, r - 18, Math.PI, 0, false);
        ctx.strokeStyle = this.colors.accentBlue;
        ctx.lineWidth = 1;
        ctx.stroke();

        // Зенит = 90°
        ctx.beginPath();
        ctx.moveTo(cx, cy - r + 2);
        ctx.lineTo(cx, cy - r + 15);
        ctx.strokeStyle = this.colors.accentBlue;
        ctx.lineWidth = 2;
        ctx.stroke();

        ctx.font = '12px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        for (let i = 0; i <= 6; i++) {
            const scaleValue = i * 15;
            const labelValue = 90 - scaleValue;
            const isMain = scaleValue % 30 === 0;
            const innerR = isMain ? r - 15 : r - 10;
            const outerR = r - 2;

            const radLeft = Math.PI / 2 + scaleValue * Math.PI / 180;
            const radRight = Math.PI / 2 - scaleValue * Math.PI / 180;

            // Левая сторона
            ctx.beginPath();
            ctx.moveTo(cx + Math.cos(radLeft) * innerR, cy - Math.sin(radLeft) * innerR);
            ctx.lineTo(cx + Math.cos(radLeft) * outerR, cy - Math.sin(radLeft) * outerR);
            ctx.strokeStyle = isMain ? this.colors.accentBlue : this.colors.border;
            ctx.lineWidth = isMain ? 2 : 1;
            ctx.stroke();

            if (isMain) {
                const labelR = r + 12;
                const label = labelValue.toString() + '°';

                ctx.fillStyle = this.colors.labelMuted;
                ctx.fillText(label, cx + Math.cos(radLeft) * labelR, cy - Math.sin(radLeft) * labelR);
            }

            // Правая сторона (кроме 90° в центре)
            if (scaleValue > 0) {
                ctx.beginPath();
                ctx.moveTo(cx + Math.cos(radRight) * innerR, cy - Math.sin(radRight) * innerR);
                ctx.lineTo(cx + Math.cos(radRight) * outerR, cy - Math.sin(radRight) * outerR);
                ctx.strokeStyle = isMain ? this.colors.accentBlue : this.colors.border;
                ctx.lineWidth = isMain ? 2 : 1;
                ctx.stroke();

                if (isMain) {
                    const labelR2 = r + 12;
                    const label2 = labelValue.toString() + '°';

                    ctx.fillStyle = this.colors.labelMuted;
                    ctx.fillText(label2, cx + Math.cos(radRight) * labelR2, cy - Math.sin(radRight) * labelR2);
                }
            }
        }

        // W и E
        ctx.font = 'bold 11px monospace';
        ctx.fillStyle = this.colors.labelMuted;
        ctx.textAlign = 'center';
        ctx.fillText('W', cx - r - 12, cy + 15);
        ctx.fillText('E', cx + r + 12, cy + 15);
    };

    /**
     * Вычисление угла на полулимбе для elevation + azimuth
     * Возвращает угол в радианах от горизонтальной оси (PI..0, т.е. верхний полукруг)
     * @param {number} el - угол места 0-90°
     * @param {number} az - азимут 0-360°
     * @returns {number} угол в радианах для позиционирования на полулимбе
     */
    ElevationIndicator.prototype._elevationToLimbAngle = function(el, az) {
        const western = this.isWesternHemisphere(az);
        // 90° - el = угол наклона от вертикали
        const tilt = 90 - el;
        if (western) {
            // Левая сторона: PI/2 + tilt° от вертикали
            return Math.PI / 2 + tilt * Math.PI / 180;
        } else {
            // Правая сторона: PI/2 - tilt° от вертикали
            return Math.PI / 2 - tilt * Math.PI / 180;
        }
    };

    /**
     * Стрелка спутника: тонкая пунктирная линия с точками из центра + маркер на полулимбе
     * Рисуется на заднем плане (до антенны)
     */
    ElevationIndicator.prototype.drawSatellitePointer = function() {
        if (this.satelliteElevation === null || !this.isVisible) { return; }

        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;

        const limbAngle = this._elevationToLimbAngle(this.satelliteElevation, this.satelliteAzimuth || this.currentAzimuth);

        // === НАСТРОЙКИ СТРЕЛКИ СПУТНИКА ===
        // var lineWidth = 2;          // Толщина пунктирной линии
        // var dashPattern = [6, 4]; // Пунктир
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
        const mx = cx + Math.cos(limbAngle) * markerR;
        const my = cy - Math.sin(limbAngle) * markerR;

        ctx.beginPath();
        ctx.arc(mx, my, markerRadius, 0, Math.PI * 2);
        ctx.strokeStyle = this.colors.satelliteMarker;
        ctx.lineWidth = markerLineWidth;
        ctx.stroke();
    };

    /**
     * Сообщение «ВНЕ ЗОНЫ НАБЛЮДЕНИЯ» по центру графика
     */
    ElevationIndicator.prototype._drawOutOfViewMessage = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const msgY = cy - this.radius * 0.3;

        ctx.save();
        ctx.font = 'bold 11px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        ctx.fillStyle = this.colors.outOfView;
        ctx.fillText('ВНЕ ЗОНЫ', cx, msgY - 8);
        ctx.fillText('НАБЛЮДЕНИЯ', cx, msgY + 8);
        ctx.restore();
    };

    /**
     * Отрисовка неподвижного постамента
     */
    ElevationIndicator.prototype.drawPedestal = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const s = this.antennaScale;

        const mountHeight = 16 * s;
        const innerArcRadius = mountHeight / 2 + 6 * s;
        const outerArcRadius = innerArcRadius + 5 * s;

        // === 1. Внешнее кольцо шарнира ===
        ctx.strokeStyle = this.colors.antennaAccent;
        ctx.lineWidth = 1.5;

        ctx.beginPath();
        ctx.arc(cx, cy, outerArcRadius, 0, Math.PI * 2, false);
        ctx.stroke();

        // === 2. Колонна — слегка расширяющаяся книзу ===
        const columnH = outerArcRadius + 50 * s;
        const colTopW = outerArcRadius;
        const colBotW = outerArcRadius + 6 * s;
        const colTop = cy;
        const colBot = cy + columnH;

        ctx.lineWidth = 1.5;
        ctx.beginPath();
        ctx.moveTo(cx - colTopW, colTop);
        ctx.lineTo(cx - colBotW, colBot);
        ctx.stroke();

        ctx.beginPath();
        ctx.moveTo(cx + colTopW, colTop);
        ctx.lineTo(cx + colBotW, colBot);
        ctx.stroke();

        // Диагональные распорки внутри колонны
        ctx.lineWidth = 0.7;
        ctx.strokeStyle = this.colors.antennaAccent;
        ctx.globalAlpha = 0.5;
        const braceCount = 2;
        for (let i = 1; i <= braceCount; i++) {
            const t = i / (braceCount + 1);
            const by = colTop + columnH * t;
            const wAtY = colTopW + (colBotW - colTopW) * t;
            ctx.beginPath();
            ctx.moveTo(cx - wAtY * 0.85, by);
            ctx.lineTo(cx + wAtY * 0.85, by);
            ctx.stroke();
        }
        ctx.globalAlpha = 1.0;

        // === 3. Фланцевая пластина (переход колонна → основание) ===
        ctx.lineWidth = 1.5;
        ctx.strokeStyle = this.colors.antennaAccent;
        const flangeH = 15 * s;
        const flangeW = colBotW + 4 * s;

        ctx.beginPath();
        ctx.rect(cx - flangeW, colBot, flangeW * 2, flangeH);
        ctx.stroke();

        // === 4. Основание — шестигранник (вид сбоку: плоские верх и низ, 6 граней) ===
        const trapTop = colBot + flangeH;
        const trapH = 28 * s;
        const trapTW = flangeW;
        const trapBW = 45 * s;
        const trapMidY = trapTop + trapH / 2;

        ctx.lineWidth = 1.5;
        ctx.beginPath();
        ctx.moveTo(cx - trapTW, trapTop);
        ctx.lineTo(cx + trapTW, trapTop);
        ctx.lineTo(cx + trapBW, trapMidY);
        ctx.lineTo(cx + trapBW, trapTop + trapH);
        ctx.lineTo(cx - trapBW, trapTop + trapH);
        ctx.lineTo(cx - trapBW, trapMidY);
        ctx.closePath();
        ctx.stroke();

        // Горизонтальная линия-ребро в середине основания
        ctx.lineWidth = 0.7;
        ctx.globalAlpha = 0.4;
        const ribW = (trapTW + trapBW) / 2;
        ctx.beginPath();
        ctx.moveTo(cx - ribW + 3 * s, trapMidY);
        ctx.lineTo(cx + ribW - 3 * s, trapMidY);
        ctx.stroke();
        ctx.globalAlpha = 1.0;
    };

    /**
     * Вычисление угла поворота антенны для AntennaDrawing
     */
    ElevationIndicator.prototype.calculateAntennaAngle = function(elevation) {
        const tilt = 90 - elevation;
        if (this.isWesternHemisphere()) {
            return -tilt;
        } else {
            return tilt;
        }
    };

    /**
     * Отрисовка антенны
     */
    ElevationIndicator.prototype.drawAntenna = function(elevation) {
        const angle = this.calculateAntennaAngle(elevation);
        window.AntennaDrawing.draw(
            this.ctx,
            this.centerX,
            this.centerY,
            angle,
            this.antennaScale,
            this.radius - 18,
            'elevation'
        );
    };

    /**
     * Привязка DOM-элементов панели информации (El ант., El КА)
     */
    ElevationIndicator.prototype.setInfoElements = function(els) {
        const getEl = function(v) {
            if (!v) {return null;}
            return typeof v === 'string' ? document.getElementById(v) : v;
        };
        this._infoEls = { ant: getEl(els.ant), sat: getEl(els.sat) };
        this._updateInfoPanelDOM();
    };

    ElevationIndicator.prototype._updateInfoPanelDOM = function() {
        const e = this._infoEls;
        if (!e.ant && !e.sat) {return;}
        const antStr = this.currentElevation !== null ? this.currentElevation.toFixed(1) + '°' : '---°';
        const satStr = this.satelliteElevation !== null ? this.satelliteElevation.toFixed(1) + '°' : '---°';
        if (e.ant) {e.ant.textContent = antStr;}
        if (e.sat) {e.sat.textContent = satStr;}
    };

    /**
     * Главная функция отрисовки
     */
    ElevationIndicator.prototype.draw = function() {
        const ctx = this.ctx;

        ctx.fillStyle = this.colors.bgPrimary;
        ctx.fillRect(0, 0, this.logicalWidth, this.logicalHeight);

        this.drawLimb();

        // Стрелка спутника на заднем плане (под антенной)
        this.drawSatellitePointer();

        this.drawPedestal();
        this.drawAntenna(this.currentElevation);

        if (!this.isVisible) {
            this._drawOutOfViewMessage();
        }

        this._updateInfoPanelDOM();
    };

    /**
     * Установка позиции антенны (азимут + угол места)
     */
    ElevationIndicator.prototype.setPosition = function(az, el) {
        this.currentAzimuth = Math.max(0, Math.min(360, az));
        this.currentElevation = Math.max(0, Math.min(90, el));
        this.draw();
    };


    /**
     * Установка угла места (обратная совместимость)
     */
    ElevationIndicator.prototype.setElevation = function(deg) {
        if (deg < 0) {
            this.currentAzimuth = 270;
            this.currentElevation = Math.max(0, Math.min(90, Math.abs(deg)));
        } else {
            this.currentAzimuth = 90;
            this.currentElevation = Math.max(0, Math.min(90, deg));
        }
        this.draw();
    };

    /**
     * Установка позиции спутника
     * @param {number|null} el - угол места спутника (null = нет данных)
     * @param {number|null} az - азимут спутника (для определения полусферы)
     */
    ElevationIndicator.prototype.setSatellitePosition = function(el, az) {
        this.satelliteElevation = (el !== null && el !== undefined)
            ? Math.max(0, Math.min(90, el))
            : null;
        this.satelliteAzimuth = (az !== null && az !== undefined) ? az : null;
        this._updateInfoPanelDOM();
    };

    /**
     * Установка видимости спутника
     */
    ElevationIndicator.prototype.setVisible = function(visible) {
        this.isVisible = visible;
    };

    /**
     * Установка NORAD ID спутника
     * @param {number|string|null} noradId - NORAD ID
     */
    ElevationIndicator.prototype.setNoradId = function(noradId) {
        this.noradId = noradId;
    };

    ElevationIndicator.prototype.getElevation = function() {
        return this.currentElevation;
    };

    ElevationIndicator.prototype.getAzimuth = function() {
        return this.currentAzimuth;
    };

    window.ElevationIndicator = ElevationIndicator;

})();
