package controller

import (
	"beatbot/audio"
	"beatbot/discord"
	"beatbot/youtube"
	"strings"
	"sync"
	"time"

	sentry "github.com/getsentry/sentry-go"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
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

type GuildPlayer struct {
	Discord               *discordgo.Session
	GuildID               string
	CurrentSong           *string
	Queue                 *GuildQueue
	PlaybackMutex         sync.Mutex
	VoiceChannelMutex     sync.Mutex
	VoiceChannelID        *string
	VoiceJoinedAt         *time.Time
	VoiceConnection       *discordgo.VoiceConnection
	playbackNotifications chan audio.PlaybackNotification
	PlaybackState         *audio.PlaybackState
}

type GuildQueueItemInteraction struct {
	UserID           string
	InteractionToken string
	AppID            string
}

type GuildQueueItem struct {
	Video       youtube.VideoResponse
	Stream      *youtube.YoutubeStream
	AddedAt     time.Time
	Interaction *GuildQueueItemInteraction
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
	mutex    sync.Mutex
}

func NewController() (*Controller, error) {
	discord, err := discord.NewSession()
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
		return nil, err
	}

	return &Controller{
		sessions: make(map[string]*GuildPlayer),
		discord:  discord,
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

	playbackNotifications := make(chan audio.PlaybackNotification, 100)
	session := &GuildPlayer{
		// inject the global discord session to the player
		// todo: I think I could just make this a global variable
		Discord: c.discord,
		GuildID: guildID,
		Queue: &GuildQueue{
			notifications: make(chan QueueEvent, 100),
		},
		playbackNotifications: playbackNotifications,
		PlaybackState:         audio.NewPlaybackState(playbackNotifications),
	}

	session.listenForQueueEvents()
	session.listenForPlaybackEvents()

	c.sessions[guildID] = session
	return session
}

func (p *GuildPlayer) Reset(interaction *GuildQueueItemInteraction) {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()

	// wait for the playback to stop
	done := make(chan bool)
	if p.PlaybackState != nil {
		go func() {
			p.PlaybackState.Reset()
			done <- true
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			log.Warn("Timeout waiting for playback to stop")
		}
	}

	// note: we don't necessarily need to quit the vc here, just reset the playback states
	p.Queue.Listening = false
	p.CurrentSong = nil
	p.PlaybackState = audio.NewPlaybackState(p.playbackNotifications)

	go discord.SendFollowup(&discord.FollowUpRequest{
		Token:   interaction.InteractionToken,
		AppID:   interaction.AppID,
		UserID:  interaction.UserID,
		Content: "the player has been reset, attempting to play next song",
	})

	p.playNext()
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
	if len(p.Queue.Items) > 1 {
		p.Queue.Items = p.Queue.Items[1:]
		logger.Tracef("popped queue, next up: %s", p.Queue.Items[0].Video.Title)
	} else {
		logger.Tracef("no more songs in queue, resetting to empty")
		p.Queue.Items = []*GuildQueueItem{}
	}
}

func (p *GuildPlayer) playNext() {
	next := p.GetNext()
	if next != nil {
		log.Tracef("next up: %s", next.Video.Title)
		// Wait up to 30 seconds for stream to be ready
		if next.Stream == nil {
			log.Tracef("waiting for stream to be ready for %s", next.Video.Title)

			go discord.SendFollowup(&discord.FollowUpRequest{
				Token:   next.Interaction.InteractionToken,
				AppID:   next.Interaction.AppID,
				UserID:  next.Interaction.UserID,
				Content: "loading " + next.Video.Title + "...",
			})

			for i := 0; i < 300; i++ {
				if next.Stream != nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}

		log.Tracef("playing from playNext: %s", next.Video.Title)
		p.play(*next.Stream)
	} else {
		log.Tracef("no more songs in queue, stopping player")
		p.CurrentSong = nil
	}
}

func (p *GuildPlayer) play(video youtube.YoutubeStream) {
	p.PlaybackMutex.Lock()
	defer p.PlaybackMutex.Unlock()

	log.Debugf("playing: %s", video.Title)

	if p.PlaybackState == nil {
		p.PlaybackState = audio.NewPlaybackState(p.playbackNotifications)
	}

	if err := p.PlaybackState.StartStream(p.VoiceConnection, video.StreamURL, video.VideoID); err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error starting stream: %v", err)
	}
}

func (p *GuildPlayer) handleAdd(event QueueEvent) {
	log.Tracef("song added: %+v", event.Item.Video.Title)
	stream, err := youtube.GetVideoStream(event.Item.Video)
	if err != nil {
		log.Errorf("Error getting video stream: %s", err)
		sentry.CaptureException(err)
		go discord.SendFollowup(&discord.FollowUpRequest{
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
		!p.PlaybackState.IsPlaying() &&
		!p.PlaybackState.IsLoading()

	// if the player is stopped, or not loading anything, play the next song in the queue
	if shouldPlay {
		next := p.GetNext()
		log.Tracef("no song playing, playing from handleAdd: %s", next.Video.Title)
		go p.play(*next.Stream)
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

	log.Tracef("joined voice channel: %s", voiceState.ChannelID)

	return nil
}

func (p *GuildPlayer) findQueueItemByVideoID(videoID string) *GuildQueueItem {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	for _, item := range p.Queue.Items {
		if item.Video.VideoID == videoID {
			return item
		}
	}
	return nil
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
				p.PlaybackState.Stop()
				p.playNext()
			case EventClear:
				log.Debug("queue has been cleared")
				// we don't stop playback here, we just dump the rest of the queue
			}
		}
	}()
}

func (p *GuildPlayer) listenForPlaybackEvents() {
	go func() {
		for event := range p.playbackNotifications {
			log.Tracef("Playback event: %s", event.Event)
			videoID := event.VideoID
			var queueItem *GuildQueueItem

			if videoID != nil {
				queueItem = p.findQueueItemByVideoID(*videoID)
			}

			switch event.Event {
			case audio.PlaybackPaused:
				p.VoiceConnection.Speaking(false)
			case audio.PlaybackResumed:
				p.VoiceConnection.Speaking(true)
			case audio.PlaybackStopped:
				p.CurrentSong = nil
				p.VoiceConnection.Speaking(false)
			case audio.PlaybackCompleted:
				p.CurrentSong = nil
				p.VoiceConnection.Speaking(false)
				p.playNext()
			case audio.PlaybackStarted:
				if queueItem != nil {
					p.CurrentSong = &queueItem.Video.Title
				}
				p.VoiceConnection.Speaking(true)
				p.popQueue()
			case audio.PlaybackError:
				p.CurrentSong = nil
				p.VoiceConnection.Speaking(false)

				err := event.Error

				// parse the error, if any
				var ffmpegTimeoutErr = false
				var errStr string
				if err != nil {
					log.Errorf("Error playing stream: %v", err)
					errStr = (*err).Error()
					if strings.Contains(errStr, "ffmpeg timed out") {
						ffmpegTimeoutErr = true
					}
				}

				// if we found a queue item, send a followup to the user notifying them of the error
				if queueItem != nil {
					var msg string
					if ffmpegTimeoutErr {
						msg = "timed out loading **" + queueItem.Video.Title + "**\nIt may be too long, try something shorter :)"
					} else if errStr != "" {
						msg = "Something went wrong while playing " + queueItem.Video.Title + "\nError: " + errStr
					} else {
						msg = "Something went wrong while playing " + queueItem.Video.Title
					}

					go discord.SendFollowup(&discord.FollowUpRequest{
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

// quits the playback state and closes the voice connection
// this also clears the stream and closes the ffmpeg process
func (p *GuildPlayer) quitPlayback() {
	p.PlaybackState.Quit()
	p.VoiceConnection.Close()
}

func (p *GuildPlayer) Add(video youtube.VideoResponse, userID string, interactionToken string, appID string) {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()

	item := &GuildQueueItem{
		Video:   video,
		AddedAt: time.Now(),
		Interaction: &GuildQueueItemInteraction{
			UserID:           userID,
			InteractionToken: interactionToken,
			AppID:            appID,
		},
	}
	p.Queue.Items = append(p.Queue.Items, item)

	select {
	case p.Queue.notifications <- QueueEvent{
		Type: EventAdd,
		Item: item,
	}:
	default:
		msg := "Queue notifications channel is full for guild " + p.GuildID
		sentry.CaptureMessage(msg)
		log.Warn(msg)
	}
}

func (p *GuildPlayer) Remove(index int) string {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()

	if len(p.Queue.Items) == 0 {
		return ""
	}

	var removed *GuildQueueItem
	removed = p.Queue.Items[index-1]
	p.Queue.Items = append(p.Queue.Items[:index-1], p.Queue.Items[index:]...)

	return removed.Video.Title
}

func (p *GuildPlayer) Skip() {
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
