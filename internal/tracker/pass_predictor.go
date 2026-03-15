package tracker

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// Ошибки предсказания пролётов.
var (
	ErrNilPropagator    = errors.New("propagator is nil")
	ErrNilObserver      = errors.New("observer is nil")
	ErrNilTLEStore      = errors.New("TLE store is nil")
	ErrInvalidTimeRange = errors.New("invalid time range for pass prediction")
	ErrMinElevation     = errors.New("minimum elevation must be between 0 and 90 degrees")
)

// Настройки алгоритма предсказания пролётов.
const (
	// Шаг грубого поиска AOS/LOS (секунды).
	coarseStepSec = 30

	// Точность бисекции AOS/LOS (секунды).
	bisectPrecisionSec = 1.0

	// Максимальное число итераций бисекции.
	maxBisectIterations = 30

	// Шаг генерации точек SkyPath (секунды).
	skyPathStepSec = 10

	// Минимальный порог элевации по умолчанию (градусы).
	DefaultMinElevation = 5.0

	// Максимальный период предсказания по умолчанию (часы).
	DefaultPredictionHours = 24
)

// computeAER вычисляет AER (азимут, элевация, дальность) для спутника в заданный момент.
// Возвращает nil при ошибке пропагации.
func computeAER(prop *Propagator, obs *Observer, t time.Time) *AER {
	eci, err := prop.Propagate(t)
	if err != nil {
		return nil
	}

	return obs.GetAER(eci)
}

// computeElevationDeg вычисляет элевацию в градусах для спутника в заданный момент.
// Возвращает -999 при ошибке (гарантирует, что ошибка не спутается с реальной элевацией).
func computeElevationDeg(prop *Propagator, obs *Observer, t time.Time) float64 {
	aer := computeAER(prop, obs, t)
	if aer == nil {
		return -999.0
	}

	return aer.ElDeg()
}

// refineBisect уточняет момент пересечения порога элевации бисекцией.
// rising=true — ищем момент перехода из ниже → выше порога (AOS).
// rising=false — ищем момент перехода из выше → ниже порога (LOS).
// Точность: ≤ bisectPrecisionSec секунд.
func refineBisect(prop *Propagator, obs *Observer, t1, t2 time.Time, minElDeg float64, rising bool) time.Time {
	for i := range maxBisectIterations {
		_ = i
		dt := t2.Sub(t1)
		if dt.Seconds() <= bisectPrecisionSec {
			break
		}

		mid := t1.Add(dt / 2)
		elMid := computeElevationDeg(prop, obs, mid)

		// rising && above → t2=mid; !rising && !above → t2=mid
		// rising && !above → t1=mid; !rising && above → t1=mid
		if (elMid >= minElDeg) == rising {
			t2 = mid
		} else {
			t1 = mid
		}
	}

	if rising {
		return t2 // Первый момент, когда El >= minEl.
	}

	return t1 // Последний момент, когда El >= minEl.
}

// findMaxElevation находит момент максимальной элевации (TCA) бисекцией по производной.
// Ищет между tAOS и tLOS, где dEl/dt ≈ 0 (переход от роста к спаду).
func findMaxElevation(prop *Propagator, obs *Observer, tAOS, tLOS time.Time) (time.Time, float64, float64) {
	// Используем метод золотого сечения для поиска максимума.
	const phi = 1.618033988749895 // Золотое сечение.
	const resphi = 2.0 - phi

	a := tAOS
	b := tLOS
	tolerance := time.Duration(bisectPrecisionSec * float64(time.Second))

	// Начальные точки.
	x1 := a.Add(time.Duration(float64(b.Sub(a)) * resphi))
	x2 := b.Add(-time.Duration(float64(b.Sub(a)) * resphi))

	el1 := computeElevationDeg(prop, obs, x1)
	el2 := computeElevationDeg(prop, obs, x2)

	for i := range maxBisectIterations {
		_ = i
		if b.Sub(a) <= tolerance {
			break
		}

		if el1 < el2 {
			a = x1
			x1 = x2
			el1 = el2
			x2 = b.Add(-time.Duration(float64(b.Sub(a)) * resphi))
			el2 = computeElevationDeg(prop, obs, x2)
		} else {
			b = x2
			x2 = x1
			el2 = el1
			x1 = a.Add(time.Duration(float64(b.Sub(a)) * resphi))
			el1 = computeElevationDeg(prop, obs, x1)
		}
	}

	// Берём середину как TCA.
	tca := a.Add(b.Sub(a) / 2)
	aer := computeAER(prop, obs, tca)
	if aer == nil {
		return tca, 0, 0
	}

	return tca, aer.ElDeg(), aer.AzDeg()
}

