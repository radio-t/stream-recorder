package main_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/radio-t/stream-recorder/app/recorder"
)

func icecastContainer(ctx context.Context, t *testing.T) testcontainers.Container {
	t.Helper()
	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    "testdata",
			Dockerfile: "icecast.Dockerfile",
		},
		ExposedPorts: []string{"8000/tcp"},
		WaitingFor:   wait.ForHTTP("http://0.0.0.0:8000/stream.mp3").WithPollInterval(time.Millisecond * 200),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	return container
}

func TestWithIcecast(t *testing.T) {
	ctx := context.Background()

	container := icecastContainer(ctx, t)

	defer func() {
		err := container.Terminate(ctx)
		require.NoError(t, err)
	}()

	endpoint, err := container.Endpoint(ctx, "")
	require.NoError(t, err)

	mc := &recorder.HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/site-api/last/1" {
				return &http.Response{
					Body: io.NopCloser((strings.NewReader(`[{"title": "Radio-t test"}]`))),
				}, nil
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("failed to do request: %w", err)
			}
			return res, nil
		},
	}

	url := "http://" + endpoint + "/stream.mp3"
	c := recorder.NewClient(mc, url, "/site-api/last/1")
	l := recorder.NewListener(c)

	stream, err := l.Listen(ctx)
	require.NoError(t, err)

	tmp := os.TempDir()
	recordsPath := path.Join(tmp, "records")
	defer func () {
		err := os.RemoveAll(recordsPath)
		require.NoError(t, err)
	}()

	r := recorder.NewRecorder(recordsPath)

	err = r.Record(ctx, stream)
	require.NoError(t, err)

	records, err := os.ReadDir(recordsPath)
	require.NoError(t, err)
	require.Len(t, records, 1)

	fi, err := records[0].Info()
	require.NoError(t, err)
	require.NotZero(t, fi.Size())
}
