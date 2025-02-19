package youtube

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"beatbot/config"

	"google.golang.org/api/option"
	ytapi "google.golang.org/api/youtube/v3"
)

type VideoResponse struct {
	Title   string `json:"title"`
	VideoID string `json:"video_id"`
}

type YoutubeStream struct {
	StreamURL  string
	Expiration int64
	Title      string
	VideoID    string
}

func ParseYoutubeUrl(_url string) string {
	parsedURL, err := url.Parse(_url)
	if err != nil {
		return ""
	}

	if parsedURL.Host == "www.youtube.com" || parsedURL.Host == "youtube.com" {
		return parsedURL.Query().Get("v")
	}

	return ""
}

func GetVideoByID(videoID string) (VideoResponse, error) {
	api_key := config.Config.Youtube.APIKey

	service, err := ytapi.NewService(context.Background(), option.WithAPIKey(api_key))
	if err != nil {
		return VideoResponse{}, fmt.Errorf("error creating YouTube client: %v", err)
	}

	call := service.Videos.List([]string{"snippet"}).Id(videoID)
	response, err := call.Do()
	if err != nil {
		return VideoResponse{}, fmt.Errorf("error querying YouTube: %v", err)
	}

	if len(response.Items) > 0 {
		return VideoResponse{
			Title:   response.Items[0].Snippet.Title,
			VideoID: videoID,
		}, nil
	}

	return VideoResponse{}, fmt.Errorf("no video found")
}

func Query(query string) []VideoResponse {
	api_key := config.Config.Youtube.APIKey

	service, err := ytapi.NewService(context.Background(), option.WithAPIKey(api_key))
	if err != nil {
		log.Fatalf("Error creating YouTube client: %v", err)
	}

	call := service.Search.List([]string{"snippet"}).
		Q(query + " (official music video|official audio|lyrics|audio|Audio)").
		MaxResults(10).
		Type("video").
		VideoCategoryId("10")

	response, err := call.Do()
	if err != nil {
		log.Fatalf("Error querying YouTube: %v", err)
	}

	videos := make([]VideoResponse, 0)

	for _, item := range response.Items {
		if item.Id.Kind == "youtube#video" {
			videoCall := service.Videos.List([]string{"contentDetails"}).Id(item.Id.VideoId)

			videoResponse, err := videoCall.Do()
			if err != nil {
				log.Printf("Error getting video details: %v", err)
				continue
			}

			if len(videoResponse.Items) > 0 {
				duration := videoResponse.Items[0].ContentDetails.Duration
				minutes := parseDuration(duration)
				if minutes <= 12 {
					videos = append(videos, VideoResponse{
						Title:   html.UnescapeString(item.Snippet.Title),
						VideoID: item.Id.VideoId,
					})
				}
			}
		}
	}

	return videos
}

func GetVideoStream(videoResponse VideoResponse) (*YoutubeStream, error) {
	ytUrl := "https://www.youtube.com/watch?v=" + videoResponse.VideoID
	cmd := exec.Command("yt-dlp",
		"-f", "bestaudio[ext=ogg]/bestaudio", // prefer ogg, but fallback to bestaudio
		"--no-audio-multistreams",
		"-g", // Print URL only
		"--no-warnings",
		ytUrl)

	output, err := cmd.Output()
	if err != nil {
		log.Printf("yt-dlp error: %v", err)
		return nil, fmt.Errorf("yt-dlp error: %v", err)
	}

	streamUrl := strings.TrimSpace(string(output))

	parsedURL, err := url.Parse(streamUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %v", err)
	}

	expiration, err := strconv.ParseInt(parsedURL.Query().Get("expire"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error parsing expiration: %v", err)
	}

	youtubeStream := YoutubeStream{
		StreamURL:  streamUrl,
		Expiration: expiration,
		Title:      videoResponse.Title,
		VideoID:    videoResponse.VideoID,
	}

	return &youtubeStream, nil
}

func parseDuration(duration string) float64 {
	// Remove PT from the duration string
	duration = strings.TrimPrefix(duration, "PT")

	var minutes float64
	if strings.Contains(duration, "H") {
		return 999 // Return large number for videos with hours
	}

	// Parse minutes
	if idx := strings.Index(duration, "M"); idx != -1 {
		m, _ := strconv.ParseFloat(duration[:idx], 64)
		minutes = m
		duration = duration[idx+1:]
	}

	// Parse seconds
	if idx := strings.Index(duration, "S"); idx != -1 {
		s, _ := strconv.ParseFloat(duration[:idx], 64)
		minutes += s / 60
	}

	return minutes
}
