package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/access"
)

// AccessHandler обслуживает роль клиента, вход по паролю мастера и передачу управления.
//
// Эндпоинты:
//   - GET  /api/access         — роль (master | controller | viewer) и состояние управления.
//   - POST /api/access/login   — {"password": "..."}; 204 при успехе, 401 неверный, 429 слишком много попыток.
//   - POST /api/access/request — запрос управления; 202 ждёт мастера, 200 права уже есть, 409 нельзя.
//   - POST /api/access/decide  — только мастеру: {"request_id": "...", "allow": true}; 204, 404 нет запроса.
//   - POST /api/access/revoke  — только мастеру: забрать разрешение; 204.
//   - POST /api/access/release — управляющему: вернуть разрешение; 204, у остальных ничего не меняет.
type AccessHandler struct {
	manager *access.Manager
}

// NewAccessHandler создаёт обработчик доступа.
func NewAccessHandler(manager *access.Manager) *AccessHandler {
	return &AccessHandler{manager: manager}
}

// AccessStatusResponse — ответ GET /api/access. Адрес управляющего и чужой запрос видит только мастер.
type AccessStatusResponse struct {
	Role             string                 `json:"role"`
	ControllerActive bool                   `json:"controller_active"`
	ControllerAddr   string                 `json:"controller_addr,omitempty"`
	Pending          *access.PendingRequest `json:"pending,omitempty"`
	RequestPending   bool                   `json:"request_pending,omitempty"`
}

// AccessErrorResponse — ошибка с машинным кодом, по которому UI выбирает подсказку.
type AccessErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type accessLoginRequest struct {
	Password string `json:"password"`
}

type accessDecideRequest struct {
	RequestID string `json:"request_id"`
	Allow     bool   `json:"allow"`
}

type accessRequestResponse struct {
	Status string `json:"status"` // pending | granted
}

// Status возвращает роль клиента и состояние управления.
func (h *AccessHandler) Status(w http.ResponseWriter, r *http.Request) {
	s := h.manager.Status(r)
	writeJSON(w, http.StatusOK, AccessStatusResponse{
		Role:             s.Role.String(),
		ControllerActive: s.ControllerActive,
		ControllerAddr:   s.ControllerAddr,
		Pending:          s.Pending,
		RequestPending:   s.RequestPending,
	})
}

// Request отправляет мастерам запрос управления.
func (h *AccessHandler) Request(w http.ResponseWriter, r *http.Request) {
	err := h.manager.RequestControl(r)
	switch {
	case err == nil && h.manager.Role(r) >= access.RoleController:
		writeJSON(w, http.StatusOK, accessRequestResponse{Status: "granted"})
	case err == nil:
		slog.InfoContext(r.Context(), "access: control requested", "remote", r.RemoteAddr)
		writeJSON(w, http.StatusAccepted, accessRequestResponse{Status: "pending"})
	case errors.Is(err, access.ErrNoMasterOnline):
		writeJSON(w, http.StatusConflict, AccessErrorResponse{Error: err.Error(), Code: "no_master_online"})
	case errors.Is(err, access.ErrRequestPending):
		writeJSON(w, http.StatusConflict, AccessErrorResponse{Error: err.Error(), Code: "request_pending"})
	default:
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}
}

// Decide применяет решение мастера по запросу управления.
func (h *AccessHandler) Decide(w http.ResponseWriter, r *http.Request) {
	var req accessDecideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if err := h.manager.Decide(req.RequestID, req.Allow); err != nil {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	slog.InfoContext(r.Context(), "access: control request decided", "allow", req.Allow)
	w.WriteHeader(http.StatusNoContent)
}

// Revoke забирает разрешение у управляющего слейва.
func (h *AccessHandler) Revoke(w http.ResponseWriter, _ *http.Request) {
	h.manager.Revoke()
	w.WriteHeader(http.StatusNoContent)
}

// Release возвращает разрешение, если оно у клиента.
func (h *AccessHandler) Release(w http.ResponseWriter, r *http.Request) {
	h.manager.Release(r)
	w.WriteHeader(http.StatusNoContent)
}

// Login делает сессию клиента мастером при верном пароле.
func (h *AccessHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req accessLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	err := h.manager.Login(r, req.Password)
	switch {
	case err == nil:
		slog.InfoContext(r.Context(), "access: master login", "remote", r.RemoteAddr)
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, access.ErrTooManyAttempts):
		slog.WarnContext(r.Context(), "access: login rate limited", "remote", r.RemoteAddr)
		writeJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: err.Error()})
	case errors.Is(err, access.ErrNoSession):
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	default:
		slog.WarnContext(r.Context(), "access: wrong master password", "remote", r.RemoteAddr)
		writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: err.Error()})
	}
}
