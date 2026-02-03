package applemusic

import (
	"errors"
	"net/url"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
)

var (
	// Regex patterns for extracting IDs
	albumRegex    = regexp.MustCompile(`/album/[^/]+/(\d+)`)
	playlistRegex = regexp.MustCompile(`/playlist/[^/]+/(pl\.[a-zA-Z0-9-]+)`)
	artistRegex   = regexp.MustCompile(`/artist/[^/]+/(\d+)`)
)

// ParseAppleMusicURL parses an Apple Music URL and extracts relevant IDs
func ParseAppleMusicURL(rawURL string) (AppleMusicRequest, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return AppleMusicRequest{}, err
	}

	// Support both music.apple.com and itunes.apple.com
	if !strings.Contains(parsedURL.Host, "apple.com") {
		log.Warnf("URL does not contain apple.com: %s", rawURL)
		return AppleMusicRequest{}, errors.New("not an Apple Music URL")
	}

	request := AppleMusicRequest{}

	// Extract country code (e.g., /us/album/...)
	pathParts := strings.Split(strings.TrimPrefix(parsedURL.Path, "/"), "/")
	if len(pathParts) > 0 {
		request.Country = pathParts[0]
		log.Tracef("Extracted country code: %s", request.Country)
	}

	// Check for track ID in query params (e.g., ?i=1646389445)
	if trackID := parsedURL.Query().Get("i"); trackID != "" {
		request.TrackID = trackID
		log.Tracef("Parsed Apple Music track URL: track=%s", trackID)

		// Also need album ID for track context
		if matches := albumRegex.FindStringSubmatch(parsedURL.Path); len(matches) > 1 {
			request.AlbumID = matches[1]
			log.Tracef("Extracted album ID from track URL: %s", request.AlbumID)
		}
		return request, nil
	}

	// Parse album, playlist, or artist
	if strings.Contains(parsedURL.Path, "/album/") {
		if matches := albumRegex.FindStringSubmatch(parsedURL.Path); len(matches) > 1 {
			request.AlbumID = matches[1]
			log.Tracef("Parsed Apple Music album URL: %s", request.AlbumID)
		}
	} else if strings.Contains(parsedURL.Path, "/playlist/") {
		if matches := playlistRegex.FindStringSubmatch(parsedURL.Path); len(matches) > 1 {
			request.PlaylistID = matches[1]
			log.Tracef("Parsed Apple Music playlist URL: %s", request.PlaylistID)
		}
	} else if strings.Contains(parsedURL.Path, "/artist/") {
		if matches := artistRegex.FindStringSubmatch(parsedURL.Path); len(matches) > 1 {
			request.ArtistID = matches[1]
			log.Tracef("Parsed Apple Music artist URL: %s", request.ArtistID)
		}
	}

	// Validate we extracted something
	if request.TrackID == "" && request.AlbumID == "" &&
		request.PlaylistID == "" && request.ArtistID == "" {
		log.Warnf("Could not parse Apple Music URL (no IDs extracted): %s", rawURL)
		return AppleMusicRequest{}, errors.New("could not parse Apple Music URL")
	}

	return request, nil
}
