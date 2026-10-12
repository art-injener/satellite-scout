// manual-layout.js — Ручной режим: сквозной таймлайн и вкладки по режимам SDR
// (ADR-004 §4.3–4.3.1).
//
// Структура DOM:
//   #layout-manual[data-tab]
//     .ml-timeline          → PassTimelineView (горки El×время), виден всегда
//     .ml-tabbar            → Панорама | Спектр | Антенна + строка состояния SDR
//     .ml-txlist            → лента передатчиков (рендерит ManualPanorama), кроме «Антенны»
//     Вкладки (.ml-tabpane):
//       Панорама — SWEEP: панорама (FFT по span + метки TX) и таблица «Эфир»
//       Спектр   — HOLD: FFT + водопад канала, результат приёма
//                  (Таблица | Hex | Диаграмма | Профиль пролёта) и колонка тракта
//       Антенна  — HOLD: Az | спектр канала | El, снизу панель наведения
//
// Вкладки и есть режим SDR: один тюнер либо обходит полосы обзора («Панорама»),
// либо стоит на канале («Спектр» и «Антенна»).
//
// Панель наведения: отсчёт «антенна | КА | расхождение» по осям (структура из
// окна ротатора gpredict, gtk-rot-ctrl.c) + ручное наведение. Поля цели AZ/EL
// сверху показывают заданное положение и обновляются стрелками; SET наводит на
// введённое. Кто цель и идёт ли за ней антенна решает План сеансов, панель читает.
//
// Жизненный цикл: activate()/deactivate() управляют таймером отрисовки.

'use strict';

