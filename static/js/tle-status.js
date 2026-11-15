// tle-status.js — индикатор актуальности TLE в шапке.
// Данные приходят SSE-событием tle_status (sse-client.js пересылает его в document).

'use strict';

(function() {
    const LEVELS = ['ok', 'warn', 'stale'];

    const SOURCE_LABELS = {
        spacetrack: 'Space-Track',
        celestrak: 'Celestrak',
        cache: 'кеш на диске',
    };

    const REASON_LABELS = {
        empty: 'TLE не загружены',
        median_age: 'Орбиты большинства КА устарели',
        iss_age: 'Орбита МКС устарела',
        network: 'Давно нет свежих TLE из сети',
        pass_drift: 'Пролёты расходятся с Heavens-Above',
    };

    /** Сдвиг пролёта: до минуты — «8 с», дальше — «1,6 мин». */
    function formatDrift(sec) {
        if (sec < 60) {
            return Math.round(sec) + ' с';
        }
        return (sec / 60).toFixed(1).replace('.', ',') + ' мин';
    }

    /** «1 пролёт», «3 пролёта», «14 пролётов». */
    function passesWord(n) {
        const mod10 = n % 10;
        const mod100 = n % 100;
        if (mod10 === 1 && mod100 !== 11) {
            return n + ' пролёт';
        }
        if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) {
            return n + ' пролёта';
        }
        return n + ' пролётов';
    }

    function describePassCheck(pc) {
        if (!pc || !pc.matched) {
            return 'Сверка с Heavens-Above не проводилась (нет сети и свежего кеша)';
        }
        let line = 'Сверка с Heavens-Above: сдвиг ' + formatDrift(pc.median_drift_s);
        if (typeof pc.iss_drift_s === 'number') {
            line += ', МКС ' + formatDrift(pc.iss_drift_s);
        }
        return line + ' (' + passesWord(pc.matched) + ')';
    }

    /** Возраст в часах: до суток — «5 ч», дальше — «1,7 сут». */
    function formatTLEAge(hours) {
        if (typeof hours !== 'number' || !Number.isFinite(hours)) {
            return '—';
        }
        if (hours < 24) {
            return Math.round(hours) + ' ч';
        }
        return (hours / 24).toFixed(1).replace('.', ',') + ' сут';
    }

    function formatFetchedAt(iso) {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) {
            return '';
        }
        return d.toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
    }

    /**
     * Текст, уровень и подсказка индикатора по payload tle_status.
     * @param {object|null} st
     * @returns {{level: string, text: string, title: string}}
     */
    const LOAD_PHASE_LABELS = {
        gp: 'орбиты (GP)',
        satcat: 'даты схода (SATCAT)',
    };

    function describeTLEStatus(st) {
        if (!st || (LEVELS.indexOf(st.level) < 0 && !st.loading)) {
            return { level: 'unknown', text: 'TLE …', title: 'Актуальность TLE: нет данных от сервера' };
        }
        if (st.loading) {
            const done = st.load_done || 0;
            const total = st.load_total || 0;
            const phase = LOAD_PHASE_LABELS[st.load_phase] || 'Space-Track';
            return {
                level: 'loading',
                text: total > 0 ? ('TLE ' + done + '/' + total + '…') : 'TLE …',
                title: 'Загрузка с Space-Track: ' + phase + ', пачка ' + done + ' из ' + total,
            };
        }
        if (!st.count) {
            return { level: st.level, text: 'TLE нет', title: REASON_LABELS.empty };
        }

        const lines = [];
        for (const reason of st.reasons || []) {
            if (REASON_LABELS[reason]) {
                lines.push('⚠ ' + REASON_LABELS[reason]);
            }
        }
        lines.push('Медиана возраста epoch: ' + formatTLEAge(st.median_age_h) + ' (' + st.count + ' КА)');
        if (typeof st.iss_age_h === 'number') {
            lines.push('МКС: ' + formatTLEAge(st.iss_age_h));
        }
        if (st.stale_count > 0) {
            lines.push('Старше порога: ' + st.stale_count + ' КА');
        }
        lines.push(describePassCheck(st.pass_check));
        if (st.source) {
            lines.push('Источник: ' + (SOURCE_LABELS[st.source] || st.source));
        }
        const fetched = st.fetched_at ? formatFetchedAt(st.fetched_at) : '';
        lines.push(fetched ? 'Загружено из сети: ' + fetched : 'Из сети ещё не загружалось');
        if (st.last_error) {
            lines.push('Ошибка сети: ' + st.last_error);
        }

        return { level: st.level, text: 'TLE ' + formatTLEAge(st.median_age_h), title: lines.join('\n') };
    }

    function render(el, label, view) {
        for (const lvl of LEVELS.concat(['unknown', 'loading'])) {
            el.classList.toggle('tle-status--' + lvl, lvl === view.level);
        }
        label.textContent = view.text;
        el.title = view.title;
    }

    function attachTLEStatus() {
        const el = document.getElementById('tle-status');
        const label = document.getElementById('tle-status-label');
        if (!el || !label) {
            return;
        }
        render(el, label, describeTLEStatus(null));
        document.addEventListener('satellite-scout-tle-status', (e) => {
            render(el, label, describeTLEStatus(e.detail));
        });
    }

    if (typeof document !== 'undefined' && typeof window !== 'undefined') {
        attachTLEStatus();
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { describeTLEStatus, formatTLEAge }; // eslint-disable-line no-undef
    }
})();
