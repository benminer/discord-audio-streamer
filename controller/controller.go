package controller

import (
	"beatbot/audio"
	"beatbot/config"
	"beatbot/database"
	"beatbot/discord"
	"beatbot/gemini"
	"beatbot/sentryhelper"
	"beatbot/spotify"
	"beatbot/youtube"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"github.com/bwmarrin/discordgo"
	spotifyclient "github.com/zmb3/spotify/v2"
)

type QueueEventType string

const (
	EventAdd   QueueEventType = "add"
	EventSkip  QueueEventType = "skip"
	EventClear QueueEventType = "clear"
)

type QueueEvent struct {
	Type QueueEventType
	Item *GuildQueueItem
}

// SongHistoryEntry stores metadata about a played song for radio mode
type SongHistoryEntry struct {
	VideoID     string
	Title       string
	ChannelName string
	SpotifyID   string // optional, populated if queued via Spotify
}

// SongHistory is a ring buffer of recently played songs
type SongHistory struct {
	entries []SongHistoryEntry
	size    int
	mu      sync.Mutex
}

func NewSongHistory(size int) *SongHistory {
	return &SongHistory{
		entries: make([]SongHistoryEntry, 0, size),
		size:    size,
	}
}

func (h *SongHistory) Add(entry SongHistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.entries) >= h.size {
		h.entries = h.entries[1:]
	}
	h.entries = append(h.entries, entry)
}

func (h *SongHistory) GetRecent(n int) []SongHistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n > len(h.entries) {
		n = len(h.entries)
	}
	// Return last n entries (most recent)
	start := len(h.entries) - n
	result := make([]SongHistoryEntry, n)
	copy(result, h.entries[start:])
	return result
}

func (h *SongHistory) GetAllVideoIDs() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make(map[string]bool, len(h.entries))
	for _, e := range h.entries {
		ids[e.VideoID] = true
	}
	return ids
}

func (h *SongHistory) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

type GuildPlayer struct {
	Discord           *discordgo.Session
	DB                *database.Database
	GuildID           string
	CurrentSong       *string
	Queue             *GuildQueue
	VoiceChannelMutex sync.Mutex
	VoiceChannelID    *string
	VoiceJoinedAt     *time.Time
	VoiceConnection   *discordgo.VoiceConnection
	Loader            *audio.Loader
	Player            *audio.Player
	LastActivityAt    time.Time
	LastTextChannelID string
	idleCheckStop     chan struct{}
	RadioEnabled      bool
	radioMutex        sync.Mutex
	SongHistory       *SongHistory
}

type GuildQueueItemInteraction struct {
	UserID           string
	InteractionToken string
	AppID            string
}

type GuildQueueItem struct {
	Video        youtube.VideoResponse
	Stream       *youtube.YoutubeStream
	LoadResult   *audio.LoadResult
	AddedAt      time.Time
	Interaction  *GuildQueueItemInteraction
	LoadAttempts int
	MaxAttempts  int
	Context      context.Context // Sentry context for tracing
}

type GuildQueue struct {
	Items         []*GuildQueueItem
	Listening     bool
	Mutex         sync.Mutex
	notifications chan QueueEvent
}

type Controller struct {
	// This is a map of guildID to the player for that guild
	sessions map[string]*GuildPlayer
	discord  *discordgo.Session
	spotify  *spotifyclient.Client
	db       *database.Database
	mutex    sync.Mutex
}

func NewController(db *database.Database) (*Controller, error) {
	discord, err := discord.NewSession()
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
		return nil, err
	}

	if discord == nil {
		return nil, errors.New("failed to create discord session")
	}

	if config.Config.Spotify.Enabled {
		err = spotify.NewSpotifyClient()
		if err != nil {
			log.Fatalf("Error creating Spotify client: %v", err)
			return nil, err
		}

		if spotify.Spotify == nil {
			return nil, errors.New("failed to create Spotify client")
		}
	}

	return &Controller{
		sessions: make(map[string]*GuildPlayer),
		discord:  discord,
		spotify:  spotify.Spotify,
		db:       db,
	}, nil
}

