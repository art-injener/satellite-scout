// Package signalclass распознаёт модуляцию сигнала по структуре IQ участка (ADR-009 §6.1):
// дерево признаков без нейросети, поэтому каждое решение объясняется словами.
package signalclass

import (
	"fmt"
	"math"
	"strings"

	"gonum.org/v1/gonum/dsp/fourier"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/signals"
)

// Модуляции, которые различает дерево.
const (
	ModUndetermined = "undetermined" // мало SNR для разбора
	ModUnknown      = "unknown"      // ни одна ветка не подошла
	ModCarrier      = "carrier"      // немодулированная несущая
	ModCW           = "cw"           // телеграф
	ModOOK          = "ook"          // манипуляция «есть/нет»
	ModFM           = "fm"           // аналоговая ЧМ без речи
	ModFMVoice      = "fm_voice"     // ЧМ, речь
	ModWFM          = "wfm"          // широкая ЧМ
	ModAFSK         = "afsk"         // тоны поверх ЧМ
	Mod2FSK         = "2fsk"         // двухпозиционная ЧМ, в том числе GFSK и GMSK
	ModLFM          = "lfm"          // линейная частотная модуляция
	ModUSB          = "usb"          // верхняя боковая
	ModLSB          = "lsb"          // нижняя боковая
)

const (
	// MinSNRDB — ниже этого SNR в полосе сигнала разбор даёт уверенные ошибки.
	MinSNRDB = 10
	// constKappa — огибающая постоянна: E|s|⁴/(E|s|²)² за вычетом шума ниже этого.
	// Соответствует σ огибающей около 0,15 (ADR-009 §6.1.3).
	constKappa = 1.12
	// onKappa — огибающая во включённом состоянии манипуляции почти постоянна.
	onKappa = 1.3
	// minOffShare — доля времени у нуля, с которой огибающая считается «есть/нет».
	minOffShare = 0.2
	// minTransitions — переходов «есть/нет» за разбор: края вспышки — это только два.
	minTransitions = 6
	// maxTransitionsPerSymbol — переходов «есть/нет» на символ у OOK, с запасом на шум.
	maxTransitionsPerSymbol = 1.2
	// cwMaxHz — манипуляция уже этого — телеграф.
	cwMaxHz = 500
	// carrierLine — доля мощности в одной линии, с которой сигнал — несущая.
	carrierLine = 0.7
	// wfmMinHz — ЧМ шире этого — широкая (вещательная).
	wfmMinHz = 100_000
	// wfmHintOccMinHz — при подсказке ширины от обнаружителя занятая полоса не уже этого.
	wfmHintOccMinHz = 40_000
	// pilotHz — пилот стерео вещательной ЧМ.
	pilotHz = 19_000
	// pilotTolHz — допуск поиска пилота.
	pilotTolHz = 500
	// pilotMinRatio — линия пилота выше медианы спектра вокруг неё (7 дБ).
	pilotMinRatio = 5
	// broadcastLoHz, broadcastHiHz — план вещания ЧМ (CCIR).
	broadcastLoHz = 87.5e6
	broadcastHiHz = 108e6
	// ServiceBroadcast — служба эфирного радио (пилот 19 кГц или план частот).
	ServiceBroadcast = "broadcast"
	// LabelBroadcast — подпись эфирного радио. Имя станции дописывается через двоеточие.
	LabelBroadcast = "Эфирное радио"
	// LabelTransmitter — любительская, портативная или спутниковая станция.
	// Где она: космос или земля, это положение, не часть подписи.
	LabelTransmitter = "Радиопередатчик"
	// lfmMinSlope — медианный наклон мгновенной частоты, с которого сигнал считается ЛЧМ, Гц/с.
	lfmMinSlope = 1e6
	// lfmMinShare — доля отсчётов, чей наклон близок к медиане.
	lfmMinShare = 0.6
	// lfmMinOccHz — ЛЧМ уже этого остаётся другим видом модуляции.
	lfmMinOccHz = 20_000
	// wideDigitalMinBaud — шире вещательной полосы и с тактами не реже этого: цифра, не ЧМ.
	wideDigitalMinBaud = 100_000
	// clockMinRatio — линия тактов выше медианы спектра вокруг неё во столько раз (10 дБ).
	clockMinRatio = 10
	// afskMaxJitter, fskMaxJitter — ошибка фазы тактов демодулятора у настоящего потока,
	// доли символа; на шуме около 0,25.
	afskMaxJitter = 0.1
	fskMaxJitter  = 0.12
	// minFlips, maxFlips — доля смен бита у данных; у одного тона биты чередуются.
	minFlips = 0.15
	maxFlips = 0.85
	// flipWarmBits — бит на установление порога демодулятора.
	flipWarmBits = 32
	// voiceCV — разброс громкости по кадрам у речи: слоги и паузы.
	voiceCV = 0.5
	// voiceFrameS — кадр громкости речи, с.
	voiceFrameS = 0.02
	// audioHz — верх звуковой полосы для громкости ЧМ.
	audioHz = 3400
	// ssbMinHz, ssbMaxHz — ширина однополосной речи.
	ssbMinHz = 1000
	ssbMaxHz = 4500
	// ssbSide — медиана мощности в нижних 40 % полосы — USB, в верхних — LSB: речь
	// сосредоточена у подавленной несущей.
	ssbSide = 0.4
	// filterMarginBins — запас фильтра полосы сигнала по краям, бинов спектра.
	filterMarginBins = 3
	maxFilterTaps    = 1025
	// trimQuantile — уровень вспышки для обрезки тишины по краям: пакет может занимать
	// несколько процентов отрезка, и 90-й процентиль мощности тогда попадает в шум.
	trimQuantile = 0.99
	// regionPadS — запас по краям вспышки при повторной разметке полосы, с.
	regionPadS = 0.005
	// pauseLevel — доля уровня сигнала над шумом, ниже которой отсчёт — пауза между пакетами.
	pauseLevel = 0.2
	// minPauseS — паузы короче не вырезаются: провалы шумоподобной огибающей короче.
	minPauseS = 0.001
	// minFSKSNRDB — ниже этого SNR в полосе не разбирается и 2FSK: ветка FSK не держит поток.
	minFSKSNRDB = 4
	// minEyeQ — раскрытие глаза 2FSK ниже порога SNR: около 0,1 % ошибочных бит. У шума
	// около 1,7, у AFSK и SSB около 2.
	minEyeQ = 3
	// eyeBlockSymbols — символов в блоке со своей фазой тактов: пакеты склеены после
	// вырезания пауз, и фаза у каждого своя.
	eyeBlockSymbols = 128
	// eyePhases — сколько фаз тактов перебирается на символ.
	eyePhases = 16
	// sstvToneLo/Hi — полоса яркости SSTV в звуке после ЧМ-детектора (синхро 1200…белый 2300).
	sstvToneLo = 1200
	sstvToneHi = 2300
	// sstvToneMinShare — доля энергии мгновенной частоты в этой полосе: ложные такты FSK
	// на строках SSTV дают unknown, а тоны яркости держат долю выше порога.
	// 0,35: запись PD120 в эфире на вырезке ~25 кС/с даёт ~0,35–0,45 (на 48 кС/с выше).
	sstvToneMinShare = 0.35
)

