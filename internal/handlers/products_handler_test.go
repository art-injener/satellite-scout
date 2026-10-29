package handlers

import (
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/art-injener/satellite-scout/internal/products"
)

func productsMux(t *testing.T) (*http.ServeMux, products.Product) {
	t.Helper()
	store, err := products.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := store.Begin(products.Meta{Kind: products.KindSSTV, Mode: "robot36"}, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	h := NewProductsHandler(store)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/products", h.List)
	mux.HandleFunc("GET /api/products/{id}", h.Image)
	return mux, p
}

func TestProductsHandler_List(t *testing.T) {
	mux, p := productsMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/products", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var list []products.Product
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != p.ID || list[0].Width != 3 {
		t.Errorf("list=%+v", list)
	}
}

func TestProductsHandler_Image(t *testing.T) {
	mux, p := productsMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/products/"+p.ID, nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	// The picture is still coming in: the browser must not keep an old copy.
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cache-control=%q, want no-store", rec.Header().Get("Cache-Control"))
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/products/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: status=%d, want 404", rec.Code)
	}
}
