package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/art-injener/satellite-scout/internal/bandscan"
	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/rfprofile"
	"github.com/art-injener/satellite-scout/internal/services"
	"github.com/art-injener/satellite-scout/internal/signalclass"
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
	// План обогащает ленту панорамы и ожидаемые TX из реестра.
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

// wireFreqPlanProvider — GetTransmitters = реестр ∪ план частот (не live SatNOGS).
func (a *app) wireFreqPlanProvider() {
	if a.tracking == nil {
		return
	}
	a.tracking.SetTransmitterProvider(&planAugment{
		base: newRegistryTXSource(a.registry).asServices(),
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

// planAllow — полоса приёмника ∩ антенны.
func (a *app) planAllow(hz int64) bool {
	if !a.bands.Allow(hz) {
		return false
	}
	if len(a.antennaBands) > 0 && !a.antennaBands.Allow(hz) {
		return false
	}
	return true
}

// planSweepFreqs — частоты плана в полосе приёмника ∩ антенны на горизонте обхода.
func (a *app) planSweepFreqs(_ string, now, until time.Time) []uint64 {
	return freqplan.ActiveFreqs(a.loadFreqPlan(), now.UnixMilli(), until.UnixMilli(), a.planAllow)
}

// planSweepTargets — TX плана для политики tx (Авто приём).
// Состав как у ленты «видимых» КА: только NORAD активной группы скользящего окна.
func (a *app) planSweepTargets(_ string, now, until time.Time) []sweep.Target {
	targets := freqplan.ActiveTargets(a.loadFreqPlan(), now.UnixMilli(), until.UnixMilli(), a.planAllow)
	if a.tracking != nil {
		ids := a.tracking.GroupNoradIDs()
		if len(ids) > 0 {
			allow := make(map[int]struct{}, len(ids))
			for _, id := range ids {
				allow[id] = struct{}{}
			}
			targets = freqplan.FilterTargetsByNorad(targets, allow)
		}
	}
	targets = freqplan.PrioritizeTargets(targets, a.rfHeardMs)
	out := make([]sweep.Target, len(targets))
	for i, t := range targets {
		out[i] = sweep.Target{FreqHz: t.FreqHz, Label: t.Name, NoradID: t.NoradID}
	}
	return out
}

// rfHeardMs — последняя энергия/кадр в радиопрофиле; 0 — молчит или нет профиля.
func (a *app) rfHeardMs(norad int, freqHz uint64) int64 {
	if a.registry == nil || norad <= 0 {
		return 0
	}
	p, ok := a.registry.RFProfile(norad)
	if !ok {
		return 0
	}
	return rfprofile.LastHeardAt(p, int64(freqHz)) //nolint:gosec // частоты TX < MaxInt64
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
		sight := freqplan.Sighting{
			CenterHz: s.CenterHz, NoradID: s.NoradID,
			Origin: s.Origin, Emission: s.Emission, SNRDB: s.SNRDB,
			LastSeenMs:    int64(s.LastSeenMs), //nolint:gosec // метки времени bandscan < MaxInt64
			BandwidthHz:   s.BandwidthHz,
			BeaconPeriodS: s.Stats.PeriodS,
		}
		if s.Structure != nil {
			sight.Modulation = s.Structure.Modulation
			sight.Baud = s.Structure.Params.Baud
			sight.Confidence = s.Structure.Confidence
			sight.Demod = s.Structure.Demod
			if len(s.Structure.DemodAlt) > 0 {
				sight.DemodAlt = append([]string(nil), s.Structure.DemodAlt...)
			}
		}
		sights = append(sights, sight)
	}
	return sights
}

// enrichSightingsFromProfiles подставляет период/режим из радиопрофиля, если эфир ещё молчит.
func (a *app) enrichSightingsFromProfiles(sights []freqplan.Sighting) {
	if a.registry == nil {
		return
	}
	for i := range sights {
		s := &sights[i]
		if s.NoradID <= 0 {
			continue
		}
		p, ok := a.registry.RFProfile(s.NoradID)
		if !ok {
			continue
		}
		tx, ok := rfprofile.FindTx(p, "", s.CenterHz)
		if !ok {
			continue
		}
		eff := rfprofile.Effective(tx)
		if s.BeaconPeriodS <= 0 {
			if tx.Detected.BeaconPeriodS > 0 {
				s.BeaconPeriodS = tx.Detected.BeaconPeriodS
			} else if eff.BeaconPeriodS > 0 {
				s.BeaconPeriodS = eff.BeaconPeriodS
			}
		}
		if s.Modulation == "" && eff.Modulation != "" {
			s.Modulation = eff.Modulation
			s.Baud = eff.Baud
		}
	}
}

// sweepDwell — гибкая стоянка: план × обнаружения → DecideHold.
// Окно «+эфир» (Air) — отдельная политика пробы без TX плана.
func (a *app) sweepDwell(pathID string, w sweep.Window, startMs, nowMs, configDwellMs int64) int64 {
	sights := a.bandSightings(pathID, w)
	a.enrichSightingsFromProfiles(sights)
	fw := freqplan.Window{CenterHz: w.CenterHz, SpanHz: w.SpanHz}
	if w.Air {
		return freqplan.DecideHoldAir(fw, startMs, nowMs, sights)
	}
	return freqplan.DecideHold(fw, startMs, nowMs, configDwellMs, a.loadFreqPlan(), sights)
}

// sweepAim — наведение OffsetHz на пик вспышки на тике снимка обхода (~4 Гц).
func (a *app) sweepAim(pathID string, w sweep.Window, startMs, nowMs int64) {
	sights := a.bandSightings(pathID, w)
	a.enrichSightingsFromProfiles(sights)
	a.maybeAimHoldChannel(pathID, w, startMs, nowMs, sights)
}

// airSweepTargets — hop «+эфир»: импульсы/неопознанное из памяти bandscan.
// Немодулированные несущие и земной continuous в план не берём — кадров нет, только крадут стоянку.
func (a *app) airSweepTargets(pathID string) []sweep.Target {
	if a.bandscan == nil {
		return nil
	}
	sigs := a.bandscan.RememberedSignals(pathID)
	out := make([]sweep.Target, 0, len(sigs))
	for _, s := range sigs {
		if !airHopWorthy(s) {
			continue
		}
		t := sweep.Target{
			FreqHz: uint64(s.CenterHz), //nolint:gosec // частоты эфира > 0
			Label:  "Эфир",
			Air:    true,
		}
		if s.Structure != nil {
			t.Demod = airLineDemod(s.Structure.Demod)
			t.Baud = s.Structure.Params.Baud
			t.Label = airHopLabel(s)
		} else if s.Emission == bandscan.EmissionPulsed {
			t.Label = "Имп."
		}
		out = append(out, t)
	}
	return out
}

// airHopWorthy — участок стоит hop: СШ и не «пустая» несущая/вещалка.
func airHopWorthy(s bandscan.Signal) bool {
	if s.SNRDB < freqplan.MinSNRDB || s.CenterHz <= 0 {
		return false
	}
	if s.Emission == bandscan.EmissionContinuous {
		return false
	}
	if s.Origin == bandscan.OriginTerrestrial {
		return false
	}
	if st := s.Structure; st != nil {
		mod := strings.ToLower(strings.TrimSpace(st.Modulation))
		if mod == "carrier" || mod == "wfm" {
			return false
		}
		if strings.Contains(st.ServiceLabel, signalclass.LabelBroadcast) {
			return false
		}
	}
	return true
}

// airLineDemod — для эфира без КА: HDLC/G3RUH не даёт CRC → линия + burst.
func airLineDemod(id string) string {
	switch id {
	case "fsk9600_g3ruh", "fsk9600_geoscan", "fsk9600_ax100_rs",
		"gfsk9600", "gmsk9600", "fsk4800", "fsk4800_ax100", "gfsk4800", "gmsk4800",
		"fsk1200", "gfsk1200":
		return "fsk9600" // baud подставит классификатор
	default:
		return id
	}
}

func airHopLabel(s bandscan.Signal) string {
	st := s.Structure
	if st == nil {
		return "Эфир"
	}
	mod := strings.ToUpper(st.Modulation)
	if mod == "" {
		mod = "Эфир"
	}
	if st.Params.Baud > 0 {
		return fmt.Sprintf("%s %.0f", mod, st.Params.Baud)
	}
	return mod
}

// AirHint — подсказка «Авто» с ближайшего участка эфира (классификатор).
func (a *app) AirHint(pathID string, freqHz uint64) (demod string, alt []string, mode string, baud float64, ok bool) {
	if a.bandscan == nil {
		return "", nil, "", 0, false
	}
	const matchHz = 25_000
	var best bandscan.Signal
	found := false
	for _, s := range a.bandscan.RememberedSignals(pathID) {
		if s.CenterHz <= 0 {
			continue
		}
		d := absU64Hz(uint64(s.CenterHz), freqHz) //nolint:gosec // CenterHz > 0
		if d > matchHz || s.Structure == nil {
			continue
		}
		if !found || s.SNRDB > best.SNRDB {
			best, found = s, true
		}
	}
	if !found || best.Structure == nil {
		return "", nil, "", 0, false
	}
	st := best.Structure
	demod = airLineDemod(st.Demod)
	alt = append([]string(nil), st.DemodAlt...)
	for i := range alt {
		alt[i] = airLineDemod(alt[i])
	}
	return demod, alt, st.Modulation, st.Params.Baud, true
}

func absU64Hz(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return b - a
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
	ctx := context.Background()
	if err := a.registry.AppendVisits(ctx, visits); err != nil {
		slog.Warn("freqplan: visits", slogKeyError, err)
		return
	}
	if err := a.registry.ApplyRFDwell(ctx, visits, sights); err != nil {
		slog.Warn("freqplan: rf profile dwell", slogKeyError, err)
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
