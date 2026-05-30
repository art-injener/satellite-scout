package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/art-injener/satellite-scout/internal/config"
)

// SettingsHandler обслуживает REST API единых настроек приложения для
// модального окна «Настройки» в UI: чтение и обновление всего конфига.
//
// Эндпоинты:
//   - GET  /api/settings — возвращает полный config.Config (без runtime-полей).
//   - PUT  /api/settings — принимает Config, валидирует, сохраняет атомарно
//     через ConfigStore.Update и возвращает список полей, требующих рестарта.
type SettingsHandler struct {
	store *config.Store
}

// NewSettingsHandler создаёт новый обработчик настроек.
func NewSettingsHandler(store *config.Store) *SettingsHandler {
	return &SettingsHandler{store: store}
}

// SettingsUpdateResponse — ответ на PUT /api/settings. RequiresRestart
// перечисляет dotted-path полей, для применения которых нужен перезапуск
// сервера (UI показывает соответствующий toast).
type SettingsUpdateResponse struct {
	Status          string   `json:"status"`
	RequiresRestart []string `json:"requires_restart,omitempty"`
}

// SettingsValidationResponse — ответ при ошибке валидации (HTTP 400).
type SettingsValidationResponse struct {
	Error  string                  `json:"error"`
	Errors config.ValidationErrors `json:"errors"`
}

// Get возвращает текущую конфигурацию в виде, пригодном для модалки настроек.
// Runtime-поля (DevMode) не сериализуются — их клиент не редактирует.
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	cfg := h.store.Get()
	if cfg == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{
			Error: "configuration not initialized",
		})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// Update принимает полный config.Config, валидирует и сохраняет.
// Для строгости — JSON-декодинг с DisallowUnknownFields, чтобы случайные опечатки
// в названиях ключей не молча игнорировались.
func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var incoming config.Config
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid JSON: " + err.Error(),
		})
		return
	}

	old := h.store.Get()

	if err := incoming.Validate(); err != nil {
		var verrs config.ValidationErrors
		if errors.As(err, &verrs) {
			writeJSON(w, http.StatusBadRequest, SettingsValidationResponse{
				Error:  "validation failed",
				Errors: verrs,
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	err := h.store.Update(func(c *config.Config) error {
		// Полная замена всего конфига (кроме runtime DevMode, которого в JSON нет).
		dev := c.DevMode
		*c = incoming
		c.DevMode = dev
		return nil
	})
	if err != nil {
		slog.Error("failed to update settings", slog.Any("error", err))
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, SettingsUpdateResponse{
		Status:          "ok",
		RequiresRestart: config.RestartRequiredFields(old, h.store.Get()),
	})
}
