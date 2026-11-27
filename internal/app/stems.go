package app

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/stems"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

var stemColors = [4]ui.Color{
	ui.RGB(245, 158, 11), // drums
	ui.RGB(168, 85, 247), // bass
	ui.RGB(20, 184, 166), // other
	ui.RGB(237, 85, 100), // vocals
}

var stemShortNames = [4]string{"DRM", "BAS", "OTH", "VOC"}
var stemKeys = [4]ui.Key{ui.KeyD, ui.KeyB, ui.KeyI, ui.KeyV}

// initStems wires the separation service to the app cache directory. It is
// called whenever the audio cache root changes.
func (a *app) initStems(root string) {
	if root == "" {
		return
	}
	dir := a.settings.StemsDirectory
	if dir == "" {
		dir = filepath.Join(root, "Echo Music Stems")
	}
	a.stems = stems.New(dir)
	a.stems.SetOptions(a.settings.StemsProvider, a.settings.StemsFast)
	a.stems.CleanupStale()
	a.stems.Progress = func(videoID string, chunk, total int) {
		a.update(func() {
			a.stemsStatus = "Separating " + videoID + ": " +
				itoa(chunk) + "/" + itoa(total)
		})
	}
	a.stems.Setup = func(text string, fraction float64) {
		a.update(func() { a.stemsStatus = text })
		_ = fraction
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := make([]byte, 0, 12)
	for v > 0 {
		digits = append(digits, byte('0'+v%10))
		v /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

// stemPlaybackPath returns a mixed WAV for the current track when global stem
// mode is on and the track was separated.
func (a *app) stemPlaybackPath(item youtube.MusicItem) string {
	if !a.settings.Stems || a.stems == nil || item.VideoID == "" {
		return ""
	}
	if !a.stems.Cached(item.VideoID) {
		return ""
	}
	path, err := a.stems.PlaybackPaths(item.VideoID, a.stemsMask)
	if err != nil {
		a.notice = "Stems: " + err.Error()
		return ""
	}
	return path
}

// applyStems restarts playback on a freshly mixed stem file at the same
// position. Queue, automix and next-track logic keep using the normal player.
func (a *app) applyStems() {
	if a.stems == nil || a.current.VideoID == "" {
		return
	}
	path := a.stemPlaybackPath(a.current)
	if path == "" {
		return
	}
	at, playing := a.player.Position(), a.player.Playing()
	if err := a.player.PlayAt(path, at, !playing); err != nil {
		a.notice = "Stems: " + err.Error()
	}
}

func (a *app) setStems(on bool) {
	a.settings.Stems = on
	a.saveSettings()
	if !on {
		a.applyStems()
		return
	}
	if a.stems == nil {
		return
	}
	a.run(func() {
		if err := a.stems.Ensure(context.Background()); err != nil {
			a.update(func() { a.notice = "Stems setup: " + err.Error() })
			return
		}
		a.update(func() { a.stemsStatus = "Stems ready" })
	})
	if a.stems.Cached(a.current.VideoID) {
		a.applyStems()
	}
}

func (a *app) toggleStem(index int) {
	if index < 0 || index >= len(a.stemsMask) {
		return
	}
	a.stemsMask[index] = !a.stemsMask[index]
	a.applyStems()
}

// stemHoldKeys implements tap-to-toggle and hold-to-mute for D/B/I/V.
// Holding longer than 250 ms mutes only that stem until the key is released,
// exactly like the Android/iOS style hold gesture.
func (a *app) stemHoldKeys(c *ui.Context) {
	// Do not mark keys as "read" unless the beta is actually active: that
	// would hide shortcut hints and change unrelated UI tests.
	if !a.settings.Stems || a.stems == nil || !a.stems.Cached(a.current.VideoID) {
		return
	}
	now := time.Now()
	for i, key := range stemKeys {
		down := c.KeyDown(key)
		if down && a.stemHold[i].IsZero() {
			a.stemHold[i] = now
			continue
		}
		if !down && !a.stemHold[i].IsZero() {
			if a.stemsHoldActive[i] {
				a.stemsMask[i] = a.stemsHoldSaved[i]
				a.stemsHoldActive[i] = false
				a.applyStems()
			} else {
				a.toggleStem(i)
			}
			a.stemHold[i] = time.Time{}
		}
		if down && !a.stemsHoldActive[i] && now.Sub(a.stemHold[i]) >= 250*time.Millisecond {
			a.stemsHoldSaved[i] = a.stemsMask[i]
			a.stemsMask[i] = false
			a.stemsHoldActive[i] = true
			a.applyStems()
		}
	}
}

// separateAll separates every playable item on the current page. Downloads
// are supplied from the existing audio resolver/cache.
func (a *app) separateAll() {
	if a.stems == nil || a.stemsTask.busy() {
		return
	}
	items := make([]youtube.MusicItem, len(a.page.playable))
	copy(items, a.page.playable)
	// Global feedback: the batch can run for hours and stays visible after
	// leaving the playlist page.
	a.notice = "Separating " + itoa(len(items)) + " tracks in the background…"
	a.stemsStatus = a.notice
	startTask(a, &a.stemsTask, noTimeout, func(ctx context.Context) (stems.BatchResult, error) {
		batch := make([]stems.BatchItem, 0, len(items))
		for _, item := range items {
			playable := item
			batch = append(batch, stems.BatchItem{
				VideoID: playable.VideoID,
				Download: func(ctx context.Context, videoID string) (string, error) {
					return a.downloadForStems(ctx, playable)
				},
			})
		}
		result := a.stems.SeparateAll(ctx, batch)
		return result, nil
	}, func(result stems.BatchResult, err error) {
		if err != nil {
			a.notice = "Stem batch: " + err.Error()
			return
		}
		summary := "Stem batch finished: " + itoa(result.Separated) +
			" separated, " + itoa(result.Skipped) + " already ready, " +
			itoa(len(result.Failed)) + " failed"
		a.stemsStatus = summary
		a.notice = summary
		if len(result.Failed) > 0 {
			a.notice += " — " + result.Failed[0]
			a.noticeHelp = true
		} else {
			a.noticeHelp = false
		}
	})
}

// separateCurrent separates the song currently playing, so stems become
// available without having to open a playlist first.
func (a *app) separateCurrent() {
	if a.stems == nil || a.stemsTask.busy() || a.current.VideoID == "" {
		return
	}
	item := a.current
	a.notice = "Separating stems for " + item.Title + "…"
	a.stemsStatus = a.notice
	startTask(a, &a.stemsTask, noTimeout, func(ctx context.Context) (stems.BatchResult, error) {
		source, err := a.downloadForStems(ctx, item)
		if err != nil {
			return stems.BatchResult{}, err
		}
		if err := a.stems.Separate(ctx, item.VideoID, source); err != nil {
			return stems.BatchResult{}, err
		}
		return stems.BatchResult{Separated: 1}, nil
	}, func(result stems.BatchResult, err error) {
		if err != nil {
			a.stemsStatus = "Stem separation failed: " + err.Error()
			a.notice = a.stemsStatus
			a.noticeHelp = true
			return
		}
		a.stemsStatus = "Stems ready for " + item.Title
		a.notice = a.stemsStatus
		a.noticeHelp = false
		a.applyStems()
	})
}

func (a *app) downloadForStems(ctx context.Context, item youtube.MusicItem) (string, error) {
	if local := a.downloaded(item.VideoID); local != "" {
		return local, nil
	}
	found, err := findAudio(ctx, a.streams, item, "", a.currentAudioCache())
	if err != nil {
		return "", err
	}
	return found.url, nil
}

// stemChips draws the four labelled stem toggles on the full-screen player.
func (a *app) stemChips(c *ui.Context) {
	if !a.settings.Stems || a.stems == nil || !a.stems.Cached(a.current.VideoID) {
		return
	}
	sc := m3.Of(c).Scheme
	ui.Row(c.Key("stem-chips")).Gap(6).Wrap().Justify(ui.Center).Children(func() {
		for i, name := range stemShortNames {
			index := i
			on := a.stemsMask[i]
			chip := ui.ButtonBase(c.Key("stem-" + name))
			chip.Height(28).Padding(6, 10).Radius(m3.Full).Gap(6).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).
				Background(m3.StateFill(sc.SurfaceContainerHighest.Alpha(0.8), sc.OnSurface, chip.Hovered(), chip.Pressed(), chip.FocusVisible())).
				Border(1, stemColors[i].Alpha(map[bool]float32{true: 0.95, false: 0.35}[on])).
				Label("Toggle " + name + " stem")
			chip.Children(func() {
				ui.Box(c.Key("stem-dot-" + name)).Width(7).Height(7).Radius(m3.Full).
					Background(stemColors[i].Alpha(map[bool]float32{true: 1, false: 0.3}[on]))
				m3.Text(c, m3.LabelSmall, name).TextColor(
					map[bool]ui.Color{true: sc.OnSurface, false: sc.OnSurfaceVariant}[on])
			})
			if chip.Clicked() {
				a.toggleStem(index)
			}
		}
	})
}

func (a *app) stemsCacheRoot() string {
	if a.settings.CacheDirectory != "" {
		return a.settings.CacheDirectory
	}
	if root, err := mygo.App.Path(mygo.PathCache); err == nil {
		return root
	}
	return ""
}

func (a *app) chooseStemsDirectory() {
	current := a.settings.StemsDirectory
	if current == "" {
		current = filepath.Join(a.stemsCacheRoot(), "Echo Music Stems")
	}
	type chosen struct{ root, err string }
	spawn(a, func() chosen {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: a.desktop.window(), Title: "Choose stems folder",
			DefaultPath: current, Directory: true, CreateDirectories: true,
		})
		if err != nil {
			return chosen{err: "Could not open the folder picker: " + err.Error()}
		}
		if len(paths) == 0 {
			return chosen{}
		}
		root, err := filepath.Abs(paths[0])
		if err != nil {
			return chosen{err: "Could not use that folder: " + err.Error()}
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return chosen{err: "Could not use that folder: " + err.Error()}
		}
		return chosen{root: root}
	}, func(c chosen) {
		if c.err != "" {
			a.stemsDirError = c.err
			return
		}
		if c.root == "" {
			return
		}
		a.settings.StemsDirectory = c.root
		a.stemsDirError = ""
		a.saveSettings()
		a.initStems(a.stemsCacheRoot())
	})
}

