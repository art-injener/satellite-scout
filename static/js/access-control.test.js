/**
 * Unit tests for AccessControl (receiver control: master and slaves).
 *
 * Run: node static/js/access-control.test.js
 *
 * Covers:
 *   - GET /api/access parsing, canControl / isMaster
 *   - request control: 202 pending, 200 granted, 409 codes
 *   - master password login: 204, 401, 429
 *   - SSE control events: request shown and closed by request_id only,
 *     granted / revoked refresh the role, denied explains the reason
 *   - decide / revoke requests
 *   - describeAccess: what the receiver panel shows for each role
 */

'use strict';

const assert = require('assert');

global.window = {};
const { AccessControl, AccessPanel, AccessRequestDialog, describeAccess } = require('./access-control.js');

/** fetch double: routes by "METHOD url", records calls. */
function makeFetch(routes) {
    const calls = [];
    function fetchImpl(url, init) {
        const method = (init && init.method) || 'GET';
        const key = method + ' ' + url;
        calls.push({ key, body: init && init.body ? JSON.parse(init.body) : null });
        let r = routes[key];
        if (typeof r === 'function') { r = r(); }
        if (!r) { return Promise.reject(new Error('unexpected ' + key)); }
        return Promise.resolve({
            ok: r.status >= 200 && r.status < 300,
            status: r.status,
            json: () => Promise.resolve(r.body || {}),
        });
    }
    fetchImpl.calls = calls;
    return fetchImpl;
}

/** document double: just enough for addEventListener / dispatch from tests. */
function makeDoc() {
    const listeners = {};
    return {
        addEventListener(type, fn) { (listeners[type] = listeners[type] || []).push(fn); },
        removeEventListener(type, fn) {
            const arr = listeners[type] || [];
            const i = arr.indexOf(fn);
            if (i >= 0) { arr.splice(i, 1); }
        },
        emit(event, data) {
            (listeners['satellite-scout-access'] || []).forEach((fn) => fn({ detail: { event, data } }));
        },
        count(type) { return (listeners[type] || []).length; },
    };
}

const flush = () => new Promise((r) => setImmediate(r));

let passed = 0;
let failed = 0;
const queue = [];
function test(name, fn) { queue.push([name, fn]); }

const VIEWER = { status: 200, body: { role: 'viewer', controller_active: false } };
const MASTER = { status: 200, body: { role: 'master', controller_active: false } };
const CONTROLLER = { status: 200, body: { role: 'controller', controller_active: true } };

test('refresh reads the role; viewer cannot control, controller and master can', async () => {
    let status = VIEWER;
    const ac = new AccessControl({ fetchImpl: makeFetch({ 'GET /api/access': () => status }), doc: makeDoc() });
    await ac.refresh();
    assert.strictEqual(ac.getState().role, 'viewer');
    assert.strictEqual(ac.canControl(), false);
    status = CONTROLLER;
    await ac.refresh();
    assert.strictEqual(ac.canControl(), true);
    assert.strictEqual(ac.isMaster(), false);
    status = MASTER;
    await ac.refresh();
    assert.strictEqual(ac.isMaster(), true);
    assert.strictEqual(ac.canControl(), true);
});

test('master sees a pending request from GET /api/access', async () => {
    const fetchImpl = makeFetch({ 'GET /api/access': { status: 200, body: {
        role: 'master', controller_active: false, pending: { request_id: 'r1', remote_addr: '192.168.1.232' },
    } } });
    const ac = new AccessControl({ fetchImpl, doc: makeDoc() });
    await ac.refresh();
    assert.deepStrictEqual(ac.getState().pending, { request_id: 'r1', remote_addr: '192.168.1.232' });
});

test('request: 202 waits for the master, a second click is not offered', async () => {
    const fetchImpl = makeFetch({ 'GET /api/access': VIEWER, 'POST /api/access/request': { status: 202, body: { status: 'pending' } } });
    const ac = new AccessControl({ fetchImpl, doc: makeDoc() });
    await ac.refresh();
    await ac.requestControl();
    assert.strictEqual(ac.getState().requestPending, true);
    assert.ok(/ждём ответа\./.test(ac.getState().notice), ac.getState().notice);
    assert.strictEqual(describeAccess(ac.getState()).canRequest, false);
});

