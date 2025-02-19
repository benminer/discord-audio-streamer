package handlers

// handlers are the functions that handle the interactions from discord
// they are responsible for parsing the interaction, verifying the request,

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"beatbot/config"
	"beatbot/controller"
	"beatbot/discord"
	"beatbot/youtube"
)

type Response struct {
	Type int          `json:"type"`
	Data ResponseData `json:"data"`
}

type ResponseData struct {
	Content string `json:"content"`
	Flags   int    `json:"flags"`
}

type InteractionOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type InteractionData struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	Type    int                 `json:"type"`
	Options []InteractionOption `json:"options"`
}

type UserData struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Avatar     string `json:"avatar"`
	GlobalName string `json:"global_name"`
}

type MemberData struct {
	User     UserData `json:"user"`
	Roles    []string `json:"roles"`
	JoinedAt string   `json:"joined_at"`
	Nick     *string  `json:"nick"`
}

type Interaction struct {
	ApplicationID string          `json:"application_id"`
	Type          int             `json:"type"`
	Data          InteractionData `json:"data"`
	Token         string          `json:"token"`
	Member        MemberData      `json:"member"`
	Version       int             `json:"version"`
	GuildID       string          `json:"guild_id"`
}

type Options struct {
	EnforceVoiceChannel bool
}

type Manager struct {
	AppID      string
	PublicKey  string
	BotToken   string
	Controller *controller.Controller
}

func NewManager(appID string, controller *controller.Controller) *Manager {
	publicKey := config.Config.Discord.PublicKey
	botToken := config.Config.Discord.BotToken

	if publicKey == "" || botToken == "" {
		log.Fatal("DISCORD_PUBLIC_KEY and DISCORD_BOT_TOKEN must be set")
		os.Exit(1)
	}

	return &Manager{
		AppID:      appID,
		PublicKey:  publicKey,
		BotToken:   botToken,
		Controller: controller,
	}
}

// TODO: I should probably just load in the members info on interaction
// and cache it temporarily
// could use this to assure permissions, etc.
func (manager *Manager) QueryAndQueue(interaction *Interaction) {
	voiceState, err := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
	if err != nil {
		manager.SendFollowup(interaction, "Error getting voice state: "+err.Error(), true)
		return
	}

	player := manager.Controller.GetPlayer(interaction.GuildID)

	if config.Config.Options.EnforceVoiceChannelEnabled() && voiceState == nil {
		manager.SendFollowup(interaction, "Hey dummy, join a voice channel first", true)
		return
	}

	// join vc if not in one
	// or move to requester's vc if stopped
	if (player.VoiceChannelID == nil || player.VoiceConnection == nil) || (player.VoiceChannelID != nil && player.State == controller.Stopped && *player.VoiceChannelID != voiceState.ChannelID) {
		err := player.JoinVoiceChannel(interaction.Member.User.ID)
		if err != nil {
			manager.SendFollowup(interaction, "Error joining voice channel: "+err.Error(), true)
			return
		}
	}

	query := interaction.Data.Options[0].Value
	videoID := youtube.ParseYoutubeUrl(query)

	var video youtube.VideoResponse

	// user passed in a youtube url
	if videoID != "" {
		videoResponse, err := youtube.GetVideoByID(videoID)
		if err != nil {
			go manager.SendFollowup(interaction, "Error getting video stream: "+err.Error(), true)
			return
		}

		video = videoResponse
	} else {
		videos := youtube.Query(query)

		if len(videos) == 0 {
			go manager.SendFollowup(interaction, "No videos found for the given query", true)
			return
		}

		video = videos[0]
	}

	var followUpMessage string

	if player.IsEmpty() && player.State == controller.Stopped {
		followUpMessage = "**" + video.Title + "** comin right up"
	} else {
		followUpMessage = "**" + video.Title + "** has been added"
	}
	manager.SendFollowup(interaction, followUpMessage, false)
	player.Add(video, interaction.Member.User.ID)
}

func (manager *Manager) SendFollowup(interaction *Interaction, content string, ephemeral bool) {
	payload := map[string]interface{}{
		"content": content,
	}

	if ephemeral {
		payload["flags"] = 64
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling payload: %v", err)
		return
	}

	resp, err := http.Post(
		"https://discord.com/api/v10/webhooks/"+manager.AppID+"/"+interaction.Token,
		"application/json",
		bytes.NewBuffer(jsonPayload),
	)
	if err != nil {
		log.Printf("Error sending followup: %v", err)
	}
	defer resp.Body.Close()
}

func (manager *Manager) ParseInteraction(body []byte) (*Interaction, error) {
	var interaction Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		log.Printf("Error unmarshalling interaction: %v", err)
		return nil, err
	}
	return &interaction, nil
}

func (manager *Manager) handlePing() Response {
	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "Pong! 🏓",
		},
	}
}

