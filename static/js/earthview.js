// EarthView - Компонент отображения карты мира с орбитами спутников
// Стиль вдохновлён программой STSPLUS (1989-2006)

(function() {
    'use strict';

    /** Половина стороны DOM-маркера (px) — совпадает с .map-sat-marker в main.css */
    const MAP_SAT_MARKER_HALF_PX = 28;

    /**
     * Поворот маркера с бэкенда (map_marker_rot_deg) — предпочтительно.
     * Запасной вариант: локальный расчёт с учётом внутреннего rotate(45) в SVG.
     */
    const MAP_SAT_SVG_OFFSET_DEG = -45;
    const MAP_SAT_DEFAULT_ROT = 0;

    // Палитра вторичных спутников без включённой трассы: крупные маркеры + высокая яркость
    // (почти белый / ледяной / мягкий акцент), чтобы не терялись на тёмной карте (UX-MAP-VIS-001).
    // Используется как в _drawSecondaryMarker, так и в _collectCalloutMarkers.
    const SECONDARY_SAT_COLORS = [
        '#ffffff', '#f0fcff', '#e8ffff', '#fffef0',
        '#f5fff8', '#ffe8f5', '#e8f4ff', '#fffff0'
    ];

    /** Кратчайший доворот от currentDeg к targetDeg (градусы, в [-180, 180]). */
    function _shortestRotDeltaDeg(currentDeg, targetDeg) {
        let d = targetDeg - currentDeg;
        d %= 360;
        if (d > 180) { d -= 360; }
        if (d < -180) { d += 360; }
        return d;
    }

    /** Обрезка имени спутника для canvas-подписей. */
    function _shortName(name, maxLen) {
        if (!name) { return ''; }
        maxLen = maxLen || 16;
        return name.length > maxLen ? name.slice(0, maxLen - 1) + '\u2026' : name;
    }

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

        this._reloadColorsFromCss();

        // Данные границ РФ (GeoJSON)
        this.russiaData = null;

        // Данные береговых линий (GeoJSON)
        this.coastlineData = null;

        // Данные полигонов суши (GeoJSON) — для заливки материков
        this.landData = null;


        // Состояние карты
        this.center = { lon: 0, lat: 0 }; // Центр карты
        this.zoom = 1.0; // Масштаб (1.0 = вся карта)

        // Спутник под наблюдением (tracking): red/green + dots + footprint.
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
            // Иначе скачок экранных координат ломает логику «влево/вправо»
            this._domMarkerState = null;
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
                console.log('EarthView: загружены границы РФ');
                return data;
            })
            .catch(function(error) {
                console.warn('EarthView: границы РФ не загружены:', error.message);
                return null;
            });
    };

    // ========== Палитра из CSS (повторно после смены темы) ==========

    EarthView.prototype._reloadColorsFromCss = function() {
        this.colors = {
            background:    cssVar('--map-ocean', '#0a1018'),
            landFill:      cssVar('--map-land', '#1a2c3c'),
            coastline:     cssVar('--map-coast', '#5a9aac'),
            grid:          cssVar('--map-grid', '#2a4050'),
            gridMajor:     cssVar('--map-grid-major', '#385868'),
            orbitFuture:   cssVar('--orbit-future', '#00cc00'),
            orbitPast:     cssVar('--orbit-past', '#d94848'),
            orbitDots:     cssVar('--orbit-dots', '#ffff00'),
            satellite:     cssVar('--sat-marker', '#ffffff'),
            satelliteGlow: cssVar('--sat-glow', '#00ffff'),
            footprint:              themeRgba('map-footprint', 'rgba(0,255,255,0.6)'),
            footprintFill:          themeRgba('map-footprint-fill', 'rgba(0,255,255,0.05)'),
            observer:      cssVar('--observer-marker', '#ff0000'),
            observerLabel:       cssVar('--observer-marker', '#ff9500'),
            observerLabelStroke: themeRgba('map-observer-label-stroke', 'rgba(0,0,0,0.9)'),
            observerLabelBg:     themeRgba('map-observer-label-bg', 'rgba(0,0,0,0.6)'),
            textPrimary:    cssVar('--text-primary', '#ffffff'),
            textSecondary:  cssVar('--map-text-secondary', '#00d4d4'),
            textGrid:       cssVar('--map-grid-text', '#7890a0'),
            satLabel:       cssVar('--sat-label', '#ffeb3b'),
            satLabelStroke: themeRgba('map-sat-label-stroke', 'rgba(0,0,0,0.85)'),
            satLabelBg:     themeRgba('map-sat-label-bg', 'rgba(0,0,0,0.6)'),
            selectedTrack:         cssVar('--selected-track', '#ffff00'),
            selectedMarker:        cssVar('--selected-marker', '#2ecc71'),
            selectedFootprint:     themeRgba('map-selected-footprint', 'rgba(93,173,226,0.6)'),
            selectedFootprintFill: themeRgba('map-selected-footprint-fill', 'rgba(93,173,226,0.12)'),
            russiaBorder:   cssVar('--map-russia-border', '#aabbcc'),
            russiaLabel:    cssVar('--map-russia-label', '#ffcc00'),
            cityLabel:  cssVar('--map-city-label', '#ffffff'),
            cityMarker: cssVar('--map-city-marker', '#cc6666'),
            mapIconStroke:        cssVar('--map-icon-stroke', 'rgba(10,14,20,0.94)'),
            mapIconTrackingFill:  cssVar('--map-icon-tracking-fill', '#00d4aa'),
            mapIconSelectedFill:  cssVar('--map-icon-selected-fill', '#ffb347'),
            mapIconNeutralFill:   cssVar('--map-icon-neutral-fill', '#e6e8eb'),
            mapIconBoomFill:      cssVar('--map-icon-boom-fill', '#8b919a')
        };
        this._mapTrackLineWidth = parseFloat(cssVar('--map-track-line-width', '1.5')) || 1.5;
        this._mapFootprintLineWidth = parseFloat(cssVar('--map-footprint-line-width', '1.5')) || 1.5;
    };

    /** Вызов после смены colors-*.css (theme-switcher): обновить кэш и перерисовать. */
    EarthView.prototype.refreshThemeColors = function() {
        this._reloadColorsFromCss();
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

        // Слои отрисовки (порядок важен: сетка → суша → берега → подписи координат)
        // Сетка под сушей — материки дают чистый фон для спутников
        if (this.options.showGrid) {
            this._drawGrid();
        }

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

        // Наблюдатель — на уровне географических объектов
        if (this.observer) {
            this._drawObserver();
        }

        // Подписи координат — поверх всех географических слоёв
        if (this.options.showGrid) {
            this._drawGridLabels();
        }

        // Слой 1: вторичные спутники (серые пунктиры).
        this._drawSecondaryLayer();

        // Слой 2: выбранный спутник (оранжевый, без точек).
        // Рисуется только если отличается от tracking (иначе tracking перекроет).
        if (this._selectedSatellite.noradId &&
            this._selectedSatellite.noradId !== this.satellite.noradId) {
            this._drawSelectedLayer();
        }

        // Слой 3: спутник под наблюдением (red/green + dots + footprint).
        if (this.satellite.noradId) {
            if (this._hasGroundTrack()) {
                this._drawGroundTrack();
            }
            if (this.options.showFootprint && this.satellite.visibilityZone && this.satellite.visibilityZone.length > 0) {
                this._drawVisibilityZone();
            }
        }

        // Выноски (линии на canvas + DOM-карточки).
        // Между трассами/футпринтом и SVG-маркерами: линии получаются под иконкой,
        // DOM-карточки — на отдельном слое поверх (z-index в CSS).
        this._drawCallouts();

        // Маркер спутника под наблюдением (tracking) — DOM SVG.
        if (this.satellite.noradId && this.satellite.position) {
            this._drawSatellite();
        } else {
            this._positionDomMarker('map-sat-tracking', 'map-sat-tracking-label', null, '', 'tracking', null);
        }

        // Зона видимости и маркер выбранного спутника — DOM SVG (если не под наблюдением).
        if (this._selectedSatellite.noradId &&
            this._selectedSatellite.noradId !== this.satellite.noradId) {
            if (this.options.showFootprint && this._selectedSatellite.visibilityZone && this._selectedSatellite.visibilityZone.length > 0) {
                this._drawSelectedVisibilityZone();
            }
            if (this._selectedSatellite.position) {
                this._drawSelectedSatelliteIcon();
            }
        } else {
            this._positionDomMarker('map-sat-selected', 'map-sat-selected-label', null, '', 'selected', null);
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
            if (coords[i][1] >= 89.5) { hasNorthPole = true; }
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
                const edgeX = goingRightToLeft ? w : 0;
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
            ctx.fillStyle = this.colors.cityMarker;
            ctx.fill();

            ctx.font = 'bold 11px sans-serif';
            ctx.fillStyle = this.colors.cityLabel;
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
     * Отрисовка наземной трассы спутника под наблюдением.
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
            ctx.lineWidth = this._mapTrackLineWidth;
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

        // Отрисовка точек (минутные метки)
        if (mode === 'dots' || mode === 'both') {
            ctx.fillStyle = this.colors.orbitDots;
            let lastDotTime = -Infinity;

            for (let k = 0; k < points.length; k++) {
                const point = points[k];
                const t = point.ts !== null ? point.ts : point.time;
                if (t - lastDotTime >= dotInterval) {
                    const pp = this.project(point.lon, point.lat);
                    ctx.beginPath();
                    ctx.arc(pp.x, pp.y, Math.max(2.5, this._mapTrackLineWidth * 1.2), 0, Math.PI * 2);
                    ctx.fill();
                    lastDotTime = t;
                }
            }
        }
    };

    /**
     * Позиционирование DOM SVG-маркера поверх canvas.
     * Бум ∥ трассе: угол map_marker_rot_deg с бэкенда (SGP4) или запасной расчёт.
     * @param {string} elId — id контейнера (.map-sat-marker)
     * @param {string} labelId — id <span> подписи
     * @param {{lon:number,lat:number,map_marker_rot_deg?:number,map_marker_fwd_lon?:number,map_marker_fwd_lat?:number}|null} pos
     * @param {string} name — имя КА
     * @param {string} [markerKey] — 'tracking' | 'selected'
     * @param {number|null} [noradId] — смена КА сбрасывает предыдущую точку
     */
    EarthView.prototype._positionDomMarker = function(elId, labelId, pos, name, markerKey, noradId) {
        const el = document.getElementById(elId);
        if (!el) { return; }
        markerKey = markerKey || '_';
        if (!this._domMarkerState) {
            this._domMarkerState = {};
        }
        let st = this._domMarkerState[markerKey];
        if (!st) {
            st = {
                prevGeo: null,
                rotDeg: MAP_SAT_DEFAULT_ROT,
                noradId: null,
                orientReady: false
            };
            this._domMarkerState[markerKey] = st;
        }
        const flipEl = el.querySelector('.map-sat-flip');
        if (!pos) {
            el.style.display = 'none';
            el.style.left = '';
            el.style.top = '';
            st.prevGeo = null;
            st.rotDeg = MAP_SAT_DEFAULT_ROT;
            st.noradId = null;
            st.orientReady = false;
            if (flipEl) {
                flipEl.style.transform = 'rotate(' + MAP_SAT_DEFAULT_ROT + 'deg)';
            }
            return;
        }
        if (noradId !== null && st.noradId !== null && noradId !== st.noradId) {
            st.prevGeo = null;
            st.rotDeg = MAP_SAT_DEFAULT_ROT;
            st.orientReady = false;
        }
        st.noradId = noradId !== null ? noradId : st.noradId;

        const pw = this.width;
        const ph = this.height;
        if (pw <= 0 || ph <= 0) { return; }
        const p = this.project(pos.lon, pos.lat);

        const serverRot = (typeof pos.map_marker_rot_deg === 'number' && !isNaN(pos.map_marker_rot_deg))
            ? pos.map_marker_rot_deg
            : null;
        const fwdOk = typeof pos.map_marker_fwd_lon === 'number' && !isNaN(pos.map_marker_fwd_lon) &&
            typeof pos.map_marker_fwd_lat === 'number' && !isNaN(pos.map_marker_fwd_lat);

        let gotOrientation = false;
        if (fwdOk) {
            const p0 = this.project(pos.lon, pos.lat);
            const p1 = this.project(pos.map_marker_fwd_lon, pos.map_marker_fwd_lat);
            const ddx = p1.x - p0.x;
            if (Math.abs(ddx) > pw / 2) {
                if (serverRot !== null) {
                    st.rotDeg += _shortestRotDeltaDeg(st.rotDeg, serverRot);
                    gotOrientation = true;
                }
            } else {
                const hDeg = Math.atan2(p1.y - p0.y, ddx) * 180 / Math.PI;
                st.rotDeg += _shortestRotDeltaDeg(st.rotDeg, hDeg + MAP_SAT_SVG_OFFSET_DEG);
                gotOrientation = true;
            }
        } else if (serverRot !== null) {
            st.rotDeg += _shortestRotDeltaDeg(st.rotDeg, serverRot);
            gotOrientation = true;
        } else if (st.prevGeo) {
            let dLon = pos.lon - st.prevGeo.lon;
            if (dLon > 180) { dLon -= 360; }
            if (dLon < -180) { dLon += 360; }
            const dLat = pos.lat - st.prevGeo.lat;
            if (Math.abs(dLon) < 90 && (Math.abs(dLon) > 1e-6 || Math.abs(dLat) > 1e-6)) {
                const cosLat = Math.cos(pos.lat * Math.PI / 180);
                const screenDx = dLon * cosLat;
                const screenDy = -dLat;
                const headingDeg = Math.atan2(screenDy, screenDx) * 180 / Math.PI;
                st.rotDeg += _shortestRotDeltaDeg(st.rotDeg, headingDeg + MAP_SAT_SVG_OFFSET_DEG);
                gotOrientation = true;
            }
        }
        st.prevGeo = { lon: pos.lon, lat: pos.lat };

        const firstOrient = gotOrientation && !st.orientReady;
        if (gotOrientation) {
            st.orientReady = true;
        }

        if (flipEl) {
            if (firstOrient) {
                flipEl.style.transition = 'none';
                flipEl.style.transform = 'rotate(' + st.rotDeg + 'deg)';
                void flipEl.offsetHeight;
                flipEl.style.transition = '';
            } else {
                flipEl.style.transform = 'rotate(' + st.rotDeg + 'deg)';
            }
        }

        const pctX = (p.x / pw) * 100;
        const pctY = (p.y / ph) * 100;
        const h = MAP_SAT_MARKER_HALF_PX;
        el.style.left = 'calc(' + pctX + '% - ' + h + 'px)';
        el.style.top = 'calc(' + pctY + '% - ' + h + 'px)';
        el.style.display = st.orientReady ? 'block' : 'none';
        const lbl = document.getElementById(labelId);
        if (lbl) {
            lbl.textContent = name ? _shortName(name) : '';
        }
    };

    /**
     * Маркер спутника под наблюдением — DOM SVG (анимированный логотип).
     */
    EarthView.prototype._drawSatellite = function() {
        this._positionDomMarker('map-sat-tracking', 'map-sat-tracking-label',
            this.satellite.position, this.satellite.name, 'tracking', this.satellite.noradId);
    };

    // ========== Выноски (callouts) ==========

    /**
     * Сбор маркеров спутников для расчёта выносок.
     * Возвращает массив объектов { id, x, y, color, name } в physical-координатах canvas.
     * @private
     */
    EarthView.prototype._collectCalloutMarkers = function() {
        const dpr = window.devicePixelRatio || 1;
        const ctx = this.ctx;
        const sm = window._stateManager;
        // Какую карточку выделять: при раздельном selected/tracking на карте «герой» UI —
        // выбранный (оранжевая иконка, футпринт selected); иначе — спутник под наблюдением.
        let selSm = sm && typeof sm.getSelectedSatelliteId === 'function' ? sm.getSelectedSatelliteId() : null;
        let trkSm = sm && typeof sm.getTrackingSatelliteId === 'function' ? sm.getTrackingSatelliteId() : null;
        if (trkSm == null && this.satellite.noradId) { trkSm = this.satellite.noradId; }
        if (selSm == null && this._selectedSatellite.noradId) { selSm = this._selectedSatellite.noradId; }
        let highlightNorad = null;
        if (trkSm && selSm && String(selSm) !== String(trkSm)) {
            highlightNorad = selSm;
        } else if (trkSm) {
            highlightNorad = trkSm;
        } else if (selSm) {
            highlightNorad = selSm;
        }
        const idEq = function(a, b) {
            if (a == null || b == null) { return false; }
            return String(a) === String(b);
        };
        const isHighlight = function(nid) { return highlightNorad != null && idEq(nid, highlightNorad); };
        // Радиусы «видимой части» иконок маркеров для сбора препятствий.
        // tracking/selected — DOM SVG-якорь 56×56, реальная иконка ≈36×36 → r=18.
        // secondary — «киношный» силуэт ~38×19 logical (корпус+бумы+панели+антенна),
        // полуширина 19 + 1 буфер → r=20.
        const ICON_R_MAIN = 18 * dpr;
        const ICON_R_SECONDARY = 20 * dpr;
        // Подбор ширины карточки под содержимое (имя + #NORAD).
        // Возвращает значение в physical px (как и остальные размеры аллокатора).
        // Шрифты подобраны под .map-sat-callout: имя 12px bold, NORAD 10px medium.
        // monospace fallback близок по метрикам к --font-mono без чтения computed style.
        const measure = (name, nid) => {
            ctx.save();
            ctx.font = 'bold 12px monospace';
            const wName = ctx.measureText(name || '').width;
            ctx.font = '500 10px monospace';
            const wNorad = ctx.measureText('#' + nid).width;
            ctx.restore();
            // Горизонтальные паддинги карточки (4+8+11) + 2px бордер ≈ 22 logical px.
            // Прибавляем небольшой буфер на разницу шрифтов и округление.
            const innerPad = 24 * dpr;
            const target = Math.max(wName, wNorad) + innerPad;
            const minW = 70 * dpr;
            const maxW = 160 * dpr;
            return Math.round(Math.max(minW, Math.min(maxW, target)));
        };

        const markers = [];
        const trkId = this.satellite.noradId;
        if (trkId && this.satellite.position) {
            const p = this.project(this.satellite.position.lon, this.satellite.position.lat);
            const name = this.satellite.name || '';
            markers.push({
                id: trkId,
                x: p.x,
                y: p.y,
                color: this.colors.satLabel || '#ffeb3b',
                name: name,
                cardWidth: measure(name, trkId),
                iconRadius: ICON_R_MAIN,
                isTracked: isHighlight(trkId),
            });
        }
        const selId = this._selectedSatellite.noradId;
        if (selId && selId !== trkId && this._selectedSatellite.position) {
            const p = this.project(this._selectedSatellite.position.lon, this._selectedSatellite.position.lat);
            const name = this._selectedSatellite.name || '';
            markers.push({
                id: selId,
                x: p.x,
                y: p.y,
                color: this.colors.mapIconSelectedFill || '#ffb347',
                name: name,
                cardWidth: measure(name, selId),
                iconRadius: ICON_R_MAIN,
                isTracked: isHighlight(selId),
            });
        }
        const ids = Object.keys(this._secondarySatellites);
        for (let i = 0; i < ids.length; i++) {
            const sat = this._secondarySatellites[ids[i]];
            const nid = parseInt(ids[i], 10);
            if (nid === trkId || nid === selId) { continue; }
            if (!sat.position) { continue; }
            const p = this.project(sat.position.lon, sat.position.lat);
            const fallback = SECONDARY_SAT_COLORS[i % SECONDARY_SAT_COLORS.length];
            const c = sm ? (sm.getMarkerColor(nid) || fallback) : fallback;
            const name = sat.name || '';
            markers.push({
                id: nid,
                x: p.x,
                y: p.y,
                color: c,
                name: name,
                cardWidth: measure(name, nid),
                iconRadius: ICON_R_SECONDARY,
                isTracked: isHighlight(nid),
            });
        }
        return markers;
    };

    /**
     * Препятствия для CalloutLayout: bbox-ы городов и точки наблюдения.
     * Координаты в physical px.
     * @private
     */
    EarthView.prototype._collectCalloutObstacles = function(markers) {
        const dpr = window.devicePixelRatio || 1;
        const obstacles = [];
        // Подписи городов: точка ~3px + текст справа ~80x14 logical
        for (let i = 0; i < this.cities.length; i++) {
            const c = this.cities[i];
            const p = this.project(c.lon, c.lat);
            obstacles.push({
                x: p.x - 4 * dpr,
                y: p.y - 8 * dpr,
                w: 90 * dpr,
                h: 16 * dpr,
            });
        }
        if (this.observer) {
            const p = this.project(this.observer.lon, this.observer.lat);
            obstacles.push({
                x: p.x - 10 * dpr,
                y: p.y - 12 * dpr,
                // Треугольник + подпись справа
                w: 100 * dpr,
                h: 24 * dpr,
            });
        }
        // Иконки спутников — препятствия для карточек чужих выносок.
        // Геометрия (stem ≥ 79px по Y, tail 24px по X) гарантирует, что
        // карточка собственной выноски никогда не попадёт в свой же bbox,
        // поэтому добавляем все маркеры без исключения.
        if (markers && markers.length) {
            for (let i = 0; i < markers.length; i++) {
                const m = markers[i];
                const r = m.iconRadius;
                if (typeof r !== 'number' || !isFinite(r) || r <= 0) { continue; }
                obstacles.push({
                    x: m.x - r,
                    y: m.y - r,
                    w: 2 * r,
                    h: 2 * r,
                });
            }
        }
        return obstacles;
    };

    /**
     * Сегменты «запретных» трасс для пост-прохода CalloutLayout (ring-режим).
     *
     * Возвращает массив `{x1, y1, x2, y2}` в physical px:
     *   1. Трасса selected (оранжевая) — `_selectedSatellite.groundTrack`.
     *   2. Трасса tracking (синяя) — `this.satellite.groundTrack` (если она
     *      отличается от selected, т.е. когда КА выбран и одновременно ведётся).
     *
     * Поддерживаем оба формата: массив точек `[{lon,lat,...}]` или объект
     * `{past:[[seg],...], future:[[seg],...]}`. Антимеридиан рвёт полилинию на
     * сегменты — пропускаем «прыжки» больше width/2 (так же, как в `_drawTrackSegment`).
     *
     * Вторичные пунктиры из `TRACK_COLOR_PALETTE` сюда НЕ включаются:
     * иначе плотный кадр блокирует размещение карточек по всему кольцу.
     *
     * @private
     */
    EarthView.prototype._collectForbiddenSegments = function() {
        const out = [];
        const selSm = this._selectedSatellite ? this._selectedSatellite.noradId : null;
        const trkSm = this.satellite ? this.satellite.noradId : null;
        // selected (оранжевая) — главный «герой» кадра.
        if (selSm) {
            this._appendTrackSegments(out, this._selectedSatellite.groundTrack);
        }
        // tracking (синяя) — добавляем только если он отличен от selected,
        // иначе тот же набор сегментов посчитался бы дважды.
        if (trkSm && (!selSm || String(trkSm) !== String(selSm))) {
            this._appendTrackSegments(out, this.satellite.groundTrack);
        }
        return out;
    };

    /**
     * Преобразует наземную трассу (массив или {past, future}) в плоский
     * список отрезков `{x1,y1,x2,y2}` в physical px. Разрывы по антимеридиану
     * (|Δx| > width/2) превращаются в границы сегментов.
     *
     * @param {Array} out — массив-аккумулятор для добавления отрезков.
     * @param {Array|Object|null} track — наземная трасса в исходном формате.
     * @private
     */
    EarthView.prototype._appendTrackSegments = function(out, track) {
        if (!track) { return; }
        const polylines = [];
        if (Array.isArray(track)) {
            polylines.push(track);
        } else if (typeof track === 'object') {
            if (Array.isArray(track.past))   { Array.prototype.push.apply(polylines, track.past); }
            if (Array.isArray(track.future)) { Array.prototype.push.apply(polylines, track.future); }
        }
        const halfW = this.width / 2;
        for (let i = 0; i < polylines.length; i++) {
            const poly = polylines[i];
            if (!poly || poly.length < 2) { continue; }
            let prev = null;
            for (let k = 0; k < poly.length; k++) {
                const pt = poly[k];
                if (!pt || typeof pt.lon !== 'number' || typeof pt.lat !== 'number') {
                    prev = null;
                    continue;
                }
                const p = this.project(pt.lon, pt.lat);
                if (prev && Math.abs(p.x - prev.x) <= halfW) {
                    out.push({ x1: prev.x, y1: prev.y, x2: p.x, y2: p.y });
                }
                prev = p;
            }
        }
    };

    /**
     * Главный слой выносок: layout → линии на canvas → DOM-карточки.
     * @private
     */
    EarthView.prototype._drawCallouts = function() {
        if (!this._calloutLayout || !this._calloutRenderer) { return; }
        const markers = this._collectCalloutMarkers();
        // Поддерживаем кеш чистым: убираем закешированные id, которых уже нет
        const ids = [];
        for (let i = 0; i < markers.length; i++) { ids.push(markers[i].id); }
        this._calloutLayout.prune(ids);

        if (markers.length === 0) {
            this._calloutRenderer.clear();
            return;
        }
        const obstacles = this._collectCalloutObstacles(markers);
        const bounds = { width: this.width, height: this.height };
        // Запретные сегменты трасс selected (оранжевая) + tracking (синяя):
        // карточки не должны их пересекать — это «герой»-трассы кадра.
        const forbiddenSegments = this._collectForbiddenSegments();
        const layouts = this._calloutLayout.layout(markers, obstacles, bounds, forbiddenSegments);
        const dpr = window.devicePixelRatio || 1;
        this._calloutRenderer.drawLines(this.ctx, layouts, dpr);
        const info = {};
        for (let j = 0; j < markers.length; j++) {
            const mid = markers[j].id;
            info[mid] = {
                name: markers[j].name,
                norad: mid,
                tracked: !!markers[j].isTracked,
            };
        }
        this._calloutRenderer.update(layouts, bounds, info);
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

        // Название точки наблюдения — обводка из темы (светлая тема: светлый ореол)
        if (this.observer.name) {
            const obsText = this.observer.name.toLocaleUpperCase();
            ctx.font = 'bold 12px sans-serif';
            ctx.textAlign = 'left';
            ctx.textBaseline = 'middle';
            const labelX = p.x + size + 3;
            ctx.strokeStyle = this.colors.observerLabelStroke || 'rgba(0,0,0,0.9)';
            ctx.lineWidth = 2;
            ctx.strokeText(obsText, labelX, p.y);
            ctx.fillStyle = this.colors.cityLabel || '#ffffff';
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
            ctx.lineWidth = this._mapFootprintLineWidth * dpr;
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
        this._initCallouts();
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
     * Инициализация подсистемы выносок (CalloutLayout + CalloutRenderer).
     * Layout рассчитывает геометрию в физических пикселях canvas
     * (поэтому размеры умножаются на dpr); Renderer отрисовывает линии на canvas
     * и поддерживает пул DOM-карточек .map-sat-callout.
     */
    EarthView.prototype._initCallouts = function() {
        if (typeof window.CalloutLayout !== 'function' ||
            typeof window.CalloutRenderer !== 'function') {
            return;
        }
        const dpr = window.devicePixelRatio || 1;
        // Размеры в physical px (canvas-координаты)
        this._calloutLayout = new window.CalloutLayout({
            stemLength:    80 * dpr,
            tailLength:    24 * dpr,
            cardWidth:    140 * dpr,
            cardHeight:    36 * dpr,
            minCardGap:     6 * dpr,
            boundsPadding:  8 * dpr,
            // Размещение карточек на расширенном PCA-эллипсе всех КА в кадре:
            // иконки и трассы остаются неперекрытыми, карточки уходят в стороны.
            // Единый эллипс на все маркеры — даже если tracked немного оторван
            // от плотной группы, он гарантированно остаётся внутри кольца.
            groupingMode: 'ring',
            ringGap:       70 * dpr,
            clusterDistance: Number.POSITIVE_INFINITY,
            // Зазор от запретных трасс (selected оранжевая, tracking синяя):
            // карточки уводятся от линии, чтобы не «прилипать» к ней визуально.
            forbiddenPadding: 5 * dpr,
        });
        const container = document.getElementById('map-callouts');
        if (container) {
            this._calloutRenderer = new window.CalloutRenderer(container, {
                lineWidth: 1.5,
                bendDotRadius: 2.5,
                fallbackColor: this.colors.satLabel || '#ffeb3b',
            });
        } else {
            this._calloutRenderer = null;
        }
    };

    /**
     * Установка позиции спутника
     * @param {number} lon - Долгота
     * @param {number} lat - Широта
     * @param {number} alt - Высота (км)
     */
    /**
     * @param {number} lon
     * @param {number} lat
     * @param {number} alt
     * @param {Object} [meta] — поля map_marker_rot_deg, map_marker_fwd_lon, map_marker_fwd_lat с state.position
     */
    EarthView.prototype.setSatellitePosition = function(lon, lat, alt, meta) {
        const p = { lon: lon, lat: lat, alt: alt || 0 };
        if (meta && typeof meta === 'object') {
            if (typeof meta.map_marker_rot_deg === 'number' && !isNaN(meta.map_marker_rot_deg)) {
                p.map_marker_rot_deg = meta.map_marker_rot_deg;
            }
            if (typeof meta.map_marker_fwd_lon === 'number' && !isNaN(meta.map_marker_fwd_lon) &&
                typeof meta.map_marker_fwd_lat === 'number' && !isNaN(meta.map_marker_fwd_lat)) {
                p.map_marker_fwd_lon = meta.map_marker_fwd_lon;
                p.map_marker_fwd_lat = meta.map_marker_fwd_lat;
            }
        }
        this.satellite.position = p;
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

    /** Полная очистка слоя наблюдения (когда наблюдения нет). */
    EarthView.prototype.clearTrackingLayer = function() {
        this.satellite.position = null;
        this.satellite.name = '';
        this.satellite.noradId = null;
        this.satellite.groundTrack = [];
        this.satellite.visibilityZone = null;
        this._positionDomMarker('map-sat-tracking', 'map-sat-tracking-label', null, '', 'tracking', null);
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

    EarthView.prototype.setSelectedSatellitePosition = function(lon, lat, alt, meta) {
        const p = { lon: lon, lat: lat, alt: alt || 0 };
        if (meta && typeof meta === 'object') {
            if (typeof meta.map_marker_rot_deg === 'number' && !isNaN(meta.map_marker_rot_deg)) {
                p.map_marker_rot_deg = meta.map_marker_rot_deg;
            }
            if (typeof meta.map_marker_fwd_lon === 'number' && !isNaN(meta.map_marker_fwd_lon) &&
                typeof meta.map_marker_fwd_lat === 'number' && !isNaN(meta.map_marker_fwd_lat)) {
                p.map_marker_fwd_lon = meta.map_marker_fwd_lon;
                p.map_marker_fwd_lat = meta.map_marker_fwd_lat;
            }
        }
        this._selectedSatellite.position = p;
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
        this._positionDomMarker('map-sat-selected', 'map-sat-selected-label', null, '', 'selected', null);
    };

    /**
     * Отрисовка слоя текущего (выбранного) спутника: сплошная жёлтая линия трассы (без пунктира).
     * @private
     */
    EarthView.prototype._drawSelectedLayer = function() {
        const sel = this._selectedSatellite;
        const track = sel.groundTrack;
        if (!track) { return; }

        const ctx = this.ctx;
        const color = this.colors.selectedTrack;
        const dpr = window.devicePixelRatio || 1;

        ctx.strokeStyle = color;
        ctx.lineWidth = 2 * dpr;
        ctx.setLineDash([]);

        let segments = [];
        if (track && typeof track === 'object' && !Array.isArray(track)) {
            if (Array.isArray(track.past)) { segments = segments.concat(track.past); }
            if (Array.isArray(track.future)) { segments = segments.concat(track.future); }
        } else if (Array.isArray(track)) {
            segments = [track];
        }

        for (let s = 0; s < segments.length; s++) {
            const seg = segments[s];
            if (!seg || seg.length < 2) { continue; }
            ctx.beginPath();
            let prevP = null;
            let moved = false;
            for (let k = 0; k < seg.length; k++) {
                const pt = this.project(seg[k].lon, seg[k].lat);
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
        const segments = this._selectedSatellite.visibilityZone;
        if (!segments || segments.length === 0) { return; }

        const ctx = this.ctx;
        const dpr = window.devicePixelRatio || 1;
        const fillColor = this.colors.selectedFootprintFill || 'rgba(93, 173, 226, 0.12)';
        const strokeColor = this.colors.selectedFootprint || 'rgba(93, 173, 226, 0.6)';

        for (let k = 0; k < segments.length; k++) {
            const seg = segments[k];
            if (!seg || seg.length < 3) { continue; }
            const projected = [];
            for (let i = 0; i < seg.length; i++) {
                projected.push(this.project(seg[i].lon, seg[i].lat));
            }
            ctx.beginPath();
            ctx.moveTo(projected[0].x, projected[0].y);
            for (let j = 1; j < projected.length; j++) {
                ctx.lineTo(projected[j].x, projected[j].y);
            }
            ctx.closePath();
            ctx.fillStyle = fillColor;
            ctx.fill();
            ctx.strokeStyle = strokeColor;
            ctx.lineWidth = this._mapFootprintLineWidth * dpr;
            ctx.stroke();
        }
    };

    /**
     * Полноценная иконка выбранного спутника (аналогично слою наблюдения, цвет — оранжевый).
     * @private
     */
    EarthView.prototype._drawSelectedSatelliteIcon = function() {
        this._positionDomMarker('map-sat-selected', 'map-sat-selected-label',
            this._selectedSatellite.position, this._selectedSatellite.name, 'selected', this._selectedSatellite.noradId);
    };

    // ========== Вторичные спутники ==========

    /**
     * Обновление позиций вторичных спутников группы.
     * Вызывается из app.js при каждом position-апдейте.
     *
     * @param {Array} satArray — массив {noradId, name, lon, lat, alt}.
     */
    EarthView.prototype.setSecondaryPositions = function(satArray) {
        if (!satArray) { return; }
        for (let i = 0; i < satArray.length; i++) {
            const s = satArray[i];
            if (!s || !s.noradId) { continue; }
            if (!this._secondarySatellites[s.noradId]) {
                this._secondarySatellites[s.noradId] = { noradId: s.noradId, name: s.name || '', track: null };
            }
            this._secondarySatellites[s.noradId].position = { lon: s.lon, lat: s.lat, alt: s.alt || 0 };
            if (s.name) { this._secondarySatellites[s.noradId].name = s.name; }
        }
        // Удаляем спутники, которые больше не в группе.
        const activeIds = {};
        for (let j = 0; j < satArray.length; j++) {
            if (satArray[j] && satArray[j].noradId) { activeIds[satArray[j].noradId] = true; }
        }
        for (const id in this._secondarySatellites) {
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
        const ids = Object.keys(this._secondarySatellites);
        const sm = window._stateManager;
        for (let i = 0; i < ids.length; i++) {
            const sat = this._secondarySatellites[ids[i]];
            const nid = parseInt(ids[i], 10);
            const trackVisible = sm && sm.isTrackVisible(nid);
            const markerColor = sm ? sm.getMarkerColor(nid) : null;
            if (sat.track && trackVisible) {
                const trackColor = sm ? sm.getTrackColor(nid) : null;
                this._drawSecondaryGroundTrack(sat, i, trackColor);
            }
            if (sat.position) {
                this._drawSecondaryMarker(sat, i, markerColor);
            }
        }
    };

    /**
     * Отрисовка пунктирной трассы вторичного спутника.
     * Вызывается только из _drawSecondaryLayer при sat.track && isTrackVisible(noradId) —
     * пока трасса в таблице не включена (⊙), линия на карте не рисуется.
     * @private
     */
    EarthView.prototype._drawSecondaryGroundTrack = function(sat, colorIdx, paletteColor) {
        const ctx = this.ctx;
        const track = sat.track;
        if (!track) { return; }

        const color = paletteColor || cssVar('--map-secondary-track-fallback', 'rgba(200, 220, 235, 0.9)');
        const dpr = window.devicePixelRatio || 1;
        const isLight = typeof getThemeId === 'function' && getThemeId() === 'light';

        /* Светлая тема: тоньше пунктир, больше «воздуха» — меньше шума на карте */
        ctx.setLineDash(isLight ? [4, 7] : [5, 5]);
        ctx.strokeStyle = color;
        const lw = isLight
            ? Math.max(1, this._mapTrackLineWidth * 0.62)
            : Math.max(1.5, this._mapTrackLineWidth);
        ctx.lineWidth = lw * dpr;

        let segments = [];
        if (Array.isArray(track.future)) { segments = segments.concat(track.future); }
        if (Array.isArray(track.past)) { segments = segments.concat(track.past); }

        for (let s = 0; s < segments.length; s++) {
            const seg = segments[s];
            if (!seg || seg.length < 2) { continue; }
            ctx.beginPath();
            const first = this.project(seg[0].lon, seg[0].lat);
            ctx.moveTo(first.x, first.y);
            for (let k = 1; k < seg.length; k++) {
                const pt = this.project(seg[k].lon, seg[k].lat);
                ctx.lineTo(pt.x, pt.y);
            }
            ctx.stroke();
        }
        ctx.setLineDash([]);
    };

    /**
     * Отрисовка маркера вторичного спутника — «киношный» силуэт.
     * Композиция (logical px): корпус 9×8 + 2 бума 1.5×1 + 2 панели 6×10 (с центральной
     * перемычкой-гридом) + штырь-антенна 1×2 + точка-приёмник r=1. Габарит ≈24×12.
     * Цвет КА несут корпус (alpha 1) и панели (alpha 0.7) — различение по цвету и подписи
     * в callout-карточке. Tracked/selected рисуются как отдельные крупные SVG-иконки.
     * @private
     */
    EarthView.prototype._drawSecondaryMarker = function(sat, colorIdx, markerColor) {
        const ctx = this.ctx;
        const pos = sat.position;
        if (!pos) { return; }

        const p = this.project(pos.lon, pos.lat);
        const dpr = window.devicePixelRatio || 1;
        const isLight = typeof getThemeId === 'function' && getThemeId() === 'light';
        const color = markerColor || SECONDARY_SAT_COLORS[colorIdx % SECONDARY_SAT_COLORS.length];

        // Габариты силуэта в logical px (умножаются на dpr внутри).
        // Габарит ≈38×19 — явно различимый на карте, не конкурирует с tracked-иконкой (56×56).
        const BODY_W   = 14;
        const BODY_H   = 12;
        const BOOM_W   = 2;
        const BOOM_H   = 1.5;
        const PANEL_W  = 10;
        const PANEL_H  = 14;
        const ANT_H    = 3;
        const ANT_DOT  = 1.5;
        const CORNER_R = 2;

        const cx = p.x;
        const cy = p.y;

        const bodyHalfW = BODY_W / 2 * dpr;
        const bodyHalfH = BODY_H / 2 * dpr;
        const boomW = BOOM_W * dpr;
        const boomH = BOOM_H * dpr;
        const panelW = PANEL_W * dpr;
        const panelH = PANEL_H * dpr;
        const antH = ANT_H * dpr;
        const antDot = ANT_DOT * dpr;
        const cornerR = CORNER_R * dpr;

        // Контур — нейтрально-тёмный, halo — мягкая тень (а не неоновое свечение).
        // На light оставляем чуть «синь», но без насыщенности; на dark — почти чёрный
        // тёплый графит, чтобы не сливалось с водой и не било в глаза неоном.
        const stroke = isLight ? 'rgba(36, 44, 56, 0.78)' : 'rgba(20, 26, 34, 0.85)';
        const halo   = isLight ? 'rgba(0, 0, 0, 0.18)'    : 'rgba(0, 0, 0, 0.45)';
        const lwHair  = isLight ? Math.max(1, dpr)          : Math.max(1.1, 1.2 * dpr);
        const lwPanel = isLight ? Math.max(1.2, 1.3 * dpr)  : Math.max(1.4, 1.6 * dpr);
        const lwBody  = isLight ? Math.max(1.4, 1.5 * dpr)  : Math.max(1.6, 1.8 * dpr);

        ctx.save();
        ctx.lineCap = 'butt';
        ctx.lineJoin = 'miter';
        // Halo = мягкая тень: даёт «массу» иконке, но без свечения и неона.
        ctx.shadowColor = halo;
        ctx.shadowBlur = 2.5 * dpr;
        ctx.shadowOffsetX = 0;
        ctx.shadowOffsetY = 1 * dpr;

        // ─── Антенна (штырь + точка) над корпусом ────────────────────
        ctx.strokeStyle = stroke;
        ctx.fillStyle = color;
        ctx.lineWidth = lwHair;
        const antTop = cy - bodyHalfH - antH;
        ctx.beginPath();
        ctx.moveTo(cx, cy - bodyHalfH);
        ctx.lineTo(cx, antTop);
        ctx.stroke();
        ctx.beginPath();
        ctx.arc(cx, antTop - antDot, antDot, 0, Math.PI * 2);
        ctx.fill();
        ctx.stroke();

        // ─── Бумы (короткие перемычки от корпуса к панелям) ─────────
        ctx.fillStyle = stroke;
        const boomY = cy - boomH / 2;
        ctx.fillRect(cx - bodyHalfW - boomW, boomY, boomW, boomH);
        ctx.fillRect(cx + bodyHalfW,         boomY, boomW, boomH);

        // ─── Солнечные панели (заливка цветом КА, чуть полупрозрачно) ───
        ctx.globalAlpha = 0.82;
        ctx.fillStyle = color;
        ctx.strokeStyle = stroke;
        ctx.lineWidth = lwPanel;
        const panelY = cy - panelH / 2;
        const leftX  = cx - bodyHalfW - boomW - panelW;
        const rightX = cx + bodyHalfW + boomW;
        ctx.beginPath();
        ctx.rect(leftX, panelY, panelW, panelH);
        ctx.fill();
        ctx.stroke();
        ctx.beginPath();
        ctx.rect(rightX, panelY, panelW, panelH);
        ctx.fill();
        ctx.stroke();

        // Грид-линии на панелях (1 вертикальная + 1 горизонтальная) — без тени.
        ctx.shadowColor = 'transparent';
        ctx.globalAlpha = 0.65;
        ctx.lineWidth = lwHair;
        ctx.beginPath();
        // Вертикальные посередине
        ctx.moveTo(leftX + panelW / 2, panelY);
        ctx.lineTo(leftX + panelW / 2, panelY + panelH);
        ctx.moveTo(rightX + panelW / 2, panelY);
        ctx.lineTo(rightX + panelW / 2, panelY + panelH);
        // Горизонтальные посередине — добавляют ощущение «солнечных ячеек»
        ctx.moveTo(leftX, panelY + panelH / 2);
        ctx.lineTo(leftX + panelW, panelY + panelH / 2);
        ctx.moveTo(rightX, panelY + panelH / 2);
        ctx.lineTo(rightX + panelW, panelY + panelH / 2);
        ctx.stroke();

        // ─── Корпус (поверх всего, full alpha, скруглён) ────────────
        ctx.globalAlpha = 1;
        ctx.shadowColor = halo;
        ctx.shadowBlur = 2.5 * dpr;
        ctx.shadowOffsetY = 1 * dpr;
        ctx.fillStyle = color;
        ctx.strokeStyle = stroke;
        ctx.lineWidth = lwBody;
        const bx = cx - bodyHalfW;
        const by = cy - bodyHalfH;
        const bw = bodyHalfW * 2;
        const bh = bodyHalfH * 2;
        ctx.beginPath();
        if (typeof ctx.roundRect === 'function') {
            ctx.roundRect(bx, by, bw, bh, cornerR);
        } else {
            ctx.rect(bx, by, bw, bh);
        }
        ctx.fill();
        ctx.stroke();

        ctx.restore();
        // Имя вторичного КА рисуется через CalloutRenderer; canvas-текст под маркером удалён.
    };

    // Экспорт
    window.EarthView = EarthView;

})();
