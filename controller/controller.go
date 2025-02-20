package controller

import (
	"beatbot/audio"
	"beatbot/discord"
	"beatbot/youtube"
	"sync"
	"time"

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

type GuildPlayerState string

const (
	Stopped GuildPlayerState = "stopped"
	Playing GuildPlayerState = "playing"
	Paused  GuildPlayerState = "paused"
)

type GuildPlayer struct {
	Discord               *discordgo.Session
	GuildID               string
	State                 GuildPlayerState
	CurrentSong           *string
	Queue                 *GuildQueue
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
		State:   Stopped,
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

func (p *GuildPlayer) GetNext() *GuildQueueItem {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	if len(p.Queue.Items) > 0 {
		log.Tracef("returning next song in queue: %s", p.Queue.Items[0].Video.Title)
		return p.Queue.Items[0]
	}
	log.Tracef("no more songs in queue")
	return nil
}

func (p *GuildPlayer) popQueue() {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	if len(p.Queue.Items) > 1 {
		p.Queue.Items = p.Queue.Items[1:]
	} else {
		p.Queue.Items = []*GuildQueueItem{}
	}
}

func (p *GuildPlayer) playNext() {
	log.Tracef("in playNext")

	next := p.GetNext()
	if next != nil {
		log.Tracef("playing: %s", next.Video.Title)
		go p.play(*next.Stream)
	} else {
		log.Tracef("no more songs in queue, stopping player")
		p.State = Stopped
		p.CurrentSong = nil
	}
}

func (p *GuildPlayer) play(video youtube.YoutubeStream) {
	log.Debugf("playing: %s", video.Title)

	p.popQueue()

	p.CurrentSong = &video.Title
	p.VoiceConnection.Speaking(true)
	defer p.VoiceConnection.Speaking(false)
	p.State = Playing

	if p.PlaybackState == nil {
		p.PlaybackState = audio.NewPlaybackState(p.playbackNotifications)
	}

	go func() {
		if err := p.PlaybackState.StartStream(p.VoiceConnection, video.StreamURL); err != nil {
			log.Errorf("Error starting stream: %v", err)
			p.VoiceConnection.Speaking(false)
		}
	}()
}

func (p *GuildPlayer) handleAdd(event QueueEvent) {
	log.Tracef("song added: %+v", event.Item.Video.Title)
	stream, err := youtube.GetVideoStream(event.Item.Video)
	if err != nil {
		log.Errorf("Error getting video stream: %s", err)
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

	// if the player is stopped, play the next song in the queue
	if p.State == Stopped && p.VoiceConnection != nil && p.VoiceChannelID != nil {
		next := p.GetNext()
		log.Tracef("no song, playing: %s", next.Video.Title)
		go p.play(*next.Stream)
	}
}

func (p *GuildPlayer) JoinVoiceChannel(userID string) error {
	voiceState, err := discord.GetMemberVoiceState(&userID, &p.GuildID)
	if err != nil {
		log.Errorf("Error getting voice state: %s", err)
		return err
	}

	vc, err := discord.JoinVoiceChannel(p.Discord, p.GuildID, voiceState.ChannelID)
	if err != nil {
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
				p.PlaybackState.Quit()
				p.playNext()
			case EventClear:
				p.State = Stopped
				p.PlaybackState.Stop()
			}
		}
	}()
}

func (p *GuildPlayer) listenForPlaybackEvents() {
	go func() {
		for event := range p.playbackNotifications {
			log.Tracef("Playback event: %s", event.Event)
			switch event.Event {
			case audio.PlaybackCompleted:
				p.playNext()
			case audio.PlaybackPaused:
				p.State = Paused
			case audio.PlaybackResumed, audio.PlaybackStarted:
				p.State = Playing
			case audio.PlaybackStopped:
				p.State = Stopped
				p.CurrentSong = nil
			case audio.PlaybackError:
				err := event.Error
				if err != nil {
					log.Errorf("Error playing stream: %v", err)
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
	p.State = Stopped
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
		log.Warnf("Queue notifications channel is full for guild %s", p.GuildID)
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
		log.Warnf("Queue notifications channel is full for guild %s", p.GuildID)
	}
}

func (p *GuildPlayer) Stop() {
	p.quitPlayback()
}

func (p *GuildPlayer) Clear() {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	p.Queue.Items = []*GuildQueueItem{}
	select {
	case p.Queue.notifications <- QueueEvent{Type: EventClear}:
	default:
		log.Warnf("Queue notifications channel is full for guild %s", p.GuildID)
	}
}

func (p *GuildPlayer) IsEmpty() bool {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	return len(p.Queue.Items) == 0
}
