package app

import (
	"math"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/audio"
	"github.com/EchoMusicApp/Echo-Music/internal/keymap"
	"github.com/EchoMusicApp/Echo-Music/internal/settings"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/player"
)

var swatches = []struct{ name, hex string }{
	{"Violet", "#6750a4"},
	{"Indigo", "#4c5ed6"},
	{"Blue", "#1b6ef3"},
	{"Sky", "#0288d1"},
	{"Teal", "#00897b"},
	{"Green", "#2e8b3a"},
	{"Lime", "#7a9a00"},
	{"Amber", "#d99a00"},
	{"Orange", "#e0620d"},
	{"Red", "#d6323a"},
	{"Rose", "#d81b78"},
	{"Orchid", "#a73fc0"},
}

type settingsRow struct {
	key     string
	heading string
	search  string
	card    func(a *app, c *ui.Context)
}

type settingsSearchState struct {
	query string
	open  bool
	field ui.Handle
}

var settingsRows = []settingsRow{
	{key: "h-account", heading: "Account and Sync"},
	{key: "account", search: "sign in profile account session", card: (*app).accountCard},
	{key: "echo-connect", search: "echo connect lan sync pair code spotify playlists handoff remote phone", card: (*app).echoConnectCard},

	{key: "h-look", heading: "Appearance and sound"},
	{key: "appearance", search: "theme appearance system light dark colour color colours colors violet indigo blue sky teal green lime amber orange red rose orchid hue palette tonal spot vibrant expressive neutral monochrome artwork dynamic matugen source wavy progress wave flat bar", card: (*app).appearanceCard},
	{key: "sound", search: "sound audio normalize loudness logarithmic equaliser volume", card: (*app).soundCard},

	{key: "h-playback", heading: "Playback"},
	{key: "playback", search: "auto skip disliked songs thumbs down premium account bitrate quality cookies yt-dlp authenticated", card: (*app).playbackCard},
	{key: "crossfade", search: "crossfade fade transition gap between tracks songs seconds", card: (*app).crossfadeCard},
	{key: "ai", search: "ai artificial intelligence generated exclude skip filter songs", card: (*app).aiFilterCard},

	{key: "h-system", heading: "System"},
	{key: "ai-control", search: "mcp ai control agent claude cursor antigravity server prompt", card: (*app).aiControlCard},
	{key: "desktop", search: "desktop linux notifications discord rich presence activity status tray icon wordmark o mark low key subtle notch macbook lyrics", card: (*app).desktopCard},
	{key: "keyboard", search: "keyboard shortcuts keys", card: (*app).keyboardCard},

	{key: "h-data", heading: "Storage and privacy"},
	{key: "cache", search: "storage cache playback downloads", card: (*app).playbackCacheCard},
	{key: "privacy", search: "privacy telemetry analytics ping anonymous crash reports data sentry posthog", card: (*app).privacyCard},
	{key: "history", search: "privacy history listening plays", card: (*app).historyCard},

	{key: "h-about", heading: "About and support"},
	{key: "updates", search: "about updates version", card: (*app).updatesCard},
	{key: "support", search: "support donate donation tip buy me a coffee upi gpay github sponsors patreon bitcoin ethereum solana crypto", card: (*app).supportCard},
	{key: "tools", search: "support tools", card: (*app).toolsCard},
	{key: "report", search: "support report problem bug", card: (*app).reportCard},
	{key: "logs", search: "logs folder diagnostics", card: (*app).logsCard},
}

func settingsRowsFor(query string) []settingsRow {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return settingsRows
	}
	var filtered []settingsRow
	for start := 0; start < len(settingsRows); {
		heading := settingsRows[start]
		end := start + 1
		for end < len(settingsRows) && settingsRows[end].heading == "" {
			end++
		}
		sectionMatches := strings.Contains(strings.ToLower(heading.heading), query)
		var matchingCards []settingsRow
		for _, row := range settingsRows[start+1 : end] {
			if sectionMatches || strings.Contains(row.search, query) {
				matchingCards = append(matchingCards, row)
			}
		}
		if len(matchingCards) > 0 {
			filtered = append(filtered, heading)
			filtered = append(filtered, matchingCards...)
		}
		start = end
	}
	return filtered
}

