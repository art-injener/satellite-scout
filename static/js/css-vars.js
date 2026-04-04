// Глобальная утилита для чтения CSS-переменных из :root.
// Используется canvas-компонентами (EarthView, SkyView, Az/El, спектр),
// которые не могут обращаться к CSS-переменным напрямую.
;(function() {
    'use strict';

    var _style = null;

    /**
     * Возвращает значение CSS-переменной из :root.
     * @param {string} name  — имя переменной (например '--bg-primary')
     * @param {string} [fallback] — значение по умолчанию, если переменная не задана
     * @returns {string}
     */
    window.cssVar = function cssVar(name, fallback) {
        try {
            if (!_style) {
                _style = getComputedStyle(document.documentElement);
            }
            var v = _style.getPropertyValue(name).trim();
            return v || fallback || '';
        } catch (_) {
            return fallback || '';
        }
    };

    /**
     * Сброс кеша стиля — вызывать при смене темы на лету.
     */
    window.cssVarReset = function cssVarReset() {
        _style = null;
    };
})();
