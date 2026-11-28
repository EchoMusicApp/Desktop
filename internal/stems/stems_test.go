package stems

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeMonoWAV(t *testing.T, path string, rate uint32, samples []int16) {
	t.Helper()
	var out bytes.Buffer
	data := len(samples) * 2
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(36+data))
	out.WriteString("WAVEfmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, rate)
	_ = binary.Write(&out, binary.LittleEndian, rate*2)
	_ = binary.Write(&out, binary.LittleEndian, uint16(2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(16))
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(data))
	for _, s := range samples {
		_ = binary.Write(&out, binary.LittleEndian, s)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPathsAndCacheRequireEveryStem(t *testing.T) {
	s := New(t.TempDir())
	if got := safeID("a/b?c"); got != "a_b_c" {
		t.Fatalf("safeID = %q", got)
	}
	multi := s.MultiPath("abc")
	if !strings.HasSuffix(multi, "abc.stems.wav") {
		t.Fatalf("multi path = %q", multi)
	}
	paths := s.Paths("abc")
	if len(paths) != 4 || !strings.HasSuffix(paths[3], "abc.vocals.wav") {
		t.Fatalf("paths = %v", paths)
	}
	if err := os.MkdirAll(filepath.Dir(multi), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(multi, make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	if s.Cached("abc") {
		t.Fatal("cache must require the mono stems")
	}
	for _, path := range paths {
		if err := os.WriteFile(path, make([]byte, 64), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !s.Cached("abc") {
		t.Fatal("complete cache was not detected")
	}
}

func TestMixSumsEnabledStems(t *testing.T) {
	s := New(t.TempDir())
	samples := []int16{100, 200, -100}
	for _, path := range s.Paths("song") {
		writeMonoWAV(t, path, 44100, samples)
	}
	path, err := s.Mix("song", [4]bool{true, false, false, true})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < 44 {
		t.Fatalf("mixed WAV missing: %v %v", info, err)
	}
	wav, err := readPCM16WAV(path)
	if err != nil {
		dump, _ := os.ReadFile(path)
		t.Fatalf("%v: % x", err, dump[:min(len(dump), 48)])
	}
	if wav.channels != 2 || wav.sampleRate != 44100 || len(wav.samples) != 6 {
		t.Fatalf("unexpected mix: %+v", wav)
	}
	if wav.samples[0] != 200 || wav.samples[1] != 200 {
		t.Fatalf("sum = %v, want 200", wav.samples[:2])
	}
	again, err := s.Mix("song", [4]bool{true, false, false, true})
	if err != nil || again != path {
		t.Fatalf("mix was not reused: %q %v", again, err)
	}
}

func TestCleanupStaleKeepsFreshTemps(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	old := filepath.Join(root, "song.stems.wav.part")
	fresh := filepath.Join(root, "other.stems.wav.part")
	for _, path := range []string{old, fresh} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(old, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	s.CleanupStale()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("stale temp still present: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh temp was removed: %v", err)
	}
}

func TestSeparateAllUsesCacheAndReportsFailures(t *testing.T) {
	s := New(t.TempDir())
	item := BatchItem{VideoID: "skip"}
	item.VideoID = "ok"
	multi := s.MultiPath("ok")
	_ = os.MkdirAll(filepath.Dir(multi), 0o755)
	_ = os.WriteFile(multi, make([]byte, 64), 0o644)
	for _, path := range s.Paths("ok") {
		_ = os.WriteFile(path, make([]byte, 64), 0o644)
	}
	result := s.SeparateAll(context.Background(), []BatchItem{item})
	if result.Separated != 0 || result.Skipped != 1 || len(result.Failed) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestOptionsAndEmbeddedHelper(t *testing.T) {
	s := New(t.TempDir())
	if s.Provider() != "auto" || !s.Fast() {
		t.Fatalf("defaults = %q %t", s.Provider(), s.Fast())
	}
	s.SetOptions("cpu", false)
	if s.Provider() != "cpu" || s.Fast() {
		t.Fatalf("options = %q %t", s.Provider(), s.Fast())
	}
	if !bytes.Contains(helperSource, []byte("PROGRESS")) {
		t.Fatal("embedded helper lost its progress reporting")
	}
	if enabled := EnabledFromKeys("dv"); !enabled[0] || !enabled[3] || enabled[1] {
		t.Fatalf("keys = %v", enabled)
	}
}
