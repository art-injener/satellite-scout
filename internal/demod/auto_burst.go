package demod

import (
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	burstMaxDuration  = 3 * time.Second       // максимальная длина буфера вспышки
	burstMinDuration  = 30 * time.Millisecond // короче не разбираем (щелчок/импульсная помеха)
	burstPreDuration  = 60 * time.Millisecond // запас до открытия порога, чтобы не срезать преамбулу
	burstTailDuration = 20 * time.Millisecond // нулевой хвост при реплее для выталкивания последних бит
)

type burstMeta struct {
	rate   float64
	freqHz uint64
	tsMs   uint64
}

// burstBuffer копит широкий выход грубого фильтра (64 кГц) во время открытого порога,
// чтобы после закрытия порога (или по лимиту 3 с) центрировать вспышку по частоте и
// прогнать целевых кандидатов КА с оптимальным фильтром.
type burstBuffer struct {
	pre       []complex64
	samples   []complex64
	meta      burstMeta
	active    bool
	hadPacket bool
	capped    bool
}

func (b *burstBuffer) reset() {
	b.pre = b.pre[:0]
	b.samples = b.samples[:0]
	b.active = false
	b.hadPacket = false
	b.capped = false
}

// feed принимает очередной блок coarse (64 кГц) и возвращает накопленную вспышку,
// когда она закончилась без декодированных потоком кадров или достигла лимита 3 с.
func (b *burstBuffer) feed(
	coarse []complex64,
	rate float64,
	open bool,
	freqHz, tsMs uint64,
	streamHadPkt bool,
) ([]complex64, burstMeta, bool) {
	if rate <= 0 || len(coarse) == 0 {
		return nil, burstMeta{}, false
	}
	if open {
		return b.feedOpen(coarse, rate, freqHz, tsMs, streamHadPkt)
	}
	return b.feedClosed(coarse, rate)
}

func (b *burstBuffer) feedOpen(
	coarse []complex64,
	rate float64,
	freqHz, tsMs uint64,
	streamHadPkt bool,
) ([]complex64, burstMeta, bool) {
	if streamHadPkt {
		b.hadPacket = true
	}
	if !b.active {
		if b.capped {
			return nil, burstMeta{}, false
		}
		b.active = true
		b.meta = burstMeta{rate: rate, freqHz: freqHz, tsMs: tsMs}
		b.hadPacket = streamHadPkt
		b.samples = append(b.samples[:0], b.pre...)
		b.pre = b.pre[:0]
	}
	if b.hadPacket {
		b.samples = b.samples[:0]
		return nil, burstMeta{}, false
	}
	maxSamples := int(burstMaxDuration.Seconds() * rate)
	b.samples = append(b.samples, coarse...)
	if len(b.samples) < maxSamples {
		return nil, burstMeta{}, false
	}
	// Достигли лимита 3 с: отдаём накопленное на однократный разбор,
	// длинная передача дальше идёт только потоковым путём.
	ready := append([]complex64(nil), b.samples[:maxSamples]...)
	meta := b.meta
	b.samples = b.samples[:0]
	b.active = false
	b.capped = true
	return ready, meta, true
}

func (b *burstBuffer) feedClosed(coarse []complex64, rate float64) ([]complex64, burstMeta, bool) {
	b.capped = false
	var ready []complex64
	var meta burstMeta
	var ok bool
	minSamples := int(burstMinDuration.Seconds() * rate)
	if b.active {
		b.active = false
		if !b.hadPacket && len(b.samples) >= minSamples {
			ready = append([]complex64(nil), b.samples...)
			meta = b.meta
			ok = true
		}
		b.samples = b.samples[:0]
		b.hadPacket = false
	}
	// Держим короткий пре-буфер до следующего открытия порога.
	if preMax := int(burstPreDuration.Seconds() * rate); preMax > 0 {
		b.pre = append(b.pre, coarse...)
		if len(b.pre) > preMax {
			copy(b.pre, b.pre[len(b.pre)-preMax:])
			b.pre = b.pre[:preMax]
		}
	}
	return ready, meta, ok
}

