package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	Stream     string
	SiteAPIUrl string
	client     *http.Client
}

func NewClient(stream, site string) *Client {
	return &Client{
		client:     &http.Client{}, //nolint:exhaustruct
		Stream:     stream,
		SiteAPIUrl: site,
	}
}

var ErrNotFound = fmt.Errorf("not found")

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
	defer res.Body.Close()
	var entries []Entry

	err = json.Unmarshal(body, &entries)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling response: %w", err)
	}

	return entries[0].Title, nil
}

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
