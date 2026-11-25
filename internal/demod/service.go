package demod

import (
	"encoding/json"
	"fmt"
	"image"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/payload"
	"github.com/art-injener/satellite-scout/internal/products"
	"github.com/art-injener/satellite-scout/internal/rfprofile"
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

// dropLogInterval — минимальный интервал между предупреждениями о пропуске IQ-фреймов.
const dropLogInterval = 5 * time.Second

// DropCounter — источник счётчика пропущенных IQ-фреймов канала подписки.
type DropCounter interface {
	DroppedFrames(pathID string, frames <-chan sdr.IQFrame) uint64
}

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
	callsigns    CallsignIndex
	pathSats     PathSatSource
	names        NameSource
	horizon      HorizonSource
	formats      ProfileCatalog
	rfFrames     RFFrameRecorder
	rfProfiles   RFProfileSource
	airHints     AirHintSource
	airHear      AirHear
	drops        DropCounter

	rxMu     sync.Mutex
	rxByPath map[string]RxActivity
}

// RxActivity — живая активность приёма на тракте для удержания стоянки обхода.
type RxActivity struct {
	LastFrameMs int64 // время последнего кадра с CRC (Unix мс); 0 — ещё не было
	Picture     bool  // прямо сейчас принимается картинка (SSTV)
}

// RxActivity возвращает текущую активность приёма на тракте.
func (s *Service) RxActivity(pathID string) RxActivity {
	if s == nil {
		return RxActivity{}
	}
	s.rxMu.Lock()
	defer s.rxMu.Unlock()
	if s.rxByPath == nil {
		return RxActivity{}
	}
	return s.rxByPath[pathID]
}

func (s *Service) recordRxFrame(pathID string, nowMs int64) {
	if s == nil || nowMs <= 0 {
		return
	}
	s.rxMu.Lock()
	if s.rxByPath == nil {
		s.rxByPath = make(map[string]RxActivity)
	}
	cur := s.rxByPath[pathID]
	if nowMs > cur.LastFrameMs {
		cur.LastFrameMs = nowMs
	}
	s.rxByPath[pathID] = cur
	s.rxMu.Unlock()
}

func (s *Service) setPictureActive(pathID string, active bool) {
	if s == nil {
		return
	}
	s.rxMu.Lock()
	if s.rxByPath == nil {
		if !active {
			s.rxMu.Unlock()
			return
		}
		s.rxByPath = make(map[string]RxActivity)
	}
	cur := s.rxByPath[pathID]
	cur.Picture = active
	s.rxByPath[pathID] = cur
	s.rxMu.Unlock()
}

// AirNote — принятый кадр/голос для строки «Эфир» (тело кадра для модалки).
type AirNote struct {
	Kind        string
	Summary     string
	Decoder     string
	Src         string
	Dest        string
	Info        string
	InfoHex     string
	Params      []TelemetryParam
	TimestampMs uint64
	NoradID     int
	SatName     string
	// CountFrame — увеличить frames_good; голос — false.
	CountFrame bool
}

// AirHear — запись приёма в таблицу «Эфир» (bandscan.RecordRx).
type AirHear interface {
	RecordRx(pathID string, freqHz uint64, note AirNote)
}

// AirHintSource — подсказка «Авто» с участка эфира (классификатор), без NORAD.
type AirHintSource interface {
	AirHint(pathID string, freqHz uint64) (demod string, alt []string, mode string, baud float64, ok bool)
}

// RFFrameRecorder — запись принятого кадра в радиопрофиль КА (ADR-012).
type RFFrameRecorder interface {
	RecordFrame(norad int, freqHz int64, demodID string, tsMs int64)
}

