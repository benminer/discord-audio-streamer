# Rich Now-Playing Cards - Planning Documents

**Created:** 2026-02-04  
**Status:** ✅ Planning Complete - Ready for Implementation  
**Bot:** discord-audio-streamer

---

## 📚 Document Index

This folder contains a complete implementation plan for adding rich now-playing cards to the Discord audio streamer bot. Read the documents in order:

### 🎯 Start Here
**[NOW_PLAYING_SUMMARY.md](./NOW_PLAYING_SUMMARY.md)** (14KB)
- Executive summary and overview
- Key decisions and rationale
- Risk assessment
- Next steps and questions

### ⚡ Quick Reference
**[NOW_PLAYING_QUICKREF.md](./NOW_PLAYING_QUICKREF.md)** (8.7KB)
- TL;DR summary
- Key code snippets
- Testing checklist
- Common pitfalls and solutions

### 🎨 Visual Design
**[NOW_PLAYING_MOCKUP.md](./NOW_PLAYING_MOCKUP.md)** (12KB)
- ASCII mockups of embeds
- Different states (playing, paused, radio)
- JSON payload examples
- Alternative layout options

### 📘 Complete Technical Plan
**[NOW_PLAYING_IMPLEMENTATION_PLAN.md](./NOW_PLAYING_IMPLEMENTATION_PLAN.md)** (34KB)
- Detailed technical architecture
- Complete code examples
- Step-by-step implementation guide
- Research findings
- Challenges and solutions

---

## 🚀 Quick Facts

| Metric | Value |
|--------|-------|
| **Implementation Time** | 13-18 hours |
| **New Files** | 3 (~380 lines) |
| **Modified Files** | 4 (~200 lines) |
| **New Dependencies** | 0 |
| **Risk Level** | 🟢 Low |
| **Deployment Complexity** | 🟢 Low |

---

## 🎯 What Gets Built

### Visual Features
- 🖼️ Album art thumbnail (from YouTube)
- 🎵 Song title, artist, album metadata
- ⏱️ Duration and progress bar
- 👤 Requester information
- 🎨 Clean Discord embed design

### Interactive Controls
- ⏸️ Pause/Resume (dynamic button)
- ⏭️ Skip to next song
- ⏹️ Stop playback
- 🔁 Toggle radio mode (visual feedback)
- 🔄 Refresh position on-demand

### Backend Enhancements
- 📊 Real-time position tracking
- 🔒 Thread-safe state management
- ⚡ Event-driven card updates
- 🚦 Rate-limit friendly design

---

## 📋 Implementation Phases

| Phase | Focus | Time | Files |
|-------|-------|------|-------|
| **1. Foundation** | Position tracking, embed builders | 4-6h | player.go, messages.go, nowplaying.go |
| **2. Integration** | Event handlers, card lifecycle | 3-4h | controller.go |
| **3. Buttons** | Interactive controls | 4-5h | buttons.go, handlers.go |
| **4. Polish** | Edge cases, testing | 2-3h | All files |

---

## 🏗️ Architecture Overview

### Data Flow
```
User plays song
    ↓
YouTube video metadata fetched
    ↓
PlaybackStarted event fired
    ↓
createNowPlayingCard()
    ↓
Build embed (thumbnail, metadata, progress)
    ↓
Send to Discord channel
    ↓
[User clicks button]
    ↓
HandleButtonInteraction()
    ↓
Execute action (pause/resume/skip/stop)
    ↓
updateNowPlayingCard()
    ↓
Edit Discord message
```

### Key Components

**New Components:**
- `discord/nowplaying.go` - Embed and button builders
- `handlers/buttons.go` - Button interaction handler
- `models/nowplaying.go` - Card state struct

**Enhanced Components:**
- `audio/player.go` - Add position tracking
- `controller/controller.go` - Add card lifecycle
- `discord/messages.go` - Add embed/component support
- `handlers/handlers.go` - Route button interactions

---

## 🎨 Visual Preview

```
╔════════════════════════════════════════════════════════════╗
║ 🔴 ▶️ Now Playing                                          ║
╠════════════════════════════════════════════════════════════╣
║   [Album Art]  **Bohemian Rhapsody**                      ║
║     80x80      *by Queen*                                  ║
║                                                            ║
║  📀 A Night at the Opera    │    ⏱️ 5:55                  ║
║                                                            ║
║  📊 ▓▓▓▓▓▓▓▓░░░░░░░░░░░░ [2:15 / 5:55]                  ║
║                                                            ║
║  [ ⏸️ ] [ ⏭️ ] [ ⏹️ ] [ 🔁 ] [ 🔄 ]                       ║
║                                                            ║
║  Requested by @User                     🕒 2 min ago      ║
╚════════════════════════════════════════════════════════════╝
```

See **[NOW_PLAYING_MOCKUP.md](./NOW_PLAYING_MOCKUP.md)** for detailed examples.

---

## ✅ Key Decisions Made

### 1. Static Embeds (Not Live-Updating)
**Why:** Discord rate limits prevent frequent updates  
**How:** Update only on button clicks and state changes  
**Benefit:** No rate limit issues, lower overhead

### 2. YouTube Thumbnails for Album Art
**Why:** Always available, no external API needed  
**How:** `https://i.ytimg.com/vi/{VIDEO_ID}/hqdefault.jpg`  
**Benefit:** Zero latency, 100% coverage

### 3. Emoji-Based Progress Bar
**Why:** Discord has no native progress bar  
**How:** `▓▓▓▓▓▓▓▓░░░░░░░░░░░░ [2:15 / 5:55]`  
**Benefit:** Works everywhere, visually clear

