package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/rfprofile"
)

// ErrInvalidRFProfile — профиль без валидного NORAD.
var ErrInvalidRFProfile = errors.New("registry: rf profile requires norad > 0")

// OnRFProfile задаёт слушателя изменений радиопрофиля (один; обычно SSE).
func (r *Registry) OnRFProfile(fn RFProfileListener) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.rfListener = fn
}

// PutRFProfile сохраняет радиопрофиль КА в памяти и, если есть база, на диске.
func (r *Registry) PutRFProfile(ctx context.Context, p rfprofile.Profile) error {
	if p.NoradID <= 0 {
		return ErrInvalidRFProfile
	}
	r.writeMu.Lock()
	r.mu.Lock()
	snap := cloneRFProfile(p)
	r.rfProfiles[p.NoradID] = snap
	r.mu.Unlock()
	var err error
	if r.store != nil {
		err = r.store.putRFProfile(ctx, p)
	}
	listener := r.rfListener
	r.writeMu.Unlock()
	if listener != nil {
		listener(snap)
	}
	return err
}

// RFProfile возвращает копию радиопрофиля КА.
func (r *Registry) RFProfile(norad int) (rfprofile.Profile, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.rfProfiles[norad]
	if !ok {
		return rfprofile.Profile{}, false
	}
	return cloneRFProfile(p), true
}

// RFProfiles возвращает копии всех радиопрофилей по возрастанию NORAD.
func (r *Registry) RFProfiles() []rfprofile.Profile {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]rfprofile.Profile, 0, len(r.rfProfiles))
	for _, p := range r.rfProfiles {
		out = append(out, cloneRFProfile(p))
	}
	slices.SortFunc(out, func(a, b rfprofile.Profile) int {
		return a.NoradID - b.NoradID
	})
	return out
}

// ApplyRFDwell обновляет радиопрофили по итогам стоянки и сохраняет их.
func (r *Registry) ApplyRFDwell(ctx context.Context, visits []freqplan.Visit, sights []freqplan.Sighting) error {
	if len(visits) == 0 {
		return nil
	}
	byNorad := make(map[int][]freqplan.Visit)
	for _, v := range visits {
		if v.NoradID <= 0 {
			continue
		}
		byNorad[v.NoradID] = append(byNorad[v.NoradID], v)
	}
	for norad, vs := range byNorad {
		p, ok := r.RFProfile(norad)
		if !ok {
			p = rfprofile.Profile{NoradID: norad}
		}
		p = rfprofile.ApplyDwell(p, vs, sights)
		if err := r.PutRFProfile(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// ApplyRFFrame фиксирует принятый кадр в радиопрофиле КА.
func (r *Registry) ApplyRFFrame(ctx context.Context, norad int, freqHz int64, demodID string, tsMs int64) error {
	if norad <= 0 || freqHz <= 0 || demodID == "" {
		return nil
	}
	p, ok := r.RFProfile(norad)
	if !ok {
		p = rfprofile.Profile{NoradID: norad}
	}
	p = rfprofile.ApplyFrame(p, "", freqHz, demodID, tsMs)
	return r.PutRFProfile(ctx, p)
}

func (s *store) putRFProfile(ctx context.Context, p rfprofile.Profile) error {
	payload, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("registry: marshal rf profile: %w", err)
	}
	updatedNs := p.UpdatedMs * int64(1_000_000)
	return s.inTx(ctx, func(tx *sql.Tx) error {
		_, err = tx.ExecContext(ctx, `INSERT INTO rf_profiles (norad, payload, updated_ns)
			VALUES (?, ?, ?)
			ON CONFLICT(norad) DO UPDATE SET payload = excluded.payload, updated_ns = excluded.updated_ns`,
			p.NoradID, string(payload), updatedNs)
		return err
	})
}

func (s *store) loadRFProfiles(ctx context.Context, r *Registry) error {
	rows, err := s.db.QueryContext(ctx, `SELECT norad, payload FROM rf_profiles`)
	if err != nil {
		return fmt.Errorf("registry: load rf profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			norad   int
			payload string
		)
		if err = rows.Scan(&norad, &payload); err != nil {
			return fmt.Errorf("registry: scan rf profile: %w", err)
		}
		var p rfprofile.Profile
		if err = json.Unmarshal([]byte(payload), &p); err != nil {
			return fmt.Errorf("registry: unmarshal rf profile %d: %w", norad, err)
		}
		p.NoradID = norad
		r.rfProfiles[norad] = p
	}
	return rows.Err()
}

func cloneRFProfile(p rfprofile.Profile) rfprofile.Profile {
	out := p
	if p.Tx != nil {
		out.Tx = make([]rfprofile.Transmitter, len(p.Tx))
		copy(out.Tx, p.Tx)
		for i := range out.Tx {
			if p.Tx[i].Detected.DemodAlt != nil {
				out.Tx[i].Detected.DemodAlt = append([]string(nil), p.Tx[i].Detected.DemodAlt...)
			}
		}
	}
	return out
}
