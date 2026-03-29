package recorder

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// StreamSource interface represents required methods that client should implement
//
//go:generate moq -out streamsource_moq_test.go . StreamSource
type StreamSource interface {
	FetchLatest(ctx context.Context) (string, error)
	FetchStream(ctx context.Context) (io.ReadCloser, error)
}

// Listener polls the SiteAPI for the current episode title, then opens the audio
// stream and returns both as a Stream ready for recording.
type Listener struct {
	client StreamSource
}

// NewListener creates new listener
func NewListener(c StreamSource) *Listener {
	return &Listener{
		client: c,
	}
}

// getStreamNumber extracts the episode number from the site API title and increments it.
// the API returns the last published episode, but we're always recording the next one.
func getStreamNumber(latest string) string {
	args := strings.Fields(latest)
	if len(args) < 2 { //nolint:mnd
		return "0"
	}
	num := args[1]
	if num == "" || num == "." || strings.Contains(num, "..") || strings.ContainsAny(num, "/\\") {
		return "0"
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return num
	}
	return strconv.Itoa(n + 1)
}

// Stream represents a stream
type Stream struct {
	Number string
	Body   io.ReadCloser
}

// NewStream creates new stream based on title and stream body
func NewStream(title string, body io.ReadCloser) *Stream {
	number := getStreamNumber(title)
	return &Stream{
		Number: number,
		Body:   body,
	}
}

// Listen fetches latest stream and returns it
func (l *Listener) Listen(ctx context.Context) (*Stream, error) {
	title, err := l.client.FetchLatest(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest: %w", err)
	}

	body, err := l.client.FetchStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stream: %w", err)
	}

	return NewStream(title, body), nil
}
