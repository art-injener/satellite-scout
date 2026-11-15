package registry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // драйвер SQLite без cgo
)

const (
	sqliteDriver = "sqlite"

	// Ожидание занятой базы и журнал WAL: чтение не ждёт записи.
	sqlitePragmas = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"

	schemaVersion = 2
)

// Время реестра — наносекунды Unix, нулевое время — 0. Наносекунды сохраняют эпоху TLE без округления.
const schemaV1 = `
CREATE TABLE IF NOT EXISTS satellites (
	norad      INTEGER NOT NULL,
	source     TEXT    NOT NULL,
	name       TEXT    NOT NULL DEFAULT '',
	status     TEXT    NOT NULL DEFAULT '',
	call_sign  TEXT    NOT NULL DEFAULT '',
	operator   TEXT    NOT NULL DEFAULT '',
	launched   TEXT    NOT NULL DEFAULT '',
	decay_ns   INTEGER NOT NULL DEFAULT 0,
	fetched_ns INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (norad, source)
);
CREATE TABLE IF NOT EXISTS orbits (
	norad      INTEGER PRIMARY KEY,
	name       TEXT    NOT NULL DEFAULT '',
	line1      TEXT    NOT NULL,
	line2      TEXT    NOT NULL,
	epoch_ns   INTEGER NOT NULL,
	source     TEXT    NOT NULL,
	fetched_ns INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS transmitters (
	source      TEXT    NOT NULL,
	norad       INTEGER NOT NULL,
	pos         INTEGER NOT NULL,
	uuid        TEXT    NOT NULL DEFAULT '',
	name        TEXT    NOT NULL DEFAULT '',
	downlink_hz INTEGER NOT NULL DEFAULT 0,
	uplink_hz   INTEGER NOT NULL DEFAULT 0,
	mode        TEXT    NOT NULL DEFAULT '',
	baud        REAL    NOT NULL DEFAULT 0,
	status      TEXT    NOT NULL DEFAULT '',
	alive       INTEGER,
	fetched_ns  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (source, norad, pos)
);
CREATE TABLE IF NOT EXISTS source_loads (
	source       TEXT PRIMARY KEY,
	last_ok_ns   INTEGER NOT NULL DEFAULT 0,
	last_fail_ns INTEGER NOT NULL DEFAULT 0,
	error        TEXT    NOT NULL DEFAULT ''
);
`

// schemaV2 — журнал стоянок обхода (энергия у TX без демода).
const schemaV2 = `
CREATE TABLE IF NOT EXISTS rx_visits (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	radio_path      TEXT    NOT NULL,
	center_hz       INTEGER NOT NULL,
	span_hz         INTEGER NOT NULL,
	window_start_ms INTEGER NOT NULL,
	window_end_ms   INTEGER NOT NULL,
	norad_id        INTEGER NOT NULL,
	name            TEXT    NOT NULL DEFAULT '',
	uuid            TEXT    NOT NULL DEFAULT '',
	freq_hz         INTEGER NOT NULL,
	energy          INTEGER NOT NULL DEFAULT 0,
	snr_db          REAL    NOT NULL DEFAULT 0,
	origin          TEXT    NOT NULL DEFAULT '',
	emission        TEXT    NOT NULL DEFAULT '',
	received        INTEGER NOT NULL DEFAULT 0,
	source          TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS rx_visits_norad_energy ON rx_visits (norad_id, energy);
`

// ErrSchemaNewer база создана более новой версией программы.
var ErrSchemaNewer = errors.New("registry: database schema is newer than supported")

// store сохраняет реестр в SQLite.
type store struct {
	db *sql.DB
}

func openStore(ctx context.Context, path string) (*store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("registry: mkdir: %w", err)
	}
	db, err := sql.Open(sqliteDriver, path+sqlitePragmas)
	if err != nil {
		return nil, fmt.Errorf("registry: open %s: %w", path, err)
	}
	// Один писатель: SQLite не любит параллельные транзакции записи.
	db.SetMaxOpenConns(1)
	s := &store{db: db}
	if err = s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *store) close() error {
	return s.db.Close()
}

