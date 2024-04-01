# Stream Recorder

stream-recorder is used to listen and record audio stream when it is available(every 5 seconds),
save as episodes in provided directory and naming them after calling site api.

## Usage
```
stream-recorder is used to listen and record audio stream when it is available,
save as episodes in provided directory and naming them after calling site api

Options:
  -dir string
        Recording directory (default "./")
  -port string
        If provided app will start REST API server on the port otherwise server is disabled
  -site string
        Radio-t API (default "https://radio-t.com/site-api/last/1")
  -stream string
        Stream url (default "https://stream.radio-t.com")
```

## Example:

```
make build
./streamrecorder --stream 'https://stream.radio-t.com' --port 8080 --dir ./records
```

## Using Docker
Run using docker compose
```bash
docker-compose up --detach
```

## Options

### `port`
HTTP Server option allows the stream-recorder to run a server for trying out the REST API on a specified port. The server will serve the following endpoints:
- `/download/<record>`: to download an episode
- `/health`: to check the application's health, warning if disk capacity exceeds 80%
- `/records`: to view recorded episodes
- `/`: provides a Swagger UI for easier interaction with the API.

### `site`
By default with fetch [Radio-t API]("https://radio-t.com/site-api/last/1") recieving data on the latest episode
