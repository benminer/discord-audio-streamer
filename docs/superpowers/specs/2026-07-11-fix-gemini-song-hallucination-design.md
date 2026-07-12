# Fix Gemini Song Hallucination via Authoritative YouTube Metadata

## Problem

When a user queues a song with a title that matches a more famous track, Gemini announces the wrong song. Example: queuing "Change" by Alex G produced the Discord message "Alright. Deftones - Change." while the audio that played was actually "Change - Alex G".

The root cause is that Gemini relies on its training-data associations rather than the exact YouTube metadata we already fetched. The YouTube API returns `snippet.ChannelTitle` on every search and lookup, but we discard it. Without that disambiguating signal, a title like "Change" is ambiguous, and Gemini picks the most culturally prominent match.

## Goal

Make Gemini announce the song that is actually queued by feeding it authoritative metadata (title + channel) and instructing it not to substitute its own knowledge.

## Approach

Tighten the Gemini prompts and enrich the metadata pipeline with the YouTube channel name. This is a two-layer fix:

1. **Authoritative metadata:** thread `ChannelName` from the YouTube API through `VideoResponse`, `SongInfo`, and `SongHistoryEntry`.
2. **Strict prompt instructions:** tell Gemini explicitly to use only the provided title and channel, and to never guess or substitute.

## Data Model Changes

- `youtube.VideoResponse` gains `ChannelName string`.
- `controller.SongInfo` gains `ChannelName string`.
- `controller.SongHistoryEntry.ChannelName` already exists but is unpopulated; wire it through.

## Metadata Population

Populate `ChannelName` from `snippet.ChannelTitle` at every `VideoResponse` construction site:

- `youtube.GetVideoByID`
- `youtube.GetPlaylistVideos` and `PlaylistVideoInfo`
- `youtube.fetchMixViaAPI`
- `youtube.fetchMixViaYtdlp` (the second `videos.list` call that resolves IDs)
- `youtube.Query`
- `handlers/youtube.go` conversion from `PlaylistVideoInfo` to `VideoResponse`

Spotify URL handling already consumes `youtube.Query`, so it inherits the fix without additional changes.

## Controller Wiring

Pass `ChannelName` through all `PlaybackState` mutations:

- `PlaybackStarted`: populate `SetCurrent` and `SongHistory.Add`.
- `syncNextFromQueue`: populate `SetNext`.
- `Remove`: snapshot `nextChannelName` before unlocking and pass to `SetNext`.
- `Shuffle`: snapshot `nextChannelName` and pass to `SetNext`.
- `attemptVoiceRecovery`: fresh item inherits `savedItem.Video.ChannelName`.
- `startRadioMode`: pass `picked.ChannelName` into `DJScriptContext`.

## Gemini Prompt Changes

Add fields to `DJScriptContext`:

- `CurrentChannelName string`
- `NextChannelName string`

Update `GenerateDJScript` prompts to include channel names and a strict instruction block. Example for transitions:

```
Current song (just finished): %s (channel: %s)
Next up: %s (channel: %s)

IMPORTANT: You MUST announce the songs using ONLY the exact titles and channels provided above. Do not guess, substitute, or use your own knowledge about what artist or song this might be.
```

Apply the same pattern to `AnnouncementIntro` and `AnnouncementRadioStart`.

Update `GenerateNowPlayingCommentary` to accept a `channelName string` parameter and include the same strict instruction in its prompt.

## Test Impact

Tests that construct `VideoResponse` or `SongInfo` literals need the new field added; zero values are acceptable, but the literal must compile.

## Files Expected to Change

- `youtube/client.go`
- `handlers/youtube.go`
- `controller/controller.go`
- `controller/playback_state.go`
- `gemini/gemini.go`
- Test files in `controller/` that construct `VideoResponse` or `SongInfo`

## Out of Scope

- Fetching or using the full video `description`. Channel title is enough disambiguation signal without the noise of a long description.
- Changing the now-playing embed or any non-Gemini user-facing text. The fix targets AI-generated announcements only.
