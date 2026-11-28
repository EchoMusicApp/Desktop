package app

import (
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/lyrics"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
)

type fullScreenState struct {
	open         bool
	lyricsMode   bool
	cover        coverState
	lyricsScroll ui.ScrollState
	followed     int
	center       bool
	artColor     ui.Color
	artColorFor  string
	artColorOK   bool
}

func (a *app) openFullScreen() {
	a.fullScreen.open = true
	a.fullScreen.followed = -1
	a.fullScreen.center = true
	a.loadLyrics()
	if a.desktop != nil && a.desktop.win != nil && !a.desktop.win.IsFullScreen() {
		a.desktop.win.SetFullScreen(true)
	}
}

func (a *app) closeFullScreen() {
	a.fullScreen.open = false
	if a.desktop != nil && a.desktop.win != nil && a.desktop.win.IsFullScreen() {
		a.desktop.win.SetFullScreen(false)
	}
}

func (a *app) fullScreenArtSeed(fallback ui.Color) ui.Color {
	fs := &a.fullScreen
	art := a.current.Thumbnail
	if art != "" && art != fs.artColorFor {
		if colour, has, ready := a.thumbs.Colour(art); ready {
			fs.artColorFor = art
			fs.artColorOK = has
			if has {
				fs.artColor = colour
			}
		}
	}
	if fs.artColorOK && fs.artColorFor == art {
		return fs.artColor
	}
	if a.theme.dynamicOK {
		return a.theme.dynamic
	}
	return fallback
}