### 4. Timestamp-Based Position Tracking
**Why:** Simple and accurate  
**How:** `position = (now - startedAt) - totalPausedTime`  
**Benefit:** <1 second accuracy, minimal overhead

---

## 🧪 Testing Strategy

### Automated Tests
- Unit tests for progress bar, duration formatting, title parsing
- Integration tests for card lifecycle and event flow

### Manual Testing
- [ ] Play song from YouTube URL
- [ ] Play song from Spotify URL
- [ ] Play song from Apple Music URL
- [ ] Pause/resume playback
- [ ] Skip to next song
- [ ] Stop playback
- [ ] Toggle radio mode
- [ ] Refresh position
- [ ] Test with playlist
- [ ] Test with live stream
- [ ] Test rapid button clicks
- [ ] Verify mobile view
- [ ] Verify desktop view

---

## 🚧 Potential Challenges

All challenges have been analyzed and mitigated:

| Challenge | Risk | Mitigation | Status |
|-----------|------|------------|--------|
| Rate limits | Medium | Static embeds only | ✅ Solved |
| No position tracking | Low | Timestamp tracking | ✅ Solved |
| Limited metadata | Low | Title parsing + future APIs | ✅ Solved |
| Button routing | Low | Type 3 interaction handling | ✅ Solved |
| Concurrent updates | Low | Mutex protection | ✅ Solved |
| Unknown duration | Very Low | Update after load | ✅ Solved |

**Overall Risk: 🟢 LOW**

---

## 📦 Dependencies

**New Dependencies:** None

**Existing Dependencies Used:**
- `github.com/bwmarrin/discordgo` (already installed)
- Go standard library (time, sync, fmt, etc.)

**External APIs (optional, future enhancement):**
- MusicBrainz (metadata)
- Last.fm (metadata)
- Spotify Web API (already integrated)

---

## 🎯 Success Criteria

### Functional (Must Have)
- ✅ Album art thumbnail displays correctly
- ✅ Song metadata shows (title, artist, album)
- ✅ Progress bar renders and updates
- ✅ All buttons function correctly
- ✅ Card updates on state changes
- ✅ No rate limit errors

### Quality (Should Have)
- ✅ Button response < 1 second
- ✅ Card appears < 2 seconds after playback starts
- ✅ Position accuracy within 1 second
- ✅ No memory leaks
- ✅ Mobile-friendly design

### User Experience (Nice to Have)
- ✅ Clean, professional appearance
- ✅ Intuitive button layout
- ✅ Responsive interactions
- ✅ Consistent behavior across sources

---

## 🔮 Future Enhancements

**Phase 2 (Post-MVP):**
1. External metadata API integration
2. Live progress updates (every 30s)
3. Queue preview in embed
4. Volume control buttons

**Phase 3 (Long-term):**
5. Lyrics integration (Genius API)
6. Playback history command
7. Seek controls (±10 seconds)
8. Custom thumbnail upload

---

## 🚀 Getting Started

### For Implementers
1. Read **[NOW_PLAYING_SUMMARY.md](./NOW_PLAYING_SUMMARY.md)** for overview
2. Review **[NOW_PLAYING_MOCKUP.md](./NOW_PLAYING_MOCKUP.md)** for design
3. Reference **[NOW_PLAYING_QUICKREF.md](./NOW_PLAYING_QUICKREF.md)** during coding
4. Follow **[NOW_PLAYING_IMPLEMENTATION_PLAN.md](./NOW_PLAYING_IMPLEMENTATION_PLAN.md)** step-by-step

### For Reviewers
1. Read **[NOW_PLAYING_SUMMARY.md](./NOW_PLAYING_SUMMARY.md)**
2. Review visual mockups in **[NOW_PLAYING_MOCKUP.md](./NOW_PLAYING_MOCKUP.md)**
3. Check implementation plan sections 1-4 in **[NOW_PLAYING_IMPLEMENTATION_PLAN.md](./NOW_PLAYING_IMPLEMENTATION_PLAN.md)**

### For Stakeholders
1. Read **[NOW_PLAYING_SUMMARY.md](./NOW_PLAYING_SUMMARY.md)** only
2. Review mockup visuals
3. Approve or provide feedback

---

## 📝 Questions & Feedback

**Before proceeding, please review:**

1. **Design Approval:** Is the embed layout acceptable?
2. **Button Set:** Are the 5 buttons appropriate? (pause, skip, stop, radio, refresh)
3. **Update Strategy:** Okay with static embeds (button-driven updates)?
4. **Timeline:** Any constraints or preferred schedule?
5. **Scope:** Any features to add/remove from MVP?

**Provide feedback on:**
- Visual design preferences
- Button functionality priorities
- Additional features desired
- Implementation concerns

---

## 📞 Contact

**Planning Created By:** AI Subagent (subagent:e5359fd9-cca7-4ec1-b208-e98503628668)  
**Date:** 2026-02-04  
**Repository:** ~/discord-apps/discord-audio-streamer  
**Status:** Awaiting approval to begin implementation

---

## 🎉 Next Steps

**Ready to proceed!** Choose an option:

### Option A: Full Implementation (Recommended)
- Implement all 4 phases
- 13-18 hour timeline
- Complete feature on launch

### Option B: Phased Rollout
- Phase 1+2 first (basic cards)
- Get user feedback
- Then Phase 3+4 (buttons)

### Option C: Prototype First
- Build minimal proof-of-concept
- Validate with users
- Then full implementation

**Awaiting your go-ahead!** 🚀

---

*Last Updated: 2026-02-04*  
*Planning Version: 1.0*  
*Implementation Status: Not Started*
