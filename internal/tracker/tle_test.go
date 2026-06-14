package tracker

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// makeTLELine добавляет корректную контрольную сумму к строке TLE (68 символов без checksum).
func makeTLELine(line68 string) string {
	if len(line68) != 68 {
		panic(fmt.Sprintf("line must be 68 chars, got %d", len(line68)))
	}
	checksum := calculateChecksum(line68)

	return line68 + strconv.Itoa(checksum)
}

// Эталонные TLE для тестов (с автоматически рассчитанными контрольными суммами).
var (
	// ISS (ZARYA) - 3-line формат.
	issLine1 = makeTLELine("1 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  999")
	issLine2 = makeTLELine("2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340")
	issTLE   = "ISS (ZARYA)\n" + issLine1 + "\n" + issLine2

	// Hubble Space Telescope - 2-line формат.
	hstLine1 = makeTLELine("1 20580U 90037B   24001.50000000  .00001234  00000-0  56789-4 0  999")
	hstLine2 = makeTLELine("2 20580  28.4700 120.3456 0002567  45.1234 315.0000 15.0987654312345")
	hstTLE   = hstLine1 + "\n" + hstLine2

	// Meteor-M2 - 3-line формат.
	meteorLine1 = makeTLELine("1 40069U 14037A   24001.50000000  .00000123  00000-0  12345-4 0  999")
	meteorLine2 = makeTLELine("2 40069  98.5200  45.6789 0001234 123.4567 236.7890 14.2098765432109")
	meteorTLE   = "METEOR-M2\n" + meteorLine1 + "\n" + meteorLine2
)

// TestValidateChecksum проверяет алгоритм Modulo-10.
func TestValidateChecksum(t *testing.T) {
	// Строки с минусами для тестирования
	lineWithMinus68 := "1 25544U 98067A   24001.50000000 -.00016717  00000-0 -10270-3 0  999"
	lineWithMinusValid := makeTLELine(lineWithMinus68)

	tests := []struct {
		name  string
		line  string
		valid bool
	}{
		{
			name:  "ISS Line1 valid",
			line:  issLine1,
			valid: true,
		},
		{
			name:  "ISS Line2 valid",
			line:  issLine2,
			valid: true,
		},
		{
			name:  "Invalid checksum",
			line:  issLine1[:68] + "0", // заменяем checksum на неверный
			valid: false,
		},
		{
			name:  "Line with minus signs",
			line:  lineWithMinusValid,
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateChecksum(tt.line)
			if got != tt.valid {
				t.Errorf("validateChecksum() = %v, want %v", got, tt.valid)
			}
		})
	}
}

// TestParseTLE_ThreeLine проверяет парсинг 3-line TLE (с названием).
func TestParseTLE_ThreeLine(t *testing.T) {
	lines := strings.Split(issTLE, "\n")

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	// Проверка имени
	if tle.Name != "ISS (ZARYA)" {
		t.Errorf("Name = %q, want %q", tle.Name, "ISS (ZARYA)")
	}

	// Проверка NORAD ID
	if tle.NoradID != 25544 {
		t.Errorf("NoradID = %d, want %d", tle.NoradID, 25544)
	}

	// Проверка классификации
	if tle.Classification != "U" {
		t.Errorf("Classification = %q, want %q", tle.Classification, "U")
	}

	// Проверка международного обозначения
	if tle.IntlDesignator != "98067A" {
		t.Errorf("IntlDesignator = %q, want %q", tle.IntlDesignator, "98067A")
	}

	// Проверка наклонения
	if math.Abs(tle.Inclination-51.64) > 0.0001 {
		t.Errorf("Inclination = %f, want %f", tle.Inclination, 51.64)
	}

	// Проверка RAAN
	if math.Abs(tle.RAAN-247.4627) > 0.0001 {
		t.Errorf("RAAN = %f, want %f", tle.RAAN, 247.4627)
	}

	// Проверка эксцентриситета (0006703 = 0.0006703)
	if math.Abs(tle.Eccentricity-0.0006703) > 0.0000001 {
		t.Errorf("Eccentricity = %f, want %f", tle.Eccentricity, 0.0006703)
	}

	// Проверка среднего движения (позиции 52:63 = "15.49815571")
	if math.Abs(tle.MeanMotion-15.49815571) > 0.00000001 {
		t.Errorf("MeanMotion = %.10f, want %.10f", tle.MeanMotion, 15.49815571)
	}

	// Проверка номера витка (последние 5 цифр из 15.4981557142340 = 42340)
	if tle.RevNumber != 42340 {
		t.Errorf("RevNumber = %d, want %d", tle.RevNumber, 42340)
	}
}

