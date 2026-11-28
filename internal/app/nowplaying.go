package app

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/keymap"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

const playerArt = 160

type panelTab uint8

const (
	tabQueue panelTab = iota
	tabLyrics
	tabRelated
)

var panelTabs = [...]struct {
	tab   panelTab
	label string
}{{tabQueue, "Up next"}, {tabLyrics, "Lyrics"}, {tabRelated, "Related"}}

type nowPlayingState struct {
	open                 bool
	closing              bool
	navigateTo           youtube.BylinePart
	navigateToSet        bool
	tab                  panelTab
	tabDraft             int
	playerShown          bool
	queueOnly            bool
	queueList            ui.ListState
	queueSelection       ui.Selection[string]
	queueSelectionAnchor int
	queueSelectionCursor int
	queueFollow          string
	queueViews           map[queueCallKey]queueRowView
	cover                coverState
	volumeDraft          float64
	volumeHeld           bool
	volumePopup          bool
	lyrics               lyricsState
	lyricsScroll         ui.ScrollState
	details              detailsState
	related              relatedState
	relatedList          ui.ListState
	autoPlayDraft        bool
	miniOpen             bool
	speedMenu            string
	speedChoices         bool
	devicesMenu          bool
	barDevicesMenu       bool
}

type relatedState struct {
	load    task
	videoID string
	items   []youtube.MusicItem
	tracks  []youtube.MusicItem
	err     string
}

type detailsState struct {
	load    task
	videoID string
	details *youtube.EpisodeDetails
	err     string
}

func (a *app) openPlayer(tab panelTab, queueOnly bool) {
	a.nowPlaying.navigateToSet = false
	a.nowPlaying.open, a.nowPlaying.closing = true, false
	a.nowPlaying.tab, a.nowPlaying.queueOnly = tab, queueOnly
	a.loadPanel()
}

func (a *app) setMiniShown(open bool) {
	a.nowPlaying.miniOpen = open
	a.loadPanel()
}

func (a *app) loadPanel() {
	if a.notch.expanded {
		a.loadLyrics()
	}
	if !a.nowPlaying.open && !a.nowPlaying.miniOpen {
		return
	}
	switch a.nowPlaying.tab {
	case tabLyrics:
		if a.current.IsEpisode() {
			a.loadDetails()
		} else {
			a.loadLyrics()
		}
	case tabRelated:
		a.loadRelated()
	case tabQueue:
	}
}

const panelTimeout = time.Minute

func (a *app) loadRelated() {
	r := &a.nowPlaying.related
	id := a.current.VideoID
	if id == "" || r.videoID == id {
		return
	}
	r.videoID, r.items, r.tracks, r.err = id, nil, nil, ""
	client := a.client()
	unavailable, none := "Related songs aren't available.", "No related songs for this track."
	if a.current.IsEpisode() {
		unavailable, none = "Related episodes aren't available.", "Nothing related to this episode."
	}
	startTask(a, &r.load, panelTimeout, func(ctx context.Context) (*youtube.BrowseResult, error) {
		return client.GetRelated(ctx, id)
	}, func(result *youtube.BrowseResult, err error) {
		switch {
		case err != nil:
			r.err = unavailable
		case len(result.Items) == 0:
			r.err = none
		default:
			r.items = result.Items
			for _, item := range r.items {
				if item.IsTrack() {
					r.tracks = append(r.tracks, item)
				}
			}
		}
	})
}

func (a *app) loadDetails() {
	d := &a.nowPlaying.details
	id := a.current.VideoID
	if id == "" || d.videoID == id {
		return
	}
	d.videoID, d.details, d.err = id, nil, ""
	client := a.client()
	startTask(a, &d.load, panelTimeout, func(ctx context.Context) (*youtube.EpisodeDetails, error) {
		return client.GetEpisodeDetails(ctx, id)
	}, func(details *youtube.EpisodeDetails, err error) {
		if err != nil {
			log.Printf("details: video_id=%s: %v", id, err)
			d.err, d.videoID = "Couldn't load the details. Try again in a moment.", ""
			return
		}
		d.details = details
	})
}

const riseDistance = 80

