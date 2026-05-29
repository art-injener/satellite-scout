// Package exclude хранит набор NORAD ID, исключённых из группы активных спутников
// и из списка пролётов. Источник истины — текстовый файл (один NORAD на строку,
// «#» — комментарий). Изменения из UI дописываются в файл, чтобы пережить рестарт.
package exclude

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// fileHeader — шапка файла исключений при полной перезаписи.
const fileHeader = "# Список исключённых NORAD ID. Один ID на строку, «#» — комментарий.\n"

// Store — потокобезопасный набор исключённых NORAD ID.
type Store struct {
	mu       sync.RWMutex
	set      map[int]struct{}
	filePath string
}

// NewStore создаёт набор исключений из текстового файла.
// Пустой путь — работа только в памяти (без записи на диск).
// Отсутствие файла на диске не считается ошибкой: набор стартует пустым.
func NewStore(filePath string) *Store {
	s := &Store{
		set:      make(map[int]struct{}),
		filePath: filePath,
	}
	if filePath != "" {
		// Отсутствие файла — нормальная ситуация, игнорируем ошибку открытия.
		_ = s.loadFile()
	}
	return s
}

// Contains сообщает, исключён ли спутник.
func (s *Store) Contains(norad int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.set[norad]
	return ok
}

// List возвращает отсортированный список исключённых NORAD ID.
func (s *Store) List() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

// Add добавляет спутник в исключения и дописывает строку в файл.
// Повторное добавление — без эффекта.
func (s *Store) Add(norad int) error {
	if norad <= 0 {
		return fmt.Errorf("norad must be positive, got %d", norad)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.set[norad]; ok {
		return nil
	}
	s.set[norad] = struct{}{}
	if s.filePath == "" {
		return nil
	}
	return s.appendLine(norad)
}

// Remove убирает спутник из исключений и перезаписывает файл.
// Удаление отсутствующего — без эффекта.
func (s *Store) Remove(norad int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.set[norad]; !ok {
		return nil
	}
	delete(s.set, norad)
	if s.filePath == "" {
		return nil
	}
	return s.saveAllLocked()
}

// listLocked возвращает отсортированный список; вызывается под захваченным mu.
func (s *Store) listLocked() []int {
	ids := make([]int, 0, len(s.set))
	for id := range s.set {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// loadFile читает исключения из файла, пропуская комментарии и пустые строки.
func (s *Store) loadFile() error {
	f, err := os.Open(s.filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if id, ok := parseLine(scanner.Text()); ok {
			s.set[id] = struct{}{}
		}
	}
	return scanner.Err()
}

// appendLine дописывает один NORAD в конец файла, сохраняя комментарии пользователя.
func (s *Store) appendLine(norad int) error {
	f, err := os.OpenFile(s.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "%d\n", norad)
	return err
}

// saveAllLocked полностью перезаписывает файл текущим набором; вызывается под mu.
func (s *Store) saveAllLocked() error {
	var b strings.Builder
	b.WriteString(fileHeader)
	for _, id := range s.listLocked() {
		b.WriteString(strconv.Itoa(id))
		b.WriteByte('\n')
	}
	return os.WriteFile(s.filePath, []byte(b.String()), 0o644)
}

// parseLine разбирает одну строку файла: отрезает комментарий после «#»,
// убирает пробелы. Возвращает NORAD и признак валидности.
func parseLine(line string) (int, bool) {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, false
	}
	id, err := strconv.Atoi(line)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
