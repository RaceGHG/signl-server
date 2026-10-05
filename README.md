# SIGNL Download Server

A small, dependency-free server that downloads movies and series to **your own storage**
(NAS, PC, Mac, Raspberry Pi) instead of your phone or Apple TV. It is the companion of the
[SIGNL](https://signlplus.delta-code.de/) app for Apple devices.

> **The server provides no content.** It only downloads what you send to it from *your own*
> sources (your IPTV provider, your Jellyfin/Plex server). There are no channel lists, no streams
> and no search for third-party content.

*Deutsch: siehe [unten](#deutsch).*

## Quick start (Docker)

```sh
docker run -d --name signl-server --restart unless-stopped \
  -p 8484:8484 -v ./signl-data:/data \
  ghcr.io/raceghg/signl-server:latest
docker logs signl-server        # shows the address and the access key
```

Or with Docker Compose: see [`docker-compose.image.yml`](docker-compose.image.yml).

In SIGNL: **Profile → Settings → Download server → Add server**, enter the address and the key.
Then choose "On <your server>" when you download a title, or set it as the default.

## Without Docker

Download a binary for macOS, Windows or Linux from the
[Releases](../../releases) page (or run `./build.sh`), start it, done. See [LIES-MICH.md](LIES-MICH.md).

## Configuration

| Variable | Option | Default | |
|---|---|---|---|
| `SIGNL_PORT` | `-port` | 8484 | port |
| `SIGNL_DATA` | `-data` | `~/SIGNL-Server` (`/data` in Docker) | media, state and key live here |
| `SIGNL_NAME` | `-name` | host name | name shown in SIGNL |
| `SIGNL_PARALLEL` | `-parallel` | 2 | simultaneous downloads |
| `SIGNL_TOKEN` | – | generated | your own access key (≥ 16 characters) |

HLS streams (`.m3u8`) need `ffmpeg` (included in the Docker image).

## Security

* Every request needs the access key. Without it the server answers `401`.
* There is **no HTTPS**. Do not expose the port to the internet — use a VPN or a reverse proxy with TLS.
* Provider URLs (which usually contain credentials) are never returned by the API.

## API (v1)

`GET /api/v1/info` · `GET|POST /api/v1/downloads` · `POST /api/v1/downloads/{id}/cancel|retry` ·
`DELETE /api/v1/downloads/{id}` · `GET /api/v1/files/{id}` (range requests) —
`Authorization: Bearer <key>` or `?token=<key>`.

## Build & test

```sh
go test ./...
./build.sh 1.0.0      # all platforms into dist/
```

MIT licensed. © 2026 DELTACODE Inh. Fabian Hellmann

---

## Deutsch

Ein kleiner Server ohne Abhängigkeiten, der Filme und Serien auf **deinen eigenen Speicher**
(NAS, PC, Mac, Raspberry Pi) lädt — statt aufs iPhone oder den Apple TV. Gegenstück zur
SIGNL-App für Apple-Geräte.

**Der Server liefert keine Inhalte.** Er lädt nur, was du ihm aus *deinen eigenen* Quellen
schickst. Anleitung für Mac, Windows, Linux und NAS: [LIES-MICH.md](LIES-MICH.md).