func (c *Controller) GetPlayer(guildID string) *GuildPlayer {
	if session, ok := c.sessions[guildID]; ok {
		return session
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if session, ok := c.sessions[guildID]; ok {
		return session
	}

	player, err := audio.NewPlayer()

	if err != nil {
		log.Errorf("Error creating player: %s", err)
		sentry.CaptureException(err)
		return nil
	}

	session := &GuildPlayer{
		// inject the global discord session to the player
		// todo: I think I could just make this a global variable
		Discord: c.discord,
		DB:      c.db,
		GuildID: guildID,
		Queue: &GuildQueue{
			notifications: make(chan QueueEvent, 100),
		},
		Loader:         audio.NewLoader(),
		Player:         player,
		LastActivityAt: time.Now(),
		idleCheckStop:  make(chan struct{}),
		SongHistory:    NewSongHistory(20),
	}

	session.listenForQueueEvents()
	session.listenForPlaybackEvents()
	session.listenForLoadEvents()
	session.startIdleChecker()

	c.sessions[guildID] = session
	return session
}

func (item *GuildQueueItem) WaitForStreamURL() bool {
	for range [300]int{} {
		if item.Stream != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return item.Stream != nil
}

func (p *GuildPlayer) Reset(ctx context.Context, interaction *GuildQueueItemInteraction) {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()

	// Stop the idle checker
	select {
	case p.idleCheckStop <- struct{}{}:
	default:
	}

	// wait for the playback to stop
	if p.Player != nil {
		p.Player.Stop()
	}

	// Disconnect from voice to ensure a clean rejoin on next play.
	// Without this, the voice connection can go stale after reset,
	// causing the next play command to hang.
	if p.VoiceConnection != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.VoiceConnection.Disconnect(ctx); err != nil {
			log.Errorf("Error disconnecting from voice during reset: %v", err)
		}
		p.VoiceConnection = nil
	}
	p.VoiceChannelID = nil

	p.Queue.Listening = false
	p.Queue.Items = nil
	p.CurrentSong = nil
	p.LastActivityAt = time.Now()

	// Restart the idle checker
	p.idleCheckStop = make(chan struct{})
	p.startIdleChecker()

	go discord.SendFollowup(&discord.FollowUpRequest{
		Token:   interaction.InteractionToken,
		AppID:   interaction.AppID,
		UserID:  interaction.UserID,
		Content: "the player has been reset",
	})
}

func (p *GuildPlayer) GetNext() *GuildQueueItem {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	if len(p.Queue.Items) > 0 {
		log.Tracef("returning next song in queue: %s", p.Queue.Items[0].Video.Title)
		return p.Queue.Items[0]
	}
	return nil
}

func (p *GuildPlayer) popQueue() {
	logger := log.WithFields(log.Fields{
		"module":  "controller",
		"method":  "popQueue",
		"guildID": p.GuildID,
	})

	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	if len(p.Queue.Items) > 0 {
		// Clear LoadResult reference to allow GC to reclaim ~55MB audio buffer
		if p.Queue.Items[0].LoadResult != nil {
			p.Queue.Items[0].LoadResult = nil
		}
		p.Queue.Items[0] = nil // Clear pointer to allow GC

		if len(p.Queue.Items) > 1 {
			p.Queue.Items = p.Queue.Items[1:]
			logger.Tracef("popped queue, next up: %s", p.Queue.Items[0].Video.Title)
		} else {
			logger.Tracef("no more songs in queue, resetting to empty")
			p.Queue.Items = []*GuildQueueItem{}
		}
	}
}

func (p *GuildPlayer) loadNext() {
	next := p.GetNext()
	if next != nil {
		log.Tracef("loading next song: %s", next.Video.Title)

		if !next.WaitForStreamURL() {
			log.Warnf("stream URL not found for %s", next.Video.Title)
			return
		}

		ctx := next.Context
		if ctx == nil {
			ctx = context.Background()
		}
		go p.Loader.Load(ctx, audio.LoadJob{
			URL:     next.Stream.StreamURL,
			VideoID: next.Video.VideoID,
			Title:   next.Video.Title,
		})
	}
}

func (p *GuildPlayer) playNext() {
	next := p.GetNext()
	if next != nil {
		log.Tracef("next up: %s", next.Video.Title)

		// Get context from queue item
		ctx := next.Context
		if ctx == nil {
			ctx = context.Background()
		}

		// Wait up to 30 seconds for stream to be ready
		if next.Stream == nil {
			log.Debugf("waiting for stream to be ready for %s", next.Video.Title)

			go discord.UpdateMessage(&discord.FollowUpRequest{
				Token:           next.Interaction.InteractionToken,
				AppID:           next.Interaction.AppID,
				UserID:          next.Interaction.UserID,
				Content:         "loading " + next.Video.Title + "...",
				GenerateContent: false,
			})

			for range [300]int{} {
				if next.Stream != nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}

		if next.LoadResult == nil {
			// load the stream
			// playback will start when the loader has finished
			go p.Loader.Load(ctx, audio.LoadJob{
				URL:     next.Stream.StreamURL,
				VideoID: next.Video.VideoID,
				Title:   next.Video.Title,
			})
		} else {
			// if song has already been loaded, play it
			log.Tracef("next song is already loaded, playing")
			go p.play(ctx, next.LoadResult)
		}
	} else {
		log.Tracef("no more songs in queue, stopping player")
		p.CurrentSong = nil
	}
}

func (p *GuildPlayer) play(ctx context.Context, data *audio.LoadResult) {
	log.Debugf("playing: %s", data.Title)
	p.LastActivityAt = time.Now()

	if err := p.Player.Play(ctx, data, p.VoiceConnection); err != nil {
		sentryhelper.CaptureException(ctx, err)
		log.Errorf("Error starting stream: %v", err)
	}
}

func (p *GuildPlayer) handleAdd(event QueueEvent) {
	log.Tracef("song added: %+v", event.Item.Video.Title)

	// Get context from queue item (isolated per-command)
	ctx := event.Item.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Add breadcrumb for queue add
	sentryhelper.AddBreadcrumb(ctx, &sentry.Breadcrumb{
		Category: "queue",
		Message:  "Song added to queue: " + event.Item.Video.Title,
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"video_id":   event.Item.Video.VideoID,
			"title":      event.Item.Video.Title,
			"guild_id":   p.GuildID,
			"guild_name": p.getGuildName(),
		},
	})

	stream, err := youtube.GetVideoStream(ctx, event.Item.Video)
	if err != nil {
		log.Errorf("Error getting video stream: %s", err)
		sentryhelper.CaptureException(ctx, err)
		go discord.UpdateMessage(&discord.FollowUpRequest{
			Token:   event.Item.Interaction.InteractionToken,
			AppID:   event.Item.Interaction.AppID,
			UserID:  event.Item.Interaction.UserID,
			Content: "Error getting video stream: " + err.Error(),
			Flags:   64,
		})
		return
	}
	log.Tracef("got stream for %s", event.Item.Video.Title)
	event.Item.Stream = stream

	shouldPlay := p.VoiceConnection != nil &&
		p.VoiceChannelID != nil &&
		p.CurrentSong == nil &&
		!p.Player.IsPlaying()

	// if the player is stopped, or not loading anything, play the next song in the queue
	if shouldPlay {
		next := p.GetNext()
		log.Tracef("no song playing, starting load job for: %s", next.Video.Title)
		nextCtx := next.Context
		if nextCtx == nil {
			nextCtx = context.Background()
		}
		go p.Loader.Load(nextCtx, audio.LoadJob{
			URL:     next.Stream.StreamURL,
			VideoID: next.Video.VideoID,
			Title:   next.Video.Title,
		})
		return
	}

	index := p.getIndexForItem(event.Item)
	log.Tracef("song is %d in the queue", index)
	if index == 0 {
		log.Tracef("song is next up, loading from stream url")
		p.loadNext()
		return
	}
}

func (p *GuildPlayer) JoinVoiceChannel(userID string) error {
	p.VoiceChannelMutex.Lock()
	defer p.VoiceChannelMutex.Unlock()

	voiceState, err := discord.GetMemberVoiceState(&userID, &p.GuildID)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error getting voice state: %s", err)
		return err
	}

	if voiceState == nil {
		return errors.New("voice state not found")
	}

	vc, err := discord.JoinVoiceChannel(p.Discord, p.GuildID, voiceState.ChannelID)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error joining voice channel: %s", err)
		return err
	}

	now := time.Now()

	p.VoiceConnection = vc
	p.VoiceChannelID = &voiceState.ChannelID
	p.VoiceJoinedAt = &now

	// Add breadcrumb for voice channel join (uses global scope since this is a guild-level operation)
	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "voice",
		Message:  "Joined voice channel: " + p.getChannelName(voiceState.ChannelID),
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"channel_id":   voiceState.ChannelID,
			"channel_name": p.getChannelName(voiceState.ChannelID),
			"guild_id":     p.GuildID,
			"guild_name":   p.getGuildName(),
		},
	})

	log.Tracef("joined voice channel: %s", voiceState.ChannelID)

	return nil
}

