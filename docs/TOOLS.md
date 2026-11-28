# Media tools

Echo Music needs `ffmpeg` for decoding/analysis, `yt-dlp` for resolving audio
and a JavaScript runtime for yt-dlp's challenge solver. Minimal builds do not
bundle them.

On startup the app checks the PATH. Anything missing is downloaded once into
`Echo Music Tools` beside the audio cache, that folder is prepended to PATH for
the process, and playback proceeds automatically. On macOS and Linux a system
`ffmpeg` is still required; yt-dlp and the JS runtime are downloaded
automatically.

Download failures are reported as a notice; retrying a track after the network
is back repeats the setup.