// RFProfileSource — чтение радиопрофиля для кандидатов «Авто».
type RFProfileSource interface {
	RFProfile(norad int) (rfprofile.Profile, bool)
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

// WithCallsigns включает привязку кадра по позывному из каталога форматов.
func (s *Service) WithCallsigns(idx CallsignIndex) *Service {
	s.callsigns = idx
	return s
}

// WithPathSats даёт подсказку КА по тракту (SigMF / конфиг replay).
func (s *Service) WithPathSats(src PathSatSource) *Service {
	s.pathSats = src
	return s
}

// WithNames подставляет отображаемое имя КА, если источник привязки его не дал.
func (s *Service) WithNames(src NameSource) *Service {
	s.names = src
	return s
}

// WithHorizon ограничивает привязку по частоте КА над горизонтом.
func (s *Service) WithHorizon(src HorizonSource) *Service {
	s.horizon = src
	return s
}

// ProfileCatalog — закреплённый профиль с карточки КА (каталог форматов).
type ProfileCatalog interface {
	ProfileFor(norad int, freqHz uint64) (profile string, ok bool)
}

// WithFormats включает подсказку профиля с карточки для режима «Авто».
func (s *Service) WithFormats(c ProfileCatalog) *Service {
	s.formats = c
	return s
}

// WithAirHints подключает подсказки классификатора по частоте канала (эфир без КА).
func (s *Service) WithAirHints(src AirHintSource) *Service {
	s.airHints = src
	return s
}

// WithAirHear пишет кадры и голос в участки band_signals.
func (s *Service) WithAirHear(h AirHear) *Service {
	s.airHear = h
	return s
}

// voiceDemod — голосовые профили без кадра: в «Данные» эфира пишем kind=voice при открытом пороге.
// CW не относится к голосу и сюда не входит.
func voiceDemod(id string) bool {
	switch id {
	case "fm", "wfm", "am", "usb", "lsb":
		return true
	default:
		return false
	}
}

// WithRFFrames включает накопление принятых кадров в радиопрофиле КА.
func (s *Service) WithRFFrames(r RFFrameRecorder) *Service {
	s.rfFrames = r
	return s
}

// WithRFProfiles включает чтение радиопрофиля для подсказок «Авто».
func (s *Service) WithRFProfiles(src RFProfileSource) *Service {
	s.rfProfiles = src
	return s
}

// WithDropCounter подключает источник счётчика пропущенных IQ-фреймов для логирования.
func (s *Service) WithDropCounter(dc DropCounter) *Service {
	s.drops = dc
	return s
}

func (s *Service) dropCounter() DropCounter {
	if s.drops != nil {
		return s.drops
	}
	if dc, ok := s.channels.(DropCounter); ok {
		return dc
	}
	return nil
}

// dopplerShift рассчитывает поправку Доплера для текущих настроек канала; nil означает отсутствие поправки.
func (s *Service) dopplerShift(cs sdr.ChannelSettings, freq uint64) *float64 {
	if !cs.DopplerAuto {
		hz := float64(cs.DopplerManualHz)
		if hz == 0 {
			return nil
		}
		return &hz
	}
	if cs.DopplerNorad == 0 || s.doppler == nil {
		return nil
	}
	rr, ok := s.doppler.RangeRateKmps(cs.DopplerNorad, s.now())
	if !ok {
		return nil
	}
	hz := tracker.DopplerShiftHz(float64(freq), rr)
	return &hz
}

// applyDoppler ставит поправку канала. «Авто» берёт орбиту КА. «Вручную» берёт герцы оператора.
// nil — поправки нет: орбита неизвестна или ручное значение нулевое.
func (s *Service) applyDoppler(chain *Chain, cs sdr.ChannelSettings, freq uint64) *float64 {
	doppler := s.dopplerShift(cs, freq)
	if doppler == nil {
		chain.SetDopplerHz(0)
		return nil
	}
	chain.SetDopplerHz(*doppler)
	return doppler
}

// publishAudio отдаёт звук блока слушателям, если раздача включена и профиль даёт звук.
func (s *Service) publishAudio(pathID string, timestampMs, freq uint64, audio []float32) {
	if s.audio != nil && len(audio) > 0 {
		s.audio.Publish(pathID, timestampMs, freq, audio)
	}
}

// TelemetryEvent — принятый кадр для таблицы телеметрии (ADR-009 §11.1).
type TelemetryEvent struct {
	RadioPath   string `json:"radio_path"`
	TimestampMs uint64 `json:"timestamp_ms"`
	FrequencyHz uint64 `json:"frequency_hz"`
	Demod       string `json:"demod"`
	Src         string `json:"src"`
	Dest        string `json:"dest"`
	Info        string `json:"info"` // данные текстом без нулей в конце, если все символы печатные; иначе пусто
	InfoHex     string `json:"info_hex"`
	// Kind / Decoder / Summary — разбор полезной нагрузки (ADR-009 §7); старые поля сохранены.
	Kind    string `json:"kind,omitempty"`
	Decoder string `json:"decoder,omitempty"`
	Summary string `json:"summary,omitempty"`
	// Params — разобранная телеметрия, если формат кадра известен.
	Params []TelemetryParam `json:"params,omitempty"`
	// Привязка кадра к КА (ADR-009 §6.3); norad_id=0 — не опознан.
	NoradID   int    `json:"norad_id,omitempty"`
	SatName   string `json:"sat_name,omitempty"`
	SatSource string `json:"sat_source,omitempty"`
	// ProductID — сборка снимка (GEOSCAN), если кусок ушёл в products.
	ProductID string `json:"product_id,omitempty"`
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
	SquelchAuto  bool    `json:"squelch_auto"`
	SquelchOpen  bool    `json:"squelch_open"`
	FramesGood   int     `json:"frames_good"`
	FramesBad    int     `json:"frames_bad"`
	LastPacketMs uint64  `json:"last_packet_ms"`
	// DemodOff — оператор выключил демодулятор: спектр идёт, кадров и снимков нет.
	DemodOff    bool    `json:"demod_off,omitempty"`
	BandwidthHz uint32  `json:"bandwidth_hz"`
	OffsetHz    int64   `json:"offset_hz"`      // по сдвигу, полосе, порогу и профилю клиенты следуют за каналом
	Baud        float64 `json:"baud,omitempty"` // символьная скорость канала; 0 — дефолт профиля
	// DopplerAuto — режим «Авто». DopplerNorad — КА, за которым следует канал.
	// DopplerManualHz — поправка оператора в режиме «Вручную».
	// DopplerHz — действующая поправка сейчас. Нет поля: поправки нет.
	// Панель держит offset_hz в нуле и сдвигает график на doppler_hz: столбец в середине экрана.
	DopplerAuto     bool     `json:"doppler_auto"`
	DopplerNorad    int      `json:"doppler_norad,omitempty"`
	DopplerManualHz int64    `json:"doppler_manual_hz"`
	DopplerHz       *float64 `json:"doppler_hz,omitempty"`
	// GainDB — усиление приёмника, отрицательное при АРУ; нет, если приёмник его не сообщил.
	GainDB *float32 `json:"gain_db,omitempty"`
	// Reception — меры хода приёма; нет, пока уровень в канале не поднимался выше порога.
	Reception *ReceptionStages `json:"reception,omitempty"`
	// Picture — картинка в приёме (профиль SSTV); frames_good у него — число картинок.
	Picture *PictureProgress `json:"picture,omitempty"`
	// TxMarkers — передатчики КА группы в окне приёмника, частота с Доплером; нет — поток не идёт.
	TxMarkers []TxMarker `json:"tx_markers,omitempty"`
	// Режим «Авто»: фаза подбора и закреплённый профиль (demod остаётся "auto").
	AutoLocked     string              `json:"auto_locked,omitempty"`
	AutoPhase      string              `json:"auto_phase,omitempty"` // search | locked
	AutoCandidates int                 `json:"auto_candidates,omitempty"`
	AutoIDs        []string            `json:"auto_ids,omitempty"`       // кандидаты в работе
	AutoStats      []AutoCandidateStat `json:"auto_stats,omitempty"`     // сводка по активным кандидатам
	DroppedFrames  uint64              `json:"dropped_frames,omitempty"` // пропуски IQ-фреймов в кольце
}

// ReceptionStages — по одной мере на этап приёма: несущая, такты, синхрослова, FEC.
type ReceptionStages struct {
	CarrierHz   float64    `json:"carrier_hz"`             // смещение от центра канала
	DeviationHz float64    `json:"deviation_hz,omitempty"` // нет — демодулятор её не меряет
	ClockJitter float64    `json:"clock_jitter"`           // ошибка фазы тактов, доли символа
	SNRDB       float32    `json:"snr_db,omitempty"`       // превышение сигнала над шумом в полосе; нет — не измерялось
	Sync        *SyncStats `json:"sync,omitempty"`         // нет — кадры без синхрослова
	FEC         *FECStats  `json:"fec,omitempty"`          // нет — профиль без FEC (ADR-009 §9.3)
}

// SyncStats — найденные синхрослова и ошибочные биты в них.
type SyncStats struct {
	Words     int `json:"words"`
	BitErrors int `json:"bit_errors"`
}

// FECStats — исправления канального кода (Golay / RS) за сеанс профиля.
type FECStats struct {
	Corrected int `json:"corrected"`
}

// Run демодулирует поток тракта до его закрытия. Подходит как sdr.StreamTap.
// Настройки канала читаются на каждый блок, так что изменения оператора применяются сразу.
func (s *Service) Run(pathID string, frames <-chan sdr.IQFrame) {
	chain := NewChain()
	auto := NewAutoPicker()
	var (
		cs           sdr.ChannelSettings
		freq         uint64
		lastStatus   time.Time
		lastDropLog  time.Time
		lastDropped  uint64
		droppedTotal uint64
		picture      pictureState
		demodOff     bool
		st           Status // состояние после последнего принятого блока
	)
	dc := s.dropCounter()
	for f := range frames {
		if dc != nil {
			droppedTotal = dc.DroppedFrames(pathID, frames)
			if droppedTotal > lastDropped {
				now := s.now()
				if lastDropLog.IsZero() || now.Sub(lastDropLog) >= dropLogInterval {
					slog.Warn("demod: IQ frames dropped",
						slog.String("radio_path", pathID),
						slog.Uint64("dropped_delta", droppedTotal-lastDropped),
						slog.Uint64("dropped_total", droppedTotal),
					)
					lastDropped = droppedTotal
					lastDropLog = now
				}
			}
		}
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
				s.setPictureActive(pathID, false)
				chain = NewChain()
				// После выключения не хватать ту же передачу mid-join: только новый VIS.
				chain.sstvVISOnly = true
				auto = NewAutoPicker()
				demodOff = true
				s.sendStatus(pathID, true, freq, cs,
					Status{DroppedFrames: droppedTotal}, s.bandMarkers(f.CenterHz, f.SampleRate))
				lastStatus = s.now()
			} else if now := s.now(); now.Sub(lastStatus) >= statusInterval {
				s.sendStatus(pathID, true, freq, cs,
					Status{DroppedFrames: droppedTotal}, s.bandMarkers(f.CenterHz, f.SampleRate))
				lastStatus = now
			}
			continue
		}
		demodOff = false
		if cs.Demod == DemodAuto {
			st = s.runAuto(pathID, f, cs, freq, auto, &picture)
		} else {
			auto = NewAutoPicker()
			doppler := s.applyDoppler(chain, cs, freq)
			for _, p := range chain.Process(f, cs) {
				s.emitTelemetry(pathID, cs, p)
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
		}
		st.DroppedFrames = droppedTotal
		s.setPictureActive(pathID, st.Picture != nil)
		if now := s.now(); lastStatus.IsZero() || now.Sub(lastStatus) >= statusInterval {
			s.sendStatus(pathID, true, freq, cs, st, s.bandMarkers(f.CenterHz, f.SampleRate))
			lastStatus = now
		}
	}
	st.DroppedFrames = droppedTotal
	s.setPictureActive(pathID, false)
	s.sendStatus(pathID, false, freq, cs, st, nil)
	if picture.id != "" {
		// Поток закрылся посреди картинки: сохраняется то, что успело проявиться.
		s.finishPicture(picture.id, nil)
	}
}