test('request: 409 codes explain why', async () => {
    let code = 'no_master_online';
    const fetchImpl = makeFetch({
        'GET /api/access': VIEWER,
        'POST /api/access/request': () => ({ status: 409, body: { error: 'x', code } }),
    });
    const ac = new AccessControl({ fetchImpl, doc: makeDoc() });
    await ac.requestControl();
    assert.ok(/некому подтвердить/.test(ac.getState().notice), ac.getState().notice);
    assert.strictEqual(ac.getState().requestPending, false);
    code = 'request_pending';
    await ac.requestControl();
    assert.ok(/уже ждёт/.test(ac.getState().notice), ac.getState().notice);
});

test('request: 200 means rights are already there, role is re-read', async () => {
    let status = VIEWER;
    const fetchImpl = makeFetch({
        'GET /api/access': () => status,
        'POST /api/access/request': () => { status = CONTROLLER; return { status: 200, body: { status: 'granted' } }; },
    });
    const ac = new AccessControl({ fetchImpl, doc: makeDoc() });
    await ac.requestControl();
    assert.strictEqual(ac.getState().role, 'controller');
});

test('login: 204 re-reads the role, 401 and 429 come back as the error text', async () => {
    let status = VIEWER;
    let loginStatus = 401;
    const fetchImpl = makeFetch({
        'GET /api/access': () => status,
        'POST /api/access/login': () => {
            if (loginStatus === 204) { status = MASTER; }
            return { status: loginStatus, body: { error: 'x' } };
        },
    });
    const ac = new AccessControl({ fetchImpl, doc: makeDoc() });
    assert.ok(/Неверный пароль/.test(await ac.login('wrong')));
    assert.deepStrictEqual(fetchImpl.calls[0].body, { password: 'wrong' });
    assert.strictEqual(ac.getState().notice, '', 'login errors belong to the dialog, not the panel');
    loginStatus = 429;
    assert.ok(/подождите/.test(await ac.login('x')));
    loginStatus = 204;
    assert.strictEqual(await ac.login('right'), '');
    assert.strictEqual(ac.getState().role, 'master');
});

test('SSE control_request shows the request; closed with another id leaves it', async () => {
    const doc = makeDoc();
    const ac = new AccessControl({ fetchImpl: makeFetch({ 'GET /api/access': MASTER }), doc });
    ac.start();
    await flush();
    doc.emit('control_request', { request_id: 'r2', remote_addr: '10.0.0.5' });
    assert.strictEqual(ac.getState().pending.request_id, 'r2');
    doc.emit('control_request_closed', { request_id: 'r1' });
    assert.strictEqual(ac.getState().pending.request_id, 'r2', 'stale close must not hide the new request');
    doc.emit('control_request_closed', { request_id: 'r2' });
    assert.strictEqual(ac.getState().pending, null);
    ac.stop();
    assert.strictEqual(doc.count('satellite-scout-access'), 0, 'stop removes the listener');
});

test('SSE granted and revoked re-read the role and tell the user', async () => {
    let status = VIEWER;
    const doc = makeDoc();
    const ac = new AccessControl({ fetchImpl: makeFetch({ 'GET /api/access': () => status }), doc });
    ac.start();
    await flush();
    status = CONTROLLER;
    doc.emit('control_granted', {});
    await flush();
    assert.strictEqual(ac.getState().role, 'controller');
    assert.strictEqual(ac.getState().notice, 'Режим «Контроль» включён.');
    status = VIEWER;
    doc.emit('control_revoked', {});
    await flush();
    assert.strictEqual(ac.getState().role, 'viewer');
    assert.strictEqual(ac.getState().notice, 'Режим «Контроль» отключён.');
    ac.stop();
});

test('SSE released: the station side re-reads that nobody controls any more', async () => {
    let status = { status: 200, body: { role: 'master', controller_active: true, controller_addr: '10.0.0.5' } };
    const doc = makeDoc();
    const ac = new AccessControl({ fetchImpl: makeFetch({ 'GET /api/access': () => status }), doc });
    ac.start();
    await flush();
    assert.strictEqual(ac.getState().controllerActive, true);
    status = MASTER;
    doc.emit('control_released', {});
    await flush();
    assert.strictEqual(ac.getState().controllerActive, false);
    ac.stop();
});

test('release posts, re-reads the role and leaves the panel quiet', async () => {
    let status = CONTROLLER;
    const fetchImpl = makeFetch({
        'GET /api/access': () => status,
        'POST /api/access/release': () => { status = VIEWER; return { status: 204 }; },
    });
    const ac = new AccessControl({ fetchImpl, doc: makeDoc() });
    await ac.refresh();
    ac.setNotice('Режим «Контроль» включён.');
    await ac.release();
    assert.ok(fetchImpl.calls.some((c) => c.key === 'POST /api/access/release'));
    assert.strictEqual(ac.getState().role, 'viewer');
    assert.strictEqual(ac.getState().notice, '');
});

