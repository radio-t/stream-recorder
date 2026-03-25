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
	dir string
}

// NewRecorder creates a new recorder
func NewRecorder(dir string) *Recorder {
	return &Recorder{
		dir: dir,
	}
}

func (r *Recorder) prepareFile(episode string) (*os.File, error) {
	fileDir := path.Join(r.dir, episode)

	if err := os.MkdirAll(fileDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s directory: %w", fileDir, err)
	}

	fileName := "rt" + episode + "_" + time.Now().Format("2006_01_02_15_04_05") + ".mp3"
	filePath := path.Join(fileDir, fileName)

	f, err := os.Create(filePath) //nolint: gosec
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}

	return f, nil
}

// Record records a stream to a file, stopping when context is cancelled
func (r *Recorder) Record(ctx context.Context, s *Stream) error {
	f, err := r.prepareFile(s.Number)
	if err != nil {
		return err
	}
	defer f.Close() //nolint: errcheck

	buf := make([]byte, buffer)

	var closeOnce sync.Once
	closeBody := func() { closeOnce.Do(func() { s.Body.Close() }) } //nolint: errcheck,gosec

	defer closeBody()

	// close stream body when context is cancelled to unblock a pending Read
	go func() {
		<-ctx.Done()
		closeBody()
	}()

	slog.Info(fmt.Sprintf("started recording %s at %v", s.Number, time.Now().Format(time.RFC3339)))
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := s.Body.Read(buf)

		if errors.Is(err, io.EOF) {
			// body may have been closed due to context cancellation
			if ctx.Err() != nil {
				return ctx.Err()
			}
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("failed to read from stream: %w", err)
		}

		_, err = f.Write(buf[:n])
		if err != nil {
			return fmt.Errorf("failed to write to file: %w", err)
		}
	}
	slog.Info(fmt.Sprintf("finished recording at %v", time.Now().Format(time.RFC3339)))

	return nil
}