// getGuildName looks up the guild name from Discord, falling back to ID if unavailable
func (p *GuildPlayer) getGuildName() string {
	if p.Discord == nil {
		return p.GuildID
	}
	guild, err := p.Discord.State.Guild(p.GuildID)
	if err != nil {
		// Try API call if not in cache
		guild, err = p.Discord.Guild(p.GuildID)
		if err != nil {
			return p.GuildID
		}
	}
	return guild.Name
}

// getChannelName looks up a channel name from Discord, falling back to ID if unavailable
func (p *GuildPlayer) getChannelName(channelID string) string {
	if p.Discord == nil || channelID == "" {
		return channelID
	}
	channel, err := p.Discord.State.Channel(channelID)
	if err != nil {
		// Try API call if not in cache
		channel, err = p.Discord.Channel(channelID)
		if err != nil {
			return channelID
		}
	}
	return channel.Name
}

func (p *GuildPlayer) findQueueItemByVideoID(videoID string) (*GuildQueueItem, int) {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	for i, item := range p.Queue.Items {
		if item.Video.VideoID == videoID {
			return item, i
		}
	}
	return nil, -1
}

func (p *GuildPlayer) removeItemByVideoID(videoID string) int {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	for i, item := range p.Queue.Items {
		if item.Video.VideoID == videoID {
			// Clear references before removing to allow GC to reclaim memory
			if p.Queue.Items[i].LoadResult != nil {
				p.Queue.Items[i].LoadResult = nil
			}
			copy(p.Queue.Items[i:], p.Queue.Items[i+1:])
			p.Queue.Items[len(p.Queue.Items)-1] = nil // Clear trailing reference
			p.Queue.Items = p.Queue.Items[:len(p.Queue.Items)-1]
			log.Tracef("removed item by videoID: %s", videoID)
			return i
		}
	}
	log.Tracef("no item found by videoID: %s", videoID)
	return -1
}

