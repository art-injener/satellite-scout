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

	// Порог MeanMotion для определения GEO-спутников (пролёты не считаем).
	geoMeanMotionThresholdPass = 0.1
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

		if rising {
			// Ищем AOS: элевация растёт через порог.
			// Если mid выше порога — точка перехода раньше.
			if elMid >= minElDeg {
				t2 = mid
			} else {
				t1 = mid
			}
		} else {
			// Ищем LOS: элевация падает через порог.
			// Если mid выше порога — точка перехода позже.
			if elMid >= minElDeg {
				t1 = mid
			} else {
				t2 = mid
			}
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
// Централизует создание Pass, включая расчёт номера орбиты.
func buildPass(prop *Propagator, obs *Observer, aosExact, tcaTime, losExact time.Time, tcaEl, tcaAz float64) *Pass {
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

	// SkyPath — траектория на небесной сфере для SVG мини-проекции.
	skyPath := computeSkyPath(prop, obs, aosExact, losExact)

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
	if prop == nil {
		return nil, ErrNilPropagator
	}

	if obs == nil {
		return nil, ErrNilObserver
	}

	if end.Before(start) || end.Equal(start) {
		return nil, fmt.Errorf("%w: start=%v, end=%v", ErrInvalidTimeRange, start, end)
	}

	if minElDeg < 0 || minElDeg > 90 {
		return nil, fmt.Errorf("%w: %f", ErrMinElevation, minElDeg)
	}

	// Пропускаем GEO-спутники — они не имеют пролётов.
	if prop.TLE() != nil && IsGeostationary(prop.TLE()) {
		return nil, nil
	}

	var passes []*Pass
	step := time.Duration(coarseStepSec) * time.Second

	t := start
	for t.Before(end) {
		el := computeElevationDeg(prop, obs, t)

		// Ошибка пропагации — пропускаем шаг.
		if el <= -900 {
			t = t.Add(step)
			continue
		}

		if el >= minElDeg {
			// Спутник уже выше порога — ищем начало пролёта (назад).
			aosRough := t
			losRough := t

			// Грубый поиск AOS (назад от t).
			for {
				prev := aosRough.Add(-step)
				if prev.Before(start) {
					aosRough = start
					break
				}
				prevEl := computeElevationDeg(prop, obs, prev)
				if prevEl < minElDeg {
					// AOS между prev и aosRough.
					aosRough = prev
					break
				}
				aosRough = prev
			}

			// Грубый поиск LOS (вперёд от t).
			for {
				next := losRough.Add(step)
				if next.After(end) {
					losRough = end
					break
				}
				nextEl := computeElevationDeg(prop, obs, next)
				if nextEl < minElDeg {
					// LOS между losRough и next.
					losRough = next
					break
				}
				losRough = next
			}

			// Уточняем AOS и LOS бисекцией.
			prevStep := aosRough
			nextStep := aosRough.Add(step)
			if nextStep.After(t) {
				nextStep = t
			}
			aosExact := refineBisect(prop, obs, prevStep, nextStep, minElDeg, true)

			prevStep = losRough.Add(-step)
			if prevStep.Before(t) {
				prevStep = t
			}
			nextStep = losRough
			losExact := refineBisect(prop, obs, prevStep, nextStep, minElDeg, false)

			// Находим TCA (максимальную элевацию).
			tcaTime, tcaEl, tcaAz := findMaxElevation(prop, obs, aosExact, losExact)

			// Формируем Pass (с номером орбиты).
			passes = append(passes, buildPass(prop, obs, aosExact, tcaTime, losExact, tcaEl, tcaAz))

			// Перепрыгиваем за LOS + 1 шаг.
			t = losExact.Add(step)
			continue
		}

		// Элевация ниже порога — ищем переход.
		nextT := t.Add(step)
		if nextT.After(end) {
			break
		}

		nextEl := computeElevationDeg(prop, obs, nextT)
		if nextEl >= minElDeg {
			// Переход через порог — уточняем AOS.
			aosExact := refineBisect(prop, obs, t, nextT, minElDeg, true)

			// Грубый поиск LOS (вперёд от nextT).
			losRough := nextT
			for {
				next := losRough.Add(step)
				if next.After(end) {
					losRough = end
					break
				}
				nextElSearch := computeElevationDeg(prop, obs, next)
				if nextElSearch < minElDeg {
					losRough = next
					break
				}
				losRough = next
			}

			// Уточняем LOS.
			losPrev := losRough.Add(-step)
			if losPrev.Before(aosExact) {
				losPrev = aosExact
			}
			losExact := refineBisect(prop, obs, losPrev, losRough, minElDeg, false)

			// Находим TCA.
			tcaTime, tcaEl, tcaAz := findMaxElevation(prop, obs, aosExact, losExact)

			// Формируем Pass (с номером орбиты).
			passes = append(passes, buildPass(prop, obs, aosExact, tcaTime, losExact, tcaEl, tcaAz))

			// Перепрыгиваем за LOS.
			t = losExact.Add(step)
			continue
		}

		t = nextT
	}

	// Сортировка по AOS.
	sort.Slice(passes, func(i, j int) bool {
		return passes[i].AOS < passes[j].AOS
	})

	return passes, nil
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
		return nil, errors.New("TLE store is nil")
	}

	if obs == nil {
		return nil, ErrNilObserver
	}

	// Получаем TLE для группы.
	tles := store.GetByGroup(group)
	if len(tles) == 0 {
		return nil, nil
	}

	var allPasses []*Pass

	for _, tle := range tles {
		// Пропускаем GEO-спутники.
		if IsGeostationary(tle) {
			continue
		}

		passes, err := PredictPassesForTLE(tle, obs, start, end, minElDeg)
		if err != nil {
			// Логируем ошибку, но продолжаем для остальных спутников.
			continue
		}

		allPasses = append(allPasses, passes...)
	}

	// Сортируем все пролёты по AOS.
	sort.Slice(allPasses, func(i, j int) bool {
		return allPasses[i].AOS < allPasses[j].AOS
	})

	return allPasses, nil
}
