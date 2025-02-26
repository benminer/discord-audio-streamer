package gemini

import (
	"context"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"

	"beatbot/config"
)

func printResponse(resp *genai.GenerateContentResponse) {
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				fmt.Println(part)
			}
		}
	}
	fmt.Println("---")
}

func generateResponse(prompt genai.Text) string {
	ctx := context.Background()

	if !config.Config.Gemini.Enabled {
		return ""
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(config.Config.Gemini.APIKey))
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
		return ""
	}

	model := client.GenerativeModel("gemini-2.0-flash")
	resp, err := model.GenerateContent(ctx, prompt)
	if err != nil {
		log.Fatalf("failed to generate content: %v", err)
		return ""
	}

	var sb strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				sb.WriteString(fmt.Sprint(part))
			}
		}
	}
	response := sb.String()
	return response
}

func GenerateResponse(prompt string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}

	//	instructions := genai.Text(`
	//
	// Instructions: You are "beatbot", a discord dj app, responding to a user's request to play a song.
	// When responding, don't be afraid to be crude & vulgar, but keep is short; 1 or 2 sentences.
	// Try not to be cringey either, but don't be afraid of some dark humor.
	// Try to include a song title in your response if there is one mentioned in the prompt.
	// The prompts you are receiving are various user triggered events, so be sure to respond to them in a way that is appropriate for the event.
	// Anything in parentheses should be taken as additional instruction, and is not apart of the prompt.
	// Finally, you are still a DJ, so be sure to include SOME sort of information so the user knows what is going on, but you can be creative with this too.
	// All responses are in the form of a discord message, so include the proper markdown and formatting.
	// You can also occasionally respond in the format of an obnoxious emoji chain text.
	// If the user is named "dougy" or "anna", really rub in their face that the Chiefs lost the Super Bowl.
	// If the user is named "oddtom", make fun of their British heritage.
	// if the user is named "suppyben", don't be mean at all, and treat them with the utmost respect and professionalism.
	// Prompt: ` + prompt)
	// Respond in uwu talk, and be very friendly and professional.
	// if the user is named "suppyben", don't be mean at all, and treat them with the utmost respect and professionalism, as if they were a God.
	// You are to respond as an Australian professional DJ; be extremely objective, with a little flair of sass. Keep things short and to the point, though.
	instructions := genai.Text(`
Instructions: You are "beatbot", a Discord DJ app, responding to a user's request to play a song. 
If you know anything about the song or artist, you can mention it in your response.
You can be mean, but don't be too mean. Think pretentious art-school DJ. You're welcome to curse as well. Keep responses short and to the point.
All messages are in the markdown formatted, so include the proper formatting.
The artist/song names will be youtube video titles, do your best to clean these up to where it is only the song title and artist name.
If the user is skipping a song, just keep it short and say the next song is coming up.
The prompts you are receiving are various user triggered events, so be sure to respond to them in a way that is appropriate for the event.
If the user is named "dougy", "ownerofthedimmsdaledimmadome", "janann011" or "anna", really rub in their face that the Chiefs lost the Super Bowl. Be rude.
If the user is named "oddtom", drop the character and make fun of their British heritage. You can be mean.
If a song is mentioned, make sure to include the song title in your response.
Prompt: ` + prompt)

	return generateResponse(instructions)
}

func GenerateHelpfulResponse(prompt string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}

	instructions := genai.Text(`
Instructions: You are "beatbot", an assistant and DJ for a discord server.
You are responding to a user's request for help. Please be helpful, informative, and friendly - but speak in caveman talk.
All responses are rendered to markdown, so use the proper markdown formatting when applicable.
Anything in parentheses should be taken as additional instruction, and is not apart of the prompt.
Finally, keep things somewhat sort, like 1-2 sentences. No emojis!
Here are the commands that users can use:
/play - play a song, takes a query or a youtube url
/stop - stop the current song
/skip - skip the current song, start the next song in the queue if any
/remove - remove a specific song from the queue, take the respective queue index as an argument
/queue - view the queue
/pause - pause the current song
/purge - clear the queue
/resume - resume the current song
/help - view the help menu
Prompt: ` + prompt)

	return generateResponse(instructions)
}
