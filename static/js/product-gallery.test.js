/**
 * Unit tests for ProductGallery.
 *
 * Run: node static/js/product-gallery.test.js
 */

'use strict';

const assert = require('assert');

function makeEl(tag) {
    const el = {
        tagName: tag,
        textContent: '',
        title: '',
        hidden: false,
        src: '',
        alt: '',
        className: '',
        children: [],
        dataset: {},
        style: {},
        _listeners: {},
        classList: {
            _set: new Set(),
            toggle(cls, on) {
                if (on) { this._set.add(cls); } else { this._set.delete(cls); }
            },
            add(cls) { this._set.add(cls); },
            remove(cls) { this._set.delete(cls); },
            contains(cls) { return this._set.has(cls); },
        },
        appendChild(child) { this.children.push(child); return child; },
        replaceChildren(...nodes) { this.children = nodes; },
        addEventListener(type, fn) { (this._listeners[type] = this._listeners[type] || []).push(fn); },
        removeEventListener(type, fn) {
            this._listeners[type] = (this._listeners[type] || []).filter((f) => f !== fn);
        },
        click() { (this._listeners.click || []).forEach((fn) => fn({})); },
    };
    return el;
}

const ids = ['manual-pics-img', 'manual-pics-empty', 'manual-pics-title', 'manual-pics-lines',
    'manual-pics-bar', 'manual-pics-list', 'manual-pics-zoom', 'manual-pics-zoom-img', 'manual-pics-zoom-title'];
let nodes;
const docListeners = {};
function resetDom() {
    nodes = new Map(ids.map((id) => [id, makeEl(id)]));
    nodes.get('manual-pics-zoom').classList.add('modal--hidden');
}
resetDom();

global.document = {
    getElementById: (id) => nodes.get(id) || null,
    createElement: (tag) => makeEl(tag),
    addEventListener(type, fn) { (docListeners[type] = docListeners[type] || []).push(fn); },
    removeEventListener(type, fn) {
        docListeners[type] = (docListeners[type] || []).filter((f) => f !== fn);
    },
};
global.window = {};

const { ProductGallery, productCaption, progressText } = require('./product-gallery.js');

let passed = 0;
let failed = 0;
async function test(name, fn) {
    resetDom();
    try {
        await fn();
        passed++;
        console.log(`  ✓ ${name}`);
    } catch (err) {
        failed++;
        console.error(`  ✗ ${name}`);
        console.error(err);
    }
}

const fmt = (ms) => 'T' + ms;
function product(id, patch) {
    return Object.assign({
        id: id, kind: 'sstv', mode: 'robot36', mode_name: 'Robot 36', radio_path: 'radio_path_0',
        frequency_hz: 436020000, width: 320, height: 240, lines: 0, done: false, started_ms: 1000,
    }, patch);
}
const listEl = () => nodes.get('manual-pics-list');
const img = () => nodes.get('manual-pics-img');
const zoom = () => nodes.get('manual-pics-zoom');
const zoomOpen = () => !zoom().classList.contains('modal--hidden');

