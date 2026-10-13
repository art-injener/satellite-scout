package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/art-injener/satellite-scout/internal/access"
)

const (
	accessTestRemote  = "192.168.1.30:40000"
	accessTestSession = "00112233445566778899aabbccddeeff"
)

func accessRequest(method, body, remote string) *http.Request {
	r := httptest.NewRequest(method, "/api/access", strings.NewReader(body))
	r.RemoteAddr = remote
	r.AddCookie(&http.Cookie{Name: access.SessionCookie, Value: accessTestSession})
	return r
}

func decodeRole(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var got AccessStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got.Role
}

func TestAccessHandler_StatusRole(t *testing.T) {
	h := NewAccessHandler(access.NewManager("pw"))

	rec := httptest.NewRecorder()
	h.Status(rec, accessRequest(http.MethodGet, "", accessTestRemote))
	if rec.Code != http.StatusOK || decodeRole(t, rec) != "viewer" {
		t.Errorf("remote: status=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Status(rec, accessRequest(http.MethodGet, "", "127.0.0.1:5000"))
	if decodeRole(t, rec) != "master" {
		t.Error("loopback must be master")
	}
}

func TestAccessHandler_Login(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"correct password", `{"password":"pw"}`, http.StatusNoContent},
		{"wrong password", `{"password":"nope"}`, http.StatusUnauthorized},
		{"invalid json", `{`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewAccessHandler(access.NewManager("pw"))
			rec := httptest.NewRecorder()
			h.Login(rec, accessRequest(http.MethodPost, tc.body, accessTestRemote))
			if rec.Code != tc.want {
				t.Errorf("status=%d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestAccessHandler_LoginMakesSessionMaster(t *testing.T) {
	h := NewAccessHandler(access.NewManager("pw"))
	h.Login(httptest.NewRecorder(), accessRequest(http.MethodPost, `{"password":"pw"}`, accessTestRemote))

	rec := httptest.NewRecorder()
	h.Status(rec, accessRequest(http.MethodGet, "", accessTestRemote))
	if decodeRole(t, rec) != "master" {
		t.Error("session must become master after login")
	}
}

func decodeStatus(t *testing.T, rec *httptest.ResponseRecorder) AccessStatusResponse {
	t.Helper()
	var got AccessStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

func decodeErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var got AccessErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got.Code
}

func TestAccessHandler_RequestWithoutMasterOnline(t *testing.T) {
	h := NewAccessHandler(access.NewManager("pw"))
	rec := httptest.NewRecorder()
	h.Request(rec, accessRequest(http.MethodPost, "", accessTestRemote))
	if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != "no_master_online" {
		t.Errorf("status=%d, want 409 no_master_online", rec.Code)
	}
}

func TestAccessHandler_RequestDecideFlow(t *testing.T) {
	m := access.NewManager("pw")
	m.ClientConnected(access.ClientInfo{Session: "", RemoteAddr: "127.0.0.1:7000"})
	h := NewAccessHandler(m)

	rec := httptest.NewRecorder()
	h.Request(rec, accessRequest(http.MethodPost, "", accessTestRemote))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request: status=%d, want 202", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Status(rec, accessRequest(http.MethodGet, "", "127.0.0.1:7000"))
	st := decodeStatus(t, rec)
	if st.Pending == nil || st.Pending.RemoteAddr != "192.168.1.30" {
		t.Fatalf("master status=%+v, want pending from 192.168.1.30", st)
	}

	other := httptest.NewRequest(http.MethodPost, "/api/access/request", nil)
	other.RemoteAddr = "192.168.1.31:1"
	other.AddCookie(&http.Cookie{Name: access.SessionCookie, Value: "ffeeddccbbaa99887766554433221100"})
	rec = httptest.NewRecorder()
	h.Request(rec, other)
	if rec.Code != http.StatusConflict || decodeErrorCode(t, rec) != "request_pending" {
		t.Errorf("second requester: status=%d, want 409 request_pending", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Decide(rec, accessRequest(http.MethodPost, `{"request_id":"stale","allow":true}`, "127.0.0.1:7000"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("stale decide: status=%d, want 404", rec.Code)
	}

	body := `{"request_id":"` + st.Pending.ID + `","allow":true}`
	rec = httptest.NewRecorder()
	h.Decide(rec, accessRequest(http.MethodPost, body, "127.0.0.1:7000"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("decide: status=%d, want 204", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Status(rec, accessRequest(http.MethodGet, "", accessTestRemote))
	if slave := decodeStatus(t, rec); slave.Role != "controller" || !slave.ControllerActive {
		t.Errorf("slave status=%+v, want controller", slave)
	}

	rec = httptest.NewRecorder()
	h.Request(rec, accessRequest(http.MethodPost, "", accessTestRemote))
	if rec.Code != http.StatusOK {
		t.Errorf("request while controlling: status=%d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.Release(rec, accessRequest(http.MethodPost, "", accessTestRemote))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("release: status=%d, want 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.Status(rec, accessRequest(http.MethodGet, "", accessTestRemote))
	if after := decodeStatus(t, rec); after.Role != "viewer" {
		t.Errorf("after release role=%q, want viewer", after.Role)
	}

	_ = m.RequestControl(accessRequest(http.MethodPost, "", accessTestRemote))
	rec = httptest.NewRecorder()
	h.Status(rec, accessRequest(http.MethodGet, "", "127.0.0.1:7000"))
	st = decodeStatus(t, rec)
	if err := m.Decide(st.Pending.ID, true); err != nil {
		t.Fatalf("second grant: %v", err)
	}

	rec = httptest.NewRecorder()
	h.Revoke(rec, accessRequest(http.MethodPost, "", "127.0.0.1:7000"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status=%d, want 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.Status(rec, accessRequest(http.MethodGet, "", accessTestRemote))
	if after := decodeStatus(t, rec); after.Role != "viewer" {
		t.Errorf("after revoke role=%q, want viewer", after.Role)
	}
}

func TestAccessHandler_DecideInvalidJSON(t *testing.T) {
	h := NewAccessHandler(access.NewManager("pw"))
	rec := httptest.NewRecorder()
	h.Decide(rec, accessRequest(http.MethodPost, `{`, "127.0.0.1:7000"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rec.Code)
	}
}

func TestAccessHandler_LoginRateLimited(t *testing.T) {
	h := NewAccessHandler(access.NewManager("pw"))
	var last int
	for range 6 {
		rec := httptest.NewRecorder()
		h.Login(rec, accessRequest(http.MethodPost, `{"password":"nope"}`, accessTestRemote))
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("status=%d after repeated failures, want 429", last)
	}
}
