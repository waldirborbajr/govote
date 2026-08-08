package web

import (
	"context"
	"net/http"

	"github.com/waldirborbajr/govote/internal/views"
)

// PageData is an alias for gradual migration. Prefer views.PageData.
type PageData = views.PageData

// Render is the preferred way to render UI fragments with HTMx + templ.
func Render(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	component := views.ComponentFor(name, data)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "erro ao renderizar", http.StatusInternalServerError)
	}
}

// Templates provides a temporary bridge so existing handlers keep compiling.
// Prefer web.Render(w, r, name, data).
var Templates = templateBridge{}

type templateBridge struct{}

func (templateBridge) ExecuteTemplate(w http.ResponseWriter, name string, data interface{}) error {
	pd := PageData{}
	if d, ok := data.(PageData); ok {
		pd = d
	}
	component := views.ComponentFor(name, pd)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Use background context because the old ExecuteTemplate API has no *http.Request.
	return component.Render(context.Background(), w)
}