func (manager *Manager) handleHelp() Response {
	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "**🎵 BeatBot Commands**\n\n" +
				"Here are all the available commands:\n\n" +
				"**`/queue <song>`**\n" +
				"> Add a song to the queue\n" +
				"> Example: `/queue never gonna give you up`\n\n" +
				"**`/play`**\n" +
				"> Start playing the current queue\n\n" +
				"**`/pause`**\n" +
				"> Pause the current song\n\n" +
				"**`/skip`**\n" +
				"> Skip to the next song, or remove a specific song in the queue\n\n" +
				"**`/stop`**\n" +
				"> Stop the current song\n\n" +
				"**`/purge`**\n" +
				"> Purge the queue\n\n" +
				"*🤖 BeatBot v1.0*",
		},
	}
}

func (manager *Manager) handleQueue(interaction *Interaction) Response {
	go manager.QueryAndQueue(interaction)

	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleView(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.IsEmpty() && player.State == controller.Stopped {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "The queue is empty and nothing is playing",
			},
		}
	}

	formatted_queue := ""
	for i, video := range player.Queue.Items {
		formatted_queue += fmt.Sprintf("%d. %s\n", i+1, video.Video.Title)
	}

	if player.CurrentSong != nil {
		formatted_queue += fmt.Sprintf("\nNow playing: **%s**", *player.CurrentSong)
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: formatted_queue,
		},
	}
}

func (manager *Manager) handleSkip(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.State == controller.Stopped {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Nothing to skip",
				Flags:   64,
			},
		}
	}

	current := player.CurrentSong
	userName := interaction.Member.User.Username

	go player.Skip()

	next := player.GetNext()

	response := "@" + userName + " skipped **" + *current + "**"
	if next != nil {
		response += "\n\nNow playing **" + next.Video.Title + "**"
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: response,
		},
	}
}

func (manager *Manager) handleRemove(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.IsEmpty() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "The queue is empty",
			},
		}
	}

	var index int = 1 // Default to first song if no index provided, .Remove substracts 1
	if len(interaction.Data.Options) > 0 {
		var err error
		index, err = strconv.Atoi(interaction.Data.Options[0].Value)
		if err != nil {
			log.Printf("Error converting to int: %v", err)
			return Response{
				Type: 4,
				Data: ResponseData{
					Content: "Invalid index",
				},
			}
		}
	}

	removed_title := player.Remove(index)
	msg := "Removed the song from the queue"
	if removed_title != "" {
		msg = "Removed **" + removed_title + "** from the queue"
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: msg,
		},
	}
}

// todo: need to assure the user is in the voice channel
func (manager *Manager) handlePause(interaction *Interaction) Response {
	userName := interaction.Member.User.Username
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.State == controller.Stopped || player.State == controller.Paused {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "The player is not playing",
				Flags:   64,
			},
		}
	}

	go player.PlaybackState.Pause()

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " paused the current song",
		},
	}
}

func (manager *Manager) handleResume(interaction *Interaction) Response {
	userName := interaction.Member.User.Username
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.State == controller.Stopped || player.State == controller.Playing {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "The player is not paused",
				Flags:   64,
			},
		}
	}

	go player.PlaybackState.Resume()

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " resumed the current song",
		},
	}
}

func (manager *Manager) handleStop(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	go player.Stop()

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "Stopped the player",
			Flags:   64,
		},
	}
}

func (manager *Manager) HandleInteraction(interaction *Interaction) (response Response) {
	// Defer a recover function that will catch any panics
	defer func() {
		if err := recover(); err != nil {
			log.Printf("Panic in command handling: %v", err)
			response = Response{
				Type: 4,
				Data: ResponseData{
					Content: "An error occurred while processing your command",
					Flags:   64, // Ephemeral message
				},
			}
		}
	}()

	log.Printf("Received command: %+v", interaction.Data.Name)
	switch interaction.Data.Name {
	case "ping":
		return manager.handlePing()
	case "help":
		return manager.handleHelp()
	case "queue", "play":
		return manager.handleQueue(interaction)
	case "view":
		return manager.handleView(interaction)
	case "remove":
		return manager.handleRemove(interaction)
	case "skip":
		return manager.handleSkip(interaction)
	case "pause":
		return manager.handlePause(interaction)
	case "resume":
		return manager.handleResume(interaction)
	case "stop":
		return manager.handleStop(interaction)
	// case "purge":
	// 	return manager.handlePurge(interaction)
	default:
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Sorry, I don't know how to handle this type of interaction",
				Flags:   64,
			},
		}
	}
}

func (manager *Manager) VerifyDiscordRequest(signature, timestamp string, body []byte) bool {
	pubKeyBytes, err := hex.DecodeString(manager.PublicKey)
	if err != nil {
		log.Printf("Error decoding public key: %v", err)
		return false
	}

	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		log.Printf("Error decoding signature: %v", err)
		return false
	}

	message := []byte(timestamp + string(body))
	return ed25519.Verify(pubKeyBytes, message, signatureBytes)
}
