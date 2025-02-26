package handlers

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"net/http"
	"os"
	"strconv"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/controller"
	"beatbot/discord"
	"beatbot/gemini"
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
	log.Debugf("Querying and queuing: %+v", interaction.Member.User.ID)
	voiceState, err := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
	if err != nil {
		log.Errorf("Error getting voice state: %v", err)
		sentry.CaptureException(err)
		manager.SendError(interaction, "Error getting voice state: "+err.Error(), true)
		return
	}

	player := manager.Controller.GetPlayer(interaction.GuildID)

	if config.Config.Options.EnforceVoiceChannelEnabled() && voiceState == nil {
		manager.SendFollowup(interaction, "The user is not in a voice channel and trying to play a song", "Hey dummy, join a voice channel first", true)
		return
	}

	shouldJoin := player.VoiceChannelID == nil ||
		player.VoiceConnection == nil ||
		(player.VoiceChannelID != nil &&
			player.IsEmpty() && !player.Player.IsPlaying() &&
			*player.VoiceChannelID != voiceState.ChannelID)

	// join vc if not in one
	// or move to requester's vc if stopped
	if shouldJoin {
		err := player.JoinVoiceChannel(interaction.Member.User.ID)
		if err != nil {
			errStr := err.Error()
			if errStr != "" && errStr == "voice state not found" {
				manager.SendFollowup(interaction, "You gotta join a voice channel first!", "Error joining voice channel: "+errStr, true)
				return
			}
			sentry.CaptureException(err)
			manager.SendError(interaction, "Error joining voice channel: "+errStr, true)
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
			sentry.CaptureException(err)
			manager.SendError(interaction, "Error getting video stream: "+err.Error(), true)
			return
		}

		video = videoResponse
	} else {
		videos := youtube.Query(query)

		if len(videos) == 0 {
			manager.SendFollowup(interaction, "There wasn't anything found for "+query, "No videos found for the given query", true)
			return
		}

		video = videos[0]
	}

	var followUpMessage string
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil

	if firstSongQueued {
		followUpMessage = "**" + video.Title + "** playing soon (also include politely that playback could take a few seconds to start, since it's the first song and needs to load)"
	} else {
		followUpMessage = "**" + video.Title + "** received, loading the song!"
	}

	manager.SendFollowup(interaction, followUpMessage, followUpMessage, false)
	player.Add(video, interaction.Member.User.ID, interaction.Token, manager.AppID)
}

func (manager *Manager) SendRequest(interaction *Interaction, content string, ephemeral bool) {
	payload := map[string]interface{}{
		"content": content,
	}

	if ephemeral {
		payload["flags"] = 64
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Errorf("Error marshalling payload: %v", err)
		return
	}

	resp, err := http.Post(
		"https://discord.com/api/v10/webhooks/"+manager.AppID+"/"+interaction.Token,
		"application/json",
		bytes.NewBuffer(jsonPayload),
	)
	if err != nil {
		log.Errorf("Error sending followup: %v", err)
	}
	defer resp.Body.Close()
}

func (manager *Manager) SendError(interaction *Interaction, content string, ephemeral bool) {
	manager.SendRequest(interaction, content, ephemeral)
}

func (manager *Manager) SendFollowup(interaction *Interaction, content string, backupContent string, ephemeral bool) {
	userName := interaction.Member.User.Username
	toSend := backupContent

	// pass in an empty string to skip the AI generation
	if content != "" {
		genText := gemini.GenerateResponse("User: " + userName + "\nEvent: " + content)
		if genText != "" {
			toSend = genText
		}
	}
	manager.SendRequest(interaction, toSend, ephemeral)
}

