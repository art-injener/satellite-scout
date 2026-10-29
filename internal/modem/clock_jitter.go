package modem

import "math"

const (
	// jitterNoLock — ошибка фазы тактов при переходах в случайные моменты (на шуме):
	// среднее |0,5 − фаза| для равномерной фазы. С неё мера стартует, пока сигнала нет.
	jitterNoLock = 0.25
	// jitterGain — вес нового перехода в средней ошибке: около 64 переходов памяти.
	jitterGain = 1.0 / 64
)

// trackJitter добавляет к средней ошибке фазы тактов переход, пришедший при фазе clock;
// без ошибки переход приходится на середину между отсчётами решения (фаза 0,5).
func trackJitter(jitter, clock float64) float64 {
	return jitter + jitterGain*(math.Abs(0.5-clock)-jitter)
}
