# Now-Playing Implementation Quick Reference

## TL;DR

Add rich embedded now-playing cards with album art, metadata, and interactive buttons to the Discord audio streamer bot.

**Implementation Time:** 13-18 hours  
**Dependencies:** None (uses existing discordgo library)  
**Risk Level:** Low

---

## Key Architecture Decisions

### ✅ Static Embeds (Not Live-Updating)
- **Why:** Discord rate limits prevent frequent updates (10 per 10 seconds)
- **Solution:** Static embeds that update only on user action (button clicks)

### ✅ YouTube Thumbnails for Album Art
- **URL:** `https://i.ytimg.com/vi/{VIDEO_ID}/hqdefault.jpg`
- **Always available, no external API needed**

### ✅ Emoji-Based Progress Bar
- **Why:** Discord has no native progress bar component
- **Example:** `▓▓▓▓▓▓▓▓░░░░░░░░░░░░ [2:15 / 5:55]`

### ✅ Position Tracking via Timestamps
- **Formula:** `position = (now - startedAt) - totalPausedTime`
- **Simple and accurate**

---

## What Gets Built

### Visual Components
1. **Embed with:**
   - Album art thumbnail (80x80)
   - Song title, artist, album
   - Duration and progress bar
   - Requester info in footer

2. **Interactive Buttons:**
   - ⏸️ Pause / ▶️ Resume (toggles)
   - ⏭️ Skip
   - ⏹️ Stop
   - 🔁 Radio Toggle (visual feedback)
   - 🔄 Refresh (update position on-demand)

### Backend Components
1. **Position Tracking** (audio/player.go)
   - Track start time, pause times, duration
   - `GetPosition()` and `GetDuration()` methods

2. **Card State Management** (controller/controller.go)
   - `NowPlayingCard` struct
   - Create/update/clear methods
   - Thread-safe with mutex

3. **Embed Builder** (discord/nowplaying.go)
   - `CreateNowPlayingEmbed()` - builds rich embed
   - `CreatePlaybackButtons()` - builds button row
   - Progress bar rendering helpers

4. **Button Handler** (handlers/buttons.go)
   - Route button interactions
   - Execute playback commands
   - Update card state

---

## Files to Create

| File | Purpose | Lines |
|------|---------|-------|
| `discord/nowplaying.go` | Embed/button builders | ~200 |
| `handlers/buttons.go` | Button interaction handler | ~150 |
| `models/nowplaying.go` | NowPlayingCard struct | ~30 |

**Total New Code:** ~380 lines

---

## Files to Modify

| File | Changes | Lines |
|------|---------|-------|
| `discord/messages.go` | Add embeds/components support | ~30 |
| `controller/controller.go` | Add card methods, integrate events | ~100 |
| `audio/player.go` | Add position tracking | ~50 |
| `handlers/handlers.go` | Route button interactions | ~20 |

**Total Modifications:** ~200 lines

---

## Implementation Phases

### Phase 1: Foundation (4-6 hours)
- Add position tracking to player
- Enhance message structures for embeds/components
- Create embed builder functions
- Add card state to GuildPlayer

### Phase 2: Playback Integration (3-4 hours)
- Integrate with playback events
- Create/update/clear card on state changes
- Test basic display

### Phase 3: Interactive Buttons (4-5 hours)
- Add button interaction handler
- Implement all button actions
- Test button functionality

### Phase 4: Polish (2-3 hours)
- Handle edge cases
- Add duration metadata
- Final testing

**Total:** 13-18 hours

---

## Discord API Quick Reference

### Embed Structure
```go
&discordgo.MessageEmbed{
    Title:       "▶️ Now Playing",
    Description: "**Song** by *Artist*",
    Color:       0xFF0000, // Red
    Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: "..."},
    Fields:      []*discordgo.MessageEmbedField{...},
    Footer:      &discordgo.MessageEmbedFooter{Text: "..."},
    Timestamp:   time.Now().Format(time.RFC3339),
}
```

### Button Structure
```go
discordgo.Button{
    Label:    "⏸️ Pause",
    Style:    discordgo.PrimaryButton, // 1=Blue, 2=Gray, 3=Green, 4=Red
    CustomID: "np_pause", // Max 100 chars
}
```

### Button Interaction Flow
1. User clicks button
2. Discord sends `INTERACTION_CREATE` (Type 3)
3. Bot must respond within 3 seconds
4. Send deferred response (Type 6)
5. Execute action
6. Update message via API

---

## Key Code Snippets

### Progress Bar
```go
func createProgressBar(pos, dur time.Duration, len int) string {
    ratio := float64(pos) / float64(dur)
    filled := int(ratio * float64(len))
    
    bar := ""
    for i := 0; i < len; i++ {
        if i < filled {
            bar += "▓"
        } else {
            bar += "░"
        }
    }
    
    return fmt.Sprintf("%s [%s / %s]", bar, 
        formatDuration(pos), formatDuration(dur))
}
```

