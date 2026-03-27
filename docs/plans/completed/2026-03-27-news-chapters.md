# News-based chapter markers and live stream metadata

## Overview
- Integrate with the Radio-T news API (`news.radio-t.com/api/v1`) to track active topic changes during recording
- Write ID3v2 chapter frames (CHAP + CTOC) into MP3 files after recording completes, with each chapter corresponding to a topic discussed on the show
- Add a `--news` CLI flag (default: `https://news.radio-t.com/api/v1`) to configure the news API base URL

## Context (from discovery)
- Files/components involved:
  - `app/recorder/client.go` — existing HTTP client pattern to follow for news API
  - `app/recorder/id3.go` — existing ID3v2.3 header writer (text frames only, needs CHAP/CTOC)
  - `app/recorder/recorder.go` — `Record()` writes ID3 header then streams audio; needs chapter injection post-recording
  - `app/main.go` — run loop, wiring, opts struct; needs `--news` flag and chapter tracker goroutine
  - `app/server/server.go` — `/live` handler needs ICY metadata injection
- Related patterns: `HTTPClient` interface with moq, `runConfig` for injectable deps, long-poll is similar to the existing 5s ticker pattern
- Dependencies: no new external deps needed; ID3v2 CHAP frames and ICY metadata are simple binary protocols

## News API reference
- `GET /news/active/id` → `{"id": "<articleId>"}` — current active topic
- `GET /news/active/wait/{ms}` → blocks until topic changes, returns `{"id": "<articleId>"}`
- `GET /news/id/{id}` → full article: `{title, activets, link, slug, ...}`
- `GET /show/start` → `{"started": "<RFC3339>"}` — show start time (zero = not started)

## Development Approach
- **Testing approach**: TDD (tests first)
- Complete each task fully before moving to the next
- Make small, focused changes
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task
- **CRITICAL: all tests must pass before starting next task**
- **CRITICAL: update this plan file when scope changes during implementation**
- Run tests after each change
- Maintain backward compatibility

## Testing Strategy
- **Unit tests**: required for every task
- Mock the news API HTTP client using the existing `HTTPClient` moq pattern
- Use injectable `nowFn` for deterministic time in chapter offset calculations
- Test ID3v2 CHAP/CTOC frame encoding with byte-level assertions
- Test ICY metadata frame encoding
- Test chapter tracker lifecycle (start/stop, topic change collection)

## Progress Tracking
- Mark completed items with `[x]` immediately when done
- Add newly discovered tasks with ➕ prefix
- Document issues/blockers with ⚠️ prefix
- Update plan if implementation deviates from original scope

## Implementation Steps

### Task 1: News API client
- [x] write tests for a `NewsClient` struct: fetch active ID, fetch article by ID, long-poll for changes
- [x] create `app/recorder/news.go` with `NewsClient` struct following the existing `Client` pattern
- [x] implement `FetchActiveID(ctx) (string, error)` — GET `/news/active/id`
- [x] implement `FetchArticle(ctx, id) (Article, error)` — GET `/news/id/{id}`
- [x] implement `WaitActiveChange(ctx, timeout) (string, error)` — GET `/news/active/wait/{ms}`
- [x] define `Article` struct with fields: `Title`, `Link`, `ActiveTS` (from JSON `title`, `link`, `activets`)
- [x] write tests for error cases (non-200, invalid JSON, context cancellation)
- [x] run tests — must pass before next task

