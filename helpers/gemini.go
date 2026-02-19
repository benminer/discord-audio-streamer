package helpers

import (
	"context"
	"fmt"

	"beatbot/config"
	"beatbot/gemini"
)

// DJFallbacks are static responses when Gemini is unavailable
var DJFallbacks = map[string]string{
	"clear":       "Queue wiped. Clean slate.",
	"skip":        "On to the next one.",
	"remove":      "That track is gone.",
	"shuffle":     "Shuffled. Fate decides now.",
	"play":        "Adding to the queue.",
	"queue":       "Queued up.",
	"pause":       "Paused.",
	"resume":      "Back to the music.",
	"volume":      "Volume adjusted.",
	"radio":       "Radio mode toggled.",
	"loop":        "Loop mode toggled.",
	"history":     "Here's what we've been vibing to.",
	"leaderboard": "Top tracks incoming.",
	"favorites":   "Your saved tracks.",
	"favorite":    "Saved to favorites.",
	"unfavorite":  "Removed from favorites.",
	"recommend":   "Let me find you something good.",
	"reset":       "Starting fresh.",
	"view":        "Queue incoming.",
	"lyrics":      "Words on the screen.",
	"stop":        "Stopped.",
}

// GenerateDJResponse generates a witty DJ-style response for a command action
func GenerateDJResponse(ctx context.Context, command string, args ...interface{}) string {
	// Check if Gemini is enabled
	if !config.Config.Gemini.Enabled {
		return getFallback(command)
	}

	// Build the prompt based on command
	prompt := buildDJPrompt(command, args)

	// Generate the response — personality is injected by gemini.GenerateRaw
	response := gemini.GenerateRaw(ctx, prompt)
	if response == "" {
		return getFallback(command)
	}

	return response
}

func getFallback(command string) string {
	if fallback, ok := DJFallbacks[command]; ok {
		return fallback
	}
	return "Action complete."
}

func buildDJPrompt(command string, args []interface{}) string {
	switch command {
	case "clear":
		count := 0
		if len(args) > 0 {
			count = args[0].(int)
		}
		if count == 0 {
			return fmt.Sprintf("Write a brief, witty DJ response to someone trying to clear an empty queue. Keep it dry and clever. One sentence.")
		}
		return fmt.Sprintf("Write a brief, witty DJ response to clearing %d tracks from the queue. Keep it dry and clever. One sentence. Reference the count.", count)

	case "skip":
		return "Write a brief, witty DJ response to skipping a song. Keep it cool and brief. One sentence."

	case "remove":
		title := ""
		if len(args) > 0 && args[0] != nil {
			title = args[0].(string)
		}
		if title != "" {
			return fmt.Sprintf("Write a brief DJ response to removing '%s' from the queue. Keep it dry. One sentence.", title)
		}
		return "Write a brief DJ response to removing a song from the queue. One sentence."

	case "shuffle":
		count := 0
		if len(args) > 0 {
			count = args[0].(int)
		}
		if count > 0 {
			return fmt.Sprintf("Write a brief, witty DJ response to shuffling %d tracks. Keep it casual and brief. One sentence.", count)
		}
		return "Write a brief DJ response to shuffling the queue. One sentence."

	case "play", "queue":
		query := ""
		if len(args) > 0 && args[0] != nil {
			query = args[0].(string)
		}
		if query != "" {
			return fmt.Sprintf("Write a brief DJ response to queuing '%s'. Keep it casual. One sentence.", query)
		}
		return "Write a brief DJ response to adding a song to the queue. One sentence."

	case "pause":
		return "Write a brief DJ response to pausing playback. Keep it cool. One sentence."

	case "resume":
		return "Write a brief DJ response to resuming playback. Keep it cool. One sentence."

	case "volume":
		vol := 0
		if len(args) > 0 {
			vol = args[0].(int)
		}
		return fmt.Sprintf("Write a brief DJ response to volume set to %d. Keep it brief. One sentence.", vol)

	case "radio":
		enabled := false
		if len(args) > 0 {
			enabled = args[0].(bool)
		}
		action := "off"
		if enabled {
			action = "on"
		}
		return fmt.Sprintf("Write a brief DJ response to radio mode being turned %s. Keep it brief. One sentence.", action)

	case "loop":
		enabled := false
		if len(args) > 0 {
			enabled = args[0].(bool)
		}
		action := "disabled"
		if enabled {
			action = "enabled"
		}
		return fmt.Sprintf("Write a brief DJ response to loop mode being %s. Keep it brief. One sentence.", action)

	case "history":
		return "Write a brief DJ response showing the recent history. Keep it casual. One sentence."

	case "leaderboard":
		return "Write a brief DJ response showing the top songs. Keep it casual. One sentence."

	case "favorites":
		return "Write a brief DJ response showing the user's favorites. Keep it casual. One sentence."

	case "favorite":
		title := ""
		if len(args) > 0 && args[0] != nil {
			title = args[0].(string)
		}
		if title != "" {
			return fmt.Sprintf("Write a brief DJ response to favoriting '%s'. Keep it brief. One sentence.", title)
		}
		return "Write a brief DJ response to favoriting a song. One sentence."

	case "unfavorite":
		return "Write a brief DJ response to unfavoriting a song. One sentence."

	case "recommend":
		return "Write a brief, witty DJ response to generating a recommendation. Be confident. One sentence."

	case "reset":
		return "Write a brief DJ response to resetting the player. Keep it brief. One sentence."

	case "view":
		count := 0
		if len(args) > 0 {
			count = args[0].(int)
		}
		if count == 0 {
			return "Write a brief DJ response to an empty queue view. Keep it dry. One sentence."
		}
		return fmt.Sprintf("Write a brief DJ response to viewing a queue with %d tracks. Keep it casual. One sentence.", count)

	case "lyrics":
		return "Write a brief DJ response to showing lyrics. Keep it brief. One sentence."

	case "stop":
		return "Write a brief DJ response to stopping playback. Keep it brief. One sentence."

	default:
		return "Write a brief DJ response. Keep it casual. One sentence."
	}
}

// GenerateClearDJResponse generates a DJ response specifically for the /clear command
func GenerateClearDJResponse(ctx context.Context, count int) string {
	if count == 0 {
		return "Nothing to clear!"
	}
	return GenerateDJResponse(ctx, "clear", count)
}
