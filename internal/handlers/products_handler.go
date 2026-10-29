package handlers

import (
	"net/http"

	"github.com/art-injener/satellite-scout/internal/products"
)

// ProductsHandler — продукты приёма для вкладки «Снимки».
type ProductsHandler struct {
	store *products.Store
}

// NewProductsHandler создаёт обработчик продуктов.
func NewProductsHandler(store *products.Store) *ProductsHandler {
	return &ProductsHandler{store: store}
}

// List — продукты от новых к старым.
func (h *ProductsHandler) List(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.List())
}

// Image — PNG продукта; проявляющаяся картинка отдаётся как есть сейчас.
func (h *ProductsHandler) Image(w http.ResponseWriter, r *http.Request) {
	b, ok := h.store.PNG(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}