// runAuto — один блок в режиме подбора профиля: кандидаты, CRC, телеметрия от победителя,
// а также картинки SSTV и звук CW при закреплении на некадровом профиле.
func (s *Service) runAuto(
	pathID string,
	f sdr.IQFrame,
	cs sdr.ChannelSettings,
	freq uint64,
	auto *AutoPicker,
	picture *pictureState,
) Status {
	hints := s.autoHints(pathID, cs, freq)
	key := autoConfigKey(cs.DopplerNorad, freq, cs.BandwidthHz, hints)
	plan := BuildCandidatePlan(hints)
	if hints.Accepted != "" {
		auto.SeedMemory(key, hints.Accepted)
	}
	auto.ConfigurePlan(key, plan)
	doppler := s.dopplerShift(cs, freq)
	var dopplerHz float64
	if doppler != nil {
		dopplerHz = *doppler
	}
	pkts, st := auto.Process(f, cs, dopplerHz)
	st.DopplerHz = doppler
	for _, tp := range pkts {
		evCS := cs
		evCS.Demod = tp.Profile
		s.emitTelemetry(pathID, evCS, tp.Packet)
	}
	s.publishAudio(pathID, f.TimestampMs, freq, auto.Audio())
	s.publishPictures(pathID, freq, f.TimestampMs, auto.Pictures(), picture)
	if picture.id != "" && st.Picture == nil {
		s.finishPicture(picture.id, nil)
		picture.id = ""
	}
	return st
}