func (a *app) nowPlayingView(c *ui.Context) {
	m3.Provide(c, a.resolveTheme(c))
	sc := m3.Of(c).Scheme
	w, h := c.Size()
	wide := w > 960
	panel := ui.Column(c.Key("now-playing")).Absolute().Left(0).Right(0).
		Background(sc.Surface).Radius(m3.ExtraLarge).Clip()
	away := float32(0)
	if a.nowPlaying.closing {
		away = riseDistance
	}
	lift := m3.Rise(panel, "rise", riseDistance, away, !a.nowPlaying.playerShown, m3.SpatialDefault)
	panel.Top(lift).Bottom(-lift).Opacity(1 - min(max(lift/riseDistance, 0), 1))
	a.nowPlaying.playerShown = true
	if a.nowPlaying.closing && lift >= riseDistance-0.5 {
		a.nowPlaying.open, a.nowPlaying.closing = false, false
		if a.nowPlaying.navigateToSet {
			part := a.nowPlaying.navigateTo
			a.nowPlaying.navigateToSet = false
			a.openTarget(part.Target(), detail{title: part.Text})
		}
		return
	}
	panel.Children(func() {
		ui.Row(c).Height(52).PaddingX(pageGutter).Gap(16).AlignItems(ui.Center).Shrink(0).
			BorderWidth(0, 0, 1, 0).BorderColor(sc.OutlineVariant.Alpha(0.2)).Children(func() {
			a.hinted(c, func() {
				if m3.IconButton(c, m3.IconButtonSpec{Icon: m3.IconExpandMore, Label: "Close the player", Key: "np-close"}).Clicked() {
					a.nowPlaying.closing = true
				}
			}, keymap.ClosePlayer)
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				m3.EmphasizedText(c, m3.TitleMedium, "Now playing").TextColor(sc.OnSurface)
			})
			if !wide {
				if m3.IconButton(c, m3.IconButtonSpec{
					Icon: m3.IconQueue, Label: "Up next", Toggle: true, Selected: a.nowPlaying.queueOnly, Kind: m3.TonalIcon, Key: "np-queue",
				}).Clicked() {
					a.nowPlaying.queueOnly = !a.nowPlaying.queueOnly
				}
			}
		})
		ui.Row(c).Grow(1).MinHeight(0).Padding(12, pageGutter, 12).Gap(32).AlignItems(ui.Stretch).Children(func() {
			if wide || !a.nowPlaying.queueOnly {
				ui.Column(c).Grow(1.15).Basis(0).MinWidth(0).MinHeight(0).Center().Children(func() {
					a.nowPlayingMain(c, w, h)
				})
			}
			if wide {
				ui.Box(c).Width(1).FillHeight().Background(sc.OutlineVariant.Alpha(0.2)).Shrink(0)
			}
			if wide || a.nowPlaying.queueOnly {
				a.sidePanel(c, wide, false)
			}
		})
	})
}

func (a *app) nowPlayingMain(c *ui.Context, windowWidth, windowHeight float32) {
	sc := m3.Of(c).Scheme
	wide := windowWidth > 960

	// Adaptive sizing for all aspect ratios
	availH := windowHeight - 290
	availW := windowWidth*0.48 - 40
	if !wide {
		availW = windowWidth - 48
	}
	artMax := min(availH, availW)
	artSize := min(max(artMax, 150), 440)

	maxControlsW := min(max(artSize+60, 360), 520)
	if !wide {
		maxControlsW = min(windowWidth-40, 500)
	}

	gapBetween := float32(8)
	controlsGap := float32(6)
	transportSize := float32(44)
	transportGap := float32(10)

	if windowHeight >= 700 {
		gapBetween = 12
		controlsGap = 8
		transportSize = 48
		transportGap = 12
	}
	if windowHeight >= 850 {
		gapBetween = 18
		controlsGap = 10
		transportSize = 56
		transportGap = 16
	}

	ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).Center().Gap(gapBetween).Children(func() {
		if a.current.IsWide() {
			wideW := min(max(availW, 240), 540)
			wideH := wideW * 9 / 16
			if wideH > availH {
				wideH = max(availH, 135)
				wideW = wideH * 16 / 9
			}
			a.nowPlayingArtworkRect(c, 1024, wideW, wideH, 16)
		} else {
			a.nowPlayingArtwork(c, 1024, artSize, 16)
		}
		ui.Column(c).FillWidth().MaxWidth(maxControlsW).Gap(controlsGap).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				a.trackLineCentered(c, m3.TitleLarge, m3.BodyMedium, true).Grow(1)
			})
			a.scrubber(c)
			a.stemPanelControls(c)
			ui.Row(c).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(transportGap).Children(func() {
				a.likeButton(c)
				a.transport(c, transportSize, transportGap)
				a.dislikeButton(c)
			})
			ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
				a.devicesButton(c, "np-devices")
				ui.Row(c).Height(32).Padding(0, 12, 0, 4).Radius(m3.Full).
					Background(sc.SurfaceContainerHighest.Alpha(0.88)).
					Border(1, sc.OutlineVariant.Alpha(0.55)).
					AlignItems(ui.Center).Children(func() {
					a.volumeControl(c, 76)
				})
			})
		})
	})
}

