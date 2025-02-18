package controller

import (
	"beatbot/discord"
	"beatbot/models"
	"beatbot/youtube"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
)

type QueueEventType string

const (
    EventAdd    QueueEventType = "add"
    EventSkip   QueueEventType = "skip"
    EventClear  QueueEventType = "clear"
)

type QueueEvent struct {
    Type    QueueEventType
    Item    GuildQueueItem
}

type GuildPlayerState string

const (
	Stopped GuildPlayerState = "stopped"
	Playing GuildPlayerState = "playing"
	Paused GuildPlayerState = "paused"
)

type GuildPlayer struct {
	Discord *discordgo.Session
	Presence *discordgo.Presence
	GuildID string
	State GuildPlayerState
	Members map[string]*models.Member
	Queue *GuildQueue
	VoiceChannelID *string
	VoiceJoinedAt *time.Time
	VoiceConnection *discordgo.VoiceConnection
}

type GuildQueueItem struct {
	Video youtube.YoutubeStream
	AddedAt time.Time
	AddedBy *string
}

type GuildQueue struct {
	Items []GuildQueueItem
	Listening bool
	Mutex sync.Mutex
	notifications chan QueueEvent
}

type Controller struct {
	// This is a map of guildID to the player for that guild
	sessions map[string]*GuildPlayer
	discord *discordgo.Session
	mutex sync.Mutex
}

func NewController() (*Controller, error) {
	discord, err := discord.NewSession()
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
		return nil, err
	}

	return &Controller{
		sessions: make(map[string]*GuildPlayer),
		discord: discord,
	}, nil
}

func (c *Controller) GetPlayer(guildID string) *GuildPlayer {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if session, ok := c.sessions[guildID]; ok {
		log.Printf("Found existing player for guild %s", guildID)
		if !session.Queue.Listening {
			session.Listen()
		}
		return session
	}

	session := &GuildPlayer{
		// propagate the global discord session to the player
		Discord: c.discord,
		GuildID: guildID,
		State: Stopped,
		Queue: &GuildQueue{
			notifications: make(chan QueueEvent, 100),
		},
	}

	if !session.Queue.Listening {
		session.Listen()
	}

	c.sessions[guildID] = session
	log.Printf("Created new player for guild %s", guildID)
	return session
}

func (p *GuildPlayer) GetNext() GuildQueueItem {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	return p.Queue.Items[0]
}

func (p *GuildPlayer) AddPresence(video youtube.YoutubeStream) {
	if p.Discord == nil || p.Discord.State == nil {
		log.Printf("Cannot add presence: Discord session or state is nil")
		return
	}

	presence := &discordgo.Presence{
		Status: "playing",
		Activities: []*discordgo.Activity{
			{
				Name: fmt.Sprintf("🎵 %s", video.Title),
				Type: discordgo.ActivityTypeStreaming,
				State: "Playing",
			},
		},
	}
	
	err := p.Discord.State.PresenceAdd(p.GuildID, presence)
	if err != nil {
		log.Printf("Error adding presence: %v", err)
		return
	}
	p.Presence = presence
}

func (p *GuildPlayer) RemovePresence() {
	if p.Presence != nil && p.Discord != nil && p.Discord.State != nil {
		err := p.Discord.State.PresenceRemove(p.GuildID, p.Presence)
		if err != nil {
			log.Printf("Error removing presence: %v", err)
		}
		p.Presence = nil
	}
}

func (p *GuildPlayer) PopQueue() {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	if len(p.Queue.Items) > 1 {
		p.Queue.Items = p.Queue.Items[1:]
	} else {
		p.Queue.Items = []GuildQueueItem{}
	}
}

func (p *GuildPlayer) Play(video youtube.YoutubeStream) {
	if p.VoiceConnection == nil {
		log.Printf("No voice connection found for guild %s", p.GuildID)
		return
	}

	if !p.VoiceConnection.Ready {
		log.Printf("Voice connection not ready for guild %s", p.GuildID)
		return
	}

	// check if the stream url is still valid
	if time.Unix(video.Expiration, 0).Before(time.Now().Add(time.Minute * 10)) {
		stream, err := youtube.GetVideoStream(youtube.VideoResponse{
			Title: video.Title,
			VideoID: video.VideoID,
		})
		if err != nil {
			log.Printf("Error getting video stream: %s", err)
			return
		}
		video = *stream
	}

	// play the stream
	p.PopQueue() // remove the incoming video from the queue, shift to next if any
	go p.VoiceConnection.Speaking(true)
	defer p.VoiceConnection.Speaking(false)
	p.State = Playing

    ffmpeg := exec.Command("ffmpeg",
        "-i", video.StreamURL,
        "-f", "s16le",          // Output format: signed 16-bit little-endian
        "-ar", "48000",         // Audio rate: 48kHz (required by Discord)
        "-ac", "2",             // Ensure stereo
        "-af", "aresample=48000",  // Simple resampling to maintain quality
        "-loglevel", "error",   
        "pipe:1")

	ffmpegout, err := ffmpeg.StdoutPipe()
	if err != nil {
		log.Printf("Error creating stdout pipe: %v", err)
		return
	}

	err = ffmpeg.Start()
	if err != nil {
		log.Printf("Error starting ffmpeg: %v", err)
		return
	}
    enc, err := opus.NewEncoder(48000, 2, opus.Application(opus.AppAudio))
    if err != nil {
        log.Printf("Error creating opus encoder: %v", err)
        return
    }
    enc.SetComplexity(10)
    enc.SetBitrateToMax()

    frameSize := 960  // 20ms at 48kHz
    buffer := make([]int16, frameSize*2)      // *2 for stereo
    opusBuffer := make([]byte, frameSize*4)   

    for {
        err := binary.Read(ffmpegout, binary.LittleEndian, &buffer)
        if err != nil {
            if err != io.EOF && err != io.ErrUnexpectedEOF {
                log.Printf("Error reading from ffmpeg: %v", err)
            }
            break
        }

		if p.State == Playing {  // Only log once when playback starts
            log.Printf("First few PCM samples: %v", buffer[:10])  // Should show alternating L/R values if stereo
        }

        n, err := enc.Encode(buffer, opusBuffer)
        if err != nil {
            log.Printf("Error encoding to opus: %v", err)
            break
        }

        p.VoiceConnection.OpusSend <- opusBuffer[:n]
    }

	// Give ffmpeg a chance to exit gracefully
	done := make(chan error, 1)
	go func() {
		done <- ffmpeg.Wait()
	}()

	// Wait for ffmpeg to finish or force kill after timeout
	select {
	case <-time.After(3 * time.Second):
		log.Printf("FFmpeg process taking too long to exit, killing...")
		ffmpeg.Process.Kill()
	case err := <-done:
		if err != nil {
			log.Printf("FFmpeg exited with error: %v", err)
		}
	}
}

