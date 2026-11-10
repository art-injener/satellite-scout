package freqplan

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

var visitsHeader = []string{
	"radio_path", "center_hz", "span_hz", "window_start_ms", "window_end_ms",
	"norad_id", "name", "uuid", "freq_hz",
	"energy", "origin", "emission", "received", "source",
}

// AppendVisits дописывает визиты в CSV; пустой список — ничего не делает.
// Если файла нет — пишет заголовок.
func AppendVisits(path string, visits []Visit) error {
	if len(visits) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("freqplan: visits mkdir: %w", err)
	}
	_, statErr := os.Stat(path)
	needHeader := os.IsNotExist(statErr)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("freqplan: visits open: %w", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if needHeader {
		if err := w.Write(visitsHeader); err != nil {
			return fmt.Errorf("freqplan: visits header: %w", err)
		}
	}
	for _, v := range visits {
		row := []string{
			v.RadioPath,
			strconv.FormatUint(v.CenterHz, 10),
			strconv.FormatUint(uint64(v.SpanHz), 10),
			strconv.FormatInt(v.WindowStartMs, 10),
			strconv.FormatInt(v.WindowEndMs, 10),
			strconv.Itoa(v.NoradID),
			v.Name,
			v.UUID,
			strconv.FormatInt(v.FreqHz, 10),
			strconv.FormatBool(v.Energy),
			v.Origin,
			v.Emission,
			strconv.FormatBool(v.Received),
			v.Source,
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("freqplan: visits row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("freqplan: visits flush: %w", err)
	}
	return nil
}

// ReadVisits читает журнал. Нет файла — nil без ошибки.
func ReadVisits(path string) ([]Visit, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("freqplan: visits open: %w", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.ReuseRecord = true
	header, err := r.Read()
	if err == io.EOF {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("freqplan: visits header: %w", err)
	}
	if len(header) < len(visitsHeader) {
		return nil, fmt.Errorf("freqplan: visits bad header columns %d", len(header))
	}
	var out []Visit
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("freqplan: visits row: %w", err)
		}
		v, err := parseVisitRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func parseVisitRow(row []string) (Visit, error) {
	if len(row) < len(visitsHeader) {
		return Visit{}, fmt.Errorf("freqplan: visits short row %d", len(row))
	}
	center, err := strconv.ParseUint(row[1], 10, 64)
	if err != nil {
		return Visit{}, fmt.Errorf("freqplan: visits center: %w", err)
	}
	span64, err := strconv.ParseUint(row[2], 10, 32)
	if err != nil {
		return Visit{}, fmt.Errorf("freqplan: visits span: %w", err)
	}
	start, err := strconv.ParseInt(row[3], 10, 64)
	if err != nil {
		return Visit{}, fmt.Errorf("freqplan: visits start: %w", err)
	}
	end, err := strconv.ParseInt(row[4], 10, 64)
	if err != nil {
		return Visit{}, fmt.Errorf("freqplan: visits end: %w", err)
	}
	norad, err := strconv.Atoi(row[5])
	if err != nil {
		return Visit{}, fmt.Errorf("freqplan: visits norad: %w", err)
	}
	freq, err := strconv.ParseInt(row[8], 10, 64)
	if err != nil {
		return Visit{}, fmt.Errorf("freqplan: visits freq: %w", err)
	}
	energy, err := strconv.ParseBool(row[9])
	if err != nil {
		return Visit{}, fmt.Errorf("freqplan: visits energy: %w", err)
	}
	received, err := strconv.ParseBool(row[12])
	if err != nil {
		return Visit{}, fmt.Errorf("freqplan: visits received: %w", err)
	}
	return Visit{
		RadioPath: row[0], CenterHz: center, SpanHz: uint32(span64),
		WindowStartMs: start, WindowEndMs: end,
		NoradID: norad, Name: row[6], UUID: row[7], FreqHz: freq,
		Energy: energy, Origin: row[10], Emission: row[11],
		Received: received, Source: row[13],
	}, nil
}
