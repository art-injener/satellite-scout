package bandscan

import (
	"math"
	"slices"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	// minFFTSize и maxFFTSize — своё БПФ обнаружителя, степень двойки.
	// От БПФ клиентов «Спектра» не зависит. Размер выбирается по частоте
	// дискретизации: на решение должно приходиться не меньше minFFTsPerDecision БПФ,
	// иначе шум одиночного БПФ выскакивает выше порога. На 2,56 МС/с остаётся 4096,
	// на 57,6 кС/с берётся 256.
	minFFTSize         = 256
	maxFFTSize         = 4096
	minFFTsPerDecision = 32
	// DecisionsPerSecond — сколько раз в секунду обнаружитель решает, где сигналы.
	DecisionsPerSecond = 6
	// ThresholdDB — превышение над шумом, с которого бин считается занятым.
	ThresholdDB = 6

	// maxGroupFFTs — сколько БПФ усредняется в группу перед максимумом за интервал.
	// Максимум одиночных БПФ у чистого шума выше среднего на 7 дБ при сотне БПФ за интервал,
	// порог тогда срабатывает на всю полосу; среднее группы сужает разброс шума.
	maxGroupFFTs = 8
	// noiseBlockBins — бинов в блоке, по которому берётся первая медиана шума.
	noiseBlockBins = 64
	// noiseWindowFraction — доля годной полосы, по которой берётся медиана медиан блоков:
	// широкая ЧМ (200 кГц) должна занимать меньше половины окна, иначе станет шумом.
	noiseWindowFraction = 0.25
	// noiseAlpha — сглаживание шума во времени, доля нового решения.
	noiseAlpha = 0.2
	// mergeGapBins — разрыв в бинах, через который соседние занятые участки сливаются.
	mergeGapBins = 2
	// nearbyMergeGapHz — после разметки: слабый край широкой ЧМ за провалом до этой
	// ширины сливается с сильным горбом (станция в эфире часто даёт такой край).
	nearbyMergeGapHz = 40_000
	// nearbyMergeMaxWidthHz — слитый участок не шире вещательной ЧМ с запасом.
	nearbyMergeMaxWidthHz = 250_000
	// nearbyMergeMinSNRDB — сливать только если есть горб не слабее порога станции.
	nearbyMergeMinSNRDB = 10
	// nearbyMergeMinBodyHz — сливать только к уже широкому телу (вещательная ЧМ),
	// а не два узких носителя через несколько килогерц.
	nearbyMergeMinBodyHz = 80_000
	// smoothBins — по скольким соседним бинам берётся медиана превышения. У сигнала
	// на полосу (FSK с линиями тонов, широкая ЧМ) отдельные бины проседают под порог,
	// и участок рвётся на куски, а медиана держится. Одиночная сильная несущая медиану
	// соседей почти не поднимает, среднее бы её размазало.
	smoothBins = 5
	// smoothThresholdDB — порог медианы: у чистого шума её максимум за решение 2,7–2,8 дБ.
	smoothThresholdDB = 3.5
)

// Peak — занятый участок одного решения, абсолютные частоты краёв.
type Peak struct {
	LoHz  float64
	HiHz  float64
	SNRDB float32 // наибольшее превышение над шумом внутри участка
	// Power — сумма превышений бинов в разах, вес пика; 0 — не измерялся (пик задан вручную).
	Power float64
	// CentroidHz — центр тяжести мощности: устойчивее середины, у слабого сигнала края
	// на пороге то отрезаются, то нет.
	CentroidHz float64
}

// CenterHz — центр тяжести, если измерен, иначе середина участка.
func (p Peak) CenterHz() float64 {
	if p.Power > 0 {
		return p.CentroidHz
	}
	return (p.LoHz + p.HiHz) / 2
}

// weight — вес пика при сложении кусков одного сигнала.
func (p Peak) weight() float64 {
	if p.Power > 0 {
		return p.Power
	}
	return 1
}

