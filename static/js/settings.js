/* ========================================================================
   SettingsModal — модальное окно «Настройки»

   Отвечает за:
     - чтение GET /api/settings и заполнение формы по data-bind путям;
     - переключение табов (Станция / Внешний вид / Наблюдатель / TLE / SatNOGS
       / Радиотракты / Исключения);
     - dirty-state, валидация, отправка PUT /api/settings;
     - live-preview наблюдателя на карте (EarthView.setObserverPreview)
       без обращения к бэкенду;
     - открытие по шестерёнке в footer и по deep-link `/settings?settings=open`.
   ======================================================================== */

(function() {
    'use strict';

    const TAB_STATION = 'station';
    const TAB_RADIO_PATHS = 'radio-paths';
    const ANTENNA_STATIONARY = 'stationary';
    const ANTENNA_ROTATABLE = 'rotatable';
    const RECEIVER_NONE = '';
    const RECEIVER_SIMULATED = 'simulated';
    const STATUS_RESET_DELAY_MS = 6000;
    // Курируемый список городов (Россия + СНГ + столицы) для быстрого
    // выбора точки наблюдения. Подгружается один раз и кешируется.
    const CITIES_URL = '/static/data/cities.json';
    const CITY_CUSTOM = '__custom__';
    // Допустимое расхождение координат между config и записью города,
    // чтобы автоматически подсветить соответствующий пункт списка.
    const CITY_MATCH_EPS_DEG = 0.001;
    const OBSERVER_FIELDS = ['station.observer.name', 'station.observer.lat',
        'station.observer.lon', 'station.observer.alt_m'];

    /**
     * Вытаскивает значение из объекта по dotted-path (например
     * `station.observer.lat`). Возвращает undefined если путь не существует.
     */
    function getByPath(obj, path) {
        const parts = path.split('.');
        let cur = obj;
        for (const p of parts) {
            if (cur == null) return undefined;
            cur = cur[p];
        }
        return cur;
    }

    /**
     * Устанавливает значение в объект по dotted-path, создавая отсутствующие
     * вложенные объекты на лету.
     */
    function setByPath(obj, path, value) {
        const parts = path.split('.');
        let cur = obj;
        for (let i = 0; i < parts.length - 1; i++) {
            const p = parts[i];
            if (cur[p] == null || typeof cur[p] !== 'object') {
                cur[p] = {};
            }
            cur = cur[p];
        }
        cur[parts[parts.length - 1]] = value;
    }

    function deepClone(v) {
        return JSON.parse(JSON.stringify(v));
    }

    /**
     * Вытаскивает значение из инпута с учётом типа (number/checkbox/csv-list/duration).
     */
    function readField(input) {
        const type = input.dataset.bindType;
        if (input.type === 'checkbox') {
            return !!input.checked;
        }
        if (type === 'csv-list') {
            return input.value
                .split(',')
                .map((s) => s.trim())
                .filter(Boolean);
        }
        if (type === 'duration') {
            return parseDurationToNanoseconds(input.value);
        }
        if (input.type === 'number') {
            const v = parseFloat(input.value);
            return Number.isFinite(v) ? v : 0;
        }
        return input.value;
    }

    /**
     * Записывает значение в инпут с учётом типа.
     */
    function writeField(input, value) {
        const type = input.dataset.bindType;
        if (input.type === 'checkbox') {
            input.checked = !!value;
            return;
        }
        if (type === 'csv-list') {
            input.value = Array.isArray(value) ? value.join(', ') : '';
            return;
        }
        if (type === 'duration') {
            input.value = formatNanosecondsToDuration(value);
            return;
        }
        input.value = value == null ? '' : String(value);
    }

    /**
     * Парсит human-friendly запись длительности ("6h", "30m", "1h30m") и
     * возвращает значение в наносекундах (формат, в котором Go time.Duration
     * сериализуется в JSON по умолчанию).
     */
    function parseDurationToNanoseconds(text) {
        if (!text || typeof text !== 'string') return 0;
        const re = /(\d+(?:\.\d+)?)\s*(ns|us|µs|ms|s|m|h)/g;
        const units = {
            ns: 1,
            us: 1e3,
            'µs': 1e3,
            ms: 1e6,
            s: 1e9,
            m: 60 * 1e9,
            h: 3600 * 1e9
        };
        let total = 0;
        let matched = false;
        let m;
        while ((m = re.exec(text)) !== null) {
            matched = true;
            total += parseFloat(m[1]) * units[m[2]];
        }
        if (!matched) {
            const v = parseFloat(text);
            if (Number.isFinite(v)) return v;
            return 0;
        }
        return total;
    }

    /**
     * Форматирует наносекунды в человекочитаемую запись ("6h", "1h30m", "5m").
     */
    function formatNanosecondsToDuration(ns) {
        if (typeof ns !== 'number' || !Number.isFinite(ns) || ns <= 0) return '';
        const sec = Math.round(ns / 1e9);
        if (sec < 60) return sec + 's';
        const min = Math.floor(sec / 60);
        const remSec = sec % 60;
        if (min < 60) {
            return remSec === 0 ? min + 'm' : min + 'm' + remSec + 's';
        }
        const h = Math.floor(min / 60);
        const remMin = min % 60;
        return remMin === 0 ? h + 'h' : h + 'h' + remMin + 'm';
    }

    function SettingsModal(options) {
        this._root = document.getElementById('settings-modal');
        if (!this._root) {
            return;
        }
        this._earthView = (options && options.earthView) || null;
        this._currentConfig = null;
        this._workingConfig = null;
        this._exclusions = [];
        this._cities = null;
        this._dirty = false;
        this._activeTab = TAB_STATION;
        this._statusTimer = null;
        this._suppressCityAutoSwitch = false;
        this._sdrDevices = [];
        this._receiverTestLogs = {};

        this._bindEvents();
        this._setActiveTab(TAB_STATION);
    }

    SettingsModal.prototype._bindEvents = function() {
        const self = this;
        // Закрытие: крестик, кнопка отмены, клик по фону.
        this._root.querySelectorAll('[data-action="cancel-settings"]').forEach((el) => {
            el.addEventListener('click', () => self.close(true));
        });

        // Кнопка Сохранить.
        const saveBtn = this._root.querySelector('[data-action="save-settings"]');
        if (saveBtn) {
            saveBtn.addEventListener('click', () => self._save());
        }

        // Табы.
        this._root.querySelectorAll('.settings-tab').forEach((btn) => {
            btn.addEventListener('click', () => self._setActiveTab(btn.dataset.tab));
        });

        // Esc — отменить.
        document.addEventListener('keydown', (e) => {
            if (self._isOpen() && e.key === 'Escape') {
                self.close(true);
                e.preventDefault();
            }
        });

        // Любые изменения инпутов внутри панели — dirty + опц. live-preview.
        const panel = this._root.querySelector('.settings-modal__panel');
        if (panel) {
            panel.addEventListener('input', (e) => self._onFieldInput(e));
            panel.addEventListener('change', (e) => self._onFieldInput(e));
        }

        // Селектор быстрого выбора города на табе «Наблюдатель».
        const citySelect = this._root.querySelector('#settings-observer-city');
        if (citySelect) {
            citySelect.addEventListener('change', () => self._onCitySelect(citySelect.value));
        }
    };

    SettingsModal.prototype._isOpen = function() {
        return this._root && !this._root.hasAttribute('hidden');
    };

    SettingsModal.prototype._setActiveTab = function(tab) {
        if (!tab) return;
        this._activeTab = tab;
        this._root.setAttribute('data-tab', tab);
        this._root.querySelectorAll('.settings-tab').forEach((btn) => {
            const isActive = btn.dataset.tab === tab;
            btn.classList.toggle('is-active', isActive);
            btn.setAttribute('aria-selected', isActive ? 'true' : 'false');
        });
        this._root.querySelectorAll('.settings-pane').forEach((pane) => {
            pane.classList.toggle('is-active', pane.dataset.pane === tab);
        });
        if (tab === TAB_RADIO_PATHS) {
            this._ensureSdrDevicesLoaded();
        }
    };

    /**
     * Открывает модалку. Загружает актуальный конфиг с сервера и список
     * исключений (для таба «Исключения»).
     */
    SettingsModal.prototype.open = function() {
        if (this._isOpen()) return;
        this._root.removeAttribute('hidden');
        this._setStatus('', null);
        this._setDirty(false);

        const self = this;
        Promise.all([
            fetch('/api/settings').then((r) => self._parseJSONOrThrow(r)),
            self._loadExclusions(),
            self._loadCities()
        ])
            .then(([cfg]) => {
                self._currentConfig = cfg;
                self._workingConfig = deepClone(cfg);
                self._renderCitiesSelect();
                self._renderForm();
                self._renderRadioPaths();
                self._renderExclusions();
                self._syncCitySelectFromConfig();
            })
            .catch((err) => {
                self._setStatus('Не удалось загрузить настройки: ' + err.message, 'error');
            });
    };

    /**
     * Закрывает модалку. Если confirmDirty=true и есть несохранённые правки —
     * запрашивает подтверждение. Сбрасывает live-preview наблюдателя.
     */
    SettingsModal.prototype.close = function(confirmDirty) {
        if (!this._isOpen()) return;
        if (confirmDirty && this._dirty) {
            const ok = window.confirm('Изменения не сохранены. Закрыть без сохранения?');
            if (!ok) return;
        }
        if (this._earthView && typeof this._earthView.clearObserverPreview === 'function') {
            this._earthView.clearObserverPreview();
        }
        this._root.setAttribute('hidden', '');
        this._setDirty(false);
        // Чистим query-параметр после deep-link открытия.
        if (window.location.search.includes('settings=open')) {
            const url = new URL(window.location.href);
            url.searchParams.delete('settings');
            window.history.replaceState({}, '', url.toString());
        }
    };

    SettingsModal.prototype._parseJSONOrThrow = function(resp) {
        if (!resp.ok) {
            return resp.text().then((text) => {
                throw new Error('HTTP ' + resp.status + ': ' + text.substring(0, 200));
            });
        }
        return resp.json();
    };

    /**
     * Подгружает курируемый список городов один раз. Если файл недоступен —
     * select остаётся с одной опцией «Свои координаты», UX не ломается.
     */
    SettingsModal.prototype._loadCities = function() {
        if (this._cities) return Promise.resolve(this._cities);
        const self = this;
        return fetch(CITIES_URL)
            .then((r) => {
                if (!r.ok) throw new Error('HTTP ' + r.status);
                return r.json();
            })
            .then((data) => {
                self._cities = (data && Array.isArray(data.groups)) ? data.groups : [];
            })
            .catch(() => {
                self._cities = [];
            });
    };

    /**
     * Рендерит выпадающий список городов в виде optgroup'ов. Первая опция —
     * «Свои координаты» (для ручного ввода).
     */
    SettingsModal.prototype._renderCitiesSelect = function() {
        const sel = this._root.querySelector('#settings-observer-city');
        if (!sel) return;
        // Удаляем все optgroup, оставляя только первую опцию (custom).
        while (sel.lastChild && sel.lastChild.tagName !== 'OPTION') {
            sel.removeChild(sel.lastChild);
        }
        // Удаляем option-ы города от прошлого рендера (всё кроме __custom__).
        Array.from(sel.querySelectorAll('option')).forEach((opt) => {
            if (opt.value !== CITY_CUSTOM) sel.removeChild(opt);
        });
        if (!this._cities || this._cities.length === 0) return;
        this._cities.forEach((group) => {
            if (!group || !Array.isArray(group.cities)) return;
            const og = document.createElement('optgroup');
            og.label = group.label || '';
            group.cities.forEach((c) => {
                if (!c || !c.name) return;
                const opt = document.createElement('option');
                opt.value = group.label + '/' + c.name;
                opt.textContent = c.name;
                opt.dataset.lat = String(c.lat);
                opt.dataset.lon = String(c.lon);
                opt.dataset.alt = String(c.alt_m != null ? c.alt_m : 0);
                og.appendChild(opt);
            });
            sel.appendChild(og);
        });
    };

    /**
     * Подбирает значение select под текущие координаты в _workingConfig.
     * Если координаты совпадают с одним из городов — выбирает его и блокирует
     * поля; иначе ставит «Свои координаты» и оставляет поля редактируемыми.
     */
    SettingsModal.prototype._syncCitySelectFromConfig = function() {
        const sel = this._root.querySelector('#settings-observer-city');
        if (!sel || !this._workingConfig) return;
        const obs = this._workingConfig.station && this._workingConfig.station.observer;
        if (!obs) {
            sel.value = CITY_CUSTOM;
            this._setObserverFieldsDisabled(false);
            return;
        }
        const match = this._findMatchingCity(obs.lat, obs.lon);
        this._suppressCityAutoSwitch = true;
        if (match) {
            sel.value = match.value;
            this._setObserverFieldsDisabled(true);
        } else {
            sel.value = CITY_CUSTOM;
            this._setObserverFieldsDisabled(false);
        }
        this._suppressCityAutoSwitch = false;
    };

    /**
     * Поиск города в курируемом списке по близости координат.
     */
    SettingsModal.prototype._findMatchingCity = function(lat, lon) {
        if (typeof lat !== 'number' || typeof lon !== 'number') return null;
        if (!this._cities) return null;
        for (const group of this._cities) {
            if (!group || !Array.isArray(group.cities)) continue;
            for (const c of group.cities) {
                if (!c) continue;
                if (Math.abs(c.lat - lat) <= CITY_MATCH_EPS_DEG &&
                    Math.abs(c.lon - lon) <= CITY_MATCH_EPS_DEG) {
                    return { value: group.label + '/' + c.name, city: c };
                }
            }
        }
        return null;
    };

    /**
     * Обработчик change на селекторе городов. Для «Свои координаты» —
     * разблокирует поля, для конкретного города — заполняет lat/lon/alt/name
     * и блокирует поля до возврата к «Свои координаты».
     */
    SettingsModal.prototype._onCitySelect = function(value) {
        if (this._suppressCityAutoSwitch) return;
        if (!value || value === CITY_CUSTOM) {
            this._setObserverFieldsDisabled(false);
            return;
        }
        const sel = this._root.querySelector('#settings-observer-city');
        if (!sel) return;
        const opt = Array.from(sel.options).find((o) => o.value === value);
        if (!opt) return;
        const lat = parseFloat(opt.dataset.lat);
        const lon = parseFloat(opt.dataset.lon);
        const alt = parseFloat(opt.dataset.alt);
        const name = opt.textContent;
        if (!Number.isFinite(lat) || !Number.isFinite(lon)) return;
        if (!this._workingConfig.station) this._workingConfig.station = {};
        if (!this._workingConfig.station.observer) this._workingConfig.station.observer = {};
        const obs = this._workingConfig.station.observer;
        obs.name = name;
        obs.lat = lat;
        obs.lon = lon;
        obs.alt_m = Number.isFinite(alt) ? alt : 0;

        this._renderForm();
        this._setObserverFieldsDisabled(true);
        this._setDirty(true);
        this._updateObserverPreview();
    };

    /**
     * Включает/выключает редактирование полей наблюдателя (name/lat/lon/alt).
     * Селектор «Свои координаты» всегда остаётся доступным, чтобы можно было
     * выйти из режима «выбран город».
     */
    SettingsModal.prototype._setObserverFieldsDisabled = function(disabled) {
        OBSERVER_FIELDS.forEach((path) => {
            const input = this._root.querySelector('[data-bind="' + path + '"]');
            if (!input) return;
            input.readOnly = !!disabled;
            input.classList.toggle('is-locked', !!disabled);
        });
    };

    SettingsModal.prototype._loadExclusions = function() {
        const self = this;
        return fetch('/api/exclusions')
            .then((r) => {
                if (r.status === 404) return { exclusions: [] };
                return self._parseJSONOrThrow(r);
            })
            .then((data) => {
                self._exclusions = (data && data.exclusions) || [];
            })
            .catch(() => {
                self._exclusions = [];
            });
    };

    SettingsModal.prototype._renderForm = function() {
        const self = this;
        const inputs = this._root.querySelectorAll('[data-bind]');
        inputs.forEach((input) => {
            const path = input.dataset.bind;
            const value = getByPath(self._workingConfig, path);
            writeField(input, value);
        });
    };

    SettingsModal.prototype._ensureSdrDevicesLoaded = function() {
        if (this._sdrDevices.length > 0) {
            return Promise.resolve(this._sdrDevices);
        }
        return this._refreshSdrDevices(false);
    };

    SettingsModal.prototype._refreshSdrDevices = function(rerender) {
        const self = this;
        return fetch('/api/sdr/devices')
            .then((r) => self._parseJSONOrThrow(r))
            .then((data) => {
                self._sdrDevices = (data && data.devices) || [];
                if (rerender !== false) {
                    self._renderRadioPaths();
                }
            })
            .catch((err) => {
                self._setStatus('Не удалось обновить список приёмников: ' + err.message, 'error');
            });
    };

    SettingsModal.prototype._renderRadioPaths = function() {
        const container = this._root.querySelector('[data-list="station.radio_paths"]');
        if (!container) return;
        container.innerHTML = '';

        if (!this._workingConfig.station) {
            this._workingConfig.station = {};
        }
        const paths = this._workingConfig.station.radio_paths || [];
        paths.forEach((rp, idx) => {
            container.appendChild(this._renderRadioPathRow(rp, idx));
        });

        const addBtn = document.createElement('button');
        addBtn.type = 'button';
        addBtn.className = 'settings-add-btn';
        addBtn.textContent = '+ Добавить радиотракт';
        addBtn.addEventListener('click', () => {
            const list = this._workingConfig.station.radio_paths || [];
            const nextID = list.length === 0 ? 1 : Math.max.apply(null, list.map((r) => r.id)) + 1;
            list.push({
                id: nextID,
                name: 'Новый тракт',
                antenna: { name: '', type: ANTENNA_STATIONARY },
                receiver: { driver: RECEIVER_SIMULATED, label: 'Имитатор (simulated)' },
                rotator: null
            });
            this._workingConfig.station.radio_paths = list;
            this._renderRadioPaths();
            this._setDirty(true);
        });
        container.appendChild(addBtn);
    };

    SettingsModal.prototype._renderRadioPathRow = function(rp, idx) {
        const self = this;
        const wrap = document.createElement('div');
        wrap.className = 'settings-radio-path';
        wrap.dataset.idx = String(idx);

        const head = document.createElement('div');
        head.className = 'settings-radio-path__head';
        head.innerHTML = '<span>Тракт #' + rp.id + '</span>';
        const remove = document.createElement('button');
        remove.type = 'button';
        remove.className = 'settings-radio-path__remove';
        remove.textContent = 'Удалить';
        remove.addEventListener('click', () => {
            self._workingConfig.station.radio_paths.splice(idx, 1);
            delete self._receiverTestLogs[idx];
            self._renderRadioPaths();
            self._setDirty(true);
        });
        head.appendChild(remove);
        wrap.appendChild(head);

        wrap.appendChild(this._radioField(idx, 'name', 'Имя тракта', 'text', rp.name, true));

        const antTitle = document.createElement('h4');
        antTitle.className = 'settings-radio-path__subtitle';
        antTitle.textContent = 'Антенна';
        wrap.appendChild(antTitle);

        const antGrid = document.createElement('div');
        antGrid.className = 'settings-radio-path__grid';
        antGrid.appendChild(this._radioField(idx, 'antenna.name', 'Название антенны', 'text', rp.antenna && rp.antenna.name, false));
        antGrid.appendChild(this._radioAntennaTypeSelect(idx, rp));
        antGrid.appendChild(this._radioOptionalNumber(idx, 'antenna.freq_min_mhz', 'Диапазон min, МГц', rp.antenna && rp.antenna.freq_min_mhz));
        antGrid.appendChild(this._radioOptionalNumber(idx, 'antenna.freq_max_mhz', 'Диапазон max, МГц', rp.antenna && rp.antenna.freq_max_mhz));
        wrap.appendChild(antGrid);

        const rotatorBlock = this._renderRotatorBlock(rp, idx);
        wrap.appendChild(rotatorBlock);

        const rxTitle = document.createElement('h4');
        rxTitle.className = 'settings-radio-path__subtitle';
        rxTitle.textContent = 'Приёмник';
        wrap.appendChild(rxTitle);

        const rxRow = document.createElement('div');
        rxRow.className = 'settings-receiver-row';
        const rxSelect = document.createElement('select');
        rxSelect.className = 'settings-field__input settings-receiver-select';
        rxSelect.dataset.role = 'receiver-select';
        this._fillReceiverSelect(rxSelect, rp.receiver || {});
        rxSelect.addEventListener('change', () => {
            self._applyReceiverSelection(idx, rxSelect.value);
        });
        rxRow.appendChild(rxSelect);

        const refreshBtn = document.createElement('button');
        refreshBtn.type = 'button';
        refreshBtn.className = 'settings-btn settings-btn--secondary settings-receiver-refresh';
        refreshBtn.textContent = 'Обновить';
        refreshBtn.addEventListener('click', () => self._refreshSdrDevices(true));
        rxRow.appendChild(refreshBtn);
        wrap.appendChild(rxRow);

        const testBtn = document.createElement('button');
        testBtn.type = 'button';
        testBtn.className = 'settings-btn settings-btn--secondary settings-receiver-test';
        testBtn.textContent = 'Тест';
        testBtn.addEventListener('click', () => self._testReceiver(idx));
        wrap.appendChild(testBtn);

        const log = document.createElement('pre');
        log.className = 'settings-receiver-log';
        log.textContent = this._receiverTestLogs[idx] || '';
        wrap.appendChild(log);

        return wrap;
    };

    SettingsModal.prototype._radioAntennaTypeSelect = function(idx, rp) {
        const self = this;
        const lbl = document.createElement('label');
        lbl.className = 'settings-field';
        const span = document.createElement('span');
        span.className = 'settings-field__label';
        span.textContent = 'Тип антенны';
        const sel = document.createElement('select');
        sel.className = 'settings-field__input';
        [
            [ANTENNA_STATIONARY, 'Стационарная'],
            [ANTENNA_ROTATABLE, 'С поворотной платформой']
        ].forEach(([value, label]) => {
            const opt = document.createElement('option');
            opt.value = value;
            opt.textContent = label;
            sel.appendChild(opt);
        });
        const current = (rp.antenna && rp.antenna.type) || ANTENNA_STATIONARY;
        sel.value = current;
        sel.addEventListener('change', () => {
            const list = self._workingConfig.station.radio_paths;
            const target = list[idx];
            if (!target) return;
            if (!target.antenna) target.antenna = {};
            target.antenna.type = sel.value;
            if (sel.value === ANTENNA_STATIONARY) {
                target.rotator = null;
            } else if (!target.rotator) {
                target.rotator = {
                    driver: 'rotctld',
                    host: '127.0.0.1',
                    port: 4533,
                    az_range: [0, 360],
                    el_range: [0, 90],
                    step_deg: 1
                };
            }
            self._renderRadioPaths();
            self._setDirty(true);
        });
        lbl.appendChild(span);
        lbl.appendChild(sel);
        return lbl;
    };

    SettingsModal.prototype._renderRotatorBlock = function(rp, idx) {
        const block = document.createElement('div');
        block.className = 'settings-rotator-block';
        const isRotatable = rp.antenna && rp.antenna.type === ANTENNA_ROTATABLE;
        block.hidden = !isRotatable;
        if (!isRotatable) {
            return block;
        }

        const title = document.createElement('h4');
        title.className = 'settings-radio-path__subtitle';
        title.textContent = 'Поворотная платформа';
        block.appendChild(title);

        const grid = document.createElement('div');
        grid.className = 'settings-radio-path__grid';
        const rot = rp.rotator || {};
        grid.appendChild(this._radioField(idx, 'rotator.driver', 'Драйвер', 'text', rot.driver, false));
        grid.appendChild(this._radioField(idx, 'rotator.host', 'Host', 'text', rot.host, false));
        grid.appendChild(this._radioField(idx, 'rotator.port', 'Порт', 'number', rot.port, false));
        block.appendChild(grid);
        return block;
    };

    SettingsModal.prototype._receiverDeviceKey = function(dev) {
        if (!dev || dev.driver === RECEIVER_SIMULATED) {
            return RECEIVER_SIMULATED;
        }
        return [dev.driver || '', dev.serial || '', dev.device_path || ''].join('|');
    };

    SettingsModal.prototype._parseReceiverDeviceKey = function(key) {
        if (!key || key === RECEIVER_NONE) {
            return { driver: '', serial: '', device_path: '', label: '' };
        }
        if (key === RECEIVER_SIMULATED) {
            return { driver: RECEIVER_SIMULATED, serial: '', device_path: '', label: 'Имитатор (simulated)' };
        }
        const parts = key.split('|');
        return {
            driver: parts[0] || '',
            serial: parts[1] || '',
            device_path: parts[2] || '',
            label: ''
        };
    };

    SettingsModal.prototype._formatReceiverOptionLabel = function(dev, offline) {
        const parts = [];
        if (dev.driver) parts.push(dev.driver);
        if (dev.label) parts.push(dev.label);
        if (dev.serial) parts.push('serial ' + dev.serial);
        if (dev.device_path) parts.push(dev.device_path);
        let text = parts.join(' · ');
        if (offline) {
            text = '(не подключён) · ' + text;
        }
        return text || dev.driver || '—';
    };

    SettingsModal.prototype._fillReceiverSelect = function(sel, receiver) {
        sel.innerHTML = '';
        const none = document.createElement('option');
        none.value = RECEIVER_NONE;
        none.textContent = '— не выбран —';
        sel.appendChild(none);

        const simulated = document.createElement('option');
        simulated.value = RECEIVER_SIMULATED;
        simulated.textContent = 'Имитатор (simulated)';
        sel.appendChild(simulated);

        const seen = {};
        (this._sdrDevices || []).forEach((dev) => {
            if (!dev || !dev.driver || dev.driver === RECEIVER_SIMULATED) return;
            const key = this._receiverDeviceKey(dev);
            if (seen[key]) return;
            seen[key] = true;
            const opt = document.createElement('option');
            opt.value = key;
            opt.textContent = this._formatReceiverOptionLabel(dev, false);
            sel.appendChild(opt);
        });

        const savedKey = this._receiverDeviceKey(receiver);
        if (savedKey && savedKey !== RECEIVER_NONE && savedKey !== RECEIVER_SIMULATED && !seen[savedKey]) {
            const offline = document.createElement('option');
            offline.value = savedKey;
            offline.textContent = this._formatReceiverOptionLabel(receiver, true);
            offline.className = 'settings-receiver-option--offline';
            sel.appendChild(offline);
        }

        if (savedKey && savedKey !== RECEIVER_NONE) {
            sel.value = savedKey;
        } else if (receiver && receiver.driver === RECEIVER_SIMULATED) {
            sel.value = RECEIVER_SIMULATED;
        } else {
            sel.value = RECEIVER_NONE;
        }
    };

    SettingsModal.prototype._applyReceiverSelection = function(idx, key) {
        const list = this._workingConfig.station.radio_paths;
        const target = list[idx];
        if (!target) return;
        const parsed = this._parseReceiverDeviceKey(key);
        if (key !== RECEIVER_NONE && key !== RECEIVER_SIMULATED) {
            const dev = (this._sdrDevices || []).find((d) => this._receiverDeviceKey(d) === key);
            if (dev && dev.label) {
                parsed.label = dev.label;
            } else if (target.receiver && target.receiver.label) {
                parsed.label = target.receiver.label;
            }
        }
        target.receiver = parsed;
        this._setDirty(true);
    };

    SettingsModal.prototype._testReceiver = function(idx) {
        const self = this;
        const rp = this._workingConfig.station.radio_paths[idx];
        if (!rp || !rp.receiver || !rp.receiver.driver) {
            this._receiverTestLogs[idx] = 'Выберите приёмник перед тестом.';
            this._renderRadioPaths();
            return;
        }
        const rx = rp.receiver;
        this._receiverTestLogs[idx] = 'Проверка…';
        this._renderRadioPaths();

        fetch('/api/sdr/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                driver: rx.driver,
                serial: rx.serial || '',
                device_path: rx.device_path || ''
            })
        })
            .then((r) => self._parseJSONOrThrow(r))
            .then((data) => {
                self._receiverTestLogs[idx] = (data.lines || []).join('\n');
                self._renderRadioPaths();
            })
            .catch((err) => {
                self._receiverTestLogs[idx] = 'Ошибка теста: ' + err.message;
                self._renderRadioPaths();
            });
    };

    SettingsModal.prototype._radioField = function(idx, subPath, label, type, value, fullWidth) {
        const self = this;
        const lbl = document.createElement('label');
        lbl.className = 'settings-field' + (fullWidth ? ' settings-field--full' : '');
        const span = document.createElement('span');
        span.className = 'settings-field__label';
        span.textContent = label;
        const input = document.createElement('input');
        input.type = type;
        input.className = 'settings-field__input';
        input.value = value == null ? '' : String(value);
        lbl.appendChild(span);
        lbl.appendChild(input);
        input.addEventListener('input', () => {
            self._setRadioPathValue(idx, subPath, type === 'number' ? parseFloat(input.value) : input.value);
            if (subPath === 'name') {
                self._setDirty(true);
            }
        });
        input.addEventListener('change', () => self._setDirty(true));
        return lbl;
    };

    SettingsModal.prototype._radioOptionalNumber = function(idx, subPath, label, value) {
        const self = this;
        const lbl = document.createElement('label');
        lbl.className = 'settings-field';
        const span = document.createElement('span');
        span.className = 'settings-field__label';
        span.textContent = label;
        const input = document.createElement('input');
        input.type = 'number';
        input.step = '0.001';
        input.min = '0';
        input.className = 'settings-field__input';
        input.value = value == null ? '' : String(value);
        lbl.appendChild(span);
        lbl.appendChild(input);
        input.addEventListener('input', () => {
            const raw = input.value.trim();
            if (raw === '') {
                self._setRadioPathValue(idx, subPath, null);
            } else {
                const v = parseFloat(raw);
                self._setRadioPathValue(idx, subPath, Number.isFinite(v) ? v : null);
            }
        });
        input.addEventListener('change', () => self._setDirty(true));
        return lbl;
    };

    SettingsModal.prototype._setRadioPathValue = function(idx, subPath, value) {
        const list = this._workingConfig.station.radio_paths;
        const target = list[idx];
        if (!target) return;
        const path = subPath.split('.');
        let cur = target;
        for (let i = 0; i < path.length - 1; i++) {
            if (cur[path[i]] == null) {
                cur[path[i]] = {};
            }
            cur = cur[path[i]];
        }
        const leaf = path[path.length - 1];
        if (value === null || value === '') {
            delete cur[leaf];
        } else {
            cur[leaf] = value;
        }
        this._setDirty(true);
    };

    SettingsModal.prototype._renderExclusions = function() {
        const container = this._root.querySelector('[data-list="exclusions"]');
        if (!container) return;
        container.innerHTML = '';

        if (!this._exclusions || this._exclusions.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'settings-exclusions__empty';
            empty.textContent = 'Список исключений пуст. Скрыть спутник можно через ПКМ в плане сеансов.';
            container.appendChild(empty);
            return;
        }

        this._exclusions.forEach((ex) => {
            const row = document.createElement('div');
            row.className = 'settings-exclusions__row';

            const id = document.createElement('span');
            id.className = 'settings-exclusions__id';
            id.textContent = String(ex.norad_id);

            const name = document.createElement('span');
            name.textContent = ex.name || '';

            const btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'settings-exclusions__remove';
            btn.textContent = 'Удалить';
            btn.addEventListener('click', () => this._removeExclusion(ex.norad_id));

            row.appendChild(id);
            row.appendChild(name);
            row.appendChild(btn);
            container.appendChild(row);
        });
    };

    SettingsModal.prototype._removeExclusion = function(noradID) {
        const self = this;
        fetch('/api/exclusions/' + noradID, { method: 'DELETE' })
            .then((r) => {
                if (!r.ok && r.status !== 204) {
                    return r.text().then((text) => {
                        throw new Error('HTTP ' + r.status + ': ' + text.substring(0, 200));
                    });
                }
                return null;
            })
            .then(() => {
                self._exclusions = self._exclusions.filter((e) => e.norad_id !== noradID);
                self._renderExclusions();
                self._setStatus('Исключение снято: NORAD ' + noradID, 'success');
            })
            .catch((err) => {
                self._setStatus('Не удалось снять исключение: ' + err.message, 'error');
            });
    };

    SettingsModal.prototype._onFieldInput = function(e) {
        const target = e.target;
        if (!target || !target.dataset || !target.dataset.bind) return;
        const path = target.dataset.bind;

        // Радиотракты — отдельный обработчик в _radioField, тут пропускаем.
        if (path.indexOf('[') !== -1) return;

        const value = readField(target);
        setByPath(this._workingConfig, path, value);
        this._setDirty(true);

        // Live-preview наблюдателя: маркер на карте — без обращения к бэку.
        if (path.startsWith('station.observer.')) {
            // Если поля редактирует пользователь — переходим на «Свои координаты».
            const sel = this._root.querySelector('#settings-observer-city');
            if (sel && sel.value !== CITY_CUSTOM && !this._suppressCityAutoSwitch) {
                this._suppressCityAutoSwitch = true;
                sel.value = CITY_CUSTOM;
                this._setObserverFieldsDisabled(false);
                this._suppressCityAutoSwitch = false;
            }
            this._updateObserverPreview();
        }
    };

    SettingsModal.prototype._updateObserverPreview = function() {
        if (!this._earthView || typeof this._earthView.setObserverPreview !== 'function') return;
        const obs = this._workingConfig.station && this._workingConfig.station.observer;
        if (!obs) return;
        const lat = parseFloat(obs.lat);
        const lon = parseFloat(obs.lon);
        if (!Number.isFinite(lat) || !Number.isFinite(lon)) return;
        this._earthView.setObserverPreview(lon, lat);
    };

    SettingsModal.prototype._setDirty = function(dirty) {
        this._dirty = dirty;
        const saveBtn = this._root.querySelector('[data-action="save-settings"]');
        if (saveBtn) saveBtn.disabled = !dirty;
    };

    SettingsModal.prototype._setStatus = function(text, kind) {
        const status = this._root.querySelector('[data-role="status"]');
        if (!status) return;
        status.textContent = text || '';
        status.classList.remove('is-error', 'is-success');
        if (kind === 'error') status.classList.add('is-error');
        if (kind === 'success') status.classList.add('is-success');
        if (this._statusTimer) {
            clearTimeout(this._statusTimer);
            this._statusTimer = null;
        }
        if (text && kind === 'success') {
            const self = this;
            this._statusTimer = setTimeout(() => self._setStatus('', null), STATUS_RESET_DELAY_MS);
        }
    };

    SettingsModal.prototype._save = function() {
        const self = this;
        this._clearFieldErrors();
        const payload = deepClone(this._workingConfig);

        fetch('/api/settings', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload)
        })
            .then((resp) => {
                if (resp.status === 400) {
                    return resp.json().then((errBody) => {
                        self._showValidationErrors(errBody);
                        throw new Error('validation');
                    });
                }
                return self._parseJSONOrThrow(resp);
            })
            .then((ack) => {
                self._currentConfig = deepClone(self._workingConfig);
                self._setDirty(false);
                if (ack && ack.requires_restart && ack.requires_restart.length > 0) {
                    self._setStatus('Сохранено. Требуется перезапуск: ' + ack.requires_restart.join(', '), 'success');
                } else {
                    self._setStatus('Сохранено', 'success');
                }
                self._applyObserverFrontUpdates();
            })
            .catch((err) => {
                if (err.message !== 'validation') {
                    self._setStatus('Ошибка сохранения: ' + err.message, 'error');
                }
            });
    };

    /**
     * Применяет изменения наблюдателя на фронте без ожидания SSE:
     * — снимает live-preview;
     * — переставляет реальный observer на канвасе (маркер не возвращается
     *   к старому положению до прихода group-update);
     * — обновляет город/координаты в footer (он рендерится сервером один раз
     *   при загрузке страницы).
     */
    SettingsModal.prototype._applyObserverFrontUpdates = function() {
        const obs = this._workingConfig
            && this._workingConfig.station
            && this._workingConfig.station.observer;
        if (!obs) return;

        if (this._earthView) {
            if (typeof this._earthView.clearObserverPreview === 'function') {
                this._earthView.clearObserverPreview();
            }
            if (typeof this._earthView.setObserver === 'function'
                && typeof obs.lon === 'number'
                && typeof obs.lat === 'number') {
                this._earthView.setObserver(obs.lon, obs.lat, obs.name || '');
            }
            if (typeof this._earthView.draw === 'function') {
                this._earthView.draw();
            }
        }

        const cityEl = document.getElementById('app-header-city');
        if (cityEl && obs.name) {
            cityEl.textContent = obs.name;
        }
        const coordsEl = document.getElementById('app-header-coords');
        if (coordsEl && typeof obs.lat === 'number' && typeof obs.lon === 'number') {
            const ns = obs.lat >= 0 ? 'N' : 'S';
            const ew = obs.lon >= 0 ? 'E' : 'W';
            coordsEl.textContent =
                Math.abs(obs.lat).toFixed(2) + '°' + ns + ' ' +
                Math.abs(obs.lon).toFixed(2) + '°' + ew;
        }
    };

    SettingsModal.prototype._showValidationErrors = function(body) {
        if (!body || !body.errors || body.errors.length === 0) {
            this._setStatus('Ошибка валидации', 'error');
            return;
        }
        const messages = [];
        body.errors.forEach((e) => {
            messages.push(e.field + ': ' + e.message);
            const input = this._root.querySelector('[data-bind="' + e.field + '"]');
            if (input) input.classList.add('is-invalid');
        });
        this._setStatus('Проверьте поля: ' + messages.join('; '), 'error');
    };

    SettingsModal.prototype._clearFieldErrors = function() {
        this._root.querySelectorAll('.settings-field__input.is-invalid').forEach((el) => {
            el.classList.remove('is-invalid');
        });
    };

    function escapeHTML(s) {
        if (s == null) return '';
        return String(s).replace(/[&<>"']/g, (c) => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            '"': '&quot;',
            "'": '&#39;'
        }[c]));
    }

    /**
     * Глобальный bootstrap. Привязывает шестерёнку в footer и обрабатывает
     * deep-link `?settings=open` (приходит после редиректа с /settings).
     */
    function bootstrapSettingsModal() {
        const earthView = window.earthView || (window.app && window.app.earthView) || null;
        const modal = new SettingsModal({ earthView: earthView });

        const gear = document.getElementById('app-header-settings');
        if (gear) {
            gear.addEventListener('click', () => modal.open());
        }

        // Deep-link: /settings → редирект на текущую страницу с ?settings=open.
        const params = new URLSearchParams(window.location.search);
        if (params.get('settings') === 'open') {
            modal.open();
        }

        window.SettingsModal = modal;
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', bootstrapSettingsModal);
    } else {
        bootstrapSettingsModal();
    }

    // Экспорт класса для тестов и для возможного программного использования.
    if (typeof module !== 'undefined' && module.exports) {
        module.exports = {
            SettingsModal: SettingsModal,
            getByPath: getByPath,
            setByPath: setByPath,
            parseDurationToNanoseconds: parseDurationToNanoseconds,
            formatNanosecondsToDuration: formatNanosecondsToDuration
        };
    } else {
        window.SettingsModalClass = SettingsModal;
    }
})();
