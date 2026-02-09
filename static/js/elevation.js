// Elevation Indicator - Индикатор угла места
// Использует AntennaDrawing для отрисовки антенны
// Шкала: 0° по краям (горизонт W/E), 90° в центре (зенит)
// - Левая зона (W): западная полусфера (азимут > 180° или az == 0°)
// - Правая зона (E): восточная полусфера (0° < азимут ≤ 180°)

(function() {
    'use strict';

    /**
     * Класс индикатора угла места
     * @param {HTMLCanvasElement} canvas - Canvas элемент для отрисовки
     */
    function ElevationIndicator(canvas) {
        this.canvas = canvas;

        // Настройка HiDPI canvas
        const logicalWidth = parseInt(canvas.getAttribute('width'), 10);
        const logicalHeight = parseInt(canvas.getAttribute('height'), 10);

        if (window.CanvasUtils) {
            this.ctx = window.CanvasUtils.setupHiDPICanvas(canvas, logicalWidth, logicalHeight);
        } else {
            this.ctx = canvas.getContext('2d');
        }

        // Используем логические размеры для расчётов
        this.centerX = logicalWidth / 2;

        // Радиус зависит только от ширины, чтобы масштаб не менялся при изменении высоты
        this.radius = logicalWidth / 2 - 25; // 125px для canvas 300px

        // Центр Y рассчитывается так, чтобы полукруг полностью помещался в canvas
        // Нужно место для: радиус + метки (≈15px сверху) + смещение вниз (5px)
        const topPadding = 15;
        this.centerY = this.radius + topPadding + 5;

        // Текущие значения
        this.currentElevation = 45; // 0-90°
        this.currentAzimuth = 270;  // 0-360° (по умолчанию западная полусфера)

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
            textMuted: '#5c6370'
        };

        // Масштаб антенны (такой же как для азимута)
        this.antennaScale = this.radius / 100 * 0.95;
    }

    /**
     * Конвертация градусов в радианы
     */
    ElevationIndicator.prototype.degToRad = function(deg) {
        return deg * Math.PI / 180;
    };

    /**
     * Определение полусферы по азимуту
     * @returns {boolean} true = западная полусфера (левая зона), false = восточная (правая)
     */
    ElevationIndicator.prototype.isWesternHemisphere = function() {
        // Западная полусфера: az > 180° или az == 0° (север относим к западной)
        return this.currentAzimuth > 180 || this.currentAzimuth === 0;
    };

    /**
     * Отрисовка полулимба с двумя зонами
     * Горизонт = 0° (края), Зенит = 90° (центр)
     * Шкала: 0° (W) → 30° → 60° → 90° (зенит) ← 60° ← 30° ← 0° (E)
     */
    ElevationIndicator.prototype.drawLimb = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;

        // Внешняя дуга (полукруг сверху)
        ctx.beginPath();
        ctx.arc(cx, cy, r, Math.PI, 0, false);
        ctx.strokeStyle = this.colors.accentBlue;
        ctx.lineWidth = 2;
        ctx.stroke();

        // Внутренняя дуга
        ctx.beginPath();
        ctx.arc(cx, cy, r - 18, Math.PI, 0, false);
        ctx.strokeStyle = this.colors.border;
        ctx.lineWidth = 2;
        ctx.stroke();

        // Вертикальная разделительная линия (зенит = 90°)
        ctx.beginPath();
        ctx.moveTo(cx, cy - r + 2);
        ctx.lineTo(cx, cy - r + 15);
        ctx.strokeStyle = this.colors.accentBlue;
        ctx.lineWidth = 2;
        ctx.stroke();

        // Деления и подписи
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        // Шкала: 0° по краям (горизонт), 90° в центре (зенит)
        // Левая сторона: 0° (лево) → 30° → 60° → 90° (верх)
        // Правая сторона: 90° (верх) → 60° → 30° → 0° (право)
        for (let i = 0; i <= 6; i++) {
            const scaleValue = i * 15; // 0, 15, 30, 45, 60, 75, 90
            const labelValue = 90 - scaleValue; // Инвертируем: 90, 75, 60, 45, 30, 15, 0
            const isMain = scaleValue % 30 === 0;
            const innerR = isMain ? r - 15 : r - 10;
            const outerR = r - 2;

            // Левая сторона: угол от PI/2 (верх, 90°) до PI (лево, 0°)
            // scaleValue=0 → rad=PI/2, scaleValue=90 → rad=PI
            const radLeft = Math.PI / 2 + scaleValue * Math.PI / 180;

            // Правая сторона: угол от PI/2 (верх, 90°) до 0 (право, 0°)
            // scaleValue=0 → rad=PI/2, scaleValue=90 → rad=0
            const radRight = Math.PI / 2 - scaleValue * Math.PI / 180;

            // === Левая сторона ===
            ctx.beginPath();
            ctx.moveTo(
                cx + Math.cos(radLeft) * innerR,
                cy - Math.sin(radLeft) * innerR
            );
            ctx.lineTo(
                cx + Math.cos(radLeft) * outerR,
                cy - Math.sin(radLeft) * outerR
            );
            ctx.strokeStyle = isMain ? this.colors.accentBlue : this.colors.border;
            ctx.lineWidth = isMain ? 2 : 1;
            ctx.stroke();

            // Подписи для левой стороны (основные деления)
            if (isMain) {
                const labelR = r + 12;
                const label = labelValue.toString() + '°';

                ctx.fillStyle = (labelValue === 90) ? this.colors.textPrimary : this.colors.textSecondary;
                ctx.fillText(
                    label,
                    cx + Math.cos(radLeft) * labelR,
                    cy - Math.sin(radLeft) * labelR
                );
            }

            // === Правая сторона (кроме 90° в центре, чтобы не дублировать) ===
            if (scaleValue > 0) {
                ctx.beginPath();
                ctx.moveTo(
                    cx + Math.cos(radRight) * innerR,
                    cy - Math.sin(radRight) * innerR
                );
                ctx.lineTo(
                    cx + Math.cos(radRight) * outerR,
                    cy - Math.sin(radRight) * outerR
                );
                ctx.strokeStyle = isMain ? this.colors.accentBlue : this.colors.border;
                ctx.lineWidth = isMain ? 2 : 1;
                ctx.stroke();

                // Подписи для правой стороны
                if (isMain) {
                    const labelR = r + 12;
                    const label = labelValue.toString() + '°';

                    ctx.fillStyle = this.colors.textSecondary;
                    ctx.fillText(
                        label,
                        cx + Math.cos(radRight) * labelR,
                        cy - Math.sin(radRight) * labelR
                    );
                }
            }
        }

        // Подписи сторон света W и E под цифрами 0°
        ctx.font = 'bold 11px monospace';
        ctx.fillStyle = this.colors.textMuted;

        // W слева (под 0° левой стороны)
        ctx.textAlign = 'center';
        ctx.fillText('W', cx - r - 12, cy + 15);

        // E справа (под 0° правой стороны)
        ctx.fillText('E', cx + r + 12, cy + 15);
    };

    /**
     * Отрисовка неподвижного полукруга с шестигранником
     */
    ElevationIndicator.prototype.drawPedestal = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const s = this.antennaScale;

        // Радиус внешнего неподвижного полукруга (чуть больше вращающегося)
        const mountHeight = 16 * s;
        const innerArcRadius = mountHeight / 2 + 6 * s;
        const outerArcRadius = innerArcRadius + 5 * s;

        ctx.strokeStyle = this.colors.accent;
        ctx.lineWidth = 2;

        // Неподвижный круг (внешний)
        ctx.beginPath();
        ctx.arc(cx, cy, outerArcRadius, 0, Math.PI * 2, false);
        ctx.stroke();

        // Линии вниз от концов полукруга + нижняя линия
        const lineLength = outerArcRadius + 5;
        // Левая линия
        ctx.beginPath();
        ctx.moveTo(cx - outerArcRadius, cy);
        ctx.lineTo(cx - outerArcRadius, cy + lineLength);
        ctx.stroke();
        // Правая линия
        ctx.beginPath();
        ctx.moveTo(cx + outerArcRadius, cy);
        ctx.lineTo(cx + outerArcRadius, cy + lineLength);
        ctx.stroke();
        // Нижняя линия (соединяет концы)
        ctx.beginPath();
        ctx.moveTo(cx - outerArcRadius, cy + lineLength);
        ctx.lineTo(cx + outerArcRadius, cy + lineLength);
        ctx.stroke();
    };

    /**
     * Вычисление угла поворота антенны для AntennaDrawing
     * @param {number} elevation - угол места 0-90°
     * @returns {number} угол поворота для отрисовки (0 = вверх)
     * 
     * Шкала: 0° = зенит (антенна вверх), 90° = горизонт (антенна в сторону)
     * Угол на шкале = 90° - elevation
     */
    ElevationIndicator.prototype.calculateAntennaAngle = function(elevation) {
        // Угол наклона от вертикали = 90° - elevation
        // elevation 90° (зенит) → наклон 0° (антенна вверх)
        // elevation 0° (горизонт) → наклон 90° (антенна в сторону)
        const tilt = 90 - elevation;

        // Для западной полусферы: антенна наклоняется влево (отрицательный угол)
        // Для восточной полусферы: антенна наклоняется вправо (положительный угол)
        if (this.isWesternHemisphere()) {
            return -tilt; // влево
        } else {
            return tilt;  // вправо
        }
    };

    /**
     * Отрисовка антенны
     */
    ElevationIndicator.prototype.drawAntenna = function(elevation) {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;

        const angle = this.calculateAntennaAngle(elevation);

        // Рисуем антенну (включает вращающуюся дугу)
        window.AntennaDrawing.draw(
            ctx,
            cx,
            cy,
            angle,
            this.antennaScale,
            this.radius - 9,
            'elevation' // viewType
        );
    };

    /**
     * Числовое значение угла места
     */
    ElevationIndicator.prototype.drawElevationValue = function(elevation) {
        const ctx = this.ctx;

        ctx.font = 'bold 16px monospace';
        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';
        ctx.fillStyle = this.colors.accent;
        ctx.fillText(elevation.toFixed(1) + '°', 8, 8);
    };

    /**
     * Главная функция отрисовки
     */
    ElevationIndicator.prototype.draw = function() {
        const ctx = this.ctx;

        // Получаем логические размеры для очистки
        const size = window.CanvasUtils ?
            window.CanvasUtils.getLogicalSize(this.canvas) :
            { width: this.canvas.width, height: this.canvas.height };

        // Очистка
        ctx.fillStyle = this.colors.bgPrimary;
        ctx.fillRect(0, 0, size.width, size.height);

        // Статический лимб
        this.drawLimb();

        // Сначала постамент (будет под антенной)
        this.drawPedestal();

        // Потом антенна (будет поверх постамента)
        this.drawAntenna(this.currentElevation);
        this.drawElevationValue(this.currentElevation);
    };

    /**
     * Установка позиции (азимут + угол места)
     * @param {number} az - азимут 0-360°
     * @param {number} el - угол места 0-90°
     */
    ElevationIndicator.prototype.setPosition = function(az, el) {
        this.currentAzimuth = Math.max(0, Math.min(360, az));
        this.currentElevation = Math.max(0, Math.min(90, el));
        this.draw();
    };

    /**
     * Установка угла места (обратная совместимость)
     * @param {number} deg - угол места
     */
    ElevationIndicator.prototype.setElevation = function(deg) {
        // Для обратной совместимости: если передан отрицательный угол,
        // интерпретируем как западную полусферу, положительный — как восточную
        if (deg < 0) {
            this.currentAzimuth = 270; // Западная полусфера
            this.currentElevation = Math.max(0, Math.min(90, Math.abs(deg)));
        } else {
            this.currentAzimuth = 90; // Восточная полусфера
            this.currentElevation = Math.max(0, Math.min(90, deg));
        }
        this.draw();
    };

    /**
     * Получение текущего угла места
     */
    ElevationIndicator.prototype.getElevation = function() {
        return this.currentElevation;
    };

    /**
     * Получение текущего азимута
     */
    ElevationIndicator.prototype.getAzimuth = function() {
        return this.currentAzimuth;
    };


    // Экспорт
    window.ElevationIndicator = ElevationIndicator;

})();
