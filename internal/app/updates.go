package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/m3"
)

const (
	updateDownloadURL      = "https://desktop.echomusic.fun/update"
	githubLatestReleaseURL = "https://api.github.com/repos/EchoMusicApp/Desktop/releases/latest"
	updateCheckTimeout     = 10 * time.Second
)

type updateState struct {
	check       task
	checkedOnce bool
	lastChecked time.Time
	available   string
	dialogOpen  bool
}

func currentAppVersion() string {
	if v := strings.TrimSpace(mygo.App.Version()); v != "" {
		return v
	}
	return "1.0.0"
}

func (a *app) autoCheckUpdatesOn() bool {
	return a.settings.AutoCheckUpdates == nil || *a.settings.AutoCheckUpdates
}

func (a *app) setAutoCheckUpdates(on bool) {
	v := on
	a.settings.AutoCheckUpdates = &v
	a.saveSettings()
}

func (a *app) checkForUpdates(manual bool) {
	a.updates.checkedOnce = true
	if a.updates.check.busy() {
		return
	}
	startTask(a, &a.updates.check, updateCheckTimeout, func(ctx context.Context) (string, error) {
		return fetchLatestReleaseTag(ctx, githubLatestReleaseURL)
	}, func(tag string, err error) {
		if err != nil {
			if manual {
				a.notice = "Could not check for updates."
			}
			return
		}
		a.updates.lastChecked = time.Now()
		latest := strings.TrimPrefix(strings.TrimSpace(tag), "v")
		current := currentAppVersion()
		if isNewerVersion(latest, current) {
			a.updates.available = latest
			a.updates.dialogOpen = true
			return
		}
		a.updates.available = ""
		if manual {
			a.notice = "Echo Music is up to date."
		}
	})
}

func fetchLatestReleaseTag(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Echo-Music-Desktop")
	resp, err := (&http.Client{Timeout: updateCheckTimeout}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases returned %d", resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.TagName, nil
}

func isNewerVersion(latest, current string) bool {
	a := parseVersionParts(latest)
	b := parseVersionParts(current)
	for i := range max(len(a), len(b)) {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}

func parseVersionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	fields := strings.Split(v, ".")
	parts := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			n = 0
		}
		parts = append(parts, n)
	}
	return parts
}

func (a *app) updateDialog(c *ui.Context) {
	sc := m3.Of(c).Scheme
	m3.Dialog(c, &a.updates.dialogOpen, 420, func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(36, 36).Radius(m3.Full).Background(sc.PrimaryContainer).Center().Children(func() {
				ui.Icon(c, m3.IconRefresh).FontSize(20).TextColor(sc.OnPrimaryContainer)
			})
			m3.EmphasizedText(c, m3.HeadlineSmall, "Update available").TextColor(sc.OnSurface)
		})
		m3.Text(c, m3.BodyMedium, fmt.Sprintf(
			"Echo Music %s is available (you are on %s). Visit desktop.echomusic.fun/update to download the latest version.",
			a.updates.available, currentAppVersion(),
		)).TextColor(sc.OnSurfaceVariant).LineHeight(1.45)
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			if m3.Button(c, m3.ButtonSpec{Label: "Later", Kind: m3.TextOnly, Key: "update-later"}).Clicked() {
				a.updates.dialogOpen = false
			}
			if m3.Button(c, m3.ButtonSpec{Label: "Download update", Kind: m3.Filled, Key: "update-download"}).Clicked() {
				a.updates.dialogOpen = false
				spawn(a, func() error { return a.openURL(updateDownloadURL) }, func(err error) {
					if err != nil {
						a.notice = "Could not open " + updateDownloadURL
					}
				})
			}
		})
	})
}
