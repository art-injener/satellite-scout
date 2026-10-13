package access

import (
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"time"
)

// События SSE о передаче управления.
const (
	EventControlRequest       = "control_request"        // мастерам: слейв просит управление
	EventControlRequestClosed = "control_request_closed" // мастерам: запрос решён, отозван или истёк
	EventControlGranted       = "control_granted"        // слейву: разрешение выдано
	EventControlDenied        = "control_denied"         // слейву: отказ или таймаут
	EventControlRevoked       = "control_revoked"        // слейву: разрешение отозвано
	EventControlReleased      = "control_released"       // мастерам: слейв отказался от управления или ушёл
)

// Причины отказа в EventControlDenied.
const (
	DenyRejected = "rejected"
	DenyTimeout  = "timeout"
)

const defaultRequestTimeout = 30 * time.Second

var (
	ErrNoMasterOnline = errors.New("no master online to approve the request")
	ErrRequestPending = errors.New("another control request is pending")
	ErrNoSuchRequest  = errors.New("no such control request")
)

// ClientInfo — одно SSE-подключение: сессия и адрес клиента.
type ClientInfo struct {
	Session    string
	RemoteAddr string
}

// Notifier доставляет событие тем SSE-подключениям, для которых match вернул true.
// match вызывается из горутины SSE-хаба и не должен брать блокировки менеджера.
type Notifier interface {
	SendWhere(event string, data []byte, match func(ClientInfo) bool)
}

// PendingRequest — запрос управления, ждущий решения мастера.
type PendingRequest struct {
	ID         string `json:"request_id"`
	RemoteAddr string `json:"remote_addr"`
}

// DeniedEvent — данные EventControlDenied.
type DeniedEvent struct {
	Reason string `json:"reason"`
}

type requestClosedEvent struct {
	ID string `json:"request_id"`
}

// Status — состояние доступа глазами клиента.
type Status struct {
	Role             Role
	ControllerActive bool
	ControllerAddr   string          // только мастеру
	Pending          *PendingRequest // только мастеру
	RequestPending   bool            // свой запрос ждёт ответа
}

type pendingRequest struct {
	PendingRequest

	session string
	timer   *time.Timer
}

type notification struct {
	event   string
	payload any
	match   func(ClientInfo) bool
}

// SetNotifier задаёт доставку событий SSE. Вызывать до запуска сервера.
func (m *Manager) SetNotifier(n Notifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifier = n
}

// ClientConnected учитывает открытое SSE-подключение.
func (m *Manager) ClientConnected(c ClientInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.online[c]++
}

// ClientDisconnected учитывает закрытое SSE-подключение. Когда у сессии не осталось
// подключений, её разрешение и ожидающий запрос снимаются.
func (m *Manager) ClientDisconnected(c ClientInfo) {
	m.mu.Lock()
	if m.online[c] <= 1 {
		delete(m.online, c)
	} else {
		m.online[c]--
	}
	var out []notification
	if c.Session != "" && !m.sessionOnlineLocked(c.Session) {
		if m.controller == c.Session {
			out = append(out, m.releaseLocked())
		}
		if p := m.pending; p != nil && p.session == c.Session {
			out = append(out, m.closePendingLocked())
		}
	}
	n := m.notifier
	m.mu.Unlock()
	dispatch(n, out)
}

// RequestControl отправляет мастерам запрос управления от сессии клиента.
// Мастеру и уже управляющему запрос не нужен: возвращается nil без события.
func (m *Manager) RequestControl(r *http.Request) error {
	sid := SessionID(r)
	if sid == "" {
		return ErrNoSession
	}
	m.mu.Lock()
	if m.roleLocked(r) != RoleViewer {
		m.mu.Unlock()
		return nil
	}
	if p := m.pending; p != nil {
		m.mu.Unlock()
		if p.session == sid {
			return nil
		}
		return ErrRequestPending
	}
	if !m.masterOnlineLocked() {
		m.mu.Unlock()
		return ErrNoMasterOnline
	}

	p := &pendingRequest{
		PendingRequest: PendingRequest{ID: newSessionID(), RemoteAddr: clientHost(r)},
		session:        sid,
	}
	p.timer = time.AfterFunc(m.requestTimeout, func() { m.expire(p.ID) })
	m.pending = p
	out := []notification{{EventControlRequest, p.PendingRequest, m.mastersMatchLocked()}}
	n := m.notifier
	m.mu.Unlock()
	dispatch(n, out)
	return nil
}