func (a *app) settingsSearchBar(c *ui.Context) {
	const buttonWidth, maxWidth = float32(40), float32(520)
	control := ui.Row(c.Key("settings-search")).Width(maxWidth).MinWidth(0).Shrink(1)
	target := float32(0)
	if a.settingsSearch.open {
		target = 1
	}
	grown := m3.Animate(control, "open", target, m3.SpatialFast)
	if !a.settingsSearch.open && grown < 0.02 {
		control.MaxWidth(buttonWidth)
		control.Children(func() {
			button := m3.IconButton(c, m3.IconButtonSpec{
				Icon: m3.IconSearch, Label: "Search settings", Kind: m3.StandardIcon,
				Size: m3.Small40, Key: "settings-search-open", Container: searchTint(c),
			})
			if button.Clicked() {
				a.settingsSearch.open = true
				a.settingsSearch.field.Focus()
			}
		})
		return
	}
	control.MaxWidth(buttonWidth + (maxWidth-buttonWidth)*min(max(grown, 0), 1))
	control.Children(func() {
		result, _, _ := m3.SearchBar(c, m3.SearchSpec{
			Value:       &a.settingsSearch.query,
			Placeholder: "Search settings",
			Key:         "settings-search-bar",
			Focus:       &a.settingsSearch.field,
			MaxWidth:    maxWidth,
			Container:   searchTint(c),
			Dismiss:     true,
		})
		if result.Changed || result.Cleared {
			a.list.ScrollTo(0, ui.Start)
		}
		if result.Dismissed {
			a.closeSettingsSearch()
		}
	})
}

func (a *app) closeSettingsSearch() {
	a.settingsSearch = settingsSearchState{}
}

func (a *app) settingsPage(c *ui.Context) {
	sc := m3.Of(c).Scheme
	query := strings.TrimSpace(a.settingsSearch.query)
	filtered := settingsRowsFor(query)
	if len(filtered) == 0 {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Box(c).Size(56, 56).Radius(m3.Full).Background(sc.SurfaceContainerHigh).Center().Children(func() {
				ui.Icon(c, m3.IconSearch).FontSize(26).TextColor(sc.OnSurfaceVariant)
			})
			m3.EmphasizedText(c, m3.TitleMedium, "No settings found").TextColor(sc.OnSurface)
			m3.Text(c, m3.BodyMedium, "Try another search.").TextColor(sc.OnSurfaceVariant)
		})
		return
	}
	w, _ := c.Size()
	if query == "" {
		a.settingsCategorizedPage(c, sc, w)
		return
	}
	a.list.Key = func(i int) any {
		if i >= len(filtered) {
			return nil
		}
		return filtered[i].key
	}
	ui.List(c, a.list, len(filtered), func(i int) {
		row := filtered[i]
		if row.card == nil {
			top := float32(20)
			if i == 0 {
				top = 8
			}
			ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
				ui.Row(c).FillWidth().MaxWidth(720).Padding(top, pageGutter+4, 8).AlignItems(ui.Center).Children(func() {
					m3.EmphasizedText(c, m3.LabelLarge, strings.ToUpper(row.heading)).
						TextColor(sc.Primary).LetterSpacing(0.8).FontFeatures("case")
				})
			})
			return
		}
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(720).Padding(0, pageGutter, 12).Children(func() {
				row.card(a, c)
			})
		})
	}).Grow(1).Padding(0, 0, a.clearance()-16).TrackScroll(a.page.scroll)
}

type settingsCategory struct {
	title    string
	subtitle string
	icon     *ui.SVG
	render   func(a *app, c *ui.Context)
}

var settingsCategories = []settingsCategory{
	{
		title:    "Account",
		subtitle: "Manage your YouTube Music session and connected profile.",
		icon:     m3.IconPerson,
		render: func(a *app, c *ui.Context) {
			a.accountCard(c)
		},
	},
	{
		title:    "Echo Connect",
		subtitle: "Pair with your phone over Wi-Fi for playback handoff, account login, and playlist sync.",
		icon:     m3.IconEchoConnect,
		render: func(a *app, c *ui.Context) {
			a.echoConnectCard(c)
		},
	},
	{
		title:    "Appearance",
		subtitle: "Customize theme mode, dynamic colors, player progress bar, and UI zoom.",
		icon:     m3.IconPalette,
		render: func(a *app, c *ui.Context) {
			a.appearanceCard(c)
		},
	},
	{
		title:    "Playback & Sound",
		subtitle: "Configure audio quality, crossfade, AI track filtering, loudness, and equalizer.",
		icon:     m3.IconMusicNote,
		render: func(a *app, c *ui.Context) {
			a.playbackCard(c)
			a.soundCard(c)
			a.crossfadeCard(c)
			a.aiFilterCard(c)
		},
	},
	{
		title:    "AI Control",
		subtitle: "Connect local AI assistants via Model Context Protocol (MCP) to control playback and queues.",
		icon:     m3.IconAutoAwesome,
		render: func(a *app, c *ui.Context) {
			a.aiControlCard(c)
		},
	},
	{
		title:    "Desktop & Shortcuts",
		subtitle: "System integrations, notch player, Discord Rich Presence, and keyboard shortcuts.",
		icon:     m3.IconComputer,
		render: func(a *app, c *ui.Context) {
			a.desktopCard(c)
			a.keyboardCard(c)
		},
	},
	{
		title:    "Storage & Privacy",
		subtitle: "Manage song cache, diagnostics telemetry, and listening history.",
		icon:     m3.IconShield,
		render: func(a *app, c *ui.Context) {
			a.playbackCacheCard(c)
			a.privacyCard(c)
			a.historyCard(c)
		},
	},
	{
		title:    "About & Support",
		subtitle: "Updates, bundled media tools, logs, bug reports, and project support.",
		icon:     m3.IconInfo,
		render: func(a *app, c *ui.Context) {
			a.updatesCard(c)
			a.toolsCard(c)
			a.reportCard(c)
			a.logsCard(c)
			a.supportCard(c)
		},
	},
}

