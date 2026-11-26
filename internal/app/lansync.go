package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/EchoMusicApp/Echo-Music/internal/lansync"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/playstate"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

func (a *app) lanSyncEnabled() bool {
	return a.settings.LANSync == nil || *a.settings.LANSync
}

func (a *app) syncLANSync() {
	if !a.lanSyncEnabled() {
		a.closeLANSync()
		return
	}
	if a.lan != nil {
		return
	}
	mgr := lansync.NewManager(a.settings.DeviceID, a.settings.DeviceName, "desktop", &appLANHandler{a: a})
	if a.settings.DeviceID == "" {
		a.settings.DeviceID = mgr.DeviceID()
		a.saveSettings()
	}
	if err := mgr.Start(context.Background()); err != nil {
		a.lanError = err.Error()
		log.Printf("lansync: failed to start: %v", err)
		return
	}
	a.lan = mgr
	a.lanError = ""
	a.lanPeers = mgr.Peers()
	log.Printf("lansync: started as %q (%s) on port %d", mgr.DeviceName(), mgr.DeviceID(), mgr.Port())
}

func (a *app) closeLANSync() {
	if a.lan != nil {
		a.lan.Stop()
		a.lan = nil
	}
	a.lanPeers = nil
	a.lanRemoteStates = nil
}

