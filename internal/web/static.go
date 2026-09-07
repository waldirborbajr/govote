// static.go serves the vendored front-end assets (embedded via internal/assets)
// directly from the binary so the UI keeps working even when the deployment
// has no (or restricted) outbound internet access to third-party CDNs —
// important for a self-hosted deployment behind Tailscale/a home network
// (e.g. a Raspberry Pi).
package web

import (
	"net/http"

	"github.com/waldirborbajr/govote/internal/assets"
)

// HandleStaticHTMX serves the vendored htmx.min.js (v1.9.12, matching what
// used to be loaded from https://unpkg.com/htmx.org@1.9.12/htmx.min.js).
func HandleStaticHTMX(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	// O conteúdo é fixo por build (versão pinada no vendoring), então pode
	// cachear agressivamente no navegador. Como a URL que os templates geram
	// inclui "?v=" + assets.HTMXVersion (hash do conteúdo embutido), o cache
	// de um ano é seguro: qualquer troca do arquivo entre builds muda o
	// hash e portanto a URL, forçando um novo fetch.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+assets.HTMXVersion+`"`)
	w.Write(assets.HTMXJS)
}

// HandleStaticCSS serves a Tailwind CSS + daisyUI 4.12.10 bundle compiled
// ahead of time with the real Tailwind CLI (npx tailwindcss --minify)
// against the actual template markup — not the cdn.tailwindcss.com runtime
// JIT compiler, which Tailwind itself warns against using in production
// (console warning: "cdn.tailwindcss.com should not be used in production").
// Regenerate it whenever the templates' markup/classes change.
func HandleStaticCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	// Ver comentário de cache em HandleStaticHTMX acima — mesma lógica.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+assets.CSSVersion+`"`)
	w.Write(assets.CSS)
}
