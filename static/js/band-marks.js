// band-marks.js — обнаруженные сигналы в строке над водопадом «Спектра».
//
// Сервер раз в секунду присылает band_signals: участки над шумом с положением
// (космос или земля), характером излучения и статистикой. Здесь они только рисуются:
// треугольник на частоте, черта по ширине участка и короткий код. Щелчок открывает
// карточку с тем же набором полей, что таблица «Эфир»: передатчик, частота, полоса,
// дрейф, модуляция, вероятность, вид передачи, положение, период, время в эфире,
// окно от первого до последнего, отношение сигнал/шум. Подпись маркера — модуляция и короткий код вида.
// Окно стоит на месте, цифры обновляются с каждым band_signals.

'use strict';

(function() {
    /** Коды ближе этого расстояния не подписываются: треугольник остаётся. */
    const CODE_GAP_PX = 52;
    /** Маркеры ближе этого (или чьи полосы перекрываются) попадают в одну подсказку. */
    const CLUSTER_PX = 16;
    const EMISSION_CODE = { continuous: 'Непр.', pulsed: 'Имп.' };
    const ORIGIN_KIND = { satellite: 'sat', satellite_probable: 'probable', terrestrial: 'ground' };
    /** Модуляция по структуре (ADR-009 §6.1): подпись на маркере и в карточке. */
    const MODULATION = {
        carrier: { code: 'Несущая', text: 'несущая' },
        cw: { code: 'CW', text: 'CW' },
        ook: { code: 'OOK', text: 'OOK' },
        fm: { code: 'ЧМ', text: 'ЧМ' },
        fm_voice: { code: 'ЧМ голос', text: 'ЧМ, голос' },
        wfm: { code: 'ШЧМ', text: 'широкая ЧМ' },
        afsk: { code: 'AFSK', text: 'AFSK' },
        '2fsk': { code: '2FSK', text: '2FSK' },
        lfm: { code: 'ЛЧМ', text: 'ЛЧМ' },
        usb: { code: 'USB', text: 'USB' },
        lsb: { code: 'LSB', text: 'LSB' },
    };

    /** Короткий код вида передачи на треугольнике. */
    const SERVICE_CODE = {
        sstv: 'SSTV',
        sstv_probable: 'SSTV?',
        broadcast: 'Радио',
        amateur_voice: 'Пер.',
        ism_burst: 'Брел',
        lora: 'LoRa',
        gsm_base: 'GSM',
        sat_voice: 'Пер.',
        sat_tx: 'Пер.',
    };

    /** Распознанная модуляция участка или null: не разобран, мало SNR, не опознан. */
    function knownModulation(sig) {
        const st = sig && sig.structure;
        return st && MODULATION[st.modulation] ? st : null;
    }

    /** Код вида передачи на маркере; пусто, если вид не поставлен. */
    function serviceCode(sig) {
        const st = sig && sig.structure;
        if (!st || !st.service) { return ''; }
        return SERVICE_CODE[st.service] || '';
    }

    /**
     * Строка «Вид передачи». Эфирное радио и SSTV оставляют свою подпись.
     * Остальные виды добавляют ступень: «по признакам» или «проба сошлась».
     */
    function serviceText(sig) {
        const st = sig && sig.structure;
        if (!st || !st.service) { return '—'; }
        const label = st.service_label || '';
        if (st.service === 'broadcast' || st.service === 'sstv' || st.service === 'sstv_probable') {
            return label || '—';
        }
        if (st.service_basis === 'probe') {
            return label ? label + ', проба сошлась' : 'проба сошлась';
        }
        if (st.service_basis === 'signature') {
            return label ? label + ', по признакам' : 'по признакам';
        }
        return label || '—';
    }

    /**
     * Короткий код на маркере: модуляция и вид источника; пока разбора нет — излучение.
     * Происхождение (КА/земля) на подпись не выводится.
     */
    function markCode(sig) {
        if (!sig) { return ''; }
        const st = knownModulation(sig);
        const svc = serviceCode(sig);
        if (st) {
            const baud = st.params && st.params.baud;
            let what = MODULATION[st.modulation].code + (baud && st.modulation !== 'cw' ? ' ' + baud : '');
            if (svc) { what += '/' + svc; }
            return what;
        }
        if (svc) { return svc; }
        return EMISSION_CODE[sig.emission] || '';
    }

    /** «2FSK, 4800 бод, девиация ±2.4 кГц»; не разобран — прочерк. Вид передачи — отдельная строка. */
    function modulationText(sig) {
        const st = sig && sig.structure;
        if (!st) { return '—'; }
        let base;
        if (st.modulation_text) {
            base = st.modulation_text;
        } else if (st.modulation === 'undetermined') {
            base = 'не определена';
        } else {
            const m = MODULATION[st.modulation];
            if (!m) {
                base = 'неизвестна';
            } else {
                const p = st.params || {};
                const parts = [m.text];
                if (p.baud) { parts.push(p.baud + ' бод'); }
                if (p.deviation_hz) { parts.push('девиация ±' + formatWidth(p.deviation_hz)); }
                if (p.wpm) { parts.push(Math.round(p.wpm) + ' WPM'); }
                base = parts.join(', ');
            }
        }
        return base;
    }

    /** «10.1 с» у импульсного с устойчивым периодом, иначе прочерк. */
    function periodText(sig) {
        const period = sig.stats && Number(sig.stats.period_s);
        if (sig.emission === 'pulsed' && period) { return period.toFixed(1) + ' с'; }
        return '—';
    }

    /** «14.2 дБ»; нет числа — прочерк. */
    function snrText(sig) {
        const v = Number(sig && sig.snr_db);
        return Number.isFinite(v) ? v + ' дБ' : '—';
    }

    /** Подписи профилей приёмника в карточке (как в панели демода). */
    const DEMOD_LABEL = {
        wfm: 'FM эфир',
        fm: 'ЧМ',
        usb: 'USB',
        lsb: 'LSB',
        cw: 'CW',
        sstv: 'SSTV',
        afsk1200: 'AFSK 1200',
        fsk9600_g3ruh: 'FSK 9600 G3RUH',
        fsk9600_geoscan: 'FSK 9600 GEOSCAN',
    };

    /** Имя КА, иначе NORAD, иначе RDS/станция из структуры, иначе прочерк. */
    function transmitterText(sig, satName) {
        if (satName) { return satName; }
        if (sig && sig.norad_id) { return 'NORAD ' + sig.norad_id; }
        const st = sig && sig.structure;
        if (st && st.station) { return st.station; }
        // Запас: имя уже в подписи службы, а поле station в кадре ещё пустое.
        const label = st && st.service_label;
        const named = 'Эфирное радио: ';
        if (typeof label === 'string' && label.indexOf(named) === 0) {
            const name = label.slice(named.length).trim();
            if (name) { return name; }
        }
        // Идёт сбор RDS — не оставляем «—», чтобы было видно прогресс.
        const progress = 'Эфирное радио (RDS ';
        if (typeof label === 'string' && label.indexOf(progress) === 0) {
            return label.slice('Эфирное радио ('.length, -1); // «RDS 2/4»
        }
        return '—';
    }

    /** Рекомендуемый демодулятор из structure.demod; нет — прочерк. */
    function demodText(sig) {
        const id = sig && sig.structure && sig.structure.demod;
        if (!id) { return '—'; }
        return DEMOD_LABEL[id] || id;
    }

    /** Местные часы «HH:MM:SS»; нулевое или пустое — прочерк. */
    function clockText(ms) {
        const t = Number(ms);
        if (!t) { return '—'; }
        const d = new Date(t);
        if (!Number.isFinite(d.getTime())) { return '—'; }
        const p = function(n) { return n < 10 ? '0' + n : String(n); };
        return p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
    }

    /** «12:04:01 → 12:05:42»; нет ни одного — прочерк. */
    function timeRangeText(sig) {
        const st = sig && sig.stats;
        const first = clockText(st && st.first_seen_ms);
        const last = clockText(sig && sig.last_seen_ms);
        if (first === '—' && last === '—') { return '—'; }
        if (first === last) { return first; }
        return first + ' → ' + last;
    }

    /** «0 Гц/с», «−162 Гц/с», «1.5 Гц/с». Нет числа — прочерк, ноль измерения остаётся нулём. */
    function formatDrift(hzS) {
        const n = Number(hzS);
        if (!Number.isFinite(n)) { return '—'; }
        const abs = Math.abs(n);
        const body = abs >= 10 ? String(Math.round(abs)) : String(Math.round(abs * 10) / 10);
        return (n < 0 ? '−' : '') + body + ' Гц/с';
    }

    /** Положение источника: космос, земля или прочерк, пока не ясно. */
    function positionText(sig) {
        switch (sig && sig.origin) {
        case 'satellite': return 'Космос';
        case 'satellite_probable': return 'Космос?';
        case 'terrestrial': return 'Земля';
        default: return '—';
        }
    }

    /** Вид маркера по происхождению: sat, probable, ground, unknown. */
    function markKind(sig) {
        return (sig && ORIGIN_KIND[sig.origin]) || 'unknown';
    }

    /** «41 с», «5 мин», «2 мин 30 с», «1 ч 2 мин». */
    function formatDuration(s) {
        const t = Math.max(0, Math.floor(Number(s) || 0));
        if (t < 60) { return t + ' с'; }
        if (t < 3600) {
            const sec = t % 60;
            return Math.floor(t / 60) + ' мин' + (sec ? ' ' + sec + ' с' : '');
        }
        return Math.floor(t / 3600) + ' ч ' + Math.floor((t % 3600) / 60) + ' мин';
    }

    /** «18 кГц», «2.5 кГц», «800 Гц». Ноль и пустое — прочерк: ширину не измеряли. */
    function formatWidth(hz) {
        const n = Number(hz);
        if (!(n > 0)) { return '—'; }
        return n >= 1000 ? Number((n / 1000).toFixed(1)) + ' кГц' : Math.round(n) + ' Гц';
    }

    /**
     * Оценка дерева по модуляции, 0…1. Процент вида передачи сюда не подставляется:
     * его нет, пока нет клетки таблицы ошибок.
     */
    function probabilityText(sig) {
        const v = Number(sig && sig.structure && sig.structure.confidence);
        if (!(v > 0)) { return '—'; }
        return Math.round(v * 100) + ' %';
    }

    /**
     * Строки карточки и таблицы «Эфир» — один набор полей.
     * @param {Object} sig
     * @param {?string} satName — имя КА для колонки «Передатчик»
     * @returns {Array<[string, string]>}
     */
    function markRows(sig, satName) {
        const st = sig.stats || {};
        return [
            ['Передатчик', transmitterText(sig, satName)],
            ['Частота', (sig.center_hz / 1e6).toFixed(4) + ' МГц'],
            ['Полоса', formatWidth(sig.bandwidth_hz || 0)],
            ['Дрейф', formatDrift(st.drift_hz_s)],
            ['Модуляция', modulationText(sig)],
            ['Вероятность', probabilityText(sig)],
            ['Вид передачи', serviceText(sig)],
            ['Положение', positionText(sig)],
            ['Демодулятор', demodText(sig)],
            ['Период', periodText(sig)],
            ['С/Ш', snrText(sig)],
            ['В эфире', st.on_s > 0 ? formatDuration(st.on_s) : '—'],
            ['Время', timeRangeText(sig)],
        ];
    }

    /** Те же строки текстом: для проверки. */
    function markTitle(sig, satName) {
        return markRows(sig, satName).map(function(row) { return row[0] + '\t' + row[1]; }).join('\n');
    }

    /** Короткая подпись маркера для чтения с экрана. */
    function markSummary(sig) {
        const freq = (sig.center_hz / 1e6).toFixed(4) + ' МГц';
        return sig.bandwidth_hz ? freq + ', полоса ' + formatWidth(sig.bandwidth_hz) : freq;
    }

    /**
     * Участки, чьи маркеры ближе gapPx к x или чья полоса его накрывает — одна подсказка.
     * @param {Array<{x: number, halfPx: number, sig: Object}>} placed
     * @param {number} x
     * @param {number} gapPx
     * @returns {Object[]}
     */
    function clusterAt(placed, x, gapPx) {
        let focusHalf = 0;
        for (let i = 0; i < (placed || []).length; i++) {
            if (Math.abs(placed[i].x - x) <= gapPx) {
                focusHalf = Math.max(focusHalf, placed[i].halfPx || 0);
            }
        }
        const out = [];
        for (let i = 0; i < (placed || []).length; i++) {
            const p = placed[i];
            const reach = gapPx + Math.max(focusHalf, p.halfPx || 0);
            if (Math.abs(p.x - x) <= reach) { out.push(p.sig); }
        }
        return out;
    }

    /**
     * Актуальные участки открытой карточки. Тот же id, свежие цифры; нет участка — null.
     * @param {?string} openId
     * @param {Array<{x: number, halfPx: number, sig: Object}>} placed
     * @param {number} gapPx
     * @returns {?Object[]}
     */
    function openCardSignals(openId, placed, gapPx) {
        if (!openId) { return null; }
        let hit = null;
        for (let i = 0; i < (placed || []).length; i++) {
            if (placed[i].sig && placed[i].sig.id === openId) { hit = placed[i]; break; }
        }
        if (!hit) { return null; }
        const cluster = clusterAt(placed, hit.x, gapPx);
        return cluster.length ? cluster : [hit.sig];
    }

    /**
     * Точка на краю карточки, ближайшая к маркеру: конец сноски.
     * @param {{left: number, right: number, top: number, bottom: number}} card
     * @param {number} x
     * @param {number} y
     * @returns {{x: number, y: number}}
     */
    function leaderAnchor(card, x, y) {
        return {
            x: Math.min(Math.max(x, card.left), card.right),
            y: Math.min(Math.max(y, card.top), card.bottom),
        };
    }

    /**
     * Пункт «Полосы канала» под участок: самый узкий доступный не уже сигнала,
     * иначе самый широкий доступный.
     * @param {?Array<{value: string, disabled: ?boolean}>} options
     * @param {number} widthHz
     * @returns {?string}
     */
    function pickBandwidth(options, widthHz) {
        let fit = null;
        let widest = null;
        for (let i = 0; i < (options || []).length; i++) {
            const o = options[i];
            if (o.disabled) { continue; }
            const hz = Number(o.value);
            if (hz >= widthHz && (!fit || hz < Number(fit.value))) { fit = o; }
            if (!widest || hz > Number(widest.value)) { widest = o; }
        }
        const pick = fit || widest;
        return pick ? String(pick.value) : null;
    }

    /**
     * @param {HTMLElement} host — строка над водопадом
     * @param {Object} opts
     * @param {HTMLCanvasElement} opts.plotCanvas — водопад: по нему горизонталь
     * @param {number} opts.marginLeft
     * @param {number} opts.marginRight
     * @param {function(Object)} [opts.onPick] — не используется карточкой
     * @param {function(number): ?string} opts.colorOf — цвет КА по NORAD
     * @param {function(number): ?string} opts.nameOf — имя КА по NORAD
     */
    function BandMarks(host, opts) {
        this._host = host;
        this._opts = opts || {};
        this._band = null;
        this._view = null;
        this._signals = [];
        this._nowMs = 0;
        this._placed = [];
        this._els = {};
        this._cardOpen = false;
        this._cardId = null;
        this._cardText = '';
        if (!host || typeof document === 'undefined') { return; }
        this._root = host;
        this._buildCard();
    }

    /** Карточка и сноска живут на body: строка маркеров их обрезает. */
    BandMarks.prototype._buildCard = function() {
        const self = this;
        const svgNs = 'http://www.w3.org/2000/svg';
        this._leader = document.createElementNS(svgNs, 'svg');
        this._leader.setAttribute('class', 'ml-bandmark-leader');
        this._leader.setAttribute('hidden', '');
        this._leaderLine = document.createElementNS(svgNs, 'line');
        this._leader.appendChild(this._leaderLine);

        this._card = document.createElement('div');
        this._card.className = 'ml-bandmark-tip';
        this._card.hidden = true;
        this._card.setAttribute('role', 'dialog');
        this._card.setAttribute('aria-label', 'Сигнал');
        const head = document.createElement('div');
        head.className = 'ml-bandmark-tip__head';
        this._cardTitle = document.createElement('span');
        const close = document.createElement('button');
        close.type = 'button';
        close.className = 'ml-bandmark-tip__close';
        close.textContent = '×';
        close.setAttribute('aria-label', 'Закрыть');
        close.addEventListener('click', function(e) {
            if (e && typeof e.stopPropagation === 'function') { e.stopPropagation(); }
            self._closeCard();
        });
        head.appendChild(this._cardTitle);
        head.appendChild(close);
        this._cardBody = document.createElement('div');
        this._cardBody.className = 'ml-bandmark-tip__body';
        this._card.appendChild(head);
        this._card.appendChild(this._cardBody);

        this._onKey = function(e) {
            if (e.key === 'Escape' && self._cardOpen) { self._closeCard(); }
        };
        document.addEventListener('keydown', this._onKey);
        document.body.appendChild(this._leader);
        document.body.appendChild(this._card);
    };

    /** Окно приёмника из кадра потока. */
    BandMarks.prototype.setBand = function(centerHz, spanHz) {
        if (!(spanHz > 0)) { return; }
        if (this._band && this._band.centerHz === centerHz && this._band.spanHz === spanHz) { return; }
        this._band = { centerHz: centerHz, spanHz: spanHz };
        this.render();
    };

    /** Видимая часть окна при масштабе: центр от центра приёмника и ширина; без ширины — всё окно. */
    BandMarks.prototype.setView = function(offsetHz, spanHz) {
        this._view = spanHz > 0 ? { offsetHz: Number(offsetHz) || 0, spanHz: spanHz } : null;
        this.render();
    };

    /**
     * @param {Array} signals — band_signals.signals
     * @param {number} nowMs — band_signals.timestamp_ms
     */
    BandMarks.prototype.setSignals = function(signals, nowMs) {
        this._signals = Array.isArray(signals) ? signals : [];
        this._nowMs = Number(nowMs) || 0;
        this.render();
    };

    BandMarks.prototype._plot = function() {
        const c = this._opts.plotCanvas;
        if (!c || !this._host || typeof c.getBoundingClientRect !== 'function') { return null; }
        const cr = c.getBoundingClientRect();
        const hr = this._host.getBoundingClientRect();
        const ml = this._opts.marginLeft || 0;
        const mr = this._opts.marginRight || 0;
        return { left: cr.left - hr.left + ml, width: cr.width - ml - mr };
    };

    /** Маркер участка: элементы живут между событиями, поэтому щелчок не сбивается. */
    BandMarks.prototype._el = function(id) {
        let el = this._els[id];
        if (el) { return el; }
        const bar = document.createElement('div');
        bar.className = 'ml-bandmark__bar';
        const btn = document.createElement('button');
        btn.type = 'button';
        const self = this;
        btn.addEventListener('click', function(e) {
            if (e && typeof e.stopPropagation === 'function') { e.stopPropagation(); }
            self._openCard(btn);
        });
        this._root.appendChild(bar);
        this._root.appendChild(btn);
        el = { bar: bar, btn: btn };
        this._els[id] = el;
        return el;
    };

    BandMarks.prototype._closeCard = function() {
        this._cardOpen = false;
        this._cardId = null;
        this._cardText = '';
        this._holdCard = null;
        if (this._card) { this._card.hidden = true; }
        if (this._leader) { this._leader.setAttribute('hidden', ''); }
    };

    /**
     * Карточка у точки на экране. Так открывается метка панорамы:
     * у неё нет кнопки в полосе «Спектра».
     * @param {number} clientX
     * @param {number} clientY
     * @param {Array} sigs
     * @param {string} focusId
     */
    BandMarks.prototype.showAt = function(clientX, clientY, sigs, focusId) {
        if (!this._card || !sigs || !sigs.length) { return; }
        const id = focusId || sigs[0].id;
        this._holdCard = { id: id, x: clientX, y: clientY };
        this._cardOpen = true;
        this._cardId = id;
        this._fillCard(id, sigs);
        this._card.hidden = false;
        this._placeCard({
            getBoundingClientRect: function() {
                return { left: clientX, top: clientY, right: clientX, bottom: clientY, width: 0, height: 0 };
            },
        });
        this._drawLeaderPoint();
    };

    /**
     * Щелчок по маркеру. Повторный щелчок меняет участок, окно не двигает.
     * @param {HTMLButtonElement} btn
     */
    BandMarks.prototype._openCard = function(btn) {
        if (!this._card || !btn || !btn._sig) { return; }
        this._holdCard = null;
        const sigs = openCardSignals(btn._sig.id, this._placed, CLUSTER_PX);
        if (!sigs) { return; }
        const place = !this._cardOpen;
        this._cardOpen = true;
        this._cardId = btn._sig.id;
        this._fillCard(btn._sig.id, sigs);
        this._card.hidden = false;
        if (place) { this._placeCard(btn); }
        this._drawLeader();
    };

    /** Сноска от точки щелчка на панораме к карточке. */
    BandMarks.prototype._drawLeaderPoint = function() {
        const h = this._holdCard;
        if (!this._cardOpen || !h || !this._card || typeof this._card.getBoundingClientRect !== 'function') {
            if (this._leader) { this._leader.setAttribute('hidden', ''); }
            return;
        }
        const c = this._card.getBoundingClientRect();
        const end = leaderAnchor(c, h.x, h.y);
        this._leader.removeAttribute('hidden');
        this._leaderLine.setAttribute('x1', String(h.x));
        this._leaderLine.setAttribute('y1', String(h.y));
        this._leaderLine.setAttribute('x2', String(end.x));
        this._leaderLine.setAttribute('y2', String(end.y));
        this._leaderLine.setAttribute('stroke', 'currentColor');
    };

    /** Свежие цифры того же участка. Окно не двигаем. Нет участка — закрываем. */
    BandMarks.prototype._refreshOpenCard = function() {
        if (!this._cardOpen) { return; }
        if (this._holdCard) {
            const id = this._holdCard.id;
            let fresh = null;
            for (let i = 0; i < this._signals.length; i++) {
                if (this._signals[i].id === id) { fresh = this._signals[i]; break; }
            }
            if (fresh) { this._fillCard(id, [fresh]); }
            this._drawLeaderPoint();
            return;
        }
        const sigs = openCardSignals(this._cardId, this._placed, CLUSTER_PX);
        if (!sigs) { this._closeCard(); return; }
        this._fillCard(this._cardId, sigs);
        this._drawLeader();
    };

    /** Текст карточки. Повтор с тем же текстом не пересобирает окно. */
    BandMarks.prototype._fillCard = function(focusId, sigs) {
        const nameOf = typeof this._opts.nameOf === 'function' ? this._opts.nameOf : null;
        const chunks = [];
        for (let i = 0; i < sigs.length; i++) {
            const sig = sigs[i];
            const name = nameOf && sig.norad_id ? nameOf(sig.norad_id) : null;
            chunks.push((sig.id === focusId ? '*' : '') + sig.id + '\n' + markTitle(sig, name));
        }
        const text = chunks.join('\n');
        if (text === this._cardText) { return; }
        this._cardText = text;
        this._cardTitle.textContent = '';
        this._cardBody.textContent = '';
        for (let i = 0; i < sigs.length; i++) {
            const sig = sigs[i];
            const name = nameOf && sig.norad_id ? nameOf(sig.norad_id) : null;
            const rows = markRows(sig, name);
            const row = document.createElement('div');
            row.className = 'ml-bandmark-tip__row' + (sig.id === focusId ? ' is-focus' : '');
            for (let j = 0; j < rows.length; j++) {
                const key = document.createElement('div');
                key.className = 'ml-bandmark-tip__k';
                key.textContent = rows[j][0];
                const val = document.createElement('div');
                val.className = 'ml-bandmark-tip__v';
                val.textContent = rows[j][1];
                row.appendChild(key);
                row.appendChild(val);
            }
            this._cardBody.appendChild(row);
        }
    };

    /** Первый показ: рядом с маркером. Дальше карточка стоит, куда её поставили. */
    BandMarks.prototype._placeCard = function(btn) {
        if (!this._card || !btn || typeof btn.getBoundingClientRect !== 'function') { return; }
        const r = btn.getBoundingClientRect();
        const card = this._card;
        const tw = card.offsetWidth || 280;
        const th = card.offsetHeight || 160;
        const viewW = typeof window !== 'undefined' ? window.innerWidth : tw + 16;
        const viewH = typeof window !== 'undefined' ? window.innerHeight : th + 16;
        let left = r.right + 36;
        if (left + tw > viewW - 8) { left = r.left - tw - 36; }
        if (left < 8) { left = 8; }
        let top = r.top - th / 2;
        if (top < 8) { top = 8; }
        if (top + th > viewH - 8) { top = Math.max(8, viewH - th - 8); }
        card.style.left = Math.round(left) + 'px';
        card.style.top = Math.round(top) + 'px';
    };

    /** Сноска от маркера к ближайшей точке края карточки. Маркер уехал — линия за ним, окно нет. */
    BandMarks.prototype._drawLeader = function() {
        const el = this._cardId && this._els[this._cardId];
        if (!this._cardOpen || !el || !this._card || typeof el.btn.getBoundingClientRect !== 'function') {
            if (this._leader) { this._leader.setAttribute('hidden', ''); }
            return;
        }
        const m = el.btn.getBoundingClientRect();
        const c = this._card.getBoundingClientRect();
        const mx = m.left + m.width / 2;
        const my = m.top + m.height / 2;
        const end = leaderAnchor(c, mx, my);
        this._leader.removeAttribute('hidden');
        this._leaderLine.setAttribute('x1', String(mx));
        this._leaderLine.setAttribute('y1', String(my));
        this._leaderLine.setAttribute('x2', String(end.x));
        this._leaderLine.setAttribute('y2', String(end.y));
        this._leaderLine.setAttribute('stroke', getComputedStyle(el.btn).color || 'currentColor');
    };

    BandMarks.prototype.render = function() {
        if (!this._root || typeof window === 'undefined' || !window.TxMarkers) { return; }
        const plot = this._plot();
        const band = this._band;
        const spanHz = this._view ? this._view.spanHz : (band ? band.spanHz : 0);
        const colorOf = typeof this._opts.colorOf === 'function' ? this._opts.colorOf : null;
        const nameOf = typeof this._opts.nameOf === 'function' ? this._opts.nameOf : null;
        const keep = {};
        const placed = [];
        let lastCodeX = -Infinity;
        for (let i = 0; i < this._signals.length; i++) {
            const sig = this._signals[i];
            const x = window.TxMarkers.markerX(sig.center_hz, band, this._view, plot);
            if (x === null || !sig.id) { continue; }
            keep[sig.id] = true;
            const halfPx = sig.bandwidth_hz * plot.width / (2 * spanHz);
            placed.push({ x: x, halfPx: halfPx, sig: sig });
            const el = this._el(sig.id);
            const kind = markKind(sig);
            const satColor = sig.norad_id && (kind === 'sat' || kind === 'probable') && colorOf
                ? colorOf(sig.norad_id) : null;
            let code = markCode(sig);
            if (x - lastCodeX < CODE_GAP_PX) { code = ''; } else if (code) { lastCodeX = x; }
            const widthPx = Math.max(2, sig.bandwidth_hz * plot.width / spanHz);

            el.bar.className = 'ml-bandmark__bar ml-bandmark--' + kind;
            el.bar.style.left = Math.round(x - widthPx / 2) + 'px';
            el.bar.style.width = Math.round(widthPx) + 'px';
            el.btn.className = 'ml-bandmark ml-bandmark--' + kind +
                (this._cardOpen && sig.id === this._cardId ? ' is-open' : '');
            el.btn.style.left = Math.round(x) + 'px';
            el.btn.textContent = code;
            el.btn.removeAttribute('title');
            el.btn.setAttribute('aria-label', markSummary(sig) +
                (nameOf && sig.norad_id && nameOf(sig.norad_id) ? ', ' + nameOf(sig.norad_id) : '') +
                ', подробности по щелчку');
            [el.bar, el.btn].forEach(function(node) {
                if (satColor) {
                    node.style.setProperty('--ml-bandmark-color', satColor);
                } else {
                    node.style.removeProperty('--ml-bandmark-color');
                }
            });
            el.btn._sig = sig;
            el.btn._x = x;
        }
        this._placed = placed;
        for (const id in this._els) {
            if (keep[id]) { continue; }
            const el = this._els[id];
            this._root.removeChild(el.bar);
            this._root.removeChild(el.btn);
            delete this._els[id];
        }
        this._refreshOpenCard();
    };

    BandMarks.prototype.destroy = function() {
        this._closeCard();
        if (this._onKey) { document.removeEventListener('keydown', this._onKey); }
        if (this._card && this._card.parentNode) { this._card.parentNode.removeChild(this._card); }
        if (this._leader && this._leader.parentNode) { this._leader.parentNode.removeChild(this._leader); }
        this._card = null;
        this._leader = null;
        if (this._root) { this._root.textContent = ''; }
        this._els = {};
        this._root = null;
    };

    BandMarks.markCode = markCode;
    BandMarks.markKind = markKind;
    BandMarks.markTitle = markTitle;
    BandMarks.markRows = markRows;
    BandMarks.markSummary = markSummary;
    BandMarks.clusterAt = clusterAt;
    BandMarks.openCardSignals = openCardSignals;
    BandMarks.leaderAnchor = leaderAnchor;
    BandMarks.pickBandwidth = pickBandwidth;

    if (typeof window !== 'undefined') {
        window.BandMarks = BandMarks;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            BandMarks: BandMarks,
            markCode: markCode,
            markKind: markKind,
            markTitle: markTitle,
            markRows: markRows,
            markSummary: markSummary,
            clusterAt: clusterAt,
            openCardSignals: openCardSignals,
            leaderAnchor: leaderAnchor,
            pickBandwidth: pickBandwidth,
        };
    }
})();
