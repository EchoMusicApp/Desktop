// Package stemplayer plays the four separated stems through four synchronized
// players. Toggling a stem only changes that player's volume: no file render,
// no source reload and no audible gap.
package stemplayer

import (
	"errors"
	"sync"
	"time"

	"github.com/EchoMusicApp/Echo-Music/internal/player"
)

const StemCount = 4

// Mixer owns the four stem players.
type Mixer struct {
	mu      sync.Mutex
	players [StemCount]*player.Player
	enabled [StemCount]bool
	master  float64
	active  bool
	videoID string
}

// New creates an empty mixer; players are created when a track starts.
func New() *Mixer {
	m := &Mixer{}
	for i := range m.enabled {
		m.enabled[i] = true
	}
	return m
}

// Start loads the four files at the same position. All sources are started
// before the mixer becomes active, so a failure never leaves half a mix.
func (m *Mixer) Start(videoID string, paths [StemCount]string, at time.Duration, playing bool, master float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	created := [StemCount]*player.Player{}
	for i, path := range paths {
		if path == "" {
			return errors.New("stemplayer: missing stem path")
		}
		p := player.New()
		created[i] = p
		if err := p.PlayAt(path, at, !playing); err != nil {
			for _, other := range created {
				if other != nil {
					other.Stop()
				}
			}
			return err
		}
	}
	for i, p := range created {
		m.players[i] = p
	}
	m.enabled = [StemCount]bool{true, true, true, true}
	m.master = master
	m.videoID = videoID
	m.active = true
	m.applyVolumesLocked()
	return nil
}

func (m *Mixer) applyVolumesLocked() {
	for i, p := range m.players {
		if p == nil {
			continue
		}
		gain := 0.0
		if m.enabled[i] {
			gain = m.master
		}
		p.SetVolume(gain)
	}
}

// SetEnabled changes only that player's volume.
func (m *Mixer) SetEnabled(index int, on bool) {
	if index < 0 || index >= StemCount {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return
	}
	m.enabled[index] = on
	m.applyVolumesLocked()
}

func (m *Mixer) Enabled() [StemCount]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// SetPlaying pauses or resumes all four players together.
func (m *Mixer) SetPlaying(playing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return
	}
	for _, p := range m.players {
		if p == nil {
			continue
		}
		if playing && !p.Playing() {
			p.Resume()
		} else if !playing && p.Playing() {
			p.Pause()
		}
	}
}

func (m *Mixer) Seek(at time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return
	}
	for _, p := range m.players {
		if p != nil {
			p.Seek(at)
		}
	}
}

func (m *Mixer) Sync(at time.Duration, playing bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return
	}
	for _, p := range m.players {
		if p == nil {
			continue
		}
		if playing && !p.Playing() {
			p.Resume()
		} else if !playing && p.Playing() {
			p.Pause()
		}
		diff := p.Position() - at
		if diff < 0 {
			diff = -diff
		}
		if diff > 250*time.Millisecond {
			p.Seek(at)
		}
	}
}

func (m *Mixer) IsPlaying() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.players {
		if p != nil && p.Playing() {
			return true
		}
	}
	return false
}

func (m *Mixer) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

func (m *Mixer) VideoID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.videoID
}

func (m *Mixer) stopLocked() {
	for i, p := range m.players {
		if p != nil {
			p.Stop()
		}
		m.players[i] = nil
	}
	m.active = false
	m.videoID = ""
}

func (m *Mixer) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}
