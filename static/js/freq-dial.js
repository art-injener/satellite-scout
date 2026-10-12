/*
 * FreqDial — цифровой индикатор-регулятор частоты в стиле газоразрядных
 * индикаторов (ИН-12 / ИВ-11). Одновременно и показывает текущую настройку,
 * и служит единственным органом управления ею: отдельного поля ввода,
 * селектора единиц и селектора шага больше нет — разрядом является позиция
 * курсора, шагом — вес этого разряда.
 *
 *   ┌─────────────────────────────────────┐
 *   │          00.437.8[0]0.000           │
 *   ├──────┬──────┬───────┬──────┬────────┤
 *   │  −   │  ‹   │  SET  │  ›   │   +    │
 *   └──────┴──────┴───────┴──────┴────────┘
 *
 * 11 разрядов покрывают 1 Гц … 10 ГГц. Значение хранится в целых герцах —
 * никакой плавающей точки, чтобы шаг 1 Гц оставался точным на 10 ГГц.
 *
 * Набранная частота уходит приёмнику (onChange) только по SET или Enter.
 * До этого индикатор в режиме перестройки: курсор мигает, приёмник стоит
 * на прежней частоте. Esc возвращает последнюю установленную.
 *
 * Управление:
 *   −/+        инкремент/декремент разряда под курсором (с переносом)
 *   ‹/›        сдвиг курсора по разрядам
 *   SET        установить набранную частоту
 *   клик       поставить курсор на разряд
 *   колесо     шаг разряда под указателем
 *   ←/→ ↑/↓    то же с клавиатуры, Home/End — крайние разряды
 *   0…9        ручной ввод в позицию курсора со сдвигом вправо
 *   Backspace  обнулить разряд слева, Delete — текущий
 *   Enter/Esc  установить / отменить набранное
 */
