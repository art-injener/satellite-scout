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
            landUrl: '/static/data/ne_110m_land.json', // полигоны суши для заливки материков
            russiaBordersUrl: '/static/data/russia_110m.geojson',
            gridStep: 30, // Шаг сетки в градусах
            showGrid: true,
            showCoastlines: true,
            showLandFill: true, // заливка материков (границы поверх)
            showRussiaBorders: true, // Границы РФ и подпись «Россия»
            showFootprint: true, // Круг видимости спутника
            trackMode: 'both', // 'line', 'dots', 'both'
            trackDotInterval: 60000 // Интервал точек в мс (1 минута)
        }, options || {});

        // Цветовая схема в стиле STSPLUS (улучшенная для читаемости)
        this.colors = {
            background: '#000010', // Тёмно-синий фон (океаны)
            landFill: '#0d1a22', // Заливка материков (тёмный сине-зелёный)
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
            russiaBorder: '#aabbcc',
            russiaLabel: '#ffcc00',
            selectedTrack: '#ffff00', // Жёлтый — пунктирная траектория текущего (выбранного) спутника
            selectedMarker: '#2ecc71', // Зелёный — маркер текущего спутника
            selectedFootprint: 'rgba(93, 173, 226, 0.6)', // Голубой — контур зоны радиовидимости выбранного (отличается от пурпурной зоны при сопровождении)
            selectedFootprintFill: 'rgba(93, 173, 226, 0.12)' // Голубой — заливка зоны
        };

        // Данные береговых линий (GeoJSON)
        this.coastlineData = null;

        // Данные полигонов суши (GeoJSON) — для заливки материков
        this.landData = null;

        // Данные границ РФ (GeoJSON)
        this.russiaData = null;

        // Состояние карты
        this.center = { lon: 0, lat: 0 }; // Центр карты
        this.zoom = 1.0; // Масштаб (1.0 = вся карта)

        // Спутник на сопровождении (tracking): red/green + dots + footprint.
        this.satellite = {
            position: null,
            groundTrack: [],
            visibilityZone: null,
            name: '',
            noradId: null
        };

        // Текущий (выбранный) спутник: пунктирная жёлтая трасса, зелёный маркер, голубая зона видимости.
        this._selectedSatellite = {
            position: null,
            groundTrack: null,
            visibilityZone: null,
            name: '',
            noradId: null
        };

        // Вторичные спутники группы: noradId → {noradId, name, position, track}
        this._secondarySatellites = {};

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
        const displayWidth = this.canvas.clientWidth;
        const displayHeight = this.canvas.clientHeight;
        if (displayWidth <= 0 || displayHeight <= 0) { return; }

        const dpr = window.devicePixelRatio || 1;
        const backingWidth = Math.round(displayWidth * dpr);
        const backingHeight = Math.round(displayHeight * dpr);

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
     * Загрузка полигонов суши (материки) для заливки
     * @param {string} url - URL GeoJSON файла
     * @returns {Promise}
     */
    EarthView.prototype.loadLand = function(url) {
        const self = this;
        url = url || this.options.landUrl;

        return fetch(url)
            .then(function(response) {
                if (!response.ok) {
                    throw new Error('Ошибка загрузки: ' + response.status);
                }
                return response.json();
            })
            .then(function(data) {
                self.landData = data;
                // eslint-disable-next-line no-console
                console.log('EarthView: загружены полигоны суши', data.features ? data.features.length : 0, 'объектов');
                return data;
            })
            .catch(function(error) {
                // eslint-disable-next-line no-console
                console.warn('EarthView: полигоны суши не загружены:', error.message);
                self.landData = null;
                return null;
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

        // Заливка материков (полигоны суши) — перед береговыми линиями
        if (this.options.showLandFill && this.landData && this.landData.features) {
            this._drawLand();
        }

        if (this.options.showCoastlines && this.coastlineData) {
            this._drawCoastlines();
        }

        if (this.options.showRussiaBorders && this.russiaData) {
            this._drawRussiaBorders();
        }

        // Столицы мира
        this._drawCities();

        // Слой 1: вторичные спутники (серые пунктиры).
        this._drawSecondaryLayer();

        // Слой 2: выбранный спутник (оранжевый, без точек).
        // Рисуется только если отличается от tracking (иначе tracking перекроет).
        if (this._selectedSatellite.noradId &&
            this._selectedSatellite.noradId !== this.satellite.noradId) {
            this._drawSelectedLayer();
        }

        // Слой 3: спутник на сопровождении (red/green + dots + footprint).
        if (this.satellite.noradId) {
            if (this._hasGroundTrack()) {
                this._drawGroundTrack();
            }
            if (this.options.showFootprint && this.satellite.visibilityZone && this.satellite.visibilityZone.length > 0) {
                this._drawVisibilityZone();
            }
        }

        // Наблюдатель
        if (this.observer) {
            this._drawObserver();
        }

        // Маркер спутника на сопровождении (tracking).
        if (this.satellite.noradId && this.satellite.position) {
            this._drawSatellite();
        }

        // Зона видимости и полноценная иконка выбранного спутника (если не на сопровождении).
        if (this._selectedSatellite.noradId &&
            this._selectedSatellite.noradId !== this.satellite.noradId) {
            if (this.options.showFootprint && this._selectedSatellite.visibilityZone && this._selectedSatellite.visibilityZone.length > 0) {
                this._drawSelectedVisibilityZone();
            }
            if (this._selectedSatellite.position) {
                this._drawSelectedSatelliteIcon();
            }
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
     * Отрисовка заливки материков (полигоны суши)
     */
    EarthView.prototype._drawLand = function() {
        const ctx = this.ctx;
        const features = this.landData.features;

        ctx.fillStyle = this.colors.landFill;

        for (let i = 0; i < features.length; i++) {
            const geometry = features[i].geometry;
            if (!geometry || !geometry.coordinates) { continue; }

            if (geometry.type === 'Polygon') {
                this._fillPolygonRing(geometry.coordinates[0]); // только внешний контур
            } else if (geometry.type === 'MultiPolygon') {
                for (let p = 0; p < geometry.coordinates.length; p++) {
                    const poly = geometry.coordinates[p];
                    if (poly && poly[0]) {
                        this._fillPolygonRing(poly[0]);
                    }
                }
            }
        }
    };

    /**
     * Заливка одного кольца полигона (внешний контур).
     *
     * Полярные полигоны (Антарктика, Арктика) содержат координаты широты ±90° и пересекают
     * антимеридиан по нижнему/верхнему краю карты. Прежний подход разбивал путь на
     * сегменты и замыкал каждый по краю карты, что давало неверную заливку: при
     * возврате по краю от yPrev к yStart вверх (на север) создавался лишний залитый
     * прямоугольник в океане, а closePath второго сегмента проводил диагональ через океан.
     *
     * Исправление: для полярных полигонов используем ЕДИНЫЙ путь с обходом через угол
     * полюса при пересечении антимеридиана. closePath() в конце замыкает путь прямо в
     * начальную точку (moveTo), не создавая лишних диагоналей.
     *
     * @param {Array} coords - Массив координат [[lon, lat], ...]
     */
    EarthView.prototype._fillPolygonRing = function(coords) {
        if (!coords || coords.length < 3) { return; }

        const ctx = this.ctx;
        const w = this.width;
        const h = this.height;

        // Определяем тип полигона: содержит ли он южный или северный полюс
        let hasSouthPole = false, hasNorthPole = false;
        for (let i = 0; i < coords.length; i++) {
            if (coords[i][1] <= -89.5) { hasSouthPole = true; }
            if (coords[i][1] >= 89.5)  { hasNorthPole = true; }
        }

        ctx.beginPath();
        let prevP = null;
        let moved = false;

        for (let i = 0; i < coords.length; i++) {
            const lon = coords[i][0];
            const lat = coords[i][1];
            const p = this.project(lon, lat);
            const px = Math.max(0, Math.min(w, p.x));
            const py = Math.max(0, Math.min(h, p.y));

            if (prevP && Math.abs(p.x - prevP.x) > w / 2) {
                // Пересечение антимеридиана
                const goingRightToLeft = p.x < prevP.x;
                const edgeX         = goingRightToLeft ? w : 0;
                const oppositeEdgeX = goingRightToLeft ? 0 : w;
                const yPrev = Math.max(0, Math.min(h, prevP.y));

                ctx.lineTo(edgeX, yPrev);

                if (hasSouthPole) {
                    // Антарктика: обходим через нижний край (южный полюс = y = h)
                    ctx.lineTo(edgeX, h);
                    ctx.lineTo(oppositeEdgeX, h);
                } else if (hasNorthPole) {
                    // Арктика: обходим через верхний край (северный полюс = y = 0)
                    ctx.lineTo(edgeX, 0);
                    ctx.lineTo(oppositeEdgeX, 0);
                }

                ctx.lineTo(px, py);
                moved = true;
            } else if (!moved) {
                ctx.moveTo(px, py);
                moved = true;
            } else {
                ctx.lineTo(px, py);
            }

            prevP = p;
        }

        // closePath замыкает путь прямо в начальную точку (moveTo)
        ctx.closePath();
        ctx.fill();
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
            const pastLen = (track.past && track.past.length) ? track.past.reduce(function(s, seg) { return s + seg.length; }, 0) : 0;
            const futureLen = (track.future && track.future.length) ? track.future.reduce(function(s, seg) { return s + seg.length; }, 0) : 0;
            return pastLen > 0 || futureLen > 0;
        }
        return false;
    };

    /**
     * Отрисовка наземной трассы сопровождаемого спутника.
     * Сплошные линии: красная — прошлая орбита, зелёная — будущая; плюс точки (минутные метки).
     */
    EarthView.prototype._drawGroundTrack = function() {
        const track = this.satellite.groundTrack;
        this.ctx.setLineDash([]); // сплошная линия

        if (Array.isArray(track)) {
            if (track.length >= 2) {
                this._drawTrackSegment(track, this.colors.orbitFuture);
            }
            return;
        }

        if (track && track.past) {
            for (let i = 0; i < track.past.length; i++) {
                const seg = track.past[i];
                if (seg && seg.length >= 2) {
                    this._drawTrackSegment(seg, this.colors.orbitPast);
                }
            }
        }
        if (track && track.future) {
            for (let j = 0; j < track.future.length; j++) {
                const segF = track.future[j];
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
            ctx.lineWidth = 1.5;
            ctx.beginPath();

            let prevP = null;
            let moved = false;

            for (let i = 0; i < points.length; i++) {
                const pt = points[i];
                const p = this.project(pt.lon, pt.lat);

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

            for (let k = 0; k < points.length; k++) {
                const point = points[k];
                const t = point.ts !== null ? point.ts : point.time;
                if (t - lastDotTime >= dotInterval) {
                    const pp = this.project(point.lon, point.lat);
                    ctx.beginPath();
                    ctx.arc(pp.x, pp.y, 2.5, 0, Math.PI * 2); // Очень маленькие точки
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
        const dpr = window.devicePixelRatio || 1;
        const s = dpr * 1.2; // базовый множитель для увеличения

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
        ctx.fillRect(p.x - 3 * s, p.y - 8 * s, 6 * s, 16 * s);

        // Солнечные панели (горизонтальные)
        ctx.fillRect(p.x - 16 * s, p.y - 3 * s, 10 * s, 6 * s);
        ctx.fillRect(p.x + 6 * s, p.y - 3 * s, 10 * s, 6 * s);

        // Дополнительные элементы панелей
        ctx.lineWidth = 1.5 * s;
        ctx.beginPath();
        ctx.moveTo(p.x - 16 * s, p.y);
        ctx.lineTo(p.x - 19 * s, p.y - 4 * s);
        ctx.moveTo(p.x - 16 * s, p.y);
        ctx.lineTo(p.x - 19 * s, p.y + 4 * s);
        ctx.moveTo(p.x + 16 * s, p.y);
        ctx.lineTo(p.x + 19 * s, p.y - 4 * s);
        ctx.moveTo(p.x + 16 * s, p.y);
        ctx.lineTo(p.x + 19 * s, p.y + 4 * s);
        ctx.stroke();

        // Сброс свечения
        ctx.shadowColor = 'transparent';
        ctx.shadowBlur = 0;

        // Название спутника — цвет и обводка (без фона)
        if (this.satellite.name) {
            const fontSize = Math.round(12 * s);
            ctx.font = 'bold ' + fontSize + 'px monospace';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'top';
            const labelY = p.y + 12 * s;
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
        ctx.moveTo(p.x, p.y - size); // вершина
        ctx.lineTo(p.x - size, p.y + size); // нижний левый
        ctx.lineTo(p.x + size, p.y + size); // нижний правый
        ctx.closePath();
        ctx.fillStyle = this.colors.observerLabel || '#ff9500';
        ctx.strokeStyle = this.colors.observerLabelStroke || 'rgba(0,0,0,0.9)';
        ctx.lineWidth = 1;
        ctx.fill();
        ctx.stroke();

        // Название точки наблюдения — белый цвет
        if (this.observer.name) {
            const obsText = this.observer.name.toLocaleUpperCase();
            ctx.font = 'bold 12px sans-serif';
            ctx.textAlign = 'left';
            ctx.textBaseline = 'middle';
            const labelX = p.x + size + 3;
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
        const ctx = this.ctx;
        const segments = this.satellite.visibilityZone;

        if (!segments || segments.length === 0) { return; }

        const dpr = window.devicePixelRatio || 1;

        for (let k = 0; k < segments.length; k++) {
            const seg = segments[k];
            if (!seg || seg.length < 3) { continue; }

            // Проецируем точки сегмента
            const projected = [];
            for (let i = 0; i < seg.length; i++) {
                projected.push(this.project(seg[i].lon, seg[i].lat));
            }

            // Заливка
            ctx.beginPath();
            ctx.moveTo(projected[0].x, projected[0].y);
            for (let j = 1; j < projected.length; j++) {
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
            for (let m = 1; m < projected.length; m++) {
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
        const self = this;
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
            this.loadLand(),
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
        this.satellite.name = name || '';
        this.satellite.noradId = noradId;
    };

    /** Полная очистка слоя «на сопровождении» (когда сопровождения нет). */
    EarthView.prototype.clearTrackingLayer = function() {
        this.satellite.position = null;
        this.satellite.name = '';
        this.satellite.noradId = null;
        this.satellite.groundTrack = [];
        this.satellite.visibilityZone = null;
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

    // ========== Выбранный спутник (selected, оранжевый) ==========

    EarthView.prototype.setSelectedSatellitePosition = function(lon, lat, alt) {
        this._selectedSatellite.position = { lon: lon, lat: lat, alt: alt || 0 };
    };

    EarthView.prototype.setSelectedSatelliteInfo = function(name, noradId) {
        this._selectedSatellite.name = name;
        this._selectedSatellite.noradId = noradId;
    };

    EarthView.prototype.setSelectedGroundTrack = function(data) {
        if (!data) {
            this._selectedSatellite.groundTrack = null;
            return;
        }
        if (Array.isArray(data)) {
            this._selectedSatellite.groundTrack = data;
            return;
        }
        this._selectedSatellite.groundTrack = {
            past: data.past || [],
            future: data.future || []
        };
    };

    EarthView.prototype.setSelectedVisibilityZone = function(segments) {
        this._selectedSatellite.visibilityZone = Array.isArray(segments) ? segments : null;
    };

    EarthView.prototype.clearSelectedSatellite = function() {
        this._selectedSatellite = { position: null, groundTrack: null, visibilityZone: null, name: '', noradId: null };
    };

    /**
     * Отрисовка слоя текущего (выбранного) спутника: пунктирная жёлтая линия, без точек.
     * @private
     */
    EarthView.prototype._drawSelectedLayer = function() {
        var sel = this._selectedSatellite;
        var track = sel.groundTrack;
        if (!track) { return; }

        var ctx = this.ctx;
        var color = this.colors.selectedTrack;
        var dpr = window.devicePixelRatio || 1;
        var dash = [6 * dpr, 4 * dpr];

        ctx.strokeStyle = color;
        ctx.lineWidth = 2 * dpr;
        ctx.setLineDash(dash);

        var segments = [];
        if (track && typeof track === 'object' && !Array.isArray(track)) {
            if (Array.isArray(track.past)) { segments = segments.concat(track.past); }
            if (Array.isArray(track.future)) { segments = segments.concat(track.future); }
        } else if (Array.isArray(track)) {
            segments = [track];
        }

        for (var s = 0; s < segments.length; s++) {
            var seg = segments[s];
            if (!seg || seg.length < 2) { continue; }
            ctx.beginPath();
            var prevP = null;
            var moved = false;
            for (var k = 0; k < seg.length; k++) {
                var pt = this.project(seg[k].lon, seg[k].lat);
                if (prevP && Math.abs(pt.x - prevP.x) > this.width / 2) {
                    ctx.stroke();
                    ctx.beginPath();
                    moved = false;
                }
                if (!moved) { ctx.moveTo(pt.x, pt.y); moved = true; }
                else { ctx.lineTo(pt.x, pt.y); }
                prevP = pt;
            }
            ctx.stroke();
        }
        ctx.setLineDash([]);
    };

    /**
     * Зона радиовидимости выбранного спутника (голубая заливка).
     * @private
     */
    EarthView.prototype._drawSelectedVisibilityZone = function() {
        var segments = this._selectedSatellite.visibilityZone;
        if (!segments || segments.length === 0) { return; }

        var ctx = this.ctx;
        var dpr = window.devicePixelRatio || 1;
        var fillColor = this.colors.selectedFootprintFill || 'rgba(93, 173, 226, 0.12)';
        var strokeColor = this.colors.selectedFootprint || 'rgba(93, 173, 226, 0.6)';

        for (var k = 0; k < segments.length; k++) {
            var seg = segments[k];
            if (!seg || seg.length < 3) { continue; }
            var projected = [];
            for (var i = 0; i < seg.length; i++) {
                projected.push(this.project(seg[i].lon, seg[i].lat));
            }
            ctx.beginPath();
            ctx.moveTo(projected[0].x, projected[0].y);
            for (var j = 1; j < projected.length; j++) {
                ctx.lineTo(projected[j].x, projected[j].y);
            }
            ctx.closePath();
            ctx.fillStyle = fillColor;
            ctx.fill();
            ctx.strokeStyle = strokeColor;
            ctx.lineWidth = 1.5 * dpr;
            ctx.stroke();
        }
    };

    /**
     * Полноценная иконка выбранного спутника (аналогично сопровождению, цвет — оранжевый).
     * @private
     */
    EarthView.prototype._drawSelectedSatelliteIcon = function() {
        var sel = this._selectedSatellite;
        var pos = sel.position;
        if (!pos) { return; }

        var ctx = this.ctx;
        var p = this.project(pos.lon, pos.lat);
        var dpr = window.devicePixelRatio || 1;
        var s = dpr * 1.2;
        var color = this.colors.selectedMarker || '#2ecc71';

        ctx.strokeStyle = color;
        ctx.fillStyle = color;
        ctx.lineWidth = 2 * s;
        ctx.shadowColor = color;
        ctx.shadowBlur = 4 * s;

        // Иконка в стиле МКС (как у спутника на сопровождении)
        ctx.fillRect(p.x - 3 * s, p.y - 8 * s, 6 * s, 16 * s);
        ctx.fillRect(p.x - 16 * s, p.y - 3 * s, 10 * s, 6 * s);
        ctx.fillRect(p.x + 6 * s, p.y - 3 * s, 10 * s, 6 * s);
        ctx.lineWidth = 1.5 * s;
        ctx.beginPath();
        ctx.moveTo(p.x - 16 * s, p.y);
        ctx.lineTo(p.x - 19 * s, p.y - 4 * s);
        ctx.moveTo(p.x - 16 * s, p.y);
        ctx.lineTo(p.x - 19 * s, p.y + 4 * s);
        ctx.moveTo(p.x + 16 * s, p.y);
        ctx.lineTo(p.x + 19 * s, p.y - 4 * s);
        ctx.moveTo(p.x + 16 * s, p.y);
        ctx.lineTo(p.x + 19 * s, p.y + 4 * s);
        ctx.stroke();

        ctx.shadowColor = 'transparent';
        ctx.shadowBlur = 0;

        if (sel.name) {
            var fontSize = Math.round(12 * s);
            ctx.font = 'bold ' + fontSize + 'px monospace';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'top';
            var labelY = p.y + 12 * s;
            ctx.strokeStyle = 'rgba(0,0,0,0.85)';
            ctx.lineWidth = 2.5;
            ctx.strokeText(sel.name, p.x, labelY);
            ctx.fillStyle = color;
            ctx.fillText(sel.name, p.x, labelY);
        }
    };

    // ========== Вторичные спутники ==========

    // Палитра цветов для вторичных спутников (серые оттенки, чтобы не мешать primary).
    var SECONDARY_SAT_COLORS = [
        '#aaaaaa', '#888888', '#cccccc', '#999999',
        '#bbbbbb', '#777777', '#dddddd', '#ffffff'
    ];

    /**
     * Обновление позиций вторичных спутников группы.
     * Вызывается из app.js при каждом position-апдейте.
     *
     * @param {Array} satArray — массив {noradId, name, lon, lat, alt}.
     */
    EarthView.prototype.setSecondaryPositions = function(satArray) {
        if (!satArray) { return; }
        for (var i = 0; i < satArray.length; i++) {
            var s = satArray[i];
            if (!s || !s.noradId) { continue; }
            if (!this._secondarySatellites[s.noradId]) {
                this._secondarySatellites[s.noradId] = { noradId: s.noradId, name: s.name || '', track: null };
            }
            this._secondarySatellites[s.noradId].position = { lon: s.lon, lat: s.lat, alt: s.alt || 0 };
            if (s.name) { this._secondarySatellites[s.noradId].name = s.name; }
        }
        // Удаляем спутники, которые больше не в группе.
        var activeIds = {};
        for (var j = 0; j < satArray.length; j++) {
            if (satArray[j] && satArray[j].noradId) { activeIds[satArray[j].noradId] = true; }
        }
        for (var id in this._secondarySatellites) {
            if (!activeIds[id]) { delete this._secondarySatellites[id]; }
        }
    };

    /**
     * Обновление трека вторичного спутника.
     * @param {number} noradId — NORAD ID.
     * @param {Object} track — {past, future}.
     */
    EarthView.prototype.setSecondaryTrack = function(noradId, track) {
        if (!noradId) { return; }
        // Обновляем трек ТОЛЬКО если спутник уже в карте (не воскрешаем удалённые).
        if (this._secondarySatellites[noradId]) {
            this._secondarySatellites[noradId].track = track;
        }
    };

    /**
     * Очистка всех вторичных спутников (при смене группы).
     */
    EarthView.prototype.clearSecondarySatellites = function() {
        this._secondarySatellites = {};
    };

    /**
     * Отрисовка слоя вторичных спутников (траектории и маркеры).
     * @private
     */
    EarthView.prototype._drawSecondaryLayer = function() {
        var ids = Object.keys(this._secondarySatellites);
        for (var i = 0; i < ids.length; i++) {
            var sat = this._secondarySatellites[ids[i]];
            if (sat.track) { this._drawSecondaryGroundTrack(sat, i); }
            if (sat.position) { this._drawSecondaryMarker(sat, i); }
        }
    };

    /**
     * Отрисовка пунктирной трассы вторичного спутника.
     * @private
     */
    EarthView.prototype._drawSecondaryGroundTrack = function(sat, colorIdx) {
        var ctx = this.ctx;
        var track = sat.track;
        if (!track) { return; }

        var color = 'rgba(200, 200, 200, 0.5)';
        var dpr = window.devicePixelRatio || 1;

        ctx.setLineDash([5, 5]);
        ctx.strokeStyle = color;
        ctx.lineWidth = 1.5 * dpr;

        var segments = [];
        if (Array.isArray(track.future)) { segments = segments.concat(track.future); }
        if (Array.isArray(track.past))   { segments = segments.concat(track.past); }

        for (var s = 0; s < segments.length; s++) {
            var seg = segments[s];
            if (!seg || seg.length < 2) { continue; }
            ctx.beginPath();
            var first = this.project(seg[0].lon, seg[0].lat);
            ctx.moveTo(first.x, first.y);
            for (var k = 1; k < seg.length; k++) {
                var pt = this.project(seg[k].lon, seg[k].lat);
                ctx.lineTo(pt.x, pt.y);
            }
            ctx.stroke();
        }
        ctx.setLineDash([]);
    };

    /**
     * Отрисовка геометрического маркера вторичного спутника.
     * Форма: circle, square, triangle или diamond — по хешу noradId.
     * @private
     */
    EarthView.prototype._drawSecondaryMarker = function(sat, colorIdx) {
        var ctx = this.ctx;
        var pos = sat.position;
        if (!pos) { return; }

        var p = this.project(pos.lon, pos.lat);
        var dpr = window.devicePixelRatio || 1;
        var r = 5 * dpr;
        var color = SECONDARY_SAT_COLORS[colorIdx % SECONDARY_SAT_COLORS.length];
        var shape = sat.noradId % 4; // 0=circle, 1=square, 2=triangle, 3=diamond

        ctx.fillStyle = color;
        ctx.strokeStyle = 'rgba(0,0,0,0.6)';
        ctx.lineWidth = 1 * dpr;

        ctx.beginPath();
        if (shape === 0) {
            ctx.arc(p.x, p.y, r, 0, Math.PI * 2);
        } else if (shape === 1) {
            ctx.rect(p.x - r, p.y - r, r * 2, r * 2);
        } else if (shape === 2) {
            ctx.moveTo(p.x, p.y - r);
            ctx.lineTo(p.x + r, p.y + r);
            ctx.lineTo(p.x - r, p.y + r);
            ctx.closePath();
        } else {
            ctx.moveTo(p.x, p.y - r);
            ctx.lineTo(p.x + r, p.y);
            ctx.lineTo(p.x, p.y + r);
            ctx.lineTo(p.x - r, p.y);
            ctx.closePath();
        }
        ctx.fill();
        ctx.stroke();

        // Подпись (мелко, без фона, чтобы не перекрывать карту)
        if (sat.name) {
            var fs = Math.round(9 * dpr);
            ctx.font = fs + 'px monospace';
            ctx.textAlign = 'center';
            ctx.textBaseline = 'top';
            ctx.strokeStyle = 'rgba(0,0,0,0.7)';
            ctx.lineWidth = 1.5;
            ctx.strokeText(sat.name, p.x, p.y + r + 2 * dpr);
            ctx.fillStyle = color;
            ctx.fillText(sat.name, p.x, p.y + r + 2 * dpr);
        }
    };

    // Экспорт
    window.EarthView = EarthView;

})();
