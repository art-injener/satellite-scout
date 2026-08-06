package dsp

// SpectrumPipeline — цепочка «IQ → окно → FFT → reducer → downsample → dBFS».
// Чистая обработка сигнала, не знает про SSTM и транспорт.
// Вызывать из одной горутины.
type SpectrumPipeline struct {
	spectrum   *Spectrum
	reducer    *Reducer
	window     []float64
	fftSize    int
	targetBins int
}

// NewSpectrumPipeline создаёт конвейер.
//   - fftSize — размер FFT (степень двойки; = размер блока IQ)
//   - targetBins — сколько бинов хотим на выходе (downsample); 0 = без сжатия
//   - mode — режим накопления (avg, max-hold и т.д.)
//   - wt — оконная функция (Hann, Blackman-Harris)
func NewSpectrumPipeline(fftSize, targetBins int, mode DisplayMode, wt WindowType) *SpectrumPipeline {
	if targetBins <= 0 || targetBins > fftSize {
		targetBins = fftSize
	}
	return &SpectrumPipeline{
		spectrum:   NewSpectrum(fftSize),
		reducer:    NewReducer(fftSize, mode),
		window:     MakeWindowCoeffs(fftSize, wt),
		fftSize:    fftSize,
		targetBins: targetBins,
	}
}

// Push принимает блок IQ ровно длины FFTSize.
// Применяет окно, считает спектр мощности и кладёт в reducer.
// Сами samples модифицируются (оконная функция).
func (p *SpectrumPipeline) Push(samples []complex64) {
	if len(samples) != p.fftSize {
		panic("dsp: SpectrumPipeline.Push: len(samples) != fftSize")
	}
	ApplyWindow(samples, p.window)
	power := p.spectrum.PowerSpectrum(samples)
	p.reducer.Push(power)
}

// Flush отдаёт накопленный спектр в dBFS, уже сжатый до targetBins.
// Если Push не вызывали — возвращает nil.
// Возвращённый слайс валиден до следующего Flush.
func (p *SpectrumPipeline) Flush() []float32 {
	dbfs := p.reducer.Flush()
	if dbfs == nil {
		return nil
	}
	if p.targetBins < p.fftSize {
		return DownsampleMax(dbfs, p.targetBins)
	}
	return dbfs
}

// Reset сбрасывает reducer, не трогая окно и FFT.
func (p *SpectrumPipeline) Reset() {
	p.reducer.Reset()
}

// FFTSize возвращает размер FFT (= размер ожидаемого блока IQ).
func (p *SpectrumPipeline) FFTSize() int {
	return p.fftSize
}

// TargetBins возвращает количество бинов на выходе.
func (p *SpectrumPipeline) TargetBins() int {
	return p.targetBins
}
