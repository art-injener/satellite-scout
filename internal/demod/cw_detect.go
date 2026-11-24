package demod

import (
	"math"
	"slices"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/modem"
)

// DemodCW — профиль телеграфа CW (звук биений 700 Гц в узкой полосе).
const DemodCW = "cw"

const (
	cwDecim         = 4     // 64 кГц делим на 4 до 16 кГц
	cwFilterCutoff  = 500.0 // полуширина полосы CW, Гц
	cwDetectTaps    = 255
	cwEnvStepSec    = 0.005 // шаг замера огибающей, 5 мс
	cwEnvWinSec     = 0.020 // согласованное окно сглаживания под точку CW, 20 мс
	cwBufMax        = 400   // скользящее окно анализа, 2 с
	cwBufMin        = 160   // минимум 0,8 с до первого решения
	cwMinNarrowFrac = 0.60  // доля мощности в ±500 Гц относительно полосы канала
	cwMinContrast   = 6.0   // контраст включённой и выключенной огибающей (~8 дБ)
	cwMinOffShare   = 0.15
	cwMaxOffShare   = 0.85
	cwMinValidRuns  = 6
	cwMinValidRatio = 0.70
	cwMinRunMs      = 25.0
	cwMaxRunMs      = 450.0
	cwMaxOffMs      = 1200.0
)

// cwDetector — узкополосный детектор телеграфа и звуковой демодулятор CW для режима «Авто».
// Выделяет полосу ±500 Гц, сглаживает огибающую окном порядка длительности точки (20 мс)
// и подтверждает телеграф по узкополосности и серии импульсов с длительностями Морзе.
type cwDetector struct {
	rate   float64
	cwRate float64
	fine   *dsp.XlatingFIR
	audio  *modem.SSBDemod

	stepSamples int
	winSteps    int

	// Накопитель текущего 5-мс шага.
	accNarrow float64
	accWide   float64
	accN      int
	accWideN  int

	// Кольцевая история замеров огибающей (по 5 мс).
	rawEnv   []float64
	ratios   []float64
	audioBuf []float32
}

func newCWDetector(midRate float64) *cwDetector {
	d := &cwDetector{}
	d.ensure(midRate)
	return d
}

func (d *cwDetector) ensure(midRate float64) {
	if midRate <= 0 || midRate == d.rate {
		return
	}
	d.rate = midRate
	d.cwRate = midRate / cwDecim
	d.fine = dsp.NewXlatingFIR(0, midRate, cwDecim,
		dsp.DesignLowpass(cwFilterCutoff, midRate, cwDetectTaps, dsp.WindowBlackmanHarris))
	d.audio = modem.NewSSBDemod(modem.SSBConfig{
		SampleRate: d.cwRate, AudioRate: AudioRate, ShiftHz: cwToneHz,
	})
	d.stepSamples = max(1, int(math.Round(cwEnvStepSec*d.cwRate)))
	d.winSteps = max(1, int(math.Round(cwEnvWinSec/cwEnvStepSec)))
	d.accNarrow, d.accWide, d.accN, d.accWideN = 0, 0, 0, 0
	d.rawEnv = d.rawEnv[:0]
	d.ratios = d.ratios[:0]
}

// PushSearch обновляет оценку телеграфа по блоку канала 64 кГц и возвращает true при уверенном CW.
func (d *cwDetector) PushSearch(ch64 []complex64, midRate float64) bool {
	if len(ch64) == 0 || midRate <= 0 {
		return false
	}
	d.ensure(midRate)
	cwIQ := d.fine.Process(ch64)
	if len(cwIQ) == 0 {
		return false
	}

	// Полная мощность в полосе канала для проверки узкополосности сигнала.
	var wideSum float64
	for _, s := range ch64 {
		re, im := float64(real(s)), float64(imag(s))
		wideSum += re*re + im*im
	}
	wideAvg := wideSum / float64(len(ch64))

	for _, s := range cwIQ {
		re, im := float64(real(s)), float64(imag(s))
		d.accNarrow += re*re + im*im
		d.accN++
		if d.accN >= d.stepSamples {
			pNarrow := d.accNarrow / float64(d.accN)
			d.rawEnv = appendCapped(d.rawEnv, pNarrow, cwBufMax)
			ratio := 0.0
			if wideAvg > 1e-18 {
				ratio = min(pNarrow/wideAvg, 1.0)
			}
			d.ratios = appendCapped(d.ratios, ratio, cwBufMax)
			d.accNarrow, d.accN = 0, 0
		}
	}
	return d.detected()
}

