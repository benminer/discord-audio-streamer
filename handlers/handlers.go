package handlers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/controller"
	"beatbot/gemini"
	"beatbot/sentryhelper"
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

// StringOrInt is a custom type that can unmarshal from either a string or number in JSON
type StringOrInt string

func (s *StringOrInt) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as a string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = StringOrInt(str)
		return nil
	}

	// If that fails, try to unmarshal as a number
	var num int64
	if err := json.Unmarshal(data, &num); err == nil {
		*s = StringOrInt(fmt.Sprintf("%d", num))
		return nil
	}

	return fmt.Errorf("cannot unmarshal %s into StringOrInt", string(data))
}

type InteractionData struct {
	ID            StringOrInt         `json:"id"`
	Name          string              `json:"name"`
	Type          int                 `json:"type"`
	Options       []InteractionOption `json:"options"`
	CustomID      string              `json:"custom_id"`
	ComponentType int                 `json:"component_type"`
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
	ChannelID     string          `json:"channel_id"`
}

type Options struct {
	EnforceVoiceChannel bool
}

type Manager struct {
	AppID      string
	PublicKey  string
	BotToken   string
	Controller *controller.Controller
	Hints      *Hints
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
		Hints:      NewHints(),
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

func (manager *Manager) ParseInteraction(body []byte) (*Interaction, error) {
	var interaction Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		log.Errorf("Error unmarshalling interaction: %v", err)
		return nil, err
	}
	return &interaction, nil
}

func (manager *Manager) HandleInteraction(interaction *Interaction) (response Response) {
	// Handle Message Component interactions (button clicks) - Type 3
	if interaction.Type == 3 {
		return manager.handleMessageComponent(interaction)
	}

	// Create transaction with cloned hub for scope isolation (breadcrumbs per-command)
	ctx, transaction := sentryhelper.StartCommandTransaction(
		context.Background(),
		interaction.Data.Name,
		interaction.GuildID,
		interaction.Member.User.ID,
	)

	// For sync responses (Type: 4), finish transaction when handler returns.
	// For async responses (Type: 5), the goroutine will finish the transaction.
	finishTransaction := true
	defer func() {
		if finishTransaction {
			transaction.Finish()
		}
	}()

	defer func() {
		if err := recover(); err != nil {
			log.Errorf("Panic in command handling: %v", err)
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in command %s: %v", interaction.Data.Name, err))
			transaction.Status = sentry.SpanStatusInternalError
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

	// Configure scope on the cloned hub (isolated to this command)
	sentryhelper.ConfigureScope(ctx, func(scope *sentry.Scope) {
		scope.SetUser(sentry.User{
			ID:       interaction.Member.User.ID,
			Username: interaction.Member.User.Username,
		})
		scope.SetContext("interaction", map[string]interface{}{
			"name":     interaction.Data.Name,
			"options":  interaction.Data.Options,
			"guild_id": interaction.GuildID,
			"user_id":  interaction.Member.User.ID,
			"username": interaction.Member.User.Username,
		})
	})

	// Always track the last text channel so we can send messages (e.g. radio announcements)
	if interaction.GuildID != "" && interaction.ChannelID != "" {
		player := manager.Controller.GetPlayer(interaction.GuildID)
		player.SetLastTextChannelID(interaction.ChannelID)
	}

	switch interaction.Data.Name {
	case "ping":
		return manager.handlePing()
	case "help":
		finishTransaction = false // goroutine will finish
		return manager.handleHelp(ctx, transaction, interaction)
	case "queue", "play":
		finishTransaction = false // goroutine will finish
		return manager.handleQueue(ctx, transaction, interaction)
	case "view":
		finishTransaction = false // goroutine will finish
		return manager.handleView(ctx, transaction, interaction)
	case "remove":
		return manager.handleRemove(ctx, interaction)
	case "clear":
		return manager.handleClear(ctx, interaction)
	case "skip":
		finishTransaction = false // goroutine will finish
		return manager.handleSkip(ctx, transaction, interaction)
	case "pause", "stop":
		return manager.handlePause(ctx, interaction)
	case "volume":
		return manager.handleVolume(ctx, interaction)
	case "resume":
		return manager.handleResume(ctx, interaction)
	case "reset":
		finishTransaction = false // goroutine will finish
		return manager.handleReset(ctx, transaction, interaction)
	case "shuffle":
		return manager.handleShuffle(ctx, interaction)
	case "radio":
		return manager.handleRadio(ctx, interaction)
	case "loop":
		return manager.handleLoop(ctx, interaction)
	case "history":
		return manager.handleHistory(interaction)
	case "leaderboard":
		return manager.handleLeaderboard(interaction)
	case "topsongs":
		finishTransaction = false
		go manager.handleTopSongs(ctx, transaction, interaction)
		return Response{Type: 5}

	case "lyrics":
		finishTransaction = false
		go manager.onLyrics(ctx, transaction, interaction)
		return Response{Type: 5}
	case "recommend":
		finishTransaction = false
		go manager.handleRecommend(ctx, transaction, interaction)
		return Response{Type: 5}
	case "favorite":
		return manager.handleFavorite(interaction)
	case "favorites":
		return manager.handleFavorites(interaction)
	case "unfavorite":
		return manager.handleUnfavorite(interaction)
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

func (manager *Manager) SendFollowup(ctx context.Context, interaction *Interaction, content string, backupContent string, ephemeral bool) {
	userName := interaction.Member.User.Username
	toSend := backupContent

	// pass in an empty string to skip the AI generation
	if content != "" {
		genText := gemini.GenerateResponse(ctx, "User: "+userName+"\nEvent: "+content)
		if genText != "" {
			toSend = genText
		}
	}
	manager.SendRequest(interaction, toSend, ephemeral)
}

func formatRelativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func parseLimitOption(interaction *Interaction) int {
	limit := 10
	for _, opt := range interaction.Data.Options {
		if opt.Name == "limit" {
			if v, err := strconv.Atoi(opt.Value); err == nil && v >= 1 && v <= 25 {
				limit = v
			}
		}
	}
	return limit
}
