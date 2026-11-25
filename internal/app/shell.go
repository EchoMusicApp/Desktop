package app

import (
	"math"
	"runtime"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/keymap"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

const (
	playerHeight      = 80
	playerGutter      = 16
	sheetInset        = 8
	pageGutter        = 24
	detailCompactSlot = 300
	plainHeadingInset = 8
	topbarBackButton  = 40
	topbarLeftBlock   = topbarBackButton + 16 + detailCompactSlot
)

func (a *app) view(c *ui.Context) {
	a.beginArtFrame()
	c.SetZoom(a.zoom())
	a.followArtwork()
	th := a.resolveTheme(c)
	m3.Provide(c, th)
	sc := th.Scheme
	a.syncWindowButtons(th.Dark)
	a.updateHints(c)

	a.syncLocation()
	if a.notice != "" {
		switch {
		case a.noticePremium:
			c.AddToast(ui.Toast{Title: a.notice, Action: "Turn on", OnAction: func() {
				a.settings.AccountAudio = true
				a.syncAudioAuth()
				a.saveSettings()
			}})
		case a.noticeHelp:
			c.AddToast(ui.Toast{Title: a.notice, Action: "Learn more", OnAction: a.openToolHelp})
		default:
			c.Toast(a.notice)
		}
		a.notice, a.noticeHelp, a.noticePremium = "", false, false
	}
	a.syncSignInDialog()

	bar := c.TitleBar()
	shell := ui.Row(c).Fill().AlignItems(ui.Stretch)
	shell.Children(func() {
		a.rail(c, bar)
		ui.Box(c).Width(1).FillHeight().Background(sc.OutlineVariant.Alpha(0.35)).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			a.sheet(c, bar)
		})
	})
	a.reserveSpaceForPlayback(shell)
	if a.fullScreen.open && a.current.VideoID != "" {
		a.fullScreenView(c)
	}
	a.signInDialog(c)
	a.newPlaylistDialog(c)
	a.deletePlaylistDialog(c)
	a.addToPlaylistDialog(c)
	a.toolHelpDialog(c)
	a.nerdDialog(c)
	a.privacyDialog(c)
	if a.desktop != nil && a.autoCheckUpdatesOn() && !a.updates.checkedOnce {
		a.checkForUpdates(false)
	}
	a.updateDialog(c)
	a.crashDialog(c)
	m3.Snackbars(c, 0)
	a.shortcuts(c)
}

func (a *app) syncWindowButtons(dark bool) {
	if a.desktop == nil || a.desktop.win == nil || a.theme.buttonsSet && a.theme.buttonsDark == dark {
		return
	}
	src := mygo.ThemeLight
	if dark {
		src = mygo.ThemeDark
	}
	a.desktop.win.SetWindowButtonsTheme(src)
	a.theme.buttonsSet, a.theme.buttonsDark = true, dark
}

func (a *app) navigate(id routeID) { a.router.Push(routeByID(id).path("")) }

func navID(id routeID) string { return routeByID(id).path("") }

func (a *app) rail(c *ui.Context, bar ui.TitleBar) {
	items, footer := a.railItems[:0], a.railFooter[:0]
	for i := range routes {
		r := &routes[i]
		if r.rail == nil || r.rail.withAccount && !a.signedIn() {
			continue
		}
		item := m3.NavItem{ID: navID(r.id), Label: r.rail.label, Icon: r.rail.icon, Selected: r.rail.filled}
		if r.rail.action != "" && a.keyboard.hints {
			item.Hint = a.hintParts(r.rail.action)
		}
		if r.rail.footer {
			footer = append(footer, item)
		} else {
			items = append(items, item)
		}
	}
	a.railItems, a.railFooter = items, footer
	r, _ := a.route()
	selected := ""
	if r.nav != routeNotFound {
		selected = navID(r.nav)
	}
	topInset := float32(0)
	if bar.Left > 0 {
		topInset = bar.Height
	}
	event := m3.Rail(c, m3.RailSpec{
		Items:    items,
		Footer:   footer,
		Selected: selected,
		Expanded: a.settings.RailExpanded,
		TopInset: topInset,
	})
	if event.Toggle {
		a.settings.RailExpanded = !a.settings.RailExpanded
		a.saveSettings()
	}
	if event.Item != "" && (event.Item != selected || r.path("") != event.Item) {
		a.router.Push(event.Item)
	} else if event.Item != "" && a.nowPlaying.open && !a.nowPlaying.closing {
		a.nowPlaying.closing = true
	}
}

