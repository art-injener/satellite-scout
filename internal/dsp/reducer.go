package dsp

import "math"

// DisplayMode — как накапливать FFT-кадры перед отдачей на UI.
// Значения совпадают с полем display_mode в SSTM-заголовке (ADR-002).
type DisplayMode uint8

const (
	ModeCurrent DisplayMode = 0x00
	ModeAvg     DisplayMode = 0x01
	ModeMaxHold DisplayMode = 0x02
	ModeMinHold DisplayMode = 0x03
	ModeExpAvg  DisplayMode = 0x05
)

// Reducer собирает несколько FFT-кадров (при 10 MSPS это ~300 штук на кадр UI)
// и отдаёт один итоговый спектр в dBFS — уже готовый для отправки клиенту.
// Вызывать из одной горутины. Результат Flush перезаписывается при следующем вызове.
type Reducer struct {
	mode  DisplayMode
	bins  int
	alpha float32   // вес нового кадра в ExpAvg (0 < α < 1)
	accum []float64 // накопленные мощности (линейный масштаб)
	out   []float32 // результат в dBFS, отдаётся через Flush
	count int
}

// NewReducer создаёт Reducer на bins частотных бинов.
// alpha задаёт сглаживание для ModeExpAvg (по умолчанию 0.5).
func NewReducer(bins int, mode DisplayMode, alpha ...float32) *Reducer {
	a := float32(0.5)
	if len(alpha) > 0 {
		a = alpha[0]
	}
	return &Reducer{
		mode:  mode,
		bins:  bins,
		alpha: a,
		accum: make([]float64, bins),
		out:   make([]float32, bins),
	}
}

// Push принимает один FFT-кадр (линейная мощность) и накапливает его.
func (r *Reducer) Push(power []float64) {
	// Для min/max/exp нулевой аккумулятор даст неправильный результат —
	// первый кадр просто запоминаем как начальное значение
	if r.count == 0 {
		switch r.mode {
		case ModeMaxHold, ModeMinHold, ModeExpAvg:
			copy(r.accum, power)
			r.count++
			return
		}
	}

	switch r.mode {
	case ModeCurrent:
		copy(r.accum, power)
	case ModeAvg:
		for i := range r.accum {
			r.accum[i] += power[i]
		}
	case ModeMaxHold:
		for i := range r.accum {
			r.accum[i] = math.Max(r.accum[i], power[i])
		}
	case ModeMinHold:
		for i := range r.accum {
			r.accum[i] = math.Min(r.accum[i], power[i])
		}
	case ModeExpAvg:
		a := float64(r.alpha)
		for i := range r.accum {
			r.accum[i] = a*power[i] + (1.0-a)*r.accum[i]
		}
	}
	r.count++
}

// Flush отдаёт итоговый спектр в dBFS и обнуляет состояние.
// Если ни одного Push не было — вернёт nil.
func (r *Reducer) Flush() []float32 {
	if r.count == 0 {
		return nil
	}

	if r.mode == ModeAvg {
		inv := 1.0 / float64(r.count)
		for i := range r.accum {
			r.accum[i] *= inv
		}
	}

	for i := 0; i < r.bins; i++ {
		p := r.accum[i]
		if p <= 0 {
			r.out[i] = DBFloor
			continue
		}
		db := float32(10.0 * math.Log10(p))
		if db < DBFloor {
			db = DBFloor
		}
		r.out[i] = db
	}

	r.Reset()
	return r.out
}

// Reset обнуляет накопленные данные.
func (r *Reducer) Reset() {
	clear(r.accum)
	r.count = 0
}

// DownsampleMax уменьшает количество бинов до target, беря максимум в каждой группе.
// Максимум сохраняет узкие пики сигналов — среднее бы их «размазало».
func DownsampleMax(bins []float32, target int) []float32 {
	if len(bins) == 0 {
		return nil
	}
	if target <= 0 {
		return nil
	}
	n := len(bins)
	if target >= n {
		out := make([]float32, n)
		copy(out, bins)
		return out
	}

	factor := n / target
	out := make([]float32, target)
	for i := 0; i < target; i++ {
		m := bins[i*factor]
		for j := 1; j < factor; j++ {
			if bins[i*factor+j] > m {
				m = bins[i*factor+j]
			}
		}
		out[i] = m
	}
	return out
}
