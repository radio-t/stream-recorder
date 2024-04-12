// Package recorder is the main functionality for listerning and recording an audio stream
// methods to call SiteAPI and stream
package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Client is a client to call SiteAPI and stream
type Client struct {
	Stream     string
	SiteAPIUrl string
	client     *http.Client
}

// NewClient creates a new client with stream and site api urls
func NewClient(stream, site string) *Client {
	return &Client{
		client:     &http.Client{}, //nolint:exhaustruct
		Stream:     stream,
		SiteAPIUrl: site,
	}
}

// ErrNotFound is returned when the stream is not found
var ErrNotFound = errors.New("not found")

// FetchLatest fetches the latest entry from the SiteAPI to find out the title of the stream
func (c *Client) FetchLatest(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.SiteAPIUrl, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("error creating request: %w", err)
	}
	res, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("error making request: %w", err)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck
	var entries []Entry

	err = json.Unmarshal(body, &entries)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling response: %w", err)
	}

	return entries[0].Title, nil
}

// FetchStream fetches the stream body
func (c *Client) FetchStream(_ context.Context) (io.ReadCloser, error) {
	// context.Background is used to prevent stopping recording
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Stream, http.NoBody) //nolint:contextcheck
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return nil, ErrNotFound
	}

	return res.Body, nil
}
