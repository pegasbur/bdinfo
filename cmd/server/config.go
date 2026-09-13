package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const configFilePath = "/config/config.json"

type sourceLocation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type favorite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type appConfig struct {
	Locations []sourceLocation `json:"locations"`
	Favorites []favorite       `json:"favorites"`
}

var configStore = struct {
	sync.RWMutex
	config appConfig
}{}

func initConfig() error {
	if err := os.MkdirAll(filepath.Dir(configFilePath), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := os.ReadFile(configFilePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read config: %w", err)
		}

		configStore.Lock()
		configStore.config = appConfig{
			Locations: []sourceLocation{
				{
					ID:   "data",
					Name: "Data",
					Path: "/data",
				},
			},
			Favorites: []favorite{},
		}
		err = saveConfigLocked()
		configStore.Unlock()

		return err
	}

	var cfg appConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if len(cfg.Locations) == 0 {
		cfg.Locations = []sourceLocation{
			{
				ID:   "data",
				Name: "Data",
				Path: "/data",
			},
		}
	}

	for i := range cfg.Locations {
		cfg.Locations[i].Path = filepath.Clean(cfg.Locations[i].Path)
	}

	for i := range cfg.Favorites {
		cfg.Favorites[i].Path = filepath.Clean(cfg.Favorites[i].Path)
	}

	configStore.Lock()
	configStore.config = cfg
	configStore.Unlock()

	return nil
}

func saveConfigLocked() error {
	data, err := json.MarshalIndent(configStore.config, "", "  ")
	if err != nil {
		return err
	}

	data = append(data, '\n')

	tmp := configFilePath + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmp, configFilePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	return nil
}

func newConfigID(prefix string) (string, error) {
	var value [4]byte

	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}

	return prefix + "-" + hex.EncodeToString(value[:]), nil
}

func configuredLocations() []sourceLocation {
	configStore.RLock()
	defer configStore.RUnlock()

	result := make([]sourceLocation, len(configStore.config.Locations))
	copy(result, configStore.config.Locations)

	return result
}

func defaultBrowseRoot() (string, error) {
	locations := configuredLocations()

	if len(locations) == 0 {
		return "", errors.New("no source locations are configured")
	}

	return locations[0].Path, nil
}

func pathInsideRoot(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func insideConfiguredLocation(path string) bool {
	for _, location := range configuredLocations() {
		if pathInsideRoot(location.Path, path) {
			return true
		}
	}

	return false
}

func locationForPath(path string) (sourceLocation, bool) {
	var match sourceLocation
	found := false

	for _, location := range configuredLocations() {
		if !pathInsideRoot(location.Path, path) {
			continue
		}

		if !found || len(location.Path) > len(match.Path) {
			match = location
			found = true
		}
	}

	return match, found
}

func handleGetLocations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"locations": configuredLocations(),
	})
}

func handleCreateLocation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "invalid JSON request: " + err.Error(),
		})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Path = strings.TrimSpace(req.Path)

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "location name is required",
		})
		return
	}

	if req.Path == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "location path is required",
		})
		return
	}

	if !filepath.IsAbs(req.Path) {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "location path must be absolute",
		})
		return
	}

	resolved, err := filepath.EvalSymlinks(filepath.Clean(req.Path))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "location is not accessible: " + err.Error(),
		})
		return
	}

	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "location must be an accessible directory",
		})
		return
	}

	id, err := newConfigID("location")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "unable to generate location ID",
		})
		return
	}

	location := sourceLocation{
		ID:   id,
		Name: req.Name,
		Path: resolved,
	}

	configStore.Lock()

	for _, existing := range configStore.config.Locations {
		if existing.Path == resolved {
			configStore.Unlock()

			writeJSON(w, http.StatusConflict, errorResponse{
				Error: "location is already configured",
			})
			return
		}
	}

	configStore.config.Locations = append(
		configStore.config.Locations,
		location,
	)

	if err := saveConfigLocked(); err != nil {
		configStore.config.Locations =
			configStore.config.Locations[:len(configStore.config.Locations)-1]

		configStore.Unlock()

		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "unable to save configuration",
		})
		return
	}

	configStore.Unlock()

	writeJSON(w, http.StatusCreated, location)
}