func (p *GuildPlayer) getIndexForItem(queueItem *GuildQueueItem) int {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	for i, item := range p.Queue.Items {
		if item == queueItem {
			return i
		}
	}
	return -1
}

func (p *GuildPlayer) listenForQueueEvents() {
	p.Queue.Listening = true
	go func() {
		for event := range p.Queue.notifications {
			log.Tracef("Queue event: %s", event.Type)
			switch event.Type {
			case EventAdd:
				p.handleAdd(event)
			case EventSkip:
				log.Printf("Skipping to next song in queue")
				sentry.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "queue",
					Message:  "Skip requested",
					Level:    sentry.LevelInfo,
					Data: map[string]interface{}{
						"guild_id":   p.GuildID,
						"guild_name": p.getGuildName(),
					},
				})
				p.Player.Stop()
				p.playNext()
				// PlaybackStopped event will play next song
			case EventClear:
				log.Debug("queue has been cleared")
				// we don't stop playback here, we just dump the rest of the queue
			}
		}
	}()
}

func (p *GuildPlayer) listenForLoadEvents() {
	log.Tracef("listening for load events")
	go func() {
		for event := range p.Loader.Notifications {
			log.Tracef("Load event: %s", event.Event)
			videoID := event.VideoID
			var queueItem *GuildQueueItem
			var queueIndex int
			if videoID != nil {
				queueItem, queueIndex = p.findQueueItemByVideoID(*videoID)
			}

			switch event.Event {
			case audio.PlaybackLoaded:
				if queueItem != nil && event.LoadResult != nil {
					if queueIndex == 0 && p.CurrentSong == nil {
						log.Tracef("loaded song is next up, playing")
						ctx := queueItem.Context
						if ctx == nil {
							ctx = context.Background()
						}
						go p.play(ctx, event.LoadResult)
					} else {
						log.Tracef("loaded song read for index %d, setting load result", queueIndex)
						queueItem.LoadResult = event.LoadResult
					}
				}
			case audio.PlaybackLoadCanceled:
				log.Tracef("load for %s canceled", *event.VideoID)
			case audio.PlaybackLoadError:
				err := event.Error
				var errStr string
				if err != nil {
					errStr = (*err).Error()
				}

				log.Tracef("[loaderror] queueItem: %+v", queueItem)

				if queueItem != nil {
					// Increment load attempts for circuit breaker
					queueItem.LoadAttempts++

					log.Warnf("Load failed for %s (attempt %d/%d): %s",
						queueItem.Video.Title, queueItem.LoadAttempts, queueItem.MaxAttempts, errStr)

					// Check if we've exceeded max retry attempts
					if queueItem.LoadAttempts >= queueItem.MaxAttempts {
						// Remove item permanently after max retries
						p.removeItemByVideoID(*event.VideoID)

						var msg string
						if strings.Contains(errStr, "ffmpeg timed out") {
							msg = "❌ Permanently removed **" + queueItem.Video.Title + "** after " +
								strconv.Itoa(queueItem.MaxAttempts) + " failed attempts\n" +
								"It may be too long, try something shorter :)"
						} else if errStr != "" {
							msg = "❌ Permanently removed **" + queueItem.Video.Title + "** after " +
								strconv.Itoa(queueItem.MaxAttempts) + " failed attempts\n" +
								"Error: " + errStr
						} else {
							msg = "❌ Permanently removed **" + queueItem.Video.Title + "** after " +
								strconv.Itoa(queueItem.MaxAttempts) + " failed attempts"
						}

						go discord.SendFollowup(&discord.FollowUpRequest{
							Token:           queueItem.Interaction.InteractionToken,
							AppID:           queueItem.Interaction.AppID,
							UserID:          queueItem.Interaction.UserID,
							Content:         msg,
							GenerateContent: false,
						})

						log.Infof("Removed %s from queue after %d failed load attempts",
							queueItem.Video.Title, queueItem.MaxAttempts)
					} else {
						// Check if this is a 403 error - refresh stream URL before retry
						if strings.Contains(errStr, "403 Forbidden") {
							log.Infof("Got 403, refreshing stream URL for %s", queueItem.Video.Title)

							// Notify user we're reloading the track (with attempt count for consistency)
							discord.SendFollowup(&discord.FollowUpRequest{
								Token:  queueItem.Interaction.InteractionToken,
								AppID:  queueItem.Interaction.AppID,
								UserID: queueItem.Interaction.UserID,
								Content: "🔄 YouTube rejected the stream, reloading **" + queueItem.Video.Title +
									"** (attempt " + strconv.Itoa(queueItem.LoadAttempts) + "/" +
									strconv.Itoa(queueItem.MaxAttempts) + ")...",
								GenerateContent: false,
							})

							retryCtx := queueItem.Context
							if retryCtx == nil {
								retryCtx = context.Background()
							}
							newStream, streamErr := youtube.GetVideoStream(retryCtx, queueItem.Video)
							if streamErr == nil {
								queueItem.Stream = newStream
								log.Infof("Successfully refreshed stream URL for %s", queueItem.Video.Title)

								go discord.SendFollowup(&discord.FollowUpRequest{
									Token:           queueItem.Interaction.InteractionToken,
									AppID:           queueItem.Interaction.AppID,
									UserID:          queueItem.Interaction.UserID,
									Content:         "✅ Stream reloaded successfully, retrying...",
									GenerateContent: false,
								})
							} else {
								log.Warnf("Failed to refresh stream URL: %v", streamErr)

								go discord.SendFollowup(&discord.FollowUpRequest{
									Token:           queueItem.Interaction.InteractionToken,
									AppID:           queueItem.Interaction.AppID,
									UserID:          queueItem.Interaction.UserID,
									Content:         "⚠️ Could not reload stream, will retry with original URL",
									GenerateContent: false,
								})
							}
						} else {
							// Retry - notify user we're retrying (non-403 errors)
							var msg string
							if strings.Contains(errStr, "ffmpeg timed out") {
								msg = "⚠️ Timeout loading **" + queueItem.Video.Title + "** (attempt " +
									strconv.Itoa(queueItem.LoadAttempts) + "/" + strconv.Itoa(queueItem.MaxAttempts) +
									"), retrying..."
							} else if errStr != "" {
								msg = "⚠️ Error loading **" + queueItem.Video.Title + "** (attempt " +
									strconv.Itoa(queueItem.LoadAttempts) + "/" + strconv.Itoa(queueItem.MaxAttempts) +
									"), retrying...\nError: " + errStr
							} else {
								msg = "⚠️ Error loading **" + queueItem.Video.Title + "** (attempt " +
									strconv.Itoa(queueItem.LoadAttempts) + "/" + strconv.Itoa(queueItem.MaxAttempts) +
									"), retrying..."
							}

							go discord.SendFollowup(&discord.FollowUpRequest{
								Token:           queueItem.Interaction.InteractionToken,
								AppID:           queueItem.Interaction.AppID,
								UserID:          queueItem.Interaction.UserID,
								Content:         msg,
								GenerateContent: false,
							})
						}

						log.Infof("Retrying load for %s (attempt %d/%d)",
							queueItem.Video.Title, queueItem.LoadAttempts, queueItem.MaxAttempts)
					}
				}

				// Always try to play next, either the retried item or the next one if removed
				go p.playNext()
			case audio.PlaybackLoading:
				log.Tracef("Loading %s", event.Event)
			default:
				log.Warnf("Unknown load event: %s", event.Event)
			}
		}
	}()
}

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
			case audio.PlaybackPaused:
				sentry.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "playback",
					Message:  "Playback paused",
					Level:    sentry.LevelInfo,
					Data: map[string]interface{}{
						"guild_id":   p.GuildID,
						"guild_name": p.getGuildName(),
					},
				})
				p.VoiceConnection.Speaking(false)
			case audio.PlaybackResumed:
				sentry.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "playback",
					Message:  "Playback resumed",
					Level:    sentry.LevelInfo,
					Data: map[string]interface{}{
						"guild_id":   p.GuildID,
						"guild_name": p.getGuildName(),
					},
				})
				p.VoiceConnection.Speaking(true)
			case audio.PlaybackStopped:
				sentry.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "playback",
					Message:  "Playback stopped",
					Level:    sentry.LevelInfo,
					Data: map[string]interface{}{
						"guild_id":   p.GuildID,
						"guild_name": p.getGuildName(),
					},
				})
				p.CurrentSong = nil
				p.VoiceConnection.Speaking(false)
			case audio.PlaybackCompleted:
				sentry.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "playback",
					Message:  "Playback completed",
					Level:    sentry.LevelInfo,
					Data: map[string]interface{}{
						"guild_id":   p.GuildID,
						"guild_name": p.getGuildName(),
						"video_id":   videoID,
					},
				})
				p.CurrentSong = nil
				p.VoiceConnection.Speaking(false)
				p.playNext()

				// If radio is enabled and queue is empty, auto-queue a similar song
				if p.IsRadioEnabled() && p.IsEmpty() && p.SongHistory.Len() > 0 {
					go p.queueRadioSong()
				}
			case audio.PlaybackStarted:
				if queueItem != nil {
					log.Tracef("playback started for %s", queueItem.Video.Title)
					p.CurrentSong = &queueItem.Video.Title
					sentry.AddBreadcrumb(&sentry.Breadcrumb{
						Category: "playback",
						Message:  "Playback started: " + queueItem.Video.Title,
						Level:    sentry.LevelInfo,
						Data: map[string]interface{}{
							"guild_id":   p.GuildID,
							"guild_name": p.getGuildName(),
							"video_id":   queueItem.Video.VideoID,
							"title":      queueItem.Video.Title,
						},
					})

					// Record in song history for radio mode
					p.SongHistory.Add(SongHistoryEntry{
						VideoID: queueItem.Video.VideoID,
						Title:   queueItem.Video.Title,
					})

					// Record play in database
					if p.DB != nil {
						userID := ""
						username := ""
						if queueItem.Interaction != nil {
							userID = queueItem.Interaction.UserID
						}
						url := "https://www.youtube.com/watch?v=" + queueItem.Video.VideoID
						if err := p.DB.RecordPlay(p.GuildID, queueItem.Video.VideoID, queueItem.Video.Title, url, userID, username, 0); err != nil {
							log.Errorf("Failed to record play in database: %v", err)
						}
					}
				}
				p.VoiceConnection.Speaking(true)
				// once a song starts playback, we can pop it from the queue
				p.popQueue()
				// if there are more songs in the queue, load the next one
				p.loadNext()
			case audio.PlaybackError:
				p.CurrentSong = nil
				p.VoiceConnection.Speaking(false)

				err := event.Error

				// parse the error, if any
				var errStr string
				if err != nil {
					log.Errorf("Error playing stream: %v", err)
					errStr = (*err).Error()
				}

				log.Tracef("[loaderror] queueItem: %+v", queueItem.Video.Title)

				// if we found a queue item, send a followup to the user notifying them of the error
				if queueItem != nil {
					var msg string
					if errStr != "" {
						msg = "Something went wrong while playing " + queueItem.Video.Title + "\nError: " + errStr
					} else {
						msg = "Something went wrong while playing " + queueItem.Video.Title
					}

					go discord.UpdateMessage(&discord.FollowUpRequest{
						Token:   queueItem.Interaction.InteractionToken,
						AppID:   queueItem.Interaction.AppID,
						UserID:  queueItem.Interaction.UserID,
						Content: msg,
					})
				}

				p.playNext()
			default:
				log.Warnf("Unknown playback event: %s", event.Event)
			}
		}
	}()
}