func (a *app) callMain(fn func()) {
	done := make(chan struct{})
	a.update(func() {
		defer close(done)
		fn()
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

type appLANHandler struct {
	a *app
}

func (h *appLANHandler) GetState() lansync.PeerState {
	var st lansync.PeerState
	h.a.callMain(func() {
		st = h.a.buildLANPeerState()
	})
	return st
}

func (h *appLANHandler) ApplyControl(cmd lansync.ControlCommand) lansync.PeerState {
	var st lansync.PeerState
	h.a.callMain(func() {
		a := h.a
		switch cmd.Action {
		case "play":
			a.resume()
		case "pause":
			a.pauseLocal()
			if cmd.ControllerPeerID != "" {
				a.attachLANControllerByID(cmd.ControllerPeerID)
			}
		case "toggle":
			a.togglePlayLocal()
		case "next":
			a.advanceLocal()
		case "previous":
			a.previousLocal()
		case "seek":
			a.seekLocal(time.Duration(cmd.PositionMs) * time.Millisecond)
		case "volume":
			v := cmd.Volume
			if v <= 1.0 && v >= 0 {
				v *= 100
			}
			a.setVolumeLocal(v)
			a.saveSettings()
		case "shuffle":
			if cmd.Shuffle != nil {
				a.setShuffleLocal(*cmd.Shuffle)
			}
		case "repeat":
			switch cmd.Repeat {
			case "one":
				a.repeat = repeatTrack
			case "all":
				a.repeat = repeatQueue
			default:
				a.repeat = repeatOff
			}
			a.syncPreload()
			a.syncSystemMedia()
		case "play_index":
			if cmd.Index >= 0 && cmd.Index < a.queue.len() {
				item := a.queue.items[cmd.Index]
				a.queue.index = cmd.Index
				a.startLocal(item)
			}
		case "queue_add":
			if cmd.Item != nil && cmd.Item.VideoID != "" {
				a.queue.insert(trackDTOToMusicItem(*cmd.Item), false, a.settings.AutoPlay)
				a.syncPreload()
			}
		}
		a.refresh()
		st = a.buildLANPeerState()
	})
	return st
}

func (h *appLANHandler) ApplyHandoff(payload lansync.HandoffPayload) lansync.PeerState {
	var st lansync.PeerState
	h.a.callMain(func() {
		a := h.a
		a.lanActivePeerID = ""
		a.lanSuppressUntil = time.Now().Add(2500 * time.Millisecond)
		if len(payload.Episodes) > 0 {
			a.mergeEpisodesFromLAN(payload.Episodes)
		}
		if len(payload.Items) > 0 {
			items := make([]youtube.MusicItem, 0, len(payload.Items))
			for _, dto := range payload.Items {
				if dto.VideoID != "" {
					items = append(items, trackDTOToMusicItem(dto))
				}
			}
			if len(items) > 0 {
				idx := payload.Index
				if idx < 0 || idx >= len(items) {
					idx = 0
				}
				target := items[idx]
				pos := time.Duration(payload.PositionMs) * time.Millisecond
				a.queue.replace(target, items, idx, payload.Source)
				a.current = target
				a.total = parseDuration(target.Duration)
				a.scrub = pos.Seconds()
				a.resumeID = target.VideoID
				a.resetUpNext(youtube.UpNextOptions{VideoID: target.VideoID})
				if payload.PlayImmediately {
					a.startTrackNow(target, pos, true)
				} else {
					a.pauseLocal()
				}
				a.savePlayback(0)
			}
		}
		a.refresh()
		st = a.buildLANPeerState()
	})
	return st
}

func (h *appLANHandler) MergeEpisodes(episodes []lansync.EpisodeDTO) []lansync.EpisodeDTO {
	var merged []lansync.EpisodeDTO
	h.a.callMain(func() {
		merged = h.a.mergeEpisodesFromLAN(episodes)
	})
	return merged
}

func (h *appLANHandler) OnPeersChanged(peers []lansync.Peer) {
	h.a.update(func() {
		h.a.lanPeers = peers
		h.a.refresh()
	})
}

func (a *app) buildLANPeerState() lansync.PeerState {
	name := a.settings.DeviceName
	id := a.settings.DeviceID
	if a.lan != nil {
		if name == "" {
			name = a.lan.DeviceName()
		}
		if id == "" {
			id = a.lan.DeviceID()
		}
	}
	repeat := "off"
	switch a.repeat {
	case repeatTrack:
		repeat = "one"
	case repeatQueue:
		repeat = "all"
	}
	queueDTOs := make([]lansync.TrackDTO, 0, len(a.queue.items))
	for _, it := range a.queue.items {
		queueDTOs = append(queueDTOs, musicItemToTrackDTO(it))
	}
	var currentDTO *lansync.TrackDTO
	if a.current.VideoID != "" {
		d := musicItemToTrackDTO(a.current)
		currentDTO = &d
	}
	return lansync.PeerState{
		DeviceID:   id,
		DeviceName: name,
		Platform:   "desktop",
		Playing:    a.player.Playing(),
		Volume:     a.volume / 100.0,
		PositionMs: a.playbackPosition().Milliseconds(),
		DurationMs: a.total.Milliseconds(),
		Shuffle:    a.shuffle,
		Repeat:     repeat,
		Current:    currentDTO,
		Queue:      queueDTOs,
		Index:      a.queue.index,
		Source:     a.queue.source,
		Episodes:   a.localEpisodesDTO(),
		UpdatedAt:  time.Now().UnixMilli(),
	}
}

func (a *app) localEpisodesDTO() []lansync.EpisodeDTO {
	out := make([]lansync.EpisodeDTO, 0, len(a.episodes))
	for _, ep := range a.episodes {
		out = append(out, lansync.EpisodeDTO{
			VideoID:    ep.VideoID,
			PositionMs: ep.Position.Milliseconds(),
		})
	}
	return out
}

func (a *app) mergeEpisodesFromLAN(remote []lansync.EpisodeDTO) []lansync.EpisodeDTO {
	merged := lansync.MergeEpisodeLists(a.localEpisodesDTO(), remote)
	next := make([]playstate.Episode, 0, len(merged))
	for _, ep := range merged {
		if ep.VideoID != "" && ep.PositionMs > 0 && !ep.Finished && len(next) < playstate.MaxEpisodes {
			next = append(next, playstate.Episode{
				VideoID:  ep.VideoID,
				Position: time.Duration(ep.PositionMs) * time.Millisecond,
			})
		}
	}
	a.episodes = next
	if a.playSave.writer.Path != "" {
		state := a.queue.state(a.playbackPosition())
		state.Episodes = a.episodes
		a.playSave.writer.Save(state)
	}
	return merged
}

func (a *app) activeLANPeer() (lansync.Peer, bool) {
	if a.lan == nil || a.lanActivePeerID == "" {
		return lansync.Peer{}, false
	}
	for _, p := range a.lanPeers {
		if p.ID == a.lanActivePeerID {
			return p, true
		}
	}
	return lansync.Peer{}, false
}

func (a *app) effectivePlaying() bool {
	if peer, ok := a.activeLANPeer(); ok {
		if st, exists := a.lanRemoteStates[peer.ID]; exists {
			return st.Playing
		}
		return false
	}
	return a.player.Playing()
}

func (a *app) attachLANControllerByID(peerID string) {
	if peerID == "" {
		return
	}
	for _, p := range a.lanPeers {
		if p.ID == peerID {
			a.attachLANController(p)
			return
		}
	}
	a.lanActivePeerID = peerID
}

func (a *app) attachLANController(peer lansync.Peer) {
	a.lanActivePeerID = peer.ID
	a.lanSuppressUntil = time.Now().Add(1500 * time.Millisecond)
	a.pauseLocal()
	a.lanStatus = "Controlling " + peer.Name
	if st, ok := a.lanRemoteStates[peer.ID]; ok {
		a.syncUIFromRemotePeerState(st)
	}
	a.refresh()
}

func (a *app) disconnectLANController() {
	a.lanActivePeerID = ""
	a.lanStatus = "Switched back to this Mac"
	a.refresh()
}

func (a *app) syncUIFromRemotePeerState(st lansync.PeerState) {
	if a.lanActivePeerID == "" || st.DeviceID != a.lanActivePeerID {
		return
	}
	if st.Current != nil && st.Current.VideoID != "" {
		item := trackDTOToMusicItem(*st.Current)
		if a.current.VideoID != item.VideoID || a.current.Title != item.Title {
			a.current = item
		}
		if st.DurationMs > 0 {
			a.total = time.Duration(st.DurationMs) * time.Millisecond
		} else {
			a.total = parseDuration(item.Duration)
		}
	}
	if !a.scrubbing {
		a.scrub = float64(st.PositionMs) / 1000.0
	}
	a.shuffle = st.Shuffle
	switch st.Repeat {
	case "one":
		a.repeat = repeatTrack
	case "all":
		a.repeat = repeatQueue
	default:
		a.repeat = repeatOff
	}
	if len(st.Queue) > 0 {
		items := make([]youtube.MusicItem, 0, len(st.Queue))
		for _, dto := range st.Queue {
			if dto.VideoID != "" {
				items = append(items, trackDTOToMusicItem(dto))
			}
		}
		if len(items) > 0 {
			idx := st.Index
			if idx < 0 || idx >= len(items) {
				idx = 0
			}
			a.queue.replace(items[idx], items, idx, st.Source)
		}
	}
}

func (a *app) sendTracksHandoffToPeer(peer lansync.Peer, tracks []youtube.MusicItem, index int, positionMs int64, source string) {
	if a.lan == nil || len(tracks) == 0 {
		return
	}
	if index < 0 || index >= len(tracks) {
		index = 0
	}
	a.lanActivePeerID = peer.ID
	a.lanSuppressUntil = time.Now().Add(1800 * time.Millisecond)
	dtos := make([]lansync.TrackDTO, 0, len(tracks))
	for _, it := range tracks {
		if it.VideoID != "" {
			dtos = append(dtos, musicItemToTrackDTO(it))
		}
	}
	if len(dtos) == 0 {
		return
	}
	if index >= len(dtos) {
		index = 0
	}
	payload := lansync.HandoffPayload{
		Items:           dtos,
		Index:           index,
		PositionMs:      positionMs,
		Source:          source,
		PlayImmediately: true,
		Episodes:        a.localEpisodesDTO(),
	}
	a.pauseLocal()
	mgr := a.lan
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st, err := mgr.SendHandoff(ctx, peer, payload)
		if err != nil {
			log.Printf("lansync: handoff to %s failed: %v", peer.Name, err)
			a.update(func() { a.lanStatus = "Handoff failed: " + err.Error(); a.refresh() })
			return
		}
		a.update(func() {
			if a.lanRemoteStates == nil {
				a.lanRemoteStates = make(map[string]lansync.PeerState)
			}
			a.lanRemoteStates[peer.ID] = st
			a.syncUIFromRemotePeerState(st)
			a.lanStatus = "Live Sync active on " + peer.Name
			a.refresh()
		})
	})
}

func (a *app) sendHandoffToPeer(peer lansync.Peer) {
	if a.lan == nil || a.current.VideoID == "" {
		return
	}
	a.lanActivePeerID = peer.ID
	a.lanSuppressUntil = time.Now().Add(1800 * time.Millisecond)
	st := a.buildLANPeerState()
	payload := lansync.HandoffPayload{
		Items:           st.Queue,
		Index:           st.Index,
		PositionMs:      st.PositionMs,
		Source:          st.Source,
		PlayImmediately: true,
		Episodes:        st.Episodes,
	}
	if len(payload.Items) == 0 && st.Current != nil {
		payload.Items = []lansync.TrackDTO{*st.Current}
		payload.Index = 0
	}
	a.pauseLocal()
	mgr := a.lan
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if st, err := mgr.SendHandoff(ctx, peer, payload); err != nil {
			log.Printf("lansync: handoff to %s failed: %v", peer.Name, err)
			a.update(func() { a.lanStatus = "Handoff failed: " + err.Error(); a.refresh() })
			return
		} else {
			a.update(func() {
				if a.lanRemoteStates == nil {
					a.lanRemoteStates = make(map[string]lansync.PeerState)
				}
				a.lanRemoteStates[peer.ID] = st
				a.syncUIFromRemotePeerState(st)
				a.lanStatus = "Live Sync active on " + peer.Name
				a.refresh()
			})
		}
	})
}

