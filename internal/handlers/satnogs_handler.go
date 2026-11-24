package handlers

import (
	"net/http"
	"strconv"

	"github.com/art-injener/satellite-scout/internal/satnogs"
)

// SatNOGSProvider — каталог передатчиков для REST (обычно реестр).
// Имя историческое: путь /api/satnogs/*, данные не обязаны быть из сети SatNOGS.
type SatNOGSProvider interface {
	GetAllTransmitters(noradID int) []satnogs.Transmitter
	GetPrimaryTransmitter(noradID int) *satnogs.TransmitterSummary
}

// SatNOGSHandler — REST-обработчик /api/satnogs/* для UI-расширений
// (dropdown с несколькими передатчиками, диагностика).
type SatNOGSHandler struct {
	provider SatNOGSProvider
}

// NewSatNOGSHandler создаёт обработчик каталога передатчиков.
func NewSatNOGSHandler(provider SatNOGSProvider) *SatNOGSHandler {
	return &SatNOGSHandler{provider: provider}
}

// SatNOGSTransmittersResponse — ответ /api/satnogs/transmitters/{norad}.
type SatNOGSTransmittersResponse struct {
	NoradID      int                         `json:"norad_id"`
	Primary      *satnogs.TransmitterSummary `json:"primary"`
	Transmitters []satnogs.Transmitter       `json:"transmitters"`
	Count        int                         `json:"count"`
}

// GetTransmitters обрабатывает GET /api/satnogs/transmitters/{norad}.
// Читает каталог (реестр); сеть ради ответа не дергается.
func (h *SatNOGSHandler) GetTransmitters(w http.ResponseWriter, r *http.Request) {
	noradStr := r.PathValue("norad")
	noradID, err := strconv.Atoi(noradStr)
	if err != nil || noradID <= 0 {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid norad id",
		})
		return
	}

	transmitters := h.provider.GetAllTransmitters(noradID)
	if transmitters == nil {
		transmitters = []satnogs.Transmitter{}
	}
	primary := h.provider.GetPrimaryTransmitter(noradID)

	writeJSON(w, http.StatusOK, SatNOGSTransmittersResponse{
		NoradID:      noradID,
		Primary:      primary,
		Transmitters: transmitters,
		Count:        len(transmitters),
	})
}
