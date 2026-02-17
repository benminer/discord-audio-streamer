package lyrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type SearchResult struct {
	ID           int    `json:\"id\"`
	TrackName    string `json:\"trackName\"`
	ArtistName   string `json:\"artistName\"`
	AlbumName    string `json:\"albumName\"`
	PlainLyrics  string `json:\"plainLyrics\"`
	SyncedLyrics string `json:\"syncedLyrics\"`
}

type Client struct {
	httpClient *http.Client
}

func New() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) Search(query string) (string, string, error) {
	u := fmt.Sprintf(\"https://lrclib.net/api/search?q=%s\", url.QueryEscape(query))
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return \"\", \"\", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return \"\", \"\", fmt.Errorf(\"lrclib API returned status %d\", resp.StatusCode)
	}

	var results []SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return \"\", \"\", err
	}

	if len(results) == 0 {
		return \"\", \"\", nil
	}

	res := results[0]

	var lyrics string
	if res.PlainLyrics != \"\" {
		lyrics = res.PlainLyrics
	} else if res.SyncedLyrics != \"\" {
		re := regexp.MustCompile(`\\[\\d+:\\d+\\.\\d+\\]`)
		lyrics = re.ReplaceAllString(res.SyncedLyrics, \"\")
		lyrics = strings.TrimSpace(lyrics)
	}

	if lyrics == \"\" {
		return \"\", res.TrackName + \" — \" + res.ArtistName, nil
	}

	trackInfo := res.TrackName + \" — \" + res.ArtistName
	return lyrics, trackInfo, nil
}