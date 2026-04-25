package tracker

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// --- Типы данных для обмена с эталонным калькулятором (skyfield) ---

// refInput — входные параметры для одного тестового кейса.
type refInput struct {
	Name          string  `json:"name"`
	TLEName       string  `json:"tle_name"`
	TLELine1      string  `json:"tle_line1"`
	TLELine2      string  `json:"tle_line2"`
	ObserverLat   float64 `json:"observer_lat"`
	ObserverLon   float64 `json:"observer_lon"`
	ObserverAltKm float64 `json:"observer_alt_km"`
	StartUnix     int64   `json:"start_unix"`
	EndUnix       int64   `json:"end_unix"`
	MinElDeg      float64 `json:"min_elevation_deg"`
}

// refPass — один пролёт от эталонного калькулятора.
type refPass struct {
	AOSUnixMs int64   `json:"aos_unix_ms"`
	AOSAz     float64 `json:"aos_az"`
	TCAUnixMs int64   `json:"tca_unix_ms"`
	TCAEl     float64 `json:"tca_el"`
	TCAAz     float64 `json:"tca_az"`
	LOSUnixMs int64   `json:"los_unix_ms"`
	LOSAz     float64 `json:"los_az"`
	Duration  float64 `json:"duration_sec"`
}

// refOutput — результат расчёта одного кейса от эталонного калькулятора.
type refOutput struct {
	Name   string    `json:"name"`
	Passes []refPass `json:"passes"`
	Error  *string   `json:"error"`
}

// Допуски сравнения наших расчётов с эталонными (skyfield).
// SGP4-реализации совпадают с высокой точностью, основное расхождение —
// в алгоритме поиска AOS/LOS (бисекция vs root-finding).
const (
	refTimeTolerance = 3 * time.Second // ±3с для AOS/TCA/LOS.
	refElTolerance   = 1.0             // ±1° для угла места.
	refAzTolerance   = 2.0             // ±2° для азимута.
	refTCAMatchLimit = 5 * time.Minute // Максимальное расхождение TCA для матчинга пролётов.
)

// refDockerfileCtx — путь к контексту Dockerfile относительно пакета tracker.
const refDockerfileCtx = "../../tests/reference"

func TestPredictPasses_ReferenceValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("reference validation requires Docker — пропуск в short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	cases := buildRefCases(t)

	// Записываем входные данные для эталонного калькулятора.
	inputPath := filepath.Join(tmpDir, "input.json")
	inputData, err := json.MarshalIndent(cases, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(inputPath, inputData, 0644))

	// Запускаем контейнер с skyfield.
	container := startRefContainer(t, ctx, tmpDir)
	defer func() {
		if termErr := container.Terminate(ctx); termErr != nil {
			t.Logf("ошибка остановки контейнера: %v", termErr)
		}
	}()

	// Читаем результат.
	outputPath := filepath.Join(tmpDir, "output.json")
	outputData, err := os.ReadFile(outputPath)
	require.NoError(t, err, "не удалось прочитать output.json — контейнер мог завершиться с ошибкой")

	var results []refOutput
	require.NoError(t, json.Unmarshal(outputData, &results))
	require.Equal(t, len(cases), len(results), "количество результатов не совпадает с количеством кейсов")

	// Сравниваем с нашими расчётами.
	for i, c := range cases {
		ref := results[i]
		t.Run(c.Name, func(t *testing.T) {
			if ref.Error != nil {
				t.Fatalf("skyfield вернул ошибку: %s", *ref.Error)
			}
			compareRefPasses(t, c, ref)
		})
	}
}