func (a *app) echoConnectCard(c *ui.Context) {
	a.settingsCard(c, "Echo Connect", m3.IconEchoConnect, func() {
		a.lanSyncSetting(c)
	})
}

func (a *app) aiControlCard(c *ui.Context) {
	a.settingsCard(c, "AI Control (MCP)", m3.IconAutoAwesome, func() {
		a.mcpSetting(c)
	})
}

func (a *app) settingsCategorizedPage(c *ui.Context, sc m3.Scheme, width float32) {
	if a.settingsTab < 0 || a.settingsTab >= len(settingsCategories) {
		a.settingsTab = 0
	}
	active := settingsCategories[a.settingsTab]

	renderNavItem := func(idx int, cat settingsCategory, compact bool) {
		selected := a.settingsTab == idx
		bg := ui.Transparent
		fg := sc.OnSurfaceVariant
		if selected {
			bg = sc.SecondaryContainer
			fg = sc.OnSecondaryContainer
		}
		btn := ui.ButtonBase(c.Key("settings-tab-" + strconv.Itoa(idx)))
		if compact {
			btn.Height(36).Padding(0, 14).Radius(m3.Full).Gap(8).Justify(ui.Start).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		} else {
			btn.FillWidth().Height(42).Padding(0, 16).Radius(m3.Full).Gap(12).Justify(ui.Start).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		}
		btn.Label(cat.title).
			Background(m3.StateFill(bg, sc.OnSurface, btn.Hovered(), btn.Pressed(), btn.FocusVisible()))
		btn.Children(func() {
			ui.Icon(c, cat.icon).FontSize(18).TextColor(fg).Shrink(0)
			if selected {
				m3.EmphasizedText(c, m3.LabelLarge, cat.title).TextColor(fg).SingleLine()
			} else {
				m3.Text(c, m3.LabelLarge, cat.title).TextColor(fg).SingleLine()
			}
		})
		if btn.Clicked() {
			a.settingsTab = idx
			a.page.scroll = &ui.ScrollState{}
		}
	}

	if width < 760 {
		ui.Scroll(c.Key("settings-narrow")).Grow(1).MinHeight(0).TrackScroll(a.page.scroll).Children(func() {
			ui.Column(c).FillWidth().Padding(8, pageGutter, a.clearance()).Gap(14).Children(func() {
				ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
					for i, cat := range settingsCategories {
						renderNavItem(i, cat, true)
					}
				})
				ui.Column(c).Gap(2).Padding(4, 2).Children(func() {
					m3.EmphasizedText(c, m3.HeadlineSmall, active.title).TextColor(sc.OnSurface)
					m3.Text(c, m3.BodyMedium, active.subtitle).TextColor(sc.OnSurfaceVariant)
				})
				ui.Column(c).FillWidth().Gap(14).Children(func() {
					active.render(a, c)
				})
			})
		})
		return
	}

	ui.Row(c.Key("settings-split")).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		// Left category rail reusing m3.Rail so icons, sizing, collapse/expand, and spacing match main rail
		ui.Column(c.Key("settings-rail-wrap")).FillHeight().MinHeight(0).Shrink(0).
			BorderWidth(0, 1, 0, 0).BorderColor(sc.OutlineVariant.Alpha(0.2)).
			Children(func() {
				navItems := make([]m3.NavItem, len(settingsCategories))
				for i, cat := range settingsCategories {
					navItems[i] = m3.NavItem{
						ID:       strconv.Itoa(i),
						Label:    cat.title,
						Icon:     cat.icon,
						Selected: cat.icon,
					}
				}
				ev := m3.Rail(c, m3.RailSpec{
					Items:      navItems,
					Selected:   strconv.Itoa(a.settingsTab),
					Expanded:   true,
					HideToggle: true,
				})
				if ev.Item != "" {
					if idx, err := strconv.Atoi(ev.Item); err == nil && idx >= 0 && idx < len(settingsCategories) {
						a.settingsTab = idx
					}
				}
			})

		// Right scrollable content pane with per-tab ScrollState to prevent scroll jitter
		scrollState := &a.settingsScrolls[a.settingsTab]
		ui.Scroll(c.Key("settings-content-" + strconv.Itoa(a.settingsTab))).Grow(1).FillHeight().MinHeight(0).TrackScroll(scrollState).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Start).Padding(16, pageGutter, a.clearance()).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(760).Gap(14).Children(func() {
					ui.Column(c).Gap(2).Padding(0, 4, 4, 4).Children(func() {
						m3.EmphasizedText(c, m3.HeadlineSmall, active.title).TextColor(sc.OnSurface)
						m3.Text(c, m3.BodyMedium, active.subtitle).TextColor(sc.OnSurfaceVariant)
					})
					active.render(a, c)
				})
			})
		})
	})
}

