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
            gridStep: 30, // Шаг сетки в градусах
            showGrid: true,
            showCoastlines: true,
            showFootprint: true, // Круг видимости спутника
            trackMode: 'both', // 'line', 'dots', 'both'
            trackDotInterval: 60000 // Интервал точек в мс (1 минута)
        }, options || {});

        // Цветовая схема в стиле STSPLUS (улучшенная для читаемости)
        this.colors = {
            background: '#000010', // Тёмно-синий фон (океаны)
            coastline: '#00d4d4', // Циан - береговые линии
            grid: '#556677', // Серый - сетка (видимый)
            gridMajor: '#667788', // Светлее - основные линии
            orbitFuture: '#00ff00', // Зелёный - будущая орбита
            orbitPast: '#ff4444', // Красный - прошлая орбита
            orbitDots: '#ffff00', // Жёлтый - точки орбиты
            satellite: '#ffffff', // Белый - маркер спутника
            satelliteGlow: '#00ffff', // Циан - свечение спутника
            footprint: 'rgba(200, 100, 255, 0.60)', // Пурпурный - контур зоны видимости (контрастирует с бирюзой и зелёным)
            footprintFill: 'rgba(200, 100, 255, 0.10)', // Пурпурный полупрозрачный - заливка зоны
            observer: '#ff0000', // Красный - наблюдатель (как в STSPLUS)
            textPrimary: '#ffffff',
            textSecondary: '#00d4d4', // Циан для подписей
            textGrid: '#ffffff', // Белые подписи сетки
            satLabel: 'rgba(200, 100, 255, 0.60)' // Ярко-жёлтый - подпись спутника (контрастирует с пурпурной зоной)
        };

        // Данные береговых линий (GeoJSON)
        this.coastlineData = null;

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
            { name: 'MOSCOW', lon: 37.62, lat: 55.75 },
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
    }

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

            // Красный кружок без заливки (маленький)
            ctx.beginPath();
            ctx.arc(p.x, p.y, 2, 0, Math.PI * 2);
            ctx.strokeStyle = '#ff0000';
            ctx.lineWidth = 1;
            ctx.stroke();

            // Название города белым (мелкий шрифт)
            ctx.font = '8px monospace';
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
            ctx.lineWidth = 0.5; // Очень тонкая линия
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
                    ctx.arc(pp.x, pp.y, 1, 0, Math.PI * 2); // Очень маленькие точки
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

        ctx.strokeStyle = this.colors.satellite;
        ctx.fillStyle = this.colors.satellite;
        ctx.lineWidth = 2;

        // Неоновая обводка (glow эффект)
        ctx.shadowColor = '#ff00ff'; // magenta неоновый
        ctx.shadowBlur = 5;
        ctx.shadowOffsetX = 0;
        ctx.shadowOffsetY = 0;

        // Иконка спутника в стиле STSPLUS (упрощённая МКС)
        // Центральный модуль
        ctx.fillRect(p.x - 2, p.y - 6, 4, 12);

        // Солнечные панели (горизонтальные)
        ctx.fillRect(p.x - 12, p.y - 2, 8, 4);
        ctx.fillRect(p.x + 4, p.y - 2, 8, 4);

        // Дополнительные элементы панелей
        ctx.lineWidth = 1;
        ctx.beginPath();
        ctx.moveTo(p.x - 12, p.y);
        ctx.lineTo(p.x - 14, p.y - 3);
        ctx.moveTo(p.x - 12, p.y);
        ctx.lineTo(p.x - 14, p.y + 3);
        ctx.moveTo(p.x + 12, p.y);
        ctx.lineTo(p.x + 14, p.y - 3);
        ctx.moveTo(p.x + 12, p.y);
        ctx.lineTo(p.x + 14, p.y + 3);
        ctx.stroke();

        // Сброс свечения
        ctx.shadowColor = 'transparent';
        ctx.shadowBlur = 0;

        // Название спутника (такой же стиль, как у наблюдателя, под иконкой)
        if (this.satellite.name) {
            ctx.font = 'bold 10px monospace';
            ctx.fillStyle = '#ffffff'; // Белый текст
            ctx.textAlign = 'center';
            ctx.textBaseline = 'top';
            ctx.fillText(this.satellite.name, p.x, p.y + 8);
        }
    };

    /**
     * Отрисовка позиции наблюдателя
     */
    EarthView.prototype._drawObserver = function() {
        const ctx = this.ctx;
        const p = this.project(this.observer.lon, this.observer.lat);

        // Оранжевый кружок без заливки (маленький)
        ctx.beginPath();
        ctx.arc(p.x, p.y, 2, 0, Math.PI * 2);
        ctx.strokeStyle = '#ffaa00'; // Оранжевый
        ctx.lineWidth = 1;
        ctx.stroke();

        // Название белым цветом
        if (this.observer.name) {
            ctx.font = 'bold 9px monospace';
            ctx.fillStyle = '#ffaa00'; // Белый текст
            ctx.textAlign = 'left';
            ctx.textBaseline = 'middle';
            ctx.fillText(this.observer.name.toLocaleUpperCase(), p.x + 5, p.y);
        }
    };

    /**
     * Отрисовка зоны видимости спутника по точкам с сервера.
     * Контур задаётся массивом {lon, lat}; при пересечении антимеридиана — разрыв линии.
     */
    EarthView.prototype._drawVisibilityZone = function() {
        const ctx = this.ctx;
        const points = this.satellite.visibilityZone;

        if (!points || points.length < 2) { return; }

        // Собираем сегменты (разрыв при пересечении антимеридиана)
        var segments = [];
        var currentSegment = [];
        var prevP = null;

        for (var i = 0; i < points.length; i++) {
            var pt = points[i];
            var p = this.project(pt.lon, pt.lat);

            if (prevP && Math.abs(p.x - prevP.x) > this.width / 2) {
                // Разрыв — сохраняем текущий сегмент и начинаем новый
                if (currentSegment.length > 0) {
                    segments.push(currentSegment);
                }
                currentSegment = [];
            }

            currentSegment.push(p);
            prevP = p;
        }
        if (currentSegment.length > 0) {
            segments.push(currentSegment);
        }

        // Рисуем заливку (если зона не разорвана)
        if (segments.length === 1 && segments[0].length > 2) {
            ctx.beginPath();
            ctx.moveTo(segments[0][0].x, segments[0][0].y);
            for (var j = 1; j < segments[0].length; j++) {
                ctx.lineTo(segments[0][j].x, segments[0][j].y);
            }
            ctx.closePath();
            ctx.fillStyle = this.colors.footprintFill;
            ctx.fill();
        }

        // Рисуем контур (тонкая линия)
        ctx.strokeStyle = this.colors.footprint;
        ctx.lineWidth = 1.5;
        ctx.setLineDash([]);

        for (var k = 0; k < segments.length; k++) {
            var seg = segments[k];
            if (seg.length < 2) continue;

            ctx.beginPath();
            ctx.moveTo(seg[0].x, seg[0].y);
            for (var m = 1; m < seg.length; m++) {
                ctx.lineTo(seg[m].x, seg[m].y);
            }
            ctx.stroke();
        }
    };

    // ========== API методы ==========

    /**
     * Инициализация компонента
     * @returns {Promise}
     */
    EarthView.prototype.init = function() {
        const self = this;
        return this.loadCoastlines().then(function() {
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
     * Установка зоны видимости спутника (контур с сервера).
     * @param {Array} points - Массив точек [{lon, lat}, ...]
     */
    EarthView.prototype.setVisibilityZone = function(points) {
        this.satellite.visibilityZone = Array.isArray(points) ? points : null;
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
     * Обработчик изменения размера
     */
    EarthView.prototype._onResize = function() {
        this.width = this.canvas.width;
        this.height = this.canvas.height;
        this.draw();
    };

    /**
     * Обновление информационной панели
     * @param {number} time - Время (timestamp)
     */
    EarthView.prototype.updateInfoPanel = function(time) {
        const pos = this.satellite.position;
        if (!pos) { return; }

        // Обновляем элементы если они существуют
        const elName = document.getElementById('info-name');
        const elNorad = document.getElementById('info-norad');
        const elLat = document.getElementById('info-lat');
        const elLon = document.getElementById('info-lon');
        const elAlt = document.getElementById('info-alt');
        const elObserver = document.getElementById('info-observer');
        const elTime = document.getElementById('info-time');

        if (elName) { elName.textContent = this.satellite.name || 'Unknown'; }
        if (elNorad) { elNorad.textContent = this.satellite.noradId || '-----'; }

        if (elLat) {
            const latDir = pos.lat >= 0 ? 'N' : 'S';
            elLat.textContent = Math.abs(pos.lat).toFixed(2) + '°' + latDir;
        }
        if (elLon) {
            const lonDir = pos.lon >= 0 ? 'E' : 'W';
            elLon.textContent = Math.abs(pos.lon).toFixed(2) + '°' + lonDir;
        }
        if (elAlt) {
            elAlt.textContent = (pos.alt || 0).toFixed(0) + ' km';
        }

        if (elObserver && this.observer) {
            elObserver.textContent = this.observer.name || 'Unknown';
        }

        if (elTime) {
            const date = new Date(time || Date.now());
            const hours = date.getUTCHours().toString().padStart(2, '0');
            const mins = date.getUTCMinutes().toString().padStart(2, '0');
            const secs = date.getUTCSeconds().toString().padStart(2, '0');
            elTime.textContent = hours + ':' + mins + ':' + secs;
        }
    };

    // Экспорт
    window.EarthView = EarthView;

})();
