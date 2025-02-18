package controller

import (
	"beatbot/discord"
	"beatbot/models"
	"beatbot/youtube"
	"encoding/binary"
	"io"
	"log"
	"os"
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
	GuildID string
	State GuildPlayerState
	Members map[string]*models.Member
	Queue *GuildQueue
	VoiceChannelID *string
	VoiceJoinedAt *time.Time
	VoiceSession *discordgo.Session
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
	mutex sync.Mutex
}

func NewController() *Controller {
	return &Controller{
		sessions: make(map[string]*GuildPlayer),
	}
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

func (p *GuildPlayer) Play(video youtube.YoutubeStream) {
	if p.VoiceConnection == nil {
		log.Printf("No voice connection found for guild %s", p.GuildID)
		return
	}

	if p.VoiceSession == nil {
		log.Printf("No voice session found for guild %s", p.GuildID)
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
	p.VoiceConnection.Speaking(true)
	defer p.VoiceConnection.Speaking(false)

	// Create ffmpeg command to decode audio and encode to opus
	ffmpeg := exec.Command("ffmpeg",
		"-i", video.StreamURL,        // Input from YouTube URL
		"-f", "s16le",          // Output format: signed 16-bit little-endian
		"-ar", "48000",         // Audio rate: 48kHz (required by Discord)
		"-ac", "2",             // Audio channels: 2 (stereo)
		"-loglevel", "error",   // Only show errors in logs
		"pipe:1")               // Output to stdout

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

	// Create opus encoder
	enc, err := opus.NewEncoder(48000, 2, opus.Application(2048))
	if err != nil {
		log.Printf("Error creating opus encoder: %v", err)
		return
	}

	// Read raw audio data in chunks and encode to opus
	buffer := make([]int16, 960*2) // 960 samples * 2 channels
	opusBuffer := make([]byte, 1000)
	
	for {
		err := binary.Read(ffmpegout, binary.LittleEndian, &buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from ffmpeg: %v", err)
			}
			break
		}

		// Encode audio data to opus
		n, err := enc.Encode(buffer, opusBuffer)
		if err != nil {
			log.Printf("Error encoding to opus: %v", err)
			break
		}

		// Send encoded opus data to Discord
		p.VoiceConnection.OpusSend <- opusBuffer[:n]
	}

	ffmpeg.Process.Kill()
	ffmpeg.Wait()
}

func (p *GuildPlayer) HandleAdd(event QueueEvent) {
	// join voice channel if not already in one
	if p.VoiceChannelID == nil || p.VoiceConnection == nil {
		member := discord.GetMember(event.Item.AddedBy, &p.GuildID)

		if os.Getenv("ENFORCE_VOICE_CHANNEL") == "true" {
			if member == nil {
				log.Printf("Member not found for user %s in guild %s", *event.Item.AddedBy, p.GuildID)
				return
			}

			if member.VoiceState.ChannelID == "" {
				log.Printf("Member %s in guild %s does not have a voice channel", *event.Item.AddedBy, p.GuildID)
				return
			}
		}

		// session, vc, err := discord.JoinVoiceChannel(p.GuildID, member.VoiceState.ChannelID)
		session, vc, err := discord.JoinVoiceChannel(p.GuildID, "1340737363047092296")
		if err != nil {
			log.Printf("Error joining voice channel: %s", err)
			return
		}

		p.VoiceSession = session
		p.VoiceConnection = vc
		p.VoiceChannelID = &member.VoiceState.ChannelID

		p.Play(event.Item.Video)
	}
	// prepare stream

	// check if the stream url is still valid
	// if time.Unix(event.Item.Video.Expiration, 0).Before(time.Now().Add(time.Minute * 10)) {
	// 	stream, err := youtube.GetVideoStream(youtube.VideoResponse{
	// 		Title: event.Item.Video.Title,
	// 		VideoID: event.Item.Video.VideoID,
	// 	})
	// 	if err != nil {
	// 		log.Printf("Error getting video stream: %s", err)
	// 		return
	// 	}
		
	// }
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





