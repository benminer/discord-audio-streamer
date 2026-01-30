package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"beatbot/gemini"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

type FollowUpRequest struct {
	Token           string
	AppID           string
	UserID          string
	Content         string
	GenerateContent bool
	Flags           int
}

func buildRequest(request *FollowUpRequest) map[string]interface{} {
	var content string = request.Content

	if request.GenerateContent {
		// Use background context since this is called from async goroutines
		content = gemini.GenerateResponse(context.Background(), request.Content)
		if content == "" {
			content = request.Content
		}
	}

	payload := map[string]interface{}{
		"content": content,
	}

	if request.Flags != 0 {
		payload["flags"] = request.Flags
	}

	return payload
}

func SendFollowup(request *FollowUpRequest) {
	payload := buildRequest(request)

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error marshalling payload: %v", err)
		return
	}

	resp, err := http.Post(
		"https://discord.com/api/v10/webhooks/"+request.AppID+"/"+request.Token,
		"application/json",
		bytes.NewBuffer(jsonPayload),
	)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error sending followup: %v", err)
		return
	}
	defer resp.Body.Close()
}

func UpdateMessage(request *FollowUpRequest) {
	payload := buildRequest(request)

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error marshalling payload: %v", err)
		return
	}

	req, err := http.NewRequest(
		"PATCH",
		"https://discord.com/api/v10/webhooks/"+request.AppID+"/"+request.Token+"/messages/@original",
		bytes.NewBuffer(jsonPayload),
	)

	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error updating message: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sentry.CaptureException(err)
		log.Errorf("Error updating message: %v", err)
		return
	}
	defer resp.Body.Close()
}