const sheetCorner = sheetInset + m3.ExtraLarge*continuousCornerReach

func (a *app) sheet(c *ui.Context, bar ui.TitleBar) {
	sc := m3.Of(c).Scheme
	tint := sc.SurfaceContainerLow
	top := float32(sheetInset)
	if bar.Right > 0 {
		top = max(top, bar.Height)
	}
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(top).Background(tint).DragWindow()
	ui.Box(c).Absolute().Top(0).Right(0).Bottom(0).Width(sheetInset).Background(tint)
	ui.Box(c).Absolute().Bottom(0).Left(0).Right(0).Height(sheetInset).Background(tint)
	ui.Box(c).Absolute().Top(0).Left(0).Size(sheetCorner, sheetCorner).Background(tint)
	ui.Box(c).Absolute().Top(0).Right(0).Size(sheetCorner, sheetCorner).Background(tint)
	ui.Box(c).Absolute().Bottom(0).Left(0).Size(sheetCorner, sheetCorner).Background(tint)
	ui.Box(c).Absolute().Bottom(0).Right(0).Size(sheetCorner, sheetCorner).Background(tint)
	sheet := ui.Column(c.Key("sheet")).Absolute().Top(top).Left(0).Right(sheetInset).Bottom(sheetInset).
		Radius(m3.ExtraLarge)
	if !a.nowPlaying.open || a.nowPlaying.closing {
		sheet.Background(sc.Surface)
		a.attachAurora(c, sheet, false)
	}
	sheet.Children(func() {
		defer sheetCorners(c, tint)
		if a.nowPlaying.open {
			if a.nowPlaying.closing {
				a.sheetContents(c)
				a.nowPlayingView(c)
				if !a.nowPlaying.open {
					a.nowPlaying.playerShown = false
				}
				return
			}
			a.nowPlayingView(c)
			if a.nowPlaying.open {
				return
			}
		}
		a.nowPlaying.playerShown = false
		a.sheetContents(c)
	})
}

func (a *app) sheetContents(c *ui.Context) {
	a.topBar(c)
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		a.routerView(c)
		a.pullIndicator(c)
	})
	if a.current.VideoID != "" {
		a.playerBar(c)
	}
}

func (a *app) attachAurora(c *ui.Context, el ui.Element, full bool) {
	th := m3.Of(c)
	if th.Style != m3.Gradient {
		return
	}
	fade := float32(1)
	if !full {
		r, _ := a.route()
		if r.id == routeSettings {
			return
		}
		if a.page != nil && a.page.scroll != nil && a.page.scroll.Y > 0 {
			fade = 1 - min(a.page.scroll.Y/140, 1)
			if fade <= 0.01 {
				return
			}
		}
	}
	sc := th.Scheme
	aurora := m3.NewScheme(m3.NewPalettes(th.Seed, m3.Vibrant), sc.Dark)
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 {
			return
		}
		p.AnimationFrameRate(30)
		t := float64(p.Now().UnixNano()) / 1e9
		a1, a2, a3 := float32(0.24)*fade, float32(0.18)*fade, float32(0.16)*fade
		if !sc.Dark {
			a1, a2, a3 = 0.14*fade, 0.11*fade, 0.09*fade
		}
		if full {
			a1 += 0.04
			a2 += 0.04
			a3 += 0.03
		}

		y1 := float32(55+35*math.Cos(t*0.22)) / r.H
		y2 := float32(85+40*math.Sin(t*0.29)) / r.H
		y3 := float32(50+30*math.Cos(t*0.25+1.2)) / r.H
		rad1 := min(r.W*0.45, 420)
		rad2 := min(r.W*0.42, 380)
		rad3 := min(r.W*0.38, 350)
		if full {
			y1 = float32(0.26 + 0.10*math.Cos(t*0.22))
			y2 = float32(0.36 + 0.12*math.Sin(t*0.29))
			y3 = float32(0.64 + 0.10*math.Cos(t*0.25+1.2))
			rad1, rad2, rad3 = r.W*0.60, r.W*0.54, r.W*0.52
		}

		draw := func() {
			p.FillRadialGradient(r, ui.RadialGradient{
				Center: ui.Point{X: float32(0.18 + 0.12*math.Sin(t*0.28)), Y: y1},
				Radius: rad1 * float32(0.92+0.08*math.Sin(t*0.37)),
				Oklab:  true,
				Stops: []ui.GradientStop{
					{Color: aurora.Primary.Alpha(a1), At: 0},
					{Color: aurora.Primary.Alpha(a1 * 0.42), At: 0.45},
					{Color: ui.Transparent, At: 1},
				},
			}, 0)
			p.FillRadialGradient(r, ui.RadialGradient{
				Center: ui.Point{X: float32(0.54 + 0.15*math.Cos(t*0.23)), Y: y2},
				Radius: rad2 * float32(0.92+0.08*math.Cos(t*0.33)),
				Oklab:  true,
				Stops: []ui.GradientStop{
					{Color: aurora.Tertiary.Alpha(a2), At: 0},
					{Color: aurora.Tertiary.Alpha(a2 * 0.40), At: 0.48},
					{Color: ui.Transparent, At: 1},
				},
			}, 0)
			p.FillRadialGradient(r, ui.RadialGradient{
				Center: ui.Point{X: float32(0.84 + 0.10*math.Sin(t*0.31+0.9)), Y: y3},
				Radius: rad3 * float32(0.92+0.08*math.Sin(t*0.27)),
				Oklab:  true,
				Stops: []ui.GradientStop{
					{Color: aurora.Secondary.Alpha(a3), At: 0},
					{Color: aurora.Secondary.Alpha(a3 * 0.38), At: 0.50},
					{Color: ui.Transparent, At: 1},
				},
			}, 0)
		}
		p.Clip(r, m3.ExtraLarge, draw)
	})
}