// Decide применяет решение мастера по запросу id. Новое разрешение отзывает прежнее.
func (m *Manager) Decide(id string, allow bool) error {
	m.mu.Lock()
	p := m.pending
	if p == nil || p.ID != id {
		m.mu.Unlock()
		return ErrNoSuchRequest
	}
	out := []notification{m.closePendingLocked()}
	if allow {
		if m.controller != "" && m.controller != p.session {
			out = append(out, notification{EventControlRevoked, struct{}{}, sessionMatch(m.controller)})
		}
		m.controller, m.controllerAddr = p.session, p.RemoteAddr
		out = append(out, notification{EventControlGranted, struct{}{}, sessionMatch(p.session)})
	} else {
		out = append(out, notification{EventControlDenied, DeniedEvent{Reason: DenyRejected}, sessionMatch(p.session)})
	}
	n := m.notifier
	m.mu.Unlock()
	dispatch(n, out)
	return nil
}

// Revoke забирает разрешение у управляющего слейва.
func (m *Manager) Revoke() {
	m.mu.Lock()
	if m.controller == "" {
		m.mu.Unlock()
		return
	}
	out := []notification{{EventControlRevoked, struct{}{}, sessionMatch(m.controller)}}
	m.controller, m.controllerAddr = "", ""
	n := m.notifier
	m.mu.Unlock()
	dispatch(n, out)
}

// Release возвращает управление: снимает разрешение, если оно у сессии запроса.
func (m *Manager) Release(r *http.Request) {
	sid := SessionID(r)
	m.mu.Lock()
	if sid == "" || m.controller != sid {
		m.mu.Unlock()
		return
	}
	out := []notification{m.releaseLocked()}
	n := m.notifier
	m.mu.Unlock()
	dispatch(n, out)
}

// releaseLocked снимает разрешение и возвращает уведомление мастерам.
func (m *Manager) releaseLocked() notification {
	m.controller, m.controllerAddr = "", ""
	return notification{EventControlReleased, struct{}{}, m.mastersMatchLocked()}
}

// Status возвращает состояние доступа для клиента запроса.
func (m *Manager) Status(r *http.Request) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{Role: m.roleLocked(r), ControllerActive: m.controller != ""}
	if s.Role == RoleMaster {
		s.ControllerAddr = m.controllerAddr
		if m.pending != nil {
			req := m.pending.PendingRequest
			s.Pending = &req
		}
	} else if m.pending != nil && m.pending.session == SessionID(r) {
		s.RequestPending = true
	}
	return s
}

// RequireControl пропускает мастера и управляющего слейва, остальным отвечает 403.
func (m *Manager) RequireControl(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.Role(r) < RoleController {
			http.Error(w, "control rights required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// expire отклоняет запрос, на который мастер не ответил вовремя.
func (m *Manager) expire(id string) {
	m.mu.Lock()
	p := m.pending
	if p == nil || p.ID != id {
		m.mu.Unlock()
		return
	}
	out := []notification{
		m.closePendingLocked(),
		{EventControlDenied, DeniedEvent{Reason: DenyTimeout}, sessionMatch(p.session)},
	}
	n := m.notifier
	m.mu.Unlock()
	dispatch(n, out)
}

// closePendingLocked снимает ожидающий запрос и возвращает уведомление мастерам.
func (m *Manager) closePendingLocked() notification {
	p := m.pending
	p.timer.Stop()
	m.pending = nil
	return notification{EventControlRequestClosed, requestClosedEvent{ID: p.ID}, m.mastersMatchLocked()}
}

func (m *Manager) sessionOnlineLocked(sid string) bool {
	for c := range m.online {
		if c.Session == sid {
			return true
		}
	}
	return false
}

func (m *Manager) masterOnlineLocked() bool {
	for c := range m.online {
		if isLoopbackAddr(c.RemoteAddr) || m.masters[c.Session] {
			return true
		}
	}
	return false
}

// mastersMatchLocked отбирает подключения мастеров по снимку сессий: функция вызывается
// из горутины SSE-хаба и не должна брать блокировку менеджера.
func (m *Manager) mastersMatchLocked() func(ClientInfo) bool {
	masters := maps.Clone(m.masters)
	return func(c ClientInfo) bool {
		return isLoopbackAddr(c.RemoteAddr) || masters[c.Session]
	}
}

func sessionMatch(sid string) func(ClientInfo) bool {
	return func(c ClientInfo) bool { return c.Session == sid }
}

// dispatch отправляет уведомления вне блокировки и вне вызывающей горутины:
// ClientDisconnected вызывается из горутины SSE-хаба, синхронная отправка в хаб её заблокировала бы.
func dispatch(n Notifier, out []notification) {
	if n == nil || len(out) == 0 {
		return
	}
	go func() {
		for _, e := range out {
			data, err := json.Marshal(e.payload)
			if err != nil {
				slog.Error("access: encode event", "event", e.event, "error", err)
				continue
			}
			n.SendWhere(e.event, data, e.match)
		}
	}()
}