// TestParseTLE_TwoLine проверяет парсинг 2-line TLE (без названия).
func TestParseTLE_TwoLine(t *testing.T) {
	lines := strings.Split(hstTLE, "\n")

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	// Имя должно быть пустым или дефолтным
	if tle.Name != "" && tle.Name != "UNKNOWN" {
		t.Errorf("Name = %q, want empty or UNKNOWN", tle.Name)
	}

	// Проверка NORAD ID
	if tle.NoradID != 20580 {
		t.Errorf("NoradID = %d, want %d", tle.NoradID, 20580)
	}

	// Проверка наклонения
	if math.Abs(tle.Inclination-28.47) > 0.0001 {
		t.Errorf("Inclination = %f, want %f", tle.Inclination, 28.47)
	}
}

// TestParseTLE_Epoch проверяет корректность парсинга эпохи.
func TestParseTLE_Epoch(t *testing.T) {
	lines := strings.Split(issTLE, "\n")

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	// Эпоха: 24001.50000000 = 2024, день 1.5 = 1 января 2024, 12:00 UTC
	expectedYear := 2024
	expectedMonth := time.January
	expectedDay := 1
	expectedHour := 12

	if tle.Epoch.Year() != expectedYear {
		t.Errorf("Epoch.Year() = %d, want %d", tle.Epoch.Year(), expectedYear)
	}
	if tle.Epoch.Month() != expectedMonth {
		t.Errorf("Epoch.Month() = %v, want %v", tle.Epoch.Month(), expectedMonth)
	}
	if tle.Epoch.Day() != expectedDay {
		t.Errorf("Epoch.Day() = %d, want %d", tle.Epoch.Day(), expectedDay)
	}
	if tle.Epoch.Hour() != expectedHour {
		t.Errorf("Epoch.Hour() = %d, want %d", tle.Epoch.Hour(), expectedHour)
	}
}

// TestParseTLE_InvalidChecksum проверяет отклонение TLE с неверной контрольной суммой.
func TestParseTLE_InvalidChecksum(t *testing.T) {
	// Создаём TLE с неверной контрольной суммой (заменяем последнюю цифру)
	invalidLine1 := issLine1[:68] + "0" // неверный checksum
	invalidTLE := "ISS (ZARYA)\n" + invalidLine1 + "\n" + issLine2

	lines := strings.Split(invalidTLE, "\n")

	_, err := ParseTLE(lines)
	if err == nil {
		t.Error("ParseTLE() expected error for invalid checksum, got nil")
	}
}

// TestParseTLE_InvalidFormat проверяет обработку некорректного формата.
func TestParseTLE_InvalidFormat(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
	}{
		{
			name:  "Empty input",
			lines: []string{},
		},
		{
			name:  "Single line",
			lines: []string{"1 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  9993"},
		},
		{
			name: "Wrong line number",
			lines: []string{
				"3 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  9993",
				"2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.49815571423456",
			},
		},
		{
			name:  "Line too short",
			lines: []string{"1 25544U 98067A", "2 25544  51.6400"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTLE(tt.lines)
			if err == nil {
				t.Error("ParseTLE() expected error, got nil")
			}
		})
	}
}

// TestParseTLE_SwappedTwoLine проверяет парсинг 2-line TLE при переставленном порядке (Line2, Line1).
func TestParseTLE_SwappedTwoLine(t *testing.T) {
	lines := []string{hstLine2, hstLine1}

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}
	if tle.NoradID != 20580 {
		t.Errorf("NoradID = %d, want 20580", tle.NoradID)
	}
}

