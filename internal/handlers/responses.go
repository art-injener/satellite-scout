package handlers

import "github.com/art-injener/satellite-scout/internal/tracker"

// ErrorResponse — стандартный ответ с ошибкой.
type ErrorResponse struct {
	Error string `json:"error"`
}

// HealthResponse — ответ проверки работоспособности.
type HealthResponse struct {
	Status string `json:"status"`
}

// ObserverConfig — координаты наблюдателя.
type ObserverConfig struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Alt float64 `json:"alt"`
}

// RadioPathInfo — компактное представление радиотракта для фронтенда.
// Передаётся в /api/config — этого достаточно, чтобы построить dropdown
// в mode-bar и понять, доступна ли кнопка "Сопровождать" для тракта.
// Полные параметры (Receiver.Defaults, Rotator.Port и т.п.) UI получает
// только через GET /api/settings, когда открыта модалка настроек.
type RadioPathInfo struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Band       string `json:"band"`
	HasRotator bool   `json:"has_rotator"`
}

// ConfigResponse — ответ с конфигурацией для фронтенда. Содержит только то,
// что нужно для инициализации UI: координаты наблюдателя, вычисленный тип
// станции и список радиотрактов в краткой форме.
type ConfigResponse struct {
	Observer    ObserverConfig  `json:"observer"`
	StationType string          `json:"station_type"`
	RadioPaths  []RadioPathInfo `json:"radio_paths"`
}

// PassesParams — параметры запроса пролётов.
type PassesParams struct {
	Group string  `json:"group"`
	Hours int     `json:"hours"`
	MinEl float64 `json:"min_el"`
}

// PassesResponse — ответ со списком пролётов.
type PassesResponse struct {
	Passes []*tracker.Pass `json:"passes"`
	Count  int             `json:"count"`
	Params PassesParams    `json:"params"`
}

// TrackingResponse — ответ на запрос отслеживания спутника.
type TrackingResponse struct {
	Status  string `json:"status"`
	NoradID int    `json:"norad_id"`
}