// standardBauds — ряд скоростей, к которому приводится измеренная.
var standardBauds = []float64{
	50, 75, 100, 110, 150, 200, 250, 300, 500, 512, 600, 1000, 1200, 2000, 2400, 3600, 4800, 9600, 19200, 38400,
}

// Params — измеренные параметры сигнала; нулевые не измерялись.
type Params struct {
	OccupiedHz  float64 `json:"occupied_hz"`
	Baud        float64 `json:"baud,omitempty"`
	DeviationHz float64 `json:"deviation_hz,omitempty"`
	WPM         float64 `json:"wpm,omitempty"`
	ChirpHzS    float64 `json:"chirp_hz_s,omitempty"`
}

// Result — что за сигнал: модуляция, уверенность 0…1, параметры и почему так решено.
// Service — служба по сигнатуре (SSTV по VIS); пусто, если не совпала.
// Station — имя из эфира (RDS PS у вещания).
// Demod — какой профиль приёмника взять для этого сигнала (id как в UI).
// RDSParts/RDSHave — сырые сегменты PS между разборами (в JSON не уходят).
type Result struct {
	Modulation     string     `json:"modulation"`
	Confidence     float64    `json:"confidence"`
	Params         Params     `json:"params"`
	Why            string     `json:"why"`
	Service        string     `json:"service,omitempty"`
	ServiceLabel   string     `json:"service_label,omitempty"`
	ServiceBasis   string     `json:"service_basis,omitempty"`
	ConfidenceKind string     `json:"confidence_kind,omitempty"`
	Station        string     `json:"station,omitempty"`
	Demod          string     `json:"demod,omitempty"`
	RDSParts       [4][2]byte `json:"-"`
	RDSHave        [4]bool    `json:"-"`
}

// analysis — участок в полосе сигнала и общие для веток измерения.
type analysis struct {
	rate      float64
	b         band
	occ       float64
	widthHint float64     // ширина участка от обнаружителя; 0 — нет подсказки
	z         []complex64 // участок в полосе сигнала без тишины по краям
	on        []complex64 // z без пауз между пакетами: по нему постоянство огибающей и частота
	p         []float64   // сглаженная мощность z
	nz        float64     // мощность шума в z
	snrDB     float64
}

// Classify распознаёт сигнал в середине участка iq с частотой отсчётов rate (после Extract).
func Classify(iq []complex64, rate float64) Result {
	return ClassifyHint(iq, rate, 0)
}

