package stemplayer

import "testing"

func TestMixerStateWithoutPlayers(t *testing.T) {
	m := New()
	if m.Active() || m.VideoID() != "" || m.IsPlaying() {
		t.Fatal("a fresh mixer must be inactive")
	}
	if enabled := m.Enabled(); enabled != [StemCount]bool{true, true, true, true} {
		t.Fatalf("enabled = %v", enabled)
	}
	m.SetEnabled(3, false) // ignored while inactive
	if enabled := m.Enabled(); !enabled[3] {
		t.Fatalf("inactive mixer changed: %v", enabled)
	}
	m.SetEnabled(-1, true)
	m.SetEnabled(StemCount, true)
	m.Seek(0)
	m.SetPlaying(true)
	m.Stop()
}

func TestStemCount(t *testing.T) {
	if StemCount != 4 {
		t.Fatalf("StemCount = %d", StemCount)
	}
}