func handleDeleteLocation(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))

	configStore.Lock()
	defer configStore.Unlock()

	if len(configStore.config.Locations) <= 1 {
		writeJSON(w, http.StatusConflict, errorResponse{
			Error: "at least one source location must remain configured",
		})
		return
	}

	index := -1

	for i, location := range configStore.config.Locations {
		if location.ID == id {
			index = i
			break
		}
	}

	if index < 0 {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error: "location not found",
		})
		return
	}

	location := configStore.config.Locations[index]

	oldLocations := append(
		[]sourceLocation(nil),
		configStore.config.Locations...,
	)
	oldFavorites := append(
		[]favorite(nil),
		configStore.config.Favorites...,
	)

	configStore.config.Locations = append(
		configStore.config.Locations[:index],
		configStore.config.Locations[index+1:]...,
	)

	// Favorites inside a removed location are no longer valid.
	filtered := configStore.config.Favorites[:0]

	for _, fav := range configStore.config.Favorites {
		if !pathInsideRoot(location.Path, fav.Path) {
			filtered = append(filtered, fav)
		}
	}

	configStore.config.Favorites = filtered

	if err := saveConfigLocked(); err != nil {
		configStore.config.Locations = oldLocations
		configStore.config.Favorites = oldFavorites

		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "unable to save configuration",
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleGetFavorites(w http.ResponseWriter, r *http.Request) {
	configStore.RLock()
	favorites := make([]favorite, len(configStore.config.Favorites))
	copy(favorites, configStore.config.Favorites)
	configStore.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"favorites": favorites,
	})
}

func handleCreateFavorite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "invalid JSON request: " + err.Error(),
		})
		return
	}

	resolved, err := safeSourcePath(req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: err.Error(),
		})
		return
	}

	info, err := os.Stat(resolved)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "favorite is not accessible",
		})
		return
	}

	if !info.IsDir() && !strings.EqualFold(filepath.Ext(resolved), ".iso") {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "favorite must be a directory or ISO file",
		})
		return
	}

	name := strings.TrimSpace(req.Name)

	if name == "" {
		name = filepath.Base(resolved)
	}

	id, err := newConfigID("favorite")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "unable to generate favorite ID",
		})
		return
	}

	fav := favorite{
		ID:   id,
		Name: name,
		Path: resolved,
	}

	configStore.Lock()

	for _, existing := range configStore.config.Favorites {
		if existing.Path == resolved {
			configStore.Unlock()

			writeJSON(w, http.StatusConflict, errorResponse{
				Error: "favorite already exists",
			})
			return
		}
	}

	configStore.config.Favorites = append(
		configStore.config.Favorites,
		fav,
	)

	if err := saveConfigLocked(); err != nil {
		configStore.config.Favorites =
			configStore.config.Favorites[:len(configStore.config.Favorites)-1]

		configStore.Unlock()

		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "unable to save configuration",
		})
		return
	}

	configStore.Unlock()

	writeJSON(w, http.StatusCreated, fav)
}

func handleDeleteFavorite(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))

	configStore.Lock()
	defer configStore.Unlock()

	index := -1

	for i, fav := range configStore.config.Favorites {
		if fav.ID == id {
			index = i
			break
		}
	}

	if index < 0 {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error: "favorite not found",
		})
		return
	}

	old := append(
		[]favorite(nil),
		configStore.config.Favorites...,
	)

	configStore.config.Favorites = append(
		configStore.config.Favorites[:index],
		configStore.config.Favorites[index+1:]...,
	)

	if err := saveConfigLocked(); err != nil {
		configStore.config.Favorites = old

		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error: "unable to save configuration",
		})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
