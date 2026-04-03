package satellitescout

import "embed"

// TemplatesFS содержит встроенные HTML-шаблоны (layouts, pages, partials).
//
//go:embed templates
var TemplatesFS embed.FS

// StaticFS содержит встроенные статические файлы (CSS, JS, данные карт, vendor).
//
//go:embed static
var StaticFS embed.FS
