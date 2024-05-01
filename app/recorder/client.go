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

// HTTPClient interface to make HTTP requests
//go:generate moq -out httpclient_moq.go . HTTPClient
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is a client to call SiteAPI and stream
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
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response: %w", err)
	}
	defer res.Body.Close()
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
		return nil, ErrNotFound
	}

	return res.Body, nil
}
