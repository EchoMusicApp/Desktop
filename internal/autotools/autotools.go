// Package autotools downloads the media tools the app needs when they are not
// already available, so a minimal release behaves like a full one. Downloads
// go into a private app directory that is prepended to PATH at runtime.
package autotools

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/EchoMusicApp/Echo-Music/internal/proc"
)

const (
	ytdlpURL  = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp"
	qjsURL    = "https://github.com/quickjs-ng/quickjs/releases/download/v0.17.0/qjs-windows-x86_64.exe"
	ffmpegURL = "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip"
)

// Ensure makes ffmpeg, yt-dlp and a JS runtime discoverable through proc.Tool.
// Progress reports one short status line per tool.
func Ensure(ctx context.Context, dir string, progress func(string)) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("autotools: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	defer func() {
		for _, entry := range []string{dir, filepath.Join(dir, "ffmpeg-download.zip")} {
			_ = os.Remove(entry)
		}
	}()

	if err := ensureYTDLP(ctx, dir, progress); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if err := ensureQJS(ctx, dir, progress); err != nil {
			return err
		}
		if err := ensureFFmpegWindows(ctx, dir, progress); err != nil {
			return err
		}
	} else {
		for _, name := range []string{"ffmpeg"} {
			if _, err := proc.Tool(name); err != nil {
				return fmt.Errorf("autotools: %s is not installed; install it with your package manager", name)
			}
		}
	}
	extendPath(dir)
	return nil
}

func extendPath(dir string) {
	path := os.Getenv("PATH")
	for _, entry := range filepath.SplitList(path) {
		if filepath.Clean(entry) == filepath.Clean(dir) {
			return
		}
	}
	if path == "" {
		_ = os.Setenv("PATH", dir)
	} else {
		_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+path)
	}
}

func hasTool(name string) bool {
	_, err := proc.Tool(name)
	return err == nil
}

func ensureYTDLP(ctx context.Context, dir string, progress func(string)) error {
	if hasTool("yt-dlp") {
		return nil
	}
	progress("Downloading yt-dlp…")
	name := "yt-dlp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	url := ytdlpURL
	if runtime.GOOS == "windows" {
		url += ".exe"
	}
	target := filepath.Join(dir, name)
	if err := download(ctx, url, target); err != nil {
		return err
	}
	return os.Chmod(target, 0o755)
}

func ensureQJS(ctx context.Context, dir string, progress func(string)) error {
	if hasTool("qjs") {
		return nil
	}
	progress("Downloading the JavaScript runtime…")
	target := filepath.Join(dir, "qjs.exe")
	if err := download(ctx, qjsURL, target); err != nil {
		return err
	}
	return os.Chmod(target, 0o755)
}

func ensureFFmpegWindows(ctx context.Context, dir string, progress func(string)) error {
	if hasTool("ffmpeg") {
		return nil
	}
	progress("Downloading ffmpeg (this happens once)…")
	archivePath := filepath.Join(dir, "ffmpeg-download.zip")
	if err := download(ctx, ffmpegURL, archivePath); err != nil {
		return err
	}
	defer os.Remove(archivePath)
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if !strings.HasSuffix(strings.ToLower(file.Name), "bin/ffmpeg.exe") {
			continue
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		target, err := os.OpenFile(filepath.Join(dir, "ffmpeg.exe"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			source.Close()
			return err
		}
		_, err = io.Copy(target, source)
		target.Close()
		source.Close()
		return err
	}
	return errors.New("autotools: ffmpeg.exe not found in the downloaded archive")
}

func download(ctx context.Context, url, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Echo-Music-Desktop/1.0")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("autotools: %s answered HTTP %d", url, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(target+".part", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		os.Remove(target + ".part")
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(target + ".part")
		return err
	}
	info, err := os.Stat(target + ".part")
	if err != nil || info.Size() < 4096 {
		os.Remove(target + ".part")
		return fmt.Errorf("autotools: %s download is empty", url)
	}
	return os.Rename(target+".part", target)
}
