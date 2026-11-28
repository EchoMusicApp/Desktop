# Windows build

The Windows executable must be built as a GUI subsystem app so launching it
does not open a console window:

```sh
go build -ldflags "-H=windowsgui" -o EchoMusicDesktop.exe .
```

The upstream `dux-go` lyrics dependency needs
`patches/dux_go_lyrics_windows.patch` applied when building with plain `go`
(the official Bazel build already applies it).

After building, copy `EchoMusicDesktop.exe` next to `EchoMusicDesktop.iss` and
compile the installer with ISCC. Both the portable exe and the setup exe in the
release were built with the `-H=windowsgui` flag.
