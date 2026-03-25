package recorder

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// ClientService interface represents required methods that client should implement
//
//go:generate moq -out clientservice_moq_test.go . ClientService
type ClientService interface {
	FetchLatest(ctx context.Context) (string, error)
	FetchStream(ctx context.Context) (io.ReadCloser, error)
}

// Listener polls the SiteAPI for the current episode title, then opens the audio
// stream and returns both as a Stream ready for recording.
type Listener struct {
	client ClientService
}

// NewListener creates new listener
func NewListener(c ClientService) *Listener {
	return &Listener{
		client: c,
	}
}

func getStreamNumber(latest string) string {
	args := strings.Fields(latest)
	if len(args) < 2 { //nolint:mnd
		return "0"
	}
	num := args[1]
	if num == "" || num == "." || strings.Contains(num, "..") || strings.ContainsAny(num, "/\\") {
		return "0"
	}
	return num
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
