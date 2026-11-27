package app

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/EchoMusicApp/Echo-Music/internal/playstate"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

type episodeProgressSync struct {
	item         youtube.MusicItem
	client       *youtube.Client
	session      *youtube.PlaybackSession
	duration     time.Duration
	lastReported time.Duration
	lastPosition time.Duration
	lastSample   time.Time
	pendingTo    time.Duration
	seekVersion  uint64
	pending      bool
	finished     bool
	finalSent    bool
	finishAt     time.Duration
}

const episodeProgressInterval = 20 * time.Second

const episodeSeekTolerance = 2 * time.Second

const episodeProgressTimeout = 15 * time.Second

func (a *app) reportPlayback(item youtube.MusicItem) {
	if !a.signedIn() {
		return
	}
	client := a.client()
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), upNextTimeout)
		defer cancel()
		if err := client.ReportPlayback(ctx, item.VideoID); err != nil {
			log.Printf("playback: could not report video_id=%s: %v", item.VideoID, err)
		}
	})
}

func episodeProgressPosition(item youtube.MusicItem, duration time.Duration) time.Duration {
	if !item.IsEpisode() || item.ProgressPercent <= 0 || item.ProgressPercent >= 100 || duration <= 0 {
		return 0
	}
	position := time.Duration(int64(duration) * int64(item.ProgressPercent) / 100)
	if position < episodeStartGuard || duration-position < episodeEndGuard {
		return 0
	}
	return position
}

func (a *app) beginEpisodeProgress(item youtube.MusicItem, position, duration time.Duration) {
	if !a.signedIn() || !item.IsEpisode() {
		return
	}
	if a.episodeProgress != nil {
		a.finishEpisodeProgress(false)
	}
	state := &episodeProgressSync{
		item: item, client: a.client(), duration: duration, lastReported: position,
		lastPosition: position, lastSample: time.Now(), seekVersion: a.seekVersion,
		pending: true,
	}
	a.episodeProgress = state
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), episodeProgressTimeout)
		defer cancel()
		session, err := state.client.BeginEpisodePlayback(ctx, item.VideoID, position, duration)
		a.update(func() {
			state.pending = false
			if err != nil {
				log.Printf("playback: could not start episode progress video_id=%s: %v", item.VideoID, err)
				if a.episodeProgress == state {
					a.episodeProgress = nil
				}
				return
			}
			state.session = session
			if state.finished {
				a.sendEpisodeProgressFinal(state)
				return
			}
			if a.episodeProgress != state {
				return
			}
		})
	})
}

func (a *app) syncEpisodeProgress() {
	state := a.episodeProgress
	if state == nil || state.finished || state.session == nil || state.pending || !a.player.Playing() {
		return
	}
	now, position := time.Now(), a.player.Position()
	if state.seekVersion != a.seekVersion {
		state.seekVersion = a.seekVersion
		state.lastReported, state.lastPosition, state.lastSample = position, position, now
		return
	}
	elapsed := now.Sub(state.lastSample)
	advanced := position - state.lastPosition
	state.lastPosition, state.lastSample = position, now
	if advanced < 0 || advanced > elapsed+episodeSeekTolerance {
		state.lastReported = position
		return
	}
	if position-state.lastReported < episodeProgressInterval {
		return
	}
	from, to := state.lastReported, position
	state.pending = true
	state.pendingTo = to
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), episodeProgressTimeout)
		defer cancel()
		err := state.session.ReportWatchtime(ctx, from, to, state.duration, "playing", false)
		a.update(func() {
			state.pending = false
			state.pendingTo = 0
			if err != nil {
				log.Printf("playback: could not report episode progress video_id=%s: %v", state.item.VideoID, err)
			} else {
				state.lastReported = to
			}
			if state.finished {
				a.sendEpisodeProgressFinal(state)
			}
		})
	})
}

func (a *app) finishEpisodeProgress(wait bool) {
	state := a.episodeProgress
	if state == nil {
		return
	}
	state.finished = true
	state.finishAt = max(a.playbackPosition(), state.lastReported)
	a.episodeProgress = nil
	if state.session == nil || state.pending {
		if !wait {
			return
		}
	}
	if state.session == nil || state.finalSent {
		return
	}
	from, to := state.lastReported, state.finishAt
	if state.pending && state.session != nil {
		from = max(from, state.pendingTo)
	}
	if to < from {
		to = from
	}
	if wait {
		state.finalSent = true
		result := make(chan error, 1)
		a.run(func() {
			ctx, cancel := context.WithTimeout(context.Background(), episodeProgressTimeout)
			defer cancel()
			result <- state.session.ReportWatchtime(ctx, from, to, state.duration, "paused", true)
		})
		if err := <-result; err != nil {
			log.Printf("playback: could not finish episode progress video_id=%s: %v", state.item.VideoID, err)
		}
		return
	}
	a.sendEpisodeProgressFinal(state)
}

