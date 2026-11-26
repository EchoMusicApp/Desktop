# Echo Connect — LAN Continuity, Pairing & Sync Guide

**Echo Connect** connects **Echo Music Desktop** and **Echo Music (Android)** directly over your local Wi-Fi network — with zero cloud servers, zero accounts required for sync infrastructure, and full cross-device continuity.

---

## Requirements

- **Echo Music Desktop** and **Echo Music (Android)** on the **same Wi-Fi network** (or connected over USB with ADB port forwarding `45870`/`45871` on client-isolated networks).
- Local mDNS / Zeroconf discovery (`_echomusic._tcp.`).

---

## 1. Pairing Your Devices (6-Digit Code)

Devices must be **paired** before playback handoff, remote control, or account/playlist sync is enabled. You can initiate pairing from **either** device:

### From Desktop
1. Open **Settings → Echo Connect** (located directly below **Account**) or click an unpaired phone in the playerbar / Now Playing device menu.
2. Click **Pair** next to your Android device.
3. Desktop displays a **6-digit code** (auto-refreshing every **60 seconds**).
4. Enter that code in the popup that appears on your Android phone and tap **Pair**.

### From Android
1. Open **Settings → Echo Connect** on your phone and tap your Desktop under **Available Devices**.
2. Enter the **6-digit code** shown on your phone into the popup that appears on **Echo Music Desktop** and click **Pair**.

Once bound, both devices save the pairing persistently and immediately perform an initial sync. Use **Unpair** in **Settings → Echo Connect** on either side to disconnect permanently.

---

## 2. Automatic Sync (Every 5 Minutes & Force Sync)

Echo Connect syncs automatically every **5 minutes** in the background and supports on-demand **Force Sync** from **Settings → Echo Connect** on either device:

- **Google / YouTube Music Account**: Automatically signs Desktop into your Android Google / YouTube Music session cookie (without overwriting if already signed into the same email).
- **Local Playlists**: Bidirectional merge between Android's Room DB and Desktop's `Library → Synced Local Playlists` (`echo_connect_playlists.json`), deduplicated by `syncId`, title, and `videoId`.
- **Spotify Playlists**: Syncs your connected Spotify playlists from Android to Desktop under **Library → Spotify Playlists**. Clicking any synced Spotify track automatically resolves and plays it via YouTube Music.
- **Podcast Episode Progress**: Merges episode playback positions (`PositionMs`) and completion flags across both devices.

Both **Settings → Echo Connect** views display **Sync Activity / Sync Details** with exact timestamps and a log of what synced.

---

## 3. Playback Handoff & Remote Control

- **Playerbar & Now Playing Device Picker**: Click the **Echo Connect** icon in the bottom playerbar or the audio output pill in **Now Playing** to switch playback between **This Mac** (local speakers/headphones) and your paired phone. Active devices show a checkmark on the right.
- **Bidirectional Remote Control**:
  - When playing on your phone, Desktop's playerbar, scrubber, transport controls, volume slider, shuffle, repeat, and track clicks control your phone live.
  - When playing on Desktop, your phone's Full Player and MiniPlayer mirror Desktop's track, artwork, and live position — seeking, pausing, skipping, or picking a new song on your phone updates Desktop immediately.
- **Move Playback (`Move here` / `Play on <Device>`)**: Transfers the full Up Next queue and exact millisecond position to the target device and switches the other device into remote controller mode.

---

## 4. Architecture & Endpoints (Developer Reference)

- **Discovery**: mDNS `_echomusic._tcp.` (`internal/lansync/mdns.go` on Desktop, `NsdManager` in `LanSyncManager.kt` on Android).
- **Local HTTP/1.1 JSON API** (`internal/lansync/server.go`):
  - `GET /v1/state` — current playback state, queue, position, and episodes.
  - `POST /v1/control` — remote `play`, `pause`, `toggle`, `next`, `previous`, `seek`, `volume`, `shuffle`, `repeat`, `play_index`.
  - `POST /v1/handoff` — transfer queue, index, exact `positionMs`, and episodes.
  - `POST /v1/pair/start`, `POST /v1/pair/verify`, `POST /v1/pair/unpair` — 6-digit pairing handshake.
  - `POST /v1/sync` — bidirectional exchange of Google account session, local playlists, Spotify playlists, and podcast episodes.
- **Client-Isolated Wi-Fi Fallback**: Primary requests use a fast `900ms` connect timeout and fall back to `127.0.0.1` with a fresh context when using ADB forwarding (`adb forward tcp:45870 tcp:45870 && adb reverse tcp:45871 tcp:45871`).
