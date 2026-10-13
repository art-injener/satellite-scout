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
// Current, Avg и ExpAvg описывают окно одного кадра UI и после Flush начинают заново;
// Avg после SetAvgFrames(n) усредняет все FFT последних n кадров UI.
// MaxHold и MinHold держат экстремум до Reset: пик, мелькнувший в одном кадре,
// остаётся на экране, пока поток не переоткроют или приёмник не перестроят.
// Вызывать из одной горутины. Результат Flush перезаписывается при следующем вызове.
type Reducer struct {
	mode    DisplayMode
	bins    int
	alpha   float32   // вес нового кадра в ExpAvg (0 < α < 1)
	accum   []float64 // накопленные мощности (линейный масштаб)
	out     []float32 // результат в dBFS, отдаётся через Flush
	count   int       // кадров с последнего сброса накопления
	pending int       // кадров с последнего Flush

	// Среднее по нескольким кадрам UI: суммы мощности и число FFT в каждом,
	// кольцо на n позиций; пустая позиция — histN == 0
	hist    [][]float64
	histN   []int
	histPos int
	total   []float64
}

// SetAvgFrames задаёт окно ModeAvg в кадрах UI; n ≤ 1 — среднее за один кадр.
// Сбрасывает накопленное.
func (r *Reducer) SetAvgFrames(n int) {
	r.hist, r.histN, r.total = nil, nil, nil
	if n > 1 {
		r.hist = make([][]float64, n)
		for i := range r.hist {
			r.hist[i] = make([]float64, r.bins)
		}
		r.histN = make([]int, n)
		r.total = make([]float64, r.bins)
	}
	r.Reset()
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
	r.pending++
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

// Flush отдаёт итоговый спектр в dBFS. Режимы окна обнуляют состояние, режимы
// удержания его сохраняют. Если с прошлого Flush не было Push — вернёт nil.
func (r *Reducer) Flush() []float32 {
	if r.pending == 0 {
		return nil
	}
	r.pending = 0

	src := r.accum
	scale := 1.0
	if r.mode == ModeAvg {
		if r.hist != nil {
			src, scale = r.pushHistory()
		} else {
			scale = 1.0 / float64(r.count)
		}
	}

	for i := 0; i < r.bins; i++ {
		p := src[i] * scale
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

	if r.mode != ModeMaxHold && r.mode != ModeMinHold {
		r.resetFrame()
	}
	return r.out
}

// pushHistory кладёт текущий кадр UI в кольцо и возвращает сумму мощностей
// по кольцу и множитель среднего. Сумма пересчитывается заново, а не
// вычитанием старого кадра: так не копится ошибка округления.
func (r *Reducer) pushHistory() (sum []float64, scale float64) {
	copy(r.hist[r.histPos], r.accum)
	r.histN[r.histPos] = r.count
	r.histPos = (r.histPos + 1) % len(r.hist)

	clear(r.total)
	n := 0
	for k := 0; k < len(r.hist); k++ {
		if r.histN[k] == 0 {
			continue
		}
		n += r.histN[k]
		for i, v := range r.hist[k] {
			r.total[i] += v
		}
	}
	return r.total, 1.0 / float64(n)
}

// resetFrame начинает новый кадр UI, не трогая историю среднего.
func (r *Reducer) resetFrame() {
	clear(r.accum)
	r.count = 0
	r.pending = 0
}

// Reset обнуляет накопленные данные, в том числе удержанные экстремумы
// и историю среднего по кадрам.
func (r *Reducer) Reset() {
	r.resetFrame()
	clear(r.histN)
	r.histPos = 0
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