// welchPowerSpectrum усредняет спектр мощности методом Уэлча (окно Blackman-Harris, шаг N/2).
func welchPowerSpectrum(samples []complex64, n int) ([]float64, bool) {
	if len(samples) < n {
		return nil, false
	}
	spec := dsp.NewSpectrum(n)
	win := dsp.MakeWindowCoeffs(n, dsp.WindowBlackmanHarris)
	buf := make([]complex64, n)
	avg := make([]float64, n)

	step := n / 2
	segs := 0
	for off := 0; off+n <= len(samples) && segs < 64; off += step {
		copy(buf, samples[off:off+n])
		dsp.ApplyWindow(buf, win)
		for i, p := range spec.PowerSpectrum(buf) {
			avg[i] += p
		}
		segs++
	}
	if segs == 0 {
		return nil, false
	}
	inv := 1.0 / float64(segs)
	for i := range avg {
		avg[i] *= inv
	}
	return avg, true
}

// estimateBurstSpectrum оценивает центр смещения Δf и занятую ширину вспышки
// в полосе грубого фильтра (64 кГц). Возвращает ok=false, если сигнал не выделяется над шумом.
func estimateBurstSpectrum(samples []complex64, rate float64) (float64, float64, bool) {
	if rate <= 0 || len(samples) < 256 {
		return 0, 0, false
	}
	n := 512
	if len(samples) < n {
		n = 256
	}
	avg, ok := welchPowerSpectrum(samples, n)
	if !ok {
		return 0, 0, false
	}

	// Плоская полоса грубого фильтра [-0.35*rate, +0.35*rate].
	center := n / 2
	halfFlat := int(math.Floor(0.35 * float64(n)))
	flatLo := max(0, center-halfFlat)
	flatHi := min(n, center+halfFlat)
	if flatHi <= flatLo {
		return 0, 0, false
	}
	sorted := make([]float64, 0, flatHi-flatLo)
	peakIdx := flatLo
	peakPower := 0.0
	for i := flatLo; i < flatHi; i++ {
		p := avg[i]
		sorted = append(sorted, p)
		if p > peakPower {
			peakPower = p
			peakIdx = i
		}
	}
	slices.Sort(sorted)
	noiseBin := sorted[len(sorted)/5] // 20-й перцентиль — шумовой бин
	if peakPower <= 0 || (noiseBin > 0 && peakPower < 4*noiseBin) {
		return 0, 0, false
	}

	// Порог по краям спектра: -13 дБ от пика, но не ниже +6 дБ над шумом.
	// Поиск ведётся в окне ±14 кГц вокруг главного пика, чтобы захватить оба горба 2-FSK.
	binHz := rate / float64(n)
	winBins := int(math.Ceil(14_000 / binHz))
	searchLo := max(flatLo, peakIdx-winBins)
	searchHi := min(flatHi-1, peakIdx+winBins)
	thresh := max(4*noiseBin, 0.05*peakPower)

	lo, hi := findBurstBounds(avg, searchLo, searchHi, peakIdx, thresh)
	centerHz := ((float64(lo)+float64(hi))/2.0 - float64(center)) * binHz
	widthHz := float64(hi-lo+1) * binHz
	return centerHz, widthHz, true
}

func findBurstBounds(avg []float64, searchLo, searchHi, peakIdx int, thresh float64) (int, int) {
	lo, hi := peakIdx, peakIdx
	for i := searchLo; i <= searchHi; i++ {
		if avg[i] >= thresh {
			lo = i
			break
		}
	}
	for i := searchHi; i >= searchLo; i-- {
		if avg[i] >= thresh {
			hi = i
			break
		}
	}
	return lo, hi
}

type burstReplayCtx struct {
	meta       burstMeta
	ch64       []complex64
	ch32       []complex64
	computed   map[demodKey][]byte
	g3ruhByKey map[demodKey][]byte
}

func (c *burstReplayCtx) bitsFor(id string, spec autoProfileSpec) ([]byte, bool) {
	bits, done := c.computed[spec.key]
	if !done {
		dem := newSharedLineDemod(spec.key, id, c.meta.rate)
		if dem == nil {
			return nil, false
		}
		ch := c.ch64
		if spec.need32 {
			ch = c.ch32
		}
		bits = dem.Demodulate(nil, ch)
		c.computed[spec.key] = bits
	}
	if !spec.g3ruh {
		return bits, true
	}
	gb, ok := c.g3ruhByKey[spec.key]
	if !ok {
		var desc modem.G3RUHDescrambler
		gb = desc.Descramble(nil, bits)
		c.g3ruhByKey[spec.key] = gb
	}
	return gb, true
}