// ClassifyHint — то же, что Classify, плюс ширина участка от обнаружителя: при многолучёвости
// внутренний замер полосы уже, а горб на спектре уже широкий.
func ClassifyHint(iq []complex64, rate, widthHintHz float64) Result {
	b, ok := findBand(welch(iq, rate))
	if !ok {
		return Result{Modulation: ModUndetermined, Why: "над шумом ничего нет"}
	}
	a := newAnalysis(iq, rate, b)
	if a != nil {
		a.widthHint = widthHintHz
	}
	if a != nil && a.snrDB >= MinSNRDB {
		return withDemod(withSSTV(a, a.tree()))
	}
	if a != nil && a.snrDB >= minFSKSNRDB {
		if r, weak := a.weakFSK(); weak {
			return withDemod(withSSTV(a, r))
		}
	}
	snr := math.Inf(-1)
	if a != nil {
		snr = a.snrDB
	}
	return Result{
		Modulation: ModUndetermined,
		Params:     Params{OccupiedHz: round10(b.occupied())},
		Why:        fmt.Sprintf("SNR %s дБ — мало для разбора", num(snr, 0)),
	}
}

// TagBroadcast ставит службу эфирного радио у широкой ЧМ в плане 87,5–108 МГц, если пилот
// ещё не распознан. Вне плана не трогает. Тот же каталог, что и у остальных видов.
func TagBroadcast(r Result, centerHz float64) Result {
	return Resolve(r, signals.Input{CenterHz: centerHz}, nil, 0)
}

// tree — дерево признаков: ЛЧМ, широкая ЧМ по ширине, затем «есть/нет», постоянная или меняющаяся.
func (a *analysis) tree() Result {
	if r, ok := a.lfm(); ok {
		return r
	}
	if a.isWideFM() {
		if r, ok := a.wideDigital(); ok {
			return r
		}
		return a.wideFM()
	}
	if r, keyed := a.keyed(); keyed {
		return r
	}
	if k := kappa(a.on, a.nz); k < constKappa {
		return a.constant(k)
	}
	return a.varying(kappa(a.z, a.nz))
}

// isWideFM — занятая полоса или горб обнаружителя в диапазоне вещательной ЧМ, не одна линия.
func (a *analysis) isWideFM() bool {
	if a.b.lineFrac >= carrierLine {
		return false
	}
	if a.occ >= wfmMinHz {
		return true
	}
	return a.widthHint >= wfmMinHz && a.occ >= wfmHintOccMinHz
}

// wideFM — широкая ЧМ; пилот 19 кГц и RDS PS дают службу эфирного радио.
func (a *analysis) wideFM() Result {
	occ := a.occ
	if a.widthHint > occ {
		occ = a.widthHint
	}
	p := Params{OccupiedHz: round10(occ)}
	why := "ширина " + hz(a.occ)
	if a.widthHint >= wfmMinHz && a.occ < wfmMinHz {
		why = "горб " + hz(a.widthHint) + ", занято " + hz(a.occ)
	}
	r := a.result(ModWFM, 0.7, p, why)
	if a.hasPilot19() {
		r.Service = ServiceBroadcast
		r.ServiceLabel = LabelBroadcast
		r.Why += ", пилот 19 кГц"
		applyRDS(&r, a.on, a.rate)
	} else if a.rate >= 2*(rdsCarrierHz+3_000) {
		// Без явного пилота всё же пробуем RDS: часть станций моно.
		if applyRDS(&r, a.on, a.rate) && r.Station != "" {
			r.Service = ServiceBroadcast
			r.ServiceLabel = LabelBroadcast + ": " + r.Station
		}
	}
	return r
}

// applyRDS пишет Station и сырые сегменты PS в r; true, если хоть что-то из RDS видно.
func applyRDS(r *Result, iq []complex64, rate float64) bool {
	ps, parts, have, nBits, nGroups, nSeg := rdsTry(iq, rate)
	r.RDSParts, r.RDSHave = parts, have
	if ps != "" {
		r.Station = ps
		r.ServiceLabel = LabelBroadcast + ": " + ps
		r.Why += ", RDS «" + ps + "»"
		return true
	}
	if nBits > 0 || nGroups > 0 || nSeg > 0 {
		r.Why += fmt.Sprintf(", RDS %d бит/%d групп/PS %d/4", nBits, nGroups, nSeg)
	}
	return nSeg > 0 || nGroups > 0
}

// withDemod ставит рекомендуемый профиль приёмника по модуляции и службе.
func withDemod(r Result) Result {
	if r.ConfidenceKind == "" && r.Modulation != "" && r.Modulation != ModUndetermined {
		r.ConfidenceKind = KindEstimate
	}
	if r.Demod != "" {
		return r
	}
	r.Demod = suggestedDemod(r)
	return r
}