// PushAudio демодулирует узкую полосу CW в звук с тоном биений 700 Гц.
func (d *cwDetector) PushAudio(ch64 []complex64, midRate float64, squelchOpen bool) []float32 {
	if len(ch64) == 0 || midRate <= 0 {
		return nil
	}
	d.ensure(midRate)
	cwIQ := d.fine.Process(ch64)
	d.audioBuf = d.audio.Demodulate(d.audioBuf[:0], cwIQ)
	if !squelchOpen {
		clear(d.audioBuf)
	}
	return d.audioBuf
}

func (d *cwDetector) detected() bool {
	n := len(d.rawEnv)
	if n < cwBufMin {
		return false
	}
	env := smoothCWEnv(d.rawEnv, d.winSteps)
	sorted := slices.Clone(env)
	slices.Sort(sorted)
	high := sorted[min(n-1, int(0.90*float64(n)))]
	low := sorted[min(n-1, int(0.10*float64(n)))]
	if high <= 1e-18 || high < cwMinContrast*max(low, 1e-18) {
		return false
	}

	lowT := low + 0.25*(high-low)
	highT := low + 0.55*(high-low)
	if !cwNarrowAndKeyed(env, d.ratios, lowT, highT) {
		return false
	}
	return cwValidMorseRuns(env, lowT, highT)
}

func smoothCWEnv(raw []float64, winSteps int) []float64 {
	env := make([]float64, len(raw))
	var sum float64
	for i, v := range raw {
		sum += v
		if i >= winSteps {
			sum -= raw[i-winSteps]
		}
		env[i] = sum / float64(min(i+1, winSteps))
	}
	return env
}

func cwNarrowAndKeyed(env, ratios []float64, lowT, highT float64) bool {
	var activeRatios []float64
	off := 0
	for i, v := range env {
		if v < lowT {
			off++
		}
		if v > highT && i < len(ratios) {
			activeRatios = append(activeRatios, ratios[i])
		}
	}
	offShare := float64(off) / float64(len(env))
	if offShare < cwMinOffShare || offShare > cwMaxOffShare || len(activeRatios) < 8 {
		return false
	}
	slices.Sort(activeRatios)
	return activeRatios[len(activeRatios)/2] >= cwMinNarrowFrac
}

func cwValidMorseRuns(env []float64, lowT, highT float64) bool {
	stateOn := env[0] > highT
	runStart := 0
	totalOn, validOn, validOff := 0, 0, 0
	stepMs := cwEnvStepSec * 1000
	for i := 1; i < len(env); i++ {
		v := env[i]
		switch {
		case stateOn && v < lowT:
			durMs := float64(i-runStart) * stepMs
			totalOn++
			if durMs >= cwMinRunMs && durMs <= cwMaxRunMs {
				validOn++
			}
			stateOn, runStart = false, i
		case !stateOn && v > highT:
			durMs := float64(i-runStart) * stepMs
			if durMs >= cwMinRunMs && durMs <= cwMaxOffMs {
				validOff++
			}
			stateOn, runStart = true, i
		}
	}
	if validOn < cwMinValidRuns || validOff < cwMinValidRuns-1 || totalOn == 0 {
		return false
	}
	return float64(validOn)/float64(totalOn) >= cwMinValidRatio
}

func appendCapped(buf []float64, v float64, maxLen int) []float64 {
	if len(buf) < maxLen {
		return append(buf, v)
	}
	copy(buf, buf[1:])
	buf[len(buf)-1] = v
	return buf
}