// buildRefCases формирует набор тестовых кейсов.
// Покрываем разные типы орбит и позиции наблюдателя.
func buildRefCases(t *testing.T) []refInput {
	t.Helper()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	endTime := baseTime.Add(24 * time.Hour)

	return []refInput{
		{
			// LEO, среднее наклонение (51.6°), классический случай.
			Name:          "ISS_Rostov",
			TLEName:       "ISS (ZARYA)",
			TLELine1:      makeTLELinePP("1 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  999"),
			TLELine2:      makeTLELinePP("2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340"),
			ObserverLat:   47.23,
			ObserverLon:   39.72,
			ObserverAltKm: 0.08,
			StartUnix:     baseTime.Unix(),
			EndUnix:       endTime.Unix(),
			MinElDeg:      5.0,
		},
		{
			// Полярная солнечно-синхронная орбита (98.5°).
			Name:          "METEOR-M2_Rostov",
			TLEName:       "METEOR-M2",
			TLELine1:      makeTLELinePP("1 40069U 14037A   24001.50000000  .00000123  00000-0  12345-4 0  999"),
			TLELine2:      makeTLELinePP("2 40069  98.5200  45.6789 0001234 123.4567 236.7890 14.2098765432109"),
			ObserverLat:   47.23,
			ObserverLon:   39.72,
			ObserverAltKm: 0.08,
			StartUnix:     baseTime.Unix(),
			EndUnix:       endTime.Unix(),
			MinElDeg:      5.0,
		},
		{
			// Тот же ISS, но наблюдатель на экваторе — другая геометрия.
			Name:          "ISS_Equator",
			TLEName:       "ISS (ZARYA)",
			TLELine1:      makeTLELinePP("1 25544U 98067A   24001.50000000  .00016717  00000-0  10270-3 0  999"),
			TLELine2:      makeTLELinePP("2 25544  51.6400 247.4627 0006703 130.5360 325.0288 15.4981557142340"),
			ObserverLat:   0.0,
			ObserverLon:   0.0,
			ObserverAltKm: 0.0,
			StartUnix:     baseTime.Unix(),
			EndUnix:       endTime.Unix(),
			MinElDeg:      5.0,
		},
		{
			// Полярная орбита, наблюдатель на высокой широте (Москва).
			Name:          "NOAA18_Moscow",
			TLEName:       "NOAA 18",
			TLELine1:      makeTLELinePP("1 28654U 05018A   24001.50000000  .00000050  00000-0  40000-4 0  999"),
			TLELine2:      makeTLELinePP("2 28654  99.0300  55.1234 0014500 100.2345 260.0123 14.1234567890123"),
			ObserverLat:   55.7558,
			ObserverLon:   37.6173,
			ObserverAltKm: 0.156,
			StartUnix:     baseTime.Unix(),
			EndUnix:       endTime.Unix(),
			MinElDeg:      5.0,
		},
	}
}

// startRefContainer поднимает Docker-контейнер с Python + skyfield.
func startRefContainer(t *testing.T, ctx context.Context, dataDir string) testcontainers.Container {
	t.Helper()

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    refDockerfileCtx,
			Dockerfile: "Dockerfile",
		},
		Cmd:   []string{"/data/input.json", "/data/output.json"},
		Files: []testcontainers.ContainerFile{},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Binds = []string{dataDir + ":/data"}
		},
		WaitingFor: wait.ForExit().WithExitTimeout(3 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("не удалось запустить Docker-контейнер (Docker недоступен?): %v", err)
	}

	return container
}

