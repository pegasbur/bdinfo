package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/autobrr/go-bdinfo/pkg/bdinfo"
)

type scanJobRequest struct {
	Path      string   `json:"path"`
	Mode      string   `json:"mode"`
	Playlist  string   `json:"playlist,omitempty"`
	Playlists []string `json:"playlists,omitempty"`
}

type scanProgress struct {
	Stage                 string  `json:"stage"`
	CurrentPlaylist       string  `json:"currentPlaylist,omitempty"`
	CurrentProcessedBytes uint64  `json:"currentProcessedBytes,omitempty"`
	CurrentTotalBytes     uint64  `json:"currentTotalBytes,omitempty"`
	CurrentPercent        float64 `json:"currentPercent,omitempty"`
	ProcessedBytes        uint64  `json:"processedBytes"`
	TotalBytes            uint64  `json:"totalBytes"`
	Percent               float64 `json:"percent"`
}

type scanJob struct {
	ID         string         `json:"id"`
	Status     string         `json:"status"`
	Request    scanJobRequest `json:"request"`
	Progress   *scanProgress  `json:"progress,omitempty"`
	ElapsedMS  int64          `json:"elapsedMs"`
	Result     *scanResponse  `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	StartedAt  *time.Time     `json:"startedAt,omitempty"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`

	cmd             *exec.Cmd
	cancelRequested bool
}

type workerMessage struct {
	Type                  string                `json:"type"`
	Progress              *bdinfo.ProgressEvent `json:"progress,omitempty"`
	CurrentPlaylist       string                `json:"currentPlaylist,omitempty"`
	CurrentProcessedBytes uint64                `json:"currentProcessedBytes,omitempty"`
	CurrentTotalBytes     uint64                `json:"currentTotalBytes,omitempty"`
	Result                *scanResponse         `json:"result,omitempty"`
	Error                 string                `json:"error,omitempty"`
}

type workerProtocol struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (p *workerProtocol) send(msg workerMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enc.Encode(msg)
}

var scanJobs = struct {
	sync.Mutex
	jobs map[string]*scanJob
}{
	jobs: make(map[string]*scanJob),
}

func runScanWorkerIfRequested() bool {
	if len(os.Args) < 2 || os.Args[1] != "_scan-worker" {
		return false
	}

	if err := executeScanWorker(); err != nil {
		os.Exit(1)
	}

	return true
}

func executeScanWorker() error {
	protocolFile := os.NewFile(uintptr(3), "scan-worker-protocol")
	if protocolFile == nil {
		return errors.New("worker protocol file descriptor is unavailable")
	}
	defer protocolFile.Close()

	protocol := &workerProtocol{
		enc: json.NewEncoder(protocolFile),
	}

	var req scanJobRequest
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 1024*1024))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		_ = protocol.send(workerMessage{
			Type:  "error",
			Error: "invalid worker request: " + err.Error(),
		})
		return err
	}

	source, err := validateSource(req.Path)
	if err != nil {
		_ = protocol.send(workerMessage{
			Type:  "error",
			Error: err.Error(),
		})
		return err
	}

	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Mode == "" {
		req.Mode = "main"
	}

	settings := bdinfo.DefaultSettings(".")
	started := time.Now()
	currentPlaylist := ""

	switch req.Mode {
	case "main":
		discovery, err := bdinfo.DiscoverPlaylists(context.Background(), bdinfo.Options{
			Path:     source,
			Settings: settings,
		})
		if err != nil {
			msg := "playlist discovery failed: " + err.Error()
			_ = protocol.send(workerMessage{Type: "error", Error: msg})
			return errors.New(msg)
		}

		mainPlaylist, _, _ := selectMainPlaylist(discovery.Playlists)
		if mainPlaylist == nil {
			msg := "no playlist available for main-feature selection"
			_ = protocol.send(workerMessage{Type: "error", Error: msg})
			return errors.New(msg)
		}

		// Same Main selection as upstream, but filter before M2TS scanning.
		settings.PlaylistOnly = mainPlaylist.Name
		currentPlaylist = mainPlaylist.Name

	case "playlist":
		playlist := strings.TrimSpace(req.Playlist)
		if playlist == "" {
			msg := "playlist is required when mode is playlist"
			_ = protocol.send(workerMessage{Type: "error", Error: msg})
			return errors.New(msg)
		}

		settings.PlaylistOnly = playlist
		currentPlaylist = playlist

	case "selected":
		response, err := runSelectedScan(
			source,
			req.Playlists,
			protocol,
			started,
		)
		if err != nil {
			_ = protocol.send(workerMessage{
				Type:  "error",
				Error: err.Error(),
			})
			return err
		}

		if err := protocol.send(workerMessage{
			Type:   "result",
			Result: response,
		}); err != nil {
			return err
		}

		return nil

	case "all":
		// Empty PlaylistOnly scans the complete applicable disc.

	default:
		msg := "mode must be main, selected, playlist, or all"
		_ = protocol.send(workerMessage{Type: "error", Error: msg})
		return errors.New(msg)
	}

	settings.MainPlaylistOnly = false
	settings.SummaryOnly = false
	settings.ForumsOnly = false

	result, err := bdinfo.Run(context.Background(), bdinfo.Options{
		Path:       source,
		ReportPath: "-",
		Settings:   settings,
		OnProgress: func(event bdinfo.ProgressEvent) {
			_ = protocol.send(workerMessage{
				Type:                  "progress",
				Progress:              &event,
				CurrentPlaylist:       currentPlaylist,
				CurrentProcessedBytes: event.ProcessedBytes,
				CurrentTotalBytes:     event.TotalBytes,
			})
		},
	})
	if err != nil {
		_ = protocol.send(workerMessage{
			Type:  "error",
			Error: err.Error(),
		})
		return err
	}

	normalizeDiscInfo(source, &result.Disc)

	result.Report = normalizeReportDiscLabel(
		source,
		result.Report,
	)

	response := scanResponse{
		Disc:       result.Disc,
		Playlists:  result.Playlists,
		Scan:       result.Scan,
		Report:     result.Report,
		DurationMS: time.Since(started).Milliseconds(),
	}

	if err := protocol.send(workerMessage{
		Type:   "result",
		Result: &response,
	}); err != nil {
		return err
	}

	return nil
}

func resolveRequestedPlaylists(
	ctx context.Context,
	source string,
	requested []string,
) ([]string, error) {
	settings := bdinfo.DefaultSettings(".")

	discovery, err := bdinfo.DiscoverPlaylists(ctx, bdinfo.Options{
		Path:     source,
		Settings: settings,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"playlist discovery failed: %w",
			err,
		)
	}

	lookup := make(map[string]string, len(discovery.Playlists))

	for _, playlist := range discovery.Playlists {
		lookup[strings.ToLower(playlist.Name)] = playlist.Name
	}

	resolved := make([]string, 0, len(requested))
	seen := make(map[string]bool)

	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		canonical, ok := lookup[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf(
				"playlist %q was not found on this disc",
				name,
			)
		}

		key := strings.ToLower(canonical)
		if seen[key] {
			continue
		}

		seen[key] = true
		resolved = append(resolved, canonical)
	}

	return resolved, nil
}

func runSelectedScan(
	source string,
	requested []string,
	protocol *workerProtocol,
	started time.Time,
) (*scanResponse, error) {
	if len(requested) == 0 {
		return nil, errors.New(
			"at least one playlist is required when mode is selected",
		)
	}

	settings := bdinfo.DefaultSettings(".")

	discovery, err := bdinfo.DiscoverPlaylists(
		context.Background(),
		bdinfo.Options{
			Path:     source,
			Settings: settings,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"playlist discovery failed: %w",
			err,
		)
	}

	byName := make(map[string]bdinfo.PlaylistInfo)

	for _, playlist := range discovery.Playlists {
		byName[strings.ToLower(playlist.Name)] = playlist
	}

	selected := make([]bdinfo.PlaylistInfo, 0, len(requested))
	seen := make(map[string]bool)

	for _, requestedName := range requested {
		playlist, ok := byName[strings.ToLower(
			strings.TrimSpace(requestedName),
		)]
		if !ok {
			return nil, fmt.Errorf(
				"playlist %q was not found on this disc",
				requestedName,
			)
		}

		key := strings.ToLower(playlist.Name)
		if seen[key] {
			continue
		}

		seen[key] = true
		selected = append(selected, playlist)
	}

	if len(selected) == 0 {
		return nil, errors.New(
			"no selected playlists were found on this disc",
		)
	}

	var totalBytes uint64

	for _, playlist := range selected {
		if playlist.SizeBytes > 0 {
			totalBytes += uint64(playlist.SizeBytes)
		}
	}

	var completedBytes uint64
	var combined scanResponse
	var report strings.Builder

	for index, playlist := range selected {
		playlistSettings := bdinfo.DefaultSettings(".")
		playlistSettings.PlaylistOnly = playlist.Name
		playlistSettings.MainPlaylistOnly = false
		playlistSettings.SummaryOnly = false
		playlistSettings.ForumsOnly = false

		result, err := bdinfo.Run(
			context.Background(),
			bdinfo.Options{
				Path:       source,
				ReportPath: "-",
				Settings:   playlistSettings,
				OnProgress: func(event bdinfo.ProgressEvent) {
					adjusted := event

					adjusted.ProcessedBytes =
						completedBytes + event.ProcessedBytes

					if totalBytes > 0 {
						adjusted.TotalBytes = totalBytes
					}

					_ = protocol.send(workerMessage{
						Type:                  "progress",
						Progress:              &adjusted,
						CurrentPlaylist:       playlist.Name,
						CurrentProcessedBytes: event.ProcessedBytes,
						CurrentTotalBytes:     event.TotalBytes,
					})
				},
			},
		)
		if err != nil {
			return nil, fmt.Errorf(
				"scan %s: %w",
				playlist.Name,
				err,
			)
		}

		normalizeDiscInfo(source, &result.Disc)

		if index == 0 {
			combined.Disc = result.Disc
			combined.Scan = result.Scan
		}

		combined.Playlists = append(
			combined.Playlists,
			result.Playlists...,
		)

		if strings.TrimSpace(result.Report) != "" {
			if report.Len() > 0 {
				report.WriteString("\n\n")
			}

			report.WriteString(strings.TrimSpace(result.Report))
			report.WriteString("\n")
		}

		if playlist.SizeBytes > 0 {
			completedBytes += uint64(playlist.SizeBytes)
		}
	}

	combined.Report = normalizeReportDiscLabel(
		source,
		report.String(),
	)
	combined.DurationMS = time.Since(started).Milliseconds()

	return &combined, nil
}

func resolveRequestedPlaylist(ctx context.Context, source, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", nil
	}

	settings := bdinfo.DefaultSettings(".")

	discovery, err := bdinfo.DiscoverPlaylists(ctx, bdinfo.Options{
		Path:     source,
		Settings: settings,
	})
	if err != nil {
		return "", err
	}

	for _, playlist := range discovery.Playlists {
		if strings.EqualFold(playlist.Name, requested) {
			return playlist.Name, nil
		}
	}

	return "", nil
}

func handleScans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{
			Error: "method not allowed",
		})
		return
	}

	var req scanJobRequest

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

	req.Path = source
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Mode == "" {
		req.Mode = "main"
	}

	switch req.Mode {
	case "main", "all":
	case "playlist":
		req.Playlist = strings.TrimSpace(req.Playlist)
		if req.Playlist == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "playlist is required when mode is playlist",
			})
			return
		}

		playlist, err := resolveRequestedPlaylist(r.Context(), source, req.Playlist)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{
				Error: "playlist discovery failed: " + err.Error(),
			})
			return
		}

		if playlist == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: fmt.Sprintf("playlist %q was not found on this disc", req.Playlist),
			})
			return
		}

		// Preserve the canonical playlist name returned by discovery.
		req.Playlist = playlist

	case "selected":
		if len(req.Playlists) == 0 {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "at least one playlist is required when mode is selected",
			})
			return
		}

		playlists, err := resolveRequestedPlaylists(
			r.Context(),
			source,
			req.Playlists,
		)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: err.Error(),
			})
			return
		}

		if len(playlists) == 0 {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "no selected playlists were found on this disc",
			})
			return
		}

		req.Playlists = playlists

	default:
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "mode must be main, selected, playlist, or all",
		})
		return
	}

	job, err := startScanJob(req)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errScanAlreadyRunning) {
			status = http.StatusConflict
		}

		writeJSON(w, status, errorResponse{
			Error: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusAccepted, snapshotScanJob(job))
}

func handleScanJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))

	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error: "scan job not found",
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		job, ok := getScanJob(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, errorResponse{
				Error: "scan job not found",
			})
			return
		}

		writeJSON(w, http.StatusOK, job)

	case http.MethodDelete:
		job, err := cancelScanJob(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeJSON(w, http.StatusNotFound, errorResponse{
					Error: "scan job not found",
				})
				return
			}

			writeJSON(w, http.StatusConflict, errorResponse{
				Error: err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusAccepted, job)

	default:
		w.Header().Set("Allow", "GET, DELETE")
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{
			Error: "method not allowed",
		})
	}
}

var errScanAlreadyRunning = errors.New("a scan is already running")

func startScanJob(req scanJobRequest) (*scanJob, error) {
	scanJobs.Lock()
	defer scanJobs.Unlock()

	for _, existing := range scanJobs.jobs {
		switch existing.Status {
		case "queued", "running", "cancelling":
			return nil, errScanAlreadyRunning
		}
	}

	id, err := newScanJobID()
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}

	protocolRead, protocolWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	var stderr bytes.Buffer

	cmd := exec.Command(executable, "_scan-worker")
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	cmd.ExtraFiles = []*os.File{protocolWrite}

	// If the web-server process disappears, do not leave an orphan scan behind.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}

	now := time.Now()

	job := &scanJob{
		ID:        id,
		Status:    "queued",
		Request:   req,
		CreatedAt: now,
		cmd:       cmd,
	}

	scanJobs.jobs[id] = job

	if err := cmd.Start(); err != nil {
		delete(scanJobs.jobs, id)
		protocolRead.Close()
		protocolWrite.Close()
		return nil, err
	}

	protocolWrite.Close()

	started := time.Now()
	job.Status = "running"
	job.StartedAt = &started

	go consumeScanWorker(id, cmd, protocolRead, &stderr)

	return job, nil
}

func consumeScanWorker(id string, cmd *exec.Cmd, protocolRead *os.File, stderr *bytes.Buffer) {
	defer protocolRead.Close()

	scanner := bufio.NewScanner(protocolRead)

	// The final BDInfo report can be much larger than Scanner's default 64 KiB.
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)

	receivedResult := false
	workerError := ""

	for scanner.Scan() {
		var msg workerMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			workerError = "invalid worker response: " + err.Error()
			continue
		}

		scanJobs.Lock()
		job := scanJobs.jobs[id]

		if job != nil {
			switch msg.Type {
			case "progress":
				if msg.Progress != nil {
					updateJobProgress(
						job,
						*msg.Progress,
						msg.CurrentPlaylist,
						msg.CurrentProcessedBytes,
						msg.CurrentTotalBytes,
					)
				}

			case "result":
				if msg.Result != nil {
					job.Result = msg.Result
					receivedResult = true
					markJobProgressCompleted(job)
				}

			case "error":
				if msg.Error != "" {
					workerError = msg.Error
				}
			}
		}

		scanJobs.Unlock()
	}

	if err := scanner.Err(); err != nil && workerError == "" {
		workerError = "worker protocol error: " + err.Error()
	}

	waitErr := cmd.Wait()
	finished := time.Now()

	scanJobs.Lock()
	defer scanJobs.Unlock()

	job := scanJobs.jobs[id]
	if job == nil {
		return
	}

	job.FinishedAt = &finished
	job.cmd = nil

	if job.cancelRequested {
		job.Status = "cancelled"
		job.Error = ""
		job.Result = nil
		return
	}

	if workerError != "" {
		job.Status = "failed"
		job.Error = workerError
		return
	}

	if waitErr != nil {
		job.Status = "failed"

		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = waitErr.Error()
		}

		job.Error = message
		return
	}

	if !receivedResult || job.Result == nil {
		job.Status = "failed"
		job.Error = "scan worker exited without returning a result"
		return
	}

	job.Status = "completed"
	job.Error = ""
}

func updateJobProgress(
	job *scanJob,
	event bdinfo.ProgressEvent,
	currentPlaylist string,
	currentProcessedBytes uint64,
	currentTotalBytes uint64,
) {
	stage := string(event.Stage)

	// Later non-byte stages should not erase meaningful byte progress.
	if event.TotalBytes == 0 &&
		job.Progress != nil &&
		job.Progress.TotalBytes > 0 {
		job.Progress.Stage = stage

		if currentPlaylist != "" {
			job.Progress.CurrentPlaylist = currentPlaylist
		}

		return
	}

	progress := &scanProgress{
		Stage:                 stage,
		CurrentPlaylist:       currentPlaylist,
		CurrentProcessedBytes: currentProcessedBytes,
		CurrentTotalBytes:     currentTotalBytes,
		ProcessedBytes:        event.ProcessedBytes,
		TotalBytes:            event.TotalBytes,
	}

	if event.TotalBytes > 0 {
		progress.Percent =
			float64(event.ProcessedBytes) * 100 /
				float64(event.TotalBytes)
	}

	if currentTotalBytes > 0 {
		progress.CurrentPercent =
			float64(currentProcessedBytes) * 100 /
				float64(currentTotalBytes)
	}

	job.Progress = progress
}

func markJobProgressCompleted(job *scanJob) {
	if job.Progress == nil {
		job.Progress = &scanProgress{
			Stage:   "done",
			Percent: 100,
		}
		return
	}

	job.Progress.Stage = "done"

	if job.Progress.TotalBytes > 0 {
		job.Progress.ProcessedBytes = job.Progress.TotalBytes
		job.Progress.Percent = 100
	}
}

func cancelScanJob(id string) (*scanJob, error) {
	scanJobs.Lock()

	job, ok := scanJobs.jobs[id]
	if !ok {
		scanJobs.Unlock()
		return nil, os.ErrNotExist
	}

	switch job.Status {
	case "completed", "failed", "cancelled":
		snapshot := snapshotScanJobLocked(job)
		scanJobs.Unlock()
		return nil, fmt.Errorf("scan is already %s", snapshot.Status)

	case "cancelling":
		snapshot := snapshotScanJobLocked(job)
		scanJobs.Unlock()
		return snapshot, nil
	}

	job.cancelRequested = true
	job.Status = "cancelling"

	cmd := job.cmd
	snapshot := snapshotScanJobLocked(job)

	scanJobs.Unlock()

	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return snapshot, err
		}
	}

	return snapshot, nil
}

func getScanJob(id string) (*scanJob, bool) {
	scanJobs.Lock()
	defer scanJobs.Unlock()

	job, ok := scanJobs.jobs[id]
	if !ok {
		return nil, false
	}

	return snapshotScanJobLocked(job), true
}

func snapshotScanJob(job *scanJob) *scanJob {
	scanJobs.Lock()
	defer scanJobs.Unlock()
	return snapshotScanJobLocked(job)
}

func snapshotScanJobLocked(job *scanJob) *scanJob {
	copy := *job
	copy.cmd = nil

	// Keep the large raw BDInfo report in server memory, but do not return
	// it with every job-status request. It has a dedicated report endpoint.
	if job.Result != nil {
		resultCopy := *job.Result
		resultCopy.Report = ""
		copy.Result = &resultCopy
	}

	if job.Progress != nil {
		progressCopy := *job.Progress
		copy.Progress = &progressCopy
	}

	if job.StartedAt != nil {
		end := time.Now()
		if job.FinishedAt != nil {
			end = *job.FinishedAt
		}
		copy.ElapsedMS = end.Sub(*job.StartedAt).Milliseconds()
	}

	return &copy
}

func newScanJobID() (string, error) {
	var raw [8]byte

	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(raw[:]), nil
}
