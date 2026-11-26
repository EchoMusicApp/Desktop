package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/lansync"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

type hostPairState struct {
	open      bool
	peer      lansync.Peer
	code      string
	expiresAt time.Time
}

func randomSixDigitCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "482910"
	}
	return fmt.Sprintf("%06d", 100000+n.Int64())
}

func randomPairToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (a *app) startDesktopPairing(peer lansync.Peer) {
	if a.lan == nil {
		return
	}
	code := randomSixDigitCode()
	exp := time.Now().Add(60 * time.Second)
	hp := &hostPairState{
		open:      true,
		peer:      peer,
		code:      code,
		expiresAt: exp,
	}
	a.hostPair = hp
	mgr := a.lan
	req := lansync.PairStartPayload{
		DeviceID:    mgr.DeviceID(),
		DeviceName:  mgr.DeviceName(),
		Platform:    "desktop",
		Port:        mgr.Port(),
		ExpiresAtMs: exp.UnixMilli(),
	}
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		_ = mgr.SendPairStart(ctx, peer, req)
		cancel()
	})
	a.refresh()
}

func (a *app) hostPairDialog(c *ui.Context) {
	hp := a.hostPair
	if hp == nil {
		return
	}
	if !hp.open {
		a.hostPair = nil
		return
	}
	if time.Now().After(hp.expiresAt) {
		// Auto-refresh 6-digit code every 60s
		a.startDesktopPairing(hp.peer)
		return
	}
	sc := m3.Of(c).Scheme
	m3.Dialog(c, &hp.open, 420, func() {
		m3.EmphasizedText(c, m3.HeadlineSmall, "Pair with "+hp.peer.Name)
		m3.Text(c, m3.BodyMedium, "Enter this 6-digit code in the popup on "+hp.peer.Name+" to bind devices and sync.").TextColor(sc.OnSurfaceVariant)
		spaced := hp.code[:3] + " " + hp.code[3:]
		m3.EmphasizedText(c, m3.DisplaySmall, spaced).TextColor(sc.Primary)
		rem := max(0, int(time.Until(hp.expiresAt).Seconds()))
		m3.Text(c, m3.LabelMedium, fmt.Sprintf("Refreshes in %ds", rem)).TextColor(sc.OnSurfaceVariant)
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if m3.Button(c, m3.ButtonSpec{Label: "Cancel", Kind: m3.TextOnly, Key: "host-pair-cancel"}).Clicked() {
				a.hostPair = nil
			}
		})
	})
}

func (a *app) handleDesktopPairVerify(req lansync.PairVerifyRequest) lansync.PairVerifyResponse {
	hp := a.hostPair
	if hp == nil || time.Now().After(hp.expiresAt) {
		return lansync.PairVerifyResponse{OK: false, Error: "Code expired. Check Desktop for the new code."}
	}
	if strings.TrimSpace(req.Code) != hp.code {
		return lansync.PairVerifyResponse{OK: false, Error: "Incorrect 6-digit code."}
	}
	token := randomPairToken()
	boundID := req.DeviceID
	if boundID == "" {
		boundID = hp.peer.ID
	}
	boundName := req.DeviceName
	if boundName == "" {
		boundName = hp.peer.Name
	}
	a.settings.PairedPeerID = boundID
	a.settings.PairedPeerName = boundName
	a.settings.PairToken = token
	a.saveSettings()
	targetPeer := hp.peer
	a.hostPair = nil
	a.lanStatus = "Paired with " + boundName
	a.appendSyncLog("Paired with " + boundName)
	a.forceSyncWithPeer(targetPeer)
	a.refresh()
	return lansync.PairVerifyResponse{OK: true, PairToken: token}
}

type pairDialogState struct {
	open      bool
	peer      lansync.Peer
	code      string
	expiresAt time.Time
	busy      bool
	err       string
}