// Decision — итог одного интервала обнаружителя.
type Decision struct {
	TimeMs     uint64 // время потока в конце интервала
	DurationMs float64
	CenterHz   uint64
	BinHz      float64
	// NoiseDBFS — шум по среднему за интервал, медиана годной полосы.
	NoiseDBFS float32
	// Excess — превышение максимума над шумом по каждому бину, дБ; вне годной полосы
	// минус бесконечность.
	Excess []float32
	Peaks  []Peak
	loBin  int // годные бины [loBin, hiBin)
	hiBin  int
}

// binFreq — абсолютная частота середины бина i. Середина Excess — центр настройки.
func (d Decision) binFreq(i int) float64 {
	return float64(d.CenterHz) + float64(i-len(d.Excess)/2)*d.BinHz
}

// UsableHz — нижний и верхний края годной полосы решения.
func (d Decision) UsableHz() (float64, float64) {
	return d.binFreq(d.loBin) - d.BinHz/2, d.binFreq(d.hiBin-1) + d.BinHz/2
}

// ExcessIn — наибольшее превышение над шумом в полосе [loHz, hiHz]; минус бесконечность,
// если полоса вне годной части.
func (d Decision) ExcessIn(loHz, hiHz float64) float32 {
	best := float32(math.Inf(-1))
	if d.BinHz <= 0 || len(d.Excess) == 0 {
		return best
	}
	mid := len(d.Excess) / 2
	from := max(d.loBin, int(math.Floor((loHz-float64(d.CenterHz))/d.BinHz+0.5))+mid)
	to := min(d.hiBin-1, int(math.Floor((hiHz-float64(d.CenterHz))/d.BinHz+0.5))+mid)
	for i := from; i <= to; i++ {
		best = max(best, d.Excess[i])
	}
	return best
}

// Detector копит БПФ блоков интервала и раз в интервал решает, какие бины заняты.
// Шум — медиана по частоте, а не во времени: у постоянной несущей медиана во времени
// равна самому сигналу. Вызывать из одной горутины.
type Detector struct {
	fftSize int
	spec    *dsp.Spectrum
	window  []float64
	chunk   []complex64
	fill    int // отсчётов в chunk

	centerHz uint64
	rate     uint32
	groupLen int // БПФ в группе при текущей частоте дискретизации

	groupSum  []float64
	groupN    int
	meanSum   []float64
	maxGroup  []float64
	groups    int
	samples   int
	lastMs    uint64
	noiseMax  []float64 // сглаженный шум статистики максимума по блокам, дБ
	noiseMean []float64 // то же для среднего
}

// NewDetector создаёт обнаружитель. Размер БПФ выбирается по первому блоку потока.
func NewDetector() *Detector {
	return &Detector{}
}

// fftSizeForRate — крупнейшая степень двойки в [minFFTSize, maxFFTSize],
// при которой на решение приходится не меньше minFFTsPerDecision БПФ.
func fftSizeForRate(rate uint32) int {
	limit := int(rate) / DecisionsPerSecond / minFFTsPerDecision
	n := maxFFTSize
	for n > minFFTSize && n > limit {
		n >>= 1
	}
	return n
}

func (d *Detector) alloc(size int) {
	d.fftSize = size
	d.spec = dsp.NewSpectrum(size)
	d.window = dsp.MakeWindowCoeffs(size, dsp.WindowBlackmanHarris)
	d.chunk = make([]complex64, size)
	d.groupSum = make([]float64, size)
	d.meanSum = make([]float64, size)
	d.maxGroup = make([]float64, size)
}

// Push принимает блок потока; usableHz — ширина годной середины окна. Возвращает решение,
// когда интервал набран. Перестройка или смена частоты дискретизации начинает интервал и шум заново.
func (d *Detector) Push(f sdr.IQFrame, usableHz uint32) (Decision, bool) {
	if f.SampleRate == 0 {
		return Decision{}, false
	}
	if f.CenterHz != d.centerHz || f.SampleRate != d.rate {
		d.retune(f.CenterHz, f.SampleRate)
	}
	d.lastMs = f.TimestampMs
	d.samples += len(f.Samples)
	for _, s := range f.Samples {
		d.chunk[d.fill] = s
		d.fill++
		if d.fill == d.fftSize {
			d.addFFT()
			d.fill = 0
		}
	}
	if d.samples < int(d.rate)/DecisionsPerSecond || d.groups == 0 {
		return Decision{}, false
	}
	return d.decide(usableHz), true
}

