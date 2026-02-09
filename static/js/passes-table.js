/**
 * PassesTable — класс для отображения таблицы пролётов спутников.
 * Загружает данные с API, рендерит таблицу с SVG мини-проекциями,
 * поддерживает обратный отсчёт. Показывает пролёты ВСЕХ групп.
 */
class PassesTable {
    /**
     * @param {string} containerId — ID контейнера для таблицы.
     * @param {Object} options — опции.
     */
    constructor(containerId, options = {}) {
        this.container = document.getElementById(containerId);
        this.options = {
            apiUrl: '/api/passes',
            refreshInterval: 60000, // 60 сек
            ...options
        };
        
        this.passes = [];
        this.countdownTimer = null;
        this.refreshTimer = null;
        
        this._boundUpdateCountdowns = this._updateCountdowns.bind(this);
    }
    
    /**
     * Инициализация таблицы: загрузка данных и настройка таймеров.
     */
    async init() {
        await this.loadPasses();
        this._startCountdownTimer();
        this._startRefreshTimer();
    }
    
    /**
     * Загрузка пролётов с API (все группы).
     */
    async loadPasses() {
        this._showLoading();
        
        try {
            // Без параметра group — загружаем все группы.
            const response = await fetch(this.options.apiUrl);
            
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
            }
            
            const data = await response.json();
            this.passes = data.passes || [];
            this._render();
        } catch (error) {
            console.error('Ошибка загрузки пролётов:', error);
            this._showError('Не удалось загрузить пролёты. Попробуйте позже.');
        }
    }
    
    /**
     * Рендер таблицы пролётов.
     */
    _render() {
        if (!this.container) return;
        
        if (this.passes.length === 0) {
            this.container.innerHTML = `
                <div class="passes-empty">
                    <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                        <circle cx="12" cy="12" r="10"/>
                        <path d="M12 6v6l4 2"/>
                    </svg>
                    <p>Нет запланированных пролётов</p>
                </div>
            `;
            return;
        }
        
        const now = Date.now();
        
        const rows = this.passes.map(pass => {
            const status = this._getPassStatus(pass, now);
            const countdown = this._formatCountdown(pass.aos - now);
            const duration = this._formatDuration(pass.duration);
            const svgMiniPath = this._renderSVGMiniPath(pass.sky_path);
            
            return `
                <tr class="pass-row pass-row--${status}" 
                    data-norad-id="${pass.norad_id}"
                    data-aos="${pass.aos}">
                    <td class="pass-cell pass-cell--name">
                        <span class="sat-name">${this._escapeHtml(pass.sat_name)}</span>
                        <span class="sat-norad">${pass.norad_id}</span>
                    </td>
                    <td class="pass-cell pass-cell--orbit">${pass.orbit_number}</td>
                    <td class="pass-cell pass-cell--time">
                        <span class="time-value">${this._formatTime(pass.aos)}</span>
                        <span class="az-value">${pass.aos_az.toFixed(0)}°</span>
                    </td>
                    <td class="pass-cell pass-cell--time">
                        <span class="time-value">${this._formatTime(pass.tca)}</span>
                        <span class="el-value el-value--max">${pass.tca_el.toFixed(1)}°</span>
                    </td>
                    <td class="pass-cell pass-cell--time">
                        <span class="time-value">${this._formatTime(pass.los)}</span>
                        <span class="az-value">${pass.los_az.toFixed(0)}°</span>
                    </td>
                    <td class="pass-cell pass-cell--duration">${duration}</td>
                    <td class="pass-cell pass-cell--countdown" data-countdown="${pass.aos}">${countdown}</td>
                    <td class="pass-cell pass-cell--minipath">${svgMiniPath}</td>
                </tr>
            `;
        }).join('');
        
        this.container.innerHTML = `
            <table class="passes-table">
                <thead>
                    <tr>
                        <th>Спутник</th>
                        <th>Орбита</th>
                        <th>AOS</th>
                        <th>TCA</th>
                        <th>LOS</th>
                        <th>Длит.</th>
                        <th>До пролёта</th>
                        <th>Траектория</th>
                    </tr>
                </thead>
                <tbody>
                    ${rows}
                </tbody>
            </table>
        `;
    }
    
    /**
     * Генерация SVG мини-проекции траектории пролёта.
     * @param {Array} skyPath — массив точек {x, y} из API (предвычислены на backend).
     * @returns {string} — SVG разметка.
     */
    _renderSVGMiniPath(skyPath) {
        if (!skyPath || skyPath.length === 0) {
            return '<svg class="mini-path" viewBox="-1.2 -1.2 2.4 2.4" width="50" height="50"></svg>';
        }
        
        // Строим path из предвычисленных X/Y координат.
        // X/Y уже в диапазоне [-1, 1], где (0,0) — зенит.
        const pathPoints = skyPath.map((p, i) => {
            const cmd = i === 0 ? 'M' : 'L';
            return `${cmd}${p.x.toFixed(3)},${p.y.toFixed(3)}`;
        }).join(' ');
        
        // Начальная и конечная точки для маркеров.
        const start = skyPath[0];
        const end = skyPath[skyPath.length - 1];
        
        return `
            <svg class="mini-path" viewBox="-1.2 -1.2 2.4 2.4" width="50" height="50">
                <!-- Круг горизонта -->
                <circle cx="0" cy="0" r="1" class="horizon-circle"/>
                <!-- Перекрестие (N-S, E-W) -->
                <line x1="0" y1="-1" x2="0" y2="1" class="crosshair"/>
                <line x1="-1" y1="0" x2="1" y2="0" class="crosshair"/>
                <!-- Траектория -->
                <path d="${pathPoints}" class="sky-track"/>
                <!-- Маркеры AOS/LOS -->
                <circle cx="${start.x.toFixed(3)}" cy="${start.y.toFixed(3)}" r="0.08" class="marker-aos"/>
                <circle cx="${end.x.toFixed(3)}" cy="${end.y.toFixed(3)}" r="0.08" class="marker-los"/>
            </svg>
        `;
    }
    
    /**
     * Определение статуса пролёта.
     * @param {Object} pass — данные пролёта.
     * @param {number} now — текущее время (Unix ms).
     * @returns {string} — 'active' | 'upcoming' | 'completed'.
     */
    _getPassStatus(pass, now) {
        if (now >= pass.aos && now <= pass.los) {
            return 'active';
        } else if (now < pass.aos) {
            return 'upcoming';
        } else {
            return 'completed';
        }
    }
    
    /**
     * Форматирование времени (HH:MM:SS UTC).
     * @param {number} timestamp — Unix ms.
     * @returns {string}
     */
    _formatTime(timestamp) {
        const date = new Date(timestamp);
        return date.toISOString().substr(11, 8);
    }
    
    /**
     * Форматирование длительности.
     * @param {number} seconds — секунды.
     * @returns {string}
     */
    _formatDuration(seconds) {
        const mins = Math.floor(seconds / 60);
        const secs = Math.floor(seconds % 60);
        return `${mins}:${secs.toString().padStart(2, '0')}`;
    }
    
    /**
     * Форматирование обратного отсчёта.
     * @param {number} ms — миллисекунды до события.
     * @returns {string}
     */
    _formatCountdown(ms) {
        if (ms <= 0) {
            return '<span class="countdown-now">сейчас</span>';
        }
        
        const totalSecs = Math.floor(ms / 1000);
        const hours = Math.floor(totalSecs / 3600);
        const mins = Math.floor((totalSecs % 3600) / 60);
        const secs = totalSecs % 60;
        
        if (hours > 0) {
            return `${hours}ч ${mins}м`;
        } else if (mins > 0) {
            return `${mins}м ${secs}с`;
        } else {
            return `${secs}с`;
        }
    }
    
    /**
     * Запуск таймера обратного отсчёта (каждую секунду).
     */
    _startCountdownTimer() {
        if (this.countdownTimer) {
            clearInterval(this.countdownTimer);
        }
        this.countdownTimer = setInterval(this._boundUpdateCountdowns, 1000);
    }
    
    /**
     * Запуск таймера автообновления данных.
     */
    _startRefreshTimer() {
        if (this.refreshTimer) {
            clearInterval(this.refreshTimer);
        }
        this.refreshTimer = setInterval(() => {
            this.loadPasses();
        }, this.options.refreshInterval);
    }
    
    /**
     * Обновление всех ячеек обратного отсчёта.
     */
    _updateCountdowns() {
        const now = Date.now();
        const cells = this.container.querySelectorAll('.pass-cell--countdown[data-countdown]');
        
        cells.forEach(cell => {
            const aos = parseInt(cell.dataset.countdown, 10);
            const diff = aos - now;
            cell.innerHTML = this._formatCountdown(diff);
        });
        
        // Обновляем статусы строк.
        const rows = this.container.querySelectorAll('.pass-row');
        rows.forEach(row => {
            const noradId = parseInt(row.dataset.noradId, 10);
            const pass = this.passes.find(p => p.norad_id === noradId);
            if (pass) {
                const status = this._getPassStatus(pass, now);
                row.className = `pass-row pass-row--${status}`;
            }
        });
    }
    
    /**
     * Показ индикатора загрузки.
     */
    _showLoading() {
        if (this.container) {
            this.container.innerHTML = `
                <div class="passes-loading">
                    <div class="spinner"></div>
                    <span>Загрузка пролётов...</span>
                </div>
            `;
        }
    }
    
    /**
     * Показ ошибки.
     * @param {string} message — текст ошибки.
     */
    _showError(message) {
        if (this.container) {
            this.container.innerHTML = `
                <div class="passes-error">
                    <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                        <circle cx="12" cy="12" r="10"/>
                        <line x1="12" y1="8" x2="12" y2="12"/>
                        <line x1="12" y1="16" x2="12.01" y2="16"/>
                    </svg>
                    <p>${this._escapeHtml(message)}</p>
                    <button class="btn btn-secondary" onclick="passesTable.loadPasses()">Повторить</button>
                </div>
            `;
        }
    }
    
    /**
     * Экранирование HTML.
     * @param {string} str — строка.
     * @returns {string}
     */
    _escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }
    
    /**
     * Очистка ресурсов.
     */
    destroy() {
        if (this.countdownTimer) {
            clearInterval(this.countdownTimer);
            this.countdownTimer = null;
        }
        if (this.refreshTimer) {
            clearInterval(this.refreshTimer);
            this.refreshTimer = null;
        }
    }
}

// Глобальная переменная для доступа из HTML.
let passesTable = null;

/**
 * Инициализация таблицы пролётов.
 * Вызывается при загрузке страницы /passes.
 */
function initPassesTable() {
    // Очищаем предыдущий экземпляр.
    if (passesTable) {
        passesTable.destroy();
    }
    
    const container = document.getElementById('passes-table-container');
    if (container) {
        passesTable = new PassesTable('passes-table-container');
        passesTable.init();
    }
}
