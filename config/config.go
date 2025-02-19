package config

import (
	"os"
)

type ConfigStruct struct {
	Discord DiscordConfig
	NGrok   NGrokConfig
	Options Options
	Youtube YoutubeConfig
}

type DiscordConfig struct {
	BotToken  string
	AppID     string
	PublicKey string
}

type NGrokConfig struct {
	Domain    string
	AuthToken string
}

type YoutubeConfig struct {
	APIKey string
}

type Options struct {
	EnforceVoiceChannel bool
	Port                string
}

func (ngrok *NGrokConfig) IsEnabled() bool {
	return ngrok.Domain != "" && ngrok.AuthToken != ""
}

func (options *Options) EnforceVoiceChannelEnabled() bool {
	return options.EnforceVoiceChannel
}

var Config *ConfigStruct

func NewConfig() {
	config := &ConfigStruct{
		Discord: DiscordConfig{
			BotToken:  os.Getenv("DISCORD_BOT_TOKEN"),
			AppID:     os.Getenv("DISCORD_APP_ID"),
			PublicKey: os.Getenv("DISCORD_PUBLIC_KEY"),
		},
		NGrok: NGrokConfig{
			Domain:    os.Getenv("NGROK_DOMAIN"),
			AuthToken: os.Getenv("NGROK_AUTHTOKEN"),
		},
		Options: Options{
			EnforceVoiceChannel: os.Getenv("ENFORCE_VOICE_CHANNEL") == "true",
			Port:                os.Getenv("PORT"),
		},
		Youtube: YoutubeConfig{
			APIKey: os.Getenv("YOUTUBE_API_KEY"),
		},
	}

	Config = config
}
