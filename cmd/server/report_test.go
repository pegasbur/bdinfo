package main

import (
	"strings"
	"testing"
)

const sampleMultiPlaylistReport = `********************
PLAYLIST: 00001.MPLS
********************
DISC INFO:
Disc Label: DISC

<--- BEGIN FORUMS PASTE --->
forum one
<---- END FORUMS PASTE ---->

QUICK SUMMARY:
Video: AVC

********************
PLAYLIST: 00002.MPLS
********************
DISC INFO:
Disc Label: DISC

<--- BEGIN FORUMS PASTE --->
forum two
<---- END FORUMS PASTE ---->

QUICK SUMMARY:
Video: HEVC
`

func TestExtractPlaylistReport(t *testing.T) {
	report, ok := extractPlaylistReport(sampleMultiPlaylistReport, "00002.mpls")
	if !ok {
		t.Fatal("expected playlist report")
	}
	if !strings.Contains(report, "PLAYLIST: 00002.MPLS") {
		t.Fatalf("wrong playlist report: %q", report)
	}
	if strings.Contains(report, "PLAYLIST: 00001.MPLS") {
		t.Fatalf("playlist report leaked previous section: %q", report)
	}
}

func TestExtractSummaryReports(t *testing.T) {
	report := extractSummaryReports(sampleMultiPlaylistReport)
	if !strings.Contains(report, "QUICK SUMMARY:\nVideo: AVC") {
		t.Fatalf("first summary missing: %q", report)
	}
	if !strings.Contains(report, "QUICK SUMMARY:\nVideo: HEVC") {
		t.Fatalf("second summary missing: %q", report)
	}
}

func TestExtractForumsReports(t *testing.T) {
	report := extractForumsReports(sampleMultiPlaylistReport)
	if !strings.Contains(report, "forum one") || !strings.Contains(report, "forum two") {
		t.Fatalf("forums blocks missing: %q", report)
	}
	if !strings.Contains(report, "<--- BEGIN FORUMS PASTE --->") ||
		!strings.Contains(report, "<---- END FORUMS PASTE ---->") {
		t.Fatalf("forums markers should be preserved: %q", report)
	}
}
