// Azimuth Indicator - Индикатор азимута с изображением антенны
// Антенна вращается вокруг центра (точка крепления к поворотному устройству)

(function() {
    'use strict';

    /**
     * Класс азимутального индикатора
     * @param {HTMLCanvasElement} canvas - Canvas элемент для отрисовки
     */
    function AzimuthIndicator(canvas) {
        this.canvas = canvas;
        this.ctx = canvas.getContext('2d');
        this.centerX = canvas.width / 2;
        this.centerY = canvas.height / 2;
        this.radius = Math.min(canvas.width, canvas.height) / 2 - 25;
        this.currentAzimuth = 0;
        
        // Цвета из CSS переменных проекта
        this.colors = {
            bgPrimary: '#0a0e14',
            bgSecondary: '#12171f',
            border: '#2a3444',
            accent: '#00d4aa',      // Основной акцент (антенна)
            accentBlue: '#00a8ff',  // Лимб
            accentRed: '#ff6b6b',   // Стрелка
            textPrimary: '#e6e8eb',
            textSecondary: '#8b919a',
            textMuted: '#5c6370'
        };
    }

    /**
     * Конвертация градусов в радианы (0° = север = верх)
     */
    AzimuthIndicator.prototype.degToRad = function(deg) {
        return (deg - 90) * Math.PI / 180;
    };

    /**
     * Отрисовка статического лимба (шкала азимута)
     */
    AzimuthIndicator.prototype.drawLimb = function() {
        var ctx = this.ctx;
        var cx = this.centerX;
        var cy = this.centerY;
        var r = this.radius;

        // Внешний круг лимба
        ctx.beginPath();
        ctx.arc(cx, cy, r, 0, Math.PI * 2);
        ctx.strokeStyle = this.colors.accentBlue;
        ctx.lineWidth = 2;
        ctx.stroke();

        // Внутренний круг
        ctx.beginPath();
        ctx.arc(cx, cy, r - 18, 0, Math.PI * 2);
        ctx.strokeStyle = this.colors.border;
        ctx.lineWidth = 1;
        ctx.stroke();

        // Деления и подписи
        ctx.font = '10px monospace';
        ctx.fillStyle = this.colors.accentBlue;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        for (var deg = 0; deg < 360; deg += 10) {
            var rad = this.degToRad(deg);
            var isMain = deg % 30 === 0;
            var innerR = isMain ? r - 15 : r - 10;
            var outerR = r - 2;

            // Линии делений
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

            // Подписи для основных делений
            if (isMain) {
                var labelR = r + 12;
                var label = deg.toString();
                
                // Кардинальные направления
                if (deg === 0) label = 'N';
                else if (deg === 90) label = 'E';
                else if (deg === 180) label = 'S';
                else if (deg === 270) label = 'W';

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
     * Отрисовка антенны (вращается с азимутом)
     * Структура (сверху вниз): шестигранник -> линии крепления -> тарелка -> круг крепления
     * Точно по оригиналу: тарелка = трапеция с дугой снизу
     */
    AzimuthIndicator.prototype.drawAntenna = function(azimuth) {
        var ctx = this.ctx;
        var scale = this.radius / 100; // Масштаб относительно радиуса

        ctx.save();
        ctx.translate(this.centerX, this.centerY);
        // Поворот: +90° чтобы антенна "смотрела" на север при азимуте 0°
        ctx.rotate(this.degToRad(azimuth) + Math.PI / 2);

        ctx.strokeStyle = this.colors.accent;
        ctx.fillStyle = 'transparent';
        ctx.lineWidth = 2;
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';

        // Центр вращения = центр круга крепления (0, 0)
        // Круг крепления находится на дуге тарелки (в нижней точке)
        
        // Масштаб для вписывания в canvas
        var s = scale * 0.75;
        
        // === Геометрия тарелки (эллипс) ===
        var dishHalfWidth = 50 * s;       // Половина ширины тарелки (горизонтальный радиус)
        var dishDepth = 35 * s;           // Глубина тарелки (вертикальный радиус)
        var dishTopY = -dishDepth;        // Y верхней линии (центр эллипса)
        var strutJoinX = 30 * s;          // X где стойки соединяются

        // === 1. Тарелка ===
        // Горизонтальная линия сверху (точно по ширине эллипса, без усов)
        ctx.beginPath();
        ctx.moveTo(-dishHalfWidth, dishTopY);
        ctx.lineTo(dishHalfWidth, dishTopY);
        ctx.stroke();

        // Эллиптическая дуга тарелки (овал, выгнутый вниз)
        ctx.beginPath();
        ctx.ellipse(0, dishTopY, dishHalfWidth, dishDepth, 0, 0, Math.PI, false);
        ctx.stroke();

        // === 2. Круг крепления (в нижней точке полукруга = в центре 0,0) ===
        var mountRadius = 16 * s;
        ctx.beginPath();
        ctx.arc(0, 0, mountRadius, 0, Math.PI * 2);
        ctx.stroke();
        ctx.fillStyle = this.colors.bgPrimary;
        ctx.fill();

        // === 3. Линии крепления (от шестигранника к горизонтальной линии) ===
        var hexRadius = 14 * s;
        var hexY = dishTopY - 35 * s;  // Центр шестигранника
        var hexBottomY = hexY + hexRadius;
        
        // Левая линия
        ctx.beginPath();
        ctx.moveTo(-9 * s, hexBottomY);
        ctx.lineTo(-strutJoinX, dishTopY);
        ctx.stroke();

        // Правая линия
        ctx.beginPath();
        ctx.moveTo(9 * s, hexBottomY);
        ctx.lineTo(strutJoinX, dishTopY);
        ctx.stroke();

        // === 4. Шестигранник (приёмник) ===
        ctx.beginPath();
        for (var i = 0; i < 6; i++) {
            var angle = (i * 60 + 30) * Math.PI / 180;
            var x = Math.cos(angle) * hexRadius;
            var y = hexY + Math.sin(angle) * hexRadius;
            if (i === 0) {
                ctx.moveTo(x, y);
            } else {
                ctx.lineTo(x, y);
            }
        }
        ctx.closePath();
        ctx.stroke();

        ctx.restore();
    };

    /**
     * Стрелка азимута (указывает направление)
     */
    AzimuthIndicator.prototype.drawAzimuthArrow = function(azimuth) {
        var ctx = this.ctx;
        var rad = this.degToRad(azimuth);
        var arrowLength = this.radius - 25;
        var arrowWidth = 5;

        ctx.save();
        ctx.translate(this.centerX, this.centerY);
        ctx.rotate(rad + Math.PI / 2);

        // Наконечник стрелки
        ctx.beginPath();
        ctx.moveTo(0, -arrowLength);
        ctx.lineTo(-arrowWidth, -arrowLength + 12);
        ctx.lineTo(0, -arrowLength + 8);
        ctx.lineTo(arrowWidth, -arrowLength + 12);
        ctx.closePath();
        ctx.fillStyle = this.colors.accentRed;
        ctx.fill();

        // Линия стрелки (от центра)
        ctx.beginPath();
        ctx.moveTo(0, -20); // Начинаем чуть дальше центра
        ctx.lineTo(0, -arrowLength + 10);
        ctx.strokeStyle = this.colors.accentRed;
        ctx.lineWidth = 2;
        ctx.stroke();

        ctx.restore();
    };

    /**
     * Числовое значение азимута (левый верхний угол)
     */
    AzimuthIndicator.prototype.drawAzimuthValue = function(azimuth) {
        var ctx = this.ctx;
        
        ctx.font = 'bold 14px monospace';
        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';

        var text = 'AZ: ' + azimuth.toFixed(1) + '°';

        // Фон для читаемости
        var padding = 4;
        var textWidth = ctx.measureText(text).width;
        
        ctx.fillStyle = 'rgba(10, 14, 20, 0.85)';
        ctx.fillRect(6, 6, textWidth + padding * 2, 20);
        
        ctx.strokeStyle = this.colors.border;
        ctx.lineWidth = 1;
        ctx.strokeRect(6, 6, textWidth + padding * 2, 20);

        ctx.fillStyle = this.colors.accent;
        ctx.fillText(text, 6 + padding, 9);
    };

    /**
     * Главная функция отрисовки
     */
    AzimuthIndicator.prototype.draw = function() {
        var ctx = this.ctx;
        
        // Очистка
        ctx.fillStyle = this.colors.bgPrimary;
        ctx.fillRect(0, 0, this.canvas.width, this.canvas.height);

        // Статические элементы
        this.drawLimb();

        // Динамические элементы
        this.drawAzimuthArrow(this.currentAzimuth);
        this.drawAntenna(this.currentAzimuth);
        this.drawAzimuthValue(this.currentAzimuth);
    };

    /**
     * Установка азимута и перерисовка
     * @param {number} deg - Азимут в градусах (0-360)
     */
    AzimuthIndicator.prototype.setAzimuth = function(deg) {
        this.currentAzimuth = ((deg % 360) + 360) % 360; // Нормализация 0-360
        this.draw();
    };

    /**
     * Получение текущего азимута
     * @returns {number} Азимут в градусах
     */
    AzimuthIndicator.prototype.getAzimuth = function() {
        return this.currentAzimuth;
    };

    /**
     * Запуск демо-анимации вращения
     * @param {number} speed - Скорость вращения (градусов за кадр)
     */
    AzimuthIndicator.prototype.startDemo = function(speed) {
        var self = this;
        speed = speed || 0.5;
        
        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
        }

        function animate() {
            self.currentAzimuth = (self.currentAzimuth + speed) % 360;
            self.draw();
            self._animationId = requestAnimationFrame(animate);
        }
        animate();
    };

    /**
     * Остановка демо-анимации
     */
    AzimuthIndicator.prototype.stopDemo = function() {
        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
            this._animationId = null;
        }
    };

    /**
     * Включение управления кликом мыши
     */
    AzimuthIndicator.prototype.enableMouseControl = function() {
        var self = this;
        var cx = this.centerX;
        var cy = this.centerY;

        this.canvas.addEventListener('click', function(e) {
            var rect = self.canvas.getBoundingClientRect();
            var scaleX = self.canvas.width / rect.width;
            var scaleY = self.canvas.height / rect.height;
            
            var x = (e.clientX - rect.left) * scaleX - cx;
            var y = (e.clientY - rect.top) * scaleY - cy;
            
            var angle = Math.atan2(y, x) * 180 / Math.PI + 90;
            if (angle < 0) angle += 360;
            
            self.stopDemo(); // Остановить демо при ручном управлении
            self.setAzimuth(angle);
        });
    };

    // Экспорт в глобальную область
    window.AzimuthIndicator = AzimuthIndicator;

})();
