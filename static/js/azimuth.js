// Azimuth Indicator - Индикатор азимута с антенной
// Использует AntennaDrawing для отрисовки антенны

(function() {
    'use strict';

    /**
     * Класс индикатора азимута
     * @param {HTMLCanvasElement} canvas - Canvas элемент для отрисовки
     */
    function AzimuthIndicator(canvas) {
        this.canvas = canvas;
        this.ctx = canvas.getContext('2d');
        this.centerX = canvas.width / 2;
        this.centerY = canvas.height / 2;
        this.radius = Math.min(canvas.width, canvas.height) / 2 - 25;  // Отступ для подписей
        this.currentAzimuth = 0;
        
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

        // Масштаб антенны (увеличен для заполнения пространства)
        this.antennaScale = this.radius / 100 * 0.95;
    }

    /**
     * Конвертация градусов в радианы
     */
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

        // Внешний круг
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
        ctx.font = '11px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        for (var deg = 0; deg < 360; deg += 15) {
            var rad = this.degToRad(deg - 90); // 0° = север (вверх)
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

            // Подписи
            if (isMain) {
                var labelR = r + 14;
                var label = deg.toString();
                
                // Стороны света
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
     * Отрисовка антенны с использованием общей функции
     */
    AzimuthIndicator.prototype.drawAntenna = function(azimuth) {
        // Вызов общей функции отрисовки антенны
        // Угол: azimuth + 90 (чтобы 0° был вверх, а не вправо)
        window.AntennaDrawing.draw(
            this.ctx,
            this.centerX,
            this.centerY,
            azimuth + 90,
            this.antennaScale,
            this.radius - 9  // arrowEndRadius
        );
    };

    /**
     * Числовое значение азимута
     */
    AzimuthIndicator.prototype.drawAzimuthValue = function(azimuth) {
        var ctx = this.ctx;
        
        ctx.font = 'bold 16px monospace';
        ctx.textAlign = 'left';
        ctx.textBaseline = 'top';
        ctx.fillStyle = this.colors.accent;
        ctx.fillText(azimuth.toFixed(1) + '°', 8, 8);
    };

    /**
     * Главная функция отрисовки
     */
    AzimuthIndicator.prototype.draw = function() {
        var ctx = this.ctx;
        
        // Очистка
        ctx.fillStyle = this.colors.bgPrimary;
        ctx.fillRect(0, 0, this.canvas.width, this.canvas.height);

        // Статический лимб
        this.drawLimb();

        // Динамическая антенна
        this.drawAntenna(this.currentAzimuth);
        this.drawAzimuthValue(this.currentAzimuth);
    };

    /**
     * Установка азимута и перерисовка
     */
    AzimuthIndicator.prototype.setAzimuth = function(deg) {
        this.currentAzimuth = ((deg % 360) + 360) % 360;
        this.draw();
    };

    /**
     * Получение текущего азимута
     */
    AzimuthIndicator.prototype.getAzimuth = function() {
        return this.currentAzimuth;
    };

    /**
     * Демо-анимация
     */
    AzimuthIndicator.prototype.startDemo = function(speed) {
        var self = this;
        speed = speed || 1;
        
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
     * Остановка демо
     */
    AzimuthIndicator.prototype.stopDemo = function() {
        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
            this._animationId = null;
        }
    };

    /**
     * Управление кликом
     */
    AzimuthIndicator.prototype.enableMouseControl = function() {
        var self = this;
        
        this.canvas.addEventListener('click', function(e) {
            var rect = self.canvas.getBoundingClientRect();
            var scaleX = self.canvas.width / rect.width;
            var scaleY = self.canvas.height / rect.height;
            
            var x = (e.clientX - rect.left) * scaleX - self.centerX;
            var y = (e.clientY - rect.top) * scaleY - self.centerY;
            
            var angle = Math.atan2(y, x) * 180 / Math.PI + 90;
            if (angle < 0) angle += 360;
            
            self.stopDemo();
            self.setAzimuth(angle);
        });
    };

    // Экспорт
    window.AzimuthIndicator = AzimuthIndicator;

})();
