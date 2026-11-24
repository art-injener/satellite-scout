package rfprofile

import (
	"strconv"

	"github.com/art-injener/satellite-scout/internal/freqplan"
)

// Key возвращает ключ передатчика: UUID из реестра либо номинал с шагом NearHz.
func Key(uuid string, freqHz int64) string {
	if uuid != "" {
		return uuid
	}
	return strconv.FormatInt(QuantizeHz(freqHz), 10)
}

// QuantizeHz округляет частоту до ближайшего шага NearHz (5 кГц).
func QuantizeHz(freqHz int64) int64 {
	step := int64(freqplan.NearHz)
	if step <= 0 {
		return freqHz
	}
	if freqHz >= 0 {
		return ((freqHz + step/2) / step) * step
	}
	return ((freqHz - step/2) / step) * step
}
