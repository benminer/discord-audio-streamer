package discord

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"beatbot/controller"
	"beatbot/models"
	"beatbot/youtube"
)

type Response struct {
	Type int `json:"type"`
	Data ResponseData `json:"data"`
}

type ResponseData struct {
	Content string `json:"content"`
	Flags int `json:"flags"`
}

type InteractionOption struct {
	Name string `json:"name"`
	Value string `json:"value"`
}

type InteractionData struct {
    ID   string `json:"id"`
    Name string `json:"name"`
    Type int    `json:"type"`
	Options []InteractionOption `json:"options"`
}

type UserData struct {
	ID string `json:"id"`
	Username string `json:"username"`
	Avatar string `json:"avatar"`
	GlobalName string `json:"global_name"`
}

type MemberData struct {
    User UserData `json:"user"`
    Roles []string `json:"roles"`
    JoinedAt string `json:"joined_at"`
    Nick *string `json:"nick"`
    // Add other member fields as needed
}

type Interaction struct {
	ApplicationID string `json:"application_id"`
    Type    int             `json:"type"`
    Data    InteractionData `json:"data"`
    Token   string         `json:"token"`
	Member  MemberData    `json:"member"`
    Version int            `json:"version"`
	GuildID string `json:"guild_id"`
}

type Options struct {
	EnforceVoiceChannel bool
}

type Client struct {
	AppID string
	PublicKey string
	BotToken string
	Controller *controller.Controller
	Options Options
}

func NewClient(appID string, controller *controller.Controller, options Options) *Client {
	publicKey := os.Getenv("DISCORD_PUBLIC_KEY")
	botToken := os.Getenv("DISCORD_BOT_TOKEN")

	if publicKey == "" || botToken == "" {
		log.Fatal("DISCORD_PUBLIC_KEY and DISCORD_BOT_TOKEN must be set")
		os.Exit(1)
	}

	return &Client{
		AppID: appID,
		PublicKey: publicKey,
		BotToken: botToken,
		Controller: controller,
		Options: options,
	}
}

// TODO: I should probably just load in the members info on interaction
// and cache it temporarily
// could use this to assure permissions, etc.
func (c *Client) HydrateGuildMember(interaction *Interaction) *models.Member {
	userId := interaction.Member.User.ID
	guildId := interaction.GuildID

	if (userId == "" || guildId == "") {
		log.Printf("User or guild ID is empty")
		return nil
	}

	member, err := models.MemberForGuild(guildId, userId, c.BotToken)
	if err != nil {
		log.Printf("Error getting member: %v", err)
		return nil
	}

	c.Controller.GetPlayer(guildId).RegisterMember(member)

	return member
}

func (c *Client) QueryAndQueue(interaction *Interaction) {
	member := c.HydrateGuildMember(interaction)

	if member == nil {
		c.SendFollowup(interaction, "Could not find member", true)
		return
	}

	if c.Options.EnforceVoiceChannel && !member.IsInVoiceChannel() {
		c.SendFollowup(interaction, "Hey dummy, join a voice channel first", true)
		return
	}

	query := interaction.Data.Options[0].Value
	videos := youtube.Query(query)

	if len(videos) == 0 {
		go c.SendFollowup(interaction, "No videos found for the given query", true)
		return
	}

	top_result := videos[0]
	c.SendFollowup(interaction, "Getting streaming url for **"+top_result.Title+"**...", true)
	stream, err := youtube.GetVideoStream(top_result)
	if err != nil {
		go c.SendFollowup(interaction, "Error getting video stream: "+err.Error(), true)
		log.Printf("Error getting video stream: %v", err)
		return
	}
	go c.SendFollowup(interaction, "Added **"+top_result.Title+"** to the queue", false)
	
	guild_player := c.Controller.GetPlayer(interaction.GuildID)
	guild_player.Queue.Add(*stream)

	// Hardcoded for now
	JoinVoiceChannel(interaction.GuildID, "1340737363047092296")
	
	// Todo: check if empty, if so, play
	// we'll need to start up some sort of audio streaming service here, 
	// This could probably live on the guild's player struct
}

func (c *Client) SendFollowup(interaction *Interaction, content string, ephemeral bool) {
	payload := map[string]interface{}{
		"content": content,
	}

	if ephemeral {
		payload["flags"] = 64
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling payload: %v", err)
		return
	}

	resp, err := http.Post(
		"https://discord.com/api/v10/webhooks/" + c.AppID + "/" + interaction.Token,
		"application/json",
		bytes.NewBuffer(jsonPayload),
	)
	if err != nil {
		log.Printf("Error sending followup: %v", err)
	}
	defer resp.Body.Close()
}

