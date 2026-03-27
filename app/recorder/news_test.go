package recorder

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewsClient_FetchActiveID(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name       string
		doFunc     func(*http.Request) (*http.Response, error)
		expectedID string
		errorFunc  assert.ErrorAssertionFunc
	}{
		{
			name: "successful fetch",
			doFunc: func(req *http.Request) (*http.Response, error) {
				assert.Contains(t, req.URL.String(), "/news/active/id")
				assert.Equal(t, "stream-recorder", req.Header.Get("User-Agent"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"id":"abc123"}`)),
				}, nil
			},
			expectedID: "abc123",
			errorFunc:  assert.NoError,
		},
		{
			name: "non-200 status",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			},
			expectedID: "",
			errorFunc:  assert.Error,
		},
		{
			name: "invalid JSON",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("not json")),
				}, nil
			},
			expectedID: "",
			errorFunc:  assert.Error,
		},
		{
			name: "http request error",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("connection refused")
			},
			expectedID: "",
			errorFunc:  assert.Error,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			nc := NewNewsClient(&HTTPClientMock{DoFunc: tc.doFunc}, "http://example.com/api/v1")
			id, err := nc.FetchActiveID(context.Background())
			tc.errorFunc(t, err)
			assert.Equal(t, tc.expectedID, id)
		})
	}
}

func TestNewsClient_FetchArticle(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name            string
		doFunc          func(*http.Request) (*http.Response, error)
		expectedArticle Article
		errorFunc       assert.ErrorAssertionFunc
	}{
		{
			name: "successful fetch",
			doFunc: func(req *http.Request) (*http.Response, error) {
				assert.Contains(t, req.URL.String(), "/news/id/abc123")
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"title":"Some News","link":"https://example.com/news","activets":"2026-03-27T10:00:00Z"}`)),
				}, nil
			},
			expectedArticle: Article{
				Title:    "Some News",
				Link:     "https://example.com/news",
				ActiveTS: time.Date(2026, 3, 27, 10, 0, 0, 0, time.UTC),
			},
			errorFunc: assert.NoError,
		},
		{
			name: "non-200 status",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			},
			expectedArticle: Article{},
			errorFunc:       assert.Error,
		},
		{
			name: "invalid JSON",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("bad")),
				}, nil
			},
			expectedArticle: Article{},
			errorFunc:       assert.Error,
		},
		{
			name: "http request error",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("timeout")
			},
			expectedArticle: Article{},
			errorFunc:       assert.Error,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			nc := NewNewsClient(&HTTPClientMock{DoFunc: tc.doFunc}, "http://example.com/api/v1")
			article, err := nc.FetchArticle(context.Background(), "abc123")
			tc.errorFunc(t, err)
			assert.Equal(t, tc.expectedArticle, article)
		})
	}
}

func TestNewsClient_WaitActiveChange(t *testing.T) {
	t.Parallel()

	testcases := []struct {
		name       string
		doFunc     func(*http.Request) (*http.Response, error)
		timeout    time.Duration
		expectedID string
		errorFunc  assert.ErrorAssertionFunc
	}{
		{
			name: "successful wait",
			doFunc: func(req *http.Request) (*http.Response, error) {
				assert.Contains(t, req.URL.String(), "/news/active/wait/5000")
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"id":"xyz789"}`)),
				}, nil
			},
			timeout:    5 * time.Second,
			expectedID: "xyz789",
			errorFunc:  assert.NoError,
		},
		{
			name: "non-200 status",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusGatewayTimeout,
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			},
			timeout:    5 * time.Second,
			expectedID: "",
			errorFunc:  assert.Error,
		},
		{
			name: "invalid JSON",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("{")),
				}, nil
			},
			timeout:    5 * time.Second,
			expectedID: "",
			errorFunc:  assert.Error,
		},
		{
			name: "http request error",
			doFunc: func(_ *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("connection reset")
			},
			timeout:    5 * time.Second,
			expectedID: "",
			errorFunc:  assert.Error,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			nc := NewNewsClient(&HTTPClientMock{DoFunc: tc.doFunc}, "http://example.com/api/v1")
			id, err := nc.WaitActiveChange(context.Background(), tc.timeout)
			tc.errorFunc(t, err)
			assert.Equal(t, tc.expectedID, id)
		})
	}
}

func TestNewsClient_TrailingSlashNormalized(t *testing.T) {
	t.Parallel()

	var requestedURL string
	nc := NewNewsClient(&HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"abc"}`)),
			}, nil
		},
	}, "http://example.com/api/v1/") // trailing slash

	_, err := nc.FetchActiveID(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "http://example.com/api/v1/news/active/id", requestedURL,
		"trailing slash should be stripped to avoid double-slash URLs")
}

func TestNewsClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	nc := NewNewsClient(&HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, req.Context().Err()
		},
	}, "http://example.com/api/v1")

	_, err := nc.FetchActiveID(ctx)
	require.Error(t, err)

	_, err = nc.FetchArticle(ctx, "abc")
	require.Error(t, err)

	_, err = nc.WaitActiveChange(ctx, 5*time.Second)
	require.Error(t, err)
}
