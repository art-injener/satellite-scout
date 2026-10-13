package access

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

// sentEvent — событие, переданное менеджером в SSE.
type sentEvent struct {
	name  string
	data  []byte
	match func(ClientInfo) bool
}

// fakeNotifier копит события; адресатов проверяет по функции отбора, как SSE-хаб.
type fakeNotifier struct {
	mu     sync.Mutex
	events []sentEvent
}

func (n *fakeNotifier) SendWhere(name string, data []byte, match func(ClientInfo) bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, sentEvent{name: name, data: data, match: match})
}

// received возвращает имена событий, дошедших до клиента.
func (n *fakeNotifier) received(c ClientInfo) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var names []string
	for _, e := range n.events {
		if e.match(c) {
			names = append(names, e.name)
		}
	}
	return names
}

// waitEvent ждёт событие у клиента и возвращает его данные.
func (n *fakeNotifier) waitEvent(t *testing.T, c ClientInfo, name string) []byte {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		n.mu.Lock()
		for _, e := range n.events {
			if e.name == name && e.match(c) {
				n.mu.Unlock()
				return e.data
			}
		}
		n.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("client %+v did not receive %q, got %v", c, name, n.received(c))
	return nil
}

func (n *fakeNotifier) hasEvent(c ClientInfo, name string) bool {
	return slices.Contains(n.received(c), name)
}

var (
	localMaster = ClientInfo{Session: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RemoteAddr: "127.0.0.1:6000"}
	slaveA      = ClientInfo{Session: sessionA, RemoteAddr: "192.168.1.20:51000"}
	slaveB      = ClientInfo{Session: sessionB, RemoteAddr: "192.168.1.21:52000"}
)

func clientRequest(c ClientInfo) *http.Request {
	return request(c.RemoteAddr, c.Session)
}

// controlSetup — менеджер с подключёнными мастером и двумя слейвами.
func controlSetup(t *testing.T) (*Manager, *fakeNotifier) {
	t.Helper()
	m, _ := newTestManager()
	n := &fakeNotifier{}
	m.SetNotifier(n)
	for _, c := range []ClientInfo{localMaster, slaveA, slaveB} {
		m.ClientConnected(c)
	}
	return m, n
}

func pendingID(t *testing.T, n *fakeNotifier) string {
	t.Helper()
	var req PendingRequest
	if err := json.Unmarshal(n.waitEvent(t, localMaster, EventControlRequest), &req); err != nil {
		t.Fatalf("decode control_request: %v", err)
	}
	if req.ID == "" {
		t.Fatal("control_request without id")
	}
	return req.ID
}

func TestRequestControl_NoMasterOnline(t *testing.T) {
	m, _ := newTestManager()
	m.SetNotifier(&fakeNotifier{})
	m.ClientConnected(slaveA)
	if err := m.RequestControl(clientRequest(slaveA)); !errors.Is(err, ErrNoMasterOnline) {
		t.Errorf("err=%v, want ErrNoMasterOnline", err)
	}
}

func TestRequestControl_PasswordMasterCountsAsOnline(t *testing.T) {
	m, _ := newTestManager()
	m.SetNotifier(&fakeNotifier{})
	m.ClientConnected(slaveA)
	m.ClientConnected(slaveB)
	if err := m.Login(clientRequest(slaveB), testPassword); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestControl(clientRequest(slaveA)); err != nil {
		t.Errorf("err=%v, want request delivered to password master", err)
	}
}

func TestRequestControl_NotifiesOnlyMasters(t *testing.T) {
	m, n := controlSetup(t)
	if err := m.RequestControl(clientRequest(slaveA)); err != nil {
		t.Fatalf("RequestControl: %v", err)
	}
	var req PendingRequest
	if err := json.Unmarshal(n.waitEvent(t, localMaster, EventControlRequest), &req); err != nil {
		t.Fatal(err)
	}
	if req.RemoteAddr != "192.168.1.20" {
		t.Errorf("remote_addr=%q, want requester host", req.RemoteAddr)
	}
	if n.hasEvent(slaveB, EventControlRequest) || n.hasEvent(slaveA, EventControlRequest) {
		t.Error("control_request must reach only masters")
	}
}