(function() {
    'use strict';

    /** Число разрядов: 11 → максимум 99.999.999.999 Гц, с запасом над 10 ГГц. */
    const DIGIT_COUNT = 11;

    /** Группировка разрядов слева направо, разделитель — точка. */
    const GROUPS = [2, 3, 3, 3];

    const DEFAULT_MAX_HZ = 10000000000; // 10 ГГц
    const DEFAULT_POS = 7;              // единицы кГц — привычный шаг перестройки

    /** Пауза до автоповтора при удержании кнопки и период повтора. */
    const REPEAT_DELAY_MS = 380;
    const REPEAT_STEP_MS = 70;

    const SVG_NS = 'http://www.w3.org/2000/svg';

    /*
     * Семисегментный разряд. Сегменты замощают контур цифры трапециями со
     * скосом 45° по углам: они соприкасаются, но не перекрываются — иначе
     * незажжённые «призрачные» сегменты давали бы яркие пятна на стыках.
     * Зазор между сегментами рисует обводка цветом окна лампы (CSS).
     *
     *    aaaa      viewBox 100×180, толщина сегмента 18,
     *   f    b     перекладина g по середине (y = 90)
     *    gggg
     *   e    c
     *    dddd
     */
    const SEG_POINTS = {
        a: '0,0 100,0 82,18 18,18',
        b: '100,0 82,18 82,81 100,90',
        c: '100,90 82,99 82,162 100,180',
        d: '0,180 100,180 82,162 18,162',
        e: '0,90 18,99 18,162 0,180',
        f: '0,0 18,18 18,81 0,90',
        g: '0,90 18,81 82,81 100,90 82,99 18,99',
    };

    const SEG_ORDER = ['a', 'b', 'c', 'd', 'e', 'f', 'g'];

    /** Какие сегменты горят для каждой цифры. */
    const DIGIT_SEGMENTS = {
        '0': 'abcdef',
        '1': 'bc',
        '2': 'abdeg',
        '3': 'abcdg',
        '4': 'bcfg',
        '5': 'acdfg',
        '6': 'acdefg',
        '7': 'abc',
        '8': 'abcdefg',
        '9': 'abcdfg',
    };

    const ACTIONS = [
        { act: 'dec', glyph: '−', label: 'Уменьшить разряд под курсором' },
        { act: 'left', glyph: '‹', label: 'Курсор на разряд влево' },
        { act: 'set', glyph: 'SET', label: 'Установить набранную частоту' },
        { act: 'right', glyph: '›', label: 'Курсор на разряд вправо' },
        { act: 'inc', glyph: '+', label: 'Увеличить разряд под курсором' },
    ];

    /** Целые герцы → '00.437.800.000' (ровно DIGIT_COUNT разрядов). */
    function formatHz(hz) {
        let n = Math.round(Math.abs(Number(hz) || 0));
        let digits = String(n);
        if (digits.length > DIGIT_COUNT) { digits = digits.slice(-DIGIT_COUNT); }
        while (digits.length < DIGIT_COUNT) { digits = '0' + digits; }
        let out = '';
        let i = 0;
        for (let g = 0; g < GROUPS.length; g++) {
            if (g > 0) { out += '.'; }
            out += digits.substr(i, GROUPS[g]);
            i += GROUPS[g];
        }
        return out;
    }

    /** Вес разряда в герцах: позиция 0 — старший разряд. */
    function placeValue(pos) {
        return Math.pow(10, DIGIT_COUNT - 1 - pos);
    }

    function clamp(v, lo, hi) {
        if (v < lo) { return lo; }
        if (v > hi) { return hi; }
        return v;
    }

    /**
     * @param {HTMLElement} root контейнер, разметку виджет строит сам
     * @param {{minHz?:number, maxHz?:number, hz?:number, pos?:number,
     *          unitLabel?:string, ariaLabel?:string,
     *          onChange?:function(number):void}} [opts]
     */
    function FreqDial(root, opts) {
        opts = opts || {};
        this._root = root || null;
        this._minHz = typeof opts.minHz === 'number' ? Math.max(0, Math.round(opts.minHz)) : 0;
        this._maxHz = typeof opts.maxHz === 'number' ? Math.round(opts.maxHz) : DEFAULT_MAX_HZ;
        if (this._maxHz < this._minHz) { this._maxHz = this._minHz; }
        this._onChange = typeof opts.onChange === 'function' ? opts.onChange : null;
        this._unitLabel = typeof opts.unitLabel === 'string' ? opts.unitLabel : '';
        this._unitAria = opts.unitAria || 'Гц';
        this._ariaLabel = opts.ariaLabel || 'Частота настройки';

        this._hz = clamp(Math.round(Number(opts.hz) || 0), this._minHz, this._maxHz);
        /** Частота, на которой стоит приёмник; _hz — то, что набрано на индикаторе. */
        this._appliedHz = this._hz;
        this._editing = false;
        this._pos = clamp(
            typeof opts.pos === 'number' ? opts.pos : DEFAULT_POS,
            0, DIGIT_COUNT - 1
        );

        this._digitEls = [];
        this._strip = null;
        this._handlers = [];
        this._repeatTimer = null;
        this._repeatInterval = null;
        this._repeated = false;

        this._build();
        this._render();
    }

    // ── Построение разметки ───────────────────────────────────────────────

    FreqDial.prototype._on = function(el, type, fn, opts) {
        if (!el || typeof el.addEventListener !== 'function') { return; }
        el.addEventListener(type, fn, opts);
        this._handlers.push([el, type, fn]);
    };

    /**
     * Разряд: обёртка-span (несёт позицию, рамку курсора и цель клика) и
     * внутри SVG из семи сегментов. Сегменты не пересобираются при смене
     * цифры — переключается только класс `is-on`.
     * @param {number} pos
     */
    FreqDial.prototype._makeDigit = function(pos) {
        const cell = document.createElement('span');
        cell.className = 'fdial__digit';
        cell.setAttribute('data-pos', String(pos));
        cell.setAttribute('data-digit', '0');
        cell.setAttribute('aria-hidden', 'true');

        const svg = document.createElementNS(SVG_NS, 'svg');
        svg.setAttribute('class', 'fdial__seg7');
        svg.setAttribute('viewBox', '0 0 100 180');
        svg.setAttribute('focusable', 'false');
        svg.setAttribute('aria-hidden', 'true');

        const segs = {};
        for (let i = 0; i < SEG_ORDER.length; i++) {
            const name = SEG_ORDER[i];
            const poly = document.createElementNS(SVG_NS, 'polygon');
            poly.setAttribute('class', 'fdial__seg');
            poly.setAttribute('points', SEG_POINTS[name]);
            svg.appendChild(poly);
            segs[name] = poly;
        }
        cell.appendChild(svg);
        cell._segs = segs;
        return cell;
    };

    /** Зажигает сегменты под указанную цифру. */
    FreqDial.prototype._paintDigit = function(cell, ch) {
        const lit = DIGIT_SEGMENTS[ch] || '';
        const segs = cell._segs;
        if (!segs) { return; }
        for (let i = 0; i < SEG_ORDER.length; i++) {
            const name = SEG_ORDER[i];
            const poly = segs[name];
            if (!poly || !poly.classList) { continue; }
            poly.classList.toggle('is-on', lit.indexOf(name) >= 0);
        }
        cell.setAttribute('data-digit', ch);
    };

    FreqDial.prototype._build = function() {
        const root = this._root;
        if (!root || typeof document === 'undefined'
            || typeof document.createElement !== 'function') { return; }
        const self = this;

        root.textContent = '';
        root.classList.add('fdial');

        const makeBtn = function(spec) {
            const b = document.createElement('button');
            b.type = 'button';
            b.className = 'fdial__btn fdial__btn--' + spec.act;
            b.textContent = spec.glyph;
            b.tabIndex = -1; // фокус остаётся на разрядах — клавиатура работает там
            b.setAttribute('aria-label', spec.label);
            b.setAttribute('data-act', spec.act);
            // Не отдаём фокус кнопке, иначе стрелки перестанут крутить разряды
            self._on(b, 'mousedown', function(e) {
                if (e && typeof e.preventDefault === 'function') { e.preventDefault(); }
            });
            self._on(b, 'click', function() { self._onAction(spec.act, false); });
            // Установка частоты — разовое действие, удержание её не повторяет
            if (spec.act !== 'set') {
                self._on(b, 'pointerdown', function() { self._startRepeat(spec.act); });
                self._on(b, 'pointerup', function() { self._stopRepeat(); });
                self._on(b, 'pointercancel', function() { self._stopRepeat(); });
                self._on(b, 'pointerleave', function() { self._stopRepeat(); });
            }
            return b;
        };

        const strip = document.createElement('div');
        strip.className = 'fdial__digits';
        strip.tabIndex = 0;
        strip.setAttribute('role', 'spinbutton');
        strip.setAttribute('aria-label', this._ariaLabel);
        strip.setAttribute('aria-valuemin', String(this._minHz));
        strip.setAttribute('aria-valuemax', String(this._maxHz));

        let idx = 0;
        for (let g = 0; g < GROUPS.length; g++) {
            if (g > 0) {
                const sep = document.createElement('span');
                sep.className = 'fdial__sep';
                sep.textContent = '.';
                sep.setAttribute('aria-hidden', 'true');
                strip.appendChild(sep);
            }
            for (let k = 0; k < GROUPS[g]; k++) {
                const cell = this._makeDigit(idx);
                strip.appendChild(cell);
                this._digitEls.push(cell);
                idx++;
            }
        }

        this._on(strip, 'keydown', function(e) { self._onKeyDown(e); });
        this._on(strip, 'click', function(e) { self._onDigitClick(e); });
        this._on(strip, 'wheel', function(e) { self._onWheel(e); }, { passive: false });
        root.appendChild(strip);
        this._strip = strip;

        // Разряды сверху, клавиши под ними — порядок DOM совпадает с визуальным
        for (let i = 0; i < ACTIONS.length; i++) {
            root.appendChild(makeBtn(ACTIONS[i]));
        }

        if (this._unitLabel) {
            const unit = document.createElement('span');
            unit.className = 'fdial__unit';
            unit.textContent = this._unitLabel;
            root.appendChild(unit);
        }
    };

    // ── Отрисовка ────────────────────────────────────────────────────────

    FreqDial.prototype._render = function() {
        const text = formatHz(this._hz);
        const digits = text.replace(/\./g, '');
        // Незначащие нули приглушены — глаз сразу цепляет реальную частоту
        let lead = 0;
        while (lead < DIGIT_COUNT - 1 && digits.charAt(lead) === '0') { lead++; }

        for (let i = 0; i < this._digitEls.length; i++) {
            const el = this._digitEls[i];
            this._paintDigit(el, digits.charAt(i));
            el.classList.toggle('is-cursor', i === this._pos);
            el.classList.toggle('is-lead', i < lead);
        }
        if (this._strip) {
            this._strip.classList.toggle('is-editing', this._editing);
            this._strip.setAttribute('aria-valuenow', String(this._hz));
            this._strip.setAttribute('aria-valuetext', text + ' ' + this._unitAria);
        }
    };

    // ── Изменение значения ───────────────────────────────────────────────

    /**
     * Набор оператором: меняется только индикатор, приёмник ждёт SET.
     * @param {number} hz
     */
    FreqDial.prototype._edit = function(hz) {
        this._hz = clamp(Math.round(hz), this._minHz, this._maxHz);
        this._editing = true;
        this._render();
    };

    /** SET / Enter: набранная частота уходит приёмнику, перестройка завершена. */
    FreqDial.prototype._apply = function() {
        const changed = this._hz !== this._appliedHz;
        this._appliedHz = this._hz;
        this._finishEdit();
        if (changed && this._onChange) { this._onChange(this._hz); }
    };

    /** Esc: индикатор возвращается к частоте, на которой стоит приёмник. */
    FreqDial.prototype._cancel = function() {
        this._hz = this._appliedHz;
        this._finishEdit();
    };

    /** Курсор перестаёт мигать: снимаем и режим перестройки, и фокус. */
    FreqDial.prototype._finishEdit = function() {
        this._editing = false;
        this._render();
        if (this._strip && typeof this._strip.blur === 'function') { this._strip.blur(); }
    };

    FreqDial.prototype._step = function(dir) {
        this._edit(this._hz + dir * placeValue(this._pos));
    };

    /** Ставит курсор на разряд; любое касание индикатора начинает перестройку. */
    FreqDial.prototype._placeCursor = function(pos) {
        this._pos = clamp(pos, 0, DIGIT_COUNT - 1);
        this._editing = true;
        this._render();
    };

    FreqDial.prototype._moveCursor = function(delta) {
        this._placeCursor(this._pos + delta);
    };

    /** Ручной ввод: цифра встаёт в позицию курсора, курсор уходит правее. */
    FreqDial.prototype._typeDigit = function(digit) {
        const digits = formatHz(this._hz).replace(/\./g, '').split('');
        digits[this._pos] = String(digit);
        const next = parseInt(digits.join(''), 10);
        if (this._pos < DIGIT_COUNT - 1) { this._pos++; }
        this._edit(isFinite(next) ? next : this._hz);
    };

    /** Обнуляет разряд, курсор при этом не двигается. */
    FreqDial.prototype._clearDigit = function(pos) {
        const digits = formatHz(this._hz).replace(/\./g, '').split('');
        if (pos < 0 || pos >= DIGIT_COUNT || digits[pos] === '0') { return; }
        digits[pos] = '0';
        const next = parseInt(digits.join(''), 10);
        this._edit(isFinite(next) ? next : this._hz);
    };

    FreqDial.prototype._onAction = function(act, fromRepeat) {
        if (!fromRepeat && this._repeated) { return; } // click после автоповтора
        if (act === 'inc') { this._step(1); }
        else if (act === 'dec') { this._step(-1); }
        else if (act === 'left') { this._moveCursor(-1); }
        else if (act === 'right') { this._moveCursor(1); }
        else if (act === 'set') { this._apply(); }
    };

    // ── Автоповтор при удержании ─────────────────────────────────────────

    FreqDial.prototype._startRepeat = function(act) {
        const self = this;
        this._stopRepeat();
        this._repeated = false;
        this._repeatTimer = setTimeout(function() {
            self._repeated = true;
            self._repeatInterval = setInterval(function() {
                self._onAction(act, true);
            }, REPEAT_STEP_MS);
        }, REPEAT_DELAY_MS);
    };

    FreqDial.prototype._stopRepeat = function() {
        if (this._repeatTimer) { clearTimeout(this._repeatTimer); this._repeatTimer = null; }
        if (this._repeatInterval) { clearInterval(this._repeatInterval); this._repeatInterval = null; }
        // Флаг снимаем после click, который браузер шлёт следом за pointerup
        const self = this;
        if (this._repeated) { setTimeout(function() { self._repeated = false; }, 0); }
    };

    // ── Ввод ─────────────────────────────────────────────────────────────

    FreqDial.prototype._onKeyDown = function(e) {
        if (!e) { return; }
        const key = e.key;
        let handled = true;

        if (key === 'Enter') { this._apply(); }
        else if (key === 'Escape') { this._cancel(); }
        else if (key === 'ArrowUp') { this._step(1); }
        else if (key === 'ArrowDown') { this._step(-1); }
        else if (key === 'ArrowLeft') { this._moveCursor(-1); }
        else if (key === 'ArrowRight') { this._moveCursor(1); }
        else if (key === 'Home') { this._placeCursor(0); }
        else if (key === 'End') { this._placeCursor(DIGIT_COUNT - 1); }
        else if (key === 'Delete') { this._clearDigit(this._pos); }
        else if (key === 'Backspace') {
            if (this._pos > 0) { this._placeCursor(this._pos - 1); this._clearDigit(this._pos); }
        } else if (typeof key === 'string' && key.length === 1 && key >= '0' && key <= '9') {
            this._typeDigit(key);
        } else {
            handled = false; // Tab и прочее отдаём браузеру
        }

        if (handled && typeof e.preventDefault === 'function') { e.preventDefault(); }
    };

    /** Возвращает позицию разряда по элементу события или null. */
    FreqDial.prototype._posFromEvent = function(e) {
        const target = e ? e.target : null;
        if (!target || typeof target.getAttribute !== 'function') { return null; }
        const raw = target.getAttribute('data-pos');
        if (raw === null) { return null; }
        const pos = parseInt(raw, 10);
        return isFinite(pos) ? clamp(pos, 0, DIGIT_COUNT - 1) : null;
    };

    FreqDial.prototype._onDigitClick = function(e) {
        const pos = this._posFromEvent(e);
        if (pos === null) { return; }
        this._placeCursor(pos);
        if (this._strip && typeof this._strip.focus === 'function') { this._strip.focus(); }
    };

    FreqDial.prototype._onWheel = function(e) {
        if (!e || typeof e.deltaY !== 'number' || e.deltaY === 0) { return; }
        if (typeof e.preventDefault === 'function') { e.preventDefault(); }
        const pos = this._posFromEvent(e);
        if (pos !== null && pos !== this._pos) { this._pos = pos; }
        this._step(e.deltaY < 0 ? 1 : -1);
    };

    // ── Публичный API ────────────────────────────────────────────────────

    /**
     * Программная установка частоты — onChange не вызывается. Приёмник уже
     * перестроен извне, поэтому значение сразу считается установленным, а
     * незавершённый набор оператора сбрасывается.
     */
    FreqDial.prototype.setHz = function(hz) {
        if (typeof hz !== 'number' || !isFinite(hz)) { return; }
        this._hz = clamp(Math.round(hz), this._minHz, this._maxHz);
        this._appliedHz = this._hz;
        this._editing = false;
        this._render();
    };

    /** Программная установка частоты в МГц — onChange не вызывается. */
    FreqDial.prototype.setMHz = function(mhz) {
        if (typeof mhz !== 'number' || !isFinite(mhz)) { return; }
        this.setHz(mhz * 1e6);
    };

    FreqDial.prototype.getHz = function() {
        return this._hz;
    };

    FreqDial.prototype.getMHz = function() {
        return this._hz / 1e6;
    };

    /** Текущее значение как на индикаторе: '00.437.800.000'. */
    FreqDial.prototype.getText = function() {
        return formatHz(this._hz);
    };

    FreqDial.prototype.getCursor = function() {
        return this._pos;
    };

    FreqDial.prototype.setCursor = function(pos) {
        if (typeof pos !== 'number' || !isFinite(pos)) { return; }
        this._pos = clamp(Math.round(pos), 0, DIGIT_COUNT - 1);
        this._render();
    };

    FreqDial.prototype.destroy = function() {
        this._stopRepeat();
        this._repeated = false;
        for (let i = 0; i < this._handlers.length; i++) {
            const h = this._handlers[i];
            if (h[0] && typeof h[0].removeEventListener === 'function') {
                h[0].removeEventListener(h[1], h[2]);
            }
        }
        this._handlers = [];
        this._digitEls = [];
        this._strip = null;
        this._onChange = null;
        this._root = null;
    };

    FreqDial.DIGIT_COUNT = DIGIT_COUNT;
    FreqDial.MAX_HZ = DEFAULT_MAX_HZ;
    FreqDial.formatHz = formatHz;
    FreqDial.DIGIT_SEGMENTS = DIGIT_SEGMENTS;

    if (typeof window !== 'undefined') {
        window.FreqDial = FreqDial;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { FreqDial: FreqDial, formatHz: formatHz }; // eslint-disable-line no-undef
    }
})();
