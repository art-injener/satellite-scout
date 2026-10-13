package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/art-injener/satellite-scout/internal/config"
)

func newTestStore(t *testing.T) *config.Store {
	t.Helper()
	dir := t.TempDir()
	store := config.NewStore(filepath.Join(dir, "config.json"))
	store.Set(config.DefaultConfig())
	if err := store.Save(); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	return store
}

func TestSettingsHandler_Get(t *testing.T) {
	store := newTestStore(t)
	h := NewSettingsHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	w := httptest.NewRecorder()
	h.Get(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got config.Config
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != config.CurrentVersion {
		t.Errorf("Version = %d, want %d", got.Version, config.CurrentVersion)
	}
	if got.Station.Observer.Lat == 0 {
		t.Error("Observer.Lat is zero — defaults missing")
	}
}

func TestSettingsHandler_UpdateHappyPath(t *testing.T) {
	store := newTestStore(t)
	h := NewSettingsHandler(store)

	updated := config.DefaultConfig()
	updated.Station.Observer.Lat = 12.34
	updated.UI.Theme = "classic"
	body, _ := json.Marshal(updated)

	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Update(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var ack SettingsUpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ack.Status != "ok" {
		t.Errorf("status = %q, want ok", ack.Status)
	}
	if len(ack.RequiresRestart) != 0 {
		t.Errorf("observer/theme changes do not require restart, got %v", ack.RequiresRestart)
	}

	if got := store.Get().Station.Observer.Lat; got != 12.34 {
		t.Errorf("store Lat = %f, want 12.34", got)
	}
}

func TestSettingsHandler_UpdateRequiresRestartOnPort(t *testing.T) {
	store := newTestStore(t)
	h := NewSettingsHandler(store)

	updated := config.DefaultConfig()
	updated.Server.Port = "9090"
	body, _ := json.Marshal(updated)

	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Update(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	var ack SettingsUpdateResponse
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !contains(ack.RequiresRestart, "server.port") {
		t.Errorf("RequiresRestart = %v, want server.port", ack.RequiresRestart)
	}
}

func TestSettingsHandler_UpdateInvalidLatReturns400(t *testing.T) {
	store := newTestStore(t)
	h := NewSettingsHandler(store)

	bad := config.DefaultConfig()
	bad.Station.Observer.Lat = 100.0
	body, _ := json.Marshal(bad)

	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Update(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	var verr SettingsValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&verr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, e := range verr.Errors {
		if e.Field == "station.observer.lat" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected lat error, got %+v", verr)
	}

	// Сторонной путь: store не должен быть мутирован при невалидном PUT.
	if got := store.Get().Station.Observer.Lat; got == 100.0 {
		t.Errorf("invalid value persisted: Lat = %f", got)
	}
}

func TestSettingsHandler_UpdateRejectsUnknownField(t *testing.T) {
	store := newTestStore(t)
	h := NewSettingsHandler(store)

	body := []byte(`{"_version":1, "future_field": "boom", "server":{"port":"8080"}, "station":{}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Update(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown field", resp.StatusCode)
	}
}

func TestSettingsHandler_UpdateInvalidJSON(t *testing.T) {
	store := newTestStore(t)
	h := NewSettingsHandler(store)

	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	h.Update(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func storeWithPassword(t *testing.T, password string) *config.Store {
	t.Helper()
	store := newTestStore(t)
	if err := store.Update(func(c *config.Config) error {
		c.Server.MasterPassword = password
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestSettingsHandler_GetHidesMasterPassword(t *testing.T) {
	h := NewSettingsHandler(storeWithPassword(t, "top-secret"))

	w := httptest.NewRecorder()
	h.Get(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))

	body := w.Body.Bytes()
	if bytes.Contains(body, []byte("top-secret")) || bytes.Contains(body, []byte("master_password")) {
		t.Errorf("master password leaked: %s", w.Body.String())
	}
}

func TestSettingsHandler_UpdateKeepsMasterPassword(t *testing.T) {
	cases := map[string]string{
		"omitted by settings modal": "",
		"sent by client":            "attacker-choice",
	}
	for name, sent := range cases {
		t.Run(name, func(t *testing.T) {
			store := storeWithPassword(t, "top-secret")
			h := NewSettingsHandler(store)

			updated := config.DefaultConfig()
			updated.UI.Theme = "classic"
			updated.Server.MasterPassword = sent
			body, _ := json.Marshal(updated)

			w := httptest.NewRecorder()
			h.Update(w, httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader(body)))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if got := store.Get().Server.MasterPassword; got != "top-secret" {
				t.Errorf("password=%q, want unchanged", got)
			}
			if got := store.Get().UI.Theme; got != "classic" {
				t.Errorf("other settings must still apply, theme=%q", got)
			}
		})
	}
}

func contains(arr []string, s string) bool {
	return slices.Contains(arr, s)
}
