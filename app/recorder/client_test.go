package recorder

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchLatest(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name          string
		doFunc        func(*http.Request) (*http.Response, error)
		expectedTitle string
		errorFunc     assert.ErrorAssertionFunc
	}{
		{
			name: "successful fetch",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[{"title":"Radio-T 904"}]`)),
				}, nil
			},
			expectedTitle: "Radio-T 904",
			errorFunc:     assert.NoError,
		},
		{
			name: "no entries",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[]`)),
				}, nil
			},
			expectedTitle: "",
			errorFunc:     assert.Error,
		},
		{
			name: "empty entry",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[{}]`)),
				}, nil
			},
			expectedTitle: "",
			errorFunc:     assert.NoError,
		},
		{
			name: "non-OK status returns error",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("invalid json")),
				}, nil
			},
			expectedTitle: "",
			errorFunc:     assert.Error,
		},
		{
			name: "error unmarshalling response",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("invalid json")),
				}, nil
			},
			expectedTitle: "",
			errorFunc:     assert.Error,
		},
		{
			name: "http request error",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("connection refused")
			},
			expectedTitle: "",
			errorFunc:     assert.Error,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient(&HTTPClientMock{
				DoFunc: tc.doFunc,
			}, "", "")

			title, err := client.FetchLatest(context.Background())

			tc.errorFunc(t, err)
			assert.Equal(t, tc.expectedTitle, title)
		})
	}
}

func TestFetchStream(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name          string
		doFunc        func(*http.Request) (*http.Response, error)
		responseBody  string
		expectedError error
		wantErr       bool
	}{
		{
			name: "successful fetch",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("test response body")),
				}, nil
			},
			responseBody: "test response body",
		},
		{
			name: "error response",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			},
			expectedError: ErrNotFound,
		},
		{
			name: "http request error",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("connection refused")
			},
			wantErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient(&HTTPClientMock{
				DoFunc: tc.doFunc,
			}, "", "")

			reader, err := client.FetchStream(context.Background())
			switch {
			case tc.expectedError != nil:
				require.ErrorIs(t, err, tc.expectedError)
			case tc.wantErr:
				require.Error(t, err)
			default:
				data, readErr := io.ReadAll(reader)
				require.NoError(t, readErr)
				assert.Equal(t, tc.responseBody, string(data))
			}
		})
	}
}