func (a *app) trackLineCentered(c *ui.Context, title, subtitle m3.Role, links bool) ui.Element {
	sc := m3.Of(c).Scheme
	return ui.Column(c).Gap(2).MinWidth(0).AlignItems(ui.Center).Children(func() {
		m3.EmphasizedText(c, title, a.current.Title).MaxLines(2).TextAlign(ui.Center).TextColor(sc.OnSurface).ContentTransition(ui.ContentTransition{})
		line, colour := a.current.Subtitle, sc.OnSurfaceVariant
		if a.playErr != "" {
			line, colour = firstLine(a.playErr), sc.Error
		}
		if links && a.playErr == "" {
			parts := a.playerByline()
			subtitle.Style(ui.RichText(c.Key(a.current.VideoID)), false).MaxLines(2).TextAlign(ui.Center).TextColor(colour).Children(func() {
				for i, part := range parts {
					if !part.Target().Opens() {
						ui.Text(c.Key(i), part.Text)
						continue
					}
					link := ui.Link(c.Key(i), part.Text, "").TextColor(colour).Disabled(a.nowPlaying.closing)
					label := "Go to artist: "
					if part.Target().Kind == youtube.TargetAlbum {
						label = "Go to album: "
					}
					link.Label(label + part.Text).Tooltip(label + part.Text)
					if link.Clicked() {
						a.openPlayerLink(part)
					}
				}
			})
			return
		}
		m3.Text(c, subtitle, line).MaxLines(2).TextAlign(ui.Center).TextColor(colour).ContentTransition(ui.ContentTransition{})
	})
}

func (a *app) devicesButton(c *ui.Context, key string) {
	sc := m3.Of(c).Scheme
	devices, err := getAudioOutputDevices()
	if err != nil {
		devices = nil
	}
	currentDevice := "This Mac"
	pillIcon := m3.IconComputer
	if activePeer, isLive := a.activeLANPeer(); isLive {
		currentDevice = activePeer.Name
		if activePeer.Platform == "android" {
			pillIcon = m3.IconSmartphone
		}
	} else {
		for _, dev := range devices {
			if dev.IsDefault {
				currentDevice = dev.Name
				break
			}
		}
	}

	btn := ui.ButtonBase(c.Key(key))
	btn.Height(32).Padding(4, 12).Radius(m3.Full).
		Background(m3.StateFill(sc.SurfaceContainerHighest.Alpha(0.88), sc.OnSurface, btn.Hovered(), btn.Pressed(), btn.FocusVisible())).
		Border(1, sc.OutlineVariant.Alpha(0.55)).
		AlignItems(ui.Center).Gap(6).Cursor(ui.CursorPointer).
		Label("Audio device: " + currentDevice).Tooltip("Select playback device")
	btn.Children(func() {
		ui.Icon(c, pillIcon).FontSize(14).TextColor(sc.Primary).Shrink(0)
		m3.Text(c, m3.LabelSmall, currentDevice).TextColor(sc.OnSurface).SingleLine().MaxWidth(140).FontWeight(500)
		ui.Icon(c, m3.IconExpandMore).FontSize(13).TextColor(sc.OnSurfaceVariant).Shrink(0)
	})

	if btn.Clicked() {
		a.nowPlaying.devicesMenu = !a.nowPlaying.devicesMenu
	}

	open := a.nowPlaying.devicesMenu
	if !open {
		return
	}
	a.renderDeviceMenu(c, btn, &open, key)
	if !open {
		a.nowPlaying.devicesMenu = false
	}
}