func (p *GuildPlayer) startIdleChecker() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		idleTimeout := time.Duration(config.Config.Options.IdleTimeoutMinutes) * time.Minute
		log.Debugf("Starting idle checker for guild %s with timeout: %v", p.GuildID, idleTimeout)

		for {
			select {
			case <-ticker.C:
				idleDuration := time.Since(p.LastActivityAt)
				if idleDuration >= idleTimeout {
					log.Infof("Guild %s has been idle for %v, disconnecting", p.GuildID, idleDuration)

					if p.LastTextChannelID != "" {
						prompt := fmt.Sprintf("The bot has been idle in the voice channel for %d minutes with no activity, so it's disconnecting now", config.Config.Options.IdleTimeoutMinutes)
						// Use background context since this is from the idle checker goroutine
						message := gemini.GenerateResponse(context.Background(), prompt)

						if message == "" {
							message = fmt.Sprintf("Been sitting here idle for %d minutes with nothing to do. I'm out - let me know when you actually want to hear something.", config.Config.Options.IdleTimeoutMinutes)
						}

						_, err := p.Discord.ChannelMessageSend(p.LastTextChannelID, message)
						if err != nil {
							log.Errorf("Failed to send idle disconnect message: %v", err)
						}
					}

					if p.Player != nil {
						p.Player.Stop()
					}

					if p.VoiceConnection != nil {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						if err := p.VoiceConnection.Disconnect(ctx); err != nil {
							log.Errorf("Error disconnecting from voice: %v", err)
						}
						cancel()
						p.VoiceConnection = nil
					}

					p.VoiceChannelID = nil
					p.Clear()
					return
				}
			case <-p.idleCheckStop:
				log.Debugf("Stopping idle checker for guild %s", p.GuildID)
				return
			}
		}
	}()
}

