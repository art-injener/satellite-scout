// Package tracker реализует парсинг TLE и расчёт позиций спутников.
package tracker

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Ошибки парсинга TLE
var (
	ErrInvalidTLEFormat  = errors.New("invalid TLE format")
	ErrInvalidChecksum   = errors.New("invalid TLE checksum")
	ErrInvalidLineNumber = errors.New("invalid TLE line number")
	ErrLineTooShort      = errors.New("TLE line too short")
	ErrNoradIDMismatch   = errors.New("NORAD ID mismatch between lines")
	ErrInvalidAlpha5     = errors.New("invalid Alpha-5 NORAD ID format")
)

// alpha5Map маппинг букв Alpha-5 формата на числовые префиксы.
// Alpha-5 используется для NORAD ID > 99999 (например, Starlink).
// Буквы I и O не используются (путаются с 1 и 0).
// A=10, B=11, ..., H=17, J=18, ..., N=22, P=23, ..., Z=33
var alpha5Map = map[byte]int{
	'A': 10, 'B': 11, 'C': 12, 'D': 13, 'E': 14, 'F': 15, 'G': 16, 'H': 17,
	'J': 18, 'K': 19, 'L': 20, 'M': 21, 'N': 22,
	'P': 23, 'Q': 24, 'R': 25, 'S': 26, 'T': 27, 'U': 28, 'V': 29, 'W': 30,
	'X': 31, 'Y': 32, 'Z': 33,
}

// TLE представляет Two-Line Element набор орбитальных данных спутника.
// Формат описан: https://celestrak.org/NORAD/documentation/tle-fmt.php
type TLE struct {
	Name           string    // Имя спутника (из Line 0, если есть)
	NoradID        int       // NORAD каталожный номер (5 цифр)
	Classification string    // Классификация: U=Unclassified, C=Classified, S=Secret
	IntlDesignator string    // Международное обозначение (COSPAR ID): YYnnnAAA
	Epoch          time.Time // Эпоха элементов (UTC)
	MeanMotionDot  float64   // Первая производная mean motion (оборотов/день²)
	MeanMotionDot2 float64   // Вторая производная mean motion (оборотов/день³)
	Bstar          float64   // Баллистический коэффициент B* (1/земных радиусов)
	EphemerisType  int       // Тип эфемерид (обычно 0)
	ElementSetNo   int       // Номер набора элементов
	Inclination    float64   // Наклонение орбиты (градусы)
	RAAN           float64   // Долгота восходящего узла (градусы)
	Eccentricity   float64   // Эксцентриситет (безразмерный, 0-1)
	ArgOfPerigee   float64   // Аргумент перигея (градусы)
	MeanAnomaly    float64   // Средняя аномалия (градусы)
	MeanMotion     float64   // Среднее движение (оборотов/день)
	RevNumber      int       // Номер витка на эпоху
	Line1          string    // Оригинальная Line 1
	Line2          string    // Оригинальная Line 2
}

// Константы формата TLE
const (
	idxLine0 = 0 // Имя спутника (опционально)
	idxLine1 = 1 // Line 1
	idxLine2 = 2 // Line 2

	TLELineLength = 69 // Длина строки TLE (включая checksum)
)

// ParseTLE парсит TLE из массива строк.
// Поддерживает 2-line формат (только Line1, Line2) и 3-line формат (Name, Line1, Line2).
func ParseTLE(lines []string) (*TLE, error) {
	if len(lines) < 2 {
		return nil, fmt.Errorf("%w: need at least 2 lines, got %d", ErrInvalidTLEFormat, len(lines))
	}

	var name, line1, line2 string

	// Определяем формат по первому символу первой строки
	firstLine := strings.TrimSpace(lines[idxLine0])
	if len(firstLine) == 0 {
		return nil, fmt.Errorf("%w: first line is empty", ErrInvalidTLEFormat)
	}

	switch firstLine[0] {
	case '1':
		// 2-line формат: Line1, Line2
		line1 = firstLine
		line2 = strings.TrimSpace(lines[idxLine1])

	case '2':
		// Некорректный порядок строк
		return nil, fmt.Errorf("%w: expected Line1, got Line2", ErrInvalidTLEFormat)

	default:
		// 3-line формат: Name, Line1, Line2
		if len(lines) < 3 {
			return nil, fmt.Errorf("%w: 3-line format requires 3 lines, got %d", ErrInvalidTLEFormat, len(lines))
		}
		name = firstLine
		line1 = strings.TrimSpace(lines[idxLine1])
		line2 = strings.TrimSpace(lines[idxLine2])
	}

	return parseTLELines(name, line1, line2)
}

