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
```

## Example

```bash
make build
./streamrecorder --stream 'https://stream.radio-t.com' --port 8080 --dir ./records
```

## Docker

```bash
docker-compose up --detach
```

## API Endpoints

When `--port` is provided, the server exposes:

- `/` - web UI listing recorded episodes
- `/health` - health check endpoint (warns if disk capacity exceeds 80%)
- `/episode/<name>` - download entire episode as ZIP archive
- `/record/<path>` - download individual MP3 file
