package recorder

import (
	"context"
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
		name           string
		doFunc         func (*http.Request) (*http.Response, error) 
		expectedTitle  string
		errorFunc      assert.ErrorAssertionFunc
	}{
		{
			name:           "successful fetch",
			doFunc: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[{"title":"Radio-T 904"}]`)),
				}, nil
			},
		  expectedTitle:  "Radio-T 904",
			errorFunc:      assert.NoError,
		},
		{
			name:           "no entries",
			doFunc: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[]`)),
				}, nil
			},
		  expectedTitle:  "",
			errorFunc:      assert.Error,
		},
		{
			name:           "empty entry",
			doFunc: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`[{}]`)),
				}, nil
			},
		  expectedTitle:  "",
			errorFunc:      assert.NoError,
		},
		{
			name:           "error unmarshalling response",
			doFunc:     		func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("invalid json")),
				}, nil
			},
			expectedTitle:  "",
			errorFunc:      assert.Error,
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
		name           string
		doFunc         func (*http.Request) (*http.Response, error)
		responseBody   string
		expectedError  error
	}{
		{
			name:           "successful fetch",
			doFunc: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("test response body")),
				}, nil
			},
			responseBody:   "test response body",
			expectedError:  nil,
		},
		{
			name:           "error response",
			doFunc: func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
				}, nil
			},
			responseBody:   "",
			expectedError:  ErrNotFound,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient(&HTTPClientMock{
				DoFunc: tc.doFunc,
			}, "", "")

			reader, err := client.FetchStream(context.Background())
			if tc.expectedError != nil {
				assert.ErrorIs(t, err, tc.expectedError)
			} else {
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, tc.responseBody, string(data))
			}
		})
	}
}