func (a *app) pairCodeDialog(c *ui.Context) {
	d := a.pairDialog
	if d == nil {
		return
	}
	if !d.open {
		a.pairDialog = nil
		return
	}
	sc := m3.Of(c).Scheme
	m3.Dialog(c, &d.open, 420, func() {
		m3.EmphasizedText(c, m3.HeadlineSmall, "Pair with "+d.peer.Name)
		m3.Text(c, m3.BodyMedium, "Enter the 6-digit Echo Connect code shown on your phone to bind devices and sync your Google account & playlists.").TextColor(sc.OnSurfaceVariant)
		rem := int(time.Until(d.expiresAt).Seconds())
		if rem > 0 {
			m3.Text(c, m3.LabelMedium, fmt.Sprintf("Code expires in %ds", rem)).TextColor(sc.Primary)
		} else {
			m3.Text(c, m3.LabelMedium, "Waiting for refreshed code from phone...").TextColor(sc.Error)
		}
		box := ui.Column(c.Key("pair-code-box")).Padding(12, 16).Radius(m3.Large).Background(sc.SurfaceContainerHighest)
		if box.FocusWithin() {
			box.Border(2, sc.Primary)
		}
		box.Children(func() {
			ui.TextInputBase(c.Key("pair-code-field"), &d.code).
				Placeholder("6-digit code").Label("Pairing Code").TextColor(sc.OnSurface).FontSize(18).AutoFocus().
				OnSubmit(a.submitPairCode)
		})
		if d.err != "" {
			m3.Text(c, m3.BodyMedium, d.err).TextColor(sc.Error).MaxLines(3)
		}
		ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if d.busy {
				m3.LoadingIndicator(c, 36, false)
			}
			if m3.Button(c, m3.ButtonSpec{Label: "Cancel", Kind: m3.TextOnly, Key: "pair-cancel"}).Clicked() {
				a.pairDialog = nil
			}
			codeTrim := strings.TrimSpace(d.code)
			if m3.Button(c, m3.ButtonSpec{Label: "Pair", Disabled: d.busy || len(codeTrim) != 6, Key: "pair-submit"}).Clicked() {
				a.submitPairCode()
			}
		})
	})
}

func (a *app) submitPairCode() {
	d := a.pairDialog
	if d == nil || d.busy || a.lan == nil {
		return
	}
	code := strings.TrimSpace(d.code)
	if len(code) != 6 {
		d.err = "Enter the 6-digit code"
		return
	}
	d.busy, d.err = true, ""
	mgr := a.lan
	peer := d.peer
	req := lansync.PairVerifyRequest{
		DeviceID:   mgr.DeviceID(),
		DeviceName: mgr.DeviceName(),
		Code:       code,
	}
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		resp, err := mgr.SendPairVerify(ctx, peer, req)
		a.update(func() {
			if a.pairDialog != d {
				return
			}
			d.busy = false
			if err != nil {
				d.err = err.Error()
				a.refresh()
				return
			}
			if !resp.OK {
				d.err = resp.Error
				if d.err == "" {
					d.err = "Invalid or expired code"
				}
				a.refresh()
				return
			}
			a.settings.PairedPeerID = peer.ID
			a.settings.PairedPeerName = peer.Name
			a.settings.PairToken = resp.PairToken
			a.saveSettings()
			a.pairDialog = nil
			a.lanStatus = "Paired with " + peer.Name
			a.appendSyncLog("Paired with " + peer.Name)
			a.forceSyncWithPeer(peer)
			a.refresh()
		})
	})
}

func (a *app) unpairPeer(peer lansync.Peer) {
	a.settings.PairedPeerID = ""
	a.settings.PairedPeerName = ""
	a.settings.PairToken = ""
	a.saveSettings()
	a.appendSyncLog("Unpaired from " + peer.Name)
	if a.lan != nil {
		mgr := a.lan
		a.run(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = mgr.SendUnpair(ctx, peer, mgr.DeviceID())
		})
	}
	a.refresh()
}

func (a *app) appendSyncLog(msg string) {
	stamp := time.Now().Format("15:04:05")
	line := "[" + stamp + "] " + msg
	a.lanSyncLogs = append([]string{line}, a.lanSyncLogs...)
	if len(a.lanSyncLogs) > 20 {
		a.lanSyncLogs = a.lanSyncLogs[:20]
	}
}

func (a *app) forceSyncWithPeer(peer lansync.Peer) {
	if a.lan == nil || a.lanSyncBusy {
		return
	}
	a.lanSyncBusy = true
	bundle := a.buildLocalSyncBundle()
	mgr := a.lan
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		reply, err := mgr.SendSync(ctx, peer, bundle)
		a.callMain(func() {
			a.lanSyncBusy = false
			if err != nil {
				a.lanStatus = "Sync failed: " + err.Error()
				a.appendSyncLog("Sync failed: " + err.Error())
			} else {
				a.applyIncomingSyncBundle(reply)
			}
			a.refresh()
		})
	})
}

