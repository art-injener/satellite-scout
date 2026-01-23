// Sky View - Азимутальная проекция неба для отображения спутников
// Улучшенная версия с анимацией и расширенной конфигурацией

(function() {
    'use strict';

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
            showSatelliteAura: true,      // Показывать окружность вокруг спутника
            showObserver: true,            // Показывать иконку наблюдателя
            azimuthStep: 30,               // Шаг меток азимута (30° или 45°)
            arrowInterval: 120000,         // Интервал стрелок на траектории (2 минуты)
            satelliteAuraRadius: 20,       // Радиус ауры спутника
            animationSpeed: 1              // Скорость анимации
        }, options || {});

        /**
         * Цветовая схема (тёмная тема)
         * Все цвета легко настраиваются для интеграции
         */
        this.colors = {
            // Фон
            background: '#0a0e14',
            skyFill: '#1a2a3a',
            
            // Сетка
            grid: '#006666',
            gridText: '#008080',
            
            // Метки углов возвышения
            elevationLabel: '#ffffff',     // Белый цвет для контраста
            elevationLabelOffsetX: -5,     // Смещение влево от центра
            elevationLabelOffsetY: -5,     // Смещение вверх
            elevationLabelSize: 8,         // Размер шрифта
            
            // Метки азимута на внешней окружности
            azimuthLabel: '#00cccc',       // Бирюзовый цвет для азимутальных меток
            
            // Траектория
            track: '#00cc00',
            trackArrow: '#88ff88',         // Цвет стрелок направления
            
            // Маркеры AOS/LOS
            aosMarker: '#00ff00',          // Зелёный - начало видимости
            losMarker: '#ff4444',          // Красный - конец видимости
            markerBorder: '#ffffff',
            
            // Спутник
            satellite: '#00ffff',
            satelliteGlow: 'rgba(0, 255, 255, 0.3)',
            satelliteSignal: 'rgba(0, 255, 200, 0.5)',
            satLabel: '#ffffff',
            
            // Аура вокруг спутника
            satelliteAura: 'rgba(0, 200, 255, 0.15)',
            satelliteAuraBorder: 'rgba(0, 200, 255, 0.4)',
            
            // Наблюдатель (центр)
            observer: '#ffaa00',
            observerSecondary: '#ff6600',
            
            // Информационный блок
            infoText: '#00d4aa',
            infoLabel: '#888888',
            timeText: '#00a8ff'
        };

        // Расчёт геометрии
        this._updateGeometry();

        // Данные спутника
        this.satellite = {
            name: '',
            track: [],
            currentPos: null
        };

        // Данные о пролёте (времена)
        this.passInfo = {
            aosTime: null,    // Время начала наблюдения (timestamp)
            losTime: null,    // Время окончания наблюдения (timestamp)
            maxElTime: null   // Время максимального угла места
        };

        // Observer
        this.observer = {
            lat: 47.23,
            lon: 39.7,
            name: 'Rostov-on-Don'
        };

        // Анимация
        this._animationPhase = 0;
        this._lastAnimTime = 0;
    }

    /**
     * Обновление геометрии при изменении размера
     * Учитывает дополнительное пространство снизу для информационной панели
     */
    SkyView.prototype._updateGeometry = function() {
        const padding = 30;           // Отступ для меток азимута
        const infoPanelHeight = 50;   // Высота информационной панели снизу
        
        this.infoPanelHeight = infoPanelHeight;
        this.centerX = this.canvas.width / 2;
        // Смещаем центр вверх, чтобы освободить место для инфо-панели
        this.centerY = (this.canvas.height - infoPanelHeight) / 2;
        this.radius = Math.min(this.centerX, this.centerY) - padding;
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
        ctx.lineWidth = 1;

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
        ctx.lineWidth = 2;
        ctx.stroke();

        // Линии N-S и E-W
        ctx.strokeStyle = this.colors.grid;
        ctx.lineWidth = 1;

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

        // Метки сторон света
        ctx.font = 'bold 12px sans-serif';
        ctx.fillStyle = this.colors.gridText;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        const labelOffset = r + 16;
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
     * Отрисовка меток азимута по внешней окружности
     */
    SkyView.prototype._drawAzimuthLabels = function() {
        const ctx = this.ctx;
        const cx = this.centerX;
        const cy = this.centerY;
        const r = this.radius;
        const step = this.options.azimuthStep;

        ctx.font = '9px sans-serif';
        ctx.fillStyle = this.colors.azimuthLabel;

        // Рисуем метки кроме основных направлений (N, E, S, W)
        for (let az = step; az < 360; az += step) {
            // Пропускаем основные направления
            if (az === 90 || az === 180 || az === 270) continue;

            const azRad = az * Math.PI / 180;
            const phi = Math.PI / 2 - azRad;
            
            // Позиция метки чуть за пределами круга
            const labelR = r + 8;
            const x = cx + labelR * Math.cos(phi);
            const y = cy - labelR * Math.sin(phi);

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
        if (!this.options.showObserver) return;

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
        ctx.moveTo(cx, cy - size);              // Вершина
        ctx.lineTo(cx - size, cy + size);       // Левый нижний угол
        ctx.lineTo(cx + size, cy + size);       // Правый нижний угол
        ctx.closePath();
        ctx.fill();
        ctx.stroke();
    };

    /**
     * Отрисовка стрелки направления на траектории
     * @param {number} x - X координата
     * @param {number} y - Y координата  
     * @param {number} angle - Угол направления в радианах
     */
    SkyView.prototype._drawArrow = function(x, y, angle) {
        const ctx = this.ctx;
        const size = 6;

        ctx.save();
        ctx.translate(x, y);
        ctx.rotate(angle);

        ctx.fillStyle = this.colors.trackArrow;
        ctx.beginPath();
        ctx.moveTo(size, 0);
        ctx.lineTo(-size * 0.5, -size * 0.5);
        ctx.lineTo(-size * 0.3, 0);
        ctx.lineTo(-size * 0.5, size * 0.5);
        ctx.closePath();
        ctx.fill();

        ctx.restore();
    };

    /**
     * Отрисовка траектории пролёта
     * Траектория — плавная кривая от внешней окружности до внешней окружности
     */
    SkyView.prototype._drawTrack = function() {
        const ctx = this.ctx;
        const track = this.satellite.track;

        if (!track || track.length < 2) {
            return;
        }

        // Фильтруем видимые точки (el >= 0, включая горизонт)
        const visibleTrack = track.filter(function(p) { return p.el >= 0; });
        if (visibleTrack.length < 2) {
            return;
        }

        // Сохраняем времена пролёта
        this.passInfo.aosTime = visibleTrack[0].time;
        this.passInfo.losTime = visibleTrack[visibleTrack.length - 1].time;

        // Преобразуем все точки в координаты canvas
        const points = [];
        for (let i = 0; i < visibleTrack.length; i++) {
            const trackPoint = visibleTrack[i];
            const p = this.azElToXY(trackPoint.az, trackPoint.el);
            points.push({ 
                x: p.x, 
                y: p.y, 
                time: trackPoint.time,
                el: trackPoint.el,
                az: trackPoint.az
            });
        }

        // Рисуем линию траектории
        ctx.strokeStyle = this.colors.track;
        ctx.lineWidth = 2;
        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';
        ctx.beginPath();

        for (let i = 0; i < points.length; i++) {
            if (i === 0) {
                ctx.moveTo(points[i].x, points[i].y);
            } else {
                ctx.lineTo(points[i].x, points[i].y);
            }
        }

        ctx.stroke();

        // Стрелки направления на траектории
        this._drawTrackArrows(points, visibleTrack);

        // Маркеры AOS (зелёный) и LOS (красный) на концах траектории
        this._drawAosLosMarkers(points);
    };

    /**
     * Отрисовка стрелок направления на траектории
     */
    SkyView.prototype._drawTrackArrows = function(points, visibleTrack) {
        const arrowInterval = this.options.arrowInterval;
        let lastArrowTime = points[0].time;

        for (let i = 1; i < points.length - 1; i++) {
            const point = visibleTrack[i];
            
            if (point.time - lastArrowTime >= arrowInterval) {
                const prev = points[i - 1];
                const curr = points[i];
                const next = points[i + 1];
                
                const dx = next.x - prev.x;
                const dy = next.y - prev.y;
                const angle = Math.atan2(dy, dx);
                
                this._drawArrow(curr.x, curr.y, angle);
                lastArrowTime = point.time;
            }
        }
    };

    /**
     * Отрисовка маркеров AOS (начало) и LOS (конец) точно на внешней окружности
     * Маркеры размещаются на пересечении траектории с горизонтом (el=0)
     * Используем экстраполяцию для нахождения точного азимута пересечения
     * @param {Array} points - Массив точек траектории с координатами
     */
    SkyView.prototype._drawAosLosMarkers = function(points) {
        if (points.length < 2) return;

        const ctx = this.ctx;
        const markerRadius = 6;

        // Находим азимут точки пересечения траектории с горизонтом (el=0)
        // Экстраполируем между первыми двумя точками
        const startAz = this._findHorizonCrossing(points[0], points[1]);
        
        // Экстраполируем между последними двумя точками
        const endAz = this._findHorizonCrossing(
            points[points.length - 1], 
            points[points.length - 2]
        );

        // Вычисляем координаты маркеров точно на внешней окружности (el=0)
        const aosPoint = this.azElToXY(startAz, 0);
        const losPoint = this.azElToXY(endAz, 0);

        // AOS маркер (зелёный) - начало видимости
        ctx.beginPath();
        ctx.arc(aosPoint.x, aosPoint.y, markerRadius, 0, Math.PI * 2);
        ctx.fillStyle = this.colors.aosMarker;
        ctx.fill();
        ctx.strokeStyle = this.colors.markerBorder;
        ctx.lineWidth = 2;
        ctx.stroke();

        // LOS маркер (красный) - конец видимости
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
        if (p1.el <= 0.01) return p1.az;
        
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
        if (deltaAz > 180) deltaAz -= 360;
        if (deltaAz < -180) deltaAz += 360;
        
        let az = p1.az + deltaAz * t;
        
        // Нормализация
        while (az < 0) az += 360;
        while (az >= 360) az -= 360;
        
        return az;
    };

    /**
     * Отрисовка маркеров AOS и LOS в указанных точках
     * @param {Object} aosPoint - Координаты {x, y} для AOS маркера
     * @param {Object} losPoint - Координаты {x, y} для LOS маркера
     */
    SkyView.prototype._drawAosLosMarkersAtPoints = function(aosPoint, losPoint) {
        const ctx = this.ctx;
        const markerRadius = 6;

        // Маркер AOS (зелёный) - начало видимости
        ctx.beginPath();
        ctx.arc(aosPoint.x, aosPoint.y, markerRadius, 0, Math.PI * 2);
        ctx.fillStyle = this.colors.aosMarker;
        ctx.fill();
        ctx.strokeStyle = this.colors.markerBorder;
        ctx.lineWidth = 2;
        ctx.stroke();

        // Маркер LOS (красный) - конец видимости
        ctx.beginPath();
        ctx.arc(losPoint.x, losPoint.y, markerRadius, 0, Math.PI * 2);
        ctx.fillStyle = this.colors.losMarker;
        ctx.fill();
        ctx.strokeStyle = this.colors.markerBorder;
        ctx.lineWidth = 2;
        ctx.stroke();
    };

    /**
     * @deprecated Используйте _drawAosLosMarkersAtPoints
     * Оставлено для обратной совместимости
     */
    SkyView.prototype._drawAosLosMarkers = function(visibleTrack) {
        if (visibleTrack.length < 2) return;
        
        const startAz = visibleTrack[0].az;
        const endAz = visibleTrack[visibleTrack.length - 1].az;
        const aosPoint = this.azElToXY(startAz, 0);
        const losPoint = this.azElToXY(endAz, 0);
        
        this._drawAosLosMarkersAtPoints(aosPoint, losPoint);
    };

    /**
     * Отрисовка ауры (окружности) вокруг спутника
     * @param {number} x - X координата спутника
     * @param {number} y - Y координата спутника
     */
    SkyView.prototype._drawSatelliteAura = function(x, y) {
        if (!this.options.showSatelliteAura) return;

        const ctx = this.ctx;
        const auraR = this.options.satelliteAuraRadius;
        
        // Пульсирующая аура
        const pulse = 1 + 0.15 * Math.sin(this._animationPhase * 2);
        const currentR = auraR * pulse;

        // Градиентная заливка
        const gradient = ctx.createRadialGradient(x, y, 0, x, y, currentR);
        gradient.addColorStop(0, this.colors.satelliteAura);
        gradient.addColorStop(1, 'rgba(0, 200, 255, 0)');

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

        // Аура вокруг спутника (если включена)
        this._drawSatelliteAura(p.x, p.y);

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
        ctx.strokeStyle = 'rgba(255, 255, 255, 0.3)';
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
        ctx.strokeStyle = '#ffffff';
        ctx.lineWidth = 1;
        ctx.strokeRect(p.x - size / 2, p.y - size / 2, size, size);

        // Анимация "передачи сигнала" (волны от спутника)
        this._drawSignalWaves(p.x, p.y);

        // Подпись спутника
        if (this.satellite.name) {
            ctx.font = 'bold 10px sans-serif';
            ctx.fillStyle = this.colors.satLabel;
            ctx.textAlign = 'left';
            ctx.textBaseline = 'bottom';
            ctx.shadowColor = 'rgba(0, 0, 0, 0.8)';
            ctx.shadowBlur = 3;
            ctx.fillText(this.satellite.name, p.x + size + 5, p.y - 2);
            ctx.shadowBlur = 0;
        }
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
        if (dist < 20) return;
        
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
                ctx.strokeStyle = `rgba(0, 255, 200, ${alpha})`;
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
        if (!timestamp) return '--:--:--';
        const date = new Date(timestamp);
        return date.toTimeString().split(' ')[0];
    };

    /**
     * Форматирование длительности
     * @param {number} durationMs - Длительность в миллисекундах
     * @returns {string} - Форматированная длительность (Xm Ys)
     */
    SkyView.prototype._formatDuration = function(durationMs) {
        if (!durationMs || durationMs < 0) return '--:--';
        const totalSec = Math.floor(durationMs / 1000);
        const min = Math.floor(totalSec / 60);
        const sec = totalSec % 60;
        return min + 'm ' + (sec < 10 ? '0' : '') + sec + 's';
    };

    /**
     * Отрисовка информационной панели внизу
     */
    SkyView.prototype._drawInfo = function() {
        const ctx = this.ctx;
        const pos = this.satellite.currentPos;
        const w = this.canvas.width;
        const h = this.canvas.height;
        const panelHeight = this.infoPanelHeight;
        const panelY = h - panelHeight;
        
        // Рамка информационной панели со скруглёнными углами
        const panelPadding = 6;
        const cornerRadius = 8;
        
        ctx.beginPath();
        ctx.roundRect(panelPadding, panelY + 4, w - panelPadding * 2, panelHeight - 8, cornerRadius);
        ctx.fillStyle = 'rgba(20, 30, 45, 0.9)';
        ctx.fill();
        ctx.strokeStyle = this.colors.grid;
        ctx.lineWidth = 1;
        ctx.stroke();

        // Разделительные линии внутри панели
        const col1X = panelPadding + 8;
        const col2X = w * 0.35;
        const col3X = w * 0.65;
        const row1Y = panelY + 18;
        const row2Y = panelY + 34;

        // === Левая колонка: Азимут и Угол места ===
        ctx.font = 'bold 10px monospace';
        ctx.textBaseline = 'middle';
        
        // Азимут
        ctx.textAlign = 'left';
        ctx.fillStyle = this.colors.infoLabel;
        ctx.fillText('Az:', col1X, row1Y);
        ctx.fillStyle = this.colors.infoText;
        const azText = pos ? pos.az.toFixed(1) + '°' : '---.-°';
        ctx.fillText(azText, col1X + 25, row1Y);

        // Угол места
        ctx.fillStyle = this.colors.infoLabel;
        ctx.fillText('El:', col1X, row2Y);
        ctx.fillStyle = this.colors.infoText;
        const elText = pos ? pos.el.toFixed(1) + '°' : '---.-°';
        ctx.fillText(elText, col1X + 25, row2Y);

        // === Средняя колонка: AOS и LOS времена ===
        const passInfo = this.passInfo;
        ctx.font = '9px monospace';
        
        // AOS время
        ctx.textAlign = 'left';
        ctx.fillStyle = this.colors.aosMarker;
        ctx.fillText('AOS:', col2X, row1Y);
        ctx.fillStyle = this.colors.timeText;
        ctx.fillText(this._formatTime(passInfo.aosTime), col2X + 32, row1Y);
        
        // LOS время
        ctx.fillStyle = this.colors.losMarker;
        ctx.fillText('LOS:', col2X, row2Y);
        ctx.fillStyle = this.colors.timeText;
        ctx.fillText(this._formatTime(passInfo.losTime), col2X + 32, row2Y);

        // === Правая колонка: Длительность и название спутника ===
        // Длительность
        ctx.textAlign = 'left';
        ctx.fillStyle = this.colors.infoLabel;
        ctx.fillText('Dur:', col3X, row1Y);
        
        if (passInfo.aosTime && passInfo.losTime) {
            const duration = passInfo.losTime - passInfo.aosTime;
            ctx.fillStyle = this.colors.timeText;
            ctx.fillText(this._formatDuration(duration), col3X + 30, row1Y);
        } else {
            ctx.fillStyle = this.colors.timeText;
            ctx.fillText('--:--', col3X + 30, row1Y);
        }
        
        // Название спутника
        ctx.fillStyle = this.colors.infoLabel;
        ctx.fillText('Sat:', col3X, row2Y);
        ctx.fillStyle = this.colors.satellite;
        ctx.font = 'bold 9px monospace';
        const satName = this.satellite.name || '---';
        ctx.fillText(satName, col3X + 30, row2Y);
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
        this._drawTrack();
        this._drawSatellite();
        this._drawInfo();
    };

    /**
     * Установка информации о спутнике
     */
    SkyView.prototype.setSatelliteInfo = function(name) {
        this.satellite.name = name;
    };

    /**
     * Установка текущей позиции спутника
     * @param {number} az - Азимут в градусах
     * @param {number} el - Угол места в градусах
     */
    SkyView.prototype.setSatellitePosition = function(az, el) {
        this.satellite.currentPos = { az: az, el: el };
    };

    /**
     * Очистка траектории
     */
    SkyView.prototype.clearTrack = function() {
        this.satellite.track = [];
        this.passInfo = { aosTime: null, losTime: null, maxElTime: null };
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
    };

    /**
     * Установка времён пролёта вручную
     * @param {number} aosTime - Время AOS (timestamp)
     * @param {number} losTime - Время LOS (timestamp)
     */
    SkyView.prototype.setPassTimes = function(aosTime, losTime) {
        this.passInfo.aosTime = aosTime;
        this.passInfo.losTime = losTime;
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

    /**
     * Демо-режим с симуляцией пролёта
     */
    SkyView.prototype.startDemo = function(speed) {
        const self = this;
        speed = speed || 1;

        this.setSatelliteInfo('ISS');
        this._lastAnimTime = Date.now();

        // Параметры симуляции пролёта
        const passDuration = 10 * 60 * 1000; // 10 минут
        const startTime = Date.now();
        let simTime = startTime;

        // Генерация траектории пролёта спутника
        // Строим ДУГУ на азимутальной проекции (как в SkyRoof)
        function generatePassTrack(baseTime) {
            self.clearTrack();

            // Параметры пролёта
            const maxEl = 30 + Math.random() * 50;           // Макс. угол места 30-80°
            const passDirection = Math.random() * 360;       // Направление пролёта (градусы)
            const passDirectionRad = passDirection * Math.PI / 180;
            
            // Кривизна дуги (смещение перпендикулярно направлению)
            // Чем выше maxEl, тем меньше кривизна (более прямой путь)
            const curvature = 0.2 + Math.random() * 0.3;     // 0.2-0.5
            
            const steps = 60;

            for (let i = 0; i <= steps; i++) {
                const t = i / steps;           // 0 → 1
                const s = (t - 0.5) * 2;       // -1 → 0 → 1
                
                // Компонента вдоль направления пролёта (линейная)
                const alongTrack = s * 0.95;   // от -0.95 до +0.95 (не доходя до края)
                
                // Компонента поперёк направления (парабола - создаёт дугу!)
                // Максимум в центре (s=0), ноль на краях (s=±1)
                const acrossTrack = curvature * (1 - s * s);
                
                // Координаты на проекции
                // alongTrack - вдоль направления пролёта
                // acrossTrack - перпендикулярно (создаёт изгиб дуги)
                const projX = alongTrack * Math.sin(passDirectionRad) + acrossTrack * Math.cos(passDirectionRad);
                const projY = -alongTrack * Math.cos(passDirectionRad) + acrossTrack * Math.sin(passDirectionRad);
                
                // Радиус на проекции
                const projRadius = Math.sqrt(projX * projX + projY * projY);
                
                // Elevation из радиуса: ro=0 → el=90°, ro=1 → el=0°
                const el = 90 * (1 - Math.min(1, projRadius));
                
                // Азимут из координат
                let az = Math.atan2(projX, -projY) * 180 / Math.PI;
                if (az < 0) az += 360;
                
                const time = baseTime + t * passDuration;
                
                // Добавляем только видимую часть (el >= 0)
                if (el >= 0) {
                    self.addTrackPoint(az, el, time);
                }
            }
        }

        generatePassTrack(simTime);

        function animate() {
            simTime += 100 * speed;

            const track = self.satellite.track;
            let currentPos = null;

            for (let i = 0; i < track.length - 1; i++) {
                if (track[i].time <= simTime && track[i + 1].time > simTime) {
                    const t1 = track[i];
                    const t2 = track[i + 1];
                    const ratio = (simTime - t1.time) / (t2.time - t1.time);

                    let az = t1.az + (t2.az - t1.az) * ratio;
                    if (Math.abs(t2.az - t1.az) > 180) {
                        if (t2.az > t1.az) {
                            az = t1.az + (t2.az - 360 - t1.az) * ratio;
                        } else {
                            az = t1.az + (t2.az + 360 - t1.az) * ratio;
                        }
                        if (az < 0) az += 360;
                        if (az >= 360) az -= 360;
                    }

                    currentPos = {
                        az: az,
                        el: t1.el + (t2.el - t1.el) * ratio
                    };
                    break;
                }
            }

            if (!currentPos || simTime > track[track.length - 1].time) {
                simTime = Date.now();
                generatePassTrack(simTime);
                currentPos = { az: track[0].az, el: track[0].el };
            }

            self.setSatellitePosition(currentPos.az, currentPos.el);
            self.draw();

            self._animationId = requestAnimationFrame(animate);
        }

        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
        }

        animate();
    };

    /**
     * Остановка демо
     */
    SkyView.prototype.stopDemo = function() {
        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
            this._animationId = null;
        }
    };

    // Экспорт
    window.SkyView = SkyView;

})();
