// Package assets embeds the vendored front-end static files (CSS, JS) and
// exposes a short content hash for each. Both internal/web (which serves the
// files with a one-year immutable Cache-Control) and internal/views (which
// renders the <link>/<script> tags) depend on this package instead of on
// each other, avoiding an import cycle between them.
//
// Why the hash exists: HandleStaticCSS/HandleStaticHTMX serve these files at
// a fixed URL with Cache-Control: public, max-age=31536000, immutable. That
// is correct as long as the URL changes whenever the content does — without
// it, a browser (or CDN) that already cached the old bytes has no reason to
// re-fetch for up to a year after a deploy ships new CSS/JS. Appending
// "?v=<hash>" to the URL solves that automatically: the hash is derived from
// the embedded bytes themselves, so it changes exactly when the vendored
// file's content changes between builds, with nothing to remember to bump by
// hand.
package assets

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed static/app.css
var CSS []byte

//go:embed static/htmx.min.js
var HTMXJS []byte

// CSSVersion and HTMXVersion are short (10 hex chars) sha256 prefixes of the
// embedded file contents, computed once at package init.
var (
	CSSVersion  = shortHash(CSS)
	HTMXVersion = shortHash(HTMXJS)
)

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:10]
}
