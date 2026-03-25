# Time-based recording schedule and auto-purge

## Overview
- Add scheduled recording window: Saturday 20:00 UTC, 2h before (18:00) to 4h after (00:00 Sunday)
- Schedule is hardcoded, enabled/disabled via single `--schedule` flag
- Add manual recording trigger via `POST /record` for ad-hoc recordings outside the window
- Add auto-purge of recordings older than N days (`--retention-days`, default 30, 0=disabled)
- Closes #16 and #17

## Context (from discovery)
- **Recording loop**: `run()` in `app/main.go` — ticks every 5s, calls `Listener.Listen` then `Recorder.Record`
- **Server**: `app/server/server.go` — routegroup-based, handlers for index/health/episode download
- **CLI flags**: `go-flags` in `app/main.go`, struct tags with env vars
- **No time/schedule logic exists** — recorder polls 24/7

## Development Approach
- **Testing approach**: TDD (tests first)
- Complete each task fully before moving to the next
- **CRITICAL: every task MUST include new/updated tests**
- **CRITICAL: all tests must pass before starting next task**
- **CRITICAL: update this plan file when scope changes during implementation**

## Testing Strategy
- **Unit tests**: table-driven tests for schedule window logic, purge logic
- **Time mocking**: inject `func() time.Time` for deterministic schedule tests

## Progress Tracking
- Mark completed items with `[x]` immediately when done
- Add newly discovered tasks with ➕ prefix
- Document issues/blockers with ⚠️ prefix

## Implementation Steps

### Task 1: Add schedule window checker
- [x] create `app/recorder/schedule.go` with `InWindow(now time.Time) bool` function
- [x] hardcode: Saturday 18:00 UTC to Sunday 00:00 UTC (show at 20:00, 2h before, 4h after)
- [x] handle week wraparound correctly
- [x] write table-driven tests: inside window, outside window, edge cases (exactly 18:00, exactly 00:00 Sunday, Friday, Monday)
- [x] run tests — must pass before next task

### Task 2: Integrate schedule into recording loop
- [x] add `--schedule` bool flag to `opts` (default: false, env: SCHEDULE)
- [x] modify `run()` to check `InWindow(time.Now())` when schedule is enabled
- [x] when outside window, log debug "outside recording window" and skip polling
- [x] write tests for `run()` with schedule enabled/disabled (use mock listener + short ticker + injected time)
- [x] run tests — must pass before next task

### Task 3: Add manual recording trigger endpoint
- [x] add `POST /record` handler to server that sets an `atomic.Bool` force-record flag
- [x] pass the flag to the recording loop — when set, override schedule for one recording session
- [x] add simple button to `index.html` that POSTs to `/record`
- [x] write test for `POST /record` endpoint (returns 200, sets flag)
- [x] write test that force-record overrides schedule window
- [x] run tests — must pass before next task

### Task 4: Add auto-purge of old recordings
- [x] create `app/recorder/purge.go` with `Purge(dir string, retentionDays int) error`
- [x] walk episode directories, delete files with ModTime older than retention threshold
- [x] remove empty episode directories after purging
- [x] add `--retention-days` flag (default: 30, 0=disabled)
- [x] write table-driven tests: old files deleted, new files kept, empty dirs removed, 0=no purge
- [x] run tests — must pass before next task

### Task 5: Integrate purge into main loop
- [x] run purge on startup if retention > 0
- [x] run purge periodically (every 24h) in a separate goroutine
- [x] log purge results
- [x] write test for periodic purge (short interval + temp dir)
- [x] run tests — must pass before next task

### Task 6: Verify acceptance criteria
- [x] run full test suite: `go test -race -short ./app/...`
- [x] run linter: `golangci-lint run` — fix any issues
- [x] run `gofmt -w` on all modified files

### Task 7: [Final] Update documentation
- [x] update README.md with new flags and features

## Technical Details

### Schedule window (hardcoded)
- Show: Saturday 20:00 UTC
- Window: Saturday 18:00 UTC — Sunday 00:00 UTC
- `InWindow` checks: `now.Weekday() == Saturday && now.Hour() >= 18` OR `now.Weekday() == Sunday && now.Hour() == 0 && now.Minute() == 0`

### CLI flags (new)
```
--schedule        Enable time-based recording (default: false)
--retention-days  Delete recordings older than N days, 0=disabled (default: 30)
```

### Manual trigger flow
```
POST /record → sets atomic.Bool → recording loop checks flag before schedule
→ starts recording, clears flag after session ends
```

### Purge logic
```
For each episode dir:
  For each file:
    if file.ModTime < now - retention_days: delete
  if dir empty: delete dir
```

## Post-Completion

**Manual verification:**
- Run with `--schedule` and verify polling only during Saturday 18:00-00:00 UTC
- Test manual trigger: `curl -X POST localhost:8080/record`
- Create old test files and verify purge on startup