test('a panel notice clears itself, the wait for an answer stays', async () => {
    const ac = new AccessControl({
        fetchImpl: makeFetch({
            'GET /api/access': VIEWER,
            'POST /api/access/request': { status: 202, body: { status: 'pending' } },
        }),
        doc: makeDoc(),
        noticeMs: 30,
    });
    ac.setNotice('Демодулятор не переключён: HTTP 405');
    assert.strictEqual(ac.getState().notice, 'Демодулятор не переключён: HTTP 405');
    await new Promise((resolve) => setTimeout(resolve, 50));
    assert.strictEqual(ac.getState().notice, '');
    await ac.requestControl();
    assert.ok(/ждём ответа/.test(ac.getState().notice), ac.getState().notice);
    await new Promise((resolve) => setTimeout(resolve, 50));
    assert.ok(/ждём ответа/.test(ac.getState().notice), 'the pending wait is not a flash message');
    ac.setNotice('');
    assert.strictEqual(ac.getState().notice, '');
    ac.stop();
});

test('SSE denied ends the wait and names the reason', async () => {
    const doc = makeDoc();
    const ac = new AccessControl({ fetchImpl: makeFetch({
        'GET /api/access': VIEWER, 'POST /api/access/request': { status: 202, body: { status: 'pending' } },
    }), doc });
    ac.start();
    await ac.requestControl();
    doc.emit('control_denied', { reason: 'timeout' });
    assert.strictEqual(ac.getState().requestPending, false);
    assert.ok(/нет 30 секунд/.test(ac.getState().notice), ac.getState().notice);
    doc.emit('control_denied', { reason: 'rejected' });
    assert.ok(/отклонён/.test(ac.getState().notice), ac.getState().notice);
    ac.stop();
});

test('decide posts the answer and hides the request; revoke posts', async () => {
    const fetchImpl = makeFetch({
        'GET /api/access': MASTER,
        'POST /api/access/decide': { status: 204 },
        'POST /api/access/revoke': { status: 204 },
    });
    const doc = makeDoc();
    const ac = new AccessControl({ fetchImpl, doc });
    ac.start();
    await flush();
    doc.emit('control_request', { request_id: 'r3', remote_addr: '10.0.0.5' });
    await ac.decide('r3', true);
    assert.deepStrictEqual(fetchImpl.calls.find((c) => c.key === 'POST /api/access/decide').body, { request_id: 'r3', allow: true });
    assert.strictEqual(ac.getState().pending, null);
    await ac.revoke();
    assert.ok(fetchImpl.calls.some((c) => c.key === 'POST /api/access/revoke'));
    ac.stop();
});

test('onChange fires on every state change and can be removed', async () => {
    const ac = new AccessControl({ fetchImpl: makeFetch({ 'GET /api/access': MASTER }), doc: makeDoc() });
    let n = 0;
    const off = ac.onChange(() => { n++; });
    await ac.refresh();
    assert.strictEqual(n, 1);
    off();
    await ac.refresh();
    assert.strictEqual(n, 1);
});

test('describeAccess: viewer is in view mode and can open the control dialog', () => {
    const d = describeAccess({ role: 'viewer', controllerActive: false, requestPending: false });
    assert.strictEqual(d.roleLabel, 'Просмотр');
    assert.strictEqual(d.showManage, true);
    assert.strictEqual(d.canRequest, true);
    assert.strictEqual(d.showRevoke, false);
    assert.strictEqual(d.showRelease, false);
});

test('describeAccess: master is in control mode and can take control back from a slave', () => {
    const d = describeAccess({ role: 'master', controllerActive: true, controllerAddr: '10.0.0.5' });
    assert.strictEqual(d.roleLabel, 'Контроль');
    assert.strictEqual(d.showRevoke, true);
    assert.ok(/10\.0\.0\.5/.test(d.detail), d.detail);
    assert.strictEqual(d.showManage, false);
    assert.strictEqual(d.showRelease, false, 'master has nobody to give control back to');
});

test('describeAccess: controller is in control mode and can only give it back; viewer sees someone else controls', () => {
    const c = describeAccess({ role: 'controller', controllerActive: true });
    assert.strictEqual(c.roleLabel, 'Контроль');
    assert.strictEqual(c.showManage || c.showRevoke, false);
    assert.strictEqual(c.showRelease, true);
    const v = describeAccess({ role: 'viewer', controllerActive: true, requestPending: false });
    assert.ok(/другой пользователь/.test(v.detail), v.detail);
});

