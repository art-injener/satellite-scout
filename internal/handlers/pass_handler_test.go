package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satellite-scout/internal/tracker"
	"github.com/stretchr/testify/require"
)

// --- Mock сервисы ---

// mockPassService — мок PassService для тестирования.
type mockPassService struct {
	passes    []*tracker.Pass
	allPasses []*tracker.Pass
	err       error
}

func (m *mockPassService) GetPasses(group string, hours int, minEl float64) ([]*tracker.Pass, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.passes, nil
}

func (m *mockPassService) GetAllGroupsPasses(hours int, minEl float64) ([]*tracker.Pass, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Если allPasses задан — возвращаем его, иначе passes.
	if m.allPasses != nil {
		return m.allPasses, nil
	}
	return m.passes, nil
}

// --- Тесты GetPasses ---

func TestPassHandler_GetPasses_Success(t *testing.T) {
	passes := []*tracker.Pass{
		{NoradID: 25544, SatName: "ISS", AOS: 1700000000000, TCAEl: 45.0},
		{NoradID: 40069, SatName: "METEOR-M2", AOS: 1700001000000, TCAEl: 30.0},
	}
	passService := &mockPassService{passes: passes}

	handler := NewPassHandler(passService)

	req := httptest.NewRequest(http.MethodGet, "/api/passes?group=test&hours=12&min_el=10", nil)
	rec := httptest.NewRecorder()

	handler.GetPasses(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp PassesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Count != 2 {
		t.Errorf("expected count=2, got %d", resp.Count)
	}
	if resp.Params.Group != "test" {
		t.Errorf("expected group=test, got %s", resp.Params.Group)
	}
	if resp.Params.Hours != 12 {
		t.Errorf("expected hours=12, got %d", resp.Params.Hours)
	}
	if resp.Params.MinEl != 10 {
		t.Errorf("expected min_el=10, got %f", resp.Params.MinEl)
	}
}

func TestPassHandler_GetPasses_AllGroups(t *testing.T) {
	// Когда group не указан — возвращаются пролёты всех групп.
	allPasses := []*tracker.Pass{
		{NoradID: 25544, SatName: "ISS", AOS: 1700000000000},
		{NoradID: 40069, SatName: "METEOR-M2", AOS: 1700001000000},
		{NoradID: 43013, SatName: "CUTE-1.7", AOS: 1700002000000},
	}
	passService := &mockPassService{allPasses: allPasses}

	handler := NewPassHandler(passService)

	// Без параметра group.
	req := httptest.NewRequest(http.MethodGet, "/api/passes", nil)
	rec := httptest.NewRecorder()

	handler.GetPasses(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp PassesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Count != 3 {
		t.Errorf("expected count=3, got %d", resp.Count)
	}
	// Когда group не указан, в ответе должен быть "all".
	if resp.Params.Group != "all" {
		t.Errorf("expected group=all, got %s", resp.Params.Group)
	}
	if resp.Params.Hours != 24 {
		t.Errorf("expected default hours=24, got %d", resp.Params.Hours)
	}
	if resp.Params.MinEl != 5 {
		t.Errorf("expected default min_el=5, got %f", resp.Params.MinEl)
	}
}

func TestPassHandler_GetPasses_InvalidParams(t *testing.T) {
	passService := &mockPassService{passes: []*tracker.Pass{}}

	handler := NewPassHandler(passService)

	// Невалидные hours и min_el — используются дефолты.
	req := httptest.NewRequest(http.MethodGet, "/api/passes?group=test&hours=abc&min_el=invalid", nil)
	rec := httptest.NewRecorder()

	handler.GetPasses(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp PassesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Params.Hours != 24 {
		t.Errorf("expected default hours=24 for invalid input, got %d", resp.Params.Hours)
	}
	if resp.Params.MinEl != 5 {
		t.Errorf("expected default min_el=5 for invalid input, got %f", resp.Params.MinEl)
	}
}

func TestPassHandler_GetPasses_OutOfRangeParams(t *testing.T) {
	passService := &mockPassService{passes: []*tracker.Pass{}}

	handler := NewPassHandler(passService)

	// hours=0 (невалидно), min_el=100 (вне диапазона).
	req := httptest.NewRequest(http.MethodGet, "/api/passes?group=test&hours=0&min_el=100", nil)
	rec := httptest.NewRecorder()

	handler.GetPasses(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp PassesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Params.Hours != 24 {
		t.Errorf("expected default hours=24 for out-of-range, got %d", resp.Params.Hours)
	}
	if resp.Params.MinEl != 5 {
		t.Errorf("expected default min_el=5 for out-of-range, got %f", resp.Params.MinEl)
	}
}

func TestPassHandler_GetPasses_ServiceError(t *testing.T) {
	passService := &mockPassService{err: errors.New("computation failed")}

	handler := NewPassHandler(passService)

	req := httptest.NewRequest(http.MethodGet, "/api/passes?group=test", nil)
	rec := httptest.NewRecorder()

	handler.GetPasses(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Error != "failed to compute passes" {
		t.Errorf("expected error message, got %s", resp.Error)
	}
}

func TestPassHandler_GetPasses_AllGroupsServiceError(t *testing.T) {
	passService := &mockPassService{err: errors.New("computation failed")}

	handler := NewPassHandler(passService)

	// Без group — вызывается GetAllGroupsPasses.
	req := httptest.NewRequest(http.MethodGet, "/api/passes", nil)
	rec := httptest.NewRecorder()

	handler.GetPasses(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Error != "failed to compute passes" {
		t.Errorf("expected error message, got %s", resp.Error)
	}
}

// --- Тест NewPassHandler ---

func TestNewPassHandler(t *testing.T) {
	passService := &mockPassService{}

	handler := NewPassHandler(passService)

	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.passService != passService {
		t.Error("passService not set")
	}
}