func (a *app) sendEpisodeProgressFinal(state *episodeProgressSync) {
	if state.session == nil || state.finalSent {
		return
	}
	from, to := state.lastReported, max(state.finishAt, state.lastReported)
	state.finalSent = true
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), episodeProgressTimeout)
		defer cancel()
		if err := state.session.ReportWatchtime(ctx, from, to, state.duration, "paused", true); err != nil {
			log.Printf("playback: could not finish episode progress video_id=%s: %v", state.item.VideoID, err)
		}
	})
}

func (a *app) leaveEpisode() {
	item := a.current
	if !item.IsEpisode() {
		return
	}
	position := a.playbackPosition()
	a.takeEpisode(item.VideoID)
	if position < episodeStartGuard || (a.total > 0 && position > a.total-episodeEndGuard) {
		return
	}
	show := item.Artist
	if show == "" {
		show = item.Subtitle
	}
	a.episodes = slices.Insert(a.episodes, 0, playstate.Episode{
		VideoID:   item.VideoID,
		Position:  position,
		Duration:  a.total,
		Title:     item.Title,
		Show:      show,
		Thumbnail: item.Thumbnail,
	})
	if a.page != nil {
		a.page.rowsDirty = true
	}
	a.episodes = a.episodes[:min(len(a.episodes), playstate.MaxEpisodes)]
}

func (a *app) takeEpisode(videoID string) time.Duration {
	i := slices.IndexFunc(a.episodes, func(e playstate.Episode) bool { return e.VideoID == videoID })
	if i < 0 {
		return 0
	}
	position := a.episodes[i].Position
	a.episodes = slices.Delete(a.episodes, i, i+1)
	if a.page != nil {
		a.page.rowsDirty = true
	}
	return position
}

func (a *app) hydrateEpisodes() {
	if len(a.episodes) == 0 {
		return
	}
	if a.episodesHydrated == nil {
		a.episodesHydrated = make(map[string]bool)
	}
	var missing []string
	for _, ep := range a.episodes {
		if ep.VideoID == "" || ep.Position <= 0 {
			continue
		}
		if (ep.Title == "" || ep.Thumbnail == "") && !a.episodesHydrated[ep.VideoID] {
			a.episodesHydrated[ep.VideoID] = true
			missing = append(missing, ep.VideoID)
		}
	}
	if len(missing) == 0 {
		return
	}
	client := a.client()
	spawn(a, func() []youtube.MusicItem {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var out []youtube.MusicItem
		for start := 0; start < len(missing); start += 25 {
			end := min(start+25, len(missing))
			items, err := client.GetQueue(ctx, missing[start:end])
			if err != nil {
				continue
			}
			out = append(out, items...)
		}
		return out
	}, func(items []youtube.MusicItem) {
		if len(items) == 0 {
			return
		}
		byID := make(map[string]youtube.MusicItem, len(items))
		for _, it := range items {
			if it.VideoID != "" {
				byID[it.VideoID] = it
			}
		}
		changed := false
		for i := range a.episodes {
			ep := &a.episodes[i]
			it, ok := byID[ep.VideoID]
			if !ok {
				continue
			}
			if ep.Title == "" && it.Title != "" {
				ep.Title = it.Title
				changed = true
			}
			if ep.Show == "" {
				show := it.Artist
				if show == "" {
					show = it.Subtitle
				}
				if show != "" {
					ep.Show = show
					changed = true
				}
			}
			if ep.Thumbnail == "" && it.Thumbnail != "" {
				ep.Thumbnail = it.Thumbnail
				changed = true
			}
			if ep.Duration <= 0 && it.Duration != "" {
				if d := parseDurationLabel(it.Duration); d > 0 {
					ep.Duration = d
					changed = true
				}
			}
		}
		if changed {
			if a.playSave.writer.Path != "" {
				state := a.queue.state(a.playbackPosition())
				state.Episodes = a.episodes
				a.playSave.writer.Save(state)
			}
			if a.page != nil {
				a.page.rowsDirty = true
			}
		}
	})
}

func parseDurationLabel(s string) time.Duration {
	var total int64
	for part := range strings.SplitSeq(strings.TrimSpace(s), ":") {
		var n int64
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				return 0
			}
			n = n*10 + int64(ch-'0')
		}
		total = total*60 + n
	}
	return time.Duration(total) * time.Second
}