// compareRefPasses сравнивает наши расчёты с эталонными для одного кейса.
func compareRefPasses(t *testing.T, input refInput, ref refOutput) {
	t.Helper()

	tle, err := ParseTLE([]string{input.TLEName, input.TLELine1, input.TLELine2})
	require.NoError(t, err)

	prop, err := NewPropagator(tle)
	require.NoError(t, err)

	obs := NewObserver(input.ObserverLat, input.ObserverLon, input.ObserverAltKm)
	start := time.Unix(input.StartUnix, 0).UTC()
	end := time.Unix(input.EndUnix, 0).UTC()

	ourPasses, err := PredictPasses(prop, obs, start, end, input.MinElDeg)
	require.NoError(t, err)

	t.Logf("наших пролётов: %d, эталонных (skyfield): %d", len(ourPasses), len(ref.Passes))

	// Для каждого пролёта логируем детали.
	for i, p := range ourPasses {
		t.Logf("  наш  [%d]: AOS %s Az=%.0f° → TCA %s El=%.1f° → LOS %s Az=%.0f°",
			i,
			time.UnixMilli(p.AOS).UTC().Format(timeFormatHMS), p.AOSAz,
			time.UnixMilli(p.TCA).UTC().Format(timeFormatHMS), p.TCAEl,
			time.UnixMilli(p.LOS).UTC().Format(timeFormatHMS), p.LOSAz)
	}

	for i, rp := range ref.Passes {
		t.Logf("  ref  [%d]: AOS %s Az=%.0f° → TCA %s El=%.1f° → LOS %s Az=%.0f°",
			i,
			time.UnixMilli(rp.AOSUnixMs).UTC().Format(timeFormatHMS), rp.AOSAz,
			time.UnixMilli(rp.TCAUnixMs).UTC().Format(timeFormatHMS), rp.TCAEl,
			time.UnixMilli(rp.LOSUnixMs).UTC().Format(timeFormatHMS), rp.LOSAz)
	}

	// Матчим пролёты по близости TCA.
	matched := 0
	unmatched := 0

	for _, rp := range ref.Passes {
		best := findClosestOurPass(ourPasses, rp.TCAUnixMs)
		if best == nil {
			t.Errorf("эталонный пролёт TCA=%s не найден в наших расчётах",
				time.UnixMilli(rp.TCAUnixMs).UTC().Format(timeFormatHMS))
			unmatched++

			continue
		}

		assertRefTimeDelta(t, "AOS", rp.AOSUnixMs, best.AOS, refTimeTolerance)
		assertRefTimeDelta(t, "TCA", rp.TCAUnixMs, best.TCA, refTimeTolerance)
		assertRefTimeDelta(t, "LOS", rp.LOSUnixMs, best.LOS, refTimeTolerance)

		assertRefFloatDelta(t, "TCA El", rp.TCAEl, best.TCAEl, refElTolerance)
		assertRefAzDelta(t, "AOS Az", rp.AOSAz, best.AOSAz, refAzTolerance)
		assertRefAzDelta(t, "LOS Az", rp.LOSAz, best.LOSAz, refAzTolerance)

		matched++
	}

	t.Logf("совпало %d/%d эталонных пролётов", matched, len(ref.Passes))

	// Допускаем расхождение в ±2 пролёта на границах окна.
	const maxExtraPasses = 2
	if len(ourPasses) > len(ref.Passes)+maxExtraPasses {
		t.Errorf("слишком много лишних пролётов: наших=%d, эталонных=%d (допуск +%d)",
			len(ourPasses), len(ref.Passes), maxExtraPasses)
	}

	if unmatched > maxExtraPasses {
		t.Errorf("слишком много непарных эталонных пролётов: %d (допуск %d)", unmatched, maxExtraPasses)
	}
}

// findClosestOurPass ищет ближайший наш пролёт по TCA к заданному времени.
func findClosestOurPass(passes []*Pass, tcaMs int64) *Pass {
	var best *Pass

	bestDelta := int64(math.MaxInt64)

	for _, p := range passes {
		delta := absInt64(p.TCA - tcaMs)
		if delta < bestDelta {
			bestDelta = delta
			best = p
		}
	}

	if bestDelta > refTCAMatchLimit.Milliseconds() {
		return nil
	}

	return best
}

func absInt64(x int64) int64 {
	if x < 0 {
		return -x
	}

	return x
}

func assertRefTimeDelta(t *testing.T, name string, wantMs, gotMs int64, tolerance time.Duration) {
	t.Helper()

	delta := time.Duration(absInt64(wantMs-gotMs)) * time.Millisecond
	if delta > tolerance {
		t.Errorf("%s: расхождение %v превышает допуск %v (эталон=%s, наш=%s)",
			name, delta, tolerance,
			time.UnixMilli(wantMs).UTC().Format(timeFormatHMS),
			time.UnixMilli(gotMs).UTC().Format(timeFormatHMS))
	}
}

func assertRefFloatDelta(t *testing.T, name string, want, got, tolerance float64) {
	t.Helper()

	delta := math.Abs(want - got)
	if delta > tolerance {
		t.Errorf("%s: расхождение %.2f° превышает допуск %.1f° (эталон=%.1f°, наш=%.1f°)",
			name, delta, tolerance, want, got)
	}
}

// assertRefAzDelta сравнивает азимуты с учётом цикличности (0° ↔ 360°).
func assertRefAzDelta(t *testing.T, name string, want, got, tolerance float64) {
	t.Helper()

	delta := math.Abs(want - got)
	if delta > 180 {
		delta = 360 - delta
	}

	if delta > tolerance {
		t.Errorf("%s: расхождение %.2f° превышает допуск %.1f° (эталон=%.1f°, наш=%.1f°)",
			name, delta, tolerance, want, got)
	}
}
