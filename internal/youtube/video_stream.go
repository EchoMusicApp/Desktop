package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// VideoStream returns a direct URL for a progressive MP4 video stream. It is
// used by the opt-in canvas feature; audio playback never calls this method.
func (c *Client) VideoStream(ctx context.Context, videoID string) (string, error) {
	if strings.TrimSpace(videoID) == "" {
		return "", errors.New("youtube: video stream requires a video ID")
	}
	if c.currentVisitorData() == "" {
		if _, err := c.executeAs(ctx, musicClient, false, "visitor_id", map[string]any{}); err != nil {
			return "", err
		}
	}
	raw, err := c.executeAs(ctx, visionOSClient, false, "player", map[string]any{
		"videoId": videoID, "contentCheckOk": true, "racyCheckOk": true,
	})
	if err != nil {
		return "", err
	}
	return parseVideoStream(raw)
}

func parseVideoStream(raw json.RawMessage) (string, error) {
	var response struct {
		PlayabilityStatus struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"playabilityStatus"`
		StreamingData struct {
			Formats         []videoFormat `json:"formats"`
			AdaptiveFormats []videoFormat `json:"adaptiveFormats"`
		} `json:"streamingData"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", err
	}
	if response.PlayabilityStatus.Status != "" && response.PlayabilityStatus.Status != "OK" {
		return "", errors.New("youtube: player says " + response.PlayabilityStatus.Status + ": " + response.PlayabilityStatus.Reason)
	}
	// Progressive formats first: they contain video and audio in one MP4 and
	// are the ones a normal player can open without a separate audio track.
	for _, group := range [][]videoFormat{response.StreamingData.Formats, response.StreamingData.AdaptiveFormats} {
		best, height := "", -1
		for _, format := range group {
			if format.URL == "" || !strings.HasPrefix(format.MimeType, "video/mp4") {
				continue
			}
			h := format.Height
			if h == 0 {
				h = parseHeight(format.MimeType)
			}
			if h > height {
				best, height = format.URL, h
			}
		}
		if best != "" {
			return best, nil
		}
	}
	return "", errors.New("youtube: no direct video stream")
}

type videoFormat struct {
	MimeType string `json:"mimeType"`
	URL      string `json:"url"`
	Height   int    `json:"height"`
}

func parseHeight(mime string) int {
	for _, size := range []string{"1080", "720", "480", "360", "240"} {
		if strings.Contains(mime, size+"p") {
			n, _ := strconv.Atoi(size)
			return n
		}
	}
	return 0
}