func (a *app) setStemsProvider(provider string) {
	a.settings.StemsProvider = provider
	if a.stems != nil {
		a.stems.SetOptions(provider, a.settings.StemsFast)
	}
	a.saveSettings()
}

// stemsCard exposes the beta settings in Playback & Sound.
func (a *app) stemsCard(c *ui.Context) {
	sc := m3.Of(c).Scheme
	a.settingsCard(c, "Stems (beta)", m3.IconMusicNote, func() {
		ui.Column(c).Gap(12).Children(func() {
			settingLabel(c, "Use separated stems", "Separate songs locally with HT-Demucs ONNX. Four stem files are cached in Echo Music Stems and mixed into the track you hear; playback keeps driving automix and next track.")
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				on := a.settings.Stems
				if m3.Switch(c, &on, "Use separated stems").Changed() {
					a.setStems(on)
				}
			})
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				on := a.settings.VideoCanvas
				if m3.Switch(c, &on, "Video canvas behind the full-screen player").Changed() {
					a.settings.VideoCanvas = on
					a.saveSettings()
					if !on {
						a.canvasPlayer = nil
					} else if a.current.VideoID != "" {
						a.run(func() { a.loadCanvas(a.current) })
					}
				}
				settingLabel(c, "Video canvas (beta)", "Loop a muted five-second clip from the song's video behind the full-screen player. Cached locally; depends on the codecs installed with the app.").Grow(1)
			})
			settingLabel(c, "Separation speed", "Fast uses a 10% overlap; Quality uses 25% and takes longer.")
			ui.Row(c).Gap(8).Wrap().Children(func() {
				for _, option := range []struct {
					label string
					fast  bool
				}{{"Fast", true}, {"Quality", false}} {
					opt := option
					if m3.Button(c, m3.ButtonSpec{
						Label: opt.label,
						Kind:  map[bool]m3.ButtonKind{true: m3.Tonal, false: m3.Outlined}[a.settings.StemsFast == opt.fast],
						Size:  m3.Small40,
						Key:   "stems-speed-" + opt.label,
					}).Clicked() {
						a.settings.StemsFast = opt.fast
						if a.stems != nil {
							a.stems.SetOptions(a.settings.StemsProvider, opt.fast)
						}
						a.saveSettings()
					}
				}
			})
			settingLabel(c, "GPU acceleration", "Auto prefers DirectML or CUDA when ONNX Runtime finds them, otherwise CPU.")
			providers := []string{"auto", "cpu", "dml", "cuda"}
			providerLabels := []string{"Auto", "CPU", "DirectML", "CUDA"}
			picked := 0
			for i, provider := range providers {
				if provider == a.settings.StemsProvider {
					picked = i
				}
			}
			if m3.ButtonGroup(c, "stems-provider", &picked, providerLabels, nil) {
				a.setStemsProvider(providers[picked])
			}
			settingLabel(c, "Stems folder", "The model and separated files are stored here. Changing the folder creates a new cache; existing files are not moved.")
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				location := a.settings.StemsDirectory
				if location == "" {
					location = filepath.Join(a.stemsCacheRoot(), "Echo Music Stems")
				}
				m3.Text(c, m3.BodyMedium, location).TextColor(sc.OnSurfaceVariant).MinWidth(0).Grow(1).SingleLine()
				if m3.IconButton(c, m3.IconButtonSpec{Icon: m3.IconFolder, Label: "Choose stems folder", Kind: m3.OutlinedIcon, Size: m3.Small40, Key: "stems-folder"}).Clicked() {
					a.chooseStemsDirectory()
				}
			})
			if a.stemsDirError != "" {
				m3.Text(c, m3.BodySmall, a.stemsDirError).TextColor(sc.Error)
			}
			if a.stemsStatus != "" {
				m3.Text(c, m3.BodySmall, a.stemsStatus).TextColor(sc.OnSurfaceVariant)
			}
		})
	})
}
