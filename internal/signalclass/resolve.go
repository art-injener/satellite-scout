package signalclass

import "github.com/art-injener/satellite-scout/internal/signals"

const (
	// BasisSignature — вид передачи по совпавшей сигнатуре.
	BasisSignature = "signature"
	// BasisProbe — проба на вырезке сошлась.
	BasisProbe = "probe"
	// KindEstimate — уверенность модуляции по формуле дерева, не доля верных ответов.
	KindEstimate = "estimate"

	// ServiceSatVoice — голос КА: ЧМ с речью и происхождение спутника.
	ServiceSatVoice = "sat_voice"
	// ServiceSatTx — узкая цифра КА.
	ServiceSatTx = "sat_tx"
)

// Resolve ставит вид передачи по каталогу сигнатур. iq — вырезка участка:
// без неё вид с обязательной пробой остаётся на ступени «по признакам».
// Уже поставленная служба (SSTV, вещание с пилотом) не заменяется.
func Resolve(r Result, in signals.Input, iq []complex64, rate float64) Result {
	if r.ConfidenceKind == "" && r.Modulation != "" && r.Modulation != ModUndetermined {
		r.ConfidenceKind = KindEstimate
	}
	if r.Service != "" {
		if r.Service == ServiceBroadcast && r.ServiceBasis == "" {
			r.ServiceBasis = BasisSignature
		}
		return r
	}
	in.Modulation = r.Modulation
	in.OccupiedHz = r.Params.OccupiedHz
	in.Baud = r.Params.Baud
	hits := signals.Match(signals.Default(), in)
	if len(hits) != 1 {
		return r
	}
	h := hits[0]
	if h.Probe == signals.ProbeRequired && len(iq) > 0 {
		if !probeHit(h.ID, iq, rate, r) {
			return r
		}
		r.Service = h.ID
		r.ServiceLabel = h.Label
		r.ServiceBasis = BasisProbe
		return r
	}
	r.Service = h.ID
	r.ServiceLabel = h.Label
	r.ServiceBasis = BasisSignature
	return r
}
