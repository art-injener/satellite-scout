package registry

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// IngestCatalogPass принимает итог прохода каталога: элементы GP, каталог sonik и SATCAT.
// Источник, который в этом проходе не ответил, оставляет в реестре прежний снимок.
func (r *Registry) IngestCatalogPass(ctx context.Context, p tracker.CatalogPass) error {
	var errs []error
	keep := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	switch {
	case p.Err != nil:
		keep(r.MarkLoad(ctx, SourceSpaceTrack, p.At, p.Err))
	case len(p.TLEs) > 0:
		keep(r.PutOrbits(ctx, SourceSpaceTrack, p.TLEs, p.At))
		keep(r.MarkLoad(ctx, SourceSpaceTrack, p.At, nil))
	}

	switch {
	case p.SonikErr != nil:
		keep(r.MarkLoad(ctx, SourceSonik, p.At, p.SonikErr))
	case len(p.Sonik) > 0:
		keep(r.ReplaceSatFacts(ctx, SourceSonik, fromSonikSats(p.Sonik), p.At))
		keep(r.MarkLoad(ctx, SourceSonik, p.At, nil))
	}

	switch {
	case p.Err != nil:
		// Без GP запрос SATCAT не выполнялся.
	case p.SatcatErr != nil:
		// Полученные пачки сохраняем, даже если хвост прохода не ответил.
		if len(p.Satcat) > 0 {
			keep(r.UpsertSatFacts(ctx, SourceSatcat, fromSatcat(p.Satcat), p.At))
		}
		keep(r.MarkLoad(ctx, SourceSatcat, p.At, p.SatcatErr))
	case len(p.Satcat) > 0:
		keep(r.ReplaceSatFacts(ctx, SourceSatcat, fromSatcat(p.Satcat), p.At))
		keep(r.MarkLoad(ctx, SourceSatcat, p.At, nil))
	}
	return errors.Join(errs...)
}

// FromSatnogs переводит передатчики SatNOGS одного КА в записи реестра, по возрастанию частоты вниз.
func FromSatnogs(norad int, list []satnogs.Transmitter, at time.Time) []Transmitter {
	out := make([]Transmitter, 0, len(list))
	for i := range list {
		t := &list[i]
		out = append(out, Transmitter{
			UUID: t.UUID, NoradID: norad, Name: t.Description,
			DownlinkHz: deref(t.DownlinkLow), UplinkHz: deref(t.UplinkLow),
			Mode: t.Mode, Baud: deref(t.Baud), Status: t.Status, Alive: new(t.Alive),
			Source: SourceSatNOGS, FetchedAt: at,
		})
	}
	slices.SortStableFunc(out, func(a, b Transmitter) int { return cmp.Compare(a.DownlinkHz, b.DownlinkHz) })
	return out
}

// FromFreqplan переводит передатчики curated, Heavens-Above или AMSAT в записи реестра.
func FromFreqplan(byNorad map[int][]freqplan.Tx, at time.Time) []Transmitter {
	ids := make([]int, 0, len(byNorad))
	for id := range byNorad {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var out []Transmitter
	for _, id := range ids {
		for _, tx := range byNorad[id] {
			out = append(out, Transmitter{
				UUID: tx.UUID, NoradID: id, Name: tx.Name, DownlinkHz: tx.FreqHz,
				Mode: tx.Mode, Baud: tx.Baud, Source: tx.Source, FetchedAt: at,
			})
		}
	}
	return out
}

func fromSonikSats(sats []tracker.SonikSat) []SatFact {
	out := make([]SatFact, 0, len(sats))
	for _, s := range sats {
		out = append(out, SatFact{
			NoradID: s.NoradID, Name: s.Name, Status: s.Status,
			CallSign: s.CallSign, Operator: s.Operator, Launched: s.Launched,
		})
	}
	return out
}

func fromSatcat(recs []tracker.SatcatRecord) []SatFact {
	out := make([]SatFact, 0, len(recs))
	for _, r := range recs {
		f := SatFact{NoradID: r.NoradID, Decay: r.Decay}
		if !r.Decay.IsZero() {
			f.Status = StatusReEntered
		}
		out = append(out, f)
	}
	return out
}

func deref[T int64 | float64](p *T) T {
	if p == nil {
		return 0
	}
	return *p
}
