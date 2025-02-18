package youtube

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"google.golang.org/api/option"
	ytapi "google.golang.org/api/youtube/v3"
)

type VideoResponse struct {
	Title string `json:"title"`
	VideoID string `json:"video_id"`
}

type YoutubeStream struct {
	StreamURL string
	Expiration int64
	Title string
	VideoID string
}

func Query(query string) []VideoResponse {
	api_key := os.Getenv("YOUTUBE_API_KEY")

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
			videos = append(videos, VideoResponse{
				Title: html.UnescapeString(item.Snippet.Title),
				VideoID: item.Id.VideoId,
			})
		}
	}

	return videos
}

func GetVideoStream(videoResponse VideoResponse) (*YoutubeStream, error) {
	// Use yt-dlp to get the best audio format URL
	// TODO: will need to be sure to keep this in $PATH
	// just use home directory for now
	ytUrl := "https://www.youtube.com/watch?v=" + videoResponse.VideoID
	cmd := exec.Command("yt-dlp",
		"-f", "bestaudio+best",  // we just want audio
		"--no-audio-multistreams", // don't try to use multistreams
		"-g",               // Print URL only
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
		StreamURL: streamUrl,
		Expiration: expiration,
		Title: videoResponse.Title,
		VideoID: videoResponse.VideoID,
	}

	return &youtubeStream, nil
}