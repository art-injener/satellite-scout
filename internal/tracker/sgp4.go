package tracker

import (
	"errors"
	"fmt"
	"math"
	"time"

	satellite "github.com/joshuaferrara/go-satellite"
)

// Ошибки SGP4 пропагации.
var (
	ErrInvalidTLEForPropagation = errors.New("invalid TLE for SGP4 propagation")
	ErrPropagationFailed        = errors.New("SGP4 propagation failed")
	ErrNilTLE                   = errors.New("TLE is nil")
	ErrInvalidStep              = errors.New("step must be positive")
)

// GravityModel определяет модель гравитации для SGP4.
type GravityModel int

const (
	// GravityWGS72 — модель WGS-72 (стандарт для TLE).
	GravityWGS72 GravityModel = iota
	// GravityWGS84 — модель WGS-84 (более точная).
	GravityWGS84
)

// ECIPosition представляет позицию и скорость спутника в системе ECI (TEME).
// Координаты в километрах, скорости в км/с.
type ECIPosition struct {
	// Позиция в ECI (км).
	X float64
	Y float64
	Z float64

	// Скорость в ECI (км/с).
	Vx float64
	Vy float64
	Vz float64

	// Время расчёта.
	Time time.Time
}

// Propagator выполняет расчёт положения спутника по алгоритму SGP4.
// Propagator (пропагатор) — стандартный термин в орбитальной механике,
// означающий модуль для предсказания положения спутника на заданный момент времени.
// Является wrapper'ом над библиотекой go-satellite.
type Propagator struct {
	tle       *TLE                // Исходный TLE (наш формат).
	satellite satellite.Satellite // Внутренняя структура go-satellite.
	gravity   GravityModel        // Модель гравитации.
}

// NewPropagator создаёт новый Propagator из TLE.
// По умолчанию использует модель гравитации WGS84.
func NewPropagator(tle *TLE) (*Propagator, error) {
	return NewPropagatorWithGravity(tle, GravityWGS84)
}

// NewPropagatorWithGravity создаёт Propagator с указанной моделью гравитации.
func NewPropagatorWithGravity(tle *TLE, gravity GravityModel) (*Propagator, error) {
	if tle == nil {
		return nil, ErrNilTLE
	}

	// Проверяем наличие оригинальных строк TLE.
	if tle.Line1 == "" || tle.Line2 == "" {
		return nil, fmt.Errorf("%w: missing Line1 or Line2", ErrInvalidTLEForPropagation)
	}

	// Выбираем модель гравитации.
	var gravConst satellite.Gravity

	switch gravity {
	case GravityWGS72:
		gravConst = satellite.GravityWGS72
	case GravityWGS84:
		gravConst = satellite.GravityWGS84
	default:
		gravConst = satellite.GravityWGS84
	}

	// Инициализируем спутник через go-satellite.
	sat := satellite.TLEToSat(tle.Line1, tle.Line2, gravConst)

	return &Propagator{
		tle:       tle,
		satellite: sat,
		gravity:   gravity,
	}, nil
}

// Propagate рассчитывает положение спутника на указанное время.
// Возвращает позицию и скорость в системе координат ECI (TEME).
func (p *Propagator) Propagate(t time.Time) (*ECIPosition, error) {
	if p == nil {
		return nil, ErrNilTLE
	}

	// Извлекаем компоненты времени.
	year, month, day := t.Date()
	hour, minute, sec := t.Clock()

	// Вызываем SGP4 пропагатор.
	position, velocity := satellite.Propagate(
		p.satellite,
		year, int(month), day,
		hour, minute, sec,
	)

	// Проверяем результат на NaN (признак ошибки пропагации).
	if isNaN(position.X) || isNaN(position.Y) || isNaN(position.Z) {
		return nil, fmt.Errorf(
			"%w: position contains NaN (possible orbital decay or invalid TLE)",
			ErrPropagationFailed,
		)
	}

	return &ECIPosition{
		X:    position.X,
		Y:    position.Y,
		Z:    position.Z,
		Vx:   velocity.X,
		Vy:   velocity.Y,
		Vz:   velocity.Z,
		Time: t,
	}, nil
}

// PropagateRange рассчитывает положения спутника на интервале времени.
// step — шаг между точками расчёта.
func (p *Propagator) PropagateRange(start, end time.Time, step time.Duration) ([]*ECIPosition, error) {
	if p == nil {
		return nil, ErrNilTLE
	}

	if step <= 0 {
		return nil, fmt.Errorf("%w: %v", ErrInvalidStep, step)
	}

	if end.Before(start) {
		start, end = end, start
	}

	var positions []*ECIPosition

	for t := start; !t.After(end); t = t.Add(step) {
		pos, err := p.Propagate(t)
		if err != nil {
			// При ошибке пропагации прекращаем.
			return positions, fmt.Errorf("propagation at %v: %w", t, err)
		}

		positions = append(positions, pos)
	}

	return positions, nil
}

// TLE возвращает исходный TLE.
func (p *Propagator) TLE() *TLE {
	if p == nil {
		return nil
	}

	return p.tle
}

// GravityModel возвращает используемую модель гравитации.
func (p *Propagator) GravityModel() GravityModel {
	if p == nil {
		return GravityWGS84
	}

	return p.gravity
}

// GMST рассчитывает Greenwich Mean Sidereal Time для указанного времени.
// Используется для преобразования ECI -> ECEF.
func GMST(t time.Time) float64 {
	year, month, day := t.Date()
	hour, minute, sec := t.Clock()

	return satellite.GSTimeFromDate(year, int(month), day, hour, minute, sec)
}

// JulianDay рассчитывает юлианскую дату для указанного времени.
func JulianDay(t time.Time) float64 {
	year, month, day := t.Date()
	hour, minute, sec := t.Clock()

	return satellite.JDay(year, int(month), day, hour, minute, sec)
}

// isNaN проверяет, является ли значение NaN.
func isNaN(f float64) bool {
	return f != f // NaN != NaN по стандарту IEEE 754.
}

// String возвращает строковое представление ECIPosition.
func (pos *ECIPosition) String() string {
	return fmt.Sprintf("ECI[%.3f, %.3f, %.3f km] V[%.6f, %.6f, %.6f km/s] @ %s",
		pos.X, pos.Y, pos.Z,
		pos.Vx, pos.Vy, pos.Vz,
		pos.Time.UTC().Format(time.RFC3339),
	)
}

// Magnitude возвращает расстояние от центра Земли в километрах.
func (pos *ECIPosition) Magnitude() float64 {
	return math.Sqrt(pos.X*pos.X + pos.Y*pos.Y + pos.Z*pos.Z)
}

// Altitude возвращает приблизительную высоту над поверхностью Земли в километрах.
// Использует средний радиус Земли (сферическая модель).
func (pos *ECIPosition) Altitude() float64 {
	const earthRadiusMean = 6371.0 // км (средний радиус).

	return pos.Magnitude() - earthRadiusMean
}

// Speed возвращает скорость спутника в км/с.
func (pos *ECIPosition) Speed() float64 {
	return math.Sqrt(pos.Vx*pos.Vx + pos.Vy*pos.Vy + pos.Vz*pos.Vz)
}
