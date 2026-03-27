# Stream Recorder

[![build](https://github.com/radio-t/stream-recorder/actions/workflows/ci.yml/badge.svg)](https://github.com/radio-t/stream-recorder/actions/workflows/ci.yml)
[![Coverage Status](https://coveralls.io/repos/github/radio-t/stream-recorder/badge.svg?branch=master)](https://coveralls.io/github/radio-t/stream-recorder?branch=master)

Stream-recorder listens and records audio stream when it is available, saving episodes to a directory with names derived from the Radio-t API.

## Usage

```
Application Options:
  -s, --stream=          Stream url (default: https://stream.radio-t.com) [$STREAM]
      --site=            Radio-t API (default: https://radio-t.com/site-api/last/1) [$SITE]
  -d, --dir=             Recording directory (default: ./) [$DIR]
  -p, --port=            If provided will start API server on the port otherwise server is disabled [$PORT]
      --dbg              Enable debug logging [$DBG]
      --schedule         Enable time-based recording (Sat 20:00 UTC, 2h before / 4h after) [$SCHEDULE]
      --retention-days=  Delete recordings older than N days, 0=disabled (default: 30) [$RETENTION_DAYS]
      --news=            News API base URL for chapter markers, empty to disable
                         (default: https://news.radio-t.com/api/v1) [$NEWS_API]
      --auth-passwd=     bcrypt hash of password for POST /record auth [$AUTH_PASSWD]
```

## Example

```bash
make build
./streamrecorder --stream 'https://stream.radio-t.com' --port 8080 --dir ./records
```

## Docker

```bash
docker compose up --detach
```

## Testing with a live stream

To test locally without waiting for the Radio-T broadcast, use any Icecast/Shoutcast stream together with a mock site API:

```bash
# start a mock site API that returns a fake episode title
python3 -c '
import http.server, json
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps([{"title": "Podcast 999"}]).encode())
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", 9999), H).serve_forever()
' &

# run the recorder against a public stream
./streamrecorder \
  --stream http://ice1.somafm.com/groovesalad-256-mp3 \
  --site http://127.0.0.1:9999/ \
  --dir ./records --port 8080 --dbg
```

The mock API returns `[{"title": "Podcast 999"}]`, so recordings appear under `records/999/`. Open http://localhost:8080 to see the web UI.

Some public Icecast streams suitable for testing:

- `http://ice1.somafm.com/groovesalad-256-mp3` — SomaFM Groove Salad (ambient)
- `http://stream.radioparadise.com/mp3-192` — Radio Paradise

## Architecture

The recorder consists of three cooperating components:

- **Client** fetches episode metadata from the Radio-T API and the raw audio stream
- **Listener** combines the two client calls into a `Stream` (episode number + audio body)
- **Recorder** writes the stream body to disk as `rt{episode}_{datetime}.mp3` with ID3v2 metadata (title, artist, recording date)

The main loop polls every 5 seconds. When the API reports a live stream, it records until the stream ends or the context is cancelled.

### Recording schedule

When `--schedule` is set, recording only happens inside a fixed time window around the [Radio-T show](https://radio-t.com/online/): Saturday 18:00 to Sunday 00:00 UTC (2 hours before and 4 hours after the 20:00 UTC start). Outside this window the recorder sleeps and skips polling.

A manual recording can be triggered at any time via the `POST /record` endpoint or the button on the web UI, which overrides the schedule for one session.

### Chapter markers

When `--news` is set (default: `https://news.radio-t.com/api/v1`), the recorder tracks active topic changes via the Radio-T news API during recording. After the stream ends, ID3v2 chapter frames (CHAP + CTOC) are injected into the MP3 file so podcast players can display per-topic navigation. Each chapter includes the topic title and a link to the corresponding article.

Set `--news ""` or `NEWS_API=""` to disable chapter tracking entirely.

### Auto-purge

On startup and every 24 hours the recorder deletes recordings older than `--retention-days` (default 30). Empty episode directories are removed as well. Set `--retention-days 0` to disable purging.

## API Endpoints

When `--port` is provided, the server exposes:

- `GET /` - web UI listing recorded episodes with playback and download links (PicoCSS)
- `GET /health` - health check (returns 200 if disk usage < 80%, 500 otherwise)
- `GET /episode/<name>` - download entire episode directory as ZIP archive
- `GET /episode/<name>/<file>` - play or download a single recording (`?download` forces download)
- `GET /live/<filename>` - stream the active recording from the current write position
- `POST /record` - trigger an immediate recording session (overrides schedule window)

### Authentication

When `--auth-passwd` is set to a bcrypt hash, `POST /record` requires a password. All GET endpoints remain open.

The password can be provided in two ways:
- **Form body** — the web UI submits a `password` field with the POST request
- **Basic auth header** — for curl/API usage (username is ignored, only the password is compared)

When auth is enabled, the web UI shows a password input field next to the "Start Recording" button. A rate limiter (1 req/sec) is applied to `POST /record` to prevent brute force.

Generate a bcrypt hash: `htpasswd -nbBC 10 "" 'yourpassword' | cut -d: -f2`
