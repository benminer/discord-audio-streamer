package applemusic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	log "github.com/sirupsen/logrus"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// scrapeTrackInfo fetches Apple Music page and extracts track metadata
func scrapeTrackInfo(ctx context.Context, country, albumID, trackID string) (*TrackInfo, error) {
	url := fmt.Sprintf("https://music.apple.com/%s/album/%s?i=%s", country, albumID, trackID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Set realistic User-Agent to avoid blocks
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	log.Tracef("Fetching Apple Music page: %s", url)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Try JSON-LD first (most reliable)
	trackInfo, err := extractFromJSONLD(doc)
	if err == nil {
		log.Debugf("Extracted track info from JSON-LD: %s by %v", trackInfo.Title, trackInfo.Artists)
		return trackInfo, nil
	}

	log.Debugf("JSON-LD extraction failed (%v), trying Open Graph fallback", err)

	// Fallback to Open Graph tags
	trackInfo, err = extractFromOpenGraph(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	log.Debugf("Extracted track info from Open Graph: %s by %v", trackInfo.Title, trackInfo.Artists)
	return trackInfo, nil
}

// extractFromJSONLD parses JSON-LD structured data
func extractFromJSONLD(doc *goquery.Document) (*TrackInfo, error) {
	var trackInfo *TrackInfo

	doc.Find("script[type='application/ld+json']").EachWithBreak(func(i int, s *goquery.Selection) bool {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(s.Text()), &data); err != nil {
			log.Tracef("Failed to parse JSON-LD block %d: %v", i, err)
			return true // Continue to next script tag
		}

		// Check if this is a MusicRecording type
		if typeVal, ok := data["@type"].(string); !ok || typeVal != "MusicRecording" {
			return true // Continue to next script tag
		}

		// Extract track title
		title := getString(data, "name")
		if title == "" {
			return true
		}

		trackInfo = &TrackInfo{
			Title: title,
		}

		// Extract artist(s)
		if artistData, ok := data["byArtist"].(map[string]interface{}); ok {
			artistName := getString(artistData, "name")
			if artistName != "" {
				trackInfo.Artists = []string{artistName}
			}
		} else if artistArray, ok := data["byArtist"].([]interface{}); ok {
			// Handle multiple artists
			artists := []string{}
			for _, a := range artistArray {
				if artistMap, ok := a.(map[string]interface{}); ok {
					if name := getString(artistMap, "name"); name != "" {
						artists = append(artists, name)
					}
				}
			}
			if len(artists) > 0 {
				trackInfo.Artists = artists
			}
		}

		// Extract album
		if albumData, ok := data["inAlbum"].(map[string]interface{}); ok {
			trackInfo.Album = getString(albumData, "name")
		}

		return false // Found what we need, stop iteration
	})

	if trackInfo == nil || trackInfo.Title == "" {
		return nil, errors.New("no JSON-LD MusicRecording data found")
	}

	if len(trackInfo.Artists) == 0 {
		return nil, errors.New("no artist data found in JSON-LD")
	}

	return trackInfo, nil
}

// extractFromOpenGraph extracts metadata from Open Graph meta tags
func extractFromOpenGraph(doc *goquery.Document) (*TrackInfo, error) {
	title, _ := doc.Find("meta[property='og:title']").Attr("content")
	if title == "" {
		// Try alternative meta tag
		title, _ = doc.Find("meta[name='twitter:title']").Attr("content")
	}

	// Try to find artist from various possible meta tags
	var artist string
	artist, _ = doc.Find("meta[property='music:musician']").Attr("content")
	if artist == "" {
		artist, _ = doc.Find("meta[property='music:musician_description']").Attr("content")
	}
	if artist == "" {
		artist, _ = doc.Find("meta[name='music:musician']").Attr("content")
	}

	// Try to find album
	album, _ := doc.Find("meta[property='music:album']").Attr("content")
	if album == "" {
		album, _ = doc.Find("meta[property='og:description']").Attr("content")
		// Description might contain "Song · Album · Year", try to extract album
		if strings.Contains(album, "·") {
			parts := strings.Split(album, "·")
			if len(parts) >= 2 {
				album = strings.TrimSpace(parts[1])
			}
		}
	}

	if title == "" {
		return nil, errors.New("no title found in Open Graph tags")
	}

	// If we still don't have artist, try extracting from page title or description
	if artist == "" {
		pageTitle, _ := doc.Find("title").First().Html()
		// Apple Music titles are often formatted as "Track Name - Artist Name"
		if strings.Contains(pageTitle, " - ") {
			parts := strings.Split(pageTitle, " - ")
			if len(parts) >= 2 {
				artist = strings.TrimSpace(parts[1])
				// Remove "on Apple Music" suffix if present
				artist = strings.TrimSuffix(artist, " on Apple Music")
			}
		}
	}

	if artist == "" {
		return nil, errors.New("no artist found in Open Graph tags or page title")
	}

	return &TrackInfo{
		Title:   title,
		Artists: []string{artist},
		Album:   album,
	}, nil
}

// getString safely extracts a string value from a map
func getString(data map[string]interface{}, key string) string {
	if val, ok := data[key].(string); ok {
		return val
	}
	return ""
}