func (a *app) pullHandoffFromPeer(peer lansync.Peer) {
	if a.lan == nil {
		return
	}
	a.lanActivePeerID = ""
	a.lanSuppressUntil = time.Now().Add(2500 * time.Millisecond)
	mgr := a.lan
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		st, err := mgr.FetchState(ctx, peer)
		if err != nil {
			a.update(func() { a.lanStatus = "Pull failed: " + err.Error(); a.refresh() })
			return
		}
		if len(st.Queue) == 0 && st.Current == nil {
			a.update(func() { a.lanStatus = peer.Name + " has nothing playing"; a.refresh() })
			return
		}
		items := st.Queue
		idx := st.Index
		if len(items) == 0 && st.Current != nil {
			items = []lansync.TrackDTO{*st.Current}
			idx = 0
		}
		// Pause the remote peer so playback moves cleanly to Desktop, and tell it Desktop is now active.
		_, _ = mgr.SendControl(ctx, peer, lansync.ControlCommand{
			Action:           "pause",
			ControllerPeerID: a.settings.DeviceID,
		})
		(&appLANHandler{a: a}).ApplyHandoff(lansync.HandoffPayload{
			Items:           items,
			Index:           idx,
			PositionMs:      st.PositionMs,
			Source:          st.Source,
			PlayImmediately: true,
			Episodes:        st.Episodes,
		})
		a.update(func() { a.lanStatus = "Continuing from " + peer.Name; a.refresh() })
	})
}