test('request plate names the requester by address, without role names', async () => {
    const doc = makeDoc();
    const ac = new AccessControl({ fetchImpl: makeFetch({ 'GET /api/access': MASTER }), doc });
    ac.start();
    await flush();
    const els = { root: makeEl(), text: makeEl(), allow: makeEl(), deny: makeEl() };
    new AccessRequestDialog(ac, els);
    doc.emit('control_request', { request_id: 'r1', remote_addr: '192.168.1.232' });
    assert.strictEqual(els.root.hidden, false);
    assert.strictEqual(els.text.textContent, '192.168.1.232 просит управление приёмником');
    ac.stop();
});

test('panel texts do not use role names', () => {
    const states = [
        { role: 'master', controllerActive: true, controllerAddr: '10.0.0.5' },
        { role: 'controller', controllerActive: true },
        { role: 'viewer', controllerActive: true },
    ];
    for (const s of states) {
        const d = describeAccess(s);
        assert.ok(!/мастер|слейв/i.test(d.roleLabel + d.detail), d.roleLabel + ' ' + d.detail);
    }
});

// ── Control dialog ────────────────────────────────────────────────────

/** DOM element double: events, classList, the few properties the panel touches. */
function makeEl() {
    const handlers = {};
    const classes = new Set();
    return {
        hidden: false, disabled: false, value: '', textContent: '', focused: false, attrs: {},
        classList: {
            add: (c) => classes.add(c), remove: (c) => classes.delete(c), contains: (c) => classes.has(c),
        },
        setAttribute(k, v) { this.attrs[k] = String(v); },
        addEventListener(t, fn) { (handlers[t] = handlers[t] || []).push(fn); },
        removeEventListener(t, fn) {
            const a = handlers[t] || [];
            const i = a.indexOf(fn);
            if (i >= 0) { a.splice(i, 1); }
        },
        fire(t, ev) { (handlers[t] || []).slice().forEach((fn) => fn(Object.assign({ preventDefault() {} }, ev))); },
        focus() { this.focused = true; },
        count(t) { return (handlers[t] || []).length; },
    };
}

/** Panel elements with the dialog inside; the dialog starts hidden as in the markup. */
function makePanelEls() {
    const els = {
        role: makeEl(), detail: makeEl(), manage: makeEl(), revoke: makeEl(), release: makeEl(), notice: makeEl(),
        modal: {
            root: makeEl(), backdrop: makeEl(), close: makeEl(), request: makeEl(),
            loginForm: makeEl(), password: makeEl(), submit: makeEl(), error: makeEl(), doc: makeEl(),
        },
    };
    els.modal.root.classList.add('modal--hidden');
    return els;
}

const isOpen = (els) => !els.modal.root.classList.contains('modal--hidden');

async function viewerPanel(routes) {
    const fetchImpl = makeFetch(Object.assign({ 'GET /api/access': VIEWER }, routes));
    const ac = new AccessControl({ fetchImpl, doc: makeDoc() });
    await ac.refresh();
    const els = makePanelEls();
    const panel = new AccessPanel(ac, els);
    return { ac, els, panel, fetchImpl };
}

test('panel: viewer sees the mode and one button; it opens the dialog', async () => {
    const { els } = await viewerPanel();
    assert.strictEqual(els.role.textContent, 'Просмотр');
    assert.strictEqual(els.manage.hidden, false);
    assert.strictEqual(isOpen(els), false);
    els.manage.fire('click');
    assert.strictEqual(isOpen(els), true);
    assert.strictEqual(els.modal.password.focused, true, 'password field gets the focus');
});

test('panel: master does not see the control button', async () => {
    const ac = new AccessControl({ fetchImpl: makeFetch({ 'GET /api/access': MASTER }), doc: makeDoc() });
    await ac.refresh();
    const els = makePanelEls();
    new AccessPanel(ac, els);
    assert.strictEqual(els.role.textContent, 'Контроль');
    assert.strictEqual(els.manage.hidden, true);
    assert.strictEqual(els.release.hidden, true);
});

test('panel: controller sees «Вернуть» and gives control back with it', async () => {
    let status = CONTROLLER;
    const fetchImpl = makeFetch({
        'GET /api/access': () => status,
        'POST /api/access/release': () => { status = VIEWER; return { status: 204 }; },
    });
    const ac = new AccessControl({ fetchImpl, doc: makeDoc() });
    await ac.refresh();
    const els = makePanelEls();
    new AccessPanel(ac, els);
    assert.strictEqual(els.manage.hidden, true);
    assert.strictEqual(els.release.hidden, false);
    els.release.fire('click');
    await flush();
    await flush();
    assert.ok(fetchImpl.calls.some((c) => c.key === 'POST /api/access/release'));
    assert.strictEqual(els.role.textContent, 'Просмотр');
    assert.strictEqual(els.release.hidden, true);
    assert.strictEqual(els.manage.hidden, false);
});

