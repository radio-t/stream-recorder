package recorder

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Purge removes recording files older than retentionDays from dir and cleans up empty
// episode directories. A retentionDays of 0 or negative disables purging. The now parameter
// controls the reference time for age calculations.
func Purge(dir string, retentionDays int, now time.Time) error {
	if retentionDays <= 0 {
		return nil
	}

	cutoff := now.AddDate(0, 0, -retentionDays)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read recordings directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		episodeDir := filepath.Join(dir, entry.Name())
		if err := purgeEpisodeDir(episodeDir, cutoff); err != nil {
			slog.Warn("purge error", slog.String("dir", episodeDir), slog.String("err", err.Error()))
		}
	}

	return nil
}

// purgeEpisodeDir removes recorder-managed files older than cutoff and deletes the directory if empty.
// only files matching the recorder naming pattern (rt<episode>_*.mp3) are considered for deletion,
// ensuring unrelated content in the same directory tree is never touched.
func purgeEpisodeDir(dir string, cutoff time.Time) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read episode directory %s: %w", dir, err)
	}

	prefix := "rt" + filepath.Base(dir) + "_"
	for _, f := range files {
		if f.IsDir() || !isRecorderFile(f.Name(), prefix) {
			continue
		}
		purgeFile(dir, f, cutoff)
	}

	// remove episode directory if empty
	remaining, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to re-read episode directory %s: %w", dir, err)
	}
	if len(remaining) == 0 {
		if err := os.Remove(dir); err != nil {
			return fmt.Errorf("failed to remove empty directory %s: %w", dir, err)
		}
		slog.Info("removed empty episode directory", slog.String("dir", dir))
	}

	return nil
}

// purgeFile removes a single file if its modification time is before the cutoff.
func purgeFile(dir string, f os.DirEntry, cutoff time.Time) {
	info, err := f.Info()
	if err != nil {
		slog.Warn("failed to stat file", slog.String("file", f.Name()), slog.String("err", err.Error()))
		return
	}
	if !info.ModTime().Before(cutoff) {
		return
	}
	path := filepath.Join(dir, f.Name())
	if err := os.Remove(path); err != nil {
		slog.Warn("failed to remove file", slog.String("file", path), slog.String("err", err.Error()))
		return
	}
	slog.Info("purged old recording", slog.String("file", path))
}

// isRecorderFile checks that a filename matches the recorder naming pattern: rt<episode>_<timestamp>.mp3
func isRecorderFile(name, prefix string) bool {
	return strings.HasPrefix(name, prefix) && strings.HasSuffix(strings.ToLower(name), ".mp3")
}
