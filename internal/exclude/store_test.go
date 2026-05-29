package exclude

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempFile создаёт временный файл с указанным содержимым и возвращает путь.
func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "exclude_norad.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}

func TestStore_LoadFromFile(t *testing.T) {
	path := writeTempFile(t, "# header\n25544\n  43666  \n\n# comment\n47959 # GRBAlpha\n")
	s := NewStore(path)

	cases := map[int]bool{
		25544: true,
		43666: true,
		47959: true,
		11111: false,
	}
	for id, want := range cases {
		if got := s.Contains(id); got != want {
			t.Errorf("Contains(%d) = %v, want %v", id, got, want)
		}
	}
}

func TestStore_ListSorted(t *testing.T) {
	path := writeTempFile(t, "47959\n25544\n43666\n")
	s := NewStore(path)

	got := s.List()
	want := []int{25544, 43666, 47959}
	if len(got) != len(want) {
		t.Fatalf("List() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("List()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestStore_AddPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exclude_norad.txt")

	s := NewStore(path)
	if err := s.Add(25544); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if !s.Contains(25544) {
		t.Error("satellite must be excluded after Add")
	}

	// Новый Store из того же файла — исключение пережило «рестарт».
	s2 := NewStore(path)
	if !s2.Contains(25544) {
		t.Error("exclusion was not persisted to file")
	}
}

func TestStore_AddIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exclude_norad.txt")
	s := NewStore(path)

	_ = s.Add(25544)
	_ = s.Add(25544)

	if got := len(s.List()); got != 1 {
		t.Errorf("after double Add list len = %d, want 1", got)
	}
}

func TestStore_AddRejectsNonPositive(t *testing.T) {
	s := NewStore("")
	if err := s.Add(0); err == nil {
		t.Error("Add(0) must return error")
	}
	if err := s.Add(-1); err == nil {
		t.Error("Add(-1) must return error")
	}
}

func TestStore_RemovePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exclude_norad.txt")
	s := NewStore(path)

	_ = s.Add(25544)
	_ = s.Add(43666)
	if err := s.Remove(25544); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if s.Contains(25544) {
		t.Error("satellite must not be excluded after Remove")
	}
	if !s.Contains(43666) {
		t.Error("Remove must not affect other satellites")
	}

	s2 := NewStore(path)
	if s2.Contains(25544) {
		t.Error("removal was not persisted to file")
	}
	if !s2.Contains(43666) {
		t.Error("remaining exclusion must be persisted to file")
	}
}

func TestStore_NoFile(t *testing.T) {
	// Пустой путь — работаем только в памяти, без записи на диск.
	s := NewStore("")
	if err := s.Add(25544); err != nil {
		t.Fatalf("Add without file returned error: %v", err)
	}
	if !s.Contains(25544) {
		t.Error("exclusion must work in memory without a file")
	}
}

func TestStore_MissingFileIsNotError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does_not_exist.txt")
	// Не должно паниковать или падать — отсутствие файла нормально.
	s := NewStore(path)
	if got := len(s.List()); got != 0 {
		t.Errorf("list must be empty for a missing file, got %d", got)
	}
}