func (a *app) fullScreenView(c *ui.Context) {
	base := a.resolveTheme(c)
	seed := a.fullScreenArtSeed(base.Seed)
	fsTheme := m3.New(m3.Config{Seed: seed, Mode: m3.Dark, Style: m3.Vibrant, Font: base.Font}, true)
	m3.Provide(c, fsTheme)
	sc := fsTheme.Scheme

	if c.Shortcut(0, ui.KeyEscape) {
		a.closeFullScreen()
		return
	}

	root := ui.Column(c.Key("fullscreen-player")).Absolute().Fill().
		Background(ui.RGB(10, 10, 14)).Label("Full screen player")
	root.Draw(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 {
			return
		}
		p.AnimationFrameRate(30)
		t := float64(p.Now().UnixNano()) / 1e9
		p.FillRadialGradient(r, ui.RadialGradient{
			Center: ui.Point{
				X: float32(0.28 + 0.16*math.Sin(t*0.26)),
				Y: float32(0.30 + 0.12*math.Cos(t*0.21)),
			},
			Radius: max(r.W, r.H) * 0.65,
			Oklab:  true,
			Stops: []ui.GradientStop{
				{Color: sc.Primary.Alpha(0.32), At: 0},
				{Color: sc.Primary.Alpha(0.12), At: 0.45},
				{Color: ui.Transparent, At: 1},
			},
		}, 0)
		p.FillRadialGradient(r, ui.RadialGradient{
			Center: ui.Point{
				X: float32(0.72 + 0.16*math.Cos(t*0.22)),
				Y: float32(0.68 + 0.12*math.Sin(t*0.25)),
			},
			Radius: max(r.W, r.H) * 0.60,
			Oklab:  true,
			Stops: []ui.GradientStop{
				{Color: sc.Tertiary.Alpha(0.26), At: 0},
				{Color: sc.Tertiary.Alpha(0.10), At: 0.48},
				{Color: ui.Transparent, At: 1},
			},
		}, 0)
		p.FillRadialGradient(r, ui.RadialGradient{
			Center: ui.Point{
				X: float32(0.50 + 0.18*math.Sin(t*0.19+1.1)),
				Y: float32(0.45 + 0.14*math.Cos(t*0.24+0.7)),
			},
			Radius: max(r.W, r.H) * 0.55,
			Oklab:  true,
			Stops: []ui.GradientStop{
				{Color: sc.Secondary.Alpha(0.20), At: 0},
				{Color: ui.Transparent, At: 1},
			},
		}, 0)
	})

	root.Children(func() {
		if a.canvasVisible() {
			ui.Box(c.Key("fs-canvas")).Absolute().Fill().PassThrough().Children(func() {
				ui.Video(c, a.canvasPlayer).Fill().Fit(ui.Cover)
			})
			ui.Box(c.Key("fs-canvas-scrim")).Absolute().Fill().PassThrough().
				Background(ui.RGB(10, 10, 14).Alpha(0.45))
		}
		ui.Row(c.Key("fs-topbar")).FillWidth().Padding(20, 28, 8).Justify(ui.SpaceBetween).AlignItems(ui.Center).Shrink(0).Children(func() {
			if m3.IconButton(c, m3.IconButtonSpec{
				Icon: m3.IconFullscreenExit, Label: "Exit full screen", Kind: m3.TonalIcon, Size: m3.Small40, Key: "fs-exit",
				Container: ui.RGB(255, 255, 255).Alpha(0.10),
			}).Clicked() {
				a.closeFullScreen()
			}
			picked := 0
			if a.fullScreen.lyricsMode {
				picked = 1
			}
			if m3.ButtonGroup(c, "fs-mode", &picked, []string{"Music art", "Lyrics"}, nil) {
				a.fullScreen.lyricsMode = picked == 1
				if a.fullScreen.lyricsMode {
					a.fullScreen.followed = -1
					a.fullScreen.center = true
					a.loadLyrics()
				}
			}
		})

		if a.fullScreen.lyricsMode {
			a.fullScreenLyrics(c)
			return
		}

		w, h := c.Size()
		ui.Column(c.Key("fs-art-wrap")).Grow(1).MinHeight(0).FillWidth().Center().Padding(0, pageGutter, 40).Gap(22).Children(func() {
			artRadius := float32(24)
			if a.current.IsWide() {
				artRadius = 20
			}
			artBox := ui.Box(c.Key("fs-art-hover")).Radius(artRadius).Clip().Cursor(ui.CursorPointer)
			px, py, hovered := artBox.PointerPosition()
			bounds := artBox.Bounds()
			targetScale, targetShine := float32(1.0), float32(0)
			targetRX, targetRY := float32(0), float32(0)
			shineX, shineY := float32(0.5), float32(0.5)
			if hovered && bounds.W > 0 && bounds.H > 0 {
				targetScale, targetShine = 1.05, 1
				nx := min(max(px/bounds.W, 0), 1)
				ny := min(max(py/bounds.H, 0), 1)
				shineX, shineY = nx, ny
				targetRY = (nx - 0.5) * 14
				targetRX = (0.5 - ny) * 14
			}
			scale := m3.Animate(artBox, "fs-hover-scale", targetScale, m3.SpatialDefault)
			shine := m3.Animate(artBox, "fs-hover-shine", targetShine, m3.EffectsDefault)
			rx := m3.Animate(artBox, "fs-hover-rx", targetRX, m3.SpatialFast)
			ry := m3.Animate(artBox, "fs-hover-ry", targetRY, m3.SpatialFast)
			sx := m3.Animate(artBox, "fs-shine-x", shineX, m3.SpatialFast)
			sy := m3.Animate(artBox, "fs-shine-y", shineY, m3.SpatialFast)
			artBox.Perspective(900).TransformOrigin(0.5, 0.5).RotateX(rx).RotateY(ry).Scale(scale, scale)
			m3.Elevation(c, artBox, 4)

			artBox.Children(func() {
				if a.current.IsWide() {
					wideW := min(max(w*0.52, 320), 720)
					wideH := wideW * 9 / 16
					if wideH > h*0.58 {
						wideH = max(h*0.58, 180)
						wideW = wideH * 16 / 9
					}
					a.playerArtworkRect(&a.fullScreen.cover, c, a.current.Thumbnail, 1024, wideW, wideH, artRadius)
				} else {
					artSize := min(min(w*0.42, h*0.54), 460)
					artSize = max(artSize, 200)
					a.playerArtwork(&a.fullScreen.cover, c, a.current.Thumbnail, 1024, artSize, artRadius)
				}
				if shine > 0.01 {
					ui.Box(c.Key("fs-art-shine")).Absolute().Fill().Radius(artRadius).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
						p.Clip(r, artRadius, func() {
							p.FillRadialGradient(r, ui.RadialGradient{
								Center: ui.Point{X: sx, Y: sy},
								Radius: max(r.W, r.H) * 0.72,
								Oklab:  true,
								Stops: []ui.GradientStop{
									{Color: ui.RGB(255, 255, 255).Alpha(0.32 * shine), At: 0},
									{Color: ui.RGB(255, 255, 255).Alpha(0.10 * shine), At: 0.42},
									{Color: ui.Transparent, At: 1},
								},
							}, artRadius)
							p.FillGradient(r, ui.LinearGradient{
								Angle: 130,
								Oklab: true,
								Stops: []ui.GradientStop{
									{Color: ui.RGB(255, 255, 255).Alpha(0.16 * shine), At: 0},
									{Color: ui.Transparent, At: 0.55},
								},
							}, artRadius)
							p.Stroke(r, ui.RGB(255, 255, 255).Alpha(0.35*shine), artRadius, 1.5)
						})
					})
				}
			})

			ui.Column(c).AlignItems(ui.Center).Gap(4).MaxWidth(640).Children(func() {
				m3.EmphasizedText(c, m3.HeadlineSmall, a.current.Title).TextColor(ui.RGB(255, 255, 255)).TextAlign(ui.Center).MaxLines(2)
				m3.Text(c, m3.BodyLarge, shownArtist(a.current)).TextColor(ui.RGB(255, 255, 255).Alpha(0.72)).TextAlign(ui.Center).SingleLine()
				a.stemPanelControls(c)
			})
		})
	})
}

