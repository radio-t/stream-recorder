# Architecture refactor

## Overview
- Restructure the codebase to give each package a clear, single responsibility
- Extract the news/chapter subsystem from `recorder/` into `app/chapters/`
- Fix naming, interface placement, and implicit coupling across the codebase
- All changes are purely structural — no behaviour changes

## Context (from discovery)
- `app/recorder/` has 8 files with unrelated concerns: stream client, listener, disk recorder, ID3 encoding, schedule, purge, news API client, chapter tracker
- `id3.go` mixes byte-level frame builders with filesystem orchestration (`InjectChapters`)
- `schedule.go` has zero dependencies on `recorder` and is only used from `main.go`
- Filename format is reconstructed independently in `recorder.go` and `purge.go`
- `runConfig` in `main.go` mixes immutable config with live mutable state
- Several naming issues: `ClientService`, `SiteAPIUrl`, `Entry`, `NewStream`
- `path.Join` used where `filepath.Join` is correct for OS paths

## Development Approach
- **Testing approach**: Regular (refactoring — move code, fix tests to match)
- Complete each task fully before moving to the next
- Make small, focused changes
- **CRITICAL: every task MUST include updated tests** for moved/changed code
- **CRITICAL: all tests must pass before starting next task**
- **CRITICAL: update this plan file when scope changes during implementation**
- Run tests and linter after each change
- Maintain backward compatibility — no behaviour changes

## Testing Strategy
- **Unit tests**: update imports and references in all affected test files
- After each move, verify `go test -race -short ./...` passes
- After each move, verify `golangci-lint run` passes
- Integration test (`TestFullPipeline`) must still pass (when Docker available)

## Progress Tracking
- Mark completed items with `[x]` immediately when done
- Add newly discovered tasks with ➕ prefix
- Document issues/blockers with ⚠️ prefix

## Implementation Steps

### Task 1: Extract chapters package

**Files:**
- Create: `app/chapters/news.go` (move from `app/recorder/news.go`)
- Create: `app/chapters/chapters.go` (move from `app/recorder/chapters.go`)
- Create: `app/chapters/inject.go` (extract `InjectChapters` + helpers from `app/recorder/id3.go`)
- Create: `app/chapters/chapters_test.go` (move from `app/recorder/chapters_test.go`)
- Create: `app/chapters/news_test.go` (move from `app/recorder/news_test.go`)
- Create: `app/chapters/inject_test.go` (extract relevant tests from `app/recorder/id3_test.go` + `app/recorder/recorder_test.go`)
- Create: `app/chapters/newsprovider_moq_test.go` (move from `app/recorder/`)
- Modify: `app/recorder/id3.go` (remove everything except `WriteID3v2Header` + its helpers `putSyncsafe`, `id3TextFrame`)
- Modify: `app/main.go` (update imports from `recorder` to `chapters`)
- Modify: `app/run_test.go` (update imports)
- Modify: `app/main_test.go` (update imports if referencing chapter types)

- [x] create `app/chapters/` package
- [x] move `news.go` + `news_test.go` to `app/chapters/`, change package declaration
- [x] move `chapters.go` + `chapters_test.go` to `app/chapters/`, update `//go:generate` directive, then run `go generate ./app/chapters/...` to regenerate the moq mock in the correct package
- [x] move `Chapter` type to `app/chapters/` (it currently lives in `chapters.go`)
- [x] extract `InjectChapters`, `writeChapteredFile`, `buildChapterFrames` from `id3.go` into `app/chapters/inject.go`
- [x] move ID3 chapter frame builders (`id3ChapFrame`, `id3CTOCFrame`, `id3WXXXFrame`, `buildChapterFrames`) and helpers (`readSyncsafe`, `putSyncsafe`, `id3TextFrame`) into `app/chapters/inject.go` alongside `InjectChapters` — keep only `WriteID3v2Header` in `recorder/id3.go` with its own copy of `putSyncsafe` and `id3TextFrame` (small, avoids cross-package dependency)
- [x] move chapter-related tests from `id3_test.go` (`TestID3ChapFrame`, `TestID3ChapFrameNoLink`, `TestID3CTOCFrame`, `TestReadSyncsafe`, `TestReadPutSyncsafeRoundtrip`) to `app/chapters/inject_test.go`
- [x] rewrite `InjectChapters` integration tests from `recorder_test.go` (`TestRecordAndInjectChapters`, `TestInjectChaptersEmpty`, `TestInjectChaptersNonexistentFile`, `TestInjectChaptersNonID3File`) in `app/chapters/inject_test.go` — create test MP3 files directly (write ID3 header + audio bytes) instead of depending on `recorder.Record`
- [x] update `main.go` and `run_test.go` imports: `recorder.Chapter` → `chapters.Chapter`, `recorder.InjectChapters` → `chapters.InjectChapters`, `recorder.NewChapterTracker` → `chapters.NewChapterTracker`, `recorder.NewNewsClient` → `chapters.NewNewsClient`
- [x] delete original files from `app/recorder/`
- [x] run `go test -race -short ./...` and `golangci-lint run` — must pass

