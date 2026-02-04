# Gemini-Powered Radio Feature Implementation

## Overview
Enhanced the Discord audio streamer's radio mode with Gemini AI to provide intelligent song recommendations based on listening history.

## Implementation Details

### Changes Made

#### 1. New Gemini Function (`gemini/gemini.go`)
```go
GenerateSongRecommendation(ctx context.Context, recentSongs []string) string
```
- Accepts the last 3 song titles from play history
- Sends to Gemini with detailed instructions to analyze musical patterns
- Returns a targeted search query (e.g., "Artist Name - Song Title")
- Includes comprehensive Sentry tracing for monitoring
- Returns empty string on failure (triggers fallback)

**Gemini Prompt Strategy:**
- Analyzes genre, mood, era, and style
- Identifies common patterns/themes
- Suggests ONE similar song not in the history
- Returns only the search query (no explanations)

#### 2. Enhanced Radio Queueing (`controller/controller.go`)
Modified `queueRadioSong()` method:
- **Step 1**: Get last 3 songs from history (reduced from 5 for focused analysis)
- **Step 2**: Call Gemini to generate recommendation query
- **Step 3**: Search YouTube with AI-generated query
- **Step 4**: Queue first non-duplicate result
- **Fallback**: Use original artist-based search if Gemini fails/disabled

**Fallback Strategy:**
1. If Gemini disabled → immediate fallback
2. If Gemini returns empty → fallback
3. If no YouTube results → fallback
4. If all results are duplicates → broader fallback search

### Architecture Benefits
- **Non-intrusive**: Existing radio logic preserved as fallback
- **Configurable**: Respects `GEMINI_ENABLED` flag
- **Monitored**: Full Sentry tracking for debugging
- **Resilient**: Multiple fallback layers prevent failures

## Testing Checklist

### Pre-Deployment Testing (Required)

1. **Gemini Integration Test**
   ```bash
   # Enable radio mode in Discord
   /radio
   
   # Play 3 different songs
   /play <song1>
   /play <song2>
   /play <song3>
   
   # Wait for queue to empty
   # Verify: Radio auto-queues a related song
   ```

2. **Check Logs for Gemini Activity**
   ```bash
   docker logs discord-music-bot | grep -i "gemini"
   docker logs discord-music-bot | grep -i "radio"
   ```
   Look for:
   - "Requesting Gemini song recommendation"
   - "Gemini recommended search query: ..."
   - No error messages

3. **Test Fallback Mechanism**
   ```bash
   # Temporarily disable Gemini
   # Edit .env: GEMINI_ENABLED=false
   # Restart container
   
   # Verify radio still works with legacy search
   ```

4. **Quality Check**
   - Do recommendations make musical sense?
   - Are they stylistically similar to recent songs?
   - Are duplicates properly filtered?

### Deployment Steps

#### From eva-apps (PR Testing)
```bash
cd ~/eva-apps/discord-audio-streamer
git checkout feat/gemini-radio-recommendations
docker build -t discord-music-bot .
docker stop discord-music-bot
docker rm discord-music-bot
docker run -d --name discord-music-bot \
  --env-file .env \
  --restart unless-stopped \
  discord-music-bot
```

#### Production Deploy (After PR Approval)
```bash
cd ~/discord-apps/discord-audio-streamer
git pull origin main
./docker-restart.sh rebuild
```

### Post-Deployment Monitoring

1. **Check Container Health**
   ```bash
   docker ps | grep discord-music-bot
   docker logs discord-music-bot --tail 50
   ```

2. **Test in Discord**
   - Enable radio mode: `/radio`
   - Play 3 songs and let queue empty
   - Observe auto-queued recommendation
   - Verify announcement message appears

3. **Monitor Sentry**
   - Check for new errors related to Gemini
   - Review breadcrumbs for radio events
   - Verify span traces are working

4. **Performance Baseline**
   - Time from queue empty → next song queued
   - Gemini API response time
   - Overall system responsiveness

## Configuration

### Environment Variables
```bash
GEMINI_ENABLED=true          # Enable/disable Gemini features
GEMINI_API_KEY=AIzaSy...    # Already configured in production
```

### Feature Flags
- Radio mode is opt-in per guild (`/radio` command)
- Gemini recommendations only trigger when:
  - Radio mode is enabled
  - Queue is empty
  - At least 3 songs in history
  - `GEMINI_ENABLED=true`

## Rollback Plan

If issues arise:

1. **Quick Rollback**
   ```bash
   cd ~/discord-apps/discord-audio-streamer
   git log -n 5  # Find previous commit hash
   git checkout <previous-commit>
   ./docker-restart.sh rebuild
   ```

2. **Disable Gemini Only**
   ```bash
   # Edit .env
   GEMINI_ENABLED=false
   docker restart discord-music-bot
   ```
   This preserves the new code but uses legacy fallback.

## Known Limitations

1. **Minimum History**: Requires 3+ songs for Gemini recommendations
2. **API Dependency**: Relies on Gemini API availability
3. **YouTube Search**: Limited to YouTube's search results quality
4. **Duplicate Detection**: Only checks VideoID, not similar versions

## Future Enhancements

- [ ] Add genre/mood preferences per guild
- [ ] Learn from skip patterns (skip = bad recommendation)
- [ ] Support Spotify search in addition to YouTube
- [ ] Cache Gemini recommendations to reduce API calls
- [ ] Add explicit user feedback ("this recommendation sucks")

## PR Link
https://github.com/benminer/discord-audio-streamer/pull/45

## Implementation Date
2026-02-04
