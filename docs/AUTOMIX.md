# Advanced automix

Ported from the Android and Flutter clients. Enable the parts you want under
**Settings → Playback & Sound → Crossfade**; they are off by default.

- **Beat alignment** analyses the playing song (BPM, beat phase, mix-in/mix-out
  points and musical key) from its local audio. The crossfade becomes 16 beats
  of the outgoing track, clamped to 4–15 s, and the incoming track starts at an
  offset that puts one of its beats at the end of the blend.
- **Harmonic mixing** maps each analysed track to the Camelot wheel and prefers
  an upcoming track in the same or an adjacent key. Tracks without an analysis
  keep the natural queue order.
- **Gapless** removes the overlap entirely.
- The analysis is cached as JSON under `Echo Music Automix Cache` beside the
  audio cache. Standby preparation times feed an adaptive pre-arm lead (0.8–12 s).

Failures in analysis only disable the automix extras; playback, queue and
crossfade keep working normally.
