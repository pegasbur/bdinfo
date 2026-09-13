package main

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The frontend build is copied into cmd/server/ui during the Docker build.
//
//go:embed ui
var embeddedUI embed.FS

var (
	uiFS         = mustUIFS()
	uiFileServer = http.FileServer(http.FS(uiFS))
)

func mustUIFS() fs.FS {
	sub, err := fs.Sub(embeddedUI, "ui")
	if err != nil {
		panic(err)
	}

	return sub
}

func handleUI(w http.ResponseWriter, r *http.Request) {
	// Serve real frontend assets directly.
	if r.URL.Path != "/" {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		if info, err := fs.Stat(uiFS, name); err == nil && !info.IsDir() {
			uiFileServer.ServeHTTP(w, r)
			return
		}
	}

	// Everything else falls back to the React entry point.
	index, err := fs.ReadFile(uiFS, "index.html")
	if err != nil {
		http.Error(w, "UI unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(index)
}
