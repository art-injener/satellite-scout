package config

import "testing"

// TestStationType_Basic — пустой список радиотрактов даёт "basic": конфигурация
// без оборудования, доступно только отслеживание спутников.
func TestStationType_Basic(t *testing.T) {
	sc := StationConfig{RadioPaths: []RadioPath{}}
	if got := sc.StationType(); got != StationTypeBasic {
		t.Errorf("StationType() = %q, want %q", got, StationTypeBasic)
	}
}

// TestStationType_BasicForNilRadioPaths — nil-слайс трактуется так же, как пустой.
func TestStationType_BasicForNilRadioPaths(t *testing.T) {
	sc := StationConfig{RadioPaths: nil}
	if got := sc.StationType(); got != StationTypeBasic {
		t.Errorf("StationType() = %q, want %q", got, StationTypeBasic)
	}
}

// TestStationType_Observation — все тракты без поворотной платформы:
// стационарные антенны, режим обзора.
func TestStationType_Observation(t *testing.T) {
	sc := StationConfig{RadioPaths: []RadioPath{
		{ID: 1, Rotator: nil},
		{ID: 2, Rotator: nil},
	}}
	if got := sc.StationType(); got != StationTypeObservation {
		t.Errorf("StationType() = %q, want %q", got, StationTypeObservation)
	}
}

// TestStationType_Tracking — все тракты с поворотной платформой:
// полноценное сопровождение по азимуту/углу места.
func TestStationType_Tracking(t *testing.T) {
	sc := StationConfig{RadioPaths: []RadioPath{
		{ID: 1, Rotator: &RotatorConfig{Driver: "rotctld"}},
		{ID: 2, Rotator: &RotatorConfig{Driver: "rotctld"}},
	}}
	if got := sc.StationType(); got != StationTypeTracking {
		t.Errorf("StationType() = %q, want %q", got, StationTypeTracking)
	}
}

// TestStationType_Hybrid — смешанный набор: один тракт с повороткой,
// другой без. Возможности UI зависят от выбранного радиотракта.
func TestStationType_Hybrid(t *testing.T) {
	sc := StationConfig{RadioPaths: []RadioPath{
		{ID: 1, Rotator: nil},
		{ID: 2, Rotator: &RotatorConfig{Driver: "rotctld"}},
	}}
	if got := sc.StationType(); got != StationTypeHybrid {
		t.Errorf("StationType() = %q, want %q", got, StationTypeHybrid)
	}
}

// TestStationType_SingleWithRotator — единственный тракт с повороткой
// тоже даёт "tracking", а не "hybrid".
func TestStationType_SingleWithRotator(t *testing.T) {
	sc := StationConfig{RadioPaths: []RadioPath{
		{ID: 1, Rotator: &RotatorConfig{Driver: "rotctld"}},
	}}
	if got := sc.StationType(); got != StationTypeTracking {
		t.Errorf("StationType() = %q, want %q", got, StationTypeTracking)
	}
}
