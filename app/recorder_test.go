package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRecorder(t *testing.T) {
	testingdir := "./test"
	ctx := context.Background()
	r := NewRecorder(testingdir)

	reader := strings.NewReader("asdf")
	b := io.NopCloser(reader)

	s := NewStream("rt testrecord", b)

	err := r.Record(ctx, s)
	if err != nil {
		t.Error(err)
	}

	if err = os.RemoveAll(testingdir); err != nil {
		t.Error(err)
	}
}
