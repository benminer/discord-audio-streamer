package youtube

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"beatbot/config"

	log "github.com/sirupsen/logrus"

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
		log.Errorf("error creating YouTube client: %v", err)
		return VideoResponse{}, fmt.Errorf("error creating YouTube client: %v", err)
	}

	call := service.Videos.List([]string{"snippet"}).Id(videoID)
	response, err := call.Do()
	if err != nil {
		log.Errorf("error querying YouTube: %v", err)
		return VideoResponse{}, fmt.Errorf("error querying YouTube: %v", err)
	}

	if len(response.Items) > 0 {
		log.Tracef("video found: %v", response.Items[0].Snippet.Title)
		return VideoResponse{
			Title:   response.Items[0].Snippet.Title,
			VideoID: videoID,
		}, nil
	}

	return VideoResponse{}, fmt.Errorf("no video found")
}

func Query(query string) []VideoResponse {
	logger := log.WithFields(log.Fields{"module": "youtube", "function": "Query"})
	api_key := config.Config.Youtube.APIKey

	service, err := ytapi.NewService(context.Background(), option.WithAPIKey(api_key))
	if err != nil {
		logger.Errorf("error creating YouTube client: %v", err)
		return []VideoResponse{}
	}

	call := service.Search.List([]string{"snippet"}).
		Q(query + " (official music video|official audio|lyrics|audio|Audio)").
		MaxResults(10).
		Type("video").
		VideoCategoryId("10")

	response, err := call.Do()
	if err != nil {
		logger.Errorf("error querying YouTube: %v", err)
		return []VideoResponse{}
	}

	videos := make([]VideoResponse, 0)

	for _, item := range response.Items {
		if item.Id.Kind == "youtube#video" {
			videoCall := service.Videos.List([]string{"contentDetails"}).Id(item.Id.VideoId)

			videoResponse, err := videoCall.Do()
			if err != nil {
				logger.Errorf("error getting video details: %v", err)
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

	logger.Tracef("found %d videos", len(videos))
	return videos
}

func GetVideoStream(videoResponse VideoResponse) (*YoutubeStream, error) {
	logger := log.WithFields(log.Fields{"module": "youtube", "video_id": videoResponse.VideoID, "function": "GetVideoStream"})
	var output []byte
	var err error

	ytUrl := "https://www.youtube.com/watch?v=" + videoResponse.VideoID
	logger.Tracef("getting video stream for %s", ytUrl)
	for attempts := 0; attempts < 3; attempts++ {
		cmd := exec.Command("yt-dlp",
			"-f", "bestaudio[ext=ogg]/bestaudio",
			"--no-audio-multistreams",
			"-g",
			"--no-warnings",
			ytUrl)

		output, err = cmd.CombinedOutput()
		if err != nil {
			logger.WithFields(log.Fields{
				"attempt": attempts + 1,
				"error":   err,
				"output":  string(output),
			}).Error("yt-dlp command failed")

			if attempts == 2 {
				return nil, fmt.Errorf("yt-dlp error after 3 attempts: %v, output: %s", err, string(output))
			}
			continue
		}
		break
	}

	streamUrl := strings.TrimSpace(string(output))
	logger.Tracef("streamUrl: %s", streamUrl)
	parsedURL, err := url.Parse(streamUrl)
	if err != nil {
		logger.Errorf("error parsing URL: %v", err)
		return nil, fmt.Errorf("error parsing URL: %v", err)
	}

	expiration, err := strconv.ParseInt(parsedURL.Query().Get("expire"), 10, 64)
	logger.Tracef("expiration: %d", expiration)
	if err != nil {
		logger.Errorf("error parsing expiration: %v", err)
		return nil, fmt.Errorf("error parsing expiration: %v", err)
	}

	return &YoutubeStream{
		StreamURL:  streamUrl,
		Expiration: expiration,
		Title:      videoResponse.Title,
		VideoID:    videoResponse.VideoID,
	}, nil
}

func parseDuration(duration string) float64 {
	duration = strings.TrimPrefix(duration, "PT")

	var minutes float64
	if strings.Contains(duration, "H") {
		return 999
	}

	// parse minutes
	if idx := strings.Index(duration, "M"); idx != -1 {
		m, _ := strconv.ParseFloat(duration[:idx], 64)
		minutes = m
		duration = duration[idx+1:]
	}

	// parse seconds
	if idx := strings.Index(duration, "S"); idx != -1 {
		s, _ := strconv.ParseFloat(duration[:idx], 64)
		minutes += s / 60
	}

	return minutes
}

func TestYoutubeDlpWithOutput() (string, error) {
	cmd := exec.Command("yt-dlp",
		"-f", "bestaudio[ext=ogg]/bestaudio",
		"--no-check-formats",      // Skip format checking which might use ffmpeg
		"--no-check-certificates", // Skip HTTPS certificate validation
		"--verbose",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ")

	output, err := cmd.Output()
	if err != nil {
		log.Errorf("error running yt-dlp: %v", err)
		return "", fmt.Errorf("error running yt-dlp: %v", err)
	}

	return string(output), nil
}
