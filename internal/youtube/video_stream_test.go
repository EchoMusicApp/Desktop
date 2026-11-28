package youtube

import (
	"encoding/json"
	"testing"
)

func TestParseVideoStreamPrefersProgressiveMP4(t *testing.T) {
	raw := json.RawMessage(`{
		"playabilityStatus":{"status":"OK"},
		"streamingData":{
			"formats":[
				{"mimeType":"video/mp4","url":"https://example.com/progressive","height":360},
				{"mimeType":"video/webm","url":"https://example.com/webm","height":720}
			],
			"adaptiveFormats":[
				{"mimeType":"video/mp4","url":"https://example.com/video-only","height":1080}
			]
		}
	}`)
	url, err := parseVideoStream(raw)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://example.com/progressive" {
		t.Fatalf("url = %q", url)
	}
}

func TestParseVideoStreamHandlesUnplayableAndMissing(t *testing.T) {
	if _, err := parseVideoStream(json.RawMessage(`{"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"sign in"}}`)); err == nil {
		t.Fatal("expected unplayable error")
	}
	if _, err := parseVideoStream(json.RawMessage(`{"playabilityStatus":{"status":"OK"}}`)); err == nil {
		t.Fatal("expected missing stream error")
	}
	if got := parseHeight(`video/mp4; codecs="avc1.640028"; 720p`); got != 720 {
		t.Fatalf("parseHeight = %d", got)
	}
}
