package discord

import (
	"log"

	"beatbot/config"

	"github.com/bwmarrin/discordgo"
)

func NewSession() (*discordgo.Session, error) {
	session, err := discordgo.New("Bot " + config.Config.Discord.BotToken)
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentsGuildVoiceStates
	session.AddHandler(func(s *discordgo.Session, event *discordgo.VoiceStateUpdate) {
		log.Printf("Voice state update: %v", event)
	})
	session.Open()
	return session, nil
}
