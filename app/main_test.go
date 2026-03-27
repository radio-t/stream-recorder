package main_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/radio-t/stream-recorder/app/recorder"
	"github.com/radio-t/stream-recorder/app/server"
)

type httpClientMock struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *httpClientMock) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func icecastContainer(ctx context.Context, t *testing.T) testcontainers.Container {
	t.Helper()
	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    "testdata",
			Dockerfile: "icecast.Dockerfile",
		},
		ExposedPorts: []string{"8000/tcp"},
		WaitingFor:   wait.ForHTTP("/stream.mp3").WithPort("8000/tcp").WithPollInterval(200 * time.Millisecond),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	return container
}

// siteAPIMock returns an httpClientMock that intercepts site API calls and proxies stream requests
func siteAPIMock() *httpClientMock {
	return &httpClientMock{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/site-api/last/1" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[{"title": "Radio-T 999"}]`)),
				}, nil
			}
			return http.DefaultClient.Do(req) //nolint:gosec,nolintlint // test mock proxies to real client
		},
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0") //nolint:gosec,noctx // test helper, binding to all interfaces is fine
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

// TestFullPipeline tests the complete recording pipeline: record stream, verify file size,
// start server, verify index page lists episodes and zip download contains the recorded MP3.
func TestFullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	container := icecastContainer(ctx, t)
	defer func() { _ = container.Terminate(ctx) }()

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	streamURL := "http://" + endpoint + "/stream.mp3"
	recordsPath := t.TempDir()

	// record stream for 2 seconds
	recordCtx, recordCancel := context.WithTimeout(ctx, 2*time.Second)
	defer recordCancel()

	mc := siteAPIMock()
	client := recorder.NewClient(mc, streamURL, "/site-api/last/1")
	listener := recorder.NewListener(client)

	stream, err := listener.Listen(recordCtx)
	require.NoError(t, err)

	rec := recorder.NewRecorder(recordsPath)
	_, err = rec.Record(recordCtx, stream)
	require.ErrorIs(t, err, context.DeadlineExceeded, "recording should stop when context times out")

	// verify episode directory created with correct name
	episodeDirs, err := os.ReadDir(recordsPath)
	require.NoError(t, err)
	require.Len(t, episodeDirs, 1)
	assert.Equal(t, "999", episodeDirs[0].Name(), "episode directory should be named after episode number")

	// verify recording file created with correct naming pattern
	episodePath := path.Join(recordsPath, "999")
	files, err := os.ReadDir(episodePath)
	require.NoError(t, err)
	require.Len(t, files, 1)

	fileName := files[0].Name()
	assert.True(t, strings.HasPrefix(fileName, "rt999_"), "file should start with rt{episode}_")
	assert.True(t, strings.HasSuffix(fileName, ".mp3"), "file should have .mp3 extension")

	// verify file size is within reasonable range (2s at 128kbps ~ 32KB, expect at least 10KB)
	filePath := path.Join(episodePath, fileName)
	fi, err := os.Stat(filePath)
	require.NoError(t, err)
	assert.Greater(t, fi.Size(), int64(10*1024), "recorded file should be at least 10KB for 2s of 128kbps audio")

	// verify valid MP3 header
	data, err := os.ReadFile(filePath) //nolint:gosec // test file path from t.TempDir()
	require.NoError(t, err)
	require.Greater(t, len(data), 3, "file should have content")
	isMP3 := (data[0] == 0xFF && (data[1]&0xE0) == 0xE0) || string(data[0:3]) == "ID3"
	assert.True(t, isMP3, "file should have valid MP3 header")

	// start server and verify HTTP endpoints
	port := freePort(t)
	srv := server.NewServer(strconv.Itoa(port), recordsPath, "", nil, nil)
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Stop(context.Background()) }()

	baseURL := fmt.Sprintf("http://localhost:%d", port)
	require.Eventually(t, func() bool {
		resp, rErr := http.Get(baseURL + "/") //nolint:noctx // test URL
		if rErr != nil {
			return false
		}
		resp.Body.Close() //nolint:errcheck,gosec
		return resp.StatusCode == http.StatusOK
	}, 3*time.Second, 100*time.Millisecond, "server should start within 3 seconds")

	t.Run("index page lists episode", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/") //nolint:noctx // test URL
		require.NoError(t, err)
		defer resp.Body.Close() //nolint:errcheck

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "999", "index page should list episode 999")
		assert.Contains(t, string(body), fileName, "index page should list the recorded file")
	})

	t.Run("episode zip download", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/episode/999") //nolint:noctx // test URL
		require.NoError(t, err)
		defer resp.Body.Close() //nolint:errcheck

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/zip", resp.Header.Get("Content-Type"))
		assert.Contains(t, resp.Header.Get("Content-Disposition"), "999.zip")

		// read zip and verify it contains the recorded MP3
		zipData, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
		require.NoError(t, err)
		require.Len(t, zr.File, 1, "zip should contain exactly one file")
		assert.Equal(t, fileName, zr.File[0].Name, "zip should contain the recorded MP3")
	})
}

// TestRecordingCancellation verifies that cancelling context during recording stops it cleanly.
// Reuses the same container image as TestFullPipeline (Docker caches the build).
func TestRecordingCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	container := icecastContainer(ctx, t)
	defer func() { _ = container.Terminate(ctx) }()

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	streamURL := "http://" + endpoint + "/stream.mp3"
	recordsPath := t.TempDir()

	mc := siteAPIMock()
	client := recorder.NewClient(mc, streamURL, "/site-api/last/1")
	listener := recorder.NewListener(client)

	recordCtx, recordCancel := context.WithCancel(ctx)

	stream, err := listener.Listen(recordCtx)
	require.NoError(t, err)

	rec := recorder.NewRecorder(recordsPath)

	// cancel recording after 500ms
	go func() {
		time.Sleep(500 * time.Millisecond)
		recordCancel()
	}()

	start := time.Now()
	_, err = rec.Record(recordCtx, stream)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.Canceled, "Record should return context.Canceled")
	assert.Less(t, elapsed, 3*time.Second, "Record should stop promptly after cancellation")

	// verify partial recording was written
	episodeDirs, err := os.ReadDir(recordsPath)
	require.NoError(t, err)
	require.Len(t, episodeDirs, 1)

	episodePath := path.Join(recordsPath, "999")
	files, err := os.ReadDir(episodePath)
	require.NoError(t, err)
	require.Len(t, files, 1, "partial recording file should exist")

	fi, err := files[0].Info()
	require.NoError(t, err)
	assert.Positive(t, fi.Size(), "partial recording should have some data")
}
