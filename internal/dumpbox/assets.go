package dumpbox

import (
	"bytes"
	"embed"
	"net/http"
	"time"
)

//go:embed assets/logo.svg assets/favicon.svg
var brandAssets embed.FS

// brandAsset serves an embedded SVG brand asset with a long-lived cache header.
func brandAsset(name string) http.HandlerFunc {
	data, err := brandAssets.ReadFile("assets/" + name)
	if err != nil {
		panic("dumpbox: missing embedded asset " + name)
	}
	modified := time.Now()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeContent(w, r, name, modified, bytes.NewReader(data))
	}
}
