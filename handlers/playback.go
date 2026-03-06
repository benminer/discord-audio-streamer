package handlers

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"beatbot/helpers"
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
