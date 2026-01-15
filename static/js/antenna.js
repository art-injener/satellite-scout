// Antenna Drawing - Общая функция отрисовки антенны
// Используется для азимутального графика и графика угла места

(function() {
    'use strict';

    /**
     * Цвета для отрисовки
     */
    const colors = {
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
     * @param {string} viewType - Тип графика: 'azimuth' или 'elevation'
     */
    function drawAntenna(ctx, centerX, centerY, angle, scale, arrowEndRadius, viewType) {
        ctx.save();
        ctx.translate(centerX, centerY);
        ctx.rotate(angle * Math.PI / 180);

        ctx.strokeStyle = colors.accent;
        ctx.fillStyle = 'transparent';
        ctx.lineWidth = 2;
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';

        const s = scale;

        // === Геометрия тарелки ===
        const dishHalfWidth = 80 * s;
        const dishDepth = 24 * s;
        const dishTopY = -dishDepth;
        const strutJoinX = 30 * s;
        const rimHeight = 7 * s;

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

        // === 2. Блок крепления (центр вращения) ===
        const mountWidth = 20 * s;
        const mountHeight = 16 * s;
        const arcRadius = mountHeight / 2 + 6 * s;

        if (viewType === 'azimuth') {
            // Вид сверху: П-образный прямоугольник + 2 вертикальные линии (ось шарнира)
            const azMountWidth = mountWidth + 10;  // Шире на 10 пикселей

            // Заливка фоном
            ctx.beginPath();
            ctx.rect(-azMountWidth / 2, -mountHeight / 2, azMountWidth, mountHeight);
            ctx.fillStyle = colors.bgPrimary;
            ctx.fill();

            // Рисуем П-образный контур (без верхней стенки)
            ctx.beginPath();
            ctx.moveTo(-azMountWidth / 2, -mountHeight / 2);
            ctx.lineTo(-azMountWidth / 2, mountHeight / 2);
            ctx.lineTo(azMountWidth / 2, mountHeight / 2);
            ctx.lineTo(azMountWidth / 2, -mountHeight / 2);
            ctx.strokeStyle = colors.accent;
            ctx.stroke();

            // 2 вертикальные линии внутри (ось шарнира, вид сверху)
            const axisGap = azMountWidth / 2 - 5 * s;  // Ближе к краям
            ctx.beginPath();
            ctx.moveTo(-axisGap, -mountHeight / 2 + 3 * s);
            ctx.lineTo(-axisGap, mountHeight / 2 - 3 * s);
            ctx.stroke();

            ctx.beginPath();
            ctx.moveTo(axisGap, -mountHeight / 2 + 3 * s);
            ctx.lineTo(axisGap, mountHeight / 2 - 3 * s);
            ctx.stroke();
        } else {
            // Вид сбоку (elevation): полукруг + шестигранник (вращается с антенной)
            ctx.beginPath();
            ctx.arc(0, 0, arcRadius, 0, Math.PI, false);
            ctx.strokeStyle = colors.accent;
            ctx.stroke();

            // Шестигранник крепления (в центре вращения)
            const mountHexRadius = 7 * s;
            ctx.beginPath();
            for (let j = 0; j < 6; j++) {
                const mountAngle = (j * 60 + 30) * Math.PI / 180;
                const mx = Math.cos(mountAngle) * mountHexRadius;
                const my = Math.sin(mountAngle) * mountHexRadius;
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
        }

        // === 3. Линии крепления ===
        const hexRadius = 12 * s;
        const hexY = dishTopY - 42 * s;
        const hexBottomY = hexY + hexRadius;

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
        for (let i = 0; i < 6; i++) {
            const hexAngle = (i * 60 + 30) * Math.PI / 180;
            const hx = Math.cos(hexAngle) * hexRadius;
            const hy = hexY + Math.sin(hexAngle) * hexRadius;
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
            const hexTopY = hexY - hexRadius;
            const arrowEndY = -arrowEndRadius;
            const arrowWidth = 5;

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
            mountWidth: mountWidth,
            mountHeight: mountHeight,
            arcRadius: arcRadius,
            hexY: hexY,
            hexRadius: hexRadius,
            scale: s
        };
    }

    /**
     * Установка цветов (для синхронизации с основными файлами)
     */
    function setColors(newColors) {
        if (newColors.bgPrimary) {colors.bgPrimary = newColors.bgPrimary;}
        if (newColors.accent) {colors.accent = newColors.accent;}
        if (newColors.accentRed) {colors.accentRed = newColors.accentRed;}
    }

    // Экспорт
    window.AntennaDrawing = {
        draw: drawAntenna,
        setColors: setColors
    };

})();
