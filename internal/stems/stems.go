// Package stems provides offline stem separation for Echo Music Desktop.
//
// The model is HT-Demucs exported to ONNX by StemSplitio (MIT). A small Python
// helper runs the inference locally; no audio is uploaded. The four mono stem
// files are mixed into a temporary stereo WAV that the existing player can
// play like any other local track, so queue, automix and next-track logic keep
// working while stems are active.
package stems

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EchoMusicApp/Echo-Music/internal/proc"
)

//go:embed separate.py
var helperSource []byte

const (
	// ModelURL is the public ONNX export used by the mobile and Flutter builds.
	ModelURL = "https://huggingface.co/StemSplitio/htdemucs-onnx/resolve/main/htdemucs_fp16weights.onnx?download=true"
	// MinModelBytes guards against accepting an HTML error page as a model.
	MinModelBytes = 80 << 20
	modelName     = "htdemucs_fp16weights.onnx"
	staleAge      = 12 * time.Hour
)

var stemNames = [4]string{"drums", "bass", "other", "vocals"}

// Names returns the four stem names in the order used by the helper.
func Names() [4]string { return stemNames }

// Service owns the cache directory and the separation pipeline.
type Service struct {
	root     string
	provider string
	fast     bool

	mu     sync.Mutex
	ready  bool
	python string
	helper string
	model  string

	// Progress reports helper chunks for the current separation.
	Progress func(videoID string, chunk, total int)
	// Setup reports one-off setup phases (python/model).
	Setup func(text string, fraction float64)
}

// New creates a service using root as the cache root.
func New(root string) *Service {
	if root == "" {
		root = filepath.Join(os.TempDir(), "echo-music-stems")
	}
	return &Service{root: root, provider: "auto", fast: true}
}

// SetOptions changes inference settings. Auto prefers DirectML/CUDA.
func (s *Service) SetOptions(provider string, fast bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch provider {
	case "cpu", "dml", "cuda", "auto":
		s.provider = provider
	}
	s.fast = fast
	s.ready = false
}

func (s *Service) Root() string { return s.root }

func safeID(videoID string) string {
	var b strings.Builder
	for _, r := range videoID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// MultiPath is the four-channel cache file produced by the helper.
func (s *Service) MultiPath(videoID string) string {
	return filepath.Join(s.root, safeID(videoID)+".stems.wav")
}

// Paths returns the four mono stem files.
func (s *Service) Paths(videoID string) [4]string {
	base := strings.TrimSuffix(s.MultiPath(videoID), ".stems.wav")
	var out [4]string
	for i, name := range stemNames {
		out[i] = base + "." + name + ".wav"
	}
	return out
}

// Cached reports both the four-channel file and all mono stems exist.
func (s *Service) Cached(videoID string) bool {
	if videoID == "" {
		return false
	}
	if !regularFile(s.MultiPath(videoID)) {
		return false
	}
	for _, path := range s.Paths(videoID) {
		if !regularFile(path) {
			return false
		}
	}
	return true
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 44
}

// CleanupStale removes interrupted-run temporary files older than 12 hours
// and leftover mixed WAVs. Files currently being written are never touched.
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
		if !strings.HasSuffix(name, ".part") && !strings.HasSuffix(name, ".decode.wav") &&
			!strings.HasPrefix(name, "mix-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < staleAge {
			continue
		}
		if err := os.Remove(filepath.Join(s.root, name)); err != nil {
			log.Printf("stems: remove stale %s: %v", name, err)
		}
	}
}

func (s *Service) reportSetup(text string, fraction float64) {
	if s.Setup != nil {
		s.Setup(text, fraction)
	}
}

