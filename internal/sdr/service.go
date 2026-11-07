package sdr

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ProbeReport — что драйвер узнал, открыв устройство.
type ProbeReport struct {
	Tuner      string
	MinFreqHz  uint64
	MaxFreqHz  uint64
	CenterHz   uint64
	SampleRate uint32
	GainDB     float32 // отрицательное — АРУ
	Samples    int     // сколько отсчётов прочитано
}

// Probe открывает устройство драйвером, настраивает его и читает немного отсчётов.
type Probe func(serial, devicePath string) (ProbeReport, error)

// Service — обнаружение SDR-устройств и их проверка для модалки настроек.
type Service struct {
	discover func() []Device
	probes   map[string]Probe
}

// NewService создаёт сервис перечисления и тестирования приёмников.
func NewService() *Service {
	return &Service{discover: discoverUSBDevices, probes: map[string]Probe{}}
}

// WithProbe подключает проверку устройства драйвером: без неё проверка только ищет его в системе.
func (s *Service) WithProbe(driver string, p Probe) *Service {
	s.probes[driver] = p
	return s
}

// ListDevices возвращает список доступных приёмников (виртуальный + обнаруженные).
func (s *Service) ListDevices() ListResponse {
	devices := make([]Device, 0, 8)
	devices = append(devices, Device{
		Driver: "simulated",
		Label:  "Имитатор (simulated)",
	})
	devices = append(devices, s.discover()...)
	return ListResponse{
		Devices:   devices,
		ScannedAt: time.Now().UTC(),
	}
}

// Test выполняет проверку выбранного приёмника и возвращает текстовый отчёт.
func (s *Service) Test(req TestRequest) TestResponse {
	driver := strings.TrimSpace(req.Driver)
	switch driver {
	case "simulated":
		return testSimulated()
	case "rtlsdr", "hackrf", "airspy":
		return s.testUSBDevice(req)
	default:
		return TestResponse{
			OK: false,
			Lines: []string{
				"Неизвестный драйвер: " + driver,
				"",
				"Итог: ошибка",
			},
		}
	}
}

func testSimulated() TestResponse {
	return TestResponse{
		OK: true,
		Lines: []string{
			"Драйвер:     simulated",
			"Модель:      Виртуальный SDR",
			"",
			"Tuner:       (виртуальный)",
			"Частоты:     24 – 1766 МГц",
			"Sample rate: до 2.4 MS/s",
			"",
			"Текущая частота:  145.900 МГц",
			"Текущее усиление: 38.0 dB",
			"",
			"Открытие:    OK",
			"Чтение IQ:   OK (256 сэмплов)",
			"",
			"Итог: устройство готово",
		},
	}
}

func (s *Service) testUSBDevice(req TestRequest) TestResponse {
	var devices []Device
	for _, d := range s.discover() {
		if d.Driver == req.Driver {
			devices = append(devices, d)
		}
	}

	lines := []string{
		fmt.Sprintf("Драйвер:     %s", req.Driver),
	}
	i, err := MatchDevice(devices, req.Serial, req.DevicePath)
	if err != nil {
		verdict := "Итог: устройство не найдено — проверьте подключение и нажмите «Обновить»"
		if errors.Is(err, ErrDeviceAmbiguous) {
			verdict = "Итог: несколько устройств с этим серийным номером, а путь устарел — выберите приёмник в списке заново"
		}
		lines = append(lines,
			"Serial:      "+emptyDash(req.Serial),
			"Путь:        "+emptyDash(req.DevicePath),
			"",
			verdict,
		)
		return TestResponse{OK: false, Lines: lines}
	}
	match := &devices[i]

	lines = append(lines,
		fmt.Sprintf("Модель:      %s", match.Label),
		fmt.Sprintf("Serial:      %s", emptyDash(match.Serial)),
		fmt.Sprintf("Путь:        %s", emptyDash(match.DevicePath)),
		"",
	)
	if probe := s.probes[req.Driver]; probe != nil {
		return probeLines(lines, probe, match)
	}
	lines = append(lines,
		"Tuner:       (опрос через SDR-драйвер — в разработке)",
		"Частоты:     см. документацию устройства",
		"",
		"Текущая частота:  —",
		"Текущее усиление: —",
		"",
		"Открытие:    устройство обнаружено в системе",
		"Чтение IQ:   требуется инициализация SDR-слоя",
		"",
		"Итог: устройство видно системе; полный probe будет доступен после SDR-001",
	)
	return TestResponse{OK: true, Lines: lines}
}

// probeLines дописывает к отчёту результат проверки устройства драйвером.
func probeLines(lines []string, probe Probe, d *Device) TestResponse {
	rep, err := probe(d.Serial, d.DevicePath)
	if err != nil {
		lines = append(lines,
			"Открытие:    ошибка: "+err.Error(),
			"",
			"Итог: ошибка — устройство занято приёмом или недоступно",
		)
		return TestResponse{OK: false, Lines: lines}
	}
	gain := fmt.Sprintf("%.1f dB", rep.GainDB)
	if rep.GainDB < 0 {
		gain = "АРУ"
	}
	lines = append(lines,
		"Tuner:       "+rep.Tuner,
		fmt.Sprintf("Частоты:     %.1f – %.0f МГц", float64(rep.MinFreqHz)/1e6, float64(rep.MaxFreqHz)/1e6),
		fmt.Sprintf("Sample rate: %.3f MS/s", float64(rep.SampleRate)/1e6),
		"",
		fmt.Sprintf("Текущая частота:  %.3f МГц", float64(rep.CenterHz)/1e6),
		"Текущее усиление: "+gain,
		"",
		"Открытие:    OK",
		fmt.Sprintf("Чтение IQ:   OK (%d сэмплов)", rep.Samples),
		"",
		"Итог: устройство готово",
	)
	return TestResponse{OK: true, Lines: lines}
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
