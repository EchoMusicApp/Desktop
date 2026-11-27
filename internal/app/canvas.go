package app

import (
	"context"
	"log"

	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/canvas"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

// initCanvas wires the opt-in video backdrop cache beside the audio cache.
func (a *app) initCanvas(root string) {
	if root == "" {
		return
	}
	a.canvas = canvas.New(root+"/Echo Music Canvas Cache", a.canvasClient())
	a.canvas.CleanupStale()
}

func (a *app) canvasClient() *youtube.Client {
	return a.client()
}

// loadCanvas prepares a muted looping clip from the song's official video.
// Nothing here touches the audio player or its stream resolver.
func (a *app) loadCanvas(item youtube.MusicItem) {
	if !a.settings.VideoCanvas || a.canvas == nil || item.VideoID == "" {
		a.update(func() { a.canvasPlayer = nil })
		return
	}
	if a.canvasID == item.VideoID && a.canvasPlayer != nil {
		return
	}
	path, err := a.canvas.Ensure(context.Background(), item.VideoID, parseDuration(item.Duration).Seconds())
	if err != nil {
		log.Printf("canvas: %s: %v", item.VideoID, err)
		a.update(func() {
			a.canvasPlayer = nil
			a.notice = "Video canvas: " + err.Error()
		})
		return
	}
	data, err := a.canvas.Read(path)
	if err != nil {
		a.update(func() { a.canvasPlayer = nil })
		return
	}
	decoder, err := ui.NewGIFDecoder(data)
	if err != nil {
		a.update(func() { a.canvasPlayer = nil })
		return
	}
	player, err := ui.NewVideoPlayer(decoder)
	if err != nil {
		a.update(func() { a.canvasPlayer = nil })
		return
	}
	player.SetLoop(true)
	a.update(func() {
		a.canvasPlayer = player
		a.canvasID = item.VideoID
	})
}

// canvasVisible reports whether a clip is ready for the current full-screen
// song. The clip only renders while audio is playing.
func (a *app) canvasVisible() bool {
	return a.settings.VideoCanvas && a.canvasPlayer != nil &&
		a.canvasID == a.current.VideoID && a.player.Playing()
}