// Ensure installs the helper, finds Python and downloads the ONNX model once.
func (s *Service) Ensure(ctx context.Context) error {
	s.mu.Lock()
	if s.ready {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	helper := filepath.Join(s.root, "separate.py")
	sum := sha256.Sum256(helperSource)
	want := hex.EncodeToString(sum[:])
	if data, err := os.ReadFile(helper); err != nil || hex.EncodeToString(sha256Sum(data)) != want {
		if err := os.WriteFile(helper, helperSource, 0o644); err != nil {
			return fmt.Errorf("stems: write helper: %w", err)
		}
	}

	python := ""
	for _, name := range []string{"python3", "python"} {
		if path, err := proc.Tool(name); err == nil {
			python = path
			break
		}
	}
	if python == "" {
		return errors.New("stems: Python 3 not found (numpy, onnxruntime and soundfile are required)")
	}
	s.reportSetup("Checking Python…", 0)
	check := proc.Command(ctx, python, "-c", "import numpy, onnxruntime, soundfile")
	if out, err := check.CombinedOutput(); err != nil {
		s.reportSetup("Installing numpy, onnxruntime and soundfile…", 0.02)
		install := proc.Command(ctx, python, "-m", "pip", "install", "--disable-pip-version-check", "numpy", "onnxruntime", "soundfile")
		if out, err := install.CombinedOutput(); err != nil {
			return fmt.Errorf("stems: install dependencies: %w: %s", err, strings.TrimSpace(string(out)))
		}
	} else {
		_ = out
	}

	model := filepath.Join(s.root, "models", modelName)
	if err := os.MkdirAll(filepath.Dir(model), 0o755); err != nil {
		return err
	}
	if info, err := os.Stat(model); err != nil || info.Size() < MinModelBytes {
		if err := s.download(ctx, ModelURL, model); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.ready, s.python, s.helper, s.model = true, python, helper, model
	s.mu.Unlock()
	s.reportSetup("Ready", 1)
	return nil
}

func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func (s *Service) download(ctx context.Context, url, target string) error {
	s.reportSetup("Downloading the separation model (166 MB)…", 0.05)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stems: model download HTTP %d", resp.StatusCode)
	}
	tmp := target + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	total := resp.ContentLength
	var written int64
	buf := make([]byte, 128<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := file.Write(buf[:n]); err != nil {
				file.Close()
				os.Remove(tmp)
				return err
			}
			written += int64(n)
			if total > 0 {
				s.reportSetup("Downloading the separation model (166 MB)…", 0.05+0.9*float64(written)/float64(total))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			file.Close()
			os.Remove(tmp)
			return readErr
		}
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if written < MinModelBytes {
		os.Remove(tmp)
		return fmt.Errorf("stems: model download too small (%d bytes)", written)
	}
	return os.Rename(tmp, target)
}

var progressLine = regexp.MustCompile(`^PROGRESS\s+(\d+)/(\d+)$`)
var providerLine = regexp.MustCompile(`^PROVIDER\s+(.+)$`)

// Separate decodes source to 44.1 kHz stereo, runs the ONNX helper and writes
// the four-channel cache plus four mono stems.
func (s *Service) Separate(ctx context.Context, videoID, sourcePath string) error {
	if videoID == "" || sourcePath == "" {
		return errors.New("stems: missing video id or source path")
	}
	if err := s.Ensure(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	python, helper, model := s.python, s.helper, s.model
	s.mu.Unlock()

	target := s.MultiPath(videoID)
	decoded := filepath.Join(s.root, safeID(videoID)+".decode.wav")
	part := target + ".part"
	ffmpeg, err := proc.Tool("ffmpeg")
	if err != nil {
		return err
	}
	decode := proc.Command(ctx, ffmpeg, "-nostdin", "-y", "-i", sourcePath, "-vn", "-ac", "2", "-ar", "44100", "-c:a", "pcm_f32le", "-f", "wav", decoded)
	if out, err := decode.CombinedOutput(); err != nil {
		os.Remove(decoded)
		return fmt.Errorf("stems: decode: %w: %s", err, strings.TrimSpace(string(out)))
	}
	defer os.Remove(decoded)

	overlap := "0.25"
	if s.fast {
		overlap = "0.1"
	}
	cmd := proc.Command(ctx, python, helper, decoded, part, model, s.provider, overlap)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if m := progressLine.FindStringSubmatch(line); m != nil {
			chunk, _ := strconv.Atoi(m[1])
			total, _ := strconv.Atoi(m[2])
			if s.Progress != nil {
				s.Progress(videoID, chunk, total)
			}
		} else if providerLine.MatchString(line) {
			log.Printf("stems: provider %s", strings.TrimSpace(line))
		}
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		os.Remove(part)
		return fmt.Errorf("stems: helper: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if !regularFile(part) {
		return fmt.Errorf("stems: helper produced no output: %s", strings.TrimSpace(stderr.String()))
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(part, target)
}

// PlaybackPaths returns an existing mixed WAV for the enabled stems, creating
// it when needed. The result can be played by the normal player.
func (s *Service) PlaybackPaths(videoID string, enabled [4]bool) (string, error) {
	if !s.Cached(videoID) {
		return "", fmt.Errorf("stems: %s is not separated", videoID)
	}
	return s.Mix(videoID, enabled)
}

// SeparateAll separates every item that has no cached stems. Downloads are
// supplied by the caller so this package stays independent of the app layer.
func (s *Service) SeparateAll(ctx context.Context, items []BatchItem) BatchResult {
	var result BatchResult
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		if item.VideoID == "" || s.Cached(item.VideoID) {
			result.Skipped++
			continue
		}
		source := item.Path
		if source == "" {
			source, item.Err = item.Download(ctx, item.VideoID)
		}
		if item.Err != nil || source == "" {
			result.Failed = append(result.Failed, fmt.Sprintf("%s: %v", item.VideoID, item.Err))
			continue
		}
		if err := s.Separate(ctx, item.VideoID, source); err != nil {
			result.Failed = append(result.Failed, fmt.Sprintf("%s: %v", item.VideoID, err))
			continue
		}
		result.Separated++
	}
	return result
}

// BatchItem is one track of a playlist batch.
type BatchItem struct {
	VideoID  string
	Path     string
	Download func(ctx context.Context, videoID string) (string, error)
	Err      error
}

// BatchResult summarizes a playlist batch.
type BatchResult struct {
	Separated int
	Skipped   int
	Failed    []string
}

// Provider returns the configured provider name.
func (s *Service) Provider() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.provider
}

// Fast reports fast/quality overlap mode.
func (s *Service) Fast() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fast
}

// AvailableProviders asks ONNX Runtime which execution providers are usable.
func (s *Service) AvailableProviders(ctx context.Context) ([]string, error) {
	if err := s.Ensure(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	python := s.python
	s.mu.Unlock()
	out, err := proc.Command(ctx, python, "-c", "import onnxruntime as ort; print(' '.join(ort.get_available_providers()))").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("stems: providers: %w", err)
	}
	return strings.Fields(string(out)), nil
}

// TempDir exposes the cache root for tests and diagnostics.
func (s *Service) TempDir() string { return s.root }
