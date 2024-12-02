package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"time"
)

// Entry API сайта radio-t.com https://radio-t.com/api-docs/
type Entry struct {
	URL        string      `json:"url"`                   // url поста
	Title      string      `json:"title"`                 // заголовок поста
	Date       time.Time   `json:"date"`                  // дата-время поста в RFC3339
	Categories []string    `json:"categories"`            // список категорий, массив строк
	Image      string      `json:"image,omitempty"`       // url картинки
	FileName   string      `json:"file_name,omitempty"`   // имя файла
	Body       string      `json:"body,omitempty"`        // тело поста в HTML
	ShowNotes  string      `json:"show_notes,omitempty"`  // пост в текстовом виде
	AudioURL   string      `json:"audio_url,omitempty"`   // url аудио файла
	TimeLabels []TimeLabel `json:"time_labels,omitempty"` // массив временых меток тем
}

// TimeLabel API сайта radio-t.com https://radio-t.com/api-docs/
type TimeLabel struct {
	Topic    string    `json:"topic"`              // название темы
	Time     time.Time `json:"time"`               // время начала в RFC3339
	Duration int       `json:"duration,omitempty"` // длительность в секундах
}

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

// Record records a stream to a file
func (r *Recorder) Record(_ context.Context, s *Stream) error {
	fileDir := path.Join(r.dir, s.Number)

	err := os.MkdirAll(fileDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create %s directory: %w", fileDir, err)
	}

	fileName := "rt" + s.Number + "_" + time.Now().Format("2006_01_02_15_04_05") + ".mp3"
	filePath := path.Join(fileDir, fileName)

	f, err := os.Create(filepath.Clean(filePath))
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	buf := make([]byte, buffer)

	defer s.Body.Close()

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
