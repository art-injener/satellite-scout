package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/services"
	"github.com/art-injener/satellite-scout/internal/sweep"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

const (
	freqCuratedPath = "data/freq_curated.json"
	freqPlanPath    = "data/freq_plan.csv"
	freqVisitsPath  = "data/freq_visits.csv"
	freqAMSATCache  = "data/amsat_active.csv"
	freqPlanEvery   = 5 * time.Minute
)

// startFreqPlan периодически собирает план частот на час вперёд и пишет CSV.
func (a *app) startFreqPlan(ctx context.Context) {
	curated, err := freqplan.LoadCurated(freqCuratedPath)
	if err != nil {
		slog.ErrorContext(ctx, "freqplan: curated", slogKeyError, err)
		return
	}
	// План обогащает ленту панорамы и ожидаемые TX (даже без SatNOGS).
	a.wireFreqPlanProvider()
	go func() {
		a.refreshFreqPlan(ctx, curated)
		t := time.NewTicker(freqPlanEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.refreshFreqPlan(ctx, curated)
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

func (a *app) refreshFreqPlan(ctx context.Context, curated *freqplan.Curated) {
	if a.pass == nil {
		return
	}
	raw, err := a.pass.GetAllGroupsPasses(services.DefaultPredictionHours, services.DefaultMinElevation)
	if err != nil {
		slog.WarnContext(ctx, "freqplan: passes", slogKeyError, err)
		return
	}
	passes := toFreqPasses(raw)
	sources := []freqplan.Source{curated}
	if a.satnogsAdapter != nil {
		ids := uniqueNorads(passes)
		a.satnogsAdapter.RequestFetch(ids)
		sources = append(sources, satnogsFreqSource(a.satnogsAdapter))
	}
	if amsat, err := freqplan.FetchAMSAT(ctx, nil, "", freqAMSATCache, freqplan.DefaultAMSATTTL); err != nil {
		slog.WarnContext(ctx, "freqplan: amsat", slogKeyError, err)
	} else {
		sources = append(sources, amsat)
	}
	entries := freqplan.Merge(passes, sources)
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

// onSweepDwellEnd пишет журнал визитов: план × обнаружения в закончившемся окне.
func (a *app) onSweepDwellEnd(pathID string, w sweep.Window, startMs, endMs int64) {
	a.freqPlanMu.Lock()
	plan := append([]freqplan.Entry(nil), a.freqPlan...)
	a.freqPlanMu.Unlock()
	if len(plan) == 0 {
		if loaded, err := freqplan.ReadPlan(freqPlanPath); err == nil {
			plan = loaded
		}
	}
	var sights []freqplan.Sighting
	if a.bandscan != nil {
		for _, s := range a.bandscan.SignalsInBand(pathID, w.CenterHz, w.SpanHz) {
			sights = append(sights, freqplan.Sighting{
				CenterHz: s.CenterHz, NoradID: s.NoradID,
				Origin: s.Origin, Emission: s.Emission, SNRDB: s.SNRDB,
			})
		}
	}
	visits := freqplan.Judge(pathID, freqplan.Window{CenterHz: w.CenterHz, SpanHz: w.SpanHz},
		startMs, endMs, plan, sights)
	if err := freqplan.AppendVisits(freqVisitsPath, visits); err != nil {
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

// satnogsFreqSource — активные downlink SatNOGS как Source плана частот.
func satnogsFreqSource(p services.TransmitterProvider) freqplan.Source {
	return freqplan.ProviderFunc(func(noradID int) []freqplan.Tx {
		list := p.GetTransmitters(noradID)
		if len(list) == 0 {
			return nil
		}
		out := make([]freqplan.Tx, 0, len(list))
		for _, tx := range list {
			if tx.FreqHz <= 0 {
				continue
			}
			out = append(out, freqplan.Tx{
				UUID: tx.UUID, Name: tx.Description, FreqHz: tx.FreqHz,
				Mode: firstNonEmpty(tx.Mode, tx.Modulation), Baud: tx.Baud,
				Source: freqplan.SourceSatNOGS,
			})
		}
		return out
	})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
