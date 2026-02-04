# Rich Now-Playing Cards Implementation Plan

**Status:** Planning Phase - Ready for Review
**Created:** 2025-02-04
**Language:** Go
**Discord Library:** discordgo v0.29.0 (forked)

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Discord API Research](#discord-api-research)
3. [Architecture Overview](#architecture-overview)
4. [Data Sources for Metadata](#data-sources-for-metadata)
5. [Code Structure](#code-structure)
6. [Embed Mockup](#embed-mockup)
7. [Implementation Steps](#implementation-steps)
8. [Challenges and Solutions](#challenges-and-solutions)
9. [Testing Strategy](#testing-strategy)

---

## Executive Summary

This plan outlines adding rich now-playing cards with album art, metadata, progress tracking, and interactive buttons to the Discord audio streamer bot. The implementation leverages Discord embeds and message components while working within the bot's existing architecture of YouTube-based playback, queue system, and radio mode.

**Key Features:**
- Album art thumbnails from YouTube or external APIs
- Song metadata (title, artist, album, duration)
- Progress bar with current playback position
- Interactive buttons (play/pause, skip, volume, etc.)
- Real-time or periodic embed updates
- Integration with existing playback notifications

**Estimated Complexity:** Medium
**Estimated Timeline:** 3-5 days (with testing)

---

## Discord API Research

### Embeds (MessageEmbed)

Discord embeds support rich formatted content within messages. Relevant fields for now-playing cards:

**Available Fields:**
```go
type MessageEmbed struct {
    Title       string           // Song title
    Description string           // Artist / album info
    URL         string           // Link to YouTube video
    Color       int              // Accent color (can use dynamic colors)
    Footer      *MessageEmbedFooter  // Progress/timestamp info
    Image       *MessageEmbedImage   // Could use for full-size art
    Thumbnail   *MessageEmbedThumbnail // Album art (perfect size)
    Fields      []*MessageEmbedField   // Duration, position, etc.
    Timestamp   string           // ISO8601 timestamp
}
```

**Thumbnail Specifications:**
- Max size: 10MB
- Formats: PNG, JPEG, WebP, GIF
- Recommended: 80x80 to 320x320 pixels
- URL-based (external URLs or Discord CDN)

**Color Field:**
- Integer representation of hex color
- Example: `0xFF5733` for orange-red
- Can extract dominant colors from album art

**Limitations:**
- Cannot nest embeds
- Max 25 fields per embed
- Field values: max 1024 characters
- Description: max 4096 characters

### Message Components (Buttons)

Discord supports interactive buttons via MessageComponent API:

**Button Structure:**
```go
type Button struct {
    Label    string       // Button text
    Style    ButtonStyle  // Primary, Secondary, Success, Danger, Link
    CustomID string       // Identifier for interaction handling
    Emoji    *ComponentEmoji  // Optional emoji
    URL      string       // For Link style buttons
    Disabled bool         // Disable state
}

type ActionsRow struct {
    Type       ComponentType  // ActionRow = 1
    Components []MessageComponent
}
```

**Button Styles:**
- `Primary` (Blue): Main actions (play/pause)
- `Secondary` (Gray): Secondary actions (skip, volume)
- `Success` (Green): Positive actions
- `Danger` (Red): Destructive actions (stop, remove)
- `Link` (Gray): External links (YouTube video)

**Limitations:**
- Max 5 buttons per action row
- Max 5 action rows per message
- Buttons trigger interaction events (need handler)
- 15-minute interaction token expiry
- Cannot update buttons after token expires without webhook

**Button Interaction Flow:**
1. User clicks button
2. Discord sends `INTERACTION_CREATE` event
3. Bot receives interaction with `CustomID`
4. Bot responds with deferred update or immediate response
5. Bot can update embed/buttons in response

### Updating Embeds

**Methods to update:**

1. **Webhook Update (Current approach):**
   - Uses interaction token
   - `PATCH /webhooks/{app_id}/{interaction_token}/messages/@original`
   - Works for 15 minutes after interaction
   - Already implemented in `discord/messages.go`

2. **Channel Message Edit:**
   - Uses bot token + message ID
   - `PATCH /channels/{channel_id}/messages/{message_id}`
   - No time limit
   - Need to store message ID for updates

3. **Interaction Response:**
   - Immediate response to button interactions
   - Type 7: `UpdateMessage` - updates the original message
   - Type 4: `ChannelMessageWithSource` - new message

**Recommendation:** Use Channel Message Edit for now-playing updates (no 15min limit)

### Progress Bar Implementation

Since Discord doesn't support native progress bars, use Unicode block characters:

**Progress Bar Characters:**
```
Empty: ░ (U+2591)
Filled: ▓ (U+2593)
Start: ▓ (or ▶)
End: ░ (or ◀)
```

**Example:**
```
▓▓▓▓▓▓▓░░░░░░░░ 2:34 / 4:12
```

**Implementation:**
```go
func RenderProgressBar(current, total time.Duration, width int) string {
    if total == 0 {
        return "░░░░░░░░░░░░░░░ 0:00 / 0:00"
    }
    
    percentage := float64(current) / float64(total)
    filled := int(percentage * float64(width))
    
    bar := strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
    currentStr := formatDuration(current)
    totalStr := formatDuration(total)
    
    return fmt.Sprintf("%s %s / %s", bar, currentStr, totalStr)
}
```

---

## Architecture Overview

### Current Architecture

**Playback State:**
- Lives in `GuildPlayer` (one per Discord guild)
- Tracks `CurrentSong`, `VoiceConnection`, `Queue`
- Playback events flow through `Player.Notifications` channel
- Event types: `PlaybackStarted`, `PlaybackPaused`, `PlaybackResumed`, `PlaybackCompleted`, `PlaybackStopped`

**Relevant Components:**
```
controller/controller.go
├── GuildPlayer struct (playback state)
│   ├── CurrentSong *string
│   ├── Player *audio.Player
│   ├── Queue *GuildQueue
│   └── listenForPlaybackEvents()
│
audio/player.go
├── Player struct (audio streaming)
│   ├── Notifications chan PlaybackNotification
│   ├── Play(), Pause(), Resume(), Stop()
│   └── Sends playback events
│
discord/messages.go
└── SendFollowup(), UpdateMessage()
    └── Current messaging via webhooks
```

**Message Flow:**
1. User interaction → `handlers/handlers.go`
2. Song queued → `GuildPlayer.Add()`
3. Playback starts → `PlaybackStarted` event
4. Currently sends followup text messages

### New Architecture for Now-Playing

**Components to Add:**

```
discord/
├── embeds.go (NEW)
│   ├── NowPlayingEmbed struct
│   ├── BuildNowPlayingEmbed()
│   ├── UpdateNowPlayingProgress()
│   └── RenderProgressBar()
│
├── components.go (NEW)
│   ├── BuildPlaybackButtons()
│   ├── HandleButtonInteraction()
│   └── Button handlers (play/pause/skip/etc.)
│
└── messages.go (MODIFY)
    └── Add EditChannelMessage() for updates

controller/
└── controller.go (MODIFY)
    └── GuildPlayer struct
        ├── NowPlayingMessageID *string (NEW)
        ├── NowPlayingChannelID *string (NEW)
        ├── sendNowPlayingCard() (NEW)
        ├── updateNowPlayingCard() (NEW)
        └── listenForPlaybackEvents() (MODIFY)

audio/
└── player.go (MODIFY)
    └── Add playback position tracking
        ├── PlaybackPosition atomic.Int64 (NEW)
        ├── GetPosition() time.Duration (NEW)
        └── Update position in Play() loop

handlers/
└── handlers.go (MODIFY)
    └── Add button interaction handler routes
```

**Data Flow:**

```
PlaybackStarted event
    ↓
sendNowPlayingCard()
    ↓
BuildNowPlayingEmbed() + BuildPlaybackButtons()
    ↓
Send to Discord channel
    ↓
Store message ID
    ↓
[Optional: Periodic updates]
    ↓
updateNowPlayingCard() every 5-10s
    ↓
EditChannelMessage() with new progress
```

**Button Interaction Flow:**

```
User clicks button
    ↓
Discord INTERACTION_CREATE
    ↓
HandleButtonInteraction()
    ↓
Parse CustomID (e.g., "play_pause:guildID")
    ↓
Execute action (pause/resume/skip)
    ↓
Update embed + buttons
    ↓
Respond with Type 7 (UpdateMessage)
```

---

## Data Sources for Metadata

### Album Art & Metadata

**Option 1: YouTube Thumbnails (Easiest, already available)**

**Pros:**
- Already have video ID from `youtube.VideoResponse`
- No additional API calls
- Reliable and fast
- Multiple resolutions available

**Thumbnail URLs:**
```go
// High quality (480x360)
fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)

// Max quality (1280x720, not always available)
fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", videoID)

// Medium quality (320x180)
fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", videoID)

// Standard (640x480)
fmt.Sprintf("https://i.ytimg.com/vi/%s/sddefault.jpg", videoID)
```

**Recommendation:** Use `hqdefault.jpg` (480x360) - good quality, always available

**Artist/Album Parsing:**
Use existing `ExtractArtist()` function in `controller.go` to parse title:
- Handles "Artist - Title" format
- Removes "(Official Video)", "[Lyrics]", etc.
- Falls back to full title

**Option 2: External APIs (More accurate, requires integration)**

**Spotify API:**
- Pros: Rich metadata, high-quality album art
- Cons: Requires matching track, rate limits, existing integration only for URL parsing
- Implementation: Search by title/artist, extract album art URL

**MusicBrainz + Cover Art Archive:**
- Pros: Free, open-source, extensive database
- Cons: Complex matching, slower than YouTube
- API: https://musicbrainz.org/doc/MusicBrainz_API

**Last.fm API:**
- Pros: Good metadata, album art
- Cons: Requires API key, rate limits
- API: https://www.last.fm/api

**Option 3: yt-dlp Metadata (Medium effort)**

Modify `GetVideoStream()` to extract more metadata:

```bash
yt-dlp --dump-json "https://youtube.com/watch?v=VIDEO_ID"
```

Returns JSON with:
- `thumbnail`: thumbnail URL
- `artist`: artist name (if available)
- `album`: album name (if available)
- `track`: track name
- `duration`: duration in seconds
- `uploader`: channel name

**Pros:**
- Already using yt-dlp
- More metadata than current API
- No additional API calls

**Cons:**
- Slower (JSON parsing)
- Not always accurate for music videos

**Recommendation Strategy:**

**Phase 1 (MVP):** YouTube thumbnails + title parsing
- Fastest to implement
- No new dependencies
- Works for 90% of tracks

**Phase 2 (Enhancement):** Add yt-dlp metadata extraction
- Improve artist/album detection
- Optional fallback if parsing fails

**Phase 3 (Polish):** External API fallback
- Only for tracks without good metadata
- Configurable (enable/disable)

---

## Code Structure

### File Organization

```
discord-audio-streamer/
├── discord/
│   ├── embeds.go (NEW)
│   │   └── Now-playing embed generation
│   ├── components.go (NEW)
│   │   └── Button generation & interaction handling
│   ├── messages.go (MODIFY)
│   │   └── Add EditChannelMessage()
│   └── session.go
│
├── controller/
│   └── controller.go (MODIFY)
│       └── Add now-playing card logic
│
├── audio/
│   ├── player.go (MODIFY)
│   │   └── Add position tracking
│   └── structs.go (MODIFY)
│       └── Add NowPlayingConfig
│
├── handlers/
│   └── handlers.go (MODIFY)
│       └── Add button interaction routes
│
└── config/
    └── config.go (MODIFY)
        └── Add now-playing config options
```

### New Files

#### `discord/embeds.go`

```go
package discord

import (
    "fmt"
    "strings"
    "time"
    "beatbot/audio"
    "github.com/bwmarrin/discordgo"
)

// NowPlayingMetadata contains all info for a now-playing card
type NowPlayingMetadata struct {
    VideoID        string
    Title          string
    Artist         string
    Album          string
    ThumbnailURL   string
    Duration       time.Duration
    CurrentPosition time.Duration
    IsPlaying      bool
    Volume         int
    GuildID        string
}

// BuildNowPlayingEmbed creates a rich embed for now-playing
func BuildNowPlayingEmbed(metadata *NowPlayingMetadata) *discordgo.MessageEmbed {
    // Extract artist from title if not provided
    artist := metadata.Artist
    if artist == "" {
        artist = ExtractArtistFromTitle(metadata.Title)
    }
    
    // Build thumbnail URL from video ID if not provided
    thumbnailURL := metadata.ThumbnailURL
    if thumbnailURL == "" {
        thumbnailURL = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", metadata.VideoID)
    }
    
    // Create progress bar
    progressBar := RenderProgressBar(metadata.CurrentPosition, metadata.Duration, 15)
    
    // Determine embed color based on playback state
    color := 0x1DB954 // Spotify green for playing
    if !metadata.IsPlaying {
        color = 0x808080 // Gray for paused
    }
    
    // Build description
    var desc strings.Builder
    if artist != metadata.Title {
        desc.WriteString(fmt.Sprintf("**Artist:** %s\n", artist))
    }
    if metadata.Album != "" {
        desc.WriteString(fmt.Sprintf("**Album:** %s\n", metadata.Album))
    }
    
    embed := &discordgo.MessageEmbed{
        Title: metadata.Title,
        URL:   fmt.Sprintf("https://www.youtube.com/watch?v=%s", metadata.VideoID),
        Description: desc.String(),
        Color: color,
        Thumbnail: &discordgo.MessageEmbedThumbnail{
            URL: thumbnailURL,
        },
        Footer: &discordgo.MessageEmbedFooter{
            Text: progressBar,
        },
        Fields: []*discordgo.MessageEmbedField{
            {
                Name:   "Duration",
                Value:  FormatDuration(metadata.Duration),
                Inline: true,
            },
            {
                Name:   "Volume",
                Value:  fmt.Sprintf("%d%%", metadata.Volume),
                Inline: true,
            },
        },
        Timestamp: time.Now().Format(time.RFC3339),
    }
    
    // Add status indicator
    if metadata.IsPlaying {
        embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
            Name:   "Status",
            Value:  "▶️ Playing",
            Inline: true,
        })
    } else {
        embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
            Name:   "Status",
            Value:  "⏸️ Paused",
            Inline: true,
        })
    }
    
    return embed
}

// UpdateNowPlayingProgress updates just the progress bar (efficient)
func UpdateNowPlayingProgress(embed *discordgo.MessageEmbed, currentPosition, duration time.Duration) *discordgo.MessageEmbed {
    if embed == nil || embed.Footer == nil {
        return embed
    }
    
    // Update progress bar in footer
    embed.Footer.Text = RenderProgressBar(currentPosition, duration, 15)
    
    return embed
}

// RenderProgressBar creates a Unicode progress bar
func RenderProgressBar(current, total time.Duration, width int) string {
    if total == 0 {
        return "░░░░░░░░░░░░░░░ 0:00 / 0:00"
    }
    
    percentage := float64(current) / float64(total)
    if percentage > 1.0 {
        percentage = 1.0
    }
    
    filled := int(percentage * float64(width))
    if filled > width {
        filled = width
    }
    
    bar := strings.Repeat("▓", filled) + strings.Repeat("░", width-filled)
    currentStr := FormatDuration(current)
    totalStr := FormatDuration(total)
    
    return fmt.Sprintf("%s %s / %s", bar, currentStr, totalStr)
}

// FormatDuration formats duration as MM:SS or HH:MM:SS
func FormatDuration(d time.Duration) string {
    d = d.Round(time.Second)
    h := d / time.Hour
    d -= h * time.Hour
    m := d / time.Minute
    d -= m * time.Minute
    s := d / time.Second
    
    if h > 0 {
        return fmt.Sprintf("%d:%02d:%02d", h, m, s)
    }
    return fmt.Sprintf("%d:%02d", m, s)
}

// ExtractArtistFromTitle parses artist from title
func ExtractArtistFromTitle(title string) string {
    // Remove common suffixes
    cleaned := title
    suffixes := []string{
        "(Official Video)", "(Official Music Video)", "(Official Audio)",
        "(Lyrics)", "(Lyric Video)", "(Audio)", "(Visualizer)",
        "[Official Video]", "[Official Music Video]", "[Official Audio]",
        "[Lyrics]", "[Lyric Video]", "[Audio]",
    }
    
    for _, suffix := range suffixes {
        cleaned = strings.Replace(cleaned, suffix, "", 1)
    }
    cleaned = strings.TrimSpace(cleaned)
    
    // Try to split on " - "
    parts := strings.SplitN(cleaned, " - ", 2)
    if len(parts) == 2 {
        artist := strings.TrimSpace(parts[0])
        // Remove featuring info
        feats := []string{" ft.", " feat.", " ft ", " feat ", " featuring "}
        for _, feat := range feats {
            if idx := strings.Index(strings.ToLower(artist), feat); idx != -1 {
                artist = strings.TrimSpace(artist[:idx])
            }
        }
        if artist != "" {
            return artist
        }
    }
    
    return cleaned
}
```

#### `discord/components.go`

```go
package discord

import (
    "fmt"
    "strings"
    "github.com/bwmarrin/discordgo"
)

// BuildPlaybackButtons creates button action rows for now-playing card
func BuildPlaybackButtons(guildID string, isPlaying bool) []discordgo.MessageComponent {
    // Create primary controls row
    primaryRow := discordgo.ActionsRow{
        Components: []discordgo.MessageComponent{
            discordgo.Button{
                Label:    "",
                Style:    discordgo.SecondaryButton,
                CustomID: fmt.Sprintf("np:prev:%s", guildID),
                Emoji: &discordgo.ComponentEmoji{
                    Name: "⏮️",
                },
                Disabled: true, // TODO: implement previous track
            },
            discordgo.Button{
                Label:    "",
                Style:    discordgo.PrimaryButton,
                CustomID: fmt.Sprintf("np:playpause:%s", guildID),
                Emoji: &discordgo.ComponentEmoji{
                    Name: getPlayPauseEmoji(isPlaying),
                },
            },
            discordgo.Button{
                Label:    "",
                Style:    discordgo.SecondaryButton,
                CustomID: fmt.Sprintf("np:skip:%s", guildID),
                Emoji: &discordgo.ComponentEmoji{
                    Name: "⏭️",
                },
            },
            discordgo.Button{
                Label:    "",
                Style:    discordgo.DangerButton,
                CustomID: fmt.Sprintf("np:stop:%s", guildID),
                Emoji: &discordgo.ComponentEmoji{
                    Name: "⏹️",
                },
            },
        },
    }
    
    // Create secondary controls row
    secondaryRow := discordgo.ActionsRow{
        Components: []discordgo.MessageComponent{
            discordgo.Button{
                Label:    "Vol -",
                Style:    discordgo.SecondaryButton,
                CustomID: fmt.Sprintf("np:voldown:%s", guildID),
                Emoji: &discordgo.ComponentEmoji{
                    Name: "🔉",
                },
            },
            discordgo.Button{
                Label:    "Vol +",
                Style:    discordgo.SecondaryButton,
                CustomID: fmt.Sprintf("np:volup:%s", guildID),
                Emoji: &discordgo.ComponentEmoji{
                    Name: "🔊",
                },
            },
            discordgo.Button{
                Label:    "Queue",
                Style:    discordgo.SecondaryButton,
                CustomID: fmt.Sprintf("np:queue:%s", guildID),
                Emoji: &discordgo.ComponentEmoji{
                    Name: "📜",
                },
            },
            discordgo.Button{
                Label:    "Shuffle",
                Style:    discordgo.SecondaryButton,
                CustomID: fmt.Sprintf("np:shuffle:%s", guildID),
                Emoji: &discordgo.ComponentEmoji{
                    Name: "🔀",
                },
            },
        },
    }
    
    return []discordgo.MessageComponent{primaryRow, secondaryRow}
}

func getPlayPauseEmoji(isPlaying bool) string {
    if isPlaying {
        return "⏸️"
    }
    return "▶️"
}

// ParseButtonCustomID extracts action and guildID from button custom ID
// Format: "np:action:guildID"
func ParseButtonCustomID(customID string) (action, guildID string, ok bool) {
    parts := strings.Split(customID, ":")
    if len(parts) != 3 || parts[0] != "np" {
        return "", "", false
    }
    return parts[1], parts[2], true
}
```

### Modified Files

#### `discord/messages.go` (Add EditChannelMessage)

```go
// Add to existing discord/messages.go

// EditChannelMessage updates a message in a channel using bot token
func EditChannelMessage(channelID, messageID string, content string, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
    payload := map[string]interface{}{}
    
    if content != "" {
        payload["content"] = content
    }
    
    if embed != nil {
        payload["embeds"] = []*discordgo.MessageEmbed{embed}
    }
    
    if components != nil {
        payload["components"] = components
    }
    
    jsonPayload, err := json.Marshal(payload)
    if err != nil {
        sentry.CaptureException(err)
        log.Errorf("Error marshalling payload: %v", err)
        return err
    }
    
    url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages/%s", channelID, messageID)
    
    req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(jsonPayload))
    if err != nil {
        sentry.CaptureException(err)
        return err
    }
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", fmt.Sprintf("Bot %s", config.Config.Discord.BotToken))
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        sentry.CaptureException(err)
        return err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        err := fmt.Errorf("failed to edit message: %s - %s", resp.Status, string(body))
        log.Error(err)
        return err
    }
    
    return nil
}

// SendChannelMessage sends a new message to a channel
func SendChannelMessage(channelID, content string, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (*discordgo.Message, error) {
    payload := map[string]interface{}{}
    
    if content != "" {
        payload["content"] = content
    }
    
    if embed != nil {
        payload["embeds"] = []*discordgo.MessageEmbed{embed}
    }
    
    if components != nil {
        payload["components"] = components
    }
    
    jsonPayload, err := json.Marshal(payload)
    if err != nil {
        sentry.CaptureException(err)
        return nil, err
    }
    
    url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)
    
    req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
    if err != nil {
        sentry.CaptureException(err)
        return nil, err
    }
    
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", fmt.Sprintf("Bot %s", config.Config.Discord.BotToken))
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        sentry.CaptureException(err)
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
        body, _ := io.ReadAll(resp.Body)
        err := fmt.Errorf("failed to send message: %s - %s", resp.Status, string(body))
        log.Error(err)
        return nil, err
    }
    
    var message discordgo.Message
    if err := json.NewDecoder(resp.Body).Decode(&message); err != nil {
        sentry.CaptureException(err)
        return nil, err
    }
    
    return &message, nil
}
```

#### `controller/controller.go` (Add now-playing logic)

```go
// Add to GuildPlayer struct
type GuildPlayer struct {
    // ... existing fields ...
    
    // Now-playing card tracking
    NowPlayingMessageID *string
    NowPlayingChannelID *string
    nowPlayingMutex     sync.Mutex
    nowPlayingUpdateStop chan struct{}
}

// Modify listenForPlaybackEvents() to send now-playing cards
func (p *GuildPlayer) listenForPlaybackEvents() {
    log.Tracef("listening for playback events")
    go func() {
        for event := range p.Player.Notifications {
            log.Tracef("Playback event: %s", event.Event)
            videoID := event.VideoID
            var queueItem *GuildQueueItem
            if videoID != nil {
                queueItem, _ = p.findQueueItemByVideoID(*videoID)
            }

            switch event.Event {
            case audio.PlaybackStarted:
                if queueItem != nil {
                    // ... existing code ...
                    
                    // Send now-playing card
                    go p.sendNowPlayingCard(queueItem)
                }
                // ... rest of existing code ...
                
            case audio.PlaybackCompleted:
            case audio.PlaybackStopped:
                // Stop now-playing updates
                p.stopNowPlayingUpdates()
                // ... existing code ...
                
            case audio.PlaybackPaused:
            case audio.PlaybackResumed:
                // Update now-playing card state
                go p.updateNowPlayingCardState()
                // ... existing code ...
            }
        }
    }()
}

// sendNowPlayingCard creates and sends a now-playing embed
func (p *GuildPlayer) sendNowPlayingCard(queueItem *GuildQueueItem) {
    if p.LastTextChannelID == "" {
        log.Debug("No text channel to send now-playing card")
        return
    }
    
    p.nowPlayingMutex.Lock()
    defer p.nowPlayingMutex.Unlock()
    
    // Get duration from video or load result
    var duration time.Duration
    if queueItem.LoadResult != nil {
        duration = queueItem.LoadResult.Duration
    }
    
    // Build metadata
    metadata := &discord.NowPlayingMetadata{
        VideoID:         queueItem.Video.VideoID,
        Title:           queueItem.Video.Title,
        Duration:        duration,
        CurrentPosition: 0,
        IsPlaying:       true,
        Volume:          p.Player.GetVolume(),
        GuildID:         p.GuildID,
    }
    
    // Build embed and buttons
    embed := discord.BuildNowPlayingEmbed(metadata)
    buttons := discord.BuildPlaybackButtons(p.GuildID, true)
    
    // Send message
    message, err := discord.SendChannelMessage(p.LastTextChannelID, "", embed, buttons)
    if err != nil {
        log.Errorf("Failed to send now-playing card: %v", err)
        sentry.CaptureException(err)
        return
    }
    
    // Store message ID for updates
    p.NowPlayingMessageID = &message.ID
    p.NowPlayingChannelID = &p.LastTextChannelID
    
    log.Debugf("Sent now-playing card: %s", message.ID)
    
    // Start periodic updates
    p.startNowPlayingUpdates(queueItem)
}

// startNowPlayingUpdates starts periodic progress updates
func (p *GuildPlayer) startNowPlayingUpdates(queueItem *GuildQueueItem) {
    // Stop any existing updater
    p.stopNowPlayingUpdates()
    
    p.nowPlayingUpdateStop = make(chan struct{})
    
    go func() {
        ticker := time.NewTicker(5 * time.Second) // Update every 5 seconds
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                if err := p.updateNowPlayingCard(queueItem); err != nil {
                    log.Warnf("Failed to update now-playing card: %v", err)
                }
            case <-p.nowPlayingUpdateStop:
                return
            }
        }
    }()
}

// stopNowPlayingUpdates stops periodic updates
func (p *GuildPlayer) stopNowPlayingUpdates() {
    if p.nowPlayingUpdateStop != nil {
        close(p.nowPlayingUpdateStop)
        p.nowPlayingUpdateStop = nil
    }
}

// updateNowPlayingCard updates progress bar
func (p *GuildPlayer) updateNowPlayingCard(queueItem *GuildQueueItem) error {
    p.nowPlayingMutex.Lock()
    defer p.nowPlayingMutex.Unlock()
    
    if p.NowPlayingMessageID == nil || p.NowPlayingChannelID == nil {
        return nil
    }
    
    // Get current position from player
    currentPosition := p.Player.GetPosition()
    
    var duration time.Duration
    if queueItem.LoadResult != nil {
        duration = queueItem.LoadResult.Duration
    }
    
    // Build updated metadata
    metadata := &discord.NowPlayingMetadata{
        VideoID:         queueItem.Video.VideoID,
        Title:           queueItem.Video.Title,
        Duration:        duration,
        CurrentPosition: currentPosition,
        IsPlaying:       p.Player.IsPlaying(),
        Volume:          p.Player.GetVolume(),
        GuildID:         p.GuildID,
    }
    
    // Build embed and buttons
    embed := discord.BuildNowPlayingEmbed(metadata)
    buttons := discord.BuildPlaybackButtons(p.GuildID, metadata.IsPlaying)
    
    // Update message
    return discord.EditChannelMessage(*p.NowPlayingChannelID, *p.NowPlayingMessageID, "", embed, buttons)
}

// updateNowPlayingCardState updates state without full rebuild (for pause/resume)
func (p *GuildPlayer) updateNowPlayingCardState() {
    p.nowPlayingMutex.Lock()
    defer p.nowPlayingMutex.Unlock()
    
    if p.NowPlayingMessageID == nil || p.NowPlayingChannelID == nil {
        return
    }
    
    // Quick state update without full metadata fetch
    // Just update buttons to reflect new state
    isPlaying := p.Player.IsPlaying()
    buttons := discord.BuildPlaybackButtons(p.GuildID, isPlaying)
    
    if err := discord.EditChannelMessage(*p.NowPlayingChannelID, *p.NowPlayingMessageID, "", nil, buttons); err != nil {
        log.Warnf("Failed to update now-playing state: %v", err)
    }
}
```

#### `audio/player.go` (Add position tracking)

```go
// Add to Player struct
type Player struct {
    // ... existing fields ...
    
    // Playback position tracking
    playbackStartTime time.Time
    playbackPosition  atomic.Int64 // microseconds
}

// Modify Play() to track position
func (p *Player) Play(ctx context.Context, data *LoadResult, voiceChannel *discordgo.VoiceConnection) error {
    // ... existing code ...
    
    // Initialize position tracking
    p.playbackStartTime = time.Now()
    p.playbackPosition.Store(0)
    
    // In the main playback loop, update position
    // After sending each opus frame:
    if !p.paused.Load() && !p.stopping.Load() {
        // Each opus frame is 20ms of audio
        currentPos := p.playbackPosition.Load() + 20000 // microseconds
        p.playbackPosition.Store(currentPos)
    }
    
    // ... rest of existing code ...
}

// GetPosition returns current playback position
func (p *Player) GetPosition() time.Duration {
    if p.playing == nil || !*p.playing {
        return 0
    }
    
    microseconds := p.playbackPosition.Load()
    return time.Duration(microseconds) * time.Microsecond
}

// Modify Pause() to freeze position
func (p *Player) Pause(ctx context.Context) {
    // ... existing code ...
    // Position tracking automatically pauses since we check paused flag
}
```

#### `handlers/handlers.go` (Add button interaction handler)

```go
// Add to Manager struct methods

// HandleInteraction routes different interaction types
func (m *Manager) HandleInteraction(w http.ResponseWriter, r *http.Request) {
    var interaction Interaction
    // ... existing parsing code ...
    
    switch interaction.Type {
    case 1: // PING
        // ... existing code ...
    case 2: // APPLICATION_COMMAND
        // ... existing code ...
    case 3: // MESSAGE_COMPONENT (buttons)
        m.handleComponentInteraction(w, &interaction)
    default:
        // ... existing code ...
    }
}

// handleComponentInteraction handles button clicks
func (m *Manager) handleComponentInteraction(w http.ResponseWriter, interaction *Interaction) {
    // Parse button custom ID
    action, guildID, ok := discord.ParseButtonCustomID(interaction.Data.Options[0].Value)
    if !ok {
        log.Warnf("Invalid button custom ID: %s", interaction.Data.Options[0].Value)
        respondWithError(w, "Invalid button")
        return
    }
    
    // Get guild player
    player := m.Controller.GetPlayer(guildID)
    if player == nil {
        respondWithError(w, "Player not found")
        return
    }
    
    // Defer update response (acknowledge button click)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "type": 6, // DEFERRED_UPDATE_MESSAGE
    })
    
    // Handle action
    ctx := context.Background()
    
    switch action {
    case "playpause":
        if player.Player.IsPlaying() {
            player.Player.Pause(ctx)
        } else {
            player.Player.Resume(ctx)
        }
        
    case "skip":
        player.Skip()
        
    case "stop":
        player.Player.Stop()
        player.Clear()
        
    case "volup":
        currentVol := player.Player.GetVolume()
        player.Player.SetVolume(currentVol + 10)
        
    case "voldown":
        currentVol := player.Player.GetVolume()
        player.Player.SetVolume(currentVol - 10)
        
    case "shuffle":
        player.Shuffle()
        
    case "queue":
        // Send queue as ephemeral message
        m.sendQueueMessage(interaction, player)
        
    default:
        log.Warnf("Unknown button action: %s", action)
    }
    
    // Update will happen via normal event flow
}

func respondWithError(w http.ResponseWriter, message string) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "type": 4, // CHANNEL_MESSAGE_WITH_SOURCE
        "data": map[string]interface{}{
            "content": message,
            "flags":   64, // EPHEMERAL
        },
    })
}
```

---

## Embed Mockup

### Visual Representation

```
╔══════════════════════════════════════════════════════╗
║  🎵 Never Gonna Give You Up                          ║
║  https://www.youtube.com/watch?v=dQw4w9WgXcQ        ║
║  ────────────────────────────────────────────────    ║
║  Artist: Rick Astley                                 ║
║  Album: Whenever You Need Somebody                   ║
║                                                      ║
║  Duration: 3:32    Volume: 100%    Status: ▶️ Playing║
║                                                      ║
║  [THUMBNAIL]                                         ║
║  480x360 image                                       ║
║                                                      ║
║  ▓▓▓▓▓▓▓▓░░░░░░░ 1:45 / 3:32                         ║
║  2025-02-04T06:00:00Z                                ║
╠══════════════════════════════════════════════════════╣
║  [⏮️]  [⏸️]  [⏭️]  [⏹️]                              ║
║                                                      ║
║  [🔉 Vol -]  [🔊 Vol +]  [📜 Queue]  [🔀 Shuffle]   ║
╚══════════════════════════════════════════════════════╝
```

### JSON Structure (Discord API)

```json
{
  "content": "",
  "embeds": [
    {
      "title": "Never Gonna Give You Up",
      "url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
      "description": "**Artist:** Rick Astley\n**Album:** Whenever You Need Somebody",
      "color": 1947972,
      "thumbnail": {
        "url": "https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg"
      },
      "fields": [
        {
          "name": "Duration",
          "value": "3:32",
          "inline": true
        },
        {
          "name": "Volume",
          "value": "100%",
          "inline": true
        },
        {
          "name": "Status",
          "value": "▶️ Playing",
          "inline": true
        }
      ],
      "footer": {
        "text": "▓▓▓▓▓▓▓▓░░░░░░░ 1:45 / 3:32"
      },
      "timestamp": "2025-02-04T06:00:00Z"
    }
  ],
  "components": [
    {
      "type": 1,
      "components": [
        {
          "type": 2,
          "style": 2,
          "custom_id": "np:prev:123456789",
          "emoji": {"name": "⏮️"},
          "disabled": true
        },
        {
          "type": 2,
          "style": 1,
          "custom_id": "np:playpause:123456789",
          "emoji": {"name": "⏸️"}
        },
        {
          "type": 2,
          "style": 2,
          "custom_id": "np:skip:123456789",
          "emoji": {"name": "⏭️"}
        },
        {
          "type": 2,
          "style": 4,
          "custom_id": "np:stop:123456789",
          "emoji": {"name": "⏹️"}
        }
      ]
    },
    {
      "type": 1,
      "components": [
        {
          "type": 2,
          "style": 2,
          "label": "Vol -",
          "custom_id": "np:voldown:123456789",
          "emoji": {"name": "🔉"}
        },
        {
          "type": 2,
          "style": 2,
          "label": "Vol +",
          "custom_id": "np:volup:123456789",
          "emoji": {"name": "🔊"}
        },
        {
          "type": 2,
          "style": 2,
          "label": "Queue",
          "custom_id": "np:queue:123456789",
          "emoji": {"name": "📜"}
        },
        {
          "type": 2,
          "style": 2,
          "label": "Shuffle",
          "custom_id": "np:shuffle:123456789",
          "emoji": {"name": "🔀"}
        }
      ]
    }
  ]
}
```

---

## Implementation Steps

### Phase 1: Foundation (Day 1)

**Goal:** Set up basic infrastructure for embeds and position tracking

1. **Add position tracking to Player**
   - [ ] Add `playbackPosition` atomic field
   - [ ] Add `GetPosition()` method
   - [ ] Update position in `Play()` loop
   - [ ] Test position accuracy with test tracks
   
2. **Create `discord/embeds.go`**
   - [ ] Implement `NowPlayingMetadata` struct
   - [ ] Implement `BuildNowPlayingEmbed()`
   - [ ] Implement `RenderProgressBar()`
   - [ ] Implement `FormatDuration()`
   - [ ] Implement `ExtractArtistFromTitle()`
   - [ ] Unit test progress bar rendering
   
3. **Create `discord/components.go`**
   - [ ] Implement `BuildPlaybackButtons()`
   - [ ] Implement `ParseButtonCustomID()`
   - [ ] Test button structure

**Validation:**
- Progress bar renders correctly for different positions
- Position tracking is accurate (±1 second)
- Embed builds without errors

### Phase 2: Integration (Day 2)

**Goal:** Integrate now-playing cards into playback flow

1. **Modify `discord/messages.go`**
   - [ ] Add `SendChannelMessage()`
   - [ ] Add `EditChannelMessage()`
   - [ ] Test message sending/editing
   
2. **Modify `controller/controller.go`**
   - [ ] Add now-playing fields to `GuildPlayer`
   - [ ] Implement `sendNowPlayingCard()`
   - [ ] Implement `updateNowPlayingCard()`
   - [ ] Implement `startNowPlayingUpdates()`
   - [ ] Implement `stopNowPlayingUpdates()`
   - [ ] Hook into `listenForPlaybackEvents()`
   
3. **Test basic flow**
   - [ ] Play a song, verify embed appears
   - [ ] Check progress bar updates every 5 seconds
   - [ ] Verify embed disappears on stop

**Validation:**
- Now-playing card appears when song starts
- Progress updates correctly
- Card cleans up on playback stop

### Phase 3: Interactive Buttons (Day 3)

**Goal:** Add button interactions

1. **Modify `handlers/handlers.go`**
   - [ ] Add `handleComponentInteraction()`
   - [ ] Route `MESSAGE_COMPONENT` type
   - [ ] Implement play/pause handler
   - [ ] Implement skip handler
   - [ ] Implement stop handler
   - [ ] Implement volume handlers
   - [ ] Implement shuffle handler
   - [ ] Implement queue display handler
   
2. **Test button interactions**
   - [ ] Click play/pause, verify state changes
   - [ ] Click skip, verify next song plays
   - [ ] Click stop, verify playback stops
   - [ ] Click volume buttons, verify volume changes
   - [ ] Click shuffle, verify queue shuffles
   - [ ] Click queue, verify queue message appears

**Validation:**
- All buttons respond correctly
- Embed updates reflect button actions
- No race conditions or crashes

### Phase 4: Polish & Edge Cases (Day 4)

**Goal:** Handle edge cases and improve UX

1. **Edge case handling**
   - [ ] Handle playback errors gracefully
   - [ ] Handle message edit failures (404, permissions)
   - [ ] Handle button clicks when player is stopped
   - [ ] Handle concurrent button clicks
   - [ ] Handle very long song titles (truncate)
   - [ ] Handle missing thumbnails (fallback)
   
2. **Configuration**
   - [ ] Add `now_playing_enabled` config option
   - [ ] Add `now_playing_update_interval` config
   - [ ] Add `now_playing_show_buttons` config
   
3. **Performance optimization**
   - [ ] Reduce update frequency when paused
   - [ ] Debounce rapid button clicks
   - [ ] Cache embed builds
   
4. **Visual improvements**
   - [ ] Extract dominant color from thumbnail
   - [ ] Add animated emoji for playing state
   - [ ] Improve progress bar aesthetics

**Validation:**
- No crashes with malformed data
- Graceful degradation when features fail
- Performance impact is minimal

### Phase 5: Testing & Documentation (Day 5)

**Goal:** Comprehensive testing and documentation

1. **Testing**
   - [ ] Unit tests for embed building
   - [ ] Unit tests for progress bar rendering
   - [ ] Unit tests for button parsing
   - [ ] Integration test: full playback flow
   - [ ] Integration test: button interactions
   - [ ] Load test: rapid button clicks
   - [ ] Edge case tests (see Phase 4)
   
2. **Documentation**
   - [ ] Update README with now-playing features
   - [ ] Add configuration examples
   - [ ] Document button custom ID format
   - [ ] Add troubleshooting section
   - [ ] Screenshot examples
   
3. **Deployment**
   - [ ] Build and test Docker image
   - [ ] Deploy to test environment
   - [ ] Monitor for errors
   - [ ] Gather user feedback

**Validation:**
- All tests pass
- Documentation is clear
- Feature works in production

---

## Challenges and Solutions

### Challenge 1: Discord API Rate Limits

**Problem:** Updating embed every second could hit rate limits (50 requests per second per endpoint)

**Solutions:**
1. **Update interval:** 5-10 seconds instead of 1 second
2. **Batch updates:** Only update when meaningful change (10% progress)
3. **Smart updates:** Stop updating when paused
4. **Exponential backoff:** If rate limited, increase interval

**Implementation:**
```go
// In startNowPlayingUpdates()
updateInterval := 5 * time.Second
if p.Player.IsPaused() {
    updateInterval = 30 * time.Second // Less frequent when paused
}

// Check if update is meaningful
lastUpdate := 0.0
currentPercent := float64(currentPos) / float64(duration) * 100
if math.Abs(currentPercent - lastUpdate) < 5.0 {
    // Skip update if less than 5% change
    continue
}
```

### Challenge 2: Button Interaction Token Expiry

**Problem:** Discord interaction tokens expire after 15 minutes, but songs can be longer

**Solutions:**
1. **Use Channel Message Edit:** Edit by message ID + bot token (no expiry)
2. **Regenerate buttons:** Create new message if token expires
3. **Stateless buttons:** CustomID contains guildID, no session state needed

**Implementation:**
Already handled by using `EditChannelMessage()` with bot token instead of webhook token.

### Challenge 3: Position Tracking Accuracy

**Problem:** Audio encoding/buffering can cause position drift

**Solutions:**
1. **Frame-based tracking:** Each opus frame is exactly 20ms
2. **Calibration:** Periodically sync with actual playback time
3. **Pause handling:** Freeze position counter when paused
4. **Resume adjustment:** Reset counter on resume

**Implementation:**
```go
// In Play() loop
frameCount := 0
frameDuration := 20 * time.Millisecond

for {
    // ... existing loop code ...
    
    if !p.paused.Load() && !p.stopping.Load() {
        frameCount++
        p.playbackPosition.Store(int64(frameCount * frameDuration.Microseconds()))
    }
}
```

### Challenge 4: Album Art Quality

**Problem:** YouTube thumbnails may not always be high quality or music-related

**Solutions:**
1. **Try maxresdefault first:** Best quality (1280x720)
2. **Fallback to hqdefault:** Always available (480x360)
3. **Future: External APIs:** Spotify/MusicBrainz for better art
4. **Caching:** Cache thumbnail URLs to avoid repeated checks

**Implementation:**
```go
func GetBestThumbnail(videoID string) string {
    // Try max quality first
    maxRes := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", videoID)
    if checkURL(maxRes) {
        return maxRes
    }
    
    // Fallback to high quality
    return fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)
}
```

### Challenge 5: Race Conditions

**Problem:** Multiple goroutines updating now-playing state concurrently

**Solutions:**
1. **Mutex protection:** `nowPlayingMutex` for all card updates
2. **Channel coordination:** Use single goroutine for updates
3. **Atomic flags:** Use atomic operations for playback state
4. **Cancellation:** Proper cleanup on playback stop

**Implementation:**
Already included in code structure:
- `nowPlayingMutex` in `GuildPlayer`
- `nowPlayingUpdateStop` channel for clean shutdown
- Atomic operations in `Player`

### Challenge 6: Message Spam

**Problem:** Creating new now-playing cards for each song could spam channel

**Solutions:**
1. **Reuse message:** Update same message instead of creating new
2. **Auto-delete old:** Delete previous card when new song starts
3. **Single active card:** Only one now-playing card per guild
4. **Configuration:** Allow disabling feature per guild

**Implementation:**
```go
func (p *GuildPlayer) sendNowPlayingCard(queueItem *GuildQueueItem) {
    // Delete old card if exists
    if p.NowPlayingMessageID != nil {
        discord.DeleteMessage(*p.NowPlayingChannelID, *p.NowPlayingMessageID)
    }
    
    // Send new card
    // ...
}
```

### Challenge 7: Button Action Feedback

**Problem:** Users need immediate feedback when clicking buttons

**Solutions:**
1. **Deferred response:** Immediate "button clicked" acknowledgment
2. **Visual feedback:** Update button appearance immediately
3. **State sync:** Ensure embed reflects action quickly
4. **Error messages:** Show ephemeral error if action fails

**Implementation:**
```go
func (m *Manager) handleComponentInteraction(w http.ResponseWriter, interaction *Interaction) {
    // Immediate acknowledgment (required within 3 seconds)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "type": 6, // DEFERRED_UPDATE_MESSAGE
    })
    
    // Action execution happens async
    // Event system will trigger embed update
}
```

### Challenge 8: Duration Unavailable

**Problem:** Song duration might not be known until fully loaded

**Solutions:**
1. **Show "Live":** Display "LIVE" or "∞" when duration unknown
2. **Update later:** Update duration once available
3. **Estimate:** Use YouTube API duration if available
4. **Progress without bar:** Show elapsed time only

**Implementation:**
```go
func RenderProgressBar(current, total time.Duration, width int) string {
    if total == 0 {
        // Duration unknown - show elapsed time only
        return fmt.Sprintf("⏱️ %s / --:--", FormatDuration(current))
    }
    
    // Normal progress bar
    // ...
}
```

---

## Testing Strategy

### Unit Tests

**File: `discord/embeds_test.go`**

```go
func TestRenderProgressBar(t *testing.T) {
    tests := []struct {
        current time.Duration
        total   time.Duration
        width   int
        want    string
    }{
        {0, 0, 15, "░░░░░░░░░░░░░░░ 0:00 / 0:00"},
        {30 * time.Second, 60 * time.Second, 10, "▓▓▓▓▓░░░░░ 0:30 / 1:00"},
        {60 * time.Second, 60 * time.Second, 10, "▓▓▓▓▓▓▓▓▓▓ 1:00 / 1:00"},
    }
    
    for _, tt := range tests {
        got := RenderProgressBar(tt.current, tt.total, tt.width)
        if got != tt.want {
            t.Errorf("RenderProgressBar(%v, %v, %d) = %q, want %q",
                tt.current, tt.total, tt.width, got, tt.want)
        }
    }
}

func TestExtractArtistFromTitle(t *testing.T) {
    tests := []struct {
        title string
        want  string
    }{
        {"Rick Astley - Never Gonna Give You Up", "Rick Astley"},
        {"Queen - Bohemian Rhapsody (Official Video)", "Queen"},
        {"Single Word Title", "Single Word Title"},
    }
    
    for _, tt := range tests {
        got := ExtractArtistFromTitle(tt.title)
        if got != tt.want {
            t.Errorf("ExtractArtistFromTitle(%q) = %q, want %q",
                tt.title, got, tt.want)
        }
    }
}
```

### Integration Tests

**File: `integration_test.go`**

```go
func TestNowPlayingFlow(t *testing.T) {
    // Setup test guild player
    controller, _ := controller.NewController(nil)
    player := controller.GetPlayer("test_guild_123")
    
    // Simulate song start
    video := youtube.VideoResponse{
        VideoID: "dQw4w9WgXcQ",
        Title:   "Rick Astley - Never Gonna Give You Up",
    }
    
    queueItem := &controller.GuildQueueItem{
        Video: video,
        LoadResult: &audio.LoadResult{
            Duration: 3*time.Minute + 32*time.Second,
        },
    }
    
    // Send now-playing card
    player.sendNowPlayingCard(queueItem)
    
    // Verify message was sent
    if player.NowPlayingMessageID == nil {
        t.Error("Expected now-playing message ID to be set")
    }
    
    // Verify updates are running
    time.Sleep(6 * time.Second)
    
    // Verify position tracking
    position := player.Player.GetPosition()
    if position < 5*time.Second {
        t.Errorf("Expected position >= 5s, got %v", position)
    }
    
    // Stop playback
    player.Player.Stop()
    player.stopNowPlayingUpdates()
}
```

### Manual Testing Checklist

- [ ] Play a song and verify embed appears with correct info
- [ ] Verify thumbnail loads correctly
- [ ] Verify progress bar updates every 5-10 seconds
- [ ] Click play/pause button, verify state changes
- [ ] Click skip button, verify next song plays
- [ ] Click stop button, verify playback stops
- [ ] Click volume up/down, verify volume changes
- [ ] Click shuffle, verify queue order changes
- [ ] Click queue button, verify queue displays
- [ ] Pause song, verify progress bar stops updating
- [ ] Resume song, verify progress continues
- [ ] Skip to next song, verify new embed appears
- [ ] Test with very long song title (truncation)
- [ ] Test with song missing thumbnail
- [ ] Test button spam (rapid clicks)
- [ ] Test with empty queue
- [ ] Test with radio mode enabled
- [ ] Test concurrent users in same guild

---

## Configuration Options

Add to `config/config.go`:

```go
type Config struct {
    // ... existing fields ...
    
    NowPlaying struct {
        Enabled        bool   `env:"NOW_PLAYING_ENABLED" envDefault:"true"`
        UpdateInterval int    `env:"NOW_PLAYING_UPDATE_INTERVAL" envDefault:"5"` // seconds
        ShowButtons    bool   `env:"NOW_PLAYING_SHOW_BUTTONS" envDefault:"true"`
        DeleteOld      bool   `env:"NOW_PLAYING_DELETE_OLD" envDefault:"true"`
    }
}
```

**Environment Variables:**
- `NOW_PLAYING_ENABLED`: Enable/disable now-playing cards (default: true)
- `NOW_PLAYING_UPDATE_INTERVAL`: Update interval in seconds (default: 5)
- `NOW_PLAYING_SHOW_BUTTONS`: Show interactive buttons (default: true)
- `NOW_PLAYING_DELETE_OLD`: Delete old card when new song starts (default: true)

---

## Future Enhancements

### Phase 2 Features (Post-MVP)

1. **Advanced Metadata**
   - Integrate Spotify API for better metadata
   - Integrate MusicBrainz for album info
   - Cache metadata for repeated tracks
   
2. **Visual Improvements**
   - Extract dominant color from album art
   - Animated progress bar
   - Custom embed themes per guild
   
3. **Interactive Features**
   - Volume slider (select menu)
   - Seek buttons (+30s, -30s)
   - Repeat/Loop toggle
   - Queue management buttons (remove specific)
   
4. **Analytics**
   - Track button usage
   - Popular songs/artists
   - User engagement metrics
   
5. **Personalization**
   - Per-guild embed style
   - Custom progress bar characters
   - Configurable button layout

---

## Conclusion

This implementation plan provides a comprehensive roadmap for adding rich now-playing cards to the Discord audio streamer bot. The approach is:

- **Pragmatic:** Leverages existing architecture and Discord API capabilities
- **Incremental:** Phased implementation allows testing at each stage
- **Robust:** Handles edge cases and rate limits
- **Extensible:** Designed for future enhancements

**Estimated Effort:** 3-5 days for full implementation and testing

**Next Steps:**
1. Review this plan with the team
2. Approve architecture and approach
3. Create feature branch: `feat/now-playing-cards`
4. Begin Phase 1 implementation

---

**Plan Status:** ✅ Ready for Review
**Author:** Sub-agent (Planning Phase)
**Review Required:** Yes
**Implementation Status:** Not Started