func (a *app) clearance() float32 {
	if a.current.VideoID == "" {
		return 24
	}
	return playerHeight + 16
}

func (a *app) topBar(c *ui.Context) {
	sc := m3.Of(c).Scheme
	r, _ := a.route()
	back := r.isDetail()
	padding := float32(pageGutter - plainHeadingInset)
	targetHeight := float32(76)
	if !back {
		scrollY := float32(0)
		if a.page != nil && a.page.scroll != nil {
			scrollY = a.page.scroll.Y
		}
		if r.id != a.lastNavRoute {
			a.lastNavRoute = r.id
			a.settingsNavHidden = false
			a.lastSettingsY = scrollY
		} else if a.page != nil && a.page.scroll != nil {
			diff := scrollY - a.lastSettingsY
			if scrollY <= 15 {
				a.settingsNavHidden = false
			} else if diff > 6 {
				a.settingsNavHidden = true
			} else if diff < -6 {
				a.settingsNavHidden = false
			}
			a.lastSettingsY = scrollY
		}
		if a.settingsNavHidden {
			targetHeight = 0
		}
	} else {
		a.lastNavRoute = r.id
		a.settingsNavHidden = false
	}
	topbar := ui.Row(c.Key("topbar")).PaddingX(padding).Gap(16).AlignItems(ui.Center).Shrink(0).DragWindow().Clip()
	navH := m3.Animate(topbar, "nav-height", targetHeight, m3.EffectsDefault)
	topbar.Height(navH)
	if navH < 76 {
		topbar.Opacity(navH / 76)
	}
	if navH <= 1 {
		return
	}
	if back {
		topbar.Background(a.detailWash(sc))
	} else if r.id == routeSettings {
		topbar.Background(sc.Surface)
	}
	topbar.BorderWidth(0, 0, 1, 0).BorderColor(sc.OutlineVariant.Alpha(0.25))
	compactTarget := float32(0)
	if a.detailCompact {
		compactTarget = 1
	}
	compact := m3.Animate(topbar, "compact-header", compactTarget, m3.EffectsSlow)
	topbar.Children(func() {
		left := ui.Row(c).Gap(16).AlignItems(ui.Center)
		if back && r.target == youtube.TargetPodcast {
			left.Grow(1).MinWidth(0)
		} else {
			left.Width(topbarLeftBlock).Shrink(0)
		}
		left.Children(func() {
			if back {
				if m3.IconButton(c, m3.IconButtonSpec{Icon: m3.IconArrowBack, Label: "Back", Key: "back"}).Clicked() {
					a.router.Back()
				}
				compactRow := ui.Row(c)
				if r.target == youtube.TargetPodcast {
					compactRow.Grow(1).MinWidth(0)
				} else {
					compactRow.Width(detailCompactSlot).Shrink(0)
				}
				compactRow.Children(func() {
					if compact > 0.01 {
						a.compactHeader(c, compact)
					}
				})
				return
			}
			title := ""
			if r.heading != nil {
				title = r.heading(c.Now())
			}
			if title != "" {
				m3.EmphasizedText(c, m3.HeadlineMedium, title).SingleLine().Margin(0, 0, 0, 8).MaxWidth(topbarLeftBlock - 8).Shrink(1).MinWidth(0).TextColor(sc.OnSurface)
			}
		})
		ui.Spacer(c)
		if r.appOwned {
			if r.id == routeSettings {
				a.settingsSearchBar(c)
			}
			return
		}
		var bar, field ui.Element
		a.hinted(c, func() { bar, field = a.searchBar(c) }, keymap.Search).Width(520).Shrink(1).MinWidth(0)
		a.searchSuggestions(c, bar, field)
		a.accountButton(c)
	})
}

