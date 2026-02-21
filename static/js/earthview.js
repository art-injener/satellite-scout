// EarthView - Компонент отображения карты мира с орбитами спутников
// Стиль вдохновлён программой STSPLUS (1989-2006)

(function() {
    'use strict';

    /**
     * Класс для отображения карты Земли с орбитами спутников
     * @param {HTMLCanvasElement} canvas - Canvas элемент для отрисовки
     * @param {Object} options - Опции конфигурации
     */
    function EarthView(canvas, options) {
        this.canvas = canvas;
        this.ctx = canvas.getContext('2d');
        this.width = canvas.width;
        this.height = canvas.height;

        // Настройки по умолчанию
        this.options = Object.assign({
            coastlineUrl: '/static/data/ne_110m_coastline.json',
            russiaBordersUrl: '/static/data/russia_110m.geojson',
            gridStep: 30, // Шаг сетки в градусах
            showGrid: true,
            showCoastlines: true,
            showRussiaBorders: true, // Границы РФ и подпись «Россия»
            showFootprint: true, // Круг видимости спутника
            trackMode: 'both', // 'line', 'dots', 'both'
            trackDotInterval: 60000 // Интервал точек в мс (1 минута)
        }, options || {});

        // Цветовая схема в стиле STSPLUS (улучшенная для читаемости)
        this.colors = {
            background: '#000010', // Тёмно-синий фон (океаны)
            coastline: '#4d9999', // #5b8a8a #00d4d4, // Циан - береговые линии
            grid: '#3a4a4a', // Серый - сетка #556677 #2a3d4d #334455 #3d5566	
            gridMajor: '#4a5e5e', // Светлее - основные линии #667788 #3a5060 #445566 #4d6677
            orbitFuture: '#00ff00', // Зелёный - будущая орбита
            orbitPast: '#ff4444', // Красный - прошлая орбита
            orbitDots: '#ffff00', // Жёлтый - точки орбиты
            satellite: '#ffffff', // Белый - маркер спутника
            satelliteGlow: '#00ffff', // Циан - свечение спутника
            footprint: 'rgba(200, 100, 255, 0.55)', // Пурпурный - контур зоны видимости (контрастирует с бирюзой и зелёным)
            footprintFill: 'rgba(200, 100, 255, 0.09)', // Пурпурный полупрозрачный - заливка зоны
            observer: '#ff0000', // маркер наблюдателя (треугольник)
            observerLabel: '#ff9500', // цвет треугольника наблюдателя — янтарный
            observerLabelStroke: 'rgba(0,0,0,0.9)', // обводка треугольника/подписи наблюдателя
            observerLabelBg: 'rgba(220, 220, 228, 0.92)', // фон под подпись наблюдателя — светло-серый
            textPrimary: '#ffffff',
            textSecondary: '#00d4d4', // Циан для подписей
            textGrid: '#ffffff', // Белые подписи сетки
            satLabel: '#ffeb3b', // подпись спутника — яркий жёлтый
            satLabelStroke: 'rgba(0,0,0,0.85)', // обводка подписи спутника
            satLabelBg: 'rgba(220, 220, 228, 0.92)', // фон под подпись спутника — светло-серый
            russiaBorder: '#aabbcc', // Границы РФ альтернатива: #8899aa, #66bb6a
            russiaLabel: '#ffcc00' // Подпись «Россия»
        };

        // Данные береговых линий (GeoJSON)
        this.coastlineData = null;

        // Данные границ РФ (GeoJSON)
        this.russiaData = null;

        // Состояние карты
        this.center = { lon: 0, lat: 0 }; // Центр карты
        this.zoom = 1.0; // Масштаб (1.0 = вся карта)

        // Данные спутника
        this.satellite = {
            position: null, // {lon, lat, alt}
            groundTrack: [], // Массив точек или {past: [[...]], future: [[...]]} с сервера
            visibilityZone: null, // Точки контура зоны видимости с сервера [{lon, lat}, ...]
            name: '',
            noradId: null
        };

        // Наблюдатель
        this.observer = null; // {lon, lat, name}

        // Столицы мира для отображения на карте (только основные)
        this.cities = [
            { name: 'МОСКВА', lon: 37.62, lat: 55.75 },
            { name: 'BEIJING', lon: 116.40, lat: 39.90 },
            { name: 'TOKYO', lon: 139.69, lat: 35.69 },
            { name: 'DELHI', lon: 77.21, lat: 28.61 },
            { name: 'NEW YORK', lon: -74.01, lat: 40.71 },
            { name: 'LONDON', lon: -0.13, lat: 51.51 },
            // { name: 'CAIRO', lon: 31.24, lat: 30.04 },
            { name: 'SYDNEY', lon: 151.21, lat: -33.87 },
            { name: 'RIO DE JANEIRO', lon: -43.17, lat: -22.91 },
            { name: 'CAPE TOWN', lon: 18.42, lat: -33.93 },
            { name: 'NAIROBI', lon: 36.82, lat: -1.29 },
            { name: 'SAN FRANCISCO', lon: -122.42, lat: 37.77 }
        ];

        // Флаг готовности
        this.ready = false;

        // Привязка обработчиков событий
        this._boundResize = this._onResize.bind(this);
        this._resizeObserver = null;
    }

    /**
     * Настройка размеров canvas под HiDPI/Retina.
     * CSS задаёт отображаемый размер (width/height: 100%), JS задаёт только буфер.
     * Читаем clientWidth/Height (размер отображения из CSS) и ставим буфер × dpr.
     */
    EarthView.prototype._setupCanvasSize = function() {
        // clientWidth/Height = размер отображения, заданный CSS (100% контейнера)
        var displayWidth = this.canvas.clientWidth;
        var displayHeight = this.canvas.clientHeight;
        if (displayWidth <= 0 || displayHeight <= 0) { return; }

        var dpr = window.devicePixelRatio || 1;
        var backingWidth = Math.round(displayWidth * dpr);
        var backingHeight = Math.round(displayHeight * dpr);

        // Устанавливаем только буфер, НЕ style (style задан CSS)
        if (this.canvas.width !== backingWidth || this.canvas.height !== backingHeight) {
            this.canvas.width = backingWidth;
            this.canvas.height = backingHeight;
            this.width = backingWidth;
            this.height = backingHeight;
        }
    };

    // ========== Проекция координат ==========

    /**
     * Преобразование географических координат в координаты canvas
     * Equirectangular (Plate Carrée) проекция
     * @param {number} lon - Долгота (-180 до 180)
     * @param {number} lat - Широта (-90 до 90)
     * @returns {Object} {x, y} координаты на canvas
     */
    EarthView.prototype.project = function(lon, lat) {
        // Нормализация долготы
        while (lon > 180) { lon -= 360; }
        while (lon < -180) { lon += 360; }

        const x = (lon + 180) / 360 * this.width;
        const y = (90 - lat) / 180 * this.height;

        return { x: x, y: y };
    };

    /**
     * Обратное преобразование - из координат canvas в географические
     * @param {number} x - X координата на canvas
     * @param {number} y - Y координата на canvas
     * @returns {Object} {lon, lat}
     */
    EarthView.prototype.unproject = function(x, y) {
        const lon = (x / this.width) * 360 - 180;
        const lat = 90 - (y / this.height) * 180;
        return { lon: lon, lat: lat };
    };

    // ========== Загрузка данных ==========

    /**
     * Загрузка данных береговых линий
     * @param {string} url - URL GeoJSON файла
     * @returns {Promise}
     */
    EarthView.prototype.loadCoastlines = function(url) {
        const self = this;
        url = url || this.options.coastlineUrl;

        return fetch(url)
            .then(function(response) {
                if (!response.ok) {
                    throw new Error('Ошибка загрузки: ' + response.status);
                }
                return response.json();
            })
            .then(function(data) {
                self.coastlineData = data;
                self.ready = true;
                // eslint-disable-next-line no-console
                console.log('EarthView: загружено', data.features.length, 'береговых линий');
                return data;
            })
            .catch(function(error) {
                // eslint-disable-next-line no-console
                console.error('EarthView: ошибка загрузки береговых линий:', error);
                throw error;
            });
    };

    /**
     * Загрузка границ РФ 
     * @param {string} url - URL GeoJSON файла
     * @returns {Promise}
     */
    EarthView.prototype.loadRussiaBorders = function(url) {
        const self = this;
        url = url || this.options.russiaBordersUrl;

        return fetch(url)
            .then(function(response) {
                if (!response.ok) {
                    throw new Error('Ошибка загрузки границ РФ: ' + response.status);
                }
                return response.json();
            })
            .then(function(data) {
                self.russiaData = data;
                // eslint-disable-next-line no-console
                console.log('EarthView: загружены границы РФ');
                return data;
            })
            .catch(function(error) {
                // eslint-disable-next-line no-console
                console.warn('EarthView: границы РФ не загружены:', error.message);
                return null;
            });
    };

    // ========== Отрисовка ==========

    /**
     * Главная функция отрисовки
     */
    EarthView.prototype.draw = function() {
        const ctx = this.ctx;

        // Очистка canvas
        ctx.fillStyle = this.colors.background;
        ctx.fillRect(0, 0, this.width, this.height);

        // Слои отрисовки (порядок важен!)
        if (this.options.showGrid) {
            this._drawGrid();
        }

        if (this.options.showCoastlines && this.coastlineData) {
            this._drawCoastlines();
        }

        if (this.options.showRussiaBorders && this.russiaData) {
            this._drawRussiaBorders();
        }

        // Столицы мира
        this._drawCities();

        // Наземная трасса спутника
        if (this._hasGroundTrack()) {
            this._drawGroundTrack();
        }

        // Зона видимости спутника (с сервера; если нет — не рисуем)
        if (this.options.showFootprint && this.satellite.visibilityZone && this.satellite.visibilityZone.length > 0) {
            this._drawVisibilityZone();
        }

        // Наблюдатель
        if (this.observer) {
            this._drawObserver();
        }

        // Спутник
        if (this.satellite.position) {
            this._drawSatellite();
        }
    };

    /**
     * Отрисовка координатной сетки
     */
    EarthView.prototype._drawGrid = function() {
        const ctx = this.ctx;
        const step = this.options.gridStep;

        // Вертикальные линии (меридианы)
        for (let lon = -180; lon <= 180; lon += step) {
            const isMajor = (lon === 0 || lon === 180 || lon === -180);
            ctx.strokeStyle = isMajor ? this.colors.gridMajor : this.colors.grid;
            ctx.lineWidth = isMajor ? 1 : 1;

            const p1 = this.project(lon, 90);
            const p2 = this.project(lon, -90);

            ctx.beginPath();
            ctx.moveTo(p1.x, p1.y);
            ctx.lineTo(p2.x, p2.y);
            ctx.stroke();
        }

        // Горизонтальные линии (параллели)
        for (let lat = -90; lat <= 90; lat += step) {
            const isMajor = (lat === 0);
            ctx.strokeStyle = isMajor ? this.colors.gridMajor : this.colors.grid;
            ctx.lineWidth = isMajor ? 1 : 1;

            const p1 = this.project(-180, lat);
            const p2 = this.project(180, lat);

            ctx.beginPath();
            ctx.moveTo(p1.x, p1.y);
            ctx.lineTo(p2.x, p2.y);
            ctx.stroke();
        }

        // Подписи координат
        this._drawGridLabels();
    };

    /**
     * Подписи координатной сетки (стиль STSPLUS)
     */
    EarthView.prototype._drawGridLabels = function() {
        const ctx = this.ctx;
        const step = this.options.gridStep;

        ctx.font = '11px monospace';
        ctx.fillStyle = this.colors.textGrid || this.colors.textSecondary;

        // Подписи долготы (внизу)
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        for (let lon = -150; lon <= 180; lon += step) {
            const p = this.project(lon, -90);
            // Формат как в STSPLUS: просто число
            const label = lon.toString();
            ctx.fillText(label, p.x, this.height - 14);
        }

        // Подписи широты (слева)
        ctx.textAlign = 'right';
        ctx.textBaseline = 'middle';
        for (let lat = -80; lat <= 80; lat += 10) {
            if (lat === 0) { continue; }
            const p = this.project(-180, lat);
            // Формат как в STSPLUS: число с минусом для южного полушария
            const label = lat.toString();
            ctx.fillText(label, 24, p.y);
        }
    };

    /**
     * Отрисовка береговых линий
     */
    EarthView.prototype._drawCoastlines = function() {
        const ctx = this.ctx;
        const features = this.coastlineData.features;

        ctx.strokeStyle = this.colors.coastline;
        ctx.lineWidth = 1;

        for (let i = 0; i < features.length; i++) {
            const feature = features[i];
            const geometry = feature.geometry;

            if (geometry.type === 'LineString') {
                this._drawLineString(geometry.coordinates);
            } else if (geometry.type === 'MultiLineString') {
                for (let j = 0; j < geometry.coordinates.length; j++) {
                    this._drawLineString(geometry.coordinates[j]);
                }
            }
        }
    };

    /**
     * Отрисовка столиц мира
     */
    EarthView.prototype._drawCities = function() {
        const ctx = this.ctx;

        for (let i = 0; i < this.cities.length; i++) {
            const city = this.cities[i];
            const p = this.project(city.lon, city.lat);

            // Кружок с мягкой заливкой (маленький)
            ctx.beginPath();
            ctx.arc(p.x, p.y, 3, 0, Math.PI * 2);
            ctx.fillStyle = '#cc6666';
            ctx.fill();

            // Название города белым (мелкий шрифт)
            ctx.font = 'bold 11px sans-serif';
            ctx.fillStyle = '#ffffff';
            ctx.textAlign = 'left';
            ctx.textBaseline = 'middle';
            ctx.fillText(city.name, p.x + 5, p.y);
        }
    };

    /**
     * Отрисовка одной линии (LineString)
     * @param {Array} coords - Массив координат [[lon, lat], ...]
     */
    EarthView.prototype._drawLineString = function(coords) {
        if (!coords || coords.length < 2) { return; }

        const ctx = this.ctx;
        ctx.beginPath();

        let prevP = null;
        let moved = false;

        for (let i = 0; i < coords.length; i++) {
            const lon = coords[i][0];
            const lat = coords[i][1];
            const p = this.project(lon, lat);

            // Проверка на пересечение края карты (антимеридиан)
            if (prevP && Math.abs(p.x - prevP.x) > this.width / 2) {
                // Разрыв линии на антимеридиане
                ctx.stroke();
                ctx.beginPath();
                moved = false;
            }

            if (!moved) {
                ctx.moveTo(p.x, p.y);
                moved = true;
            } else {
                ctx.lineTo(p.x, p.y);
            }

            prevP = p;
        }

        ctx.stroke();
    };

    /**
     * Отрисовка границ РФ и подписи «Россия»
     */
    EarthView.prototype._drawRussiaBorders = function() {
        if (!this.russiaData || !this.russiaData.features || this.russiaData.features.length === 0) {
            return;
        }

        const ctx = this.ctx;
        ctx.strokeStyle = this.colors.russiaBorder || '#ffcc00';
        ctx.lineWidth = 1.5;

        for (let f = 0; f < this.russiaData.features.length; f++) {
            const feature = this.russiaData.features[f];
            const geom = feature.geometry;
            if (!geom || !geom.coordinates) { continue; }

            if (geom.type === 'Polygon') {
                this._drawLineString(geom.coordinates[0]);
            } else if (geom.type === 'MultiPolygon') {
                for (let p = 0; p < geom.coordinates.length; p++) {
                    const ring = geom.coordinates[p][0];
                    if (ring && ring.length >= 2) {
                        this._drawLineString(ring);
                    }
                }
            }
        }

        // Подпись «Россия» — в центре территории (приблизительно 95°E, 60°N)
        const labelLon = 95;
        const labelLat = 60;
        const labelPoint = this.project(labelLon, labelLat);
        ctx.font = 'bold 14px sans-serif';
        ctx.fillStyle = this.colors.russiaLabel || '#ffffff';
        ctx.strokeStyle = 'rgba(0,0,0,0.8)';
        ctx.lineWidth = 3;
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';
        const text = 'Россия';
        ctx.strokeText(text, labelPoint.x, labelPoint.y);
        ctx.fillText(text, labelPoint.x, labelPoint.y);
    };

    /**
     * Проверка наличия данных трассы (массив точек или формат с сервера {past, future}).
     */
    EarthView.prototype._hasGroundTrack = function() {
        const track = this.satellite.groundTrack;
        if (Array.isArray(track)) {
            return track.length > 0;
        }
        if (track && typeof track === 'object' && (track.past || track.future)) {
            var pastLen = (track.past && track.past.length) ? track.past.reduce(function(s, seg) { return s + seg.length; }, 0) : 0;
            var futureLen = (track.future && track.future.length) ? track.future.reduce(function(s, seg) { return s + seg.length; }, 0) : 0;
            return pastLen > 0 || futureLen > 0;
        }
        return false;
    };

    /**
     * Отрисовка наземной трассы спутника.
     * Поддерживает формат с сервера {past: [[...]], future: [[...]]} и плоский массив.
     */
    EarthView.prototype._drawGroundTrack = function() {
        const track = this.satellite.groundTrack;

        if (Array.isArray(track)) {
            if (track.length >= 2) {
                this._drawTrackSegment(track, this.colors.orbitFuture);
            }
            return;
        }

        if (track && track.past) {
            for (var i = 0; i < track.past.length; i++) {
                var seg = track.past[i];
                if (seg && seg.length >= 2) {
                    this.ctx.setLineDash([4, 4]);
                    this._drawTrackSegment(seg, this.colors.orbitPast);
                    this.ctx.setLineDash([]);
                }
            }
        }
        if (track && track.future) {
            for (var j = 0; j < track.future.length; j++) {
                var segF = track.future[j];
                if (segF && segF.length >= 2) {
                    this._drawTrackSegment(segF, this.colors.orbitFuture);
                }
            }
        }
    };

    /**
     * Отрисовка сегмента орбиты
     * @param {Array} points - Массив точек [{lon, lat, time} или {lon, lat, ts}]
     * @param {string} color - Цвет линии
     */
    EarthView.prototype._drawTrackSegment = function(points, color) {
        const ctx = this.ctx;
        const mode = this.options.trackMode;
        const dotInterval = this.options.trackDotInterval;

        // Отрисовка линии
        if (mode === 'line' || mode === 'both') {
            ctx.strokeStyle = color;
            ctx.lineWidth = 1.2; 
            ctx.beginPath();

            var prevP = null;
            var moved = false;

            for (var i = 0; i < points.length; i++) {
                var pt = points[i];
                var p = this.project(pt.lon, pt.lat);

                // Проверка на пересечение антимеридиана
                if (prevP && Math.abs(p.x - prevP.x) > this.width / 2) {
                    ctx.stroke();
                    ctx.beginPath();
                    moved = false;
                }

                if (!moved) {
                    ctx.moveTo(p.x, p.y);
                    moved = true;
                } else {
                    ctx.lineTo(p.x, p.y);
                }

                prevP = p;
            }

            ctx.stroke();
        }

        // Отрисовка точек (минутные метки) - жёлтым цветом
        if (mode === 'dots' || mode === 'both') {
            ctx.fillStyle = this.colors.orbitDots; // Жёлтый
            var lastDotTime = -Infinity;

            for (var k = 0; k < points.length; k++) {
                var point = points[k];
                var t = point.ts != null ? point.ts : point.time;
                if (t - lastDotTime >= dotInterval) {
                    var pp = this.project(point.lon, point.lat);
                    ctx.beginPath();
                    ctx.arc(pp.x, pp.y, 2, 0, Math.PI * 2); // Очень маленькие точки
                    ctx.fill();
                    lastDotTime = t;
                }
            }
        }
    };

    /**
     * Отрисовка маркера спутника (значок МКС в стиле STSPLUS)
     */
    EarthView.prototype._drawSatellite = function() {
        const ctx = this.ctx;
        const pos = this.satellite.position;
        const p = this.project(pos.lon, pos.lat);

        // Масштаб для HiDPI (буфер увеличен на dpr, значит пиксели нужно масштабировать)
        var dpr = window.devicePixelRatio || 1;
        var s = dpr * 1.2; // базовый множитель для увеличения

        ctx.strokeStyle = this.colors.satellite;
        ctx.fillStyle = this.colors.satellite;
        ctx.lineWidth = 2 * s;

        // Неоновая обводка (glow эффект)
        ctx.shadowColor = '#ff00ff'; // magenta неоновый
        ctx.shadowBlur = 4 * s;
        ctx.shadowOffsetX = 0;
        ctx.shadowOffsetY = 0;

        // Иконка спутника в стиле STSPLUS (упрощённая МКС)
        // Центральный модуль
        ctx.fillRect(p.x - 3*s, p.y - 8*s, 6*s, 16*s);

        // Солнечные панели (горизонтальные)
        ctx.fillRect(p.x - 16*s, p.y - 3*s, 10*s, 6*s);
        ctx.fillRect(p.x + 6*s, p.y - 3*s, 10*s, 6*s);

        // Дополнительные элементы панелей
        ctx.lineWidth = 1.5 * s;
        ctx.beginPath();
        ctx.moveTo(p.x - 16*s, p.y);
        ctx.lineTo(p.x - 19*s, p.y - 4*s);
        ctx.moveTo(p.x - 16*s, p.y);
        ctx.lineTo(p.x - 19*s, p.y + 4*s);
        ctx.moveTo(p.x + 16*s, p.y);
        ctx.lineTo(p.x + 19*s, p.y - 4*s);
        ctx.moveTo(p.x + 16*s, p.y);
        ctx.lineTo(p.x + 19*s, p.y + 4*s);
        ctx.stroke();

        // Сброс свечения
        ctx.shadowColor = 'transparent';
        ctx.shadowBlur = 0;

        // Название спутника — цвет и обводка (без фона)
        if (this.satellite.name) {
            var fontSize = Math.round(12 * s);
            ctx.font = 'bold ' + fontSize + 'px monospace';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'top';
            var labelY = p.y + 12 * s;
            ctx.strokeStyle = this.colors.satLabelStroke || 'rgba(0,0,0,0.85)';
            ctx.lineWidth = 2.5;
            ctx.strokeText(this.satellite.name, p.x, labelY);
            ctx.fillStyle = this.colors.satLabel || '#ffeb3b';
            ctx.fillText(this.satellite.name, p.x, labelY);
        }
    };

    /**
     * Отрисовка позиции наблюдателя
     */
    EarthView.prototype._drawObserver = function() {
        const ctx = this.ctx;
        const p = this.project(this.observer.lon, this.observer.lat);

        // Треугольник
        const size = 8;
        ctx.beginPath();
        ctx.moveTo(p.x, p.y - size);           // вершина
        ctx.lineTo(p.x - size, p.y + size);    // нижний левый
        ctx.lineTo(p.x + size, p.y + size);    // нижний правый
        ctx.closePath();
        ctx.fillStyle = this.colors.observerLabel || '#ff9500';
        ctx.strokeStyle = this.colors.observerLabelStroke || 'rgba(0,0,0,0.9)';
        ctx.lineWidth = 1;
        ctx.fill();
        ctx.stroke();

        // Название точки наблюдения — белый цвет
        if (this.observer.name) {
            var obsText = this.observer.name.toLocaleUpperCase();
            ctx.font = 'bold 12px sans-serif';
            ctx.textAlign = 'left';
            ctx.textBaseline = 'middle';
            var labelX = p.x + size + 3;
            ctx.strokeStyle = 'rgba(0,0,0,0.9)';
            ctx.lineWidth = 2;
            ctx.strokeText(obsText, labelX, p.y);
            ctx.fillStyle = '#ffffff';
            ctx.fillText(obsText, labelX, p.y);
        }
    };

    /**
     * Отрисовка зоны видимости спутника.
     * Сервер присылает готовые сегменты (замкнутые полигоны, разбитые по ±180°).
     * Каждый сегмент рисуется как замкнутый контур с заливкой.
     */
    EarthView.prototype._drawVisibilityZone = function() {
        var ctx = this.ctx;
        var segments = this.satellite.visibilityZone;

        if (!segments || segments.length === 0) { return; }

        var dpr = window.devicePixelRatio || 1;

        for (var k = 0; k < segments.length; k++) {
            var seg = segments[k];
            if (!seg || seg.length < 3) { continue; }

            // Проецируем точки сегмента
            var projected = [];
            for (var i = 0; i < seg.length; i++) {
                projected.push(this.project(seg[i].lon, seg[i].lat));
            }

            // Заливка
            ctx.beginPath();
            ctx.moveTo(projected[0].x, projected[0].y);
            for (var j = 1; j < projected.length; j++) {
                ctx.lineTo(projected[j].x, projected[j].y);
            }
            ctx.closePath();
            ctx.fillStyle = this.colors.footprintFill;
            ctx.fill();

            // Контур
            ctx.strokeStyle = this.colors.footprint;
            ctx.lineWidth = 1.5 * dpr;
            ctx.setLineDash([]);
            ctx.beginPath();
            ctx.moveTo(projected[0].x, projected[0].y);
            for (var m = 1; m < projected.length; m++) {
                ctx.lineTo(projected[m].x, projected[m].y);
            }
            ctx.closePath();
            ctx.stroke();
        }
    };

    // ========== API методы ==========

    /**
     * Инициализация компонента
     * @returns {Promise}
     */
    EarthView.prototype.init = function() {
        var self = this;
        this._setupCanvasSize();
        if (typeof ResizeObserver !== 'undefined') {
            this._resizeObserver = new ResizeObserver(function() {
                self._onResize();
            });
            this._resizeObserver.observe(this.canvas.parentElement);
        } else {
            window.addEventListener('resize', this._boundResize);
        }
        return Promise.all([
            this.loadCoastlines(),
            this.loadRussiaBorders()
        ]).then(function() {
            self.draw();
            return self;
        });
    };

    /**
     * Установка позиции спутника
     * @param {number} lon - Долгота
     * @param {number} lat - Широта
     * @param {number} alt - Высота (км)
     */
    EarthView.prototype.setSatellitePosition = function(lon, lat, alt) {
        this.satellite.position = { lon: lon, lat: lat, alt: alt || 0 };
    };

    /**
     * Установка информации о спутнике
     * @param {string} name - Название
     * @param {number} noradId - NORAD ID
     */
    EarthView.prototype.setSatelliteInfo = function(name, noradId) {
        this.satellite.name = name;
        this.satellite.noradId = noradId;
    };

    /**
     * Установка наземной трассы спутника.
     * Принимает формат с сервера {past: [[{lon, lat, ts}...]], future: [[...]]}
     * или плоский массив точек [{lon, lat, time}].
     * @param {Array|Object} data - Массив точек или объект {past, future}
     */
    EarthView.prototype.setGroundTrack = function(data) {
        if (!data) {
            this.satellite.groundTrack = [];
            return;
        }
        if (Array.isArray(data)) {
            this.satellite.groundTrack = data;
            return;
        }
        this.satellite.groundTrack = {
            past: data.past || [],
            future: data.future || []
        };
    };

    /**
     * Установка зоны видимости спутника (сегменты с сервера).
     * @param {Array} segments - Массив сегментов [[{lon, lat}, ...], ...]
     */
    EarthView.prototype.setVisibilityZone = function(segments) {
        this.satellite.visibilityZone = Array.isArray(segments) ? segments : null;
    };

    /**
     * Добавление точки к наземной трассе спутника
     * @param {number} lon - Долгота
     * @param {number} lat - Широта
     * @param {number} time - Время (timestamp)
     */
    EarthView.prototype.addTrackPoint = function(lon, lat, time) {
        this.satellite.groundTrack.push({
            lon: lon,
            lat: lat,
            time: time || Date.now()
        });
    };

    /**
     * Очистка наземной трассы спутника
     */
    EarthView.prototype.clearGroundTrack = function() {
        this.satellite.groundTrack = [];
    };

    /**
     * Установка позиции наблюдателя
     * @param {number} lon - Долгота
     * @param {number} lat - Широта
     * @param {string} name - Название локации
     */
    EarthView.prototype.setObserver = function(lon, lat, name) {
        this.observer = { lon: lon, lat: lat, name: name || '' };
    };

    /**
     * Обработчик изменения размера (поддержка HiDPI: пересчёт буфера и отрисовка)
     */
    EarthView.prototype._onResize = function() {
        this._setupCanvasSize();
        this.draw();
    };

    // Экспорт
    window.EarthView = EarthView;

})();