func (manager *Manager) ParseInteraction(body []byte) (*Interaction, error) {
	var interaction Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		log.Errorf("Error unmarshalling interaction: %v", err)
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

func (manager *Manager) onHelp(interaction *Interaction) {
	response := gemini.GenerateHelpfulResponse("(user issued the help command, return a nicely formatted help menu)")
	manager.SendRequest(interaction, response, false)
}

func (manager *Manager) handleHelp(interaction *Interaction) Response {
	go manager.onHelp(interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleQueue(interaction *Interaction) Response {
	go manager.QueryAndQueue(interaction)

	return Response{
		Type: 5,
	}
}

func (manager *Manager) onView(interaction *Interaction) {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.IsEmpty() && !player.Player.IsPlaying() && player.CurrentSong == nil {
		manager.SendFollowup(interaction, "The queue is empty and nothing is playing", "The queue is empty and nothing is playing", false)
	}

	formatted_queue := ""
	for i, video := range player.Queue.Items {
		formatted_queue += fmt.Sprintf("%d. %s\n", i+1, video.Video.Title)
	}

	if player.CurrentSong != nil {
		formatted_queue += fmt.Sprintf("\nNow playing: **%s**", *player.CurrentSong)
	}

	manager.SendFollowup(interaction, "", formatted_queue, false)
}

func (manager *Manager) handleView(interaction *Interaction) Response {
	go manager.onView(interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) onSkip(interaction *Interaction) {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if !player.Player.IsPlaying() && player.CurrentSong == nil {
		manager.SendFollowup(interaction, "user tried to skip but nothing is playing", "Nothing to skip", true)
		return
	}

	current := player.CurrentSong
	userName := interaction.Member.User.Username

	go player.Skip()

	next := player.GetNext()

	response := "@" + userName + " skipped **" + *current + "**"
	if next != nil {
		response += "\n\nNow playing **" + next.Video.Title + "**"
	}

	manager.SendFollowup(interaction, response, response, false)
}

func (manager *Manager) handlePurge(interaction *Interaction) {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	go player.Clear()

	manager.SendFollowup(interaction, "Queue purged", "Queue purged", false)
}

func (manager *Manager) handleSkip(interaction *Interaction) Response {
	go manager.onSkip(interaction)
	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleReset(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	go player.Reset(&controller.GuildQueueItemInteraction{
		UserID:           interaction.Member.User.ID,
		InteractionToken: interaction.Token,
		AppID:            manager.AppID,
	})

	return Response{
		Type: 5,
	}
}

func (manager *Manager) handleRemove(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.IsEmpty() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "the queue is empty",
			},
		}
	}

	var index int = 1 // Default to first song if no index provided, .Remove substracts 1
	if len(interaction.Data.Options) > 0 {
		var err error
		index, err = strconv.Atoi(interaction.Data.Options[0].Value)
		if err != nil {
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
	userName := interaction.Member.User.Username

	if removed_title != "" {
		msg = "@" + userName + " removed **" + removed_title + "** from the queue"
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: msg,
		},
	}
}

func (manager *Manager) handleVolume(interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	volume, err := strconv.Atoi(interaction.Data.Options[0].Value)
	if err != nil {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Invalid volume",
			},
		}
	}

	player.Player.SetVolume(volume)

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "Volume set to " + interaction.Data.Options[0].Value,
		},
	}
}

// todo: need to assure the user is in the voice channel
func (manager *Manager) handlePause(interaction *Interaction) Response {
	userName := interaction.Member.User.Username
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if !player.Player.IsPlaying() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "nothing is playing",
			},
		}
	}

	go player.Player.Pause()

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

	if !player.Player.IsPlaying() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "nothing is playing",
			},
		}
	}

	go player.Player.Resume()

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " resumed the current song",
		},
	}
}

func (manager *Manager) HandleInteraction(interaction *Interaction) (response Response) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("Panic in command handling: %v", err)
			response = Response{
				Type: 4,
				Data: ResponseData{
					Content: "An error occurred while processing your command",
					Flags:   64,
				},
			}
		}
	}()

	log.Debugf("Received command: %+v", interaction.Data.Name)

	sentry.CurrentHub().ConfigureScope(func(scope *sentry.Scope) {
		scope.SetContext("interaction", map[string]interface{}{
			"name":     interaction.Data.Name,
			"options":  interaction.Data.Options,
			"guild_id": interaction.GuildID,
			"user_id":  interaction.Member.User.ID,
			"username": interaction.Member.User.Username,
		})
	})

	switch interaction.Data.Name {
	case "ping":
		return manager.handlePing()
	case "help":
		return manager.handleHelp(interaction)
	case "queue", "play":
		return manager.handleQueue(interaction)
	case "view":
		return manager.handleView(interaction)
	case "remove":
		return manager.handleRemove(interaction)
	case "skip":
		return manager.handleSkip(interaction)
	case "pause", "stop":
		return manager.handlePause(interaction)
	case "volume":
		return manager.handleVolume(interaction)
	case "resume":
		return manager.handleResume(interaction)
	case "reset":
		return manager.handleReset(interaction)
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
		log.Errorf("Error decoding public key: %v", err)
		return false
	}

	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		log.Errorf("Error decoding signature: %v", err)
		return false
	}

	message := []byte(timestamp + string(body))
	return ed25519.Verify(pubKeyBytes, message, signatureBytes)
}