test('dialog: asking the master sends the request and closes', async () => {
    const { els, fetchImpl, ac } = await viewerPanel({ 'POST /api/access/request': { status: 202, body: {} } });
    els.manage.fire('click');
    els.modal.request.fire('click');
    await flush();
    assert.ok(fetchImpl.calls.some((c) => c.key === 'POST /api/access/request'));
    assert.strictEqual(isOpen(els), false);
    assert.ok(/ждём ответа/.test(els.notice.textContent), 'the outcome is shown in the panel');
    els.manage.fire('click');
    assert.strictEqual(els.modal.request.disabled, true, 'second request is not offered while waiting');
    assert.strictEqual(els.modal.request.textContent, 'Запрос отправлен');
    assert.strictEqual(ac.getState().requestPending, true);
});

test('dialog: the request button says just «Запросить»', async () => {
    const { els } = await viewerPanel();
    assert.strictEqual(els.modal.request.textContent, 'Запросить');
});

test('dialog: wrong password keeps it open with the error, right one closes it', async () => {
    let loginStatus = 401;
    let status = VIEWER;
    const { els } = await viewerPanel({
        'GET /api/access': () => status,
        'POST /api/access/login': () => {
            if (loginStatus === 204) { status = MASTER; }
            return { status: loginStatus, body: {} };
        },
    });
    els.manage.fire('click');
    els.modal.password.value = 'wrong';
    els.modal.loginForm.fire('submit');
    await flush();
    assert.strictEqual(isOpen(els), true);
    assert.ok(/Неверный пароль/.test(els.modal.error.textContent), els.modal.error.textContent);
    assert.strictEqual(els.notice.textContent, '', 'panel stays quiet');

    loginStatus = 204;
    els.modal.password.value = 'right';
    els.modal.loginForm.fire('submit');
    await flush();
    await flush();
    assert.strictEqual(isOpen(els), false);
    assert.strictEqual(els.modal.password.value, '', 'password is not kept in the field');
    assert.strictEqual(els.role.textContent, 'Контроль');
});

test('dialog: empty password is not sent', async () => {
    const { els, fetchImpl } = await viewerPanel();
    els.manage.fire('click');
    els.modal.loginForm.fire('submit');
    await flush();
    assert.ok(!fetchImpl.calls.some((c) => c.key === 'POST /api/access/login'));
    assert.ok(els.modal.error.textContent.length > 0);
});

test('dialog: closes by the cross, the backdrop and Escape; reopening clears the error', async () => {
    const { els } = await viewerPanel();
    for (const close of [() => els.modal.close.fire('click'), () => els.modal.backdrop.fire('click'),
        () => els.modal.doc.fire('keydown', { key: 'Escape' })]) {
        els.manage.fire('click');
        els.modal.error.textContent = 'old';
        close();
        assert.strictEqual(isOpen(els), false);
    }
    els.manage.fire('click');
    assert.strictEqual(els.modal.error.textContent, '');
    els.modal.doc.fire('keydown', { key: 'Enter' });
    assert.strictEqual(isOpen(els), true, 'other keys do not close it');
});

test('dialog: control granted while it is open closes it', async () => {
    const { els, ac } = await viewerPanel();
    els.manage.fire('click');
    ac._set({ role: 'controller' });
    assert.strictEqual(isOpen(els), false);
});

test('panel: destroy removes every listener, including the dialog ones', async () => {
    const { els, panel } = await viewerPanel();
    panel.destroy();
    assert.strictEqual(els.manage.count('click'), 0);
    assert.strictEqual(els.release.count('click'), 0);
    assert.strictEqual(els.modal.request.count('click'), 0);
    assert.strictEqual(els.modal.loginForm.count('submit'), 0);
    assert.strictEqual(els.modal.doc.count('keydown'), 0);
});

(async () => {
    for (const [name, fn] of queue) {
        try {
            await fn();
            passed++;
            console.log('  ✓ ' + name);
        } catch (err) {
            failed++;
            console.log('  ✗ ' + name);
            console.log('    ' + (err && err.stack ? err.stack : err));
        }
    }
    console.log(`\n${passed} passed, ${failed} failed`);
    if (failed > 0) { process.exit(1); }
})();
