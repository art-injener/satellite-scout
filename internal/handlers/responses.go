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

// ConfigResponse — ответ с конфигурацией.
type ConfigResponse struct {
	Observer ObserverConfig `json:"observer"`
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
