package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeExclusionStore — мок хранилища исключений для тестов хендлера.
type fakeExclusionStore struct {
	added []int
}

func (f *fakeExclusionStore) Add(norad int) error {
	f.added = append(f.added, norad)
	return nil
}

// fakeInvalidator считает вызовы сброса кеша.
type fakeInvalidator struct {
	calls int
}

func (f *fakeInvalidator) InvalidateCache() { f.calls++ }

// fakeRefresher считает вызовы форсированного обновления группы.
type fakeRefresher struct {
	calls int
}

func (f *fakeRefresher) ForceGroupUpdate() { f.calls++ }

func TestExclusionsHandler_Add_OK(t *testing.T) {
	store := &fakeExclusionStore{}
	inv := &fakeInvalidator{}
	ref := &fakeRefresher{}
	h := NewExclusionsHandler(store, inv, ref)

	req := httptest.NewRequest(http.MethodPost, "/api/exclusions", strings.NewReader(`{"norad_id":25544}`))
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(store.added) != 1 || store.added[0] != 25544 {
		t.Errorf("store.added = %v, want [25544]", store.added)
	}
	if inv.calls != 1 {
		t.Errorf("invalidator calls = %d, want 1", inv.calls)
	}
	if ref.calls != 1 {
		t.Errorf("refresher calls = %d, want 1", ref.calls)
	}
}

func TestExclusionsHandler_Add_InvalidNorad(t *testing.T) {
	store := &fakeExclusionStore{}
	h := NewExclusionsHandler(store, &fakeInvalidator{}, &fakeRefresher{})

	req := httptest.NewRequest(http.MethodPost, "/api/exclusions", strings.NewReader(`{"norad_id":0}`))
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(store.added) != 0 {
		t.Errorf("nothing must be added for invalid norad, got %v", store.added)
	}
}

func TestExclusionsHandler_Add_BadBody(t *testing.T) {
	h := NewExclusionsHandler(&fakeExclusionStore{}, &fakeInvalidator{}, &fakeRefresher{})

	req := httptest.NewRequest(http.MethodPost, "/api/exclusions", strings.NewReader(`not-json`))
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
