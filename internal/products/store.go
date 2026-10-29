// Package products хранит продукты приёма — картинки, собранные демодулятором:
// пока картинка проявляется, она в памяти, готовая сохраняется в PNG.
package products

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// KindSSTV — картинка SSTV.
const KindSSTV = "sstv"

// maxProducts — сколько последних продуктов помнит список.
const maxProducts = 50

// idTime — время начала в имени продукта и файла.
const idTime = "20060102T150405Z"

// ErrUnknown — продукта с таким идентификатором нет.
var ErrUnknown = errors.New("unknown product")

// Meta — откуда продукт: тракт, вид, режим и частота канала.
type Meta struct {
	RadioPath   string `json:"radio_path,omitempty"`
	Kind        string `json:"kind"`
	Mode        string `json:"mode"`
	ModeName    string `json:"mode_name,omitempty"`
	FrequencyHz uint64 `json:"frequency_hz,omitempty"`
}

// Product — продукт для списка «Снимки» и SSE `product`.
type Product struct {
	Meta
	ID         string `json:"id"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Lines      int    `json:"lines"` // проявившиеся строки
	Done       bool   `json:"done"`
	StartedMs  uint64 `json:"started_ms"`
	FinishedMs uint64 `json:"finished_ms,omitempty"`
}

type entry struct {
	Product
	img *image.RGBA // своя копия, пока картинка проявляется; у готовой — файл
}

// Store — продукты в памяти и PNG в каталоге. Потокобезопасен: пишет горутина
// демодулятора, читают обработчики HTTP.
type Store struct {
	dir string
	now func() time.Time

	mu      sync.Mutex
	entries []*entry // от старых к новым
}

// NewStore создаёт каталог при необходимости и поднимает сохранённые картинки.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("products dir: %w", err)
	}
	s := &Store{dir: dir, now: time.Now}
	files, err := filepath.Glob(filepath.Join(dir, "*.png"))
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	for _, f := range files {
		if e, ok := loadSaved(f); ok {
			s.push(e)
		}
	}
	return s, nil
}

// loadSaved восстанавливает продукт по имени файла «время-режим.png» и размеру картинки.
func loadSaved(path string) (*entry, bool) {
	id := strings.TrimSuffix(filepath.Base(path), ".png")
	stamp, mode, ok := strings.Cut(id, "-")
	if !ok {
		return nil, false
	}
	started, err := time.Parse(idTime, stamp)
	if err != nil {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return nil, false
	}
	ms := uint64(started.UnixMilli())
	return &entry{Product: Product{
		Meta: Meta{Kind: KindSSTV, Mode: mode}, ID: id, Width: cfg.Width, Height: cfg.Height,
		Lines: cfg.Height, Done: true, StartedMs: ms, FinishedMs: ms,
	}}, true
}

// Begin заводит продукт, картинка которого начала проявляться.
func (s *Store) Begin(m Meta, img *image.RGBA) Product {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	id := now.Format(idTime) + "-" + m.Mode
	for n := 2; s.find(id) != nil; n++ {
		id = fmt.Sprintf("%s-%s_%d", now.Format(idTime), m.Mode, n)
	}
	b := img.Bounds()
	e := &entry{
		Product: Product{Meta: m, ID: id, Width: b.Dx(), Height: b.Dy(), StartedMs: uint64(now.UnixMilli())},
		img:     clone(img),
	}
	s.push(e)
	return e.Product
}

// Update запоминает проявившиеся строки; неизвестный продукт — пустой.
func (s *Store) Update(id string, lines int, img *image.RGBA) Product {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.find(id)
	if e == nil || e.img == nil {
		return Product{}
	}
	draw.Draw(e.img, e.img.Bounds(), img, img.Bounds().Min, draw.Src)
	e.Lines = lines
	return e.Product
}

// Finish сохраняет итоговую картинку в PNG и отпускает копию в памяти. Без картинки
// (приём оборвался) сохраняется проявившаяся часть, и строк остаётся сколько было.
func (s *Store) Finish(id string, img *image.RGBA) (Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.find(id)
	if e == nil || e.img == nil {
		return Product{}, fmt.Errorf("%w: %q", ErrUnknown, id)
	}
	lines := e.Height
	if img == nil {
		img, lines = e.img, e.Lines
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return Product{}, err
	}
	if err := os.WriteFile(filepath.Join(s.dir, id+".png"), buf.Bytes(), 0o644); err != nil {
		return Product{}, fmt.Errorf("save product: %w", err)
	}
	e.img = nil
	e.Lines, e.Done = lines, true
	e.FinishedMs = uint64(s.now().UnixMilli())
	return e.Product, nil
}

// List — продукты от новых к старым.
func (s *Store) List() []Product {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Product, 0, len(s.entries))
	for i := len(s.entries) - 1; i >= 0; i-- {
		out = append(out, s.entries[i].Product)
	}
	return out
}

// PNG — картинка продукта: проявляющаяся — как есть сейчас, готовая — из файла.
func (s *Store) PNG(id string) ([]byte, bool) {
	s.mu.Lock()
	e := s.find(id)
	if e == nil {
		s.mu.Unlock()
		return nil, false
	}
	if e.img == nil {
		s.mu.Unlock()
		b, err := os.ReadFile(filepath.Join(s.dir, id+".png"))
		return b, err == nil
	}
	img := clone(e.img)
	s.mu.Unlock()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

func (s *Store) find(id string) *entry {
	for _, e := range s.entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func (s *Store) push(e *entry) {
	s.entries = append(s.entries, e)
	if len(s.entries) > maxProducts {
		s.entries = slices.Delete(s.entries, 0, len(s.entries)-maxProducts)
	}
}

func clone(img *image.RGBA) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}
