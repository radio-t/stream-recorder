package server

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	return NewServer("0", dir)
}

func newRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, http.NoBody)
	require.NoError(t, err)
	return req
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

	t.Run("non-root path returns 404", func(t *testing.T) {
		dir := setupTestDir(t)
		srv := newTestServer(t, dir)

		req := newRequest(t, "/favicon.ico")
		rec := httptest.NewRecorder()

		srv.IndexHandler(rec, req)

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
			rec := httptest.NewRecorder()

			srv.DownloadEpisodeHandler(rec, req)

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
		rec := httptest.NewRecorder()

		srv.DownloadEpisodeHandler(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("path traversal returns 400", func(t *testing.T) {
		req := newRequest(t, "/episode/../../etc")
		rec := httptest.NewRecorder()

		srv.DownloadEpisodeHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("empty episode name returns 400", func(t *testing.T) {
		req := newRequest(t, "/episode/")
		rec := httptest.NewRecorder()

		srv.DownloadEpisodeHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("dot episode name returns 400", func(t *testing.T) {
		req := newRequest(t, "/episode/.")
		rec := httptest.NewRecorder()

		srv.DownloadEpisodeHandler(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("regular file instead of directory returns 404", func(t *testing.T) {
		fileDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(fileDir, "notadir"), []byte("data"), 0o600))
		fileSrv := newTestServer(t, fileDir)

		req := newRequest(t, "/episode/notadir")
		rec := httptest.NewRecorder()

		fileSrv.DownloadEpisodeHandler(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
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