// TestParseTLEBatch_SkipsComments проверяет, что строки-комментарии пропускаются.
func TestParseTLEBatch_SkipsComments(t *testing.T) {
	batch := "# Comment line\n" + issTLE + "\n# Another comment\n" + meteorTLE

	tles, err := ParseTLEBatch(batch)
	if err != nil {
		t.Fatalf("ParseTLEBatch() error = %v", err)
	}
	if len(tles) != 2 {
		t.Fatalf("ParseTLEBatch() returned %d TLEs, want 2", len(tles))
	}
}

// TestParseTLEBatch проверяет парсинг нескольких TLE.
func TestParseTLEBatch(t *testing.T) {
	batch := issTLE + "\n" + meteorTLE

	tles, err := ParseTLEBatch(batch)
	if err != nil {
		t.Fatalf("ParseTLEBatch() error = %v", err)
	}

	if len(tles) != 2 {
		t.Fatalf("ParseTLEBatch() returned %d TLEs, want 2", len(tles))
	}

	// Проверка первого TLE (ISS)
	if tles[0].NoradID != 25544 {
		t.Errorf("First TLE NoradID = %d, want 25544", tles[0].NoradID)
	}
	if tles[0].Name != "ISS (ZARYA)" {
		t.Errorf("First TLE Name = %q, want %q", tles[0].Name, "ISS (ZARYA)")
	}

	// Проверка второго TLE (Meteor-M2)
	if tles[1].NoradID != 40069 {
		t.Errorf("Second TLE NoradID = %d, want 40069", tles[1].NoradID)
	}
	if tles[1].Name != "METEOR-M2" {
		t.Errorf("Second TLE Name = %q, want %q", tles[1].Name, "METEOR-M2")
	}
}

// TestParseExponent проверяет парсинг научной нотации TLE.
func TestParseExponent(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"10270-3", 0.0001027},    // 0.10270 * 10^-3 = 1.027e-4
		{"00000-0", 0.0},          // 0
		{"-10270-3", -0.0001027},  // -0.10270 * 10^-3
		{"56789-4", 0.000056789},  // 0.56789 * 10^-4 = 5.6789e-5
		{"12345+0", 0.12345},      // 0.12345 * 10^0
		{"12345-5", 0.0000012345}, // 0.12345 * 10^-5 = 1.2345e-6
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseExponent(tt.input)
			if math.Abs(got-tt.expected) > 1e-12 {
				t.Errorf("parseExponent(%q) = %e, want %e", tt.input, got, tt.expected)
			}
		})
	}
}

// TestTLE_String проверяет восстановление TLE в строковый формат.
func TestTLE_String(t *testing.T) {
	lines := strings.Split(issTLE, "\n")

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	// Line1 и Line2 должны быть сохранены
	if tle.Line1 == "" {
		t.Error("Line1 is empty")
	}
	if tle.Line2 == "" {
		t.Error("Line2 is empty")
	}
}

// TestParseTLE_Bstar проверяет парсинг BSTAR коэффициента.
func TestParseTLE_Bstar(t *testing.T) {
	lines := strings.Split(issTLE, "\n")

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	// BSTAR: 10270-3 = 0.10270 * 10^-3 = 0.0001027
	expectedBstar := 0.0001027
	if math.Abs(tle.Bstar-expectedBstar) > 1e-8 {
		t.Errorf("Bstar = %e, want %e", tle.Bstar, expectedBstar)
	}
}

// TestParseTLE_MeanMotionDerivatives проверяет парсинг производных mean motion.
func TestParseTLE_MeanMotionDerivatives(t *testing.T) {
	lines := strings.Split(issTLE, "\n")

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	// MeanMotionDot: .00016717
	expectedDot := 0.00016717
	if math.Abs(tle.MeanMotionDot-expectedDot) > 1e-10 {
		t.Errorf("MeanMotionDot = %e, want %e", tle.MeanMotionDot, expectedDot)
	}
}