func (a *app) buildLocalSyncBundle() lansync.SyncBundle {
	var acct *lansync.AccountSyncDTO
	if s := a.account.session; s != nil && s.cookie != "" {
		acct = &lansync.AccountSyncDTO{
			Cookie:        s.cookie,
			Name:          s.details.Name,
			Email:         s.details.Email,
			ChannelHandle: s.channel,
		}
	}
	var local []lansync.PlaylistSyncDTO
	for _, pl := range a.syncedPlaylists {
		if !pl.IsSpotify {
			local = append(local, pl)
		}
	}
	return lansync.SyncBundle{
		SenderID:       a.settings.DeviceID,
		SenderName:     a.settings.DeviceName,
		SentAtMs:       time.Now().UnixMilli(),
		GoogleAccount:  acct,
		LocalPlaylists: local,
		Episodes:       a.localEpisodesDTO(),
	}
}

func (a *app) applyIncomingSyncBundle(bundle lansync.SyncBundle) {
	a.lanLastSyncAt = time.Now()
	var parts []string

	// 1. Google Account auto-login if Desktop is not signed in (or signed in to a different email)
	if bundle.GoogleAccount != nil && bundle.GoogleAccount.Cookie != "" {
		in := bundle.GoogleAccount
		curEmail := ""
		if a.account.session != nil {
			curEmail = a.account.session.details.Email
		}
		if a.account.session == nil || (in.Email != "" && !strings.EqualFold(curEmail, in.Email)) {
			cookie := in.Cookie
			nameHint := in.Name
			a.run(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				opened, err := a.account.openSession(ctx, cookie, sessionSource{email: in.Email})
				if err == nil && opened != nil {
					if a.account.creds.store != nil {
						_ = a.account.creds.store.Save(ctx, cookie)
					}
					a.callMain(func() {
						a.useSession(opened, sessionSource{})
						label := opened.details.Name
						if label == "" {
							label = nameHint
						}
						a.appendSyncLog("Google account signed in (" + label + ")")
						a.refresh()
					})
				} else if err != nil {
					a.callMain(func() {
						a.appendSyncLog("Account sync failed: " + err.Error())
					})
				}
			})
			parts = append(parts, "Account")
		} else {
			parts = append(parts, "Account (up to date)")
		}
	}

	// 2. Local playlists + Spotify playlists (no overlap / deduplicated)
	localCount := a.mergeSyncedPlaylists(bundle.LocalPlaylists, false)
	spotifyCount := a.mergeSyncedPlaylists(bundle.SpotifyPlaylists, true)
	if localCount > 0 {
		parts = append(parts, fmt.Sprintf("%d local playlists", localCount))
	}
	if spotifyCount > 0 {
		parts = append(parts, fmt.Sprintf("%d Spotify playlists", spotifyCount))
	}

	// 3. Podcast episodes
	if len(bundle.Episodes) > 0 {
		a.mergeEpisodesFromLAN(bundle.Episodes)
		parts = append(parts, fmt.Sprintf("%d episodes", len(bundle.Episodes)))
	}

	summary := "Synced"
	if len(parts) > 0 {
		summary += ": " + strings.Join(parts, ", ")
	}
	a.lanStatus = summary
	a.appendSyncLog(summary)
	if r, _ := a.route(); r.id == routeLibrary {
		a.navigate(routeLibrary)
	}
}

