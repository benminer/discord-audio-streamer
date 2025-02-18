package controller

import (
	"beatbot/models"
	"beatbot/youtube"
	"log"
	"sync"
	"time"
)

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
}

type GuildQueue struct {
	Items []youtube.YoutubeStream
	Mutex sync.Mutex
}

type Controller struct {
	// This is a map of guild_id to the player for that guild
	sessions map[string]*GuildPlayer
	mutex sync.Mutex
}

func NewController() *Controller {
	return &Controller{
		sessions: make(map[string]*GuildPlayer),
	}
}

func (p *GuildPlayer) RegisterMember(member *models.Member) {
	if p.Members == nil {
		p.Members = make(map[string]*models.Member)
	}
	p.Members[member.User.ID] = member
}

func (c *Controller) GetPlayer(guildID string) *GuildPlayer {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if session, ok := c.sessions[guildID]; ok {
		log.Printf("Found existing player for guild %s", guildID)
		return session
	}

	session := &GuildPlayer{
		GuildID: guildID,
		State: Stopped,
		Queue: &GuildQueue{},
	}
	c.sessions[guildID] = session
	log.Printf("Created new player for guild %s", guildID)
	return session
}

func (q *GuildQueue) Add(video youtube.YoutubeStream) {
	q.Mutex.Lock()
	defer q.Mutex.Unlock()
	q.Items = append(q.Items, video)
}

func (q *GuildQueue) Remove(index ...int) string {
	q.Mutex.Lock()
	defer q.Mutex.Unlock()

	if len(q.Items) == 0 {
		return ""
	}

	// If no index is provided, remove the first item
	if len(index) == 0 {
		removed := q.Items[0]
		q.Items = q.Items[1:]
		return removed.Title
	}

	// If an index is provided, check if it's valid
	if index[0] <= 0 || index[0] > len(q.Items) {
		return ""
	}

	removeIndex := index[0] - 1
	removed := q.Items[removeIndex]
	q.Items = append(q.Items[:removeIndex], q.Items[removeIndex+1:]...)
	return removed.Title
}

func (q *GuildQueue) Clear() {
	q.Mutex.Lock()
	defer q.Mutex.Unlock()
	q.Items = []youtube.YoutubeStream{}
}

func (q *GuildQueue) IsEmpty() bool {
	q.Mutex.Lock()
	defer q.Mutex.Unlock()
	return len(q.Items) == 0
}





