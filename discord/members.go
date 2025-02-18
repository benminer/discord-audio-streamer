package discord

import (
	"log"
	"os"

	"beatbot/models"
)

func  GetMember(userId *string, guildId *string) *models.Member {
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	if (userId == nil || guildId == nil) {
		log.Printf("User or guild ID is empty")
		return nil
	}

	member, err := models.MemberForGuild(*guildId, *userId, botToken)
	if err != nil {
		log.Printf("Error getting member: %v", err)
		return nil
	}

	return member
}