func (p *GuildPlayer) HandleAdd(event QueueEvent) {
	// join voice channel if not already in one
	if p.VoiceChannelID == nil || p.VoiceConnection == nil {
		voiceState, err := discord.GetMemberVoiceState(event.Item.AddedBy, &p.GuildID)
		if err != nil {
			log.Printf("Error getting voice state: %s", err)
			return
		}

		if voiceState == nil {
			log.Printf("Voice state not found for user %s in guild %s", *event.Item.AddedBy, p.GuildID)
			return
		}

		// session, vc, err := discord.JoinVoiceChannel(p.GuildID, member.VoiceState.ChannelID)
		vc, err := discord.JoinVoiceChannel(p.Discord, p.GuildID, voiceState.ChannelID)
		if err != nil {
			log.Printf("Error joining voice channel: %s", err)
			return
		}

		now := time.Now()

		p.VoiceConnection = vc
		p.VoiceChannelID = &voiceState.ChannelID
		p.VoiceJoinedAt = &now

		p.Play(event.Item.Video)
	}
}

func (p *GuildPlayer) Listen() {
	p.Queue.Listening = true
	log.Printf("Listening for notifications for guild %s", p.GuildID)
	go func() {
		for event := range p.Queue.notifications {
			log.Printf("Received notification: %s", event.Type)
			switch event.Type {
			case EventAdd:
				log.Printf("New song added: %s", event.Item.Video.Title)
				p.HandleAdd(event)
				// Handle new song added
				
			case EventSkip:
				log.Printf("Song skipped: %s", event.Item.Video.Title)
				// Handle song skip - e.g., stop current playback and start next song
				
			case EventClear:
				log.Printf("Queue cleared")
				// Handle queue clear
			}
		}
	}()
}

func (p *GuildPlayer) Stop() {
	p.Queue.Listening = false
	log.Printf("Stopping notifications for guild %s", p.GuildID)
	close(p.Queue.notifications)
}

func (p *GuildPlayer) RegisterMember(member *models.Member) {
	if p.Members == nil {
		p.Members = make(map[string]*models.Member)
	}
	p.Members[member.User.ID] = member
}

func (p *GuildPlayer ) Add(video youtube.YoutubeStream, userID string) {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	item := GuildQueueItem{Video: video, AddedAt: time.Now(), AddedBy: &userID}
	p.Queue.Items = append(p.Queue.Items, item)

	select {
		case p.Queue.notifications <- QueueEvent{Type: EventAdd, Item: item}:
		default:
			log.Printf("Queue notifications channel is full for guild %s", p.GuildID)
	}
}

func (p *GuildPlayer) Remove(index ...int) string {
    p.Queue.Mutex.Lock()
    defer p.Queue.Mutex.Unlock()

    if len(p.Queue.Items) == 0 {
        return ""
    }

    var removed GuildQueueItem
    if len(index) == 0 {
        removed = p.Queue.Items[0]
        p.Queue.Items = p.Queue.Items[1:]
    } else if index[0] > 0 && index[0] <= len(p.Queue.Items) {
        removeIndex := index[0] - 1
        removed = p.Queue.Items[removeIndex]
        p.Queue.Items = append(p.Queue.Items[:removeIndex], p.Queue.Items[removeIndex+1:]...)
    } else {
        return ""
    }

    select {
    case p.Queue.notifications <- QueueEvent{Type: EventSkip, Item: removed}:
    default:
        log.Printf("Queue notifications channel is full for guild %s", p.GuildID)
    }

    return removed.Video.Title
}

func (p *GuildPlayer) Clear() {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	p.Queue.Items = []GuildQueueItem{}
	select {
	case p.Queue.notifications <- QueueEvent{Type: EventClear}:
	default:
		log.Printf("Queue notifications channel is full for guild %s", p.GuildID)
	}
}

func (p *GuildPlayer) IsEmpty() bool {
	p.Queue.Mutex.Lock()
	defer p.Queue.Mutex.Unlock()
	return len(p.Queue.Items) == 0
}






