package tracker

import (
	"math"
	"time"
)

// Константы WGS84 эллипсоида.
const (
	// WGS84A — экваториальный радиус Земли (большая полуось), км.
	WGS84A = 6378.137

	// WGS84F — сплюснутость эллипсоида.
	WGS84F = 1.0 / 298.257223563

	// WGS84B — полярный радиус Земли (малая полуось), км.
	WGS84B = WGS84A * (1.0 - WGS84F)

	// WGS84E2 — квадрат первого эксцентриситета.
	WGS84E2 = 2*WGS84F - WGS84F*WGS84F

	// WGS84EP2 — квадрат второго эксцентриситета.
	WGS84EP2 = WGS84E2 / (1.0 - WGS84E2)

	// OmegaEarth — угловая скорость вращения Земли, рад/с.
	OmegaEarth = 7.292115e-5

	// Deg2Rad — коэффициент перевода градусов в радианы.
	Deg2Rad = math.Pi / 180.0

	// Rad2Deg — коэффициент перевода радианов в градусы.
	Rad2Deg = 180.0 / math.Pi
)

// ECEFPosition представляет позицию в системе ECEF (Earth-Centered Earth-Fixed).
// Координаты в километрах.
type ECEFPosition struct {
	X    float64   // X координата, км.
	Y    float64   // Y координата, км.
	Z    float64   // Z координата, км.
	Time time.Time // Время расчёта.
}

// LLA представляет географические координаты.
type LLA struct {
	Lat float64 // Широта в радианах.
	Lon float64 // Долгота в радианах.
	Alt float64 // Высота над эллипсоидом, км.
}

// AER представляет топоцентрические координаты (относительно наблюдателя).
type AER struct {
	Az    float64 // Азимут в радианах (от севера по часовой стрелке).
	El    float64 // Угол места (elevation) в радианах.
	Range float64 // Дальность до объекта, км.
}

// Observer представляет позицию наблюдателя на поверхности Земли.
type Observer struct {
	Lat float64 // Широта в градусах.
	Lon float64 // Долгота в градусах.
	Alt float64 // Высота над уровнем моря, км.
}

// ECIToECEF преобразует координаты из ECI (TEME) в ECEF.
// Выполняет поворот вокруг оси Z на угол GMST.
func ECIToECEF(eci *ECIPosition) *ECEFPosition {
	if eci == nil {
		return nil
	}

	// Получаем GMST (Greenwich Mean Sidereal Time) в радианах.
	gmst := GMST(eci.Time)

	// Поворот вокруг оси Z.
	cosGMST := math.Cos(gmst)
	sinGMST := math.Sin(gmst)

	return &ECEFPosition{
		X:    eci.X*cosGMST + eci.Y*sinGMST,
		Y:    -eci.X*sinGMST + eci.Y*cosGMST,
		Z:    eci.Z,
		Time: eci.Time,
	}
}

// ECEFToECI преобразует координаты из ECEF в ECI (TEME).
// Обратное преобразование к ECIToECEF.
func ECEFToECI(ecef *ECEFPosition) *ECIPosition {
	if ecef == nil {
		return nil
	}

	// Получаем GMST в радианах.
	gmst := GMST(ecef.Time)

	// Обратный поворот вокруг оси Z.
	cosGMST := math.Cos(gmst)
	sinGMST := math.Sin(gmst)

	return &ECIPosition{
		X:    ecef.X*cosGMST - ecef.Y*sinGMST,
		Y:    ecef.X*sinGMST + ecef.Y*cosGMST,
		Z:    ecef.Z,
		Time: ecef.Time,
	}
}

// LLAToECEF преобразует географические координаты в ECEF.
// Широта и долгота в LLA должны быть в радианах.
func LLAToECEF(lla *LLA) *ECEFPosition {
	if lla == nil {
		return nil
	}

	sinLat := math.Sin(lla.Lat)
	cosLat := math.Cos(lla.Lat)
	sinLon := math.Sin(lla.Lon)
	cosLon := math.Cos(lla.Lon)

	// radiusN — радиус кривизны в первом вертикале.
	radiusN := WGS84A / math.Sqrt(1.0-WGS84E2*sinLat*sinLat)

	return &ECEFPosition{
		X: (radiusN + lla.Alt) * cosLat * cosLon,
		Y: (radiusN + lla.Alt) * cosLat * sinLon,
		Z: (radiusN*(1.0-WGS84E2) + lla.Alt) * sinLat,
	}
}

// ECEFToLLA преобразует координаты ECEF в географические (LLA).
// Использует итеративный алгоритм Bowring.
// Возвращает широту и долготу в радианах, высоту в км.
func ECEFToLLA(ecef *ECEFPosition) *LLA {
	if ecef == nil {
		return nil
	}

	x, y, z := ecef.X, ecef.Y, ecef.Z

	// Долгота — простое вычисление.
	lon := math.Atan2(y, x)

	// Расстояние от оси Z.
	p := math.Sqrt(x*x + y*y)

	// Начальное приближение широты (сферическая модель).
	lat := math.Atan2(z, p*(1.0-WGS84E2))

	// Итеративный алгоритм Bowring для уточнения широты.
	const maxIterations = 10
	const tolerance = 1e-12

	for range maxIterations {
		sinLat := math.Sin(lat)
		radiusN := WGS84A / math.Sqrt(1.0-WGS84E2*sinLat*sinLat)

		latNew := math.Atan2(z+WGS84E2*radiusN*sinLat, p)

		if math.Abs(latNew-lat) < tolerance {
			lat = latNew
			break
		}

		lat = latNew
	}

	// Высота.
	sinLat := math.Sin(lat)
	cosLat := math.Cos(lat)
	radiusN := WGS84A / math.Sqrt(1.0-WGS84E2*sinLat*sinLat)

	var alt float64
	if math.Abs(cosLat) > 1e-10 {
		alt = p/cosLat - radiusN
	} else {
		// Вблизи полюсов.
		alt = math.Abs(z)/math.Abs(sinLat) - radiusN*(1.0-WGS84E2)
	}

	return &LLA{
		Lat: lat,
		Lon: lon,
		Alt: alt,
	}
}

