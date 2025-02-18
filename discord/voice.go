package discord

import (
	"log"
	"os"

	"github.com/bwmarrin/discordgo"
)

func JoinVoiceChannel(guildId string, channelId string) (session *discordgo.Session, vc *discordgo.VoiceConnection, err error) {
	session, err = discordgo.New("Bot " + os.Getenv("DISCORD_BOT_TOKEN"))
	if err != nil {
		log.Printf("Error creating Discord session: %v", err)
		return nil, nil, err
	}

	session.Open()

	session.Identify.Intents = discordgo.IntentsGuildVoiceStates

	session.AddHandler(func(s *discordgo.Session, event *discordgo.VoiceStateUpdate) {
		log.Printf("Voice state update: %v", event)
	})

	vc, err = session.ChannelVoiceJoin(guildId, channelId, false, true)
	if err != nil {
		log.Printf("Error joining voice channel: %v", err)
		return nil, nil, err
	}

	log.Printf("Joined voice channel: %v", vc)

	return session, vc, nil
}

func LeaveVoiceChannel(session *discordgo.Session, vc *discordgo.VoiceConnection) {
	vc.Close()
	session.Close()
}