### Task 2: Move schedule to main

**Files:**
- Modify: `app/main.go` (add `inScheduleWindow` function)
- Modify: `app/run_test.go` (move schedule tests here)
- Delete: `app/recorder/schedule.go`
- Delete: `app/recorder/schedule_test.go`

- [ ] move `InScheduleWindow` from `app/recorder/schedule.go` to `app/main.go` as unexported `inScheduleWindow`
- [ ] move schedule test cases from `app/recorder/schedule_test.go` to `app/run_test.go`
- [ ] update the call in `pollAndRecord` from `recorder.InScheduleWindow` to `inScheduleWindow`
- [ ] delete `app/recorder/schedule.go` and `app/recorder/schedule_test.go`
- [ ] run tests and linter — must pass

### Task 3: Make filename format explicit

**Files:**
- Modify: `app/recorder/recorder.go` (extract filename builder)
- Modify: `app/recorder/purge.go` (use shared function)

- [ ] add exported `RecordingFileName(episode string, t time.Time) string` function in `recorder.go`
- [ ] add exported `RecordingFilePrefix(episode string) string` function in `recorder.go`
- [ ] refactor `prepareFile` to use `RecordingFileName`
- [ ] refactor `isRecorderFile` in `purge.go` to use `RecordingFilePrefix`
- [ ] write unit tests for `RecordingFileName` and `RecordingFilePrefix`
- [ ] run tests and linter — must pass

### Task 4: Split runConfig into config and state

**Files:**
- Modify: `app/main.go` (split struct, update signatures)
- Modify: `app/run_test.go` (update test setup)

- [ ] create `recordingState` struct with `forceRecord *atomic.Bool` and `recording *atomic.Bool`
- [ ] keep `runConfig` with only immutable fields: `schedule`, `tickInterval`, `nowFn`, `newChapterTracker`, `injectChapters`
- [ ] update `run`, `pollAndRecord`, `recordStream` signatures to take both `runConfig` and `*recordingState`
- [ ] update `newRunConfig` to return only config (state is created separately in `main`)
- [ ] update all test call sites in `run_test.go`
- [ ] run tests and linter — must pass

### Task 5: Make Recorder.OnReady a constructor option

**Files:**
- Modify: `app/recorder/recorder.go` (change constructor)
- Modify: `app/main.go` (update call site)
- Modify: `app/recorder/recorder_test.go` (update test setup)
- Modify: `app/main_test.go` (update if needed)

- [ ] change `NewRecorder(dir string)` to `NewRecorder(dir string, onReady func())` — pass `nil` when not needed
- [ ] make `OnReady` unexported field `onReady`
- [ ] update `main.go` call site
- [ ] update tests
- [ ] run tests and linter — must pass

### Task 6: Fix naming

**Files:**
- Modify: `app/recorder/client.go` (rename types and fields)
- Modify: `app/recorder/listener.go` (rename interface)
- Modify: `app/recorder/client_test.go`
- Modify: `app/recorder/listener_test.go`
- Modify: `app/recorder/httpclient_moq_test.go` (regenerate if needed)
- Modify: `app/recorder/clientservice_moq_test.go` (regenerate)

