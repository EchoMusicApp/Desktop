package ga4

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCaptureSendsMeasurementProtocolPayload(t *testing.T) {
	var gotQuery string
	var gotPayload payload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := &Client{
		MeasurementID: "G-TEST123",
		APISecret:     "secret-xyz",
		Endpoint:      srv.URL,
		HTTP:          srv.Client(),
	}
	if err := c.Capture("install", map[string]any{"os": "darwin", "version": "1.0.0"}); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if gotQuery != "api_secret=secret-xyz&measurement_id=G-TEST123" {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotPayload.ClientID == "" || len(gotPayload.Events) != 1 || gotPayload.Events[0].Name != "install" {
		t.Fatalf("unexpected payload: %+v", gotPayload)
	}
	if gotPayload.Events[0].Params["os"] != "darwin" {
		t.Fatalf("params = %+v", gotPayload.Events[0].Params)
	}
}

func TestCaptureValidation(t *testing.T) {
	if err := (&Client{APISecret: "s"}).Capture("install", nil); err == nil {
		t.Fatal("expected error for missing measurement id")
	}
	if err := (&Client{MeasurementID: "G-1"}).Capture("install", nil); err == nil {
		t.Fatal("expected error for missing api secret")
	}
	if err := (&Client{MeasurementID: "G-1", APISecret: "s"}).Capture("", nil); err == nil {
		t.Fatal("expected error for missing event name")
	}
}
