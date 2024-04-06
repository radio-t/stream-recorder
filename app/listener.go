package main

import (
	"context"
	"fmt"
	"io"
	"strings"
)

type Listener struct {
	client *Client
}

func NewListener(c *Client) *Listener {
	return &Listener{
		client: c,
	}
}

func getStreamNumber(latest string) string {
	args := strings.Split(latest, " ")
	if len(args) < 2 { //nolint:gomnd
		return "0"
	}
	return args[1]
}

type Stream struct {
	Number string
	Body   io.ReadCloser
}

func NewStream(title string, body io.ReadCloser) *Stream {
	number := getStreamNumber(title)
	return &Stream{
		Number: number,
		Body:   body,
	}
}

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
