package discord

import (
	"log"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"
)

func JoinVoiceChannel(guildId string, channelId string) {
	session, err := discordgo.New("Bot " + os.Getenv("DISCORD_BOT_TOKEN"))
	if err != nil {
		log.Printf("Error creating Discord session: %v", err)
	}

	session.Open()

	session.Identify.Intents = discordgo.IntentsGuildVoiceStates

	session.AddHandler(func(s *discordgo.Session, event *discordgo.VoiceStateUpdate) {
		log.Printf("Voice state update: %v", event)
	})

	v, err := session.ChannelVoiceJoin(guildId, channelId, false, true)
	if err != nil {
		log.Printf("Error joining voice channel: %v", err)
	}

	log.Printf("Joined voice channel: %v", v)

	time.Sleep(10 * time.Second)

	v.Close()
	log.Printf("Disconnected from voice channel")
	session.Close()
}

