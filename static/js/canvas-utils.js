// Canvas Utilities - Утилиты для работы с canvas
// Поддержка HiDPI/Retina дисплеев

(function() {
    'use strict';

    /**
     * Настройка canvas с учётом devicePixelRatio для четкой отрисовки
     * @param {HTMLCanvasElement} canvas - Canvas элемент
     * @param {number} width - Логическая ширина (CSS пиксели)
     * @param {number} height - Логическая высота (CSS пиксели)
     * @returns {CanvasRenderingContext2D} Контекст с применённым масштабированием
     */
    function setupHiDPICanvas(canvas, width, height) {
        const dpr = window.devicePixelRatio || 1;

        // Устанавливаем CSS размеры (видимые размеры)
        canvas.style.width = width + 'px';
        canvas.style.height = height + 'px';

        // Устанавливаем внутренние размеры с учётом DPR
        canvas.width = width * dpr;
        canvas.height = height * dpr;

        // Получаем контекст и масштабируем его
        const ctx = canvas.getContext('2d');
        ctx.scale(dpr, dpr);

        return ctx;
    }

    /**
     * Получить логические размеры canvas (без учёта DPR)
     * @param {HTMLCanvasElement} canvas
     * @returns {{width: number, height: number}}
     */
    function getLogicalSize(canvas) {
        const dpr = window.devicePixelRatio || 1;
        return {
            width: canvas.width / dpr,
            height: canvas.height / dpr
        };
    }

    /**
     * Проверка, настроен ли canvas для HiDPI
     * @param {HTMLCanvasElement} canvas
     * @returns {boolean}
     */
    function isHiDPIEnabled(canvas) {
        const dpr = window.devicePixelRatio || 1;
        const expectedWidth = parseInt(canvas.style.width, 10) * dpr;
        return Math.abs(canvas.width - expectedWidth) < 1;
    }

    // Экспорт в глобальную область
    window.CanvasUtils = {
        setupHiDPICanvas: setupHiDPICanvas,
        getLogicalSize: getLogicalSize,
        isHiDPIEnabled: isHiDPIEnabled,
        getDevicePixelRatio: function() {
            return window.devicePixelRatio || 1;
        }
    };

})();