func (c *Client) ParseInteraction(body []byte) (*Interaction, error) {
	var interaction Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		log.Printf("Error unmarshalling interaction: %v", err)
		return nil, err
	}

	log.Printf("Parsed interaction: %+v", interaction)
	return &interaction, nil
}

func (c *Client) HandlePing(interaction *Interaction) Response {
	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "Pong! 🏓",
		},
	}
}

func (c *Client) HandleHelp(interaction *Interaction) Response {
	return Response{
		Type: 4,
		Data: ResponseData{
			Content: "**🎵 BeatBot Commands**\n\n" +
				"Here are all the available commands:\n\n" +
				"**`/queue <song>`**\n" +
				"> Add a song to the queue\n" +
				"> Example: `/queue never gonna give you up`\n\n" +
				"**`/play`**\n" +
				"> Start playing the current queue\n\n" +
				"**`/pause`**\n" +
				"> Pause the current song\n\n" +
				"**`/skip`**\n" +
				"> Skip to the next song, or remove a specific song in the queue\n\n" +
				"**`/stop`**\n" +
				"> Stop the current song\n\n" +
				"**`/purge`**\n" +
				"> Purge the queue\n\n" +
				"*🤖 BeatBot v1.0*",
		},
	}
}

func (c *Client) HandleQueue(interaction *Interaction) Response {
    go c.QueryAndQueue(interaction)
    
    return Response{
        Type: 5,
        Data: ResponseData{
            Content: "🔍 Searching for \"**" + interaction.Data.Options[0].Value + "**\"...",
        },
    }
}

func (c *Client) HandleView(interaction *Interaction) Response {
	guild_player := c.Controller.GetPlayer(interaction.GuildID)
	queue := guild_player.Queue

	if queue.IsEmpty() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "The queue is empty",
			},
		}
	}

	formatted_queue := ""
	for i, video := range queue.Items {
		formatted_queue += fmt.Sprintf("%d. %s\n", i+1, video.Title)
	}

	log.Printf("Formatted queue: %s", formatted_queue)
	
	return Response{
		Type: 4,
		Data: ResponseData{
			Content: formatted_queue,
		},
	}
}

func (c *Client) HandleRemove(interaction *Interaction) Response {
	guild_player := c.Controller.GetPlayer(interaction.GuildID)

	if guild_player.Queue.IsEmpty() {
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "The queue is empty",
			},
		}
	}

	var index int = 1  // Default to first song if no index provided, .Remove substracts 1
	if len(interaction.Data.Options) > 0 {
		var err error
		index, err = strconv.Atoi(interaction.Data.Options[0].Value)
		if err != nil {
			log.Printf("Error converting to int: %v", err)
			return Response{
				Type: 4,
				Data: ResponseData{
					Content: "Invalid index",
				},
			}
		}
	}

	removed_title := guild_player.Queue.Remove(index)
	msg := "Removed the song from the queue"
	if removed_title != "" {
		msg = "Removed **" + removed_title + "** from the queue"
	}

	return Response{
		Type: 4,
		Data: ResponseData{
			Content: msg,
		},
	}
}

func (c *Client) HandleInteraction(interaction *Interaction) (response Response) {
	// Defer a recover function that will catch any panics
	defer func() {
		if err := recover(); err != nil {
			log.Printf("Panic in command handling: %v", err)
			response = Response{
				Type: 4,
				Data: ResponseData{
					Content: "An error occurred while processing your command",
					Flags:   64, // Ephemeral message
				},
			}
		}
	}()

	log.Printf("Received command: %+v", interaction.Data.Name)
	switch interaction.Data.Name{
	case "ping":
		return c.HandlePing(interaction)
	case "help":
		return c.HandleHelp(interaction)
	case "queue":
		return c.HandleQueue(interaction)
	case "view":
		return c.HandleView(interaction)
	case "skip":
		return c.HandleRemove(interaction)
	// case "play":
	// 	return c.HandlePlay(interaction)
	// case "pause":
	// 	return c.HandlePause(interaction)
	default:
		log.Printf("Unknown interaction type: %d", interaction.Type)
		return Response{
			Type: 4,
			Data: ResponseData{
				Content: "Sorry, I don't know how to handle this type of interaction",
				Flags:   64,
			},
		}
	}
}

func (c *Client) VerifyDiscordRequest(signature, timestamp string, body []byte) bool {
	pubKeyBytes, err := hex.DecodeString(c.PublicKey)
	if err != nil {
		log.Printf("Error decoding public key: %v", err)
		return false
	}

	signatureBytes, err := hex.DecodeString(signature)
	if err != nil {
		log.Printf("Error decoding signature: %v", err)
		return false
	}

	message := []byte(timestamp + string(body))
	return ed25519.Verify(pubKeyBytes, message, signatureBytes)
}