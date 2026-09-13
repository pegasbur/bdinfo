package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	bdinfo "github.com/autobrr/go-bdinfo/pkg/bdinfo"
)

type discoverRequest struct {
	Path string `json:"path"`
}

type discoverResponse struct {
	Disc                  bdinfo.DiscInfo       `json:"disc"`
	Playlists             []bdinfo.PlaylistInfo `json:"playlists"`
	MainPlaylist          *bdinfo.PlaylistInfo  `json:"mainPlaylist,omitempty"`
	ValidPlaylistCount    int                   `json:"validPlaylistCount"`
	FilteredPlaylistCount int                   `json:"filteredPlaylistCount"`
	Scan                  bdinfo.ScanInfo       `json:"scan"`
	DurationMS            int64                 `json:"durationMs"`
}

type scanResponse struct {
	Disc       bdinfo.DiscInfo       `json:"disc"`
	Playlists  []bdinfo.PlaylistInfo `json:"playlists"`
	Scan       bdinfo.ScanInfo       `json:"scan"`
	Report     string                `json:"report,omitempty"`
	DurationMS int64                 `json:"durationMs"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func main() {

	if err := initConfig(); err != nil {
		log.Fatal("configuration error: ", err)
	}

	if runScanWorkerIfRequested() {
		return
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/locations", handleGetLocations)
	mux.HandleFunc("POST /api/locations", handleCreateLocation)
	mux.HandleFunc("DELETE /api/locations/{id}", handleDeleteLocation)

	mux.HandleFunc("GET /api/favorites", handleGetFavorites)
	mux.HandleFunc("POST /api/favorites", handleCreateFavorite)
	mux.HandleFunc("DELETE /api/favorites/{id}", handleDeleteFavorite)

	mux.HandleFunc("GET /", handleUI)

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
		})
	})

	mux.HandleFunc("POST /api/discover", handleDiscover)
	mux.HandleFunc("GET /api/browse", handleBrowse)
	mux.HandleFunc("POST /api/scans", handleScans)
	mux.HandleFunc("GET /api/scans/{id}/report", handleScanReport)
	mux.HandleFunc("GET /api/scans/{id}", handleScanJob)
	mux.HandleFunc("DELETE /api/scans/{id}", handleScanJob)

	addr := ":4646"
	log.Printf("go-bdinfo listening on %s", addr)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func handleDiscover(w http.ResponseWriter, r *http.Request) {
	var req discoverRequest

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "invalid JSON request: " + err.Error(),
		})
		return
	}

	source, err := validateSource(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	settings := bdinfo.DefaultSettings(".")

	started := time.Now()

	result, err := bdinfo.DiscoverPlaylists(r.Context(), bdinfo.Options{
		Path:     source,
		Settings: settings,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: err.Error(),
		})
		return
	}

	normalizeDiscInfo(source, &result.Disc)

	mainPlaylist, validCount, filteredCount := selectMainPlaylist(result.Playlists)

	writeJSON(w, http.StatusOK, discoverResponse{
		Disc:                  result.Disc,
		Playlists:             result.Playlists,
		MainPlaylist:          mainPlaylist,
		ValidPlaylistCount:    validCount,
		FilteredPlaylistCount: filteredCount,
		Scan:                  result.Scan,
		DurationMS:            time.Since(started).Milliseconds(),
	})
}

func normalizeDiscInfo(source string, disc *bdinfo.DiscInfo) {
	if disc == nil {
		return
	}

	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return
	}

	if !strings.EqualFold(filepath.Base(source), "BDMV") {
		return
	}

	// Preserve a meaningful label discovered by go-bdinfo.
	current := strings.TrimSpace(disc.Label)
	if current != "" && !strings.EqualFold(current, "BDMV") {
		return
	}

	parent := filepath.Base(filepath.Dir(source))
	if parent == "" || parent == "." || parent == string(filepath.Separator) {
		return
	}

	disc.Label = parent
}

func normalizeReportDiscLabel(source, report string) string {
	if strings.TrimSpace(report) == "" {
		return report
	}

	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return report
	}

	if !strings.EqualFold(filepath.Base(source), "BDMV") {
		return report
	}

	fallback := filepath.Base(filepath.Dir(source))
	if fallback == "" || fallback == "." || fallback == string(filepath.Separator) {
		return report
	}

	lines := strings.Split(report, "\n")

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")

		if !strings.HasPrefix(trimmed, "Disc Label:") {
			continue
		}

		rest := trimmed[len("Disc Label:"):]
		current := strings.TrimSpace(rest)

		// A real label wins. Replace only empty or generic BDMV.
		if current != "" && !strings.EqualFold(current, "BDMV") {
			continue
		}

		indentLength := len(line) - len(trimmed)
		indent := line[:indentLength]

		valueStart := len(rest) - len(strings.TrimLeft(rest, " \t"))
		spacing := rest[:valueStart]

		if spacing == "" {
			spacing = " "
		}

		lines[i] = indent + "Disc Label:" + spacing + fallback
	}

	return strings.Join(lines, "\n")
}

func validateSource(input string) (string, error) {
	source, err := safeSourcePath(input)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(source)
	if err != nil {
		return "", errors.New("source not accessible: " + err.Error())
	}

	if !info.IsDir() && !strings.EqualFold(filepath.Ext(source), ".iso") {
		return "", errors.New("source must be a Blu-ray directory or ISO file")
	}

	return source, nil
}

func safeSourcePath(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("path is required")
	}

	path := input

	if !filepath.IsAbs(path) {
		root, err := defaultBrowseRoot()
		if err != nil {
			return "", err
		}

		path = filepath.Join(root, path)
	}

	path = filepath.Clean(path)

	if !insideConfiguredLocation(path) {
		return "", errors.New("path must be inside a configured location")
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	if !insideConfiguredLocation(resolved) {
		return "", errors.New("resolved path escapes configured locations")
	}

	return resolved, nil
}

func selectMainPlaylist(playlists []bdinfo.PlaylistInfo) (*bdinfo.PlaylistInfo, int, int) {
	if len(playlists) == 0 {
		return nil, 0, 0
	}

	valid := make([]bdinfo.PlaylistInfo, 0, len(playlists))
	for _, p := range playlists {
		if p.IsValid {
			valid = append(valid, p)
		}
	}

	filteredCount := len(playlists) - len(valid)

	// Match go-bdinfo --main:
	// use valid playlists when at least one exists;
	// otherwise fall back to all playlists.
	candidates := playlists
	if len(valid) > 0 {
		candidates = valid
	}

	main := candidates[0]

	for _, p := range candidates[1:] {
		if p.LengthSeconds > main.LengthSeconds {
			main = p
			continue
		}
		if p.LengthSeconds < main.LengthSeconds {
			continue
		}

		if p.SizeBytes > main.SizeBytes {
			main = p
			continue
		}
		if p.SizeBytes < main.SizeBytes {
			continue
		}

		if p.TotalBitrateBps > main.TotalBitrateBps {
			main = p
			continue
		}
		if p.TotalBitrateBps < main.TotalBitrateBps {
			continue
		}

		if p.Name < main.Name {
			main = p
		}
	}

	return &main, len(valid), filteredCount
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("JSON response error: %v", err)
	}
}
