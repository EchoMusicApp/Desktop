# Video canvas (beta)

An opt-in backdrop behind the full-screen player. It mirrors the Android app's
canvas: a muted five-second clip is taken from the centre of the song's video
and looped behind the artwork and lyrics. Audio playback is never changed.

Enable it under **Settings → Playback & Sound → Stems (beta) → Video canvas**.

## How it works

- The official video stream is resolved anonymously through the same YouTube
  client used for artwork and playback metadata.
- The bundled `ffmpeg` extracts a 480 px, 10 fps, five-second animated GIF into
  `Echo Music Canvas Cache` beside the audio cache. Clips older than seven days
  are ignored and stale extracts are cleaned at startup.
- mygo decodes the GIF as a looping video, so no new native media backend is
  added to the app. The backdrop only renders while the song is playing.

## Limits

- Videos that YouTube marks unplayable, or tracks whose "video" is a still
  cover, have no canvas; the normal artwork stays on screen.
- A missing clip only affects the backdrop. Playback, stems, automix and the
  queue continue normally.
- The first song with the option enabled needs a short download and extract.
