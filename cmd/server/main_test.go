package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autobrr/go-bdinfo/pkg/bdinfo"
)

func TestSelectMainPlaylistPrefersValidLongest(t *testing.T) {
	playlists := []bdinfo.PlaylistInfo{
		{Name: "00001.MPLS", LengthSeconds: 100, SizeBytes: 1000, TotalBitrateBps: 10, IsValid: true},
		{Name: "00002.MPLS", LengthSeconds: 200, SizeBytes: 2000, TotalBitrateBps: 20, IsValid: true},
		{Name: "00003.MPLS", LengthSeconds: 300, SizeBytes: 3000, TotalBitrateBps: 30, IsValid: false},
	}

	main, valid, filtered := selectMainPlaylist(playlists)
	if main == nil {
		t.Fatal("expected a main playlist")
	}
	if main.Name != "00002.MPLS" {
		t.Fatalf("main playlist = %q, want 00002.MPLS", main.Name)
	}
	if valid != 2 || filtered != 1 {
		t.Fatalf("counts = valid %d filtered %d, want 2 and 1", valid, filtered)
	}
}

func TestSelectMainPlaylistFallsBackWhenAllFiltered(t *testing.T) {
	playlists := []bdinfo.PlaylistInfo{
		{Name: "00001.MPLS", LengthSeconds: 100, IsValid: false},
		{Name: "00002.MPLS", LengthSeconds: 200, IsValid: false},
	}

	main, valid, filtered := selectMainPlaylist(playlists)
	if main == nil || main.Name != "00002.MPLS" {
		t.Fatalf("main = %#v, want 00002.MPLS", main)
	}
	if valid != 0 || filtered != 2 {
		t.Fatalf("counts = valid %d filtered %d, want 0 and 2", valid, filtered)
	}
}

func TestPathInsideRootRespectsPathBoundaries(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "data")

	if !pathInsideRoot(root, filepath.Join(root, "movies", "disc")) {
		t.Fatal("expected nested path to be inside root")
	}
	if !pathInsideRoot(root, root) {
		t.Fatal("expected root itself to be inside root")
	}
	if pathInsideRoot(root, root+"2") {
		t.Fatal("sibling path with shared prefix must not be inside root")
	}
}

func TestNormalizeReportDiscLabelOnlyReplacesGenericBDMV(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "Release.Name", "BDMV")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	generic := "Disc Label: BDMV\nOther: value\n"
	got := normalizeReportDiscLabel(source, generic)
	want := "Disc Label: Release.Name\nOther: value\n"
	if got != want {
		t.Fatalf("generic label result = %q, want %q", got, want)
	}

	real := "Disc Label: REAL_LABEL\n"
	if got := normalizeReportDiscLabel(source, real); got != real {
		t.Fatalf("meaningful label changed: %q", got)
	}
}
