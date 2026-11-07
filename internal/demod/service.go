package demod

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"image"
	"log/slog"
	"strconv"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/products"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sstv"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// События SSE демодулятора.
const (
	EventTelemetry = "telemetry"
	EventStatus    = "demod_status"
	EventProduct   = "product"
)

// statusInterval — как часто панель приёмника получает состояние демодулятора.
const statusInterval = time.Second

// ChannelSource — текущие настройки канала и усиление приёмника тракта (хаб приёмников).
type ChannelSource interface {
	Channel(pathID string) (sdr.ChannelSettings, error)
	GainDB(pathID string) (float32, error)
}

// Sink — получатель событий (рассылка SSE).
type Sink func(eventType string, data []byte)

// DopplerSource — радиальная скорость КА относительно станции в момент t, км/с
// (сервис сопровождения); false — орбита КА неизвестна.
type DopplerSource interface {
	RangeRateKmps(noradID int, t time.Time) (float64, bool)
}

// Service принимает потоки трактов попутно со зрителями и рассылает принятые кадры
// и состояние демодулятора.
type Service struct {
	channels ChannelSource
	emit     Sink
	now      func() time.Time
	products *products.Store
	audio    *AudioHub
	doppler  DopplerSource
	// transmitters — ожидаемые передатчики для маркеров на спектре; nil — маркеров нет.
	transmitters TransmitterSource
}

// NewService создаёт сервис демодуляции.
func NewService(channels ChannelSource, emit Sink) *Service {
	return &Service{channels: channels, emit: emit, now: time.Now}
}

// WithProducts включает сбор картинок: без хранилища профиль SSTV принимает, но ничего не публикует.
func (s *Service) WithProducts(store *products.Store) *Service {
	s.products = store
	return s
}

// WithAudio включает раздачу звука профилей голоса и SSTV слушателям.
func (s *Service) WithAudio(hub *AudioHub) *Service {
	s.audio = hub
	return s
}

// WithDoppler даёт орбиты КА: канал с doppler_norad следует за Доплером этого КА.
func (s *Service) WithDoppler(src DopplerSource) *Service {
	s.doppler = src
	return s
}

// followDoppler ставит канал цепочки на Доплер КА norad для частоты freq сейчас и возвращает
// этот сдвиг; nil — канал за Доплером не следует или орбита КА неизвестна, канал стоит на сдвиге.
func (s *Service) followDoppler(chain *Chain, norad int, freq uint64) *float64 {
	chain.SetDopplerHz(0)
	if norad == 0 || s.doppler == nil {
		return nil
	}
	rr, ok := s.doppler.RangeRateKmps(norad, s.now())
	if !ok {
		return nil
	}
	hz := tracker.DopplerShiftHz(float64(freq), rr)
	chain.SetDopplerHz(hz)
	return &hz
}

// publishAudio отдаёт звук блока слушателям, если раздача включена и профиль даёт звук.
func (s *Service) publishAudio(pathID string, timestampMs, freq uint64, audio []float32) {
	if s.audio != nil && len(audio) > 0 {
		s.audio.Publish(pathID, timestampMs, freq, audio)
	}
}

// TelemetryEvent — принятый кадр для таблицы телеметрии.
type TelemetryEvent struct {
	RadioPath   string `json:"radio_path"`
	TimestampMs uint64 `json:"timestamp_ms"`
	FrequencyHz uint64 `json:"frequency_hz"`
	Demod       string `json:"demod"`
	Src         string `json:"src"`
	Dest        string `json:"dest"`
	Info        string `json:"info"` // данные текстом без нулей в конце, если все символы печатные; иначе пусто
	InfoHex     string `json:"info_hex"`
	// Params — разобранная телеметрия, если формат кадра известен.
	Params []TelemetryParam `json:"params,omitempty"`
}

