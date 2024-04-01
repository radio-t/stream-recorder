package main

import (
	"context"
	"io"
	"os"
	"testing"
)

type body struct {
	c int
}

func (b *body) Read(p []byte) (int, error) { //nolint:staticcheck
	data := []byte("asdf")

	if b.c < 10 {
		p = data //nolint:ineffassign,staticcheck,wastedassign
	} else {
		return 0, io.EOF
	}

	b.c += 1
	return len(data), nil
}

func (b *body) Close() error {
	return nil
}

func TestRecorder(t *testing.T) {
	testingdir := "./test"
	ctx := context.Background()
	r := NewRecorder(testingdir)
	b := &body{}

	s := NewStream("rt testrecord", b)

	err := r.Record(ctx, s)
	if err != nil {
		t.Error(err)
	}

	if err = os.RemoveAll(testingdir); err != nil {
		t.Error(err)
	}
}
