// demod-panel.js — блок «Обработка»: этапы приёма по порядку (сигнал, несущая,
// такты, синхрослово, кадры), диагноз первого неисправного этапа и лента кадров за 60 с.
//
// Живой режим — из SSE `demod_status` (internal/demod.StatusEvent), mock — пока у станции
// нет радиотракта. Шапка канала обновляется вызовом setChannel() из ManualLayout.

'use strict';

(function() {

    const STAGE_KEYS = ['signal', 'carrier', 'clock', 'sync', 'frames'];
    const STAGE_STATES = ['ok', 'warn', 'bad', 'idle'];
    // Ошибка фазы тактов ниже этой доли символа — захват: на шуме она около четверти.
    const CLOCK_LOCK_JITTER = 0.12;
    // BER по синхрословам выше этого — линия на грани: CRC будет сходиться редко.
    const SYNC_BER_WARN = 0.05;
    const STRIP_WINDOW_MS = 60000;
    const STAGE_NAMES = { signal: 'Сигнал', carrier: 'Несущая', clock: 'Такты', sync: 'Синхро', frames: 'Кадры' };

    /** Частота в MHz для отображения (десятичная запятая). */
    function formatFreqMHz(freqMHz) {
        if (typeof freqMHz !== 'number' || !isFinite(freqMHz)) {
            return '—';
        }
        return freqMHz.toFixed(3).replace('.', ',');
    }

    /** Частота в Гц или кГц с десятичной запятой; signed — со знаком, минус типографский. */
    function formatHz(hz, signed) {
        const abs = Math.abs(hz);
        const text = abs >= 1000
            ? (abs / 1000).toFixed(1).replace('.', ',') + ' кГц'
            : Math.round(abs) + ' Гц';
        if (!signed || Math.round(abs) === 0) { return text; }
        return (hz < 0 ? '−' : '+') + text;
    }

    // Подписи демодуляторов сервера (internal/sdr.Demods).
    const DEMOD_LABELS = {
        afsk1200: 'AFSK 1200',
        fsk9600_g3ruh: 'FSK 9600 G3RUH',
        fsk9600_geoscan: 'FSK 9600 GEOSCAN',
        sstv: 'SSTV',
        fm: 'FM',
        wfm: 'FM эфир',
        usb: 'USB',
        lsb: 'LSB',
        cw: 'CW',
    };

    // Профили, у которых итог приёма — звук: тактов, синхрослов и кадров у них нет.
    const VOICE_DEMODS = { fm: true, wfm: true, usb: true, lsb: true, cw: true };
    const SSB_DEMODS = { usb: true, lsb: true };

    /** Подпись демодулятора по идентификатору сервера; неизвестный — как есть. */
    function demodLabel(id) {
        return DEMOD_LABELS[id] || id;
    }

    /** Уровень в dBm по поправке тракта, без неё — в dBFS; минус типографский. */
    function formatLevel(dbfs, levelOffset) {
        const text = typeof levelOffset === 'number'
            ? (dbfs + levelOffset).toFixed(0) + ' dBm'
            : dbfs.toFixed(1) + ' dBFS';
        return text.replace('-', '−');
    }

    function formatAge(ms) {
        const s = Math.max(0, ms) / 1000;
        return s < 120 ? Math.round(s) + ' с' : Math.round(s / 60) + ' мин';
    }

    /** Форма слова «кадр» для числа. */
    function framesWord(n) {
        const d = n % 10;
        const dd = n % 100;
        if (d === 1 && dd !== 11) { return 'кадр'; }
        if (d >= 2 && d <= 4 && (dd < 12 || dd > 14)) { return 'кадра'; }
        return 'кадров';
    }

    function stage(state, value, sub) {
        return { state: state, value: value, sub: sub || '' };
    }

    /** Этап под своим именем: у SSTV вместо тактов и синхрослов — VIS и строки. */
    function named(name, s) {
        s.name = name;
        return s;
    }

    /**
     * Этапы SSTV после несущей. Декодер слушает канал и при закрытом пороге,
     * поэтому ход картинки не приглушается. Кадров у SSTV нет: строка скрыта,
     * итог приёма — в диагнозе и на вкладке «Снимки».
     */
    function sstvStages(st, out) {
        const p = st.picture;
        out.clock = named('VIS', p
            ? stage('ok', p.mode_name || p.mode, 'заголовок принят')
            : stage('idle', 'ждём', 'заголовок VIS'));
        out.sync = named('Строки', p
            ? stage('ok', p.lines + ' / ' + p.height, 'строк')
            : stage('idle', '—', 'строк'));
        out.frames = stage('idle', '—');
        out.frames.hidden = true;
        return out;
    }

    /** Этапы голоса после несущей: идёт ли звук. Несущая SSB подавлена, мерить нечего. */
    function voiceStages(st, out) {
        if (SSB_DEMODS[st.demod]) { out.carrier = stage('idle', '—', 'подавлена'); }
        out.clock = named('Звук', st.active && st.squelch_open
            ? stage('ok', 'идёт', 'порог открыт')
            : stage('idle', 'тишина', 'порог закрыт'));
        out.sync = stage('idle', '—');
        out.sync.hidden = true;
        out.frames = stage('idle', '—');
        out.frames.hidden = true;
        return out;
    }

    /**
     * Этапы приёма из `demod_status`. Меры несущей, тактов и синхрослов сервер снимает
     * только с сигнала выше порога: при закрытом пороге они остаются от последнего сигнала
     * и показываются приглушёнными.
     * @param {Object} st — событие сервера
     * @param {?number} levelOffset — сдвиг dBFS в dBm, null — без поправки
     */
    function stagesFromStatus(st, levelOffset) {
        if (st.demod_off) {
            const idle = stage('idle', '—');
            return { signal: idle, carrier: stage('idle', '—'), clock: stage('idle', '—'), sync: stage('idle', '—'), frames: stage('idle', '—') };
        }
        const open = st.active && st.squelch_open;
        const rx = st.reception;
        const dim = function(state) { return open ? state : 'idle'; };
        const out = {
            signal: stage(open ? 'ok' : 'idle', formatLevel(st.level_dbfs, levelOffset),
                'порог ' + formatLevel(st.squelch_dbfs, levelOffset)),
        };

        if (!rx) {
            out.carrier = stage('idle', '—');
            out.clock = stage('idle', '—');
            out.sync = stage('idle', '—');
        } else {
            const bw = st.bandwidth_hz || 0;
            const drift = bw > 0 && Math.abs(rx.carrier_hz) > bw / 4;
            out.carrier = stage(dim(drift ? 'warn' : 'ok'), formatHz(rx.carrier_hz, true),
                rx.deviation_hz ? 'девиация ' + formatHz(rx.deviation_hz, false) : '');

            const locked = rx.clock_jitter < CLOCK_LOCK_JITTER;
            out.clock = stage(dim(locked ? 'ok' : 'bad'), locked ? 'захват' : 'нет захвата',
                'джиттер ' + Math.round(rx.clock_jitter * 100) + ' %');

            if (!rx.sync) {
                out.sync = stage('idle', 'HDLC', 'флаги, без синхрослова');
            } else if (rx.sync.words === 0) {
                out.sync = stage('idle', '0');
            } else {
                const ber = rx.sync.bit_errors / (32 * rx.sync.words);
                out.sync = stage(dim(ber > SYNC_BER_WARN ? 'warn' : 'ok'), String(rx.sync.words),
                    'BER ' + ber.toExponential(1).replace('.', ','));
            }
        }

        if (st.demod === 'sstv') { return sstvStages(st, out); }
        if (VOICE_DEMODS[st.demod]) { return voiceStages(st, out); }

        const good = st.frames_good;
        const bad = st.frames_bad;
        const framesState = good > 0 ? 'ok' : (bad > 0 ? 'bad' : 'idle');
        out.frames = stage(framesState, good + bad > 0 ? good + ' / ' + bad : '—', 'CRC верна / нет');
        return out;
    }

    /**
     * Диагноз: первый этап, на котором приём обрывается, или итог приёма.
     * @param {Object} st — событие сервера
     * @param {Object} stages — результат stagesFromStatus
     * @param {number} nowMs — текущее время, мс эпохи
     */
    function diagnose(st, stages, nowMs) {
        const good = st.frames_good;
        const age = st.last_packet_ms ? ', последний ' + formatAge(nowMs - st.last_packet_ms) + ' назад' : '';
        const rx = st.reception;
        if (!st.active) { return { state: 'idle', text: 'Поток приёмника остановлен' }; }
        if (st.demod_off) { return { state: 'idle', text: 'Демодулятор выключен' }; }
        if (!st.supported) { return { state: 'bad', text: 'Демодулятор ' + st.demod + ' не реализован' }; }
        if (st.demod === 'sstv') { return diagnoseSSTV(st); }
        if (!st.squelch_open) {
            return good > 0
                ? { state: 'idle', text: 'Сигнала нет; кадров принято: ' + good + age }
                : { state: 'idle', text: 'Нет сигнала выше порога' };
        }
        if (stages.carrier.state === 'warn') {
            return { state: 'warn', text: 'Несущая ушла на ' + stages.carrier.value + ' от центра канала: поправьте сдвиг' };
        }
        if (VOICE_DEMODS[st.demod]) { return { state: 'ok', text: 'Канал открыт, идёт звук' }; }
        if (stages.clock.state === 'bad') {
            return { state: 'bad', text: 'Нет захвата тактов: проверьте профиль и полосу' };
        }
        if (good === 0) {
            const syncs = rx && rx.sync ? rx.sync.words : null;
            if (syncs === 0) { return { state: 'warn', text: 'Такты есть, синхрослово не найдено' }; }
            if (st.frames_bad > 0 || syncs > 0) {
                return { state: 'bad', text: syncs === null
                    ? 'Флаги HDLC есть, CRC не сходится'
                    : 'Синхрослова есть, CRC не сходится' };
            }
            return { state: 'idle', text: 'Сигнал есть, ждём кадр' };
        }
        return { state: 'ok', text: 'Приём идёт: ' + good + ' ' + framesWord(good) + age };
    }

    function diagnoseSSTV(st) {
        const p = st.picture;
        if (p && p.lines === 0) {
            return { state: 'ok', text: 'Обнаружен сигнал SSTV: ' + (p.mode_name || p.mode) };
        }
        if (p) {
            return { state: 'ok', text: 'Идёт обработка: ' + (p.mode_name || p.mode) + ', ' + p.lines + ' из ' + p.height + ' строк' };
        }
        if (st.frames_good > 0) {
            return { state: 'ok', text: 'Приём завершён; принято картинок: ' + st.frames_good };
        }
        if (!st.squelch_open) { return { state: 'idle', text: 'Нет сигнала выше порога' }; }
        return { state: 'idle', text: 'Сигнал есть, ждём заголовок VIS' };
    }

    /** Этапы имитации, пока у станции нет радиотракта. */
    function mockStages(active) {
        if (!active) {
            const out = {};
            for (const k of STAGE_KEYS) { out[k] = stage('idle', '—'); }
            return out;
        }
        return {
            signal: stage('ok', '−42 dBm', 'порог −85 dBm'),
            carrier: stage('ok', '+1,2 кГц', 'девиация 2,4 кГц'),
            clock: stage('ok', 'захват', 'джиттер 4 %'),
            sync: stage('ok', '131', 'BER 3,2e-4'),
            frames: stage('ok', '128 / 3', 'CRC верна / нет'),
        };
    }

    /**
     * FrameStrip — кадры за последнее окно времени для ленты: верные и с ошибкой CRC.
     * @param {number} windowMs
     */
    function FrameStrip(windowMs) {
        this._window = windowMs;
        this._items = [];
    }

    /** Добавляет count кадров вида kind ('good' | 'bad') в момент tMs. */
    FrameStrip.prototype.add = function(kind, tMs, count) {
        for (let i = 0; i < count; i++) {
            this._items.push({ kind: kind, t: tMs });
        }
    };

    FrameStrip.prototype.clear = function() {
        this._items = [];
    };

    /** Кадры окна, заканчивающегося в nowMs; x — доля окна от его начала (0…1). */
    FrameStrip.prototype.points = function(nowMs) {
        const start = nowMs - this._window;
        this._items = this._items.filter(function(it) { return it.t >= start; });
        const w = this._window;
        return this._items.map(function(it) {
            return { kind: it.kind, x: Math.min(1, (it.t - start) / w) };
        });
    };

    /**
     * DemodPanel — шапка канала и блок «Обработка».
     * @param {Object} [opts]
     * @param {function(): number} [opts.now] — часы, мс эпохи
     */
    function DemodPanel(opts) {
        opts = opts || {};
        this._now = opts.now || Date.now;
        this._channel = null;
        this._live = false;
        this._counts = { good: 0, bad: 0 };
        this._strip = new FrameStrip(STRIP_WINDOW_MS);

        this._els = {
            lockHead: document.getElementById('manual-ch-lock'),
            name: document.getElementById('manual-ch-name'),
            freq: document.getElementById('manual-ch-freq'),
            pipeline: document.getElementById('manual-ch-pipeline'),
            diag: document.getElementById('manual-dm-diag'),
            strip: document.getElementById('manual-dm-strip'),
            stripBox: document.getElementById('manual-dm-strip-box'),
            stages: {},
        };
        for (const k of STAGE_KEYS) {
            this._els.stages[k] = {
                row: document.getElementById('manual-st-' + k),
                name: document.getElementById('manual-st-' + k + '-name'),
                value: document.getElementById('manual-st-' + k + '-val'),
                sub: document.getElementById('manual-st-' + k + '-sub'),
            };
        }
    }

    /**
     * Живой режим — у станции есть радиотракт: этапы только из applyStatus.
     * @param {boolean} live
     */
    DemodPanel.prototype.setLive = function(live) {
        if (this._live === live) { return; }
        this._live = live;
        this._counts = { good: 0, bad: 0 };
        this._strip.clear();
        if (live) {
            this._render(mockStages(false), { state: 'idle', text: 'Ждём состояние приёмника' });
            this._renderStrip();
        } else if (this._channel) {
            this.setChannel(this._channel);
        }
    };

    /**
     * Состояние демодулятора из SSE `demod_status`.
     * @param {Object} st
     * @param {?number} levelOffset — сдвиг dBFS в dBm, null — без поправки
     */
    DemodPanel.prototype.applyStatus = function(st, levelOffset) {
        if (!this._live || !st) { return; }
        const now = this._now();
        const c = this._counts;
        if (st.frames_good < c.good || st.frames_bad < c.bad) {
            this._strip.clear(); // новый сеанс потока: счётчики начались заново
        } else {
            this._strip.add('good', now, st.frames_good - c.good);
            this._strip.add('bad', now, st.frames_bad - c.bad);
        }
        c.good = st.frames_good;
        c.bad = st.frames_bad;

        const stages = stagesFromStatus(st, levelOffset);
        this._render(stages, diagnose(st, stages, now));
        // Лента кадров у SSTV и голоса пуста: картинка идёт минуты, звук кадров не даёт.
        if (this._els.stripBox) { this._els.stripBox.hidden = st.demod === 'sstv' || !!VOICE_DEMODS[st.demod]; }
        this._renderStrip();
    };

    DemodPanel.prototype.setChannel = function(ch) {
        if (!ch || typeof ch.freqMHz !== 'number') {
            return;
        }
        this._channel = ch;
        this._renderHeader(ch);
        if (this._live) {
            return;
        }
        const active = ch.active !== false;
        this._render(mockStages(active), active
            ? { state: 'ok', text: 'Имитация: у станции нет радиотракта' }
            : { state: 'idle', text: 'Нет сигнала выше порога' });
    };

    DemodPanel.prototype.getChannel = function() {
        return this._channel;
    };

    DemodPanel.prototype._renderHeader = function(ch) {
        const els = this._els;
        const locked = ch.active !== false;

        if (els.lockHead) {
            els.lockHead.classList.toggle('ml-demod__channel-led--lock', locked);
            els.lockHead.classList.toggle('ml-demod__channel-led--idle', !locked);
            els.lockHead.title = locked ? 'Lock' : 'Нет сигнала';
        }
        if (els.name) {
            els.name.textContent = ch.label || 'Unknown';
        }
        if (els.freq) {
            els.freq.textContent = formatFreqMHz(ch.freqMHz);
        }
        if (els.pipeline) {
            els.pipeline.textContent = ch.modulation || '—';
        }
    };

    DemodPanel.prototype._render = function(stages, diag) {
        for (const k of STAGE_KEYS) {
            const el = this._els.stages[k];
            const s = stages[k];
            if (el.row) {
                el.row.hidden = s.hidden === true;
                for (const state of STAGE_STATES) {
                    el.row.classList.toggle('ml-stage--' + state, s.state === state);
                }
            }
            if (el.name) { el.name.textContent = s.name || STAGE_NAMES[k]; }
            if (el.value) { el.value.textContent = s.value; }
            if (el.sub) { el.sub.textContent = s.sub; }
        }
        const d = this._els.diag;
        if (d) {
            d.textContent = diag.text;
            for (const state of STAGE_STATES) {
                d.classList.toggle('ml-diag--' + state, diag.state === state);
            }
        }
    };

    DemodPanel.prototype._renderStrip = function() {
        const host = this._els.strip;
        if (!host) { return; }
        host.replaceChildren();
        for (const p of this._strip.points(this._now())) {
            const dot = document.createElement('span');
            dot.className = 'ml-strip__dot ml-strip__dot--' + p.kind;
            dot.style.left = (p.x * 100) + '%';
            host.appendChild(dot);
        }
    };

    DemodPanel.prototype.destroy = function() {
        this._channel = null;
        this._strip.clear();
    };

    if (typeof window !== 'undefined') {
        window.DemodPanel = DemodPanel;
        window.formatDemodFreqMHz = formatFreqMHz;
        window.demodLabel = demodLabel;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            DemodPanel: DemodPanel,
            FrameStrip: FrameStrip,
            formatFreqMHz: formatFreqMHz,
            formatHz: formatHz,
            demodLabel: demodLabel,
            stagesFromStatus: stagesFromStatus,
            diagnose: diagnose,
        };
    }
})();
