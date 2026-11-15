package main

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/services"
	"github.com/art-injener/satellite-scout/internal/sweep"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

const (
	freqCuratedPath = "data/freq_curated.json"
	freqPlanPath    = "data/freq_plan.csv"
	freqAMSATCache  = "data/amsat_active.csv"
	freqHACache     = "data/heavensabove_amateur.html"
	freqPlanEvery   = 5 * time.Minute

	// freqFetchTimeout — без сети загрузка не должна держать план: дальше идёт кеш на диске.
	freqFetchTimeout = 15 * time.Second
	// passCheckHorizon — страница Heavens-Above покрывает сутки пролётов.
	passCheckHorizon = 24 * time.Hour
	// haLocationTolDeg — страница посчитана для других координат, если они расходятся сильнее.
	haLocationTolDeg = 0.01
)

// startFreqPlan периодически собирает план частот на час вперёд и пишет CSV.
func (a *app) startFreqPlan(ctx context.Context) {
	curated, err := freqplan.LoadCurated(freqCuratedPath)
	if err != nil {
		slog.ErrorContext(ctx, "freqplan: curated", slogKeyError, err)
		return
	}
	a.seedRegistry(ctx, curated)
	// План обогащает ленту панорамы и ожидаемые TX (даже без SatNOGS).
	a.wireFreqPlanProvider()
	go func() {
		a.refreshFreqPlan(ctx)
		t := time.NewTicker(freqPlanEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.refreshFreqPlan(ctx)
			}
		}
	}()
}

// wireFreqPlanProvider — GetTransmitters = SatNOGS ∪ план частот.
func (a *app) wireFreqPlanProvider() {
	if a.tracking == nil {
		return
	}
	a.tracking.SetTransmitterProvider(&planAugment{
		base: a.satnogsAdapter,
		plan: func() []freqplan.Entry {
			a.freqPlanMu.Lock()
			defer a.freqPlanMu.Unlock()
			return append([]freqplan.Entry(nil), a.freqPlan...)
		},
	})
}

func (a *app) refreshFreqPlan(ctx context.Context) {
	if a.pass == nil {
		return
	}
	raw, err := a.pass.GetAllGroupsPasses(services.DefaultPredictionHours, services.DefaultMinElevation)
	if err != nil {
		slog.WarnContext(ctx, "freqplan: passes", slogKeyError, err)
		return
	}
	passes := toFreqPasses(raw)
	obs := a.configStore.Get().Station.Observer
	haURL := freqplan.HeavensAboveURL(obs.Lat, obs.Lon)
	haCtx, haCancel := context.WithTimeout(ctx, freqFetchTimeout)
	ha, err := freqplan.FetchHeavensAbove(haCtx, nil, haURL, freqHACache, freqplan.DefaultHeavensAboveTTL)
	haCancel()
	if err != nil {
		slog.WarnContext(ctx, "freqplan: heavens-above", slogKeyError, err)
	}
	a.checkPassesAgainstHA(ctx, ha, obs.Lat, obs.Lon)
	amsatCtx, amsatCancel := context.WithTimeout(ctx, freqFetchTimeout)
	amsat, amsatErr := freqplan.FetchAMSAT(amsatCtx, nil, "", freqAMSATCache, freqplan.DefaultAMSATTTL)
	amsatCancel()
	if amsatErr != nil {
		slog.WarnContext(ctx, "freqplan: amsat", slogKeyError, amsatErr)
	}
	ids := uniqueNorads(passes)
	// Сначала HA/AMSAT/sonik в реестр, затем SatNOGS только по пробелам (ADR-011).
	a.feedRegistry(ctx, ha, err, amsat, amsatErr, ids)
	// Новые сведения о живости меняют отбор КА в пролётах.
	a.pass.InvalidateCache()
	// Приоритет на близкой частоте задаёт реестр: curated, Heavens-Above, AMSAT, sonik, SatNOGS.
	entries := freqplan.Merge(passes, []freqplan.Source{bandSource{
		src: a.registry, bands: a.bands, antenna: a.antennaBands,
	}})
	if err := freqplan.WritePlan(freqPlanPath, entries); err != nil {
		slog.WarnContext(ctx, "freqplan: write", slogKeyError, err)
		return
	}
	a.freqPlanMu.Lock()
	a.freqPlan = entries
	a.freqPlanMu.Unlock()
	// Лента и маркеры читают GetTransmitters — обновляем группу на клиентах.
	if a.tracking != nil {
		a.tracking.ForceGroupUpdate()
	}
	slog.InfoContext(ctx, "freqplan updated",
		slog.Int("entries", len(entries)),
		slog.Int("passes", len(passes)),
		slog.String("path", freqPlanPath),
	)
}

// planSweepFreqs — частоты плана в полосе приёмника ∩ антенны на горизонте обхода.
func (a *app) planSweepFreqs(_ string, now, until time.Time) []uint64 {
	plan := a.loadFreqPlan()
	bands, antenna := a.bands, a.antennaBands
	return freqplan.ActiveFreqs(plan, now.UnixMilli(), until.UnixMilli(), func(hz int64) bool {
		if !bands.Allow(hz) {
			return false
		}
		if len(antenna) > 0 && !antenna.Allow(hz) {
			return false
		}
		return true
	})
}