// suggestedDemod — id профиля UI для этого сигнала; пусто, если однозначно не сказать.
func suggestedDemod(r Result) string {
	switch {
	case r.Service == sstvService || r.Service == sstvProbable:
		return "sstv"
	case r.Modulation == ModWFM:
		return "wfm"
	case r.Modulation == ModFMVoice, r.Modulation == ModFM:
		return "fm"
	case r.Modulation == ModAFSK:
		return "afsk1200"
	case r.Modulation == ModUSB:
		return "usb"
	case r.Modulation == ModLSB:
		return "lsb"
	case r.Modulation == ModCW:
		return "cw"
	case r.Modulation == Mod2FSK && r.Params.Baud == 9600:
		return "fsk9600_g3ruh"
	default:
		return ""
	}
}

// hasPilot19 — в выходе ЧМ-детектора есть линия около 19 кГц (стереопилот вещания).
func (a *analysis) hasPilot19() bool {
	if a.rate < 2*(pilotHz+pilotTolHz) || len(a.on) < 64 {
		return false
	}
	f, ratio := clockLine(instFreq(a.on, a.rate), a.rate, pilotHz-pilotTolHz, pilotHz+pilotTolHz)
	return ratio >= pilotMinRatio && math.Abs(f-pilotHz) <= pilotTolHz
}

// weakFSK — 2FSK ниже порога SNR. SNR в полосе не учитывает энергию на символ: у медленного
// потока в широкой полосе биты читаются и при 6–8 дБ. Поэтому ответ ветки FSK принимается,
// если глаз раскрыт; ветки других модуляций при таком SNR уверенно ошибаются.
func (a *analysis) weakFSK() (Result, bool) {
	r := a.tree()
	if r.Modulation != Mod2FSK {
		return Result{}, false
	}
	q := eyeQ(a.on, a.rate, r.Params.Baud)
	if q < minEyeQ {
		return Result{}, false
	}
	r.Why += ", биты читаются: Q " + num(q, 1)
	return r, true
}

// newAnalysis оставляет полосу сигнала узким фильтром и отрезает тишину по краям вспышки.
// Спектр всего отрезка разбавлен тишиной вокруг короткой вспышки: скаты сигнала уходят
// под порог разметки, полоса выходит уже сигнала, и фильтр их срезает. Поэтому полоса
// размечается заново по самой вспышке.
func newAnalysis(iq []complex64, rate float64, b band) *analysis {
	a, from, to := cutBurst(iq, rate, b)
	if a == nil || to-from >= len(iq)*9/10 {
		return a
	}
	region := iq[from:to]
	if b2, ok := findBand(welch(region, rate)); ok {
		if a2, _, _ := cutBurst(region, rate, b2); a2 != nil {
			return a2
		}
	}
	return a
}