// ParseTLEBatch парсит несколько TLE из одной строки.
// TLE разделяются пустыми строками или идут подряд (3-line формат).
func ParseTLEBatch(data string) ([]*TLE, error) {
	lines := strings.Split(data, "\n")
	var tles []*TLE
	var currentLines []string

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		// Пустая строка — возможный разделитель
		if trimmed == "" {
			if len(currentLines) >= 2 {
				tle, err := ParseTLE(currentLines)
				if err != nil {
					return nil, fmt.Errorf("parsing TLE: %w", err)
				}
				tles = append(tles, tle)
				currentLines = nil
			}
			continue
		}

		currentLines = append(currentLines, trimmed)

		// Проверяем, готов ли TLE к парсингу
		if tle := tryParseTLE(currentLines); tle != nil {
			parsed, err := ParseTLE(currentLines)
			if err != nil {
				return nil, fmt.Errorf("parsing TLE: %w", err)
			}
			tles = append(tles, parsed)
			currentLines = nil
		}
	}

	// Обработка последнего TLE
	if len(currentLines) >= 2 {
		tle, err := ParseTLE(currentLines)
		if err != nil {
			return nil, fmt.Errorf("parsing TLE: %w", err)
		}
		tles = append(tles, tle)
	}

	return tles, nil
}

// tryParseTLE проверяет, можно ли распарсить накопленные строки как TLE.
// Возвращает не-nil если строки образуют валидный TLE.
func tryParseTLE(lines []string) []string {
	n := len(lines)
	if n < 2 {
		return nil
	}

	switch n {
	case 2:
		// 2-line формат: Line1 + Line2
		if lines[0][0] == '1' && lines[1][0] == '2' {
			return lines
		}
	case 3:
		// 3-line формат: Name + Line1 + Line2
		if lines[0][0] != '1' && lines[0][0] != '2' {
			return lines
		}
	}

	return nil
}

// parseTLELines выполняет парсинг Line1 и Line2.
func parseTLELines(name, line1, line2 string) (*TLE, error) {
	// Проверка минимальной длины строк
	if len(line1) < TLELineLength {
		return nil, fmt.Errorf("%w: Line1 length %d, need %d", ErrLineTooShort, len(line1), TLELineLength)
	}
	if len(line2) < TLELineLength {
		return nil, fmt.Errorf("%w: Line2 length %d, need %d", ErrLineTooShort, len(line2), TLELineLength)
	}

	// Проверка номеров строк
	if line1[0] != '1' {
		return nil, fmt.Errorf("%w: Line1 starts with %c, expected 1", ErrInvalidLineNumber, line1[0])
	}
	if line2[0] != '2' {
		return nil, fmt.Errorf("%w: Line2 starts with %c, expected 2", ErrInvalidLineNumber, line2[0])
	}

	// Проверка контрольных сумм
	if !validateChecksum(line1) {
		return nil, fmt.Errorf("%w: Line1", ErrInvalidChecksum)
	}
	if !validateChecksum(line2) {
		return nil, fmt.Errorf("%w: Line2", ErrInvalidChecksum)
	}

	tle := &TLE{
		Name:  name,
		Line1: line1,
		Line2: line2,
	}

	// Парсинг Line 1
	if err := parseLine1(tle, line1); err != nil {
		return nil, fmt.Errorf("parsing Line1: %w", err)
	}

	// Парсинг Line 2
	if err := parseLine2(tle, line2); err != nil {
		return nil, fmt.Errorf("parsing Line2: %w", err)
	}

	// Проверка совпадения NORAD ID между строками (с поддержкой Alpha-5)
	noradID2, err := parseNoradID(strings.TrimSpace(line2[2:7]))
	if err != nil {
		return nil, fmt.Errorf("parsing NORAD ID from Line2: %w", err)
	}
	if tle.NoradID != noradID2 {
		return nil, fmt.Errorf("%w: Line1=%d, Line2=%d", ErrNoradIDMismatch, tle.NoradID, noradID2)
	}

	return tle, nil
}