// ObserverToECEF преобразует позицию наблюдателя в ECEF.
// Координаты Observer в градусах, результат в километрах.
func ObserverToECEF(obs *Observer) *ECEFPosition {
	if obs == nil {
		return nil
	}

	lla := &LLA{
		Lat: obs.Lat * Deg2Rad,
		Lon: obs.Lon * Deg2Rad,
		Alt: obs.Alt,
	}

	return LLAToECEF(lla)
}

// ECEFToAER вычисляет азимут, угол места и дальность от наблюдателя до объекта.
// satECEF — позиция спутника в ECEF.
// obsECEF — позиция наблюдателя в ECEF.
// obsLLA — географические координаты наблюдателя (широта/долгота в радианах).
// Возвращает AER: азимут и угол места в радианах, дальность в км.
func ECEFToAER(satECEF, obsECEF *ECEFPosition, obsLLA *LLA) *AER {
	if satECEF == nil || obsECEF == nil || obsLLA == nil {
		return nil
	}

	// Вектор от наблюдателя к спутнику в ECEF.
	dx := satECEF.X - obsECEF.X
	dy := satECEF.Y - obsECEF.Y
	dz := satECEF.Z - obsECEF.Z

	// Дальность.
	rng := math.Sqrt(dx*dx + dy*dy + dz*dz)

	// Преобразование в топоцентрическую систему координат (SEZ или ENU).
	// Используем ENU (East-North-Up) для удобства расчёта азимута.
	sinLat := math.Sin(obsLLA.Lat)
	cosLat := math.Cos(obsLLA.Lat)
	sinLon := math.Sin(obsLLA.Lon)
	cosLon := math.Cos(obsLLA.Lon)

	// Преобразование ECEF → ENU.
	// E (East)  = -sinLon*dx + cosLon*dy
	// N (North) = -sinLat*cosLon*dx - sinLat*sinLon*dy + cosLat*dz
	// U (Up)    = cosLat*cosLon*dx + cosLat*sinLon*dy + sinLat*dz
	e := -sinLon*dx + cosLon*dy
	n := -sinLat*cosLon*dx - sinLat*sinLon*dy + cosLat*dz
	u := cosLat*cosLon*dx + cosLat*sinLon*dy + sinLat*dz

	// Угол места (elevation).
	el := math.Asin(u / rng)

	// Азимут (от севера по часовой стрелке).
	az := math.Atan2(e, n)
	if az < 0 {
		az += 2 * math.Pi
	}

	return &AER{
		Az:    az,
		El:    el,
		Range: rng,
	}
}

// NewLLAFromDegrees создаёт LLA из координат в градусах.
func NewLLAFromDegrees(latDeg, lonDeg, altKm float64) *LLA {
	return &LLA{
		Lat: latDeg * Deg2Rad,
		Lon: lonDeg * Deg2Rad,
		Alt: altKm,
	}
}

// LatDeg возвращает широту в градусах.
func (lla *LLA) LatDeg() float64 {
	return lla.Lat * Rad2Deg
}

// LonDeg возвращает долготу в градусах.
func (lla *LLA) LonDeg() float64 {
	return lla.Lon * Rad2Deg
}

// NewObserver создаёт Observer с координатами в градусах.
func NewObserver(latDeg, lonDeg, altKm float64) *Observer {
	return &Observer{
		Lat: latDeg,
		Lon: lonDeg,
		Alt: altKm,
	}
}

// AzDeg возвращает азимут в градусах.
func (aer *AER) AzDeg() float64 {
	return aer.Az * Rad2Deg
}

// ElDeg возвращает угол места в градусах.
func (aer *AER) ElDeg() float64 {
	return aer.El * Rad2Deg
}

// ToLLA преобразует Observer в LLA (радианы).
func (obs *Observer) ToLLA() *LLA {
	if obs == nil {
		return nil
	}
	return &LLA{
		Lat: obs.Lat * Deg2Rad,
		Lon: obs.Lon * Deg2Rad,
		Alt: obs.Alt,
	}
}

// GetAER вычисляет AER от наблюдателя до спутника по его ECI позиции.
// Удобный метод, объединяющий ECIToECEF и ECEFToAER.
func (obs *Observer) GetAER(eci *ECIPosition) *AER {
	if obs == nil || eci == nil {
		return nil
	}

	satECEF := ECIToECEF(eci)
	obsECEF := ObserverToECEF(obs)
	obsLLA := obs.ToLLA()

	return ECEFToAER(satECEF, obsECEF, obsLLA)
}
