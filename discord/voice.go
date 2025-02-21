package discord

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"

	"github.com/bwmarrin/discordgo"
)

func JoinVoiceChannel(session *discordgo.Session, guildId string, channelId string) (vc *discordgo.VoiceConnection, err error) {
	vc, err = session.ChannelVoiceJoin(guildId, channelId, false, true)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error joining voice channel: %v", err)
		return nil, err
	}

	// Add connection check with timeout
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		if vc.Ready && vc.OpusSend != nil {
			return vc, nil
		}
		time.Sleep(time.Second)
	}

	// If we couldn't establish a proper connection, clean up and return error
	vc.Close()
	sentry.CaptureMessage(fmt.Sprintf("failed to establish stable voice connection after %d seconds", maxRetries))
	return nil, fmt.Errorf("failed to establish stable voice connection after %d seconds", maxRetries)
}

func LeaveVoiceChannel(vc *discordgo.VoiceConnection) {
	vc.Close()
}

type DiscordErrorResponse struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type VoiceStateUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Avatar        string `json:"avatar"`
	Discriminator string `json:"discriminator"`
}

type VoiceStateMember struct {
	User     VoiceStateUser `json:"user"`
	Nick     *string        `json:"nick"`
	Roles    []string       `json:"roles"`
	JoinedAt string         `json:"joined_at"`
}

type VoiceState struct {
	ChannelID  string           `json:"channel_id"`
	GuildID    string           `json:"guild_id"`
	UserID     string           `json:"user_id"`
	Member     VoiceStateMember `json:"member"`
	SessionID  string           `json:"session_id"`
	Deaf       bool             `json:"deaf"`
	Mute       bool             `json:"mute"`
	SelfDeaf   bool             `json:"self_deaf"`
	SelfMute   bool             `json:"self_mute"`
	SelfVideo  bool             `json:"self_video"`
	SelfStream bool             `json:"self_stream"`
	Suppress   bool             `json:"suppress"`
}

func MakeRequestWithRetries(client *http.Client, req *http.Request) (*http.Response, error) {
	logger := log.WithFields(log.Fields{
		"module": "discord.voice",
		"method": req.Method,
		"url":    req.URL.String(),
	})

	retries := 3

	for i := 0; i < retries; i++ {
		resp, err := client.Do(req)
		if err != nil {
			logger.Errorf("error making request: %v", err)
			time.Sleep(time.Millisecond * time.Duration(100*(i+1)))
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("failed to make request after %d retries", retries)
}

func GetMemberVoiceState(userId *string, guildId *string) (*VoiceState, error) {
	if userId == nil || guildId == nil {
		return nil, fmt.Errorf("user or guild ID is empty")
	}

	log.Tracef("getting voice state for user %s in guild %s", *userId, *guildId)

	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf("https://discord.com/api/v10/guilds/%s/voice-states/%s", *guildId, *userId), nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bot %s", config.Config.Discord.BotToken))

	resp, err := MakeRequestWithRetries(client, req)
	if err != nil {
		return nil, fmt.Errorf("error getting voice state: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp DiscordErrorResponse
		if err := json.Unmarshal(body, &errorResp); err != nil {
			return nil, fmt.Errorf("error parsing error response: %v", err)
		}

		// If user is not in voice channel, return nil without error
		if errorResp.Code == 10065 {
			return nil, nil
		}

		return nil, fmt.Errorf("discord API error: %s (code: %d)", errorResp.Message, errorResp.Code)
	}

	var voiceState VoiceState
	if err := json.Unmarshal(body, &voiceState); err != nil {
		return nil, fmt.Errorf("error parsing voice state: %v", err)
	}

	return &voiceState, nil
}
