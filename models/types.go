package models

type Member struct {
	User struct {
		ID            string `json:"id"`
		Username      string `json:"username"`
		Avatar        string `json:"avatar"`
		GlobalName    string `json:"global_name"`
		Discriminator string `json:"discriminator"`
		PublicFlags   int    `json:"public_flags"`
		Flags         int    `json:"flags"`
	} `json:"user"`
	Nick       *string  `json:"nick"`
	Roles      []string `json:"roles"`
	JoinedAt   string   `json:"joined_at"`
	Mute       bool     `json:"mute"`
	Deaf       bool     `json:"deaf"`
	VoiceState struct {
		ChannelID string `json:"channel_id"`
	} `json:"voice"`
}

func (m *Member) IsInVoiceChannel() bool {
	return m.VoiceState.ChannelID != ""
}

func (m *Member) GetActiveVoiceChannel() string {
	if m.VoiceState.ChannelID == "" {
		return ""
	}
	return m.VoiceState.ChannelID
}

type GuildSettings struct {
	Tone   string `json:"tone"`
	Volume int    `json:"volume"`
}
