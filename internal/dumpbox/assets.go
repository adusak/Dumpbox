package dumpbox

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"time"
)

//go:embed assets/*
var brandAssets embed.FS

var applicationAssetVersion = assetVersion("app.css", "app.js")

func assetVersion(names ...string) string {
	hash := sha256.New()
	for _, name := range names {
		data, err := brandAssets.ReadFile("assets/" + name)
		if err != nil {
			panic("dumpbox: missing embedded asset " + name)
		}
		_, _ = hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))[:12]
}

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