func (a *app) renderDeviceMenu(c *ui.Context, anchor ui.Element, open *bool, key string) {
	sc := m3.Of(c).Scheme
	devices, _ := getAudioOutputDevices()
	m3.Menu(c, anchor, open, 276, func() {
		ui.Column(c.Key(key+"-list")).Padding(4, 0).Gap(2).Children(func() {
			ui.Row(c).Padding(6, 14, 4, 14).Children(func() {
				m3.Text(c, m3.LabelSmall, "THIS MAC").TextColor(sc.OnSurfaceVariant).FontFeatures("case")
			})
			if len(devices) == 0 {
				isLocalActive := a.lanActivePeerID == ""
				row := ui.ButtonBase(c.Key(key + "-local-mac"))
				row.Height(40).PaddingX(12).Gap(10).AlignItems(ui.Center).Radius(m3.Medium).Cursor(ui.CursorPointer).
					Label("This Mac").Role(ui.RoleMenuItem).
					Background(m3.StateFill(ui.Transparent, sc.OnSurface, row.Hovered(), row.Pressed(), row.FocusVisible()))
				row.Children(func() {
					col := sc.OnSurfaceVariant
					textColor := sc.OnSurface
					if isLocalActive {
						col = sc.Primary
						textColor = sc.Primary
					}
					ui.Icon(c, m3.IconComputer).FontSize(16).TextColor(col).Shrink(0)
					m3.LabelMedium.Style(ui.Text(c, "This Mac"), false).Grow(1).SingleLine().TextColor(textColor)
					if isLocalActive {
						ui.Icon(c, m3.IconCheck).FontSize(16).TextColor(sc.Primary).Shrink(0)
					}
				})
				if row.Clicked() {
					if peer, ok := a.activeLANPeer(); ok {
						a.pullHandoffFromPeer(peer)
					} else {
						a.disconnectLANController()
					}
					*open = false
				}
			} else {
				for _, dev := range devices {
					d := dev
					isSelected := a.lanActivePeerID == "" && d.IsDefault
					row := ui.ButtonBase(c.Key(key + "-dev-" + strconv.Itoa(int(d.ID))))
					row.Height(40).PaddingX(12).Gap(10).AlignItems(ui.Center).Radius(m3.Medium).Cursor(ui.CursorPointer).
						Label(d.Name).Role(ui.RoleMenuItem).
						Background(m3.StateFill(ui.Transparent, sc.OnSurface, row.Hovered(), row.Pressed(), row.FocusVisible()))
					row.Children(func() {
						devColor := sc.OnSurfaceVariant
						textColor := sc.OnSurface
						if isSelected {
							devColor = sc.Primary
							textColor = sc.Primary
						}
						ui.Icon(c, m3.IconComputer).FontSize(16).TextColor(devColor).Shrink(0)
						m3.LabelMedium.Style(ui.Text(c, d.Name), false).Grow(1).SingleLine().TextColor(textColor)
						if isSelected {
							ui.Icon(c, m3.IconCheck).FontSize(16).TextColor(sc.Primary).Shrink(0)
						}
					})
					if row.Clicked() {
						if peer, ok := a.activeLANPeer(); ok {
							a.pullHandoffFromPeer(peer)
						} else if a.lanActivePeerID != "" {
							a.disconnectLANController()
						}
						_ = setAudioOutputDevice(d.ID)
						*open = false
					}
				}
			}
			if a.lanSyncEnabled() && len(a.lanPeers) > 0 {
				ui.Row(c).Padding(8, 14, 4, 14).Children(func() {
					m3.Text(c, m3.LabelSmall, "ECHO CONNECT").TextColor(sc.Primary).FontFeatures("case")
				})
				for _, peer := range a.lanPeers {
					p := peer
					isLive := a.lanActivePeerID == p.ID
					isPaired := a.settings.PairedPeerID == p.ID
					row := ui.ButtonBase(c.Key(key + "-lan-" + p.ID))
					row.Height(40).PaddingX(12).Gap(10).AlignItems(ui.Center).Radius(m3.Medium).Cursor(ui.CursorPointer).
						Label(p.Name).Role(ui.RoleMenuItem).
						Background(m3.StateFill(ui.Transparent, sc.OnSurface, row.Hovered(), row.Pressed(), row.FocusVisible()))
					row.Children(func() {
						peerIcon := m3.IconEchoConnect
						iconColor := sc.OnSurfaceVariant
						textColor := sc.OnSurface
						if isLive {
							iconColor = sc.Primary
							textColor = sc.Primary
						}
						ui.Icon(c, peerIcon).FontSize(16).TextColor(iconColor).Shrink(0)
						m3.LabelMedium.Style(ui.Text(c, p.Name), false).Grow(1).SingleLine().TextColor(textColor)
						if !isPaired {
							m3.Text(c, m3.LabelSmall, "Pair in Settings").TextColor(sc.OnSurfaceVariant)
						} else if isLive {
							ui.Icon(c, m3.IconCheck).FontSize(16).TextColor(sc.Primary).Shrink(0)
						}
					})
					if row.Clicked() {
						if !isPaired {
							a.startDesktopPairing(p)
						} else if !isLive {
							if a.current.VideoID != "" {
								a.sendHandoffToPeer(p)
							} else {
								a.attachLANController(p)
							}
						}
						*open = false
					}
				}
			}
		})
	})
}

