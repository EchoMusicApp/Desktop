package automix

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/EchoMusicApp/Echo-Music/internal/proc"
)

var safePattern = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Service analyses and caches beat/key results for songs.
type Service struct {
	root         string
	mu           sync.Mutex
	cache        map[string]*Result
	preparations []float64
}

// New creates a service with a persistent cache under root.
func New(root string) *Service {
	if root == "" {
		root = filepath.Join(os.TempDir(), "echo-music-automix")
	}
	s := &Service{root: root, cache: make(map[string]*Result)}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		if r, err := DecodeJSON(data); err == nil {
			s.cache[entry.Name()[:len(entry.Name())-5]] = r
		}
	}
	return s
}

func (s *Service) Root() string { return s.root }

func (s *Service) path(videoID string) string {
	return filepath.Join(s.root, safePattern.ReplaceAllString(videoID, "_")+".json")
}

// Cached returns a previously computed analysis without touching disk again.
func (s *Service) Cached(videoID string) *Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cache[videoID]
}

// AnalyzePath decodes a local file (or direct URL) to 22.05 kHz mono PCM with
// the bundled ffmpeg and computes the beat grid and musical key.
func (s *Service) AnalyzePath(ctx context.Context, videoID, source string, durationMs int) (*Result, error) {
	if videoID == "" || source == "" {
		return nil, errors.New("automix: missing video id or source")
	}
	if cached := s.Cached(videoID); cached != nil {
		return cached, nil
	}
	ffmpeg, err := proc.Tool("ffmpeg")
	if err != nil {
		return nil, err
	}
	cmd := proc.Command(ctx, ffmpeg, "-nostdin", "-v", "error", "-i", source,
		"-vn", "-ac", "1", "-ar", "22050", "-f", "f32le", "pipe:1")
	data, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(data) < 22050*4 {
		return nil, errors.New("automix: audio too short to analyse")
	}
	mono := make([]float32, len(data)/4)
	for i := range mono {
		bits := binary.LittleEndian.Uint32(data[i*4 : i*4+4])
		mono[i] = float32fromBits(bits)
	}
	tailStart := len(mono) - 30*22050
	if tailStart < 0 {
		tailStart = 0
	}
	result := Analyze(mono, 22050, mono[tailStart:], durationMs)
	if result == nil {
		return nil, errors.New("automix: no beat grid found")
	}
	encoded, err := EncodeJSON(result)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.path(videoID), encoded, 0o644); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[videoID] = result
	s.mu.Unlock()
	return result, nil
}

// RecordPreparation feeds the adaptive pre-arm lead estimator.
func (s *Service) RecordPreparation(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preparations = append(s.preparations, d.Seconds()*1000)
	if len(s.preparations) > 8 {
		s.preparations = s.preparations[len(s.preparations)-8:]
	}
}

// PreArmLead returns the current adaptive lead in milliseconds.
func (s *Service) PreArmLead(fallbackMs int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return PreArmLeadMs(s.preparations, fallbackMs)
}

func float32fromBits(bits uint32) float32 {
	return math.Float32frombits(bits)
}
