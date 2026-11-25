package app

import (
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"github.com/EchoMusicApp/Echo-Music/internal/history"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

func (a *app) recordHistory(item youtube.MusicItem) {
	if item.VideoID == "" {
		return
	}
	a.loadHistory()
	a.history = history.Append(a.history, history.Entry{
		VideoID:  item.VideoID,
		Title:    item.Title,
		Artist:   shownArtist(item),
		PlayedAt: time.Now(),
	})
	a.saveHistory()
}

func (a *app) loadHistory() {
	if a.historyLoaded || a.historyLoading {
		return
	}
	a.historyLoading = true
	path := a.historyPath
	spawn(a, func() []history.Entry { return history.Read(path) }, func(entries []history.Entry) {
		pending := len(a.history) > 0
		if a.historyDiscard {
			entries, pending, a.historyDiscard = nil, true, false
		}
		a.history = history.Append(entries, a.history...)
		a.historyLoaded, a.historyLoading = true, false
		if pending {
			a.saveHistory()
		}
	})
}

func (a *app) saveHistory() {
	if a.historyPath == "" || !a.historyLoaded {
		return
	}
	a.historyWriter.Save(a.historyPath, a.history)
}

func (a *app) clearHistory() {
	a.history = nil
	if !a.historyLoaded {
		a.historyDiscard = true
		a.loadHistory()
		return
	}
	a.saveHistory()
}

type statsMemo struct {
	summary history.Summary
	entries int
	last    time.Time
	days    int
	today   int
	valid   bool
}

func (a *app) statsSummary(now time.Time, days int) history.Summary {
	var last time.Time
	if n := len(a.history); n > 0 {
		last = a.history[n-1].PlayedAt
	}
	memo := &a.stats
	today := history.CalendarDay(now)
	if memo.valid && memo.entries == len(a.history) && memo.last.Equal(last) && memo.days == days && memo.today == today {
		return memo.summary
	}
	*memo = statsMemo{
		summary: history.Summarize(a.history, now, days),
		entries: len(a.history), last: last, days: days, today: today, valid: true,
	}
	return memo.summary
}

var statsRanges = []int{7, 14, 30}

func (a *app) statsPage(c *ui.Context) {
	summary := a.statsSummary(c.Now(), a.statsRangeDays)
	ui.Scroll(c).Grow(1).TrackScroll(a.page.scroll).Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(8, pageGutter, a.clearance()).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(840).Gap(20).Children(func() {
				ui.Row(c).Gap(16).AlignItems(ui.Center).Wrap().Children(func() {
					settingLabel(c, "Listening stats", "From plays on this device.").Grow(1)
					labels := []string{"7 days", "14 days", "30 days"}
					picked := 1
					for i, days := range statsRanges {
						if a.statsRangeDays == days {
							picked = i
						}
					}
					if m3.ButtonGroup(c, "stats-range", &picked, labels, nil) {
						a.statsRangeDays = statsRanges[picked]
					}
				})
				if summary.Plays == 0 && a.historyLoaded {
					a.message(c, m3.IconTrending, "No plays yet", "Songs you listen to in Echo Music are counted here, for the last "+strconv.Itoa(a.statsRangeDays)+" days.", "", nil)
					return
				}
				a.statsTotals(c, summary)
				a.playsChart(c, summary)
				w, _ := c.Size()
				if w >= 780 && len(summary.TopTracks) > 0 && len(summary.TopArtists) > 0 {
					ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Stretch).Children(func() {
						ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
							a.topList(c, "Most played", summary.TopTracks, true)
						})
						ui.Column(c).Grow(1).Basis(0).MinWidth(0).Children(func() {
							a.topList(c, "Top artists", summary.TopArtists, false)
						})
					})
				} else {
					a.topList(c, "Most played", summary.TopTracks, true)
					a.topList(c, "Top artists", summary.TopArtists, false)
				}
			})
		})
	})
}