func (a *app) settingsCard(c *ui.Context, title string, icon *ui.SVG, build func()) {
	sc := m3.Of(c).Scheme
	ui.Column(c).Padding(16, 20).Gap(12).Radius(m3.ExtraLarge).
		Background(sc.SurfaceContainerLow).
		Border(1, sc.OutlineVariant.Alpha(0.24)).
		Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(30, 30).Radius(m3.Small).Background(sc.PrimaryContainer.Alpha(0.5)).Center().Children(func() {
					ui.Icon(c, icon).FontSize(17).TextColor(sc.Primary)
				})
				m3.EmphasizedText(c, m3.TitleMedium, title).TextColor(sc.OnSurface)
			})
			ui.Box(c).Height(1).FillWidth().Background(sc.OutlineVariant.Alpha(0.1))
			build()
		})
}

func settingLabel(c *ui.Context, title, body string) ui.Element {
	sc := m3.Of(c).Scheme
	return ui.Column(c).Gap(3).MinWidth(0).Children(func() {
		m3.EmphasizedText(c, m3.BodyLarge, title).TextColor(sc.OnSurface)
		if body != "" {
			m3.Text(c, m3.BodySmall, body).TextColor(sc.OnSurfaceVariant).LineHeight(1.35)
		}
	})
}

func (a *app) appearanceCard(c *ui.Context) {
	sc := m3.Of(c).Scheme
	th := m3.Of(c)
	a.settingsCard(c, "Appearance", m3.IconPalette, func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Theme", "Follow the desktop, or stay light or dark.").Grow(1)
			mode := int(th.Mode)
			if m3.ButtonGroup(c, "mode", &mode, []string{"System", "Light", "Dark"}, nil) {
				a.settings.SetMode(m3.Modes()[mode])
				a.saveSettings()
			}
		})

		ui.Row(c).Gap(16).AlignItems(ui.Center).Wrap().Children(func() {
			settingLabel(c, "Colour source", "").Grow(1)
			source := 0
			if a.settings.Matugen {
				source = 2
			} else if a.settings.Dynamic {
				source = 1
			}
			if m3.ButtonGroup(c, "colour-source", &source, []string{"Fixed", "Artwork", "Matugen"}, nil) {
				a.settings.Dynamic, a.settings.Matugen = source == 1, source == 2
				a.saveSettings()
			}
		})
		if a.settings.Matugen && a.colors == nil {
			m3.Text(c, m3.BodyMedium, "Waiting for colors.json from matugen.").TextColor(sc.OnSurfaceVariant)
		}

		if !a.settings.Dynamic && !a.settings.Matugen {
			ui.Column(c).Gap(14).Children(func() {
				settingLabel(c, "Colour", "Every colour in the app grows from this one.")
				ui.Row(c).Gap(8).Wrap().Children(func() {
					for _, swatch := range swatches {
						if a.swatch(c, swatch.name, swatch.hex) {
							a.settings.Seed, a.settings.Dynamic, a.settings.Matugen = swatch.hex, false, false
							a.saveSettings()
						}
					}
				})
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					m3.Text(c, m3.LabelLarge, "Hue").TextColor(sc.OnSurfaceVariant).Width(36).Shrink(0)
					hue, _ := m3.HueOf(th.Seed)
					value := hue
					slider := m3.Slider(c, &value, 0, 360, m3.SliderSpec{Label: "Hue", Thickness: 16, Hue: true, Key: "hue"})
					if slider.Changed() {
						a.settings.Seed, a.settings.Dynamic, a.settings.Matugen = settings.SeedHex(m3.FromHue(value)), false, false
						a.saveSettings()
					}
				})
			})
		}

		if !a.settings.Matugen {
			ui.Column(c).Gap(14).Children(func() {
				settingLabel(c, "Palette", "How boldly the colour is used.")
				ui.Row(c).Gap(12).Wrap().Children(func() {
					for _, style := range m3.Styles() {
						if a.styleTile(c, style) {
							a.settings.Style = int(style)
							a.saveSettings()
						}
					}
				})
			})
		}

		// The bar that shows the position in a track: a wave that travels, or
		// a flat line for those who would rather it kept still.
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Wavy progress bar", "Draw the position slider as a wave that moves while music plays. Turn it off for a flat bar.").Grow(1)
			on := a.settings.WavyProgress
			if m3.Switch(c, &on, "Use wavy progress bar").Changed() {
				a.settings.WavyProgress = on
				a.saveSettings()
			}
		})

		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Zoom", "Make everything larger or smaller.").Grow(1)
			zoom := settings.ClampZoom(a.settings.Zoom)
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				a.hinted(c, func() {
					if m3.IconButton(c, m3.IconButtonSpec{
						Icon: m3.IconRemove, Label: "Zoom out", Kind: m3.StandardIcon, Size: m3.Small40,
						Key: "zoom-out", Disabled: zoom <= settings.MinZoom,
					}).Clicked() {
						a.zoomBy(-1)
					}
				}, keymap.ZoomOut)
				m3.EmphasizedText(c, m3.TitleMedium, strconv.Itoa(zoom)+"%").Width(64).TextAlign(ui.Center).TextColor(sc.OnSurface)
				a.hinted(c, func() {
					if m3.IconButton(c, m3.IconButtonSpec{
						Icon: m3.IconAdd, Label: "Zoom in", Kind: m3.StandardIcon, Size: m3.Small40,
						Key: "zoom-in", Disabled: zoom >= settings.MaxZoom,
					}).Clicked() {
						a.zoomBy(1)
					}
				}, keymap.ZoomIn)
			})
		})
	})
}