func (d *Detector) retune(centerHz uint64, rate uint32) {
	d.centerHz, d.rate = centerHz, rate
	if size := fftSizeForRate(rate); size != d.fftSize {
		d.alloc(size)
	}
	ffts := int(rate) / DecisionsPerSecond / d.fftSize
	d.groupLen = min(maxGroupFFTs, max(1, ffts/4))
	d.fill = 0
	d.noiseMax, d.noiseMean = nil, nil
	d.resetInterval()
}

func (d *Detector) resetInterval() {
	clear(d.groupSum)
	clear(d.meanSum)
	clear(d.maxGroup)
	d.groupN, d.groups, d.samples = 0, 0, 0
}

func (d *Detector) addFFT() {
	dsp.ApplyWindow(d.chunk, d.window)
	p := d.spec.PowerSpectrum(d.chunk)
	for i, v := range p {
		d.groupSum[i] += v
	}
	d.groupN++
	if d.groupN < d.groupLen {
		return
	}
	inv := 1 / float64(d.groupN)
	for i, v := range d.groupSum {
		m := v * inv
		d.meanSum[i] += m
		d.maxGroup[i] = max(d.maxGroup[i], m)
	}
	clear(d.groupSum)
	d.groupN = 0
	d.groups++
}

func (d *Detector) decide(usableHz uint32) Decision {
	n := d.fftSize
	binHz := float64(d.rate) / float64(n)
	half := int(math.Min(float64(usableHz)/binHz, float64(n)) / 2)
	dec := Decision{
		TimeMs:     d.lastMs,
		DurationMs: float64(d.samples) * 1000 / float64(d.rate),
		CenterHz:   d.centerHz,
		BinHz:      binHz,
		Excess:     make([]float32, n),
		loBin:      n/2 - half,
		hiBin:      n/2 + half,
	}
	maxDB := make([]float64, n)
	meanDB := make([]float64, n)
	inv := 1 / float64(d.groups)
	for i := dec.loBin; i < dec.hiBin; i++ {
		maxDB[i] = toDB(d.maxGroup[i])
		meanDB[i] = toDB(d.meanSum[i] * inv)
	}
	noiseMax := blockNoise(maxDB[dec.loBin:dec.hiBin])
	noiseMean := blockNoise(meanDB[dec.loBin:dec.hiBin])
	d.noiseMax = smooth(d.noiseMax, noiseMax)
	d.noiseMean = smooth(d.noiseMean, noiseMean)

	for i := range dec.Excess {
		dec.Excess[i] = float32(math.Inf(-1))
	}
	for i := dec.loBin; i < dec.hiBin; i++ {
		dec.Excess[i] = float32(maxDB[i] - d.noiseMax[(i-dec.loBin)/noiseBlockBins])
	}
	dec.NoiseDBFS = float32(median(slices.Clone(d.noiseMean)))
	dec.Peaks = d.peaks(dec)
	d.resetInterval()
	return dec
}

// peaks сливает соседние занятые бины в участки; разрыв до mergeGapBins не делит участок.
// Бин занят, если выше порога он сам или медиана его соседей. Участок засчитывается,
// только если хотя бы один бин в нём выше порога сам: медиана склеивает куски сигнала,
// но новых сигналов не находит.
func (d *Detector) peaks(dec Decision) []Peak {
	smoothed := smoothExcess(dec)
	var out []Peak
	start, last := -1, -1
	var snr float32
	flush := func() {
		if start < 0 || snr < ThresholdDB {
			return
		}
		var pw, pwf float64
		for i := start; i <= last; i++ {
			if e := float64(dec.Excess[i]); e > 0 {
				w := math.Pow(10, e/10)
				pw += w
				pwf += w * dec.binFreq(i)
			}
		}
		out = append(out, Peak{
			LoHz: dec.binFreq(start) - dec.BinHz/2, HiHz: dec.binFreq(last) + dec.BinHz/2, SNRDB: snr,
			Power: pw, CentroidHz: pwf / pw,
		})
	}
	for i := dec.loBin; i < dec.hiBin; i++ {
		if dec.Excess[i] < ThresholdDB && smoothed[i] < smoothThresholdDB {
			continue
		}
		if start < 0 || i-last > mergeGapBins+1 {
			flush()
			start, snr = i, dec.Excess[i]
		}
		last = i
		snr = max(snr, dec.Excess[i])
	}
	flush()
	return mergeNearbyPeaks(out)
}

