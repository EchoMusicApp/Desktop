// Package canvas implements the opt-in video backdrop behind the full-screen
// player. It mirrors the mobile app: a short looping clip is taken from the
// centre of the song's official video and shown silently behind the artwork.
//
// No video decoder is added to the audio pipeline. ffmpeg (already bundled)
// extracts a five-second animated GIF that mygo's decoder plays as a video.
package canvas

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EchoMusicApp/Echo-Music/internal/proc"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

const (
	clipSeconds = 5
	clipFPS     = 10
	clipWidth   = 480
	staleAge    = 12 * time.Hour
)

var safePattern = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Service caches extracted clips on disk.
type Service struct {
	root   string
	client *youtube.Client
	mu     sync.Mutex
	busy   map[string]bool
}

// New creates a canvas service using root as cache directory.
func New(root string, client *youtube.Client) *Service {
	if root == "" {
		root = filepath.Join(os.TempDir(), "echo-music-canvas")
	}
	return &Service{root: root, client: client, busy: make(map[string]bool)}
}

func (s *Service) Root() string { return s.root }

func safeID(videoID string) string { return safePattern.ReplaceAllString(videoID, "_") }

// Path is the cached GIF for a video.
func (s *Service) Path(videoID string) string {
	return filepath.Join(s.root, safeID(videoID)+".canvas.gif")
}

// CachedPath returns the GIF when it already exists.
func (s *Service) CachedPath(videoID string) string {
	path := s.Path(videoID)
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() && info.Size() > 1024 && time.Since(info.ModTime()) < 7*24*time.Hour {
		return path
	}
	return ""
}

// CleanupStale removes old clips and interrupted extracts.
func (s *Service) CleanupStale() {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".canvas.gif") && !strings.HasSuffix(name, ".canvas.gif.part") {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < staleAge {
			continue
		}
		if err := os.Remove(filepath.Join(s.root, name)); err != nil {
			log.Printf("canvas: remove stale %s: %v", name, err)
		}
	}
}

// Ensure extracts a looping GIF clip when none is cached. durationSec is the
// song length, used to take the clip from the centre of the video.
func (s *Service) Ensure(ctx context.Context, videoID string, durationSec float64) (string, error) {
	if videoID == "" {
		return "", errors.New("canvas: missing video ID")
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return "", err
	}
	if cached := s.CachedPath(videoID); cached != "" {
		return cached, nil
	}
	s.mu.Lock()
	if s.busy[videoID] {
		s.mu.Unlock()
		return "", errors.New("canvas: clip is already being prepared")
	}
	s.busy[videoID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.busy, videoID)
		s.mu.Unlock()
	}()

	if s.client == nil {
		return "", errors.New("canvas: no YouTube client")
	}
	url, err := s.client.VideoStream(ctx, videoID)
	if err != nil {
		return "", err
	}
	ffmpeg, err := proc.Tool("ffmpeg")
	if err != nil {
		return "", err
	}
	start := durationSec/2 - clipSeconds/2
	if start < 0 {
		start = 0
	}
	target := s.Path(videoID)
	tmp := target + ".part"
	os.Remove(tmp)
	filter := fmt.Sprintf("fps=%d,scale=%d:-2:flags=fast_bilinear", clipFPS, clipWidth)
	args := []string{
		"-nostdin", "-y", "-user_agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"-ss", strconv.FormatFloat(start, 'f', 2, 64),
		"-i", url, "-t", strconv.Itoa(clipSeconds),
		"-vf", filter, "-an", "-loop", "0", "-f", "gif", tmp,
	}
	cmd := proc.Command(ctx, ffmpeg, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("canvas: ffmpeg: %w: %s", err, strings.TrimSpace(string(out)))
	}
	info, err := os.Stat(tmp)
	if err != nil || info.Size() < 1024 {
		os.Remove(tmp)
		return "", errors.New("canvas: empty clip")
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return target, nil
}

// Read returns the cached GIF bytes for mygo's video decoder.
func (s *Service) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}
