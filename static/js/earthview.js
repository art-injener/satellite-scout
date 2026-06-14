// EarthView - Компонент отображения карты мира с орбитами спутников
// Стиль вдохновлён программой STSPLUS (1989-2006)

(function() {
    'use strict';

    /** Половина стороны DOM-маркера (px) — совпадает с .map-sat-marker в main.css */
    const MAP_SAT_MARKER_HALF_PX = 28;

    /**
     * Дискретные уровни масштабирования карты.
     * 4 ступени под границы детализации векторных данных (50m / 10m).
     */
    const MAP_ZOOM_LEVELS = [1.0, 1.5, 2.5, 4.0];

    /**
     * Длительность phased-анимации по умолчанию (мс).
     * При смене уровня zoom карта «перерисовывается заново» — на этом интервале
     * береговые линии и границы РФ ведутся «карандашом» сегмент за сегментом.
     */
    const DEFAULT_ZOOM_ANIM_MS = 1500;

    /**
     * Поворот маркера с бэкенда (map_marker_rot_deg) — предпочтительно.
     * Запасной вариант: локальный расчёт с учётом внутреннего rotate(45) в SVG.
     */
    const MAP_SAT_SVG_OFFSET_DEG = -45;
    const MAP_SAT_DEFAULT_ROT = 0;

    /**
     * Порог «прыжка по долготе» внутри одного сегмента трассы (в градусах).
     * При observerLon ≠ 0 пиксельная защита `|Δx| > thresh` не срабатывает для
     * пары точек, разделённых большим Δlon, но лежащих по одну сторону от
     * антимеридиана окна (например, lon=-120° и lon=+30° при observerLon=+40°
     * дают |Δx|≈427 < threshold=512 — линия тянется через половину canvas).
     * Дополнительная проверка |Δlon| > порог разрывает такие «палки».
     * Нормальный шаг SGP4 (30 с) для LEO даёт Δlon ≈ 3.7°, для GEO ≈ 0°,
     * polar — также ≤ 5°. 30° — заведомо больше любого нормального соседства.
     */
    const TRACK_LON_JUMP_DEG = 30;

    /**
     * Порог «прыжка по широте» внутри одного сегмента трассы (в градусах).
     * Физически за 30 с (шаг SGP4) КА не может изменить широту больше чем
     * на ~5° (LEO ~6.5°/мин по великому кругу). 15° — порог с запасом, но
     * заведомо меньше любых артефактов «диагональной палки» (≥ 25°). Ловит
     * случаи, когда backend передаёт точки с прыжком через полюс, у которых
     * Δlon мал из-за raw-lon SGP4 и Δx проекции тоже мал.
     */
    const TRACK_LAT_JUMP_DEG = 15;

    // Палитра вторичных спутников без включённой трассы: крупные маркеры + высокая яркость
    // (почти белый / ледяной / мягкий акцент), чтобы не терялись на тёмной карте.
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
            trackDotInterval: 60000, // Интервал точек в мс (1 минута)
            // Стиль анимации zoom:
            //   'phased'  — послойная векторная перерисовка с «карандашной» прорисовкой контуров (по умолчанию);
            //   'instant' — без анимации, мгновенный переход.
            animStyle: 'phased',
            // Длительность phased-анимации (мс): за это время карта целиком прорисовывается заново.
            zoomAnimDurationMs: DEFAULT_ZOOM_ANIM_MS
        }, options || {});

        this._reloadColorsFromCss();

        // Данные границ РФ (GeoJSON)
        this.russiaData = null;

        // Данные береговых линий (GeoJSON)
        this.coastlineData = null;

        // Данные полигонов суши (GeoJSON) — для заливки материков
        this.landData = null;


        // Состояние карты: центр всегда совпадает с наблюдателем после setObserver();
        // до этого — (0,0). Pan не предусмотрен, центр меняется только через resetView/setObserver.
        this.center = { lon: 0, lat: 0 };
        this._zoomIdx = 0;
        this.zoom = MAP_ZOOM_LEVELS[this._zoomIdx];
        // Состояние phased-анимации: { startTs, fromIdx, toIdx, raf } или null.
        this._zoomAnim = null;
        // Coalesce draw(): не чаще одного _drawStatic за кадр (POSITION SSE может приходить пачкой).
        this._drawCoalesceRaf = 0;

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

        // Колбэк выбора спутника по клику на карточку выноски (selected).
        // По умолчанию no-op; внешний код переопределяет через
        // `earthView.onSatelliteClick = (noradId) => {...}`.
        this.onSatelliteClick = function(_noradId) {};

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
     * Эффективная широта центра проекции с учётом зажима (clamp).
     *
     * При zoom=1 карта всегда «в полный globe» по Y — центр Y фиксирован на экваторе.
     * При zoom>1 центр Y берётся из наблюдателя (this.center.lat), но зажимается так,
     * чтобы карта целиком закрывала canvas по высоте: |cy| ≤ 90·(1 − 1/zoom).
     * Это убирает пустые поля сверху/снизу, когда наблюдатель близко к полюсу.
     */
    EarthView.prototype._effectiveCenterLat = function() {
        const z = this.zoom || 1;
        if (z <= 1) { return 0; }
        const maxAbs = 90 * (1 - 1 / z);
        const cy = (this.center && typeof this.center.lat === 'number') ? this.center.lat : 0;
        if (cy > maxAbs) { return maxAbs; }
        if (cy < -maxAbs) { return -maxAbs; }
        return cy;
    };

    /**
     * Преобразование географических координат в координаты canvas
     * Equirectangular (Plate Carrée) проекция
     * @param {number} lon - Долгота (-180 до 180)
     * @param {number} lat - Широта (-90 до 90)
     * @returns {Object} {x, y} координаты на canvas
     */
    EarthView.prototype.project = function(lon, lat) {
        // Δlon относительно центра, нормализуем в [-180, 180)
        let dLon = lon - this.center.lon;
        while (dLon >= 180) { dLon -= 360; }
        while (dLon < -180) { dLon += 360; }

        const z = this.zoom || 1;
        const x = (dLon / 360) * this.width * z + this.width / 2;

        // По Y: на zoom=1 — весь мир (lat ∈ [-90, 90] → y ∈ [0, height]) с центром
        // на экваторе. На zoom>1 — Y масштабируется так же, как X (сохраняем пропорции
        // карты), и центрируется на широте наблюдателя c зажимом, чтобы карта всегда
        // полностью закрывала canvas по высоте.
        const cy = this._effectiveCenterLat();
        const y = (this.height / 2) - ((lat - cy) / 90) * (this.height / 2) * z;

        return { x: x, y: y };
    };

    /**
     * Обратное преобразование - из координат canvas в географические
     * @param {number} x - X координата на canvas
     * @param {number} y - Y координата на canvas
     * @returns {Object} {lon, lat}
     */
    EarthView.prototype.unproject = function(x, y) {
        const z = this.zoom || 1;
        const dLon = ((x - this.width / 2) / (this.width * z)) * 360;
        let lon = this.center.lon + dLon;
        while (lon >= 180) { lon -= 360; }
        while (lon < -180) { lon += 360; }
        const cy = this._effectiveCenterLat();
        const lat = cy + ((this.height / 2 - y) / (this.height / 2 * z)) * 90;
        return { lon: lon, lat: lat };
    };

    /**
     * Порог для детекции «прыжка» через антимеридиан в полилиниях.
     * При zoom>1 видимая ширина карты в пикселях растёт пропорционально zoom,
     * поэтому фиксированный this.width/2 даёт ложные разрывы.
     * @returns {number} половина «полной ширины» проекции в пикселях.
     */
    EarthView.prototype._antimeridianThreshold = function() {
        return (this.width * (this.zoom || 1)) / 2;
    };

    /**
     * Точка в видимой области canvas (с допуском padding для plotted-объектов
     * за краями, например, иконок, центр которых вне viewport, но часть видна).
     * @param {{x:number, y:number}} p — экранные координаты в physical px.
     * @param {number} [padding=0] — допуск в physical px.
     * @returns {boolean}
     */
    EarthView.prototype._isInViewport = function(p, padding) {
        if (!p) { return false; }
        const pad = padding || 0;
        return p.x >= -pad && p.x <= this.width + pad &&
               p.y >= -pad && p.y <= this.height + pad;
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
     * Главная функция отрисовки.
     * Если активна phased-анимация zoom — делегирует _drawPhased();
     * иначе вызывает «статическое ядро» _drawStatic().
     */
    EarthView.prototype.draw = function() {
        if (this._zoomAnim) {
            this._drawPhased(this._now());
            return;
        }
        if (this._drawCoalesceRaf) { return; }
        const self = this;
        if (typeof requestAnimationFrame !== 'undefined') {
            this._drawCoalesceRaf = requestAnimationFrame(function() {
                self._drawCoalesceRaf = 0;
                self._drawStatic();
            });
        } else {
            this._drawStatic();
        }
    };

    /** Синхронная отрисовка без coalesce (resize, завершение phased-zoom). */
    EarthView.prototype.drawNow = function() {
        if (this._drawCoalesceRaf && typeof cancelAnimationFrame !== 'undefined') {
            cancelAnimationFrame(this._drawCoalesceRaf);
            this._drawCoalesceRaf = 0;
        }
        if (this._zoomAnim) {
            this._drawPhased(this._now());
            return;
        }
        this._drawStatic();
    };

    /**
     * «Статическое ядро» отрисовки — все слои за один проход без защиты от
     * phased-анимации. Используется как из публичной draw(), так и из
     * _drawPhased на завершающих стадиях, чтобы корректно перепозиционировать
     * DOM-маркеры и выноски без рекурсии.
     * @private
     */
    EarthView.prototype._drawStatic = function() {
        const ctx = this.ctx;

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

        // Зоны радиовидимости — задний план (под трассами и маркерами), чтобы
        // прозрачная заливка не «гасила» иконки спутников и не сбивала чтение
        // трасс. Selected рисуется ПОСЛЕ tracking — оранжевая/синяя обводка
        // выбранного КА видна, даже если зоны перекрываются.
        if (this.satellite.noradId &&
            this.options.showFootprint &&
            this.satellite.visibilityZone && this.satellite.visibilityZone.length > 0) {
            this._drawVisibilityZone();
        }
        if (this._selectedSatellite.noradId &&
            this._selectedSatellite.noradId !== this.satellite.noradId &&
            this.options.showFootprint &&
            this._selectedSatellite.visibilityZone && this._selectedSatellite.visibilityZone.length > 0) {
            this._drawSelectedVisibilityZone();
        }

        // Слой 1: вторичные спутники (серые пунктиры).
        this._drawSecondaryLayer();

        // Слой 2: выбранный спутник (оранжевый, без точек).
        // Рисуется только если отличается от tracking (иначе tracking перекроет).
        if (this._selectedSatellite.noradId &&
            this._selectedSatellite.noradId !== this.satellite.noradId) {
            this._drawSelectedLayer();
        }

        // Слой 3: трасса спутника под наблюдением (red/green + dots).
        if (this.satellite.noradId && this._hasGroundTrack()) {
            this._drawGroundTrack();
        }

        // Выноски (линии на canvas + DOM-карточки).
        // Между трассами и SVG-маркерами: линии получаются под иконкой,
        // DOM-карточки — на отдельном слое поверх (z-index в CSS).
        this._drawCallouts();

        // Маркер спутника под наблюдением (tracking) — DOM SVG.
        if (this.satellite.noradId && this.satellite.position) {
            this._drawSatellite();
        } else {
            this._positionDomMarker('map-sat-tracking', 'map-sat-tracking-label', null, '', 'tracking', null);
        }

        // Маркер выбранного спутника — DOM SVG (если не под наблюдением).
        if (this._selectedSatellite.noradId &&
            this._selectedSatellite.noradId !== this.satellite.noradId) {
            if (this._selectedSatellite.position) {
                this._drawSelectedSatelliteIcon();
            }
        } else {
            this._positionDomMarker('map-sat-selected', 'map-sat-selected-label', null, '', 'selected', null);
        }
    };

    /**
     * Отрисовка координатной сетки.
     * Меридианы — вертикальные линии на спроецированной X (отсекаем те, что вне канваса).
     * Параллели — горизонтальные линии по Y, рисуем напрямую от x=0 до x=width:
     * через project() рисовать нельзя, т.к. при сдвинутом центре `project(-180, lat).x`
     * и `project(180, lat).x` после нормализации дают одну и ту же точку → линия длины 0.
     */
    EarthView.prototype._drawGrid = function() {
        const ctx = this.ctx;
        const step = this.options.gridStep;
        const w = this.width;
        const h = this.height;

        // Меридианы (вертикали).
        for (let lon = -180; lon <= 180; lon += step) {
            const isMajor = (lon === 0 || lon === 180 || lon === -180);
            const p = this.project(lon, 0);
            if (p.x < 0 || p.x > w) { continue; }
            ctx.strokeStyle = isMajor ? this.colors.gridMajor : this.colors.grid;
            ctx.lineWidth = 1;
            ctx.beginPath();
            ctx.moveTo(p.x, 0);
            ctx.lineTo(p.x, h);
            ctx.stroke();
        }

        // Параллели (горизонтали) — Y зависит только от lat, рисуем во всю ширину канваса.
        for (let lat = -90; lat <= 90; lat += step) {
            const isMajor = (lat === 0);
            const p = this.project(0, lat);
            ctx.strokeStyle = isMajor ? this.colors.gridMajor : this.colors.grid;
            ctx.lineWidth = 1;
            ctx.beginPath();
            ctx.moveTo(0, p.y);
            ctx.lineTo(w, p.y);
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

        // Подписи долготы (внизу) — только для меридианов, попадающих в видимую область.
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        for (let lon = -150; lon <= 180; lon += step) {
            const p = this.project(lon, -90);
            if (p.x < 0 || p.x > this.width) { continue; }
            const label = lon.toString();
            ctx.fillText(label, p.x, this.height - 14);
        }

        // Подписи широты (слева)
        ctx.textAlign = 'right';
        ctx.textBaseline = 'middle';
        for (let lat = -80; lat <= 80; lat += 10) {
            if (lat === 0) { continue; }
            const p = this.project(-180, lat);
            if (p.y < 0 || p.y > this.height) { continue; }
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
        const threshold = this._antimeridianThreshold();

        // Определяем тип полигона: содержит ли он южный или северный полюс.
        let hasSouthPole = false, hasNorthPole = false;
        for (let i = 0; i < coords.length; i++) {
            if (coords[i][1] <= -89.5) { hasSouthPole = true; }
            if (coords[i][1] >= 89.5) { hasNorthPole = true; }
        }

        // ── Полярный полигон (Антарктида / Россия с границей через 180°): ────────
        // обход антимеридиана через нижний/верхний край канваса, единый замкнутый путь.
        if (hasSouthPole || hasNorthPole) {
            ctx.beginPath();
            let prevP = null;
            let moved = false;

            for (let i = 0; i < coords.length; i++) {
                const p = this.project(coords[i][0], coords[i][1]);
                const px = Math.max(0, Math.min(w, p.x));
                const py = Math.max(0, Math.min(h, p.y));

                if (prevP && Math.abs(p.x - prevP.x) > threshold) {
                    const goingRTL = p.x < prevP.x;
                    const edgeX = goingRTL ? w : 0;
                    const oppositeEdgeX = goingRTL ? 0 : w;
                    const yPrev = Math.max(0, Math.min(h, prevP.y));

                    ctx.lineTo(edgeX, yPrev);
                    if (hasSouthPole) {
                        ctx.lineTo(edgeX, h);
                        ctx.lineTo(oppositeEdgeX, h);
                    } else {
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
            ctx.closePath();
            ctx.fill();
            return;
        }

        // ── Обычный (не-полярный) полигон: разбиваем по швам антимеридиана. ──────
        // При сдвинутом center.lon шов проходит на lon = center.lon ± 180°. Если
        // полигон пересекает шов, его нужно разделить на два «полу-полигона»,
        // каждый замкнуть по краю канваса — иначе путь протягивается через всю карту.
        const proj = new Array(coords.length);
        for (let i = 0; i < coords.length; i++) {
            proj[i] = this.project(coords[i][0], coords[i][1]);
        }

        const chunks = [];
        let cur = [proj[0]];
        for (let i = 1; i < proj.length; i++) {
            const a = proj[i - 1];
            const b = proj[i];
            if (Math.abs(b.x - a.x) > threshold) {
                // Завершаем текущий кусок выходом на ближний край канваса.
                const goingRTL = b.x < a.x;
                const exitX = goingRTL ? w : 0;
                const enterX = goingRTL ? 0 : w;
                cur.push({ x: exitX, y: a.y });
                chunks.push(cur);
                cur = [{ x: enterX, y: b.y }];
            }
            cur.push(b);
        }
        chunks.push(cur);

        // Кольцо замкнуто, поэтому первый и последний куски лежат по одну сторону шва —
        // склеиваем их обратно, чтобы заливка получилась цельной.
        if (chunks.length >= 2) {
            chunks[0] = chunks[chunks.length - 1].concat(chunks[0]);
            chunks.pop();
        }

        for (let s = 0; s < chunks.length; s++) {
            const c = chunks[s];
            if (c.length < 3) { continue; }
            ctx.beginPath();
            const x0 = Math.max(0, Math.min(w, c[0].x));
            const y0 = Math.max(0, Math.min(h, c[0].y));
            ctx.moveTo(x0, y0);
            for (let i = 1; i < c.length; i++) {
                const px = Math.max(0, Math.min(w, c[i].x));
                const py = Math.max(0, Math.min(h, c[i].y));
                ctx.lineTo(px, py);
            }
            ctx.closePath();
            ctx.fill();
        }
    };

    /**
     * Отрисовка береговых линий
     */
    /**
     * Отрисовка береговых линий.
     * @param {number} [progress] — доля длины [0..1]. Если задан и < 1 — каждая
     *   полилиния рисуется частично, от первой точки к последней (эффект «карандаша»).
     */
    EarthView.prototype._drawCoastlines = function(progress) {
        const ctx = this.ctx;
        const features = this.coastlineData.features;

        ctx.strokeStyle = this.colors.coastline;
        ctx.lineWidth = 1;

        for (let i = 0; i < features.length; i++) {
            const feature = features[i];
            const geometry = feature.geometry;

            if (geometry.type === 'LineString') {
                this._drawLineString(geometry.coordinates, progress);
            } else if (geometry.type === 'MultiLineString') {
                for (let j = 0; j < geometry.coordinates.length; j++) {
                    this._drawLineString(geometry.coordinates[j], progress);
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
            // Отсекаем города вне видимой области: на zoom>1 их большинство.
            if (!this._isInViewport(p, 80)) { continue; }

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
     * Отрисовка одной линии (LineString).
     * @param {Array} coords - Массив координат [[lon, lat], ...]
     * @param {number} [progress] - Доля длины полилинии для отрисовки [0..1].
     *   Если не задан или ≥ 1 — рисуется вся полилиния (поведение по умолчанию).
     *   Если < 1 — рисуется только начальная часть пропорционально progress; последний
     *   сегмент при этом дорисовывается частично, давая эффект ведения «карандашом».
     */
    EarthView.prototype._drawLineString = function(coords, progress) {
        if (!coords || coords.length < 2) { return; }

        // Решаем, рисовать ли частично. usePartial=false — старое поведение.
        const usePartial = (typeof progress === 'number' && progress >= 0 && progress < 1);
        let pointsCount = coords.length;
        let tailFrac = 0;

        if (usePartial) {
            // Прогресс трактуем как долю «пройденных сегментов» (N-1 сегментов всего).
            const exact = (coords.length - 1) * progress;
            const whole = Math.floor(exact);
            tailFrac = exact - whole;
            pointsCount = whole + 1; // сколько целых точек уже отрисовано
            if (pointsCount < 1) { pointsCount = 1; }
            if (pointsCount >= coords.length) {
                pointsCount = coords.length;
                tailFrac = 0;
            }
            if (pointsCount < 2 && tailFrac <= 0) { return; } // ещё нечего рисовать
        }

        const ctx = this.ctx;
        ctx.beginPath();

        const threshold = this._antimeridianThreshold();
        let prevP = null;
        let moved = false;

        for (let i = 0; i < pointsCount; i++) {
            const lon = coords[i][0];
            const lat = coords[i][1];
            const p = this.project(lon, lat);

            // Разрыв линии на антимеридиане.
            if (prevP && Math.abs(p.x - prevP.x) > threshold) {
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

        // «Кончик карандаша»: частичный сегмент к ближайшей не отрисованной точке.
        if (usePartial && tailFrac > 0 && pointsCount < coords.length && prevP) {
            const lonNext = coords[pointsCount][0];
            const latNext = coords[pointsCount][1];
            const pn = this.project(lonNext, latNext);
            // Не рисуем «хвост» через антимеридиан — иначе линия растянется через всю карту.
            if (Math.abs(pn.x - prevP.x) <= threshold) {
                const ix = prevP.x + (pn.x - prevP.x) * tailFrac;
                const iy = prevP.y + (pn.y - prevP.y) * tailFrac;
                if (!moved) {
                    ctx.moveTo(prevP.x, prevP.y);
                }
                ctx.lineTo(ix, iy);
            }
        }

        ctx.stroke();
    };

    /**
     * Отрисовка границ РФ и подписи «Россия».
     * @param {number} [progress] — доля длины [0..1] для phased-анимации.
     */
    EarthView.prototype._drawRussiaBorders = function(progress) {
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
                this._drawLineString(geom.coordinates[0], progress);
            } else if (geom.type === 'MultiPolygon') {
                for (let p = 0; p < geom.coordinates.length; p++) {
                    const ring = geom.coordinates[p][0];
                    if (ring && ring.length >= 2) {
                        this._drawLineString(ring, progress);
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

        // past и future уже разрезаны на бэке по антимеридиану через
        // splitAtAntimeridian — рисуем каждый сегмент отдельно. Длительность
        // покрытия (см. GenerateDefaultGroundTrack) подобрана так, чтобы трасса
        // покрывала ровно 360° по долготе, поэтому на equirectangular-карте
        // линия идёт от края до края без gap'ов.
        //
        // Чтобы между past и future не возникала «дыра» в районе текущей позиции
        // КА (бэк делит сегменты по now без overlapping-точки), bridge'м
        // дополняем последний past-сегмент первой точкой первого future-сегмента.
        const pastSegs = (track && track.past) ? track.past : [];
        const futureSegs = (track && track.future) ? track.future : [];
        const bridgedPast = this._bridgePastFuture(pastSegs, futureSegs);

        if (Array.isArray(bridgedPast)) {
            for (let s = 0; s < bridgedPast.length; s++) {
                const seg = bridgedPast[s];
                if (Array.isArray(seg) && seg.length >= 2) {
                    this._drawTrackSegment(seg, this.colors.orbitPast);
                }
            }
        }
        for (let s = 0; s < futureSegs.length; s++) {
            const seg = futureSegs[s];
            if (Array.isArray(seg) && seg.length >= 2) {
                this._drawTrackSegment(seg, this.colors.orbitFuture);
            }
        }
    };

    /**
     * Соединительный «мост» между past и future — возвращает копию массива past,
     * у которого последний сегмент дополнен первой точкой первого future-сегмента.
     * Сами входные массивы не мутируются.
     * @param {Array<Array>} past — past-сегменты из бэкенда (или null/undefined).
     * @param {Array<Array>} future — future-сегменты (или null/undefined).
     * @returns {Array<Array>|null} модифицированный past или null/исходный, если соединять нечего.
     * @private
     */
    EarthView.prototype._bridgePastFuture = function(past, future) {
        if (!past || past.length === 0) { return past || null; }
        if (!future || future.length === 0) { return past; }
        const lastPastSeg = past[past.length - 1];
        const firstFutureSeg = future[0];
        if (!lastPastSeg || lastPastSeg.length === 0) { return past; }
        if (!firstFutureSeg || firstFutureSeg.length === 0) { return past; }

        // Защита от «палки через всю карту»: если последняя точка past и первая
        // точка future физически далеко друг от друга по lon или по времени —
        // bridge не делаем. Такое возможно, когда past содержит только boundary-
        // точку окна (КА вблизи антимеридиана окна) или сегменты разделены
        // несколькими шагами SGP4 (gap в данных). Защита `_drawTrackSegment` по
        // |Δx|>thresh не срабатывает, если точки на одной стороне окна → линия
        // тянется наискосок через половину canvas.
        const a = lastPastSeg[lastPastSeg.length - 1];
        const b = firstFutureSeg[0];
        if (a && b) {
            // Δlon с учётом перехода через ±180°.
            let dLon = (b.lon || 0) - (a.lon || 0);
            while (dLon > 180) { dLon -= 360; }
            while (dLon < -180) { dLon += 360; }
            const aTs = (a.ts !== undefined && a.ts !== null) ? a.ts : a.time;
            const bTs = (b.ts !== undefined && b.ts !== null) ? b.ts : b.time;
            const hasTs = (aTs !== undefined && aTs !== null && bTs !== undefined && bTs !== null);
            const dTs = hasTs ? Math.abs(bTs - aTs) : 0;
            // Пороги: 30° по долготе ≈ 8 шагов SGP4 (~4 мин LEO),
            // 120000 мс = 4 шага по 30 с — заведомо больше нормального соседства
            // (нормальное соседство past_last ↔ now_point ≈ 1 шаг). Δlat>15° —
            // тоже аномалия (LEO ~6.5°/мин по великому кругу): bridge через
            // полюс при «zigzag»-данных давал диагональ через половину карты.
            const BRIDGE_MAX_LON_GAP_DEG = 30;
            const BRIDGE_MAX_LAT_GAP_DEG = 15;
            const BRIDGE_MAX_TS_GAP_MS = 120000;
            const dLat = Math.abs((b.lat || 0) - (a.lat || 0));
            if (Math.abs(dLon) > BRIDGE_MAX_LON_GAP_DEG ||
                dLat > BRIDGE_MAX_LAT_GAP_DEG ||
                (dTs > 0 && dTs > BRIDGE_MAX_TS_GAP_MS)) {
                return past;
            }
        }

        const extended = lastPastSeg.slice();
        extended.push(firstFutureSeg[0]);
        const out = past.slice(0, past.length - 1);
        out.push(extended);
        return out;
    };

    /**
     * Отрисовка одного сегмента орбиты — линия + опциональные точки-метки.
     * Защита от ложных «склеек» через антимеридиан: если соседние спроецированные
     * точки слишком далеко друг от друга по x (> _antimeridianThreshold), считаем
     * это переходом через ±180° и начинаем новый sub-path (на бэке такие сегменты
     * уже разрезаны splitAtAntimeridian, но dpr/zoom-аномалии страхуем).
     * @param {Array} points - Массив точек [{lon, lat, time} или {lon, lat, ts}]
     * @param {string} color - Цвет линии
     */
    EarthView.prototype._drawTrackSegment = function(points, color) {
        if (!points || points.length < 2) { return; }
        const ctx = this.ctx;
        const mode = this.options.trackMode;
        const dotInterval = this.options.trackDotInterval;

        if (mode === 'line' || mode === 'both') {
            ctx.strokeStyle = color;
            ctx.lineWidth = this._mapTrackLineWidth;
            ctx.setLineDash([]);
            ctx.beginPath();
            const thresh = this._antimeridianThreshold();
            let prevP = null;
            let prevLon = null;
            let prevLat = null;
            for (let k = 0; k < points.length; k++) {
                const p = this.project(points[k].lon, points[k].lat);
                const lon = points[k].lon;
                const lat = points[k].lat;
                let isJump = false;
                if (prevP && Math.abs(p.x - prevP.x) > thresh) {
                    isJump = true;
                } else if (prevLon !== null) {
                    let dLon = lon - prevLon;
                    while (dLon > 180) { dLon -= 360; }
                    while (dLon < -180) { dLon += 360; }
                    if (Math.abs(dLon) > TRACK_LON_JUMP_DEG) { isJump = true; }
                }
                if (!isJump && prevLat !== null && Math.abs(lat - prevLat) > TRACK_LAT_JUMP_DEG) {
                    isJump = true;
                }
                if (k === 0 || isJump) {
                    ctx.moveTo(p.x, p.y);
                } else {
                    ctx.lineTo(p.x, p.y);
                }
                prevP = p;
                prevLon = lon;
                prevLat = lat;
            }
            ctx.stroke();
        }

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
            if (Math.abs(ddx) > this._antimeridianThreshold()) {
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

        // Скрываем DOM-маркер, если центр иконки вышел за пределы канваса.
        // Иначе при zoom>1 маркер «прилипает» к краю и создаёт фантомного спутника.
        // Допуск = MAP_SAT_MARKER_HALF_PX, чтобы иконка, чей центр чуть за краем,
        // но половина которой видна в кадре, всё же отображалась.
        const inView = this._isInViewport(p, MAP_SAT_MARKER_HALF_PX);
        const pctX = (p.x / pw) * 100;
        const pctY = (p.y / ph) * 100;
        const h = MAP_SAT_MARKER_HALF_PX;
        el.style.left = 'calc(' + pctX + '% - ' + h + 'px)';
        el.style.top = 'calc(' + pctY + '% - ' + h + 'px)';
        el.style.display = (st.orientReady && inView) ? 'block' : 'none';
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
        const ICON_R_HIGHLIGHT = Math.round(ICON_R_MAIN * 1.32);
        const ICON_R_SECONDARY = 20 * dpr;
        const iconRadiusFor = function(nid, isSecondary) {
            if (isHighlight(nid)) { return ICON_R_HIGHLIGHT; }
            return isSecondary ? ICON_R_SECONDARY : ICON_R_MAIN;
        };
        // Карта nid → alias из текущей группы (satellite_group_update от SSE).
        // Используется для второй строки карточки (alias / второе имя КА).
        const aliasMap = {};
        if (sm && typeof sm.getSatelliteGroup === 'function') {
            const grp = sm.getSatelliteGroup();
            if (grp && Array.isArray(grp.satellites)) {
                for (let i = 0; i < grp.satellites.length; i++) {
                    const s = grp.satellites[i];
                    if (s && s.norad_id != null && s.sat_alias) {
                        aliasMap[s.norad_id] = s.sat_alias;
                    }
                }
            }
        }
        const aliasOf = (nid) => {
            if (nid == null) { return ''; }
            const a = aliasMap[nid];
            return a ? String(a) : '';
        };
        // Подбор ширины карточки под содержимое (имя + alias).
        // Возвращает значение в physical px (как и остальные размеры аллокатора).
        // Шрифты подобраны под .map-sat-callout: имя 11px bold, sub 9px medium.
        // monospace fallback близок по метрикам к --font-mono без чтения computed style.
        const measure = (name, alias) => {
            ctx.save();
            ctx.font = 'bold 11px monospace';
            const wName = ctx.measureText(name || '').width;
            ctx.font = '500 9px monospace';
            const wAlias = ctx.measureText(alias || '').width;
            ctx.restore();
            // Горизонтальные паддинги карточки (4+7) + 2px бордер ≈ 13 logical px.
            // Прибавляем небольшой буфер на разницу шрифтов и округление.
            const innerPad = 16 * dpr;
            const target = Math.max(wName, wAlias) + innerPad;
            const minW = 56 * dpr;
            const maxW = 130 * dpr;
            const bucket = 8 * dpr;
            const raw = Math.round(Math.max(minW, Math.min(maxW, target)));
            return Math.round(raw / bucket) * bucket;
        };

        const markers = [];
        const trkId = this.satellite.noradId;
        if (trkId && this.satellite.position) {
            const p = this.project(this.satellite.position.lon, this.satellite.position.lat);
            const name = this.satellite.name || '';
            const alias = aliasOf(trkId);
            markers.push({
                id: trkId,
                x: p.x,
                y: p.y,
                color: this.colors.satLabel || '#ffeb3b',
                name: name,
                alias: alias,
                cardWidth: measure(name, alias),
                iconRadius: iconRadiusFor(trkId, false),
                isTracked: isHighlight(trkId),
            });
        }
        const selId = this._selectedSatellite.noradId;
        if (selId && selId !== trkId && this._selectedSatellite.position) {
            const p = this.project(this._selectedSatellite.position.lon, this._selectedSatellite.position.lat);
            const name = this._selectedSatellite.name || '';
            const alias = aliasOf(selId);
            markers.push({
                id: selId,
                x: p.x,
                y: p.y,
                color: this.colors.mapIconSelectedFill || '#ffb347',
                name: name,
                alias: alias,
                cardWidth: measure(name, alias),
                iconRadius: iconRadiusFor(selId, false),
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
            const alias = aliasOf(nid);
            markers.push({
                id: nid,
                x: p.x,
                y: p.y,
                color: c,
                name: name,
                alias: alias,
                cardWidth: measure(name, alias),
                iconRadius: iconRadiusFor(nid, true),
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
                const iconGap = 10 * dpr;
                obstacles.push({
                    x: m.x - r - iconGap,
                    y: m.y - r - iconGap,
                    w: 2 * (r + iconGap),
                    h: 2 * (r + iconGap),
                });
            }
        }
        return obstacles;
    };

    /**
     * Запретные сегменты трасс для layout выносок.
     * Все видимые на карте трассы (selected, tracking, вторичные с «глазом»),
     * чтобы карточки и leader-линии не пересекали орбитальные линии.
     *
     * @private
     */
    EarthView.prototype._collectForbiddenSegments = function() {
        const out = [];
        const selSm = this._selectedSatellite ? this._selectedSatellite.noradId : null;
        const trkSm = this.satellite ? this.satellite.noradId : null;

        if (selSm) {
            this._appendTrackSegments(out, this._selectedSatellite.groundTrack);
        }
        if (trkSm && (!selSm || String(trkSm) !== String(selSm))) {
            this._appendTrackSegments(out, this.satellite.groundTrack);
        }
        // Вторичные пунктиры не включаем: при группе из N КА сегментов тысячи,
        // SA callout-layout блокирует main thread и замирают водопады TX в Авто.
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
            if (Array.isArray(track.past)) { Array.prototype.push.apply(polylines, track.past); }
            if (Array.isArray(track.future)) { Array.prototype.push.apply(polylines, track.future); }
        }
        const halfW = this._antimeridianThreshold();
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
        // clusterDistance зависит от размера canvas (physical px).
        this._calloutLayout.opts.clusterDistance = Math.min(this.width, this.height) * 0.4;
        const obstacles = this._collectCalloutObstacles(markers);
        const bounds = { width: this.width, height: this.height };
        // Запретные сегменты трасс selected (оранжевая) + tracking (синяя):
        // карточки не должны их пересекать — это «герой»-трассы кадра.
        const forbiddenSegments = this._collectForbiddenSegments();
        const layouts = this._calloutLayout.layout(markers, obstacles, bounds, forbiddenSegments);
        this._calloutRenderer.drawLinesOverlay(layouts, bounds);
        const info = {};
        for (let j = 0; j < markers.length; j++) {
            const mid = markers[j].id;
            info[mid] = {
                name: markers[j].name,
                alias: markers[j].alias || '',
                tracked: Boolean(markers[j].isTracked),
            };
        }
        this._calloutRenderer.update(layouts, bounds, info);
    };

    /**
     * Отрисовка позиции наблюдателя
     */
    EarthView.prototype._drawObserver = function() {
        const ctx = this.ctx;
        // Если активен live-preview из модалки настроек — рисуем маркер по
        // временным координатам, не трогая постоянную observer (бэкенд при
        // этом продолжает считать трассы по старой точке до Save).
        const src = this._observerPreview || this.observer;
        const p = this.project(src.lon, src.lat);
        // Отсекаем наблюдателя вне видимой области: на zoom>1, если центр
        // карты сместился (например, во время тестов с искусственным центром).
        if (!this._isInViewport(p, 60)) { return; }

        // Значок наземной станции: треугольная мачта-вышка вниз от точки +
        // излучатель в самой точке наблюдения + две статичные дуги радиоволн
        // симметрично по бокам. Центр значка (излучатель) = гео-координата.
        const dpr = window.devicePixelRatio || 1;
        const mastH = 16 * dpr; // высота мачты вниз от точки
        const mastHalf = 7 * dpr; // полуширина основания мачты
        const crossY = p.y + mastH * 0.55; // уровень поперечины
        const crossHalf = mastHalf * 0.55;
        const waveR1 = 8 * dpr; // радиус ближней дуги волн
        const waveR2 = 13 * dpr; // радиус дальней дуги волн
        const waveSpread = Math.PI / 4; // ±45° раствор дуг от горизонтали
        const emitterR = 2.4 * dpr; // радиус излучателя

        const iconColor = this.colors.observer || '#d4a040';
        const haloColor = this.colors.observerLabelStroke || 'rgba(0,0,0,0.9)';

        // Контуры мачты и волн одним path — рисуем дважды: тёмный ореол под низ,
        // затем основной цвет поверх (читаемость на любой теме карты).
        const tracePaths = function() {
            ctx.beginPath();
            // Мачта: две ноги от вершины (точка) к основанию
            ctx.moveTo(p.x, p.y);
            ctx.lineTo(p.x - mastHalf, p.y + mastH);
            ctx.moveTo(p.x, p.y);
            ctx.lineTo(p.x + mastHalf, p.y + mastH);
            // Основание мачты
            ctx.moveTo(p.x - mastHalf, p.y + mastH);
            ctx.lineTo(p.x + mastHalf, p.y + mastH);
            // Поперечина
            ctx.moveTo(p.x - crossHalf, crossY);
            ctx.lineTo(p.x + crossHalf, crossY);
            // Левые дуги радиоволн (выпуклостью влево)
            ctx.moveTo(p.x - waveR1, p.y);
            ctx.arc(p.x, p.y, waveR1, Math.PI - waveSpread, Math.PI + waveSpread);
            ctx.moveTo(p.x - waveR2, p.y);
            ctx.arc(p.x, p.y, waveR2, Math.PI - waveSpread, Math.PI + waveSpread);
            // Правые дуги радиоволн (выпуклостью вправо)
            ctx.moveTo(p.x + waveR1 * Math.cos(waveSpread), p.y - waveR1 * Math.sin(waveSpread));
            ctx.arc(p.x, p.y, waveR1, -waveSpread, waveSpread);
            ctx.moveTo(p.x + waveR2 * Math.cos(waveSpread), p.y - waveR2 * Math.sin(waveSpread));
            ctx.arc(p.x, p.y, waveR2, -waveSpread, waveSpread);
        };

        ctx.lineCap = 'round';
        ctx.lineJoin = 'round';

        // Тёмный ореол
        tracePaths();
        ctx.strokeStyle = haloColor;
        ctx.lineWidth = 3 * dpr;
        ctx.stroke();

        // Основной цвет
        tracePaths();
        ctx.strokeStyle = iconColor;
        ctx.lineWidth = 1.6 * dpr;
        ctx.stroke();

        // Излучатель в точке наблюдения
        ctx.beginPath();
        ctx.arc(p.x, p.y, emitterR, 0, Math.PI * 2);
        ctx.fillStyle = iconColor;
        ctx.strokeStyle = haloColor;
        ctx.lineWidth = Number(dpr);
        ctx.fill();
        ctx.stroke();

        // Название точки наблюдения — справа за дугами волн; обводка из темы.
        // В режиме preview подпись не дублируем — координаты в форме настроек
        // понятны без неё, а старое имя справа исчезает только после Save.
        if (!this._observerPreview && this.observer.name) {
            const obsText = this.observer.name.toLocaleUpperCase();
            ctx.font = 'bold 12px sans-serif';
            ctx.textAlign = 'left';
            ctx.textBaseline = 'middle';
            const labelX = p.x + waveR2 + 4 * dpr;
            ctx.strokeStyle = haloColor;
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
        const segments = this.satellite.visibilityZone;
        if (!segments || segments.length === 0) { return; }

        const dpr = window.devicePixelRatio || 1;
        const lineWidth = this._mapFootprintLineWidth * dpr;
        this.ctx.setLineDash([]);

        for (let k = 0; k < segments.length; k++) {
            this._drawZoneRing(segments[k], this.colors.footprintFill, this.colors.footprint, lineWidth);
        }
    };

    /**
     * Отрисовка кольца зоны видимости с корректной обработкой антимеридианного шва.
     * Если кольцо целиком укладывается в видимую долготу — рисуем заливку и обводку
     * замкнутым полигоном. Если кольцо пересекает шов (`center.lon ± 180°`) — рисуем
     * только обводку, разбивая её на сегменты по швам, чтобы линия не «протянулась»
     * горизонтально через всю карту.
     * @param {Array<{lon:number,lat:number}>} pts — точки кольца.
     * @param {string} fillStyle — стиль заливки (или null).
     * @param {string} strokeStyle — стиль обводки.
     * @param {number} lineWidth — толщина обводки (в physical px).
     * @private
     */
    EarthView.prototype._drawZoneRing = function(pts, fillStyle, strokeStyle, lineWidth) {
        if (!pts || pts.length < 3) { return; }

        const ctx = this.ctx;
        const w = this.width;
        const h = this.height;
        const z = this.zoom || 1;
        const cy = this._effectiveCenterLat();
        const halfW = w / 2;
        const halfH = h / 2;
        const wz = w * z;

        const projY = function(lat) { return halfH - ((lat - cy) / 90) * halfH * z; };
        const stroke = function() {
            if (strokeStyle) {
                ctx.strokeStyle = strokeStyle;
                ctx.lineWidth = lineWidth;
                ctx.stroke();
            }
        };
        const fill = function() {
            if (fillStyle) { ctx.fillStyle = fillStyle; ctx.fill(); }
        };

        // Обнаружение полярного footprint: backend для polar-footprint
        // выдаёт ОДИН сегмент с точками по всей долготе (огибает полюс).
        // Признак — circular-spread долгот ≈ 360° (footprint покрывает все
        // долготы). Считаем spread как 360°−max_gap между соседними
        // отсортированными lon (учитывая циклический «шов» 360°→0°): это
        // правильно отличает узкую дугу через ±180° (gap=356°→spread=4°)
        // от настоящего полярного footprint (gap≈5°→spread≈355°).
        // Сторона (north/south) — по знаку среднего lat. НЕ полагаемся на
        // достижение lat=±85°: footprint малого радиуса вокруг полюса
        // может не иметь высокоширотных точек, при этом он полностью
        // охватывает полюс на сфере.
        let sumLatRaw = 0;
        const sortedLons = new Array(pts.length);
        for (let i = 0; i < pts.length; i++) {
            sumLatRaw += pts[i].lat;
            sortedLons[i] = pts[i].lon;
        }
        sortedLons.sort(function(a, b) { return a - b; });
        let maxGap = 0;
        for (let i = 1; i < sortedLons.length; i++) {
            const g = sortedLons[i] - sortedLons[i - 1];
            if (g > maxGap) { maxGap = g; }
        }
        const wrapGap = 360 - (sortedLons[sortedLons.length - 1] - sortedLons[0]);
        if (wrapGap > maxGap) { maxGap = wrapGap; }
        const lonSpread = 360 - maxGap;
        const avgLat = sumLatRaw / pts.length;
        const isPolar = lonSpread > 270;
        const isNorthPolar = isPolar && avgLat >= 0;
        const isSouthPolar = isPolar && avgLat < 0;

        ctx.save();
        ctx.beginPath();
        ctx.rect(0, 0, w, h);
        ctx.clip();

        if (isNorthPolar || isSouthPolar) {
            // Для polar footprint строим «нижнюю огибающую» по корзинам
            // долгот: для северного — min(lat) в каждой корзине, для южного —
            // max(lat). Затем замыкаем полигон через верхний/нижний край
            // canvas (`edgeY` за пределами видимой области, clip отсекает).
            const bins = 72;
            const step = 360 / bins;
            const envelope = new Array(bins).fill(null);
            for (let i = 0; i < pts.length; i++) {
                let dLon = pts[i].lon - this.center.lon;
                while (dLon > 180) { dLon -= 360; }
                while (dLon <= -180) { dLon += 360; }
                const idx = Math.floor((dLon + 180) / step);
                const safeIdx = Math.max(0, Math.min(bins - 1, idx));
                if (envelope[safeIdx] === null) {
                    envelope[safeIdx] = pts[i].lat;
                } else if (isNorthPolar) {
                    if (pts[i].lat < envelope[safeIdx]) { envelope[safeIdx] = pts[i].lat; }
                } else {
                    if (pts[i].lat > envelope[safeIdx]) { envelope[safeIdx] = pts[i].lat; }
                }
            }
            for (let i = 0; i < bins; i++) {
                if (envelope[i] === null) {
                    let left = i - 1, right = i + 1;
                    while (left >= 0 && envelope[left] === null) { left--; }
                    while (right < bins && envelope[right] === null) { right++; }
                    if (left >= 0 && right < bins) {
                        const f = (i - left) / (right - left);
                        envelope[i] = envelope[left] * (1 - f) + envelope[right] * f;
                    } else if (left >= 0) {
                        envelope[i] = envelope[left];
                    } else if (right < bins) {
                        envelope[i] = envelope[right];
                    } else {
                        envelope[i] = isNorthPolar ? 90 : -90;
                    }
                }
            }

            const edgeY = isNorthPolar ? -10 : (h + 10);
            const leftEdgeX = -wz / 2 + halfW;
            const rightEdgeX = wz / 2 + halfW;
            // Crontinuity на швах ±180°: по lon footprint цикличен — берём
            // среднее между крайними корзинами (envelope[0] и envelope[N-1]),
            // чтобы линия на левом и правом краю canvas была одной высоты
            // (без видимого «уступа» при wrap).
            const seamLat = (envelope[0] + envelope[bins - 1]) / 2;
            const seamY = projY(seamLat);

            // Fill и stroke рисуем РАЗНЫМИ path'ами, иначе stroke по
            // замыкающим диагоналям к (corner_x, edgeY) попадает в
            // видимую область у краёв carta и создаёт «трапеции».
            // Fill: замкнутый полигон через top/bottom edge.
            // Stroke: только нижняя огибающая, начинается на левом краю
            // canvas и заканчивается на правом — без обрыва.
            const drawOnce = function(offsetX) {
                if (fillStyle) {
                    ctx.beginPath();
                    ctx.moveTo(leftEdgeX + offsetX, edgeY);
                    ctx.lineTo(leftEdgeX + offsetX, seamY);
                    for (let i = 0; i < bins; i++) {
                        const lonDeg = -180 + (i + 0.5) * step;
                        const x = (lonDeg / 360) * wz + halfW + offsetX;
                        const y = projY(envelope[i]);
                        ctx.lineTo(x, y);
                    }
                    ctx.lineTo(rightEdgeX + offsetX, seamY);
                    ctx.lineTo(rightEdgeX + offsetX, edgeY);
                    ctx.closePath();
                    fill();
                }
                if (strokeStyle) {
                    ctx.beginPath();
                    ctx.moveTo(leftEdgeX + offsetX, seamY);
                    for (let i = 0; i < bins; i++) {
                        const lonDeg = -180 + (i + 0.5) * step;
                        const x = (lonDeg / 360) * wz + halfW + offsetX;
                        const y = projY(envelope[i]);
                        ctx.lineTo(x, y);
                    }
                    ctx.lineTo(rightEdgeX + offsetX, seamY);
                    stroke();
                }
            };
            drawOnce(0);
            drawOnce(wz);
            drawOnce(-wz);
            ctx.restore();
            return;
        }

        // Обычный footprint: continuous unwrap проекция (раскручиваем lon
        // последовательно, без normalize Δlon). Это сохраняет непрерывность
        // полилинии при пересечении швов карты — нет «диагонали через всю
        // карту» при closePath. Затем mass-shift сдвигает фигуру в текущую
        // копию canvas, и 3 копии со смещением ±wz отрисовывают
        // обёрнутые части.
        const unwrap = new Array(pts.length);
        unwrap[0] = pts[0].lon;
        for (let i = 1; i < pts.length; i++) {
            let step = pts[i].lon - pts[i - 1].lon;
            while (step > 180) { step -= 360; }
            while (step <= -180) { step += 360; }
            unwrap[i] = unwrap[i - 1] + step;
        }
        let sumLon = 0;
        for (let i = 0; i < unwrap.length; i++) { sumLon += unwrap[i]; }
        const avgLon = sumLon / unwrap.length;
        let lonShift = 0;
        while (avgLon + lonShift - this.center.lon > 180) { lonShift -= 360; }
        while (avgLon + lonShift - this.center.lon < -180) { lonShift += 360; }
        const proj = new Array(pts.length);
        for (let i = 0; i < pts.length; i++) {
            const dLon = unwrap[i] + lonShift - this.center.lon;
            proj[i] = { x: (dLon / 360) * wz + halfW, y: projY(pts[i].lat) };
        }

        const drawOnce = function(offsetX) {
            ctx.beginPath();
            ctx.moveTo(proj[0].x + offsetX, proj[0].y);
            for (let i = 1; i < proj.length; i++) {
                ctx.lineTo(proj[i].x + offsetX, proj[i].y);
            }
            ctx.closePath();
            fill();
            stroke();
        };
        drawOnce(0);
        drawOnce(wz);
        drawOnce(-wz);
        ctx.restore();
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
        this._initZoomControls();
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
     * Привязка DOM-кнопок масштабирования и двойного клика по канвасу.
     * Безопасно работает, если кнопок нет на странице (пропускает шаги).
     * @private
     */
    EarthView.prototype._initZoomControls = function() {
        const self = this;
        const btnIn = document.getElementById('map-zoom-in');
        const btnOut = document.getElementById('map-zoom-out');
        const btnReset = document.getElementById('map-zoom-reset');

        if (btnIn) {
            btnIn.addEventListener('click', function() { self.zoomIn(); });
        }
        if (btnOut) {
            btnOut.addEventListener('click', function() { self.zoomOut(); });
        }
        if (btnReset) {
            btnReset.addEventListener('click', function() { self.resetView(); });
        }

        // Колбэк синхронизации disabled-состояний на крайних уровнях.
        this.onZoomChange = function(_zoom, idx, total) {
            if (btnIn) { btnIn.disabled = (idx >= total - 1); }
            if (btnOut) { btnOut.disabled = (idx <= 0); }
        };
        // Установить начальное состояние disabled.
        this._notifyZoomChange();

        // Двойной клик по канвасу — сброс масштаба.
        if (this.canvas) {
            this.canvas.addEventListener('dblclick', function(e) {
                if (e.target === self.canvas) {
                    self.resetView();
                }
            });
        }
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
        // Размеры карточек уменьшены, чтобы иконки КА не терялись на фоне
        // плотной группы выносок (UX: карточки 110×30 logical px, шрифты 11/9).
        this._calloutLayout = new window.CalloutLayout({
            stemLength:    72 * dpr,
            tailLength:    18 * dpr,
            cardWidth:    110 * dpr,
            cardHeight:    28 * dpr,
            minCardGap:     6 * dpr,
            boundsPadding:  8 * dpr,
            groupingMode: 'anneal',
            annealSweeps: 200,
            annealMaxSegments: 120,
            annealStepPx: 12 * dpr,
            annealSeedRadius: (72 + 18 + 55) * dpr,
            annealCacheThreshold: 8 * dpr,
            annealSeed: 42,
            clusterDistance: Math.min(this.width, this.height) * 0.4,
            ringGap: 70 * dpr,
            forbiddenPadding: 8 * dpr,
            iconObstacleGap: 10 * dpr,
            leaderCardPadding: 4 * dpr,
            cardWidthBucket: 8 * dpr,
        });
        const container = document.getElementById('map-callouts');
        if (container) {
            const self = this;
            this._calloutRenderer = new window.CalloutRenderer(container, {
                lineWidth: 1.5,
                bendDotRadius: 2.5,
                fallbackColor: this.colors.satLabel || '#ffeb3b',
                // Клик по карточке выноски → выбор спутника текущим (selected).
                // Делегирование наружу: внешний код может переопределить
                // `earthView.onSatelliteClick = (noradId) => {...}`.
                onCardClick: function(noradId) {
                    if (typeof self.onSatelliteClick === 'function') {
                        try { self.onSatelliteClick(noradId); } catch (e) { /* swallow */ }
                    }
                },
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
        // Центр карты всегда «прилипает» к наблюдателю.
        this._syncCenterToObserver();
    };

    /**
     * Live-preview маркера наблюдателя. Используется модалкой настроек:
     * пока пользователь правит координаты, маркер двигается на канвасе без
     * обращения к бэкенду (трассы/зона/группа не пересчитываются до Save).
     * Координаты сохраняются в `_observerPreview`; реальная `observer`
     * остаётся прежней до подтверждения.
     * @param {number} lon Долгота preview, градусы.
     * @param {number} lat Широта preview, градусы.
     */
    EarthView.prototype.setObserverPreview = function(lon, lat) {
        if (typeof lon !== 'number' || typeof lat !== 'number') {return;}
        if (Number.isNaN(lon) || Number.isNaN(lat)) {return;}
        this._observerPreview = { lon: lon, lat: lat };
        this.draw();
    };

    /**
     * Сбрасывает live-preview наблюдателя (Cancel модалки настроек).
     * Маркер возвращается в текущую (сохранённую) позицию.
     */
    EarthView.prototype.clearObserverPreview = function() {
        if (this._observerPreview) {
            this._observerPreview = null;
            this.draw();
        }
    };

    // ========== Управление масштабом карты ==========

    /**
     * Синхронизирует центр карты с позицией наблюдателя.
     * Если наблюдатель не задан — центр сбрасывается в (0, 0).
     * @private
     */
    EarthView.prototype._syncCenterToObserver = function() {
        if (this.observer && typeof this.observer.lon === 'number' && typeof this.observer.lat === 'number') {
            this.center = { lon: this.observer.lon, lat: this.observer.lat };
        } else {
            this.center = { lon: 0, lat: 0 };
        }
    };

    /**
     * Установить уровень масштабирования по индексу LEVELS.
     * Запускает phased-анимацию (R4), если animStyle === 'phased'.
     * @param {number} idx - Индекс в массиве MAP_ZOOM_LEVELS.
     * @returns {boolean} true, если уровень изменился.
     */
    EarthView.prototype.setZoomLevel = function(idx) {
        const clamped = Math.max(0, Math.min(MAP_ZOOM_LEVELS.length - 1, idx | 0));
        if (clamped === this._zoomIdx) {
            return false;
        }
        const fromIdx = this._zoomIdx;
        this._zoomIdx = clamped;
        this.zoom = MAP_ZOOM_LEVELS[clamped];
        if (this._calloutLayout && typeof this._calloutLayout.reset === 'function') {
            this._calloutLayout.reset();
        }
        if (this.options.animStyle === 'phased') {
            this._startZoomAnim(fromIdx, clamped);
        } else {
            this._zoomAnim = null;
            this.draw();
        }
        this._notifyZoomChange();
        return true;
    };

    /** Текущий уровень zoom (множитель). */
    EarthView.prototype.getZoom = function() {
        return this.zoom;
    };

    /** Индекс текущего уровня zoom в MAP_ZOOM_LEVELS. */
    EarthView.prototype.getZoomLevel = function() {
        return this._zoomIdx;
    };

    /** Увеличить масштаб на одну ступень. */
    EarthView.prototype.zoomIn = function() {
        return this.setZoomLevel(this._zoomIdx + 1);
    };

    /** Уменьшить масштаб на одну ступень. */
    EarthView.prototype.zoomOut = function() {
        return this.setZoomLevel(this._zoomIdx - 1);
    };

    /**
     * Сбросить масштаб к 1.0 и центр — на наблюдателя (или (0,0) при отсутствии).
     */
    EarthView.prototype.resetView = function() {
        this._syncCenterToObserver();
        return this.setZoomLevel(0);
    };

    /**
     * Колбэк для UI-кнопок (включить/отключить + / − на крайних уровнях).
     * Можно переопределить снаружи: earthView.onZoomChange = function(level, idx) {…}.
     * @private
     */
    EarthView.prototype._notifyZoomChange = function() {
        if (typeof this.onZoomChange === 'function') {
            try {
                this.onZoomChange(this.zoom, this._zoomIdx, MAP_ZOOM_LEVELS.length);
            } catch (e) {
                // eslint-disable-next-line no-console
                console.warn('EarthView.onZoomChange handler error:', e);
            }
        }
    };

    /**
     * Стадии phased-анимации zoom — задаются в долях от полной длительности
     * (`options.zoomAnimDurationMs`). Стадия с `endFrac > startFrac` имеет
     * собственный прогресс [0..1], который передаётся в соответствующий слой;
     * стадия с `endFrac === startFrac` — мгновенное появление слоя.
     *
     * Дизайн раскладки (DOS-bootup эстетика):
     *   0%        — очистка холста;
     *   5%        — появляется сетка координат;
     *   5 → 75%   — «карандашная» прорисовка береговых линий и границ РФ;
     *   78%       — появляется заливка континентов (поверх неё контур уже полный);
     *   85%       — появляются города и наблюдатель;
     *   100%      — анимация заканчивается, рисуется полный финальный кадр.
     *
     * В каждом кадре отрисовка идёт строго в одном проходе и в порядке слоёв
     * (background → grid → land(fill) → coast(stroke) → observer), без двойных
     * перерисовок одного и того же слоя.
     * @private
     */
    EarthView.PHASED_STAGES = [
        { key: 'clear', startFrac: 0.00, endFrac: 0.00 },
        { key: 'grid', startFrac: 0.05, endFrac: 0.05 },
        { key: 'coast', startFrac: 0.05, endFrac: 0.75 },
        { key: 'land', startFrac: 0.78, endFrac: 0.78 },
        { key: 'observer', startFrac: 0.85, endFrac: 0.85 },
        { key: 'dynamic', startFrac: 1.00, endFrac: 1.00 }
    ];

    /** Текущее время (с поддержкой замены в тестах). @private */
    EarthView.prototype._now = function() {
        return (typeof performance !== 'undefined' && performance.now)
            ? performance.now()
            : Date.now();
    };

    /**
     * Запуск phased-анимации zoom.
     * За время `options.zoomAnimDurationMs` карта перерисовывается заново:
     * сетка → береговые контуры (карандашом) → заливка суши → города → финальный кадр.
     * @param {number} fromIdx — предыдущий индекс уровня (для логов/расширения).
     * @param {number} toIdx — целевой индекс уровня.
     * @private
     */
    EarthView.prototype._startZoomAnim = function(fromIdx, toIdx) {
        const self = this;
        // Прерываем предыдущую анимацию, если она ещё крутится.
        if (this._zoomAnim && this._zoomAnim.raf && typeof cancelAnimationFrame !== 'undefined') {
            cancelAnimationFrame(this._zoomAnim.raf);
        }
        this._zoomAnim = {
            startTs: this._now(),
            fromIdx: fromIdx,
            toIdx: toIdx,
            raf: null
        };
        const tick = function() {
            if (!self._zoomAnim) { return; }
            const now = self._now();
            self._drawPhased(now);
            // Если ещё в анимации — следующий кадр.
            if (self._zoomAnim) {
                if (typeof requestAnimationFrame !== 'undefined') {
                    self._zoomAnim.raf = requestAnimationFrame(tick);
                } else {
                    self._zoomAnim.raf = setTimeout(tick, 16);
                }
            }
        };
        if (typeof requestAnimationFrame !== 'undefined') {
            this._zoomAnim.raf = requestAnimationFrame(tick);
        } else {
            this._zoomAnim.raf = setTimeout(tick, 16);
        }
    };

    /**
     * Послойная отрисовка одного кадра анимации zoom.
     *
     * До достижения стадии `observer` рисуется только статическая часть карты:
     * background → grid → land(fill) → coast(stroke, прогресс «карандашом»).
     * Динамические DOM-маркеры (selected/tracking) и DOM-карточки выносок при этом
     * скрыты, чтобы не висеть на старых пиксельных координатах прошлого зума.
     *
     * С достижения `observer` (≥85%) выполняется полная `draw()` каждый кадр —
     * это естественным образом перепозиционирует все DOM-маркеры и выноски под
     * актуальную проекцию.
     *
     * @param {number} now — текущая отметка времени.
     * @private
     */
    EarthView.prototype._drawPhased = function(now) {
        const anim = this._zoomAnim;
        if (!anim) {
            // Защита от гонок: если анимация была сброшена снаружи — статический кадр.
            this.draw();
            return;
        }
        const dur = Math.max(1, this.options.zoomAnimDurationMs || DEFAULT_ZOOM_ANIM_MS);
        const elapsed = now - anim.startTs;
        const frac = elapsed / dur;
        const stages = EarthView.PHASED_STAGES;

        // Найти стадию по ключу.
        const stageByKey = {};
        for (let i = 0; i < stages.length; i++) { stageByKey[stages[i].key] = stages[i]; }

        const reached = function(key) {
            const s = stageByKey[key];
            return s ? frac >= s.startFrac : false;
        };
        const stageProgress = function(key) {
            const s = stageByKey[key];
            if (!s) { return 0; }
            if (s.endFrac <= s.startFrac) { return frac >= s.startFrac ? 1 : 0; }
            const p = (frac - s.startFrac) / (s.endFrac - s.startFrac);
            return p < 0 ? 0 : (p > 1 ? 1 : p);
        };

        // С момента, когда пора рисовать наблюдателя/города, делегируем полной
        // отрисовке (через ядро _drawStatic, чтобы избежать рекурсии через draw):
        // это корректно позиционирует DOM-маркеры selected/tracking и DOM-карточки
        // выносок под актуальную проекцию (текущий zoom/center).
        if (reached('observer')) {
            this._setCalloutsLayerVisible(true);
            this._drawStatic();
            if (frac >= 1) { this._zoomAnim = null; }
            return;
        }

        // Иначе — phased «отрисовка карты»: чистим холст и рисуем статические слои.
        const ctx = this.ctx;
        ctx.fillStyle = this.colors.background;
        ctx.fillRect(0, 0, this.width, this.height);

        if (reached('grid') && this.options.showGrid) {
            this._drawGrid();
        }
        // Заливка материков появляется одним кадром после прорисовки контуров «карандашом».
        // Рисуем её ДО stroke, чтобы контур ложился поверх и не перекрывался.
        if (reached('land') && this.options.showLandFill && this.landData && this.landData.features) {
            this._drawLand();
        }
        if (reached('coast')) {
            const cp = stageProgress('coast');
            if (cp > 0) {
                if (this.options.showCoastlines && this.coastlineData) {
                    this._drawCoastlines(cp);
                }
                if (this.options.showRussiaBorders && this.russiaData) {
                    this._drawRussiaBorders(cp);
                }
            }
        }

        // Прячем DOM-маркеры и карточки выносок без сброса orientReady
        // (временное скрытие на стадиях phased-анимации zoom).
        this._hideDomMarkersForZoom();
        this._setCalloutsLayerVisible(false);
    };

    /**
     * Скрыть DOM-маркеры selected/tracking на время phased-анимации zoom.
     * Не трогает _domMarkerState — после zoom ориентация и позиция сохраняются.
     * @private
     */
    EarthView.prototype._hideDomMarkersForZoom = function() {
        if (typeof document === 'undefined') { return; }
        const ids = ['map-sat-tracking', 'map-sat-selected'];
        for (let i = 0; i < ids.length; i++) {
            const el = document.getElementById(ids[i]);
            if (el) { el.style.display = 'none'; }
        }
    };

    /**
     * Управление видимостью контейнера DOM-карточек выносок (#map-callouts).
     * Используется во время phased-анимации, чтобы карточки не висели на старых
     * пиксельных координатах при перерисовке карты.
     * @param {boolean} visible
     * @private
     */
    EarthView.prototype._setCalloutsLayerVisible = function(visible) {
        if (typeof document === 'undefined') { return; }
        const layer = document.getElementById('map-callouts');
        if (!layer) { return; }
        layer.style.visibility = visible ? '' : 'hidden';
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
     * Отрисовка слоя текущего (выбранного) спутника: одна цветовая
     * линия (--selected-track, жёлтый в тёмных темах / синий в светлых),
     * но разный СТИЛЬ для past и future — прошлая часть рисуется
     * круглыми точками, будущая сплошной линией. Контраст стиля помогает
     * быстро понять направление движения КА: «откуда пришёл» (точки)
     * vs «куда летит» (сплошная).
     * @private
     */
    EarthView.prototype._drawSelectedLayer = function() {
        const sel = this._selectedSatellite;
        const track = sel.groundTrack;
        if (!track) { return; }

        const ctx = this.ctx;
        const trackColor = this.colors.selectedTrack;
        const pastColor = trackColor;
        const futureColor = trackColor;
        const dpr = window.devicePixelRatio || 1;

        ctx.lineWidth = 2 * dpr;

        // Сегменты уже разрезаны на бэке по антимеридиану. Bridge'м past↔future,
        // чтобы не было «дыры» в районе текущей позиции КА. Дополнительно к
        // пиксельной защите `|Δx|>thresh` разрываем линию при `|Δlon|>30°` —
        // ловит «палки» через половину карты при observerLon ≠ 0 (см. doc у
        // TRACK_LON_JUMP_DEG).
        const drawSeg = function(self, seg, color) {
            if (!Array.isArray(seg) || seg.length < 2) { return; }
            ctx.strokeStyle = color;
            ctx.beginPath();
            const thresh = self._antimeridianThreshold();
            let prevP = null;
            let prevLon = null;
            let prevLat = null;
            for (let k = 0; k < seg.length; k++) {
                const p = self.project(seg[k].lon, seg[k].lat);
                const lon = seg[k].lon;
                const lat = seg[k].lat;
                let isJump = false;
                if (prevP && Math.abs(p.x - prevP.x) > thresh) {
                    isJump = true;
                } else if (prevLon !== null) {
                    let dLon = lon - prevLon;
                    while (dLon > 180) { dLon -= 360; }
                    while (dLon < -180) { dLon += 360; }
                    if (Math.abs(dLon) > TRACK_LON_JUMP_DEG) { isJump = true; }
                }
                if (!isJump && prevLat !== null && Math.abs(lat - prevLat) > TRACK_LAT_JUMP_DEG) {
                    isJump = true;
                }
                if (k === 0 || isJump) {
                    ctx.moveTo(p.x, p.y);
                } else {
                    ctx.lineTo(p.x, p.y);
                }
                prevP = p;
                prevLon = lon;
                prevLat = lat;
            }
            ctx.stroke();
        };

        if (track && typeof track === 'object' && !Array.isArray(track)) {
            const pastSegs = Array.isArray(track.past) ? track.past : [];
            const futureSegs = Array.isArray(track.future) ? track.future : [];
            const bridgedPast = this._bridgePastFuture(pastSegs, futureSegs);
            if (Array.isArray(bridgedPast)) {
                // Past — крупные круглые точки через ~8 px:
                // setLineDash([0, gap]) + lineCap='round' → нулевая длина
                // штриха, gap между точками. Толщина 3*dpr — точки крупнее
                // солидной future-линии и хорошо заметны.
                const prevCap = ctx.lineCap;
                const prevWidth = ctx.lineWidth;
                ctx.lineCap = 'round';
                ctx.lineWidth = 4 * dpr;
                ctx.setLineDash([0, 10]);
                for (let s = 0; s < bridgedPast.length; s++) {
                    drawSeg(this, bridgedPast[s], pastColor);
                }
                ctx.lineCap = prevCap || 'butt';
                ctx.lineWidth = prevWidth;
            }
            ctx.setLineDash([]);
            for (let s = 0; s < futureSegs.length; s++) {
                drawSeg(this, futureSegs[s], futureColor);
            }
        } else if (Array.isArray(track)) {
            ctx.setLineDash([]);
            drawSeg(this, track, futureColor);
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

        const dpr = window.devicePixelRatio || 1;
        const fillColor = this.colors.selectedFootprintFill || 'rgba(93, 173, 226, 0.12)';
        const strokeColor = this.colors.selectedFootprint || 'rgba(93, 173, 226, 0.6)';
        const lineWidth = this._mapFootprintLineWidth * dpr;

        for (let k = 0; k < segments.length; k++) {
            this._drawZoneRing(segments[k], fillColor, strokeColor, lineWidth);
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

        // Каждый сегмент рисуется отдельным sub-path (бэк уже разрезал по
        // антимеридиану через splitAtAntimeridian); bridge'м past↔future для
        // закрытия gap'a в районе текущей позиции КА. Дополнительная защита от
        // «палки через половину карты» при observerLon ≠ 0: разрыв при |Δlon|>30°
        // (см. TRACK_LON_JUMP_DEG).
        const self = this;
        const drawSeg = function(seg) {
            if (!Array.isArray(seg) || seg.length < 2) { return; }
            ctx.beginPath();
            const thresh = self._antimeridianThreshold();
            let prevP = null;
            let prevLon = null;
            let prevLat = null;
            for (let k = 0; k < seg.length; k++) {
                const p = self.project(seg[k].lon, seg[k].lat);
                const lon = seg[k].lon;
                const lat = seg[k].lat;
                let isJump = false;
                if (prevP && Math.abs(p.x - prevP.x) > thresh) {
                    isJump = true;
                } else if (prevLon !== null) {
                    let dLon = lon - prevLon;
                    while (dLon > 180) { dLon -= 360; }
                    while (dLon < -180) { dLon += 360; }
                    if (Math.abs(dLon) > TRACK_LON_JUMP_DEG) { isJump = true; }
                }
                if (!isJump && prevLat !== null && Math.abs(lat - prevLat) > TRACK_LAT_JUMP_DEG) {
                    isJump = true;
                }
                if (k === 0 || isJump) {
                    ctx.moveTo(p.x, p.y);
                } else {
                    ctx.lineTo(p.x, p.y);
                }
                prevP = p;
                prevLon = lon;
                prevLat = lat;
            }
            ctx.stroke();
        };

        const pastSegs = Array.isArray(track.past) ? track.past : [];
        const futureSegs = Array.isArray(track.future) ? track.future : [];
        const bridgedPast = this._bridgePastFuture(pastSegs, futureSegs);
        if (Array.isArray(bridgedPast)) {
            for (let s = 0; s < bridgedPast.length; s++) {
                drawSeg(bridgedPast[s]);
            }
        }
        for (let s = 0; s < futureSegs.length; s++) {
            drawSeg(futureSegs[s]);
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
        const BODY_W = 14;
        const BODY_H = 12;
        const BOOM_W = 2;
        const BOOM_H = 1.5;
        const PANEL_W = 10;
        const PANEL_H = 14;
        const ANT_H = 3;
        const ANT_DOT = 1.5;
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
        const halo = isLight ? 'rgba(0, 0, 0, 0.18)' : 'rgba(0, 0, 0, 0.45)';
        const lwHair = isLight ? Math.max(1, dpr) : Math.max(1.1, 1.2 * dpr);
        const lwPanel = isLight ? Math.max(1.2, 1.3 * dpr) : Math.max(1.4, 1.6 * dpr);
        const lwBody = isLight ? Math.max(1.4, 1.5 * dpr) : Math.max(1.6, 1.8 * dpr);

        ctx.save();
        ctx.lineCap = 'butt';
        ctx.lineJoin = 'miter';
        // Halo = мягкая тень: даёт «массу» иконке, но без свечения и неона.
        ctx.shadowColor = halo;
        ctx.shadowBlur = 2.5 * dpr;
        ctx.shadowOffsetX = 0;
        ctx.shadowOffsetY = Number(dpr);

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
        ctx.fillRect(cx + bodyHalfW, boomY, boomW, boomH);

        // ─── Солнечные панели (заливка цветом КА, чуть полупрозрачно) ───
        ctx.globalAlpha = 0.82;
        ctx.fillStyle = color;
        ctx.strokeStyle = stroke;
        ctx.lineWidth = lwPanel;
        const panelY = cy - panelH / 2;
        const leftX = cx - bodyHalfW - boomW - panelW;
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
        ctx.shadowOffsetY = Number(dpr);
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
    window.MAP_ZOOM_LEVELS = MAP_ZOOM_LEVELS;

    // CommonJS-экспорт для node-тестов.
    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { EarthView: EarthView, MAP_ZOOM_LEVELS: MAP_ZOOM_LEVELS }; // eslint-disable-line no-undef
    }

})();
