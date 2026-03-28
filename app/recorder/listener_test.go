package recorder_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radio-t/stream-recorder/app/recorder"
)

func TestListener(t *testing.T) {
	t.Parallel()
	testcases := []struct {
		name      string
		client    *recorder.StreamSourceMock
		expected  string
		errorFunc assert.ErrorAssertionFunc
	}{
		{
			name: "happy test",
			client: &recorder.StreamSourceMock{
				FetchLatestFunc: func(_ context.Context) (string, error) {
					return "streamrecorderepisode test", nil
				},
				FetchStreamFunc: func(_ context.Context) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("8717895732")), nil
				},
			},
			expected:  "8717895732",
			errorFunc: assert.NoError,
		},
		{
			name: "404 test",
			client: &recorder.StreamSourceMock{
				FetchLatestFunc: func(_ context.Context) (string, error) {
					return "streamrecorderepisode test", nil
				},
				FetchStreamFunc: func(_ context.Context) (io.ReadCloser, error) {
					return nil, recorder.ErrNotFound
				},
			},
			expected:  "",
			errorFunc: assert.Error,
		},
		{
			name: "fetch latest error",
			client: &recorder.StreamSourceMock{
				FetchLatestFunc: func(_ context.Context) (string, error) {
					return "", fmt.Errorf("connection refused")
				},
				FetchStreamFunc: func(_ context.Context) (io.ReadCloser, error) {
					return nil, nil
				},
			},
			expected:  "",
			errorFunc: assert.Error,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := recorder.NewListener(tc.client)

			s, err := l.Listen(context.TODO())

			tc.errorFunc(t, err)
			if err != nil {
				return
			}

			assert.NotNil(t, s.Body)

			defer s.Body.Close() //nolint:errcheck

			buf := make([]byte, 10)
			_, err = s.Body.Read(buf)
			require.NoError(t, err)

			got := string(buf)

			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestNewStreamSanitisesNumber(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		title    string
		expected string
	}{
		{name: "normal episode", title: "Radio-T 999", expected: "999"},
		{name: "single word returns 0", title: "Radio-T", expected: "0"},
		{name: "empty string returns 0", title: "", expected: "0"},
		{name: "path traversal returns 0", title: "Radio-T ../../../etc", expected: "0"},
		{name: "slash in number returns 0", title: "Radio-T foo/bar", expected: "0"},
		{name: "backslash in number returns 0", title: "Radio-T foo\\bar", expected: "0"},
		{name: "dots in number returns 0", title: "Radio-T ..", expected: "0"},
		{name: "single dot returns 0", title: "Radio-T .", expected: "0"},
		{name: "double space handled", title: "Radio-T  999", expected: "999"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := recorder.NewStream(tc.title, io.NopCloser(strings.NewReader("")))
			assert.Equal(t, tc.expected, s.Number)
		})
	}
}