func (a *app) mergeSyncedPlaylists(incoming []lansync.PlaylistSyncDTO, isSpotify bool) int {
	if len(incoming) == 0 {
		return 0
	}
	count := 0
	for _, in := range incoming {
		in.IsSpotify = isSpotify
		if in.SyncID == "" {
			prefix := "local:"
			if isSpotify {
				prefix = "spotify:"
			}
			in.SyncID = prefix + strings.ToLower(strings.TrimSpace(in.Title))
		}
		idx := slices.IndexFunc(a.syncedPlaylists, func(existing lansync.PlaylistSyncDTO) bool {
			return existing.SyncID == in.SyncID ||
				(existing.IsSpotify == isSpotify && strings.EqualFold(strings.TrimSpace(existing.Title), strings.TrimSpace(in.Title)))
		})
		if idx == -1 {
			in.Tracks = dedupeTrackDTOs(in.Tracks)
			a.syncedPlaylists = append(a.syncedPlaylists, in)
		} else {
			cur := a.syncedPlaylists[idx]
			if in.UpdatedAtMs >= cur.UpdatedAtMs {
				cur.Title = in.Title
				if in.Thumbnail != "" {
					cur.Thumbnail = in.Thumbnail
				}
				cur.UpdatedAtMs = in.UpdatedAtMs
			}
			cur.Tracks = dedupeTrackDTOs(append(cur.Tracks, in.Tracks...))
			a.syncedPlaylists[idx] = cur
		}
		count++
	}
	a.saveSyncedPlaylists()
	return count
}

func dedupeTrackDTOs(tracks []lansync.TrackDTO) []lansync.TrackDTO {
	seen := make(map[string]struct{}, len(tracks))
	out := make([]lansync.TrackDTO, 0, len(tracks))
	for _, t := range tracks {
		key := t.VideoID
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(t.Title)) + "|" + strings.ToLower(strings.TrimSpace(t.Artist))
		}
		if key == "|" || key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	return out
}

func syncedPlaylistsPath() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "echo_connect_playlists.json")
}

func (a *app) loadSyncedPlaylists() {
	p := syncedPlaylistsPath()
	if p == "" {
		return
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var list []lansync.PlaylistSyncDTO
	if err := json.Unmarshal(raw, &list); err == nil {
		a.syncedPlaylists = list
	}
}

func (a *app) saveSyncedPlaylists() {
	p := syncedPlaylistsPath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if raw, err := json.MarshalIndent(a.syncedPlaylists, "", "  "); err == nil {
		_ = os.WriteFile(p, raw, 0o600)
	}
}

func (a *app) syncedLibrarySections() []youtube.MusicSection {
	var localItems, spotifyItems []youtube.MusicItem
	for _, pl := range a.syncedPlaylists {
		sub := fmt.Sprintf("%d tracks", len(pl.Tracks))
		if pl.IsSpotify {
			sub = "Spotify · " + sub
		} else {
			sub = "Local · " + sub
		}
		item := youtube.MusicItem{
			ID:         "synced:" + pl.SyncID,
			PlaylistID: "synced:" + pl.SyncID,
			Title:      pl.Title,
			Subtitle:   sub,
			Thumbnail:  pl.Thumbnail,
			Kind:       youtube.KindPlaylist,
		}
		if pl.IsSpotify {
			spotifyItems = append(spotifyItems, item)
		} else {
			localItems = append(localItems, item)
		}
	}
	var sections []youtube.MusicSection
	if len(localItems) > 0 {
		sections = append(sections, youtube.MusicSection{Title: "Synced Local Playlists", Items: localItems})
	}
	if len(spotifyItems) > 0 {
		sections = append(sections, youtube.MusicSection{Title: "Spotify Playlists", Items: spotifyItems})
	}
	return sections
}

func (a *app) syncWithPairedPeer() {
	if a.lan == nil || a.settings.PairedPeerID == "" {
		return
	}
	for _, p := range a.lanPeers {
		if p.ID == a.settings.PairedPeerID {
			a.forceSyncWithPeer(p)
			return
		}
	}
}

func (a *app) syncedPlaylistBrowseResult(id string) *youtube.BrowseResult {
	syncID := strings.TrimPrefix(id, "synced:")
	for _, pl := range a.syncedPlaylists {
		if pl.SyncID == syncID {
			items := make([]youtube.MusicItem, 0, len(pl.Tracks))
			for _, t := range pl.Tracks {
				if t.VideoID == "" && t.Title == "" {
					continue
				}
				items = append(items, trackDTOToMusicItem(t))
			}
			desc := "Synced Local Playlist"
			if pl.IsSpotify {
				desc = "Synced from Spotify"
			}
			return &youtube.BrowseResult{
				Title:       pl.Title,
				Description: desc,
				Thumbnail:   pl.Thumbnail,
				Items:       items,
			}
		}
	}
	return nil
}
