package controller

import (
	"fmt"
	"sync"
	"testing"
)

func TestSongHistory(t *testing.T) {
	sh := NewSongHistory(3)

	entries := []SongHistoryEntry{
		{VideoID: "1", Title: "Song 1"},
		{VideoID: "2", Title: "Song 2"},
		{VideoID: "3", Title: "Song 3"},
		{VideoID: "4", Title: "Song 4"}, // This should cause wrap-around
	}

	for i, e := range entries {
		sh.Add(e)
		t.Run(fmt.Sprintf("AfterAdd%d", i+1), func(t *testing.T) {
			expectedLen := min(3, i+1)
			if got := sh.Len(); got != expectedLen {
				t.Errorf("Len() = %d, want %d", got, expectedLen)
			}
		})
	}

	// Test GetRecent
	recent := sh.GetRecent(2)
	if len(recent) != 2 || recent[0].VideoID != "3" || recent[1].VideoID != "4" {
		t.Errorf("GetRecent(2) = %v, want last 2: 3,4", recent)
	}

	recentAll := sh.GetRecent(10) // more than size
	if len(recentAll) != 3 {
		t.Errorf("GetRecent(10) len=%d, want 3", len(recentAll))
	}

	// Test GetAllVideoIDs
	ids := sh.GetAllVideoIDs()
	expectedIDs := map[string]bool{"2": true, "3": true, "4": true}
	if len(ids) != 3 || !ids["2"] || !ids["3"] || !ids["4"] {
		t.Errorf("GetAllVideoIDs() = %v, want %v", ids, expectedIDs)
	}
}

func TestGuildPlayerIsEmpty(t *testing.T) {
	player := &GuildPlayer{
		Queue: &GuildQueue{},
	}

	if !player.IsEmpty() {
		t.Error("expected empty queue")
	}

	player.Queue.Items = append(player.Queue.Items, &GuildQueueItem{})

	if player.IsEmpty() {
		t.Error("expected non-empty queue")
	}

	// Test race condition mentally: lock protects
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			player.IsEmpty()
		}()
	}
	wg.Wait()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