// azElToXY вычисляет координаты полярной проекции из азимута и угла места.
// Формула:
//
//	r   = 1 - el_rad/(π/2)          — радиус: 0 на зените, 1 на горизонте
//	phi = π/2 - az_rad              — угол: N=вверх, E=вправо
//	X   = r * cos(phi)              — горизонталь [-1..1]
//	Y   = -(r * sin(phi))           — вертикаль [-1..1], инвертирована для SVG
//
// Ориентация: N вверху (Y<0), S внизу (Y>0), E справа (X>0), W слева (X<0).
func azElToXY(azDeg, elDeg float64) (float64, float64) {
	azRad := azDeg * Deg2Rad
	elRad := elDeg * Deg2Rad
	r := 1 - elRad/(math.Pi/2)
	phi := math.Pi/2 - azRad
	x := r * math.Cos(phi)
	y := -(r * math.Sin(phi)) // Инверсия Y для SVG-системы координат.
	// Округляем до 4 знаков — достаточно для SVG viewBox -1..1.
	x = math.Round(x*10000) / 10000
	y = math.Round(y*10000) / 10000
	return x, y
}

// makeAzElPoint создаёт AzElPoint с предвычисленными X/Y полярной проекции.
func makeAzElPoint(azDeg, elDeg float64, t time.Time) AzElPoint {
	x, y := azElToXY(azDeg, elDeg)
	return AzElPoint{
		Az:   math.Round(azDeg*10) / 10,
		El:   math.Round(elDeg*10) / 10,
		X:    x,
		Y:    y,
		Time: t.UnixMilli(),
	}
}

// findSkyAOS находит время восхода (el=0) для sky path.
// Ищет назад от aosExact с мелким шагом до el<0, бисектирует между соседними точками.
// Глубина поиска 60 минут — покрывает любые LEO/MEO пролёты.
func findSkyAOS(prop *Propagator, obs *Observer, aosExact time.Time) time.Time {
	const step = 2 * time.Second // мелкий шаг — надёжно находим ближайший переход
	const maxIter = 1800         // 1800 * 2 = 60 минут

	prev := aosExact
	cur := aosExact
	for range maxIter {
		cur = cur.Add(-step)
		if computeElevationDeg(prop, obs, cur) < 0 {
			// Нашли переход el<0 → el>=0 между cur и prev
			return refineBisect(prop, obs, cur, prev, 0.0, true)
		}
		prev = cur
	}
	return aosExact // Не нашли — спутник всегда над горизонтом
}

// findSkyLOS находит время захода (el=0) для sky path.
// Ищет вперёд от losExact с мелким шагом до el<0, бисектирует между соседними точками.
// Глубина поиска 60 минут — покрывает любые LEO/MEO пролёты.
func findSkyLOS(prop *Propagator, obs *Observer, losExact time.Time) time.Time {
	const step = 2 * time.Second
	const maxIter = 1800 // 1800 * 2 = 60 минут

	prev := losExact
	cur := losExact
	for range maxIter {
		cur = cur.Add(step)
		if computeElevationDeg(prop, obs, cur) < 0 {
			// Нашли переход el>=0 → el<0 между prev и cur
			return refineBisect(prop, obs, prev, cur, 0.0, false)
		}
		prev = cur
	}
	return losExact // Не нашли
}

