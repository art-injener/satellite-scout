// Package rfprofile — накопленный радиопрофиль КА (ADR-012).
package rfprofile

// MinDetectedConfirmations — порог слоя «обнаружено» для действующего режима.
const MinDetectedConfirmations = 3

// Profile — одна запись на NORAD со списком передатчиков.
type Profile struct {
	NoradID   int           `json:"norad_id"`
	Tx        []Transmitter `json:"tx"`
	BestKey   string        `json:"best_key,omitempty"`
	UpdatedMs int64         `json:"updated_ms"`
}

// Transmitter — передатчик внутри профиля.
// Слой «заявлено» не хранится: читается из реестра transmitters.
type Transmitter struct {
	Key    string `json:"key"`
	UUID   string `json:"uuid,omitempty"`
	FreqHz int64  `json:"freq_hz"`

	Detected Detected `json:"detected"`
	Accepted Accepted `json:"accepted"`
	Stats    Stats    `json:"stats"`
}

// Detected — слой «обнаружено»: классификатор по стоянкам.
type Detected struct {
	Modulation    string  `json:"modulation,omitempty"`
	Baud          float64 `json:"baud,omitempty"`
	BandwidthHz   int64   `json:"bandwidth_hz,omitempty"`
	BeaconPeriodS float64 `json:"beacon_period_s,omitempty"`
	OffsetHz      float64 `json:"offset_hz,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
	Confirmations int      `json:"confirmations,omitempty"`
	LastSeenMs    int64    `json:"last_seen_ms,omitempty"`
	Demod         string   `json:"demod,omitempty"`
	DemodAlt      []string `json:"demod_alt,omitempty"`
}

// Accepted — слой «принято»: демод с верной CRC.
type Accepted struct {
	DemodID     string `json:"demod_id,omitempty"`
	FramesTotal int    `json:"frames_total,omitempty"`
	LastFrameMs int64  `json:"last_frame_ms,omitempty"`
}

// Stats — статистика стоянок и слышимости.
type Stats struct {
	VisitsTotal      int     `json:"visits_total,omitempty"`
	VisitsWithEnergy int     `json:"visits_with_energy,omitempty"`
	VisitsWithFrames int     `json:"visits_with_frames,omitempty"`
	BestSNRDB        float32 `json:"best_snr_db,omitempty"`
	LastHeardMs      int64   `json:"last_heard_ms,omitempty"`
}