func (s *Service) autoHints(pathID string, cs sdr.ChannelSettings, freq uint64) CandidateHints {
	h := CandidateHints{KnownSat: cs.DopplerNorad > 0}
	var detectedMode string
	var detectedBaud float64
	if s.rfProfiles != nil && cs.DopplerNorad > 0 {
		if p, ok := s.rfProfiles.RFProfile(cs.DopplerNorad); ok {
			if tx, ok := rfprofile.FindTx(p, "", int64(freq)); ok { //nolint:gosec // частоты канала < MaxInt64
				eff := rfprofile.Effective(tx)
				switch eff.Source {
				case rfprofile.SourceAccepted:
					if allowedAutoCandidate(eff.Demod) {
						h.Accepted = eff.Demod
					}
				case rfprofile.SourceDetected:
					if allowedAutoCandidate(eff.Demod) {
						h.Blind = eff.Demod
					}
					for _, alt := range eff.DemodAlt {
						if allowedAutoCandidate(alt) {
							h.BlindAlt = append(h.BlindAlt, alt)
						}
					}
					detectedMode, detectedBaud = eff.Modulation, eff.Baud
				}
			}
		}
	}
	if s.formats != nil && cs.DopplerNorad > 0 {
		if p, ok := s.formats.ProfileFor(cs.DopplerNorad, freq); ok {
			h.CardProfile = p
		}
	}
	if s.transmitters != nil && cs.DopplerNorad > 0 {
		if txHints := MatchTXModeBauds(s.transmitters.ExpectedTransmitters(), cs.DopplerNorad, freq); len(txHints) > 0 {
			h.TXHints = txHints
			h.Mode, h.Baud = txHints[0].Mode, txHints[0].Baud
		}
	}
	if h.Mode == "" && detectedMode != "" {
		h.Mode, h.Baud = detectedMode, detectedBaud
	}
	// Подсказка эфирного классификатора применяется только для неизвестного Эфира
	// (DopplerNorad == 0) или если по КА вообще нет данных в реестре/карточке/радиопрофиле.
	hasSatData := h.Accepted != "" || h.CardProfile != "" || len(h.TXHints) > 0 ||
		h.Blind != "" || len(h.BlindAlt) > 0
	if !h.KnownSat || !hasSatData {
		s.applyAirHints(pathID, freq, &h)
	}
	return h
}

