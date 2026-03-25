# Stream Recorder

[![build](https://github.com/radio-t/stream-recorder/actions/workflows/ci.yml/badge.svg)](https://github.com/radio-t/stream-recorder/actions/workflows/ci.yml)
[![Coverage Status](https://coveralls.io/repos/github/radio-t/stream-recorder/badge.svg?branch=master)](https://coveralls.io/github/radio-t/stream-recorder?branch=master)

Stream-recorder listens and records audio stream when it is available, saving episodes to a directory with names derived from the Radio-t API.

## Usage

```
Application Options:
  -s, --stream= Stream url (default: https://stream.radio-t.com) [$STREAM]
      --site=   Radio-t API (default: https://radio-t.com/site-api/last/1) [$SITE]
  -d, --dir=    Recording directory (default: ./) [$DIR]
  -p, --port=   If provided will start API server on the port otherwise server is disabled [$PORT]
      --dbg     Enable debug logging [$DBG]
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

## API Endpoints

When `--port` is provided, the server exposes:

- `GET /` - web UI listing recorded episodes (PicoCSS)
- `GET /health` - health check (returns 200 if disk usage < 80%, 500 otherwise)
- `GET /episode/<name>` - download entire episode directory as ZIP archive
