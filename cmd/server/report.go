package main

import (
	"net/http"
	"strings"
)

type scanReportResponse struct {
	ID       string `json:"id"`
	View     string `json:"view"`
	Playlist string `json:"playlist,omitempty"`
	Report   string `json:"report"`
}

func handleScanReport(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error: "scan job not found",
		})
		return
	}

	result, status, ok := getStoredScanResult(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error: "scan job not found",
		})
		return
	}

	if status != "completed" || result == nil {
		writeJSON(w, http.StatusConflict, errorResponse{
			Error: "scan has not completed",
		})
		return
	}

	if strings.TrimSpace(result.Report) == "" {
		writeJSON(w, http.StatusConflict, errorResponse{
			Error: "scan report is unavailable",
		})
		return
	}

	view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
	if view == "" {
		view = "standard"
	}

	switch view {
	case "standard", "summary", "forums":
	default:
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "view must be standard, summary, or forums",
		})
		return
	}

	playlist := strings.TrimSpace(r.URL.Query().Get("playlist"))

	report := result.Report

	if playlist != "" {
		canonical := ""

		for _, candidate := range result.Playlists {
			if strings.EqualFold(candidate.Name, playlist) {
				canonical = candidate.Name
				break
			}
		}

		if canonical == "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: "playlist was not part of this scan",
			})
			return
		}

		selected, ok := extractPlaylistReport(report, canonical)
		if !ok {
			writeJSON(w, http.StatusNotFound, errorResponse{
				Error: "playlist report was not found",
			})
			return
		}

		playlist = canonical
		report = selected
	}

	switch view {
	case "summary":
		report = extractSummaryReports(report)

	case "forums":
		report = extractForumsReports(report)
	}

	if strings.TrimSpace(report) == "" {
		writeJSON(w, http.StatusNotFound, errorResponse{
			Error: view + " report was not found",
		})
		return
	}

	writeJSON(w, http.StatusOK, scanReportResponse{
		ID:       id,
		View:     view,
		Playlist: playlist,
		Report:   report,
	})
}

func getStoredScanResult(id string) (*scanResponse, string, bool) {
	scanJobs.Lock()
	defer scanJobs.Unlock()

	job, ok := scanJobs.jobs[id]
	if !ok {
		return nil, "", false
	}

	if job.Result == nil {
		return nil, job.Status, true
	}

	result := *job.Result
	return &result, job.Status, true
}

func extractPlaylistReport(report, playlist string) (string, bool) {
	for _, section := range splitPlaylistSections(report) {
		name := playlistNameFromSection(section)
		if strings.EqualFold(name, playlist) {
			return strings.TrimSpace(section) + "\n", true
		}
	}

	return "", false
}

func splitPlaylistSections(report string) []string {
	const marker = "********************\nPLAYLIST: "

	var starts []int
	offset := 0

	for {
		idx := strings.Index(report[offset:], marker)
		if idx < 0 {
			break
		}

		start := offset + idx
		starts = append(starts, start)
		offset = start + len(marker)

		if offset >= len(report) {
			break
		}
	}

	if len(starts) == 0 {
		return nil
	}

	sections := make([]string, 0, len(starts))

	for i, start := range starts {
		end := len(report)
		if i+1 < len(starts) {
			end = starts[i+1]
		}

		sections = append(
			sections,
			strings.TrimSpace(report[start:end]),
		)
	}

	return sections
}

func playlistNameFromSection(section string) string {
	const marker = "********************\nPLAYLIST: "

	if !strings.HasPrefix(section, marker) {
		return ""
	}

	rest := section[len(marker):]

	if idx := strings.IndexByte(rest, '\n'); idx >= 0 {
		rest = rest[:idx]
	}

	return strings.TrimSpace(rest)
}

func reportSections(report string) []string {
	sections := splitPlaylistSections(report)
	if len(sections) > 0 {
		return sections
	}

	return []string{report}
}

func extractSummaryReports(report string) string {
	var summaries []string

	for _, section := range reportSections(report) {
		idx := strings.Index(section, "QUICK SUMMARY:")
		if idx < 0 {
			continue
		}

		summary := strings.TrimSpace(section[idx:])
		if summary != "" {
			summaries = append(summaries, summary)
		}
	}

	return strings.Join(summaries, "\n\n")
}

func extractForumsReports(report string) string {
	const beginMarker = "<--- BEGIN FORUMS PASTE --->"
	const endMarker = "<---- END FORUMS PASTE ---->"

	var forums []string

	for _, section := range reportSections(report) {
		begin := strings.Index(section, beginMarker)
		if begin < 0 {
			continue
		}

		rest := section[begin:]
		end := strings.Index(rest, endMarker)
		if end < 0 {
			continue
		}

		end += len(endMarker)

		block := strings.TrimSpace(rest[:end])
		if block != "" {
			forums = append(forums, block)
		}
	}

	return strings.Join(forums, "\n\n")
}
