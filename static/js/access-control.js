// access-control.js — управление приёмником: мастер и слейвы.
//
// Мастер — тот, кто открыл интерфейс на машине с сервером, или вошёл по паролю
// мастера. Остальные смотрят спектр, а перестраивать приёмник могут, только
// получив разрешение мастера. Разрешение одно на всех: новое отзывает прежнее.
//
// Роль и состояние — GET /api/access. События передачи управления приходят по
// SSE (sse-client.js пересылает их событием документа satellite-scout-access).
// События разных действий могут прийти не по порядку, поэтому запрос у мастера
// закрывается только по control_request_closed с тем же request_id.

'use strict';

(function() {
    const ACCESS_EVENT = 'satellite-scout-access';

    // Тексты для пользователя обходятся без названий ролей: только режимы «Контроль» и «Просмотр»
    const NOTICE = {
        pending: 'Запрос отправлен, ждём ответа.',
        noMaster: 'Сейчас некому подтвердить запрос. Войдите по паролю станции.',
        alreadyPending: 'Запрос уже ждёт ответа.',
        granted: 'Режим «Контроль» включён.',
        rejected: 'Запрос отклонён.',
        timeout: 'Ответа нет 30 секунд, запрос снят.',
        revoked: 'Режим «Контроль» отключён.',
        badPassword: 'Неверный пароль.',
        tooMany: 'Слишком много попыток, подождите минуту.',
        emptyPassword: 'Введите пароль станции.',
    };

    const MODE_CONTROL = 'Режим: Контроль';
    const MODE_VIEW = 'Режим: Просмотр';

    /**
     * Что показать в панели управления для текущего состояния. Режимов для
     * пользователя два: мастер и слейв с разрешением оба в «Контроле».
     * @param {{role: string, controllerActive?: boolean, controllerAddr?: string,
     *          requestPending?: boolean}} s
     */
    function describeAccess(s) {
        if (s.role === 'master') {
            return {
                roleLabel: MODE_CONTROL,
                detail: s.controllerActive
                    ? 'Приёмником управляет ' + (s.controllerAddr || 'другой пользователь')
                    : '',
                showManage: false,
                canRequest: false,
                showRevoke: !!s.controllerActive,
                showRelease: false,
            };
        }
        if (s.role === 'controller') {
            return {
                roleLabel: MODE_CONTROL, detail: '',
                showManage: false, canRequest: false, showRevoke: false, showRelease: true,
            };
        }
        return {
            roleLabel: MODE_VIEW,
            detail: s.controllerActive ? 'Приёмником управляет другой пользователь' : '',
            showManage: true,
            canRequest: !s.requestPending,
            showRevoke: false,
            showRelease: false,
        };
    }

    /**
     * @param {{fetchImpl?: Function, doc?: Object}} [opts]
     */
    function AccessControl(opts) {
        opts = opts || {};
        this._fetch = opts.fetchImpl || function(u, i) { return fetch(u, i); };
        this._doc = opts.doc || (typeof document !== 'undefined' ? document : null);
        this._state = {
            role: 'viewer',
            controllerActive: false,
            controllerAddr: '',
            pending: null,
            requestPending: false,
            notice: '',
        };
        this._listeners = [];
        this._onEvent = null;
    }

    AccessControl.prototype.getState = function() {
        return this._state;
    };

    AccessControl.prototype.canControl = function() {
        return this._state.role === 'master' || this._state.role === 'controller';
    };

    AccessControl.prototype.isMaster = function() {
        return this._state.role === 'master';
    };

    /** @returns {function(): void} отписка */
    AccessControl.prototype.onChange = function(cb) {
        const list = this._listeners;
        list.push(cb);
        return function() {
            const i = list.indexOf(cb);
            if (i >= 0) { list.splice(i, 1); }
        };
    };

    AccessControl.prototype._set = function(patch) {
        Object.assign(this._state, patch);
        const list = this._listeners.slice();
        for (let i = 0; i < list.length; i++) {
            try { list[i](this._state); } catch (err) { console.error('[AccessControl] listener error:', err); }
        }
    };

    /** Сообщение пользователю в панели управления. */
    AccessControl.prototype.setNotice = function(text) {
        this._set({ notice: text || '' });
    };

    AccessControl.prototype.start = function() {
        if (this._onEvent || !this._doc) { return; }
        const self = this;
        this._onEvent = function(e) { self._handleEvent(e.detail || {}); };
        this._doc.addEventListener(ACCESS_EVENT, this._onEvent);
        this.refresh();
    };

    AccessControl.prototype.stop = function() {
        if (this._onEvent && this._doc) {
            this._doc.removeEventListener(ACCESS_EVENT, this._onEvent);
        }
        this._onEvent = null;
    };

    AccessControl.prototype.refresh = function() {
        const self = this;
        return this._fetch('/api/access', { credentials: 'same-origin', cache: 'no-store' })
            .then(function(r) { return r.json(); })
            .then(function(s) {
                self._set({
                    role: s.role || 'viewer',
                    controllerActive: !!s.controller_active,
                    controllerAddr: s.controller_addr || '',
                    pending: s.pending || null,
                    requestPending: !!s.request_pending,
                });
            })
            .catch(function(err) { console.error('[AccessControl] status failed:', err); });
    };

    AccessControl.prototype._post = function(url, body) {
        return this._fetch(url, {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body || {}),
        });
    };

    AccessControl.prototype.requestControl = function() {
        const self = this;
        return this._post('/api/access/request').then(function(r) {
            if (r.status === 202) {
                self._set({ requestPending: true, notice: NOTICE.pending });
                return null;
            }
            if (r.status === 200) { return self.refresh(); }
            return r.json().then(function(b) {
                const code = b && b.code;
                self._set({
                    notice: code === 'no_master_online' ? NOTICE.noMaster
                        : code === 'request_pending' ? NOTICE.alreadyPending
                            : 'Запрос не принят: ' + ((b && b.error) || ('HTTP ' + r.status)),
                });
            });
        }).catch(function(err) { self.setNotice('Запрос не отправлен: ' + err.message); });
    };

    /**
     * Ошибку входа показывает окно, в котором вводили пароль, а не панель.
     * @returns {Promise<string>} пустая строка — вошли мастером, иначе текст ошибки
     */
    AccessControl.prototype.login = function(password) {
        const self = this;
        return this._post('/api/access/login', { password: password }).then(function(r) {
            if (r.status === 204) {
                self._set({ notice: '' });
                return self.refresh().then(function() { return ''; });
            }
            return r.status === 401 ? NOTICE.badPassword
                : r.status === 429 ? NOTICE.tooMany : 'Вход не выполнен: HTTP ' + r.status;
        }).catch(function(err) {
            return 'Вход не выполнен: ' + err.message;
        });
    };

    AccessControl.prototype.decide = function(requestId, allow) {
        const self = this;
        return this._post('/api/access/decide', { request_id: requestId, allow: !!allow }).then(function() {
            // 404: запрос уже закрыт таймаутом или новым запросом — скрываем в любом случае
            if (self._state.pending && self._state.pending.request_id === requestId) {
                self._set({ pending: null });
            }
            return self.refresh();
        }).catch(function(err) { console.error('[AccessControl] decide failed:', err); });
    };

    AccessControl.prototype.revoke = function() {
        const self = this;
        return this._post('/api/access/revoke').then(function() { return self.refresh(); })
            .catch(function(err) { console.error('[AccessControl] revoke failed:', err); });
    };

    /** Вернуть управление, полученное по запросу. */
    AccessControl.prototype.release = function() {
        const self = this;
        return this._post('/api/access/release').then(function() {
            self._set({ notice: '' });
            return self.refresh();
        }).catch(function(err) { self.setNotice('Управление не возвращено: ' + err.message); });
    };

    AccessControl.prototype._handleEvent = function(d) {
        const data = d.data || {};
        switch (d.event) {
            case 'control_request':
                this._set({ pending: { request_id: data.request_id, remote_addr: data.remote_addr } });
                break;
            case 'control_request_closed':
                if (this._state.pending && this._state.pending.request_id === data.request_id) {
                    this._set({ pending: null });
                }
                break;
            case 'control_granted':
                this._set({ requestPending: false, notice: NOTICE.granted });
                this.refresh();
                break;
            case 'control_denied':
                this._set({ requestPending: false, notice: data.reason === 'timeout' ? NOTICE.timeout : NOTICE.rejected });
                break;
            case 'control_revoked':
                this._set({ notice: NOTICE.revoked });
                this.refresh();
                break;
            case 'control_released':
                this.refresh();
                break;
            default:
                break;
        }
    };

    // ── Панель управления в колонке приёмника ─────────────────────────────

    /**
     * В панели — режим и одна кнопка; запрос у мастера и вход по паролю — в окне.
     * @param {AccessControl} ac
     * @param {{role, detail, manage, revoke, release, notice,
     *          modal?: {root, backdrop, close, request, loginForm, password, submit, error, doc?}}} els
     */
    function AccessPanel(ac, els) {
        this._ac = ac;
        this._els = els;
        this._handlers = [];
        const self = this;
        const m = els.modal || {};
        this._modal = m;
        this._on(els.manage, 'click', function() { self.openModal(); });
        this._on(els.revoke, 'click', function() { ac.revoke(); });
        this._on(els.release, 'click', function() { ac.release(); });
        this._on(m.close, 'click', function() { self.closeModal(); });
        this._on(m.backdrop, 'click', function() { self.closeModal(); });
        this._on(m.doc || (typeof document !== 'undefined' ? document : null), 'keydown', function(e) {
            if (e.key === 'Escape' && self.isModalOpen()) { self.closeModal(); }
        });
        this._on(m.request, 'click', function() {
            ac.requestControl().then(function() { self.closeModal(); });
        });
        this._on(m.loginForm, 'submit', function(e) {
            if (e && typeof e.preventDefault === 'function') { e.preventDefault(); }
            self._login();
        });
        this._off = ac.onChange(function() { self.render(); });
        this.render();
    }

    AccessPanel.prototype.isModalOpen = function() {
        const root = this._modal.root;
        return !!root && !root.classList.contains('modal--hidden');
    };

    AccessPanel.prototype.openModal = function() {
        const m = this._modal;
        if (!m.root || this._ac.canControl()) { return; }
        if (m.error) { m.error.textContent = ''; }
        if (m.password) { m.password.value = ''; }
        m.root.classList.remove('modal--hidden');
        this.render();
        if (m.password && typeof m.password.focus === 'function') { m.password.focus(); }
    };

    AccessPanel.prototype.closeModal = function() {
        const m = this._modal;
        if (m.root) { m.root.classList.add('modal--hidden'); }
        if (m.password) { m.password.value = ''; }
    };

    AccessPanel.prototype._login = function() {
        const m = this._modal;
        const self = this;
        const pwd = m.password ? m.password.value : '';
        if (!pwd) {
            if (m.error) { m.error.textContent = NOTICE.emptyPassword; }
            return;
        }
        if (m.submit) { m.submit.disabled = true; }
        this._ac.login(pwd).then(function(err) {
            if (m.submit) { m.submit.disabled = false; }
            if (!err) { self.closeModal(); return; }
            if (m.error) { m.error.textContent = err; }
        });
    };

    AccessPanel.prototype._on = function(el, type, fn) {
        if (!el) { return; }
        el.addEventListener(type, fn);
        this._handlers.push([el, type, fn]);
    };

    AccessPanel.prototype.render = function() {
        const els = this._els;
        const s = this._ac.getState();
        const d = describeAccess(s);
        if (els.role) { els.role.textContent = d.roleLabel; els.role.setAttribute('data-role', s.role); }
        if (els.detail) { els.detail.textContent = d.detail; els.detail.hidden = !d.detail; }
        if (els.manage) { els.manage.hidden = !d.showManage; }
        if (els.revoke) { els.revoke.hidden = !d.showRevoke; }
        if (els.release) { els.release.hidden = !d.showRelease; }
        if (els.notice) { els.notice.textContent = s.notice || ''; }
        const m = this._modal;
        if (m.request) {
            m.request.disabled = !d.canRequest;
            m.request.textContent = d.canRequest ? 'Запросить' : 'Запрос отправлен';
        }
        // Права появились, пока окно открыто (мастер разрешил): окно больше не нужно
        if (this._ac.canControl() && this.isModalOpen()) { this.closeModal(); }
    };

    AccessPanel.prototype.destroy = function() {
        for (let i = 0; i < this._handlers.length; i++) {
            const h = this._handlers[i];
            h[0].removeEventListener(h[1], h[2]);
        }
        this._handlers = [];
        if (this._off) { this._off(); this._off = null; }
    };

    // ── Запрос управления у мастера (виден в любом режиме) ────────────────

    /**
     * @param {AccessControl} ac
     * @param {{root, text, allow, deny}} els
     */
    function AccessRequestDialog(ac, els) {
        this._ac = ac;
        this._els = els;
        const self = this;
        const answer = function(allow) {
            const p = ac.getState().pending;
            if (p) { ac.decide(p.request_id, allow); }
        };
        if (els.allow) { els.allow.addEventListener('click', function() { answer(true); }); }
        if (els.deny) { els.deny.addEventListener('click', function() { answer(false); }); }
        ac.onChange(function() { self.render(); });
        this.render();
    }

    AccessRequestDialog.prototype.render = function() {
        const p = this._ac.isMaster() ? this._ac.getState().pending : null;
        if (!this._els.root) { return; }
        this._els.root.hidden = !p;
        if (p && this._els.text) {
            this._els.text.textContent = p.remote_addr + ' просит управление приёмником';
        }
    };

    AccessControl.describeAccess = describeAccess;
    AccessControl.AccessPanel = AccessPanel;
    AccessControl.AccessRequestDialog = AccessRequestDialog;

    if (typeof window !== 'undefined') {
        window.AccessControl = AccessControl;
    }

    if (typeof module !== 'undefined' && module.exports) { // eslint-disable-line no-undef
        module.exports = { // eslint-disable-line no-undef
            AccessControl: AccessControl,
            AccessPanel: AccessPanel,
            AccessRequestDialog: AccessRequestDialog,
            describeAccess: describeAccess,
        };
    }
})();