func (a *app) routerView(c *ui.Context) {
	a.router.View(c, func(page *ui.Route) {
		r, _ := matchRoute(page.Path())
		page.Title(r.title)
		switch {
		case r.needsAccount != nil && !a.signedIn():
			prompt := *r.needsAccount
			if a.account.expired != "" {
				prompt = signInPrompt{m3.IconLogin, "Your session expired", "Sign in again to see " + prompt.what + ".", prompt.what}
			}
			ui.Column(c).Fill().Center().Children(func() {
				a.message(c, prompt.icon, prompt.title, prompt.body, "Sign in", a.openSignIn)
			})
		case r.view != nil:
			r.view(a, c)
		default:
			a.pageList(c)
		}
	})
}

func (a *app) message(c *ui.Context, icon *ui.SVG, title, body, action string, run func()) {
	sc := m3.Of(c).Scheme
	ui.Column(c).FillWidth().Center().Gap(12).Padding(72, 32).Children(func() {
		ui.Box(c).Size(80, 80).Radius(m3.Full).Background(sc.PrimaryContainer.Alpha(0.35)).
			Border(1, sc.Primary.Alpha(0.18)).Center().Children(func() {
			ui.Icon(c, icon).FontSize(34).TextColor(sc.Primary)
		})
		ui.Box(c).Height(4)
		m3.EmphasizedText(c, m3.HeadlineSmall, title).TextAlign(ui.Center).TextColor(sc.OnSurface)
		if body != "" {
			m3.Text(c, m3.BodyMedium, body).TextColor(sc.OnSurfaceVariant).MaxLines(3).MaxWidth(380).TextAlign(ui.Center)
		}
		if action != "" && run != nil {
			ui.Box(c).Height(6)
			if m3.Button(c, m3.ButtonSpec{Label: action, Kind: m3.Tonal, Size: m3.Small40, Key: "message-action"}).Clicked() {
				run()
			}
		}
	})
}

func sheetCorners(c *ui.Context, tint ui.Color) {
	n := float32(m3.ExtraLarge) * sheetReach
	for i, corner := range sheetCornerShapes {
		piece := ui.Icon(c, corner).FontSize(n).TextColor(tint).Absolute().PassThrough()
		switch i {
		case 0:
			piece.Top(0).Left(0)
		case 1:
			piece.Top(0).Right(0)
		case 2:
			piece.Bottom(0).Left(0)
		default:
			piece.Bottom(0).Right(0)
		}
	}
}

const continuousCornerReach = 1.528665

var sheetReach = map[bool]float32{false: 1, true: continuousCornerReach}[runtime.GOOS == "darwin"]

var sheetCornerShapes = [4]*ui.SVG{
	cornerShape(false, false), cornerShape(true, false), cornerShape(false, true), cornerShape(true, true),
}

func cornerShape(right, bottom bool) *ui.SVG {
	reach := 100 * float64(sheetReach)
	point := func(x, y float64) string {
		if right {
			x = reach - x
		}
		if bottom {
			y = reach - y
		}
		return strconv.FormatFloat(x, 'f', 3, 64) + " " + strconv.FormatFloat(y, 'f', 3, 64)
	}
	path := "M" + point(0, 0)
	if runtime.GOOS == "darwin" {
		path += "L" + point(reach, 0) +
			"C" + point(108.849323, 0) + " " + point(86.840694, 0) + " " + point(63.149399, 7.491139) +
			"C" + point(37.282383, 16.905956) + " " + point(16.905956, 37.282383) + " " + point(7.491139, 63.149399) +
			"C" + point(0, 86.840694) + " " + point(0, 108.849323) + " " + point(0, reach)
	} else {
		const quarterCircleBezier = 0.5522847498
		path += "L" + point(100, 0) + "C" + point(100*(1-quarterCircleBezier), 0) + " " + point(0, 100*(1-quarterCircleBezier)) + " " + point(0, 100)
	}
	path += "Z"
	size := strconv.FormatFloat(reach, 'f', 3, 64)
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + size + ` ` + size + `" fill="currentColor"><path d="` + path + `"/></svg>`))
}
