package lansync

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrackDTO is the wire representation of a queued song or podcast episode shared between Android and Desktop.
type TrackDTO struct {
	VideoID     string `json:"videoId"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album,omitempty"`
	DurationSec int    `json:"durationSec,omitempty"`
	Thumbnail   string `json:"thumbnail,omitempty"`
	IsPodcast   bool   `json:"isPodcast,omitempty"`
}

// EpisodeDTO represents podcast episode listening progress synced between devices.
type EpisodeDTO struct {
	VideoID    string `json:"videoId"`
	PositionMs int64  `json:"positionMs"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Finished   bool   `json:"finished,omitempty"`
	UpdatedAt  int64  `json:"updatedAt,omitempty"`
	Title      string `json:"title,omitempty"`
	Show       string `json:"show,omitempty"`
	Thumbnail  string `json:"thumbnail,omitempty"`
}

// PeerState is returned by GET /v1/state and POST /v1/control.
type PeerState struct {
	DeviceID   string       `json:"deviceId"`
	DeviceName string       `json:"deviceName"`
	Platform   string       `json:"platform"`
	Playing    bool         `json:"playing"`
	Volume     float64      `json:"volume"`
	PositionMs int64        `json:"positionMs"`
	DurationMs int64        `json:"durationMs"`
	Shuffle    bool         `json:"shuffle"`
	Repeat     string       `json:"repeat"`
	Current    *TrackDTO    `json:"current,omitempty"`
	Queue      []TrackDTO   `json:"queue"`
	Index      int          `json:"index"`
	Source     string       `json:"source,omitempty"`
	Episodes   []EpisodeDTO `json:"episodes,omitempty"`
	UpdatedAt  int64        `json:"updatedAt"`
}

// ControlCommand is sent to POST /v1/control for Echo Connect remote control.
type ControlCommand struct {
	Action           string    `json:"action"` // play, pause, toggle, next, previous, seek, volume, play_index, queue_add, shuffle, repeat
	PositionMs       int64     `json:"positionMs,omitempty"`
	Volume           float64   `json:"volume,omitempty"`
	Index            int       `json:"index,omitempty"`
	Shuffle          *bool     `json:"shuffle,omitempty"`
	Repeat           string    `json:"repeat,omitempty"`
	ControllerPeerID string    `json:"controllerPeerId,omitempty"`
	Item             *TrackDTO `json:"item,omitempty"`
}

// HandoffPayload is sent to POST /v1/handoff to transfer playback and Up Next queue to a peer.
type HandoffPayload struct {
	Items           []TrackDTO   `json:"items"`
	Index           int          `json:"index"`
	PositionMs      int64        `json:"positionMs"`
	Source          string       `json:"source,omitempty"`
	PlayImmediately bool         `json:"playImmediately"`
	Episodes        []EpisodeDTO `json:"episodes,omitempty"`
}

// EpisodesSyncPayload is exchanged on POST /v1/episodes/sync.
type EpisodesSyncPayload struct {
	Episodes []EpisodeDTO `json:"episodes"`
}

// Handler bridges the LAN Sync HTTP server to the host app's main-thread state.
type Handler interface {
	GetState() PeerState
	ApplyControl(cmd ControlCommand) PeerState
	ApplyHandoff(payload HandoffPayload) PeerState
	MergeEpisodes(episodes []EpisodeDTO) []EpisodeDTO
	OnPeersChanged(peers []Peer)
}

type Manager struct {
	deviceID   string
	deviceName string
	platform   string
	handler    Handler

	mu     sync.Mutex
	cancel context.CancelFunc
	srv    *http.Server
	ln     net.Listener
	mdns   *mdnsService
	port   int
	client *http.Client
}

func NewManager(deviceID, deviceName, platform string, handler Handler) *Manager {
	if deviceID == "" {
		deviceID = randomHexID()
	}
	if strings.TrimSpace(deviceName) == "" {
		deviceName = defaultDeviceName()
	}
	if platform == "" {
		platform = "desktop"
	}
	return &Manager{
		deviceID:   deviceID,
		deviceName: deviceName,
		platform:   platform,
		handler:    handler,
		client:     &http.Client{Timeout: 5 * time.Second},
	}
}

func (m *Manager) DeviceID() string   { return m.deviceID }
func (m *Manager) DeviceName() string { return m.deviceName }

func (m *Manager) Port() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.port
}

const (
	preferredDesktopPort = 45871
	preferredAndroidPort = 45870
)

func (m *Manager) Start(parent context.Context) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", preferredDesktopPort))
	if err != nil {
		ln, err = net.Listen("tcp", ":0")
	}
	if err != nil {
		cancel()
		m.mu.Unlock()
		return err
	}
	tcpAddr, _ := ln.Addr().(*net.TCPAddr)
	port := 0
	if tcpAddr != nil {
		port = tcpAddr.Port
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/state", m.handleState)
	mux.HandleFunc("/v1/control", m.handleControl)
	mux.HandleFunc("/v1/handoff", m.handleHandoff)
	mux.HandleFunc("/v1/episodes/sync", m.handleEpisodesSync)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	mdns := newMDNSService(m.deviceID, m.deviceName, m.platform, port, func(peers []Peer) {
		if m.handler != nil {
			m.handler.OnPeersChanged(peers)
		}
	})

	m.cancel = cancel
	m.srv = srv
	m.ln = ln
	m.port = port
	m.mdns = mdns
	m.mu.Unlock()

	go func() {
		_ = srv.Serve(ln)
	}()

	if err := mdns.Start(ctx); err != nil {
		// HTTP server still works even if multicast socket failed in restricted sandboxes.
		_ = err
	}
	go m.adbBridgeLoop(ctx, port)
	return nil
}

func findADBPath() string {
	if p, err := exec.LookPath("adb"); err == nil {
		return p
	}
	candidates := []string{
		"/opt/homebrew/bin/adb",
		"/usr/local/bin/adb",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, home+"/Library/Android/sdk/platform-tools/adb")
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// adbBridgeLoop sets up adb reverse/forward tunnels when an Android device is attached over USB,
// allowing Echo Connect to work seamlessly even on Wi-Fi networks with AP/client isolation.
func (m *Manager) adbBridgeLoop(ctx context.Context, desktopPort int) {
	adbPath := findADBPath()
	if adbPath == "" {
		return
	}
	syncTunnels := func() {
		if desktopPort > 0 {
			cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			_ = exec.CommandContext(cctx, adbPath, "reverse", fmt.Sprintf("tcp:%d", desktopPort), fmt.Sprintf("tcp:%d", desktopPort)).Run()
			cancel()
		}
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = exec.CommandContext(cctx, adbPath, "forward", fmt.Sprintf("tcp:%d", preferredAndroidPort), fmt.Sprintf("tcp:%d", preferredAndroidPort)).Run()
		cancel()
		for _, p := range m.Peers() {
			if p.Platform == "android" && p.Port > 0 && p.Port != preferredAndroidPort {
				cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				_ = exec.CommandContext(cctx, adbPath, "forward", fmt.Sprintf("tcp:%d", p.Port), fmt.Sprintf("tcp:%d", p.Port)).Run()
				cancel()
			}
		}
	}
	syncTunnels()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			syncTunnels()
		}
	}
}

func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	srv := m.srv
	ln := m.ln
	mdns := m.mdns
	m.cancel = nil
	m.srv = nil
	m.ln = nil
	m.mdns = nil
	m.port = 0
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if mdns != nil {
		mdns.Stop()
	}
	if srv != nil {
		ctx, c := context.WithTimeout(context.Background(), time.Second)
		_ = srv.Shutdown(ctx)
		c()
	}
	if ln != nil {
		_ = ln.Close()
	}
}

func (m *Manager) Peers() []Peer {
	m.mu.Lock()
	mdns := m.mdns
	m.mu.Unlock()
	if mdns == nil {
		return nil
	}
	peers := mdns.Peers()
	sort.Slice(peers, func(i, j int) bool {
		return strings.ToLower(peers[i].Name) < strings.ToLower(peers[j].Name)
	})
	return peers
}

func (m *Manager) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, m.handler.GetState())
}

func (m *Manager) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cmd ControlCommand
	if err := decodeJSONBody(r.Body, &cmd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, m.handler.ApplyControl(cmd))
}

func (m *Manager) handleHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload HandoffPayload
	if err := decodeJSONBody(r.Body, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, m.handler.ApplyHandoff(payload))
}

func (m *Manager) handleEpisodesSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload EpisodesSyncPayload
	if err := decodeJSONBody(r.Body, &payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	merged := m.handler.MergeEpisodes(payload.Episodes)
	writeJSON(w, EpisodesSyncPayload{Episodes: merged})
}

// --- Client RPCs ---

func (m *Manager) FetchState(ctx context.Context, peer Peer) (PeerState, error) {
	pCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	out, err := m.fetchStateURL(pCtx, peer.BaseURL()+"/v1/state")
	cancel()
	if err != nil && peer.Host != "127.0.0.1" && peer.Port > 0 {
		fbCtx, fbCancel := context.WithTimeout(context.Background(), 2*time.Second)
		fb, fbErr := m.fetchStateURL(fbCtx, fmt.Sprintf("http://127.0.0.1:%d/v1/state", peer.Port))
		fbCancel()
		if fbErr == nil {
			return fb, nil
		}
	}
	return out, err
}

func (m *Manager) fetchStateURL(ctx context.Context, url string) (PeerState, error) {
	var out PeerState
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("peer returned %s", resp.Status)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out)
	return out, err
}

func (m *Manager) SendControl(ctx context.Context, peer Peer, cmd ControlCommand) (PeerState, error) {
	var out PeerState
	err := m.postJSONWithFallback(ctx, peer, "/v1/control", cmd, &out)
	return out, err
}

func (m *Manager) SendHandoff(ctx context.Context, peer Peer, payload HandoffPayload) (PeerState, error) {
	var out PeerState
	err := m.postJSONWithFallback(ctx, peer, "/v1/handoff", payload, &out)
	return out, err
}

func (m *Manager) SyncEpisodes(ctx context.Context, peer Peer, episodes []EpisodeDTO) ([]EpisodeDTO, error) {
	var out EpisodesSyncPayload
	err := m.postJSONWithFallback(ctx, peer, "/v1/episodes/sync", EpisodesSyncPayload{Episodes: episodes}, &out)
	return out.Episodes, err
}

func (m *Manager) postJSONWithFallback(ctx context.Context, peer Peer, path string, body any, out any) error {
	pCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	err := m.postJSON(pCtx, peer.BaseURL()+path, body, out)
	cancel()
	if err != nil && peer.Host != "127.0.0.1" && peer.Port > 0 {
		fbCtx, fbCancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		fbErr := m.postJSON(fbCtx, fmt.Sprintf("http://127.0.0.1:%d%s", peer.Port, path), body, out)
		fbCancel()
		if fbErr == nil {
			return nil
		}
	}
	return err
}

func (m *Manager) postJSON(ctx context.Context, url string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("peer returned %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// MergeEpisodeLists merges local and remote podcast episode progress slices, preferring finished
// episodes and further playback positions (or newer updatedAt timestamps when available).
func MergeEpisodeLists(local, remote []EpisodeDTO) []EpisodeDTO {
	byID := make(map[string]EpisodeDTO, len(local)+len(remote))
	mergeOne := func(ep EpisodeDTO) {
		if ep.VideoID == "" {
			return
		}
		cur, exists := byID[ep.VideoID]
		if !exists {
			byID[ep.VideoID] = ep
			return
		}
		winner := cur
		switch {
		case ep.Finished && !cur.Finished:
			winner = ep
		case cur.Finished && !ep.Finished:
			winner = cur
		case ep.UpdatedAt > 0 && cur.UpdatedAt > 0 && ep.UpdatedAt != cur.UpdatedAt:
			if ep.UpdatedAt > cur.UpdatedAt {
				winner = ep
			}
		case ep.PositionMs > cur.PositionMs:
			winner = ep
		}
		// Preserve richest metadata across both sides.
		if winner.Title == "" {
			winner.Title = firstNonEmpty(ep.Title, cur.Title)
		}
		if winner.Show == "" {
			winner.Show = firstNonEmpty(ep.Show, cur.Show)
		}
		if winner.Thumbnail == "" {
			winner.Thumbnail = firstNonEmpty(ep.Thumbnail, cur.Thumbnail)
		}
		if winner.DurationMs == 0 {
			winner.DurationMs = max(ep.DurationMs, cur.DurationMs)
		}
		byID[ep.VideoID] = winner
	}
	for _, ep := range local {
		mergeOne(ep)
	}
	for _, ep := range remote {
		mergeOne(ep)
	}
	out := make([]EpisodeDTO, 0, len(byID))
	for _, ep := range byID {
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].VideoID < out[j].VideoID
	})
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func decodeJSONBody(body io.Reader, dst any) error {
	return json.NewDecoder(io.LimitReader(body, 4<<20)).Decode(dst)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func randomHexID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func defaultDeviceName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "Echo Desktop"
	}
	h = strings.TrimSuffix(h, ".local")
	return h
}
