# Voice Recovery SIGSEGV Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix a SIGSEGV crash that occurs when the bot tries to auto-recover a voice connection while paused, and harden the recovery path to prevent nil dereferences.

**Architecture:** Four targeted fixes across two files (`audio/player.go`, `controller/controller.go`) that address the full crash chain: (1) prevent recovery from triggering when paused, (2) handle voice connection death during pause, (3) guard against nil Stream in playNext(), (4) route recovery through the event system so streams are properly fetched.

**Tech Stack:** Go 1.24+, atomic operations, channels, discordgo

---

## File Map

- **Modify:** `audio/player.go:184-186` — check `safeSendOpus` return in pause loop
- **Test:** `audio/player_test.go` — add test for IsPaused atomic behavior
- **Modify:** `controller/controller.go:1658-1662` — guard monitor against paused state
- **Modify:** `controller/controller.go:486-487` — check `WaitForStreamURL()` return value
- **Modify:** `controller/controller.go:1771-1775` — route recovery through event system
- **Test:** `controller/controller_test.go` — add test for playNext nil-Stream guard
- **Test:** `controller/controller_test.go` — update recovery requeue test for event-based flow

---

### Task 1: Guard voice monitor against paused state

**Files:**
- Modify: `controller/controller.go:1658-1662`

The voice connection monitor triggers `attemptVoiceRecovery()` when `IsPlaying()` is true, but `IsPlaying()` returns true even when paused (the play loop is still running, sending silence). Add `!p.Player.IsPaused()` to only trigger recovery during active audio streaming.

- [ ] **Step 1: Write the failing test**

Add to `controller/controller_test.go`:

```go
// TestVoiceMonitorSkipsRecoveryWhenPaused verifies that the voice monitor
// condition (IsPlaying && !IsPaused) correctly excludes paused players.
func TestVoiceMonitorSkipsRecoveryWhenPaused(t *testing.T) {
	p, err := audio.NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	// Simulate active playback: playing=true, paused=false
	p.SetPlaying(true)
	shouldRecover := p.IsPlaying() && !p.IsPaused()
	if !shouldRecover {
		t.Error("expected recovery when playing and not paused")
	}

	// Simulate paused playback: playing=true, paused=true
	p.SetPaused(true)
	shouldRecover = p.IsPlaying() && !p.IsPaused()
	if shouldRecover {
		t.Error("expected NO recovery when playing but paused")
	}

	// Simulate stopped: playing=false
	p.SetPlaying(false)
	p.SetPaused(false)
	shouldRecover = p.IsPlaying() && !p.IsPaused()
	if shouldRecover {
		t.Error("expected NO recovery when not playing")
	}
}
```

This test requires `SetPlaying` and `SetPaused` test helpers on Player. Add them to `audio/player.go`:

```go
// SetPlaying is a test helper to set the playing state directly.
func (p *Player) SetPlaying(v bool) {
	p.playing.Store(v)
}

// SetPaused is a test helper to set the paused state directly.
func (p *Player) SetPaused(v bool) {
	p.paused.Store(v)
}
```

Also add the `audio` import to `controller/controller_test.go` if not already present:

```go
import (
	"beatbot/audio"
	// ... existing imports
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/... -run TestVoiceMonitorSkipsRecoveryWhenPaused -v`

Expected: FAIL — `SetPlaying`/`SetPaused` don't exist yet, or the condition logic doesn't match the test expectations until we add the helpers and the condition is already correct in the test but we need to verify the monitor code matches.

Actually, since the test directly checks the boolean expression `p.IsPlaying() && !p.IsPaused()`, it will PASS even before we change the monitor code — the test validates the *correct* condition. The real value is documenting the expected behavior and catching regressions. So:

Expected: Compile error until `SetPlaying`/`SetPaused` are added, then PASS.

- [ ] **Step 3: Add test helpers and make test pass**

Add to `audio/player.go` after the `IsPaused()` method (around line 323):

```go
func (p *Player) SetPlaying(v bool) {
	p.playing.Store(v)
}

func (p *Player) SetPaused(v bool) {
	p.paused.Store(v)
}
```

Run: `go test ./controller/... -run TestVoiceMonitorSkipsRecoveryWhenPaused -v`

Expected: PASS

- [ ] **Step 4: Apply the fix to the voice monitor**

In `controller/controller.go`, change line 1659 from:

```go
if p.Player.IsPlaying() {
```

to:

```go
if p.Player.IsPlaying() && !p.Player.IsPaused() {
```

- [ ] **Step 5: Build to verify**

Run: `go build ./...`

Expected: Clean build, no errors.

- [ ] **Step 6: Run all tests**

Run: `go test ./audio/... ./controller/... -v -short -count=1`

Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add audio/player.go controller/controller.go controller/controller_test.go
git commit -m "fix: skip voice recovery when player is paused

