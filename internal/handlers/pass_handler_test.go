package handlers

import (
	"bytes"
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
	passes []*tracker.Pass
	err    error
}

func (m *mockPassService) GetPasses(group string, hours int, minEl float64) ([]*tracker.Pass, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.passes, nil
}

// mockTrackingService — мок TrackingService для тестирования.
type mockTrackingService struct {
	trackedID int
	err       error
}

func (m *mockTrackingService) TrackSatellite(noradID int) error {
	if m.err != nil {
		return m.err
	}
	m.trackedID = noradID
	return nil
}

// --- Тесты GetPasses ---

func TestPassHandler_GetPasses_Success(t *testing.T) {
	passes := []*tracker.Pass{
		{NoradID: 25544, SatName: "ISS", AOS: 1700000000000, TCAEl: 45.0},
		{NoradID: 40069, SatName: "METEOR-M2", AOS: 1700001000000, TCAEl: 30.0},
	}
	passService := &mockPassService{passes: passes}
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

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

func TestPassHandler_GetPasses_DefaultParams(t *testing.T) {
	passService := &mockPassService{passes: []*tracker.Pass{}}
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

	req := httptest.NewRequest(http.MethodGet, "/api/passes", nil)
	rec := httptest.NewRecorder()

	handler.GetPasses(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp PassesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Params.Group != "amateur" {
		t.Errorf("expected default group=amateur, got %s", resp.Params.Group)
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
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

	// Невалидные hours и min_el — используются дефолты.
	req := httptest.NewRequest(http.MethodGet, "/api/passes?hours=abc&min_el=invalid", nil)
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
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

	// hours=0 (невалидно), min_el=100 (вне диапазона).
	req := httptest.NewRequest(http.MethodGet, "/api/passes?hours=0&min_el=100", nil)
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
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

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

// --- Тесты SetTrackingCurrent ---

func TestPassHandler_SetTrackingCurrent_Success(t *testing.T) {
	passService := &mockPassService{}
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

	body := bytes.NewBufferString(`{"norad_id": 25544}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/current", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.SetTrackingCurrent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if trackingService.trackedID != 25544 {
		t.Errorf("expected tracked ID 25544, got %d", trackingService.trackedID)
	}

	var resp TrackingResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Status != "tracking" {
		t.Errorf("expected status=tracking, got %s", resp.Status)
	}
	if resp.NoradID != 25544 {
		t.Errorf("expected norad_id=25544, got %d", resp.NoradID)
	}
}

func TestPassHandler_SetTrackingCurrent_InvalidBody(t *testing.T) {
	passService := &mockPassService{}
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

	body := bytes.NewBufferString(`invalid json`)
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/current", body)
	rec := httptest.NewRecorder()

	handler.SetTrackingCurrent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Error != "invalid request body" {
		t.Errorf("expected 'invalid request body', got %s", resp.Error)
	}
}

func TestPassHandler_SetTrackingCurrent_MissingNoradID(t *testing.T) {
	passService := &mockPassService{}
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/current", body)
	rec := httptest.NewRecorder()

	handler.SetTrackingCurrent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Error != "norad_id is required and must be positive" {
		t.Errorf("expected validation error, got %s", resp.Error)
	}
}

func TestPassHandler_SetTrackingCurrent_NegativeNoradID(t *testing.T) {
	passService := &mockPassService{}
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

	body := bytes.NewBufferString(`{"norad_id": -1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/current", body)
	rec := httptest.NewRecorder()

	handler.SetTrackingCurrent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestPassHandler_SetTrackingCurrent_ServiceError(t *testing.T) {
	passService := &mockPassService{}
	trackingService := &mockTrackingService{err: errors.New("tracking failed")}

	handler := NewPassHandler(passService, trackingService)

	body := bytes.NewBufferString(`{"norad_id": 25544}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/current", body)
	rec := httptest.NewRecorder()

	handler.SetTrackingCurrent(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Error != "failed to track satellite" {
		t.Errorf("expected error message, got %s", resp.Error)
	}
}

func TestPassHandler_SetTrackingCurrent_NotFound(t *testing.T) {
	passService := &mockPassService{}
	// Симулируем ошибку "not found" (сообщение совпадает с SatelliteNotFoundError).
	trackingService := &mockTrackingService{err: errors.New("satellite not found in TLE store: 99999")}

	handler := NewPassHandler(passService, trackingService)

	body := bytes.NewBufferString(`{"norad_id": 99999}`)
	req := httptest.NewRequest(http.MethodPost, "/api/tracking/current", body)
	rec := httptest.NewRecorder()

	handler.SetTrackingCurrent(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	if resp.Error != "satellite not found" {
		t.Errorf("expected 'satellite not found', got %s", resp.Error)
	}
}

// --- Тест NewPassHandler ---

func TestNewPassHandler(t *testing.T) {
	passService := &mockPassService{}
	trackingService := &mockTrackingService{}

	handler := NewPassHandler(passService, trackingService)

	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
	if handler.passService != passService {
		t.Error("passService not set")
	}
	if handler.trackingService != trackingService {
		t.Error("trackingService not set")
	}
}