// computeSkyPath генерирует массив точек Az/El/X/Y через пролёт для SVG мини-проекции.
// Шаг — skyPathStepSec секунд. Включает точки AOS и LOS.
// X/Y — предвычисленные координаты полярной проекции, фронтенд рисует их напрямую.
func computeSkyPath(prop *Propagator, obs *Observer, tAOS, tLOS time.Time) []AzElPoint {
	duration := tLOS.Sub(tAOS)
	step := time.Duration(skyPathStepSec) * time.Second

	// Оцениваем количество точек.
	numPoints := int(duration/step) + 2 // +2 для AOS и LOS.
	points := make([]AzElPoint, 0, numPoints)

	for t := tAOS; !t.After(tLOS); t = t.Add(step) {
		aer := computeAER(prop, obs, t)
		if aer == nil {
			continue
		}

		points = append(points, makeAzElPoint(aer.AzDeg(), aer.ElDeg(), t))
	}

	// Добавляем точку LOS, если последняя точка не совпала.
	if len(points) > 0 {
		lastTime := time.UnixMilli(points[len(points)-1].Time)
		if lastTime.Before(tLOS) {
			aer := computeAER(prop, obs, tLOS)
			if aer != nil {
				points = append(points, makeAzElPoint(aer.AzDeg(), aer.ElDeg(), tLOS))
			}
		}
	}

	return points
}

// buildPass формирует структуру Pass из рассчитанных параметров пролёта.
// Централизует создание Pass, включая расчёт номера орбиты и SkyPath.
func buildPass(
	prop *Propagator,
	obs *Observer,
	aosExact, tcaTime, losExact time.Time,
	tcaEl, tcaAz float64,
) *Pass {
	tle := prop.TLE()

	// AER в точках AOS и LOS.
	aosAER := computeAER(prop, obs, aosExact)
	losAER := computeAER(prop, obs, losExact)

	aosAz := 0.0
	if aosAER != nil {
		aosAz = aosAER.AzDeg()
	}

	losAz := 0.0
	if losAER != nil {
		losAz = losAER.AzDeg()
	}

	// SkyPath — траектория на небесной сфере для визуализации (от горизонта до горизонта).
	skyAOS := findSkyAOS(prop, obs, aosExact)
	skyLOS := findSkyLOS(prop, obs, losExact)
	skyPath := computeSkyPath(prop, obs, skyAOS, skyLOS)

	// Параметры спутника.
	duration := losExact.Sub(aosExact).Seconds()
	satName := ""
	noradID := 0
	orbitNumber := 0

	if tle != nil {
		satName = tle.Name
		noradID = tle.NoradID
		// Номер орбиты на момент TCA
		orbitNumber = ComputeOrbitNumber(tle, tcaTime)
	}

	return &Pass{
		NoradID:     noradID,
		SatName:     satName,
		OrbitNumber: orbitNumber,
		AOS:         aosExact.UnixMilli(),
		AOSAz:       math.Round(aosAz*10) / 10,
		TCA:         tcaTime.UnixMilli(),
		TCAEl:       math.Round(tcaEl*10) / 10,
		TCAAz:       math.Round(tcaAz*10) / 10,
		LOS:         losExact.UnixMilli(),
		LOSAz:       math.Round(losAz*10) / 10,
		Duration:    math.Round(duration*10) / 10,
		SkyPath:     skyPath,
	}
}

// PredictPasses предсказывает пролёты одного спутника над точкой наблюдения.
// prop — пропагатор спутника (из NewPropagator).
// obs — позиция наблюдателя.
// start, end — временной диапазон предсказания.
// minElDeg — минимальный угол места (градусы), обычно 5°.
// Возвращает отсортированный по AOS список пролётов.
func PredictPasses(prop *Propagator, obs *Observer, start, end time.Time, minElDeg float64) ([]*Pass, error) {
	if err := validatePredictArgs(prop, obs, start, end, minElDeg); err != nil {
		return nil, err
	}

	if prop.TLE() != nil && IsGeostationary(prop.TLE()) {
		return nil, nil
	}

	var passes []*Pass
	step := time.Duration(coarseStepSec) * time.Second

	t := start
	for t.Before(end) {
		el := computeElevationDeg(prop, obs, t)

		if el <= -900 {
			t = t.Add(step)
			continue
		}

		if el >= minElDeg {
			pass, nextT := scanPassAboveThreshold(prop, obs, t, end, minElDeg, step)
			passes = append(passes, pass)
			t = nextT
			continue
		}

		nextT := t.Add(step)
		if nextT.After(end) {
			break
		}

		nextEl := computeElevationDeg(prop, obs, nextT)
		if nextEl >= minElDeg {
			pass, scanT := scanPassFromCrossing(prop, obs, t, nextT, end, minElDeg, step)
			passes = append(passes, pass)
			t = scanT
			continue
		}

		t = nextT
	}

	sort.Slice(passes, func(i, j int) bool {
		return passes[i].AOS < passes[j].AOS
	})

	return passes, nil
}