// loadFreqPlan — копия плана в памяти или CSV на диске.
func (a *app) loadFreqPlan() []freqplan.Entry {
	a.freqPlanMu.Lock()
	plan := append([]freqplan.Entry(nil), a.freqPlan...)
	a.freqPlanMu.Unlock()
	if len(plan) == 0 {
		if loaded, err := freqplan.ReadPlan(freqPlanPath); err == nil {
			return loaded
		}
	}
	return plan
}

// bandSightings — участки bandscan в окне обхода.
func (a *app) bandSightings(pathID string, w sweep.Window) []freqplan.Sighting {
	if a.bandscan == nil {
		return nil
	}
	var sights []freqplan.Sighting
	for _, s := range a.bandscan.SignalsInBand(pathID, w.CenterHz, w.SpanHz) {
		sights = append(sights, freqplan.Sighting{
			CenterHz: s.CenterHz, NoradID: s.NoradID,
			Origin: s.Origin, Emission: s.Emission, SNRDB: s.SNRDB,
			LastSeenMs: int64(s.LastSeenMs), //nolint:gosec // метки времени bandscan < MaxInt64
		})
	}
	return sights
}

// sweepDwell — гибкая стоянка: план × обнаружения → DecideHold.
func (a *app) sweepDwell(pathID string, w sweep.Window, startMs, nowMs, configDwellMs int64) int64 {
	return freqplan.DecideHold(
		freqplan.Window{CenterHz: w.CenterHz, SpanHz: w.SpanHz},
		startMs, nowMs, configDwellMs,
		a.loadFreqPlan(), a.bandSightings(pathID, w),
	)
}

// onSweepDwellEnd пишет журнал визитов: план × обнаружения в закончившемся окне.
func (a *app) onSweepDwellEnd(pathID string, w sweep.Window, startMs, endMs int64) {
	plan := a.loadFreqPlan()
	sights := a.bandSightings(pathID, w)
	visits := freqplan.Judge(pathID, freqplan.Window{CenterHz: w.CenterHz, SpanHz: w.SpanHz},
		startMs, endMs, plan, sights)
	if a.registry == nil {
		return
	}
	if err := a.registry.AppendVisits(context.Background(), visits); err != nil {
		slog.Warn("freqplan: visits", slogKeyError, err)
		return
	}
	if len(visits) > 0 {
		slog.Info("freqplan visits",
			slog.String("radio_path", pathID),
			slog.Uint64("center_hz", w.CenterHz),
			slog.Int("visits", len(visits)),
		)
	}
}

// checkPassesAgainstHA сверяет наши пролёты на сутки с Heavens-Above и обновляет статус TLE.
// Без сети работает по кешу страницы; нет будущих пролётов в кеше, значит нет и сверки.
func (a *app) checkPassesAgainstHA(ctx context.Context, ha *freqplan.HeavensAbove, lat, lon float64) {
	if a.tleStore == nil {
		return
	}
	now := time.Now()
	var check *tracker.PassCheck
	if ref := haRefPasses(ha, lat, lon, now, now.Add(passCheckHorizon)); len(ref) > 0 {
		ours, err := a.pass.GetAllGroupsPasses(int(passCheckHorizon.Hours()), services.DefaultMinElevation)
		if err != nil {
			slog.WarnContext(ctx, "pass check: passes", slogKeyError, err)
			return
		}
		c := tracker.ComparePasses(ours, ref)
		if c.Matched > 0 {
			c.Source = freqplan.SourceHeavensAbove
			c.CheckedAt = now
			check = &c
		}
		attrs := []any{
			slog.Int("reference", len(ref)),
			slog.Int("matched", c.Matched),
			slog.Float64("median_drift_s", c.MedianDriftS),
		}
		if c.ISSDriftS != nil {
			attrs = append(attrs, slog.Float64("iss_drift_s", *c.ISSDriftS))
		}
		slog.InfoContext(ctx, "pass check vs heavens-above", attrs...)
	}
	a.tleStore.SetPassCheck(check)
	a.kickTLEStatus()
}

// haRefPasses — пролёты страницы в окне [from, to], если она посчитана для координат станции.
func haRefPasses(ha *freqplan.HeavensAbove, lat, lon float64, from, to time.Time) []tracker.RefPass {
	haLat, haLon, ok := ha.Location()
	if !ok || math.Abs(haLat-lat) > haLocationTolDeg || math.Abs(haLon-lon) > haLocationTolDeg {
		return nil
	}
	var out []tracker.RefPass
	for _, p := range ha.Passes() {
		if p.Start.Before(from) || p.End.After(to) {
			continue
		}
		out = append(out, tracker.RefPass{NoradID: p.NoradID, Start: p.Start, End: p.End})
	}
	return out
}

func toFreqPasses(in []*tracker.Pass) []freqplan.Pass {
	out := make([]freqplan.Pass, 0, len(in))
	for _, p := range in {
		if p == nil {
			continue
		}
		out = append(out, freqplan.Pass{
			NoradID: p.NoradID, SatName: p.SatName, AOS: p.AOS, LOS: p.LOS,
		})
	}
	return out
}

func uniqueNorads(passes []freqplan.Pass) []int {
	seen := make(map[int]struct{}, len(passes))
	var out []int
	for _, p := range passes {
		if _, ok := seen[p.NoradID]; ok {
			continue
		}
		seen[p.NoradID] = struct{}{}
		out = append(out, p.NoradID)
	}
	return out
}
