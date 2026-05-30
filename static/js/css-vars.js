// Глобальная утилита для чтения CSS-переменных из :root.
// Используется canvas-компонентами (EarthView, SkyView, Az/El, спектр),
// которые не могут обращаться к CSS-переменным напрямую.
//
// Для rgba-значений CSS custom properties ненадёжны (getComputedStyle
// возвращает пустую строку для rgba-записей). Поэтому все rgba-цвета
// хранятся в JS-карте _THEME_RGBA, а доступ к ним — через themeRgba().
(function() {
    'use strict';

    let _style = null;
    let _themeId = null;

    // ── rgba-цвета, сгруппированные по теме ──────────────────────
    const _THEME_RGBA = {
        'classic': {
            // earthview
            'map-footprint':            'rgba(0, 255, 255, 0.6)',
            'map-footprint-fill':       'rgba(0, 255, 255, 0.12)',
            'map-selected-footprint':      'rgba(93, 173, 226, 0.6)',
            'map-selected-footprint-fill': 'rgba(93, 173, 226, 0.17)',
            'map-observer-label-stroke': 'rgba(0,0,0,0.9)',
            'map-observer-label-bg':     'rgba(0,0,0,0.6)',
            'map-sat-label-stroke':      'rgba(0,0,0,0.85)',
            'map-sat-label-bg':          'rgba(0,0,0,0.6)',
            // skyview
            'sky-satellite-glow':   'rgba(0, 255, 255, 0.3)',
            'sky-satellite-signal': 'rgba(0, 255, 200, 0.5)',
            'sky-satellite-aura':   'rgba(197, 88, 88, 1)',
            // azimuth / elevation
            'ind-satellite-line': 'rgba(255, 255, 255, 0.5)',
            'ind-out-of-view':    'rgba(255, 107, 107, 0.7)'
        },
        'ops-center': {
            // earthview
            'map-footprint':            'rgba(255, 100, 50, 0.85)',
            'map-footprint-fill':       'rgba(255, 100, 50, 0.13)',
            'map-selected-footprint':      'rgba(240, 240, 250, 0.70)',
            'map-selected-footprint-fill': 'rgba(240, 240, 250, 0.10)',
            'map-observer-label-stroke': 'rgba(0,0,0,0.9)',
            'map-observer-label-bg':     'rgba(220, 220, 228, 0.92)',
            'map-sat-label-stroke':      'rgba(0,0,0,0.85)',
            'map-sat-label-bg':          'rgba(220, 220, 228, 0.92)',
            // skyview
            'sky-satellite-glow':   'rgba(122, 184, 208, 0.25)',
            'sky-satellite-signal': 'rgba(80, 184, 104, 0.3)',
            'sky-satellite-aura':   'rgba(208, 85, 69, 0.7)',
            // azimuth / elevation
            'ind-satellite-line': 'rgba(200, 208, 216, 0.35)',
            'ind-out-of-view':    'rgba(208, 85, 69, 0.5)'
        },
        // Breeze Dark (оригинальный KDE Breeze Dark, почти чёрный)
        'breeze-dark': {
            'map-footprint':            'rgba(61, 174, 233, 0.80)',
            'map-footprint-fill':       'rgba(61, 174, 233, 0.16)',
            'map-selected-footprint':      'rgba(61, 174, 233, 0.60)',
            'map-selected-footprint-fill': 'rgba(61, 174, 233, 0.18)',
            'map-observer-label-stroke': 'rgba(0,0,0,0.9)',
            'map-observer-label-bg':     'rgba(0,0,0,0.6)',
            'map-sat-label-stroke':      'rgba(0,0,0,0.85)',
            'map-sat-label-bg':          'rgba(0,0,0,0.6)',
            'sky-satellite-glow':   'rgba(61, 174, 233, 0.25)',
            'sky-satellite-signal': 'rgba(39, 174, 96, 0.35)',
            'sky-satellite-aura':   'rgba(218, 68, 83, 0.70)',
            'ind-satellite-line': 'rgba(228, 230, 232, 0.35)',
            'ind-out-of-view':    'rgba(218, 68, 83, 0.50)'
        },
        // Breeze (нейтральный серый, светлее dark): акценты KDE Breeze
        'breeze': {
            'map-footprint':            'rgba(61, 174, 233, 0.80)',
            'map-footprint-fill':       'rgba(61, 174, 233, 0.16)',
            'map-selected-footprint':      'rgba(61, 174, 233, 0.60)',
            'map-selected-footprint-fill': 'rgba(61, 174, 233, 0.18)',
            'map-observer-label-stroke': 'rgba(0,0,0,0.9)',
            'map-observer-label-bg':     'rgba(0,0,0,0.6)',
            'map-sat-label-stroke':      'rgba(0,0,0,0.85)',
            'map-sat-label-bg':          'rgba(0,0,0,0.6)',
            'sky-satellite-glow':   'rgba(61, 174, 233, 0.25)',
            'sky-satellite-signal': 'rgba(39, 174, 96, 0.35)',
            'sky-satellite-aura':   'rgba(218, 68, 83, 0.70)',
            'ind-satellite-line': 'rgba(228, 230, 232, 0.35)',
            'ind-out-of-view':    'rgba(218, 68, 83, 0.50)'
        },
        // Breeze-Steel (угольный со стальным подтоном)
        'breeze-steel': {
            'map-footprint':            'rgba(61, 174, 233, 0.80)',
            'map-footprint-fill':       'rgba(61, 174, 233, 0.16)',
            'map-selected-footprint':      'rgba(61, 174, 233, 0.60)',
            'map-selected-footprint-fill': 'rgba(61, 174, 233, 0.18)',
            'map-observer-label-stroke': 'rgba(0,0,0,0.9)',
            'map-observer-label-bg':     'rgba(0,0,0,0.6)',
            'map-sat-label-stroke':      'rgba(0,0,0,0.85)',
            'map-sat-label-bg':          'rgba(0,0,0,0.6)',
            'sky-satellite-glow':   'rgba(61, 174, 233, 0.25)',
            'sky-satellite-signal': 'rgba(39, 174, 96, 0.35)',
            'sky-satellite-aura':   'rgba(218, 68, 83, 0.70)',
            'ind-satellite-line': 'rgba(224, 228, 232, 0.35)',
            'ind-out-of-view':    'rgba(218, 68, 83, 0.50)'
        },
        // Breeze Light (оригинальный KDE Breeze, светлый фон #eff0f1)
        'breeze-light': {
            'map-footprint':            'rgba(218, 68, 83, 0.85)',
            'map-footprint-fill':       'rgba(218, 68, 83, 0.19)',
            'map-selected-footprint':      'rgba(41, 128, 185, 0.65)',
            'map-selected-footprint-fill': 'rgba(41, 128, 185, 0.22)',
            'map-observer-label-stroke': 'rgba(239, 240, 241, 0.95)',
            'map-observer-label-bg':     'rgba(239, 240, 241, 0.92)',
            'map-sat-label-stroke':      'rgba(239, 240, 241, 0.95)',
            'map-sat-label-bg':          'rgba(239, 240, 241, 0.90)',
            'sky-satellite-glow':   'rgba(41, 128, 185, 0.28)',
            'sky-satellite-signal': 'rgba(39, 174, 96, 0.35)',
            'sky-satellite-aura':   'rgba(218, 68, 83, 0.55)',
            'ind-satellite-line': 'rgba(65, 75, 90, 0.40)',
            'ind-out-of-view':    'rgba(218, 68, 83, 0.50)'
        },
        // STSPLUS: ретро-палитра, footprint серый (как в оригинале), яркие DOS-акценты
        'stsplus': {
            'map-footprint':            'rgba(180, 180, 180, 0.60)',
            'map-footprint-fill':       'rgba(180, 180, 180, 0.08)',
            'map-selected-footprint':      'rgba(0, 255, 255, 0.55)',
            'map-selected-footprint-fill': 'rgba(0, 255, 255, 0.12)',
            'map-observer-label-stroke': 'rgba(0,0,16,0.92)',
            'map-observer-label-bg':     'rgba(0,0,16,0.70)',
            'map-sat-label-stroke':      'rgba(0,0,16,0.90)',
            'map-sat-label-bg':          'rgba(0,0,16,0.70)',
            'sky-satellite-glow':   'rgba(0, 255, 255, 0.35)',
            'sky-satellite-signal': 'rgba(0, 255, 200, 0.5)',
            'sky-satellite-aura':   'rgba(255, 68, 68, 0.80)',
            'ind-satellite-line': 'rgba(255, 255, 255, 0.5)',
            'ind-out-of-view':    'rgba(255, 68, 68, 0.7)'
        },
        // Светлая тема: значения согласованы с static/css/colors-light.css (themeRgba для canvas)
        'light': {
            'map-footprint':            'rgba(175, 48, 32, 0.88)',
            'map-footprint-fill':       'rgba(175, 48, 32, 0.23)',
            'map-selected-footprint':      'rgba(26, 114, 184, 0.68)',
            'map-selected-footprint-fill': 'rgba(26, 114, 184, 0.26)',
            'map-observer-label-stroke': 'rgba(255, 255, 255, 0.92)',
            'map-observer-label-bg':     'rgba(204, 208, 216, 0.92)',
            'map-sat-label-stroke':      'rgba(214, 218, 225, 0.95)',
            'map-sat-label-bg':          'rgba(204, 208, 216, 0.88)',
            'sky-satellite-glow':   'rgba(8, 80, 106, 0.30)',
            'sky-satellite-signal': 'rgba(14, 122, 40, 0.35)',
            'sky-satellite-aura':   'rgba(176, 24, 24, 0.55)',
            'ind-satellite-line': 'rgba(63, 74, 90, 0.45)',
            'ind-out-of-view':    'rgba(200, 50, 50, 0.5)'
        }
    };

    /**
     * Возвращает значение CSS-переменной из :root.
     * @param {string} name  — имя переменной (например '--bg-primary')
     * @param {string} [fallback] — значение по умолчанию
     * @returns {string}
     */
    window.cssVar = function cssVar(name, fallback) {
        try {
            if (!_style) {
                _style = getComputedStyle(document.documentElement);
            }
            const v = _style.getPropertyValue(name).trim();
            return v || fallback || '';
        } catch (_) {
            return fallback || '';
        }
    };

    /**
     * Возвращает текущий идентификатор темы (кешируется).
     * @returns {string} значение --theme-id из CSS (classic, ops-center, light, breeze, breeze-steel, breeze-dark, breeze-light, …)
     */
    window.getThemeId = function getThemeId() {
        if (!_themeId) {
            _themeId = cssVar('--theme-id', 'classic');
        }
        return _themeId;
    };

    /**
     * Возвращает rgba-цвет из JS-карты по ключу и текущей теме.
     * @param {string} key — ключ из _THEME_RGBA (например 'map-footprint')
     * @param {string} [fallback] — значение если ключ не найден
     * @returns {string}
     */
    window.themeRgba = function themeRgba(key, fallback) {
        const id = getThemeId();
        const palette = _THEME_RGBA[id] || _THEME_RGBA['classic'];
        return palette[key] || fallback || '';
    };

    /**
     * Сброс кеша стиля и темы — вызывать при смене темы на лету.
     */
    window.cssVarReset = function cssVarReset() {
        _style = null;
        _themeId = null;
    };
})();
