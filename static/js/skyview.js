// Sky View - Азимутальная проекция неба для отображения спутников
// Улучшенная версия с анимацией и расширенной конфигурацией

(function() {
    'use strict';

    /** Обрезка имени спутника для canvas-подписей (макс. 16 символов). */
    function _shortName(name, maxLen) {
        if (!name) { return ''; }
        maxLen = maxLen || 16;
        return name.length > maxLen ? name.slice(0, maxLen - 1) + '\u2026' : name;
    }

    /**
     * Класс Sky View - азимутальная проекция неба
     *
     * @param {HTMLCanvasElement} canvas - Canvas элемент
     * @param {Object} options - Опции конфигурации
     *
     * Настраиваемые параметры (options):
     * - showGrid: boolean - показывать сетку (default: true)
     * - showLabels: boolean - показывать метки (default: true)
     * - showSatelliteAura: boolean - показывать окружность вокруг спутника (default: true)
     * - showObserver: boolean - показывать иконку наблюдателя в центре (default: true)
     * - azimuthStep: number - шаг меток азимута в градусах (default: 30)
     * - arrowInterval: number - интервал между стрелками на траектории в мс (default: 120000)
     * - satelliteAuraRadius: number - радиус ауры спутника (default: 20)
     * - animationSpeed: number - скорость анимации пульсации (default: 1)
     */
    function SkyView(canvas, options) {
        this.canvas = canvas;
        this.ctx = canvas.getContext('2d');

        // Опции по умолчанию
        this.options = Object.assign({
            showGrid: true,
            showLabels: true,
            showSatelliteAura: true, // Показывать окружность вокруг спутника
            showObserver: true, // Показывать иконку наблюдателя
            azimuthStep: 30, // Шаг меток азимута (30° или 45°)
            arrowInterval: 600000, // Интервал стрелок на траектории (10 минут)
            satelliteAuraRadius: 20, // Радиус ауры спутника
            animationSpeed: 1 // Скорость анимации
        }, options || {});

        /**
         * Цветовая схема из CSS-переменных темы (colors-*.css)
         */
        this.colors = {
            background: cssVar('--sky-bg',   '#0c1420'),
            skyFill:    cssVar('--sky-fill',  '#182838'),

            grid:     cssVar('--sky-grid',      '#3a5060'),
            gridText: cssVar('--sky-grid-text',  '#d0d8e0'),

            elevationLabel: cssVar('--sky-elevation-label', '#d0d8e0'),
            elevationLabelOffsetX: -5,
            elevationLabelOffsetY: -5,
            elevationLabelSize: 11,

            cardinalLabel: cssVar('--sky-label-muted', '#d0d8e0'),

            azimuthLabel: cssVar('--sky-azimuth-label', '#d0d8e0'),

            track:      cssVar('--sky-track',       '#00cc00'),
            trackArrow: cssVar('--sky-track-arrow',  '#66dd66'),

            aosMarker:    cssVar('--sky-aos',           '#00ff00'),
            losMarker:    cssVar('--sky-los',           '#ff4444'),
            markerBorder: cssVar('--sky-marker-border', '#ffffff'),

            satellite:       cssVar('--sky-satellite',        '#00ffff'),
            satelliteGlow:   themeRgba('sky-satellite-glow',   'rgba(0, 255, 255, 0.3)'),
            satelliteSignal: themeRgba('sky-satellite-signal',  'rgba(0, 255, 200, 0.5)'),
            satLabel:        cssVar('--sky-satellite-label',  '#ffffff'),

            satelliteAura:       themeRgba('sky-satellite-aura', 'rgba(197, 88, 88, 1)'),
            satelliteAuraBorder: cssVar('--sky-satellite-aura-border', '#ff8888'),

            observer:          cssVar('--sky-observer',           '#ffaa00'),
            observerSecondary: cssVar('--sky-observer-secondary', '#ff6600'),

            infoText:  cssVar('--sky-info-text',  '#00d4aa'),
            infoLabel: cssVar('--sky-info-label',  '#ffffff'),
            timeText:  cssVar('--sky-time-text',   '#708898'),

            selectedTrack:  cssVar('--sky-selected-track',  '#ffff00'),
            selectedSatLabel: cssVar('--sky-selected-sat-label', '#d8c878'),
            selectedMarker: cssVar('--sky-selected-marker', '#2ecc71'),

            /* Обводки canvas (подписи, стрелки, «солнечные панели») — заданы в теме */
            canvasTextStroke: cssVar('--sky-canvas-text-stroke', 'rgba(0, 0, 0, 0.9)'),
            satPanelLine:     cssVar('--sky-sat-panel-line', 'rgba(255, 255, 255, 0.3)'),
            satBodyOutline:   cssVar('--sky-sat-body-outline', '#ffffff'),
            arrowOutline:     cssVar('--sky-arrow-outline', 'rgba(0, 0, 0, 0.75)'),
            signalWaveRgb:    (function() {
                var s = cssVar('--sky-signal-wave-rgb', '0, 255, 200').trim().replace(/\s/g, '');
                return s || '0,255,200';
            })()
        };

        this._skyGridLineW = parseFloat(cssVar('--sky-grid-line-width', '1')) || 1;
        this._skyGridOuterW = parseFloat(cssVar('--sky-grid-outer-width', '2')) || 2;

        // Расчёт геометрии
        this._updateGeometry();

        // Спутник на слежении (tracking) — текущий стиль (зелёный + аура).
        this.satellite = {
            name: '',
            noradId: null,
            track: [],
            currentPos: null
        };

        // Выбранный спутник (selected) — оранжевый трек + маркер.
        this._selectedSatellite = {
            name: '',
            noradId: null,
            track: [],
            currentPos: null
        };

        // Вторичные спутники группы: noradId → {noradId, name, track, currentPos, isVisible}
        this._secondarySatellites = {};

        // Данные о пролёте (времена и позиции маркеров для синхронизации надписей)
        this.passInfo = {
            aosTime: null,
            losTime: null,
            maxElTime: null,
            aosCanvasY: null, // Y маркера AOS на canvas (верхняя/нижняя полусфера)
            losCanvasY: null,
            aosAz: null, // азимут AOS (для сортировки при одной полусфере)
            losAz: null
        };

        // Данные пролёта для текущего (выбранного) спутника — для инфопанели при отображении выбранного
        this._selectedPassInfo = { aosTime: null, losTime: null };

        // Observer
        this.observer = {
            lat: 47.23,
            lon: 39.7,
            name: 'Ростов-на-Дону'
        };

        // Анимация
        this._animationPhase = 0;
        this._lastAnimTime = 0;

        // Опциональные DOM-элементы для текстового блока под графиком (обновляются при setSatelliteInfo/setPassTimes и раз в секунду для «Осталось»)
        this._infoEls = { norad: null, aos: null, los: null, dur: null, remaining: null };

        /** Смещение клиентских часов относительно серверных (мс), для корректного «Осталось». */
        this._serverSkewMs = 0;
    }

    /**
     * Обновление геометрии: окружность ВСЕГДА занимает квадратную область с минимальными полями
     * (только под метки сторон света N/S/E/W и цифры азимута)
     */
    SkyView.prototype._updateGeometry = function() {
        const w = this.canvas.width;
        const h = this.canvas.height;
        // Минимальный отступ — только для вывода символов сторон света и меток азимута
        const padding = 16;

        this.centerX = w / 2;
        this.centerY = h / 2;
        this.radius = Math.min(w, h) / 2 - padding;
        if (this.radius < 20) { this.radius = 20; }
    };

    /**
     * Преобразование Az/El в координаты XY на canvas
     * Формула:
     *   ro = 1 - elevation / (π/2)  -- радиус: 0° = край, 90° = центр
     *   phi = π/2 - azimuth         -- угол: North вверху
     *
     * @param {number} azDeg - Азимут в градусах (0° = North, 90° = East)
     * @param {number} elDeg - Угол места в градусах (0° = горизонт, 90° = зенит)
     * @returns {{x: number, y: number}}
     */
    SkyView.prototype.azElToXY = function(azDeg, elDeg) {
        const azRad = azDeg * Math.PI / 180;
        const elRad = elDeg * Math.PI / 180;
        const halfPi = Math.PI / 2;

        // Нормализованный радиус (0 в центре при el=90°, 1 на краю при el=0°)
        const ro = 1 - elRad / halfPi;
        // Угол в системе координат canvas (North вверху)
        const phi = halfPi - azRad;

        return {
            x: this.centerX + this.radius * ro * Math.cos(phi),
            y: this.centerY - this.radius * ro * Math.sin(phi)
        };
    };

    /**
     * Получение точки на внешней окружности для заданного азимута
     * @param {number} azDeg - Азимут в градусах
     * @returns {{x: number, y: number}}
     */
    SkyView.prototype._getEdgePoint = function(azDeg) {
        return this.azElToXY(azDeg, 0);
    };

    /**
     * Отрисовка фона и сетки
     */
    SkyView.prototype._drawBackground = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;

        // Очистка фона
        ctx.fillStyle = this.colors.background;
        ctx.fillRect(0, 0, this.canvas.width, this.canvas.height);

        // Заливка неба (круг)
        ctx.beginPath();
        ctx.arc(cx, cy, r, 0, Math.PI * 2);
        ctx.fillStyle = this.colors.skyFill;
        ctx.fill();

        // Концентрические круги (каждые 30° elevation)
        ctx.strokeStyle = this.colors.grid;
        ctx.lineWidth = this._skyGridLineW;

        for (let el = 30; el <= 60; el += 30) {
            const ro = 1 - (el / 90);
            const circleR = r * ro;

            if (circleR > 0) {
                ctx.beginPath();
                ctx.arc(cx, cy, circleR, 0, Math.PI * 2);
                ctx.stroke();
            }
        }

        // Внешний круг (горизонт, 0° elevation)
        ctx.beginPath();
        ctx.arc(cx, cy, r, 0, Math.PI * 2);
        ctx.strokeStyle = this.colors.grid;
        ctx.lineWidth = this._skyGridOuterW;
        ctx.stroke();

        // Линии N-S и E-W
        ctx.strokeStyle = this.colors.grid;
        ctx.lineWidth = this._skyGridLineW;

        // N-S (вертикальная)
        ctx.beginPath();
        ctx.moveTo(cx, cy - r);
        ctx.lineTo(cx, cy + r);
        ctx.stroke();

        // E-W (горизонтальная)
        ctx.beginPath();
        ctx.moveTo(cx - r, cy);
        ctx.lineTo(cx + r, cy);
        ctx.stroke();

        // Диагональные линии сетки (опционально для 45°)
        if (this.options.azimuthStep === 45) {
            const diag = r * Math.SQRT1_2;
            ctx.setLineDash([3, 3]);

            // NE-SW
            ctx.beginPath();
            ctx.moveTo(cx + diag, cy - diag);
            ctx.lineTo(cx - diag, cy + diag);
            ctx.stroke();

            // NW-SE
            ctx.beginPath();
            ctx.moveTo(cx - diag, cy - diag);
            ctx.lineTo(cx + diag, cy + diag);
            ctx.stroke();

            ctx.setLineDash([]);
        }

        // Метки сторон света — приглушённый белый
        ctx.font = '15px sans-serif';
        ctx.fillStyle = this.colors.cardinalLabel;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        const labelOffset = r + 10; // Снаружи окружности
        ctx.fillText('N', cx, cy - labelOffset);
        ctx.fillText('S', cx, cy + labelOffset);
        ctx.fillText('E', cx + labelOffset, cy);
        ctx.fillText('W', cx - labelOffset, cy);

        // Метки азимута по внешней окружности
        this._drawAzimuthLabels();

        // Метки углов возвышения - контрастные и смещённые
        this._drawElevationLabels();
    };

    /**
     * Отрисовка меток азимута по внешней окружности: засечки от окружности, подписи чуть дальше
     */
    SkyView.prototype._drawAzimuthLabels = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;
        const step = this.options.azimuthStep;
        const tickLen = 5; // длина засечки от окружности
        const labelOffset = 14; // отступ подписи от окружности (было 8)

        ctx.font = '12px sans-serif';
        ctx.fillStyle = this.colors.azimuthLabel;

        for (let az = step; az < 360; az += step) {
            if (az === 90 || az === 180 || az === 270) { continue; }

            const azRad = az * Math.PI / 180;
            const phi = Math.PI / 2 - azRad;
            const cosP = Math.cos(phi);
            const sinP = Math.sin(phi);

            // Засечка: от окружности наружу
            ctx.strokeStyle = this.colors.azimuthLabel;
            ctx.lineWidth = 1;
            ctx.beginPath();
            ctx.moveTo(cx + r * cosP, cy - r * sinP);
            ctx.lineTo(cx + (r + tickLen) * cosP, cy - (r + tickLen) * sinP);
            ctx.stroke();

            // Подпись градусов чуть дальше от окружности
            const labelR = r + labelOffset;
            const x = cx + labelR * cosP;
            const y = cy - labelR * sinP;

            ctx.textAlign = 'center';
            ctx.textBaseline = 'middle';
            ctx.fillText(az + '°', x, y);
        }
    };

    /**
     * Отрисовка меток углов возвышения
     * Смещены влево и вверх для лучшей видимости
     */
    SkyView.prototype._drawElevationLabels = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;
        const offsetX = this.colors.elevationLabelOffsetX;
        const offsetY = this.colors.elevationLabelOffsetY;
        const fontSize = this.colors.elevationLabelSize;

        ctx.font = fontSize + 'px sans-serif';
        ctx.fillStyle = this.colors.elevationLabel;
        ctx.textAlign = 'right';
        ctx.textBaseline = 'bottom';

        for (let el = 30; el <= 60; el += 30) {
            const ro = 1 - (el / 90);
            const labelX = cx + offsetX;
            const labelY = cy - r * ro + offsetY;

            ctx.fillText(el + '°', labelX, labelY);
        }
    };

    /**
     * Отрисовка иконки наблюдателя в центре (зенит)
     * Простой маленький треугольник
     */
    SkyView.prototype._drawObserver = function() {
        if (!this.options.showObserver) {return;}

        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;

        // Размеры треугольника
        const size = 8;

        // Треугольник (вершиной вверх)
        ctx.fillStyle = this.colors.observer;
        ctx.strokeStyle = this.colors.observer;
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(cx, cy - size); // Вершина
        ctx.lineTo(cx - size, cy + size); // Левый нижний угол
        ctx.lineTo(cx + size, cy + size); // Правый нижний угол
        ctx.closePath();
        ctx.fill();
        ctx.stroke();
    };

    /**
     * Отрисовка стрелки направления на траектории.
     * @param {number} x - X координата
     * @param {number} y - Y координата
     * @param {number} angle - Угол направления в радианах
     * @param {string} [fillColor] - цвет заливки (если не задан — trackArrow)
     */
    SkyView.prototype._drawArrow = function(x, y, angle, fillColor) {
        const ctx = this.ctx;
        const size = 9;

        ctx.save();
        ctx.translate(x, y);
        ctx.rotate(angle);

        ctx.beginPath();
        ctx.moveTo(size, 0);
        ctx.lineTo(-size * 0.5, -size * 0.5);
        ctx.lineTo(-size * 0.3, 0);
        ctx.lineTo(-size * 0.5, size * 0.5);
        ctx.closePath();
        ctx.fillStyle = fillColor || this.colors.trackArrow;
        ctx.fill();
        ctx.strokeStyle = this.colors.arrowOutline;
        ctx.lineWidth = 1.5;
        ctx.stroke();

        ctx.restore();
    };

    /**
     * Отрисовка траектории пролёта
     * Линия продлевается до окружности горизонта (el=0) для совпадения с маркерами AOS/LOS
     */
    SkyView.prototype._drawTrack = function() {
        const ctx = this.ctx;
        const track = this.satellite.track;

        if (!track || track.length < 2) {
            this.passInfo.aosCanvasY = null;
            this.passInfo.losCanvasY = null;
            this.passInfo.aosAz = null;
            this.passInfo.losAz = null;
            return;
        }

        const visibleTrack = track.filter(function(p) { return p.el >= 0; });
        if (visibleTrack.length < 2) {
            this.passInfo.aosCanvasY = null;
            this.passInfo.losCanvasY = null;
            this.passInfo.aosAz = null;
            this.passInfo.losAz = null;
            return;
        }

        // AOS/LOS берутся из setPassTimes() — точные значения с бэкенда;
        // здесь НЕ перезаписываем, чтобы «Осталось» совпадало с таблицей.

        // Азимуты пересечения с горизонтом (el=0) для AOS и LOS
        const startAz = this._findHorizonCrossing(visibleTrack[0], visibleTrack[1]);
        const endAz = this._findHorizonCrossing(
            visibleTrack[visibleTrack.length - 1],
            visibleTrack[visibleTrack.length - 2]
        );

        const aosEdge = this.azElToXY(startAz, 0);
        const losEdge = this.azElToXY(endAz, 0);

        // Внутренние точки (видимая часть трека) — для стрелок
        const innerPoints = [];
        for (let i = 0; i < visibleTrack.length; i++) {
            const tp = visibleTrack[i];
            const p = this.azElToXY(tp.az, tp.el);
            innerPoints.push({ x: p.x, y: p.y, time: tp.time, el: tp.el, az: tp.az });
        }

        // Полный путь: от края окружности (AOS) через видимые точки до края (LOS)
        const allPoints = [
            { x: aosEdge.x, y: aosEdge.y, time: visibleTrack[0].time, el: 0, az: startAz }
        ].concat(innerPoints).concat([
            { x: losEdge.x, y: losEdge.y, time: visibleTrack[visibleTrack.length - 1].time, el: 0, az: endAz }
        ]);

        // Рисуем линию через все точки (от края до края окружности)
        ctx.strokeStyle = this.colors.track;
        ctx.lineWidth = 2;
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';
        ctx.beginPath();

        for (let i = 0; i < allPoints.length; i++) {
            if (i === 0) {
                ctx.moveTo(allPoints[i].x, allPoints[i].y);
            } else {
                ctx.lineTo(allPoints[i].x, allPoints[i].y);
            }
        }

        ctx.stroke();

        // Стрелки — только на внутренних точках (не на краях окружности)
        this._drawTrackArrows(innerPoints, visibleTrack);

        // Маркеры AOS/LOS — точно на концах линии
        this._drawAosLosMarkers(allPoints);
    };

    /**
     * Отрисовка стрелок направления на траектории.
     * Всегда рисует стрелку в точке TCA (макс. элевация) + дополнительные по интервалу.
     * @param {Array} points - массив точек с x, y, time, el
     * @param {Array} visibleTrack - массив точек трека с time
     * @param {string} [arrowColor] - цвет стрелок (для текущего спутника — selectedTrack)
     */
    SkyView.prototype._drawTrackArrows = function(points, visibleTrack, arrowColor) {
        if (points.length < 3) {return;}

        // Стрелка в точке TCA (максимальная элевация) — рисуется всегда
        let tcaIdx = Math.floor((points.length - 1) / 2);
        let maxEl = -Infinity;
        for (let i = 1; i < points.length - 1; i++) {
            if (points[i].el > maxEl) {
                maxEl = points[i].el;
                tcaIdx = i;
            }
        }

        let prev = points[tcaIdx - 1];
        let curr = points[tcaIdx];
        let next = points[tcaIdx + 1];
        let dx = next.x - prev.x;
        let dy = next.y - prev.y;
        this._drawArrow(curr.x, curr.y, Math.atan2(dy, dx), arrowColor);

        // Дополнительные стрелки по интервалу (для длинных пролётов)
        const arrowInterval = this.options.arrowInterval;
        let lastArrowTime = points[0].time;

        for (let i = 1; i < points.length - 1; i++) {
            if (i === tcaIdx) {continue;}

            const point = visibleTrack[i];
            if (point.time - lastArrowTime >= arrowInterval) {
                prev = points[i - 1];
                curr = points[i];
                next = points[i + 1];
                dx = next.x - prev.x;
                dy = next.y - prev.y;
                this._drawArrow(curr.x, curr.y, Math.atan2(dy, dx), arrowColor);
                lastArrowTime = point.time;
            }
        }
    };

    /**
     * Отрисовка маркеров AOS и LOS точно на концах линии траектории
     * @param {Array} points - Полный массив точек (первая = AOS на окружности, последняя = LOS)
     */
    SkyView.prototype._drawAosLosMarkers = function(points) {
        if (points.length < 2) {return;}

        const ctx = this.ctx;
        const markerRadius = 6;

        const aosPoint = points[0];
        const losPoint = points[points.length - 1];

        this.passInfo.aosCanvasY = aosPoint.y;
        this.passInfo.losCanvasY = losPoint.y;
        this.passInfo.aosAz = aosPoint.az;
        this.passInfo.losAz = losPoint.az;

        // AOS маркер (зелёный) — начало видимости
        ctx.beginPath();
        ctx.arc(aosPoint.x, aosPoint.y, markerRadius, 0, Math.PI * 2);
        ctx.fillStyle = this.colors.aosMarker;
        ctx.fill();
        ctx.strokeStyle = this.colors.markerBorder;
        ctx.lineWidth = 2;
        ctx.stroke();

        // LOS маркер (красный) — конец видимости
        ctx.beginPath();
        ctx.arc(losPoint.x, losPoint.y, markerRadius, 0, Math.PI * 2);
        ctx.fillStyle = this.colors.losMarker;
        ctx.fill();
        ctx.strokeStyle = this.colors.markerBorder;
        ctx.lineWidth = 2;
        ctx.stroke();
    };

    /**
     * Находит азимут точки пересечения траектории с горизонтом (el=0)
     * Использует линейную интерполяцию между двумя точками
     * @param {Object} p1 - Первая точка {az, el}
     * @param {Object} p2 - Вторая точка {az, el}
     * @returns {number} - Азимут точки пересечения с el=0
     */
    SkyView.prototype._findHorizonCrossing = function(p1, p2) {
        // Если обе точки на горизонте или выше, возвращаем азимут первой
        if (p1.el <= 0.01) {return p1.az;}

        // Линейная интерполяция для нахождения азимута при el=0
        // az = az1 + (az2 - az1) * (0 - el1) / (el2 - el1)
        const deltaEl = p2.el - p1.el;

        if (Math.abs(deltaEl) < 0.001) {
            // Точки почти на одной высоте
            return p1.az;
        }

        const t = (0 - p1.el) / deltaEl;

        // Обработка перехода через 0°/360°
        let deltaAz = p2.az - p1.az;
        if (deltaAz > 180) {deltaAz -= 360;}
        if (deltaAz < -180) {deltaAz += 360;}

        let az = p1.az + deltaAz * t;

        // Нормализация
        while (az < 0) {az += 360;}
        while (az >= 360) {az -= 360;}

        return az;
    };

    /**
     * Отрисовка ауры (окружности) вокруг спутника. Рисуется на заднем плане (до траектории и значка).
     * @param {number} [x] - X (если не задано — вычисляется из currentPos)
     * @param {number} [y] - Y (если не задано — вычисляется из currentPos)
     */
    SkyView.prototype._drawSatelliteAura = function(x, y) {
        if (!this.options.showSatelliteAura) {return;}

        // Если x,y не переданы — вычисляем из текущей позиции спутника
        if (x === undefined || y === undefined) {
            const pos = this.satellite.currentPos;
            if (!pos || pos.el <= 0) {return;}
            const p = this.azElToXY(pos.az, pos.el);
            x = p.x;
            y = p.y;
        }

        const ctx = this.ctx;
        const auraR = this.options.satelliteAuraRadius;

        // Пульсирующая аура
        const pulse = 1 + 0.15 * Math.sin(this._animationPhase * 2);
        const currentR = auraR * pulse;

        // Градиентная заливка (тёмная в центре)
        const gradient = ctx.createRadialGradient(x, y, 0, x, y, currentR);
        gradient.addColorStop(0, this.colors.satelliteAura);
        gradient.addColorStop(1, 'rgba(180, 80, 80, 0)');

        ctx.beginPath();
        ctx.arc(x, y, currentR, 0, Math.PI * 2);
        ctx.fillStyle = gradient;
        ctx.fill();

        // Граница ауры
        ctx.beginPath();
        ctx.arc(x, y, currentR, 0, Math.PI * 2);
        ctx.strokeStyle = this.colors.satelliteAuraBorder;
        ctx.lineWidth = 1;
        ctx.stroke();
    };

    /**
     * Отрисовка спутника с анимацией
     */
    SkyView.prototype._drawSatellite = function() {
        const ctx = this.ctx;
        const pos = this.satellite.currentPos;

        if (!pos || pos.el <= 0) {
            return;
        }

        const p = this.azElToXY(pos.az, pos.el);

        // Аура рисуется в draw() до траектории, спутник — поверх

        // Анимация свечения
        const glowPulse = 0.5 + 0.5 * Math.sin(this._animationPhase * 3);

        // Внешнее свечение
        ctx.shadowColor = this.colors.satellite;
        ctx.shadowBlur = 8 + 4 * glowPulse;

        // Иконка спутника (улучшенный дизайн)
        const size = 8;

        // Центральный блок (корпус)
        ctx.fillStyle = this.colors.satellite;
        ctx.fillRect(p.x - size / 2, p.y - size / 2, size, size);

        // "Солнечные панели" - с градиентом
        const panelWidth = size * 1.2;
        const panelHeight = size * 0.5;

        // Левая панель
        ctx.fillRect(p.x - size / 2 - panelWidth - 2, p.y - panelHeight / 2, panelWidth, panelHeight);
        // Правая панель
        ctx.fillRect(p.x + size / 2 + 2, p.y - panelHeight / 2, panelWidth, panelHeight);

        // Линии на панелях (детализация)
        ctx.strokeStyle = this.colors.satPanelLine;
        ctx.lineWidth = 1;

        // Линии на левой панели
        for (let i = 1; i < 3; i++) {
            const lx = p.x - size / 2 - panelWidth - 2 + (panelWidth / 3) * i;
            ctx.beginPath();
            ctx.moveTo(lx, p.y - panelHeight / 2);
            ctx.lineTo(lx, p.y + panelHeight / 2);
            ctx.stroke();
        }

        // Линии на правой панели
        for (let i = 1; i < 3; i++) {
            const rx = p.x + size / 2 + 2 + (panelWidth / 3) * i;
            ctx.beginPath();
            ctx.moveTo(rx, p.y - panelHeight / 2);
            ctx.lineTo(rx, p.y + panelHeight / 2);
            ctx.stroke();
        }

        // Обводка корпуса
        ctx.shadowBlur = 0;
        ctx.strokeStyle = this.colors.satBodyOutline;
        ctx.lineWidth = 1;
        ctx.strokeRect(p.x - size / 2, p.y - size / 2, size, size);

        // Подписи спутника на слежении — обводка из темы (светлая тема: светлый ореол)
        ctx.shadowBlur = 0;
        ctx.lineJoin = 'round';
        ctx.miterLimit = 2;
        ctx.strokeStyle = this.colors.canvasTextStroke;
        ctx.lineWidth = 3;

        if (this.satellite.name) {
            const nm = _shortName(this.satellite.name);
            ctx.font = 'bold 12px sans-serif';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'bottom';
            ctx.strokeText(nm, p.x, p.y - 12);
            ctx.fillStyle = this.colors.satLabel;
            ctx.fillText(nm, p.x, p.y - 12);
        }

        const azel = '[' + pos.az.toFixed(1) + '\u00b0/' + pos.el.toFixed(1) + '\u00b0]';
        ctx.font = 'bold 12px sans-serif';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        ctx.strokeText(azel, p.x, p.y + 10);
        ctx.fillStyle = this.colors.satLabel;
        ctx.fillText(azel, p.x, p.y + 10);
    };

    /**
     * Анимация волн сигнала от спутника к наблюдателю
     * @param {number} x - X координата спутника
     * @param {number} y - Y координата спутника
     */
    SkyView.prototype._drawSignalWaves = function(x, y) {
        const ctx = this.ctx;
        const phase = this._animationPhase;
        const cx = this.centerX;
        const cy = this.centerY;

        // Вычисляем направление к наблюдателю (центру)
        const dx = cx - x;
        const dy = cy - y;
        const dist = Math.sqrt(dx * dx + dy * dy);

        // Если спутник слишком близко к центру, не рисуем волны
        if (dist < 20) {return;}

        // Угол направления к наблюдателю
        const angleToObserver = Math.atan2(dy, dx);

        // Смещение центра волн в сторону наблюдателя
        const waveOffsetX = Math.cos(angleToObserver) * 12;
        const waveOffsetY = Math.sin(angleToObserver) * 12;
        const waveCenterX = x + waveOffsetX;
        const waveCenterY = y + waveOffsetY;

        // Рисуем 2 волны с разной фазой, направленные к наблюдателю
        for (let i = 0; i < 2; i++) {
            const wavePhase = (phase + i * Math.PI) % (Math.PI * 2);
            const waveProgress = wavePhase / (Math.PI * 2);

            if (waveProgress < 0.7) {
                const waveR = 8 + waveProgress * 20;
                const alpha = 0.5 * (1 - waveProgress / 0.7);

                // Дуга направлена к наблюдателю
                const arcStart = angleToObserver - Math.PI * 0.35;
                const arcEnd = angleToObserver + Math.PI * 0.35;

                ctx.beginPath();
                ctx.arc(waveCenterX, waveCenterY, waveR, arcStart, arcEnd);
                ctx.strokeStyle = 'rgba(' + this.colors.signalWaveRgb + ',' + alpha + ')';
                ctx.lineWidth = 2;
                ctx.stroke();
            }
        }
    };

    /**
     * Форматирование времени из timestamp
     * @param {number} timestamp - Unix timestamp в мс
     * @returns {string} - Форматированное время HH:MM:SS
     */
    SkyView.prototype._formatTime = function(timestamp) {
        if (!timestamp) {return '--:--:--';}
        const date = new Date(timestamp);
        return date.toTimeString().split(' ')[0];
    };

    /**
     * Форматирование длительности
     * @param {number} durationMs - Длительность в миллисекундах
     * @returns {string} - Форматированная длительность (Xm Ys)
     */
    SkyView.prototype._formatDuration = function(durationMs) {
        if (!durationMs || durationMs < 0) {return '--:--';}
        const totalSec = Math.floor(durationMs / 1000);
        const min = Math.floor(totalSec / 60);
        const sec = totalSec % 60;
        return min + 'm ' + (sec < 10 ? '0' : '') + sec + 's';
    };

    /**
     * Обновление фазы анимации
     */
    SkyView.prototype._updateAnimation = function() {
        const now = Date.now();
        const delta = now - this._lastAnimTime;
        this._lastAnimTime = now;

        // Обновляем фазу анимации
        this._animationPhase += (delta / 1000) * this.options.animationSpeed * 2;
        if (this._animationPhase > Math.PI * 2) {
            this._animationPhase -= Math.PI * 2;
        }
    };

    /**
     * Главная функция отрисовки
     */
    SkyView.prototype.draw = function() {
        this._updateAnimation();
        this._updateGeometry();
        this._drawBackground();
        this._drawObserver();
        // Слой 1: вторичные (серые пунктиры).
        this._drawSecondaryLayer();
        // Слой 2: выбранный спутник (оранжевый), если отличается от tracking.
        if (this._selectedSatellite.noradId &&
            this._selectedSatellite.noradId !== this.satellite.noradId) {
            this._drawSelectedLayer();
        }
        // Слой 3: спутник на слежении (текущий стиль).
        if (this.satellite.noradId) {
            this._drawSatelliteAura();
            this._drawTrack();
            this._drawSatellite();
        }
    };

    /**
     * Установка информации о спутнике
     * @param {string} name - Название спутника
     * @param {number|string} [noradId] - NORAD ID спутника
     */
    SkyView.prototype.setSatelliteInfo = function(name, noradId) {
        this.satellite.name = name;
        if (noradId !== undefined) {
            this.satellite.noradId = noradId;
        }
        this._updateInfoPanelDOM();
    };

    /**
     * Установка текущей позиции спутника
     * @param {number} az - Азимут в градусах
     * @param {number} el - Угол места в градусах
     */
    SkyView.prototype.setSatellitePosition = function(az, el) {
        const a = Number(az);
        const e = Number(el);
        if (isNaN(a) || isNaN(e)) {
            this.satellite.currentPos = null;
            return;
        }
        this.satellite.currentPos = { az: a, el: e };
    };

    /**
     * Очистка траектории
     */
    SkyView.prototype.clearTrack = function() {
        this.satellite.track = [];
        this.passInfo = { aosTime: null, losTime: null, maxElTime: null, aosCanvasY: null, losCanvasY: null, aosAz: null, losAz: null };
        this._updateInfoPanelDOM();
    };

    /**
     * Добавление точки траектории
     * @param {number} az - Азимут
     * @param {number} el - Угол места
     * @param {number} time - Время (timestamp)
     */
    SkyView.prototype.addTrackPoint = function(az, el, time) {
        this.satellite.track.push({ az: az, el: el, time: time });
    };

    /**
     * Установка траектории целиком
     * @param {Array} track - Массив точек [{az, el, time}, ...]
     */
    SkyView.prototype.setTrack = function(track) {
        this.satellite.track = track || [];

        // Автоматически определяем времена AOS/LOS
        if (track && track.length > 0) {
            const visible = track.filter(function(p) { return p.el > 0; });
            if (visible.length > 0) {
                this.passInfo.aosTime = visible[0].time;
                this.passInfo.losTime = visible[visible.length - 1].time;
            }
        }
        this._updateInfoPanelDOM();
    };

    /**
     * Установка времён пролёта вручную
     * @param {number} aosTime - Время AOS (timestamp)
     * @param {number} losTime - Время LOS (timestamp)
     */
    SkyView.prototype.setPassTimes = function(aosTime, losTime) {
        this.passInfo.aosTime = aosTime;
        this.passInfo.losTime = losTime;
        this._updateInfoPanelDOM();
    };

    /**
     * Привязка DOM-элементов для текстового блока под графиком (AOS, LOS, Длит., Осталось)
     * @param {Object} els - { aos, los, dur, remaining } — id строки или HTMLElement
     */
    SkyView.prototype.setInfoElements = function(els) {
        const getEl = function(v) {
            if (!v) {return null;}
            return typeof v === 'string' ? document.getElementById(v) : v;
        };
        this._infoEls = {
            norad: getEl(els.norad),
            aos: getEl(els.aos),
            los: getEl(els.los),
            dur: getEl(els.dur),
            remaining: getEl(els.remaining)
        };
        this._updateInfoPanelDOM();
    };

    /** Обновление текстового блока под графиком: AOS, LOS, Длит., время до конца сеанса (Осталось).
     * При отображении выбранного спутника (отличного от отслеживаемого) показываются данные выбранного. */
    SkyView.prototype._updateInfoPanelDOM = function() {
        const e = this._infoEls;
        if (!e.aos && !e.los && !e.dur && !e.remaining) {return;}

        const showSelected = this._selectedSatellite.noradId && this._selectedSatellite.noradId !== this.satellite.noradId;
        const info = showSelected ? this._selectedPassInfo : this.passInfo;
        const noradId = showSelected ? this._selectedSatellite.noradId : this.satellite.noradId;

        const now = Date.now() + (this._serverSkewMs || 0);
        const aosStr = this._formatTime(info.aosTime);
        const losStr = this._formatTime(info.losTime);
        const durMs = (info.aosTime && info.losTime) ? (info.losTime - info.aosTime) : 0;
        const durStr = this._formatDuration(durMs);
        let remainingStr = '—';
        if (info.aosTime && info.losTime && now >= info.aosTime && now <= info.losTime) {
            const remainingMs = info.losTime - now;
            if (remainingMs > 0) {
                remainingStr = this._formatDuration(remainingMs);
            }
        }

        if (e.norad) {e.norad.textContent = noradId ? String(noradId) : '—';}
        if (e.aos) {e.aos.textContent = aosStr;}
        if (e.los) {e.los.textContent = losStr;}
        if (e.dur) {e.dur.textContent = durStr;}
        if (e.remaining) {e.remaining.textContent = remainingStr;}
    };

    /**
     * Включение/выключение отображения ауры спутника
     * @param {boolean} show - Показывать ауру
     */
    SkyView.prototype.setShowSatelliteAura = function(show) {
        this.options.showSatelliteAura = show;
    };

    /**
     * Установка радиуса ауры спутника
     * @param {number} radius - Радиус в пикселях
     */
    SkyView.prototype.setSatelliteAuraRadius = function(radius) {
        this.options.satelliteAuraRadius = radius;
    };

    /**
     * Обновление цветовой схемы
     * @param {Object} colors - Объект с цветами для обновления
     */
    SkyView.prototype.setColors = function(colors) {
        Object.assign(this.colors, colors);
    };


    // ========== Текущий (выбранный) спутник ==========

    SkyView.prototype.setSelectedSatellitePosition = function(az, el) {
        this._selectedSatellite.currentPos = { az: Number(az), el: Number(el) };
    };

    SkyView.prototype.setSelectedSatelliteInfo = function(name, noradId) {
        this._selectedSatellite.name = name || '';
        this._selectedSatellite.noradId = noradId || null;
    };

    SkyView.prototype.setSelectedTrack = function(track) {
        this._selectedSatellite.track = track || [];
        this._updateInfoPanelDOM();
    };

    /** Синхронизация часов: разница между серверным и клиентским временем (мс). */
    SkyView.prototype.setServerSkew = function(skewMs) {
        this._serverSkewMs = skewMs || 0;
    };

    /** Установка времён AOS/LOS для выбранного спутника (для инфопанели под графиком). */
    SkyView.prototype.setSelectedPassTimes = function(aosTime, losTime) {
        this._selectedPassInfo.aosTime = aosTime || null;
        this._selectedPassInfo.losTime = losTime || null;
        this._updateInfoPanelDOM();
    };

    SkyView.prototype.clearSelectedSatellite = function() {
        this._selectedSatellite = { name: '', noradId: null, track: [], currentPos: null };
        this._selectedPassInfo = { aosTime: null, losTime: null };
        this._updateInfoPanelDOM();
    };

    /**
     * Значок спутника такой же формы, как у спутника на слежении (корпус + панели + линии),
     * без анимации и без пульсирующего круга. Используется для текущего (выбранного) спутника.
     * @param {CanvasRenderingContext2D} ctx
     * @param {number} x - центр X
     * @param {number} y - центр Y
     * @param {string} fillColor - цвет заливки (например selectedMarker)
     */
    SkyView.prototype._drawSatelliteIconStatic = function(ctx, x, y, fillColor) {
        const size = 8; // как у маркера на слежении
        const panelWidth = size * 1.2;
        const panelHeight = size * 0.5;

        ctx.fillStyle = fillColor;
        // Центральный блок (корпус)
        ctx.fillRect(x - size / 2, y - size / 2, size, size);
        // Солнечные панели
        ctx.fillRect(x - size / 2 - panelWidth - 2, y - panelHeight / 2, panelWidth, panelHeight);
        ctx.fillRect(x + size / 2 + 2, y - panelHeight / 2, panelWidth, panelHeight);
        // Линии на панелях (детализация)
        ctx.strokeStyle = this.colors.satPanelLine;
        ctx.lineWidth = 1;
        for (let i = 1; i < 3; i++) {
            const lx = x - size / 2 - panelWidth - 2 + (panelWidth / 3) * i;
            ctx.beginPath();
            ctx.moveTo(lx, y - panelHeight / 2);
            ctx.lineTo(lx, y + panelHeight / 2);
            ctx.stroke();
        }
        for (let j = 1; j < 3; j++) {
            const rx = x + size / 2 + 2 + (panelWidth / 3) * j;
            ctx.beginPath();
            ctx.moveTo(rx, y - panelHeight / 2);
            ctx.lineTo(rx, y + panelHeight / 2);
            ctx.stroke();
        }
        // Обводка корпуса
        ctx.strokeStyle = this.colors.satBodyOutline;
        ctx.lineWidth = 1;
        ctx.strokeRect(x - size / 2, y - size / 2, size, size);
    };

    /**
     * Отрисовка слоя текущего (выбранного) спутника: сплошная жёлтая трасса
     * от горизонта до горизонта, стрелки направления, маркеры AOS/LOS на лимбе.
     * @private
     */
    SkyView.prototype._drawSelectedLayer = function() {
        const sel = this._selectedSatellite;
        const ctx = this.ctx;

        if (sel.track && sel.track.length >= 2) {
            const visibleTrack = sel.track.filter(function(p) { return p.el >= 0; });
            if (visibleTrack.length < 2) {
                // Недостаточно видимых точек — ничего не рисуем.
            } else {
                // Азимуты пересечения с горизонтом (el=0) для AOS и LOS.
                const startAz = this._findHorizonCrossing(visibleTrack[0], visibleTrack[1]);
                const endAz = this._findHorizonCrossing(
                    visibleTrack[visibleTrack.length - 1],
                    visibleTrack[visibleTrack.length - 2]
                );
                const aosEdge = this.azElToXY(startAz, 0);
                const losEdge = this.azElToXY(endAz, 0);

                // Внутренние точки (видимая часть трека).
                const innerPoints = [];
                for (let i = 0; i < visibleTrack.length; i++) {
                    const tp = visibleTrack[i];
                    const p = this.azElToXY(tp.az, tp.el);
                    innerPoints.push({ x: p.x, y: p.y, time: tp.time, el: tp.el, az: tp.az });
                }

                // Полный путь: от горизонта (AOS) через видимые точки до горизонта (LOS).
                const allPoints = [
                    { x: aosEdge.x, y: aosEdge.y, time: visibleTrack[0].time, el: 0, az: startAz }
                ].concat(innerPoints).concat([
                    { x: losEdge.x, y: losEdge.y, time: visibleTrack[visibleTrack.length - 1].time, el: 0, az: endAz }
                ]);

                // Сплошная жёлтая линия от горизонта до горизонта.
                ctx.strokeStyle = this.colors.selectedTrack;
                ctx.lineWidth = 2;
                ctx.setLineDash([]);
                ctx.lineCap = 'round';
                ctx.lineJoin = 'round';
                ctx.beginPath();
                for (let j = 0; j < allPoints.length; j++) {
                    if (j === 0) { ctx.moveTo(allPoints[j].x, allPoints[j].y); }
                    else { ctx.lineTo(allPoints[j].x, allPoints[j].y); }
                }
                ctx.stroke();

                // Стрелки направления (цвет трека выбранного спутника).
                this._drawTrackArrows(innerPoints, visibleTrack, this.colors.selectedTrack);

                // Маркеры AOS/LOS на окружности горизонта.
                const markerRadius = 5;
                ctx.beginPath();
                ctx.arc(aosEdge.x, aosEdge.y, markerRadius, 0, Math.PI * 2);
                ctx.fillStyle = this.colors.aosMarker;
                ctx.fill();
                ctx.strokeStyle = this.colors.markerBorder;
                ctx.lineWidth = 1.5;
                ctx.stroke();
                ctx.beginPath();
                ctx.arc(losEdge.x, losEdge.y, markerRadius, 0, Math.PI * 2);
                ctx.fillStyle = this.colors.losMarker;
                ctx.fill();
                ctx.stroke();
            }
        }

        // Маркер — такой же значок, как на слежении, другим цветом и без анимации.
        if (sel.currentPos && sel.currentPos.el > 0) {
            const mp = this.azElToXY(sel.currentPos.az, sel.currentPos.el);
            this._drawSatelliteIconStatic(ctx, mp.x, mp.y, this.colors.selectedMarker);

            ctx.lineJoin = 'round';
            ctx.miterLimit = 2;

            if (sel.name) {
                const nameLabel = _shortName(sel.name);
                ctx.font = 'bold 12px sans-serif';
                ctx.textAlign = 'center';
                ctx.textBaseline = 'bottom';
                ctx.strokeStyle = this.colors.canvasTextStroke;
                ctx.lineWidth = 3;
                ctx.strokeText(nameLabel, mp.x, mp.y - 12);
                ctx.fillStyle = this.colors.selectedSatLabel;
                ctx.fillText(nameLabel, mp.x, mp.y - 12);
            }

            const azelLabel = '[' + sel.currentPos.az.toFixed(1) + '\u00b0/' + sel.currentPos.el.toFixed(1) + '\u00b0]';
            ctx.font = 'bold 12px sans-serif';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'top';
            ctx.strokeStyle = this.colors.canvasTextStroke;
            ctx.lineWidth = 3;
            ctx.strokeText(azelLabel, mp.x, mp.y + 10);
            ctx.fillStyle = this.colors.selectedSatLabel;
            ctx.fillText(azelLabel, mp.x, mp.y + 10);
        }
    };

    // ========== Вторичные спутники ==========

    /**
     * Обновление вторичных спутников группы.
     * @param {Array} satArray — массив {noradId, name, az, el, track, isVisible}.
     */
    SkyView.prototype.setSecondaryPositions = function(satArray) {
        if (!satArray) { return; }
        const newMap = {};
        for (let i = 0; i < satArray.length; i++) {
            const s = satArray[i];
            if (!s || !s.noradId) { continue; }
            const existing = this._secondarySatellites[s.noradId] || {};
            newMap[s.noradId] = {
                noradId: s.noradId,
                name: s.name || existing.name || '',
                currentPos: (s.az !== null && s.el !== null) ? { az: s.az, el: s.el } : existing.currentPos || null,
                track: existing.track || null,
                isVisible: s.isVisible !== undefined ? s.isVisible : true
            };
        }
        this._secondarySatellites = newMap;
    };

    /**
     * Очистка всех вторичных спутников (при смене группы / смене primary).
     */
    SkyView.prototype.clearSecondarySatellites = function() {
        this._secondarySatellites = {};
    };

    /**
     * Обновление трека вторичного спутника.
     * @param {number} noradId — NORAD ID.
     * @param {Array} track — массив [{az, el, time}, ...].
     */
    SkyView.prototype.setSecondaryTrack = function(noradId, track) {
        // Обновляем только существующие записи (не воскрешаем удалённые).
        if (this._secondarySatellites[noradId]) {
            this._secondarySatellites[noradId].track = track;
        }
    };

    /**
     * Отрисовка вторичных спутников (траектории + маркеры).
     * @private
     */
    SkyView.prototype._drawSecondaryLayer = function() {
        const ids = Object.keys(this._secondarySatellites);
        const sm = window._stateManager;
        for (let i = 0; i < ids.length; i++) {
            const sat = this._secondarySatellites[ids[i]];
            const nid = parseInt(ids[i], 10);
            const markerColor = sm ? sm.getMarkerColor(nid) : null;
            const trackColor = sm ? sm.getTrackColor(nid) : null;
            if (sat.track && sat.track.length > 0) {
                this._drawSecondaryTrack(sat, trackColor);
            }
            if (sat.currentPos && sat.currentPos.el > 0) {
                this._drawSecondaryMarker(sat, markerColor);
            }
        }
    };

    /**
     * Пунктирная траектория вторичного спутника (серый цвет для читаемости).
     * @private
     */
    SkyView.prototype._drawSecondaryTrack = function(sat, paletteColor) {
        const ctx = this.ctx;
        const track = sat.track;
        if (!track || track.length < 2) { return; }

        const isLight = typeof getThemeId === 'function' && getThemeId() === 'light';
        ctx.strokeStyle = paletteColor || 'rgba(160, 160, 160, 0.85)';
        ctx.lineWidth = isLight ? 1.05 : 1.5;
        ctx.setLineDash(isLight ? [3, 5] : [4, 3]);
        ctx.beginPath();

        let started = false;
        for (let i = 0; i < track.length; i++) {
            const pt = track[i];
            if (pt.el < 0) { started = false; continue; }
            const xy = this.azElToXY(pt.az, pt.el);
            if (!started) {
                ctx.moveTo(xy.x, xy.y);
                started = true;
            } else {
                ctx.lineTo(xy.x, xy.y);
            }
        }
        ctx.stroke();
        ctx.setLineDash([]);
    };

    /**
     * Маркер вторичного спутника — серый заполненный кружок + обводка (хорошо виден на фоне).
     * @private
     */
    SkyView.prototype._drawSecondaryMarker = function(sat, paletteColor) {
        const ctx = this.ctx;
        const pos = sat.currentPos;
        if (!pos || pos.el < 0) { return; }

        const xy = this.azElToXY(pos.az, pos.el);
        const isLight = typeof getThemeId === 'function' && getThemeId() === 'light';
        const r = isLight ? 3 : 4;

        ctx.beginPath();
        ctx.arc(xy.x, xy.y, r, 0, Math.PI * 2);
        ctx.fillStyle = paletteColor || 'rgba(140, 140, 140, 0.95)';
        ctx.fill();
        ctx.strokeStyle = paletteColor
            ? (isLight ? 'rgba(42,48,58,0.32)' : 'rgba(255,255,255,0.7)')
            : 'rgba(220, 220, 220, 0.9)';
        ctx.lineWidth = isLight ? 1 : 1.5;
        ctx.stroke();
    };

    // Экспорт
    window.SkyView = SkyView;

})();
