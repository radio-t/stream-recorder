# Auth for POST /record endpoint

## Overview
Add optional auth protection to the `POST /record` endpoint in stream-recorder. When `--auth-passwd` is set (bcrypt hash), the force-record action requires a password. All GET endpoints remain open — browsing, downloading, health checks, and live streaming are read-only and harmless.

When auth is enabled, the UI shows a password input field next to the "Start Recording" button. The handler accepts password from both form body (web UI) and basic auth header (curl/API).

## Context
- `app/server/server.go` — Server struct, NewServer constructor, ForceRecordHandler, IndexHandler
- `app/server/server_test.go` — table-driven tests, `setupTestDir`, `newTestServer`, `serveHTTP` helpers
- `app/server/static/index.html` — embedded template with conditional "Start Recording" button
- `app/main.go` — CLI flags with env vars, `startServer` function
- `app/main_test.go` — has `server.NewServer` call at line 142 that needs signature update
- Auth pattern: bcrypt hash comparison via `golang.org/x/crypto/bcrypt` (already in go.mod as indirect dep)

## Solution Overview
- Add `--auth-passwd` flag (env `AUTH_PASSWD`) accepting a bcrypt hash string
- Pass it through to `NewServer` as an `authPasswd` field
- `ForceRecordHandler` checks auth when `authPasswd` is set — accepts password from form body or basic auth header
- Template shows password input + record button when auth is enabled; plain button when auth is disabled
- No session cookies — password is submitted with each POST
- Rate limiting on `POST /record` via `tollbooth` to prevent brute force

## Technical Details
- `authPasswd` field on Server struct — empty string means auth disabled
- `checkAuth(r *http.Request) bool` method — checks two sources:
  1. form value `password` from POST body (web UI submits this)
  2. basic auth header (curl/API usage, username ignored, only password compared)
  - compares password against bcrypt hash via `bcrypt.CompareHashAndPassword`
- `ForceRecordHandler` guard: if `authPasswd != ""` and `checkAuth` fails, return 403
- rate limiting: `tollbooth.HTTPMiddleware(tollbooth.NewLimiter(1, nil))` applied to `POST /record` route group — 1 req/sec prevents brute force
- Template: when `AuthEnabled` is true, shows `<input type="password" name="password">` alongside the submit button
- Docker compose gets `AUTH_PASSWD` env var with a pre-generated bcrypt hash
- `golang.org/x/crypto` is already in go.mod (indirect); importing bcrypt promotes it to direct
- add `github.com/didip/tollbooth/v8` dependency

## Development Approach
- **testing approach**: regular (code first, then tests)
- complete each task fully before moving to the next
- every task includes new/updated tests
- all tests must pass before starting next task

## Implementation Steps

### Task 1: Add authPasswd to Server struct and constructor

**Files:**
- Modify: `app/server/server.go`
- Modify: `app/server/server_test.go`
- Modify: `app/main_test.go`

- [x] add `authPasswd string` field to `Server` struct
- [x] add `authPasswd string` parameter to `NewServer` (after `dir`, before `forceRecord`)
- [x] update all existing `NewServer` calls in `server_test.go` to pass `""` for authPasswd
- [x] update `newTestServer` helper to pass `""` for authPasswd
- [x] update `server.NewServer` call in `app/main_test.go` to pass `""` for authPasswd
- [x] run `go test ./app/server/ ./app/` — must pass before next task

### Task 2: Implement auth check, rate limiter, and protect ForceRecordHandler

**Files:**
- Modify: `app/server/server.go`
- Modify: `app/server/server_test.go`

- [x] add `github.com/didip/tollbooth/v8` dependency
- [x] add `checkAuth(r *http.Request) bool` method — check form value "password" and basic auth header, compare against bcrypt hash
- [x] add auth guard to `ForceRecordHandler`: if `authPasswd != ""` and `checkAuth` fails, return 403
- [x] apply tollbooth rate limiter (1 req/sec) to `POST /record` route when auth is enabled
- [x] write tests for `checkAuth` — correct password via form body, correct password via basic auth, wrong password, missing credentials
- [x] write tests for `ForceRecordHandler` with auth enabled — 403 without creds, 403 with wrong password, redirect with correct password (form body), redirect with correct password (basic auth)
- [x] write test for `ForceRecordHandler` with auth disabled (empty authPasswd) — works without creds as before
- [x] run `go test ./app/server/` — must pass before next task

### Task 3: Add password form to UI when auth is enabled

**Files:**
- Modify: `app/server/server.go` (IndexHandler template data)
- Modify: `app/server/static/index.html`
- Modify: `app/server/server_test.go`

- [x] add `AuthEnabled bool` field to IndexHandler template data struct, set from `s.authPasswd != ""`
- [x] update template: when auth disabled, show plain "Start Recording" button (unchanged)
- [x] update template: when auth enabled, show password input field + "Start Recording" button in the same form, POST includes password field
- [x] write test: with auth enabled, index page contains `type="password"` input
- [x] write test: with auth disabled, index page shows plain "Start Recording" without password field
- [x] run `go test ./app/server/` — must pass before next task

### Task 4: Wire CLI flag and update main.go

**Files:**
- Modify: `app/main.go`

- [x] add `AuthPasswd string` field to opts struct with `long:"auth-passwd" env:"AUTH_PASSWD" description:"bcrypt hash of password for POST /record auth"`
- [x] pass `opts.AuthPasswd` to `startServer` and through to `server.NewServer`
- [x] update `startServer` signature to accept authPasswd parameter
- [x] run `go test ./app/...` — must pass before next task

### Task 5: Verify acceptance criteria
- [x] verify: POST /record returns 403 when auth enabled and no credentials provided
- [x] verify: POST /record succeeds with correct password in form body
- [x] verify: POST /record succeeds with correct password via basic auth header
- [x] verify: all GET endpoints work without auth regardless of auth setting
- [x] verify: password field shown in UI when auth enabled
- [x] verify: no password field in UI when auth disabled
- [x] run full test suite: `go test ./...`
- [x] run linter: `golangci-lint run`

### Task 6: [Final] Update documentation
- [x] update README.md with `--auth-passwd` flag documentation
- [x] update docker-compose.yml example with `AUTH_PASSWD` env var
- [x] move this plan to `docs/plans/completed/`

## Post-Completion

**Docker compose update (master-node):**
- generate bcrypt hash: `htpasswd -nbBC 10 "" '<password>' | cut -d: -f2`
- add `AUTH_PASSWD` env var to stream-recorder service in master-node's docker-compose.yml
- redeploy: `make deploy-configs && ssh server "cd /srv/radio-t/master-node && docker compose up -d stream-recorder"`
