// mode-bar.js — связка ModeManager с шапкой и селектором тракта в колонке «Приёмник».
// Заполняет оба dropdown радиотрактов, переключатель режима (toggle), проксирует в ModeManager.
// Секция «Тракт» шапки скрыта: выбор в колонке «Приёмник» на «SDR».

'use strict';

(function() {
    /**
     * @typedef {{getMode:Function,setMode:Function,getRadioPath:Function,getRadioPathId:Function,setRadioPath:Function,getRadioPaths:Function,availableModes:Function,isBasic:Function,onModeChange:Function,onRadioPathChange:Function}} ModeManagerLike
     */

    /**
     * Привязать готовый ModeManager к разметке app-header в base.html.
     * @param {ModeManagerLike} manager
     * @returns {{destroy: () => void} | null}
     */
    function attachModeBar(manager) {
        const header = document.getElementById('app-header');
        if (!header || !manager) {
            return null;
        }

        const modeButtons = Array.from(header.querySelectorAll('.app-header__mode-toggle__btn'));
        const pathSection = header.querySelector('.app-header__path');
        const dropdowns = [
            header.querySelector('#app-header-path'),
            document.getElementById('manual-rx-path'),
        ].filter(Boolean);
        const isBasic = manager.isBasic();

        // Basic-станция: все кнопки disabled, dropdown с placeholder.
        const available = isBasic ? new Set() : new Set(manager.availableModes());
        for (const btn of modeButtons) {
            const mode = btn.dataset.mode;
            const enabled = !isBasic && available.has(mode);
            btn.disabled = !enabled;
            btn.setAttribute('aria-disabled', enabled ? 'false' : 'true');
        }

        for (const dropdown of dropdowns) {
            fillPathDropdown(dropdown, manager);
        }

        /** Визуальное состояние toggle: одна кнопка «включена» (--on + aria-pressed). */
        function reflectActiveMode(mode) {
            for (const btn of modeButtons) {
                const isOn = btn.dataset.mode === mode;
                btn.classList.toggle('app-header__mode-toggle__btn--on', isOn);
                btn.setAttribute('aria-pressed', isOn ? 'true' : 'false');
            }
            // Тракт в шапке спрятан в обоих режимах. Выбор остаётся в колонке «Приёмник».
            if (pathSection) {
                pathSection.hidden = true;
            }
        }
        reflectActiveMode(manager.getMode());

        const onModeClick = function(ev) {
            const btn = ev.currentTarget;
            if (btn.disabled) {
                return;
            }
            const mode = btn.dataset.mode;
            const previous = manager.getMode();
            reflectActiveMode(mode);
            if (!manager.setMode(mode)) {
                reflectActiveMode(previous);
            }
        };

        for (const btn of modeButtons) {
            btn.addEventListener('click', onModeClick);
        }

        const onDropdownChange = function(ev) {
            const source = ev && ev.currentTarget ? ev.currentTarget : null;
            if (!source) {
                return;
            }
            const id = Number(source.value);
            if (!Number.isFinite(id) || manager.setRadioPath(id)) {
                return;
            }
            const current = manager.getRadioPath();
            if (current && current.id != null) {
                source.value = String(current.id);
            }
        };
        for (const dropdown of dropdowns) {
            dropdown.addEventListener('change', onDropdownChange);
        }

        const offMode = manager.onModeChange(function(mode) {
            reflectActiveMode(mode);
        });
        const offPath = manager.onRadioPathChange(function(rp) {
            if (!rp || rp.id == null) {
                return;
            }
            const id = String(rp.id);
            for (const dropdown of dropdowns) {
                dropdown.value = id;
                markDropdownTitle(dropdown);
            }
        });

        return {
            destroy: function() {
                for (const btn of modeButtons) {
                    btn.removeEventListener('click', onModeClick);
                }
                for (const dropdown of dropdowns) {
                    dropdown.removeEventListener('change', onDropdownChange);
                }
                if (typeof offMode === 'function') {
                    offMode();
                }
                if (typeof offPath === 'function') {
                    offPath();
                }
            },
        };
    }

    /** Заполняет селектор тракта списком станции и текущим выбором. */
    function fillPathDropdown(dropdown, manager) {
        dropdown.innerHTML = '';
        const paths = manager.getRadioPaths();
        if (paths.length === 0) {
            const opt = document.createElement('option');
            opt.value = '';
            opt.textContent = '— не задан —';
            dropdown.appendChild(opt);
            dropdown.disabled = true;
            return;
        }
        dropdown.disabled = false;
        for (const rp of paths) {
            const opt = document.createElement('option');
            opt.value = String(rp.id);
            opt.textContent = formatPathLabel(rp);
            dropdown.appendChild(opt);
        }
        const currentId = manager.getRadioPathId();
        if (currentId != null) {
            dropdown.value = String(currentId);
        }
        markDropdownTitle(dropdown);
    }

    /** Полное имя тракта во всплывающей подсказке: в колонке подпись может не влезть. */
    function markDropdownTitle(dropdown) {
        if (!dropdown || !dropdown.options) {
            return;
        }
        const idx = dropdown.selectedIndex;
        const opt = typeof idx === 'number' ? dropdown.options[idx] : null;
        dropdown.title = opt && opt.textContent ? opt.textContent : '';
    }

    /** Подпись опции в дропдауне: "1. VHF Обзорный" или "2. UHF Поворотный (R)". */
    function formatPathLabel(rp) {
        const base = String(rp.id) + '. ' + (rp.name || ('Тракт ' + rp.id));
        return rp.has_rotator ? base + ' (R)' : base;
    }

    if (typeof window !== 'undefined') {
        window.attachModeBar = attachModeBar;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { attachModeBar, formatPathLabel }; // eslint-disable-line no-undef
    }
})();
