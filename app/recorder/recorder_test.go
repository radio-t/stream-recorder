package recorder_test

import (
	"context"
	"errors"
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
	r := recorder.NewRecorder(dir, nil)

	reader := strings.NewReader("some audio data")
	s := recorder.NewStream("rt testrecord", io.NopCloser(reader))

	filePath, err := r.Record(ctx, s)
	require.NoError(t, err)
	assert.NotEmpty(t, filePath)

	// verify the episode directory and file were created
	entries, err := os.ReadDir(filepath.Join(dir, "testrecord"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.True(t, strings.HasPrefix(entries[0].Name(), "rttestrecord_"))
	assert.True(t, strings.HasSuffix(entries[0].Name(), ".mp3"))

	// verify file starts with ID3 header and contains the audio data
	data, err := os.ReadFile(filepath.Join(dir, "testrecord", entries[0].Name())) //nolint:gosec
	require.NoError(t, err)
	assert.Equal(t, "ID3", string(data[:3]), "file should start with ID3 header")
	assert.Contains(t, string(data), "Radio-T testrecord", "ID3 header should contain episode title")
	assert.True(t, strings.HasSuffix(string(data), "some audio data"), "file should end with audio data")
}

// slowReader blocks on reads until data is sent through a channel or context is cancelled.
// this simulates a live stream that produces data slowly.
type slowReader struct {
	ch        chan []byte
	done      chan struct{}
	reading   chan struct{} // closed when the first Read starts
	delivered chan struct{} // closed once a Read has handed over data
	mu        sync.Mutex
	closeOnce sync.Once
	readOnce  sync.Once
	dataOnce  sync.Once
	buf       []byte
}

func newSlowReader() *slowReader {
	return &slowReader{
		ch:        make(chan []byte, 10),
		done:      make(chan struct{}),
		reading:   make(chan struct{}),
		delivered: make(chan struct{}),
	}
}

func (r *slowReader) Read(p []byte) (int, error) {
	r.readOnce.Do(func() { close(r.reading) })

	// drain buffered data first
	r.mu.Lock()
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		r.mu.Unlock()
		r.dataOnce.Do(func() { close(r.delivered) })
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
		r.dataOnce.Do(func() { close(r.delivered) })
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
	r := recorder.NewRecorder(dir, nil)

	sr := newSlowReader()
	s := recorder.NewStream("rt 998", sr)

	ctx, cancel := context.WithCancel(context.Background())

	// send some initial data
	sr.send([]byte("chunk1"))

	type result struct {
		filePath string
		err      error
	}
	resCh := make(chan result, 1)
	go func() {
		fp, err := r.Record(ctx, s)
		resCh <- result{fp, err}
	}()

	// wait until the chunk has actually been handed over: once Read returns it, streamToFile
	// writes it before checking the context again, so the file is guaranteed to hold audio
	select {
	case <-sr.delivered:
	case <-time.After(2 * time.Second):
		t.Fatal("recorder did not read the first chunk")
	}

	// cancel context — Record should stop promptly
	cancel()

	select {
	case res := <-resCh:
		require.ErrorIs(t, res.err, context.Canceled)
		assert.NotEmpty(t, res.filePath, "file path should be returned on context cancellation")
		assert.FileExists(t, res.filePath, "recorded file should exist on disk")
	case <-time.After(2 * time.Second):
		t.Fatal("Record did not stop within 2 seconds after context cancellation")
	}
}

func TestRecorderCancellationBeforeAudioRemovesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := recorder.NewRecorder(dir, nil)

	sr := newSlowReader() // never sends anything, so the first read blocks until cancellation
	s := recorder.NewStream("rt testrecord", sr)

	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		filePath string
		err      error
	}
	resCh := make(chan result, 1)
	go func() {
		fp, err := r.Record(ctx, s)
		resCh <- result{fp, err}
	}()

	// the first read only happens after the ID3 header is written, so waiting for it puts the
	// cancellation squarely in the window this test is about: file created, header written,
	// not one audio byte yet
	episodeDir := filepath.Join(dir, "testrecord")
	select {
	case <-sr.reading:
	case <-time.After(2 * time.Second):
		t.Fatal("recorder did not reach its first read")
	}
	cancel()

	select {
	case res := <-resCh:
		require.ErrorIs(t, res.err, context.Canceled)
		assert.Empty(t, res.filePath, "a header-only file is not a recording and must not be reported")
	case <-time.After(2 * time.Second):
		t.Fatal("Record did not stop within 2 seconds after context cancellation")
	}

	_, statErr := os.Stat(episodeDir)
	assert.True(t, os.IsNotExist(statErr), "the header-only file and its episode directory should be removed")
}

