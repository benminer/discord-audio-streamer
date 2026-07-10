package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/audio"
	"beatbot/discord"
	"beatbot/gemini"
	"beatbot/helpers"
	"beatbot/sentryhelper"

	"github.com/bwmarrin/discordgo"
	"gopkg.in/hraban/opus.v2"
)

func (manager *Manager) handleVolume(ctx context.Context, interaction *Interaction) Response {
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

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateDJResponse(djCtx, "volume", volume)
	hint := manager.Hints.ShowIfApplicable(interaction.GuildID)

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: djResponse + hint,
		},
	}
}

// todo: need to assure the user is in the voice channel
func (manager *Manager) handlePause(ctx context.Context, interaction *Interaction) Response {
	userName := interaction.Member.User.Username
	player := manager.Controller.GetPlayer(interaction.GuildID)

	if !player.Player.IsPlaying() {
		hint := manager.Hints.ShowIfApplicable(interaction.GuildID)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "nothing is playing" + hint,
			},
		}
	}

	go player.Player.Pause(ctx)

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateDJResponse(djCtx, "pause")
	hint := manager.Hints.ShowIfApplicable(interaction.GuildID)

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " paused - " + djResponse + hint,
		},
	}
}

func (manager *Manager) handleResume(ctx context.Context, interaction *Interaction) Response {
	userName := interaction.Member.User.Username
	player := manager.Controller.GetPlayer(interaction.GuildID)
	player.LastActivityAt = time.Now()

	if !player.Player.IsPlaying() {
		hint := manager.Hints.ShowIfApplicable(interaction.GuildID)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "nothing is playing" + hint,
			},
		}
	}

	go player.Player.Resume(ctx)

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateDJResponse(djCtx, "resume")
	hint := manager.Hints.ShowIfApplicable(interaction.GuildID)

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "@" + userName + " - " + djResponse + hint,
		},
	}
}

func (manager *Manager) handleLoop(ctx context.Context, interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	newState := player.ToggleLoop()

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateDJResponse(djCtx, "loop", newState)
	hint := manager.Hints.ShowIfApplicable(interaction.GuildID)

	var emoji, status string
	if newState {
		emoji = "🔂"
		status = "**enabled**"
	} else {
		emoji = "➡️"
		status = "**disabled**"
	}

	msg := fmt.Sprintf("%s Loop mode %s — %s", emoji, status, djResponse)
	if song := player.GetCurrentSong(); song != nil && newState {
		msg += fmt.Sprintf(" (current: **%s**)", *song)
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: msg + hint,
			Flags:   64,
		},
	}
}

func (manager *Manager) handleRadio(ctx context.Context, interaction *Interaction) Response {
	player := manager.Controller.GetPlayer(interaction.GuildID)

	enabled := player.ToggleRadio()

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateDJResponse(djCtx, "radio", enabled)
	hint := manager.Hints.ShowIfApplicable(interaction.GuildID)

	var msg string
	if enabled {
		msg = "📻 Radio mode **enabled** — " + djResponse
	} else {
		msg = "📻 Radio mode **disabled** — " + djResponse
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: msg + hint,
		},
	}
}

func (manager *Manager) handleAnnounce(ctx context.Context, interaction *Interaction) Response {
	guildID := interaction.GuildID
	player := manager.Controller.GetPlayer(guildID)

	// Check if "voice" option was provided
	var voiceOption string
	for _, opt := range interaction.Data.Options {
		if opt.Name == "voice" {
			voiceOption = opt.Value
			break
		}
	}

	if voiceOption != "" {
		// Validate voice name against gemini.AvailableVoices (case-insensitive)
		var matchedVoice string
		for _, v := range gemini.AvailableVoices {
			if strings.EqualFold(v, voiceOption) {
				matchedVoice = v
				break
			}
		}

		if matchedVoice == "" {
			voiceList := strings.Join(gemini.AvailableVoices, ", ")
			return Response{
				Type: 4,
				Data: ResponseData{
					Content: fmt.Sprintf("Unknown voice **%s**. Available voices: %s", voiceOption, voiceList),
					Flags:   64,
				},
			}
		}

		// Save to DB and update in-memory state
		if player.DB != nil {
			if err := player.DB.SetGuildSetting(guildID, "announce_voice", matchedVoice); err != nil {
				log.Errorf("Failed to save announce voice: %v", err)
			}
		}
		player.AnnounceVoice = matchedVoice

		hint := manager.Hints.ShowIfApplicable(guildID)

		return Response{
			Type: 4,
			Data: ResponseData{
				Content: fmt.Sprintf("🎙️ DJ voice set to **%s**", matchedVoice) + hint,
			},
		}
	}

	// Toggle announce enabled
	player.AnnounceEnabled = !player.AnnounceEnabled

	enabledStr := "false"
	if player.AnnounceEnabled {
		enabledStr = "true"
	}

	if player.DB != nil {
		if err := player.DB.SetGuildSetting(guildID, "announce_enabled", enabledStr); err != nil {
			log.Errorf("Failed to save announce setting: %v", err)
		}
	}

	// Generate DJ response with a tight deadline so we never blow Discord's 3s interaction limit
	djCtx, djCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer djCancel()
	djResponse := helpers.GenerateDJResponse(djCtx, "announce", player.AnnounceEnabled)
	hint := manager.Hints.ShowIfApplicable(guildID)

	var msg string
	if player.AnnounceEnabled {
		msg = "🎙️ Voice announcements **enabled** — " + djResponse
	} else {
		msg = "🔇 Voice announcements **disabled** — " + djResponse
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: msg + hint,
		},
	}
}

