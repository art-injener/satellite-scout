package demod

import (
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

// События SSE демодулятора.
const (
	EventTelemetry = "telemetry"
	EventStatus    = "demod_status"
)

// statusInterval — как часто панель приёмника получает состояние демодулятора.
const statusInterval = time.Second

// ChannelSource — текущие настройки канала тракта (хаб приёмников).
type ChannelSource interface {
	Channel(pathID string) (sdr.ChannelSettings, error)
}

// Sink — получатель событий (рассылка SSE).
type Sink func(eventType string, data []byte)

// Service принимает потоки трактов попутно со зрителями и рассылает принятые кадры
// и состояние демодулятора.
type Service struct {
	channels ChannelSource
	emit     Sink
	now      func() time.Time
}

// NewService создаёт сервис демодуляции.
func NewService(channels ChannelSource, emit Sink) *Service {
	return &Service{channels: channels, emit: emit, now: time.Now}
}

// TelemetryEvent — принятый кадр для таблицы телеметрии.
type TelemetryEvent struct {
	RadioPath   string `json:"radio_path"`
	TimestampMs uint64 `json:"timestamp_ms"`
	FrequencyHz uint64 `json:"frequency_hz"`
	Demod       string `json:"demod"`
	Src         string `json:"src"`
	Dest        string `json:"dest"`
	Info        string `json:"info"` // данные текстом, если все символы печатные; иначе пусто
	InfoHex     string `json:"info_hex"`
}

// StatusEvent — состояние демодулятора тракта для панели приёмника.
type StatusEvent struct {
	RadioPath    string  `json:"radio_path"`
	Active       bool    `json:"active"` // поток устройства идёт
	Demod        string  `json:"demod"`
	Supported    bool    `json:"supported"`
	FrequencyHz  uint64  `json:"frequency_hz"`
	LevelDBFS    float32 `json:"level_dbfs"`
	SquelchDBFS  float64 `json:"squelch_dbfs"`
	SquelchOpen  bool    `json:"squelch_open"`
	FramesGood   int     `json:"frames_good"`
	FramesBad    int     `json:"frames_bad"`
	LastPacketMs uint64  `json:"last_packet_ms"`
}

// Run демодулирует поток тракта до его закрытия. Подходит как sdr.StreamTap.
// Настройки канала читаются на каждый блок, так что изменения оператора применяются сразу.
func (s *Service) Run(pathID string, frames <-chan sdr.IQFrame) {
	chain := NewChain()
	var (
		cs         sdr.ChannelSettings
		freq       uint64
		lastStatus time.Time
	)
	for f := range frames {
		var err error
		if cs, err = s.channels.Channel(pathID); err != nil {
			slog.Warn("demod: channel settings", slog.String("radio_path", pathID), slog.Any("error", err))
			continue
		}
		freq = uint64(int64(f.CenterHz) + cs.OffsetHz)
		for _, p := range chain.Process(f, cs) {
			s.send(EventTelemetry, telemetryEvent(pathID, cs.Demod, p))
		}
		if now := s.now(); lastStatus.IsZero() || now.Sub(lastStatus) >= statusInterval {
			s.send(EventStatus, statusEvent(pathID, true, freq, cs, chain.Status()))
			lastStatus = now
		}
	}
	s.send(EventStatus, statusEvent(pathID, false, freq, cs, chain.Status()))
}

func (s *Service) send(eventType string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("demod: marshal event", slog.String("event", eventType), slog.Any("error", err))
		return
	}
	s.emit(eventType, data)
}

func telemetryEvent(pathID, demod string, p Packet) TelemetryEvent {
	ev := TelemetryEvent{
		RadioPath:   pathID,
		TimestampMs: p.TimestampMs,
		FrequencyHz: p.FrequencyHz,
		Demod:       demod,
		Src:         callsign(p.Frame.Src),
		Dest:        callsign(p.Frame.Dest),
		InfoHex:     hex.EncodeToString(p.Frame.Info),
	}
	if printable(p.Frame.Info) {
		ev.Info = string(p.Frame.Info)
	}
	return ev
}

func statusEvent(pathID string, active bool, freq uint64, cs sdr.ChannelSettings, st Status) StatusEvent {
	return StatusEvent{
		RadioPath:    pathID,
		Active:       active,
		Demod:        st.Demod,
		Supported:    st.Supported,
		FrequencyHz:  freq,
		LevelDBFS:    st.LevelDBFS,
		SquelchDBFS:  cs.SquelchDBFS,
		SquelchOpen:  active && st.SquelchOpen,
		FramesGood:   st.Good,
		FramesBad:    st.Bad,
		LastPacketMs: st.LastPacketMs,
	}
}

// callsign — позывной с SSID, как его пишут радиолюбители: SCOUT-1; SSID 0 не пишется.
func callsign(a ax25.Address) string {
	if a.SSID == 0 {
		return a.Call
	}
	return a.Call + "-" + strconv.Itoa(int(a.SSID))
}

func printable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
