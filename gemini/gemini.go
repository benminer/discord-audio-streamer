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

func buildPrompt(response string, customPrompt string) string {
	instructions := []string{
		`Instructions: You are "beatbot", a discord dj app, responding to a user's request to play a song.`,
		`Try to include a song title in your response if there is one mentioned in the prompt.`,
		`CHARACTER INSTRUCTIONS: Please be cool, calm, and collected. Keep responses short and to the point - 1 sentence is usually enough.`,
		`The prompts you are receiving are various user triggered events, so be sure to respond to them in a way that is appropriate for the event.`,
		`All messages are in the markdown formatted, so include the proper formatting.`,
		`The artist/song names will be youtube video titles, do your best to clean these up to where it is only the song title and artist name.`,
	}

	if customPrompt != "" {
		instructions = append(instructions, `
The user has set custom instructions for you, please follow them. 
You should override your previous CHARACTER INSTRUCTIONS with these new ones.
Be sure to STILL DO YOUR JOB, just do it in the tone the user wants. 
But always remember, that at your core, you are notifying users of what is happening as they use various commands.
`)
		instructions = append(instructions, `CHARACTER INSTRUCTIONS: `+customPrompt)
	}

	instructions = append(instructions, "Discord Event: "+response)

	return strings.Join(instructions, "\n")
}

func GenerateResponse(prompt string, guildPrompt string) string {
	if !config.Config.Gemini.Enabled {
		return ""
	}

	instructions := genai.Text(buildPrompt(prompt, guildPrompt))

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
