package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type browseEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Selectable bool      `json:"selectable"`
	ModifiedAt time.Time `json:"modifiedAt"`
	SizeBytes  int64     `json:"sizeBytes,omitempty"`
}

type browseResponse struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent,omitempty"`
	Entries []browseEntry `json:"entries"`
}

func handleBrowse(w http.ResponseWriter, r *http.Request) {
	requested := strings.TrimSpace(r.URL.Query().Get("path"))
	if requested == "" {
		root, err := defaultBrowseRoot()
		if err != nil {
			writeJSON(w, http.StatusConflict, errorResponse{
				Error: err.Error(),
			})
			return
		}

		requested = root
	}

	dir, err := safeSourcePath(requested)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	info, err := os.Stat(dir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "path not accessible: " + err.Error(),
		})
		return
	}

	if !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "browse path must be a directory",
		})
		return
	}

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "unable to read directory: " + err.Error(),
		})
		return
	}

	response := browseResponse{
		Path:    dir,
		Entries: make([]browseEntry, 0, len(dirEntries)),
	}

	if location, ok := locationForPath(dir); ok &&
		filepath.Clean(dir) != filepath.Clean(location.Path) {
		parent := filepath.Dir(dir)

		if pathInsideRoot(location.Path, parent) {
			response.Parent = parent
		}
	}

	for _, entry := range dirEntries {
		entryPath := filepath.Join(dir, entry.Name())

		// Broken symlinks and symlinks escaping the permitted root
		// are not exposed by the browser.
		resolved, err := safeSourcePath(entryPath)
		if err != nil {
			continue
		}

		info, err := os.Stat(resolved)
		if err != nil {
			continue
		}

		switch {
		case info.IsDir():
			response.Entries = append(response.Entries, browseEntry{
				Name:       entry.Name(),
				Path:       resolved,
				Type:       "directory",
				Selectable: isBluRayDirectory(resolved),
				ModifiedAt: info.ModTime(),
			})

		case strings.EqualFold(filepath.Ext(resolved), ".iso"):
			response.Entries = append(response.Entries, browseEntry{
				Name:       entry.Name(),
				Path:       resolved,
				Type:       "iso",
				Selectable: true,
				ModifiedAt: info.ModTime(),
				SizeBytes:  info.Size(),
			})
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func isBluRayDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}

	if !strings.EqualFold(filepath.Base(path), "BDMV") {
		return false
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}

	hasIndex := false
	hasPlaylist := false
	hasStream := false

	for _, entry := range entries {
		switch {
		case strings.EqualFold(entry.Name(), "index.bdmv") && !entry.IsDir():
			hasIndex = true

		case strings.EqualFold(entry.Name(), "PLAYLIST") && entry.IsDir():
			hasPlaylist = true

		case strings.EqualFold(entry.Name(), "STREAM") && entry.IsDir():
			hasStream = true
		}
	}

	return hasIndex && hasPlaylist && hasStream
}
