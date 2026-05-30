// Переключение цветовой темы без перезагрузки: смена colors-*.css, cssVarReset(), перерисовка canvas.
// Выбор сохраняется в localStorage (ss-ui-theme). В <head> дублируется тот же ключ — без мигания при F5.
//
// Версию ?v= контролирует только base.html (серверный шаблон); JS берёт её из текущего href тега <link>.
(function() {
    'use strict';

    const STORAGE_KEY = 'ss-ui-theme';

    const THEMES = [
        { id: 'default', label: 'Operations Center' },
        { id: 'classic', label: 'Classic' },
        { id: 'stsplus', label: 'STSPLUS' },
        { id: 'light', label: 'Светлая' },
        { id: 'breeze-light', label: 'Breeze' },
        { id: 'breeze', label: 'Breeze (серый)' },
        { id: 'breeze-steel', label: 'Breeze Steel' },
        { id: 'breeze-dark', label: 'Breeze Dark' }
    ];

    /** Regex для замены имени темы в href: colors-XXXX.css (с сохранением ?v= и пр.) */
    const RE_THEME_IN_HREF = /(colors-)[a-z0-9-]+(\.css)/;

    function parseThemeFromHref(href) {
        const m = String(href || '').match(/colors-([a-z0-9-]+)\.css/);
        return m ? m[1] : null;
    }

    /** Построить новый href, заменив имя темы в текущем. Сохраняет ?v= и путь. */
    function replaceThemeInHref(currentHref, newId) {
        return String(currentHref).replace(RE_THEME_IN_HREF, '$1' + newId + '$2');
    }

    function isAllowed(id) {
        for (let i = 0; i < THEMES.length; i++) {
            if (THEMES[i].id === id) { return true; }
        }
        return false;
    }

    function redrawAll() {
        if (typeof window.cssVarReset === 'function') {
            window.cssVarReset();
        }
        if (window.AntennaDrawing && typeof window.AntennaDrawing.refreshFromCss === 'function') {
            window.AntennaDrawing.refreshFromCss();
        }
        if (window.earthView && typeof window.earthView.refreshThemeColors === 'function') {
            window.earthView.refreshThemeColors();
        }
        if (window.skyView && typeof window.skyView.refreshThemeColors === 'function') {
            window.skyView.refreshThemeColors();
        }
        if (window.azimuthIndicator && typeof window.azimuthIndicator.refreshThemeColors === 'function') {
            window.azimuthIndicator.refreshThemeColors();
        }
        if (window.elevationIndicator && typeof window.elevationIndicator.refreshThemeColors === 'function') {
            window.elevationIndicator.refreshThemeColors();
        }
        if (window.earthView && typeof window.earthView.draw === 'function') {
            window.earthView.draw();
        }
        if (window.skyView && typeof window.skyView.draw === 'function') {
            window.skyView.draw();
        }
        if (window.azimuthIndicator && typeof window.azimuthIndicator.draw === 'function') {
            window.azimuthIndicator.draw();
        }
        if (window.elevationIndicator && typeof window.elevationIndicator.draw === 'function') {
            window.elevationIndicator.draw();
        }
        if (window._bottomPanel && typeof window._bottomPanel.refreshAfterThemeChange === 'function') {
            window._bottomPanel.refreshAfterThemeChange();
        } else if (window._bottomPanel && typeof window._bottomPanel.refreshWaterfall === 'function') {
            window._bottomPanel.refreshWaterfall();
        }
        try {
            const tid = parseThemeFromHref(document.getElementById('theme-colorsheet').href);
            window.dispatchEvent(new CustomEvent('satwatch-theme-applied', { detail: { fileTheme: tid } }));
        } catch (_e) { /* ignore */ }
    }

    /**
     * Применить тему по имени файла (default, classic, light, breeze, …).
     * @param {string} id
     * @param {boolean} [save=true] — писать в localStorage
     */
    function applyTheme(id, save) {
        const link = document.getElementById('theme-colorsheet');
        if (!link || !isAllowed(id)) { return; }

        const currentHref = link.getAttribute('href') || '';
        const next = replaceThemeInHref(currentHref, id);
        if (currentHref === next) { return; }

        let finished = false;
        function finish() {
            if (finished) { return; }
            finished = true;
            redrawAll();
        }

        const onload = function() {
            link.removeEventListener('load', onload);
            finish();
        };
        link.addEventListener('load', onload);
        link.setAttribute('href', next);

        if (save !== false) {
            try {
                localStorage.setItem(STORAGE_KEY, id);
            } catch (_e) { /* ignore */ }
            document.cookie = 'ss-theme=' + id + '; path=/; max-age=31536000; SameSite=Lax';
        }

        window.setTimeout(function() {
            link.removeEventListener('load', onload);
            finish();
        }, 120);
    }

    function syncSelectValue(sel) {
        const link = document.getElementById('theme-colorsheet');
        if (!link || !sel) { return; }
        const cur = parseThemeFromHref(link.href);
        if (cur && isAllowed(cur)) {
            sel.value = cur;
        }
    }

    function init() {
        const sel = document.getElementById('theme-select');
        if (!sel) { return; }

        sel.innerHTML = '';
        for (let i = 0; i < THEMES.length; i++) {
            const opt = document.createElement('option');
            opt.value = THEMES[i].id;
            opt.textContent = THEMES[i].label;
            sel.appendChild(opt);
        }
        syncSelectValue(sel);

        sel.addEventListener('change', function() {
            applyTheme(sel.value, true);
        });
    }

    document.addEventListener('DOMContentLoaded', init);

    window.applySatWatchTheme = applyTheme;
})();
