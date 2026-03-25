# Stream Recorder

[![build](https://github.com/radio-t/stream-recorder/actions/workflows/ci.yml/badge.svg)](https://github.com/radio-t/stream-recorder/actions/workflows/ci.yml)
[![Coverage Status](https://coveralls.io/repos/github/radio-t/stream-recorder/badge.svg?branch=master)](https://coveralls.io/github/radio-t/stream-recorder?branch=master)

Stream-recorder listens and records audio stream when it is available, saving episodes to a directory with names derived from the Radio-t API.

## Usage

```
Application Options:
  -s, --stream=           Stream url (default: https://stream.radio-t.com) [$STREAM]
      --site=             Radio-t API (default: https://radio-t.com/site-api/last/1) [$SITE]
  -d, --dir=              Recording directory (default: ./) [$DIR]
  -p, --port=             If provided will start API server on the port otherwise server is disabled [$PORT]
      --dbg               Enable debug logging [$DBG]
      --schedule-enabled  Enable time-based recording schedule [$SCHEDULE_ENABLED]
      --schedule-day=     Day of week for the show (UTC) (default: saturday) [$SCHEDULE_DAY]
      --schedule-hour=    Hour of show start in UTC (0-23) (default: 20) [$SCHEDULE_HOUR]
      --before-hours=     Hours before show to start recording (default: 2) [$BEFORE_HOURS]
      --after-hours=      Hours after show start to keep recording (default: 4) [$AFTER_HOURS]
      --retention-days=   Delete recordings older than N days, 0=disabled (default: 30) [$RETENTION_DAYS]
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

## Architecture

The recorder consists of three cooperating components:

- **Client** fetches episode metadata from the Radio-T API and the raw audio stream
- **Listener** combines the two client calls into a `Stream` (episode number + audio body)
- **Recorder** writes the stream body to disk as `rt{episode}_{datetime}.mp3`

The main loop polls every 5 seconds. When the API reports a live stream, it records until the stream ends or the context is cancelled.

### Recording schedule

When `--schedule-enabled` is set, recording only happens inside a configurable time window around the show. The window is calculated as:

```
start = schedule_day at (schedule_hour - before_hours) UTC
end   = schedule_day at (schedule_hour + after_hours) UTC
```

With the defaults this means Saturday 18:00 to Sunday 00:00 UTC. Outside this window the recorder sleeps and skips polling.

A manual recording can be triggered at any time via the `POST /record` endpoint or the button on the web UI, which overrides the schedule for one session.

### Auto-purge

On startup and every 24 hours the recorder deletes recordings older than `--retention-days` (default 30). Empty episode directories are removed as well. Set `--retention-days 0` to disable purging.

## API Endpoints

When `--port` is provided, the server exposes:

- `GET /` - web UI listing recorded episodes (PicoCSS)
- `GET /health` - health check (returns 200 if disk usage < 80%, 500 otherwise)
- `GET /episode/<name>` - download entire episode directory as ZIP archive
- `POST /record` - trigger an immediate recording session (overrides schedule window)