func (a *app) fullScreenLyrics(c *ui.Context) {
	l := &a.nowPlaying.lyrics
	fs := &a.fullScreen
	white := ui.RGB(255, 255, 255)
	if l.load.busy() || l.videoID != a.current.VideoID && l.err == "" {
		ui.Column(c).Grow(1).Center().Children(func() {
			m3.LoadingIndicatorIn(c, 40, white).Label("Loading lyrics")
		})
		return
	}
	if len(l.synced) > 0 {
		at := a.playbackPosition()
		if a.player.Playing() {
			c.AnimationFrame()
		}
		now := lyrics.IndexAt(l.synced, at)
		if fs.followed != now {
			fs.followed, fs.center = now, true
		}
		scroll := &fs.lyricsScroll
		if now <= 0 && fs.center {
			fs.center = false
			scroll.Y = 0
		}
		viewport := ui.Rect{}
		view := ui.Scroll(c.Key("fs-lyrics")).TrackScroll(scroll).Grow(1).FillWidth()
		viewport = view.LayoutBounds()
		pad := max(viewport.H/2-48, 80)
		view.Padding(pad, 64, pad).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(920).AlignSelf(ui.Center).Gap(12).Children(func() {
				for i, line := range l.synced {
					text := line.Text
					if text == "" {
						text = "♪"
					}
					distance := i - now
					if distance < 0 {
						distance = -distance
					}
					opacity, scale := lyricLook(distance)
					targetGlow := float32(0)
					if i == now {
						targetGlow = 1
					}
					row := ui.ButtonBase(c.Key(i)).FillWidth().Padding(10, 16).Radius(m3.Large).
						Justify(ui.Start).Cursor(ui.CursorPointer).Label(text)
					row.Background(m3.StateFill(ui.Transparent, white, row.Hovered(), row.Pressed(), row.FocusVisible()))
					glow := float32(0)
					if distance <= 3 {
						opacity = m3.Animate(row, "fs-lyric-op", opacity, lyricFadeSpring)
						scale = m3.Animate(row, "fs-lyric-scale", scale, lyricLineSpring)
						glow = m3.Animate(row, "fs-lyric-glow", targetGlow, lyricFadeSpring)
					}
					row.Opacity(opacity).TransformOrigin(0, 0.5).Scale(scale, scale).Children(func() {
						if len(line.Words) > 0 {
							m3.EmphasizedRichText(c, m3.DisplayMedium, lyricWords(line, at, white, white.Alpha(0.6*glow))...).
								FontSize(44).FontWeight(800).LineHeight(1.24).TextAlign(ui.Start)
						} else {
							txt := m3.EmphasizedText(c, m3.DisplayMedium, text).
								FontSize(44).FontWeight(800).LineHeight(1.24).TextAlign(ui.Start).TextColor(white)
							if glow > 0.02 {
								txt.TextShadow(0, 0, 20*glow, white.Alpha(0.45*glow))
							}
						}
					})
					if row.Clicked() {
						a.seek(line.At)
					}
					if i == now && fs.center && viewport.H > 0 {
						bounds := row.LayoutBounds()
						if bounds.H > 0 {
							off := (bounds.Y + bounds.H/2) - (viewport.Y + viewport.H/2)
							if max(off, -off) > centeredWithinDIPs {
								scroll.ScrollTo(0, scroll.Y+off, lyricScroll)
							}
							fs.center = false
						}
					}
				}
			})
		})
		return
	}
	if l.text != "" {
		ui.Scroll(c.Key("fs-lyrics-plain")).TrackScroll(&fs.lyricsScroll).Grow(1).FillWidth().
			Padding(40, 64, 40).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(920).AlignSelf(ui.Center).Children(func() {
				m3.EmphasizedText(c, m3.HeadlineLarge, l.text).
					FontSize(32).FontWeight(700).TextColor(white.Alpha(0.92)).LineHeight(1.42).TextAlign(ui.Start)
			})
		})
		return
	}
	ui.Column(c).Grow(1).Center().Children(func() {
		msg := l.err
		if msg == "" {
			msg = "No lyrics available"
		}
		m3.Text(c, m3.TitleMedium, msg).TextColor(white.Alpha(0.65)).TextAlign(ui.Center)
	})
}
