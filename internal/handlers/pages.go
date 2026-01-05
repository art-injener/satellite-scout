package handlers

import (
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
)

const (
	// Константы для шаблонов.
	templateGlob     = "*.html"
	templateBaseName = "base.html"

	slogKeyError = "error"
)

// PageHandler обрабатывает рендеринг HTML страниц.
type PageHandler struct {
	templates *template.Template
	mu        sync.RWMutex
	devMode   bool
	tmplDir   string
}

// NewPageHandler создаёт новый обработчик страниц.
// Если devMode равен true, шаблоны перезагружаются при каждом запросе.
func NewPageHandler(tmplDir string, devMode bool) (*PageHandler, error) {
	h := &PageHandler{
		devMode: devMode,
		tmplDir: tmplDir,
	}

	if err := h.loadTemplates(); err != nil {
		return nil, err
	}

	return h, nil
}

func (h *PageHandler) loadTemplates() error {
	pattern := filepath.Join(h.tmplDir, "**", templateGlob)
	tmpl, err := template.ParseGlob(pattern)
	if err != nil {
		// Попытка загрузки из подкаталогов
		tmpl = template.New("")

		// Сначала загружаем layouts
		layoutPattern := filepath.Join(h.tmplDir, "layouts", templateGlob)
		tmpl, err = tmpl.ParseGlob(layoutPattern)
		if err != nil {
			return err
		}

		// Загружаем страницы
		pagesPattern := filepath.Join(h.tmplDir, "pages", templateGlob)
		tmpl, err = tmpl.ParseGlob(pagesPattern)
		if err != nil {
			return err
		}

		// Загружаем частичные шаблоны
		partialsPattern := filepath.Join(h.tmplDir, "partials", templateGlob)
		tmpl, err = tmpl.ParseGlob(partialsPattern)
		if err != nil {
			return err
		}
	}

	h.mu.Lock()
	h.templates = tmpl
	h.mu.Unlock()

	return nil
}

func (h *PageHandler) render(w http.ResponseWriter, name string, data any) {
	if h.devMode {
		if err := h.loadTemplates(); err != nil {
			slog.Error("failed to reload templates", slogKeyError, err)
			http.Error(w, "Template error", http.StatusInternalServerError)
			return
		}
	}

	h.mu.RLock()
	tmpl := h.templates
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("failed to render template", "name", name, slogKeyError, err)
		http.Error(w, "Render error", http.StatusInternalServerError)
	}
}

// PageData содержит общие данные для рендеринга страниц.
type PageData struct {
	Title     string
	ActiveTab string
}

// Index перенаправляет на страницу отслеживания.
func (h *PageHandler) Index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/tracking", http.StatusFound)
}

// Tracking рендерит страницу отслеживания (вкладка 1).
func (h *PageHandler) Tracking(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:     "Отслеживание - SatWatch",
		ActiveTab: "tracking",
	}
	h.render(w, templateBaseName, data)
}

// Receiver рендерит страницу приёмника (вкладка 2).
func (h *PageHandler) Receiver(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:     "Приёмник - SatWatch",
		ActiveTab: "receiver",
	}
	h.render(w, templateBaseName, data)
}

// Simulation рендерит страницу имитации (вкладка 3).
func (h *PageHandler) Simulation(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:     "Имитация - SatWatch",
		ActiveTab: "simulation",
	}
	h.render(w, templateBaseName, data)
}
