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
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "scan job not found"})
		return
	}

	result, status, ok := getStoredScanResult(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "scan job not found"})
		return
	}
	if status != "completed" || result == nil {
		writeJSON(w, http.StatusConflict, errorResponse{Error: "scan has not completed"})
		return
	}

	view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
	if view == "" {
		view = "standard"
	}
	switch view {
	case "standard", "summary", "forums":
	default:
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "view must be standard, summary, or forums"})
		return
	}

	playlist := strings.TrimSpace(r.URL.Query().Get("playlist"))
	report, canonical, ok := selectStoredReport(result, playlist, view)
	if !ok {
		if playlist != "" {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "playlist report was not found"})
		} else {
			writeJSON(w, http.StatusConflict, errorResponse{Error: "scan report is unavailable"})
		}
		return
	}

	if strings.TrimSpace(report) == "" {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: view + " report was not found"})
		return
	}

	writeJSON(w, http.StatusOK, scanReportResponse{
		ID:       id,
		View:     view,
		Playlist: canonical,
		Report:   report,
	})
}

func selectStoredReport(result *scanResponse, requestedPlaylist, view string) (string, string, bool) {
	if len(result.Reports) > 0 {
		if requestedPlaylist != "" {
			canonical := ""
			for _, name := range result.ReportOrder {
				if strings.EqualFold(name, requestedPlaylist) {
					canonical = name
					break
				}
			}
			if canonical == "" {
				return "", "", false
			}
			report := result.Reports[canonical]
			return transformReportView(report, view), canonical, strings.TrimSpace(report) != ""
		}

		parts := make([]string, 0, len(result.ReportOrder))
		for _, name := range result.ReportOrder {
			report := strings.TrimSpace(result.Reports[name])
			if report == "" {
				continue
			}
			transformed := strings.TrimSpace(transformReportView(report, view))
			if transformed != "" {
				parts = append(parts, transformed)
			}
		}
		if len(parts) == 0 {
			return "", "", false
		}
		return strings.Join(parts, "\n\n") + "\n", "", true
	}

	report := result.Report
	if strings.TrimSpace(report) == "" {
		return "", "", false
	}

	canonical := ""
	if requestedPlaylist != "" {
		for _, candidate := range result.Playlists {
			if strings.EqualFold(candidate.Name, requestedPlaylist) {
				canonical = candidate.Name
				break
			}
		}
		if canonical == "" {
			return "", "", false
		}
		selected, ok := extractPlaylistReport(report, canonical)
		if !ok {
			return "", "", false
		}
		report = selected
	}

	return transformReportView(report, view), canonical, true
}

func transformReportView(report, view string) string {
	switch view {
	case "summary":
		return extractSummaryReports(report)
	case "forums":
		return extractForumsReports(report)
	default:
		return report
	}
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

	// Results are immutable once a job reaches a terminal state, so a shallow
	// struct copy is sufficient and avoids duplicating potentially large report
	// strings/maps on every report request.
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
