package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

func feedCachePath(path string) string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	name := strings.Trim(path, "/")
	if name == "" {
		name = "home"
	}
	name = strings.ReplaceAll(name, "/", "_")
	return filepath.Join(dir, "Echo Music", "feeds", name+".json")
}

func isCachedFeedRoute(id routeID) bool {
	return id == routeHome || id == routePodcasts || id == routeExplore
}

func readFeedCache(path string) *youtube.BrowseResult {
	file := feedCachePath(path)
	if file == "" {
		return nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var res youtube.BrowseResult
	if err := json.Unmarshal(data, &res); err != nil || len(res.Sections) == 0 {
		return nil
	}
	return &res
}

func writeFeedCache(path string, res *youtube.BrowseResult) {
	if res == nil || len(res.Sections) == 0 {
		return
	}
	file := feedCachePath(path)
	if file == "" {
		return
	}
	data, err := json.Marshal(res)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(file), 0o755)
	_ = os.WriteFile(file, data, 0o644)
}

func (a *app) warmFeedThumbnails(res *youtube.BrowseResult) {
	if res == nil || a.thumbs == nil {
		return
	}
	limit := 16
	for i := 0; i < len(res.Sections) && i < 3 && limit > 0; i++ {
		for _, item := range res.Sections[i].Items {
			if item.Thumbnail == "" {
				continue
			}
			if item.IsWide() {
				a.thumbs.WideBitmap(item.Thumbnail, 512)
			} else {
				a.thumbs.Bitmap(item.Thumbnail, 512)
			}
			limit--
			if limit <= 0 {
				break
			}
		}
	}
}