// parseLine1 извлекает данные из Line 1.
// Формат Line 1:
//
//	Col  1      Line Number (1)
//	Col  3-7    Satellite Number (NORAD ID, поддерживает Alpha-5)
//	Col  8      Classification (U/C/S)
//	Col 10-17   International Designator
//	Col 19-32   Epoch (YY + DDD.DDDDDDDD)
//	Col 34-43   First Derivative of Mean Motion
//	Col 45-52   Second Derivative of Mean Motion
//	Col 54-61   BSTAR drag term
//	Col 63      Ephemeris Type
//	Col 65-68   Element Set Number
//	Col 69      Checksum
func parseLine1(tle *TLE, line string) error {
	var err error

	// NORAD ID (cols 3-7) с поддержкой Alpha-5 формата
	noradStr := strings.TrimSpace(line[2:7])
	tle.NoradID, err = parseNoradID(noradStr)
	if err != nil {
		return fmt.Errorf("NORAD ID: %w", err)
	}

	// Classification (col 8)
	tle.Classification = string(line[7])

	// International Designator (cols 10-17)
	tle.IntlDesignator = strings.TrimSpace(line[9:17])

	// Epoch (cols 19-32): YYDDD.DDDDDDDD
	epochStr := strings.TrimSpace(line[18:32])
	tle.Epoch, err = parseEpoch(epochStr)
	if err != nil {
		return fmt.Errorf("epoch: %w", err)
	}

	// Mean Motion Dot (cols 34-43): включая знак
	meanMotionDotStr := strings.TrimSpace(line[33:43])
	tle.MeanMotionDot, err = strconv.ParseFloat(meanMotionDotStr, 64)
	if err != nil {
		return fmt.Errorf("mean motion dot: %w", err)
	}

	// Mean Motion Dot2 (cols 45-52): научная нотация TLE
	meanMotionDot2Str := strings.TrimSpace(line[44:52])
	tle.MeanMotionDot2 = parseExponent(meanMotionDot2Str)

	// BSTAR (cols 54-61): научная нотация TLE
	bstarStr := strings.TrimSpace(line[53:61])
	tle.Bstar = parseExponent(bstarStr)

	// Ephemeris Type (col 63)
	ephTypeStr := strings.TrimSpace(line[62:63])
	if ephTypeStr != "" && ephTypeStr != " " {
		tle.EphemerisType, _ = strconv.Atoi(ephTypeStr)
	}

	// Element Set Number (cols 65-68)
	elemSetStr := strings.TrimSpace(line[64:68])
	if elemSetStr != "" {
		tle.ElementSetNo, _ = strconv.Atoi(elemSetStr)
	}

	return nil
}

