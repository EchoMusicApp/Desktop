package autotools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtendPathIsIdempotent(t *testing.T) {
	original := os.Getenv("PATH")
	defer os.Setenv("PATH", original)
	dir := filepath.Join(t.TempDir(), "tools")
	os.Setenv("PATH", "/usr/bin")
	extendPath(dir)
	extendPath(dir)
	got := os.Getenv("PATH")
	if !strings.HasPrefix(got, dir) || strings.Count(got, dir) != 1 {
		t.Fatalf("PATH = %q", got)
	}
}

func TestDownloadWritesAndValidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("echo", 2048)))
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "tool")
	if err := download(context.Background(), server.URL, target); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Size() < 4096 {
		t.Fatalf("download = %v %v", info, err)
	}
	if _, err := os.Stat(target + ".part"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
}

func TestEnsureRejectsEmptyDirectory(t *testing.T) {
	if err := Ensure(context.Background(), "", nil); err == nil {
		t.Fatal("expected an error for an empty directory")
	}
}
