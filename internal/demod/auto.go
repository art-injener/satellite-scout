package demod

import (
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/art-injener/satellite-scout/internal/m10"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

// Окна автоподбора по умолчанию.
const (
	autoWinDefault    = 30 * time.Second // окно накопления кадров для HDLC без FEC
	autoReviewDefault = 30 * time.Second // без кадров после закрепления запускается повторный подбор
	autoNeedGoodHDLC  = 2
	autoNeedGoodFEC   = 1
)

// demodKey — ключ общего линейного демодулятора поверх узкого канала 64 или 32 кГц.
type demodKey string

const (
	demodKeyFSK1200   demodKey = "fsk1200"
	demodKeyFSK2400   demodKey = "fsk2400"
	demodKeyFSK4800   demodKey = "fsk4800"
	demodKeyFSK9600   demodKey = "fsk9600"
	demodKeyFSKM10    demodKey = "fsk_m10"
	demodKeyAFSK1200  demodKey = "afsk1200"
	demodKeyBPSK1200  demodKey = "bpsk1200"
	demodKeyBPSK9600  demodKey = "bpsk9600"
	demodKeyQPSKCCSDS demodKey = "qpsk_ccsds"
)

type autoProfileSpec struct {
	key    demodKey
	need32 bool   // канал 32 кГц вместо 64 кГц
	g3ruh  bool   // пропустить биты через общий дескремблер G3RUH
	dfKind string // тип выделителя кадров для дедупликации идентичных трактов
}

// pipelineSig определяет физически эквивалентную ветку (демодулятор, дескремблер, выделитель кадров).
type pipelineSig struct {
	key    demodKey
	g3ruh  bool
	dfKind string
}

func (s autoProfileSpec) sig() pipelineSig {
	return pipelineSig{key: s.key, g3ruh: s.g3ruh, dfKind: s.dfKind}
}

// autoSpecFor возвращает общий линейный демодулятор и тип дефреймера для кадрового профиля.
// FSK/GFSK/GMSK одной скорости имеют идентичную физику в FSKDemod и разделяют один поток бит.
func autoSpecFor(id string) autoProfileSpec {
	switch id {
	case "fsk1200", "gfsk1200":
		return autoProfileSpec{key: demodKeyFSK1200, dfKind: "hdlc"}
	case "gmsk2400":
		return autoProfileSpec{key: demodKeyFSK2400, dfKind: "hdlc"}
	case "gmsk2400_usp":
		return autoProfileSpec{key: demodKeyFSK2400, dfKind: "usp"}
	case "fsk4800", "gfsk4800", "gmsk4800":
		return autoProfileSpec{key: demodKeyFSK4800, dfKind: "hdlc"}
	case "fsk4800_ax100", "gmsk4800_ax100":
		return autoProfileSpec{key: demodKeyFSK4800, dfKind: "ax100"}
	case "gmsk4800_usp":
		return autoProfileSpec{key: demodKeyFSK4800, dfKind: "usp"}
	case "rs41":
		return autoProfileSpec{key: demodKeyFSK4800, dfKind: "rs41"}
	case "fsk9600_geoscan":
		return autoProfileSpec{key: demodKeyFSK9600, dfKind: "geoscan"}
	case "fsk9600_ax100_rs", "gmsk9600_ax100_rs":
		return autoProfileSpec{key: demodKeyFSK9600, dfKind: "ax100_rs"}
	case "gmsk9600_ax100":
		return autoProfileSpec{key: demodKeyFSK9600, dfKind: "ax100"}
	case "gmsk9600_usp":
		return autoProfileSpec{key: demodKeyFSK9600, dfKind: "usp"}
	case "fsk9600_g3ruh", "gfsk9600", "gmsk9600":
		return autoProfileSpec{key: demodKeyFSK9600, g3ruh: true, dfKind: "hdlc"}
	case "m10":
		return autoProfileSpec{key: demodKeyFSKM10, dfKind: "m10"}
	case "afsk1200":
		return autoProfileSpec{key: demodKeyAFSK1200, need32: true, dfKind: "hdlc"}
	case "bpsk1200":
		return autoProfileSpec{key: demodKeyBPSK1200, need32: true, dfKind: "hdlc"}
	case "bpsk9600":
		return autoProfileSpec{key: demodKeyBPSK9600, dfKind: "hdlc"}
	case "qpsk_ccsds":
		return autoProfileSpec{key: demodKeyQPSKCCSDS, dfKind: "ccsds"}
	default:
		p := profiles[id]
		return autoProfileSpec{key: demodKey("custom:" + id), need32: p.fineDecim == 2, dfKind: "custom:" + id}
	}
}

// needGoodFor возвращает число валидных кадров, достаточное для закрепления профиля.
// Профили с синхрословом и сильным FEC/CRC (AX.100, USP, GEOSCAN, RS41, CCSDS) закрепляются с 1 кадра;
// профили HDLC без FEC (и M10) требуют 2 кадра в окне.
func needGoodFor(id string) int {
	switch id {
	case "fsk4800_ax100", "gmsk4800_ax100", "gmsk9600_ax100",
		"fsk9600_ax100_rs", "gmsk9600_ax100_rs",
		"gmsk2400_usp", "gmsk4800_usp", "gmsk9600_usp",
		demodGEOSCAN, "rs41", "qpsk_ccsds":
		return autoNeedGoodFEC
	default:
		return autoNeedGoodHDLC
	}
}

func newSharedLineDemod(key demodKey, id string, midRate float64) lineDemod {
	switch key {
	case demodKeyFSK1200:
		return modem.NewFSKDemod(modem.FSK1200(midRate))
	case demodKeyFSK2400:
		return modem.NewFSKDemod(modem.GMSK2400(midRate))
	case demodKeyFSK4800:
		return modem.NewFSKDemod(modem.FSK4800(midRate))
	case demodKeyFSK9600:
		return modem.NewFSKDemod(modem.FSK9600(midRate))
	case demodKeyFSKM10:
		return modem.NewFSKDemod(modem.FSKConfig{
			SampleRate: midRate, Baud: m10.Baud, DeviationHz: 3000, BT: 0.5,
		})
	case demodKeyAFSK1200:
		return modem.NewAFSKDemod(modem.AFSK1200(midRate / 2))
	case demodKeyBPSK1200:
		return modem.NewBPSKDemod(modem.BPSK1200(midRate / 2))
	case demodKeyBPSK9600:
		return modem.NewBPSKDemod(modem.BPSK9600(midRate))
	case demodKeyQPSKCCSDS:
		return modem.NewQPSKDemod(modem.QPSKCCSDS(midRate))
	default:
		p := profiles[id]
		if p.newDemod != nil {
			r := midRate
			if p.fineDecim > 1 {
				r /= float64(p.fineDecim)
			}
			return p.newDemod(r)
		}
		return nil
	}
}

// AutoPicker — параллельный подбор кадрового профиля на одном общем канале 64 кГц.
// Децимация IQ выполняется один раз на блок, линейные демодуляторы расшарены по скоростям
// и питают все активные дефреймеры одновременно без жёсткого лимита кандидатов.
// Поддерживаются некадровые кандидаты sstv (по заголовку VIS) и cw (по телеграфным посылкам).
// Успешный выбор кадрового профиля помнится по ключу условий и восстанавливается при возврате.
type AutoPicker struct {
	window time.Duration
	review time.Duration
	now    func() time.Time

	key        string
	candidates []string
	frontend   channelFrontend
	demodRate  float64
	demods     map[demodKey]lineDemod
	g3ruh      modem.G3RUHDescrambler
	deframers  map[pipelineSig]deframer
	hits       map[string][]time.Time

	sstvRx      pictureRx
	cwDet       *cwDetector
	pictures    []sstv.Event
	lastPicture *PictureProgress
	hasPicture  bool
	audio       []float32

	pinned       string
	lastGood     time.Time
	lastPacketMs uint64
	memory       map[string]string // ключ условий соответствует закреплённому профилю

	bitsBuf  map[demodKey][]byte
	g3ruhBuf []byte
	frameBuf [][]byte
}

// NewAutoPicker создаёт подборщик с окнами по умолчанию.
func NewAutoPicker() *AutoPicker {
	return &AutoPicker{
		window:    autoWinDefault,
		review:    autoReviewDefault,
		now:       time.Now,
		demods:    make(map[demodKey]lineDemod),
		deframers: make(map[pipelineSig]deframer),
		hits:      make(map[string][]time.Time),
		memory:    make(map[string]string),
		bitsBuf:   make(map[demodKey][]byte),
	}
}

// SeedMemory задаёт закрепление из радиопрофиля (слой «принято»), чтобы pin
// переживал перезапуск сервера. Не затирает уже выбранный в этой сессии профиль.
func (a *AutoPicker) SeedMemory(key, id string) {
	if a == nil || key == "" || id == "" {
		return
	}
	if a.memory == nil {
		a.memory = make(map[string]string)
	}
	if a.memory[key] != "" {
		return
	}
	a.memory[key] = id
}

// Configure обновляет ключ условий и список кандидатов.
// При том же ключе обновление подсказок выполняется инкрементально: накопленные попадания
// и работающие дефреймеры с демодуляторами сохраняются. При смене ключа прежнее закрепление
// запоминается в memory и восстанавливается при возврате.
func (a *AutoPicker) Configure(key string, candidates []string) {
	if a == nil {
		return
	}
	if a.memory == nil {
		a.memory = make(map[string]string)
	}
	if a.deframers == nil {
		a.deframers = make(map[pipelineSig]deframer)
	}
	if a.hits == nil {
		a.hits = make(map[string][]time.Time)
	}
	if a.demods == nil {
		a.demods = make(map[demodKey]lineDemod)
	}
	if a.bitsBuf == nil {
		a.bitsBuf = make(map[demodKey][]byte)
	}

	sameKey := key == a.key
	sameCand := slicesEqual(candidates, a.candidates)
	if sameKey && sameCand {
		return
	}

	if sameKey {
		a.configureSameKey(key, candidates)
		return
	}

	if a.key != "" && a.pinned != "" && !isAutoNonFrameProfile(a.pinned) {
		a.remember(a.key, a.pinned)
	}
	a.key = key
	a.candidates = append([]string(nil), candidates...)
	a.deframers = make(map[pipelineSig]deframer, len(a.candidates))
	a.hits = make(map[string][]time.Time)
	a.demods = make(map[demodKey]lineDemod)
	a.g3ruh = modem.G3RUHDescrambler{}
	a.demodRate = 0
	a.sstvRx = nil
	a.cwDet = nil
	a.pictures = nil
	a.lastPicture = nil
	a.hasPicture = false
	a.audio = nil
	for _, id := range a.candidates {
		if isAutoNonFrameProfile(id) {
			continue
		}
		sig := autoSpecFor(id).sig()
		if a.deframers[sig] == nil {
			if df := newProfileDeframer(id); df != nil {
				a.deframers[sig] = df
			}
		}
	}
	a.pinned = ""
	a.lastGood = time.Time{}
	a.lastPacketMs = 0
	if mem := a.memory[key]; mem != "" && candidateHas(a.candidates, mem) {
		a.pinned = mem
		a.lastGood = a.now() // свежий заход: не считать простой с прошлого hop
	}
	slog.Info("demod auto: configure",
		slog.String("key", key),
		slog.Int("candidates", len(a.candidates)),
		slog.String("pinned", a.pinned),
	)
}

func newProfileDeframer(id string) deframer {
	p, ok := profiles[id]
	if !ok || p.newDeframer == nil {
		return nil
	}
	return p.newDeframer()
}

func (a *AutoPicker) configureSameKey(key string, candidates []string) {
	a.candidates = append([]string(nil), candidates...)
	keepID := make(map[string]bool, len(a.candidates))
	keepSig := make(map[pipelineSig]bool, len(a.candidates))
	for _, id := range a.candidates {
		keepID[id] = true
		if isAutoNonFrameProfile(id) {
			continue
		}
		sig := autoSpecFor(id).sig()
		keepSig[sig] = true
		a.ensureDeframer(id, sig)
	}
	for sig := range a.deframers {
		if !keepSig[sig] {
			delete(a.deframers, sig)
		}
	}
	for id := range a.hits {
		if !keepID[id] {
			delete(a.hits, id)
		}
	}
	if !keepID[DemodSSTV] {
		a.sstvRx = nil
		a.lastPicture = nil
		a.hasPicture = false
	}
	if !keepID[DemodCW] {
		a.cwDet = nil
	}
	if a.pinned != "" && !keepID[a.pinned] {
		a.pinned = ""
		a.lastGood = time.Time{}
	}
	if a.pinned == "" {
		if mem := a.memory[key]; mem != "" && keepID[mem] {
			a.pinned = mem
			a.lastGood = a.now()
		}
	}
	slog.Info("demod auto: configure",
		slog.String("key", key),
		slog.Int("candidates", len(a.candidates)),
		slog.String("pinned", a.pinned),
	)
}

func (a *AutoPicker) ensureDeframer(id string, sig pipelineSig) {
	if a.deframers[sig] != nil {
		return
	}
	if df := newProfileDeframer(id); df != nil {
		a.deframers[sig] = df
	}
}

// Locked — закреплённый профиль; пусто, пока идёт подбор.
func (a *AutoPicker) Locked() string {
	if a == nil {
		return ""
	}
	return a.pinned
}

// Candidates — текущий список id.
func (a *AutoPicker) Candidates() []string {
	if a == nil {
		return nil
	}
	return append([]string(nil), a.candidates...)
}

// Pictures — события картинок SSTV последнего блока в режиме «Авто».
func (a *AutoPicker) Pictures() []sstv.Event {
	if a == nil {
		return nil
	}
	return a.pictures
}

// Audio — звук последнего блока (SSTV или CW) в режиме «Авто».
func (a *AutoPicker) Audio() []float32 {
	if a == nil {
		return nil
	}
	return a.audio
}

type taggedPacket struct {
	Profile string
	Packet  Packet
}

// Process гоняет всех кандидатов (или только закреплённый) поверх одного канального фронтенда.
func (a *AutoPicker) Process(f sdr.IQFrame, base sdr.ChannelSettings, dopplerHz float64) (pkts []taggedPacket, st Status) {
	if a == nil || len(a.candidates) == 0 {
		st.Demod = DemodAuto
		st.Supported = true
		st.AutoPhase = "search"
		return nil, st
	}
	a.pictures = a.pictures[:0]
	a.audio = a.audio[:0]
	now := a.now()
	if a.pinned != "" {
		return a.processPinned(f, base, dopplerHz, now)
	}
	return a.processSearch(f, base, dopplerHz, now)
}

func (a *AutoPicker) ensureDemodRate(midRate float64) {
	if midRate <= 0 || midRate == a.demodRate {
		return
	}
	a.demodRate = midRate
	a.demods = make(map[demodKey]lineDemod)
	a.g3ruh = modem.G3RUHDescrambler{}
	a.sstvRx = nil
	a.cwDet = nil
}

func (a *AutoPicker) ensureSSTVRx() pictureRx {
	if a.sstvRx == nil && a.frontend.midRate > 0 {
		a.sstvRx = newSSTVRxVISOnly(a.frontend.midRate / 2)
	}
	return a.sstvRx
}

func (a *AutoPicker) ensureCWDet() *cwDetector {
	if a.cwDet == nil && a.frontend.midRate > 0 {
		a.cwDet = newCWDetector(a.frontend.midRate)
	}
	return a.cwDet
}

func (a *AutoPicker) lineDemod(spec autoProfileSpec, id string) lineDemod {
	d := a.demods[spec.key]
	if d == nil {
		d = newSharedLineDemod(spec.key, id, a.frontend.midRate)
		if d != nil {
			a.demods[spec.key] = d
		}
	}
	return d
}

func (a *AutoPicker) deframerFor(id string, sig pipelineSig) deframer {
	df := a.deframers[sig]
	if df == nil {
		df = newProfileDeframer(id)
		if df != nil {
			a.deframers[sig] = df
		}
	}
	return df
}

func (a *AutoPicker) processPinned(f sdr.IQFrame, base sdr.ChannelSettings, dopplerHz float64, now time.Time) ([]taggedPacket, Status) {
	id := a.pinned
	spec := autoSpecFor(id)
	need32 := spec.need32 || id == DemodSSTV
	fr := a.frontend.process(f, base, dopplerHz, need32)
	a.ensureDemodRate(a.frontend.midRate)

	st := Status{
		LevelDBFS:    fr.levelDBFS,
		SquelchOpen:  fr.open,
		PassbandHz:   a.frontend.passbandHz,
		LastPacketMs: a.lastPacketMs,
	}

	var out []taggedPacket
	switch id {
	case DemodSSTV:
		if rx := a.ensureSSTVRx(); rx != nil {
			evs := rx.Push(fr.ch32)
			a.applySSTVEvents(evs, rx, fr.open, now, &st)
		}
		if fr.above {
			a.frontend.measureCarrierSNR(fr.ch64, &st)
		}
	case DemodCW:
		if det := a.ensureCWDet(); det != nil {
			a.audio = append(a.audio[:0], det.PushAudio(fr.ch64, a.frontend.midRate, fr.open)...)
		}
		if fr.open {
			a.lastGood = now
		}
		if fr.above {
			a.frontend.measureCarrierSNR(fr.ch64, &st)
		}
	default:
		df := a.deframerFor(id, spec.sig())
		dem := a.lineDemod(spec, id)
		if fr.open && dem != nil && df != nil {
			out = a.runPinnedBlock(id, spec, dem, df, fr, f, base, now, &st)
		} else if df != nil {
			st.Good, st.Bad = df.Good(), df.Bad()
		}
	}
	a.frontend.updatePrev(fr.ch64)

	fillAutoStatus(&st, a.pinned, "locked", a.candidates)
	if !a.hasPicture && !a.lastGood.IsZero() && now.Sub(a.lastGood) >= a.review {
		prev := a.pinned
		a.forget(a.key)
		a.unpin()
		slog.Info("demod auto: unpin",
			slog.String("key", a.key),
			slog.String("previous", prev),
		)
		fillAutoStatus(&st, "", "search", a.candidates)
	}
	return out, st
}

func (a *AutoPicker) applySSTVEvents(evs []sstv.Event, rx pictureRx, open bool, now time.Time, st *Status) {
	a.pictures = append(a.pictures[:0], evs...)
	for _, e := range evs {
		switch e.Kind {
		case sstv.EventDone:
			a.hasPicture = false
			a.lastPicture = nil
			a.lastGood = now
		case sstv.EventStart, sstv.EventLines:
			a.hasPicture = true
			a.lastGood = now
			a.lastPicture = &PictureProgress{
				Mode:     e.Mode.ID,
				ModeName: e.Mode.Name,
				Lines:    e.Lines,
				Height:   e.Mode.Height,
			}
		}
	}
	if a.hasPicture {
		a.lastGood = now
	}
	st.Picture = a.lastPicture
	st.Good = rx.Done()
	a.audio = append(a.audio[:0], rx.Audio()...)
	if !open {
		clear(a.audio)
	}
}

func (a *AutoPicker) runPinnedBlock(
	id string,
	spec autoProfileSpec,
	dem lineDemod,
	df deframer,
	fr frontendResult,
	f sdr.IQFrame,
	base sdr.ChannelSettings,
	now time.Time,
	st *Status,
) []taggedPacket {
	ch := fr.ch64
	if spec.need32 {
		ch = fr.ch32
	}
	if lf, ok := dem.(levelFreezer); ok {
		lf.SetFreeze(!fr.above)
	}
	bits := dem.Demodulate(a.bitsBuf[spec.key][:0], ch)
	a.bitsBuf[spec.key] = bits
	inBits := bits
	if spec.g3ruh {
		a.g3ruhBuf = a.g3ruh.Descramble(a.g3ruhBuf[:0], bits)
		inBits = a.g3ruhBuf
	}
	a.frameBuf = df.Push(a.frameBuf[:0], inBits)
	freqHz := uint64(int64(f.CenterHz) + base.OffsetHz) //nolint:gosec // частоты канала > 0
	pkts := buildPackets(f.TimestampMs, freqHz, a.frameBuf)
	out := make([]taggedPacket, 0, len(pkts))
	for _, p := range pkts {
		out = append(out, taggedPacket{Profile: id, Packet: p})
		a.lastGood = now
		a.lastPacketMs = f.TimestampMs
	}
	st.LastPacketMs = a.lastPacketMs
	fillProfileMetrics(st, dem, df, fr.above)
	if fr.above {
		a.frontend.measureCarrierSNR(fr.ch64, st)
	}
	return out
}

func (a *AutoPicker) processSearch(f sdr.IQFrame, base sdr.ChannelSettings, dopplerHz float64, now time.Time) ([]taggedPacket, Status) {
	hasSSTV := candidateHas(a.candidates, DemodSSTV)
	hasCW := candidateHas(a.candidates, DemodCW)
	need32 := hasSSTV
	if !need32 {
		for _, id := range a.candidates {
			if !isAutoNonFrameProfile(id) && autoSpecFor(id).need32 {
				need32 = true
				break
			}
		}
	}
	fr := a.frontend.process(f, base, dopplerHz, need32)
	a.ensureDemodRate(a.frontend.midRate)

	st := Status{
		LevelDBFS:    fr.levelDBFS,
		SquelchOpen:  fr.open,
		PassbandHz:   a.frontend.passbandHz,
		LastPacketMs: a.lastPacketMs,
	}

	// SSTV декодер слушает канал непрерывно (без порога), чтобы не сдвигать фазу развёртки.
	var sstvEvs []sstv.Event
	sstvStarted := false
	if hasSSTV {
		if rx := a.ensureSSTVRx(); rx != nil {
			sstvEvs = rx.Push(fr.ch32)
			for _, e := range sstvEvs {
				if e.Kind == sstv.EventStart {
					sstvStarted = true
					break
				}
			}
		}
	}

	if !fr.open {
		if sstvStarted && a.sstvRx != nil {
			a.pinned = DemodSSTV
			a.lastGood = now
			a.applySSTVEvents(sstvEvs, a.sstvRx, fr.open, now, &st)
			slog.Info("demod auto: locked",
				slog.String("key", a.key),
				slog.String("profile", DemodSSTV),
			)
			a.frontend.updatePrev(fr.ch64)
			fillAutoStatus(&st, DemodSSTV, "locked", a.candidates)
			return nil, st
		}
		a.frontend.updatePrev(fr.ch64)
		st.AutoStats = a.collectAutoStats()
		fillAutoStatus(&st, "", "search", a.candidates)
		return nil, st
	}

	freqHz := uint64(int64(f.CenterHz) + base.OffsetHz) //nolint:gosec // частоты канала > 0
	computed := make(map[demodKey][]byte, len(a.demods)+4)
	seenSig := make(map[pipelineSig]bool, len(a.candidates))
	g3ruhReady := false
	var g3ruhBits []byte

	var out []taggedPacket
	var primaryDem lineDemod
	var primaryDF deframer
	bestGood, bestSync, bestBad := -1, -1, -1
	lockedID := ""

	for _, id := range a.candidates {
		if isAutoNonFrameProfile(id) {
			continue
		}
		spec := autoSpecFor(id)
		sig := spec.sig()
		if seenSig[sig] {
			// Эквивалентный физический тракт уже проверен более приоритетным кандидатом.
			continue
		}
		seenSig[sig] = true
		df := a.deframerFor(id, sig)
		if df == nil {
			continue
		}
		dem := a.lineDemod(spec, id)
		if dem == nil {
			continue
		}
		bits, ok := computed[spec.key]
		if !ok {
			ch := fr.ch64
			if spec.need32 {
				ch = fr.ch32
			}
			if lf, okFreeze := dem.(levelFreezer); okFreeze {
				lf.SetFreeze(!fr.above)
			}
			bits = dem.Demodulate(a.bitsBuf[spec.key][:0], ch)
			a.bitsBuf[spec.key] = bits
			computed[spec.key] = bits
		}
		inBits := bits
		if spec.g3ruh {
			if !g3ruhReady {
				a.g3ruhBuf = a.g3ruh.Descramble(a.g3ruhBuf[:0], bits)
				g3ruhBits = a.g3ruhBuf
				g3ruhReady = true
			}
			inBits = g3ruhBits
		}

		a.frameBuf = df.Push(a.frameBuf[:0], inBits)
		for _, p := range buildPackets(f.TimestampMs, freqHz, a.frameBuf) {
			out = append(out, taggedPacket{Profile: id, Packet: p})
			a.noteHit(id, now)
			a.lastPacketMs = f.TimestampMs
		}

		good, bad := df.Good(), df.Bad()
		syncWords := 0
		if sc, okSync := df.(syncCounter); okSync {
			syncWords = sc.SyncWords()
		}
		if primaryDem == nil || isBetterCandidate(good, syncWords, bad, bestGood, bestSync, bestBad) {
			primaryDem, primaryDF = dem, df
			bestGood, bestSync, bestBad = good, syncWords, bad
		}

		if lockedID == "" {
			if hits := a.hitsInWindow(id, now); hits >= needGoodFor(id) {
				lockedID = id
				primaryDem, primaryDF = dem, df
				a.pinned = id
				a.lastGood = now
				a.remember(a.key, id)
				slog.Info("demod auto: locked",
					slog.String("key", a.key),
					slog.String("profile", id),
					slog.Int("hits", hits),
				)
			}
		}
	}

	// Некадровые кандидаты закрепляются только если ни один кадровый профиль не победил.
	if lockedID == "" && sstvStarted && a.sstvRx != nil {
		lockedID = DemodSSTV
		a.pinned = DemodSSTV
		a.lastGood = now
		a.applySSTVEvents(sstvEvs, a.sstvRx, fr.open, now, &st)
		slog.Info("demod auto: locked",
			slog.String("key", a.key),
			slog.String("profile", DemodSSTV),
		)
	} else if lockedID == "" && hasCW {
		if det := a.ensureCWDet(); det != nil && det.PushSearch(fr.ch64, a.frontend.midRate) {
			lockedID = DemodCW
			a.pinned = DemodCW
			a.lastGood = now
			slog.Info("demod auto: locked",
				slog.String("key", a.key),
				slog.String("profile", DemodCW),
			)
		}
	}

	st.LastPacketMs = a.lastPacketMs
	if !isAutoNonFrameProfile(lockedID) && primaryDem != nil && primaryDF != nil {
		fillProfileMetrics(&st, primaryDem, primaryDF, fr.above)
	}
	if fr.above {
		a.frontend.measureCarrierSNR(fr.ch64, &st)
	}
	a.frontend.updatePrev(fr.ch64)

	if lockedID != "" {
		fillAutoStatus(&st, lockedID, "locked", a.candidates)
		return out, st
	}
	st.AutoStats = a.collectAutoStats()
	fillAutoStatus(&st, "", "search", a.candidates)
	return out, st
}

func isBetterCandidate(good, syncWords, bad, bestGood, bestSync, bestBad int) bool {
	if good != bestGood {
		return good > bestGood
	}
	if syncWords != bestSync {
		return syncWords > bestSync
	}
	return bad > bestBad
}

const maxAutoStats = 8

// collectAutoStats собирает компактную сводку по уникальным кадровым кандидатам
// с ненулевой активностью (синхрослова или кадры) для диагностики в фазе подбора.
func (a *AutoPicker) collectAutoStats() []AutoCandidateStat {
	seen := make(map[pipelineSig]bool, len(a.candidates))
	var stats []AutoCandidateStat
	for _, id := range a.candidates {
		if isAutoNonFrameProfile(id) {
			continue
		}
		sig := autoSpecFor(id).sig()
		if seen[sig] {
			continue
		}
		seen[sig] = true
		df := a.deframers[sig]
		if df == nil {
			continue
		}
		good, bad := df.Good(), df.Bad()
		syncWords := 0
		if sc, ok := df.(syncCounter); ok {
			syncWords = sc.SyncWords()
		}
		if good == 0 && bad == 0 && syncWords == 0 {
			continue
		}
		stats = append(stats, AutoCandidateStat{
			ID:        id,
			SyncWords: syncWords,
			Good:      good,
			Bad:       bad,
		})
	}
	if len(stats) == 0 {
		return nil
	}
	slices.SortStableFunc(stats, func(x, y AutoCandidateStat) int {
		switch {
		case x.Good != y.Good:
			return y.Good - x.Good
		case x.SyncWords != y.SyncWords:
			return y.SyncWords - x.SyncWords
		case x.Bad != y.Bad:
			return y.Bad - x.Bad
		default:
			return 0
		}
	})
	if len(stats) > maxAutoStats {
		stats = stats[:maxAutoStats]
	}
	return stats
}

func fillProfileMetrics(st *Status, dem lineDemod, df deframer, above bool) {
	st.Good, st.Bad = df.Good(), df.Bad()
	if fc, ok := df.(fecCounter); ok {
		st.FECCounted = true
		st.FECCorrected = fc.FECCorrected()
	}
	if !above {
		return
	}
	if m, ok := dem.(clockMeter); ok {
		st.ClockJitter = m.ClockJitter()
	}
	if m, ok := dem.(deviationMeter); ok {
		st.DeviationHz = m.DeviationHz()
	}
	if sc, ok := df.(syncCounter); ok {
		st.SyncCounted = true
		st.SyncWords, st.SyncBitErrors = sc.SyncWords(), sc.SyncBitErrors()
	}
}

func fillAutoStatus(st *Status, locked, phase string, candidates []string) {
	st.Demod = DemodAuto
	st.Supported = true
	st.AutoLocked = locked
	st.AutoPhase = phase
	st.AutoCandidates = len(candidates)
	st.AutoIDs = append([]string(nil), candidates...)
}

func (a *AutoPicker) noteHit(id string, now time.Time) {
	a.hits[id] = append(a.hits[id], now)
	a.hits[id] = trimHits(a.hits[id], now, a.window)
}

func (a *AutoPicker) hitsInWindow(id string, now time.Time) int {
	a.hits[id] = trimHits(a.hits[id], now, a.window)
	return len(a.hits[id])
}

func trimHits(hits []time.Time, now time.Time, win time.Duration) []time.Time {
	if win <= 0 || len(hits) == 0 {
		return hits
	}
	cut := now.Add(-win)
	i := 0
	for i < len(hits) && hits[i].Before(cut) {
		i++
	}
	return hits[i:]
}

func (a *AutoPicker) remember(key, id string) {
	if a == nil || key == "" || id == "" {
		return
	}
	if a.memory == nil {
		a.memory = make(map[string]string)
	}
	a.memory[key] = id
}

func (a *AutoPicker) forget(key string) {
	if a == nil || a.memory == nil || key == "" {
		return
	}
	delete(a.memory, key)
}

func (a *AutoPicker) unpin() {
	a.pinned = ""
	a.lastGood = time.Time{}
	a.hits = make(map[string][]time.Time)
	a.demods = make(map[demodKey]lineDemod)
	a.g3ruh = modem.G3RUHDescrambler{}
	a.demodRate = 0
	a.sstvRx = nil
	a.cwDet = nil
	a.lastPicture = nil
	a.hasPicture = false
	a.pictures = nil
	a.audio = nil
	a.deframers = make(map[pipelineSig]deframer, len(a.candidates))
	for _, id := range a.candidates {
		if isAutoNonFrameProfile(id) {
			continue
		}
		sig := autoSpecFor(id).sig()
		if a.deframers[sig] == nil {
			if df := newProfileDeframer(id); df != nil {
				a.deframers[sig] = df
			}
		}
	}
}

func candidateHas(ids []string, id string) bool {
	return slices.Contains(ids, id)
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// autoConfigKey — устойчивый ключ TX/КА для памяти выбора.
// При известном NORAD живая частота с доплером не входит: иначе каждый hop
// того же TX считался бы новым. Без NORAD берётся номинал с шагом 5 кГц.
func autoConfigKey(norad int, freqHz uint64, bw uint32, hints CandidateHints) string {
	_ = bw
	key := "n" + strconv.Itoa(norad) +
		"c" + hints.CardProfile +
		"m" + hints.Mode +
		"b" + strconv.FormatFloat(hints.Baud, 'f', -1, 64)
	if norad <= 0 {
		key += "f" + strconv.FormatUint(freqHz/5_000*5_000, 10)
	}
	return key
}