- [ ] rename `ClientService` interface to `StreamSource` in `listener.go`
- [ ] unexport and rename `SiteAPIUrl` to `siteAPIURL` in `client.go`
- [ ] unexport and rename `Entry` to `siteEntry` in `client.go` (only used internally)
- [ ] regenerate moq mocks: `go generate ./app/recorder/...`
- [ ] update all references in tests
- [ ] run tests and linter — must pass

### Task 7: Replace bool return with named type

**Files:**
- Modify: `app/main.go` (add type, update functions)

- [ ] define `type loopAction bool` with constants `continueLoop loopAction = false` and `stopLoop loopAction = true`
- [ ] change return type of `pollAndRecord` and `recordStream` from `bool` to `loopAction`
- [ ] update the `run` loop to use `stopLoop` comparison
- [ ] run tests and linter — must pass

### Task 8: Fix path.Join and tailFile params

**Files:**
- Modify: `app/recorder/recorder.go` (`path.Join` → `filepath.Join`, `path.Dir` → `filepath.Dir`)
- Modify: `app/server/server.go` (`path.Join` → `filepath.Join`)
- Modify: `app/main_test.go` (`path.Join` → `filepath.Join`)

- [ ] replace `path.Join` and `path.Dir` with `filepath.Join` and `filepath.Dir` in `recorder.go`
- [ ] replace `path.Join` with `filepath.Join` in `server.go` (all file path uses)
- [ ] replace `path.Join` with `filepath.Join` in `main_test.go` (3 instances)
- [ ] remove `"path"` import where no longer needed, keep `"path/filepath"`
- [ ] run tests and linter — must pass

### Task 9: Verify acceptance criteria

- [ ] verify all packages have clear single responsibilities
- [ ] verify no circular imports
- [ ] run full test suite: `go test -race -short ./...`
- [ ] run linter: `golangci-lint run`
- [ ] verify test coverage hasn't decreased: `go test -race -coverprofile=/tmp/coverage.out -short ./... && go tool cover -func=/tmp/coverage.out | tail -1`

### Task 10: [Final] Update documentation

- [ ] update README.md if package structure is referenced
- [ ] move this plan to `docs/plans/completed/`

## Technical Details

### New package structure after refactor
```
app/
  main.go              — CLI, wiring, run loop, schedule check
  run_test.go          — tests for run loop + schedule
  main_test.go         — integration tests
  recorder/
    client.go          — HTTP client for stream + site API
    listener.go        — combines client calls into Stream
    recorder.go        — writes stream to disk, filename format
    id3.go             — pure ID3v2 frame encoding (no file I/O)
    purge.go           — old file deletion
    *_test.go
  chapters/
    news.go            — news API client (FetchActiveID, FetchArticle, WaitActiveChange)
    chapters.go        — ChapterTracker (long-poll topic changes, collect Chapter entries)
    inject.go          — InjectChapters (file rewrite with CHAP/CTOC frames)
    *_test.go
  server/
    server.go          — HTTP server, handlers, live streaming
    static/index.html
    *_test.go
```

### Type movements
- `recorder.Chapter` → `chapters.Chapter`
- `recorder.Article` → `chapters.Article`
- `recorder.NewsClient` → `chapters.NewsClient`
- `recorder.ChapterTracker` → `chapters.ChapterTracker`
- `recorder.InjectChapters` → `chapters.InjectChapters`
- `recorder.NewsProvider` → `chapters.NewsProvider`
- `recorder.InScheduleWindow` → `main.inScheduleWindow` (unexported)

### ID3 code split
- `recorder/id3.go` keeps only: `WriteID3v2Header`, `putSyncsafe`, `id3TextFrame` (the header writer and its helpers)
- `chapters/inject.go` gets: `InjectChapters`, `writeChapteredFile`, `buildChapterFrames`, `id3ChapFrame`, `id3CTOCFrame`, `id3WXXXFrame`, `readSyncsafe`, `putSyncsafe`, `id3TextFrame` (duplicated small helpers — avoids cross-package dependency)

## Post-Completion

**Manual verification**:
- Run integration test with Docker if available: `go test -race ./app/...`
- Verify `go build ./app` produces a working binary
- Verify `docker compose build` still works