func (a *app) nowPlayingArtwork(c *ui.Context, imageSize int, artSize, radius float32) {
	a.nowPlayingArtworkRect(c, imageSize, artSize, artSize, radius)
}

func (a *app) nowPlayingArtworkRect(c *ui.Context, imageSize int, artW, artH, radius float32) {
	key := itemKey("now-playing-menu", a.current)
	var menuButton ui.Element
	sc := m3.Of(c).Scheme
	artwork := a.playerArtworkRect(&a.nowPlaying.cover, c, a.current.Thumbnail, imageSize, artW, artH, radius, func() {
		if a.current.IsVideo() {
			m3.VideoBadge(c, artH)
		}
		ui.Row(c).Gap(6).Attach(ui.AnchorTopRight, ui.AnchorTopRight).Top(8).Right(8).Children(func() {
			if m3.IconButton(c, m3.IconButtonSpec{
				Icon: m3.IconFullscreen, Label: "Full screen", Key: key + "-fullscreen",
				Container: sc.SurfaceContainerHighest.Alpha(0.85),
			}).Clicked() {
				a.openFullScreen()
			}
			menuButton = m3.IconButton(c, m3.IconButtonSpec{
				Icon: m3.IconMore, Label: "More options for " + a.current.Title, Key: key + "-button",
				Container: sc.SurfaceContainerHighest.Alpha(0.85),
			})
		})
	})
	m3.Elevation(c, artwork, 3)
	if menuButton.Clicked() {
		switch {
		case a.menu.suppressKey == key:
			a.menu.suppressKey = ""
		case a.menu.opened(key):
			a.menu.close()
		default:
			a.menu.show(key, false, 0, 0)
		}
	} else {
		a.menu.suppressKey = ""
	}
	wasOpen := a.menu.opened(key)
	if a.menu.owns(key) {
		a.songMenu(c, menuButton, key, a.current, songOptions{})
	}
	if wasOpen && !a.menu.opened(key) && menuButton.Hovered() {
		a.menu.suppressKey = key
	}
}

func panelSurface(sc m3.Scheme) ui.Color { return sc.SurfaceContainerLow.Alpha(0.9) }