func (a *app) playbackCacheCard(c *ui.Context) {
	sc := m3.Of(c).Scheme
	a.settingsCard(c, "Playback cache", m3.IconFolder, func() {
		ui.Column(c).Gap(12).Children(func() {
			settingLabel(c, "Keep recent songs", "Enter any non-negative whole number and press Enter to apply. Set 0 to turn the cache off. Older songs are removed first; each is kept in the format YouTube serves it, in a private Echo Music cache folder.")
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				input := ui.TextInputBase(c.Key("audio-cache-limit"), &a.cacheSongsText).
					Width(128).Height(48).Shrink(0).Padding(12).Radius(m3.Medium).Background(sc.SurfaceContainerHigh).
					TextColor(sc.OnSurface).FontSize(16).Label("Songs to cache").Placeholder("10")
				m3.Text(c, m3.BodyMedium, "songs").Width(48).Shrink(0).TextColor(sc.OnSurfaceVariant)
				if input.Submitted() {
					if limit, err := strconv.Atoi(strings.TrimSpace(a.cacheSongsText)); err == nil && limit >= 0 {
						a.setAudioCacheLimit(limit)
						a.cacheLimitError = ""
					} else {
						a.cacheLimitError = "Enter a whole number of 0 or more."
					}
				}
			})
			if a.cacheLimitError != "" {
				m3.Text(c, m3.BodyMedium, a.cacheLimitError).TextColor(sc.Error)
			}
			settingLabel(c, "Cache folder", "Echo Music creates a private ‘Echo Music Audio Cache’ folder inside this location. Changing folders removes the songs Echo Music cached in the old one.")
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				location := a.cacheDirectoryText
				if location == "" {
					location = "Default app cache folder"
				}
				m3.Text(c, m3.BodyMedium, location).TextColor(sc.OnSurfaceVariant).MinWidth(0).Grow(1).SingleLine()
				if m3.IconButton(c, m3.IconButtonSpec{Icon: m3.IconFolder, Label: "Choose cache folder", Kind: m3.OutlinedIcon, Size: m3.Small40, Key: "audio-cache-folder"}).Clicked() {
					a.chooseAudioCacheDirectory()
				}
			})
			if a.cacheDirError != "" {
				m3.Text(c, m3.BodyMedium, a.cacheDirError).TextColor(sc.Error)
			}
		})
	})
}