func (a *app) statsTotals(c *ui.Context, summary history.Summary) {
	sc := m3.Of(c).Scheme
	avgPerDay := 0
	if a.statsRangeDays > 0 {
		avgPerDay = (summary.Plays + a.statsRangeDays/2) / a.statsRangeDays
	}
	cards := []struct {
		value, label string
		icon         *ui.SVG
	}{
		{strconv.Itoa(summary.Plays), "Total plays", m3.IconPlay},
		{strconv.Itoa(summary.Artists), "Unique artists", m3.IconPerson},
		{strconv.Itoa(avgPerDay), "Avg per day", m3.IconTrending},
	}
	ui.Row(c).FillWidth().Gap(14).Children(func() {
		for _, card := range cards {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Padding(18, 20).Gap(10).Radius(m3.ExtraLarge).
				Background(sc.SurfaceContainerLow).Border(1, sc.OutlineVariant.Alpha(0.25)).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(28, 28).Radius(m3.Small).Background(sc.PrimaryContainer.Alpha(0.55)).Center().Children(func() {
						ui.Icon(c, card.icon).FontSize(16).TextColor(sc.OnPrimaryContainer)
					})
					m3.Text(c, m3.LabelMedium, card.label).TextColor(sc.OnSurfaceVariant).SingleLine()
				})
				m3.EmphasizedText(c, m3.DisplaySmall, card.value).TextColor(sc.OnSurface)
			})
		}
	})
}

func (a *app) playsChart(c *ui.Context, summary history.Summary) {
	sc := m3.Of(c).Scheme
	a.settingsCard(c, "Plays per day", m3.IconTrending, func() {
		most := 1
		labels := make([]string, len(summary.ByDay))
		for i, d := range summary.ByDay {
			most = max(most, d.Plays)
			labels[i] = d.Day.Format("Jan 2")
		}
		x := chart.NewBand(labels...).Padding(0.32, 0.2)
		y := chart.NewLinear(0, float64(most)).Nice(4)
		chart.Plot(c, chart.Axes{Left: y.Ticks(4)}, func(area ui.Element) {
			area.Height(190)
			for i, d := range summary.ByDay {
				span := x.Band(i)
				height := y.Span(0, float64(d.Plays))
				bar := chart.Rect(c, span, height)
				fill := sc.Primary
				if d.Plays == 0 {
					fill = sc.SurfaceContainerHighest.Alpha(0.55)
				}
				bar.Background(fill).Radius(m3.Small)
			}
		}).Label("Plays per day").Gap(8)
	})
}

func (a *app) topList(c *ui.Context, title string, counts []history.Count, showArtist bool) {
	if len(counts) == 0 {
		return
	}
	sc := m3.Of(c).Scheme
	icon := m3.IconMusicNote
	if !showArtist {
		icon = m3.IconPerson
	}
	a.settingsCard(c, title, icon, func() {
		ui.Column(c).Gap(2).Children(func() {
			for i, tc := range counts {
				if i > 0 {
					ui.Box(c).Height(1).FillWidth().Background(sc.OutlineVariant.Alpha(0.12))
				}
				ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(10, 4).Children(func() {
					rankBg, rankFg := sc.SurfaceContainerHigh, sc.OnSurfaceVariant
					if i < 3 {
						rankBg, rankFg = sc.PrimaryContainer.Alpha(0.75), sc.OnPrimaryContainer
					}
					ui.Box(c).Size(26, 26).Radius(m3.Full).Background(rankBg).Center().Shrink(0).Children(func() {
						m3.Text(c, m3.LabelMedium, strconv.Itoa(i+1)).TextColor(rankFg).FontWeight(600)
					})
					ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
						name := tc.Title
						if name == "" {
							name = "Unknown track"
						}
						m3.EmphasizedText(c, m3.BodyMedium, name).SingleLine().TextColor(sc.OnSurface)
						if showArtist && tc.Artist != "" {
							m3.Text(c, m3.BodySmall, tc.Artist).SingleLine().TextColor(sc.OnSurfaceVariant)
						}
					})
					ui.Box(c).Padding(4, 10).Radius(m3.Full).Background(sc.SurfaceContainerHigh).Shrink(0).Children(func() {
						m3.Text(c, m3.LabelSmall, pluralPlays(tc.Plays)).TextColor(sc.OnSurfaceVariant).SingleLine()
					})
				})
			}
		})
	})
}

func pluralPlays(n int) string {
	if n == 1 {
		return "1 play"
	}
	return strconv.Itoa(n) + " plays"
}
