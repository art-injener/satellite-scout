// Package access решает, кто может управлять приёмником и менять настройки.
//
// Мастер — запрос с машины сервера (loopback) или сессия, вошедшая по паролю мастера.
// Остальные — зрители. Сессия — случайный идентификатор в cookie, его выдаёт сервер.
package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

var (
	ErrBadPassword     = errors.New("wrong master password")
	ErrTooManyAttempts = errors.New("too many failed login attempts")
	ErrNoSession       = errors.New("no session")
)

// SessionCookie — имя cookie с идентификатором сессии.
const SessionCookie = "scout_session"

const (
	maxFailedLogins   = 5
	failedLoginWindow = time.Minute
	sessionIDBytes    = 16
)

// Role — права запроса. Порядок значим: старшая роль включает права младшей.
type Role int

const (
	RoleViewer     Role = iota
	RoleController      // слейв с разрешением мастера: управляет приёмником, но не настройками
	RoleMaster
)

func (r Role) String() string {
	switch r {
	case RoleMaster:
		return "master"
	case RoleController:
		return "controller"
	case RoleViewer:
		return "viewer"
	}
	return "viewer"
}

// Manager хранит сессии мастеров, разрешение на управление и неудачные попытки входа.
type Manager struct {
	passwordHash   [sha256.Size]byte
	hasPassword    bool
	now            func() time.Time
	requestTimeout time.Duration

	mu       sync.Mutex
	masters  map[string]bool        // сессии, вошедшие по паролю
	failures map[string][]time.Time // неудачные попытки входа по адресу клиента

	notifier       Notifier
	online         map[ClientInfo]int // открытые SSE-подключения
	controller     string             // сессия с разрешением на управление
	controllerAddr string
	pending        *pendingRequest
}

// NewManager создаёт менеджер доступа. Пустой пароль не подходит ни к чему.
func NewManager(password string) *Manager {
	return &Manager{
		passwordHash:   sha256.Sum256([]byte(password)),
		hasPassword:    password != "",
		now:            time.Now,
		requestTimeout: defaultRequestTimeout,
		masters:        make(map[string]bool),
		failures:       make(map[string][]time.Time),
		online:         make(map[ClientInfo]int),
	}
}

// Role определяет права запроса.
func (m *Manager) Role(r *http.Request) Role {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.roleLocked(r)
}

func (m *Manager) roleLocked(r *http.Request) Role {
	if isLoopback(r) {
		return RoleMaster
	}
	sid := SessionID(r)
	switch {
	case sid == "":
		return RoleViewer
	case m.masters[sid]:
		return RoleMaster
	case sid == m.controller:
		return RoleController
	default:
		return RoleViewer
	}
}

// Login делает сессию запроса мастером при верном пароле.
// После maxFailedLogins неудач за failedLoginWindow адрес клиента временно не принимается.
func (m *Manager) Login(r *http.Request, password string) error {
	sid := SessionID(r)
	if sid == "" {
		return ErrNoSession
	}
	addr := clientHost(r)
	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()

	recent := m.failures[addr][:0]
	for _, t := range m.failures[addr] {
		if now.Sub(t) < failedLoginWindow {
			recent = append(recent, t)
		}
	}
	m.failures[addr] = recent
	if len(recent) >= maxFailedLogins {
		return ErrTooManyAttempts
	}

	// Сравниваются хэши одной длины: время сравнения не выдаёт длину пароля.
	got := sha256.Sum256([]byte(password))
	if !m.hasPassword || subtle.ConstantTimeCompare(got[:], m.passwordHash[:]) != 1 {
		m.failures[addr] = append(recent, now)
		return ErrBadPassword
	}
	delete(m.failures, addr)
	m.masters[sid] = true
	return nil
}

// RequireMaster пропускает к обработчику только мастера, остальным отвечает 403.
func (m *Manager) RequireMaster(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.Role(r) != RoleMaster {
			http.Error(w, "master rights required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

type sessionKey struct{}

// EnsureSession выдаёт cookie сессии, если её нет или она испорчена.
// Идентификатор сразу доступен обработчику через SessionID.
func (m *Manager) EnsureSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(SessionCookie); err == nil && validSessionID(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		sid := newSessionID()
		// Без Secure: станция открывается по HTTP в локальной сети, с Secure браузер не вернёт cookie.
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // см. комментарий выше
			Name:     SessionCookie,
			Value:    sid,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sid)))
	})
}

// SessionID возвращает идентификатор сессии запроса или пустую строку.
func SessionID(r *http.Request) string {
	if sid, ok := r.Context().Value(sessionKey{}).(string); ok {
		return sid
	}
	if c, err := r.Cookie(SessionCookie); err == nil && validSessionID(c.Value) {
		return c.Value
	}
	return ""
}

func newSessionID() string {
	b := make([]byte, sessionIDBytes)
	_, _ = rand.Read(b) // crypto/rand.Read не возвращает ошибку
	return hex.EncodeToString(b)
}

func validSessionID(s string) bool {
	if len(s) != 2*sessionIDBytes {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// isLoopback — запрос пришёл с той же машины. Заголовкам прокси не доверяем:
// обратный прокси на этой машине сделал бы мастером любого.
func isLoopback(r *http.Request) bool {
	return isLoopbackAddr(r.RemoteAddr)
}

func isLoopbackAddr(remoteAddr string) bool {
	ip := net.ParseIP(hostOf(remoteAddr))
	return ip != nil && ip.IsLoopback()
}

func clientHost(r *http.Request) string {
	return hostOf(r.RemoteAddr)
}

func hostOf(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