func (a *app) soundCard(c *ui.Context) {
	sc := m3.Of(c).Scheme
	a.settingsCard(c, "Sound", m3.IconVolumeUp, func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Normalize loudness", "Adjust track gain to keep songs at a more consistent volume. Requires a signed-in YouTube session.").Grow(1)
			on := a.settings.NormalizeLoudness
			if m3.Switch(c, &on, "Normalize loudness").Changed() {
				a.setNormalizeLoudness(on)
			}
		})
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Logarithmic volume", "Give the volume slider finer steps at low volumes.").Grow(1)
			on := a.settings.LogVolume
			if m3.Switch(c, &on, "Logarithmic volume").Changed() {
				a.settings.LogVolume = on
				a.saveSettings()
				a.applyLoudness()
			}
		})
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Equaliser", "Shape ten bands of the sound as you adjust them, without restarting the stream.").Grow(1)
			on := a.settings.EQOn
			if m3.Switch(c, &on, "Equaliser on").Changed() {
				a.settings.EQOn = on
				a.applyEQ()
			}
		})
		a.normalizeEQBands()
		ui.Row(c).Gap(8).Wrap().Children(func() {
			for _, preset := range eqPresets {
				selected := slices.Equal(a.settings.EQBands, preset.bands)
				if m3.Button(c, m3.ButtonSpec{
					Label: preset.name, Kind: m3.Tonal, Size: m3.ExtraSmall32,
					Toggle: true, Selected: selected, Disabled: !a.settings.EQOn, Key: "eq-preset-" + preset.name,
				}).Clicked() {
					copy(a.settings.EQBands, preset.bands)
					copy(a.eqApplied[:], preset.bands)
					a.applyEQ()
				}
			}
		})
		ui.Row(c).FillWidth().MaxWidth(640).Gap(4).Justify(ui.SpaceBetween).Children(func() {
			for i := range player.EQBandCount {
				ui.Column(c).Gap(6).Width(m3.MinTarget).AlignItems(ui.Center).Children(func() {
					m3.Slider(c, &a.settings.EQBands[i], -eqBoostRangeDB, eqBoostRangeDB, m3.SliderSpec{
						Label: eqLabels[i] + " dB", Key: "eq-" + strconv.Itoa(i),
						Thickness: 6, Vertical: true, Disabled: !a.settings.EQOn,
					}).Height(148).FillWidth()
					if a.eqApplied[i] != a.settings.EQBands[i] {
						copy(a.eqApplied[:], a.settings.EQBands)
						a.player.SetEQ(a.eqBands())
						a.scheduleEQ()
					}
					m3.Text(c, m3.LabelLarge, eqLabels[i]).TextColor(sc.OnSurfaceVariant)
				})
			}
		})
	})
}

func (a *app) playbackCard(c *ui.Context) {
	a.settingsCard(c, "Playback", m3.IconMusicNote, func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Play audio as my account", "Gives YouTube Premium its higher bitrate. Needs sign-in; starting a song takes a few seconds longer.").Grow(1)
			on := a.settings.AccountAudio
			if m3.Switch(c, &on, "Play audio as my account").Changed() {
				a.settings.AccountAudio = on
				a.syncAudioAuth()
				a.saveSettings()
			}
		})
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Auto-skip disliked songs", "Only songs known to be disliked are skipped.").Grow(1)
			on := a.settings.AutoSkipDisliked
			if m3.Switch(c, &on, "Skip disliked songs").Changed() {
				a.settings.AutoSkipDisliked = on
				a.saveSettings()
			}
		})
	})
}

func (a *app) crossfadeCard(c *ui.Context) {
	sc := m3.Of(c).Scheme
	a.settingsCard(c, "Crossfade", m3.IconMusicNote, func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Fade between tracks", "").Grow(1)
			on := a.settings.Crossfade
			if m3.Switch(c, &on, "Crossfade on").Changed() {
				a.settings.Crossfade = on
				a.applyCrossfade()
				a.saveSettings()
			}
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			m3.Text(c, m3.LabelLarge, "Duration").TextColor(sc.OnSurfaceVariant).Width(64).Shrink(0)
			value := float64(settings.ClampCrossfade(a.settings.CrossfadeSeconds))
			slider := m3.Slider(c, &value, settings.MinCrossfade, settings.MaxCrossfade, m3.SliderSpec{
				Label: "Crossfade duration", Thickness: 16, Key: "crossfade-seconds", Disabled: !a.settings.Crossfade,
			}).Grow(1)
			if slider.Changed() {
				a.settings.CrossfadeSeconds = int(math.Round(value))
				a.applyCrossfade()
				a.saveSettings()
			}
			m3.EmphasizedText(c, m3.TitleMedium, strconv.Itoa(settings.ClampCrossfade(a.settings.CrossfadeSeconds))+" s").
				Width(48).Shrink(0).TextAlign(ui.End).TextColor(sc.OnSurface)
		})
	})
}

func (a *app) aiFilterCard(c *ui.Context) {
	a.settingsCard(c, "AI-generated songs", m3.IconMusicNote, func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Skip AI-generated songs",
				"Skip songs YouTube marks as made with AI. Echo Music checks before each song plays; unlabelled AI music may still play.").Grow(1)
			on := a.settings.ExcludeAIGenerated
			if m3.Switch(c, &on, "Exclude AI-generated songs").Changed() {
				a.setExcludeAIGenerated(on)
			}
		})
	})
}

