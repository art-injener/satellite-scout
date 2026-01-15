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
            trackDotInterval: 60000, // Интервал точек в мс (1 минута)
            orbitPeriodMinutes: 92, // Период орбиты (МКС ~92 мин)
            orbitRevolutions: 3 // Количество витков для отображения
        }, options || {});

        // Цветовая схема в стиле STSPLUS
        this.colors = {
            background: '#000010', // Тёмно-синий фон (океаны)
            coastline: '#00d4d4', // Циан - береговые линии
            grid: '#0044aa', // Синий - сетка (как в STSPLUS)
            gridMajor: '#0066cc', // Яркий синий - основные линии
            orbitFuture: '#00ff00', // Зелёный - будущая орбита
            orbitPast: '#ff4444', // Красный - прошлая орбита
            orbitDots: '#ffff00', // Жёлтый - точки орбиты
            satellite: '#ffffff', // Белый - маркер спутника
            satelliteGlow: '#00ffff', // Циан - свечение спутника
            footprint: '#aaaaaa', // Серый - круг видимости (пунктир)
            observer: '#ff0000', // Красный - наблюдатель (как в STSPLUS)
            textPrimary: '#ffffff',
            textSecondary: '#00d4d4', // Циан для подписей
            textGrid: '#ffffff' // Белые подписи сетки
        };

        // Данные береговых линий (GeoJSON)
        this.coastlineData = null;

        // Состояние карты
        this.center = { lon: 0, lat: 0 }; // Центр карты
        this.zoom = 1.0; // Масштаб (1.0 = вся карта)

        // Данные спутника
        this.satellite = {
            position: null, // {lon, lat, alt}
            groundTrack: [], // Массив точек орбиты
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
            { name: 'CAIRO', lon: 31.24, lat: 30.04 },
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

        // Орбита (ground track)
        if (this.satellite.groundTrack.length > 0) {
            this._drawGroundTrack();
        }

        // Круг видимости спутника (footprint)
        if (this.options.showFootprint && this.satellite.position) {
            this._drawFootprint();
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
     * Отрисовка ground track орбиты
     */
    EarthView.prototype._drawGroundTrack = function() {
        const track = this.satellite.groundTrack;

        if (track.length < 2) { return; }

        // Отрисовка всей орбиты зелёным
        this._drawTrackSegment(track, this.colors.orbitFuture);
    };

    /**
     * Отрисовка сегмента орбиты
     * @param {Array} points - Массив точек [{lon, lat, time}]
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

            let prevP = null;
            let moved = false;

            for (let i = 0; i < points.length; i++) {
                const p = this.project(points[i].lon, points[i].lat);

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
            let lastDotTime = -Infinity;

            for (let i = 0; i < points.length; i++) {
                const point = points[i];
                // Рисуем точку каждую минуту
                if (point.time - lastDotTime >= dotInterval) {
                    const p = this.project(point.lon, point.lat);
                    ctx.beginPath();
                    ctx.arc(p.x, p.y, 1, 0, Math.PI * 2); // Очень маленькие точки
                    ctx.fill();
                    lastDotTime = point.time;
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

        // Название спутника
        if (this.satellite.name) {
            ctx.font = 'bold 11px monospace';
            ctx.fillStyle = this.colors.textPrimary;
            ctx.textAlign = 'left';
            ctx.textBaseline = 'bottom';
            ctx.fillText(this.satellite.name, p.x + 18, p.y - 4);
        }
    };

    /**
     * Отрисовка позиции наблюдателя
     */
    EarthView.prototype._drawObserver = function() {
        const ctx = this.ctx;
        const p = this.project(this.observer.lon, this.observer.lat);

        // Жёлтый кружок с заливкой
        ctx.beginPath();
        ctx.arc(p.x, p.y, 4, 0, Math.PI * 2);
        ctx.fillStyle = '#ffff00'; // Жёлтый
        ctx.fill();

        // Название белым цветом
        if (this.observer.name) {
            ctx.font = 'bold 10px monospace';
            ctx.fillStyle = '#ffffff'; // Белый текст
            ctx.textAlign = 'left';
            ctx.textBaseline = 'middle';
            ctx.fillText(this.observer.name, p.x + 8, p.y);
        }
    };

    /**
     * Отрисовка круга видимости спутника (footprint)
     * Радиус видимости зависит от высоты орбиты
     */
    EarthView.prototype._drawFootprint = function() {
        const ctx = this.ctx;
        const pos = this.satellite.position;

        // Расчёт углового радиуса видимости
        // Формула: cos(rho) = R_earth / (R_earth + altitude)
        const R_EARTH = 6371; // км
        const altitude = pos.alt || 420; // Высота орбиты (по умолчанию МКС ~420 км)
        const rho = Math.acos(R_EARTH / (R_EARTH + altitude)) * 180 / Math.PI;

        // Рисуем круг видимости пунктирной линией
        ctx.strokeStyle = this.colors.footprint;
        ctx.lineWidth = 1;
        ctx.setLineDash([5, 5]);

        ctx.beginPath();

        // Генерируем точки окружности
        const numPoints = 72; // Точек для плавного круга
        let prevP = null;

        for (let i = 0; i <= numPoints; i++) {
            const angle = (i / numPoints) * 360;
            const angleRad = angle * Math.PI / 180;

            // Вычисляем точку на окружности (сферическая геометрия)
            const latRad = pos.lat * Math.PI / 180;
            const rhoRad = rho * Math.PI / 180;

            const pointLat = Math.asin(
                Math.sin(latRad) * Math.cos(rhoRad) +
                Math.cos(latRad) * Math.sin(rhoRad) * Math.cos(angleRad)
            ) * 180 / Math.PI;

            const pointLon = pos.lon + Math.atan2(
                Math.sin(angleRad) * Math.sin(rhoRad) * Math.cos(latRad),
                Math.cos(rhoRad) - Math.sin(latRad) * Math.sin(pointLat * Math.PI / 180)
            ) * 180 / Math.PI;

            const p = this.project(pointLon, pointLat);

            // Проверка на пересечение антимеридиана
            if (prevP && Math.abs(p.x - prevP.x) > this.width / 2) {
                ctx.stroke();
                ctx.beginPath();
                ctx.moveTo(p.x, p.y);
            } else if (i === 0) {
                ctx.moveTo(p.x, p.y);
            } else {
                ctx.lineTo(p.x, p.y);
            }

            prevP = p;
        }

        ctx.stroke();
        ctx.setLineDash([]); // Сброс пунктира
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
     * Установка ground track орбиты
     * @param {Array} points - Массив точек [{lon, lat, time}]
     */
    EarthView.prototype.setGroundTrack = function(points) {
        this.satellite.groundTrack = points || [];
    };

    /**
     * Добавление точки к ground track
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
     * Очистка ground track
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
     * Демо-анимация движения спутника
     * @param {number} speed - Скорость (множитель времени, 1 = реальное время)
     */
    EarthView.prototype.startDemo = function(speed) {
        const self = this;
        speed = speed || 1;

        // Тестовые данные МКС-подобной орбиты
        this.setSatelliteInfo('ISS', 25544);
        // Ростов-на-Дону
        this.setObserver(39.7, 47.23, 'Rostov-on-Don');

        const inclination = 51.6; // Наклонение орбиты МКС (градусы)
        const orbitalPeriod = 92 * 60 * 1000; // Период орбиты в мс (~92 минуты)
        const revolutions = this.options.orbitRevolutions || 3; // Количество витков

        // Время симуляции
        let simTime = Date.now();
        let lastTrackUpdateTime = 0;
        let currentPointIndex = 0;

        // Функция расчёта позиции спутника по времени
        function calcPosition(time) {
            // Угловая скорость (градусов в мс)
            const angularSpeed = 360 / orbitalPeriod;
            // Текущий угол на орбите
            const angle = (time * angularSpeed) % 360;
            // Долгота (учитываем вращение Земли: -360°/24ч)
            const earthRotation = (time / (24 * 60 * 60 * 1000)) * 360;
            const lon = (angle - earthRotation) % 360;
            // Нормализация долготы
            const normalizedLon = lon > 180 ? lon - 360 : (lon < -180 ? lon + 360 : lon);
            // Широта (синусоида с наклонением)
            const lat = inclination * Math.sin(angle * Math.PI / 180);

            return { lon: normalizedLon, lat: lat };
        }

        // Генерация статичной орбиты на несколько витков вперёд
        function generateTrack(baseTime) {
            self.clearGroundTrack();

            // Генерируем орбиту: только вперёд на 3 витка
            const futureDuration = orbitalPeriod * revolutions;
            const step = 30000; // Шаг 30 секунд для плавности

            for (let dt = 0; dt <= futureDuration; dt += step) {
                const time = baseTime + dt;
                const pos = calcPosition(time);
                self.addTrackPoint(pos.lon, pos.lat, time);
            }

            lastTrackUpdateTime = baseTime;
            currentPointIndex = 0; // Начинаем с первой точки
        }

        // Поиск ближайшей точки на орбите для текущего времени
        function findCurrentPointIndex() {
            const track = self.satellite.groundTrack;
            for (let i = 0; i < track.length; i++) {
                if (track[i].time >= simTime) {
                    return Math.max(0, i - 1);
                }
            }
            return track.length - 1;
        }

        function animate() {
            // Обновление симулированного времени (медленнее)
            simTime += 200 * speed; // +0.2 секунды * speed за кадр (~12x реального времени)

            // Проверяем, нужно ли обновить орбиту (каждый виток)
            const timeSinceUpdate = simTime - lastTrackUpdateTime;
            if (timeSinceUpdate > orbitalPeriod * 0.9) {
                // Спутник близко к концу отображаемой орбиты - обновляем
                generateTrack(simTime);
            }

            // Движение спутника по точкам орбиты
            const track = self.satellite.groundTrack;
            if (track.length > 0) {
                // Находим текущую точку на орбите
                currentPointIndex = findCurrentPointIndex();

                if (currentPointIndex < track.length) {
                    const point = track[currentPointIndex];
                    self.setSatellitePosition(point.lon, point.lat, 420);
                }
            }

            self.draw();
            self.updateInfoPanel(simTime);
            self._animationId = requestAnimationFrame(animate);
        }

        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
        }

        // Генерируем орбиту один раз при старте
        generateTrack(simTime);
        animate();
    };

    /**
     * Обновление информационной панели
     * @param {number} simTime - Симулированное время
     */
    EarthView.prototype.updateInfoPanel = function(simTime) {
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
            elAlt.textContent = (pos.alt || 420).toFixed(0) + ' km';
        }

        if (elObserver && this.observer) {
            elObserver.textContent = this.observer.name || 'Unknown';
        }

        if (elTime) {
            const date = new Date(simTime);
            const hours = date.getUTCHours().toString().padStart(2, '0');
            const mins = date.getUTCMinutes().toString().padStart(2, '0');
            const secs = date.getUTCSeconds().toString().padStart(2, '0');
            elTime.textContent = hours + ':' + mins + ':' + secs;
        }
    };

    /**
     * Остановка демо-анимации
     */
    EarthView.prototype.stopDemo = function() {
        if (this._animationId) {
            cancelAnimationFrame(this._animationId);
            this._animationId = null;
        }
    };

    // Экспорт
    window.EarthView = EarthView;

})();
