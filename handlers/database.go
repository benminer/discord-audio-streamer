package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"

	"beatbot/config"
	"beatbot/discord"
	"beatbot/gemini"
	"beatbot/sentryhelper"
	"beatbot/youtube"
)

func (manager *Manager) handleHistory(interaction *Interaction) Response {
	db := manager.Controller.GetDB()
	if db == nil {
		return Response{Type: 4, Data: ResponseData{Content: "Database is not available.", Flags: 64}}
	}

	limit := parseLimitOption(interaction)
	records, err := db.GetHistory(interaction.GuildID, limit)
	if err != nil {
		log.Errorf("Error fetching history: %v", err)
		return Response{Type: 4, Data: ResponseData{Content: "Failed to fetch history.", Flags: 64}}
	}

	if len(records) == 0 {
		return Response{Type: 4, Data: ResponseData{Content: "🎵 No songs played yet!"}}
	}

	var sb strings.Builder
	sb.WriteString("🎵 **Recently Played**\n\n")
	for i, r := range records {
		requester := r.RequestedByUsername
		if requester == "" {
			// Try to fetch username from cache or Discord API
			if r.RequestedByUserID != "" {
				requester = db.GetOrFetchUsername(interaction.GuildID, r.RequestedByUserID)
			} else {
				requester = "Unknown"
			}
		}
		sb.WriteString(fmt.Sprintf("**%d.** %s\n　　↳ requested by **%s** · %s\n", i+1, r.Title, requester, formatRelativeTime(r.PlayedAt)))
	}

	content := sb.String()
	if len(content) > 1900 {
		content = content[:1900]
		if idx := strings.LastIndex(content, "\n"); idx > 0 {
			content = content[:idx]
		}
		content += "\n..."
	}

	return Response{Type: 4, Data: ResponseData{Content: content}}
}

func (manager *Manager) handleLeaderboard(interaction *Interaction) Response {
	db := manager.Controller.GetDB()
	if db == nil {
		return Response{Type: 4, Data: ResponseData{Content: "Database is not available.", Flags: 64}}
	}

	limit := parseLimitOption(interaction)
	records, err := db.GetMostPlayed(interaction.GuildID, limit)
	if err != nil {
		log.Errorf("Error fetching leaderboard: %v", err)
		return Response{Type: 4, Data: ResponseData{Content: "Failed to fetch leaderboard.", Flags: 64}}
	}

	if len(records) == 0 {
		return Response{Type: 4, Data: ResponseData{Content: "🏆 No songs played yet!"}}
	}

	medals := []string{"🥇", "🥈", "🥉"}
	var sb strings.Builder
	sb.WriteString("🏆 **Most Played Songs**\n\n")
	for i, r := range records {
		prefix := fmt.Sprintf("**%d.**", i+1)
		if i < 3 {
			prefix = medals[i]
		}
		plays := "play"
		if r.PlayCount != 1 {
			plays = "plays"
		}
		sb.WriteString(fmt.Sprintf("%s %s\n　　↳ **%d** %s · last played %s\n", prefix, r.Title, r.PlayCount, plays, formatRelativeTime(r.LastPlayed)))
	}

	content := sb.String()
	if len(content) > 1900 {
		content = content[:1900]
		if idx := strings.LastIndex(content, "\n"); idx > 0 {
			content = content[:idx]
		}
		content += "\n..."
	}

	return Response{Type: 4, Data: ResponseData{Content: content}}
}

