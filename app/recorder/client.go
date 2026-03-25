// Package recorder provides stream discovery, fetching and recording.
// Client fetches metadata (episode title) from the SiteAPI and the audio stream body.
// Listener combines both into a Stream (episode number + body). Recorder writes
// the stream body to disk, one file per recording session.
package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// HTTPClient interface to make HTTP requests
//
//go:generate moq -out httpclient_moq_test.go . HTTPClient
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client fetches episode metadata from the SiteAPI and the raw audio stream.
type Client struct {
	client     HTTPClient
	Stream     string
	SiteAPIUrl string
}

// NewClient creates a new client with stream and site api urls
func NewClient(c HTTPClient, stream, site string) *Client {
	return &Client{
		client:     c,
		Stream:     stream,
		SiteAPIUrl: site,
	}
}

// ErrNotFound is returned when the stream is not found
var ErrNotFound = errors.New("not found")

// Entry represents a single entry from the SiteAPI
type Entry struct {
	Title string `json:"title"`
}

// FetchLatest fetches the latest entry from the SiteAPI to find out the title of the stream
func (c *Client) FetchLatest(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.SiteAPIUrl, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	res, err := c.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("error making request: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("site API returned status %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response: %w", err)
	}

	var entries []Entry
	err = json.Unmarshal(body, &entries)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling response: %w", err)
	}

	if len(entries) == 0 {
		return "", ErrNotFound
	}

	return entries[0].Title, nil
}

// FetchStream fetches the stream body
func (c *Client) FetchStream(ctx context.Context) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Stream, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		res.Body.Close() //nolint:errcheck,gosec
		return nil, ErrNotFound
	}

	return res.Body, nil
}
