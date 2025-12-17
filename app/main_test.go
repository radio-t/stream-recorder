package main_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/radio-t/stream-recorder/app/recorder"
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
		WaitingFor:   wait.ForHTTP("/stream.mp3").WithPort("8000/tcp").WithPollInterval(time.Millisecond * 200),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	return container
}

// TestRecordingPipeline tests the recording pipeline with a real Icecast stream.
// This is an integration test that verifies Client, Listener, and Recorder work together.
// Note: Site API is mocked, only the stream is real.
func TestRecordingPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	container := icecastContainer(ctx, t)
	defer func() { _ = container.Terminate(ctx) }()

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	// mock Site API, use real HTTP for stream
	mc := &httpClientMock{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/site-api/last/1" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[{"title": "Radio-T 999"}]`)),
				}, nil
			}
			return http.DefaultClient.Do(req)
		},
	}

	streamURL := "http://" + endpoint + "/stream.mp3"
	client := recorder.NewClient(mc, streamURL, "/site-api/last/1")
	listener := recorder.NewListener(client)

	stream, err := listener.Listen(ctx)
	require.NoError(t, err)

	recordsPath := t.TempDir()
	rec := recorder.NewRecorder(recordsPath)

	err = rec.Record(ctx, stream)
	require.NoError(t, err)

	// verify episode directory created with correct name
	episodeDirs, err := os.ReadDir(recordsPath)
	require.NoError(t, err)
	require.Len(t, episodeDirs, 1)
	assert.Equal(t, "999", episodeDirs[0].Name(), "episode directory should be named after episode number")

	// verify recording file created with correct naming pattern
	episodePath := path.Join(recordsPath, episodeDirs[0].Name())
	files, err := os.ReadDir(episodePath)
	require.NoError(t, err)
	require.Len(t, files, 1)

	fileName := files[0].Name()
	assert.True(t, strings.HasPrefix(fileName, "rt999_"), "file should start with rt{episode}_")
	assert.True(t, strings.HasSuffix(fileName, ".mp3"), "file should have .mp3 extension")

	// verify file has content and valid MP3 header
	filePath := path.Join(episodePath, fileName)
	data, err := os.ReadFile(filePath) //nolint:gosec // test file path from t.TempDir()
	require.NoError(t, err)
	require.Greater(t, len(data), 3, "file should have content")

	// check for MP3 frame sync (0xFF 0xFB/FA/F3/F2) or ID3 tag
	isMP3 := (data[0] == 0xFF && (data[1]&0xE0) == 0xE0) || string(data[0:3]) == "ID3"
	assert.True(t, isMP3, "file should have valid MP3 header")
}
