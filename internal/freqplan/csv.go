package freqplan

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Заголовок freq_plan.csv.
var planHeader = []string{
	"norad_id", "name", "uuid", "freq_hz", "mode", "baud",
	"aos_ms", "los_ms", "source", "updated_ms",
}

// WritePlan атомарно пишет план в CSV (tmp + rename).
func WritePlan(path string, entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("freqplan: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("freqplan: create: %w", err)
	}
	w := csv.NewWriter(f)
	if err := w.Write(planHeader); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("freqplan: header: %w", err)
	}
	now := time.Now().UnixMilli()
	for _, e := range entries {
		updated := e.UpdatedMs
		if updated == 0 {
			updated = now
		}
		row := []string{
			strconv.Itoa(e.NoradID),
			e.Name,
			e.UUID,
			strconv.FormatInt(e.FreqHz, 10),
			e.Mode,
			formatBaud(e.Baud),
			strconv.FormatInt(e.AOS, 10),
			strconv.FormatInt(e.LOS, 10),
			e.Source,
			strconv.FormatInt(updated, 10),
		}
		if err := w.Write(row); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("freqplan: row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("freqplan: flush: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("freqplan: close: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("freqplan: rename: %w", err)
	}
	return nil
}

// ReadPlan читает план из CSV. Нет файла — nil без ошибки.
func ReadPlan(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("freqplan: open: %w", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.ReuseRecord = true
	header, err := r.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("freqplan: header: %w", err)
	}
	if len(header) < len(planHeader) {
		return nil, fmt.Errorf("freqplan: bad header columns %d", len(header))
	}
	var out []Entry
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("freqplan: row: %w", err)
		}
		e, err := parsePlanRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func parsePlanRow(row []string) (Entry, error) {
	if len(row) < len(planHeader) {
		return Entry{}, fmt.Errorf("freqplan: short row %d", len(row))
	}
	norad, err := strconv.Atoi(row[0])
	if err != nil {
		return Entry{}, fmt.Errorf("freqplan: norad: %w", err)
	}
	freq, err := strconv.ParseInt(row[3], 10, 64)
	if err != nil {
		return Entry{}, fmt.Errorf("freqplan: freq: %w", err)
	}
	baud, err := parseBaud(row[5])
	if err != nil {
		return Entry{}, err
	}
	aos, err := strconv.ParseInt(row[6], 10, 64)
	if err != nil {
		return Entry{}, fmt.Errorf("freqplan: aos: %w", err)
	}
	los, err := strconv.ParseInt(row[7], 10, 64)
	if err != nil {
		return Entry{}, fmt.Errorf("freqplan: los: %w", err)
	}
	updated, err := strconv.ParseInt(row[9], 10, 64)
	if err != nil {
		return Entry{}, fmt.Errorf("freqplan: updated: %w", err)
	}
	return Entry{
		NoradID: norad, Name: row[1], UUID: row[2], FreqHz: freq,
		Mode: row[4], Baud: baud, AOS: aos, LOS: los,
		Source: row[8], UpdatedMs: updated,
	}, nil
}

func formatBaud(b float64) string {
	if b == 0 {
		return ""
	}
	return strconv.FormatFloat(b, 'f', -1, 64)
}

func parseBaud(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("freqplan: baud: %w", err)
	}
	return v, nil
}
