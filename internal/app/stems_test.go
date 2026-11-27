package app

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EchoMusicApp/Echo-Music/internal/settings"
	"github.com/EchoMusicApp/Echo-Music/internal/stems"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

func writeStemWAV(t *testing.T, path string, sample int16) {
	t.Helper()
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(46))
	out.WriteString("WAVEfmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(16))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint32(44100))
	_ = binary.Write(&out, binary.LittleEndian, uint32(88200))
	_ = binary.Write(&out, binary.LittleEndian, uint16(2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(16))
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(2))
	_ = binary.Write(&out, binary.LittleEndian, sample)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStemPlaybackUsesCachedMixAndMask(t *testing.T) {
	root := t.TempDir()
	service := stems.New(filepath.Join(root, "stems"))
	if err := os.MkdirAll(service.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	paths := service.Paths("song")
	for i, path := range paths {
		writeStemWAV(t, path, int16((i+1)*100))
	}
	if err := os.WriteFile(service.MultiPath("song"), make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &app{
		settings:  settings.Settings{Stems: true},
		stems:     service,
		stemsMask: [4]bool{true, true, true, true},
	}
	path := a.stemPlaybackPath(youtube.MusicItem{VideoID: "song"})
	if path == "" || !strings.Contains(filepath.Base(path), "mix-song") {
		t.Fatalf("stem playback path = %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	a.stemsMask[1] = false
	other := a.stemPlaybackPath(youtube.MusicItem{VideoID: "song"})
	if other == path {
		t.Fatal("changing the mask must produce a different mix")
	}
	a.settings.Stems = false
	if got := a.stemPlaybackPath(youtube.MusicItem{VideoID: "song"}); got != "" {
		t.Fatalf("disabled stems still returned %q", got)
	}
}

func TestStemDefaultsAndHelpers(t *testing.T) {
	s := settings.Defaults()
	if s.Stems || !s.StemsFast || s.StemsProvider != "auto" {
		t.Fatalf("defaults: %+v", s)
	}
	if got := itoa(0); got != "0" {
		t.Fatalf("itoa(0) = %q", got)
	}
	if got := itoa(1234); got != "1234" {
		t.Fatalf("itoa(1234) = %q", got)
	}
	if len(stemKeys) != 4 || stemShortNames[3] != "VOC" {
		t.Fatal("stem shortcut metadata is incomplete")
	}
}