func (s *store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("registry: schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("%w: %d > %d", ErrSchemaNewer, version, schemaVersion)
	}
	if version < 1 {
		if _, err := s.db.ExecContext(ctx, schemaV1); err != nil {
			return fmt.Errorf("registry: create schema v1: %w", err)
		}
		if err := s.setUserVersion(ctx, 1); err != nil {
			return err
		}
		version = 1
	}
	if version < 2 {
		if _, err := s.db.ExecContext(ctx, schemaV2); err != nil {
			return fmt.Errorf("registry: create schema v2: %w", err)
		}
		if err := s.setUserVersion(ctx, 2); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) setUserVersion(ctx context.Context, version int) error {
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return fmt.Errorf("registry: set schema version: %w", err)
	}
	return nil
}

// load поднимает сохранённое в ещё не опубликованный реестр.
func (s *store) load(ctx context.Context, r *Registry) error {
	if err := s.loadFacts(ctx, r); err != nil {
		return err
	}
	if err := s.loadOrbits(ctx, r); err != nil {
		return err
	}
	if err := s.loadTx(ctx, r); err != nil {
		return err
	}
	if err := s.loadLoads(ctx, r); err != nil {
		return err
	}
	return s.loadHeard(ctx, r)
}

func (s *store) loadFacts(ctx context.Context, r *Registry) error {
	rows, err := s.db.QueryContext(ctx, `SELECT norad, source, name, status, call_sign, operator, launched,
		decay_ns, fetched_ns FROM satellites`)
	if err != nil {
		return fmt.Errorf("registry: load satellites: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			f                SatFact
			decayNs, fetchNs int64
		)
		if err = rows.Scan(&f.NoradID, &f.Source, &f.Name, &f.Status, &f.CallSign, &f.Operator, &f.Launched,
			&decayNs, &fetchNs); err != nil {
			return fmt.Errorf("registry: scan satellite: %w", err)
		}
		f.Decay, f.FetchedAt = fromNs(decayNs), fromNs(fetchNs)
		r.putFactLocked(f)
	}
	return rows.Err()
}

func (s *store) loadOrbits(ctx context.Context, r *Registry) error {
	rows, err := s.db.QueryContext(ctx, `SELECT norad, name, line1, line2, epoch_ns, source, fetched_ns FROM orbits`)
	if err != nil {
		return fmt.Errorf("registry: load orbits: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			o                Orbit
			epochNs, fetchNs int64
		)
		if err = rows.Scan(&o.NoradID, &o.Name, &o.Line1, &o.Line2, &epochNs, &o.Source, &fetchNs); err != nil {
			return fmt.Errorf("registry: scan orbit: %w", err)
		}
		o.Epoch, o.FetchedAt = fromNs(epochNs), fromNs(fetchNs)
		r.orbits[o.NoradID] = o
	}
	return rows.Err()
}

func (s *store) loadTx(ctx context.Context, r *Registry) error {
	rows, err := s.db.QueryContext(ctx, `SELECT source, norad, uuid, name, downlink_hz, uplink_hz, mode, baud,
		status, alive, fetched_ns FROM transmitters ORDER BY source, norad, pos`)
	if err != nil {
		return fmt.Errorf("registry: load transmitters: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			tx      Transmitter
			alive   sql.NullBool
			fetchNs int64
		)
		if err = rows.Scan(&tx.Source, &tx.NoradID, &tx.UUID, &tx.Name, &tx.DownlinkHz, &tx.UplinkHz, &tx.Mode,
			&tx.Baud, &tx.Status, &alive, &fetchNs); err != nil {
			return fmt.Errorf("registry: scan transmitter: %w", err)
		}
		if alive.Valid {
			v := alive.Bool
			tx.Alive = &v
		}
		tx.FetchedAt = fromNs(fetchNs)
		r.setTxLocked(tx.NoradID, tx.Source, append(r.txs[tx.NoradID][tx.Source], tx))
	}
	return rows.Err()
}