// TestParseNoradID_Alpha5 проверяет парсинг NORAD ID в формате Alpha-5.
func TestParseNoradID_Alpha5(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		wantErr  bool
	}{
		// Стандартные числовые ID
		{"25544", 25544, false},
		{"00001", 1, false},
		{"99999", 99999, false},

		// Alpha-5 формат
		{"A0000", 100000, false}, // A = 10, 10*10000 + 0 = 100000
		{"A0001", 100001, false},
		{"A9999", 109999, false},
		{"B0000", 110000, false}, // B = 11
		{"H9999", 179999, false}, // H = 17
		{"J0000", 180000, false}, // J = 18 (пропускаем I)
		{"N9999", 229999, false}, // N = 22
		{"P0000", 230000, false}, // P = 23 (пропускаем O)
		{"Z0000", 330000, false}, // Z = 33
		{"Z9999", 339999, false}, // Максимальный Alpha-5

		// Ошибки
		{"I0000", 0, true}, // I не используется
		{"O0000", 0, true}, // O не используется
		{"", 0, true},      // Пустая строка
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseNoradID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseNoradID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("parseNoradID(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// TestParseTLE_NameStartsWithDigit проверяет парсинг 3-line TLE,
// когда имя спутника начинается с цифры (например "239ALFEROV (RS61S)").
// Регрессия: парсер путал такое имя с Line2, т.к. проверял только первый символ.
func TestParseTLE_NameStartsWithDigit(t *testing.T) {
	alferovLine1 := makeTLELine("1 64881U 25155F   26093.62321510  .00026195  00000+0  87764-3 0  999")
	alferovLine2 := makeTLELine("2 64881  97.4505  62.4262 0009884  84.4621 275.7747 15.30861630 6939")

	tests := []struct {
		name    string
		satName string
		noradID int
	}{
		{"имя начинается с '2'", "239ALFEROV (RS61S)", 64881},
		{"имя начинается с '1'", "1KUNS-PF", 64881},
		{"имя — чистое число", "2024-155F", 64881},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := []string{tt.satName, alferovLine1, alferovLine2}
			tle, err := ParseTLE(lines)
			if err != nil {
				t.Fatalf("ParseTLE() error = %v", err)
			}
			if tle.Name != tt.satName {
				t.Errorf("Name = %q, want %q", tle.Name, tt.satName)
			}
			if tle.NoradID != tt.noradID {
				t.Errorf("NoradID = %d, want %d", tle.NoradID, tt.noradID)
			}
		})
	}
}

// TestParseTLEBatch_NameStartsWithDigit проверяет batch-парсинг при именах,
// начинающихся с цифр. Воспроизводит реальный баг с 239ALFEROV + GEOSCAN.
func TestParseTLEBatch_NameStartsWithDigit(t *testing.T) {
	alferovLine1 := makeTLELine("1 64881U 25155F   26093.62321510  .00026195  00000+0  87764-3 0  999")
	alferovLine2 := makeTLELine("2 64881  97.4505  62.4262 0009884  84.4621 275.7747 15.30861630 6939")
	geoscanLine1 := makeTLELine("1 64890U 25155Q   26093.63132392  .00020644  00000+0  72393-3 0  999")
	geoscanLine2 := makeTLELine("2 64890  97.4501  62.2192 0009375  74.4540 285.7734 15.29432755 6938")

	batch := "239ALFEROV (RS61S)\n" + alferovLine1 + "\n" + alferovLine2 + "\n" +
		"GEOSCAN 2 (RS92S2)\n" + geoscanLine1 + "\n" + geoscanLine2

	tles, err := ParseTLEBatch(batch)
	if err != nil {
		t.Fatalf("ParseTLEBatch() error = %v", err)
	}
	if len(tles) != 2 {
		t.Fatalf("ParseTLEBatch() returned %d TLEs, want 2", len(tles))
	}

	if tles[0].Name != "239ALFEROV (RS61S)" {
		t.Errorf("tles[0].Name = %q, want %q", tles[0].Name, "239ALFEROV (RS61S)")
	}
	if tles[0].NoradID != 64881 {
		t.Errorf("tles[0].NoradID = %d, want 64881", tles[0].NoradID)
	}
	if tles[1].Name != "GEOSCAN 2 (RS92S2)" {
		t.Errorf("tles[1].Name = %q, want %q", tles[1].Name, "GEOSCAN 2 (RS92S2)")
	}
	if tles[1].NoradID != 64890 {
		t.Errorf("tles[1].NoradID = %d, want 64890", tles[1].NoradID)
	}
}