func (a *app) refreshLANRemoteStates() {
	if a.lan == nil || len(a.lanPeers) == 0 {
		return
	}
	mgr := a.lan
	peers := append([]lansync.Peer(nil), a.lanPeers...)
	a.run(func() {
		for _, peer := range peers {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			st, err := mgr.FetchState(ctx, peer)
			cancel()
			if err == nil {
				p := peer
				s := st
				a.update(func() {
					if a.lanRemoteStates == nil {
						a.lanRemoteStates = make(map[string]lansync.PeerState)
					}
					a.lanRemoteStates[p.ID] = s
					if p.ID == a.lanActivePeerID {
						a.syncUIFromRemotePeerState(s)
					}
					a.refresh()
				})
			}
		}
	})
}

func (a *app) syncEpisodesWithPeers() {
	if a.lan == nil || len(a.lanPeers) == 0 {
		return
	}
	mgr := a.lan
	peers := append([]lansync.Peer(nil), a.lanPeers...)
	local := a.localEpisodesDTO()
	a.run(func() {
		for _, peer := range peers {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			merged, err := mgr.SyncEpisodes(ctx, peer, local)
			cancel()
			if err == nil && len(merged) > 0 {
				a.callMain(func() {
					a.mergeEpisodesFromLAN(merged)
				})
			}
		}
	})
}

func (a *app) sendRemoteControl(peer lansync.Peer, cmd lansync.ControlCommand) {
	if a.lan == nil {
		return
	}
	mgr := a.lan
	a.run(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		st, err := mgr.SendControl(ctx, peer, cmd)
		a.update(func() {
			if err != nil {
				a.lanStatus = "Control failed: " + err.Error()
			} else {
				if a.lanRemoteStates == nil {
					a.lanRemoteStates = make(map[string]lansync.PeerState)
				}
				a.lanRemoteStates[peer.ID] = st
				if peer.ID == a.lanActivePeerID {
					a.syncUIFromRemotePeerState(st)
				}
			}
			a.refresh()
		})
	})
}

func musicItemToTrackDTO(item youtube.MusicItem) lansync.TrackDTO {
	durSec := int(parseDuration(item.Duration).Seconds())
	return lansync.TrackDTO{
		VideoID:     item.VideoID,
		Title:       item.Title,
		Artist:      item.Artist,
		Album:       item.Album,
		DurationSec: durSec,
		Thumbnail:   item.Thumbnail,
		IsPodcast:   item.IsEpisode(),
	}
}

