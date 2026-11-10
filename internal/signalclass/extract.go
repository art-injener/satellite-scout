package signalclass

import "github.com/art-injener/satellite-scout/internal/dsp"

const (
	// sliceFactor — во сколько раз частота отсчётов участка больше его ширины: по краям
	// остаётся шум, по нему меряется уровень шума.
	sliceFactor = 2.5
	// minSliceRate — нижняя частота отсчётов участка: у узких сигналов (CW, несущая) иначе
	// мало шума для оценки и грубое разрешение по времени.
	minSliceRate = 25_000.0
	// extractTapsPerDecimation — длина фильтра на единицу прореживания: окно Ханна даёт
	// переход около пятой части выходной частоты отсчётов.
	extractTapsPerDecimation = 16
	// extractCutoff — срез фильтра вырезки в долях выходной частоты отсчётов.
	extractCutoff = 0.35
	// wfmExtractMinHz — вырезка не уже канала с пилотом и RDS (≈2·(75+57) кГц).
	// Горб обнаружителя часто 120–150 кГц и иначе срезает 57 кГц.
	wfmExtractMinHz = 220_000.0
)

// ExtractBandwidth — ширина для Extract: у широкой ЧМ не уже канала с RDS.
func ExtractBandwidth(bandwidthHz float64) float64 {
	if bandwidthHz >= wfmMinHz {
		return max(bandwidthHz, wfmExtractMinHz)
	}
	return bandwidthHz
}

// ExtractBandwidthAt — как ExtractBandwidth, плюс в плане 87,5–108 МГц всегда
// оставляем место под пилот и RDS, даже если горб обнаружителя уже 80–100 кГц.
func ExtractBandwidthAt(centerHz, bandwidthHz float64) float64 {
	if centerHz >= broadcastLoHz && centerHz <= broadcastHiHz {
		return max(bandwidthHz, wfmExtractMinHz)
	}
	return ExtractBandwidth(bandwidthHz)
}

// Extract переносит участок со сдвигом offsetHz от центра на ноль и прореживает поток
// до частоты не ниже 2,5 ширины участка bandwidthHz. Возвращает отсчёты без разгона
// фильтра и их частоту.
func Extract(iq []complex64, rate, offsetHz, bandwidthHz float64) ([]complex64, float64) {
	bandwidthHz = ExtractBandwidth(bandwidthHz)
	want := max(sliceFactor*bandwidthHz, minSliceRate)
	dec := max(1, int(rate/want))
	out := rate / float64(dec)
	taps := dsp.DesignLowpass(extractCutoff*out, rate, extractTapsPerDecimation*dec+1, dsp.WindowHann)
	y := dsp.NewXlatingFIR(offsetHz, rate, dec, taps).Process(iq)
	warm := len(taps)/dec + 1
	if len(y) <= warm {
		return nil, out
	}
	return y[warm:], out
}
