package app

import (
	"context"
	"time"

	"github.com/EchoMusicApp/Echo-Music/internal/automix"
	"github.com/EchoMusicApp/Echo-Music/internal/settings"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

func (a *app) initAutomix(root string) {
	if root == "" {
		return
	}
	a.automix = automix.New(root + "/Echo Music Automix Cache")
}

func (a *app) automixEnabled() bool {
	return a.settings.Crossfade || a.settings.AutomixBeatAlign || a.settings.AutomixHarmonic
}

// analyzeCurrent computes the beat grid and key of the playing song in the
// background. Failures only clear the automix extras, never playback.
func (a *app) analyzeCurrent(item youtube.MusicItem, source string) {
	if a.automix == nil || !a.automixEnabled() || item.VideoID == "" {
		return
	}
	started := time.Now()
	result, err := a.automix.AnalyzePath(context.Background(), item.VideoID, source,
		int(parseDuration(item.Duration).Milliseconds()))
	if err != nil {
		a.update(func() { a.automixStatus = "Automix: " + err.Error() })
		return
	}
	a.automix.RecordPreparation(time.Since(started))
	a.update(func() {
		a.beatCurrent = result
		label := result.KeyLabel()
		if label != "" {
			label += " · "
		}
		a.automixStatus = label + itoa(int(result.BPM)) + " BPM"
		a.applyAutomixCrossfade()
	})
}

// analyzeAhead prepares the beat grid of a downloaded next track so harmonic
// queue selection has something to work with.
func (a *app) analyzeAhead(item youtube.MusicItem) {
	if a.automix == nil || !a.settings.AutomixHarmonic || a.automix.Cached(item.VideoID) != nil {
		return
	}
	path := a.downloaded(item.VideoID)
	if path == "" {
		return
	}
	a.run(func() {
		_, _ = a.automix.AnalyzePath(context.Background(), item.VideoID, path,
			int(parseDuration(item.Duration).Milliseconds()))
	})
}

// applyAutomixCrossfade uses a 16-beat blend when beat alignment is enabled,
// or zero overlap in gapless mode.
func (a *app) applyAutomixCrossfade() {
	if !a.settings.Crossfade {
		return
	}
	if a.settings.AutomixGapless {
		a.player.SetCrossfade(0)
		return
	}
	ms := settings.ClampCrossfade(a.settings.CrossfadeSeconds) * 1000
	if a.settings.AutomixBeatAlign && a.beatCurrent != nil {
		ms = automix.BlendDurationMs(a.beatCurrent, 16, 4000, 15000)
	}
	a.player.SetCrossfade(time.Duration(ms) * time.Millisecond)
}

// harmonicallyNext picks a compatible upcoming track from analyses already
// cached. With no analysis it returns the natural next index unchanged.
func (a *app) harmonicallyNext(next int, candidates []int) int {
	if a.automix == nil || !a.settings.AutomixHarmonic || a.beatCurrent == nil {
		return next
	}
	from := automix.CamelotFor(a.beatCurrent)
	if from == nil {
		return next
	}
	keys := make(map[int]*automix.CamelotKey, len(candidates)+1)
	keys[next] = from
	for _, index := range candidates {
		result := a.automix.Cached(a.queue.at(index).VideoID)
		keys[index] = automix.CamelotFor(result)
	}
	return automix.PickHarmonicNext(next, candidates, from, keys)
}