func TestRequestControl_OnePendingAtATime(t *testing.T) {
	m, _ := controlSetup(t)
	if err := m.RequestControl(clientRequest(slaveA)); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestControl(clientRequest(slaveA)); err != nil {
		t.Errorf("repeat from same requester: err=%v, want nil", err)
	}
	if err := m.RequestControl(clientRequest(slaveB)); !errors.Is(err, ErrRequestPending) {
		t.Errorf("err=%v, want ErrRequestPending", err)
	}
}

func TestRequestControl_MasterNeedsNoRequest(t *testing.T) {
	m, n := controlSetup(t)
	if err := m.RequestControl(clientRequest(localMaster)); err != nil {
		t.Errorf("err=%v", err)
	}
	if n.hasEvent(localMaster, EventControlRequest) {
		t.Error("master request must not create a pending request")
	}
}

func TestDecide_AllowGrantsControl(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	id := pendingID(t, n)

	if err := m.Decide(id, true); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got := m.Role(clientRequest(slaveA)); got != RoleController {
		t.Errorf("role=%v, want controller", got)
	}
	if got := m.Role(clientRequest(slaveB)); got != RoleViewer {
		t.Errorf("other slave role=%v, want viewer", got)
	}
	n.waitEvent(t, slaveA, EventControlGranted)
	n.waitEvent(t, localMaster, EventControlRequestClosed)

	h := m.RequireControl(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	h(rec, clientRequest(slaveA))
	if rec.Code != http.StatusNoContent {
		t.Errorf("controller: status=%d, want 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	h(rec, clientRequest(slaveB))
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer: status=%d, want 403", rec.Code)
	}
}

func TestDecide_ControllerIsNotMaster(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	_ = m.Decide(pendingID(t, n), true)

	h := m.RequireMaster(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	h(rec, clientRequest(slaveA))
	if rec.Code != http.StatusForbidden {
		t.Errorf("controller on master-only route: status=%d, want 403", rec.Code)
	}
}

func TestDecide_Deny(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	if err := m.Decide(pendingID(t, n), false); err != nil {
		t.Fatal(err)
	}
	if got := m.Role(clientRequest(slaveA)); got != RoleViewer {
		t.Errorf("role=%v, want viewer", got)
	}
	n.waitEvent(t, slaveA, EventControlDenied)
	if err := m.RequestControl(clientRequest(slaveB)); err != nil {
		t.Errorf("after decision a new request must be accepted: %v", err)
	}
}

func TestDecide_UnknownRequest(t *testing.T) {
	m, n := controlSetup(t)
	if err := m.Decide("nope", true); !errors.Is(err, ErrNoSuchRequest) {
		t.Errorf("no pending: err=%v, want ErrNoSuchRequest", err)
	}
	_ = m.RequestControl(clientRequest(slaveA))
	pendingID(t, n)
	if err := m.Decide("stale-id", true); !errors.Is(err, ErrNoSuchRequest) {
		t.Errorf("stale id: err=%v, want ErrNoSuchRequest", err)
	}
	if got := m.Role(clientRequest(slaveA)); got != RoleViewer {
		t.Error("stale decision must not grant control")
	}
}

func TestRequestControl_Timeout(t *testing.T) {
	m, n := controlSetup(t)
	m.requestTimeout = 20 * time.Millisecond
	_ = m.RequestControl(clientRequest(slaveA))

	var denied DeniedEvent
	if err := json.Unmarshal(n.waitEvent(t, slaveA, EventControlDenied), &denied); err != nil {
		t.Fatal(err)
	}
	if denied.Reason != DenyTimeout {
		t.Errorf("reason=%q, want %q", denied.Reason, DenyTimeout)
	}
	n.waitEvent(t, localMaster, EventControlRequestClosed)
	if err := m.RequestControl(clientRequest(slaveB)); err != nil {
		t.Errorf("after timeout a new request must be accepted: %v", err)
	}
}

func TestGrant_OnlyOneController(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	_ = m.Decide(pendingID(t, n), true)

	n.mu.Lock()
	n.events = nil
	n.mu.Unlock()
	_ = m.RequestControl(clientRequest(slaveB))
	_ = m.Decide(pendingID(t, n), true)

	if got := m.Role(clientRequest(slaveA)); got != RoleViewer {
		t.Errorf("previous controller role=%v, want viewer", got)
	}
	if got := m.Role(clientRequest(slaveB)); got != RoleController {
		t.Errorf("new controller role=%v", got)
	}
	n.waitEvent(t, slaveA, EventControlRevoked)
}

func TestRevoke(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	_ = m.Decide(pendingID(t, n), true)

	m.Revoke()
	if got := m.Role(clientRequest(slaveA)); got != RoleViewer {
		t.Errorf("role=%v, want viewer", got)
	}
	n.waitEvent(t, slaveA, EventControlRevoked)
}

func TestRelease_ControllerGivesControlBack(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	_ = m.Decide(pendingID(t, n), true)

	m.Release(clientRequest(slaveA))
	if got := m.Role(clientRequest(slaveA)); got != RoleViewer {
		t.Errorf("role=%v, want viewer", got)
	}
	if s := m.Status(clientRequest(localMaster)); s.ControllerActive {
		t.Errorf("master status=%+v, want no controller", s)
	}
	n.waitEvent(t, localMaster, EventControlReleased)
	if n.hasEvent(slaveA, EventControlRevoked) {
		t.Error("released control is not a revoke")
	}
}

func TestRelease_OnlyControllerCanRelease(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	_ = m.Decide(pendingID(t, n), true)

	m.Release(clientRequest(slaveB))
	m.Release(clientRequest(localMaster))
	if got := m.Role(clientRequest(slaveA)); got != RoleController {
		t.Errorf("role=%v, want controller kept", got)
	}
	if n.hasEvent(localMaster, EventControlReleased) {
		t.Error("no release happened")
	}
}

func TestDisconnect_NotifiesMastersWhenControllerLeaves(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	_ = m.Decide(pendingID(t, n), true)

	m.ClientDisconnected(slaveA)
	n.waitEvent(t, localMaster, EventControlReleased)
}

func TestDisconnect_DropsGrantWithLastTab(t *testing.T) {
	m, n := controlSetup(t)
	secondTab := ClientInfo{Session: slaveA.Session, RemoteAddr: "192.168.1.20:51001"}
	m.ClientConnected(secondTab)
	_ = m.RequestControl(clientRequest(slaveA))
	_ = m.Decide(pendingID(t, n), true)

	m.ClientDisconnected(slaveA)
	if got := m.Role(clientRequest(slaveA)); got != RoleController {
		t.Errorf("one tab still open: role=%v, want controller", got)
	}
	m.ClientDisconnected(secondTab)
	if got := m.Role(clientRequest(slaveA)); got != RoleViewer {
		t.Errorf("all tabs closed: role=%v, want viewer", got)
	}
}

func TestDisconnect_CancelsPendingRequest(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	id := pendingID(t, n)

	m.ClientDisconnected(slaveA)
	n.waitEvent(t, localMaster, EventControlRequestClosed)
	if err := m.Decide(id, true); !errors.Is(err, ErrNoSuchRequest) {
		t.Errorf("err=%v, want ErrNoSuchRequest after requester left", err)
	}
}

func TestStatus(t *testing.T) {
	m, n := controlSetup(t)
	_ = m.RequestControl(clientRequest(slaveA))
	id := pendingID(t, n)

	ms := m.Status(clientRequest(localMaster))
	if ms.Pending == nil || ms.Pending.ID != id {
		t.Errorf("master must see pending request, got %+v", ms.Pending)
	}
	if s := m.Status(clientRequest(slaveA)); !s.RequestPending || s.Pending != nil {
		t.Errorf("requester status=%+v, want own request flag without details", s)
	}
	if s := m.Status(clientRequest(slaveB)); s.RequestPending || s.Pending != nil {
		t.Errorf("other slave status=%+v, want nothing pending", s)
	}

	_ = m.Decide(id, true)
	ms = m.Status(clientRequest(localMaster))
	if !ms.ControllerActive || ms.ControllerAddr != "192.168.1.20" {
		t.Errorf("master status=%+v, want controller 192.168.1.20", ms)
	}
	if s := m.Status(clientRequest(slaveB)); !s.ControllerActive || s.ControllerAddr != "" {
		t.Errorf("viewer status=%+v, want controller flag without address", s)
	}
}