### Task 2: Chapter tracker
- [x] write tests for a `ChapterTracker` that collects chapter markers during recording
- [x] create `app/recorder/chapters.go` with `ChapterTracker` struct
- [x] implement `Run(ctx)` — long-polls news API, collects `Chapter{Title, Link, Offset}` entries
- [x] `Chapter` struct: `Title string`, `Link string`, `Offset time.Duration` (from recording start)
- [x] implement `Chapters() []Chapter` — returns collected chapters (thread-safe)
- [x] tracker should use injectable `nowFn` for testable time offsets
- [x] tracker should handle API errors gracefully (log and retry, don't crash)
- [x] write tests for: topic changes collected with correct offsets, API errors retried, context cancellation stops tracker
- [x] run tests — must pass before next task

### Task 3: ID3v2 CHAP and CTOC frames
- [x] write tests for CHAP frame encoding (frame ID, start/end times, embedded TIT2 sub-frame)
- [x] write tests for CTOC frame encoding (table of contents referencing CHAP frame IDs)
- [x] extend `app/recorder/id3.go` with `id3ChapFrame(id, title, link string, start, end time.Duration) []byte`
- [x] add `id3CTOCFrame(chapIDs []string) []byte` — top-level TOC referencing all chapters
- [x] write `WriteID3v2Chapters(w io.WriteSeeker, chapters []Chapter) error` — writes CHAP+CTOC frames, updates the ID3 header size
- [x] write tests for full chapter injection: header rewrite with size update, multiple chapters, empty chapter list (no-op)
- [x] run tests — must pass before next task

### Task 4: Inject chapters after recording
- [x] write tests for the post-recording chapter injection flow
- [x] modify `Record()` or add a wrapper that accepts `[]Chapter` and injects them after stream EOF
- [x] the ID3 header is already written at file start; chapters need to be appended after the header and the header size updated (seek back to byte 6, rewrite syncsafe size)
- [x] alternatively, write chapters to a temp file and concat (simpler but more I/O)
- [x] write tests for: chapters written to file, no chapters = no modification, file integrity (ID3 header + chapters + audio data all valid)
- [x] run tests — must pass before next task

### Task 5: Wire chapter tracking into the recording loop
- [x] write tests for `run()` / `pollAndRecord()` with chapter tracking enabled
- [x] add `--news` flag to opts (default: `https://news.radio-t.com/api/v1`, env: `NEWS_API`)
- [x] add `newsAPI string` to `runConfig`
- [x] start `ChapterTracker` when recording begins, stop when recording ends
- [x] pass collected chapters to the chapter injection step after `Record()` returns
- [x] chapter tracking disabled when `--news` is empty
- [x] write tests for: tracker started/stopped with recording lifecycle, chapters passed to injection, disabled when empty
- [x] run tests — must pass before next task

### Task 6: Verify acceptance criteria
- [x] verify all requirements from Overview are implemented
- [x] verify edge cases: no news API configured, API down during recording, no topic changes during recording
- [x] run full test suite (unit tests)
- [x] run linter — all issues must be fixed
- [x] verify test coverage meets project standard

### Task 7: [Final] Update documentation
- [x] update README.md: document `--news` flag and chapter markers feature
- [x] update `docker-compose.yml`: add `NEWS_API` environment variable (commented out with default)

## Technical Details

### ID3v2 CHAP frame format (ID3v2.3)
```
Frame header: "CHAP" (4 bytes) + size (4 bytes BE) + flags (2 bytes = 0x0000)
Frame data:
  - Element ID: null-terminated ASCII string (e.g., "chp0\x00")
  - Start time: uint32 BE (milliseconds)
  - End time: uint32 BE (milliseconds, 0xFFFFFFFF if unknown/last)
  - Start offset: uint32 BE (0xFFFFFFFF = not used)
  - End offset: uint32 BE (0xFFFFFFFF = not used)
  - Sub-frames: embedded TIT2 frame with chapter title
```

### ID3v2 CTOC frame format
```
Frame header: "CTOC" (4 bytes) + size (4 bytes BE) + flags (2 bytes = 0x0000)
Frame data:
  - Element ID: null-terminated ASCII string (e.g., "toc0\x00")
  - Flags: 1 byte (0x03 = top-level + ordered)
  - Entry count: 1 byte
  - Entries: null-terminated element IDs referencing CHAP frames
```

### Chapter injection strategy
Since the ID3 header is already written at the start of the file, chapters are appended after the existing header frames. The approach:
1. After recording ends, read the current ID3 header size from bytes 6-9
2. Seek to the end of the existing ID3 tag (offset 10 + decoded syncsafe size)
3. Read remaining audio data into a buffer (or temp file for large recordings)
4. Write CHAP + CTOC frames at the current position
5. Rewrite the syncsafe size at bytes 6-9 to include the new frames
6. Write back the audio data

Alternative (simpler): rewrite the entire ID3 tag from scratch with all frames (existing + chapters) since we know the content.

## Post-Completion
*Items requiring manual intervention or external systems — no checkboxes, informational only*

**Manual verification**:
- Test with a real Radio-T broadcast to verify chapter timestamps align with actual topic switches
- Verify chapter markers display correctly in podcast players (Apple Podcasts, Overcast, Pocket Casts)
- Test with news API unavailable — recording should work normally without chapters
