package config

import (
	"os"
	"strconv"
)

type ConfigStruct struct {
	Discord DiscordConfig
	NGrok   NGrokConfig
	Options Options
	Youtube YoutubeConfig
	Gemini  GeminiConfig
	Spotify SpotifyConfig
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
	ClientID      string
	ClientSecret  string
	Enabled       bool
	PlaylistLimit int
}

type Options struct {
	EnforceVoiceChannel bool
	Port                string
	IdleTimeoutMinutes  int
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
			IdleTimeoutMinutes:  getIdleTimeout(),
		},
		Youtube: YoutubeConfig{
			APIKey: os.Getenv("YOUTUBE_API_KEY"),
		},
		Gemini: GeminiConfig{
			Enabled: os.Getenv("GEMINI_ENABLED") == "true",
			APIKey:  os.Getenv("GEMINI_API_KEY"),
		},
		Spotify: SpotifyConfig{
			ClientID:      os.Getenv("SPOTIFY_CLIENT_ID"),
			ClientSecret:  os.Getenv("SPOTIFY_CLIENT_SECRET"),
			Enabled:       os.Getenv("SPOTIFY_ENABLED") == "true",
			PlaylistLimit: getPlaylistLimit(),
		},
	}

	Config = config
}

func getIdleTimeout() int {
	timeoutStr := os.Getenv("IDLE_TIMEOUT_MINUTES")
	if timeoutStr == "" {
		return 20
	}
	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil || timeout <= 0 {
		return 20
	}
	return timeout
}

func getPlaylistLimit() int {
	limitStr := os.Getenv("SPOTIFY_PLAYLIST_LIMIT")
	if limitStr == "" {
		return 10
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		return 10
	}
	if limit > 50 {
		return 50 // Cap at 50 for API and performance reasons
	}
	return limit
}
