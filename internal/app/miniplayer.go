package app

import (
	"runtime"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/m3"
)

const (
	miniWidth  = 312
	miniArt    = 42
	miniHeight = 620

	miniPadding         = 10
	miniGap             = 6
	miniIconButtonSide  = 32
	miniVolumeSliderMax = miniWidth - 2*miniPadding - 2*(miniIconButtonSide+miniGap)
)

func (a *app) miniPlayer(c *ui.Context) {
	a.beginArtFrame()
	a.followArtwork()
	m3.Provide(c, a.resolveTheme(c))
	sc := m3.Of(c).Scheme
	surface := sc.SurfaceContainer
	if c.Vibrancy() {
		surface = ui.Transparent
	}
	col := ui.Column(c.Key("mini")).Fill().Padding(12, miniPadding, miniPadding, miniPadding).Gap(miniGap).Background(surface)
	if runtime.GOOS != "darwin" {
		c.Root().Background(ui.Transparent)
		col.Radius(m3.ExtraLarge)
	}
	col.Children(func() {
		ui.Column(c.Key("mini-content")).Grow(1).MinHeight(0).Gap(miniGap).Children(func() {
			ui.Column(c.Key("mini-card")).Padding(8, 10).Gap(6).Radius(m3.Large).
				Background(sc.SurfaceContainerLow).Border(1, sc.OutlineVariant.Alpha(0.22)).Shrink(0).Children(func() {
				ui.Row(c.Key("mini-player")).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
					a.playerArtwork(&a.miniCover, c, a.current.Thumbnail, playerArt, miniArt, 8, func() {
						if a.current.IsVideo() {
							m3.VideoBadge(c, miniArt)
						}
					})
					if a.current.VideoID == "" {
						ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
							m3.EmphasizedText(c, m3.TitleMedium, "Nothing playing").TextColor(sc.OnSurface)
							m3.Text(c, m3.BodySmall, "Pick something in Echo Music").TextColor(sc.OnSurfaceVariant)
						})
					} else {
						a.trackLine(c, m3.TitleSmall, m3.BodySmall, false).Grow(1)
					}
				})
				ui.Row(c.Key("mini-scrubber")).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).Children(func() {
					a.scrubber(c)
				})
				ui.Row(c.Key("mini-transport")).Justify(ui.Center).AlignItems(ui.Center).Shrink(0).Children(func() {
					a.transport(c, 36, 4)
				})
				ui.Row(c).Height(30).Padding(0, 8, 0, 2).Radius(m3.Full).
					Background(sc.SurfaceContainerHigh.Alpha(0.75)).AlignItems(ui.Center).Shrink(0).Children(func() {
					a.volumeControl(c, miniVolumeSliderMax)
				})
			})
			a.sidePanel(c, false, true)
		})
		ui.Row(c).Gap(8).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			if m3.Button(c, m3.ButtonSpec{Label: "Open Echo Music", Icon: m3.IconOpenInNew, Kind: m3.Tonal, Size: m3.Small40, Key: "mini-open"}).Clicked() {
				a.desktop.closeMini()
				a.desktop.bringToFront()
			}
			if m3.Button(c, m3.ButtonSpec{Label: "Quit", Kind: m3.TextOnly, Size: m3.Small40, Key: "mini-quit"}).Clicked() {
				mygo.App.Quit()
			}
		})
	})
}

type mini struct {
	win atomic.Pointer[mygo.Window]
}

func (d *desktop) showMini(view func(*ui.Context), shown func(bool)) {
	if d == nil || d.tray == nil {
		return
	}
	if d.mini.win.Load() != nil {
		return
	}
	fl := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:         d.tray,
		Placement:    mygo.PlacementBottom,
		Width:        miniWidth,
		Height:       miniHeight,
		Focusable:    true,
		Shadow:       true,
		Popover:      runtime.GOOS == "darwin",
		Vibrancy:     mygo.VibrancyPopover,
		CornerRadius: int(m3.ExtraLarge),
		Content:      ui.View(view),
	})
	d.mini.win.Store(fl)
	shown(true)
	fl.OnClosed(func() {
		d.mini.win.CompareAndSwap(fl, nil)
		d.update(func() { shown(false) })
	})
}

func (d *desktop) closeMini() {
	if d == nil {
		return
	}
	if fl := d.mini.win.Load(); fl != nil {
		fl.Close()
	}
}