### Position Calculation
```go
func (p *Player) GetPosition() time.Duration {
    if !p.IsPlaying() {
        return 0
    }
    
    elapsed := time.Since(p.startedAt)
    if p.paused.Load() {
        elapsed = p.pausedAt.Sub(p.startedAt)
    }
    return elapsed - p.totalPausedTime
}
```

### Card Update
```go
func (p *GuildPlayer) updateNowPlayingCard() {
    p.cardMutex.Lock()
    defer p.cardMutex.Unlock()
    
    if p.NowPlayingCard == nil {
        return
    }
    
    embed := discord.CreateNowPlayingEmbed(...)
    buttons := discord.CreatePlaybackButtons(...)
    
    discord.EditMessageWithComponents(
        p.NowPlayingCard.ChannelID,
        p.NowPlayingCard.MessageID,
        []*discordgo.MessageEmbed{embed},
        buttons,
    )
}
```

---

## Testing Checklist

### Functional
- [ ] Card appears on playback start
- [ ] Thumbnail loads correctly
- [ ] Metadata displays correctly
- [ ] Progress bar renders properly
- [ ] Buttons work (pause, resume, skip, stop, radio, refresh)
- [ ] Card updates on state changes
- [ ] Card clears on playback end

### Edge Cases
- [ ] Handle missing thumbnail (fallback)
- [ ] Handle very long titles (truncate)
- [ ] Handle live streams (no duration)
- [ ] Handle radio-queued songs (no requester)
- [ ] Handle concurrent button clicks
- [ ] Handle bot disconnect during playback

### Performance
- [ ] No rate limit errors under normal usage
- [ ] Button response < 1 second
- [ ] Card appears within 2 seconds of playback
- [ ] No memory leaks from old cards

---

## Common Pitfalls & Solutions

### ❌ Problem: Live-updating embeds hit rate limits
**✅ Solution:** Only update on user action (button clicks)

### ❌ Problem: No position tracking in player
**✅ Solution:** Add timestamp tracking (startedAt, pausedAt)

### ❌ Problem: YouTube only provides title
**✅ Solution:** Parse title for artist/song, use thumbnail

### ❌ Problem: Duration unknown until loaded
**✅ Solution:** Update embed after LoadResult available

### ❌ Problem: Button interactions not routed
**✅ Solution:** Check interaction.Type, route Type 3 to button handler

### ❌ Problem: Concurrent card updates
**✅ Solution:** Add mutex to GuildPlayer.cardMutex

---

## Configuration

No new config needed! Uses existing:
- Discord session (already connected)
- YouTube video IDs (already available)
- Player state (already tracked)

---

## Rollout Strategy

### Phase 1: Soft Launch
- Deploy to single test server
- Monitor for errors/rate limits
- Gather user feedback

### Phase 2: Full Deployment
- Deploy to all servers
- Announce feature in changelog
- Monitor metrics

### Rollback Plan
- Feature can be disabled via boolean flag
- No database changes, easy rollback
- Old text-based notifications still work

---

## Future Enhancements

1. **External Metadata API** (MusicBrainz, Last.fm)
   - Accurate artist/album info
   - Higher quality album art

2. **Live Progress Updates**
   - Update every 30 seconds (rate limit safe)
   - WebSocket alternative

3. **Queue Preview in Embed**
   - Show next 3-5 songs

4. **Lyrics Integration**
   - Genius API
   - Button to show/hide lyrics

5. **Playback History**
   - "Recently Played" command
   - Reuse embed format

6. **Volume Controls**
   - Volume up/down buttons
   - Display current volume

---

## Success Criteria

### Must Have (MVP)
✅ Album art displayed  
✅ Song metadata shown  
✅ Progress bar rendered  
✅ All buttons functional  
✅ No rate limit errors  

### Nice to Have (Future)
⏳ External metadata lookup  
⏳ Live progress updates  
⏳ Queue preview  
⏳ Lyrics integration  

---

## Questions & Answers

**Q: Will this hit Discord rate limits?**  
A: No. Static embeds + button-driven updates stay well under limits.

**Q: What if YouTube thumbnail doesn't load?**  
A: Use fallback image or Discord's default placeholder.

**Q: How accurate is position tracking?**  
A: Within 1 second (timestamp-based, updated on refresh).

**Q: Can users spam buttons?**  
A: Discord enforces 3-second cooldown per interaction automatically.

**Q: What about Spotify/Apple Music tracks?**  
A: Same system - they already get YouTube video IDs for playback.

---

*Last Updated: 2026-02-04*
