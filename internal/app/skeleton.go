package app

import (
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

func skeletonPulse(c *ui.Context) ui.Color {
	sc := m3.Of(c).Scheme
	c.AnimationFrame()
	t := float64(c.Now().UnixNano()) / 1e9
	alpha := float32(0.42 + 0.24*(0.5+0.5*math.Sin(t*3.2)))
	return sc.SurfaceContainerHighest.Alpha(alpha)
}

func skeletonBox(c *ui.Context, w, h, radius float32, tone ui.Color) ui.Element {
	return ui.Box(c).Size(w, h).Radius(radius).Background(tone).Shrink(0)
}

func (a *app) pageSkeleton(c *ui.Context) {
	tone := skeletonPulse(c)
	r, _ := a.route()
	switch {
	case r.isDetail():
		a.detailSkeleton(c, r, tone)
	case r.grid:
		a.gridSkeleton(c, tone)
	case r.id == routeSearch:
		a.searchSkeleton(c, tone)
	case r.id == routePodcasts:
		a.podcastsSkeleton(c, tone)
	default:
		a.feedSkeleton(c, tone)
	}
}

func (a *app) streamingSkeleton(c *ui.Context) {
	tone := skeletonPulse(c)
	r, _ := a.route()
	if r.isDetail() || r.id == routeSearch {
		isPodcast := r.target == youtube.TargetPodcast
		ui.Column(c).Padding(8, pageGutter-8, 16, pageGutter-8).Gap(4).Children(func() {
			for i := 0; i < 3; i++ {
				if isPodcast {
					a.episodeRowSkeleton(c, tone)
				} else {
					a.trackRowSkeleton(c, false, tone)
				}
			}
		})
		return
	}
	a.cardShelfSkeleton(c, r.id == routePodcasts, tone)
}

func (a *app) podcastsSkeleton(c *ui.Context, tone ui.Color) {
	ui.Column(c).Padding(4, 0, 24, 0).Gap(16).Children(func() {
		a.cardShelfSkeleton(c, false, tone) // Popular shows (square)
		a.cardShelfSkeleton(c, true, tone)  // Popular episodes (wide 16:9)
		a.cardShelfSkeleton(c, true, tone)  // Category episodes (wide 16:9)
	})
}

func (a *app) feedSkeleton(c *ui.Context, tone ui.Color) {
	ui.Column(c).Padding(4, 0, 24, 0).Gap(16).Children(func() {
		a.cardShelfSkeleton(c, false, tone)
		a.columnShelfSkeleton(c, false, tone)
		a.cardShelfSkeleton(c, false, tone)
	})
}

func (a *app) cardShelfSkeleton(c *ui.Context, wide bool, tone ui.Color) {
	w, h := float32(cardWidth), float32(cardArt)
	if wide {
		w, h = float32(cardWideWidth), float32(cardWideArtH)
	}
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Padding(16, pageGutter, 4, pageGutter).Children(func() {
			skeletonBox(c, 180, 24, m3.Small, tone)
		})
		ui.Row(c).Padding(0, pageGutter-8).Gap(shelfGap).Clip().Children(func() {
			for i := 0; i < 6; i++ {
				a.cardSkeleton(c, w, h, tone)
			}
		})
	})
}

func (a *app) cardSkeleton(c *ui.Context, w, h float32, tone ui.Color) {
	ui.Column(c).Width(w).Shrink(0).Padding(0, 0, 12, 0).Gap(10).Children(func() {
		skeletonBox(c, w, h, m3.Medium, tone)
		ui.Column(c).Padding(0, 4).Gap(6).Children(func() {
			skeletonBox(c, w*0.82, 14, m3.Full, tone)
			skeletonBox(c, w*0.56, 12, m3.Full, tone)
		})
	})
}

func (a *app) columnShelfSkeleton(c *ui.Context, wide bool, tone ui.Color) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Padding(16, pageGutter, 4, pageGutter).Children(func() {
			skeletonBox(c, 160, 24, m3.Small, tone)
		})
		ui.Row(c).Padding(0, pageGutter-8).Gap(shelfGap).Clip().Children(func() {
			for col := 0; col < 3; col++ {
				ui.Column(c).Width(columnWidth).Shrink(0).Children(func() {
					for row := 0; row < 4; row++ {
						a.trackRowSkeleton(c, wide, tone)
					}
				})
			}
		})
	})
}

