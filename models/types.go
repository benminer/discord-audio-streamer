package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type Member struct {
	User struct {
		ID string `json:"id"`
		Username string `json:"username"`
		Avatar string `json:"avatar"`
		GlobalName string `json:"global_name"`
		Discriminator string `json:"discriminator"`
		PublicFlags int `json:"public_flags"`
		Flags int `json:"flags"`
	} `json:"user"`
	Nick *string `json:"nick"`
	Roles []string `json:"roles"`
	JoinedAt string `json:"joined_at"`
	Mute bool `json:"mute"`
	Deaf bool `json:"deaf"`
	VoiceState struct {
		ChannelID string `json:"channel_id"`
	} `json:"voice"`
} 

func MemberForGuild(guildID string, userID string, botToken string) (*Member, error) {
	req, err := http.NewRequest("GET", 
		fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/%s", guildID, userID),
		nil)

	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bot "+botToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	log.Printf("Response body: %s", string(body))

	var member Member
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&member); err != nil {
		return nil, err
	}

	return &member, nil
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