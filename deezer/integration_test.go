//go:build integration

package deezer

import (
	"context"
	"testing"
	"time"
)

func TestIntegration_SearchArtist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	artist, err := SearchArtist(ctx, "Daft Punk")
	if err != nil {
		t.Fatalf("SearchArtist: %v", err)
	}
	if artist == nil {
		t.Fatal("expected non-nil artist")
	}
	if artist.Name != "Daft Punk" {
		t.Errorf("artist.Name = %q, want %q", artist.Name, "Daft Punk")
	}
	if artist.ID == 0 {
		t.Error("artist.ID should not be 0")
	}
}

func TestIntegration_GetArtistRadio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tracks, err := GetArtistRadio(ctx, 27) // Daft Punk
	if err != nil {
		t.Fatalf("GetArtistRadio: %v", err)
	}
	if len(tracks) == 0 {
		t.Fatal("expected tracks from artist radio")
	}
	for _, track := range tracks {
		if track.Title == "" {
			t.Error("track has empty title")
		}
		if track.Artist.Name == "" {
			t.Error("track has empty artist name")
		}
	}
}

func TestIntegration_GetCharts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	charts, err := GetCharts(ctx)
	if err != nil {
		t.Fatalf("GetCharts: %v", err)
	}
	if len(charts.Tracks.Data) == 0 {
		t.Fatal("expected chart tracks")
	}
}

func TestIntegration_SearchTrack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	track, err := SearchTrack(ctx, "Daft Punk", "Around the World")
	if err != nil {
		t.Fatalf("SearchTrack: %v", err)
	}
	if track == nil {
		t.Fatal("expected non-nil track")
	}
	if track.Duration == 0 {
		t.Error("track duration should not be 0")
	}
}

func TestIntegration_GetTrackDetail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	detail, err := GetTrack(ctx, 3135556) // Around the World
	if err != nil {
		t.Fatalf("GetTrack: %v", err)
	}
	if detail.Title == "" {
		t.Error("expected non-empty title")
	}
	// BPM may be 0 for some tracks
	t.Logf("BPM: %.1f", detail.BPM)
}

func TestIntegration_GetGenreStations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	genres, err := GetGenreStations(ctx)
	if err != nil {
		t.Fatalf("GetGenreStations: %v", err)
	}
	if len(genres) == 0 {
		t.Fatal("expected genre stations")
	}
}
