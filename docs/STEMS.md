# Stems (beta)

Stems let you mute or solo the four parts of a song while it plays: drums,
bass, other and vocals. The feature is off by default; enable it under
**Settings → Playback & Sound → Stems (beta)**.

## How it works

- Separation is fully offline. HT-Demucs (MIT), exported to ONNX by
  StemSplitio, runs through the bundled Python helper in `internal/stems`.
- On first use Echo Music checks Python and its `numpy`, `onnxruntime` and
  `soundfile` packages, then downloads the 166 MB model once.
- The helper writes a four-channel cache file plus four mono WAV files under
  `Echo Music Stems` inside the audio cache folder.
- The enabled mono stems are mixed into a temporary stereo WAV that the normal
  player plays. Queue, crossfade, automix and next-track therefore keep working
  exactly as they do for any local file.
- Toggling a stem reuses a cached mix for that on/off pattern, so it is quick.

## Controls

- The full-screen player shows coloured `DRM`, `BAS`, `OTH` and `VOC` chips;
  tap one to toggle that stem.
- Hold `D`, `B`, `I` or `V` for a quarter second to mute that stem while the
  key is held; releasing it restores the previous mix. A quick tap toggles.
- The **Separate all tracks** button on a playlist separates every track that
  is not already cached. The current song keeps playing while it runs.

## Speed and hardware

- **Fast** uses a 10% overlap; **Quality** uses 25% and takes longer.
- The ONNX provider is chosen automatically (DirectML/CUDA when available,
  otherwise CPU). CPU separation takes roughly half the song length.

## Limitations

- Only songs available as local audio can be separated.
- Interrupted runs leave temporary files; Echo Music removes files older than
  12 hours at startup.
