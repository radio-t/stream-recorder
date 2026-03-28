package chapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPClient interface to make HTTP requests
//
//go:generate moq -out httpclient_moq_test.go . HTTPClient
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Article represents a news article from the Radio-T news API.
type Article struct {
	Title    string    `json:"title"`
	Link     string    `json:"link"`
	ActiveTS time.Time `json:"activets"`
}

// NewsClient fetches news data from the Radio-T news API.
type NewsClient struct {
	client  HTTPClient
	baseURL string
}

// NewNewsClient creates a new news API client with the given base URL.
// trailing slashes are stripped to prevent double-slash URLs.
func NewNewsClient(c HTTPClient, baseURL string) *NewsClient {
	return &NewsClient{client: c, baseURL: strings.TrimRight(baseURL, "/")}
}

// FetchActiveID returns the ID of the currently active news topic.
func (nc *NewsClient) FetchActiveID(ctx context.Context) (string, error) {
	return nc.fetchID(ctx, nc.baseURL+"/news/active/id")
}

// WaitActiveChange long-polls until the active topic changes or the timeout elapses.
// It returns the new active topic ID.
func (nc *NewsClient) WaitActiveChange(ctx context.Context, timeout time.Duration) (string, error) {
	ms := int64(timeout / time.Millisecond)
	return nc.fetchID(ctx, fmt.Sprintf("%s/news/active/wait/%d", nc.baseURL, ms))
}

// FetchArticle fetches the full article details for the given article ID.
func (nc *NewsClient) FetchArticle(ctx context.Context, id string) (Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nc.baseURL+"/news/id/"+url.PathEscape(id), http.NoBody)
	if err != nil {
		return Article{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "stream-recorder")

	resp, err := nc.client.Do(req)
	if err != nil {
		return Article{}, fmt.Errorf("fetch article %s: %w", id, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return Article{}, fmt.Errorf("news API returned status %d for article %s", resp.StatusCode, id)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Article{}, fmt.Errorf("read article response: %w", err)
	}

	var article Article
	if err := json.Unmarshal(body, &article); err != nil {
		return Article{}, fmt.Errorf("unmarshal article: %w", err)
	}
	return article, nil
}

// fetchID is a helper that GETs the given URL and decodes a JSON response with an "id" field.
func (nc *NewsClient) fetchID(ctx context.Context, reqURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "stream-recorder")

	resp, err := nc.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch active ID: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("news API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	return result.ID, nil
}
