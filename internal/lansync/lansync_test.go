package lansync

import (
	"context"
	"net"
	"testing"
	"time"
)

type fakeHandler struct {
	state    PeerState
	episodes []EpisodeDTO
}

func (f *fakeHandler) GetState() PeerState { return f.state }
func (f *fakeHandler) ApplyControl(cmd ControlCommand) PeerState {
	switch cmd.Action {
	case "play":
		f.state.Playing = true
	case "pause":
		f.state.Playing = false
	case "seek":
		f.state.PositionMs = cmd.PositionMs
	case "volume":
		f.state.Volume = cmd.Volume
	}
	return f.state
}
func (f *fakeHandler) ApplyHandoff(payload HandoffPayload) PeerState {
	f.state.Queue = payload.Items
	f.state.Index = payload.Index
	f.state.PositionMs = payload.PositionMs
	f.state.Source = payload.Source
	f.state.Playing = payload.PlayImmediately
	f.episodes = MergeEpisodeLists(f.episodes, payload.Episodes)
	return f.state
}
func (f *fakeHandler) MergeEpisodes(episodes []EpisodeDTO) []EpisodeDTO {
	f.episodes = MergeEpisodeLists(f.episodes, episodes)
	return f.episodes
}
func (f *fakeHandler) OnPeersChanged([]Peer)                 {}
func (f *fakeHandler) OnPairStart(PairStartPayload)          {}
func (f *fakeHandler) OnPairVerify(PairVerifyRequest) PairVerifyResponse {
	return PairVerifyResponse{OK: true}
}
func (f *fakeHandler) OnUnpair(string)                       {}
func (f *fakeHandler) BuildSyncBundle() SyncBundle           { return SyncBundle{} }
func (f *fakeHandler) ApplySyncBundle(b SyncBundle) SyncBundle { return b }

func TestMDNSPacketRoundtrip(t *testing.T) {
	pkt := buildServiceResponse(
		ServiceType,
		"EchoMusic-abc123._echomusic._tcp.local.",
		"echomusic-abc123.local.",
		42420,
		net.IPv4(192, 168, 1, 55),
		map[string]string{
			"id":       "abc123",
			"name":     "Pixel 7a",
			"platform": "android",
			"version":  "1",
		},
	)

	msg, err := parseDNSMessage(pkt)
	if err != nil {
		t.Fatalf("parseDNSMessage failed: %v", err)
	}
	if !msg.isResponse {
		t.Fatal("expected response packet")
	}

	svc := newMDNSService("desktop-1", "MacBook", "desktop", 50000, nil)
	svc.handlePacket(nil, &net.UDPAddr{IP: net.IPv4(192, 168, 1, 55), Port: 5353}, pkt)

	peers := svc.Peers()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].ID != "abc123" || peers[0].Name != "Pixel 7a" || peers[0].Platform != "android" || peers[0].Port != 42420 {
		t.Fatalf("unexpected peer: %+v", peers[0])
	}
}

func TestHTTPControlHandoffAndEpisodeSync(t *testing.T) {
	h := &fakeHandler{
		state: PeerState{
			DeviceID:   "desk-1",
			DeviceName: "Mac",
			Platform:   "desktop",
			Volume:     0.5,
		},
		episodes: []EpisodeDTO{{VideoID: "ep1", PositionMs: 30000, UpdatedAt: 100}},
	}
	mgr := NewManager("desk-1", "Mac", "desktop", h)
	if err := mgr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Stop()

	peer := Peer{ID: "desk-1", Name: "Mac", Host: "127.0.0.1", Port: mgr.Port(), LastSeen: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	st, err := mgr.SendControl(ctx, peer, ControlCommand{Action: "volume", Volume: 0.85})
	if err != nil || st.Volume != 0.85 {
		t.Fatalf("volume control failed: st=%+v err=%v", st, err)
	}

	st, err = mgr.SendHandoff(ctx, peer, HandoffPayload{
		Items:           []TrackDTO{{VideoID: "song1", Title: "Hello", Artist: "Adele"}},
		Index:           0,
		PositionMs:      42500,
		PlayImmediately: true,
		Episodes:        []EpisodeDTO{{VideoID: "ep1", PositionMs: 90000, UpdatedAt: 200}},
	})
	if err != nil || !st.Playing || st.PositionMs != 42500 || len(st.Queue) != 1 {
		t.Fatalf("handoff failed: st=%+v err=%v", st, err)
	}

	merged, err := mgr.SyncEpisodes(ctx, peer, []EpisodeDTO{{VideoID: "ep2", PositionMs: 12000, UpdatedAt: 150}})
	if err != nil || len(merged) != 2 {
		t.Fatalf("episode sync failed: merged=%+v err=%v", merged, err)
	}
}