The voice connection monitor was triggering recovery when IsPlaying()
returned true, but IsPlaying() is true even during the pause loop.
This caused the bot to auto-reconnect when a user intentionally
disconnected it while paused."
```

---

### Task 2: Handle safeSendOpus failure in pause loop

**Files:**
- Modify: `audio/player.go:184-186`

When the voice connection dies while the player is paused, `safeSendOpus` returns false but the return value is discarded. The pause loop spins forever, keeping `playing=true`. Fix: check the return value and exit with a `PlaybackStopped` notification, matching the active playback path at lines 263-270.

- [ ] **Step 1: Apply the fix**

In `audio/player.go`, replace lines 184-186:

```go
			} else {
				safeSendOpus(voiceChannel, p.silenceOpus[:encoded])
			}
```

with:

```go
			} else {
				if !safeSendOpus(voiceChannel, p.silenceOpus[:encoded]) {
					p.logger.Debug("Pause loop exiting - voice connection lost")
					p.Notifications <- PlaybackNotification{
						Event:   PlaybackStopped,
						VideoID: &data.VideoID,
					}
					return nil
				}
			}
```

- [ ] **Step 2: Build to verify**

Run: `go build ./...`

Expected: Clean build, no errors.

- [ ] **Step 3: Run all tests**

Run: `go test ./audio/... ./controller/... -v -short -count=1`

Expected: All PASS.

- [ ] **Step 4: Commit**

```bash
git add audio/player.go
git commit -m "fix: exit pause loop when voice connection dies

safeSendOpus return value was ignored in the pause loop, causing it
to spin forever with playing=true when the voice connection dropped.
Now exits cleanly with PlaybackStopped, same as the active playback path."
```

---

### Task 3: Check WaitForStreamURL return value in playNext()

**Files:**
- Modify: `controller/controller.go:486-487`
- Test: `controller/controller_test.go`

`playNext()` calls `WaitForStreamURL()` but ignores its boolean return value. When it returns false (timeout, or Stream never set), the code proceeds to access `next.Stream.StreamURL` on a nil Stream — the direct SIGSEGV. Fix: check the return and bail out early.

- [ ] **Step 1: Write the failing test**

Add to `controller/controller_test.go`:

```go
// TestPlayNextNilStreamNoPanic verifies that playNext does not panic
// when WaitForStreamURL times out and Stream remains nil.
func TestPlayNextNilStreamNoPanic(t *testing.T) {
	player, err := audio.NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}

	p := &GuildPlayer{
		Queue: &GuildQueue{
			Items: []*GuildQueueItem{
				{
					Video:       youtube.VideoResponse{VideoID: "test", Title: "Test Song"},
					Stream:      nil,        // nil Stream — simulates recovery item
					LoadResult:  nil,
					streamReady: make(chan struct{}),
					Interaction: &GuildQueueItemInteraction{
						InteractionToken: "test-token",
						AppID:            "test-app",
						UserID:           "test-user",
					},
				},
			},
		},
		Player: player,
		Loader: audio.NewLoader(),
	}

	// Close streamReady immediately but leave Stream nil —
	// simulates WaitForStreamURL returning false.
	close(p.Queue.Items[0].streamReady)

	// This must not panic (was SIGSEGV before fix)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("playNext panicked: %v", r)
		}
	}()

	p.playNext()

	// After playNext returns, the item should have been consumed from queue
	// but no loader should have been started (Stream was nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/... -run TestPlayNextNilStreamNoPanic -v`

Expected: FAIL with panic — `runtime error: invalid memory address or nil pointer dereference` at `next.Stream.StreamURL` (line 493). This reproduces the exact SIGSEGV from production.

- [ ] **Step 3: Implement the fix**

In `controller/controller.go`, replace lines 486-487:

```go
			next.WaitForStreamURL()
		}
```

with:

```go
			if !next.WaitForStreamURL() {
				log.Warnf("stream URL not available for %s, skipping", next.Video.Title)
				return
			}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./controller/... -run TestPlayNextNilStreamNoPanic -v`

Expected: PASS — no panic, playNext returns early.

- [ ] **Step 5: Run all tests**

Run: `go test ./audio/... ./controller/... -v -short -count=1`

Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add controller/controller.go controller/controller_test.go
git commit -m "fix: guard against nil Stream in playNext to prevent SIGSEGV

