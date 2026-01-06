// Antenna Drawing - Общая функция отрисовки антенны
// Используется для азимутального графика и графика угла места

(function() {
    'use strict';

    /**
     * Цвета для отрисовки
     */
    var colors = {
        bgPrimary: '#0a0e14',
        accent: '#00d4aa',
        accentRed: '#ff6b6b'
    };

    /**
     * Отрисовка антенны
     * @param {CanvasRenderingContext2D} ctx - Контекст canvas
     * @param {number} centerX - X координата центра вращения
     * @param {number} centerY - Y координата центра вращения
     * @param {number} angle - Угол поворота в градусах (0 = вверх)
     * @param {number} scale - Масштаб отрисовки
     * @param {number} arrowEndRadius - Радиус для конца стрелки (опционально)
     */
    function drawAntenna(ctx, centerX, centerY, angle, scale, arrowEndRadius) {
        ctx.save();
        ctx.translate(centerX, centerY);
        ctx.rotate(angle * Math.PI / 180);

        ctx.strokeStyle = colors.accent;
        ctx.fillStyle = 'transparent';
        ctx.lineWidth = 2;
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';

        var s = scale;

        // === Геометрия тарелки ===
        var dishHalfWidth = 80 * s;
        var dishDepth = 24 * s;
        var dishTopY = -dishDepth;
        var strutJoinX = 30 * s;
        var rimHeight = 7 * s;

        // === 1. Тарелка ===
        // Заливка тарелки цветом фона (чтобы перекрыть элементы под ней)
        ctx.beginPath();
        ctx.ellipse(0, dishTopY, dishHalfWidth, rimHeight, 0, 0, Math.PI * 2);
        ctx.moveTo(dishHalfWidth, dishTopY);
        ctx.ellipse(0, dishTopY, dishHalfWidth, dishDepth, 0, 0, Math.PI, false);
        ctx.closePath();
        ctx.fillStyle = colors.bgPrimary;
        ctx.fill();

        // Верхний край: узкий эллипс (ободок)
        ctx.beginPath();
        ctx.ellipse(0, dishTopY, dishHalfWidth, rimHeight, 0, 0, Math.PI * 2);
        ctx.strokeStyle = colors.accent;
        ctx.stroke();

        // Нижняя дуга тарелки (основной эллипс)
        ctx.beginPath();
        ctx.ellipse(0, dishTopY, dishHalfWidth, dishDepth, 0, 0, Math.PI, false);
        ctx.stroke();

        // === 2. Шестигранник крепления (центр вращения) ===
        var mountRadius = 7.5 * s;
        
        // Дуга вокруг шестигранника (вращается вместе с антенной)
        var arcRadius = mountRadius + 6 * s;
        ctx.beginPath();
        ctx.arc(0, 0, arcRadius, 0, Math.PI, false);
        ctx.stroke();

        // Шестигранник
        ctx.beginPath();
        for (var j = 0; j < 6; j++) {
            var mountAngle = (j * 60 + 30) * Math.PI / 180;
            var mx = Math.cos(mountAngle) * mountRadius;
            var my = Math.sin(mountAngle) * mountRadius;
            if (j === 0) {
                ctx.moveTo(mx, my);
            } else {
                ctx.lineTo(mx, my);
            }
        }
        ctx.closePath();
        ctx.stroke();
        ctx.fillStyle = colors.bgPrimary;
        ctx.fill();

        // === 3. Линии крепления ===
        var hexRadius = 12 * s;
        var hexY = dishTopY - 42 * s;
        var hexBottomY = hexY + hexRadius;

        ctx.strokeStyle = colors.accent;
        ctx.beginPath();
        ctx.moveTo(-9 * s, hexBottomY);
        ctx.lineTo(-strutJoinX, dishTopY);
        ctx.stroke();

        ctx.beginPath();
        ctx.moveTo(9 * s, hexBottomY);
        ctx.lineTo(strutJoinX, dishTopY);
        ctx.stroke();

        // === 4. Шестигранник приёмника ===
        ctx.beginPath();
        for (var i = 0; i < 6; i++) {
            var hexAngle = (i * 60 + 30) * Math.PI / 180;
            var hx = Math.cos(hexAngle) * hexRadius;
            var hy = hexY + Math.sin(hexAngle) * hexRadius;
            if (i === 0) {
                ctx.moveTo(hx, hy);
            } else {
                ctx.lineTo(hx, hy);
            }
        }
        ctx.closePath();
        ctx.stroke();

        // === 5. Красная стрелка ===
        if (arrowEndRadius) {
            var hexTopY = hexY - hexRadius;
            var arrowEndY = -arrowEndRadius;
            var arrowWidth = 5;

            ctx.beginPath();
            ctx.moveTo(0, hexTopY - 2);
            ctx.lineTo(0, arrowEndY + 12);
            ctx.strokeStyle = colors.accentRed;
            ctx.lineWidth = 2;
            ctx.stroke();

            ctx.beginPath();
            ctx.moveTo(0, arrowEndY);
            ctx.lineTo(-arrowWidth, arrowEndY + 12);
            ctx.lineTo(0, arrowEndY + 8);
            ctx.lineTo(arrowWidth, arrowEndY + 12);
            ctx.closePath();
            ctx.fillStyle = colors.accentRed;
            ctx.fill();
        }

        ctx.restore();

        // Возвращаем позицию шестигранника крепления для постамента
        return {
            mountRadius: mountRadius,
            hexY: hexY,
            hexRadius: hexRadius,
            scale: s
        };
    }

    /**
     * Установка цветов (для синхронизации с основными файлами)
     */
    function setColors(newColors) {
        if (newColors.bgPrimary) colors.bgPrimary = newColors.bgPrimary;
        if (newColors.accent) colors.accent = newColors.accent;
        if (newColors.accentRed) colors.accentRed = newColors.accentRed;
    }

    // Экспорт
    window.AntennaDrawing = {
        draw: drawAntenna,
        setColors: setColors
    };

})();