// replayCenteredBurst сдвигает накопленную вспышку на -centerHz в 0 Гц, фильтрует оптимальным
// ФНЧ и прогоняет свежие экземпляры кадровых кандидатов cands.
func (a *AutoPicker) replayCenteredBurst(
	samples []complex64,
	meta burstMeta,
	cands []string,
	now time.Time,
	allowLock bool,
) ([]taggedPacket, string, float64) {
	centerHz, widthHz, ok := estimateBurstSpectrum(samples, meta.rate)
	if !ok {
		return nil, "", 0
	}

	rctx := buildBurstReplayCtx(samples, meta, cands, centerHz, widthHz)
	seenSig := make(map[pipelineSig]bool, len(cands))
	var out []taggedPacket
	lockedID := ""
	for _, id := range cands {
		if !isFrameProfile(id) {
			continue
		}
		spec := autoSpecFor(id)
		sig := spec.sig()
		if seenSig[sig] {
			continue
		}
		seenSig[sig] = true
		pkts := a.runBurstCandidate(rctx, id, spec, now, allowLock)
		out = append(out, pkts...)
		if allowLock && lockedID == "" {
			if hits := a.hitsInWindow(id, now); hits >= needGoodFor(id) {
				lockedID = id
				a.pinned = id
				a.lastGood = now
				a.remember(a.key, id)
				slog.Info("demod auto: locked (burst)",
					slog.String(slogAttrKey, a.key),
					slog.String("profile", id),
					slog.Float64("center_hz", math.Round(centerHz)),
					slog.Float64("width_hz", math.Round(widthHz)),
					slog.Int("hits", hits),
				)
			}
		}
	}
	return out, lockedID, centerHz
}

func buildBurstReplayCtx(
	samples []complex64,
	meta burstMeta,
	cands []string,
	centerHz, widthHz float64,
) *burstReplayCtx {
	maxBW := float64(autoBandwidthFor(cands, sdr.MaxChannelBandwidthHz))
	optBW := max(3_000, min(maxBW, widthHz*1.35))
	cutoff := min(optBW/2, 0.15*meta.rate)
	fineCoeffs := dsp.DesignLowpass(cutoff, meta.rate, fineTaps, dsp.WindowBlackmanHarris)

	tailLen := int(burstTailDuration.Seconds() * meta.rate)
	padded := make([]complex64, len(samples)+tailLen)
	copy(padded, samples)

	need32 := false
	for _, id := range cands {
		if isFrameProfile(id) && autoSpecFor(id).need32 {
			need32 = true
			break
		}
	}
	ch64 := dsp.NewXlatingFIR(centerHz, meta.rate, 1, fineCoeffs).Process(padded)
	var ch32 []complex64
	if need32 {
		ch32 = dsp.NewXlatingFIR(centerHz, meta.rate, 2, fineCoeffs).Process(padded)
	}
	return &burstReplayCtx{
		meta:       meta,
		ch64:       ch64,
		ch32:       ch32,
		computed:   make(map[demodKey][]byte, len(cands)),
		g3ruhByKey: make(map[demodKey][]byte, len(cands)),
	}
}

func (a *AutoPicker) runBurstCandidate(
	rctx *burstReplayCtx,
	id string,
	spec autoProfileSpec,
	now time.Time,
	allowLock bool,
) []taggedPacket {
	df := newProfileDeframer(id)
	if df == nil {
		return nil
	}
	inBits, ok := rctx.bitsFor(id, spec)
	if !ok {
		return nil
	}
	frames := df.Push(nil, inBits)
	pkts := buildPackets(rctx.meta.tsMs, rctx.meta.freqHz, frames)
	out := make([]taggedPacket, 0, len(pkts))
	for _, p := range pkts {
		out = append(out, taggedPacket{Profile: id, Packet: p})
		a.lastPacketMs = rctx.meta.tsMs
		if allowLock {
			a.noteHit(id, now)
		} else {
			a.lastGood = now
		}
	}
	return out
}
