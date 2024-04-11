package recorder_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/radio-t/stream-recorder/app/recorder"
)

func TestRecorder(t *testing.T) {
	testingdir := "./test"
	ctx := context.Background()
	r := recorder.NewRecorder(testingdir)

	reader := strings.NewReader("asdf")
	b := io.NopCloser(reader)

	s := recorder.NewStream("rt testrecord", b)

	err := r.Record(ctx, s)
	if err != nil {
		t.Error(err)
	}

	if err = os.RemoveAll(testingdir); err != nil {
		t.Error(err)
	}
}