func (s *store) loadLoads(ctx context.Context, r *Registry) error {
	rows, err := s.db.QueryContext(ctx, `SELECT source, last_ok_ns, last_fail_ns, error FROM source_loads`)
	if err != nil {
		return fmt.Errorf("registry: load source_loads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			l            SourceLoad
			okNs, failNs int64
		)
		if err = rows.Scan(&l.Source, &okNs, &failNs, &l.Err); err != nil {
			return fmt.Errorf("registry: scan source_load: %w", err)
		}
		l.LastOK, l.LastFail = fromNs(okNs), fromNs(failNs)
		r.loads[l.Source] = l
	}
	return rows.Err()
}

func (s *store) upsertFacts(ctx context.Context, source string, facts []SatFact) error {
	return s.writeFacts(ctx, source, facts, false)
}

func (s *store) replaceFacts(ctx context.Context, source string, facts []SatFact) error {
	return s.writeFacts(ctx, source, facts, true)
}

func (s *store) writeFacts(ctx context.Context, source string, facts []SatFact, replace bool) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if replace {
			if _, err := tx.ExecContext(ctx, `DELETE FROM satellites WHERE source = ?`, source); err != nil {
				return err
			}
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO satellites
			(norad, source, name, status, call_sign, operator, launched, decay_ns, fetched_ns)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, f := range facts {
			if _, err = stmt.ExecContext(ctx, f.NoradID, source, f.Name, f.Status, f.CallSign, f.Operator,
				f.Launched, toNs(f.Decay), toNs(f.FetchedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

// replaceTx заменяет передатчики источника: norads == nil значит весь источник, иначе только эти NORAD.
func (s *store) replaceTx(ctx context.Context, source string, norads []int, byNorad map[int][]Transmitter) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := deleteTx(ctx, tx, source, norads); err != nil {
			return err
		}
		return insertTx(ctx, tx, source, byNorad)
	})
}

func deleteTx(ctx context.Context, tx *sql.Tx, source string, norads []int) error {
	if norads == nil {
		_, err := tx.ExecContext(ctx, `DELETE FROM transmitters WHERE source = ?`, source)
		return err
	}
	for _, id := range norads {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM transmitters WHERE source = ? AND norad = ?`, source, id); err != nil {
			return err
		}
	}
	return nil
}

// insertTx пишет передатчики с номером позиции: порядок внутри КА переживает перезапуск.
func insertTx(ctx context.Context, tx *sql.Tx, source string, byNorad map[int][]Transmitter) error {
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO transmitters
		(source, norad, pos, uuid, name, downlink_hz, uplink_hz, mode, baud, status, alive, fetched_ns)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for id, list := range byNorad {
		for pos, t := range list {
			var alive any
			if t.Alive != nil {
				alive = *t.Alive
			}
			if _, err = stmt.ExecContext(ctx, source, id, pos, t.UUID, t.Name, t.DownlinkHz, t.UplinkHz, t.Mode,
				t.Baud, t.Status, alive, toNs(t.FetchedAt)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *store) putOrbits(ctx context.Context, orbits []Orbit) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO orbits
			(norad, name, line1, line2, epoch_ns, source, fetched_ns) VALUES (?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, o := range orbits {
			if _, err = stmt.ExecContext(ctx, o.NoradID, o.Name, o.Line1, o.Line2, toNs(o.Epoch), o.Source,
				toNs(o.FetchedAt)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *store) putLoad(ctx context.Context, l SourceLoad) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO source_loads (source, last_ok_ns, last_fail_ns, error)
		VALUES (?, ?, ?, ?)`, l.Source, toNs(l.LastOK), toNs(l.LastFail), l.Err)
	if err != nil {
		return fmt.Errorf("registry: save source_load: %w", err)
	}
	return nil
}

func (s *store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("registry: begin: %w", err)
	}
	if err = fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			err = errors.Join(err, rbErr)
		}
		return fmt.Errorf("registry: write: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("registry: commit: %w", err)
	}
	return nil
}

func toNs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func fromNs(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}