func trackDTOToMusicItem(dto lansync.TrackDTO) youtube.MusicItem {
	dur := ""
	if dto.DurationSec > 0 {
		m := dto.DurationSec / 60
		s := dto.DurationSec % 60
		dur = fmt.Sprintf("%d:%02d", m, s)
	}
	kind := youtube.KindTrack
	if dto.IsPodcast {
		kind = youtube.KindEpisode
	}
	return youtube.MusicItem{
		VideoID:   dto.VideoID,
		Title:     dto.Title,
		Artist:    dto.Artist,
		Album:     dto.Album,
		Duration:  dur,
		Thumbnail: dto.Thumbnail,
		Kind:      kind,
	}
}

func (a *app) lanSyncSetting(c *ui.Context) {
	sc := m3.Of(c).Scheme
	ui.Column(c.Key("lan-sync")).Gap(6).Children(func() {
		on := a.lanSyncEnabled()
		a.desktopSwitch(c, "Echo Connect (LAN Sync)", "Discover Echo Music on your Wi-Fi for instant playback handoff, remote control, and podcast progress sync.",
			on, func(v bool) {
				a.settings.LANSync = &v
				a.syncLANSync()
			})
		if on {
			if a.lanError != "" {
				m3.Text(c, m3.BodySmall, "LAN Sync error: "+a.lanError).TextColor(sc.Error)
			} else if a.lan != nil {
				m3.Text(c, m3.BodySmall, "Discoverable on Wi-Fi as "+a.lan.DeviceName()).TextColor(sc.OnSurfaceVariant)
			}
			if a.lanStatus != "" && !strings.Contains(a.lanStatus, "http://") {
				m3.Text(c, m3.BodySmall, a.lanStatus).TextColor(sc.Primary)
			}
			if len(a.lanPeers) == 0 {
				m3.Text(c, m3.BodySmall, "No nearby Echo Music devices found on this Wi-Fi yet.").TextColor(sc.OnSurfaceVariant)
			} else {
				ui.Column(c).Gap(8).Children(func() {
					for _, peer := range a.lanPeers {
						p := peer
						isLive := a.lanActivePeerID == p.ID
						st, hasState := a.lanRemoteStates[p.ID]
						peerIcon := m3.IconComputer
						if p.Platform == "android" {
							peerIcon = m3.IconSmartphone
						}
						card := ui.Column(c.Key("lan-peer-"+p.ID)).Gap(10).Padding(12).Radius(m3.Medium).Background(sc.SurfaceContainer)
						if isLive {
							card.Border(1, sc.Primary.Alpha(0.5))
						}
						card.Children(func() {
							ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
								iconColor := sc.OnSurfaceVariant
								if isLive {
									iconColor = sc.Primary
								}
								ui.Icon(c, peerIcon).FontSize(22).TextColor(iconColor).Shrink(0)
								ui.Column(c).Grow(1).Gap(2).Children(func() {
									title := p.Name
									if isLive {
										title += " · Active"
									}
									m3.Text(c, m3.BodyMedium, title).TextColor(sc.OnSurface).FontWeight(500)
									sub := "Ready on Wi-Fi"
									if hasState && st.Current != nil && st.Current.Title != "" {
										sub = st.Current.Title + " — " + st.Current.Artist
									}
									m3.Text(c, m3.BodySmall, sub).TextColor(sc.OnSurfaceVariant).SingleLine()
								})
							})
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								if isLive {
									if m3.Button(c, m3.ButtonSpec{Label: "Move here", Kind: m3.Tonal, Key: "lan-pull-" + p.ID}).Clicked() {
										a.pullHandoffFromPeer(p)
									}
									if m3.Button(c, m3.ButtonSpec{Label: "Disconnect", Kind: m3.Outlined, Key: "lan-disc-" + p.ID}).Clicked() {
										a.disconnectLANController()
									}
								} else {
									if a.current.VideoID != "" {
										if m3.Button(c, m3.ButtonSpec{Label: "Play on " + p.Name, Kind: m3.Tonal, Key: "lan-send-" + p.ID}).Clicked() {
											a.sendHandoffToPeer(p)
										}
									} else {
										if m3.Button(c, m3.ButtonSpec{Label: "Control", Kind: m3.Tonal, Key: "lan-ctrl-" + p.ID}).Clicked() {
											a.attachLANController(p)
										}
									}
									if hasState && st.Current != nil && st.Current.Title != "" {
										if m3.Button(c, m3.ButtonSpec{Label: "Move here", Kind: m3.Outlined, Key: "lan-pull-" + p.ID}).Clicked() {
											a.pullHandoffFromPeer(p)
										}
									}
								}
							})
						})
					}
				})
			}
		}
	})
}