// TelemetryParam — параметр телеметрии для таблицы: ключевые показываются в строке,
// остальные — в подробностях кадра.
type TelemetryParam struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	Key   bool   `json:"key,omitempty"`
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
	// DemodOff — оператор выключил демодулятор: спектр идёт, кадров и снимков нет.
	DemodOff    bool   `json:"demod_off,omitempty"`
	BandwidthHz uint32 `json:"bandwidth_hz"`
	OffsetHz    int64  `json:"offset_hz"` // по сдвигу, полосе, порогу и профилю клиенты следуют за каналом
	// DopplerAuto — режим «Авто». DopplerNorad — КА, за которым следует канал.
	// DopplerHz — его сдвиг сейчас. Нет поля: орбита неизвестна.
	// Канал на спектре стоит на offset_hz плюс doppler_hz.
	DopplerAuto  bool     `json:"doppler_auto"`
	DopplerNorad int      `json:"doppler_norad,omitempty"`
	DopplerHz    *float64 `json:"doppler_hz,omitempty"`
	// GainDB — усиление приёмника, отрицательное при АРУ; нет, если приёмник его не сообщил.
	GainDB *float32 `json:"gain_db,omitempty"`
	// Reception — меры хода приёма; нет, пока уровень в канале не поднимался выше порога.
	Reception *ReceptionStages `json:"reception,omitempty"`
	// Picture — картинка в приёме (профиль SSTV); frames_good у него — число картинок.
	Picture *PictureProgress `json:"picture,omitempty"`
	// TxMarkers — передатчики КА группы в окне приёмника, частота с Доплером; нет — поток не идёт.
	TxMarkers []TxMarker `json:"tx_markers,omitempty"`
}

// ReceptionStages — по одной мере на этап приёма: несущая, такты, синхрослова.
type ReceptionStages struct {
	CarrierHz   float64    `json:"carrier_hz"`             // смещение от центра канала
	DeviationHz float64    `json:"deviation_hz,omitempty"` // нет — демодулятор её не меряет
	ClockJitter float64    `json:"clock_jitter"`           // ошибка фазы тактов, доли символа
	Sync        *SyncStats `json:"sync,omitempty"`         // нет — кадры без синхрослова
}

// SyncStats — найденные синхрослова и ошибочные биты в них.
type SyncStats struct {
	Words     int `json:"words"`
	BitErrors int `json:"bit_errors"`
}

// Run демодулирует поток тракта до его закрытия. Подходит как sdr.StreamTap.
// Настройки канала читаются на каждый блок, так что изменения оператора применяются сразу.
func (s *Service) Run(pathID string, frames <-chan sdr.IQFrame) {
	chain := NewChain()
	var (
		cs         sdr.ChannelSettings
		freq       uint64
		lastStatus time.Time
		picture    pictureState
		demodOff   bool
		st         Status // состояние после последнего принятого блока
	)
	for f := range frames {
		var err error
		if cs, err = s.channels.Channel(pathID); err != nil {
			slog.Warn("demod: channel settings", slog.String("radio_path", pathID), slog.Any("error", err))
			continue
		}
		freq = uint64(int64(f.CenterHz) + cs.OffsetHz)
		if cs.DemodOff {
			if !demodOff {
				// Выключение обрывает недоснятый кадр и забывает состояние, чтобы включение начало с чистого.
				if picture.id != "" {
					s.finishPicture(picture.id, nil)
					picture = pictureState{}
				}
				chain = NewChain()
				demodOff = true
				s.sendStatus(pathID, true, freq, cs, Status{}, s.bandMarkers(f.CenterHz, f.SampleRate))
				lastStatus = s.now()
			} else if now := s.now(); now.Sub(lastStatus) >= statusInterval {
				s.sendStatus(pathID, true, freq, cs, Status{}, s.bandMarkers(f.CenterHz, f.SampleRate))
				lastStatus = now
			}
			continue
		}
		demodOff = false
		doppler := s.followDoppler(chain, cs.DopplerNorad, freq)
		for _, p := range chain.Process(f, cs) {
			s.send(EventTelemetry, telemetryEvent(pathID, cs.Demod, p))
		}
		s.publishAudio(pathID, f.TimestampMs, freq, chain.Audio())
		s.publishPictures(pathID, freq, f.TimestampMs, chain.Pictures(), &picture)
		if picture.id != "" && chain.Status().Picture == nil {
			// Цепочка бросила картинку (перестройка, другой профиль): сохраняется принятая часть.
			s.finishPicture(picture.id, nil)
			picture.id = ""
		}
		st = chain.Status()
		st.DopplerHz = doppler
		if now := s.now(); lastStatus.IsZero() || now.Sub(lastStatus) >= statusInterval {
			s.sendStatus(pathID, true, freq, cs, st, s.bandMarkers(f.CenterHz, f.SampleRate))
			lastStatus = now
		}
	}
	s.sendStatus(pathID, false, freq, cs, st, nil)
	if picture.id != "" {
		// Поток закрылся посреди картинки: сохраняется то, что успело проявиться.
		s.finishPicture(picture.id, nil)
	}
}

