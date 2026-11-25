package ga4

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultEndpoint = "https://www.google-analytics.com/mp/collect"

const Timeout = 10 * time.Second

type Client struct {
	MeasurementID string
	APISecret     string
	Endpoint      string
	HTTP          *http.Client
}

type payload struct {
	ClientID string      `json:"client_id"`
	Events   []eventItem `json:"events"`
}

type eventItem struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params,omitempty"`
}

func (c *Client) Capture(event string, properties map[string]any) error {
	if c.MeasurementID == "" {
		return errors.New("ga4: no measurement id")
	}
	if c.APISecret == "" {
		return errors.New("ga4: no api secret")
	}
	if event == "" {
		return errors.New("ga4: no event name")
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	var params map[string]any
	if len(properties) > 0 {
		params = make(map[string]any, len(properties))
		maps.Copy(params, properties)
	}
	body, err := json.Marshal(payload{
		ClientID: id,
		Events:   []eventItem{{Name: event, Params: params}},
	})
	if err != nil {
		return err
	}
	base := c.Endpoint
	if base == "" {
		base = DefaultEndpoint
	}
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("measurement_id", c.MeasurementID)
	q.Set("api_secret", c.APISecret)
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		reply, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("ga4: %s answered %s: %s", event, response.Status, strings.TrimSpace(string(reply)))
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
