# Stream recorder: test bench and first working version

## Overview
- Get stream-recorder to a working, tested state addressing umputun's feedback from issue #2
- Simplify first (less code to test), then fix bugs, then add comprehensive tests
- Features (time-based recording, retention) are out of scope — separate issues

## Context (from discovery)
- **Issue #2**: comprehensive code review from umputun with architecture, bug, and quality feedback
- **Existing integration test**: `app/main_test.go` — Icecast testcontainer, tests recorder pipeline only
- **Test data**: `app/testdata/icecast.Dockerfile` + `icecast.xml` + `untsa.mp3`
- **Server has zero tests**: `app/server/` — 4 files, no tests
- **Key bugs**: `Record` ignores context, redundant `os.Stat`, 128-byte buffer
- **Architecture excess**: server split across 4 files, `index` struct over-engineered, `DownloadRecordHandler` unnecessary, `Listener` abstraction unclear
- **Code quality**: debug tied to revision instead of `--dbg`, non-idiomatic var names

## Development Approach
- **Testing approach**: TDD (tests first)
- Simplify architecture first — cut unnecessary code before writing tests for it
- Complete each task fully before moving to the next
- **CRITICAL: every task MUST include new/updated tests** for code changes
- **CRITICAL: all tests must pass before starting next task**
- **CRITICAL: update this plan file when scope changes during implementation**

## Testing Strategy
- **Integration tests**: Icecast testcontainer for real stream recording + server verification
- **Server tests**: httptest-based tests for all handlers
- **Unit tests**: table-driven tests for recorder package
- **File correctness**: verify recorded file size approximately matches source

## Progress Tracking
- Mark completed items with `[x]` immediately when done
- Add newly discovered tasks with ➕ prefix
- Document issues/blockers with ⚠️ prefix

## Implementation Steps

### Task 1: Consolidate server into single file and simplify
- [x] merge `health.go`, `index.go`, `download.go` into `server.go`
- [x] remove `index` struct — replace with a helper function `listEpisodes(dir)` returning simple data for template
- [x] remove `DownloadRecordHandler` and its route (umputun: not useful)
- [x] parse template once in `NewServer`, store on `Server` struct
- [x] delete the now-empty files (`health.go`, `index.go`, `download.go`)
- [x] run `go build ./app/...` — must compile

### Task 2: Simplify Listener/Stream/Recorder and fix main.go
- [x] add doc comments on `Client`, `Listener`, `Recorder` explaining flow: Client fetches metadata + stream body → Listener combines into Stream → Recorder writes to disk
- [x] rename/inline in main.go: remove `myclient`/`streamlistener`, use short names or inline construction
- [x] add `Dbg bool` flag to opts (`--dbg`), replace `revision == "local"` debug logic
- [x] remove redundant `os.IsNotExist` check before `MkdirAll` in `recorder.go`
- [x] increase read buffer from 128 bytes to 32KB
- [x] run `go build ./app/...` — must compile

### Task 3: Fix context propagation in Recorder.Record
- [ ] write test in `recorder_test.go`: cancel context during recording, verify it stops promptly
- [ ] update `Recorder.Record` to use context — check `ctx.Done()` in the read loop
- [ ] update existing recorder tests to pass context properly
- [ ] run tests — must pass before next task

### Task 4: Add server tests
- [ ] create `app/server/server_test.go` with test helper (temp dir with fake episode/recording files)
- [ ] write test for `IndexHandler` — returns HTML listing episodes and files
- [ ] write test for `HealthHandler` — returns 200 when disk OK
- [ ] write test for `DownloadEpisodeHandler` — returns valid zip with correct files
- [ ] run tests — must pass before next task

### Task 5: Extend integration test — full pipeline + server verification
- [ ] update `app/main_test.go` — verify recorded file size is within reasonable range of source (not just "has content")
- [ ] add test: record stream → start server → HTTP GET `/` → verify HTML contains episode
- [ ] add test: HTTP GET `/episode/999` → verify zip download contains the recorded MP3
- [ ] add test: cancel context during recording → verify it stops cleanly
- [ ] run tests — must pass before next task

### Task 6: Verify acceptance criteria
- [ ] verify all issue #2 feedback items are addressed
- [ ] run full test suite: `go test -race ./app/...`
- [ ] run linter: `golangci-lint run` — fix any issues
- [ ] verify test coverage: `go test -race -coverprofile=coverage.out ./app/... && go tool cover -func=coverage.out`
- [ ] run `gofmt -w` on all modified files

### Task 7: [Final] Update documentation
- [ ] update README.md with current state
- [ ] comment on issue #2 with what's been addressed

## Technical Details

### Server consolidation target
All in `server.go`: `Server` struct, `NewServer`, `Start`, `Stop`, `IndexHandler`, `HealthHandler`, `DownloadEpisodeHandler`, `listEpisodes` helper, `getCapacity`.

### Context propagation in Record
```go
func (r *Recorder) Record(ctx context.Context, s *Stream) error {
    // ...setup file...
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        n, err := s.Body.Read(buf)
        // ...write...
    }
}
```

### Integration test architecture
```
TestFullPipeline:
  1. Start Icecast testcontainer (streams untsa.mp3)
  2. Record via Client → Listener → Recorder to temp dir
  3. Verify file size ≈ source size
  4. Start Server pointing at temp dir
  5. GET / → HTML contains episode listing
  6. GET /episode/999 → zip contains MP3
  7. Cancel ctx → recording stops
```

## Post-Completion

**Manual verification:**
- Run with real Radio-T stream URL end-to-end
- Test docker-compose against live stream
- Verify graceful shutdown with Ctrl+C during active recording

**Future work (separate issues):**
- Time-based recording (2h before / 4h after Saturday 23:00 MSK)
- Retention policy (auto-delete recordings older than 30 days)
- Consider `routegroup` for server routing