// parseLine2 извлекает данные из Line 2.
// Формат Line 2:
//
//	Col  1      Line Number (2)
//	Col  3-7    Satellite Number (NORAD ID)
//	Col  9-16   Inclination (degrees)
//	Col 18-25   RAAN (degrees)
//	Col 27-33   Eccentricity (decimal point assumed)
//	Col 35-42   Argument of Perigee (degrees)
//	Col 44-51   Mean Anomaly (degrees)
//	Col 53-63   Mean Motion (revs/day)
//	Col 64-68   Revolution Number at Epoch
//	Col 69      Checksum
func parseLine2(tle *TLE, line string) error {
	var err error

	// Inclination (cols 9-16)
	inclStr := strings.TrimSpace(line[8:16])
	tle.Inclination, err = strconv.ParseFloat(inclStr, 64)
	if err != nil {
		return fmt.Errorf("inclination: %w", err)
	}

	// RAAN (cols 18-25)
	raanStr := strings.TrimSpace(line[17:25])
	tle.RAAN, err = strconv.ParseFloat(raanStr, 64)
	if err != nil {
		return fmt.Errorf("RAAN: %w", err)
	}

	// Eccentricity (cols 27-33): без десятичной точки, подразумевается 0.
	eccStr := strings.TrimSpace(line[26:33])
	eccInt, err := strconv.ParseFloat("0."+eccStr, 64)
	if err != nil {
		return fmt.Errorf("eccentricity: %w", err)
	}
	tle.Eccentricity = eccInt

	// Argument of Perigee (cols 35-42)
	argPeriStr := strings.TrimSpace(line[34:42])
	tle.ArgOfPerigee, err = strconv.ParseFloat(argPeriStr, 64)
	if err != nil {
		return fmt.Errorf("argument of perigee: %w", err)
	}

	// Mean Anomaly (cols 44-51)
	meanAnomStr := strings.TrimSpace(line[43:51])
	tle.MeanAnomaly, err = strconv.ParseFloat(meanAnomStr, 64)
	if err != nil {
		return fmt.Errorf("mean anomaly: %w", err)
	}

	// Mean Motion (cols 53-63)
	meanMotionStr := strings.TrimSpace(line[52:63])
	tle.MeanMotion, err = strconv.ParseFloat(meanMotionStr, 64)
	if err != nil {
		return fmt.Errorf("mean motion: %w", err)
	}

	// Revolution Number (cols 64-68)
	revNumStr := strings.TrimSpace(line[63:68])
	if revNumStr != "" {
		tle.RevNumber, _ = strconv.Atoi(revNumStr)
	}

	return nil
}

// validateChecksum проверяет контрольную сумму строки TLE по алгоритму Modulo-10.
// Алгоритм: сумма всех цифр + 1 за каждый минус, mod 10 = последняя цифра.
func validateChecksum(line string) bool {
	if len(line) < TLELineLength {
		return false
	}

	checksumIdx := TLELineLength - 1
	calculated := calculateChecksum(line[:checksumIdx])
	expected := int(line[checksumIdx] - '0')

	return calculated == expected
}

// calculateChecksum вычисляет контрольную сумму TLE по алгоритму Modulo-10.
func calculateChecksum(line string) int {
	sum := 0
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c >= '0' && c <= '9':
			sum += int(c - '0')
		case c == '-':
			sum += 1
			// Буквы, пробелы, точки и другие символы не учитываются
		}
	}
	return sum % 10
}

// parseNoradID парсит NORAD ID с поддержкой Alpha-5 формата.
// Стандартный формат: 5 цифр (00001-99999)
// Alpha-5 формат: буква + 4 цифры (A0000-Z9999 = 100000-339999)
// Используется для спутников с NORAD ID > 99999 (Starlink и др.)
func parseNoradID(s string) (int, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("%w: empty string", ErrInvalidAlpha5)
	}

	firstChar := s[0]

	// Проверяем, является ли первый символ буквой (Alpha-5)
	if firstChar >= 'A' && firstChar <= 'Z' {
		prefix, ok := alpha5Map[firstChar]
		if !ok {
			return 0, fmt.Errorf("%w: invalid letter %c (I and O not allowed)", ErrInvalidAlpha5, firstChar)
		}

		// Оставшиеся 4 цифры
		if len(s) < 5 {
			return 0, fmt.Errorf("%w: too short", ErrInvalidAlpha5)
		}

		rest, err := strconv.Atoi(s[1:5])
		if err != nil {
			return 0, fmt.Errorf("%w: %v", ErrInvalidAlpha5, err)
		}

		// Alpha-5: prefix * 10000 + rest
		// A0000 = 10 * 10000 + 0 = 100000
		// Z9999 = 33 * 10000 + 9999 = 339999
		return prefix*10000 + rest, nil
	}

	// Стандартный числовой формат
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid NORAD ID: %w", err)
	}

	return id, nil
}