// validatePredictArgs проверяет аргументы PredictPasses.
func validatePredictArgs(prop *Propagator, obs *Observer, start, end time.Time, minElDeg float64) error {
	if prop == nil {
		return ErrNilPropagator
	}
	if obs == nil {
		return ErrNilObserver
	}
	if end.Before(start) || end.Equal(start) {
		return fmt.Errorf("%w: start=%v, end=%v", ErrInvalidTimeRange, start, end)
	}
	if minElDeg < 0 || minElDeg > 90 {
		return fmt.Errorf("%w: %f", ErrMinElevation, minElDeg)
	}
	return nil
}

// findRoughAOSBackward ищет грубый момент AOS (el < minElDeg) назад от t.
// Ограничение: не дальше 60 минут назад.
func findRoughAOSBackward(
	prop *Propagator,
	obs *Observer,
	t time.Time,
	minElDeg float64,
	step time.Duration,
) time.Time {
	aosRough := t
	backLimit := t.Add(-60 * time.Minute)
	for {
		prev := aosRough.Add(-step)
		if prev.Before(backLimit) {
			return prev
		}
		if computeElevationDeg(prop, obs, prev) < minElDeg {
			return prev
		}
		aosRough = prev
	}
}

// findRoughLOSForward ищет грубый момент LOS (el < minElDeg) вперёд от t.
func findRoughLOSForward(
	prop *Propagator,
	obs *Observer,
	t, end time.Time,
	minElDeg float64,
	step time.Duration,
) time.Time {
	losRough := t
	for {
		next := losRough.Add(step)
		if next.After(end) {
			return end
		}
		if computeElevationDeg(prop, obs, next) < minElDeg {
			return next
		}
		losRough = next
	}
}

// scanPassAboveThreshold обрабатывает случай, когда спутник уже выше порога элевации.
// Ищет AOS назад и LOS вперёд, уточняет бисекцией, строит Pass.
func scanPassAboveThreshold(
	prop *Propagator, obs *Observer,
	t, end time.Time, minElDeg float64, step time.Duration,
) (*Pass, time.Time) {
	aosRough := findRoughAOSBackward(prop, obs, t, minElDeg, step)
	losRough := findRoughLOSForward(prop, obs, t, end, minElDeg, step)

	bisectEnd := aosRough.Add(step)
	if bisectEnd.After(t) {
		bisectEnd = t
	}
	aosExact := refineBisect(prop, obs, aosRough, bisectEnd, minElDeg, true)

	bisectStart := losRough.Add(-step)
	if bisectStart.Before(t) {
		bisectStart = t
	}
	losExact := refineBisect(prop, obs, bisectStart, losRough, minElDeg, false)

	tcaTime, tcaEl, tcaAz := findMaxElevation(prop, obs, aosExact, losExact)
	pass := buildPass(prop, obs, aosExact, tcaTime, losExact, tcaEl, tcaAz)
	return pass, losExact.Add(step)
}

