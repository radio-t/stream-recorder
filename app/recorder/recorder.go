package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
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

// RecordingFileName returns the full filename for a recording of the given episode at time t.
func RecordingFileName(episode string, t time.Time) string {
	return RecordingFilePrefix(episode) + t.Format("2006_01_02_15_04_05") + ".mp3"
}

// RecordingFilePrefix returns the filename prefix shared by all recordings of the given episode.
func RecordingFilePrefix(episode string) string {
	return "rt" + episode + "_"
}

func (r *Recorder) prepareFile(episode string) (*os.File, error) {
	fileDir := path.Join(r.dir, episode)

	if err := os.MkdirAll(fileDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s directory: %w", fileDir, err)
	}

	fileName := RecordingFileName(episode, time.Now())
	filePath := path.Join(fileDir, fileName)

	f, err := os.Create(filePath) //nolint: gosec
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	return f, nil
}

// Record records a stream to a file, stopping when context is cancelled.
// returns the file path of the recorded file on success.
func (r *Recorder) Record(ctx context.Context, s *Stream) (string, error) { //nolint:gocyclo,funlen // complexity is inherent to correct io.Reader + context handling
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
		os.Remove(f.Name())           //nolint: errcheck,gosec // best-effort cleanup
		os.Remove(path.Dir(f.Name())) //nolint: errcheck,gosec // removes dir only if empty
		return "", ctx.Err()
	}

	buf := make([]byte, buffer)

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
		return "", fmt.Errorf("failed to write ID3 header: %w", err)
	}

	slog.Info(fmt.Sprintf("started recording %s at %v", s.Number, time.Now().Format(time.RFC3339)))
	for {
		select {
		case <-ctx.Done():
			return f.Name(), ctx.Err()
		default:
		}

		n, readErr := s.Body.Read(buf)

		// per io.Reader contract, always process n > 0 bytes before considering the error
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return "", fmt.Errorf("failed to write to file: %w", writeErr)
			}
		}

		if errors.Is(readErr, io.EOF) {
			// body may have been closed due to context cancellation
			if ctx.Err() != nil {
				return f.Name(), ctx.Err()
			}
			break
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return f.Name(), ctx.Err()
			}
			return "", fmt.Errorf("failed to read from stream: %w", readErr)
		}
	}
	slog.Info(fmt.Sprintf("finished recording at %v", time.Now().Format(time.RFC3339)))

	return f.Name(), nil
}
