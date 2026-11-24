// manual-layout.js — Ручной режим: сквозной таймлайн и вкладки по режимам SDR
// (ADR-004 §4.3–4.3.1).
//
// Структура DOM:
//   #layout-manual[data-tab]
//     .ml-timeline          → PassTimelineView (горки El×время), виден всегда
//     .ml-tabbar            → рейка слева: Панорама, Спектр, Антенна
//     .ml-txlist            → лента передатчиков (рендерит ManualPanorama), кроме «Антенны»
//     Вкладки (.ml-tabpane):
//       Панорама — SWEEP: панорама (FFT по span + метки TX) и таблица «Эфир»
//       Спектр   — HOLD: FFT + водопад канала, результат приёма
//                  (Таблица | Диаграмма | Снимки) и колонка тракта
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
    /** Строка обнаруженных сигналов над водопадом: '0' — скрыта. */
    const STORAGE_BAND_MARKS = 'ux.manual.specBandMarks';
    /** display_mode кадра SSTM (ADR-002): в «Пике» за удержанием идёт мгновенный кадр. */
    const FRAME_MODE_CURRENT = 0x00;
    const FRAME_MODE_MAXHOLD = 0x02;
    /** Цифры маркера обновляются не чаще раза в этот интервал: 20 раз в секунду их не прочитать. */
    const MARKER_REFRESH_MS = 250;
    /**
     * Доля нового кадра в линии на экране (0…1). Не путать с серверным «Накоплением»:
     * здесь только сглаживание картинки, чтобы не дёргать canvas на каждом FFT.
     */
    const DISPLAY_SMOOTH_ALPHA = 0.35;
    /** Масштаб спектра: самое узкое окно в бинах кадра (уже — одни ступеньки). */
    const MIN_ZOOM_BINS = 32;
    /** Масштаб на пиксель прокрутки колеса: щелчок колеса (100) даёт примерно ×1,2. */
    const WHEEL_ZOOM_RATE = 0.002;
    /**
     * Горизонтальной прокруткой считаем жест, у которого смещение по X больше
     * вертикального в столько раз. Иначе шум второй оси перестраивает приёмник.
     */
    const WHEEL_PAN_RATIO = 2;
    /** Как часто промотка шлёт частоту приёмнику, пока жест ещё идёт. */
    const SCRUB_TUNE_MS = 150;
    const MARKER_EMPTY = '—';
    /** Допуск наведения: сверх него рассогласование подсвечивается, градусы. */
    const POINTING_TOLERANCE_DEG = 2;
    const STORAGE_ROT_STEP = 'ux.rotStep';
    const STORAGE_ROT_MODE = 'ux.rotMode';
    const STORAGE_REC_DIR = 'ux.manualRecDir';
    /** Шаг крестовины по умолчанию, градусы. */
    const ROT_STEP_DEFAULT_DEG = 1;
    /** Папка записей IQ, пока оператор не задал свою. */
    const REC_DIR_DEFAULT = 'data/recordings/';
    /** Пауза до автоповтора кнопок −/+ и период повтора, как у индикатора частоты. */
    const STEP_REPEAT_DELAY_MS = 380;
    const STEP_REPEAT_EVERY_MS = 70;
    /**
     * Поля со степпером. kind задаёт допустимые символы:
     * усиление — 0…49.6 с шагом 0.1, доплер — целые герцы, порог — уровень с десятыми.
     */
    const NUMERIC_FIELDS = {
        gain: { kind: 'gain', step: 0.1, min: 0, max: 49.6, decimals: 1 },
        dopplerHz: { kind: 'offset', step: 1, decimals: 0 },
        squelch: { kind: 'squelch', step: 1, decimals: 1 },
        baud: { kind: 'gain', step: 1, min: 50, max: 100000, decimals: 0 },
    };

    /**
     * Заполняет select профилей демодулятора из ответа /api/sdr/demod-profiles.
     * @param {HTMLSelectElement} sel
     * @param {Array<{id:string,label:string}>} profiles
     * @param {string} [keepId]
     */
    function fillDemodProfileSelect(sel, profiles, keepId) {
        if (!sel || !Array.isArray(profiles) || !profiles.length) { return; }
        const prev = keepId || selectedValue(sel) || 'auto';
        sel.innerHTML = '';
        if (sel.options && sel.options.length) {
            sel.options.length = 0;
        }
        for (let i = 0; i < profiles.length; i++) {
            const p = profiles[i];
            if (!p || !p.id) { continue; }
            const opt = document.createElement('option');
            opt.value = p.id;
            opt.textContent = p.label || p.id;
            if (typeof sel.appendChild === 'function') {
                sel.appendChild(opt);
            } else if (sel.options && typeof sel.options.add === 'function') {
                sel.options.add(opt);
            } else if (sel.options) {
                sel.options[sel.options.length] = opt;
            }
        }
        if (!selectByValue(sel, prev)) {
            selectByValue(sel, 'auto');
        }
    }

    /** Профиль с гибкой скоростью: поле Baud видно в панели. */
    function profileBaudFlexible(byId, id) {
        const p = byId && byId[id];
        return !!(p && p.baud_flexible);
    }

    /**
     * Оставляет в строке только цифры числа.
     * Усиление: без минуса, одна точка. Сдвиг: минус в начале, дробь отбрасывается.
     * Порог: минус в начале и одна точка.
     * @param {*} raw
     * @param {string} kind
     * @returns {string}
     */
    function filterNumericText(raw, kind) {
        const allowMinus = kind !== 'gain';
        const allowDot = kind !== 'offset';
        const s = String(raw == null ? '' : raw);
        let out = '';
        let seenDot = false;
        for (let i = 0; i < s.length; i++) {
            const ch = s.charAt(i);
            if (ch >= '0' && ch <= '9') {
                out += ch;
                continue;
            }
            if (ch === '-' && allowMinus && out.length === 0) {
                out = '-';
                continue;
            }
            if (ch !== '.') { continue; }
            if (!allowDot) { break; }
            if (!seenDot) {
                seenDot = true;
                out += '.';
            }
        }
        return out;
    }

    /**
     * Сдвигает число на delta и возвращает строку в пределах spec.min и spec.max.
     * Пустая строка и незавершённый ввод («-», «.») считаются нулём.
     * @param {*} raw
     * @param {number} delta
     * @param {{kind:string, min?:number, max?:number, decimals?:number}} spec
     * @returns {string}
     */
    function stepNumericText(raw, delta, spec) {
        const filtered = filterNumericText(raw, spec.kind);
        let n = Number(filtered);
        if (!isFinite(n)) { n = 0; }
        n += delta;
        if (typeof spec.min === 'number' && n < spec.min) { n = spec.min; }
        if (typeof spec.max === 'number' && n > spec.max) { n = spec.max; }
        return formatStepped(n, spec.decimals || 0);
    }

    /** Строка числа без хвостовых нулей. Минус у нуля не оставляем. */
    function formatStepped(n, decimals) {
        if (!decimals) {
            const rounded = Math.round(n);
            return rounded === 0 ? '0' : String(rounded);
        }
        const factor = Math.pow(10, decimals);
        const rounded = Math.round(n * factor) / factor;
        const text = rounded.toFixed(decimals).replace(/0+$/, '').replace(/\.$/, '');
        return text === '-0' ? '0' : text;
    }

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

    /** Сколько после смены окна шапка пишет, откуда перестроился приёмник, мс. */
    const SWEEP_RETUNE_SHOW_MS = 1500;

    function sweepMHzText(hz) {
        return ((hz || 0) / 1e6).toFixed(3).replace('.', ',') + ' МГц';
    }

    /**
     * Шапка панорамы по состоянию обхода: частота стоянки, строка состояния
     * и доля прошедшей стоянки.
     * @param {?Object} st — sweep_state; null — обхода у тракта нет
     * @param {string} emptyText — пояснение, когда st нет
     * @param {number} nowMs
     * @param {?{fromHz:number, untilMs:number}} retune — недавняя смена окна
     * @returns {{freq:string, status:string, progress:?number}}
     */
    function sweepBarModel(st, emptyText, nowMs, retune) {
        const idle = function(status) { return { freq: '', status: status, progress: null }; };
        if (!st) { return idle(emptyText || ''); }
        const n = (st.windows || []).length;
        if (!n) { return idle('Обход: ни одно окно не помещается в полосу приёмника'); }
        if (!st.running) { return idle('Обход остановлен · окон: ' + n); }
        const win = st.windows[st.index] || {};
        const end = st.window_end_ms || nowMs;
        const left = Math.max(0, Math.ceil((end - nowMs) / 1000));
        const tail = retune && retune.fromHz && nowMs < retune.untilMs
            ? 'перестройка с ' + sweepMHzText(retune.fromHz)
            : 'осталось ' + formatMinSec(left);
        const start = st.window_start_ms;
        const progress = start > 0 && end > start
            ? Math.max(0, Math.min(1, (nowMs - start) / (end - start))) : null;
        return {
            freq: sweepMHzText(win.center_hz),
            status: 'окно ' + (st.index + 1) + '/' + n + ' · ' + tail,
            progress: progress,
        };
    }

    /**
     * Строка статуса «Авто приём»: имя TX, частота, i/N, остаток стоянки.
     * @param {?Object} st — sweep_state
     * @param {number} nowMs
     * @returns {string}
     */
    function autoRxStatusModel(st, nowMs) {
        if (!st || !st.running || st.policy !== 'tx') { return ''; }
        const wins = st.windows || [];
        const n = wins.length;
        if (!n) { return ''; }
        const win = wins[st.index] || {};
        const name = win.label || 'TX';
        const end = st.window_end_ms || nowMs;
        const left = Math.max(0, Math.ceil((end - nowMs) / 1000));
        return name + ' · ' + sweepMHzText(win.center_hz) +
            ' · ' + (st.index + 1) + '/' + n +
            ' · осталось ' + formatMinSec(left);
    }

    /**
     * NORAD текущей цели «Авто приём» из окна обхода; 0 — не Авто / нет цели.
     * @param {?Object} st
     * @param {boolean} autoRx
     * @returns {number}
     */
    function autoRxTargetNorad(st, autoRx) {
        if (!autoRx || !st || !st.running || st.policy !== 'tx') { return 0; }
        const win = (st.windows || [])[st.index];
        return win && win.norad_id > 0 ? win.norad_id : 0;
    }

    /**
     * Доплер текущей цели hop по маркерам на спектре, Гц.
     * Без этого при выключенном демодуляторе LO на номинале, а риска TX — уже
     * со сдвигом Доплера, и «середина» с маркером расходятся на десятки кГц.
     * При нескольких TX одного NORAD — маркер ближе к center_hz окна (не первый в списке).
     * @param {number} noradId
     * @param {number} centerHz — center_hz окна hop
     * @param {Array} markers — _txMarkList
     * @returns {number}
     */
    function autoRxDopplerFromMarkers(noradId, centerHz, markers) {
        const m = autoRxMatchTx(noradId, centerHz, markers);
        if (!m || !(m.freqHz > 0) || !(m.nominalHz > 0)) { return 0; }
        return m.freqHz - m.nominalHz;
    }

    /**
     * Маркер TX цели hop: тот же NORAD, номинал ближе всего к center_hz окна.
     * Без подходящего маркера — null (профиль демода не трогаем).
     * @param {number} noradId
     * @param {number} centerHz
     * @param {Array} markers — _txMarkList
     * @returns {?Object}
     */
    /**
     * Подпись канала при профиле «Авто»: проба кандидатов или выбранный профиль.
     * @param {object} st — demod_status
     * @param {function(string): string} [nameOf] — id профиля → подпись
     */
    function autoDemodStatusLabel(st, nameOf) {
        if (!st || st.demod !== 'auto') { return ''; }
        const label = typeof nameOf === 'function' ? nameOf : function(id) { return id; };
        if (st.auto_phase === 'locked' && st.auto_locked) {
            return 'Выбран ' + label(st.auto_locked);
        }
        const ids = Array.isArray(st.auto_ids) ? st.auto_ids : [];
        if (ids.length) {
            return 'Проба — ' + ids.map(label).join(' · ');
        }
        const n = typeof st.auto_candidates === 'number' ? st.auto_candidates : 0;
        return n > 0 ? ('Проба — ' + n) : 'Проба…';
    }

    function autoRxMatchTx(noradId, centerHz, markers) {
        if (!(noradId > 0) || !Array.isArray(markers) || !markers.length) { return null; }
        let best = null;
        let bestDist = Infinity;
        for (let i = 0; i < markers.length; i++) {
            const m = markers[i];
            if (!m || m.noradId !== noradId) { continue; }
            const nom = m.nominalHz > 0 ? m.nominalHz : 0;
            if (!(nom > 0)) { continue; }
            const dist = Math.abs(nom - (centerHz || 0));
            if (dist < bestDist) {
                bestDist = dist;
                best = m;
            }
        }
        return best;
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
        /** Полоса обзора из последнего кадра, Гц. */
        this._streamSpanHz = null;
        /** Масштаб спектра «Спектра» (окно SpectrumStream.zoomView); null — вся полоса. */
        this._zoom = null;
        /** Действующая поправка Доплера, Гц. Пока демодулятор включён, середина графика стоит на ней. */
        this._dopplerViewHz = 0;
        /** Видимый сглаженный кадр для линии под курсором. */
        this._cursorTrace = null;
        this._cursorClientX = null;
        this._cursorClientY = null;
        /** Бинов в последнем кадре: по ним режется окно масштаба. */
        this._lastBins = 0;
        this._zoomBtn = null;
        /** Шкала dB по данным потока, общая для спектров и водопада. */
        this._streamRange = null;
        /** Накопление спектра (SPEC_MODES), его кнопки и N для «Накопления». */
        this._specMode = SPEC_MODE_DEFAULT;
        this._specModeButtons = [];
        this._specAvgN = SPEC_AVG_N_DEFAULT;
        this._specAvgNSelects = [];
        /** Последний кадр удержания: рисуется вместе со следующим мгновенным. */
        this._heldValues = null;
        /** Последний кадр потока, ждущий отрисовки (coalesce до rAF). */
        this._pendingFrame = null;
        /** Id requestAnimationFrame отрисовки спектра; 0 — не запланирована. */
        this._drawRaf = 0;
        /** Сглаживание линии на экране (не серверное накопление). */
        this._displaySmooth = null;
        /** Сдвиг уровней из dBFS в dBm на входе антенны; null — экран в dBFS. */
        this._levelOffset = null;
        /** Тракт, для которого получен _levelOffset. */
        this._levelPath = null;
        /** Запросы перестройки в пути: пока они есть, кадры не двигают регулятор. */
        this._tunePending = 0;
        /** Идёт запрос перестройки: следующий ждёт и уходит последним, без очереди. */
        this._tuneFlight = false;
        /** Последняя запрошенная частота, МГц. Пока запрос в пути, сюда пишется более новая. */
        this._tuneQueuedMHz = null;
        /**
         * Куда перемотка спектра уже поставила регулятор, Гц.
         * Пока кадр потока не догнал это значение, старый центр регулятор не возвращает.
         */
        this._scrubHz = null;
        /** Таймер отложенной перестройки: пока идёт, копится последняя частота. */
        this._scrubTimer = null;
        /** Раньше этого момента промотка новую частоту не шлёт. */
        this._scrubNotBefore = 0;
        /** Жест уже отпущен: догон частоты не ждёт паузу. */
        this._scrubFlush = false;
        /** Кнопка ещё зажата: приёмник ждёт отпускания, картинка едет за указателем. */
        this._scrubHold = false;
        /** Один кадр экрана на пачку движений указателя, чтобы водопад не дёргался. */
        this._scrubVisRaf = 0;
        this._scrubVisBefore = null;
        /** Последняя линия спектра в долях шкалы, на всю полосу кадра. Для сдвига без нового кадра. */
        this._lastLine = null;
        this._offAccess = null;
        this._accessPanel = null;
        this._panorama = null;
        this._passProfile = null;
        this._passTimeline = null;
        this._demodPanel = null;
        this._gallery = null;
        this._demodInfoModal = null;
        this._onPanoramaTune = null;

        this._activeTab = 'spectrum';
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
        this._view = 'spectrum';
        /** Состояние обхода тракта (GET /api/sdr/sweep, SSE sweep_state); null — обхода нет. */
        this._sweep = null;
        this._sweepEmptyText = '';
        /** Поток снимков окон и тракт, для которого он открыт. */
        this._panStream = null;
        this._panPath = null;
        this._sweepTimer = null;
        /** Недавняя смена окна обхода: {fromHz, untilMs} для шапки. */
        this._sweepRetune = null;
        this._sweepStatusEl = null;
        this._sweepToggleEl = null;
        this._sweepFreqEl = null;
        this._sweepPathEl = null;
        this._sweepProgressEl = null;
        /** «Авто приём»: обход крутит тюнер на Спектре; ручная настройка приёмника закрыта. */
        this._autoRx = false;
        this._autoRxIncludeAir = false;
        this._autoRxEl = null;
        this._autoRxAirEl = null;
        this._autoRxStatusEl = null;
        /** center_hz и ключ последнего применённого hop (norad:center:modulation). */
        this._autoRxCenterHz = 0;
        this._autoRxApplyKey = '';
        /** Последний offset_hz с сервера: при «Авто приёме» не затирать наведение на вспышку. */
        this._channelOffsetHz = 0;
        this._resultTabButtons = [];
        this._resultViews = [];
        this._resultView = 'table';
        /** Участки «Эфира» во вкладке «Таблица»: band_signals + Кадры/Данные из telemetry. */
        this._airSignals = [];
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
        /** Поля колонки приёмника и демодулятора (отправка на сервер). */
        this._rxFields = null;
        /** Выключатель демодулятора с сервера: null, пока статус не пришёл. */
        this._serverDemodOff = null;
        this._demodHydrated = false;
        /** Оператор переключил выключатель, и сервер ещё не подтвердил. */
        this._demodToggleDirty = false;
        /** Правка канала до первого статуса: уйдёт, когда станет известен выключатель. */
        this._demodApplyQueued = false;
        this._rxApplyTimer = null;
        this._demodApplyTimer = null;
        /** Кнопки −/+ у усиления, сдвига и порога. В режиме просмотра они недоступны. */
        this._rxStepButtons = [];
        this._stepRepeatTimer = null;
        this._stepRepeatInterval = null;
        this._stepRepeated = false;
        /** Канал демодулятора поверх спектра и водопада. */
        this._chanOverlay = null;
        /** Риски передатчиков на «Спектре» и лента, по которой они подписываются. */
        this._txMarkers = null;
        this._txList = [];
        /** Риски с подписями из последнего статуса: по ним щелчок по сигналу КА ставит номинал. */
        this._txMarkList = [];
        /** Обнаруженные сигналы в строке над водопадом. */
        this._bandMarks = null;
        /** Таблица «Эфир» под панорамой — markRows. */
        this._airTable = null;
        /** Таблица «Эфир» под спектром — hearRows (HEAR). */
        this._specAirTable = null;

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
        this._initAirTable();
        this._initProductGallery();
        this._initDemodEvents();
        this._initRxPanel();
        this._subscribeState();
    }

    // ── Регистрация обработчиков с авто-очисткой ──────────────────────────

    ManualLayout.prototype._on = function(el, type, fn) {
        if (!el) { return; }
        el.addEventListener(type, fn);
        this._handlers.push([el, type, fn]);
    };

    // ── Вкладки: Спектр (HOLD) | Антенна ───────────────────────────────────
    //
    // «Панорама» скрыта: позже отдельный независимый режим. Обход на Спектре
    // ведёт «Авто приём» (_ensureSweep), без визита на панораму.
    // Активная вкладка между сессиями не сохраняется, вход открывает «Спектр».

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
     * Выбор вкладки оператором: «Спектр» и «Антенна». «Панорама» скрыта и
     * сюда не пускаем: обход не завязан на её UI.
     * @param {string} tabId — 'spectrum' | 'antenna' | (игнор: 'panorama')
     */
    ManualLayout.prototype._selectTab = function(tabId) {
        if (tabId === 'panorama') { return; }
        if (tabId === 'antenna') {
            this._enterAntenna();
            return;
        }
        this._setView(tabId);
        this._showTab(tabId);
    };

    /**
     * «Антенна» показывает спектр канала. Со «Спектра» канал не меняется.
     * С «Панорамы» приёмник встаёт на выделенный там передатчик, если обход
     * не идёт: перестройка остановила бы его, а «Антенна» и так покажет окно.
     */
    ManualLayout.prototype._enterAntenna = function() {
        const fromSweep = this._view !== 'spectrum';
        this._showTab('antenna');
        if (!fromSweep) { return; }
        const tx = !this._sweepRunning() && this._panorama &&
            typeof this._panorama.getSelectedTx === 'function'
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

    /**
     * Идёт приём, который оборвёт смена частоты / демода / полосы / тракта:
     * картинка SSTV (VIS есть, строки не закончены) или свежий кадр ТМИ.
     */
    ManualLayout.prototype._receptionBusy = function() {
        const st = this._demodStatus;
        if (!st || !st.active) { return false; }
        const p = st.picture;
        if (p && p.mode && (p.height == null || p.lines == null || p.lines < p.height)) {
            return true;
        }
        const last = Number(st.last_packet_ms) || 0;
        const now = Number(st.server_time_ms) || Date.now();
        return !!(st.squelch_open && last > 0 && now - last < 3000);
    };

    /**
     * Спросить, если действие оборвёт приём. reason — что собираются сделать.
     * @param {string} reason
     * @returns {boolean}
     */
    ManualLayout.prototype._confirmBreakReception = function(reason) {
        if (this._rxBreakArmed) { return true; }
        if (!this._receptionBusy()) { return true; }
        const what = reason || 'изменение параметров';
        return this._confirm('Идёт приём. ' + what + ' его оборвёт. Продолжить?');
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

    /** Снять сопровождение: клиентски + POST + сброс подсветки. Вид не меняем. */
    ManualLayout.prototype._clearFollow = function() {
        this._followFreq = null;
        if (this._panorama && typeof this._panorama.setFollowedFreq === 'function') {
            this._panorama.setFollowedFreq(null);
        }
        if (this._sm && typeof this._sm.clearTrackingSatellite === 'function') {
            this._sm.clearTrackingSatellite();
        }
        this._postTrackingReset();
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
     * Передатчики ленты — downlink из реестра у КА группы (поле transmitters
     * в satellite_group_update). Нет данных — список пуст. Передатчик КА,
     * который ещё не поднялся над горизонтом, показан молчащим.
     * @param {?Object} group
     * @returns {Array<Object>}
     */
    function transmittersFromGroup(group) {
        const list = [];
        if (!group || !Array.isArray(group.satellites)) { return list; }
        for (let i = 0; i < group.satellites.length; i++) {
            const sat = group.satellites[i];
            const txs = Array.isArray(sat.transmitters) ? sat.transmitters : [];
            // Основное имя из TLE (ISS, RS-44), не alias из скобок (ZARYA, BREEZE-KM R/B)
            const name = sat.sat_name || sat.sat_alias || ('NORAD ' + sat.norad_id);
            for (let j = 0; j < txs.length; j++) {
                const tx = txs[j];
                if (!(tx.freq_hz > 0)) { continue; }
                // В ленте только имя КА: частота и модуляция — соседние колонки.
                // Длинное description из первоисточника («Mode U - … AX.25…») уходит в подсказку.
                list.push({
                    uuid: tx.uuid,
                    noradId: sat.norad_id,
                    freqMHz: tx.freq_hz / 1e6,
                    modulation: tx.modulation || tx.mode || '',
                    baud: tx.baud || 0,
                    label: name,
                    description: tx.description || '',
                    active: sat.is_visible === true,
                });
            }
        }
        return list;
    }

    /**
     * Лента передатчиков по группе: КА из Плана сеансов, поэтому «Следить»
     * ведёт КА с позицией и пролётом.
     */
    ManualLayout.prototype._buildTransmittersFromGroup = function() {
        if (!this._panorama || typeof this._panorama.setTransmitters !== 'function') { return; }
        if (!this._sm || typeof this._sm.getSatelliteGroup !== 'function') { return; }
        this._txList = transmittersFromGroup(this._sm.getSatelliteGroup());
        const sm = this._sm;
        if (sm && typeof sm.getMarkerColor === 'function') {
            for (let i = 0; i < this._txList.length; i++) {
                this._txList[i].chipColor = sm.getMarkerColor(this._txList[i].noradId) || null;
            }
        }
        this._panorama.setTransmitters(this._txList);
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

    // ── Результат приёма: переключение «Таблица | Диаграмма | Снимки» ─────

    ManualLayout.prototype._initDemodResultTabs = function() {
        const tabs = document.getElementById('manual-result-tabs');
        if (!tabs) { return; }
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
    };

    /** @param {string} view — 'table' | 'chart' | 'pictures' */
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
            if (self._passProfile) { self._passProfile.draw(); }
        });
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
     * Что показывать: 'panorama' — снимки окон обхода, спектр канала выключен;
     * 'spectrum' — поток канала, панорама заморожена, обход остановлен.
     * Поток канала можно смотреть и на «Антенне».
     * @param {string} view — 'panorama' | 'spectrum'.
     */
    ManualLayout.prototype._setView = function(view) {
        const next = view === 'spectrum' ? 'spectrum' : 'panorama';
        this._view = next;
        const holdOnAntenna = next === 'spectrum' && this._activeTab === 'antenna';
        if (this._activeTab !== next && !holdOnAntenna) {
            this._showTab(next);
        }

        if (next === 'spectrum') {
            if (this._panorama) { this._panorama.deactivate(); }
            this._stopPanoramaFeed();
            // При «Авто приёме» обход остаётся: Спектр и «Эфир» едут за тюнером.
            if (this._sweepRunning() && !this._autoRx) { this._postSweep(false); }
            if (this._active) {
                this._startSpectrumTimer();
                if (this._wf) { this._wf.start(); }
                this._startStream();
            }
        } else {
            this._stopSpectrumTimer();
            this._stopStream();
            if (this._active) { this._startPanoramaFeed(); }
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
        this._rxFields = {
            span: document.getElementById('manual-rx-span'),
            gain: document.getElementById('manual-rx-gain'),
            agc: document.getElementById('manual-rx-agc'),
            dopplerHz: document.getElementById('manual-rx-doppler-val'),
            bandwidth: document.getElementById('manual-rx-filter-bw'),
            squelch: this._squelchInput,
            demod: this._rxPipelineSelect,
            baud: document.getElementById('manual-rx-baud'),
            baudField: document.getElementById('manual-rx-baud-field'),
            doppler: document.getElementById('manual-rx-doppler-mode'),
            applyDemod: document.getElementById('manual-demod-apply'),
        };
        this._demodProfilesById = {};
        this._dopplerNow = document.getElementById('manual-rx-doppler-now');
        const self = this;
        const agc = this._rxFields.agc;

        this._initFreqDial();
        this._initAudio();
        this._showMarker(null);

        this._bindNumericFields();
        this._loadDemodProfiles();
        this._on(agc, 'click', function() {
            self._setAgc(agc.getAttribute('aria-pressed') !== 'true');
            self._flushSettings('receiver');
        });
        this._on(this._rxPipelineSelect, 'change', function() {
            self._prepareAutoChannel();
            self._applyBandwidthLimit();
            self._syncBaudField();
            self._syncEyeFromPipeline();
            self._applyAccess();
            self._flushSettings('demod');
        });
        this._on(this._rxFields.applyDemod, 'click', function() {
            self._syncEyeFromPipeline();
            self._flushSettings('demod');
        });
        this._autoRxEl = document.getElementById('manual-rx-auto');
        this._autoRxAirEl = document.getElementById('manual-rx-auto-air');
        this._autoRxStatusEl = document.getElementById('manual-rx-auto-status');
        if (this._autoRxEl) {
            this._on(this._autoRxEl, 'click', function() { self._toggleAutoRx(); });
        }
        if (this._autoRxAirEl) {
            try {
                this._autoRxAirEl.checked = localStorage.getItem('ux.autoRxIncludeAir') === '1';
            } catch (e) { /* нет storage */ }
            this._autoRxIncludeAir = !!this._autoRxAirEl.checked;
            this._on(this._autoRxAirEl, 'change', function() {
                self._autoRxIncludeAir = !!self._autoRxAirEl.checked;
                try {
                    localStorage.setItem('ux.autoRxIncludeAir', self._autoRxIncludeAir ? '1' : '0');
                } catch (err) { /* нет storage */ }
            });
        }
        // Полоса и АРУ уходят сразу; набор усиления ждёт паузу.
        this._on(this._rxFields.span, 'change', function() { self._flushSettings('receiver'); });
        this._on(this._rxFields.bandwidth, 'change', function() {
            self._syncChannelOverlay();
            self._flushSettings('demod');
        });
        this._on(this._rxFields.doppler, 'change', function() {
            self._syncDopplerEntry();
            self._flushSettings('demod');
        });
        this._syncDopplerEntry();

        this._syncEyeFromPipeline();
        this._applyBandwidthLimit();
        this._renderAutoRx();
    };

    /**
     * «Слушать» и громкость: звук канала включается сразу, без «Установить»,
     * и права «Контроль» не требует — канал он не меняет.
     */
    ManualLayout.prototype._initAudio = function() {
        const btn = document.getElementById('manual-rx-audio');
        if (!btn || typeof window.AudioPlayer !== 'function') { return; }
        this._audioBtn = btn;
        this._audio = new window.AudioPlayer();
        const self = this;
        const vol = document.getElementById('manual-rx-volume');
        if (vol) {
            this._audio.setVolume(Number(vol.value) / 100);
            this._on(vol, 'input', function() { self._audio.setVolume(Number(vol.value) / 100); });
        }
        this._on(btn, 'click', function() {
            self._setListening(btn.getAttribute('aria-pressed') !== 'true');
        });
    };

    /** Звук канала выбранного тракта; без тракта слушать нечего. */
    ManualLayout.prototype._setListening = function(on) {
        if (!this._audio) { return; }
        const path = on ? this._radioPathName() : null;
        if (path) { this._audio.start(path); } else { this._audio.stop(); }
        if (this._audioBtn) { this._audioBtn.setAttribute('aria-pressed', path ? 'true' : 'false'); }
    };

    /**
     * Спутник, за Доплером которого идёт канал: «Авто» — настроенный передатчик,
     * а до настройки — тот, что уже задан на сервере; «Вручную» — 0.
     */
    ManualLayout.prototype._dopplerNorad = function() {
        const f = this._rxFields;
        if (!f || selectedValue(f.doppler) !== 'auto') { return 0; }
        // 0 на hop «Эфир» должен уйти на сервер: иначе || подставит старый NORAD с TX.
        if (this._chanNorad != null) { return this._chanNorad; }
        return this._serverDopplerNorad || 0;
    };

    /** Поправка Доплера из статуса: подпись, столбец и середина графика. */
    ManualLayout.prototype._showDoppler = function(st) {
        const hz = typeof st.doppler_hz === 'number' && isFinite(st.doppler_hz) ? st.doppler_hz : null;
        if (this._chanOverlay) { this._chanOverlay.setShift(hz || 0); }
        if (this._dopplerNow) { this._dopplerNow.textContent = hz === null ? '' : formatDopplerKHz(hz); }
        const next = hz || 0;
        if (next === this._dopplerViewHz) { return; }
        const before = this._specWindow();
        this._dopplerViewHz = next;
        if (!demodChecked()) { return; }
        const after = this._specWindow();
        if (!before || !after || (before.centerHz === after.centerHz && before.spanHz === after.spanHz)) { return; }
        this._applySpecView();
        if (this._wf && before.spanHz > 0 && typeof this._wf.remap === 'function') {
            this._wf.remap((before.centerHz - before.spanHz / 2) / 1e6, (before.centerHz + before.spanHz / 2) / 1e6);
        }
    };

    /** «Ручн.» — степпер в герцах; «Авто» — живая поправка. Unit «Гц» не показываем: узкая колонка. */
    ManualLayout.prototype._syncDopplerEntry = function() {
        const manual = !this._rxFields || selectedValue(this._rxFields.doppler) !== 'auto';
        const step = document.getElementById('manual-rx-doppler-step');
        const unit = document.getElementById('manual-rx-doppler-unit');
        if (step) { step.hidden = !manual; }
        if (unit) { unit.hidden = true; }
        if (this._dopplerNow) { this._dopplerNow.hidden = manual; }
    };

    function formatDopplerKHz(hz) {
        return (hz < 0 ? '−' : '+') + (Math.abs(hz) / 1000).toFixed(2) + ' кГц';
    }

    /** Поля усиления, сдвига и порога: только цифры и кнопки −/+. */
    ManualLayout.prototype._bindNumericFields = function() {
        const f = this._rxFields;
        if (!f) { return; }
        const keys = ['gain', 'dopplerHz', 'squelch', 'baud'];
        for (let i = 0; i < keys.length; i++) {
            this._bindNumericField(f[keys[i]], NUMERIC_FIELDS[keys[i]]);
        }
    };

    /** Каталог профилей с сервера → селектор и подписи панели «Обработка». */
    ManualLayout.prototype._loadDemodProfiles = function() {
        const self = this;
        if (typeof fetch !== 'function') {
            this._syncBaudField();
            return;
        }
        fetch('/api/sdr/demod-profiles', { credentials: 'same-origin' })
            .then(function(r) { return r.ok ? r.json() : Promise.reject(new Error('profiles')); })
            .then(function(list) {
                self._applyDemodProfiles(list);
            })
            .catch(function() {
                self._syncBaudField();
            });
    };

    ManualLayout.prototype._applyDemodProfiles = function(list) {
        const byId = {};
        const labels = {};
        if (Array.isArray(list)) {
            for (let i = 0; i < list.length; i++) {
                const p = list[i];
                if (!p || !p.id) { continue; }
                byId[p.id] = p;
                labels[p.id] = p.label || p.id;
            }
        }
        this._demodProfilesById = byId;
        if (typeof window !== 'undefined' && window.DemodPanel && typeof window.DemodPanel.setLabels === 'function') {
            window.DemodPanel.setLabels(labels);
        }
        fillDemodProfileSelect(this._rxPipelineSelect, list, selectedValue(this._rxPipelineSelect));
        this._syncBaudField();
        this._syncEyeFromPipeline();
    };

    /** Показать поле Baud только для baud_flexible; подставить дефолт профиля при пустом. */
    ManualLayout.prototype._syncBaudField = function() {
        const f = this._rxFields;
        if (!f || !f.baudField) { return; }
        const id = selectedValue(f.demod);
        const flex = profileBaudFlexible(this._demodProfilesById, id);
        f.baudField.hidden = !flex;
        if (!flex || !f.baud) { return; }
        const p = this._demodProfilesById[id];
        if (f.baud.value === '' || f.baud.value === '0') {
            const defBaud = p && p.baud > 0 ? p.baud : (id === 'ook' ? 480 : 9600);
            f.baud.value = String(Math.round(defBaud));
        }
    };

    ManualLayout.prototype._bindNumericField = function(input, spec) {
        if (!input || !spec) { return; }
        const self = this;
        this._on(input, 'input', function() {
            const next = filterNumericText(input.value, spec.kind);
            if (input.value !== next) { input.value = next; }
        });
        // Усиление — приёмник, сдвиг и порог — демодулятор. Пауза в наборе схлопывает символы в один запрос.
        const applyKind = spec.kind === 'gain' ? 'receiver' : 'demod';
        this._on(input, 'input', function() { self._scheduleSettings(applyKind); });
        this._on(input, 'keydown', function(e) { self._onNumericKey(input, spec, e); });
        if (!input.id) { return; }
        this._bindStepButton(document.getElementById(input.id + '-dec'), input, spec, -1);
        this._bindStepButton(document.getElementById(input.id + '-inc'), input, spec, 1);
    };

    ManualLayout.prototype._bindStepButton = function(btn, input, spec, dir) {
        if (!btn) { return; }
        const self = this;
        this._rxStepButtons.push(btn);
        // Кнопка не забирает фокус у поля, иначе набор цифр обрывается.
        this._on(btn, 'mousedown', function(e) {
            if (e && typeof e.preventDefault === 'function') { e.preventDefault(); }
        });
        this._on(btn, 'click', function() { self._onStepButton(input, spec, dir, false); });
        this._on(btn, 'pointerdown', function() { self._startStepRepeat(input, spec, dir); });
        this._on(btn, 'pointerup', function() { self._stopStepRepeat(); });
        this._on(btn, 'pointercancel', function() { self._stopStepRepeat(); });
        this._on(btn, 'pointerleave', function() { self._stopStepRepeat(); });
    };

    /** Клавиатура: стрелки шагают, прочие символы кроме цифр и знака не вводятся. */
    ManualLayout.prototype._onNumericKey = function(input, spec, e) {
        if (!e || !input || input.disabled) { return; }
        const key = e.key;
        if (key === 'ArrowUp' || key === 'ArrowDown') {
            if (typeof e.preventDefault === 'function') { e.preventDefault(); }
            this._onStepButton(input, spec, key === 'ArrowUp' ? 1 : -1, true);
            return;
        }
        if ((key === '-' && spec.kind === 'gain') || (key === '.' && spec.kind === 'offset')) {
            if (typeof e.preventDefault === 'function') { e.preventDefault(); }
            return;
        }
        if (key === '-' || key === '.' || (typeof key === 'string' && key.length === 1 && key >= '0' && key <= '9')) {
            return;
        }
        if (key === 'Backspace' || key === 'Delete' || key === 'Tab' || key === 'Enter' || key === 'Escape'
            || key === 'ArrowLeft' || key === 'ArrowRight' || key === 'Home' || key === 'End') {
            return;
        }
        if (e.ctrlKey || e.metaKey || e.altKey) { return; }
        if (typeof key === 'string' && key.length === 1 && typeof e.preventDefault === 'function') {
            e.preventDefault();
        }
    };

    /**
     * Шаг поля. fromRepeat отличает автоповтор от click, который браузер
     * присылает следом за отпусканием кнопки и иначе шагнул бы ещё раз.
     */
    ManualLayout.prototype._onStepButton = function(input, spec, dir, fromRepeat) {
        if (!fromRepeat && this._stepRepeated) { return; }
        if (!input || input.disabled || !spec) { return; }
        input.value = stepNumericText(input.value, spec.step * (dir < 0 ? -1 : 1), spec);
        if (typeof input.dispatchEvent === 'function' && typeof Event === 'function') {
            input.dispatchEvent(new Event('input', { bubbles: true }));
        }
    };

    ManualLayout.prototype._startStepRepeat = function(input, spec, dir) {
        const self = this;
        this._stopStepRepeat();
        this._stepRepeated = false;
        this._stepRepeatTimer = setTimeout(function() {
            self._stepRepeated = true;
            self._stepRepeatInterval = setInterval(function() {
                self._onStepButton(input, spec, dir, true);
            }, STEP_REPEAT_EVERY_MS);
        }, STEP_REPEAT_DELAY_MS);
    };

    ManualLayout.prototype._stopStepRepeat = function() {
        if (this._stepRepeatTimer) {
            clearTimeout(this._stepRepeatTimer);
            this._stepRepeatTimer = null;
        }
        if (this._stepRepeatInterval) {
            clearInterval(this._stepRepeatInterval);
            this._stepRepeatInterval = null;
        }
        const self = this;
        if (this._stepRepeated) {
            setTimeout(function() { self._stepRepeated = false; }, 0);
        }
    };

    /** Значение выбранного пункта селектора; null — пунктов нет. */
    function selectedValue(sel) {
        const opt = sel && sel.options ? sel.options[sel.selectedIndex] : null;
        return opt ? opt.value : null;
    }

    /** Выбирает пункт селектора по значению; false — такого пункта нет. */
    function selectByValue(sel, value) {
        if (!sel || !sel.options) { return false; }
        for (let j = 0; j < sel.options.length; j++) {
            if (String(sel.options[j].value) === String(value)) {
                sel.selectedIndex = j;
                return true;
            }
        }
        return false;
    }

    /** Подпись частоты дискретизации: «2.048 МГц», «300 кГц». */
    function rateLabel(hz) {
        return hz >= 1e6
            ? Number((hz / 1e6).toFixed(3)) + ' МГц'
            : Number((hz / 1e3).toFixed(1)) + ' кГц';
    }

    /**
     * Пункты «Полосы» по списку частот устройства; тот же список не перестраивается.
     * Значение — частота дискретизации, подпись — видимая ширина из spans (края окна сервер отрезает).
     */
    function fillSpanOptions(sel, rates, spans) {
        if (!sel || !sel.options) { return; }
        const same = sel.options.length === rates.length && rates.every(function(hz, j) {
            return String(sel.options[j].value) === String(hz);
        });
        if (same) { return; }
        while (sel.options.length) { sel.remove(0); }
        rates.forEach(function(hz, j) {
            const opt = document.createElement('option');
            opt.value = String(hz);
            opt.text = rateLabel(Array.isArray(spans) && spans[j] > 0 ? spans[j] : hz);
            sel.add(opt);
        });
    }

    /** Кнопка АРУ: состояние приходит из статуса приёмника, в браузере не хранится. */
    ManualLayout.prototype._setAgc = function(on) {
        const btn = this._rxFields ? this._rxFields.agc : null;
        if (!btn) { return; }
        btn.classList.toggle('is-on', on);
        btn.setAttribute('aria-pressed', on ? 'true' : 'false');
    };

    /**
     * Частота, набранная на индикаторе. SET её ещё не отправлял: сюда она
     * попадает вместе с остальными настройками приёмника.
     * @returns {number}
     */
    ManualLayout.prototype._dialHz = function() {
        if (this._freqDial && typeof this._freqDial.getHz === 'function') {
            const hz = this._freqDial.getHz();
            if (hz > 0) { return Math.round(hz); }
        }
        return Math.round(this._rxFreqMHz * 1e6);
    };

    /**
     * Отложенная отправка секции. Повторный ввод сбрасывает ожидание, уходит последнее значение.
     * @param {'receiver'|'demod'} kind
     */
    ManualLayout.prototype._scheduleSettings = function(kind) {
        const key = kind === 'receiver' ? '_rxApplyTimer' : '_demodApplyTimer';
        const self = this;
        if (this[key]) { clearTimeout(this[key]); }
        this[key] = setTimeout(function() {
            self[key] = null;
            self._flushSettings(kind);
        }, ManualLayout.applyDebounceMs);
    };

    /**
     * Отправка секции сразу. Сбрасывает ожидающий набор, чтобы он не ушёл вторым запросом.
     * @param {'receiver'|'demod'} kind
     */
    ManualLayout.prototype._flushSettings = function(kind) {
        const key = kind === 'receiver' ? '_rxApplyTimer' : '_demodApplyTimer';
        if (this[key]) {
            clearTimeout(this[key]);
            this[key] = null;
        }
        if (kind === 'receiver') { this._applyReceiverSettings(); } else { this._applyDemodSettings(); }
    };

    /**
     * Настройки приёмника: частота, полоса и усиление (полоса/АРУ сразу, усиление после паузы).
     * Демодулятор не трогает. SET на индикаторе по-прежнему шлёт только частоту.
     */
    ManualLayout.prototype._applyReceiverSettings = function() {
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        if (ac && !ac.canControl()) {
            ac.setNotice('Менять настройки приёмника можно только в режиме «Контроль».');
            return;
        }
        if (this._autoRx) {
            if (ac) { ac.setNotice('При «Авто приёме» частоту и усиление задаёт обход.'); }
            return;
        }
        const path = this._radioPathName();
        const f = this._rxFields;
        if (!path || !f || typeof fetch !== 'function') { return; }

        const centerHz = this._dialHz();
        if (!(centerHz > 0)) {
            if (ac) { ac.setNotice('Настройки приёмника не применены: частота не задана.'); }
            return;
        }
        const agcOn = !!f.agc && f.agc.getAttribute('aria-pressed') === 'true';
        const gain = agcOn ? -1 : Number(f.gain ? f.gain.value : NaN);
        if (!isFinite(gain)) {
            if (ac) { ac.setNotice('Настройки приёмника не применены: усиление должно быть числом.'); }
            return;
        }
        const self = this;
        const cal = this._calibrationDb();
        postSettings(ac, '/api/sdr/receiver', {
            radio_path: path,
            center_hz: centerHz,
            sample_rate_hz: Number(selectedValue(f.span)),
            gain_db: gain,
        }, 'Настройки приёмника не применены', function(b) {
            if (!b || !b.status || self._radioPathName() !== path) { return; }
            const applied = b.status.center_hz > 0 ? b.status.center_hz : centerHz;
            self._rxFreqMHz = applied / 1e6;
            if (self._freqDial) { self._freqDial.setHz(applied); }
            self._applyReceiverStatus(path, cal, b.status, 'receiver');
        });
    };

    /**
     * «Установить» секции «Демодулятор»: профиль, полоса канала, порог и Доплер.
     * Порог сервер держит в dBFS. На экране он в dBm, если усиление уже известно,
     * и переводится по этому усилению, а не по ещё не отправленному.
     */
    ManualLayout.prototype._applyDemodSettings = function(fromQueue) {
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        if (ac && !ac.canControl()) {
            ac.setNotice('Менять настройки демодулятора можно только в режиме «Контроль».');
            return;
        }
        // Повтор из очереди уже спрашивал, пока статус не пришёл.
        if (!fromQueue && !this._confirmBreakReception('Смена профиля или полосы канала')) {
            return;
        }
        const path = this._radioPathName();
        const f = this._rxFields;
        if (!path || !f || typeof fetch !== 'function') { return; }

        const manualHz = Number(f.dopplerHz ? f.dopplerHz.value : 0);
        let squelch = Number(f.squelch ? f.squelch.value : NaN);
        if (![manualHz, squelch].every(isFinite)) {
            if (ac) { ac.setNotice('Настройки демодулятора не применены: доплер и порог должны быть числами.'); }
            return;
        }
        if (this._levelOffset !== null) { squelch -= this._levelOffset; }
        // Выключенный по умолчанию переключатель ещё не знает сервер. Не гасим им приём.
        const box = document.getElementById('manual-rx-channel-show');
        if (box && !this._demodHydrated && !this._demodToggleDirty) {
            this._demodApplyQueued = true;
            return;
        }

        const self = this;
        const cal = this._calibrationDb();
        const demodId = selectedValue(f.demod);
        const body = {
            radio_path: path,
            // При «Авто приёме» сервер наводит Offset на пик вспышки — сохраняем его.
            offset_hz: this._autoRx ? Math.round(this._channelOffsetHz || 0) : 0,
            bandwidth_hz: Number(selectedValue(f.bandwidth)),
            squelch_dbfs: Number(squelch.toFixed(1)),
            demod: demodId,
            demod_off: this._demodOffForApply(),
            doppler_norad: this._dopplerNorad(),
            doppler_auto: selectedValue(f.doppler) === 'auto',
            doppler_manual_hz: Math.round(manualHz),
        };
        if (profileBaudFlexible(this._demodProfilesById, demodId) && f.baud) {
            const baud = Number(f.baud.value);
            if (isFinite(baud) && baud > 0) { body.baud = baud; }
        }
        postSettings(ac, '/api/sdr/channel', body, 'Настройки демодулятора не применены', function(b) {
            if (b && b.status && self._radioPathName() === path) {
                self._applyReceiverStatus(path, cal, b.status, 'demod');
            }
        });
    };

    /**
     * Отправка настроек. failText — начало сообщения об ошибке.
     * @param {Object|null} ac
     * @param {string} url
     * @param {Object} body
     * @param {string} failText
     * @param {function(Object): void} onOk
     */
    function postSettings(ac, url, body, failText, onOk) {
        const notice = function(text) { if (ac) { ac.setNotice(text); } };
        fetch(url, {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        })
            .then(function(r) {
                if (r.status === 403) {
                    notice('Режим «Контроль» отключён, ' + failText.charAt(0).toLowerCase() + failText.slice(1) + '.');
                    if (ac) { ac.refresh(); }
                    return null;
                }
                return r.json().then(function(b) {
                    if (!r.ok) {
                        notice(failText + ': ' + ((b && b.error) || ('HTTP ' + r.status)));
                        return;
                    }
                    notice('');
                    onOk(b);
                }, function() {
                    if (!r.ok) { notice(failText + ': HTTP ' + r.status); }
                });
            })
            .catch(function(err) {
                notice(failText + ': ' + err.message);
            });
    }

    /** Поправка dBm тракта из конфига; undefined — не задана. */
    ManualLayout.prototype._calibrationDb = function() {
        const mm = (typeof window !== 'undefined') ? window._modeManager : null;
        const rp = mm && typeof mm.getRadioPath === 'function' ? mm.getRadioPath() : null;
        return rp ? rp.calibration_db : undefined;
    };

    /**
     * Статус приёмника: единицы уровня по его усилению, поля колонки по его настройкам.
     * @param {string} path
     * @param {*} cal — поправка dBm тракта на момент запроса
     * @param {Object} st
     */
    /**
     * @param {string} [part] — «receiver» или «demod»: заполнить только свою секцию.
     *   Без part обе секции следуют за статусом.
     */
    ManualLayout.prototype._applyReceiverStatus = function(path, cal, st, part) {
        if (part !== 'demod' && typeof cal === 'number') {
            this._levelPath = path;
            this._setLevelOffset(window.SpectrumStream.dbmOffset(cal, st.gain_db));
        }
        this._fillReceiverFields(st, part);
    };

    /**
     * Канал и усиление, изменённые на сервере (с этого или другого клиента): поля, канал на спектре
     * и единицы уровня следуют за ними. Статус приходит каждую секунду, но поля трогает только
     * смена настроек, поэтому недописанная правка оператора не затирается.
     */
    ManualLayout.prototype._followServerChannel = function(st) {
        if (typeof st.offset_hz !== 'number' && typeof st.bandwidth_hz !== 'number') { return; }
        if (typeof st.offset_hz === 'number') { this._channelOffsetHz = st.offset_hz; }
        const ch = {
            offset_hz: st.offset_hz, bandwidth_hz: st.bandwidth_hz, squelch_dbfs: st.squelch_dbfs,
            demod: st.demod, demod_off: !!st.demod_off, doppler_norad: st.doppler_norad || 0,
            doppler_auto: typeof st.doppler_auto === 'boolean' ? st.doppler_auto : null,
            doppler_manual_hz: typeof st.doppler_manual_hz === 'number' ? st.doppler_manual_hz : null,
            baud: typeof st.baud === 'number' ? st.baud : 0,
        };
        if (typeof st.gain_db === 'number' && st.gain_db !== this._serverGain) {
            // Порог в dBm зависит от усиления, поэтому канал заполняется заново вместе с ним.
            this._applyReceiverStatus(st.radio_path, this._calibrationDb(), { gain_db: st.gain_db, channel: ch });
            return;
        }
        if (channelKey(ch) === this._serverChannel) {
            // Настройки те же, но выключатель мог остаться в положении из разметки.
            this._showDemodSwitch(!!ch.demod_off);
            return;
        }
        this._fillReceiverFields({ channel: ch });
    };

    function channelKey(ch) {
        return [ch.offset_hz, ch.bandwidth_hz, ch.squelch_dbfs, ch.demod, ch.demod_off ? 1 : 0,
            ch.doppler_norad || 0, ch.doppler_auto === true ? 1 : ch.doppler_auto === false ? 0 : '',
            ch.doppler_manual_hz, ch.baud || 0].join('|');
    }

    /**
     * Что писать в demod_off. После статуса это значение сервера, пока оператор сам не переключил.
     * @returns {boolean}
     */
    ManualLayout.prototype._demodOffForApply = function() {
        if (this._demodToggleDirty) { return !demodChecked(); }
        if (typeof this._serverDemodOff === 'boolean') { return this._serverDemodOff; }
        return !demodChecked();
    };

    /**
     * Выключатель по статусу сервера. Пока запрос переключения в пути, положение не затирается.
     * Поле demod_off в ответе пропущено, когда демодулятор включён.
     * @param {boolean} off
     */
    ManualLayout.prototype._showDemodSwitch = function(off) {
        this._serverDemodOff = !!off;
        this._demodHydrated = true;
        // Статус сервера задаёт положение. Свой щелчок до статуса не остаётся на экране.
        this._demodToggleDirty = false;
        const box = document.getElementById('manual-rx-channel-show');
        if (!box) { return; }
        box.checked = !off;
        if (this._chanOverlay) { this._chanOverlay.setVisible(box.checked); }
    };

    /** Галочка в заголовке «Демодулятор»: включена, пока сервер не сказал обратное. */
    function demodChecked() {
        const box = document.getElementById('manual-rx-channel-show');
        return !box || box.checked;
    }

    /**
     * Поля колонки по статусу сервера; порог — в единицах экрана.
     * @param {string} [part] — «receiver» или «demod», иначе обе секции.
     */
    ManualLayout.prototype._fillReceiverFields = function(st, part) {
        const f = this._rxFields;
        if (!f) { return; }
        if (part !== 'demod') {
            if (Array.isArray(st.sample_rates) && st.sample_rates.length) {
                fillSpanOptions(f.span, st.sample_rates, st.spans_hz);
            }
            if (typeof st.sample_rate === 'number') { selectByValue(f.span, st.sample_rate); }
            if (typeof st.gain_db === 'number') {
                this._serverGain = st.gain_db;
                this._setAgc(st.gain_db < 0);
                if (st.gain_db >= 0 && f.gain) { f.gain.value = String(st.gain_db); }
            }
        }
        if (part === 'receiver') { return; }
        const ch = st.channel;
        if (!ch) { return; }
        if (typeof ch.offset_hz === 'number') { this._channelOffsetHz = ch.offset_hz; }
        this._serverChannel = channelKey(ch);
        this._showDemodSwitch(!!ch.demod_off);
        if (this._demodApplyQueued) {
            // Поля оператора важнее пришедшего статуса: уходит его правка и выключатель сервера.
            this._demodApplyQueued = false;
            this._applyDemodSettings(true);
            return;
        }
        if (f.dopplerHz && typeof ch.doppler_manual_hz === 'number') {
            f.dopplerHz.value = String(ch.doppler_manual_hz);
        }
        if (typeof ch.bandwidth_hz === 'number') { selectByValue(f.bandwidth, ch.bandwidth_hz); }
        if (f.squelch && typeof ch.squelch_dbfs === 'number') {
            f.squelch.value = String(Number((ch.squelch_dbfs + (this._levelOffset || 0)).toFixed(1)));
        }
        if (ch.demod && selectByValue(f.demod, ch.demod)) { this._syncEyeFromPipeline(); }
        if (f.baud && typeof ch.baud === 'number' && ch.baud > 0) {
            f.baud.value = String(Math.round(ch.baud));
        }
        this._syncBaudField();
        this._applyBandwidthLimit();
        this._serverDopplerNorad = ch.doppler_norad || 0;
        // Явный doppler_auto с сервера задаёт режим. Старый статус без поля режим не трогает:
        // панель открывается на «Авто», а «Вручную» оператора не сбрасывается каждым кадром.
        if (ch.doppler_auto === true || ch.doppler_auto === false) {
            selectByValue(f.doppler, ch.doppler_auto ? 'auto' : 'off');
        } else if (this._serverDopplerNorad) {
            selectByValue(f.doppler, 'auto');
        }
        this._syncDopplerEntry();
        this._showDemodSwitch(!!ch.demod_off);
        this._syncChannelOverlay();
        this._applyAccess();
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
     * Ручная перестройка оператором (SET или колесо на регуляторе). Спектр и
     * водопад сдвинутся, когда придут кадры с новым центром.
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
            this._tuneQueuedMHz = null;
            this._revertDial();
            return false;
        }
        if (this._autoRx) {
            if (ac) { ac.setNotice('При «Авто приёме» частоту задаёт обход.'); }
            this._tuneQueuedMHz = null;
            this._revertDial();
            return false;
        }
        const path = this._radioPathName();
        if (!path || typeof fetch !== 'function' || typeof freqMHz !== 'number' || !isFinite(freqMHz)) { return false; }
        // Колесо и перемотка шлют часто; спрашиваем один раз за эпизод приёма.
        if (this._receptionBusy() && !this._rxBreakArmed) {
            if (!this._confirm('Идёт приём. Перестройка приёмника его оборвёт. Продолжить?')) {
                this._tuneQueuedMHz = null;
                this._revertDial();
                return false;
            }
            this._rxBreakArmed = true;
        }
        // Перемотка шлёт сдвиг на каждое движение указателя. В пути один запрос,
        // более новая частота уходит следом, промежуточные не копятся.
        this._tuneQueuedMHz = freqMHz;
        if (this._tuneFlight) { return true; }
        this._dispatchTune();
        return true;
    };

    /** Отправляет последнюю запрошенную частоту. По ответу догоняет, если за время запроса цель сменилась. */
    ManualLayout.prototype._dispatchTune = function() {
        const path = this._radioPathName();
        const freqMHz = this._tuneQueuedMHz;
        if (!path || typeof fetch !== 'function' || typeof freqMHz !== 'number' || !isFinite(freqMHz)) { return; }
        const sentHz = Math.round(freqMHz * 1e6);
        const self = this;
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        const notice = function(text) { if (ac) { ac.setNotice(text); } };
        this._tuneFlight = true;
        this._tunePending++;
        fetch('/api/sdr/tune', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ radio_path: path, center_hz: sentHz }),
        })
            .then(function(r) {
                if (r.ok) { notice(''); return; }
                self._tuneQueuedMHz = null;
                self._revertDial();
                if (r.status === 403) {
                    notice('Режим «Контроль» отключён, приёмник не перестроен.');
                    if (ac) { ac.refresh(); }
                    return;
                }
                return r.json().then(function(b) {
                    notice('Приёмник не перестроен: ' + ((b && b.error) || ('HTTP ' + r.status)));
                }, function() {
                    notice('Приёмник не перестроен: HTTP ' + r.status);
                });
            })
            .catch(function(err) {
                self._tuneQueuedMHz = null;
                self._revertDial();
                notice('Приёмник не перестроен: ' + err.message);
            })
            .then(function() {
                self._tunePending--;
                self._tuneFlight = false;
                const queued = self._tuneQueuedMHz;
                const flush = self._scrubFlush;
                self._scrubFlush = false;
                if (typeof queued === 'number' && Math.round(queued * 1e6) !== sentHz) {
                    // Промотка не шлёт чаще паузы. Отпускание кнопки эту паузу не ждёт.
                    if (self._scrubHz !== null && !flush && Date.now() < (self._scrubNotBefore || 0)) {
                        self._armScrubTune();
                        return;
                    }
                    self._dispatchTune();
                }
            });
    };

    /** Регулятор и картинка на частоту приёмника из потока, если она уже известна. */
    ManualLayout.prototype._revertDial = function() {
        const before = this._specWindow();
        this._scrubHz = null;
        this._tuneQueuedMHz = null;
        if (this._scrubTimer) {
            clearTimeout(this._scrubTimer);
            this._scrubTimer = null;
        }
        if (this._streamCenterHz === null) { return; }
        this._rxFreqMHz = this._streamCenterHz / 1e6;
        if (this._freqDial) { this._freqDial.setHz(this._streamCenterHz); }
        this._commitSpecWindow(before);
        this._repaintSpectrum();
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

    /**
     * Профиль «Авто»: полоса канала под кадровые профили до 9600 (18 кГц).
     * Поля залочены — значение ставит код, не оператор.
     */
    ManualLayout.prototype._prepareAutoChannel = function() {
        const f = this._rxFields;
        if (!f || !f.demod || selectedValue(f.demod) !== 'auto') { return; }
        if (f.bandwidth) { selectByValue(f.bandwidth, '18000'); }
        if (f.doppler) { selectByValue(f.doppler, 'auto'); }
        this._syncDopplerEntry();
    };

    /** Без прав или при «Авто приёме» поля приёмника только показывают настройки. */
    ManualLayout.prototype._applyAccess = function() {
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        const noControl = !!ac && !ac.canControl();
        const rxLocked = noControl || !!this._autoRx;
        const demodLocked = noControl;
        if (this._freqDial && typeof this._freqDial.setReadOnly === 'function') {
            this._freqDial.setReadOnly(rxLocked);
        }
        const f = this._rxFields || {};
        const rxKeys = { span: 1, gain: 1, agc: 1 };
        for (const key in f) {
            if (!f[key]) { continue; }
            f[key].disabled = rxKeys[key] ? rxLocked : demodLocked;
        }
        // При «Автоприёме» профиль всегда «Авто» и руками не переключается.
        if (this._autoRx && f.demod) {
            selectByValue(f.demod, 'auto');
            f.demod.disabled = true;
        }
        // Профиль «Авто»: полосу, порог и Доплер задаёт подбор/hop — руками не крутим.
        const autoProfile = !!(f.demod && selectedValue(f.demod) === 'auto');
        if (autoProfile) {
            this._prepareAutoChannel();
            ['bandwidth', 'squelch', 'doppler', 'dopplerHz', 'applyDemod'].forEach(function(key) {
                if (f[key]) { f[key].disabled = true; }
            });
        }
        const buttons = this._rxStepButtons || [];
        for (let b = 0; b < buttons.length; b++) {
            const btn = buttons[b];
            if (!btn) { continue; }
            const id = btn.id || '';
            if (id.indexOf('gain') >= 0) {
                btn.disabled = rxLocked;
            } else if (autoProfile && (id.indexOf('squelch') >= 0 || id.indexOf('doppler') >= 0)) {
                btn.disabled = true;
            } else {
                btn.disabled = demodLocked;
            }
        }
        if (rxLocked || autoProfile) { this._stopStepRepeat(); }
        this._syncDopplerEntry();
        this._renderSweepBar();
        this._renderAutoRx();
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
        this._streamSpanHz = null;
        this._streamRange = new window.SpectrumStream.AutoRange();
        this._displaySmooth = typeof window.SpectrumStream.DisplaySmooth === 'function'
            ? new window.SpectrumStream.DisplaySmooth(DISPLAY_SMOOTH_ALPHA)
            : null;
        this._heldValues = null;
        this._pendingFrame = null;
        this._requestReceiverStatus(path);
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
     * Статус приёмника тракта: по нему заполняются поля колонки и, если у тракта есть
     * поправка, экран переходит в dBm. Пока ответа нет, у того же тракта остаются
     * прежние единицы.
     * @param {string} path
     */
    ManualLayout.prototype._requestReceiverStatus = function(path) {
        const cal = this._calibrationDb();
        if (typeof cal !== 'number' || path !== this._levelPath) {
            this._levelPath = null;
            this._setLevelOffset(null);
        }
        if (typeof fetch !== 'function') { return; }
        const self = this;
        fetch('/api/sdr/status?radio_path=' + encodeURIComponent(path), { credentials: 'same-origin' })
            .then(function(r) { return r.ok ? r.json() : null; })
            .then(function(st) {
                if (self._streamPath !== path || !st) { return; }
                self._applyReceiverStatus(path, cal, st);
            })
            .catch(function() { /* без статуса остаются dBFS и прежние поля */ });
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
        if (this._displaySmooth) { this._displaySmooth.reset(); }
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
        if (this._drawRaf && typeof cancelAnimationFrame === 'function') {
            cancelAnimationFrame(this._drawRaf);
        }
        this._drawRaf = 0;
        this._pendingFrame = null;
        if (this._stream) {
            this._stream.stop();
            this._stream = null;
        }
        this._streamPath = null;
        this._lastLine = null;
        this._markerShownMs = null;
        this._showMarker(null);
    };

    /**
     * Кадр с потока: удержание «Пика» запоминаем сразу, остальное — в очередь на rAF.
     * Если кадров больше, чем успевает экран, рисуем только последний (ADR-002 § сценарий 7).
     * @param {{header: Object, values: ?Float32Array}} frame
     */
    ManualLayout.prototype._onStreamFrame = function(frame) {
        const h = frame.header;
        // Пока кнопку не отпустили, кадр с другой частоты не подменяет картинку:
        // сигнал должен ехать за указателем, а не прыгать к промежуточной настройке.
        if (this._scrubHold && this._streamCenterHz && h.centerHz && h.centerHz !== this._streamCenterHz) {
            return;
        }
        if (frame.values && frame.values.length > 1) { this._lastBins = frame.values.length; }
        this._applyStreamBand(h.centerHz, h.spanHz);
        this._showRbw(h.spanHz, this._lastBins);
        if (!frame.values || !this._active || !this._streamRange) { return; }
        // В «Пике» сервер шлёт удержание и следом мгновенный: удержание только копим
        if (this._specMode === 'maxhold' && h.displayMode === FRAME_MODE_MAXHOLD) {
            this._heldValues = this._levelOffset === null
                ? frame.values
                : window.SpectrumStream.shiftLevels(frame.values, this._levelOffset);
            return;
        }
        this._pendingFrame = frame;
        if (this._drawRaf) { return; }
        const self = this;
        this._drawRaf = requestAnimationFrame(function() {
            self._drawRaf = 0;
            self._flushPendingFrame();
        });
    };

    /**
     * Кусок линии под текущее окно. Края за кадром добиваются padValue.
     * @param {Float32Array|Array} values
     * @param {?Object} sl
     * @param {number} padValue
     */
    function sliceShown(values, sl, padValue) {
        if (!sl || !values) { return values; }
        const src = values.subarray
            ? values.subarray(sl.start, sl.start + sl.count)
            : values.slice(sl.start, sl.start + sl.count);
        let left = sl.padLeft || 0;
        let right = sl.padRight || 0;
        if (left < 0) { right += left; left = 0; }
        if (right < 0) { left += right; right = 0; }
        if (left < 0) { left = 0; }
        if (right < 0) { right = 0; }
        if (!left && !right) { return src; }
        const n = left + src.length + right;
        if (typeof values.subarray === 'function') {
            const out = new values.constructor(n);
            out.fill(padValue);
            out.set(src, left);
            return out;
        }
        const out = new Array(n);
        for (let i = 0; i < n; i++) { out[i] = padValue; }
        for (let i = 0; i < src.length; i++) { out[left + i] = src[i]; }
        return out;
    }

    /** Отрисовка последнего отложенного кадра: сглаживание линии, шкала, FFT, водопад. */
    ManualLayout.prototype._flushPendingFrame = function() {
        const frame = this._pendingFrame;
        this._pendingFrame = null;
        if (!frame || !frame.values || !this._active || !this._streamRange) { return; }
        const h = frame.header;
        const raw = this._levelOffset === null
            ? frame.values
            : window.SpectrumStream.shiftLevels(frame.values, this._levelOffset);
        // Линия на экране сглажена; маркер читает сырой кадр (как уровень на входе)
        const values = this._displaySmooth ? this._displaySmooth.update(raw) : raw;
        // Водопад берёт сглаженный мгновенный; удержание «Пика» — как пришло
        const peak = this._specMode === 'maxhold';
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
        this._lastLine = line;
        // «Спектр» и водопад показывают окно масштаба, «Антенна» — всю полосу
        const win = this._specWindow();
        const sl = win && win.slice && win.slice.start + win.slice.count <= values.length ? win.slice : null;
        const shownDb = sliceShown(values, sl, -200);
        this._cursorTrace = win && win.spanHz > 0
            ? { values: shownDb, centerHz: win.centerHz, spanHz: win.spanHz }
            : null;
        if (this._fft) {
            this._fft.squelchDb = this._squelchDbForSpectrum();
            if (heldLine) { this._fft.draw(sliceShown(line, sl, 0), sliceShown(heldLine, sl, 0)); } else { this._fft.draw(sliceShown(line, sl, 0)); }
        }
        if (this._wf) { this._wf.pushLine(sliceShown(line, sl, 0)); }
        if (this._cursorClientX !== null) { this._paintCursor(); }
        if (this._antFft && this._activeTab === 'antenna') {
            this._antFft.squelchDb = this._squelchDbForSpectrum();
            if (heldLine) { this._antFft.draw(line, heldLine); } else { this._antFft.draw(line); }
            this._drawExpectedFreq(this._antFft, this._expectedFreqMHz());
        }
        const ts = h.timestampMs;
        if (this._markerShownMs === null || !(ts - this._markerShownMs < MARKER_REFRESH_MS)) {
            this._markerShownMs = typeof ts === 'number' ? ts : null;
            let mk;
            if (sl) {
                // Пик ищем в видимой части. Пустые края (Доплер увёл окно за полосу) не считаем сигналом.
                const shown = sliceShown(raw, sl, -200);
                if ((sl.padLeft || 0) || (sl.padRight || 0)) {
                    mk = window.SpectrumStream.markerReadout(shown, win.centerHz, win.spanHz);
                } else {
                    const binHz = h.spanHz / raw.length;
                    const leftHz = h.centerHz - h.spanHz / 2 + sl.start * binHz;
                    mk = window.SpectrumStream.markerReadout(shown, leftHz + sl.count * binHz / 2, sl.count * binHz);
                }
            } else {
                mk = window.SpectrumStream.markerReadout(raw, h.centerHz, h.spanHz);
            }
            this._showMarker(mk);
        }
    };

    /** Перерисовать линию спектра из последнего кадра под текущее окно, без новой строки водопада. */
    ManualLayout.prototype._repaintSpectrum = function() {
        const line = this._lastLine;
        const fft = this._fft;
        if (!line || !fft || typeof fft.draw !== 'function') { return; }
        const win = this._specWindow();
        const sl = win && win.slice && win.slice.start + win.slice.count <= line.length ? win.slice : null;
        fft.draw(sliceShown(line, sl, 0));
    };

    /**
     * Что видно на «Спектре»: центр и полоса графика, срез бинов кадра и центр
     * окна от центра приёмника. Без масштаба — вся полоса приёмника.
     * @returns {?{centerHz: number, spanHz: number, offsetHz: number, slice: ?Object}}
     */
    ManualLayout.prototype._specWindow = function() {
        const c = this._streamCenterHz;
        const s = this._streamSpanHz;
        if (!c) { return null; }
        const full = { centerHz: c, spanHz: s > 0 ? s : 0, offsetHz: 0, slice: null };
        if (!(s > 0) || !(this._lastBins > 1) || !window.SpectrumStream) { return full; }
        const dop = this._viewDopplerHz();
        // Промотка ещё не догнана кадром: окно смотрит на намеченную частоту,
        // пустые края добиваются, пока приёмник не перестроится.
        const scrubOff = this._scrubHz !== null ? this._scrubHz - c : 0;
        if (scrubOff) {
            const span = this._zoom ? this._zoom.spanHz : s;
            const sl = window.SpectrumStream.shiftedSlice(this._lastBins, s, span, dop + scrubOff);
            if (!sl) { return full; }
            return { centerHz: c + dop + scrubOff, spanHz: span, offsetHz: dop + scrubOff, slice: sl };
        }
        // Увеличение только сужает окно вокруг середины: столбца демодулятора
        // колесо не сдвигает, и после перестройки оно не прыгает обратно.
        if (this._zoom) {
            const sl = window.SpectrumStream.viewSlice(this._lastBins, s, {
                offsetHz: dop, spanHz: this._zoom.spanHz,
            });
            return { centerHz: c + sl.offsetHz, spanHz: sl.spanHz, offsetHz: sl.offsetHz, slice: sl };
        }
        if (!dop) { return full; }
        const sl = window.SpectrumStream.shiftedSlice(this._lastBins, s, s, dop);
        if (!sl) { return full; }
        return { centerHz: c + sl.offsetHz, spanHz: sl.spanHz, offsetHz: sl.offsetHz, slice: sl };
    };

    /** Центр и полоса «Спектра», водопада и канала — по окну масштаба. */
    ManualLayout.prototype._applySpecView = function() {
        const win = this._specWindow();
        if (!win) { return; }
        const centerMHz = win.centerHz / 1e6;
        const spanMHz = win.spanHz > 0 ? win.spanHz / 1e6 : null;
        if (this._fft) {
            this._fft.freqCenterMHz = centerMHz;
            if (spanMHz) { this._fft.freqSpanMHz = spanMHz; }
        }
        if (this._wf) {
            this._wf._freqCenterMHz = centerMHz;
            if (spanMHz) { this._wf._freqSpanMHz = spanMHz; }
        }
        if (this._chanOverlay && typeof this._chanOverlay.setView === 'function') {
            // Столбец демодулятора остаётся посредине и пока частота только намечена.
            const dop = this._viewDopplerHz();
            const col = this._scrubHz !== null ? dop : win.offsetHz;
            this._chanOverlay.setView(col, win.slice ? win.spanHz : 0);
        }
        if (this._txMarkers) { this._txMarkers.setView(win.offsetHz, win.slice ? win.spanHz : 0); }
        if (this._bandMarks) { this._bandMarks.setView(win.offsetHz, win.slice ? win.spanHz : 0); }
    };

    /**
     * Новое окно масштаба. Водопад переносит накопленные строки под новую шкалу,
     * поэтому история при прокрутке колеса не пропадает.
     * @param {?{offsetHz: number, spanHz: number}} view — null: вся полоса
     */
    ManualLayout.prototype._setZoom = function(view) {
        const before = this._specWindow();
        this._zoom = view;
        this._showZoom();
        this._commitSpecWindow(before);
    };

    /**
     * Окно спектра уже сменилось. Подписи и водопад переезжают на новую шкалу,
     * накопленные строки не стираются.
     * @param {?Object} before — окно до смены
     */
    ManualLayout.prototype._commitSpecWindow = function(before) {
        const after = this._specWindow();
        if (!before || !after || (before.centerHz === after.centerHz && before.spanHz === after.spanHz)) { return; }
        this._applySpecView();
        if (this._wf && before.spanHz > 0 && typeof this._wf.remap === 'function') {
            this._wf.remap((before.centerHz - before.spanHz / 2) / 1e6, (before.centerHz + before.spanHz / 2) / 1e6);
        }
    };

    /** «1:1» доступна, только пока спектр увеличен. */
    ManualLayout.prototype._showZoom = function() {
        if (this._zoomBtn) { this._zoomBtn.disabled = !this._zoom; }
    };

    /**
     * Колесо над спектром и водопадом: вертикальная прокрутка меняет масштаб
     * вокруг середины, явная горизонтальная крутит приёмник на видимые герцы.
     */
    ManualLayout.prototype._onSpecWheel = function(e) {
        const full = this._streamSpanHz;
        const win = this._specWindow();
        if (!win || !(full > 0) || !(this._lastBins > 1) || !this._fftCanvas || !this._fft) { return; }
        e.preventDefault();
        const rect = this._fftCanvas.getBoundingClientRect();
        const ml = this._fft._marginLeft || 0;
        const mr = this._fft._marginRight || 0;
        const plotW = rect.width - ml - mr;
        if (!(plotW > 0)) { return; }
        // Строки и страницы прокрутки переводим в пиксели
        const unit = e.deltaMode === 1 ? 16 : (e.deltaMode === 2 ? plotW : 1);
        const SS = window.SpectrumStream;
        const ax = Math.abs(e.deltaX || 0);
        const ay = Math.abs(e.deltaY || 0);
        if (ax > ay * WHEEL_PAN_RATIO) {
            this._panSpec(e.deltaX * unit * win.spanHz / plotW);
            return;
        }
        if (!e.deltaY) { return; }
        const factor = Math.exp(-e.deltaY * unit * WHEEL_ZOOM_RATE);
        const minSpanHz = MIN_ZOOM_BINS * full / this._lastBins;
        // Середина уже показанного окна: увеличение её не сдвигает.
        const atHz = win.offsetHz;
        const cur = { offsetHz: win.offsetHz, spanHz: this._zoom ? this._zoom.spanHz : win.spanHz };
        this._setZoom(SS.zoomView(cur, full, factor, atHz, minSpanHz));
    };

    /** Масштаб «Спектра»: колесо, перетаскивание по графику и кнопка «1:1». */
    ManualLayout.prototype._initSpecZoom = function(host) {
        const self = this;
        this._zoomBtn = document.getElementById('manual-spec-zoom-reset');
        this._on(this._zoomBtn, 'click', function() { self._setZoom(null); });
        this._showZoom();
        if (!host || typeof window.SpectrumStream !== 'function' ||
            typeof window.SpectrumStream.zoomView !== 'function') {
            return;
        }
        const onWheel = function(e) { self._onSpecWheel(e); };
        // passive: false, иначе preventDefault не удержит страницу от прокрутки
        host.addEventListener('wheel', onWheel, { passive: false });
        this._handlers.push([host, 'wheel', onWheel]);
    };

    /**
     * Шумовая полоса окна Блэкмана-Харриса в бинах: чистый тон на спектре сервера
     * занимает примерно столько, как полоса разрешения у анализатора.
     */
    const SPECTRUM_ENBW_BINS = 2;

    /** Подпись «RBW» над спектром: от полосы и числа бинов кадра, масштаб её не меняет. */
    ManualLayout.prototype._showRbw = function(spanHz, bins) {
        const el = document.getElementById('manual-spec-rbw');
        if (!el || !(spanHz > 0) || !(bins > 0)) { return; }
        const hz = spanHz / bins * SPECTRUM_ENBW_BINS;
        const text = 'RBW ' + (hz < 1000
            ? Math.round(hz) + ' Гц'
            : String(Math.round(hz / 100) / 10).replace('.', ',') + ' кГц');
        if (el.textContent !== text) { el.textContent = text; }
    };

    /** Линия под курсором: частота и уровень точки, на которую смотрит указатель. */
    ManualLayout.prototype._initSpecCursor = function(host) {
        this._cursorRoot = document.getElementById('manual-spec-cursor');
        this._cursorLine = document.getElementById('manual-spec-cursor-line');
        this._cursorMark = document.getElementById('manual-spec-cursor-mark');
        this._cursorTag = document.getElementById('manual-spec-cursor-tag');
        if (!host || !this._cursorRoot) { return; }
        this._cursorRoot.hidden = true;
        const self = this;
        this._on(host, 'pointermove', function(e) { self._onSpecPointer(e); });
        this._on(host, 'pointerleave', function() { self._onSpecPointerLeave(); });
    };

    ManualLayout.prototype._onSpecPointer = function(e) {
        if (!e || typeof e.clientX !== 'number') { return; }
        this._cursorClientX = e.clientX;
        this._cursorClientY = typeof e.clientY === 'number' ? e.clientY : null;
        this._paintCursor();
    };

    ManualLayout.prototype._onSpecPointerLeave = function() {
        this._cursorClientX = null;
        this._cursorClientY = null;
        this._hideCursor();
    };

    ManualLayout.prototype._hideCursor = function() {
        if (this._cursorRoot) { this._cursorRoot.hidden = true; }
    };

    /**
     * Рисует линию, если указатель над графиком, а не над подписями дБ слева.
     * Частота — точка оси под указателем, уровень — ближайший бин линии.
     */
    ManualLayout.prototype._paintCursor = function() {
        const root = this._cursorRoot;
        const canvas = this._fftCanvas;
        const fft = this._fft;
        const trace = this._cursorTrace;
        const host = document.getElementById('manual-spectrum');
        if (!root || !canvas || !fft || !trace || !host || this._cursorClientX === null) {
            this._hideCursor();
            return;
        }
        const plot = canvas.getBoundingClientRect();
        const box = host.getBoundingClientRect();
        const ml = fft._marginLeft || 0;
        const mr = fft._marginRight || 0;
        const plotW = plot.width - ml - mr;
        if (!(plotW > 0) || !(box.width > 0)) { this._hideCursor(); return; }
        const plotLeft = plot.left || 0;
        const boxLeft = box.left || 0;
        const plotTop = plot.top || 0;
        const boxTop = box.top || 0;
        const frac = (this._cursorClientX - plotLeft - ml) / plotW;
        const sampleFn = window.SpectrumStream && window.SpectrumStream.cursorSample;
        const sample = sampleFn ? sampleFn(trace.values, trace.centerHz, trace.spanHz, frac) : null;
        if (!sample) { this._hideCursor(); return; }
        const x = this._cursorClientX - boxLeft;
        if (this._cursorLine) { this._cursorLine.style.left = x + 'px'; }
        const dbMin = typeof fft._dbMin === 'number' ? fft._dbMin : -120;
        const dbMax = typeof fft._dbMax === 'number' ? fft._dbMax : 0;
        const mt = fft._marginTop || 0;
        const mb = fft._marginBottom || 0;
        const plotH = plot.height - mt - mb;
        if (this._cursorMark && plotH > 0 && dbMax > dbMin) {
            const v = Math.max(0, Math.min(1, (sample.dbfs - dbMin) / (dbMax - dbMin)));
            const y = (plotTop - boxTop) + mt + plotH * (1 - v);
            this._cursorMark.style.left = x + 'px';
            this._cursorMark.style.top = y + 'px';
            this._cursorMark.hidden = false;
        }
        if (this._cursorTag) {
            const unitEl = document.getElementById('manual-rx-marker-unit');
            const unit = unitEl && unitEl.textContent ? unitEl.textContent : 'dBFS';
            const level = sample.dbfs.toFixed(1).replace('-', '−');
            this._cursorTag.textContent = (sample.freqHz / 1e6).toFixed(3) + ' МГц  ' + level + ' ' + unit;
            const flip = x > box.width - 150;
            this._cursorTag.style.left = x + 'px';
            this._cursorTag.style.transform = flip ? 'translateX(calc(-100% - 8px))' : 'translateX(8px)';
            let top = this._cursorClientY === null ? 8 : this._cursorClientY - boxTop - 22;
            if (top < 4) { top = 4; }
            if (top > box.height - 22) { top = box.height - 22; }
            this._cursorTag.style.top = top + 'px';
        }
        root.hidden = false;
    };

    /**
     * Перетаскивание по графику и горизонтальное колесо крутят приёмник:
     * вправо — частота ниже. Масштаб при этом не сбрасывается.
     */
    ManualLayout.prototype._panSpec = function(dHz) {
        if (!(this._streamSpanHz > 0) || typeof dHz !== 'number' || !isFinite(dHz) || dHz === 0) { return; }
        this._scrubTune(dHz);
    };

    /**
     * Насколько можно увести центр картинки от центра кадра, Гц.
     * Увеличенное окно ездит внутри полосы. На всей полосе любой видимый сигнал
     * доезжает до центра, дальше кадра двигать некуда.
     * @param {number} full
     * @param {number} viewSpan
     * @returns {number}
     */
    function scrubLimitHz(full, viewSpan) {
        if (!(full > 0)) { return 0; }
        const view = viewSpan > 0 && viewSpan < full ? viewSpan : full;
        // Внутри полосы окно ездит до её края. Видимый сигнал при этом доезжает до центра.
        return Math.max((full - view) / 2, view / 2);
    }

    /**
     * Перемотка на dHz от уже намеченной частоты. Картинка едет за указателем,
     * приёмник получает частоту только когда кнопку отпустили.
     * @param {number} dHz
     */
    ManualLayout.prototype._scrubTune = function(dHz) {
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        if (ac && !ac.canControl()) {
            ac.setNotice('Перестраивать приёмник можно только в режиме «Контроль».');
            return;
        }
        if (this._scrubHz === null && this._streamCenterHz === null) { return; }
        const base = this._scrubHz !== null ? this._scrubHz : this._streamCenterHz;
        let next = Math.round(base + dHz);
        if (this._streamCenterHz) {
            const viewSpan = this._zoom ? this._zoom.spanHz : this._streamSpanHz;
            const limit = scrubLimitHz(this._streamSpanHz, viewSpan);
            const dop = this._viewDopplerHz();
            let total = dop + (next - this._streamCenterHz);
            if (total > limit) { total = limit; }
            if (total < -limit) { total = -limit; }
            next = Math.round(this._streamCenterHz + (total - dop));
        }
        if (!(next > 0) || next === base) { return; }
        if (!this._scrubVisRaf) { this._scrubVisBefore = this._specWindow(); }
        this._scrubHz = next;
        this._rxFreqMHz = next / 1e6;
        if (this._freqDial) { this._freqDial.setHz(next); }
        this._scheduleScrubVisual();
        this._armScrubTune();
    };

    /** Сдвигает линию и водопад один раз за кадр экрана, на последнюю частоту жеста. */
    ManualLayout.prototype._scheduleScrubVisual = function() {
        if (this._scrubVisRaf) { return; }
        const self = this;
        this._scrubVisRaf = requestAnimationFrame(function() {
            self._scrubVisRaf = 0;
            self._flushScrubVisual();
        });
    };

    /** Дорисовывает спектр под уже намеченную частоту. */
    ManualLayout.prototype._flushScrubVisual = function() {
        const before = this._scrubVisBefore;
        this._scrubVisBefore = null;
        if (before) { this._commitSpecWindow(before); }
        this._repaintSpectrum();
    };

    /**
     * Запоминает последнюю частоту промотки и шлёт её не чаще паузы.
     * Пока кнопка зажата, не шлёт: перестройка посреди жеста подменяет картинку.
     */
    ManualLayout.prototype._armScrubTune = function() {
        if (this._scrubHold) { return; }
        this._tuneQueuedMHz = this._rxFreqMHz;
        if (this._scrubTimer) { return; }
        const wait = Math.max(0, (this._scrubNotBefore || 0) - Date.now());
        const self = this;
        this._scrubTimer = setTimeout(function() {
            self._scrubTimer = null;
            if (self._scrubHz === null) { return; }
            self._scrubNotBefore = Date.now() + SCRUB_TUNE_MS;
            self._requestTune(self._rxFreqMHz);
        }, wait > 0 ? wait : SCRUB_TUNE_MS);
    };

    /** Кнопку отпустили: картинка доезжает до точки отпускания, уходит одна частота. */
    ManualLayout.prototype._endSpecScrub = function() {
        this._scrubHold = false;
        if (this._scrubVisRaf) {
            cancelAnimationFrame(this._scrubVisRaf);
            this._scrubVisRaf = 0;
            this._flushScrubVisual();
        }
        if (this._scrubTimer) {
            clearTimeout(this._scrubTimer);
            this._scrubTimer = null;
        }
        if (this._scrubHz === null) { return; }
        this._scrubFlush = true;
        this._scrubNotBefore = Date.now() + SCRUB_TUNE_MS;
        this._requestTune(this._rxFreqMHz);
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
        // «Полоса» следует только за сменой в потоке, чтобы не сбить выбор оператора
        const spanChanged = spanHz > 0 && spanHz !== this._streamSpanHz;
        const centerMHz = centerHz / 1e6;
        if (this._antFft) {
            this._antFft.freqCenterMHz = centerMHz;
            if (spanHz > 0) { this._antFft.freqSpanMHz = spanHz / 1e6; }
        }
        this._streamCenterHz = centerHz;
        if (spanChanged) {
            this._streamSpanHz = spanHz;
            if (this._rxFields) { selectByValue(this._rxFields.span, spanHz); }
        }
        // Масштаб переживает перестройку: окно остаётся той же ширины и снова
        // по центру частоты приёмника, чтобы в увеличении можно было крутить приёмник.
        if (changed && !first && !spanChanged && this._zoom) {
            const off = this._viewDopplerHz();
            this._zoom = { offsetHz: off, spanHz: this._zoom.spanHz };
        }
        if (spanChanged && !first && this._zoom) {
            this._zoom = null;
            this._showZoom();
        }
        this._applySpecView();
        // Промотка уже сдвинула историю на намеченную частоту. Кадр со старым
        // или промежуточным центром её не стирает.
        const keepHistory = this._scrubHz !== null && !spanChanged;
        if (this._wf && (changed || spanChanged)) {
            if (first) {
                // Шкала нарисована по заглушке. Первый кадр водопад не чистит,
                // поэтому подписи частот перерисовываем отдельно.
                this._wf.refresh();
            } else if (!keepHistory) {
                this._wf.clear();
                if (this._active) { this._wf.start(); }
            }
        }
        if ((changed || spanChanged) && !first && this._displaySmooth) {
            this._displaySmooth.reset();
        }
        if (this._chanOverlay && this._streamSpanHz) {
            this._chanOverlay.setBand(centerHz, this._streamSpanHz);
        }
        if (this._txMarkers && this._streamSpanHz) {
            this._txMarkers.setBand(centerHz, this._streamSpanHz);
        }
        if (this._bandMarks && this._streamSpanHz) {
            this._bandMarks.setBand(centerHz, this._streamSpanHz);
        }
        if (changed) { this._syncChannelOverlay(); }
        if (this._scrubHz !== null && centerHz === this._scrubHz) { this._scrubHz = null; }
        const editing = this._freqDial && typeof this._freqDial.isEditing === 'function' &&
            this._freqDial.isEditing();
        // Перемотка уже показала новую частоту: кадр со старым центром её не затирает.
        if (changed && this._tunePending === 0 && !editing && this._scrubHz === null) {
            this._rxFreqMHz = centerMHz;
            if (this._freqDial) { this._freqDial.setHz(centerHz); }
        }
    };

    /**
     * Порог шумоподавления в единицах шкалы спектра (dBm при известной калибровке).
     * null — статуса ещё нет, линию не рисуем.
     */
    ManualLayout.prototype._squelchDbForSpectrum = function() {
        const st = this._demodStatus;
        if (!st || typeof st.squelch_dbfs !== 'number' || !isFinite(st.squelch_dbfs)) {
            return null;
        }
        return st.squelch_dbfs + (this._levelOffset || 0);
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
        // afsk раньше fsk: иначе «AFSK 1200» цепляется за подпись «FSK 1200».
        const families = ['bpsk', 'afsk', 'gmsk', 'qpsk', 'fsk'];
        for (let i = 0; i < sel.options.length; i++) {
            const opt = sel.options[i];
            if (!opt.text) { continue; }
            const optKey = opt.text.toLowerCase().replace(/\s+/g, '');
            if (optKey === targetKey) {
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
        for (let i = 0; i < sel.options.length; i++) {
            const opt = sel.options[i];
            if (!opt.text) { continue; }
            const optKey = opt.text.toLowerCase().replace(/\s+/g, '');
            if (targetKey.indexOf(optKey) >= 0 || optKey.indexOf(targetKey) >= 0) {
                sel.selectedIndex = i;
                return;
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
        // Eye/constellation переехал в модалку «Информация»: модуляцию передаём туда.
        if (!this._demodInfoModal) { return; }
        const pipeline = this._getPipelineLabel();
        if (pipeline) { this._demodInfoModal.setEyeModulation(pipeline); }
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
        const specWrap = (this._fftCanvas ? this._fftCanvas.closest('.ml-spectrum') : null) ||
            document.getElementById('manual-spectrum');
        if (specWrap && typeof window.ChannelOverlay === 'function') {
            this._chanOverlay = new window.ChannelOverlay(specWrap, {
                plotCanvas: this._fftCanvas,
                clickTargets: [this._fftCanvas, this._wfCanvas],
                marginLeft: this._fft ? this._fft._marginLeft : 32,
                marginRight: this._fft ? this._fft._marginRight : 4,
                onPan: function(dHz) {
                    self._scrubHold = true;
                    self._panSpec(dHz);
                },
                onPanEnd: function() { self._endSpecScrub(); },
            });
        }
        if (specWrap && typeof window.TxMarkers === 'function') {
            this._txMarkers = new window.TxMarkers(specWrap, {
                plotCanvas: this._fftCanvas,
                marginLeft: this._fft ? this._fft._marginLeft : 32,
                marginRight: this._fft ? this._fft._marginRight : 4,
                onPick: function(m) { self._pickTxMarker(m); },
                colorOf: function(noradId) {
                    return self._sm && typeof self._sm.getMarkerColor === 'function'
                        ? self._sm.getMarkerColor(noradId) : null;
                },
            });
        }
        this._initBandMarks();
        this._initSpecZoom(specWrap);
        this._initSpecCursor(specWrap);
        this._initChannelMark();
        if (specWrap) {
            this._observeRaw(specWrap, function() {
                if (self._fft) { self._fft._resize(); }
                if (self._wf) { self._wf.refresh(); }
                if (self._chanOverlay) { self._chanOverlay.render(); }
                if (self._txMarkers) { self._txMarkers.render(); }
                if (self._bandMarks) { self._bandMarks.render(); }
            });
        }
    };

    /** Строка обнаруженных сигналов над водопадом и кнопка «Маркеры», которая её прячет. */
    ManualLayout.prototype._initBandMarks = function() {
        const strip = document.getElementById('manual-wf-marks');
        if (!strip || typeof window.BandMarks !== 'function') { return; }
        const self = this;
        this._bandMarks = new window.BandMarks(strip, {
            plotCanvas: this._wfCanvas,
            marginLeft: this._fft ? this._fft._marginLeft : 32,
            marginRight: this._fft ? this._fft._marginRight : 4,
            onPick: function(sig) { self._pickBandSignal(sig); },
            colorOf: function(noradId) {
                return self._sm && typeof self._sm.getMarkerColor === 'function'
                    ? self._sm.getMarkerColor(noradId) : null;
            },
            nameOf: function(noradId) { return self._satNameOf(noradId); },
        });
        const btn = document.getElementById('manual-spec-marks');
        let shown = true;
        try { shown = localStorage.getItem(STORAGE_BAND_MARKS) !== '0'; } catch (_e) { /* noop */ }
        const show = function(on) {
            strip.hidden = !on;
            if (btn) {
                btn.setAttribute('aria-pressed', on ? 'true' : 'false');
                if (btn.classList) { btn.classList.toggle('is-on', on); }
            }
            // Водопад меняет высоту без смены размера контейнера спектра
            if (self._wf) { self._wf.refresh(); }
            if (on) { self._bandMarks.render(); }
        };
        show(shown);
        this._on(btn, 'click', function() {
            shown = !shown;
            try { localStorage.setItem(STORAGE_BAND_MARKS, shown ? '1' : '0'); } catch (_e) { /* noop */ }
            show(shown);
        });
    };

    /**
     * Щелчок по обнаруженному сигналу. Сигнал КА с риской идёт через неё: приёмник
     * на номинал и «Доплер: Авто». Остальные перестраивают приёмник на центр участка,
     * Доплер вручную и ноль. Ширина — пункт «Полосы канала» под участок.
     * @param {Object} sig — участок из band_signals
     */
    ManualLayout.prototype._pickBandSignal = function(sig) {
        const f = this._rxFields;
        if (!f || !sig || !this._streamCenterHz) { return; }
        const bw = window.BandMarks.pickBandwidth(f.bandwidth ? f.bandwidth.options : null, sig.bandwidth_hz);
        const satellite = sig.origin === 'satellite' || sig.origin === 'satellite_probable';
        let tx = null;
        for (let i = 0; satellite && i < this._txMarkList.length; i++) {
            const m = this._txMarkList[i];
            if (m.noradId === sig.norad_id &&
                (!tx || Math.abs(m.freqHz - sig.center_hz) < Math.abs(tx.freqHz - sig.center_hz))) {
                tx = m;
            }
        }
        if (tx) {
            this._pickTxMarker(tx, bw);
            return;
        }
        if (this._requestTune(sig.center_hz / 1e6) === false) { return; }
        if (bw) { selectByValue(f.bandwidth, bw); }
        if (f.dopplerHz) { f.dopplerHz.value = '0'; }
        selectByValue(f.doppler, 'off');
        this._syncDopplerEntry();
        const svc = sig.structure && sig.structure.service;
        if (svc === 'sstv' || svc === 'sstv_probable') {
            this._setRxPipeline('SSTV');
            this._syncEyeFromPipeline();
        }
        this._syncChannelOverlay();
        this._applyDemodSettings();
    };

    /** Риски передатчиков из статуса демодулятора: подписи по ленте и группе. */
    ManualLayout.prototype._showTxMarkers = function(st) {
        if (!this._txMarkers || !window.TxMarkers) { return; }
        const group = this._sm && typeof this._sm.getSatelliteGroup === 'function' ? this._sm.getSatelliteGroup() : null;
        this._txMarkList = window.TxMarkers.markersWithLabels(st.tx_markers, this._txList, group);
        this._txMarkers.setMarkers(this._txMarkList);
        // Маркеры позже окна: Доплер шкалы и профиль демода под TX цели hop.
        if (this._autoRx) { this._syncAutoRxTarget(); }
    };

    /**
     * Щелчок по риске: приёмник на номинал передатчика, Доплер «Авто» за его КА.
     * Сервер прибавляет Доплер, график держит сигнал в середине.
     * @param {{noradId: number, nominalHz: number, label: string, tx: ?Object}} m
     * @param {?string} [bandwidthHz] — пункт «Полосы канала»; без него ширина прежняя
     */
    ManualLayout.prototype._pickTxMarker = function(m, bandwidthHz) {
        const f = this._rxFields;
        if (!f || !m || !this._streamCenterHz) { return; }
        if (this._requestTune(m.nominalHz / 1e6) === false) { return; }
        if (bandwidthHz) { selectByValue(f.bandwidth, bandwidthHz); }
        selectByValue(f.doppler, 'auto');
        this._syncDopplerEntry();
        const freqMHz = m.nominalHz / 1e6;
        if (m.tx) {
            if (this._demodPanel) { this._demodPanel.setChannel(m.tx); }
            this._setRxPipeline(m.tx.modulation);
            this._applyBandwidthLimit();
        }
        this._chanLabel = m.label;
        this._chanFreqMHz = freqMHz;
        this._chanNorad = m.noradId;
        this._syncEyeFromPipeline();
        this._syncChannelOverlay();
        this._applyDemodSettings();
    };

    /** Переключатель в заголовке «Демодулятор»: выключает приём и линии канала, спектр остаётся. */
    ManualLayout.prototype._initChannelMark = function() {
        const box = document.getElementById('manual-rx-channel-show');
        if (!box) { return; }
        if (this._chanOverlay) { this._chanOverlay.setVisible(box.checked); }
        const self = this;
        this._on(box, 'change', function() {
            self._setDemodOff(!box.checked);
        });
    };

    /** Выключатель уходит сразу, не дожидаясь «Установить». Без прав переключатель возвращается. */
    ManualLayout.prototype._setDemodOff = function(off) {
        const self = this;
        const box = document.getElementById('manual-rx-channel-show');
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        const show = function(on) {
            if (box) { box.checked = on; }
            if (this._chanOverlay) { this._chanOverlay.setVisible(on); }
        }.bind(this);
        if (ac && !ac.canControl()) {
            // Переключатель уже перещёлкнут. off — запрошенное «выключено» и прежнее положение.
            show(off);
            ac.setNotice('Выключать демодулятор можно только в режиме «Контроль».');
            return;
        }
        this._demodToggleDirty = true;
        show(!off);
        this._snapZoomToDoppler();
        // Включение: синхронизируем канал с UI. Один POST /demod не меняет поле Demod —
        // иначе на сервере остаётся старый профиль, а в селекторе уже «Авто».
        if (!off && this._rxFields) {
            if (this._autoRx && this._rxFields.demod) {
                selectByValue(this._rxFields.demod, 'auto');
            }
            this._prepareAutoChannel();
            this._applyDemodSettings();
        }
        const path = this._radioPathName();
        if (!path || typeof fetch !== 'function') { return; }
        const notice = function(text) { if (ac) { ac.setNotice(text); } };
        fetch('/api/sdr/demod', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ radio_path: path, demod_off: off }),
        })
            .then(function(r) {
                if (r.ok) { notice(''); return null; }
                self._demodToggleDirty = false;
                show(off);
                if (r.status === 403) {
                    notice('Режим «Контроль» отключён, демодулятор не переключён.');
                    if (ac) { ac.refresh(); }
                    return null;
                }
                return r.json().then(function(b) {
                    notice('Демодулятор не переключён: ' + ((b && b.error) || ('HTTP ' + r.status)));
                }, function() {
                    notice('Демодулятор не переключён: HTTP ' + r.status);
                });
            })
            .catch(function(err) {
                show(off);
                self._demodToggleDirty = false;
                notice('Демодулятор не переключён: ' + err.message);
            });
    };

    /**
     * Самая широкая слышимая полоса у профилей, которые переносят полосу в звук напрямую
     * (как sdr.DemodMaxBandwidthHz на сервере): у SSB тон отсчитывается от края полосы,
     * у CW — 700 Гц от середины. Шире сервер настройку не примет.
     */
    const WIDE_FM_BW = 200000;
    const PROFILE_MAX_BW = { usb: 4000, lsb: 4000, cw: 1000, wfm: WIDE_FM_BW };
    const WIDTH_NOT_HEARD = 'У этого профиля шире не слышно';
    const WIDTH_BROADCAST = 'У этого профиля полоса 200 кГц';

    /** Предел ширины выбранного профиля, Гц; 0 — без предела. */
    ManualLayout.prototype._profileMaxWidth = function() {
        return PROFILE_MAX_BW[selectedValue(this._rxPipelineSelect)] || 0;
    };

    /** Ширины больше слышимой у профиля серые; выбранная такая сужается до самой широкой из слышимых. */
    ManualLayout.prototype._applyBandwidthLimit = function() {
        const sel = this._rxFields ? this._rxFields.bandwidth : null;
        if (!sel || !sel.options) { return; }
        const max = this._profileMaxWidth();
        const wideFM = selectedValue(this._rxPipelineSelect) === 'wfm';
        let hasWide = false;
        for (let i = 0; i < sel.options.length; i++) {
            if (Number(sel.options[i].value) === WIDE_FM_BW) { hasWide = true; }
        }
        let widest = null;
        for (let i = 0; i < sel.options.length; i++) {
            const opt = sel.options[i];
            const hz = Number(opt.value);
            const tooWide = max > 0 && hz > max;
            const broadcastOnly = hz === WIDE_FM_BW && !wideFM;
            const notBroadcast = wideFM && hasWide && hz !== WIDE_FM_BW;
            opt.disabled = tooWide || broadcastOnly || notBroadcast;
            opt.title = '';
            if (opt.disabled) { opt.title = notBroadcast ? WIDTH_BROADCAST : WIDTH_NOT_HEARD; }
            if (!opt.disabled && (!widest || hz > Number(widest.value))) { widest = opt; }
        }
        if (widest && sel.options[sel.selectedIndex] && sel.options[sel.selectedIndex].disabled) {
            selectByValue(sel, widest.value);
        }
        this._syncChannelOverlay();
    };

    /** Столбец на спектре: ширина из списка, центр частоты приёмника. SSB подсвечивает половину. */
    ManualLayout.prototype._syncChannelOverlay = function() {
        const f = this._rxFields;
        if (!f) { return; }
        const bw = Number(selectedValue(f.bandwidth));
        if (!this._chanOverlay) { return; }
        this._chanOverlay.setMaxWidth(this._profileMaxWidth());
        this._chanOverlay.setChannel(0, bw);
        const demod = selectedValue(f.demod);
        if (typeof this._chanOverlay.setSide === 'function') {
            this._chanOverlay.setSide(demod === 'usb' || demod === 'lsb' ? demod : '');
        }
    };

    /**
     * Сдвиг шкалы спектра под Доплер: при включённом демодуляторе — с сервера;
     * в «Авто приём» без демода — с маркера текущей цели hop (LO на номинале).
     * @returns {number}
     */
    ManualLayout.prototype._viewDopplerHz = function() {
        if (demodChecked()) { return this._dopplerViewHz || 0; }
        if (!this._autoRx) { return 0; }
        const norad = autoRxTargetNorad(this._sweep, true);
        const win = this._sweep && (this._sweep.windows || [])[this._sweep.index];
        const centerHz = win && win.center_hz > 0 ? win.center_hz : 0;
        return autoRxDopplerFromMarkers(norad, centerHz, this._txMarkList);
    };

    /** Включение демодулятора возвращает увеличенный график на частоту Доплера. */
    ManualLayout.prototype._snapZoomToDoppler = function() {
        if (!this._zoom) {
            this._applySpecView();
            return;
        }
        const off = this._viewDopplerHz();
        if (this._zoom.offsetHz === off) {
            this._applySpecView();
            return;
        }
        this._setZoom({ offsetHz: off, spanHz: this._zoom.spanHz });
    };

    /**
     * Цель hop «Авто приём» → NORAD канала / участок эфира, Доплер и профиль демода.
     * TX: профиль «Авто». Эфир: demod/baud с классификатора (OOK/зонды — сразу профиль).
     */
    ManualLayout.prototype._syncAutoRxTarget = function() {
        if (!this._autoRx || !this._sweep) { return; }
        const win = (this._sweep.windows || [])[this._sweep.index] || {};
        const centerHz = win.center_hz > 0 ? win.center_hz : 0;
        const norad = autoRxTargetNorad(this._sweep, this._autoRx);
        const air = !!win.air || (!norad && centerHz > 0);
        if (!norad && !air) { return; }

        const f = this._rxFields;
        const prevNorad = this._chanNorad;
        const prevCenter = this._autoRxCenterHz || 0;
        this._autoRxCenterHz = centerHz;

        let demodId = 'auto';
        if (air) {
            this._chanNorad = 0;
            this._chanLabel = win.label || 'Эфир';
            if (centerHz > 0) { this._chanFreqMHz = centerHz / 1e6; }
            if (f) {
                selectByValue(f.doppler, 'manual');
                this._syncDopplerEntry();
            }
            // Линия без кадра (OOK/зонды) — сразу профиль; иначе «Авто» с подсказкой structure.
            demodId = win.demod || 'auto';
            if (f && f.demod) {
                selectByValue(f.demod, demodId);
                if (demodId === 'auto') { this._prepareAutoChannel(); }
            }
            if (f && f.baud && win.baud > 0) {
                f.baud.value = String(Math.round(win.baud));
            }
            if (this._demodPanel) {
                this._demodPanel.setChannel({
                    freqMHz: centerHz / 1e6,
                    label: this._chanLabel,
                    modulation: win.label || demodId,
                    active: true,
                });
            }
            this._applyBandwidthLimit();
            this._syncEyeFromPipeline();
            this._syncChannelOverlay();
        } else {
            this._chanNorad = norad;
            if (f) {
                selectByValue(f.doppler, 'auto');
                this._syncDopplerEntry();
            }
            const m = autoRxMatchTx(norad, centerHz, this._txMarkList);
            if (m) {
                if (m.label) { this._chanLabel = m.label; }
                if (m.nominalHz > 0) { this._chanFreqMHz = m.nominalHz / 1e6; }
                if (this._demodPanel && m.tx) { this._demodPanel.setChannel(m.tx); }
                if (f && f.demod) {
                    selectByValue(f.demod, 'auto');
                    this._prepareAutoChannel();
                }
                this._applyBandwidthLimit();
                this._syncEyeFromPipeline();
                this._syncChannelOverlay();
            }
        }

        const hopChanged = norad !== prevNorad || centerHz !== prevCenter || air;
        const applyKey = (air ? 'air' : norad) + ':' + centerHz + ':' + demodId;
        const keyChanged = applyKey !== this._autoRxApplyKey;
        const demodBox = document.getElementById('manual-rx-channel-show');
        const canApply = air || this._dopplerNorad();
        if ((hopChanged || keyChanged) && canApply && demodBox && demodBox.checked) {
            this._autoRxApplyKey = applyKey;
            this._applyDemodSettings();
        }
        this._applySpecView();
    };

    /** Подпись канала на спектре: имя профиля. Цвет — открыт ли порог. */
    ManualLayout.prototype._showChannelStatus = function(st) {
        if (!this._chanOverlay || !st) { return; }
        const sel = this._rxPipelineSelect;
        const nameOf = function(id) {
            if (!sel || !sel.options) { return id; }
            for (let i = 0; i < sel.options.length; i++) {
                if (sel.options[i].value === id && sel.options[i].text) {
                    return String(sel.options[i].text).replace(/\s*\(AX\.25\)$/, '').trim();
                }
            }
            return id;
        };
        let label = autoDemodStatusLabel(st, nameOf);
        if (!label) {
            label = st.demod || '';
            if (sel && sel.options) {
                for (let i = 0; i < sel.options.length; i++) {
                    if (sel.options[i].value === st.demod && sel.options[i].text) {
                        label = String(sel.options[i].text).replace(/\s*\(AX\.25\)$/, '').trim();
                        break;
                    }
                }
            }
        }
        this._chanOverlay.setStatus(label, st.squelch_open ? 'open' : 'idle');
    };

    ManualLayout.prototype._initPanorama = function() {
        this._sweepStatusEl = document.getElementById('manual-sweep-status');
        this._sweepToggleEl = document.getElementById('manual-sweep-toggle');
        this._sweepFreqEl = document.getElementById('manual-sweep-freq');
        this._sweepPathEl = document.getElementById('manual-sweep-path');
        this._sweepProgressEl = document.getElementById('manual-sweep-progress');
        const self = this;
        if (this._sweepToggleEl) {
            this._on(this._sweepToggleEl, 'click', function() { self._toggleSweep(); });
        }
        this._renderSweepBar();
        if (typeof window.ManualPanorama !== 'function') { return; }
        const selfPan = this;
        this._panorama = new window.ManualPanorama({
            onSignal: function(sig, x, y) { selfPan._showAirCard([sig], sig.id, x, y); },
            onTransmitter: function(tx, x, y) { selfPan._showTransmitterCard(tx, x, y); },
        });
    };

    /**
     * Карточка участка у точки щелчка. Та же, что у маркера на «Спектре».
     * @param {Array} sigs
     * @param {string} focusId
     * @param {number} x
     * @param {number} y
     */
    ManualLayout.prototype._showAirCard = function(sigs, focusId, x, y) {
        if (!this._bandMarks || typeof this._bandMarks.showAt !== 'function' || !sigs || !sigs.length) { return; }
        this._bandMarks.showAt(x, y, sigs, focusId);
    };

    /**
     * Щелчок по метке передатчика на панораме. Если рядом уже есть участок
     * эфира, карточка его. Иначе — имя, частота и модуляция из каталога.
     * @param {Object} tx
     * @param {number} x
     * @param {number} y
     */
    ManualLayout.prototype._showTransmitterCard = function(tx, x, y) {
        if (!tx || typeof tx.freqMHz !== 'number') { return; }
        const hz = tx.freqMHz * 1e6;
        const marks = this._panorama && typeof this._panorama.marks === 'function' ? this._panorama.marks() : [];
        let best = null;
        let bestD = Infinity;
        for (let i = 0; i < marks.length; i++) {
            const sig = marks[i];
            const reach = Math.max(25000, (sig.bandwidth_hz || 0) / 2);
            const d = Math.abs(sig.center_hz - hz);
            if (d <= reach && d < bestD) {
                best = sig;
                bestD = d;
            }
        }
        if (best) {
            this._showAirCard([best], best.id, x, y);
            return;
        }
        const structure = {};
        if (tx.label) { structure.station = tx.label; }
        if (tx.modulation) { structure.modulation_text = tx.modulation; }
        this._showAirCard([{
            id: 'tx:' + (tx.uuid || tx.freqMHz),
            center_hz: hz,
            norad_id: tx.noradId || 0,
            origin: tx.noradId ? 'satellite' : 'unknown',
            stats: {},
            structure: structure,
        }], 'tx:' + (tx.uuid || tx.freqMHz), x, y);
    };

    // ── Обход диапазонов: состояние, поток снимков окон, Старт / Стоп ─────

    /** @returns {boolean} */
    ManualLayout.prototype._sweepRunning = function() {
        return !!(this._sweep && this._sweep.running);
    };

    /**
     * Панорама на экране: состояние обхода по REST, затем поток снимков окон.
     * Без секции sweep у тракта (404) поток не открывается — переподключаться не к чему.
     */
    ManualLayout.prototype._startPanoramaFeed = function() {
        const path = this._radioPathName();
        if (this._panPath === path && path !== null) { return; }
        this._stopPanoramaFeed();
        if (!path) {
            this._applySweepState(null, 'Нет радиотракта');
            return;
        }
        this._panPath = path;
        if (typeof fetch !== 'function') { return; }
        const self = this;
        fetch('/api/sdr/sweep?radio_path=' + encodeURIComponent(path), { credentials: 'same-origin' })
            .then(function(r) {
                if (r.status === 404) { return null; }
                return r.ok ? r.json() : Promise.reject(new Error('HTTP ' + r.status));
            })
            .then(function(st) {
                if (self._panPath !== path) { return; }
                if (!st) {
                    self._applySweepState(null, 'Обход не настроен: нет диапазонов sweep у тракта');
                    return;
                }
                self._applySweepState(st);
                self._openPanoramaStream(path);
            })
            .catch(function(err) {
                if (self._panPath === path) { self._applySweepState(null, 'Обход недоступен: ' + err.message); }
            });
    };

    ManualLayout.prototype._openPanoramaStream = function(path) {
        if (typeof window.SpectrumStream !== 'function') { return; }
        const self = this;
        this._panStream = new window.SpectrumStream({
            url: '/api/stream/v1/panorama?radio_path=' + encodeURIComponent(path),
            onFrame: function(frame) {
                if (self._panorama && typeof self._panorama.putFrame === 'function') {
                    self._panorama.putFrame(frame.header, frame.values);
                }
            },
        });
        this._panStream.start();
    };

    ManualLayout.prototype._stopPanoramaFeed = function() {
        if (this._panStream) {
            this._panStream.stop();
            this._panStream = null;
        }
        this._panPath = null;
    };

    /**
     * Новое состояние обхода; null — обхода у тракта нет, emptyText поясняет почему.
     * @param {?Object} st
     * @param {string} [emptyText]
     */
    ManualLayout.prototype._applySweepState = function(st, emptyText) {
        const prev = this._sweep;
        if (prev && prev.running && st && st.running && prev.index !== st.index && prev.windows) {
            const from = prev.windows[prev.index];
            this._sweepRetune = from ? { fromHz: from.center_hz, untilMs: Date.now() + SWEEP_RETUNE_SHOW_MS } : null;
        }
        this._sweep = st || null;
        this._sweepEmptyText = st ? '' : (emptyText || '');
        // «Авто приём» = обход policy=tx на сервере (после перезагрузки страницы тоже).
        const wasAuto = !!this._autoRx;
        this._autoRx = !!(st && st.running && st.policy === 'tx');
        if (wasAuto && !this._autoRx) {
            this._clearAutoRxHop();
        }
        if (wasAuto !== this._autoRx) {
            this._applyAccess();
        } else {
            this._renderAutoRx();
        }
        if (this._autoRx) {
            this._syncAutoRxTarget();
        }
        const p = this._panorama;
        if (p && typeof p.setSweepState === 'function') {
            p.setSweepState(st || { running: false, windows: [], index: 0, cycle: 0 });
            p.setEmptyText(this._sweepEmptyText);
        }
        this._renderSweepBar();
    };

    /** Шапка панорамы: частота стоянки, окно и остаток, ход стоянки, кнопка. Приёмник — селектор. */
    ManualLayout.prototype._renderSweepBar = function() {
        const st = this._sweep;
        const bar = sweepBarModel(st, this._sweepEmptyText, Date.now(), this._sweepRetune);
        if (this._sweepFreqEl) { this._sweepFreqEl.textContent = bar.freq; }
        if (this._sweepStatusEl) { this._sweepStatusEl.textContent = bar.status; }
        const fill = this._sweepProgressEl;
        if (fill) {
            const pct = bar.progress === null ? 0 : Math.round(bar.progress * 1000) / 10;
            const prevPct = parseFloat(fill.style.width) || 0;
            // Новое окно начинает полосу с нуля сразу, без обратной анимации
            fill.style.transition = pct < prevPct ? 'none' : '';
            fill.style.width = pct + '%';
        }
        const btn = this._sweepToggleEl;
        if (!btn) { return; }
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        const readOnly = !!ac && !ac.canControl();
        const running = !!(st && st.running);
        btn.textContent = running ? 'Стоп' : 'Старт';
        btn.setAttribute('aria-pressed', running ? 'true' : 'false');
        btn.disabled = !st || readOnly;
        btn.title = readOnly ? 'Запускать обход можно только в режиме «Контроль»' : '';
    };

    ManualLayout.prototype._startSweepTimer = function() {
        if (this._sweepTimer) { return; }
        const self = this;
        // Шапка панорамы и строка «Авто приём» считают «осталось» от Date.now —
        // без тика подпись замирает до следующего sweep_state.
        this._sweepTimer = setInterval(function() {
            self._renderSweepBar();
            self._renderAutoRx();
        }, 1000);
    };

    ManualLayout.prototype._stopSweepTimer = function() {
        if (this._sweepTimer) {
            clearInterval(this._sweepTimer);
            this._sweepTimer = null;
        }
    };

    /**
     * Состояние обхода текущего тракта. На «Спектре» без визита на «Панораму»
     * ещё не загружено — тогда GET /api/sdr/sweep.
     * @param {function(?Object)} done — состояние или null
     */
    ManualLayout.prototype._ensureSweep = function(done) {
        const path = this._radioPathName();
        const finish = function(st) { if (done) { done(st); } };
        if (!path || typeof fetch !== 'function') { finish(null); return; }
        if (this._sweep) { finish(this._sweep); return; }
        const self = this;
        fetch('/api/sdr/sweep?radio_path=' + encodeURIComponent(path), { credentials: 'same-origin' })
            .then(function(r) {
                if (r.status === 404) { return null; }
                return r.ok ? r.json() : Promise.reject(new Error('HTTP ' + r.status));
            })
            .then(function(st) {
                if (self._radioPathName() !== path) { finish(null); return; }
                // Пока ждали ответ, состояние мог выставить тумблер или SSE — не затираем.
                if (self._sweep) { finish(self._sweep); return; }
                if (!st) {
                    self._applySweepState(null, 'Обход не настроен: нет диапазонов sweep у тракта');
                    finish(null);
                    return;
                }
                self._applySweepState(st);
                finish(st);
            })
            .catch(function(err) {
                if (self._radioPathName() === path && !self._sweep) {
                    self._applySweepState(null, 'Обход недоступен: ' + err.message);
                }
                finish(self._sweep || null);
            });
    };

    /**
     * Поставить обход в running. Без подтверждения: вызов уже решил, что режим меняется.
     * @param {boolean} running
     * @param {function()} [done]
     * @param {string} [policy] — «tx» для Авто приёма; иначе survey
     */
    ManualLayout.prototype._postSweep = function(running, done, policy) {
        const path = this._radioPathName();
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        const notice = function(text) { if (ac) { ac.setNotice(text); } };
        const finish = function() { if (done) { done(); } };
        if (!this._sweep || !path || typeof fetch !== 'function') { finish(); return; }
        const self = this;
        const body = { radio_path: path, running: !!running };
        if (running && policy) { body.policy = policy; }
        if (running && policy === 'tx' && this._autoRxIncludeAir) { body.include_air = true; }
        fetch('/api/sdr/sweep', {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        })
            .then(function(r) {
                if (r.status === 403 && ac) { ac.refresh(); }
                return r.json().then(function(b) {
                    if (!r.ok) { throw new Error((b && b.error) || ('HTTP ' + r.status)); }
                    notice('');
                    if (self._radioPathName() === path) { self._applySweepState(b); }
                });
            })
            .catch(function(err) {
                notice((running ? 'Обход не запущен: ' : 'Обход не остановлен: ') + err.message);
            })
            .then(finish);
    };

    /** Старт или стоп обхода тракта. Запуск перестраивает приёмник, поэтому запись IQ — с подтверждением. */
    ManualLayout.prototype._toggleSweep = function() {
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        if (ac && !ac.canControl()) {
            ac.setNotice('Запускать обход можно только в режиме «Контроль».');
            return;
        }
        const path = this._radioPathName();
        if (!this._sweep || !path || typeof fetch !== 'function') { return; }
        const running = !this._sweep.running;
        if (running && this._isRecording()) {
            if (!this._confirm('Идёт запись IQ. Обход перестроит приёмник, и запись остановится. Продолжить?')) {
                return;
            }
            this._stopRecording();
        }
        if (running && !this._confirmBreakReception('Обход')) {
            return;
        }
        this._postSweep(running);
    };

    /** Подпись, доступность тумблера «Авто приём» и строка текущей цели. */
    ManualLayout.prototype._renderAutoRx = function() {
        const btn = this._autoRxEl;
        if (btn) {
            const ac = (typeof window !== 'undefined') ? window._accessControl : null;
            const readOnly = !!ac && !ac.canControl();
            const on = !!this._autoRx;
            btn.setAttribute('aria-pressed', on ? 'true' : 'false');
            btn.disabled = readOnly;
            btn.title = readOnly
                ? 'Включать «Авто приём» можно только в режиме «Контроль»'
                : 'Обход TX над горизонтом: настройка на частоту передатчика и стоянка';
        }
        if (this._autoRxStatusEl) {
            this._autoRxStatusEl.textContent = this._autoRx
                ? autoRxStatusModel(this._sweep, Date.now())
                : '';
        }
    };

    /** Сброс следа hop при выключении «Авто приём». */
    ManualLayout.prototype._clearAutoRxHop = function() {
        this._autoRxCenterHz = 0;
        this._autoRxApplyKey = '';
        this._channelOffsetHz = 0;
    };

    /**
     * Тумблер «Авто приём»: обход по TX плана (policy=tx), Спектр остаётся
     * открытым, ручная настройка приёмника закрыта. Профиль демода ставит hop.
     */
    ManualLayout.prototype._toggleAutoRx = function() {
        const ac = (typeof window !== 'undefined') ? window._accessControl : null;
        if (ac && !ac.canControl()) {
            ac.setNotice('Включать «Авто приём» можно только в режиме «Контроль».');
            return;
        }
        const path = this._radioPathName();
        if (!path || typeof fetch !== 'function') { return; }
        if (this._autoRx) {
            this._autoRx = false;
            this._clearAutoRxHop();
            // Выкл обхода: демод гасится, в селекторе остаётся «Авто» (и на сервере).
            if (this._rxFields && this._rxFields.demod) {
                selectByValue(this._rxFields.demod, 'auto');
                this._prepareAutoChannel();
            }
            this._applyAccess();
            this._setDemodOff(true);
            if (this._sweepRunning()) { this._postSweep(false); }
            return;
        }
        const self = this;
        this._ensureSweep(function(st) {
            if (!st) {
                if (ac) {
                    ac.setNotice(self._sweepEmptyText ||
                        'Обход не настроен: нет диапазонов у тракта.');
                }
                return;
            }
            if (self._isRecording()) {
                if (!self._confirm('Идёт запись IQ. Обход перестроит приёмник, и запись остановится. Продолжить?')) {
                    return;
                }
                self._stopRecording();
            }
            if (!self._confirmBreakReception('Обход')) {
                return;
            }
            self._autoRx = true;
            if (self._rxFields && self._rxFields.demod) {
                selectByValue(self._rxFields.demod, 'auto');
            }
            self._applyAccess();
            // Канал с auto — только если демод уже включён (галочка есть и нажата).
            const box = document.getElementById('manual-rx-channel-show');
            if (box && box.checked) {
                self._applyDemodSettings();
            }
            self._postSweep(true, function() {
                if (self._autoRx && !self._sweepRunning()) {
                    self._autoRx = false;
                    self._clearAutoRxHop();
                    self._applyAccess();
                }
            }, 'tx');
        });
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
        canvas.title = 'Колесо мыши: масштаб по времени (лево у NOW, горизонт вправо)';
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
        this._initDemodInfoModal();
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

    /** Модалка «Информация» по демодулятору: метрики, графики, eye/constellation. */
    ManualLayout.prototype._initDemodInfoModal = function() {
        if (typeof window.DemodInfoModal !== 'function') { return; }
        this._demodInfoModal = new window.DemodInfoModal();
        const btn = document.getElementById('manual-demod-info');
        if (btn && this._demodInfoModal) {
            const self = this;
            btn.addEventListener('click', function() {
                if (self._demodInfoModal.isOpen()) { self._demodInfoModal.close(); return; }
                self._demodInfoModal.open();
                // Сразу показываем последние известные метрики, иначе пусто до следующего статуса.
                self._pushDemodInfo();
            });
        }
    };

    /** Передаёт в модалку текущий статус, историю, диагноз и модуляцию для eye. */
    ManualLayout.prototype._pushDemodInfo = function() {
        if (!this._demodInfoModal || !this._demodStatus) { return; }
        const diag = this._demodPanel && this._demodPanel._lastDiag ? this._demodPanel._lastDiag : null;
        const history = this._demodPanel ? this._demodPanel.getHistory() : null;
        const modStr = this._demodStatus.demod || '';
        this._demodInfoModal.update(this._demodStatus, this._levelOffset, history, diag, modStr);
    };

    /** Имя КА по NORAD из текущей группы (маркеры и таблица «Эфир»). */
    ManualLayout.prototype._satNameOf = function(noradId) {
        const group = this._sm && typeof this._sm.getSatelliteGroup === 'function'
            ? this._sm.getSatelliteGroup() : null;
        return window.TxMarkers ? window.TxMarkers.satNames(group)[noradId] || null : null;
    };

    /** Таблицы «Эфир»: панорама (markRows, клик настраивает) и спектр (hearRows, только чтение). */
    ManualLayout.prototype._initAirTable = function() {
        if (typeof window.AirTable !== 'function') { return; }
        const self = this;
        const nameOf = function(noradId) { return self._satNameOf(noradId); };
        const panBody = document.getElementById('manual-air-tbody');
        if (panBody) {
            this._airTable = new window.AirTable(panBody, {
                onPick: function(sig) { self._pickBandSignal(sig); },
                nameOf: nameOf,
            });
        }
        const specBody = document.getElementById('manual-spec-air-tbody');
        if (specBody) {
            const hear = window.BandMarks && typeof window.BandMarks.hearRows === 'function'
                ? window.BandMarks.hearRows : null;
            // Без onPick: tune только с маркеров над водопадом, не из таблицы.
            this._specAirTable = new window.AirTable(specBody, {
                nameOf: nameOf,
                rowsFn: hear,
                onOpenPayload: function(sig) { self._openHearPayload(sig); },
            });
        }
        this._bindHearPayloadModal();
    };

    /** Закрытие модалки «Кадр» (крестик, фон, Escape) — один раз на сессию layout. */
    ManualLayout.prototype._bindHearPayloadModal = function() {
        if (this._hearPayloadBound) { return; }
        const modal = document.getElementById('manual-tmi-details');
        if (!modal) { return; }
        const self = this;
        this._onHearPayloadClick = function(e) {
            const closer = e.target && e.target.closest
                ? e.target.closest('[data-action="close"]') : null;
            if (closer && modal.contains(closer)) { self._closeHearPayload(); }
        };
        this._onHearPayloadKey = function(e) {
            if (e.key === 'Escape') { self._closeHearPayload(); }
        };
        modal.addEventListener('click', this._onHearPayloadClick);
        document.addEventListener('keydown', this._onHearPayloadKey);
        this._hearPayloadModal = modal;
        this._hearPayloadBound = true;
    };

    ManualLayout.prototype._closeHearPayload = function() {
        if (this._hearPayloadModal) {
            this._hearPayloadModal.classList.add('modal--hidden');
        }
    };

    /**
     * Модалка «Кадр» для ячейки «Данные» в «Эфире»: поля telemetry + простая визуализация.
     * @param {Object} sig
     */
    ManualLayout.prototype._openHearPayload = function(sig) {
        this._bindHearPayloadModal();
        const modal = this._hearPayloadModal || document.getElementById('manual-tmi-details');
        const titleEl = document.getElementById('manual-tmi-details-title');
        const tbody = document.getElementById('manual-tmi-details-tbody');
        const viz = document.getElementById('manual-tmi-details-viz');
        if (!modal || !tbody) { return; }
        const BM = typeof window !== 'undefined' ? window.BandMarks : null;
        const kindLabel = (sig && sig.hear_kind)
            || (BM && typeof BM.formatHearData === 'function'
                ? BM.formatHearData({
                    kind: sig && sig.last_kind,
                    info_hex: sig && sig.last_info_hex,
                }) : '')
            || (sig && sig.last_summary) || 'Кадр';
        const kind = kindLabel === '—' ? 'Кадр' : kindLabel;
        const freq = sig && sig.center_hz
            ? ((sig.center_hz / 1e6).toFixed(4) + ' МГц') : '';
        if (titleEl) {
            titleEl.textContent = [kind, freq].filter(Boolean).join(' · ');
        }
        tbody.textContent = '';
        let details = (sig && sig.hear_details) || [];
        if (!details.length && BM && typeof BM.lastPayloadEvent === 'function'
            && typeof BM.payloadDetails === 'function') {
            const ev = BM.lastPayloadEvent(sig);
            if (ev) { details = BM.payloadDetails(ev); }
        }
        if (!details.length && sig && (sig.last_kind || sig.last_summary || sig.last_decoder)) {
            details = [
                { key: 'тип', value: kind },
                { key: 'декодер', value: sig.last_decoder || '—' },
                { key: 'сводка', value: sig.last_summary || '—' },
            ];
        }
        if (sig && sig.last_rx_ms && details.length) {
            const d = new Date(sig.last_rx_ms);
            const p = function(n) { return n < 10 ? '0' + n : String(n); };
            details = details.concat([{
                key: 'обновлён',
                value: p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds()),
            }]);
        }
        for (let i = 0; i < details.length; i++) {
            const tr = document.createElement('tr');
            const tdKey = document.createElement('td');
            tdKey.className = 'ml-tmi-details__key';
            tdKey.textContent = details[i].key;
            tr.appendChild(tdKey);
            const tdVal = document.createElement('td');
            tdVal.className = 'ml-tmi-details__val';
            tdVal.textContent = details[i].value;
            tr.appendChild(tdVal);
            tbody.appendChild(tr);
        }
        this._paintHearPayloadViz(viz, sig);
        modal.classList.remove('modal--hidden');
    };

    /**
     * Простая графика в модалке: ключевые числа маяка шкалами; снимок — подсказка.
     * @param {?HTMLElement} viz
     * @param {Object} sig
     */
    ManualLayout.prototype._paintHearPayloadViz = function(viz, sig) {
        if (!viz) { return; }
        viz.textContent = '';
        const BM = typeof window !== 'undefined' ? window.BandMarks : null;
        const ev = (sig && sig.hear_ev)
            || (BM && typeof BM.lastPayloadEvent === 'function' ? BM.lastPayloadEvent(sig) : null);
        if (!ev) {
            viz.hidden = true;
            return;
        }
        if (ev.kind === 'image') {
            viz.hidden = false;
            viz.className = 'ml-tmi-details__viz ml-tmi-details__viz--note';
            const p = document.createElement('p');
            p.textContent = (ev.summary || 'Кусок снимка') +
                '. Собранная картинка — на вкладке «Снимки», когда заработает сборщик.';
            viz.appendChild(p);
            return;
        }
        const keys = (ev.params || []).filter(function(p) { return p && p.key; });
        if (ev.kind !== 'beacon' || !keys.length) {
            viz.hidden = true;
            return;
        }
        viz.hidden = false;
        viz.className = 'ml-tmi-details__viz';
        let maxAbs = 0;
        const nums = [];
        for (let i = 0; i < keys.length && i < 6; i++) {
            const num = parseFloat(String(keys[i].value).replace(',', '.'));
            nums.push(num);
            if (isFinite(num) && Math.abs(num) > maxAbs) { maxAbs = Math.abs(num); }
        }
        if (maxAbs <= 0) { maxAbs = 1; }
        for (let i = 0; i < keys.length && i < 6; i++) {
            const p = keys[i];
            const row = document.createElement('div');
            row.className = 'ml-tmi-details__bar';
            const name = document.createElement('span');
            name.className = 'ml-tmi-details__bar-name';
            name.textContent = p.name || p.id || '';
            row.appendChild(name);
            const track = document.createElement('span');
            track.className = 'ml-tmi-details__bar-track';
            const fill = document.createElement('span');
            fill.className = 'ml-tmi-details__bar-fill';
            const num = nums[i];
            const pct = isFinite(num) ? Math.max(4, Math.min(100, 100 * Math.abs(num) / maxAbs)) : 0;
            fill.style.width = pct + '%';
            track.appendChild(fill);
            row.appendChild(track);
            const val = document.createElement('span');
            val.className = 'ml-tmi-details__bar-val';
            val.textContent = p.unit ? (p.value + ' ' + p.unit) : String(p.value);
            row.appendChild(val);
            viz.appendChild(row);
        }
    };

    /**
     * Таблицы «Эфир» — зеркало band_signals (кадры/голос уже на бэке).
     * @param {Array} signals
     */
    ManualLayout.prototype._setAirSignals = function(signals) {
        const next = Array.isArray(signals) ? signals.slice() : [];
        this._airSignals = next;
        if (this._airTable) { this._airTable.setSignals(next); }
        if (this._specAirTable) { this._specAirTable.setSignals(next); }
    };

    ManualLayout.prototype._initProductGallery = function() {
        if (typeof window.ProductGallery !== 'function' || !document.getElementById('manual-pics-list')) { return; }
        this._gallery = new window.ProductGallery();
        this._gallery.load();
    };

    /**
     * Кадры и состояние демодулятора из SSE — только для тракта, выбранного в шапке.
     * Картинки — со всех трактов: они складываются в общий список «Снимков».
     */
    ManualLayout.prototype._initDemodEvents = function() {
        const self = this;
        this._on(document, 'satellite-scout-demod', function(e) {
            const d = e.detail || {};
            if (d.event === 'product') {
                if (self._gallery) { self._gallery.applyProduct(d.data); }
                return;
            }
            const path = self._radioPathName();
            if (!path || !d.data || d.data.radio_path !== path) { return; }
            if (d.event === 'band_signals') {
                // Ось спектра/панорамы — только свежие (HoldMs); таблица «Эфир» — полный час.
                const all = d.data.signals;
                const nowMs = d.data.timestamp_ms;
                const BM = typeof window !== 'undefined' ? window.BandMarks : null;
                const axis = BM && typeof BM.freshSignals === 'function'
                    ? BM.freshSignals(all, nowMs) : all;
                if (self._bandMarks) { self._bandMarks.setSignals(axis, nowMs); }
                if (self._panorama && typeof self._panorama.setSignals === 'function') {
                    self._panorama.setSignals(axis);
                }
                self._setAirSignals(all);
            } else if (d.event === 'sweep_state') {
                self._applySweepState(d.data);
            } else if (d.event === 'demod_status') {
                // Сначала настройки: уровень на панели пересчитывается по новому усилению.
                self._demodStatus = d.data;
                if (!self._receptionBusy()) { self._rxBreakArmed = false; }
                self._followServerChannel(d.data);
                if (self._demodPanel) { self._demodPanel.applyStatus(d.data, self._levelOffset); }
                self._showChannelStatus(d.data);
                self._showDoppler(d.data);
                self._showTxMarkers(d.data);
                if (self._demodInfoModal && self._demodInfoModal.isOpen()) { self._pushDemodInfo(); }
            }
        });
    };

    /** Живой приём, когда у станции есть радиотракт; без него панель демода на mock. */
    ManualLayout.prototype._applyLiveDemod = function() {
        const live = this._radioPathName() !== null;
        if (this._demodPanel) { this._demodPanel.setLive(live); }
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
            this._applyBandwidthLimit();
        }

        const label = tx ? (tx.label || 'Unknown') : 'Unknown';
        this._chanLabel = label;
        this._chanFreqMHz = freqMHz;
        const prevNorad = this._chanNorad;
        this._chanNorad = tx && tx.noradId ? tx.noradId : null;

        this._syncEyeFromPipeline();
        // Канал на «Авто» идёт за Доплером нового спутника сразу, без «Установить»
        if (this._chanNorad && this._chanNorad !== prevNorad && this._dopplerNorad()) {
            this._applyDemodSettings();
        }

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
            if (self._passTimeline && typeof self._passTimeline.setSelectedId === 'function') {
                self._passTimeline.setSelectedId(self._sm.getSelectedSatelliteId());
            }
        }));
        if (SE.SATELLITE_GROUP_UPDATE) {
            this._unsubscribers.push(this._sm.subscribe(SE.SATELLITE_GROUP_UPDATE, function() {
                self._buildTransmittersFromGroup();
                self._syncPassTimeline();
            }));
        }
        if (SE.SAT_RADIO) {
            this._unsubscribers.push(this._sm.subscribe(SE.SAT_RADIO, function() {
                if (self._airTable && self._airSignals) {
                    self._airTable.setSignals(self._airSignals);
                }
                if (self._specAirTable && self._airSignals) {
                    self._specAirTable.setSignals(self._airSignals);
                }
            }));
        }
        this._buildTransmittersFromGroup();
        this._syncPassTimeline();
    };

    /** Горки пролётов и окно времени — по группе Плана сеансов, подсветка — сопровождаемый КА. */
    ManualLayout.prototype._syncPassTimeline = function() {
        if (!this._passTimeline || !this._sm) { return; }
        const PTV = window.PassTimelineView;
        const sm = this._sm;
        const group = typeof sm.getSatelliteGroup === 'function' ? sm.getSatelliteGroup() : null;
        if (PTV && typeof PTV.passesFromGroup === 'function') {
            const r = PTV.passesFromGroup(group);
            this._passTimeline.setPasses(r.passes, r.window);
        }
        // Палитра всех КА ленты; цветная горка — только пока пролёт в окне сеанса (считается при draw)
        const colors = {};
        if (group && typeof sm.getMarkerColor === 'function') {
            const add = function(list) {
                if (!Array.isArray(list)) { return; }
                for (let i = 0; i < list.length; i++) {
                    const nid = list[i].norad_id;
                    const c = sm.getMarkerColor(nid);
                    if (c) { colors[nid] = c; }
                }
            };
            add(group.satellites);
            add(group.timeline);
        }
        if (typeof this._passTimeline.setSatColors === 'function') {
            this._passTimeline.setSatColors(colors);
        } else if (typeof this._passTimeline.setSessionColors === 'function') {
            this._passTimeline.setSessionColors(colors);
        }
        if (typeof sm.getTrackingSatelliteId === 'function') {
            this._passTimeline.setTrackingId(sm.getTrackingSatelliteId());
        }
        if (typeof sm.getSelectedSatelliteId === 'function' &&
            typeof this._passTimeline.setSelectedId === 'function') {
            this._passTimeline.setSelectedId(sm.getSelectedSatelliteId());
        }
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
        // Вход открывает «Спектр»; приёмник при этом не перестраивается.
        // GET sweep сразу: «Авто приём» восстанавливается с бэка после перезагрузки.
        this._showTab('spectrum');
        this._setView('spectrum');
        this._startSweepTimer();
        this._ensureSweep();
        if (this._passProfile) { this._passProfile.draw(); }
        if (this._passTimeline) { this._passTimeline.draw(); }
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
            if (self._active && self._view === 'panorama') { self._startPanoramaFeed(); }
            else if (self._active) {
                // Новый тракт — своё состояние обхода («Авто приём» с бэка).
                self._sweep = null;
                self._ensureSweep();
            }
            if (self._audio && self._audio.isPlaying()) { self._setListening(true); }
        });
        this._unsubscribers.push(this._offRadioPath);
    };

    /** Вид «Антенны» и панель наведения — по текущему тракту. */
    ManualLayout.prototype._applyRadioPath = function() {
        this._detectAntennaView();
        this._detectRotatorKind();
        this._applyRotAvailability();
        this._updateIndicators();
        this._applyLiveDemod();
    };

    ManualLayout.prototype.deactivate = function() {
        if (!this._active) { return; }
        this._active = false;
        this._stopSpectrumTimer();
        this._stopStream();
        this._stopPanoramaFeed();
        this._stopSweepTimer();
        this._setListening(false);
        if (this._panorama) { this._panorama.deactivate(); }
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
        if (this._demodInfoModal) { this._demodInfoModal.refreshAfterThemeChange(); }
        // Палитра трасс уже переназначена: обновить LED ленты, риски на «Спектре» и маркеры над водопадом
        this._buildTransmittersFromGroup();
        if (this._txMarkers) { this._txMarkers.render(); }
        if (this._bandMarks) { this._bandMarks.render(); }
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
        // ~1 с: линия NOW и перекраска горок при входе/выходе из окна сеанса
        this._tlTickCount = (this._tlTickCount || 0) + 1;
        if (this._passTimeline && this._tlTickCount >= 12) {
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
        if (this._rxApplyTimer) { clearTimeout(this._rxApplyTimer); this._rxApplyTimer = null; }
        if (this._demodApplyTimer) { clearTimeout(this._demodApplyTimer); this._demodApplyTimer = null; }
        if (this._scrubTimer) { clearTimeout(this._scrubTimer); this._scrubTimer = null; }
        if (this._scrubVisRaf) { cancelAnimationFrame(this._scrubVisRaf); this._scrubVisRaf = 0; }
        this._stopStepRepeat();
        this._stopSpectrumTimer();
        this._stopStream();
        this._stopPanoramaFeed();
        this._stopSweepTimer();
        this._setListening(false);
        if (this._chanOverlay) {
            this._chanOverlay.destroy();
            this._chanOverlay = null;
        }
        if (this._txMarkers) {
            this._txMarkers.destroy();
            this._txMarkers = null;
        }
        if (this._bandMarks) {
            this._bandMarks.destroy();
            this._bandMarks = null;
        }
        if (this._airTable) {
            this._airTable.destroy();
            this._airTable = null;
        }
        if (this._specAirTable) {
            this._specAirTable.destroy();
            this._specAirTable = null;
        }
        if (this._hearPayloadModal && this._onHearPayloadClick) {
            this._hearPayloadModal.removeEventListener('click', this._onHearPayloadClick);
        }
        if (this._onHearPayloadKey) {
            document.removeEventListener('keydown', this._onHearPayloadKey);
        }
        this._hearPayloadModal = null;
        this._onHearPayloadClick = null;
        this._onHearPayloadKey = null;
        this._hearPayloadBound = false;
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
        if (this._demodInfoModal) {
            this._demodInfoModal.destroy();
            this._demodInfoModal = null;
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
        if (this._passTimeline && typeof this._passTimeline.destroy === 'function') {
            this._passTimeline.destroy();
        }
        this._passTimeline = null;
        this._airSignals = [];
        if (this._gallery) {
            this._gallery.destroy();
            this._gallery = null;
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
    ManualLayout.sweepBarModel = sweepBarModel;
    ManualLayout.autoRxStatusModel = autoRxStatusModel;
    ManualLayout.autoRxTargetNorad = autoRxTargetNorad;
    ManualLayout.autoRxDopplerFromMarkers = autoRxDopplerFromMarkers;
    ManualLayout.autoRxMatchTx = autoRxMatchTx;
    ManualLayout.autoDemodStatusLabel = autoDemodStatusLabel;
    /** Пауза набора усиления, сдвига и порога, мс. Повторный ввод её сбрасывает. */
    ManualLayout.applyDebounceMs = 400;

    ManualLayout.transmittersFromGroup = transmittersFromGroup;
    ManualLayout.filterNumericText = filterNumericText;
    ManualLayout.stepNumericText = stepNumericText;
    ManualLayout.fillDemodProfileSelect = fillDemodProfileSelect;
    ManualLayout.profileBaudFlexible = profileBaudFlexible;

    if (typeof window !== 'undefined') {
        window.ManualLayout = ManualLayout;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = {
            ManualLayout: ManualLayout,
            fillDemodProfileSelect: fillDemodProfileSelect,
            profileBaudFlexible: profileBaudFlexible,
        }; // eslint-disable-line no-undef
    }
})();
