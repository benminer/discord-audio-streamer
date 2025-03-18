package config

import (
	"os"
)

type ConfigStruct struct {
	Discord  DiscordConfig
	NGrok    NGrokConfig
	Options  Options
	Youtube  YoutubeConfig
	Gemini   GeminiConfig
	Spotify  SpotifyConfig
	Database DatabaseConfig
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

type GeminiConfig struct {
	Enabled bool
	APIKey  string
}

type SpotifyConfig struct {
	ClientID     string
	ClientSecret string
	Enabled      bool
}

type DatabaseConfig struct {
	Path    string
	Enabled bool
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
		Gemini: GeminiConfig{
			Enabled: os.Getenv("GEMINI_ENABLED") == "true",
			APIKey:  os.Getenv("GEMINI_API_KEY"),
		},
		Spotify: SpotifyConfig{
			ClientID:     os.Getenv("SPOTIFY_CLIENT_ID"),
			ClientSecret: os.Getenv("SPOTIFY_CLIENT_SECRET"),
			Enabled:      os.Getenv("SPOTIFY_ENABLED") == "true",
		},
		Database: DatabaseConfig{
			Path:    os.Getenv("DATABASE_PATH"),
			Enabled: os.Getenv("DATABASE_ENABLED") == "true",
		},
	}

	Config = config
}