func (a *app) sidePanel(c *ui.Context, wide, compactQueue bool) {
	sc := m3.Of(c).Scheme
	card := ui.Column(c.Key("np-side")).Radius(m3.Large).Background(sc.SurfaceContainerLow).Clip().
		Border(1, sc.OutlineVariant.Alpha(0.2)).Gap(4)
	if compactQueue {
		card.Padding(6, 0, 0)
	} else {
		card.Padding(10, 0, 0)
	}
	if wide {
		card.Grow(0.85).Basis(0).MinWidth(360).MaxWidth(640).Shrink(0)
	} else {
		card.Grow(1).MinWidth(0).MinHeight(0)
	}
	card.Children(func() {
		a.nowPlayingTabs(c, compactQueue)
		switch a.nowPlaying.tab {
		case tabLyrics:
			if a.current.IsEpisode() {
				a.detailsView(c)
			} else {
				a.lyricsView(c)
			}
		case tabRelated:
			a.relatedView(c)
		case tabQueue:
			a.queueView(c, compactQueue)
		}
	})
	a.lyricsSourceDialog(c)
}

func (a *app) nowPlayingTabs(c *ui.Context, compact bool) {
	sc := m3.Of(c).Scheme
	a.nowPlaying.tabDraft = int(a.nowPlaying.tab)
	selectedTab := &a.nowPlaying.tabDraft
	group := ui.TabsBase(c.Key("np-tabs"), selectedTab, len(panelTabs))
	tabs := group.List
	if compact {
		tabs.Padding(4, 8, 0)
	} else {
		tabs.Padding(6, 12, 6)
	}
	tabs.Gap(6).Children(func() {
		for i, t := range panelTabs {
			button := group.Tab(i)
			selected := *selectedTab == i
			container, content := ui.Transparent, sc.OnSurfaceVariant
			if selected {
				container, content = sc.SecondaryContainer, sc.OnSecondaryContainer
			}
			label := t.label
			if t.tab == tabLyrics && a.current.IsEpisode() {
				label = "Details"
			}
			button.Grow(1).Basis(0).MinWidth(0).Height(36).Center().Radius(m3.Full).
				Cursor(ui.CursorPointer).Label(label).Tooltip(label).
				Background(m3.StateFill(container, content, button.Hovered(), button.Pressed(), button.FocusVisible()))
			button.Children(func() {
				role := m3.LabelMedium
				if compact {
					role = m3.LabelSmall
				}
				text := m3.Text(c, role, label).SingleLine().TextAlign(ui.Center).TextColor(content)
				if selected {
					text.FontWeight(600)
				}
			})
		}
	})
	if tabs.Changed() {
		a.nowPlaying.tab = panelTabs[*selectedTab].tab
		a.loadPanel()
	}
}

func (a *app) queueSourceLine(c *ui.Context, compact bool) {
	sc := m3.Of(c).Scheme
	role := m3.LabelLarge
	if compact {
		role = m3.LabelMedium
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		m3.Text(c, role, "Playing from").Shrink(0).TextColor(sc.OnSurfaceVariant)
		m3.EmphasizedText(c, role, a.queue.source).Grow(1).MinWidth(0).SingleLine()
	})
}

func queueRowKey(q *queue, row int) string {
	if q.isDivider(row) {
		return "queue-recommendations"
	}
	if index, ok := q.trackAt(row); ok {
		return itemKey("queue", q.at(index)) + "#" + strconv.Itoa(index)
	}
	return ""
}

func (a *app) selectQueueRow(index int, mods ui.Modifiers) {
	row := a.queue.rowOf(index)
	key := queueRowKey(&a.queue, row)
	if mods&ui.Shift != 0 {
		anchor := a.nowPlaying.queueSelectionAnchor
		if anchor < 0 || anchor >= a.queue.len() {
			a.nowPlaying.queueSelection.Clear()
			a.nowPlaying.queueSelection.Add(key)
			a.nowPlaying.queueSelectionAnchor = index
		} else {
			a.nowPlaying.queueSelection.Clear()
			for i := min(anchor, index); i <= max(anchor, index); i++ {
				a.nowPlaying.queueSelection.Add(queueRowKey(&a.queue, a.queue.rowOf(i)))
			}
		}
	} else {
		selection := &a.nowPlaying.queueSelection
		if selection.Has(key) {
			selection.Remove(key)
		} else {
			selection.Add(key)
		}
		a.nowPlaying.queueSelectionAnchor = index
	}
	a.nowPlaying.queueSelectionCursor = row
}