(function() {
    const SPECTRUM_TICK_MS = 80;
    /** Скорость света для доплеровского сдвига, м/с. */
    const C_LIGHT = 299792458;
    /** Настройка приёмника, пока канал не выбран: диапазон телеметрии 435–437 МГц. */
    const DEFAULT_BAND = { centerMHz: 436, spanMHz: 2 };
    /** Полоса спектра, настроенного на канал передатчика, МГц. */
    const CHANNEL_SPAN_MHZ = 0.192;
    /** Допуск наведения: сверх него рассогласование подсвечивается, градусы. */
    const POINTING_TOLERANCE_DEG = 2;
    const STORAGE_AGC = 'ux.manualRxAgc';
    const STORAGE_ROT_STEP = 'ux.rotStep';
    const STORAGE_ROT_MODE = 'ux.rotMode';
    const STORAGE_REC_DIR = 'ux.manualRecDir';
    /** Шаг крестовины по умолчанию, градусы. */
    const ROT_STEP_DEFAULT_DEG = 1;
    /** Папка записей IQ, пока оператор не задал свою. */
    const REC_DIR_DEFAULT = 'data/recordings/';

    /**
     * Имя файла записи IQ: канал и время старта в UTC, формат SigMF.
     * Пример: ISS-FSK_20261007_180509Z.sigmf-data
     * @param {string} channel
     * @param {Date} at
     * @returns {string}
     */
    function makeRecordingName(channel, at) {
        const safe = String(channel || '').trim()
            .replace(/[^A-Za-z0-9._-]+/g, '_')
            .replace(/^_+|_+$/g, '') || 'iq';
        const p2 = function(n) { return (n < 10 ? '0' : '') + n; };
        const day = at.getUTCFullYear() + p2(at.getUTCMonth() + 1) + p2(at.getUTCDate());
        const time = p2(at.getUTCHours()) + p2(at.getUTCMinutes()) + p2(at.getUTCSeconds());
        return safe + '_' + day + '_' + time + 'Z.sigmf-data';
    }

    /** Демонстрационный AX.25-кадр для hex-вывода до подключения живого демодулятора. */
    function makeDemoFrame() {
        const header = [
            0x86, 0xa2, 0x40, 0x40, 0x40, 0x40, 0x60,
            0x9c, 0x94, 0x6e, 0xa0, 0x40, 0x40, 0x61,
            0x03, 0xf0,
        ];
        const payload = 'ISS TLM V=27.4 I=1.82 T=+18.3 MODE=NOMINAL';
        const frame = header.slice();
        for (let i = 0; i < payload.length; i++) {
            frame.push(payload.charCodeAt(i));
        }
        frame.push(0x4b, 0x2f);
        return frame;
    }

    /** Метка времени (мс) в HH:MM по локальным часам. */
    function formatClock(ms) {
        const d = new Date(ms);
        const pad = function(v) { return (v < 10 ? '0' : '') + v; };
        return pad(d.getHours()) + ':' + pad(d.getMinutes());
    }

    /** Секунды в MM:SS; часы сверх 99 минут показываем как HH:MM:SS. */
    function formatMinSec(totalSec) {
        const pad = function(v) { return (v < 10 ? '0' : '') + v; };
        if (totalSec >= 6000) {
            const h = Math.floor(totalSec / 3600);
            return h + ':' + pad(Math.floor((totalSec % 3600) / 60)) + ':' + pad(totalSec % 60);
        }
        return pad(Math.floor(totalSec / 60)) + ':' + pad(totalSec % 60);
    }

    /**
     * @param {SatelliteStateManager} stateManager
     */
    function ManualLayout(stateManager) {
        this._sm = stateManager || null;
        this._active = false;
        this._spectrumTimer = null;
        this._unsubscribers = [];
        this._resizeObservers = [];
        this._handlers = [];

        this._az = null;
        this._el = null;
        this._fft = null;
        this._wf = null;
        /** Компактный спектр канала на вкладке «Антенна». */
        this._antFft = null;
        this._dataSource = null;
        this._panorama = null;
        this._passProfile = null;
        this._passTimeline = null;
        this._demodPanel = null;
        this._tmiTable = null;
        this._eyeView = null;
        this._onPanoramaTune = null;

        this._activeTab = 'panorama';
        this._tabButtons = [];
        this._tabPanes = {};
        /** Имя передатчика, на который настроен приёмник: из него имя файла записи. */
        this._chanLabel = '';
        /** Номинал и КА этого передатчика: от них считается ожидаемая частота. */
        this._chanFreqMHz = null;
        this._chanNorad = null;
        this._rotPanel = null;
        this._rotGoBtn = null;
        this._rotDtEl = null;
        this._rotAosEl = null;
        this._rotLosEl = null;
        this._rotSlewEl = null;
        this._rotAzSetEl = null;
        this._rotElSetEl = null;
        this._rotTargetAz = 0;
        this._rotTargetEl = 0;
        this._rotStepDeg = ROT_STEP_DEFAULT_DEG;
        this._rotStepButtons = [];
        this._rotNudges = [];
        this._rotFollowing = false;
        this._rotHasRotator = false;
        this._rotMode = 'manual';
        this._rotModeButtons = [];
        this._rotModebarEl = null;
        this._onPanoramaFollow = null;
        this._followFreq = null;
        this._view = 'panorama';
        this._resultTabButtons = [];
        this._resultViews = [];
        this._resultHexEl = null;
        this._resultView = 'table';
        /** Запись IQ: элементы секции «Данные», папка и текущий файл (null — не пишем). */
        this._rec = null;

        this._rxFreqMHz = DEFAULT_BAND.centerMHz;
        this._freqDial = null;
        this._rxMeters = null;
        this._rxPipelineSelect = null;
        this._rxPanelRoot = null;

        this._layoutRoot = document.getElementById('layout-manual');
        this._azCanvas = document.getElementById('manual-azimuth-view');
        this._elCanvas = document.getElementById('manual-elevation-view');
        this._fftCanvas = document.getElementById('manual-fft');
        this._wfCanvas = document.getElementById('manual-wf');
        this._scaleCanvas = document.getElementById('manual-spec-scale');
        this._antCanvas = document.getElementById('manual-ant-spectrum');
        // Тракт без поворотки: на месте Az/El лепесток диаграммы и усиление за пролёт
        this._lobeCanvas = document.getElementById('manual-omni-lobe');
        this._gainCanvas = document.getElementById('manual-omni-gain');
        this._omniKind = null;
        this._omniLobe = null;
        this._omniGain = null;

        this._initTabs();
        this._initDemodResultTabs();
        this._initRecorder();
        this._initIndicators();
        this._initRotPanel();
        this._initSpectrum();
        this._initPanorama();
        this._initPassProfile();
        this._initPassTimeline();
        this._detectAntennaView();
        this._initDemodPanel();
        this._initTelemetryTable();
        this._initEyeConstellation();
        this._initRxPanel();
        this._subscribeState();
    }

    // ── Регистрация обработчиков с авто-очисткой ──────────────────────────

    ManualLayout.prototype._on = function(el, type, fn) {
        if (!el) { return; }
        el.addEventListener(type, fn);
        this._handlers.push([el, type, fn]);
    };

    // ── Вкладки: Панорама (SWEEP) | Спектр (HOLD) | Антенна ───────────────
    //
    // Активная вкладка между сессиями не сохраняется: вход в Ручной всегда
    // открывает «Панораму», иначе перезагрузка страницы молча включала бы приём.

    ManualLayout.prototype._initTabs = function() {
        const tabbar = document.getElementById('manual-tabbar');
        const root = this._layoutRoot;
        if (!tabbar || !root || typeof root.querySelectorAll !== 'function') { return; }
        const self = this;
        this._tabButtons = Array.prototype.slice.call(
            tabbar.querySelectorAll('.ml-tabbar__btn[data-tab]')
        );
        const panes = root.querySelectorAll('.ml-tabpane[data-pane]');
        for (let i = 0; i < panes.length; i++) {
            this._tabPanes[panes[i].getAttribute('data-pane')] = panes[i];
        }
        for (let j = 0; j < this._tabButtons.length; j++) {
            (function(btn) {
                self._on(btn, 'click', function() {
                    const tab = btn.getAttribute('data-tab');
                    if (tab) { self._selectTab(tab); }
                });
            })(this._tabButtons[j]);
        }
        this._showTab(this._activeTab);
    };

    /**
     * Выбор вкладки оператором: «Панорама» включает обзор, «Спектр» и «Антенна» приём.
     * @param {string} tabId — 'panorama' | 'spectrum' | 'antenna'
     */
    ManualLayout.prototype._selectTab = function(tabId) {
        if (tabId === 'antenna') {
            this._enterAntenna();
            return;
        }
        if (tabId === 'panorama' && this._view === 'spectrum' && this._isRecording() &&
            !this._confirm('Идёт запись IQ. Тюнер уйдёт в обзор, и запись остановится. Продолжить?')) {
            return;
        }
        this._setView(tabId);
        this._showTab(tabId);
    };

    /**
     * «Антенна» показывает спектр канала, а SDR один, поэтому вход включает приём.
     * Со «Спектра» канал не меняется. С «Панорамы» приёмник встаёт на выделенный
     * там передатчик, без выделения на последний канал приёма, а если приёма ещё
     * не было, на диапазон по умолчанию.
     */
    ManualLayout.prototype._enterAntenna = function() {
        const fromSweep = this._view !== 'spectrum';
        this._showTab('antenna');
        if (!fromSweep) { return; }
        const tx = this._panorama && typeof this._panorama.getSelectedTx === 'function'
            ? this._panorama.getSelectedTx()
            : null;
        if (tx && tx.freqMHz !== this._chanFreqMHz) {
            this._tuneChannel({ freqMHz: tx.freqMHz, tx: tx }, { keepTab: true });
            return;
        }
        this._setView('spectrum');
    };

    /** Показывает панель вкладки; режим SDR не трогает. */
    ManualLayout.prototype._showTab = function(tabId) {
        this._activeTab = tabId;
        for (let i = 0; i < this._tabButtons.length; i++) {
            const btn = this._tabButtons[i];
            btn.setAttribute('aria-selected',
                btn.getAttribute('data-tab') === tabId ? 'true' : 'false');
        }
        const paneKeys = Object.keys(this._tabPanes);
        for (let k = 0; k < paneKeys.length; k++) {
            this._tabPanes[paneKeys[k]].classList.toggle('is-active', paneKeys[k] === tabId);
        }
        // По атрибуту CSS раскладывает сетку: лента, колонка приёмника
        if (this._layoutRoot && typeof this._layoutRoot.setAttribute === 'function') {
            this._layoutRoot.setAttribute('data-tab', tabId);
        }
        if (this._active) {
            const self = this;
            requestAnimationFrame(function() { self._refreshActiveTab(); });
        }
    };

    /** Подтверждение оператора — отдельным методом, чтобы подменять в тестах. */
    ManualLayout.prototype._confirm = function(message) {
        if (typeof window !== 'undefined' && typeof window.confirm === 'function') {
            return window.confirm(message);
        }
        return true;
    };

    /** Канвасы скрытой панели имеют нулевой размер — пересчитываем после показа. */
    ManualLayout.prototype._refreshActiveTab = function() {
        if (this._activeTab === 'antenna') {
            this._resizeIndicator(this._az, this._azCanvas);
            this._resizeIndicator(this._el, this._elCanvas);
            this._resizeIndicator(this._omniLobe, this._lobeCanvas);
            this._resizeIndicator(this._omniGain, this._gainCanvas);
        }
        if (this._activeTab === 'spectrum') {
            if (this._fft) { this._fft._resize(); }
            if (this._wf) { this._wf.refresh(); }
            if (this._eyeView && typeof this._eyeView.refresh === 'function') {
                this._eyeView.refresh();
            }
            if (this._resultView === 'chart' && this._passProfile) {
                this._passProfile.draw();
            }
        }
    };

    // ── Панель наведения ──────────────────────────────────────────────────
    //
    // Кто цель и идёт ли за ней антенна решает План сеансов: «Сопровождать»
    // ставит tracking-КА, «Сброс» снимает. Панель это читает через StateManager
    // и своего тумблера слежения не заводит — иначе два источника правды.
    // Заданное положение антенны — одно состояние `_rotTargetAz/_rotTargetEl`.

    ManualLayout.prototype._initRotPanel = function() {
        const panel = document.getElementById('manual-rot-panel');
        if (!panel) { return; }
        const self = this;
        this._rotPanel = panel;
        this._rotGoBtn = document.getElementById('manual-rot-go');
        this._rotDtEl = document.getElementById('manual-rot-dt');
        this._rotAosEl = document.getElementById('manual-rot-aos');
        this._rotLosEl = document.getElementById('manual-rot-los');
        this._rotSlewEl = document.getElementById('manual-rot-slew');
        this._rotAzSetEl = document.getElementById('manual-rot-az-set');
        this._rotElSetEl = document.getElementById('manual-rot-el-set');
        this._rotNudges = Array.prototype.slice.call(panel.querySelectorAll('.ml-rot__nudge[data-nudge]'));
        this._rotStepButtons = Array.prototype.slice.call(panel.querySelectorAll('.ml-rot__step[data-step]'));

        let savedStep = null;
        try { savedStep = localStorage.getItem(STORAGE_ROT_STEP); } catch (_e) { /* noop */ }
        const stepNum = parseFloat(savedStep);
        this._setRotStep(isFinite(stepNum) ? stepNum : ROT_STEP_DEFAULT_DEG, true);

        for (let k = 0; k < this._rotStepButtons.length; k++) {
            (function(btn) {
                self._on(btn, 'click', function() {
                    self._setRotStep(parseFloat(btn.getAttribute('data-step')), false);
                });
            })(this._rotStepButtons[k]);
        }
        for (let j = 0; j < this._rotNudges.length; j++) {
            (function(btn) {
                self._on(btn, 'click', function() {
                    self._nudgeTarget(btn.getAttribute('data-nudge'));
                });
            })(this._rotNudges[j]);
        }

        // SET наводит антенну на значения в полях. Enter в поле — то же.
        this._on(this._rotGoBtn, 'click', function() { self._applyExactTarget(); });
        const onEnter = function(e) {
            if (e.key === 'Enter') { self._applyExactTarget(); }
        };
        this._on(this._rotAzSetEl, 'keydown', onEnter);
        this._on(this._rotElSetEl, 'keydown', onEnter);

        // Тумблер режима ротатора (Ручной | Авто) — только для направленного тракта
        this._rotModebarEl = document.getElementById('manual-rot-modebar');
        this._rotModeButtons = Array.prototype.slice.call(panel.querySelectorAll('.ml-rot__modebtn[data-rot-set]'));
        for (let m = 0; m < this._rotModeButtons.length; m++) {
            (function(btn) {
                self._on(btn, 'click', function() {
                    self._setRotMode(btn.getAttribute('data-rot-set'), false);
                });
            })(this._rotModeButtons[m]);
        }
        let savedMode = null;
        try { savedMode = localStorage.getItem(STORAGE_ROT_MODE); } catch (_e) { /* noop */ }
        this._rotMode = savedMode === 'auto' ? 'auto' : 'manual';

        this._detectRotatorKind();

        this._seedTargetFromAntenna();
        this._renderRotTargetFields();
        this._applyRotMode(true);
        this._applyRotAvailability();
    };

    /** Тип тракта: есть ли поворотка. Читаем из ModeManager текущего тракта. */
    ManualLayout.prototype._detectRotatorKind = function() {
        const mm = (typeof window !== 'undefined') ? window._modeManager : null;
        this._rotHasRotator = Boolean(mm && typeof mm.hasRotator === 'function' && mm.hasRotator());
        if (this._rotPanel) {
            this._rotPanel.setAttribute('data-rot-kind', this._rotHasRotator ? 'dir' : 'none');
        }
        if (this._rotModebarEl) { this._rotModebarEl.hidden = !this._rotHasRotator; }
        // На всенаправленном тракте колонку «Антенна» ведёт _updateAntDelta (прочерк),
        // а не индикатор — иначе его draw перезатирает «—» текущим азимутом.
        if (this._az && typeof this._az.setInfoElements === 'function') {
            this._az.setInfoElements({ ant: this._rotHasRotator ? 'manual-az-info-ant' : null, sat: 'manual-az-info-sat' });
        }
        if (this._el && typeof this._el.setInfoElements === 'function') {
            this._el.setInfoElements({ ant: this._rotHasRotator ? 'manual-el-info-ant' : null, sat: 'manual-el-info-sat' });
        }
        // Направление антенны и хост тракта — справочно в шапке тумблера
        const hwEl = document.getElementById('manual-rot-modebar-hw');
        if (hwEl) {
            const rp = (mm && typeof mm.getRadioPath === 'function') ? mm.getRadioPath() : null;
            hwEl.textContent = rp ? (rp.name || '') : '';
        }
    };

    /**
     * Переключение режима ротатора.
     * @param {string} mode — 'auto' | 'manual'.
     * @param {boolean} [silent] — без persist и без сброса сопровождения.
     */
    ManualLayout.prototype._setRotMode = function(mode, silent) {
        const next = mode === 'auto' ? 'auto' : 'manual';
        const prev = this._rotMode;
        this._rotMode = next;
        if (!silent) {
            try { localStorage.setItem(STORAGE_ROT_MODE, next); } catch (_e) { /* noop */ }
        }
        // Уход из Авто снимает сопровождение: в Ручном антенну ведёт оператор.
        if (!silent && prev === 'auto' && next === 'manual' && this._isFollowing()) {
            this._clearFollow();
        }
        this._applyRotMode(silent);
    };

    /** Приводит вид панели и follow-иконки к текущему режиму/типу тракта. */
    ManualLayout.prototype._applyRotMode = function() {
        const auto = this._rotHasRotator && this._rotMode === 'auto';
        for (let i = 0; i < this._rotModeButtons.length; i++) {
            const on = this._rotModeButtons[i].getAttribute('data-rot-set') === this._rotMode;
            this._rotModeButtons[i].classList.toggle('is-on', on);
            this._rotModeButtons[i].setAttribute('aria-pressed', on ? 'true' : 'false');
        }
        if (this._rotPanel) {
            this._rotPanel.setAttribute('data-rot-mode', this._rotMode);
        }
        this._applyRotAvailability();
        this._updateIndicators();
    };

    /** true — антенна должна автоматически вести КА (направленный тракт + Авто). */
    ManualLayout.prototype._isAutoTracking = function() {
        return this._rotHasRotator && this._rotMode === 'auto' && this._isFollowing();
    };

    /** POST /api/tracking/current — взять КА на сопровождение (как в плане сеансов). */
    ManualLayout.prototype._postTracking = function(noradId) {
        const clientId = (typeof window.getClientId === 'function') ? window.getClientId() : '';
        fetch('/api/tracking/current', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Client-Id': clientId },
            body: JSON.stringify({ norad_id: noradId, client_id: clientId }),
        }).catch(function() { /* сеть недоступна — состояние не меняем */ });
    };

    /** POST /api/tracking/reset — снять сопровождение. */
    ManualLayout.prototype._postTrackingReset = function() {
        const clientId = (typeof window.getClientId === 'function') ? window.getClientId() : '';
        fetch('/api/tracking/reset', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Client-Id': clientId },
            body: JSON.stringify({ client_id: clientId }),
        }).catch(function() { /* noop */ });
    };

    /**
     * Клик по follow-иконке передатчика: включить/снять сопровождение его КА.
     * Включение = tune приёмника + tracking КА (§ADR-004 4.4). Повторный клик = сброс.
     * @param {{noradId:number, freqMHz:number, tx:Object}} detail
     */
    ManualLayout.prototype._toggleFollow = function(detail) {
        if (!detail || typeof detail.freqMHz !== 'number') { return; }
        // Ключ по частоте: клик по уже активному передатчику — снять; по другому
        // (даже того же КА) — перестроить приёмник, сопровождение остаётся на его КА.
        if (this._followFreq !== null && Math.abs(detail.freqMHz - this._followFreq) < 1e-6) {
            this._clearFollow();
            return;
        }
        // Сопровождение КА (клиентски + POST для реального ротатора, PHASE-8).
        // Ротатор физически наводится только на направленном тракте в «Авто»
        // (см. _isAutoTracking). На всенаправленном это просто выбор активного КА.
        if (detail.noradId && this._sm && typeof this._sm.setTrackingSatellite === 'function') {
            const name = detail.tx && (detail.tx.label || '');
            this._sm.setTrackingSatellite(detail.noradId, name);
            this._postTracking(detail.noradId);
        }
        this._followFreq = detail.freqMHz;
        if (this._panorama && typeof this._panorama.setFollowedFreq === 'function') {
            this._panorama.setFollowedFreq(this._followFreq);
        }
        // Настроить приёмник на передатчик — главное, ради чего клик (§4.6).
        this._tuneChannel(detail);
    };

    /** Снять сопровождение: клиентски + POST + сброс подсветки. */
    ManualLayout.prototype._clearFollow = function() {
        this._followFreq = null;
        if (this._panorama && typeof this._panorama.setFollowedFreq === 'function') {
            this._panorama.setFollowedFreq(null);
        }
        if (this._sm && typeof this._sm.clearTrackingSatellite === 'function') {
            this._sm.clearTrackingSatellite();
        }
        this._postTrackingReset();
        this._setView('panorama');
    };

    /** Печатает заданное положение в поля цели (обновляется стрелками и SET). */
    ManualLayout.prototype._renderRotTargetFields = function() {
        if (this._rotAzSetEl && document.activeElement !== this._rotAzSetEl) {
            this._rotAzSetEl.value = this._rotTargetAz.toFixed(1);
            this._rotAzSetEl.parentNode.classList.remove('ml-rot__input-cell--bad');
        }
        if (this._rotElSetEl && document.activeElement !== this._rotElSetEl) {
            this._rotElSetEl.value = this._rotTargetEl.toFixed(1);
            this._rotElSetEl.parentNode.classList.remove('ml-rot__input-cell--bad');
        }
    };

    /**
     * Mock-передатчики привязываем к реальным КА текущей группы: иначе «Следить»
     * ведёт КА без позиции — антенна не движется, AOS/LOS пусты. Частота и модуляция
     * остаются шаблонными (до TX-DB-001). Primary-КА получает два передатчика,
     * чтобы был виден кейс «тот же КА, другая частота».
     */
    ManualLayout.prototype._buildTransmittersFromGroup = function() {
        if (!this._panorama || typeof this._panorama.setTransmitters !== 'function') { return; }
        if (!this._sm || typeof this._sm.getSatelliteGroup !== 'function') { return; }
        const group = this._sm.getSatelliteGroup();
        if (!group || !Array.isArray(group.satellites) || group.satellites.length === 0) { return; }

        const templates = [
            { freqMHz: 145.825, modulation: 'FM', chipColor: '#00ffc8', active: true, suffix: 'VOICE' },
            { freqMHz: 437.800, modulation: 'FSK 1k2', chipColor: '#d070ff', active: true, suffix: 'FSK' },
            { freqMHz: 435.250, modulation: 'BPSK31', chipColor: '#ffaa22', active: true, suffix: 'BPSK' },
            { freqMHz: 145.920, modulation: 'CW', chipColor: '#8fa8bc', active: false, suffix: 'CW' },
            { freqMHz: 437.100, modulation: 'APT', chipColor: '#8fa8bc', active: false, suffix: 'APT' },
        ];
        const sats = group.satellites.slice();
        const list = [];
        let ti = 0;
        for (let i = 0; i < sats.length && ti < templates.length; i++) {
            const sat = sats[i];
            const name = sat.sat_alias || sat.sat_name || ('NORAD ' + sat.norad_id);
            // Первый КА группы — два передатчика (кейс «тот же КА, другая частота»),
            // остальные по одному.
            const count = (i === 0 && templates.length >= 2) ? 2 : 1;
            for (let c = 0; c < count && ti < templates.length; c++) {
                const t = templates[ti++];
                list.push({
                    freqMHz: t.freqMHz, modulation: t.modulation, chipColor: t.chipColor,
                    active: t.active, noradId: sat.norad_id,
                    label: name + ' · ' + t.suffix,
                });
            }
        }
        if (list.length) { this._panorama.setTransmitters(list); }
    };

    /** true, если План сеансов держит КА под сопровождением. */
    ManualLayout.prototype._isFollowing = function() {
        if (!this._sm || typeof this._sm.getTrackingSatelliteId !== 'function') { return false; }
        return Boolean(this._sm.getTrackingSatelliteId());
    };

    /**
     * Заданное положение = текущее положение антенны. Нужно при старте и при снятии
     * сопровождения: иначе заданное осталось бы от прошлого состояния и следующая
     * команда дёрнула бы антенну в устаревшую точку.
     */
    ManualLayout.prototype._seedTargetFromAntenna = function() {
        if (this._az && typeof this._az.currentAzimuth === 'number') {
            this._rotTargetAz = this._az.currentAzimuth;
        }
        if (this._el && typeof this._el.currentElevation === 'number') {
            this._rotTargetEl = this._el.currentElevation;
        }
    };

    /** Ручное наведение доступно, только пока сопровождение снято. */
    ManualLayout.prototype._applyRotAvailability = function() {
        // Ручное наведение доступно только на направленном тракте в режиме «Ручной».
        // На всенаправленном наводить нечем; в «Авто» антенна ведёт КА сама.
        const manualAllowed = this._rotHasRotator && this._rotMode === 'manual';
        for (let i = 0; i < (this._rotNudges || []).length; i++) {
            this._rotNudges[i].disabled = !manualAllowed;
        }
        for (let k = 0; k < (this._rotStepButtons || []).length; k++) {
            this._rotStepButtons[k].disabled = !manualAllowed;
        }
        if (this._rotGoBtn) { this._rotGoBtn.disabled = !manualAllowed; }
        if (this._rotAzSetEl) { this._rotAzSetEl.disabled = !manualAllowed; }
        if (this._rotElSetEl) { this._rotElSetEl.disabled = !manualAllowed; }
        // Почему крестовина заблокирована — только во всплывающей подсказке, не на экране
        if (this._rotSlewEl) {
            this._rotSlewEl.title = manualAllowed ? '' : (this._rotHasRotator ? 'Авто' : 'Неподвижная');
        }
        if (this._rotPanel) {
            this._rotPanel.setAttribute('data-rot-follow', this._isAutoTracking() ? 'on' : 'off');
        }
    };

    /**
     * Выбор шага крестовины.
     * @param {number} stepDeg
     * @param {boolean} [silent] — без persist (при восстановлении).
     */
    ManualLayout.prototype._setRotStep = function(stepDeg, silent) {
        const allowed = [];
        for (let i = 0; i < this._rotStepButtons.length; i++) {
            allowed.push(parseFloat(this._rotStepButtons[i].getAttribute('data-step')));
        }
        const step = allowed.indexOf(stepDeg) >= 0 ? stepDeg : ROT_STEP_DEFAULT_DEG;
        this._rotStepDeg = step;
        if (!silent) {
            try { localStorage.setItem(STORAGE_ROT_STEP, String(step)); } catch (_e) { /* noop */ }
        }
        for (let j = 0; j < this._rotStepButtons.length; j++) {
            const btn = this._rotStepButtons[j];
            const on = parseFloat(btn.getAttribute('data-step')) === step;
            btn.classList.toggle('is-on', on);
            btn.setAttribute('aria-pressed', on ? 'true' : 'false');
        }
    };

    /**
     * Шаг крестовиной по заданному положению антенны.
     * @param {string} dir — 'az+' | 'az-' | 'el+' | 'el-'.
     */
    ManualLayout.prototype._nudgeTarget = function(dir) {
        const azimuth = dir.charAt(0) === 'a';
        const sign = dir.charAt(dir.length - 1) === '+' ? 1 : -1;
        const delta = sign * this._rotStepDeg;
        if (azimuth) {
            this._rotTargetAz = ((this._rotTargetAz + delta) % 360 + 360) % 360;
        } else {
            this._rotTargetEl = Math.max(0, Math.min(90, this._rotTargetEl + delta));
        }
        this._applyRotTarget();
    };

    /** Навести на значения в полях (кнопка SET / Enter). */
    ManualLayout.prototype._applyExactTarget = function() {
        const az = this._parseAngle(this._rotAzSetEl, 0, 360);
        const el = this._parseAngle(this._rotElSetEl, 0, 90);
        if (az === false || el === false) { return; }
        if (az !== null) { this._rotTargetAz = az % 360; }
        if (el !== null) { this._rotTargetEl = el; }
        if (this._rotAzSetEl) { this._rotAzSetEl.blur(); }
        if (this._rotElSetEl) { this._rotElSetEl.blur(); }
        this._applyRotTarget();
    };

    /**
     * Разбирает угол из поля ввода; помечает ячейку при ошибке.
     * @returns {?number|boolean} число, null для пустого поля, false при ошибке.
     */
    ManualLayout.prototype._parseAngle = function(input, min, max) {
        if (!input) { return null; }
        const cell = input.parentNode;
        if (cell) { cell.classList.remove('ml-rot__input-cell--bad'); }
        const raw = String(input.value).trim().replace(',', '.');
        if (!raw) { return null; }
        const val = parseFloat(raw);
        if (!isFinite(val) || val < min || val > max) {
            if (cell) { cell.classList.add('ml-rot__input-cell--bad'); }
            return false;
        }
        return val;
    };

    /** Ставит антенну в заданное положение — отсчёт и расхождение пересчитываются. */
    ManualLayout.prototype._applyRotTarget = function() {
        if (this._az) {
            this._az.setAzimuth(this._rotTargetAz);
            this._az.draw();
        }
        if (this._el) {
            this._el.setPosition(this._rotTargetAz, this._rotTargetEl);
            this._el.draw();
        }
        this._renderRotTargetFields();
        this._refreshPointingTable();
    };

    /** Перерисовывает расхождение по состоянию индикаторов. */
    ManualLayout.prototype._refreshPointingTable = function() {
        const satAz = this._az ? this._az.satelliteAzimuth : null;
        const satEl = this._el ? this._el.satelliteElevation : null;
        this._updateAntDelta(satAz, satEl);
    };

    /**
     * Строка «Пролёт» таблицы: время AOS/LOS и остаток. aos/los — в миллисекундах,
     * сравниваются с локальными часами так же, как в плане сеансов (right-panel.js).
     * @param {?number} noradId
     */
    ManualLayout.prototype._renderRotTargetInfo = function(noradId) {
        if (!this._rotDtEl) { return; }
        const label = document.getElementById('manual-rot-dt-lbl');
        const pass = noradId ? this._findPassInfo(noradId) : null;
        const aosMs = pass && pass.aos ? pass.aos : 0;
        const losMs = pass && pass.los ? pass.los : 0;

        if (this._rotAosEl) { this._rotAosEl.textContent = aosMs ? formatClock(aosMs) : '--:--'; }
        if (this._rotLosEl) { this._rotLosEl.textContent = losMs ? formatClock(losMs) : '--:--'; }

        if (!losMs) {
            this._rotDtEl.textContent = '--:--';
            if (label) { label.textContent = 'осталось'; }
            return;
        }
        const now = this._nowMs();
        const beforePass = aosMs && now < aosMs;
        if (label) { label.textContent = beforePass ? 'до AOS' : 'осталось'; }
        const leftSec = Math.round(((beforePass ? aosMs : losMs) - now) / 1000);
        this._rotDtEl.textContent = leftSec <= 0 ? '00:00' : formatMinSec(leftSec);
    };

    /** Данные пролёта для КА из последней группы. */
    ManualLayout.prototype._findPassInfo = function(noradId) {
        if (!this._sm || typeof this._sm.getSatelliteGroup !== 'function') { return null; }
        const group = this._sm.getSatelliteGroup();
        if (!group || !Array.isArray(group.satellites)) { return null; }
        for (let i = 0; i < group.satellites.length; i++) {
            if (group.satellites[i].norad_id === noradId) { return group.satellites[i]; }
        }
        return null;
    };

    /** Время в миллисекундах — отдельным методом, чтобы подменять в тестах. */
    ManualLayout.prototype._nowMs = function() {
        return Date.now();
    };

    // ── Результат приёма: переключение «Таблица | Hex» ─────────────────────

    ManualLayout.prototype._initDemodResultTabs = function() {
        const tabs = document.getElementById('manual-result-tabs');
        if (!tabs) { return; }
        this._resultHexEl = document.getElementById('manual-demod-hex');
        const buttons = Array.prototype.slice.call(
            tabs.querySelectorAll('.ml-demod__result-tab[data-view]')
        );
        const pane = tabs.closest ? tabs.closest('.ml-demod__col--result') : null;
        const views = pane
            ? Array.prototype.slice.call(pane.querySelectorAll('.ml-demod__result-view[data-view]'))
            : [];
        this._resultTabButtons = buttons;
        this._resultViews = views;

        const self = this;
        for (let i = 0; i < buttons.length; i++) {
            (function(btn) {
                self._on(btn, 'click', function() {
                    self._switchResultView(btn.getAttribute('data-view'));
                });
            })(buttons[i]);
        }
        this._renderHexDump();
    };

    /** @param {string} view — 'table' | 'hex' | 'chart' */
    ManualLayout.prototype._switchResultView = function(view) {
        let i;
        for (i = 0; i < this._resultTabButtons.length; i++) {
            const btn = this._resultTabButtons[i];
            btn.setAttribute('aria-selected',
                btn.getAttribute('data-view') === view ? 'true' : 'false');
        }
        for (i = 0; i < this._resultViews.length; i++) {
            const v = this._resultViews[i];
            v.classList.toggle('is-active', v.getAttribute('data-view') === view);
        }
        this._resultView = view;
        // Скрытый канвас имеет нулевой размер — пересчитываем после показа
        if (typeof requestAnimationFrame !== 'function') { return; }
        const self = this;
        if (view !== 'chart') { return; }
        // Вид «Диаграмма»: глазковая диаграмма и профиль пролёта рядом
        requestAnimationFrame(function() {
            if (self._eyeView && typeof self._eyeView._resize === 'function') {
                self._eyeView._resize();
            }
            if (self._passProfile) { self._passProfile.draw(); }
        });
    };

    /**
     * Дамп кадра: смещение, 16 байт hex, ASCII-колонка.
     * @param {Array<number>|Uint8Array} [bytes] — при отсутствии рисуется демо-кадр
     */
    ManualLayout.prototype._renderHexDump = function(bytes) {
        if (!this._resultHexEl) { return; }
        const data = bytes || makeDemoFrame();
        const lines = [];
        for (let off = 0; off < data.length; off += 16) {
            let hex = '';
            let ascii = '';
            for (let i = 0; i < 16; i++) {
                if (off + i < data.length) {
                    const b = data[off + i] & 0xff;
                    hex += (b < 16 ? '0' : '') + b.toString(16) + ' ';
                    ascii += (b >= 0x20 && b < 0x7f) ? String.fromCharCode(b) : '.';
                } else {
                    hex += '   ';
                }
                if (i === 7) { hex += ' '; }
            }
            const offStr = '0000' + off.toString(16);
            lines.push(offStr.slice(-4) + '  ' + hex + ' |' + ascii + '|');
        }
        this._resultHexEl.textContent = lines.join('\n');
    };

    // ── Запись IQ (mock до live SDR) ───────────────────────────────────────

    /**
     * Секция «Данные»: папка записи и транспорт ● / ■. Пока запись не идёт,
     * в поле папка и её можно править; во время записи — полный путь файла,
     * имя которого сгенерировано в момент старта.
     */
    ManualLayout.prototype._initRecorder = function() {
        const pathEl = document.getElementById('manual-rec-path');
        const startBtn = document.getElementById('manual-rec-start');
        const stopBtn = document.getElementById('manual-rec-stop');
        if (!pathEl || !startBtn || !stopBtn) { return; }

        let dir = null;
        try { dir = localStorage.getItem(STORAGE_REC_DIR); } catch (_e) { /* noop */ }
        this._rec = { pathEl: pathEl, startBtn: startBtn, stopBtn: stopBtn,
            dir: dir || REC_DIR_DEFAULT, file: null };

        const self = this;
        this._on(pathEl, 'change', function() {
            if (self._rec.file) { return; }
            self._rec.dir = String(pathEl.value || '').trim() || REC_DIR_DEFAULT;
            try { localStorage.setItem(STORAGE_REC_DIR, self._rec.dir); } catch (_e) { /* noop */ }
            self._renderRecorder();
        });
        this._on(startBtn, 'click', function() { self._startRecording(); });
        this._on(stopBtn, 'click', function() { self._stopRecording(); });
        this._renderRecorder();
    };

    ManualLayout.prototype._startRecording = function() {
        const rec = this._rec;
        if (!rec || rec.file) { return; }
        const name = makeRecordingName(this._chanLabel, new Date());
        rec.file = (/[/\\]$/.test(rec.dir) ? rec.dir : rec.dir + '/') + name;
        this._renderRecorder();
    };

    ManualLayout.prototype._stopRecording = function() {
        if (!this._rec || !this._rec.file) { return; }
        this._rec.file = null;
        this._renderRecorder();
    };

    /** @returns {boolean} */
    ManualLayout.prototype._isRecording = function() {
        return !!(this._rec && this._rec.file);
    };

    ManualLayout.prototype._renderRecorder = function() {
        const rec = this._rec;
        if (!rec) { return; }
        const on = !!rec.file;
        rec.pathEl.value = on ? rec.file : rec.dir;
        rec.pathEl.readOnly = on;
        rec.pathEl.title = rec.pathEl.value;
        // Путь длиннее поля: прокручиваем к концу, чтобы было видно имя файла
        rec.pathEl.scrollLeft = on ? (rec.pathEl.scrollWidth || 0) : 0;
        rec.startBtn.disabled = on;
        rec.startBtn.setAttribute('aria-pressed', on ? 'true' : 'false');
        rec.stopBtn.disabled = !on;
    };

    // ── Режим SDR: обзор (SWEEP) или приём канала (HOLD), ADR-004 §4.4 ─────

    /**
     * Переключение обзора и приёма. 'panorama' — панорама сканирует, спектр/водопад
     * выключены и очищены; 'spectrum' — панорама заморожена, спектр/водопад активны.
     * Вкладка следует за режимом; приём можно смотреть и на «Антенне», обзор — нет.
     * @param {string} view — 'panorama' | 'spectrum'.
     */
    ManualLayout.prototype._setView = function(view) {
        const next = view === 'spectrum' ? 'spectrum' : 'panorama';
        this._view = next;
        // Тюнер уходит с канала — писать больше нечего
        if (next === 'panorama') { this._stopRecording(); }
        const holdOnAntenna = next === 'spectrum' && this._activeTab === 'antenna';
        if (this._activeTab !== next && !holdOnAntenna) {
            this._showTab(next);
        }

        if (next === 'spectrum') {
            // HOLD: панорама стоит, спектр/водопад идут
            if (this._panorama) { this._panorama.deactivate(); }
            if (this._active) {
                this._startSpectrumTimer();
                if (this._wf) { this._wf.start(); }
            }
        } else {
            // SWEEP: спектр/водопад выключены и очищены, панорама сканирует
            this._stopSpectrumTimer();
            if (this._wf) { this._wf.clear(); }
            if (this._fftCanvas && typeof this._fftCanvas.getContext === 'function') {
                const ctx = this._fftCanvas.getContext('2d');
                if (ctx && typeof ctx.clearRect === 'function') {
                    ctx.clearRect(0, 0, this._fftCanvas.width, this._fftCanvas.height);
                }
            }
            if (this._panorama && this._active) { this._panorama.activate(); }
        }
    };

    ManualLayout.prototype._initRxPanel = function() {
        const root = document.getElementById('manual-rx-panel');
        if (!root) { return; }
        this._rxPanelRoot = root;
        this._rxPipelineSelect = document.getElementById('manual-rx-pipeline');
        this._rxMeters = {
            rfBar: document.getElementById('manual-rx-rf-bar'),
            rfVal: document.getElementById('manual-rx-rf-val'),
            audioBar: document.getElementById('manual-rx-audio-bar'),
            audioVal: document.getElementById('manual-rx-audio-val'),
        };
        const self = this;

        this._initFreqDial();
        this._updateRxMeters(-67.1, -21.1);

        this._initPill(document.getElementById('manual-rx-agc'), STORAGE_AGC, false);

        this._on(this._rxPipelineSelect, 'change', function() {
            self._syncEyeFromPipeline();
        });
        this._on(document.getElementById('manual-rx-apply'), 'click', function() {
            self._syncEyeFromPipeline();
        });

        this._syncEyeFromPipeline();
    };

    /**
     * Индикатор-регулятор частоты (freq-dial.js) — единственный орган
     * управления настройкой: он же и показывает её. Шаг задаёт разряд под
     * курсором, поэтому отдельные поле ввода, единица и селектор шага убраны.
     */
    ManualLayout.prototype._initFreqDial = function() {
        const host = document.getElementById('manual-rx-dial');
        if (!host || typeof window.FreqDial !== 'function') { return; }
        const self = this;
        this._freqDial = new window.FreqDial(host, {
            hz: Math.round(this._rxFreqMHz * 1e6),
            ariaLabel: 'Частота настройки приёмника, Гц',
            onChange: function(hz) {
                self._rxFreqMHz = hz / 1e6;
                self._onOperatorRetune(self._rxFreqMHz);
            },
        });
    };

    /**
     * Ручная перестройка оператором: сдвигаем центр спектра/водопада за
     * индикатором, чтобы канал не «уезжал» из окна.
     * @param {number} freqMHz
     */
    ManualLayout.prototype._onOperatorRetune = function(freqMHz) {
        if (this._dataSource) { this._dataSource.freqCenterMHz = freqMHz; }
        if (this._fft) { this._fft.freqCenterMHz = freqMHz; }
        if (this._antFft) { this._antFft.freqCenterMHz = freqMHz; }
        if (this._wf) { this._wf._freqCenterMHz = freqMHz; }
    };

    /**
     * Обновляет индикаторы уровней. Диапазон шкалы: −120…0 dBFS.
     * @param {number} rfDbfs
     * @param {number} audioDb
     */
    ManualLayout.prototype._updateRxMeters = function(rfDbfs, audioDb) {
        const m = this._rxMeters;
        if (!m) { return; }
        function pct(db) {
            if (typeof db !== 'number' || !isFinite(db)) { return 0; }
            const p = (db + 120) / 120 * 100;
            return Math.max(0, Math.min(100, p));
        }
        if (m.rfBar) { m.rfBar.style.width = pct(rfDbfs).toFixed(1) + '%'; }
        if (m.rfVal && typeof rfDbfs === 'number' && isFinite(rfDbfs)) {
            m.rfVal.textContent = rfDbfs.toFixed(1) + ' dBFS';
        }
        if (m.audioBar) { m.audioBar.style.width = pct(audioDb).toFixed(1) + '%'; }
        if (m.audioVal && typeof audioDb === 'number' && isFinite(audioDb)) {
            m.audioVal.textContent = audioDb.toFixed(1) + ' dB';
        }
    };

    ManualLayout.prototype._initPill = function(btn, key, defaultOn) {
        if (!btn) { return; }
        let on = defaultOn;
        try {
            const raw = localStorage.getItem(key);
            if (raw !== null) { on = raw === '1' || raw === 'true'; }
        } catch (_e) { /* noop */ }
        btn.classList.toggle('is-on', on);
        btn.setAttribute('aria-pressed', on ? 'true' : 'false');
        this._on(btn, 'click', function() {
            const next = btn.getAttribute('aria-pressed') !== 'true';
            btn.classList.toggle('is-on', next);
            btn.setAttribute('aria-pressed', next ? 'true' : 'false');
            try { localStorage.setItem(key, next ? '1' : '0'); } catch (_e) { /* noop */ }
        });
    };

    /** Настройка извне (панорама, TX-список) — индикатор просто следует за ней. */
    ManualLayout.prototype._setRxFreqMHz = function(freqMHz) {
        if (typeof freqMHz !== 'number' || !isFinite(freqMHz)) { return; }
        this._rxFreqMHz = freqMHz;
        if (this._freqDial) { this._freqDial.setMHz(freqMHz); }
    };

    ManualLayout.prototype._setRxPipeline = function(modulation) {
        const sel = this._rxPipelineSelect;
        if (!sel || !modulation) { return; }
        const target = String(modulation).trim().toLowerCase();
        const targetKey = target.replace(/\s+/g, '');
        const families = ['bpsk', 'afsk', 'gmsk', 'qpsk', 'fsk'];
        for (let i = 0; i < sel.options.length; i++) {
            const opt = sel.options[i];
            if (!opt.text) { continue; }
            const optKey = opt.text.toLowerCase().replace(/\s+/g, '');
            if (optKey === targetKey || targetKey.indexOf(optKey) >= 0 || optKey.indexOf(targetKey) >= 0) {
                sel.selectedIndex = i;
                return;
            }
        }
        for (let f = 0; f < families.length; f++) {
            if (targetKey.indexOf(families[f]) < 0) { continue; }
            for (let j = 0; j < sel.options.length; j++) {
                if (!sel.options[j].text) { continue; }
                if (sel.options[j].text.toLowerCase().replace(/\s+/g, '').indexOf(families[f]) >= 0) {
                    sel.selectedIndex = j;
                    return;
                }
            }
        }
    };

    ManualLayout.prototype._getPipelineLabel = function() {
        const sel = this._rxPipelineSelect || document.getElementById('manual-rx-pipeline');
        if (sel && sel.options && sel.selectedIndex >= 0) {
            const opt = sel.options[sel.selectedIndex];
            return opt && opt.text ? String(opt.text).trim() : '';
        }
        return '';
    };

    ManualLayout.prototype._syncEyeFromPipeline = function() {
        if (!this._eyeView) { return; }
        const pipeline = this._getPipelineLabel();
        if (pipeline) { this._eyeView.setModulation(pipeline); }
    };

    // ── Инициализация виджетов ────────────────────────────────────────────

    ManualLayout.prototype._initIndicators = function() {
        const self = this;
        if (this._azCanvas && typeof window.AzimuthIndicator === 'function') {
            this._az = new window.AzimuthIndicator(this._azCanvas);
            this._az.setInfoElements({
                ant: 'manual-az-info-ant',
                sat: 'manual-az-info-sat',
            });
            this._observeWrap(this._azCanvas, function() {
                self._resizeIndicator(self._az, self._azCanvas);
            });
        }
        if (this._elCanvas && typeof window.ElevationIndicator === 'function') {
            this._el = new window.ElevationIndicator(this._elCanvas);
            this._el.setInfoElements({
                ant: 'manual-el-info-ant',
                sat: 'manual-el-info-sat',
            });
            this._observeWrap(this._elCanvas, function() {
                self._resizeIndicator(self._el, self._elCanvas);
            });
        }
    };

    /**
     * Вид вкладки «Антенна» по тракту: без поворотки и с известной конструкцией —
     * лепесток и усиление за пролёт, иначе индикаторы Az/El. Конфиг станции
     * приходит позже конструктора, поэтому вызывается и при activate().
     */
    ManualLayout.prototype._detectAntennaView = function() {
        const mm = (typeof window !== 'undefined') ? window._modeManager : null;
        const model = (typeof window !== 'undefined') ? window.OmniAntenna : null;
        const kind = mm && typeof mm.hasRotator === 'function' && !mm.hasRotator() &&
            typeof mm.antennaKind === 'function' ? mm.antennaKind() : null;
        this._omniKind = model && model.isOmniKind(kind) ? kind : null;

        if (this._omniKind && !this._omniLobe) { this._initOmniViews(); }
        if (this._omniLobe) { this._omniLobe.setKind(this._omniKind); }
        if (this._omniGain) { this._omniGain.setKind(this._omniKind); }

        const pane = this._tabPanes.antenna;
        if (pane && typeof pane.setAttribute === 'function') {
            pane.setAttribute('data-ant-view', this._omniKind ? 'omni' : 'dir');
        }
    };

    ManualLayout.prototype._initOmniViews = function() {
        const self = this;
        if (this._lobeCanvas && typeof window.OmniLobeView === 'function') {
            this._omniLobe = new window.OmniLobeView(this._lobeCanvas);
            this._observeWrap(this._lobeCanvas, function() {
                self._resizeIndicator(self._omniLobe, self._lobeCanvas);
            });
        }
        if (this._gainCanvas && typeof window.OmniGainView === 'function') {
            this._omniGain = new window.OmniGainView(this._gainCanvas);
            this._observeWrap(this._gainCanvas, function() {
                self._resizeIndicator(self._omniGain, self._gainCanvas);
            });
        }
    };

    /**
     * Направление на КА и его пролёт для вида без поворотки.
     * @param {?number} noradId
     * @param {?number} az
     * @param {?number} el
     */
    ManualLayout.prototype._updateOmni = function(noradId, az, el) {
        if (!this._omniLobe && !this._omniGain) { return; }
        const model = window.OmniAntenna;
        if (this._omniLobe) {
            this._omniLobe.setSatellite(az, el);
            this._omniLobe.draw();
        }
        if (this._omniGain) {
            const pass = noradId && model ? model.skyPathPass(this._findPassInfo(noradId)) : null;
            this._omniGain.setSatellite(az, el);
            this._omniGain.setPass(pass);
            this._omniGain.draw();
        }
    };

    ManualLayout.prototype._initSpectrum = function() {
        if (typeof window.SpectrumDataSource !== 'function' ||
            typeof window.FFTSpectrumView !== 'function' ||
            typeof window.WaterfallView !== 'function') {
            return;
        }
        this._dataSource = new window.SpectrumDataSource({
            bins: 512,
            freqCenterMHz: DEFAULT_BAND.centerMHz,
            freqSpanMHz: DEFAULT_BAND.spanMHz,
        });
        if (this._fftCanvas) {
            this._fft = new window.FFTSpectrumView(this._fftCanvas, {
                freqCenterMHz: this._dataSource.freqCenterMHz,
                freqSpanMHz: this._dataSource.freqSpanMHz,
            });
        }
        if (this._antCanvas) {
            this._antFft = new window.FFTSpectrumView(this._antCanvas, {
                freqCenterMHz: this._dataSource.freqCenterMHz,
                freqSpanMHz: this._dataSource.freqSpanMHz,
            });
        }
        if (this._wfCanvas) {
            const ml = this._fft ? this._fft._marginLeft : 32;
            const mr = this._fft ? this._fft._marginRight : 4;
            this._wf = new window.WaterfallView(this._wfCanvas, this._scaleCanvas, {
                freqCenterMHz: this._dataSource.freqCenterMHz,
                freqSpanMHz: this._dataSource.freqSpanMHz,
                marginLeft: ml,
                marginRight: mr,
            });
            this._wf.clear();
            this._wf.start();
        }
        const self = this;
        const specWrap = this._fftCanvas ? this._fftCanvas.closest('.ml-spectrum') : null;
        if (specWrap) {
            this._observeRaw(specWrap, function() {
                if (self._fft) { self._fft._resize(); }
                if (self._wf) { self._wf.refresh(); }
            });
        }
    };

    ManualLayout.prototype._initPanorama = function() {
        if (typeof window.ManualPanorama !== 'function') { return; }
        this._panorama = new window.ManualPanorama();
    };

    ManualLayout.prototype._initPassProfile = function() {
        if (typeof window.PassProfileView !== 'function') { return; }
        const canvas = document.getElementById('manual-pass-profile-canvas');
        if (!canvas) { return; }
        this._passProfile = new window.PassProfileView(canvas, {
            satName: 'ISS',
            noradId: 25544,
            maxEl: 67,
            maxDopplerHz: 3000,
        });
        const self = this;
        if (typeof ResizeObserver !== 'undefined') {
            const ro = new ResizeObserver(function() {
                if (self._passProfile && self._active) { self._passProfile.draw(); }
            });
            ro.observe(canvas.parentElement || canvas);
            this._resizeObservers.push(ro);
        }
    };

    ManualLayout.prototype._initPassTimeline = function() {
        if (typeof window.PassTimelineView !== 'function') { return; }
        const canvas = document.getElementById('manual-pass-timeline');
        if (!canvas) { return; }
        this._passTimeline = new window.PassTimelineView(canvas);
        const self = this;
        if (typeof ResizeObserver !== 'undefined') {
            const ro = new ResizeObserver(function() {
                if (self._passTimeline && self._active) { self._passTimeline.draw(); }
            });
            ro.observe(canvas.parentElement || canvas);
            this._resizeObservers.push(ro);
        }
    };

    ManualLayout.prototype._initDemodPanel = function() {
        if (typeof window.DemodPanel !== 'function') { return; }
        this._demodPanel = new window.DemodPanel();
        const root = this._layoutRoot || document.getElementById('layout-manual');
        if (!root) { return; }
        const self = this;
        this._onPanoramaTune = function(e) {
            self._tuneChannel(e.detail || {});
        };
        root.addEventListener('panorama:tune', this._onPanoramaTune);
        this._onPanoramaFollow = function(e) {
            self._toggleFollow(e.detail || {});
        };
        root.addEventListener('panorama:follow', this._onPanoramaFollow);
    };

    ManualLayout.prototype._initTelemetryTable = function() {
        if (typeof window.TelemetryTable !== 'function') { return; }
        this._tmiTable = new window.TelemetryTable();
    };

    ManualLayout.prototype._initEyeConstellation = function() {
        if (typeof window.EyeConstellationView !== 'function') { return; }
        const self = this;
        this._eyeView = new window.EyeConstellationView();
        this._eyeView._onAutoRestore = function() {
            self._syncEyeFromPipeline();
        };
    };

    // ── Перенастройка канала (клик по TX на панораме → § 4.6) ──────────────

    /**
     * @param {Object} detail — { freqMHz, tx }
     * @param {Object} [opts] — keepTab: остаться на текущей вкладке приёма («Антенна»)
     */
    ManualLayout.prototype._tuneChannel = function(detail, opts) {
        const freqMHz = detail.freqMHz;
        const tx = detail.tx || null;
        if (typeof freqMHz !== 'number' || !isFinite(freqMHz)) { return; }

        if (this._demodPanel && tx) {
            this._demodPanel.setChannel(tx);
        } else if (this._demodPanel) {
            this._demodPanel.setChannel({
                freqMHz: freqMHz,
                label: 'Unknown',
                modulation: '—',
                active: true,
            });
        }

        if (this._dataSource) {
            this._dataSource.freqCenterMHz = freqMHz;
            this._dataSource.freqSpanMHz = CHANNEL_SPAN_MHZ;
            if (typeof this._dataSource.reset === 'function') {
                this._dataSource.reset();
            }
        }
        if (this._fft) {
            this._fft.freqCenterMHz = freqMHz;
            this._fft.freqSpanMHz = CHANNEL_SPAN_MHZ;
        }
        if (this._antFft) {
            this._antFft.freqCenterMHz = freqMHz;
            this._antFft.freqSpanMHz = CHANNEL_SPAN_MHZ;
        }
        if (this._wf) {
            this._wf._freqCenterMHz = freqMHz;
            this._wf._freqSpanMHz = CHANNEL_SPAN_MHZ;
            this._wf.clear();
            if (this._active) { this._wf.start(); }
        }

        this._setRxFreqMHz(freqMHz);

        if (tx && tx.modulation) {
            this._setRxPipeline(tx.modulation);
        }

        const label = tx ? (tx.label || 'Unknown') : 'Unknown';
        if (this._tmiTable) {
            this._tmiTable.setChannel(label, freqMHz);
        }
        this._chanLabel = label;
        this._chanFreqMHz = freqMHz;
        this._chanNorad = tx && tx.noradId ? tx.noradId : null;

        this._syncEyeFromPipeline();

        // Настройка на канал переводит SDR в приём и открывает «Спектр» (§4.6)
        this._setView('spectrum');
        if (!(opts && opts.keepTab)) { this._showTab('spectrum'); }
    };

    // ── Подписка на состояние КА ──────────────────────────────────────────

    ManualLayout.prototype._subscribeState = function() {
        if (!this._sm || !window.StateEventType) { return; }
        const self = this;
        const SE = window.StateEventType;
        this._unsubscribers.push(this._sm.subscribe(SE.POSITION, function() {
            self._updateIndicators();
        }));
        this._unsubscribers.push(this._sm.subscribe(SE.TRACKING_CHANGE, function() {
            self._updateIndicators();
            if (self._passTimeline) {
                self._passTimeline.setTrackingId(self._sm.getTrackingSatelliteId());
            }
            if (!self._sm.getTrackingSatelliteId()) {
                self._followFreq = null;
                if (self._panorama && typeof self._panorama.setFollowedFreq === 'function') {
                    self._panorama.setFollowedFreq(null);
                }
            }
        }));
        this._unsubscribers.push(this._sm.subscribe(SE.SELECTED_CHANGE, function() {
            self._updateIndicators();
        }));
        if (SE.SATELLITE_GROUP_UPDATE) {
            this._unsubscribers.push(this._sm.subscribe(SE.SATELLITE_GROUP_UPDATE, function() {
                self._buildTransmittersFromGroup();
            }));
        }
        this._buildTransmittersFromGroup();
    };

    // ── Az/El: обновление по приоритету tracking → selected ───────────────

    ManualLayout.prototype._updateIndicators = function() {
        if (!this._az && !this._el && !this._omniLobe && !this._omniGain) { return; }
        const sm = this._sm;
        if (!sm) { return; }

        const trackingId = sm.getTrackingSatelliteId();
        const targetId = trackingId || sm.getSelectedSatelliteId();

        if (!targetId) {
            this._clearIndicators();
            return;
        }

        const state = sm.getState(targetId);
        if (!state || !state.position) { return; }

        const az = state.position.az;
        const el = state.position.el;

        // Антенна идёт за КА, пока План сеансов держит его под сопровождением;
        // иначе она стоит там, куда её навели, и расхождение осмысленно.
        const follow = this._isAutoTracking();
        if (this._rotFollowing && !follow) {
            // Сопровождение сняли — заданное подтягиваем к фактическому,
            // чтобы следующая команда не дёрнула антенну в устаревшую точку.
            this._seedTargetFromAntenna();
        }
        if (this._rotFollowing !== follow) {
            this._rotFollowing = follow;
            this._applyRotAvailability();
        }
        if (this._az) {
            this._az.setSatellitePosition(az);
            if (follow) { this._az.setAzimuth(az); }
            this._az.setNoradId(targetId);
        }
        if (this._el) {
            this._el.setSatellitePosition(el, az);
            if (follow) { this._el.setPosition(az, el); }
            this._el.setNoradId(targetId);
        }

        if (follow) {
            this._rotTargetAz = az;
            this._rotTargetEl = el;
            this._renderRotTargetFields();
        }
        this._updateAntDelta(az, el);
        this._renderRotTargetInfo(targetId);
        this._updateOmni(targetId, az, el);
    };

    /**
     * Рассогласование «антенна − КА» в таблице наведения вкладки «Антенна».
     * Азимут считаем по кратчайшей дуге: 359° и 1° расходятся на 2°, не на 358°.
     * @param {?number} satAz — азимут КА, градусы.
     * @param {?number} satEl — угол места КА, градусы.
     */
    ManualLayout.prototype._updateAntDelta = function(satAz, satEl) {
        const azEl = document.getElementById('manual-az-info-delta');
        const elEl = document.getElementById('manual-el-info-delta');
        if (!azEl && !elEl) { return; }

        // Всенаправленная антенна не наводится — «Антенна» и «Расхожд.» прочерк,
        // в колонке «КА» остаётся расчётное направление (виртуальная цель, §4.2).
        if (!this._rotHasRotator) {
            const dash = function(id) { const e = document.getElementById(id); if (e) { e.textContent = '—'; } };
            dash('manual-az-info-ant'); dash('manual-el-info-ant');
            this._renderDelta(azEl, null);
            this._renderDelta(elEl, null);
            return;
        }

        const antAz = this._az ? this._az.currentAzimuth : null;
        const antEl = this._el ? this._el.currentElevation : null;

        let dAz = null;
        if (typeof antAz === 'number' && typeof satAz === 'number') {
            dAz = ((antAz - satAz + 540) % 360) - 180;
        }
        let dEl = null;
        if (typeof antEl === 'number' && typeof satEl === 'number') {
            dEl = antEl - satEl;
        }

        this._renderDelta(azEl, dAz);
        this._renderDelta(elEl, dEl);
    };

    /** Печатает рассогласование со знаком и помечает выход за допуск. */
    ManualLayout.prototype._renderDelta = function(el, deltaDeg) {
        if (!el) { return; }
        if (typeof deltaDeg !== 'number' || !isFinite(deltaDeg)) {
            el.textContent = '---°';
            el.classList.remove('is-off');
            return;
        }
        const sign = deltaDeg > 0 ? '+' : (deltaDeg < 0 ? '−' : '');
        el.textContent = sign + Math.abs(deltaDeg).toFixed(1) + '°';
        el.classList.toggle('is-off', Math.abs(deltaDeg) > POINTING_TOLERANCE_DEG);
    };

    ManualLayout.prototype._clearIndicators = function() {
        if (this._az) {
            this._az.setSatellitePosition(null);
            this._az.setNoradId(null);
            this._az.draw();
        }
        if (this._el) {
            this._el.setSatellitePosition(null, null);
            this._el.setNoradId(null);
            this._el.draw();
        }
        this._updateAntDelta(null, null);
        this._renderRotTargetInfo(null);
        this._updateOmni(null, null, null);
    };

    // ── Размеры (ResizeObserver) ──────────────────────────────────────────

    ManualLayout.prototype._resizeIndicator = function(indicator, canvas) {
        if (!indicator || !canvas || !canvas.parentElement) { return; }
        const w = canvas.parentElement.clientWidth;
        const h = canvas.parentElement.clientHeight;
        if (w > 0 && h > 0) { indicator.resize(w, h); }
    };

    ManualLayout.prototype._observeWrap = function(canvas, callback) {
        if (typeof ResizeObserver === 'undefined' || !canvas || !canvas.parentElement) {
            return;
        }
        const ro = new ResizeObserver(callback);
        ro.observe(canvas.parentElement);
        this._resizeObservers.push(ro);
        callback();
    };

    ManualLayout.prototype._observeRaw = function(node, callback) {
        if (typeof ResizeObserver === 'undefined' || !node) { return; }
        const ro = new ResizeObserver(callback);
        ro.observe(node);
        this._resizeObservers.push(ro);
    };

    // ── Управление активностью ────────────────────────────────────────────

    ManualLayout.prototype.activate = function() {
        if (this._active) {
            this.refresh();
            return;
        }
        this._active = true;
        this._subscribeRadioPath();
        this._applyRadioPath();
        const self = this;
        requestAnimationFrame(function() {
            requestAnimationFrame(function() {
                self.refresh();
            });
        });
        // Вход в Ручной всегда начинается с обзора (ADR-004 §4.3.1)
        this._showTab('panorama');
        this._setView('panorama');
        if (this._passProfile) { this._passProfile.draw(); }
        if (this._passTimeline) { this._passTimeline.draw(); }
        if (this._tmiTable) { this._tmiTable.activate(); }
        if (this._eyeView) { this._eyeView.activate(); }
        this._refreshActiveTab();
    };

    /**
     * Подписка на смену тракта в шапке. ModeManager появляется только после
     * /api/config, позже конструктора, поэтому подписываемся при первом activate().
     */
    ManualLayout.prototype._subscribeRadioPath = function() {
        if (this._offRadioPath) { return; }
        const mm = (typeof window !== 'undefined') ? window._modeManager : null;
        if (!mm || typeof mm.onRadioPathChange !== 'function') { return; }
        const self = this;
        this._offRadioPath = mm.onRadioPathChange(function() {
            self._applyRadioPath();
            self._refreshActiveTab();
        });
        this._unsubscribers.push(this._offRadioPath);
    };

    /** Вид «Антенны» и панель наведения — по текущему тракту. */
    ManualLayout.prototype._applyRadioPath = function() {
        this._detectAntennaView();
        this._detectRotatorKind();
        this._applyRotAvailability();
        this._updateIndicators();
    };

    ManualLayout.prototype.deactivate = function() {
        if (!this._active) { return; }
        this._active = false;
        this._stopSpectrumTimer();
        if (this._panorama) { this._panorama.deactivate(); }
        if (this._tmiTable) { this._tmiTable.deactivate(); }
        if (this._eyeView) { this._eyeView.deactivate(); }
    };

    /** @returns {boolean} */
    ManualLayout.prototype.isActive = function() {
        return this._active;
    };

    ManualLayout.prototype.refresh = function() {
        if (this._fft) { this._fft._resize(); }
        if (this._wf) { this._wf.refresh(); }
        this._refreshActiveTab();
    };

    ManualLayout.prototype.refreshAfterThemeChange = function() {
        if (this._wf) {
            this._wf._imageData = null;
            this._wf.clear();
            if (this._active) { this._wf.start(); }
        }
        if (this._az && typeof this._az.refreshThemeColors === 'function') {
            this._az.refreshThemeColors();
            this._az.draw();
        }
        if (this._el && typeof this._el.refreshThemeColors === 'function') {
            this._el.refreshThemeColors();
            this._el.draw();
        }
        [this._omniLobe, this._omniGain].forEach(function(v) {
            if (v) { v.refreshThemeColors(); v.draw(); }
        });
        if (this._panorama) { this._panorama.refreshAfterThemeChange(); }
        if (this._passProfile) { this._passProfile.refreshAfterThemeChange(); }
        if (this._passTimeline) { this._passTimeline.refreshAfterThemeChange(); }
        if (this._eyeView) { this._eyeView.refreshAfterThemeChange(); }
        this.refresh();
    };

    // ── Таймер спектра/водопада ───────────────────────────────────────────

    ManualLayout.prototype._startSpectrumTimer = function() {
        if (this._spectrumTimer || !this._dataSource) { return; }
        const self = this;
        this._spectrumTimer = setInterval(function() { self._spectrumTick(); }, SPECTRUM_TICK_MS);
    };

    ManualLayout.prototype._stopSpectrumTimer = function() {
        if (this._spectrumTimer) {
            clearInterval(this._spectrumTimer);
            this._spectrumTimer = null;
        }
    };

    ManualLayout.prototype._spectrumTick = function() {
        if (!this._active || !this._dataSource) { return; }
        this._dataSource.generateLine();
        const line = this._dataSource.getLine();
        if (this._fft) { this._fft.draw(line); }
        if (this._wf) { this._wf.pushLine(line); }
        if (this._antFft && this._activeTab === 'antenna') {
            this._antFft.draw(line);
            this._drawExpectedFreq(this._antFft, this._expectedFreqMHz());
        }

        this._ppTickCount = (this._ppTickCount || 0) + 1;
        if (this._passProfile && this._ppTickCount >= 12) {
            this._ppTickCount = 0;
            this._passProfile.draw();
        }

        // Ленте пролётов хватает минутного разрешения — перерисовываем реже
        this._tlTickCount = (this._tlTickCount || 0) + 1;
        if (this._passTimeline && this._tlTickCount >= 60) {
            this._tlTickCount = 0;
            this._passTimeline.draw();
        }
    };

    /**
     * Частота, на которой должен быть слышен настроенный передатчик сейчас:
     * номинал со сдвигом Доплера по радиальной скорости его КА. Перестройка
     * индикатором её не меняет — по ней видно, насколько приёмник ушёл с сигнала.
     * @returns {?number} МГц или null, если передатчик или скорость неизвестны.
     */
    ManualLayout.prototype._expectedFreqMHz = function() {
        const f = this._chanFreqMHz;
        if (typeof f !== 'number' || !isFinite(f) || !this._sm) { return null; }
        const norad = this._chanNorad;
        const state = norad ? this._sm.getState(norad) : null;
        const rr = state && state.position ? state.position.range_rate : null;
        if (typeof rr !== 'number' || !isFinite(rr)) { return null; }
        // КА удаляется (range_rate > 0): частота ниже номинала
        return f * (1 - rr / C_LIGHT);
    };

    /**
     * Пунктир ожидаемой частоты поверх спектра канала. Рисуем по геометрии
     * FFTSpectrumView; частота за пределами полосы не показывается.
     * @param {Object} view — FFTSpectrumView
     * @param {?number} freqMHz
     */
    ManualLayout.prototype._drawExpectedFreq = function(view, freqMHz) {
        if (freqMHz === null || !view || !view._canvas || !view._ctx) { return; }
        const canvas = view._canvas;
        const plotW = canvas.width - view._marginLeft - view._marginRight;
        const fMin = view.freqCenterMHz - view.freqSpanMHz / 2;
        const frac = (freqMHz - fMin) / view.freqSpanMHz;
        if (plotW < 4 || frac < 0 || frac > 1) { return; }
        const x = Math.round(view._marginLeft + frac * plotW) + 0.5;
        const ctx = view._ctx;
        const color = getComputedStyle(document.documentElement)
            .getPropertyValue('--accent-primary').trim() || '#7ab8d0';
        ctx.save();
        ctx.strokeStyle = color;
        ctx.lineWidth = 1;
        ctx.setLineDash([2, 3]);
        ctx.beginPath();
        ctx.moveTo(x, 0);
        ctx.lineTo(x, canvas.height);
        ctx.stroke();
        ctx.setLineDash([]);
        ctx.font = '9px monospace';
        ctx.fillStyle = color;
        ctx.textBaseline = 'top';
        // Подпись по ту сторону линии, где есть место
        const label = 'ожид. ' + freqMHz.toFixed(4);
        const onLeft = x > canvas.width - 90;
        ctx.textAlign = onLeft ? 'right' : 'left';
        ctx.fillText(label, onLeft ? x - 4 : x + 4, 2);
        ctx.restore();
    };

    // ── Очистка ──────────────────────────────────────────────────────────

    ManualLayout.prototype.destroy = function() {
        this._stopSpectrumTimer();
        let i;
        for (i = 0; i < this._unsubscribers.length; i++) {
            const off = this._unsubscribers[i];
            if (typeof off === 'function') { off(); }
        }
        this._unsubscribers = [];
        for (i = 0; i < this._resizeObservers.length; i++) {
            this._resizeObservers[i].disconnect();
        }
        this._resizeObservers = [];
        for (i = 0; i < this._handlers.length; i++) {
            const h = this._handlers[i];
            h[0].removeEventListener(h[1], h[2]);
        }
        this._handlers = [];
        this._az = null;
        this._el = null;
        this._fft = null;
        this._wf = null;
        this._antFft = null;
        this._omniLobe = null;
        this._omniGain = null;
        this._dataSource = null;
        if (this._panorama) {
            this._panorama.destroy();
            this._panorama = null;
        }
        if (this._demodPanel) {
            this._demodPanel.destroy();
            this._demodPanel = null;
        }
        const root = this._layoutRoot || document.getElementById('layout-manual');
        if (root && this._onPanoramaTune) {
            root.removeEventListener('panorama:tune', this._onPanoramaTune);
        }
        if (root && this._onPanoramaFollow) {
            root.removeEventListener('panorama:follow', this._onPanoramaFollow);
        }
        this._onPanoramaTune = null;
        this._onPanoramaFollow = null;
        this._passProfile = null;
        this._passTimeline = null;
        if (this._tmiTable) {
            this._tmiTable.destroy();
            this._tmiTable = null;
        }
        if (this._eyeView) {
            this._eyeView.destroy();
            this._eyeView = null;
        }
        if (this._freqDial) {
            this._freqDial.destroy();
            this._freqDial = null;
        }
        this._rxPanelRoot = null;
        this._rxPipelineSelect = null;
        this._rec = null;
    };

    // ── Экспорт ──────────────────────────────────────────────────────────

    ManualLayout.makeRecordingName = makeRecordingName;

    if (typeof window !== 'undefined') {
        window.ManualLayout = ManualLayout;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { ManualLayout: ManualLayout }; // eslint-disable-line no-undef
    }
})();