(async () => {
    await test('productCaption names the mode, time and frequency', () => {
        assert.strictEqual(productCaption(product('a'), fmt), 'Robot 36 · T1000 · 436,020 МГц');
        assert.strictEqual(productCaption(product('a', { mode_name: undefined, frequency_hz: undefined }), fmt),
            'robot36 · T1000');
    });

    await test('progressText tells receiving, done and cut short apart', () => {
        assert.strictEqual(progressText(product('a', { lines: 120 })), '120 / 240 строк');
        assert.strictEqual(progressText(product('a', { lines: 240, done: true })), 'готово');
        assert.strictEqual(progressText(product('a', { lines: 120, done: true })), 'оборвана на 120 из 240');
    });

    await test('empty gallery shows the hint', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [], formatTime: fmt });
        await g.load();
        assert.strictEqual(nodes.get('manual-pics-empty').hidden, false);
        assert.strictEqual(img().hidden, true);
        assert.strictEqual(listEl().children.length, 0);
        g.destroy();
    });

    await test('load shows the newest saved picture', async () => {
        let url = '';
        const g = new ProductGallery({
            fetchJSON: async (u) => {
                url = u;
                return [product('b', { lines: 240, done: true, started_ms: 2000 }), product('a', { done: true, lines: 240 })];
            },
            formatTime: fmt,
        });
        await g.load();
        assert.strictEqual(url, '/api/products');
        assert.strictEqual(img().hidden, false);
        assert.strictEqual(img().src, '/api/products/b?l=240');
        assert.strictEqual(nodes.get('manual-pics-title').textContent, 'Robot 36 · T2000 · 436,020 МГц');
        assert.strictEqual(listEl().children.length, 2);
        assert.ok(listEl().children[0].classList.contains('is-selected'));
        g.destroy();
    });

    await test('a picture coming in is followed line by line', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [], formatTime: fmt });
        await g.load();
        g.applyProduct(product('a'));
        g.applyProduct(product('a', { lines: 120 }));
        assert.strictEqual(img().src, '/api/products/a?l=120');
        assert.strictEqual(nodes.get('manual-pics-lines').textContent, '120 / 240 строк');
        assert.strictEqual(nodes.get('manual-pics-bar').style.width, '50%');
        assert.ok(listEl().children[0].classList.contains('is-live'));
        g.applyProduct(product('a', { lines: 240, done: true }));
        assert.strictEqual(nodes.get('manual-pics-lines').textContent, 'готово');
        assert.ok(!listEl().children[0].classList.contains('is-live'));
        g.destroy();
    });

    await test('thumbnails reuse their nodes and load once the picture is done', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [product('a', { done: true, lines: 240 })], formatTime: fmt });
        await g.load();
        const old = listEl().children[0];
        g.applyProduct(product('b', { started_ms: 3000 }));
        assert.strictEqual(listEl().children.length, 2);
        assert.strictEqual(listEl().children[1], old, 'existing item node is kept');
        const thumb = (li) => li.children[0].children[0];
        assert.strictEqual(thumb(old).src, '/api/products/a?l=240');
        assert.strictEqual(thumb(listEl().children[0]).src, '', 'no thumbnail while receiving');
        g.applyProduct(product('b', { started_ms: 3000, lines: 240, done: true }));
        assert.strictEqual(thumb(listEl().children[0]).src, '/api/products/b?l=240');
        g.destroy();
    });

    await test('picking an older picture pins it until the newest is picked again', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [product('a', { done: true, lines: 240 })], formatTime: fmt });
        await g.load();
        g.applyProduct(product('b', { started_ms: 3000 }));
        assert.strictEqual(img().src, '/api/products/b?l=0', 'a new picture is shown');
        listEl().children[1].children[0].click();
        assert.strictEqual(img().src, '/api/products/a?l=240');
        g.applyProduct(product('b', { started_ms: 3000, lines: 60 }));
        assert.strictEqual(img().src, '/api/products/a?l=240', 'pinned picture stays');
        listEl().children[0].children[0].click();
        g.applyProduct(product('b', { started_ms: 3000, lines: 90 }));
        assert.strictEqual(img().src, '/api/products/b?l=90');
        g.destroy();
    });

    await test('a picture cut short on air clears the main view until the next one', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [product('old', { done: true, lines: 240, started_ms: 500 })], formatTime: fmt });
        await g.load();
        g.applyProduct(product('a', { lines: 52 }));
        g.applyProduct(product('a', { lines: 52, done: true }));
        assert.strictEqual(img().hidden, true);
        assert.strictEqual(nodes.get('manual-pics-empty').hidden, false);
        assert.strictEqual(nodes.get('manual-pics-empty').textContent, 'Картинка оборвана на 52 из 240 строк; ждём следующую');
        assert.strictEqual(nodes.get('manual-pics-title').textContent, '');
        assert.strictEqual(listEl().children.length, 2, 'the cut picture stays in the list');
        assert.ok(!listEl().children[0].classList.contains('is-selected'));

        g.applyProduct(product('b', { started_ms: 3000, lines: 10 }));
        assert.strictEqual(img().hidden, false);
        assert.strictEqual(img().src, '/api/products/b?l=10');
        g.destroy();
    });

    await test('a cut picture opens again from its thumbnail, a pinned one is not cleared', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [product('old', { done: true, lines: 240, started_ms: 500 })], formatTime: fmt });
        await g.load();
        g.applyProduct(product('a', { lines: 52 }));
        g.applyProduct(product('a', { lines: 52, done: true }));
        listEl().children[0].children[0].click();
        assert.strictEqual(img().hidden, false);
        assert.strictEqual(img().src, '/api/products/a?l=52');

        g.applyProduct(product('b', { started_ms: 3000, lines: 10 }));
        listEl().children[2].children[0].click();
        g.applyProduct(product('b', { started_ms: 3000, lines: 20, done: true }));
        assert.strictEqual(img().src, '/api/products/old?l=240', 'pinned picture stays');
        g.destroy();
    });

    await test('a failed list request leaves the gallery usable', async () => {
        const g = new ProductGallery({ fetchJSON: async () => { throw new Error('offline'); }, formatTime: fmt });
        await g.load();
        g.applyProduct(product('a'));
        assert.strictEqual(listEl().children.length, 1);
        g.destroy();
    });

    await test('clicking the picture opens it scaled up in a modal that follows the lines', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [], formatTime: fmt });
        await g.load();
        g.applyProduct(product('a', { lines: 120 }));
        assert.strictEqual(zoomOpen(), false);
        img().click();
        assert.strictEqual(zoomOpen(), true);
        assert.strictEqual(nodes.get('manual-pics-zoom-img').src, '/api/products/a?l=120');
        assert.strictEqual(nodes.get('manual-pics-zoom-title').textContent,
            'Robot 36 · T1000 · 436,020 МГц · 120 / 240 строк');
        g.applyProduct(product('a', { lines: 180 }));
        assert.strictEqual(nodes.get('manual-pics-zoom-img').src, '/api/products/a?l=180');
        g.destroy();
    });

    await test('the modal closes on Escape, the close button and the backdrop', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [product('a', { done: true, lines: 240 })], formatTime: fmt });
        await g.load();
        const fire = (type, e) => (docListeners[type] || []).forEach((fn) => fn(e));
        const clickZoom = (target) => (zoom()._listeners.click || []).forEach((fn) => fn({ target: target }));

        img().click();
        fire('keydown', { key: 'Escape' });
        assert.strictEqual(zoomOpen(), false, 'Escape closes');

        img().click();
        clickZoom(nodes.get('manual-pics-zoom-img'));
        assert.strictEqual(zoomOpen(), true, 'a click on the picture keeps it open');
        clickZoom({ dataset: { action: 'close' } });
        assert.strictEqual(zoomOpen(), false, 'close button or backdrop closes');

        g.destroy();
        assert.strictEqual((docListeners.keydown || []).length, 0, 'destroy drops the key listener');
    });

    await test('clicking with no picture opens nothing', async () => {
        const g = new ProductGallery({ fetchJSON: async () => [], formatTime: fmt });
        await g.load();
        img().click();
        assert.strictEqual(zoomOpen(), false);
        g.destroy();
    });

    console.log(`\n${passed} passed, ${failed} failed\n`);
    process.exit(failed > 0 ? 1 : 0);
})();