func (manager *Manager) handleFavorite(interaction *Interaction) Response {
	db := manager.Controller.GetDB()
	if db == nil {
		return Response{Type: 4, Data: ResponseData{Content: "Database is not available.", Flags: 64}}
	}

	player := manager.Controller.GetPlayer(interaction.GuildID)
	// Capture both atomically-safe snapshots before using either.
	currentSong := player.GetCurrentSong()
	currentItem := player.GetCurrentItem()
	if currentSong == nil || currentItem == nil {
		return Response{Type: 4, Data: ResponseData{Content: "Nothing is currently playing.", Flags: 64}}
	}

	userID := interaction.Member.User.ID
	song := currentItem

	if db.IsFavorite(userID, interaction.GuildID, song.Video.VideoID) {
		return Response{Type: 4, Data: ResponseData{
			Content: fmt.Sprintf("**%s** is already in your favorites.", song.Video.Title),
			Flags:   64,
		}}
	}

	videoURL := "https://www.youtube.com/watch?v=" + song.Video.VideoID
	if err := db.AddFavorite(userID, interaction.GuildID, song.Video.VideoID, song.Video.Title, videoURL); err != nil {
		log.Errorf("Error adding favorite: %v", err)
		return Response{Type: 4, Data: ResponseData{Content: "Failed to save favorite. Try again.", Flags: 64}}
	}

	return Response{Type: 4, Data: ResponseData{
		Content: fmt.Sprintf("❤️ Added **%s** to your favorites.", song.Video.Title),
		Flags:   64,
	}}
}

func (manager *Manager) handleFavorites(interaction *Interaction) Response {
	db := manager.Controller.GetDB()
	if db == nil {
		return Response{Type: 4, Data: ResponseData{Content: "Database is not available.", Flags: 64}}
	}

	userID := interaction.Member.User.ID
	records, err := db.GetFavorites(userID, interaction.GuildID, 25)
	if err != nil {
		log.Errorf("Error fetching favorites: %v", err)
		return Response{Type: 4, Data: ResponseData{Content: "Failed to fetch favorites.", Flags: 64}}
	}

	if len(records) == 0 {
		return Response{Type: 4, Data: ResponseData{
			Content: "You haven't saved any favorites yet. Use `/favorite` while a song is playing!",
			Flags:   64,
		}}
	}

	var sb strings.Builder
	sb.WriteString("❤️ **Your Favorites**\n\n")
	for i, r := range records {
		sb.WriteString(fmt.Sprintf("`%d.` [%s](%s)\n", i+1, r.Title, r.URL))
	}

	return Response{Type: 4, Data: ResponseData{Content: sb.String()}}
}

func (manager *Manager) handleUnfavorite(interaction *Interaction) Response {
	db := manager.Controller.GetDB()
	if db == nil {
		return Response{Type: 4, Data: ResponseData{Content: "Database is not available.", Flags: 64}}
	}

	userID := interaction.Member.User.ID

	var songNumber int
	for _, opt := range interaction.Data.Options {
		if opt.Name == "song_number" {
			n, err := strconv.Atoi(opt.Value)
			if err != nil || n < 1 {
				return Response{Type: 4, Data: ResponseData{Content: "Invalid song number.", Flags: 64}}
			}
			songNumber = n
			break
		}
	}
	if songNumber == 0 {
		return Response{Type: 4, Data: ResponseData{Content: "Please provide a song number from `/favorites`.", Flags: 64}}
	}

	title, err := db.RemoveFavoriteByIndex(userID, interaction.GuildID, songNumber)
	if err != nil {
		return Response{Type: 4, Data: ResponseData{Content: "That number isn't in your favorites list.", Flags: 64}}
	}

	return Response{Type: 4, Data: ResponseData{
		Content: fmt.Sprintf("Removed **%s** from your favorites.", title),
		Flags:   64,
	}}
}