// failingReader returns the given data on the first read and then always fails, simulating a
// stream that drops mid-recording.
type failingReader struct {
	data []byte
	err  error
	done bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, r.err
	}
	r.done = true
	n := copy(p, r.data)
	return n, nil
}

func (r *failingReader) Close() error { return nil }

func TestRecorderStreamFailureKeepsRecordedAudio(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sentinel := errors.New("connection reset")

	r := recorder.NewRecorder(dir, nil)
	s := recorder.NewStream("rt testrecord", &failingReader{data: []byte("some audio data"), err: sentinel})

	filePath, err := r.Record(context.Background(), s)

	require.ErrorIs(t, err, sentinel)
	require.NotEmpty(t, filePath, "a recording holding audio should be reported to the caller")

	data, readErr := os.ReadFile(filePath) //nolint:gosec // path from the recorder
	require.NoError(t, readErr)
	assert.Equal(t, "ID3", string(data[:3]), "file should start with ID3 header")
	assert.True(t, strings.HasSuffix(string(data), "some audio data"), "captured audio should be kept")
}

func TestRecorderStreamFailureBeforeAudioRemovesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sentinel := errors.New("connection reset")

	r := recorder.NewRecorder(dir, nil)
	s := recorder.NewStream("rt testrecord", &failingReader{data: nil, err: sentinel, done: true})

	filePath, err := r.Record(context.Background(), s)

	require.ErrorIs(t, err, sentinel)
	assert.Empty(t, filePath, "a recording without audio should not be reported as a file")

	_, statErr := os.Stat(filepath.Join(dir, "testrecord"))
	assert.True(t, os.IsNotExist(statErr), "empty episode directory should be removed")
}

func TestRecordingFileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		episode string
		ts      time.Time
		want    string
	}{
		{
			name:    "standard episode number",
			episode: "999",
			ts:      time.Date(2026, 3, 25, 14, 30, 45, 0, time.UTC),
			want:    "rt999_2026_03_25_14_30_45.mp3",
		},
		{
			name:    "different episode",
			episode: "100",
			ts:      time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
			want:    "rt100_2025_01_02_03_04_05.mp3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, recorder.RecordingFileName(tt.episode, tt.ts))
		})
	}
}

func TestRecordingFilePrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		episode string
		want    string
	}{
		{name: "numeric episode", episode: "999", want: "rt999_"},
		{name: "another episode", episode: "100", want: "rt100_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, recorder.RecordingFilePrefix(tt.episode))
		})
	}
}

func TestRecordingFileName_ConsistentWithPrefix(t *testing.T) {
	t.Parallel()
	episode := "777"
	ts := time.Date(2026, 6, 15, 10, 20, 30, 0, time.UTC)
	fileName := recorder.RecordingFileName(episode, ts)
	prefix := recorder.RecordingFilePrefix(episode)
	assert.True(t, strings.HasPrefix(fileName, prefix),
		"RecordingFileName %q should start with RecordingFilePrefix %q", fileName, prefix)
	assert.True(t, strings.HasSuffix(fileName, ".mp3"))
}

func TestRecorderContextAlreadyCancelled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := recorder.NewRecorder(dir, nil)

	sr := newSlowReader()
	s := recorder.NewStream("rt 887", sr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Record starts

	_, err := r.Record(ctx, s)
	require.ErrorIs(t, err, context.Canceled)

	// verify no episode directory or zero-byte file was created
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "no files should be created when context is already cancelled")
}
