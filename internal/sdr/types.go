package sdr

import "time"

// Device — обнаруженный или виртуальный SDR-приёмник для UI настроек.
type Device struct {
	Driver     string `json:"driver"`
	Label      string `json:"label"`
	Serial     string `json:"serial,omitempty"`
	DevicePath string `json:"device_path,omitempty"`
}

// ListResponse — ответ GET /api/sdr/devices.
type ListResponse struct {
	Devices   []Device  `json:"devices"`
	ScannedAt time.Time `json:"scanned_at"`
}

// TestRequest — тело POST /api/sdr/test.
type TestRequest struct {
	Driver     string `json:"driver"`
	Serial     string `json:"serial,omitempty"`
	DevicePath string `json:"device_path,omitempty"`
}

// TestResponse — результат проверки приёмника.
type TestResponse struct {
	OK    bool     `json:"ok"`
	Lines []string `json:"lines"`
}
