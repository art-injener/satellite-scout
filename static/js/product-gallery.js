// product-gallery.js — вкладка «Снимки»: картинки, принятые демодулятором (SSTV).
//
// Список — из GET /api/products, дальше — SSE `product` (internal/products.Product):
// начало картинки, ход по строкам и итог. Проявляющаяся картинка показывается крупно,
// пока оператор не выбрал другую; миниатюры грузятся один раз, когда картинка готова.
// Щелчок по крупной картинке открывает её в окне во весь экран. Если картинку, за которой
// следит вкладка, оборвали (перестройка, пропал сигнал), крупный кадр очищается до следующей.

'use strict';

(function() {

    const MAX_ITEMS = 50;

    function pad2(n) {
        return n < 10 ? '0' + n : String(n);
    }

    /** Местное время ЧЧ:ММ:СС. */
    function localTime(ms) {
        const d = new Date(ms);
        return pad2(d.getHours()) + ':' + pad2(d.getMinutes()) + ':' + pad2(d.getSeconds());
    }

    /** Подпись картинки: режим, время начала и частота канала, если известна. */
    function productCaption(p, formatTime) {
        const parts = [p.mode_name || p.mode, formatTime(p.started_ms)];
        if (p.frequency_hz) {
            parts.push((p.frequency_hz / 1e6).toFixed(3).replace('.', ',') + ' МГц');
        }
        return parts.join(' · ');
    }

    /** Ход картинки: строки в приёме, «готово» или где приём оборвался. */
    function progressText(p) {
        if (!p.done) { return p.lines + ' / ' + p.height + ' строк'; }
        if (p.lines >= p.height) { return 'готово'; }
        return 'оборвана на ' + p.lines + ' из ' + p.height;
    }

    function imageURL(p) {
        return '/api/products/' + encodeURIComponent(p.id) + '?l=' + p.lines;
    }

    /**
     * @param {Object} [opts]
     * @param {function(string): Promise<Array>} [opts.fetchJSON] — запрос списка (подменяется в тестах)
     * @param {function(number): string} [opts.formatTime] — время для подписей
     */
    function ProductGallery(opts) {
        opts = opts || {};
        this._fetchJSON = opts.fetchJSON || function(url) {
            return fetch(url).then(function(r) {
                if (!r.ok) { throw new Error('HTTP ' + r.status); }
                return r.json();
            });
        };
        this._formatTime = opts.formatTime || localTime;
        this._items = []; // от новых к старым
        this._nodes = new Map(); // id: элементы миниатюры
        this._selected = null;
        this._pinned = false; // оператор выбрал не самую новую картинку
        this._cut = null; // оборванная картинка, вместо которой показывается ожидание следующей
        this._shownURL = '';
        this._zoomURL = '';
        this._els = {
            img: document.getElementById('manual-pics-img'),
            empty: document.getElementById('manual-pics-empty'),
            title: document.getElementById('manual-pics-title'),
            lines: document.getElementById('manual-pics-lines'),
            bar: document.getElementById('manual-pics-bar'),
            list: document.getElementById('manual-pics-list'),
            zoom: document.getElementById('manual-pics-zoom'),
            zoomImg: document.getElementById('manual-pics-zoom-img'),
            zoomTitle: document.getElementById('manual-pics-zoom-title'),
        };
        this._hint = this._els.empty ? this._els.empty.textContent : '';
        const self = this;
        this._onImgClick = function() { self._openZoom(); };
        this._onZoomClick = function(e) {
            if (e.target && e.target.dataset && e.target.dataset.action === 'close') { self._closeZoom(); }
        };
        this._onKeydown = function(e) {
            if (e.key === 'Escape') { self._closeZoom(); }
        };
        if (this._els.img) { this._els.img.addEventListener('click', this._onImgClick); }
        if (this._els.zoom) {
            this._els.zoom.addEventListener('click', this._onZoomClick);
            document.addEventListener('keydown', this._onKeydown);
        }
        this._render();
    }

    /** Подтягивает сохранённые картинки; ошибка сети оставляет галерею пустой. */
    ProductGallery.prototype.load = function() {
        const self = this;
        return Promise.resolve()
            .then(function() { return self._fetchJSON('/api/products'); })
            .then(function(list) {
                if (!Array.isArray(list)) { return; }
                for (let i = list.length - 1; i >= 0; i--) { self._upsert(list[i]); }
                self._render();
            })
            .catch(function(err) {
                console.warn('[ProductGallery] list:', err);
            });
    };

    /** Событие SSE `product`. */
    ProductGallery.prototype.applyProduct = function(p) {
        if (!p || !p.id) { return; }
        const cur = this._current();
        const followed = !this._pinned && cur !== null && cur.id === p.id;
        this._upsert(p);
        if (followed && p.done && p.lines < p.height) {
            this._cut = p;
        } else if (this._cut && p.id !== this._cut.id && !this._pinned) {
            this._cut = null;
        }
        this._render();
    };

    /** Выбор картинки щелчком по миниатюре. */
    ProductGallery.prototype.select = function(id) {
        this._selected = id;
        this._cut = null;
        this._pinned = this._items.length > 0 && this._items[0].id !== id;
        this._render();
    };

    ProductGallery.prototype.destroy = function() {
        for (const n of this._nodes.values()) {
            n.button.removeEventListener('click', n.onClick);
        }
        this._nodes.clear();
        if (this._els.img) { this._els.img.removeEventListener('click', this._onImgClick); }
        if (this._els.zoom) {
            this._els.zoom.removeEventListener('click', this._onZoomClick);
            document.removeEventListener('keydown', this._onKeydown);
        }
        this._closeZoom();
    };

    ProductGallery.prototype._current = function() {
        if (this._cut) { return null; }
        return this._items.find((p) => p.id === this._selected) || this._items[0] || null;
    };

    ProductGallery.prototype._zoomOpen = function() {
        return !!this._els.zoom && !this._els.zoom.classList.contains('modal--hidden');
    };

    ProductGallery.prototype._openZoom = function() {
        if (!this._els.zoom || !this._current()) { return; }
        this._els.zoom.classList.remove('modal--hidden');
        this._renderZoom();
    };

    ProductGallery.prototype._closeZoom = function() {
        if (this._els.zoom) { this._els.zoom.classList.add('modal--hidden'); }
    };

    /** Окно показывает ту же картинку, что и вкладка, и дорисовывается вместе с ней. */
    ProductGallery.prototype._renderZoom = function() {
        const cur = this._current();
        const els = this._els;
        if (!cur || !this._zoomOpen()) { return; }
        const url = imageURL(cur);
        if (els.zoomImg && this._zoomURL !== url) {
            els.zoomImg.src = url;
            this._zoomURL = url;
        }
        if (els.zoomImg) { els.zoomImg.alt = productCaption(cur, this._formatTime); }
        if (els.zoomTitle) {
            els.zoomTitle.textContent = productCaption(cur, this._formatTime) + ' · ' + progressText(cur);
        }
    };

    ProductGallery.prototype._upsert = function(p) {
        const i = this._items.findIndex(function(x) { return x.id === p.id; });
        if (i >= 0) {
            this._items[i] = p;
            return;
        }
        this._items.unshift(p);
        this._items.sort(function(a, b) { return b.started_ms - a.started_ms; });
        if (this._items.length > MAX_ITEMS) {
            for (const old of this._items.splice(MAX_ITEMS)) { this._dropNode(old.id); }
        }
        if (!this._pinned) { this._selected = this._items[0].id; }
    };

    ProductGallery.prototype._dropNode = function(id) {
        const n = this._nodes.get(id);
        if (!n) { return; }
        n.button.removeEventListener('click', n.onClick);
        this._nodes.delete(id);
    };

    ProductGallery.prototype._node = function(p) {
        let n = this._nodes.get(p.id);
        if (n) { return n; }
        const self = this;
        const li = document.createElement('li');
        li.className = 'ml-pics__item';
        const button = document.createElement('button');
        button.className = 'ml-pics__thumb';
        const thumb = document.createElement('img');
        thumb.alt = '';
        const cap = document.createElement('span');
        cap.className = 'ml-pics__thumb-cap';
        button.appendChild(thumb);
        button.appendChild(cap);
        li.appendChild(button);
        n = { li: li, button: button, thumb: thumb, cap: cap, onClick: function() { self.select(p.id); } };
        button.addEventListener('click', n.onClick);
        this._nodes.set(p.id, n);
        return n;
    };

    ProductGallery.prototype._render = function() {
        const els = this._els;
        const cur = this._current();
        if (els.empty) {
            els.empty.hidden = cur !== null;
            els.empty.textContent = this._cut
                ? 'Картинка оборвана на ' + this._cut.lines + ' из ' + this._cut.height + ' строк; ждём следующую'
                : this._hint;
        }
        if (els.img) {
            els.img.hidden = cur === null;
            if (cur) {
                // img.src в браузере возвращает абсолютный адрес: сравниваем с запомненным.
                const url = imageURL(cur);
                if (this._shownURL !== url) {
                    els.img.src = url;
                    this._shownURL = url;
                }
                els.img.alt = productCaption(cur, this._formatTime);
            }
        }
        if (els.title) { els.title.textContent = cur ? productCaption(cur, this._formatTime) : ''; }
        if (els.lines) { els.lines.textContent = cur ? progressText(cur) : ''; }
        if (els.bar) {
            els.bar.style.width = cur && cur.height > 0 ? Math.round(cur.lines / cur.height * 100) + '%' : '0%';
        }
        this._renderZoom();
        if (!els.list) { return; }
        const lis = [];
        for (const p of this._items) {
            const n = this._node(p);
            n.cap.textContent = this._formatTime(p.started_ms) + ' ' + (p.mode_name || p.mode);
            n.li.classList.toggle('is-selected', cur !== null && p.id === cur.id);
            n.li.classList.toggle('is-live', !p.done);
            if (p.done && !n.thumb.src) { n.thumb.src = imageURL(p); }
            lis.push(n.li);
        }
        els.list.replaceChildren.apply(els.list, lis);
    };

    window.ProductGallery = ProductGallery;

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            ProductGallery: ProductGallery,
            productCaption: productCaption,
            progressText: progressText,
        };
    }
})();
