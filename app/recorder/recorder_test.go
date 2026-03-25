package recorder_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radio-t/stream-recorder/app/recorder"
)

func TestRecorder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	r := recorder.NewRecorder(dir)

	reader := strings.NewReader("some audio data")
	s := recorder.NewStream("rt testrecord", io.NopCloser(reader))

	err := r.Record(ctx, s)
	require.NoError(t, err)

	// verify the episode directory and file were created
	entries, err := os.ReadDir(filepath.Join(dir, "testrecord"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.True(t, strings.HasPrefix(entries[0].Name(), "rttestrecord_"))
	assert.True(t, strings.HasSuffix(entries[0].Name(), ".mp3"))

	// verify file content matches what was written
	data, err := os.ReadFile(filepath.Join(dir, "testrecord", entries[0].Name())) //nolint:gosec
	require.NoError(t, err)
	assert.Equal(t, "some audio data", string(data))
}

// slowReader blocks on reads until data is sent through a channel or context is cancelled.
// this simulates a live stream that produces data slowly.
type slowReader struct {
	ch        chan []byte
	done      chan struct{}
	mu        sync.Mutex
	closeOnce sync.Once
	buf       []byte
}

func newSlowReader() *slowReader {
	return &slowReader{
		ch:   make(chan []byte, 10),
		done: make(chan struct{}),
	}
}

func (r *slowReader) Read(p []byte) (int, error) {
	// drain buffered data first
	r.mu.Lock()
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		r.mu.Unlock()
		return n, nil
	}
	r.mu.Unlock()

	// wait for more data or close signal
	select {
	case data, ok := <-r.ch:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, data)
		if n < len(data) {
			r.mu.Lock()
			r.buf = append(r.buf, data[n:]...)
			r.mu.Unlock()
		}
		return n, nil
	case <-r.done:
		return 0, io.EOF
	}
}

func (r *slowReader) Close() error {
	r.closeOnce.Do(func() { close(r.done) })
	return nil
}

func (r *slowReader) send(data []byte) {
	r.ch <- data
}

func TestRecorderContextCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := recorder.NewRecorder(dir)

	sr := newSlowReader()
	s := recorder.NewStream("rt 999", sr)

	ctx, cancel := context.WithCancel(context.Background())

	// send some initial data
	sr.send([]byte("chunk1"))

	errCh := make(chan error, 1)
	go func() {
		errCh <- r.Record(ctx, s)
	}()

	// give Record time to read the first chunk
	time.Sleep(50 * time.Millisecond)

	// cancel context — Record should stop promptly
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Record did not stop within 2 seconds after context cancellation")
	}
}

func TestRecorderContextAlreadyCancelled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := recorder.NewRecorder(dir)

	sr := newSlowReader()
	s := recorder.NewStream("rt 888", sr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Record starts

	err := r.Record(ctx, s)
	require.ErrorIs(t, err, context.Canceled)

	// verify no episode directory or zero-byte file was created
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created when context is already cancelled")
}
