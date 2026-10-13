package handlers

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/access"
)

// presenceRecorder запоминает подключения и отключения, о которых сообщил хаб.
type presenceRecorder struct {
	mu           sync.Mutex
	connected    []access.ClientInfo
	disconnected []access.ClientInfo
}

func (p *presenceRecorder) ClientConnected(c access.ClientInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connected = append(p.connected, c)
}

func (p *presenceRecorder) ClientDisconnected(c access.ClientInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disconnected = append(p.disconnected, c)
}

// snapshot returns copies of the connected and disconnected lists.
func (p *presenceRecorder) snapshot() ([]access.ClientInfo, []access.ClientInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]access.ClientInfo(nil), p.connected...), append([]access.ClientInfo(nil), p.disconnected...)
}

func registerClientInfo(hub *SSEHub, info access.ClientInfo) *sseClient {
	client := &sseClient{events: make(chan SSEEvent, clientEventBufferSize), info: info}
	hub.register <- client
	return client
}

func TestSSEHub_SendWhere(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	master := registerClientInfo(hub, access.ClientInfo{Session: "m", RemoteAddr: "127.0.0.1:1"})
	slave := registerClientInfo(hub, access.ClientInfo{Session: "s", RemoteAddr: "192.168.1.5:2"})
	waitForClientCount(t, hub, 2)

	hub.SendWhere("control_request", []byte(`{}`), func(c access.ClientInfo) bool { return c.Session == "m" })

	select {
	case e := <-master.events:
		if e.Type != "control_request" {
			t.Errorf("event=%q", e.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("matching client got nothing")
	}
	select {
	case e := <-slave.events:
		t.Errorf("non-matching client got %q", e.Type)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSSEHub_PresenceFromHTTPRequest(t *testing.T) {
	hub := NewSSEHub()
	presence := &presenceRecorder{}
	hub.SetPresence(presence)
	ctx := t.Context()
	go hub.Run(ctx)

	server := httptest.NewServer(access.NewManager("pw").EnsureSession(hub))
	defer server.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	req.AddCookie(&http.Cookie{Name: access.SessionCookie, Value: accessTestSession})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	waitForClientCount(t, hub, 1)
	resp.Body.Close()
	waitForClientCount(t, hub, 0)

	connected, disconnected := presence.snapshot()
	if len(connected) != 1 || len(disconnected) != 1 {
		t.Fatalf("connected=%v disconnected=%v, want one each", connected, disconnected)
	}
	c := connected[0]
	if c.Session != accessTestSession || c.RemoteAddr == "" {
		t.Errorf("client info=%+v, want session from cookie and remote address", c)
	}
	if disconnected[0] != c {
		t.Errorf("disconnected %+v, want same client %+v", disconnected[0], c)
	}
}