// ToggleRadio toggles radio mode for the guild and returns the new state
func (p *GuildPlayer) ToggleRadio() bool {
	p.radioMutex.Lock()
	defer p.radioMutex.Unlock()
	p.RadioEnabled = !p.RadioEnabled
	return p.RadioEnabled
}

// IsRadioEnabled returns whether radio mode is on
func (p *GuildPlayer) IsRadioEnabled() bool {
	p.radioMutex.Lock()
	defer p.radioMutex.Unlock()
	return p.RadioEnabled
}

// ExtractArtist parses common YouTube music title formats to extract the artist name.
// Returns the artist if found, or the full title as fallback.
func ExtractArtist(title string) string {
	// Remove common suffixes like (Official Video), [Official Music Video], (Lyrics), etc.
	cleaned := title
	for _, suffix := range []string{
		"(Official Video)", "(Official Music Video)", "(Official Audio)",
		"(Lyrics)", "(Lyric Video)", "(Audio)", "(Visualizer)",
		"[Official Video]", "[Official Music Video]", "[Official Audio]",
		"[Lyrics]", "[Lyric Video]", "[Audio]",
	} {
		cleaned = strings.Replace(cleaned, suffix, "", 1)
	}
	cleaned = strings.TrimSpace(cleaned)

	// Try to split on " - " to get "Artist - Title"
	parts := strings.SplitN(cleaned, " - ", 2)
	if len(parts) == 2 {
		artist := strings.TrimSpace(parts[0])
		// Remove "ft.", "feat." etc from artist for cleaner search
		for _, feat := range []string{" ft.", " feat.", " ft ", " feat ", " featuring "} {
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

// queueRadioSong finds and queues a similar song based on play history
func (p *GuildPlayer) queueRadioSong() {
	logger := log.WithFields(log.Fields{
		"module":  "controller",
		"method":  "queueRadioSong",
		"guildID": p.GuildID,
	})

	if p.SongHistory.Len() == 0 {
		logger.Debug("no song history for radio, skipping")
		return
	}

	ctx := context.Background()

	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "radio",
		Message:  "Radio auto-queuing triggered",
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"guild_id":   p.GuildID,
			"guild_name": p.getGuildName(),
		},
	})

	// Get recent songs to build a search query
	recent := p.SongHistory.GetRecent(5)
	historyIDs := p.SongHistory.GetAllVideoIDs()

	// Also exclude anything currently in queue
	p.Queue.Mutex.Lock()
	for _, item := range p.Queue.Items {
		historyIDs[item.Video.VideoID] = true
	}
	p.Queue.Mutex.Unlock()

	// Pick a random recent song's artist for variety
	idx := rand.Intn(len(recent))
	artist := ExtractArtist(recent[idx].Title)
	query := artist + " music"

	logger.Infof("Radio searching for: %s", query)

	videos := youtube.Query(ctx, query)
	if len(videos) == 0 {
		logger.Warn("radio: no YouTube results found")
		return
	}

	// Find first result not already played
	var picked *youtube.VideoResponse
	for i := range videos {
		if !historyIDs[videos[i].VideoID] {
			picked = &videos[i]
			break
		}
	}

	if picked == nil {
		logger.Info("radio: all search results already in history, trying broader query")
		// Fallback: try with a different recent song
		fallbackIdx := (idx + 1) % len(recent)
		fallbackArtist := ExtractArtist(recent[fallbackIdx].Title)
		fallbackQuery := fallbackArtist + " songs"

		videos = youtube.Query(ctx, fallbackQuery)
		for i := range videos {
			if !historyIDs[videos[i].VideoID] {
				picked = &videos[i]
				break
			}
		}
	}

	if picked == nil {
		logger.Warn("radio: could not find a non-duplicate song")
		return
	}

	logger.Infof("Radio queuing: %s", picked.Title)

	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "radio",
		Message:  "Radio auto-queued: " + picked.Title,
		Level:    sentry.LevelInfo,
		Data: map[string]interface{}{
			"guild_id":   p.GuildID,
			"guild_name": p.getGuildName(),
			"video_id":   picked.VideoID,
			"title":      picked.Title,
			"query":      query,
		},
	})

	// Send announcement to the text channel
	if p.LastTextChannelID != "" && p.Discord != nil {
		msg := "📻 **Radio:** queued **" + picked.Title + "**"
		if _, err := p.Discord.ChannelMessageSend(p.LastTextChannelID, msg); err != nil {
			logger.Errorf("Failed to send radio announcement: %v", err)
		}
	}

	// Use a synthetic interaction for radio-queued items
	// We use empty interaction token/appID since there's no user interaction
	p.Add(ctx, *picked, "", "", "")
}

