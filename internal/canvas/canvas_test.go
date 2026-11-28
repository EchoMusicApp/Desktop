package canvas

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPathCacheAndStaleCleanup(t *testing.T) {
	root := t.TempDir()
	s := New(root, nil)
	if got := s.Path("a/b"); got != filepath.Join(root, "a_b.canvas.gif") {
		t.Fatalf("path = %q", got)
	}
	if s.CachedPath("song") != "" {
		t.Fatal("missing clip reported as cached")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := s.Path("song")
	if err := os.WriteFile(path, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	if s.CachedPath("song") != path {
		t.Fatal("valid clip not detected")
	}
	stale := s.Path("old")
	if err := os.WriteFile(stale, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	s.CleanupStale()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale clip still present: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fresh clip removed: %v", err)
	}
}

func TestEnsureWithoutClientFailsCleanly(t *testing.T) {
	s := New(t.TempDir(), nil)
	if _, err := s.Ensure(context.Background(), "song", 200); err == nil {
		t.Fatal("expected an error when no YouTube client is configured")
	}
	if _, err := s.Ensure(context.Background(), "", 200); err == nil {
		t.Fatal("expected an error for an empty video ID")
	}
}

func TestClipSettings(t *testing.T) {
	if clipSeconds != 5 || clipFPS != 10 || clipWidth != 480 {
		t.Fatalf("clip settings: %d %d %d", clipSeconds, clipFPS, clipWidth)
	}
}
