package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/deezer"
	"beatbot/discord"
	"beatbot/sentryhelper"
	"beatbot/youtube"
)

// handleCharts shows (or queues) Deezer's current global top tracks.
func (manager *Manager) handleCharts(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in handleCharts: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	if !config.Config.Deezer.Enabled {
		manager.SendError(interaction, "Charts aren't enabled on this bot right now.", true)
		return
	}

	// "play" is declared as a STRING option (not BOOLEAN) because InteractionOption.Value
	// is typed as a plain Go string - Discord sends BOOLEAN option values as a raw JSON
	// bool, which fails to unmarshal into that field. The "volume" command follows the
	// same convention for numeric input.
	var playMode bool
	for _, opt := range interaction.Data.Options {
		if opt.Name == "play" {
			playMode = strings.EqualFold(opt.Value, "true")
			break
		}
	}

	chartCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	charts, err := deezer.GetCharts(chartCtx)
	if err != nil {
		log.Errorf("Error fetching Deezer charts: %v", err)
		sentryhelper.CaptureException(ctx, err)
		manager.SendError(interaction, "Couldn't fetch the charts right now: "+err.Error(), true)
		return
	}

	tracks := charts.Tracks.Data
	if len(tracks) == 0 {
		manager.SendRequest(interaction, "No chart data available right now. Try again later.", true)
		return
	}
	if len(tracks) > 10 {
		tracks = tracks[:10]
	}

	if playMode {
		voiceState, err := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
		if err != nil {
			log.Errorf("Error getting voice state: %v", err)
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Error getting voice state: "+err.Error(), true)
			return
		}
		if voiceState == nil {
			manager.SendRequest(interaction, "Join a voice channel first! 🎤", true)
			return
		}

		player := manager.Controller.GetPlayer(interaction.GuildID)
		if player.ShouldJoinVoice(voiceState.ChannelID) {
			if err := player.JoinVoiceChannel(interaction.Member.User.ID); err != nil {
				sentryhelper.CaptureException(ctx, err)
				manager.SendError(interaction, "Error joining voice channel: "+err.Error(), true)
				return
			}
		}

		var queued []string
		for _, t := range tracks {
			query := t.Artist.Name + " - " + t.TitleShort
			results := youtube.Query(ctx, query)
			if len(results) == 0 {
				continue
			}
			player.Add(ctx, results[0], interaction.Member.User.ID, interaction.Token, manager.AppID, nil)
			queued = append(queued, results[0].Title)
		}

		if len(queued) == 0 {
			manager.SendRequest(interaction, "Couldn't find any of the trending tracks on YouTube. Try again later.", true)
			return
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📊 **Queued %d trending tracks:**\n", len(queued)))
		for i, title := range queued {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, title))
		}

		manager.SendFollowup(ctx, interaction, "", sb.String(), false)
		return
	}

	var sb strings.Builder
	sb.WriteString("📊 **What's Trending**\n\n")
	for i, t := range tracks {
		sb.WriteString(fmt.Sprintf("`%2d.` **%s** — %s\n", i+1, t.TitleShort, t.Artist.Name))
	}
	sb.WriteString("\n*Use `/charts play:true` to queue these tracks.*")

	manager.SendFollowup(ctx, interaction, "", sb.String(), false)
}
