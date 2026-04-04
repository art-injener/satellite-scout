package handlers

import (
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
)

const (
	// Константы для шаблонов.
	templateBaseName = "base.html"

	// Glob-паттерны для загрузки шаблонов из fs.FS.
	layoutsGlob  = "layouts/*.html"
	pagesGlob    = "pages/*.html"
	partialsGlob = "partials/*.html"

	// Маршруты.
	trackingPath = "/tracking"

	slogKeyError = "error"
)

// PageHandler обрабатывает рендеринг HTML страниц.
type PageHandler struct {
	templates *template.Template
	mu        sync.RWMutex
	devMode   bool
	fsys      fs.FS
	theme     string
}

// NewPageHandler создаёт новый обработчик страниц.
// fsys — файловая система с шаблонами (embed.FS или os.DirFS).
// Если devMode равен true, шаблоны перезагружаются при каждом запросе.
// theme — имя цветовой темы (файл colors-{theme}.css).
func NewPageHandler(fsys fs.FS, devMode bool, theme string) (*PageHandler, error) {
	h := &PageHandler{
		devMode: devMode,
		fsys:    fsys,
		theme:   theme,
	}

	if err := h.loadTemplates(); err != nil {
		return nil, err
	}

	return h, nil
}

// PageData содержит общие данные для рендеринга страниц.
type PageData struct {
	Title     string
	ActiveTab string
	Theme     string
}

// pageData создаёт PageData с общими полями (тема).
func (h *PageHandler) pageData(title, tab string) PageData {
	return PageData{Title: title, ActiveTab: tab, Theme: h.theme}
}

// Index перенаправляет на страницу отслеживания.
func (h *PageHandler) Index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, trackingPath, http.StatusFound)
}

// Tracking рендерит страницу отслеживания (вкладка 1).
func (h *PageHandler) Tracking(w http.ResponseWriter, r *http.Request) {
	h.render(w, h.pageData("Сеанс - Satellite Scout", "tracking"))
}

// Receiver рендерит страницу приёмника (вкладка 3).
func (h *PageHandler) Receiver(w http.ResponseWriter, r *http.Request) {
	h.render(w, h.pageData("Приёмник - Satellite Scout", "receiver"))
}

// Passes рендерит страницу пролётов (вкладка 2).
func (h *PageHandler) Passes(w http.ResponseWriter, r *http.Request) {
	h.render(w, h.pageData("План сеансов - Satellite Scout", "passes"))
}

// Simulation рендерит страницу имитации (вкладка 4).
func (h *PageHandler) Simulation(w http.ResponseWriter, r *http.Request) {
	h.render(w, h.pageData("Имитация - Satellite Scout", "simulation"))
}

func (h *PageHandler) loadTemplates() error {
	tmpl, err := template.New("").ParseFS(h.fsys,
		layoutsGlob,
		pagesGlob,
	)
	if err != nil {
		return err
	}

	// partials могут отсутствовать — не критично, логируем для диагностики.
	if _, parseErr := tmpl.ParseFS(h.fsys, partialsGlob); parseErr != nil {
		slog.Debug("partials not loaded (optional)", slog.Any("error", parseErr))
	}

	h.mu.Lock()
	h.templates = tmpl
	h.mu.Unlock()

	return nil
}

func (h *PageHandler) render(w http.ResponseWriter, data any) {
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
	if err := tmpl.ExecuteTemplate(w, templateBaseName, data); err != nil {
		slog.Error("failed to render template", slogKeyError, err)
		http.Error(w, "Render error", http.StatusInternalServerError)
	}
}
