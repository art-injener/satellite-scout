package sdr

import (
	"fmt"
	"strings"
	"time"
)

// Service — обнаружение SDR-устройств и их проверка для модалки настроек.
type Service struct{}

// NewService создаёт сервис перечисления и тестирования приёмников.
func NewService() *Service {
	return &Service{}
}

// ListDevices возвращает список доступных приёмников (виртуальный + обнаруженные).
func (s *Service) ListDevices() ListResponse {
	devices := []Device{
		{
			Driver: "simulated",
			Label:  "Имитатор (simulated)",
		},
	}
	devices = append(devices, discoverUSBDevices()...)
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
		return testUSBDevice(req)
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

func testUSBDevice(req TestRequest) TestResponse {
	devices := discoverUSBDevices()
	var match *Device
	for i := range devices {
		d := &devices[i]
		if d.Driver != req.Driver {
			continue
		}
		if req.Serial != "" && d.Serial == req.Serial {
			match = d
			break
		}
		if req.DevicePath != "" && d.DevicePath == req.DevicePath {
			match = d
			break
		}
	}

	lines := []string{
		fmt.Sprintf("Драйвер:     %s", req.Driver),
	}
	if match == nil {
		lines = append(lines,
			"Serial:      "+emptyDash(req.Serial),
			"Путь:        "+emptyDash(req.DevicePath),
			"",
			"Итог: устройство не найдено — проверьте подключение и нажмите «Обновить»",
		)
		return TestResponse{OK: false, Lines: lines}
	}

	lines = append(lines,
		fmt.Sprintf("Модель:      %s", match.Label),
		fmt.Sprintf("Serial:      %s", emptyDash(match.Serial)),
		fmt.Sprintf("Путь:        %s", emptyDash(match.DevicePath)),
		"",
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

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
