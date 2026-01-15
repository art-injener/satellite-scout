// Elevation Indicator - Индикатор угла места
// Использует AntennaDrawing для отрисовки антенны
// Шкала от -90° до +90° (0° = зенит)

(function() {
    'use strict';

    /**
     * Класс индикатора угла места
     * @param {HTMLCanvasElement} canvas - Canvas элемент для отрисовки
     */
    function ElevationIndicator(canvas) {
        this.canvas = canvas;
        this.ctx = canvas.getContext('2d');
        this.centerX = canvas.width / 2;
        this.centerY = canvas.height / 2; // Центр как у азимута
        this.radius = Math.min(canvas.width, canvas.height) / 2 - 25; // Как у азимута
        this.currentElevation = 45;

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
     * Отрисовка полулимба (от -90° до +90°)
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
        ctx.lineWidth = 2;  // Толще
        ctx.stroke();

        // Деления и подписи
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        // От -90° до +90° с шагом 15°
        for (let elev = -90; elev <= 90; elev += 15) {
            // Конвертация: -90° -> PI (лево), 0° -> PI/2 (верх), +90° -> 0 (право)
            const rad = Math.PI - (elev + 90) * Math.PI / 180;
            const isMain = elev % 30 === 0;
            const innerR = isMain ? r - 15 : r - 10;
            const outerR = r - 2;

            // Линии делений
            ctx.beginPath();
            ctx.moveTo(
                cx + Math.cos(rad) * innerR,
                cy - Math.sin(rad) * innerR
            );
            ctx.lineTo(
                cx + Math.cos(rad) * outerR,
                cy - Math.sin(rad) * outerR
            );
            ctx.strokeStyle = isMain ? this.colors.accentBlue : this.colors.border;
            ctx.lineWidth = isMain ? 2 : 1;
            ctx.stroke();

            // Подписи для основных делений
            if (isMain) {
                const labelR = r + 12;
                const label = elev.toString() + '°';

                ctx.fillStyle = (elev === 0) ? this.colors.textPrimary : this.colors.textSecondary;
                ctx.fillText(
                    label,
                    cx + Math.cos(rad) * labelR,
                    cy - Math.sin(rad) * labelR
                );
            }
        }
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
        // Шестигранник теперь вращается с антенной (в antenna.js)
    };

    /**
     * Отрисовка антенны (дуга теперь в antenna.js)
     */
    ElevationIndicator.prototype.drawAntenna = function(elevation) {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;

        // Рисуем антенну (включает вращающуюся дугу)
        window.AntennaDrawing.draw(
            ctx,
            cx,
            cy,
            elevation,
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

        // Очистка
        ctx.fillStyle = this.colors.bgPrimary;
        ctx.fillRect(0, 0, this.canvas.width, this.canvas.height);

        // Статический лимб
        this.drawLimb();

        // Сначала постамент (будет под антенной)
        this.drawPedestal();

        // Потом антенна (будет поверх постамента)
        this.drawAntenna(this.currentElevation);
        this.drawElevationValue(this.currentElevation);
    };

    /**
     * Установка угла места и перерисовка
     */
    ElevationIndicator.prototype.setElevation = function(deg) {
        this.currentElevation = Math.max(-90, Math.min(90, deg));
        this.draw();
    };

    /**
     * Получение текущего угла места
     */
    ElevationIndicator.prototype.getElevation = function() {
        return this.currentElevation;
    };

    /**
     * Демо-анимация
     */
    ElevationIndicator.prototype.startDemo = function(speed) {
        const self = this;
        speed = speed || 0.5;
        let direction = 1;

        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
        }

        function animate() {
            self.currentElevation += speed * direction;
            if (self.currentElevation >= 90) {
                direction = -1;
            } else if (self.currentElevation <= -90) {
                direction = 1;
            }
            self.draw();
            self._animationId = requestAnimationFrame(animate);
        }
        animate();
    };

    /**
     * Остановка демо
     */
    ElevationIndicator.prototype.stopDemo = function() {
        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
            this._animationId = null;
        }
    };

    /**
     * Управление кликом
     */
    ElevationIndicator.prototype.enableMouseControl = function() {
        const self = this;

        this.canvas.addEventListener('click', function(e) {
            const rect = self.canvas.getBoundingClientRect();
            const scaleX = self.canvas.width / rect.width;
            const scaleY = self.canvas.height / rect.height;

            const x = (e.clientX - rect.left) * scaleX - self.centerX;
            const y = (e.clientY - rect.top) * scaleY - self.centerY;

            // Конвертация в угол места
            let angle = -Math.atan2(y, x) * 180 / Math.PI + 90;
            if (angle > 180) {angle -= 360;}
            angle = Math.max(-90, Math.min(90, angle));

            self.stopDemo();
            self.setElevation(angle);
        });
    };

    // Экспорт
    window.ElevationIndicator = ElevationIndicator;

})();
