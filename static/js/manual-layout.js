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
// Спектр и водопад рисуются из потока сервера (spectrum-stream.js). Приёмник
// один на всех клиентов тракта: центр и полосу берём из заголовка кадра, а
// регулятор частоты показывает, где приёмник стоит на самом деле. Перестроить
// его могут мастер и слейв с разрешением (access-control.js); у остальных
// регулятор только показывает частоту.
//
// Жизненный цикл: activate()/deactivate() открывают и закрывают поток и
// управляют таймером перерисовки профиля и ленты пролётов.

'use strict';

(function() {
    const SPECTRUM_TICK_MS = 80;
    /** Скорость света для доплеровского сдвига, м/с. */
    const C_LIGHT = 299792458;
    /** Подписи осей до первого кадра потока: диапазон телеметрии 435–437 МГц. */
    const DEFAULT_BAND = { centerMHz: 436, spanMHz: 2 };
    /** Частота кадров потока спектра. */
    const STREAM_FPS = 20;
    /** Отсчётов в полосе: одинаково во всех режимах и при любой ширине графика. */
    const STREAM_BINS = 4096;
    /** Накопление спектра — значение mode в запросе потока; считает сервер (ADR-002 §III). */
    const SPEC_MODES = ['current', 'avg', 'maxhold'];
    const SPEC_MODE_DEFAULT = 'current';
    const STORAGE_SPEC_MODE = 'ux.manual.specMode';
    /** «Накопление»: среднее по N циклам, цикл — кадр потока. */
    const SPEC_AVG_N = [4, 10, 20, 50];
    const SPEC_AVG_N_DEFAULT = 10;
    const STORAGE_SPEC_AVG_N = 'ux.manual.specAvgN';
    /** display_mode кадра SSTM (ADR-002): в «Пике» за удержанием идёт мгновенный кадр. */
    const FRAME_MODE_CURRENT = 0x00;
    const FRAME_MODE_MAXHOLD = 0x02;
    /** Цифры маркера обновляются не чаще раза в этот интервал: 20 раз в секунду их не прочитать. */
    const MARKER_REFRESH_MS = 250;
    const MARKER_EMPTY = '—';
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
        /** Поток спектра: открыт только в приёме (HOLD) при активном Ручном. */
        this._stream = null;
        this._streamPath = null;
        /** Центр приёмника из последнего кадра, Гц; null — кадров ещё не было. */
        this._streamCenterHz = null;
        /** Шкала dB по данным потока, общая для спектров и водопада. */
        this._streamRange = null;
        /** Накопление спектра (SPEC_MODES), его кнопки и N для «Накопления». */
        this._specMode = SPEC_MODE_DEFAULT;
        this._specModeButtons = [];
        this._specAvgN = SPEC_AVG_N_DEFAULT;
        this._specAvgNSelects = [];
        /** Последний кадр удержания: рисуется вместе со следующим мгновенным. */
        this._heldValues = null;
        /** Сдвиг уровней из dBFS в dBm на входе антенны; null — экран в dBFS. */
        this._levelOffset = null;
        /** Тракт, для которого получен _levelOffset. */
        this._levelPath = null;
        /** Запросы перестройки в пути: пока они есть, кадры не двигают регулятор. */
        this._tunePending = 0;
        this._offAccess = null;
        this._accessPanel = null;
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
        /** Маркер пика под индикатором частоты и время кадра последнего обновления цифр, мс. */
        this._marker = null;
        this._markerShownMs = null;
        /** Подписи единиц уровня (dBFS или dBm) и поле порога шумоподавления. */
        this._levelUnits = [];
        this._squelchInput = null;
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
        this._initSpecMode();
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
                this._startStream();
            }
        } else {
            // SWEEP: спектр/водопад выключены и очищены, панорама сканирует
            this._stopSpectrumTimer();
            this._stopStream();
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
        this._marker = {
            freq: document.getElementById('manual-rx-marker-freq'),
            level: document.getElementById('manual-rx-marker-level'),
            bar: document.getElementById('manual-rx-marker-bar'),
        };
        this._levelUnits = [
            document.getElementById('manual-rx-marker-unit'),
            document.getElementById('manual-rx-squelch-unit'),
        ];
        this._squelchInput = document.getElementById('manual-rx-squelch');
        const self = this;

        this._initFreqDial();
        this._showMarker(null);

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
     * Ручная перестройка оператором (SET на регуляторе). Спектр и водопад
     * сдвинутся, когда придут кадры с новым центром.
     * @param {number} freqMHz
     */
    ManualLayout.prototype._onOperatorRetune = function(freqMHz) {
        this._requestTune(freqMHz);
    };

    /** Имя тракта на сервере для выбранного в шапке; null — тракта нет. */
    ManualLayout.prototype._radioPathName = function() {
        const mm = (typeof window !== 'undefined') ? window._modeManager : null;
        const id = mm && typeof mm.getRadioPathId === 'function' ? mm.getRadioPathId() : null;
        return id === null || id === undefined ? null : 'radio_path_' + id;
    };

    /**
     * Перестройка общего приёмника. Без прав запрос не уходит; отказ сервера
     * возвращает регулятор на частоту, где приёмник стоит.
     * @param {number} freqMHz
     */
    ManualLayout.prototype._requestTune = function(freqMHz) {
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        if (ac && !ac.canControl()) {
            ac.setNotice('Перестраивать приёмник можно только в режиме «Контроль».');
            this._revertDial();
            return;
        }
        const path = this._radioPathName();
        if (!path || typeof fetch !== 'function') { return; }
        const self = this;
        const notice = function(text) { if (ac) { ac.setNotice(text); } };
        this._tunePending++;
        fetch('/api/sdr/tune', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ radio_path: path, center_hz: Math.round(freqMHz * 1e6) }),
        })
            .then(function(r) {
                if (r.ok) { return null; }
                self._revertDial();
                if (r.status === 403) {
                    notice('Режим «Контроль» отключён, приёмник не перестроен.');
                    if (ac) { ac.refresh(); }
                    return null;
                }
                return r.json().then(function(b) {
                    notice('Приёмник не перестроен: ' + ((b && b.error) || ('HTTP ' + r.status)));
                }, function() {
                    notice('Приёмник не перестроен: HTTP ' + r.status);
                });
            })
            .catch(function(err) {
                self._revertDial();
                notice('Приёмник не перестроен: ' + err.message);
            })
            .then(function() { self._tunePending--; });
    };

    /** Регулятор на частоту приёмника из потока, если она уже известна. */
    ManualLayout.prototype._revertDial = function() {
        if (this._streamCenterHz === null) { return; }
        this._rxFreqMHz = this._streamCenterHz / 1e6;
        if (this._freqDial) { this._freqDial.setHz(this._streamCenterHz); }
    };

    // ── Права на управление приёмником ────────────────────────────────────

    /**
     * AccessControl создаёт app.js после конструктора, поэтому подписываемся
     * при первом activate(), как и на смену тракта.
     */
    ManualLayout.prototype._subscribeAccess = function() {
        if (this._offAccess) { return; }
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        if (!ac || typeof ac.onChange !== 'function') { return; }
        const self = this;
        this._offAccess = ac.onChange(function() { self._applyAccess(); });
        this._unsubscribers.push(this._offAccess);
        const Panel = window.AccessControl && window.AccessControl.AccessPanel;
        if (typeof Panel === 'function' && !this._accessPanel) {
            this._accessPanel = new Panel(ac, {
                role: document.getElementById('manual-access-role'),
                detail: document.getElementById('manual-access-detail'),
                manage: document.getElementById('manual-access-manage'),
                revoke: document.getElementById('manual-access-revoke'),
                release: document.getElementById('manual-access-release'),
                notice: document.getElementById('manual-access-notice'),
                modal: {
                    root: document.getElementById('access-modal'),
                    backdrop: document.getElementById('access-modal-backdrop'),
                    close: document.getElementById('access-modal-close'),
                    request: document.getElementById('access-modal-request'),
                    loginForm: document.getElementById('access-modal-login'),
                    password: document.getElementById('access-modal-password'),
                    submit: document.getElementById('access-modal-submit'),
                    error: document.getElementById('access-modal-error'),
                },
            });
        }
    };

    /** Без прав регулятор только показывает частоту приёмника. */
    ManualLayout.prototype._applyAccess = function() {
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        if (this._freqDial && typeof this._freqDial.setReadOnly === 'function') {
            this._freqDial.setReadOnly(!!ac && !ac.canControl());
        }
    };

    // ── Поток спектра ─────────────────────────────────────────────────────

    /** Открывает поток выбранного тракта; при смене тракта переоткрывает. */
    ManualLayout.prototype._startStream = function() {
        const path = this._radioPathName();
        if (this._stream && this._streamPath === path) { return; }
        this._stopStream();
        if (!path || typeof window.SpectrumStream !== 'function') { return; }
        const self = this;
        this._streamPath = path;
        this._streamCenterHz = null;
        this._streamRange = new window.SpectrumStream.AutoRange();
        this._heldValues = null;
        this._requestLevelOffset(path);
        const windowMs = this._specMode === 'avg'
            ? '&window_ms=' + Math.round(this._specAvgN * 1000 / STREAM_FPS)
            : '';
        this._stream = new window.SpectrumStream({
            url: '/api/stream/v1/spectrum?radio_path=' + encodeURIComponent(path) +
                '&bins=' + STREAM_BINS + '&fps=' + STREAM_FPS +
                '&mode=' + this._specMode + windowMs,
            onFrame: function(frame) { self._onStreamFrame(frame); },
        });
        this._stream.start();
    };

    /**
     * dBm на экране, если у приёмника тракта есть поправка; усиление берётся из статуса
     * приёмника. Пока ответа нет, у того же тракта остаются прежние единицы.
     * @param {string} path
     */
    ManualLayout.prototype._requestLevelOffset = function(path) {
        const mm = (typeof window !== 'undefined') ? window._modeManager : null;
        const rp = mm && typeof mm.getRadioPath === 'function' ? mm.getRadioPath() : null;
        const cal = rp ? rp.calibration_db : undefined;
        if (typeof cal !== 'number' || path !== this._levelPath) {
            this._levelPath = null;
            this._setLevelOffset(null);
        }
        if (typeof cal !== 'number' || typeof fetch !== 'function') { return; }
        const self = this;
        fetch('/api/sdr/status?radio_path=' + encodeURIComponent(path), { credentials: 'same-origin' })
            .then(function(r) { return r.ok ? r.json() : null; })
            .then(function(st) {
                if (self._streamPath !== path) { return; }
                self._levelPath = path;
                self._setLevelOffset(window.SpectrumStream.dbmOffset(cal, st && st.gain_db));
            })
            .catch(function() { /* без статуса остаются dBFS */ });
    };

    /**
     * Меняет единицы уровня. Порог шумоподавления пересчитывается, чтобы значить
     * тот же уровень; шкала и удержание были в прежних единицах и начинаются заново.
     * @param {?number} offset — сдвиг dBFS в dBm, null — dBFS
     */
    ManualLayout.prototype._setLevelOffset = function(offset) {
        if (offset === this._levelOffset) { return; }
        const delta = (offset || 0) - (this._levelOffset || 0);
        this._levelOffset = offset;
        const unit = offset === null ? 'dBFS' : 'dBm';
        for (let i = 0; i < this._levelUnits.length; i++) {
            if (this._levelUnits[i]) { this._levelUnits[i].textContent = unit; }
        }
        const sq = this._squelchInput;
        if (sq && sq.value !== '' && isFinite(Number(sq.value))) {
            sq.value = String(Number((Number(sq.value) + delta).toFixed(1)));
        }
        if (this._streamRange) { this._streamRange = new window.SpectrumStream.AutoRange(); }
        this._heldValues = null;
    };

    /**
     * Переоткрывает открытый поток с текущими параметрами: так меняется режим
     * накопления и сбрасывается удержание (ADR-002 §III, отдельного REST нет).
     */
    ManualLayout.prototype._reopenStream = function() {
        if (!this._stream) { return; }
        this._stopStream();
        this._startStream();
    };

    // ── Накопление спектра: Мгн. | Накопление (N) | Пик, «Сброс» ──────────

    /** Переключатели над спектром «Спектра» и над компактным спектром «Антенны»: состояние одно. */
    const SPEC_MODE_GROUPS = [
        { modes: 'manual-spec-mode', avgN: 'manual-spec-avg-n', reset: 'manual-spec-reset' },
        { modes: 'manual-ant-spec-mode', avgN: 'manual-ant-spec-avg-n', reset: 'manual-ant-spec-reset' },
    ];

    ManualLayout.prototype._initSpecMode = function() {
        const self = this;
        this._specModeButtons = [];
        this._specAvgNSelects = [];
        for (let g = 0; g < SPEC_MODE_GROUPS.length; g++) {
            const ids = SPEC_MODE_GROUPS[g];
            const group = document.getElementById(ids.modes);
            if (group && typeof group.querySelectorAll === 'function') {
                this._specModeButtons = this._specModeButtons.concat(
                    Array.prototype.slice.call(group.querySelectorAll('[data-spec-mode]')));
            }
            const select = document.getElementById(ids.avgN);
            if (select) {
                this._specAvgNSelects.push(select);
                this._on(select, 'change', function() { self._setSpecAvgN(Number(select.value)); });
            }
            this._on(document.getElementById(ids.reset), 'click', function() { self._reopenStream(); });
        }
        let saved = null;
        let savedN = null;
        try {
            saved = localStorage.getItem(STORAGE_SPEC_MODE);
            savedN = Number(localStorage.getItem(STORAGE_SPEC_AVG_N));
        } catch (_e) { /* noop */ }
        this._specMode = SPEC_MODES.indexOf(saved) >= 0 ? saved : SPEC_MODE_DEFAULT;
        this._specAvgN = SPEC_AVG_N.indexOf(savedN) >= 0 ? savedN : SPEC_AVG_N_DEFAULT;
        for (let i = 0; i < this._specModeButtons.length; i++) {
            (function(btn) {
                self._on(btn, 'click', function() {
                    self._setSpecMode(btn.getAttribute('data-spec-mode'));
                });
            })(this._specModeButtons[i]);
        }
        this._renderSpecMode();
    };

    /** @param {string} mode — один из SPEC_MODES */
    ManualLayout.prototype._setSpecMode = function(mode) {
        if (SPEC_MODES.indexOf(mode) < 0 || mode === this._specMode) { return; }
        this._specMode = mode;
        try { localStorage.setItem(STORAGE_SPEC_MODE, mode); } catch (_e) { /* noop */ }
        this._renderSpecMode();
        this._reopenStream();
    };

    /** @param {number} n — один из SPEC_AVG_N; поток переоткрывается только в «Накоплении» */
    ManualLayout.prototype._setSpecAvgN = function(n) {
        if (SPEC_AVG_N.indexOf(n) < 0 || n === this._specAvgN) { return; }
        this._specAvgN = n;
        try { localStorage.setItem(STORAGE_SPEC_AVG_N, String(n)); } catch (_e) { /* noop */ }
        this._renderSpecMode();
        if (this._specMode === 'avg') { this._reopenStream(); }
    };

    ManualLayout.prototype._renderSpecMode = function() {
        for (let i = 0; i < this._specModeButtons.length; i++) {
            const btn = this._specModeButtons[i];
            const on = btn.getAttribute('data-spec-mode') === this._specMode;
            btn.classList.toggle('is-on', on);
            btn.setAttribute('aria-pressed', on ? 'true' : 'false');
        }
        for (let s = 0; s < this._specAvgNSelects.length; s++) {
            this._specAvgNSelects[s].value = String(this._specAvgN);
            this._specAvgNSelects[s].hidden = this._specMode !== 'avg';
        }
    };

    ManualLayout.prototype._stopStream = function() {
        if (this._stream) {
            this._stream.stop();
            this._stream = null;
        }
        this._streamPath = null;
        this._markerShownMs = null;
        this._showMarker(null);
    };

    /** @param {{header: Object, values: ?Float32Array}} frame */
    ManualLayout.prototype._onStreamFrame = function(frame) {
        const h = frame.header;
        this._applyStreamBand(h.centerHz, h.spanHz);
        if (!frame.values || !this._active || !this._streamRange) { return; }
        const values = this._levelOffset === null
            ? frame.values
            : window.SpectrumStream.shiftLevels(frame.values, this._levelOffset);
        // В «Пике» сервер шлёт удержание и следом мгновенный кадр: рисуем их вместе,
        // водопад и маркер берут мгновенный
        const peak = this._specMode === 'maxhold';
        if (peak && h.displayMode === FRAME_MODE_MAXHOLD) {
            this._heldValues = values;
            return;
        }
        const held = peak && h.displayMode === FRAME_MODE_CURRENT ? this._heldValues : null;
        const range = this._streamRange.update(values, held);
        const views = [this._fft, this._antFft];
        for (let i = 0; i < views.length; i++) {
            if (!views[i]) { continue; }
            views[i]._dbMin = range.min;
            views[i]._dbMax = range.max;
        }
        const toUnit = window.SpectrumStream.dbfsToUnit;
        const line = toUnit(values, range.min, range.max);
        const heldLine = held ? toUnit(held, range.min, range.max) : null;
        const drawOn = function(view) {
            if (heldLine) { view.draw(line, heldLine); } else { view.draw(line); }
        };
        if (this._fft) { drawOn(this._fft); }
        if (this._wf) { this._wf.pushLine(line); }
        if (this._antFft && this._activeTab === 'antenna') {
            drawOn(this._antFft);
            this._drawExpectedFreq(this._antFft, this._expectedFreqMHz());
        }
        const ts = h.timestampMs;
        if (this._markerShownMs === null || !(ts - this._markerShownMs < MARKER_REFRESH_MS)) {
            this._markerShownMs = typeof ts === 'number' ? ts : null;
            this._showMarker(window.SpectrumStream.markerReadout(values, h.centerHz, h.spanHz));
        }
    };

    /**
     * Центр и полоса видов — из кадра: данные на экране соответствуют им.
     * Смена центра значит, что приёмник перестроили (может быть, другой
     * оператор): водопад с прежней частотой очищается, регулятор следует за
     * приёмником, если оператор сейчас ничего не набирает и не ждёт перестройки.
     */
    ManualLayout.prototype._applyStreamBand = function(centerHz, spanHz) {
        if (!centerHz) { return; }
        const first = this._streamCenterHz === null;
        const changed = centerHz !== this._streamCenterHz;
        const centerMHz = centerHz / 1e6;
        const spanMHz = spanHz > 0 ? spanHz / 1e6 : null;
        const views = [this._fft, this._antFft];
        for (let i = 0; i < views.length; i++) {
            if (!views[i]) { continue; }
            views[i].freqCenterMHz = centerMHz;
            if (spanMHz) { views[i].freqSpanMHz = spanMHz; }
        }
        if (this._wf) {
            this._wf._freqCenterMHz = centerMHz;
            if (spanMHz) { this._wf._freqSpanMHz = spanMHz; }
            if (changed && !first) {
                this._wf.clear();
                if (this._active) { this._wf.start(); }
            }
        }
        this._streamCenterHz = centerHz;
        const editing = this._freqDial && typeof this._freqDial.isEditing === 'function' &&
            this._freqDial.isEditing();
        if (changed && this._tunePending === 0 && !editing) {
            this._rxFreqMHz = centerMHz;
            if (this._freqDial) { this._freqDial.setHz(centerHz); }
        }
    };

    /**
     * Цифры пика под индикатором частоты; полоса амплитуды — шкала −120…0 dBFS
     * и в dBm: она показывает запас до перегрузки АЦП.
     * Единицы стоят в разметке, здесь только числа: крупные цифры не прыгают.
     * @param {?{freqHz: number, dbfs: number}} mk — уровень в единицах экрана; null: данных нет, прочерки
     */
    ManualLayout.prototype._showMarker = function(mk) {
        const m = this._marker;
        if (!m) { return; }
        const num = function(v, digits) { return v.toFixed(digits).replace('-', '−'); };
        const fs = mk ? mk.dbfs - (this._levelOffset || 0) : 0;
        const pct = mk ? Math.max(0, Math.min(100, (fs + 120) / 120 * 100)) : 0;
        if (m.freq) { m.freq.textContent = mk ? num(mk.freqHz / 1e6, 3) : MARKER_EMPTY; }
        if (m.level) { m.level.textContent = mk ? num(mk.dbfs, 1) : MARKER_EMPTY; }
        if (m.bar) { m.bar.style.width = pct.toFixed(1) + '%'; }
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
        if (typeof window.FFTSpectrumView !== 'function' ||
            typeof window.WaterfallView !== 'function') {
            return;
        }
        const band = { freqCenterMHz: DEFAULT_BAND.centerMHz, freqSpanMHz: DEFAULT_BAND.spanMHz };
        if (this._fftCanvas) {
            this._fft = new window.FFTSpectrumView(this._fftCanvas, band);
        }
        if (this._antCanvas) {
            this._antFft = new window.FFTSpectrumView(this._antCanvas, band);
        }
        if (this._wfCanvas) {
            const ml = this._fft ? this._fft._marginLeft : 32;
            const mr = this._fft ? this._fft._marginRight : 4;
            this._wf = new window.WaterfallView(this._wfCanvas, this._scaleCanvas, {
                freqCenterMHz: band.freqCenterMHz,
                freqSpanMHz: band.freqSpanMHz,
                marginLeft: ml,
                marginRight: mr,
            });
            // Строка водопада — кадр потока: шкала времени по его частоте
            this._wf._tickMs = 1000 / STREAM_FPS;
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

        this._setRxFreqMHz(freqMHz);
        this._requestTune(freqMHz);

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
        this._subscribeAccess();
        this._applyAccess();
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
            if (self._stream) { self._startStream(); }
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
        this._stopStream();
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

    // ── Таймер приёма: профиль и лента пролётов (спектр рисует поток) ─────

    ManualLayout.prototype._startSpectrumTimer = function() {
        if (this._spectrumTimer) { return; }
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
        if (!this._active) { return; }
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
        this._stopStream();
        if (this._accessPanel) {
            this._accessPanel.destroy();
            this._accessPanel = null;
        }
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
