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
		client    *recorder.ClientlikeMock
		expected  string
		errorFunc assert.ErrorAssertionFunc
	}{
		{
			name: "happy test",
			client: &recorder.ClientlikeMock{
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
			client: &recorder.ClientlikeMock{
				FetchLatestFunc: func(_ context.Context) (string, error) {
					return "streamrecorderepisode test", nil
				},
				FetchStreamFunc: func(_ context.Context) (io.ReadCloser, error) {
					return nil, recorder.ErrNotFound
				},
			},
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

			assert.Equal(t, tc.expected, got, fmt.Sprintf(`expected stream of: %q but got: %q`, tc.expected, got))
		})
	}
}
