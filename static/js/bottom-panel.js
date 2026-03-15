// Нижняя панель: переключение вкладок (Антенна / Спектр / ТМИ)
// + компактный водопад-дисплей на вкладке «Антенна».

(function() {
    'use strict';

    // ────────────────────────────────────────────────────────────────────────────
    // WaterfallCompact — анимированный водопад (hot colormap, scrolling rows)
    // ────────────────────────────────────────────────────────────────────────────

    // Параметры спектра: центр и ширина в МГц (для шкалы и подписей)
    var DEFAULT_FREQ_CENTER_MHZ = 435.625;
    var DEFAULT_FREQ_SPAN_MHZ = 2; // полоса 2 МГц

    function WaterfallCompact(canvas, scaleCanvas) {
        this._canvas = canvas;
        this._scaleCanvas = scaleCanvas || null;
        this._ctx = canvas.getContext('2d');
        this._imageData = null;
        this._timer = null;
        this._running = false;
        this._phase = 0; // для медленного «дрейфа» симулированного сигнала
        this._freqCenterMHz = DEFAULT_FREQ_CENTER_MHZ;
        this._freqSpanMHz = DEFAULT_FREQ_SPAN_MHZ;
    }

    // Горячая цветовая палитра: 0 = чёрный → 0.5 = зелёный → 1 = белый
    WaterfallCompact.prototype._hotColor = function(v) {
        v = Math.max(0, Math.min(1, v));
        var r, g, b;
        if (v < 0.2) {
            r = 0; g = 0; b = Math.round(v / 0.2 * 180);
        } else if (v < 0.4) {
            var t = (v - 0.2) / 0.2;
            r = 0; g = Math.round(t * 255); b = Math.round((1 - t) * 180);
        } else if (v < 0.6) {
            var t = (v - 0.4) / 0.2;
            r = Math.round(t * 255); g = 255; b = 0;
        } else if (v < 0.8) {
            var t = (v - 0.6) / 0.2;
            r = 255; g = Math.round((1 - t) * 255); b = 0;
        } else {
            var t = (v - 0.8) / 0.2;
            r = 255; g = Math.round(t * 255); b = Math.round(t * 255);
        }
        return [r, g, b];
    };

    // Подбор размеров canvas под текущий контейнер (учитываем шкалу частот 18px).
    // Используем getBoundingClientRect контейнера и canvas: у flex-контейнера размеры могут быть 0 до завершения layout.
    // Минимальные размеры задаём всегда, чтобы водопад и шкала не оставались пустыми.
    WaterfallCompact.prototype._resize = function() {
        var container = this._canvas.parentElement;
        if (!container) { return; }
        var headerH = 20;
        var scaleH = 18;
        var containerRect = container.getBoundingClientRect();
        var canvasRect = this._canvas.getBoundingClientRect();
        var w = Math.floor(containerRect.width) || Math.floor(canvasRect.width) || container.clientWidth || 0;
        var totalH = Math.floor(containerRect.height) || Math.floor(canvasRect.height) || container.clientHeight || 0;
        var availableH = totalH - headerH - scaleH;
        var h = Math.max(60, availableH);
        // Если контейнер ещё без размера (layout не готов) — используем размеры canvas или минимум для отрисовки
        if (w < 10) {
            w = Math.floor(canvasRect.width) || this._canvas.offsetWidth || 300;
        }
        if (h < 10) {
            h = Math.floor(canvasRect.height) || this._canvas.offsetHeight || 150;
        }
        if (w >= 10) {
            this._drawFreqScale(w);
        }
        if (w < 2 || h < 2) { return; }
        if (this._canvas.width !== w || this._canvas.height !== h) {
            this._canvas.width = w;
            this._canvas.height = h;
            this._imageData = null; // сбрасываем буфер при смене размера
        }
    };

    /**
     * Расчёт шага шкалы частот (МГц) по ширине полосы и ширине области отрисовки.
     * Формула: maxTicks = floor(widthPx / minSpacingPx), stepMHz = spanMHz / maxTicks,
     * затем step округляется до «красивого» значения, чтобы метки не сливались.
     * @param {number} spanMHz - ширина полосы (МГц)
     * @param {number} widthPx - ширина области отрисовки (пиксели)
     * @param {number} minSpacingPx - минимальный интервал между центрами меток (пиксели)
     * @returns {number} шаг по частоте (МГц)
     */
    function calcFreqScaleStep(spanMHz, widthPx, minSpacingPx) {
        minSpacingPx = minSpacingPx || 52;
        var maxTicks = Math.max(2, Math.floor(widthPx / minSpacingPx));
        var idealStep = spanMHz / maxTicks;
        var niceSteps = [0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.25, 0.5, 1, 2];
        for (var i = 0; i < niceSteps.length; i++) {
            if (niceSteps[i] >= idealStep) { return niceSteps[i]; }
        }
        return niceSteps[niceSteps.length - 1];
    }

    // Отрисовка шкалы частот (МГц) над водопадом
    WaterfallCompact.prototype._drawFreqScale = function(width) {
        var scaleCanvas = this._scaleCanvas;
        if (!scaleCanvas || !width) { return; }
        var ctx = scaleCanvas.getContext('2d');
        var freqMin = this._freqCenterMHz - this._freqSpanMHz / 2;
        var freqMax = this._freqCenterMHz + this._freqSpanMHz / 2;
        var stepMHz = calcFreqScaleStep(this._freqSpanMHz, width, 52);
        if (scaleCanvas.width !== width || scaleCanvas.height !== 18) {
            scaleCanvas.width = width;
            scaleCanvas.height = 18;
        }
        var bg = '#1a1e24';
        var fg = '#8a9199';
        try {
            var root = document.documentElement;
            bg = getComputedStyle(root).getPropertyValue('--bg-tertiary').trim() || bg;
            fg = getComputedStyle(root).getPropertyValue('--text-muted').trim() || fg;
        } catch (e) {}
        ctx.fillStyle = bg;
        ctx.fillRect(0, 0, width, 18);
        ctx.strokeStyle = fg;
        ctx.fillStyle = fg;
        ctx.font = '10px monospace';
        ctx.textAlign = 'center';
        ctx.textBaseline = 'top';
        var decimals = stepMHz >= 1 ? 1 : (stepMHz >= 0.1 ? 2 : 3);
        for (var f = Math.ceil(freqMin / stepMHz) * stepMHz; f <= freqMax; f += stepMHz) {
            var x = ((f - freqMin) / (freqMax - freqMin)) * width;
            ctx.beginPath();
            ctx.moveTo(x, 0);
            ctx.lineTo(x, 6);
            ctx.stroke();
            ctx.fillText(f.toFixed(decimals), x, 8);
        }
        // Обновляем подписи в шапке
        var freqEl = document.getElementById('wf-freq');
        var resEl = document.getElementById('wf-res');
        if (freqEl) { freqEl.textContent = freqMin.toFixed(3) + ' – ' + freqMax.toFixed(3) + ' MHz'; }
        if (resEl && width > 0) {
            var spanHz = this._freqSpanMHz * 1e6;
            var resHz = Math.round(spanHz / width);
            resEl.textContent = resHz + ' Hz/pix';
        }
    };

    WaterfallCompact.prototype._tick = function() {
        this._resize();

        var w = this._canvas.width;
        var h = this._canvas.height;
        if (w < 2 || h < 2) { return; }

        // Ленивая инициализация imageData
        if (!this._imageData || this._imageData.width !== w || this._imageData.height !== h) {
            this._imageData = this._ctx.createImageData(w, h);
            // Заполняем чёрным
            var d = this._imageData.data;
            for (var i = 3; i < d.length; i += 4) { d[i] = 255; }
        }

        var data = this._imageData.data;

        // Прокрутка вниз на 1 строку — новые данные сверху, водопад «опускается»
        data.copyWithin(w * 4, 0, (h - 1) * w * 4);

        // Генерация новой строки сверху (offset 0)
        var offset = 0;
        this._phase += 0.015;

        // Центр симулированного сигнала — медленно дрейфует (Допплер-эффект)
        var signalCenter = 0.5 + 0.06 * Math.sin(this._phase);
        var signalWidth = 0.03;

        for (var x = 0; x < w; x++) {
            var fx = x / w; // нормированная частота [0..1]

            // Шум
            var noise = 0.04 + Math.random() * 0.06;

            // Симулированный сигнал (гауссов пик)
            var dist = (fx - signalCenter) / signalWidth;
            var signal = 0.75 * Math.exp(-0.5 * dist * dist) * (0.85 + Math.random() * 0.3);

            var v = noise + signal;
            var col = this._hotColor(v);

            var i = offset + x * 4;
            data[i]     = col[0];
            data[i + 1] = col[1];
            data[i + 2] = col[2];
            data[i + 3] = 255;
        }

        this._ctx.putImageData(this._imageData, 0, 0);
    };

    // Принудительное обновление размеров и шкалы (после смены вкладки или разворота панели).
    WaterfallCompact.prototype.refresh = function() {
        this._resize();
        if (this._running) {
            this._tick();
        }
    };

    WaterfallCompact.prototype.start = function() {
        if (this._running) { return; }
        this._running = true;
        this._resize();
        var self = this;
        this._timer = setInterval(function() { self._tick(); }, 80);
        // Отложенные refresh после layout (flex/grid может посчитаться с задержкой).
        function scheduleRefresh() {
            self.refresh();
        }
        requestAnimationFrame(function() {
            requestAnimationFrame(scheduleRefresh);
        });
        setTimeout(scheduleRefresh, 50);
        setTimeout(scheduleRefresh, 200);
    };

    WaterfallCompact.prototype.stop = function() {
        this._running = false;
        if (this._timer) {
            clearInterval(this._timer);
            this._timer = null;
        }
    };

    // ────────────────────────────────────────────────────────────────────────────
    // BottomPanel — переключение вкладок + инициализация содержимого
    // ────────────────────────────────────────────────────────────────────────────

    function BottomPanel() {
        this._panes = {};
        this._waterfall = null;
        this._currentTab = localStorage.getItem('ux.bottomTab') || 'antenna';
        this._resizeBound = null;

        this._collectPanes();
        this._initTabs();
        this._initSDRForm();
        this._initTMIExport();
        this._switchTab(this._currentTab, false);
        this._startWaterfall();
        this._bindResize();
    }

    // Собираем ссылки на pane-контейнеры
    BottomPanel.prototype._collectPanes = function() {
        var els = document.querySelectorAll('.bp-pane');
        for (var i = 0; i < els.length; i++) {
            var id = els[i].id.replace('bp-pane-', '');
            this._panes[id] = els[i];
        }
    };

    // Привязываем клики по кнопкам аккордеона в sidebar (иконки Антенна / Спектр / ТМИ)
    BottomPanel.prototype._initTabs = function() {
        var self = this;
        var tabs = document.querySelectorAll('.sidebar-accordion__btn');
        for (var i = 0; i < tabs.length; i++) {
            (function(tab) {
                tab.addEventListener('click', function() {
                    self._switchTab(tab.getAttribute('data-tab'), true);
                });
            })(tabs[i]);
        }
    };

    // Подпись текущего режима для заголовка нижней панели
    var TAB_LABELS = { antenna: 'Антенна', spectrum: 'Спектр', tmi: 'ТМИ' };

    // Переключение активной вкладки
    BottomPanel.prototype._switchTab = function(name, save) {
        // Кнопки аккордеона в sidebar
        var tabs = document.querySelectorAll('.sidebar-accordion__btn');
        for (var i = 0; i < tabs.length; i++) {
            var active = tabs[i].getAttribute('data-tab') === name;
            tabs[i].classList.toggle('active', active);
        }
        // Pane-контейнеры
        for (var key in this._panes) {
            this._panes[key].classList.toggle('bp-pane--hidden', key !== name);
        }
        // Подпись в заголовке нижней панели — столбиком (по одной букве)
        var modeEl = document.getElementById('bottom-panel-mode');
        if (modeEl) {
            var label = TAB_LABELS[name] || name;
            modeEl.innerHTML = label.split('').map(function(c) {
                return '<span>' + (c === ' ' ? '\u00A0' : c) + '</span>';
            }).join('');
        }
        this._currentTab = name;
        if (save) {
            localStorage.setItem('ux.bottomTab', name);
        }
        // После переключения на Антенну — принудительное обновление водопада и шкалы (layout уже обновлён)
        if (name === 'antenna' && this._waterfall) {
            var self = this;
            requestAnimationFrame(function() {
                requestAnimationFrame(function() {
                    self.refreshWaterfall();
                });
            });
        }
    };

    // Принудительное обновление водопада и шкалы (размеры + один кадр). Вызывать после разворота панели или resize.
    BottomPanel.prototype.refreshWaterfall = function() {
        if (this._waterfall) {
            this._waterfall.refresh();
        }
    };

    // Запуск водопада (вкладка Антенна) + шкала частот.
    // ResizeObserver вызывает refresh при появлении у контейнера ненулевого размера (после layout).
    BottomPanel.prototype._startWaterfall = function() {
        var canvas = document.getElementById('waterfall-compact');
        var scaleCanvas = document.getElementById('waterfall-freq-scale');
        if (!canvas) { return; }
        this._waterfall = new WaterfallCompact(canvas, scaleCanvas);
        this._waterfall.start();
        var container = canvas.parentElement;
        if (container && typeof ResizeObserver !== 'undefined') {
            var self = this;
            this._waterfallContainer = container;
            this._waterfallResizeObserver = new ResizeObserver(function() {
                if (self._currentTab === 'antenna') {
                    self.refreshWaterfall();
                }
            });
            this._waterfallResizeObserver.observe(container);
        }
    };

    // Заглушки формы SDR
    BottomPanel.prototype._initSDRForm = function() {
        var gainSlider = document.getElementById('sdr-gain');
        var gainVal = document.getElementById('sdr-gain-val');
        if (gainSlider && gainVal) {
            gainSlider.addEventListener('input', function() {
                gainVal.textContent = gainSlider.value;
            });
        }

        var startBtn = document.getElementById('sdr-start');
        if (startBtn) {
            startBtn.addEventListener('click', function() {
                console.log('[BottomPanel] TODO: POST /api/sdr/start', {
                    freq: document.getElementById('sdr-freq') && document.getElementById('sdr-freq').value,
                    gain: gainSlider && gainSlider.value,
                    bw:   document.getElementById('sdr-bw') && document.getElementById('sdr-bw').value,
                    mod:  document.getElementById('sdr-mod') && document.getElementById('sdr-mod').value,
                    baud: document.getElementById('sdr-baud') && document.getElementById('sdr-baud').value
                });
            });
        }
    };

    // Заглушки кнопок экспорта ТМИ
    BottomPanel.prototype._initTMIExport = function() {
        var csvBtn = document.getElementById('tmi-export-csv');
        var jsonBtn = document.getElementById('tmi-export-json');
        if (csvBtn) {
            csvBtn.addEventListener('click', function() {
                console.log('[BottomPanel] TODO: export TMI as CSV');
            });
        }
        if (jsonBtn) {
            jsonBtn.addEventListener('click', function() {
                console.log('[BottomPanel] TODO: export TMI as JSON');
            });
        }
    };

    BottomPanel.prototype._bindResize = function() {
        var self = this;
        this._resizeBound = function() {
            if (self._currentTab === 'antenna') {
                self.refreshWaterfall();
            }
        };
        window.addEventListener('resize', this._resizeBound);
    };

    BottomPanel.prototype.destroy = function() {
        if (this._resizeBound) {
            window.removeEventListener('resize', this._resizeBound);
            this._resizeBound = null;
        }
        if (this._waterfallResizeObserver && this._waterfallContainer) {
            this._waterfallResizeObserver.unobserve(this._waterfallContainer);
            this._waterfallResizeObserver = null;
            this._waterfallContainer = null;
        }
        if (this._waterfall) {
            this._waterfall.stop();
            this._waterfall = null;
        }
    };

    // Экспортируем
    window.BottomPanel = BottomPanel;
    window.WaterfallCompact = WaterfallCompact;

})();
