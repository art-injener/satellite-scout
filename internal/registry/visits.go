package registry

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/art-injener/satellite-scout/internal/freqplan"
)

// AppendVisits дописывает визиты стоянки в журнал. Пустой список — ничего не делает.
// Визиты с energy=true помечают NORAD как «слышали» (в памяти и, если есть база, на диске).
func (r *Registry) AppendVisits(ctx context.Context, visits []freqplan.Visit) error {
	if len(visits) == 0 {
		return nil
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	r.mu.Lock()
	for _, v := range visits {
		if v.Energy && v.NoradID > 0 {
			r.heard[v.NoradID] = struct{}{}
		}
	}
	r.mu.Unlock()

	if r.store == nil {
		return nil
	}
	return r.store.appendVisits(ctx, visits)
}

// ListVisits читает журнал визитов по порядку записи. Без базы — nil.
func (r *Registry) ListVisits(ctx context.Context) ([]freqplan.Visit, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.store == nil {
		return nil, nil
	}
	return r.store.listVisits(ctx)
}

func (s *store) appendVisits(ctx context.Context, visits []freqplan.Visit) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO rx_visits
			(radio_path, center_hz, span_hz, window_start_ms, window_end_ms,
			 norad_id, name, uuid, freq_hz, energy, snr_db, origin, emission, received, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, v := range visits {
			if _, err = stmt.ExecContext(ctx,
				v.RadioPath, v.CenterHz, v.SpanHz, v.WindowStartMs, v.WindowEndMs,
				v.NoradID, v.Name, v.UUID, v.FreqHz, boolInt(v.Energy), float64(v.SNRDB),
				v.Origin, v.Emission, boolInt(v.Received), v.Source,
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *store) listVisits(ctx context.Context) ([]freqplan.Visit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT radio_path, center_hz, span_hz, window_start_ms, window_end_ms,
		norad_id, name, uuid, freq_hz, energy, snr_db, origin, emission, received, source
		FROM rx_visits ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("registry: list visits: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []freqplan.Visit
	for rows.Next() {
		var (
			v          freqplan.Visit
			energy     int
			received   int
			snr        float64
			center, hz int64
			span       int64
		)
		if err = rows.Scan(
			&v.RadioPath, &center, &span, &v.WindowStartMs, &v.WindowEndMs,
			&v.NoradID, &v.Name, &v.UUID, &hz, &energy, &snr, &v.Origin, &v.Emission, &received, &v.Source,
		); err != nil {
			return nil, fmt.Errorf("registry: scan visit: %w", err)
		}
		v.CenterHz = uint64(center)
		v.SpanHz = uint32(span)
		v.FreqHz = hz
		v.Energy = energy != 0
		v.Received = received != 0
		v.SNRDB = float32(snr)
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *store) loadHeard(ctx context.Context, r *Registry) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT norad_id FROM rx_visits WHERE energy != 0`)
	if err != nil {
		return fmt.Errorf("registry: load heard: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var norad int
		if err = rows.Scan(&norad); err != nil {
			return fmt.Errorf("registry: scan heard: %w", err)
		}
		if norad > 0 {
			r.heard[norad] = struct{}{}
		}
	}
	return rows.Err()
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
