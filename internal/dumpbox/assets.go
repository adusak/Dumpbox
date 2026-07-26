package dumpbox

import (
	"bytes"
	"embed"
	"net/http"
	"time"
)

//go:embed assets/*
var brandAssets embed.FS

func staticAsset(name, contentType string) http.HandlerFunc {
	data, err := brandAssets.ReadFile("assets/" + name)
	if err != nil {
		panic("dumpbox: missing embedded asset " + name)
	}
	modified := time.Now()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeContent(w, r, name, modified, bytes.NewReader(data))
	}
}

func brandAsset(name string) http.HandlerFunc {
	return staticAsset(name, "image/svg+xml")
}