// parseExponent парсит научную нотацию TLE вида "12345-6" или "-12345-6".
// Формат: [знак]NNNNN[+-]E, означает ±0.NNNNN × 10^(±E)
func parseExponent(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "00000-0" || s == "00000+0" {
		return 0.0
	}

	// Определяем знак мантиссы
	sign := 1.0
	switch s[0] {
	case '-':
		sign = -1.0
		s = s[1:]
	case '+':
		s = s[1:]
	}

	// Ищем позицию экспоненты (последний + или -)
	expPos := -1
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '+' || s[i] == '-' {
			expPos = i
			break
		}
	}

	if expPos == -1 {
		// Нет экспоненты, просто число
		val, _ := strconv.ParseFloat("0."+s, 64)
		return sign * val
	}

	// Мантисса и экспонента
	mantissaStr := s[:expPos]
	expStr := s[expPos:]

	mantissa, _ := strconv.ParseFloat("0."+mantissaStr, 64)
	exp, _ := strconv.Atoi(expStr)

	return sign * mantissa * math.Pow(10, float64(exp))
}

// parseEpoch парсит эпоху TLE из формата YYDDD.DDDDDDDD.
// YY: год (00-56 = 2000-2056, 57-99 = 1957-1999)
// DDD.DDDDDDDD: день года с дробной частью
func parseEpoch(epochStr string) (time.Time, error) {
	if len(epochStr) < 7 {
		return time.Time{}, fmt.Errorf("epoch string too short: %s", epochStr)
	}

	// Парсим год
	yearStr := epochStr[:2]
	year, err := strconv.Atoi(yearStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing year: %w", err)
	}

	// Преобразуем 2-значный год в 4-значный
	if year >= 57 {
		year += 1900
	} else {
		year += 2000
	}

	// Парсим день года с дробной частью
	dayStr := epochStr[2:]
	dayOfYear, err := strconv.ParseFloat(dayStr, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing day of year: %w", err)
	}

	// Создаём время: 1 января года + (dayOfYear - 1) дней
	// dayOfYear=1.0 означает начало 1 января
	// dayOfYear=1.5 означает полдень 1 января
	baseTime := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	duration := time.Duration((dayOfYear - 1) * 24 * float64(time.Hour))

	return baseTime.Add(duration), nil
}

// OrbitalPeriod возвращает орбитальный период в минутах.
func (tle *TLE) OrbitalPeriod() float64 {
	if tle.MeanMotion == 0 {
		return 0
	}
	return 1440.0 / tle.MeanMotion // 1440 минут в сутках
}

// SemiMajorAxis возвращает большую полуось орбиты в километрах.
// Использует формулу: a = (μ / n²)^(1/3)
// где μ = 398600.4418 км³/с² (гравитационный параметр Земли)
//
//	n = mean motion в рад/с
func (tle *TLE) SemiMajorAxis() float64 {
	const mu = 398600.4418 // км³/с²

	// Преобразуем mean motion из оборотов/день в рад/с
	n := tle.MeanMotion * 2 * math.Pi / 86400.0

	if n == 0 {
		return 0
	}

	return math.Pow(mu/(n*n), 1.0/3.0)
}

// Apogee возвращает высоту апогея в километрах над поверхностью Земли.
func (tle *TLE) Apogee() float64 {
	const earthRadius = 6378.137 // км
	a := tle.SemiMajorAxis()
	return a*(1+tle.Eccentricity) - earthRadius
}

// Perigee возвращает высоту перигея в километрах над поверхностью Земли.
func (tle *TLE) Perigee() float64 {
	const earthRadius = 6378.137 // км
	a := tle.SemiMajorAxis()
	return a*(1-tle.Eccentricity) - earthRadius
}

// Age возвращает возраст TLE (время с эпохи).
func (tle *TLE) Age() time.Duration {
	return time.Since(tle.Epoch)
}

// IsStale возвращает true если TLE старше указанного количества дней.
func (tle *TLE) IsStale(maxAgeDays float64) bool {
	ageDays := tle.Age().Hours() / 24
	return ageDays > maxAgeDays
}

// String возвращает TLE в 3-line формате.
func (tle *TLE) String() string {
	if tle.Name != "" {
		return fmt.Sprintf("%s\n%s\n%s", tle.Name, tle.Line1, tle.Line2)
	}
	return fmt.Sprintf("%s\n%s", tle.Line1, tle.Line2)
}