// cutBurst — участок в полосе b без тишины по краям и без пауз между пакетами; вторым и
// третьим значением — границы вспышки в iq с запасом regionPadS.
func cutBurst(iq []complex64, rate float64, b band) (*analysis, int, int) {
	s := welch(iq, rate)
	bw := b.hiHz - b.loHz
	cutoff := bw/2 + filterMarginBins*s.binHz
	trans := max(0.2*cutoff, rate/200)
	taps := dsp.DesignLowpass(cutoff, rate, min(int(3.1*rate/trans), maxFilterTaps), dsp.WindowHann)
	z := dsp.NewXlatingFIR((b.loHz+b.hiHz)/2, rate, 1, taps).Process(iq)
	if len(z) <= 2*len(taps) {
		return nil, 0, 0
	}
	z = z[len(taps):]
	var h2 float64
	for _, h := range taps {
		h2 += h * h
	}
	// Шум белый: на бин спектра его σ²/N, через фильтр — σ²·Σh².
	nz := b.noise * float64(len(s.p)) * h2

	pw := make([]float64, len(z))
	for i, v := range z {
		pw[i] = sqAbs(v)
	}
	w := max(1, int(math.Round(rate/bw)))
	p := movingAvg(pw, w)
	thr := nz + 0.5*(quantile(p, trimQuantile)-nz)
	first, last := -1, -1
	for i, v := range p {
		if v > thr {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 || last-first < 64 {
		return nil, 0, 0
	}
	z, p = z[first:last+1], p[first:last+1]
	on := withoutPauses(z, p, nz, max(int(minPauseS*rate), 4*w), len(taps)/2+w)
	var m2 float64
	for _, v := range on {
		m2 += sqAbs(v)
	}
	m2 /= float64(len(on))
	// Шум в полосе сигнала: плотность nz/(rate·Σh²) на ширину bw.
	snr := (m2 - nz) / (nz * bw / (rate * h2))
	// Отсчёт z[i] — середина фильтра на iq[i+len(taps)/2].
	pad := int(regionPadS * rate)
	from := max(0, first+len(taps)/2-pad)
	to := min(len(iq), last+len(taps)/2+1+pad)
	return &analysis{
		rate: rate, b: b, occ: b.occupied(),
		z: z, on: on, p: p, nz: nz,
		snrDB: 10 * math.Log10(max(snr, 1e-12)),
	}, from, to
}

// withoutPauses — z без пауз между пакетами: участков, где сглаженная мощность p не короче
// minRun отсчётов ниже pauseLevel уровня сигнала, с запасом guard на длину фильтра.
// Если пауз почти всё, возвращает z.
func withoutPauses(z []complex64, p []float64, nz float64, minRun, guard int) []complex64 {
	low := nz + pauseLevel*(quantile(p, 0.9)-nz)
	drop := make([]bool, len(z))
	for i := 0; i < len(p); {
		if p[i] >= low {
			i++
			continue
		}
		j := i
		for j < len(p) && p[j] < low {
			j++
		}
		if j-i >= minRun {
			for k := max(0, i-guard); k < min(len(p), j+guard); k++ {
				drop[k] = true
			}
		}
		i = j
	}
	on := make([]complex64, 0, len(z))
	for i, v := range z {
		if !drop[i] {
			on = append(on, v)
		}
	}
	if len(on) < 64 {
		return z
	}
	return on
}

// kappa — E|s|⁴/(E|s|²)² сигнала без шума: 1 у постоянной огибающей, 1/d у манипуляции
// с долей d, около 2 у речи и шумоподобных. Шум мощности nz вычитается из моментов.
func kappa(z []complex64, nz float64) float64 {
	var m2, m4 float64
	for _, v := range z {
		q := sqAbs(v)
		m2 += q
		m4 += q * q
	}
	m2 /= float64(len(z))
	m4 /= float64(len(z))
	s := m2 - nz
	if s <= 0 {
		return math.Inf(1)
	}
	return (m4 - 4*s*nz - 2*nz*nz) / (s * s)
}

// keyed — огибающая с двумя состояниями: телеграф или OOK.
func (a *analysis) keyed() (Result, bool) {
	high := quantile(a.p, 0.9)
	lowT, highT := a.nz+0.2*(high-a.nz), a.nz+0.6*(high-a.nz)
	off, transitions, start := 0, 0, 0
	on := true
	var runs []float64
	var onZ []complex64
	for i, v := range a.p {
		if v < lowT {
			off++
		}
		if v > highT {
			onZ = append(onZ, a.z[i])
		}
		switch {
		case !on && v > highT:
			on, start = true, i
			transitions++
		case on && v < lowT:
			on = false
			runs = append(runs, float64(i-start))
			transitions++
		}
	}
	offShare := float64(off) / float64(len(a.p))
	if offShare < minOffShare || transitions < minTransitions || len(onZ) < 64 {
		return Result{}, false
	}
	kOn := kappa(onZ, a.nz)
	if kOn >= onKappa {
		return Result{}, false
	}
	q := min((offShare-minOffShare)/minOffShare, (onKappa-kOn)/(onKappa-1))
	if a.occ < cwMaxHz {
		p := Params{OccupiedHz: round10(a.occ)}
		why := "огибающая есть/нет, узко " + hz(a.occ)
		if len(runs) >= 3 {
			dot := quantile(runs, 0.2) / a.rate
			p.WPM = math.Round(1.2/dot*10) / 10
			why += fmt.Sprintf(", точка %s мс", num(dot*1000, 0))
		}
		return a.result(ModCW, q, p, why), true
	}
	// Манипуляция переключается не чаще раза на символ; шумоподобная огибающая
	// проваливается к нулю много чаще своих случайных «тактов».
	baud, ok := a.clock(a.p, min(a.occ, 0.45*a.rate))
	if !ok || float64(transitions)*a.rate/float64(len(a.p)) > maxTransitionsPerSymbol*baud {
		return Result{}, false
	}
	p := Params{OccupiedHz: round10(a.occ), Baud: baud}
	return a.result(ModOOK, q, p, "огибающая есть/нет, такты "+num(baud, 0)), true
}

// constant — огибающая постоянна: несущая, широкая ЧМ, AFSK, 2FSK или аналоговая ЧМ.
func (a *analysis) constant(k float64) Result {
	p := Params{OccupiedHz: round10(a.occ)}
	if a.occ >= wfmMinHz {
		return a.result(ModWFM, (constKappa-k)/(constKappa-1), p,
			"огибающая постоянна, ширина "+hz(a.occ))
	}
	d := instFreq(a.on, a.rate)
	// Одна линия в спектре — обычно несущая. У медленного SSTV (PD120) срез 1 с
	// тоже выглядит линией: тоны 1200…2300 Гц после ЧМ-детектора отличают его.
	if a.b.lineFrac > carrierLine && sstvToneShare(d, a.rate) < sstvToneMinShare {
		return a.result(ModCarrier, (a.b.lineFrac-carrierLine)/(1-carrierLine), p,
			"огибающая постоянна, одна линия в спектре")
	}
	if r, ok := a.afsk(p); ok {
		return r
	}
	r, clocked := a.fsk(d, p)
	if r.Modulation != "" {
		return r
	}
	if cv := loudnessSpread(d, a.rate); cv > voiceCV {
		return a.result(ModFMVoice, (cv-voiceCV)/voiceCV, p,
			"огибающая постоянна, частота меняется плавно, громкость слогами")
	}
	if clocked > 0 {
		share := sstvToneShare(d, a.rate)
		if share >= sstvToneMinShare {
			return a.result(ModFM, share, p,
				"огибающая постоянна, тоны в полосе SSTV, такты "+num(clocked, 0)+" не держат поток")
		}
		// Ложный такт в полосе SSTV: ведущий 1900, VIS ~1100, синхро 1200 и его
		// гармоника/половина (~600) на медленной развёртке PD — поток не цифровой.
		if clocked >= 500 && clocked <= sstvToneHi {
			r := a.result(ModFM, 0.5, p, "огибающая постоянна, один тон "+hz(clocked))
			r.Service = sstvProbable
			r.ServiceLabel = "SSTV?"
			return r
		}
		// Такты есть, а поток демодулятор не держит: цифра на пороге, аналогом её не назвать.
		return Result{
			Modulation: ModUnknown, Confidence: 0.3, Params: p,
			Why: "огибающая постоянна, такты " + num(clocked, 0) + ", но поток не держится",
		}
	}
	return a.result(ModFM, 0.5, p, "огибающая постоянна, частота меняется плавно")
}

// sstvToneShare — доля энергии спектра мгновенной частоты в 1200…2300 Гц (яркость SSTV).
func sstvToneShare(d []float64, rate float64) float64 {
	if len(d) < 64 || rate <= 0 {
		return 0
	}
	n := 1
	for n < len(d) {
		n <<= 1
	}
	var mean float64
	for _, v := range d {
		mean += v
	}
	mean /= float64(len(d))
	buf := make([]float64, n)
	for i, v := range d {
		buf[i] = v - mean
	}
	c := fourier.NewFFT(n).Coefficients(nil, buf)
	lo := max(1, int(math.Ceil(sstvToneLo*float64(n)/rate)))
	hi := min(len(c)-1, int(sstvToneHi*float64(n)/rate))
	nyq := min(len(c)-1, int(0.45*float64(n)))
	if hi <= lo || nyq <= 1 {
		return 0
	}
	var band, total float64
	for i := 1; i <= nyq; i++ {
		p := real(c[i])*real(c[i]) + imag(c[i])*imag(c[i])
		total += p
		if i >= lo && i <= hi {
			band += p
		}
	}
	if total <= 0 {
		return 0
	}
	return band / total
}

func (a *analysis) afsk(p Params) (Result, bool) {
	cfg := modem.AFSK1200(a.rate)
	dem := modem.NewAFSKDemod(cfg)
	dem.Demodulate(nil, a.on)
	j := dem.ClockJitter()
	if j > afskMaxJitter {
		return Result{}, false
	}
	p.Baud = cfg.Baud
	return a.result(ModAFSK, (afskMaxJitter-j)/afskMaxJitter, p,
		fmt.Sprintf("огибающая постоянна, тоны %s/%s Гц, такты 1200", num(cfg.MarkHz, 0), num(cfg.SpaceHz, 0))), true
}

// fsk — такты по переходам мгновенной частоты, затем демодулятор на этой скорости:
// у настоящего потока такты ровные, а биты меняются через раз в среднем. Пустой ответ —
// не FSK; вторым значением — скорость, если линия тактов нашлась.
func (a *analysis) fsk(d []float64, p Params) (Result, float64) {
	return a.fskLimited(d, p, min(1.2*a.occ, 0.45*a.rate))
}

// lfm — мгновенная частота идёт с почти постоянным наклоном: свип или пачка свипов.
// Наклон считается по коротким кускам: разность соседних отсчётов в шуме больше самого свипа.
func (a *analysis) lfm() (Result, bool) {
	if a.occ < lfmMinOccHz || len(a.on) < 256 {
		return Result{}, false
	}
	d := instFreq(a.on, a.rate)
	const block = 64
	if len(d) < block*2 {
		return Result{}, false
	}
	slopes := make([]float64, 0, len(d)/block)
	for start := 0; start+block <= len(d); start += block / 2 {
		var sx, sy, sxx, sxy float64
		for i := 0; i < block; i++ {
			x := float64(i)
			y := d[start+i]
			sx += x
			sy += y
			sxx += x * x
			sxy += x * y
		}
		den := float64(block)*sxx - sx*sx
		if den == 0 {
			continue
		}
		slopes = append(slopes, (float64(block)*sxy-sx*sy)/den*a.rate)
	}
	if len(slopes) < 8 {
		return Result{}, false
	}
	m := quantile(slopes, 0.5)
	if math.Abs(m) < lfmMinSlope {
		return Result{}, false
	}
	tol := math.Abs(m) * 0.35
	close := 0
	for _, s := range slopes {
		if math.Abs(s-m) <= tol {
			close++
		}
	}
	share := float64(close) / float64(len(slopes))
	if share < lfmMinShare {
		return Result{}, false
	}
	p := Params{OccupiedHz: round10(a.occ), ChirpHzS: math.Round(m)}
	return a.result(ModLFM, share, p, "частота идёт линейно, наклон "+num(m, 0)+" Гц/с, полоса "+hz(a.occ)), true
}

// wideDigital — занятая полоса как у вещания, но такты цифры: GMSK около 200 кГц
// не называется широкой ЧМ. Такты GSM чаще самой полосы, поэтому предел поиска выше.
func (a *analysis) wideDigital() (Result, bool) {
	if kappa(a.on, a.nz) >= constKappa {
		return Result{}, false
	}
	p := Params{OccupiedHz: round10(a.occ)}
	d := instFreq(a.on, a.rate)
	r, baud := a.fskLimited(d, p, min(0.45*a.rate, 400_000))
	if baud < wideDigitalMinBaud {
		return Result{}, false
	}
	if r.Modulation == Mod2FSK {
		return r, true
	}
	// Демодулятор на такой скорости может не захватить глаз, но линия тактов уже
	// отличает цифру от вещательной ЧМ.
	p.Baud = baud
	return a.result(Mod2FSK, 0.55, p, "огибающая постоянна, такты "+num(baud, 0)+", полоса "+hz(a.occ)), true
}

func (a *analysis) fskLimited(d []float64, p Params, fmax float64) (Result, float64) {
	smooth := 1
	if a.occ > 0 && a.occ < wideDigitalMinBaud {
		smooth = max(1, int(math.Round(a.rate/a.occ)))
	}
	ds := movingAvg(d, smooth)
	baud, ok := a.clock(ds, fmax)
	if !ok {
		return Result{}, 0
	}
	dem := modem.NewFSKDemod(modem.FSKConfig{SampleRate: a.rate, Baud: baud, DeviationHz: a.occ / 4})
	bits := dem.Demodulate(nil, a.on)
	j := dem.ClockJitter()
	if j > fskMaxJitter || len(bits) <= 2*flipWarmBits {
		return Result{}, baud
	}
	flips := 0
	for i := flipWarmBits + 1; i < len(bits); i++ {
		if bits[i] != bits[i-1] {
			flips++
		}
	}
	share := float64(flips) / float64(len(bits)-flipWarmBits-1)
	switch {
	case share > maxFlips:
		return a.result(ModFM, 0.5, p, "огибающая постоянна, один тон "+hz(baud/2)), baud
	case share < minFlips:
		return Result{}, baud
	}
	p.Baud = baud
	p.DeviationHz = round10(dem.DeviationHz())
	return a.result(Mod2FSK, (fskMaxJitter-j)/fskMaxJitter, p, fmt.Sprintf(
		"огибающая постоянна, 2 уровня частоты, такты %s, девиация ±%s", num(baud, 0), hz(p.DeviationHz))), baud
}

// clock — скорость по линии тактов в переходах ряда x: квадрат его разности.
func (a *analysis) clock(x []float64, fmax float64) (float64, bool) {
	e := make([]float64, len(x)-1)
	for i := range e {
		dx := x[i+1] - x[i]
		e[i] = dx * dx
	}
	f, ratio := clockLine(e, a.rate, 50, fmax)
	if ratio < clockMinRatio {
		return 0, false
	}
	return snapBaud(f), true
}

// eyeQ — раскрытие глаза 2FSK: выход ЧМ-детектора, усреднённый по символу, в серединах
// символов делится на два уровня; Q — расстояние между уровнями на сумму их разбросов.
func eyeQ(z []complex64, rate, baud float64) float64 {
	sps := rate / baud
	y := movingAvg(instFreq(z, rate), max(1, int(math.Round(sps))))
	blk := max(1, int(eyeBlockSymbols*sps))
	var s []float64
	for start := 0; start < len(y); start += blk {
		if b := y[start:min(len(y), start+blk)]; start == 0 || len(b) >= blk/4 {
			s = appendEyeSamples(s, b, sps)
		}
	}
	if len(s) < 32 {
		return 0
	}
	return twoLevelQ(s)
}

// appendEyeSamples дописывает выборки блока y через символ при фазе с наибольшим
// средним отклонением от медианы — там глаз открыт шире всего.
func appendEyeSamples(dst, y []float64, sps float64) []float64 {
	mid := quantile(y, 0.5)
	bestPh, bestDev := 0.0, -1.0
	for k := range eyePhases {
		ph := float64(k) * sps / eyePhases
		var dev float64
		n := 0
		for t := ph; int(t) < len(y); t += sps {
			dev += math.Abs(y[int(t)] - mid)
			n++
		}
		if n > 0 && dev/float64(n) > bestDev {
			bestPh, bestDev = ph, dev/float64(n)
		}
	}
	for t := bestPh; int(t) < len(y); t += sps {
		dst = append(dst, y[int(t)])
	}
	return dst
}

// twoLevelQ делит выборки на два уровня (k-средних с двумя центрами) и возвращает
// расстояние между центрами на сумму разбросов. У одного горба около 1,3.
func twoLevelQ(s []float64) float64 {
	thr := quantile(s, 0.5)
	var m0, m1 float64
	for range 10 {
		var sum0, sum1, n0, n1 float64
		for _, v := range s {
			if v < thr {
				sum0, n0 = sum0+v, n0+1
			} else {
				sum1, n1 = sum1+v, n1+1
			}
		}
		if n0 == 0 || n1 == 0 {
			return 0
		}
		m0, m1 = sum0/n0, sum1/n1
		thr = (m0 + m1) / 2
	}
	var v0, v1, n0, n1 float64
	for _, v := range s {
		if v < thr {
			v0, n0 = v0+(v-m0)*(v-m0), n0+1
		} else {
			v1, n1 = v1+(v-m1)*(v-m1), n1+1
		}
	}
	return (m1 - m0) / (math.Sqrt(v0/n0) + math.Sqrt(v1/n1))
}

// varying — огибающая меняется без двух состояний: пока различаются USB и LSB. Линии
// в спектре речи — гармоники голоса, поэтому по ним SSB не отсекается; у AM боковые
// симметричны, и медиана мощности остаётся посередине.
func (a *analysis) varying(k float64) Result {
	p := Params{OccupiedHz: round10(a.occ)}
	if a.occ >= ssbMinHz && a.occ <= ssbMaxHz && k > onKappa {
		pos := (a.b.medianHz - a.b.occLo) / a.occ
		switch {
		case pos < ssbSide:
			return a.result(ModUSB, (ssbSide-pos)/ssbSide, p,
				"огибающая меняется, мощность у нижнего края полосы")
		case pos > 1-ssbSide:
			return a.result(ModLSB, (pos-1+ssbSide)/ssbSide, p,
				"огибающая меняется, мощность у верхнего края полосы")
		}
	}
	return Result{
		Modulation: ModUnknown, Confidence: 0.3, Params: p,
		Why: fmt.Sprintf("огибающая меняется (κ %s), ширина %s", num(k, 2), hz(a.occ)),
	}
}

// result — уверенность растёт с запасом признака q и с SNR выше порога.
func (a *analysis) result(mod string, q float64, p Params, why string) Result {
	qSNR := (a.snrDB - MinSNRDB) / 10
	c := 0.5 + 0.45*clamp01(min(q, qSNR))
	return Result{Modulation: mod, Confidence: math.Round(c*100) / 100, Params: p, Why: why}
}

// instFreq — мгновенная частота, Гц.
func instFreq(z []complex64, rate float64) []float64 {
	d := make([]float64, len(z)-1)
	k := rate / (2 * math.Pi)
	for i := range d {
		d[i] = phaseStep(z[i+1], z[i]) * k
	}
	return d
}

// loudnessSpread — разброс громкости звука ЧМ по кадрам: σ/среднее дисперсии частоты
// в звуковой полосе. У речи слоги и паузы, у тона и шума громкость ровная.
func loudnessSpread(d []float64, rate float64) float64 {
	taps := dsp.DesignLowpass(audioHz, rate, 63, dsp.WindowHann)
	audio := firReal(d, taps)
	frame := int(voiceFrameS * rate)
	var vars []float64
	for start := 0; start+frame <= len(audio); start += frame {
		var m, m2 float64
		for _, v := range audio[start : start+frame] {
			m += v
			m2 += v * v
		}
		m /= float64(frame)
		vars = append(vars, m2/float64(frame)-m*m)
	}
	if len(vars) < 4 {
		return 0
	}
	var mean, sq float64
	for _, v := range vars {
		mean += v
	}
	mean /= float64(len(vars))
	for _, v := range vars {
		sq += (v - mean) * (v - mean)
	}
	if mean <= 0 {
		return 0
	}
	return math.Sqrt(sq/float64(len(vars))) / mean
}

func firReal(x, taps []float64) []float64 {
	if len(x) < len(taps) {
		return nil
	}
	out := make([]float64, len(x)-len(taps)+1)
	for i := range out {
		var acc float64
		for k, h := range taps {
			acc += h * x[i+k]
		}
		out[i] = acc
	}
	return out
}

// snapBaud приводит скорость к стандартной, если она ближе 2 %.
func snapBaud(f float64) float64 {
	for _, b := range standardBauds {
		if math.Abs(f-b) <= 0.02*b {
			return b
		}
	}
	return math.Round(f)
}

func sqAbs(v complex64) float64 {
	re, im := float64(real(v)), float64(imag(v))
	return re*re + im*im
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func round10(v float64) float64 { return math.Round(v/10) * 10 }

// num — число с запятой и prec знаками после неё.
func num(v float64, prec int) string {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return "—"
	}
	return strings.Replace(fmt.Sprintf("%.*f", prec, v), ".", ",", 1)
}

// hz — частота для пояснения: «850 Гц», «12,5 кГц».
func hz(v float64) string {
	if math.Abs(v) < 1000 {
		return num(v, 0) + " Гц"
	}
	s := num(v/1000, 1)
	return strings.TrimSuffix(s, ",0") + " кГц"
}
