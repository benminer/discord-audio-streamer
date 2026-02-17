package discord

import (
	"strings"
)

// ParseButtonCustomID extracts action and guildID from button custom ID
// Format: "np:action:guildID"
func ParseButtonCustomID(customID string) (action, guildID string, ok bool) {
	parts := strings.Split(customID, ":")
	if len(parts) != 3 || parts[0] != "np" {
		return "", "", false
	}
	return parts[1], parts[2], true
}