func (a *app) gridSkeleton(c *ui.Context, tone ui.Color) {
	cols := max(1, a.gridColumns(&route{grid: true}))
	ui.Column(c).Padding(16, pageGutter-8).Gap(16).Children(func() {
		for r := 0; r < 3; r++ {
			ui.Row(c).Gap(cardGap).Children(func() {
				for col := 0; col < cols; col++ {
					a.cardSkeleton(c, cardWidth, cardArt, tone)
				}
			})
		}
	})
}

func (a *app) detailSkeleton(c *ui.Context, r *route, tone ui.Color) {
	artSize, gap := float32(220), float32(28)
	if a.narrowHero() {
		artSize, gap = 150, 20
	}
	artRadius := float32(m3.Medium)
	if r.target == youtube.TargetArtist {
		artRadius = m3.Full
	}
	isPodcast := r.target == youtube.TargetPodcast
	ui.Column(c).Children(func() {
		ui.Row(c).Padding(8, pageGutter, 28).Gap(gap).AlignItems(ui.Center).Children(func() {
			skeletonBox(c, artSize, artSize, artRadius, tone)
			ui.Column(c).Grow(1).MinWidth(0).Gap(10).Children(func() {
				skeletonBox(c, 76, 12, m3.Full, tone)
				skeletonBox(c, 280, 32, m3.Small, tone)
				skeletonBox(c, 180, 16, m3.Full, tone)
				if isPodcast {
					skeletonBox(c, 420, 14, m3.Full, tone)
					skeletonBox(c, 360, 14, m3.Full, tone)
				}
				ui.Box(c).Height(6)
				ui.Row(c).Gap(8).Children(func() {
					if !isPodcast {
						skeletonBox(c, 96, 40, m3.Full, tone)
						skeletonBox(c, 110, 40, m3.Full, tone)
					} else {
						skeletonBox(c, 128, 40, m3.Full, tone)
					}
				})
			})
		})
		ui.Column(c).Padding(0, pageGutter-8).Gap(4).Children(func() {
			for i := 0; i < 6; i++ {
				if isPodcast {
					a.episodeRowSkeleton(c, tone)
				} else {
					a.trackRowSkeleton(c, false, tone)
				}
			}
		})
	})
}

func (a *app) searchSkeleton(c *ui.Context, tone ui.Color) {
	ui.Column(c).Padding(8, pageGutter-8).Gap(16).Children(func() {
		ui.Row(c).Padding(12, 12).Gap(16).AlignItems(ui.Center).Children(func() {
			skeletonBox(c, 72, 72, m3.Medium, tone)
			ui.Column(c).Gap(8).Grow(1).Children(func() {
				skeletonBox(c, 220, 20, m3.Small, tone)
				skeletonBox(c, 140, 14, m3.Full, tone)
			})
		})
		for i := 0; i < 7; i++ {
			a.trackRowSkeleton(c, false, tone)
		}
	})
}

func (a *app) episodeRowSkeleton(c *ui.Context, tone ui.Color) {
	const artH = float32(96)
	const artW = float32(170)
	ui.Row(c).Height(120).Padding(8, 8).Gap(16).AlignItems(ui.Center).Children(func() {
		skeletonBox(c, artW, artH, m3.Medium, tone)
		ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
			skeletonBox(c, 280, 16, m3.Full, tone)
			skeletonBox(c, 420, 12, m3.Full, tone)
			skeletonBox(c, 320, 12, m3.Full, tone)
			skeletonBox(c, 160, 12, m3.Full, tone)
		})
	})
}

func (a *app) trackRowSkeleton(c *ui.Context, wide bool, tone ui.Color) {
	h, artH, artW := float32(rowHeight), float32(48), float32(48)
	if wide {
		artW = float32(int(artH * 16 / 9))
	}
	ui.Row(c).Height(h).Padding(8, 8).Gap(12).AlignItems(ui.Center).Children(func() {
		skeletonBox(c, artW, artH, m3.Medium, tone)
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			skeletonBox(c, 200, 14, m3.Full, tone)
			skeletonBox(c, 130, 12, m3.Full, tone)
		})
		skeletonBox(c, 36, 12, m3.Full, tone)
	})
}
