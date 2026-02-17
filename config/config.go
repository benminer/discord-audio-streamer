package config

import (
	"os"
	"strconv"
)

type ConfigStruct struct {
	Discord    DiscordConfig
	Tunnel     TunnelConfig
	Options    Options
	Youtube    YoutubeConfig
	Gemini     GeminiConfig
	Spotify    SpotifyConfig
}

type DiscordConfig struct {
	BotToken  string
	AppID     string
	PublicKey string
}

type TunnelConfig struct {
	CloudflareTunnelURL string
}

type YoutubeConfig struct {
	APIKey        string
	PlaylistLimit int
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
	AudioBitrate        int // Audio bitrate in bps (e.g., 96000 for 96 kbps)
}

func (t *TunnelConfig) IsCloudflare() bool {
	return t.CloudflareTunnelURL != ""
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
		Tunnel: TunnelConfig{
			CloudflareTunnelURL: os.Getenv("CLOUDFLARE_TUNNEL_URL"),
		},
		Options: Options{
			EnforceVoiceChannel: os.Getenv("ENFORCE_VOICE_CHANNEL") == "true",
			Port:                os.Getenv("PORT"),
			IdleTimeoutMinutes:  getIdleTimeout(),
			AudioBitrate:        getAudioBitrate(),
		},
		Youtube: YoutubeConfig{
			APIKey:        os.Getenv("YOUTUBE_API_KEY"),
			PlaylistLimit: getYouTubePlaylistLimit(),
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

func getYouTubePlaylistLimit() int {
	limitStr := os.Getenv("YOUTUBE_PLAYLIST_LIMIT")
	if limitStr == "" {
		return 15
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		return 15
	}
	if limit > 50 {
		return 50 // Cap at 50 (YouTube API max per page)
	}
	return limit
}

func getAudioBitrate() int {
	bitrateStr := os.Getenv("AUDIO_BITRATE")
	if bitrateStr == "" {
		return 128000 // Default to 128 kbps - max for regular voice channels
	}
	bitrate, err := strconv.Atoi(bitrateStr)
	if err != nil || bitrate <= 0 {
		return 128000
	}
	// Discord supports 8 kbps to 512 kbps for Opus
	// Practical ranges: 8-128 kbps (voice), up to 384 kbps (stage/boost)
	if bitrate < 8000 {
		return 8000 // Minimum 8 kbps
	}
	if bitrate > 512000 {
		return 512000 // Maximum 512 kbps (Discord Opus limit)
	}
	return bitrate
}