func (manager *Manager) handleRecommend(ctx context.Context, transaction *sentry.Span, interaction *Interaction) {
	defer func() {
		if err := recover(); err != nil {
			sentryhelper.CaptureException(ctx, fmt.Errorf("panic in handleRecommend: %v", err))
			transaction.Status = sentry.SpanStatusInternalError
		}
		transaction.Finish()
	}()

	if !config.Config.Gemini.Enabled {
		manager.SendFollowup(ctx, interaction, "", "AI recommendations require Gemini to be enabled.", true)
		return
	}

	db := manager.Controller.GetDB()
	if db == nil {
		manager.SendFollowup(ctx, interaction, "", "No database available. Play some songs first to build listening history!", true)
		return
	}

	history, err := db.GetHistory(interaction.GuildID, 10)
	if err != nil {
		log.Errorf("Error fetching history for recommend: %v", err)
		sentryhelper.CaptureException(ctx, err)
		manager.SendFollowup(ctx, interaction, "", "Failed to fetch listening history.", true)
		return
	}

	log.Infof("Recommend: fetched %d history records for guild %s", len(history), interaction.GuildID)

	if len(history) < 3 {
		manager.SendFollowup(ctx, interaction, "", "Need at least 3 songs in history to make a smart recommendation. Play some more first! 🎵", true)
		return
	}

	var songTitles []string
	historyVideoIDs := make(map[string]bool)
	for _, r := range history {
		songTitles = append(songTitles, r.Title)
		if r.VideoID != "" {
			historyVideoIDs[r.VideoID] = true
		}
	}

	query := gemini.GenerateSongRecommendation(ctx, songTitles)
	if query == "" {
		log.Warnf("Recommend: Gemini returned empty query despite %d history records for guild %s", len(history), interaction.GuildID)
		manager.SendFollowup(ctx, interaction, "", "Couldn't generate a recommendation right now. Try again in a moment! 🤖", true)
		return
	}

	log.Infof("Recommend: Gemini query='%s' for guild %s", query, interaction.GuildID)

	// Voice check like in other handlers
	voiceState, err := discord.GetMemberVoiceState(&interaction.Member.User.ID, &interaction.GuildID)
	if err != nil {
		log.Errorf("Error getting voice state: %v", err)
		sentryhelper.CaptureException(ctx, err)
		manager.SendError(interaction, "Error getting voice state: "+err.Error(), true)
		return
	}

	if voiceState == nil {
		manager.SendFollowup(ctx, interaction, "user not in voice channel", "Join a voice channel first! 🎤", true)
		return
	}

	player := manager.Controller.GetPlayer(interaction.GuildID)

	if player.ShouldJoinVoice(voiceState.ChannelID) {
		err := player.JoinVoiceChannel(interaction.Member.User.ID)
		if err != nil {
			errStr := err.Error()
			if errStr == "voice state not found" {
				manager.SendFollowup(ctx, interaction, "", "You gotta join a voice channel first!", true)
				return
			}
			sentryhelper.CaptureException(ctx, err)
			manager.SendError(interaction, "Error joining voice channel: "+errStr, true)
			return
		}
	}

	// Search YouTube
	videos := youtube.Query(ctx, query)
	log.Infof("Recommend: YouTube returned %d results for query='%s' guild=%s", len(videos), query, interaction.GuildID)
	if len(videos) == 0 {
		manager.SendFollowup(ctx, interaction, "", "No suitable tracks found for this recommendation. Try again soon! 🔍", true)
		return
	}

	// Pick first result not already in history
	var video youtube.VideoResponse
	picked := false
	for _, v := range videos {
		if !historyVideoIDs[v.VideoID] {
			video = v
			picked = true
			break
		}
	}
	if !picked {
		// All results are in history — just take the top result rather than failing
		log.Warnf("Recommend: all YouTube results already in history, using top result anyway for guild %s", interaction.GuildID)
		video = videos[0]
	}
	log.Debugf("Recommend selected video: %s (ID: %s)", video.Title, video.VideoID)

	// Determine if first song
	firstSongQueued := player.IsEmpty() && !player.Player.IsPlaying() && player.GetCurrentSong() == nil

	var backupMsg string
	if firstSongQueued {
		backupMsg = fmt.Sprintf("🎵 **Now spinning** your AI DJ pick: **%s**\\n\\n(First song - give it a sec to load!)", video.Title)
	} else {
		backupMsg = fmt.Sprintf("🎵 **Queued up** the AI DJ recommendation: **%s**\\n\\nGood vibes incoming! ✨", video.Title)
	}

	// Queue it
	player.Add(ctx, video, interaction.Member.User.ID, interaction.Token, manager.AppID, nil)

	// DJ personality response
	aiPrompt := fmt.Sprintf(`User %s used /recommend.

AI DJ queued **%s** based on recent listening history.

Announce it like a cool DJ - excited, conversational, with music nerd vibe.`,
		interaction.Member.User.Username, video.Title)

	manager.SendFollowup(ctx, interaction, aiPrompt, backupMsg, false)
}