// scanPassFromCrossing обрабатывает переход через порог элевации между tBelow и tAbove.
func scanPassFromCrossing(
	prop *Propagator, obs *Observer,
	tBelow, tAbove, end time.Time, minElDeg float64, step time.Duration,
) (*Pass, time.Time) {
	aosExact := refineBisect(prop, obs, tBelow, tAbove, minElDeg, true)
	losRough := findRoughLOSForward(prop, obs, tAbove, end, minElDeg, step)

	losPrev := losRough.Add(-step)
	if losPrev.Before(aosExact) {
		losPrev = aosExact
	}
	losExact := refineBisect(prop, obs, losPrev, losRough, minElDeg, false)

	tcaTime, tcaEl, tcaAz := findMaxElevation(prop, obs, aosExact, losExact)
	pass := buildPass(prop, obs, aosExact, tcaTime, losExact, tcaEl, tcaAz)
	return pass, losExact.Add(step)
}

// PredictPassesForTLE предсказывает пролёты спутника по TLE.
// Удобный метод, создающий Propagator из TLE.
func PredictPassesForTLE(tle *TLE, obs *Observer, start, end time.Time, minElDeg float64) ([]*Pass, error) {
	if tle == nil {
		return nil, ErrNilTLE
	}

	prop, err := NewPropagator(tle)
	if err != nil {
		return nil, fmt.Errorf("creating propagator for NORAD %d: %w", tle.NoradID, err)
	}

	return PredictPasses(prop, obs, start, end, minElDeg)
}

// PredictAllPasses предсказывает пролёты для всех спутников в TLEStore из указанной группы.
// Возвращает все пролёты, отсортированные по AOS.
func PredictAllPasses(
	store *TLEStore,
	obs *Observer,
	group string,
	start, end time.Time,
	minElDeg float64,
) ([]*Pass, error) {
	if store == nil {
		return nil, ErrNilTLEStore
	}

	if obs == nil {
		return nil, ErrNilObserver
	}

	tles := store.GetByGroup(group)
	if len(tles) == 0 {
		return nil, nil
	}

	passes := predictPassesForTLEs(tles, obs, start, end, minElDeg)
	for _, p := range passes {
		p.Group = group
	}

	return passes, nil
}

// PredictPassesForAll рассчитывает пролёты для ВСЕХ спутников в хранилище.
// Итерирует по группам, заполняет поле Pass.Group именем группы.
// Возвращает пролёты отсортированные по AOS (ближайшие первыми).
func PredictPassesForAll(
	store *TLEStore,
	obs *Observer,
	start, end time.Time,
	minElDeg float64,
) ([]*Pass, error) {
	if store == nil {
		return nil, ErrNilTLEStore
	}

	if obs == nil {
		return nil, ErrNilObserver
	}

	groups := store.Groups()
	if len(groups) == 0 {
		return nil, nil
	}

	var allPasses []*Pass

	for _, group := range groups {
		passes, err := PredictAllPasses(store, obs, group, start, end, minElDeg)
		if err != nil {
			continue
		}
		for _, p := range passes {
			p.Group = group
		}
		allPasses = append(allPasses, passes...)
	}

	// Дедупликация: один спутник может входить в несколько групп (например cubesat и amateur).
	allPasses = deduplicatePassesByNoradAndAOS(allPasses)

	sort.Slice(allPasses, func(i, j int) bool {
		return allPasses[i].AOS < allPasses[j].AOS
	})

	return allPasses, nil
}

// deduplicatePassesByNoradAndAOS оставляет один пролёт на пару (NoradID, AOS).
func deduplicatePassesByNoradAndAOS(passes []*Pass) []*Pass {
	seen := make(map[string]struct{})
	var out []*Pass
	for _, p := range passes {
		key := fmt.Sprintf("%d:%d", p.NoradID, p.AOS)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}

// predictPassesForTLEs — внутренняя функция расчёта пролётов для списка TLE.
func predictPassesForTLEs(
	tles []*TLE,
	obs *Observer,
	start, end time.Time,
	minElDeg float64,
) []*Pass {
	var allPasses []*Pass

	for _, tle := range tles {
		if IsGeostationary(tle) {
			continue
		}

		passes, err := PredictPassesForTLE(tle, obs, start, end, minElDeg)
		if err != nil {
			continue
		}

		allPasses = append(allPasses, passes...)
	}

	sort.Slice(allPasses, func(i, j int) bool {
		return allPasses[i].AOS < allPasses[j].AOS
	})

	return allPasses
}
