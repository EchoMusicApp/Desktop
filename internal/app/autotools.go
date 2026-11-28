package app

import (
	"context"
	"path/filepath"

	"github.com/EchoMusicApp/Echo-Music/internal/autotools"
)

// setupMediaTools downloads ffmpeg, yt-dlp and the JS runtime once when they
// are not already on PATH, so playback works out of the box in minimal builds.
func (a *app) setupMediaTools() {
	root := a.stemsCacheRoot()
	if root == "" {
		return
	}
	dir := filepath.Join(root, "Echo Music Tools")
	a.run(func() {
		err := autotools.Ensure(context.Background(), dir, func(text string) {
			a.update(func() { a.toolsStatus = text })
		})
		a.update(func() {
			if err != nil {
				a.notice = "Media tools setup: " + err.Error()
				a.toolsStatus = ""
				return
			}
			a.toolsStatus = ""
		})
	})
}