// mergeNearbyPeaks сливает слабый край с соседним сильным горбом: провал до
// nearbyMergeGapHz, вместе не шире nearbyMergeMaxWidthHz, горб ≥ nearbyMergeMinSNRDB.
// Соседние вещательные станции (~200–400 кГц) не сходятся.
func mergeNearbyPeaks(peaks []Peak) []Peak {
	if len(peaks) < 2 {
		return peaks
	}
	out := make([]Peak, 0, len(peaks))
	cur := peaks[0]
	for _, p := range peaks[1:] {
		gap := p.LoHz - cur.HiHz
		width := p.HiHz - cur.LoHz
		body := max(cur.HiHz-cur.LoHz, p.HiHz-p.LoHz)
		strong := max(cur.SNRDB, p.SNRDB) >= nearbyMergeMinSNRDB
		if gap >= 0 && gap <= nearbyMergeGapHz && width <= nearbyMergeMaxWidthHz &&
			body >= nearbyMergeMinBodyHz && strong {
			pw := cur.Power + p.Power
			cen := (cur.LoHz + p.HiHz) / 2
			if pw > 0 {
				cen = (cur.CentroidHz*cur.Power + p.CentroidHz*p.Power) / pw
			}
			cur = Peak{
				LoHz: cur.LoHz, HiHz: p.HiHz, SNRDB: max(cur.SNRDB, p.SNRDB),
				Power: pw, CentroidHz: cen,
			}
			continue
		}
		out = append(out, cur)
		cur = p
	}
	return append(out, cur)
}

// smoothExcess — медиана превышения по smoothBins соседним бинам годной полосы;
// у краёв окно укорачивается.
func smoothExcess(dec Decision) []float32 {
	out := make([]float32, len(dec.Excess))
	var w [smoothBins]float32
	for i := dec.loBin; i < dec.hiBin; i++ {
		n := 0
		for j := max(dec.loBin, i-smoothBins/2); j <= min(dec.hiBin-1, i+smoothBins/2); j++ {
			w[n] = dec.Excess[j]
			n++
		}
		s := w[:n]
		slices.Sort(s)
		if n%2 == 1 {
			out[i] = s[n/2]
		} else {
			out[i] = (s[n/2-1] + s[n/2]) / 2
		}
	}
	return out
}

// blockNoise — шум по блокам: медиана блока, затем медиана медиан в окне соседних блоков.
func blockNoise(db []float64) []float64 {
	n := (len(db) + noiseBlockBins - 1) / noiseBlockBins
	meds := make([]float64, n)
	for b := range meds {
		meds[b] = median(slices.Clone(db[b*noiseBlockBins : min(len(db), (b+1)*noiseBlockBins)]))
	}
	k := max(1, int(float64(n)*noiseWindowFraction/2))
	out := make([]float64, n)
	for b := range out {
		out[b] = median(slices.Clone(meds[max(0, b-k):min(n, b+k+1)]))
	}
	return out
}

// smooth сглаживает шум во времени; при смене числа блоков начинает с нового значения.
func smooth(prev, next []float64) []float64 {
	if len(prev) != len(next) {
		return next
	}
	for i := range prev {
		prev[i] += noiseAlpha * (next[i] - prev[i])
	}
	return prev
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return math.Inf(-1)
	}
	slices.Sort(v)
	m := len(v) / 2
	if len(v)%2 == 1 {
		return v[m]
	}
	return (v[m-1] + v[m]) / 2
}

func toDB(p float64) float64 {
	if p <= 0 {
		return float64(dsp.DBFloor)
	}
	return 10 * math.Log10(p)
}