func (manager *Manager) handleVoiceDemo(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in handleVoiceDemo: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	guildID := interaction.GuildID

	// Check if user is in a voice channel
	voiceState, err := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
	if err != nil {
		log.Errorf("Error getting voice state: %v", err)
		sentryhelper.CaptureException(ctx, err)
		manager.SendError(interaction, "Error getting voice state: "+err.Error(), true)
		return
	}
	if voiceState == nil {
		manager.SendRequest(interaction, "Join a voice channel first!", true)
		return
	}

	player := manager.Controller.GetPlayer(guildID)

	// Join voice if needed
	if player.ShouldJoinVoice(voiceState.ChannelID) {
		if err := player.JoinVoiceChannel(interaction.Member.User.ID); err != nil {
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Error joining voice channel: "+err.Error(), true)
			return
		}
	}

	// Get voice from option, or from guild settings, or default to "Kore"
	var voice string
	for _, opt := range interaction.Data.Options {
		if opt.Name == "voice" {
			voice = opt.Value
			break
		}
	}
	if voice == "" {
		voice = player.AnnounceVoice
	}
	if voice == "" && player.DB != nil {
		if v, _ := player.DB.GetGuildSetting(guildID, "announce_voice"); v != "" {
			voice = v
		}
	}
	if voice == "" {
		voice = "Kore"
	}

	// Generate a short demo script via Gemini
	script := gemini.GenerateRaw(ctx, "Say something cool and brief as a radio DJ in one sentence. No markdown.")
	if script == "" {
		script = "Testing, testing. Your DJ is live and ready to drop some beats."
	}

	// Generate TTS audio
	audioBytes, err := gemini.GenerateTTSAudio(ctx, script, voice, "")
	if err != nil {
		log.Errorf("TTS generation failed: %v", err)
		sentryhelper.CaptureException(ctx, err)
		manager.SendError(interaction, "TTS generation failed: "+err.Error(), true)
		return
	}

	// Convert to Discord format (48kHz stereo) via FFmpeg
	samples, convErr := audio.ConvertTTSToDiscord(audioBytes)
	if convErr != nil {
		log.Errorf("TTS audio conversion failed: %v", convErr)
		sentryhelper.CaptureException(ctx, convErr)
		manager.SendError(interaction, "Audio conversion failed: "+convErr.Error(), true)
		return
	}
	ttsPlayback := &audio.TTSPlayback{Samples: samples}

	// Get the voice connection
	player.VoiceChannelMutex.RLock()
	vc := player.VoiceConnection
	player.VoiceChannelMutex.RUnlock()

	if vc == nil {
		manager.SendError(interaction, "Not connected to a voice channel", true)
		return
	}

	// Create a temporary Opus encoder (matches player.go settings)
	encoder, err := opus.NewEncoder(48000, 2, opus.AppAudio)
	if err != nil {
		log.Errorf("Error creating opus encoder: %v", err)
		sentryhelper.CaptureException(ctx, err)
		manager.SendError(interaction, "Error creating audio encoder", true)
		return
	}
	encoder.SetComplexity(10)
	encoder.SetBitrateToMax()

	// Play TTS frames through the voice connection
	vc.Speaking(true)
	time.Sleep(50 * time.Millisecond)

	frameBuf := make([]int16, 960*2)
	opusBuf := make([]byte, 960*4)

	for ttsPlayback.ReadFrame(frameBuf) {
		encoded, encErr := encoder.Encode(frameBuf, opusBuf)
		if encErr != nil {
			log.Warnf("Error encoding TTS frame: %v", encErr)
			break
		}
		if !trySendOpus(vc, opusBuf[:encoded]) {
			break
		}
	}

	vc.Speaking(false)

	// Send followup message with the voice name and the script text
	manager.SendRequest(interaction, fmt.Sprintf("🎙️ Voice preview (**%s**): *%s*", voice, script), false)
}

// trySendOpus sends opus data to the voice connection, recovering from panics
// caused by sending on a closed channel (voice disconnected mid-playback).
func trySendOpus(vc *discordgo.VoiceConnection, data []byte) (sent bool) {
	defer func() {
		if r := recover(); r != nil {
			sent = false
		}
	}()
	vc.OpusSend <- data
	return true
}
