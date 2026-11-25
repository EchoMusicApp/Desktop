package youtube

import (
	"context"
	"encoding/json"
	"testing"
)

func TestProbeVideoStream(t *testing.T) {
	c := NewClient(Options{})
	raw, err := c.executeAs(context.Background(), visionOSClient, false, "player", map[string]any{
		"videoId": "kJQP7kiw5Fk", "contentCheckOk": true, "racyCheckOk": true,
	})
	if err != nil {
		t.Fatalf("executeAs err: %v", err)
	}
	var res struct {
		StreamingData struct {
			Formats []struct {
				Itag int `json:"itag"`
				MimeType string `json:"mimeType"`
				Bitrate int `json:"bitrate"`
				Width int `json:"width"`
				Height int `json:"height"`
				URL string `json:"url"`
			} `json:"formats"`
			AdaptiveFormats []struct {
				Itag int `json:"itag"`
				MimeType string `json:"mimeType"`
				Bitrate int `json:"bitrate"`
				Width int `json:"width"`
				Height int `json:"height"`
				URL string `json:"url"`
			} `json:"adaptiveFormats"`
		} `json:"streamingData"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal err: %v", err)
	}
	t.Logf("Formats count: %d", len(res.StreamingData.Formats))
	for i, f := range res.StreamingData.Formats {
		t.Logf("  format %d: itag=%d mime=%s %dx%d hasURL=%v", i, f.Itag, f.MimeType, f.Width, f.Height, f.URL != "")
	}
	t.Logf("AdaptiveFormats count: %d", len(res.StreamingData.AdaptiveFormats))
	for i, f := range res.StreamingData.AdaptiveFormats {
		if f.Width > 0 {
			t.Logf("  adaptive video %d: itag=%d mime=%s %dx%d hasURL=%v", i, f.Itag, f.MimeType, f.Width, f.Height, f.URL != "")
		}
	}
}
