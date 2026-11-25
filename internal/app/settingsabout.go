package app

import (
	"context"
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/report"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
)

func (a *app) updatesCard(c *ui.Context) {
	a.settingsCard(c, "Updates", m3.IconRefresh, func() {
		version := currentAppVersion()
		ui.Column(c).Gap(12).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				settingLabel(c, "Echo Music "+version, a.updateSubtitle()).Grow(1)
				label := "Check for updates"
				if a.updates.check.busy() {
					label = "Checking…"
				}
				if m3.Button(c, m3.ButtonSpec{
					Label: label, Kind: m3.Tonal, Size: m3.Small40, Disabled: a.updates.check.busy(), Key: "check-updates",
				}).Clicked() {
					a.checkForUpdates(true)
				}
			})
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				settingLabel(c, "Check automatically", "Check GitHub releases for a new version once a day.").Grow(1)
				on := a.autoCheckUpdatesOn()
				if m3.Switch(c, &on, "Check automatically").Changed() {
					a.setAutoCheckUpdates(on)
				}
			})
		})
	})
}

func (a *app) updateSubtitle() string {
	if a.updates.available != "" {
		return "Echo Music " + a.updates.available + " is available."
	}
	if !a.updates.lastChecked.IsZero() {
		return "Last checked " + a.updates.lastChecked.Format("Jan 2, 15:04") + "."
	}
	return "Checks version tags from GitHub releases."
}

type supportOption struct {
	key, title, subtitle, target string
	isLink                       bool
}

var supportOptions = []supportOption{
	{key: "coffee", title: "Buy Me a Coffee", subtitle: "buymeacoffee.com/iad1tya", target: "https://buymeacoffee.com/iad1tya", isLink: true},
	{key: "upi", title: "UPI / GPay", subtitle: "iad1tya@upi", target: "iad1tya@upi"},
	{key: "github", title: "GitHub Sponsors", subtitle: "github.com/sponsors/iad1tya", target: "https://github.com/sponsors/iad1tya", isLink: true},
	{key: "patreon", title: "Patreon", subtitle: "patreon.com/cw/iad1tya", target: "https://www.patreon.com/cw/iad1tya", isLink: true},
	{key: "btc", title: "Bitcoin", subtitle: "bc1qcvyr7eekha8uytmffcvgzf4h7xy7shqzke35fy", target: "bc1qcvyr7eekha8uytmffcvgzf4h7xy7shqzke35fy"},
	{key: "eth", title: "Ethereum", subtitle: "0x51bc91022E2dCef9974D5db2A0e22d57B360e700", target: "0x51bc91022E2dCef9974D5db2A0e22d57B360e700"},
	{key: "sol", title: "Solana", subtitle: "9wjca3EQnEiqzqgy7N5iqS1JGXJiknMQv6zHgL96t94S", target: "9wjca3EQnEiqzqgy7N5iqS1JGXJiknMQv6zHgL96t94S"},
}

func (a *app) supportCard(c *ui.Context) {
	sc := m3.Of(c).Scheme
	a.settingsCard(c, "Support my work", m3.IconCoffee, func() {
		m3.Text(c, m3.BodyMedium, "Every coffee, sponsorship, or share keeps the projects going.").TextColor(sc.OnSurfaceVariant)
		ui.Column(c).Gap(2).Children(func() {
			for i, opt := range supportOptions {
				if i > 0 {
					ui.Box(c).Height(1).FillWidth().Background(sc.OutlineVariant.Alpha(0.12))
				}
				ui.Row(c).Gap(16).AlignItems(ui.Center).Padding(8, 0).Children(func() {
					settingLabel(c, opt.title, opt.subtitle).Grow(1).MinWidth(0)
					if opt.isLink {
						if m3.Button(c, m3.ButtonSpec{Label: "Visit", Kind: m3.Tonal, Size: m3.Small40, Key: "support-visit-" + opt.key}).Clicked() {
							a.openSupportURL(opt.title, opt.target)
						}
					} else {
						if m3.Button(c, m3.ButtonSpec{Label: "Copy", Kind: m3.Tonal, Size: m3.Small40, Key: "support-copy-" + opt.key}).Clicked() {
							c.WriteClipboard(opt.target)
							a.notice = "Copied " + opt.title + " address"
						}
					}
				})
			}
		})
	})
}

