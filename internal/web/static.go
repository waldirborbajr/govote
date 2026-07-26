// static.go embeds vendored front-end assets directly into the binary so the
// UI keeps working even when the deployment has no (or restricted) outbound
// internet access to third-party CDNs — important for a self-hosted
// deployment behind Tailscale/a home network (e.g. a Raspberry Pi).
package web

import (
	_ "embed"
	"net/http"
)

//go:embed static/htmx.min.js
var htmxJS []byte

// HandleStaticHTMX serves the vendored htmx.min.js (v1.9.12, matching what
// used to be loaded from https://unpkg.com/htmx.org@1.9.12/htmx.min.js).
func HandleStaticHTMX(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	// O conteúdo é fixo por build (versão pinada no vendoring), então pode
	// cachear agressivamente no navegador.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Write(htmxJS)
}