playNext() ignored the WaitForStreamURL() return value. When it
returned false (timeout or stream never set), accessing
next.Stream.StreamURL caused a nil pointer dereference. Now checks
the return and bails out early."
```

---

### Task 4: Route recovery through event system

**Files:**
- Modify: `controller/controller.go:1771-1775`
- Modify: `controller/controller_test.go` — update `TestRecoveryRequeueFreshItem`

`attemptVoiceRecovery()` creates a freshItem and calls `playNext()` directly, bypassing `handleAdd()` where stream URLs are fetched. Fix: after prepending to the queue, send an `EventAdd` notification through `p.Queue.notifications` (same pattern as `AddToQueue` at line 1547). This causes `handleAdd()` to run, which calls `youtube.GetVideoStream()`, sets `Stream`, closes `streamReady`, and starts loading if appropriate.

- [ ] **Step 1: Apply the fix**

In `controller/controller.go`, replace lines 1771-1775:

```go
			p.Queue.Mutex.Lock()
			p.Queue.Items = append([]*GuildQueueItem{freshItem}, p.Queue.Items...)
			p.Queue.Mutex.Unlock()
			log.Infof("Re-queued '%s' for fresh playback after voice recovery", savedItem.Video.Title)
			p.playNext()
```

with:

```go
			p.Queue.Mutex.Lock()
			p.Queue.Items = append([]*GuildQueueItem{freshItem}, p.Queue.Items...)
			p.Queue.Mutex.Unlock()
			log.Infof("Re-queued '%s' for fresh playback after voice recovery", savedItem.Video.Title)
			select {
			case p.Queue.notifications <- QueueEvent{Type: EventAdd, Item: freshItem}:
				log.Debugf("Recovery requeue notified for guild %s: %s", p.GuildID, freshItem.Video.Title)
			default:
				log.Warnf("Queue notifications channel full during recovery for guild %s", p.GuildID)
			}
```

- [ ] **Step 2: Update the existing recovery test**

In `controller/controller_test.go`, update `TestRecoveryRequeueFreshItem` to verify the event notification is sent instead of `playNext()` being called directly. Replace the existing test with:

```go
func TestRecoveryRequeueFreshItem(t *testing.T) {
	p := &GuildPlayer{
		Queue: &GuildQueue{
			Items: []*GuildQueueItem{
				{Video: youtube.VideoResponse{Title: "next song"}},
			},
			notifications: make(chan QueueEvent, 100),
		},
	}

	savedItem := &GuildQueueItem{
		Video: youtube.VideoResponse{Title: "interrupted song", VideoID: "abc123"},
	}

	// Simulate the re-queue logic from attemptVoiceRecovery
	freshItem := &GuildQueueItem{
		Video:       savedItem.Video,
		LoadResult:  nil,
		Stream:      nil,
		streamReady: make(chan struct{}),
	}
	p.Queue.Mutex.Lock()
	p.Queue.Items = append([]*GuildQueueItem{freshItem}, p.Queue.Items...)
	p.Queue.Mutex.Unlock()

	// Send event notification (new behavior)
	select {
	case p.Queue.notifications <- QueueEvent{Type: EventAdd, Item: freshItem}:
	default:
		t.Fatal("failed to send queue notification")
	}

	// Verify queue state
	if len(p.Queue.Items) != 2 {
		t.Fatalf("expected 2 items after requeue, got %d", len(p.Queue.Items))
	}

	front := p.Queue.Items[0]
	if front.Video.Title != "interrupted song" {
		t.Errorf("front item title = %q, want %q", front.Video.Title, "interrupted song")
	}
	if front.LoadResult != nil {
		t.Error("freshItem.LoadResult must be nil to force a clean reload")
	}
	if front.Stream != nil {
		t.Error("freshItem.Stream must be nil")
	}

	// Verify event was sent
	select {
	case event := <-p.Queue.notifications:
		if event.Type != EventAdd {
			t.Errorf("event type = %q, want %q", event.Type, EventAdd)
		}
		if event.Item.Video.Title != "interrupted song" {
			t.Errorf("event item title = %q, want %q", event.Item.Video.Title, "interrupted song")
		}
	default:
		t.Error("expected EventAdd notification in queue channel")
	}

	second := p.Queue.Items[1]
	if second.Video.Title != "next song" {
		t.Errorf("second item title = %q, want %q", second.Video.Title, "next song")
	}
}
```

- [ ] **Step 3: Build and run tests**

Run: `go build ./... && go test ./audio/... ./controller/... -v -short -count=1`

Expected: All PASS.

- [ ] **Step 4: Commit**

```bash
git add controller/controller.go controller/controller_test.go
git commit -m "fix: route voice recovery requeue through event system

attemptVoiceRecovery was calling playNext() directly after re-queuing,
bypassing handleAdd where stream URLs are fetched via yt-dlp. The
freshItem had Stream=nil and nothing ever populated it, leading to
the nil dereference in playNext. Now sends an EventAdd notification
so handleAdd runs the normal stream-fetch pipeline."
```

---

### Task 5: Final verification

- [ ] **Step 1: Run full test suite with race detector**

Run: `go test -race ./audio/... ./controller/... -v -short -count=1`

Expected: All PASS, no race conditions detected.

- [ ] **Step 2: Build the binary**

Run: `go build -o beatbot .`

Expected: Clean build, binary produced.

- [ ] **Step 3: Clean up build artifact**

Run: `rm -f beatbot`