func (a *app) openSupportURL(name, link string) {
	spawn(a, func() error { return a.openURL(link) }, func(err error) {
		if err != nil {
			a.notice = "Could not open " + name + ": " + err.Error()
		}
	})
}

func (a *app) loadToolVersions() {
	if a.toolsLoaded || a.toolsLookup.busy() {
		return
	}
	version := mygo.App.Version()
	startTask(a, &a.toolsLookup, reportTimeout, func(ctx context.Context) (report.Info, error) {
		return a.collectInfo(ctx, version), nil
	}, func(info report.Info, _ error) {
		a.tools, a.toolsLoaded = info, true
	})
}

func (a *app) toolsCard(c *ui.Context) {
	a.settingsCard(c, "Playback tools", m3.IconSettings, func() {
		yt, ff := "Looking up…", "Looking up…"
		if a.toolsLoaded {
			yt, ff = a.tools.YtDlp, a.tools.FFmpeg
		}
		settingLabel(c, "yt-dlp "+yt, "Finds the audio of each track. The full build includes it; the minimal build uses PATH and needs Node.js or Bun.")
		settingLabel(c, "ffmpeg "+ff, "Decodes audio for playback. The full build includes it; the minimal build uses the version on PATH.")
	})
}

const reportTimeout = 15 * time.Second

func (a *app) reportCard(c *ui.Context) {
	sc := m3.Of(c).Scheme
	a.settingsCard(c, "Support", m3.IconError, func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			settingLabel(c, "Something not working?", "Open a new issue on GitHub with the Echo Music, system, yt-dlp and ffmpeg versions filled in. Nothing about your account or listening is included.").Grow(1)
			spec := m3.ButtonSpec{Label: "Report a problem", Kind: m3.Tonal, Size: m3.Small40, Key: "report-problem"}
			if a.report.busy() {
				spec.Disabled = true
			}
			if m3.Button(c, spec).Clicked() {
				a.reportProblem()
			}
		})
		if a.reportError != "" {
			m3.Text(c, m3.BodyMedium, a.reportError).TextColor(sc.Error).MaxLines(3)
		}
	})
}

func (a *app) reportProblem() {
	a.reportError = ""
	version := mygo.App.Version()
	startTask(a, &a.report, reportTimeout, func(ctx context.Context) (string, error) {
		return a.collectInfo(ctx, version).URL(), nil
	}, func(link string, err error) {
		if err == nil {
			err = a.openURL(link)
		}
		if err != nil {
			log.Printf("report: %v", err)
			a.reportError = "Could not open the browser: " + err.Error()
		}
	})
}

func (a *app) logsCard(c *ui.Context) {
	a.settingsCard(c, "Logs", m3.IconFolder, func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			body := "Echo Music keeps a log of what it does and what goes wrong, with no passwords or cookies in it."
			if a.logsDir == "" {
				body = "Echo Music could not open its log file, so nothing is being logged."
			}
			settingLabel(c, "Log files", body).Grow(1)
			if m3.Button(c, m3.ButtonSpec{Label: "Open logs folder", Kind: m3.Tonal, Size: m3.Small40, Disabled: a.logsDir == "", Key: "open-logs"}).Clicked() {
				a.openLogs()
			}
		})
	})
}

func (a *app) openLogs() {
	dir := a.logsDir
	if dir == "" {
		return
	}
	spawn(a, func() error { return a.openPath(dir) }, func(err error) {
		if err != nil {
			a.notice = "Could not open the logs folder: " + err.Error()
		}
	})
}