func (s *Service) applyAirHints(pathID string, freq uint64, h *CandidateHints) {
	if s.airHints == nil {
		return
	}
	demod, alt, mode, baud, ok := s.airHints.AirHint(pathID, freq)
	if !ok {
		return
	}
	if h.Blind == "" && allowedAutoCandidate(demod) {
		h.Blind = demod
	}
	for _, a := range alt {
		if allowedAutoCandidate(a) {
			h.BlindAlt = append(h.BlindAlt, a)
		}
	}
	if mode != "" {
		h.AirMode, h.AirBaud = mode, baud
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

// emitTelemetry шлёт кадр в SSE и при известном NORAD пишет слой «принято» в радиопрофиль.
func (s *Service) emitTelemetry(pathID string, cs sdr.ChannelSettings, p Packet) {
	ev := s.telemetryEvent(pathID, cs, p)
	s.publishGeoscanChunk(pathID, &ev, p)
	s.send(EventTelemetry, ev)
	// Сырая «линия» без CRC не удерживает стоянку обхода, кадры с проверкой целостности — да.
	if ev.Kind != payload.KindLine {
		s.recordRxFrame(pathID, s.now().UnixMilli())
	}
	kind := ev.Kind
	if kind == "" {
		kind = payload.KindUnknown
	}
	summary := ev.Summary
	if summary == "" {
		summary = payload.Label(kind)
	}
	s.noteAir(pathID, ev.FrequencyHz, AirNote{
		Kind: kind, Summary: summary, Decoder: ev.Decoder,
		Src: ev.Src, Dest: ev.Dest, Info: ev.Info, InfoHex: ev.InfoHex, Params: ev.Params,
		TimestampMs: ev.TimestampMs, NoradID: ev.NoradID, SatName: ev.SatName, CountFrame: true,
	})
	if s.rfFrames == nil || ev.NoradID <= 0 || ev.Demod == "" || ev.FrequencyHz == 0 || !isFrameProfile(ev.Demod) {
		return
	}
	s.rfFrames.RecordFrame(ev.NoradID, int64(ev.FrequencyHz), ev.Demod, int64(ev.TimestampMs)) //nolint:gosec // метки времени кадра < MaxInt64
}

// publishGeoscanChunk собирает JPEG из кусков geoscan.image в products и шлёт SSE product.
func (s *Service) publishGeoscanChunk(pathID string, ev *TelemetryEvent, p Packet) {
	if s == nil || s.products == nil || ev == nil || ev.Kind != payload.KindImage {
		return
	}
	pl := payload.Decode(payload.Frame{
		Raw: p.Raw, Profile: ev.Demod, FrequencyHz: p.FrequencyHz, TimestampMs: p.TimestampMs,
	})
	if pl.Decoder != "geoscan.image" || len(pl.Chunk) == 0 {
		return
	}
	prod := s.products.PutGeoscanChunk(products.Meta{
		RadioPath: pathID, Kind: products.KindGEOSCAN, Mode: "geoscan", ModeName: "GEOSCAN",
		FrequencyHz: ev.FrequencyHz, NoradID: ev.NoradID,
	}, pl.Offset, pl.Chunk)
	if prod.ID == "" {
		return
	}
	ev.ProductID = prod.ID
	if prod.Height > 0 {
		pct := 100 * prod.Lines / prod.Height
		if pct > 100 {
			pct = 100
		}
		ev.Summary = fmt.Sprintf("Снимок · %d%% · кусков %d", pct, prod.Chunks)
	}
	s.send(EventProduct, prod)
}

// noteAir — участок эфира на частоте приёма (кадр или голос).
func (s *Service) noteAir(pathID string, freqHz uint64, note AirNote) {
	if s.airHear == nil || pathID == "" || freqHz == 0 || note.Kind == "" {
		return
	}
	if note.SatName == "" && note.NoradID > 0 && s.names != nil {
		note.SatName = s.names.SatName(note.NoradID)
	}
	s.airHear.RecordRx(pathID, freqHz, note)
}

// telemetryEvent собирает SSE-кадр и привязывает его к КА (ADR-009 §6.3).
func (s *Service) telemetryEvent(pathID string, cs sdr.ChannelSettings, p Packet) TelemetryEvent {
	ev := telemetryEvent(pathID, cs.Demod, p)
	bind := s.resolveBinding(pathID, cs, ev.Src, ev.Dest, p.FrequencyHz)
	if bind.NoradID > 0 {
		ev.NoradID = bind.NoradID
		ev.SatName = bind.SatName
		ev.SatSource = bind.Source
	}
	return ev
}

func telemetryEvent(pathID, demod string, p Packet) TelemetryEvent {
	raw := p.Raw
	if len(raw) == 0 && len(p.Frame.Info) > 0 {
		// Старые тесты собирают Packet только из Frame — восстанавливаем тело для разбора.
		if b, err := p.Frame.Bytes(); err == nil && len(b) >= 2 {
			raw = b[:len(b)-2]
		}
	}
	pl := payload.Decode(payload.Frame{
		Raw:         raw,
		Profile:     demod,
		FrequencyHz: p.FrequencyHz,
		TimestampMs: p.TimestampMs,
	})
	ev := TelemetryEvent{
		RadioPath:   pathID,
		TimestampMs: p.TimestampMs,
		FrequencyHz: p.FrequencyHz,
		Demod:       demod,
		Src:         pl.Src,
		Dest:        pl.Dest,
		Info:        pl.Info,
		InfoHex:     pl.InfoHex,
		Kind:        pl.Kind,
		Decoder:     pl.Decoder,
		Summary:     pl.Summary,
	}
	if ev.Src == "" && p.Frame.Src.Call != "" {
		ev.Src = callsign(p.Frame.Src)
	}
	if ev.Dest == "" && p.Frame.Dest.Call != "" {
		ev.Dest = callsign(p.Frame.Dest)
	}
	if len(pl.Params) > 0 {
		ev.Params = make([]TelemetryParam, len(pl.Params))
		for i, prm := range pl.Params {
			ev.Params[i] = TelemetryParam{ID: prm.ID, Name: prm.Name, Value: prm.Value, Unit: prm.Unit, Key: prm.Key}
		}
	}
	return ev
}

func (s *Service) resolveBinding(pathID string, cs sdr.ChannelSettings, src, dest string, freqHz uint64) SatBinding {
	if s == nil {
		return SatBinding{}
	}
	in := ResolveInput{
		SrcCall: src, DestCall: dest,
		FrequencyHz: freqHz, BandwidthHz: cs.BandwidthHz,
		Now: s.now(), Doppler: s.doppler, Horizon: s.horizon,
		Callsigns: s.callsigns, Names: s.names,
	}
	if s.transmitters != nil {
		in.Transmitters = s.transmitters.ExpectedTransmitters()
	}
	if s.pathSats != nil {
		if h, ok := s.pathSats.Hint(pathID); ok {
			in.Path = h
		}
	}
	return ResolveSat(in)
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
	s.noteAirVoice(pathID, ev)
}

// noteAirVoice — при открытом пороге голосового профиля (в ручном режиме) пишем kind=voice в эфир.
func (s *Service) noteAirVoice(pathID string, ev StatusEvent) {
	if !ev.Active || !ev.SquelchOpen || ev.DemodOff {
		return
	}
	if !voiceDemod(ev.Demod) {
		return
	}
	norad := ev.DopplerNorad
	ts := ev.LastPacketMs
	if ts == 0 {
		ts = uint64(s.now().UnixMilli()) //nolint:gosec // UnixMilli > 0
	}
	s.noteAir(pathID, ev.FrequencyHz, AirNote{
		Kind: payload.KindVoice, Summary: payload.Label(payload.KindVoice), Decoder: ev.Demod,
		TimestampMs: ts, NoradID: norad, CountFrame: false,
	})
}

func statusEvent(pathID string, active bool, freq uint64, cs sdr.ChannelSettings, st Status) StatusEvent {
	dropped := st.DroppedFrames
	if cs.DemodOff {
		st = Status{}
	}
	demod := st.Demod
	if demod == "" {
		demod = cs.Demod
	}
	sqAuto := cs.SquelchAuto || demod == DemodAuto
	sqDBFS := cs.SquelchDBFS
	if !cs.DemodOff && active && st.SquelchAuto {
		sqAuto = true
		sqDBFS = st.SquelchDBFS
	}
	ev := StatusEvent{
		RadioPath:       pathID,
		Active:          active,
		Demod:           demod,
		DemodOff:        cs.DemodOff,
		Supported:       st.Supported,
		FrequencyHz:     freq,
		LevelDBFS:       st.LevelDBFS,
		SquelchDBFS:     sqDBFS,
		SquelchAuto:     sqAuto,
		SquelchOpen:     active && st.SquelchOpen,
		FramesGood:      st.Good,
		FramesBad:       st.Bad,
		LastPacketMs:    st.LastPacketMs,
		BandwidthHz:     cs.BandwidthHz,
		OffsetHz:        cs.OffsetHz,
		Baud:            cs.Baud,
		DopplerAuto:     cs.DopplerAuto,
		DopplerNorad:    cs.DopplerNorad,
		DopplerManualHz: cs.DopplerManualHz,
		DopplerHz:       st.DopplerHz,
		Picture:         st.Picture,
		AutoLocked:      st.AutoLocked,
		AutoPhase:       st.AutoPhase,
		AutoCandidates:  st.AutoCandidates,
		AutoIDs:         st.AutoIDs,
		AutoStats:       st.AutoStats,
		DroppedFrames:   dropped,
	}
	if st.Measured {
		ev.Reception = &ReceptionStages{CarrierHz: st.CarrierHz, DeviationHz: st.DeviationHz,
			ClockJitter: st.ClockJitter, SNRDB: st.SNRDB}
		if st.SyncCounted {
			ev.Reception.Sync = &SyncStats{Words: st.SyncWords, BitErrors: st.SyncBitErrors}
		}
		if st.FECCounted {
			ev.Reception.FEC = &FECStats{Corrected: st.FECCorrected}
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
