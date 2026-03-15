package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"beatbot/config"
	"beatbot/gemini"

	"github.com/bwmarrin/discordgo"
	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

// ErrMissingPermissions is returned when Discord responds with 403/code 50013
type ErrMissingPermissions struct {
	OriginalError error
}

func (e *ErrMissingPermissions) Error() string {
	return e.OriginalError.Error()
}

func (e *ErrMissingPermissions) Unwrap() error {
	return e.OriginalError
}

// IsMissingPermissions checks if an error is a Discord 50013 Missing Permissions error
func IsMissingPermissions(err error) bool {
	if err == nil {
		return false
	}
	var permErr *ErrMissingPermissions
	return errors.As(err, &permErr)
}

// classifyError wraps the error as ErrMissingPermissions if the response body contains code 50013
func classifyError(baseErr error, responseBody []byte) error {
	var discordErr DiscordErrorResponse
	if json.Unmarshal(responseBody, &discordErr) == nil && discordErr.Code == 50013 {
		return &ErrMissingPermissions{OriginalError: baseErr}
	}
	return baseErr
}

type FollowUpRequest struct {
	Token           string
	AppID           string
	UserID          string
	Content         string
	GenerateContent bool
	Flags           int
}

func buildRequest(request *FollowUpRequest) map[string]interface{} {
	var content string = request.Content

	if request.GenerateContent {
		// Use background context since this is called from async goroutines
		content = gemini.GenerateResponse(context.Background(), request.Content)
		if content == "" {
			content = request.Content
		}
	}

	payload := map[string]interface{}{
		"content": content,
	}

	if request.Flags != 0 {
		payload["flags"] = request.Flags
	}

	return payload
}

func SendFollowup(request *FollowUpRequest) {
	payload := buildRequest(request)

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error marshalling payload: %v", err)
		return
	}

	resp, err := http.Post(
		"https://discord.com/api/v10/webhooks/"+request.AppID+"/"+request.Token,
		"application/json",
		bytes.NewBuffer(jsonPayload),
	)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error sending followup: %v", err)
		return
	}
	defer resp.Body.Close()
}

func UpdateMessage(request *FollowUpRequest) {
	payload := buildRequest(request)

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error marshalling payload: %v", err)
		return
	}

	req, err := http.NewRequest(
		"PATCH",
		"https://discord.com/api/v10/webhooks/"+request.AppID+"/"+request.Token+"/messages/@original",
		bytes.NewBuffer(jsonPayload),
	)

	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error updating message: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error updating message: %v", err)
		return
	}
	defer resp.Body.Close()
}

// SendChannelMessage sends a new message to a channel using bot token
// Returns the created message for storing the message ID
func SendChannelMessage(channelID, content string, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) (*discordgo.Message, error) {
	payload := map[string]interface{}{}

	if content != "" {
		payload["content"] = content
	}

	if embed != nil {
		payload["embeds"] = []*discordgo.MessageEmbed{embed}
	}

	if components != nil {
		payload["components"] = components
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages", channelID)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bot %s", config.Config.Discord.BotToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sentry.CaptureException(err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		baseErr := fmt.Errorf("failed to send message: %s - %s", resp.Status, string(body))
		log.Error(baseErr)
		return nil, classifyError(baseErr, body)
	}

	var message discordgo.Message
	if err := json.NewDecoder(resp.Body).Decode(&message); err != nil {
		sentry.CaptureException(err)
		return nil, err
	}

	return &message, nil
}

// EditChannelMessage updates an existing message in a channel using bot token
// Used for updating now-playing cards without the 15-minute webhook token limit
func EditChannelMessage(channelID, messageID string, content string, embed *discordgo.MessageEmbed, components []discordgo.MessageComponent) error {
	payload := map[string]interface{}{}

	if content != "" {
		payload["content"] = content
	}

	if embed != nil {
		payload["embeds"] = []*discordgo.MessageEmbed{embed}
	}

	if components != nil {
		payload["components"] = components
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error marshalling payload: %v", err)
		return err
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/messages/%s", channelID, messageID)

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		sentry.CaptureException(err)
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bot %s", config.Config.Discord.BotToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		baseErr := fmt.Errorf("failed to edit message: %s - %s", resp.Status, string(body))
		log.Error(baseErr)
		return classifyError(baseErr, body)
	}

	return nil
}

// BotInviteURL returns the OAuth2 authorize URL for reinstalling the bot with updated permissions
func BotInviteURL() string {
	return fmt.Sprintf("https://discord.com/oauth2/authorize?client_id=%s", config.Config.Discord.AppID)
}

// SendPermissionErrorFallback sends a plain-text message explaining the bot needs to be
// reinstalled with updated permissions. If this message also fails, it just logs the error.
func SendPermissionErrorFallback(channelID string) {
	msg := fmt.Sprintf(
		"⚠️ I couldn't send the now-playing card because I'm missing permissions in this channel.\n"+
			"A server admin needs to reinstall the bot to grant updated permissions:\n%s",
		BotInviteURL(),
	)
	_, err := SendChannelMessage(channelID, msg, nil, nil)
	if err != nil {
		log.Warnf("Failed to send permission error fallback message: %v", err)
	}
}