func (p *GuildPlayer) Add(ctx context.Context, video youtube.VideoResponse, userID string, interactionToken string, appID string) {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()

	p.LastActivityAt = time.Now()

	item := &GuildQueueItem{
		Video:   video,
		AddedAt: time.Now(),
		Interaction: &GuildQueueItemInteraction{
			UserID:           userID,
			InteractionToken: interactionToken,
			AppID:            appID,
		},
		LoadAttempts: 0,
		MaxAttempts:  3, // Circuit breaker: max 3 attempts per item
		// Detach from original transaction since load/playback happens later.
		// The hub is preserved for breadcrumb isolation.
		Context: sentryhelper.DetachFromTransaction(ctx),
	}
	p.Queue.Items = append(p.Queue.Items, item)

	select {
	case p.Queue.notifications <- QueueEvent{
		Type: EventAdd,
		Item: item,
	}:
	default:
		// This is a warning, not an error - queue is full but not broken
		msg := "Queue notifications channel is full for guild " + p.GuildID
		sentryhelper.CaptureMessage(ctx, msg)
		log.Warn(msg)
	}
}

func (p *GuildPlayer) Remove(index int) string {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()

	if len(p.Queue.Items) == 0 {
		return ""
	}

	if index < 1 || index > len(p.Queue.Items) {
		return ""
	}

	removed := p.Queue.Items[index-1]
	// Clear LoadResult before removing to allow GC to reclaim memory
	if removed != nil && removed.LoadResult != nil {
		removed.LoadResult = nil
	}
	copy(p.Queue.Items[index-1:], p.Queue.Items[index:])
	p.Queue.Items[len(p.Queue.Items)-1] = nil // Clear trailing reference
	p.Queue.Items = p.Queue.Items[:len(p.Queue.Items)-1]

	if removed == nil {
		return ""
	}
	return removed.Video.Title
}

func (p *GuildPlayer) Skip() {
	p.LastActivityAt = time.Now()

	select {
	case p.Queue.notifications <- QueueEvent{Type: EventSkip}:
	default:
		msg := "Queue notifications channel is full for guild " + p.GuildID
		sentry.CaptureMessage(msg)
		log.Warn(msg)
	}
}

func (p *GuildPlayer) Clear() {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	p.Queue.Items = []*GuildQueueItem{}
	select {
	case p.Queue.notifications <- QueueEvent{Type: EventClear}:
	default:
		msg := "Queue notifications channel is full for guild " + p.GuildID
		sentry.CaptureMessage(msg)
		log.Warn(msg)
	}
}

func (p *GuildPlayer) IsEmpty() bool {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	return len(p.Queue.Items) == 0
}

func (p *GuildPlayer) Shuffle() int {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()

	if len(p.Queue.Items) <= 1 {
		return len(p.Queue.Items)
	}

	// Shuffle only affects queued songs; currently playing song (if any) is not in queue
	rand.Shuffle(len(p.Queue.Items), func(i, j int) {
		p.Queue.Items[i], p.Queue.Items[j] = p.Queue.Items[j], p.Queue.Items[i]
	})

	return len(p.Queue.Items)
}
