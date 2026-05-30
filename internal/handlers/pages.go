package handlers

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"sync"

	"github.com/art-injener/satellite-scout/internal/config"
)

// ConfigGetter — поставщик актуального конфига. Используется PageHandler,
// чтобы при каждом рендере подставлять свежие значения наблюдателя/темы.
type ConfigGetter interface {
	Get() *config.Config
}

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
	cfg       ConfigGetter
}

// NewPageHandler создаёт новый обработчик страниц.
// fsys — файловая система с шаблонами (embed.FS или os.DirFS).
// Если devMode равен true, шаблоны перезагружаются при каждом запросе.
// theme — имя цветовой темы по умолчанию (файл colors-{theme}.css).
// cfg — поставщик актуального конфига (наблюдатель, тема). Может быть nil
// для тестов; тогда подставляются пустые значения.
func NewPageHandler(fsys fs.FS, devMode bool, theme string, cfg ConfigGetter) (*PageHandler, error) {
	h := &PageHandler{
		devMode: devMode,
		fsys:    fsys,
		theme:   theme,
		cfg:     cfg,
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

	// ObserverName — отображаемое имя точки наблюдения (например, "Москва").
	ObserverName string
	// ObserverCoordsLabel — отформатированные координаты ("55.76°N 37.62°E").
	ObserverCoordsLabel string

	// ShowAllTracksOnStart — стартовое состояние master-toggle «глазика»
	// в шапке таблицы плана сеансов. Прокидывается в data-атрибут на <body>
	// и читается синхронно скриптами инициализации. Дальнейшее переключение
	// глазика — runtime-only (в config не пишется).
	ShowAllTracksOnStart bool
}

// Допустимые имена тем (совпадают с файлами colors-*.css).
var allowedThemes = map[string]bool{
	"default": true, "classic": true, "stsplus": true, "light": true,
	"breeze-light": true, "breeze": true, "breeze-steel": true, "breeze-dark": true,
}

// themeFromCookie возвращает тему из cookie ss-theme, если она допустима.
func themeFromCookie(r *http.Request) string {
	c, err := r.Cookie("ss-theme")
	if err != nil || c.Value == "" {
		return ""
	}
	if allowedThemes[c.Value] {
		return c.Value
	}
	return ""
}

// pageData создаёт PageData с общими полями (тема, наблюдатель).
// Приоритет темы: cookie ss-theme > актуальный config.UI.Theme > дефолт.
// Наблюдатель берётся из текущего config (свежий снимок на каждый рендер).
func (h *PageHandler) pageData(title, tab string, r *http.Request) PageData {
	theme := h.theme
	observerName := ""
	observerCoords := ""
	showAll := false

	if h.cfg != nil {
		if cfg := h.cfg.Get(); cfg != nil {
			if cfg.UI.Theme != "" {
				theme = cfg.UI.Theme
			}
			showAll = cfg.UI.ShowAllTracksOnStart
			obs := cfg.Station.Observer
			observerName = obs.Name
			observerCoords = formatCoords(obs.Lat, obs.Lon)
		}
	}
	if ct := themeFromCookie(r); ct != "" {
		theme = ct
	}
	return PageData{
		Title:                title,
		ActiveTab:            tab,
		Theme:                theme,
		ObserverName:         observerName,
		ObserverCoordsLabel:  observerCoords,
		ShowAllTracksOnStart: showAll,
	}
}

// formatCoords форматирует координаты в стиле "55.76°N 37.62°E".
func formatCoords(lat, lon float64) string {
	ns := "N"
	if lat < 0 {
		ns = "S"
	}
	ew := "E"
	if lon < 0 {
		ew = "W"
	}
	return fmt.Sprintf("%.2f°%s %.2f°%s", math.Abs(lat), ns, math.Abs(lon), ew)
}

// Index перенаправляет на страницу отслеживания.
func (h *PageHandler) Index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, trackingPath, http.StatusFound)
}

// Tracking рендерит страницу отслеживания (вкладка 1).
func (h *PageHandler) Tracking(w http.ResponseWriter, r *http.Request) {
	h.render(w, h.pageData("Сеанс - Satellite Scout", "tracking", r))
}

// Receiver рендерит страницу приёмника (вкладка 3).
func (h *PageHandler) Receiver(w http.ResponseWriter, r *http.Request) {
	h.render(w, h.pageData("Приёмник - Satellite Scout", "receiver", r))
}

// Simulation рендерит страницу имитации (вкладка 4).
func (h *PageHandler) Simulation(w http.ResponseWriter, r *http.Request) {
	h.render(w, h.pageData("Имитация - Satellite Scout", "simulation", r))
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
