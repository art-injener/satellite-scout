package access

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testPassword = "s3cret"

// fakeClock — управляемое время для проверки окна ограничения попыток.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestManager() (*Manager, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
	m := NewManager(testPassword)
	m.now = clock.now
	return m, clock
}

// request собирает запрос с адресом клиента и, если задана, сессией.
func request(remoteAddr, session string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	if session != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: session})
	}
	return r
}

const (
	remoteAddr = "192.168.1.20:51000"
	sessionA   = "0123456789abcdef0123456789abcdef"
	sessionB   = "fedcba9876543210fedcba9876543210"
)

func TestRole_LoopbackIsMaster(t *testing.T) {
	m, _ := newTestManager()
	for _, addr := range []string{"127.0.0.1:5000", "[::1]:5000"} {
		if got := m.Role(request(addr, "")); got != RoleMaster {
			t.Errorf("%s: role=%v, want master", addr, got)
		}
	}
}

func TestRole_RemoteIsViewer(t *testing.T) {
	m, _ := newTestManager()
	for _, addr := range []string{remoteAddr, "[fe80::1]:5000", "garbage"} {
		if got := m.Role(request(addr, sessionA)); got != RoleViewer {
			t.Errorf("%s: role=%v, want viewer", addr, got)
		}
	}
}

func TestLogin_CorrectPasswordMakesSessionMaster(t *testing.T) {
	m, _ := newTestManager()
	if err := m.Login(request(remoteAddr, sessionA), testPassword); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := m.Role(request(remoteAddr, sessionA)); got != RoleMaster {
		t.Errorf("logged-in session role=%v, want master", got)
	}
	if got := m.Role(request(remoteAddr, sessionB)); got != RoleViewer {
		t.Errorf("other session role=%v, want viewer", got)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	m, _ := newTestManager()
	err := m.Login(request(remoteAddr, sessionA), "nope")
	if !errors.Is(err, ErrBadPassword) {
		t.Fatalf("err=%v, want ErrBadPassword", err)
	}
	if got := m.Role(request(remoteAddr, sessionA)); got != RoleViewer {
		t.Errorf("role=%v, want viewer", got)
	}
}

func TestLogin_WithoutSession(t *testing.T) {
	m, _ := newTestManager()
	if err := m.Login(request(remoteAddr, ""), testPassword); !errors.Is(err, ErrNoSession) {
		t.Errorf("err=%v, want ErrNoSession", err)
	}
}

func TestLogin_EmptyPasswordNeverMatches(t *testing.T) {
	m := NewManager("")
	if err := m.Login(request(remoteAddr, sessionA), ""); !errors.Is(err, ErrBadPassword) {
		t.Errorf("err=%v, want ErrBadPassword", err)
	}
}

func TestLogin_RateLimitPerAddress(t *testing.T) {
	m, clock := newTestManager()
	for i := range maxFailedLogins {
		if err := m.Login(request(remoteAddr, sessionA), "nope"); !errors.Is(err, ErrBadPassword) {
			t.Fatalf("attempt %d: err=%v, want ErrBadPassword", i+1, err)
		}
	}
	if err := m.Login(request(remoteAddr, sessionA), testPassword); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("err=%v, want ErrTooManyAttempts even with correct password", err)
	}
	if err := m.Login(request("192.168.1.21:4000", sessionB), testPassword); err != nil {
		t.Errorf("other address must not be limited: %v", err)
	}

	clock.advance(failedLoginWindow)
	if err := m.Login(request(remoteAddr, sessionA), testPassword); err != nil {
		t.Errorf("after window: %v", err)
	}
}

func TestEnsureSession_IssuesCookieOnce(t *testing.T) {
	m, _ := newTestManager()
	var seen string
	h := m.EnsureSession(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = SessionID(r)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request(remoteAddr, ""))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookie {
		t.Fatalf("cookies=%v, want one %s", cookies, SessionCookie)
	}
	c := cookies[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Errorf("cookie flags: HttpOnly=%v SameSite=%v Path=%q", c.HttpOnly, c.SameSite, c.Path)
	}
	if seen != c.Value || !validSessionID(seen) {
		t.Errorf("handler saw session %q, cookie %q", seen, c.Value)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request(remoteAddr, sessionA))
	if len(rec.Result().Cookies()) != 0 {
		t.Error("valid session must not be reissued")
	}
	if seen != sessionA {
		t.Errorf("handler saw %q, want existing %q", seen, sessionA)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, request(remoteAddr, "not-a-session"))
	if len(rec.Result().Cookies()) != 1 {
		t.Error("malformed session must be replaced")
	}
}

func TestRequireMaster(t *testing.T) {
	m, _ := newTestManager()
	h := m.RequireMaster(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	h(rec, request(remoteAddr, sessionA))
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer: status=%d, want 403", rec.Code)
	}

	rec = httptest.NewRecorder()
	h(rec, request("127.0.0.1:5000", ""))
	if rec.Code != http.StatusNoContent {
		t.Errorf("local master: status=%d, want 204", rec.Code)
	}

	_ = m.Login(request(remoteAddr, sessionA), testPassword)
	rec = httptest.NewRecorder()
	h(rec, request(remoteAddr, sessionA))
	if rec.Code != http.StatusNoContent {
		t.Errorf("password master: status=%d, want 204", rec.Code)
	}
}

func TestRole_String(t *testing.T) {
	if RoleMaster.String() != "master" || RoleViewer.String() != "viewer" {
		t.Errorf("got %q, %q", RoleMaster.String(), RoleViewer.String())
	}
}