// TestParseTLEBatch_ManyDigitNames проверяет batch с несколькими спутниками,
// имена которых начинаются с цифр (последовательность без пустых строк).
func TestParseTLEBatch_ManyDigitNames(t *testing.T) {
	sat1Line1 := makeTLELine("1 64891U 25155R   26093.62796926  .00026097  00000+0  86580-3 0  999")
	sat1Line2 := makeTLELine("2 64891  97.4498  62.5393 0008772  89.4093 270.8154 15.31190667 6940")
	sat2Line1 := makeTLELine("1 64892U 25155S   26093.61859638  .00026821  00000+0  88650-3 0  999")
	sat2Line2 := makeTLELine("2 64892  97.4501  62.5596 0008792  90.0492 270.1756 15.31305917 6940")

	batch := "2SAT-ALPHA\n" + sat1Line1 + "\n" + sat1Line2 + "\n" +
		"1SAT-BETA\n" + sat2Line1 + "\n" + sat2Line2

	tles, err := ParseTLEBatch(batch)
	if err != nil {
		t.Fatalf("ParseTLEBatch() error = %v", err)
	}
	if len(tles) != 2 {
		t.Fatalf("ParseTLEBatch() returned %d TLEs, want 2", len(tles))
	}
	if tles[0].Name != "2SAT-ALPHA" {
		t.Errorf("tles[0].Name = %q, want %q", tles[0].Name, "2SAT-ALPHA")
	}
	if tles[1].Name != "1SAT-BETA" {
		t.Errorf("tles[1].Name = %q, want %q", tles[1].Name, "1SAT-BETA")
	}
}

// TestIsTLEDataLine проверяет вспомогательную функцию различения TLE-строк от имён.
func TestIsTLEDataLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{"валидная Line1", issLine1, true},
		{"валидная Line2", issLine2, true},
		{"имя 239ALFEROV", "239ALFEROV (RS61S)", false},
		{"имя 1KUNS-PF", "1KUNS-PF", false},
		{"короткая строка с '2'", "2 64881", false},
		{"пустая строка", "", false},
		{"имя ISS", "ISS (ZARYA)", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTLEDataLine(tt.line)
			if got != tt.expected {
				t.Errorf("isTLEDataLine(%q) = %v, want %v", tt.line, got, tt.expected)
			}
		})
	}
}

// TestParseTLE_Alpha5_Starlink проверяет парсинг TLE со Starlink (Alpha-5 NORAD ID).
func TestParseTLE_Alpha5_Starlink(t *testing.T) {
	// Симулируем Starlink TLE с Alpha-5 NORAD ID (A0001 = 100001)
	starlinkLine1 := makeTLELine("1 A0001U 24001A   24001.50000000  .00000123  00000-0  12345-4 0  999")
	starlinkLine2 := makeTLELine("2 A0001  53.0000 123.4567 0001234  90.0000 270.0000 15.0000000000001")
	starlinkTLE := "STARLINK-99999\n" + starlinkLine1 + "\n" + starlinkLine2

	lines := strings.Split(starlinkTLE, "\n")

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	// NORAD ID должен быть 100001 (A0001 в Alpha-5)
	if tle.NoradID != 100001 {
		t.Errorf("NoradID = %d, want 100001", tle.NoradID)
	}

	if tle.Name != "STARLINK-99999" {
		t.Errorf("Name = %q, want %q", tle.Name, "STARLINK-99999")
	}
}

// TestMeanAltitudeKm_ISS — средняя высота орбиты ISS в типичном диапазоне LEO.
func TestMeanAltitudeKm_ISS(t *testing.T) {
	lines := strings.Split(issTLE, "\n")

	tle, err := ParseTLE(lines)
	if err != nil {
		t.Fatalf("ParseTLE() error = %v", err)
	}

	mean := tle.MeanAltitudeKm()
	if mean < 350 || mean > 450 {
		t.Errorf("MeanAltitudeKm() = %.1f, want ISS LEO range 350..450 km", mean)
	}

	apogee := tle.Apogee()
	perigee := tle.Perigee()
	if math.Abs(mean-(apogee+perigee)/2) > 0.01 {
		t.Errorf("MeanAltitudeKm() = %.3f, want (apogee+perigee)/2 = %.3f", mean, (apogee+perigee)/2)
	}
}