func (a *app) swatch(c *ui.Context, name, hex string) bool {
	th := m3.Of(c)
	sc := th.Scheme
	seed, _ := settings.ParseSeed(hex)
	fill, on := seed, ui.RGB(255, 255, 255)
	if m3.Contrast(fill, on) < 3 {
		on = ui.RGB(24, 24, 27)
	}
	selected := !a.settings.Dynamic && !a.settings.Matugen && hex == a.settings.Seed
	b := ui.ButtonBase(c.Key("swatch-" + hex))
	rest := float32(24)
	if selected {
		rest = 14
	}
	radius := m3.Animate(b, "r", rest, m3.SpatialFast)
	if b.Pressed() {
		radius = 10
	}
	b.Size(48, 48).Radius(max(radius, 0)).Background(fill).Center().Cursor(ui.CursorPointer).Label(name).Tooltip(name)
	border := sc.OutlineVariant.Alpha(0.2)
	if selected {
		border = sc.Surface
	} else if b.Hovered() {
		border = sc.OnSurface.Alpha(0.25)
	}
	b.Border(2.5, border)
	if selected {
		m3.Elevation(c, b, 1)
	}
	b.Children(func() {
		if selected {
			ui.Icon(c, m3.IconCheck).FontSize(24).TextColor(on)
		}
	})
	return b.Clicked()
}

func (a *app) stylePreview(seed ui.Color, dark bool, style m3.Style) m3.Scheme {
	t := &a.theme
	if t.previews == nil || seed != t.previewSeed || dark != t.previewDark {
		t.previews, t.previewSeed, t.previewDark = make(map[m3.Style]m3.Scheme, len(m3.Styles())), seed, dark
	}
	if preview, ok := t.previews[style]; ok {
		return preview
	}
	preview := m3.NewScheme(m3.NewPalettes(seed, style), dark)
	t.previews[style] = preview
	return preview
}

func (a *app) styleTile(c *ui.Context, style m3.Style) bool {
	th := m3.Of(c)
	sc := th.Scheme
	preview := a.stylePreview(th.Seed, th.Dark, style)
	selected := th.Style == style
	b := ui.ButtonBase(c.Key("style-" + style.String()))
	b.Column().AlignItems(ui.Stretch).Width(132).MinHeight(105).Padding(12).Gap(8).Radius(m3.Large - 2).Cursor(ui.CursorPointer).
		Label(style.String())
	if style == m3.Gradient {
		vibrant := a.stylePreview(th.Seed, th.Dark, m3.Vibrant)
		b.LinearGradient(ui.LinearGradient{From: preview.PrimaryContainer, To: vibrant.PrimaryContainer, Angle: 135, Oklab: true})
	} else {
		b.Background(preview.PrimaryContainer)
	}
	border := sc.OutlineVariant.Alpha(0.18)
	if selected {
		border = sc.Primary
	} else if b.Hovered() {
		border = sc.OnSurface.Alpha(0.25)
	}
	b.Border(2.5, border)
	b.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			for _, dot := range []ui.Color{preview.Primary, preview.Secondary, preview.Tertiary} {
				ui.Box(c).Size(20, 20).Radius(m3.Full).Background(dot)
			}
			ui.Spacer(c)
			if selected {
				ui.Icon(c, m3.IconCheck).FontSize(18).TextColor(preview.OnPrimaryContainer)
			}
		})
		ui.Column(c).Gap(2).Children(func() {
			m3.EmphasizedText(c, m3.TitleSmall, style.String()).TextColor(preview.OnPrimaryContainer)
			m3.Text(c, m3.BodySmall, style.Blurb()).TextColor(preview.OnPrimaryContainer.Alpha(0.8)).MaxLines(2)
		})
	})
	return b.Clicked()
}

func (a *app) accountCard(c *ui.Context) {
	sc := m3.Of(c).Scheme
	a.settingsCard(c, "Account", m3.IconPerson, func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			if current := a.account.session; current != nil {
				a.avatar(c, current.name(), current.details.Thumbnail, 128, 56)
				ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
					m3.EmphasizedText(c, m3.TitleMedium, current.name()).SingleLine()
					if current.details.Email != "" {
						m3.Text(c, m3.BodyMedium, current.details.Email).SingleLine().TextColor(sc.OnSurfaceVariant)
					}
				})
				if m3.Button(c, m3.ButtonSpec{Label: "Sign out", Icon: m3.IconLogout, Kind: m3.Tonal, Size: m3.Small40, Key: "sign-out"}).Clicked() {
					a.signOut()
				}
				return
			}
			settingLabel(c, "Not signed in", "Sign in to see your library. Browsing and playing work without it.").Grow(1)
			if m3.Button(c, m3.ButtonSpec{Label: "Sign in", Icon: m3.IconLogin, Size: m3.Small40, Key: "sign-in"}).Clicked() {
				a.openSignIn()
			}
		})
		if s := &a.account.signIn; s.err != "" && !s.open {
			m3.Text(c, m3.BodyMedium, s.err).TextColor(sc.Error).MaxLines(3)
		}
	})
}

