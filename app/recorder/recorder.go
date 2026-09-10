package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const buffer = 32 * 1024 // 32KB read buffer

// Recorder writes a Stream's audio body to disk, creating one MP3 file per session
// inside a per-episode subdirectory.
type Recorder struct {
	dir     string
	onReady func() // called after the output file is created, before streaming begins
}

// NewRecorder creates a new recorder. onReady, when non-nil, is called after the
// output file is created but before streaming begins.
func NewRecorder(dir string, onReady func()) *Recorder {
	return &Recorder{
		dir:     dir,
		onReady: onReady,
	}
}

// recordingTimeLayout is the timestamp layout used in recording file names.
const recordingTimeLayout = "2006_01_02_15_04_05"

// recordingExt is the extension used for recording files.
const recordingExt = ".mp3"

// RecordingFileName returns the full filename for a recording of the given episode at time t.
func RecordingFileName(episode string, t time.Time) string {
	return RecordingFilePrefix(episode) + t.Format(recordingTimeLayout) + recordingExt
}

// RecordingFilePrefix returns the filename prefix shared by all recordings of the given episode.
func RecordingFilePrefix(episode string) string {
	return "rt" + episode + "_"
}

func (r *Recorder) prepareFile(episode string) (*os.File, error) {
	fileDir := filepath.Join(r.dir, episode)

	if err := os.MkdirAll(fileDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s directory: %w", fileDir, err)
	}

	fileName := RecordingFileName(episode, time.Now())
	filePath := filepath.Join(fileDir, fileName)

	f, err := os.Create(filePath) //nolint: gosec
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	return f, nil
}

// Record records a stream to a file, stopping when context is cancelled.
// returns the file path of the recorded file on success. a session that captured audio returns
// its path alongside any error, so the caller can log and finalise it. a session that captured
// none is removed and only the error is returned, cancellation included: a file holding just an
// ID3 header is not a recording.
func (r *Recorder) Record(ctx context.Context, s *Stream) (string, error) {
	var closeOnce sync.Once
	closeBody := func() { closeOnce.Do(func() { s.Body.Close() }) } //nolint: errcheck,gosec
	defer closeBody()

	// check context before creating any files on disk
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	f, err := r.prepareFile(s.Number)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint: errcheck

	if r.onReady != nil {
		r.onReady()
	}

	// if context was cancelled between the check above and file creation, clean up the empty file
	if ctx.Err() != nil {
		discardFile(f)
		return "", ctx.Err()
	}

	// close stream body when context is cancelled to unblock a pending Read.
	// the done channel ensures the goroutine exits when Record returns normally (EOF).
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			closeBody()
		case <-done:
		}
	}()

	if err := WriteID3v2Header(f, s.Number, time.Now()); err != nil {
		discardFile(f)
		return "", fmt.Errorf("failed to write ID3 header: %w", err)
	}

	slog.Info(fmt.Sprintf("started recording %s at %v", s.Number, time.Now().Format(time.RFC3339)))
	audioWritten, err := streamToFile(ctx, f, s.Body)
	return finishRecording(f, audioWritten, err)
}

// finishRecording decides what a finished session hands back. a file holding audio is reported
// with whatever error ended it, so the caller can log and finalise it. anything else is removed:
// whether the stream ended straight away, something failed, or a cancellation landed before the
// first audio byte, a file holding just an ID3 header is not a recording.
func finishRecording(f *os.File, audioWritten bool, err error) (string, error) {
	if audioWritten {
		return f.Name(), err
	}

	discardFile(f)
	if err == nil {
		return "", fmt.Errorf("stream ended without any audio")
	}
	return "", err
}

// streamToFile copies the stream body into f until it ends or fails, reporting whether any
// audio made it to disk.
func streamToFile(ctx context.Context, f *os.File, body io.Reader) (audioWritten bool, err error) {
	buf := make([]byte, buffer)
	for {
		select {
		case <-ctx.Done():
			return audioWritten, ctx.Err()
		default:
		}

		n, readErr := body.Read(buf)

		// per io.Reader contract, always process n > 0 bytes before considering the error
		if n > 0 {
			// a short write reports the bytes it did store, which still count as audio on disk
			written, writeErr := f.Write(buf[:n])
			audioWritten = audioWritten || written > 0
			if writeErr != nil {
				return audioWritten, fmt.Errorf("failed to write to file: %w", writeErr)
			}
		}

		if readErr != nil {
			// body may have been closed due to context cancellation
			if ctx.Err() != nil {
				return audioWritten, ctx.Err()
			}
			if errors.Is(readErr, io.EOF) {
				return audioWritten, nil
			}
			return audioWritten, fmt.Errorf("failed to read from stream: %w", readErr)
		}
	}
}

// discardFile closes and removes a recording that holds no audio, so a failed session leaves
// nothing behind for the server to list and offer for download.
// a removal that fails is logged, since the leftover file stays visible in the web UI.
func discardFile(f *os.File) {
	name := f.Name()
	f.Close() //nolint:errcheck,gosec
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to remove empty recording",
			slog.String("file", name), slog.String("err", err.Error()))
	}
	os.Remove(filepath.Dir(name)) //nolint:errcheck,gosec // removes dir only if empty
}