// pictureState — картинка, которая сейчас проявляется в тракте.
type pictureState struct {
	id     string
	sentMs uint64 // время потока, когда ушло последнее событие хода
}

// publishPictures переносит события декодера картинок в хранилище и SSE `product`;
// ход проявления — не чаще statusInterval по времени потока: запись проигрывается
// быстрее реального.
func (s *Service) publishPictures(pathID string, freq, nowMs uint64, events []sstv.Event, st *pictureState) {
	if s.products == nil {
		return
	}
	for _, e := range events {
		switch e.Kind {
		case sstv.EventStart:
			if st.id != "" {
				s.finishPicture(st.id, nil)
			}
			p := s.products.Begin(products.Meta{
				RadioPath: pathID, Kind: products.KindSSTV, Mode: e.Mode.ID, ModeName: e.Mode.Name, FrequencyHz: freq,
			}, e.Image)
			st.id, st.sentMs = p.ID, nowMs
			s.send(EventProduct, p)
		case sstv.EventLines:
			if st.id == "" {
				continue
			}
			p := s.products.Update(st.id, e.Lines, e.Image)
			if nowMs-st.sentMs >= uint64(statusInterval.Milliseconds()) {
				s.send(EventProduct, p)
				st.sentMs = nowMs
			}
		case sstv.EventDone:
			if st.id != "" {
				s.finishPicture(st.id, e.Image)
				st.id = ""
			}
		}
	}
}

func (s *Service) finishPicture(id string, img *image.RGBA) {
	p, err := s.products.Finish(id, img)
	if err != nil {
		slog.Error("demod: save picture", slog.String("id", id), slog.Any("error", err))
		return
	}
	s.send(EventProduct, p)
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
	// Кадры фиксированной длины дополняются нулями в конце: текст показывается и без них.
	if text := bytes.TrimRight(p.Frame.Info, "\x00"); printable(text) {
		ev.Info = string(text)
	}
	if demod == demodGEOSCAN {
		ev.Params = geoscanParams(p.Frame)
	}
	return ev
}

func geoscanParams(f ax25.Frame) []TelemetryParam {
	params, ok := geoscan.BeaconParams(f)
	if !ok {
		return nil
	}
	out := make([]TelemetryParam, 0, len(params))
	for _, p := range params {
		out = append(out, TelemetryParam{ID: p.ID, Name: p.Name, Value: p.Value, Unit: p.Unit, Key: p.Key})
	}
	return out
}

// sendStatus рассылает состояние демодулятора вместе с усилением приёмника и маркерами передатчиков.
func (s *Service) sendStatus(pathID string, active bool, freq uint64, cs sdr.ChannelSettings, st Status,
	markers []TxMarker) {
	ev := statusEvent(pathID, active, freq, cs, st)
	ev.TxMarkers = markers
	if g, err := s.channels.GainDB(pathID); err == nil {
		ev.GainDB = &g
	}
	s.send(EventStatus, ev)
}

func statusEvent(pathID string, active bool, freq uint64, cs sdr.ChannelSettings, st Status) StatusEvent {
	if cs.DemodOff {
		st = Status{}
	}
	demod := st.Demod
	if demod == "" {
		demod = cs.Demod
	}
	ev := StatusEvent{
		RadioPath:    pathID,
		Active:       active,
		Demod:        demod,
		DemodOff:     cs.DemodOff,
		Supported:    st.Supported,
		FrequencyHz:  freq,
		LevelDBFS:    st.LevelDBFS,
		SquelchDBFS:  cs.SquelchDBFS,
		SquelchOpen:  active && st.SquelchOpen,
		FramesGood:   st.Good,
		FramesBad:    st.Bad,
		LastPacketMs: st.LastPacketMs,
		BandwidthHz:  cs.BandwidthHz,
		OffsetHz:     cs.OffsetHz,
		DopplerAuto:  cs.DopplerAuto,
		DopplerNorad: cs.DopplerNorad,
		DopplerHz:    st.DopplerHz,
		Picture:      st.Picture,
	}
	if st.Measured {
		ev.Reception = &ReceptionStages{CarrierHz: st.CarrierHz, DeviationHz: st.DeviationHz, ClockJitter: st.ClockJitter}
		if st.SyncCounted {
			ev.Reception.Sync = &SyncStats{Words: st.SyncWords, BitErrors: st.SyncBitErrors}
		}
	}
	return ev
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