func (a *app) chooseAudioCacheDirectory() {
	defaultRoot, _ := mygo.App.Path(mygo.PathCache)
	current := a.cacheDirectoryText
	if current == "" {
		current = defaultRoot
	}
	limit, parent := a.settings.CacheSongs, a.desktop.window()
	type chosen struct {
		root  string
		cache *audio.Cache
		err   string
	}
	spawn(a, func() chosen {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: parent, Title: "Choose audio cache folder", DefaultPath: current,
			Directory: true, CreateDirectories: true,
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
		if filepath.Clean(root) == filepath.Clean(current) {
			return chosen{root: root}
		}
		cache, root, err := audio.Open(root, limit)
		if err != nil {
			return chosen{err: "Could not use that folder: " + err.Error()}
		}
		return chosen{root: root, cache: cache}
	}, func(c chosen) {
		if c.err != "" {
			a.cacheDirError = c.err
			return
		}
		if c.root == "" {
			return
		}
		if c.cache != nil {
			c.cache.SetLimit(a.settings.CacheSongs)
			if previous := a.replaceAudioCache(c.cache); previous != nil {
				a.run(func() {
					previous.Close()
					if previous.Dir() != c.cache.Dir() {
						previous.Purge()
					}
				})
			}
		}
		a.settings.CacheDirectory = c.root
		if filepath.Clean(c.root) == filepath.Clean(defaultRoot) {
			a.settings.CacheDirectory = ""
		}
		a.cacheDirectoryText, a.cacheDirError = c.root, ""
		a.saveSettings()
	})
}

func (a *app) desktopCard(c *ui.Context) {
	a.settingsCard(c, "Desktop", m3.IconNotifications, func() {
		ui.Column(c).Gap(4).Children(func() {
			if runtime.GOOS == "darwin" {
				a.notchSetting(c)
				ui.Row(c.Key("keep-running")).Gap(16).AlignItems(ui.Center).Padding(8, 0).Children(func() {
					settingLabel(c, "Keep running when closed", "Hide the window and keep Echo Music running. Reopen it from the Dock, notch or menu bar.").Grow(1)
					choice := a.closeToBackgroundChoice()
					on := choice != nil && *choice
					if m3.Switch(c, &on, "Keep running in background").Changed() {
						a.answerClose(on, true)
					}
				})
			} else {
				a.desktopSwitch(c, "Track notifications", "Announce each new track. Clicking one brings Echo Music back.",
					a.settings.NotifyTrack, func(on bool) {
						a.settings.NotifyTrack = on
						if !on {
							a.notificationArtwork.stop()
						}
					})
				a.desktopSwitch(c, "Menu bar icon", "Keep Echo Music in the menu bar with play, next and quit.",
					a.settings.ShowTray, func(on bool) {
						a.settings.ShowTray = on
						a.syncDesktop()
					})
				if trayMarkSupported(runtime.GOOS) {
					ui.Row(c).Gap(16).AlignItems(ui.Center).Padding(8, 0).Children(func() {
						settingLabel(c, "Tray icon", "Choose which icon appears in the system tray; the O mark is more subtle.").Grow(1)
						mark := 0
						if a.settings.TrayMark == settings.TrayMarkOMark {
							mark = 1
						}
						if m3.ButtonGroup(c, "tray-mark", &mark, []string{"Wordmark", "O mark"}, nil) {
							if mark == 1 {
								a.settings.TrayMark = settings.TrayMarkOMark
							} else {
								a.settings.TrayMark = settings.TrayMarkWordmark
							}
							a.syncDesktop()
							a.saveSettings()
						}
					})
				}
				a.desktopSwitch(c, "Close to tray", "Closing the window keeps Echo Music running in the tray instead of quitting.",
					a.settings.CloseToTray != nil && *a.settings.CloseToTray, func(on bool) {
						a.answerClose(on, true)
					})
			}
			a.desktopSwitch(c, "Global shortcut", "Play or pause from anywhere with "+toggleShortcut+".",
				a.settings.GlobalShortcut, func(on bool) {
					a.settings.GlobalShortcut = on
					a.syncShortcut()
				})
			a.desktopSwitch(c, "Discord Rich Presence", "Share the track, artist, cover and elapsed time while music plays. The status clears when playback pauses or stops.",
				a.settings.DiscordPresence, func(on bool) {
					a.settings.DiscordPresence = on
					a.syncSystemMedia()
				})
			a.desktopSwitch(c, "Open at login", "Start Echo Music when you log in.",
				a.settings.OpenAtLogin, func(on bool) {
					a.settings.OpenAtLogin = on
					a.desktop.setOpenAtLogin(on)
				})
		})
	})
}

func (a *app) desktopSwitch(c *ui.Context, title, body string, on bool, apply func(bool)) {
	ui.Row(c).Gap(16).AlignItems(ui.Center).Padding(6, 0).Children(func() {
		settingLabel(c, title, body).Grow(1)
		if m3.Switch(c, &on, title).Changed() {
			apply(on)
			a.saveSettings()
		}
	})
}