func (a *app) queueView(c *ui.Context, compact bool) {
	sc := &m3.Of(c).Scheme
	q := &a.queue
	gap, top, pull, pad := float32(20), float32(8), float32(8), float32(16)
	rowGap := float32(12)
	if compact {
		gap, top, pull, pad, rowGap = 8, 0, 8, 10, 8
	}
	ui.Column(c).Padding(top, 16, 0).Gap(gap).Children(func() {
		if q.source != "" {
			a.queueSourceLine(c, compact)
		}
		ui.Row(c).Gap(rowGap).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				if compact {
					m3.EmphasizedText(c, m3.LabelMedium, "Auto-play")
				} else {
					m3.EmphasizedText(c, m3.TitleMedium, "Auto-play")
					m3.Text(c, m3.BodySmall, "Add similar music when this queue ends.").TextColor(sc.OnSurfaceVariant).SingleLine()
				}
			})
			a.nowPlaying.autoPlayDraft = a.settings.AutoPlay
			on := &a.nowPlaying.autoPlayDraft
			switchControl := m3.Switch
			if compact {
				switchControl = m3.CompactSwitch
			}
			if switchControl(c, on, "Toggle auto-play").Changed() {
				a.setAutoPlay(*on)
			}
		})
		if a.upNext.err != "" {
			m3.Text(c, m3.BodySmall, "Recommendations aren't available right now.").TextColor(sc.Error).Shrink(0)
		}
		ui.Row(c).Margin(-pull, 0).Padding(0, 0, pad).AlignItems(ui.Center).Children(func() {
			m3.Text(c, m3.BodySmall, strconv.Itoa(q.remaining())+" up next").Grow(1).TextColor(sc.OnSurfaceVariant)
			clear := func() {
				m3.Button(c, m3.ButtonSpec{Label: "Clear queue", Kind: m3.TextOnly, Size: m3.ExtraSmall32, Disabled: q.remaining() < 1, Key: "queue-clear"}).OnClick(a.clearUpcoming)
			}
			if compact {
				ui.Row(c).Shrink(0).Margin(0, -16, 0, 0).Children(clear)
				return
			}
			clear()
		})
	})
	if q.len() == 0 {
		a.message(c, m3.IconQueue, "The queue is empty", "", "", nil)
		return
	}
	list := &a.nowPlaying.queueList
	list.Key = func(row int) any { return queueRowKey(q, row) }
	list.Selection = &a.nowPlaying.queueSelection
	list.Selected = &a.nowPlaying.queueSelectionCursor
	list.MenuKeepsSelection = true
	list.Label = func(row int) string {
		if q.isDivider(row) {
			return "Auto-play recommendations"
		}
		if index, ok := q.trackAt(row); ok {
			return q.at(index).Title
		}
		return ""
	}
	list.Header = q.isDivider
	list.Reorder = a.moveQueue
	if a.swipeAllowed(c) {
		list.Reorder = nil
	}
	if a.nowPlaying.queueFollow != a.current.VideoID {
		a.nowPlaying.queueFollow = a.current.VideoID
		list.ScrollTo(q.rowOf(q.index), ui.Start)
	}
	listBottomPadding := float32(16)
	if compact {
		listBottomPadding = 4
	}
	view := ui.List(c, list, q.rows(), func(row int) {
		index, ok := q.trackAt(row)
		if !ok {
			height := float32(40)
			topPadding := float32(8)
			if compact {
				height = 32
				topPadding = 4
			}
			ui.Column(c).Height(height).Padding(topPadding, 8, 0).Justify(ui.Center).Background(sc.SurfaceContainerLow.Mix(sc.Surface, 0.1)).Children(func() {
				m3.EmphasizedText(c, m3.TitleSmall, "Recommended").TextColor(sc.OnSurfaceVariant).SingleLine()
			})
			return
		}
		o := songOptions{directQueue: true, source: q.source, list: "queue", position: index, row: row, compact: compact}
		o.calls, o.rowKey = a.queueRowCalls(q.at(index), o, row)
		o.callsSet = true
		a.songRow(c, q.at(index), q.items, o)
	}).Grow(1).Padding(0, 8, listBottomPadding).FocusRing(false)
	if index, ok := q.trackAt(a.nowPlaying.queueSelectionCursor); ok {
		if view.Submitted() {
			a.clearQueueSelection()
			a.start(q.moveTo(index))
		}
		if list.Shortcut(c, ui.Shift, ui.KeyF10) {
			a.menu.show(songKey(q.at(index), songOptions{list: "queue", position: index}), false, 0, 0)
			c.Invalidate()
		}
	}
}

