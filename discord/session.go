package discord

import (
	"log"

	"beatbot/config"

	logrus "github.com/sirupsen/logrus"

	"github.com/bwmarrin/discordgo"
)

func NewSession() (*discordgo.Session, error) {
	session, err := discordgo.New("Bot " + config.Config.Discord.BotToken)
	if err != nil {
		log.Fatalf("Error creating Discord session: %v", err)
		return nil, err
	}
	session.Identify.Intents = discordgo.IntentsGuildVoiceStates

	// Enable DAVE E2EE for voice connections
	session.DaveSessionCreate = NewDaveSessionCreate()
	logrus.Info("DAVE E2EE voice encryption enabled")

	session.Open()
	return session, nil
}
