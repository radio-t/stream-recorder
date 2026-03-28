package server

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// setupTestDir creates a temp directory with fake episode directories and files.
func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// create episode directories with recording files
	episodes := map[string][]string{
		"999": {"rt999_2025-01-01.mp3", "rt999_2025-01-02.mp3"},
		"998": {"rt998_2025-01-03.mp3"},
	}
	for ep, files := range episodes {
		epDir := filepath.Join(dir, ep)
		require.NoError(t, os.Mkdir(epDir, 0o750))
		for _, f := range files {
			require.NoError(t, os.WriteFile(filepath.Join(epDir, f), []byte("fake mp3 data for "+f), 0o600))
		}
	}

	return dir
}

func newTestServer(t *testing.T, dir string) *Server {
	t.Helper()
	return NewServer("0", dir, "", nil, nil)
}

func newRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
	require.NoError(t, err)
	return req
}

// serveHTTP routes the request through the server's router, so path values and route matching work
func serveHTTP(srv *Server, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	return rec
}

func TestIndexHandler(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		contains []string
		excludes []string
		status   int
	}{
		{
			name:     "lists episodes and files",
			setup:    setupTestDir,
			contains: []string{"999", "998", "rt999_2025-01-01.mp3", "rt999_2025-01-02.mp3", "rt998_2025-01-03.mp3"},
			excludes: []string{"Start Recording"},
			status:   http.StatusOK,
		},
		{
			name: "empty directory shows no rows",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			contains: []string{"no rows"},
			excludes: []string{"999"},
			status:   http.StatusOK,
		},
		{
			name: "non-existent directory returns error",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "nonexistent")
			},
			status: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.setup(t)
			srv := newTestServer(t, dir)

			req := newRequest(t, "/")
			rec := httptest.NewRecorder()

			srv.IndexHandler(rec, req)

			assert.Equal(t, tc.status, rec.Code)
			body := rec.Body.String()
			for _, s := range tc.contains {
				assert.Contains(t, body, s)
			}
			for _, s := range tc.excludes {
				assert.NotContains(t, body, s)
			}
		})
	}

	t.Run("shows Start Recording button when forceRecord is set", func(t *testing.T) {
		dir := setupTestDir(t)
		var flag atomic.Bool
		srv := NewServer("0", dir, "", &flag, nil)

		req := newRequest(t, "/")
		rec := httptest.NewRecorder()
		srv.IndexHandler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Start Recording")
	})

	t.Run("shows recording in progress status", func(t *testing.T) {
		dir := setupTestDir(t)
		var forceFlag atomic.Bool
		var recFlag atomic.Bool
		recFlag.Store(true)
		srv := NewServer("0", dir, "", &forceFlag, &recFlag)

		req := newRequest(t, "/")
		rec := httptest.NewRecorder()
		srv.IndexHandler(rec, req)

		body := rec.Body.String()
		assert.Contains(t, body, "Recording in progress")
		assert.Contains(t, body, "disabled")
	})

	t.Run("shows record requested status", func(t *testing.T) {
		dir := setupTestDir(t)
		var forceFlag atomic.Bool
		forceFlag.Store(true)
		srv := NewServer("0", dir, "", &forceFlag, nil)

		req := newRequest(t, "/")
		rec := httptest.NewRecorder()
		srv.IndexHandler(rec, req)

		body := rec.Body.String()
		assert.Contains(t, body, "Waiting for stream")
		assert.Contains(t, body, "disabled")
	})

	t.Run("shows password field when auth enabled", func(t *testing.T) {
		dir := setupTestDir(t)
		var flag atomic.Bool
		hash, hashErr := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
		require.NoError(t, hashErr)
		srv := NewServer("0", dir, string(hash), &flag, nil)

		req := newRequest(t, "/")
		rec := httptest.NewRecorder()
		srv.IndexHandler(rec, req)

		body := rec.Body.String()
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, body, `type="password"`)
		assert.Contains(t, body, `name="password"`)
		assert.Contains(t, body, "Start Recording")
	})

	t.Run("no password field when auth disabled", func(t *testing.T) {
		dir := setupTestDir(t)
		var flag atomic.Bool
		srv := NewServer("0", dir, "", &flag, nil)

		req := newRequest(t, "/")
		rec := httptest.NewRecorder()
		srv.IndexHandler(rec, req)

		body := rec.Body.String()
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, body, "Start Recording")
		assert.NotContains(t, body, `type="password"`)
	})

	t.Run("non-root path returns 404", func(t *testing.T) {
		dir := setupTestDir(t)
		srv := newTestServer(t, dir)

		req := newRequest(t, "/favicon.ico")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestHealthHandler(t *testing.T) {
	t.Run("healthy disk returns 200", func(t *testing.T) {
		dir := t.TempDir()
		srv := newTestServer(t, dir)
		srv.warnCapacity = 100 // any capacity is below threshold

		req := newRequest(t, "/health")
		rec := httptest.NewRecorder()

		srv.HealthHandler(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Empty(t, rec.Body.String())
	})

	t.Run("disk above threshold returns 500", func(t *testing.T) {
		dir := t.TempDir()
		srv := newTestServer(t, dir)
		srv.warnCapacity = 0 // any capacity exceeds threshold

		req := newRequest(t, "/health")
		rec := httptest.NewRecorder()

		srv.HealthHandler(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.Contains(t, rec.Body.String(), "disk is")
	})

	t.Run("non-existent directory returns error", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nonexistent")
		srv := newTestServer(t, dir)

		req := newRequest(t, "/health")
		rec := httptest.NewRecorder()

		srv.HealthHandler(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestDownloadEpisodeHandler(t *testing.T) {
	tests := []struct {
		name          string
		episode       string
		expectedFiles []string
	}{
		{
			name:          "downloads episode as zip with all files",
			episode:       "999",
			expectedFiles: []string{"rt999_2025-01-01.mp3", "rt999_2025-01-02.mp3"},
		},
		{
			name:          "downloads single-file episode",
			episode:       "998",
			expectedFiles: []string{"rt998_2025-01-03.mp3"},
		},
	}

	dir := setupTestDir(t)
	srv := newTestServer(t, dir)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequest(t, "/episode/"+tc.episode)
			rec := serveHTTP(srv, req)

			resp := rec.Result()
			defer resp.Body.Close() //nolint:errcheck

			assert.Equal(t, "application/zip", resp.Header.Get("Content-Type"))
			assert.Contains(t, resp.Header.Get("Content-Disposition"), tc.episode+".zip")

			// read and verify zip contents
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
			require.NoError(t, err)

			fileNames := make([]string, 0, len(zipReader.File))
			for _, f := range zipReader.File {
				fileNames = append(fileNames, f.Name)

				// verify file content matches what we wrote
				rc, err := f.Open()
				require.NoError(t, err)
				content, err := io.ReadAll(rc)
				require.NoError(t, err)
				require.NoError(t, rc.Close())
				assert.Equal(t, "fake mp3 data for "+f.Name, string(content))
			}

			assert.ElementsMatch(t, tc.expectedFiles, fileNames)
		})
	}

	t.Run("non-existent episode returns 404", func(t *testing.T) {
		req := newRequest(t, "/episode/000")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("path traversal returns 400", func(t *testing.T) {
		req := newRequest(t, "/episode/..%2F..%2Fetc")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("regular file instead of directory returns 404", func(t *testing.T) {
		fileDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(fileDir, "notadir"), []byte("data"), 0o600))
		fileSrv := newTestServer(t, fileDir)

		req := newRequest(t, "/episode/notadir")
		rec := serveHTTP(fileSrv, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestForceRecordHandler(t *testing.T) {
	t.Run("POST /record sets force flag and redirects to index", func(t *testing.T) {
		dir := t.TempDir()
		var flag atomic.Bool
		srv := NewServer("0", dir, "", &flag, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", http.NoBody)
		require.NoError(t, err)

		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/", rec.Header().Get("Location"))
		assert.True(t, flag.Load(), "force record flag should be set after POST /record")
	})

	t.Run("route not registered when forceRecord is nil", func(t *testing.T) {
		dir := t.TempDir()
		srv := NewServer("0", dir, "", nil, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", http.NoBody)
		require.NoError(t, err)

		rec := serveHTTP(srv, req)
		assert.NotEqual(t, http.StatusOK, rec.Code, "POST /record should not succeed when forceRecord is nil")
	})
}

func TestCheckAuth(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	require.NoError(t, err)

	tests := []struct {
		name   string
		setup  func(t *testing.T) *http.Request
		expect bool
	}{
		{
			name: "correct password via form body",
			setup: func(t *testing.T) *http.Request {
				t.Helper()
				body := strings.NewReader(url.Values{"password": {"secret123"}}.Encode())
				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", body)
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return req
			},
			expect: true,
		},
		{
			name: "correct password via basic auth",
			setup: func(t *testing.T) *http.Request {
				t.Helper()
				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", http.NoBody)
				require.NoError(t, err)
				req.SetBasicAuth("any", "secret123")
				return req
			},
			expect: true,
		},
		{
			name: "wrong password via form body",
			setup: func(t *testing.T) *http.Request {
				t.Helper()
				body := strings.NewReader(url.Values{"password": {"wrong"}}.Encode())
				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", body)
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				return req
			},
			expect: false,
		},
		{
			name: "wrong password via basic auth",
			setup: func(t *testing.T) *http.Request {
				t.Helper()
				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", http.NoBody)
				require.NoError(t, err)
				req.SetBasicAuth("any", "wrong")
				return req
			},
			expect: false,
		},
		{
			name: "correct password via query param is rejected",
			setup: func(t *testing.T) *http.Request {
				t.Helper()
				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record?password=secret123", http.NoBody)
				require.NoError(t, err)
				return req
			},
			expect: false,
		},
		{
			name: "no credentials",
			setup: func(t *testing.T) *http.Request {
				t.Helper()
				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", http.NoBody)
				require.NoError(t, err)
				return req
			},
			expect: false,
		},
	}

	dir := t.TempDir()
	var flag atomic.Bool
	srv := NewServer("0", dir, string(hash), &flag, nil)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.setup(t)
			assert.Equal(t, tc.expect, srv.checkAuth(req))
		})
	}
}

func TestForceRecordHandler_Auth(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	require.NoError(t, err)

	t.Run("returns 403 without credentials when auth enabled", func(t *testing.T) {
		dir := t.TempDir()
		var flag atomic.Bool
		srv := NewServer("0", dir, string(hash), &flag, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", http.NoBody)
		require.NoError(t, err)

		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.False(t, flag.Load(), "force record flag should not be set without credentials")
	})

	t.Run("returns 403 with wrong password when auth enabled", func(t *testing.T) {
		dir := t.TempDir()
		var flag atomic.Bool
		srv := NewServer("0", dir, string(hash), &flag, nil)

		body := strings.NewReader(url.Values{"password": {"wrong"}}.Encode())
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.False(t, flag.Load(), "force record flag should not be set with wrong password")
	})

	t.Run("redirects with correct password via form body", func(t *testing.T) {
		dir := t.TempDir()
		var flag atomic.Bool
		srv := NewServer("0", dir, string(hash), &flag, nil)

		body := strings.NewReader(url.Values{"password": {"secret123"}}.Encode())
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/", rec.Header().Get("Location"))
		assert.True(t, flag.Load(), "force record flag should be set with correct password")
	})

	t.Run("redirects with correct password via basic auth", func(t *testing.T) {
		dir := t.TempDir()
		var flag atomic.Bool
		srv := NewServer("0", dir, string(hash), &flag, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", http.NoBody)
		require.NoError(t, err)
		req.SetBasicAuth("any", "secret123")

		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/", rec.Header().Get("Location"))
		assert.True(t, flag.Load(), "force record flag should be set with correct basic auth")
	})

	t.Run("works without credentials when auth disabled", func(t *testing.T) {
		dir := t.TempDir()
		var flag atomic.Bool
		srv := NewServer("0", dir, "", &flag, nil)

		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/record", http.NoBody)
		require.NoError(t, err)

		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Equal(t, "/", rec.Header().Get("Location"))
		assert.True(t, flag.Load(), "force record flag should be set without auth when disabled")
	})
}

func TestGETEndpoints_WithAuthEnabled(t *testing.T) {
	dir := setupTestDir(t)
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	require.NoError(t, err)

	var forceFlag atomic.Bool
	var recFlag atomic.Bool
	srv := NewServer("0", dir, string(hash), &forceFlag, &recFlag)
	srv.warnCapacity = 100

	t.Run("index page accessible without credentials", func(t *testing.T) {
		req := newRequest(t, "/")
		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "Stream Recorder")
	})

	t.Run("health endpoint accessible without credentials", func(t *testing.T) {
		req := newRequest(t, "/health")
		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("episode download accessible without credentials", func(t *testing.T) {
		req := newRequest(t, "/episode/999")
		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	})

	t.Run("file download accessible without credentials", func(t *testing.T) {
		req := newRequest(t, "/episode/999/rt999_2025-01-01.mp3")
		rec := serveHTTP(srv, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("live stream endpoint accessible without credentials", func(t *testing.T) {
		req := newRequest(t, "/live/test.mp3")
		rec := serveHTTP(srv, req)
		// 404 because not recording, but NOT 403 - auth is not checked
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Contains(t, rec.Body.String(), "not recording")
	})
}

func TestDownloadFileHandler(t *testing.T) {
	dir := setupTestDir(t)
	srv := newTestServer(t, dir)

	t.Run("serves file inline by default", func(t *testing.T) {
		req := newRequest(t, "/episode/999/rt999_2025-01-01.mp3")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotContains(t, rec.Header().Get("Content-Disposition"), "attachment")
		assert.Contains(t, rec.Body.String(), "fake mp3 data for rt999_2025-01-01.mp3")
	})

	t.Run("forces download with ?download query param", func(t *testing.T) {
		req := newRequest(t, "/episode/999/rt999_2025-01-01.mp3?download")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
	})

	t.Run("non-existent file returns 404", func(t *testing.T) {
		req := newRequest(t, "/episode/999/nonexistent.mp3")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("path traversal in file returns 400", func(t *testing.T) {
		req := newRequest(t, "/episode/999/..%2F..%2Fetc")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("path traversal in folder returns 400", func(t *testing.T) {
		req := newRequest(t, "/episode/..%2F999/file.mp3")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestLiveStreamHandler(t *testing.T) {
	t.Run("returns 404 when not recording", func(t *testing.T) {
		dir := setupTestDir(t)
		srv := newTestServer(t, dir)

		req := newRequest(t, "/live/test.mp3")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("returns 404 when recording flag set but no files", func(t *testing.T) {
		dir := t.TempDir()
		var recording atomic.Bool
		recording.Store(true)
		srv := NewServer("0", dir, "", nil, &recording)

		req := newRequest(t, "/live/test.mp3")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("streams audio when recording then stops", func(t *testing.T) {
		dir := setupTestDir(t)
		var recording atomic.Bool
		recording.Store(true)
		srv := NewServer("0", dir, "", nil, &recording)

		// stop recording after a short delay so tailFile exits
		go func() {
			time.Sleep(400 * time.Millisecond)
			recording.Store(false)
		}()

		req := newRequest(t, "/live/test.mp3")
		rec := serveHTTP(srv, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "audio/mpeg", rec.Header().Get("Content-Type"))
		assert.Equal(t, "no-cache, no-store", rec.Header().Get("Cache-Control"))
	})
}

func TestActiveFileInfo(t *testing.T) {
	t.Run("returns empty when no episodes", func(t *testing.T) {
		dir := t.TempDir()
		srv := newTestServer(t, dir)

		name, path := srv.activeFileInfo()
		assert.Empty(t, name)
		assert.Empty(t, path)
	})

	t.Run("returns last file of last episode", func(t *testing.T) {
		dir := setupTestDir(t)
		srv := newTestServer(t, dir)

		name, filePath := srv.activeFileInfo()
		assert.Equal(t, "999", name)
		assert.Contains(t, filePath, "999")
		assert.Contains(t, filePath, ".mp3")
	})
}

func TestValidPathSegment(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"episode1", true},
		{"file.mp3", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../etc", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.valid, validPathSegment(tc.input))
		})
	}
}

func TestIsSubPath(t *testing.T) {
	tests := []struct {
		base, target string
		want         bool
	}{
		{"/data", "/data/episode1", true},
		{"/data", "/data/episode1/file.mp3", true},
		{"/data", "/data", false},               // base itself is not "under" base
		{"/data", "/data2/file", false},         // sibling directory
		{"/data", "/other/file", false},         // completely different path
		{"/data", "/data/../etc/passwd", false}, // traversal cleaned by filepath.Clean
		{"data", "data/episode1", true},         // relative paths
		{"data", "data/ep/file.mp3", true},
		{"./", "999", true},          // default --dir value with clean join
		{"./", "999/file.mp3", true}, // nested under default dir
	}
	for _, tc := range tests {
		t.Run(tc.base+"_"+tc.target, func(t *testing.T) {
			assert.Equal(t, tc.want, isSubPath(tc.base, tc.target))
		})
	}
}

func TestListEpisodes(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		expected []episode
		wantErr  bool
	}{
		{
			name: "lists episodes with files",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				epDir := filepath.Join(dir, "999")
				require.NoError(t, os.Mkdir(epDir, 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(epDir, "a.mp3"), []byte("data"), 0o600))
				return dir
			},
			expected: []episode{{Name: "999", Files: []string{"a.mp3"}}},
		},
		{
			name: "skips regular files in root",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("data"), 0o600))
				epDir := filepath.Join(dir, "999")
				require.NoError(t, os.Mkdir(epDir, 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(epDir, "a.mp3"), []byte("data"), 0o600))
				return dir
			},
			expected: []episode{{Name: "999", Files: []string{"a.mp3"}}},
		},
		{
			name: "empty directory returns empty slice",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			expected: []episode{},
		},
		{
			name: "non-existent directory returns error",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "nonexistent")
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.setup(t)
			episodes, err := listEpisodes(dir)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expected, episodes)
		})
	}
}
