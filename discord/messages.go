package discord

import (
	"bytes"
	"encoding/json"
	"net/http"

	sentry "github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
)

type FollowUpRequest struct {
	Token   string
	AppID   string
	UserID  string
	Content string
	Flags   int
}

func SendFollowup(request *FollowUpRequest) {
	payload := map[string]interface{}{
		"content": request.Content,
	}

	if request.Flags != 0 {
		payload["flags"] = request.Flags
	}

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