func (a *app) relatedView(c *ui.Context) {
	r := &a.nowPlaying.related
	switch {
	case r.load.busy():
		ui.Column(c).Grow(1).Center().Children(func() { m3.LoadingIndicator(c, 56, true) })
	case r.err != "":
		a.message(c, m3.IconExplore, r.err, "", "", nil)
	default:
		items, tracks := r.items, r.tracks
		list := &a.nowPlaying.relatedList
		list.Key = func(i int) any {
			if i >= len(items) {
				return nil
			}
			return itemKey("related", items[i]) + "#" + strconv.Itoa(i)
		}
		list.Label = func(i int) string {
			if i >= len(items) {
				return ""
			}
			return items[i].Title
		}
		ui.List(c, list, len(items), func(i int) {
			a.songRow(c, items[i], tracks, songOptions{directQueue: true, source: "Related", list: "related", position: i, track: -1})
		}).Grow(1).Padding(0, 8, 16)
	}
}

func (a *app) detailsView(c *ui.Context) {
	sc := m3.Of(c).Scheme
	d := &a.nowPlaying.details
	switch {
	case d.load.busy():
		ui.Column(c).Grow(1).Center().Children(func() { m3.LoadingIndicator(c, 56, true) })
	case d.err != "":
		a.message(c, m3.IconLyrics, d.err, "", "", nil)
	case d.details != nil:
		details := d.details
		ui.Scroll(c.Key("details-"+d.videoID)).Grow(1).Padding(8, 24, 24).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				if details.Strapline != "" {
					m3.Text(c, m3.BodyMedium, details.Strapline).TextColor(sc.OnSurfaceVariant)
				}
				m3.EmphasizedText(c, m3.TitleLarge, details.Title).TextColor(sc.OnSurface).Selectable()
				if details.Show != "" {
					a.detailsShow(c, details)
				}
				m3.BodyMedium.Style(ui.RichText(c), false).Margin(8, 0, 0).TextColor(sc.OnSurfaceVariant).Selectable().Children(func() {
					for i, run := range details.Description {
						part := ui.Text(c.Key(i), run.Text)
						switch {
						case run.Timestamp:
							part.TextColor(sc.Primary).FontWeight(600).Cursor(ui.CursorPointer)
							if part.Clicked() {
								a.seek(run.At)
							}
						case run.URL != "":
							part.TextColor(sc.Primary).Cursor(ui.CursorPointer).Tooltip(run.URL)
							if part.Clicked() {
								if err := a.openURL(run.URL); err != nil {
									log.Printf("details: could not open %s: %v", run.URL, err)
								}
							}
						}
					}
				})
			})
		})
	}
}

func (a *app) detailsShow(c *ui.Context, details *youtube.EpisodeDetails) {
	sc := m3.Of(c).Scheme
	show := ui.ButtonBase(c.Key("details-show")).Row().AlignSelf(ui.Start).Gap(8).AlignItems(ui.Center).
		Padding(4, 8, 4, 4).Margin(0, -4).Radius(m3.Full).Cursor(ui.CursorPointer).Label(details.Show)
	show.Background(m3.Layer(sc.SurfaceContainerLow, sc.OnSurface, hoverOpacity(show.Hovered() || show.FocusVisible())))
	show.Children(func() {
		if details.ShowThumbnail != "" {
			a.art(c, details.ShowThumbnail, 48, 24, m3.Full)
		}
		m3.Text(c, m3.TitleSmall, details.Show).TextColor(sc.OnSurface).SingleLine()
	})
	if show.Clicked() && details.ShowID != "" {
		a.openTarget(youtube.Target{Kind: youtube.TargetPodcast, ID: details.ShowID}, detail{title: details.Show, art: details.ShowThumbnail})
	}
}
