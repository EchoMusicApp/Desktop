# Contributing to Echo Music Desktop

Thank you for helping improve **Echo Music Desktop**, the native desktop client for [Echo Music](https://github.com/EchoMusicApp/Echo-Music).

## Development Setup

1. Install **Go 1.27+** (matching `go.mod`).
2. Clone the repository:
   ```bash
   git clone https://github.com/EchoMusicApp/Desktop.git
   cd Desktop
   ```
3. Run the app in development mode or run the test suite:
   ```bash
   go test ./...
   ```
4. If you need bundled `yt-dlp`, `ffmpeg`, and `QuickJS` locally:
   ```bash
   ./scripts/fetch-tools.sh darwin-arm64
   ```

## Code Style & Checks

- Format all Go code with `gofmt`.
- Run `go vet ./...` and `go test ./...` before opening a pull request.
- If `golangci-lint` is installed, run `golangci-lint run` (configured via `.golangci.yml`).
- Keep changes focused and add tests for new behavior where practical.

## Pull Requests

1. Fork the repository and create a branch from `main`.
2. Make your changes and verify tests pass on your platform.
3. Open a pull request describing what changed and why.
