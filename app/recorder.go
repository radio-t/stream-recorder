package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"time"
)

const buffer = 128

// Recorder records a
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

	_, err := os.Stat(fileDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(fileDir, os.ModePerm)
	}
	if err != nil {
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

// Record records a stream to a file
func (r *Recorder) Record(_ context.Context, s *Stream) error {
	f, err := r.prepareFile(s.Number)
	if err != nil {
		return err
	}
	defer f.Close() //nolint: errcheck

	buf := make([]byte, buffer)

	defer s.Body.Close() //nolint: errcheck

	slog.Info(fmt.Sprintf("started recording %s at %v", s.Number, time.Now().Format(time.RFC3339)))
	for {
		n, err := s.Body.Read(buf)

		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
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
