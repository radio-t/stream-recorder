package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPurge(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		retentionDays int
		createDir     bool // whether to create the recordings directory before setup
		setup         func(t *testing.T, dir string)
		wantRemoved   []string // relative paths that should be deleted
		wantKept      []string // relative paths that should remain
		wantErr       bool
	}{
		{
			name:          "zero retention days disables purge",
			retentionDays: 0,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "999/rt999_2025_01_01_00_00_00.mp3", now.AddDate(0, 0, -60))
			},
			wantKept: []string{"999/rt999_2025_01_01_00_00_00.mp3"},
		},
		{
			name:          "old files deleted, new files kept",
			retentionDays: 30,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "100/rt100_2026_02_08_12_00_00.mp3", now.AddDate(0, 0, -45))
				makeFile(t, dir, "200/rt200_2026_03_15_12_00_00.mp3", now.AddDate(0, 0, -10))
			},
			wantRemoved: []string{"100/rt100_2026_02_08_12_00_00.mp3"},
			wantKept:    []string{"200/rt200_2026_03_15_12_00_00.mp3"},
		},
		{
			name:          "empty episode directory removed after purge",
			retentionDays: 30,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "100/rt100_2026_02_13_12_00_00.mp3", now.AddDate(0, 0, -40))
				makeFile(t, dir, "100/rt100_2026_02_18_12_00_00.mp3", now.AddDate(0, 0, -35))
			},
			wantRemoved: []string{"100/rt100_2026_02_13_12_00_00.mp3", "100/rt100_2026_02_18_12_00_00.mp3", "100"},
		},
		{
			name:          "directory with mixed old and new files kept",
			retentionDays: 30,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "150/rt150_2026_02_13_12_00_00.mp3", now.AddDate(0, 0, -40))
				makeFile(t, dir, "150/rt150_2026_03_20_12_00_00.mp3", now.AddDate(0, 0, -5))
			},
			wantRemoved: []string{"150/rt150_2026_02_13_12_00_00.mp3"},
			wantKept:    []string{"150/rt150_2026_03_20_12_00_00.mp3", "150"},
		},
		{
			name:          "file exactly at retention boundary kept",
			retentionDays: 30,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "300/rt300_2026_02_23_12_00_00.mp3", now.AddDate(0, 0, -30))
			},
			wantKept: []string{"300/rt300_2026_02_23_12_00_00.mp3"},
		},
		{
			name:          "non-existent directory returns no error",
			retentionDays: 30,
			createDir:     false,
			setup:         func(_ *testing.T, _ string) {},
			wantErr:       false,
		},
		{
			name:          "negative retention days disables purge",
			retentionDays: -1,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "999/rt999_2025_01_01_00_00_00.mp3", now.AddDate(0, 0, -60))
			},
			wantKept: []string{"999/rt999_2025_01_01_00_00_00.mp3"},
		},
		{
			name:          "non-mp3 files are not deleted",
			retentionDays: 30,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "100/rt100_2026_02_08_12_00_00.mp3", now.AddDate(0, 0, -45))
				makeFile(t, dir, "100/notes.txt", now.AddDate(0, 0, -45))
				makeFile(t, dir, "100/config.json", now.AddDate(0, 0, -45))
			},
			wantRemoved: []string{"100/rt100_2026_02_08_12_00_00.mp3"},
			wantKept:    []string{"100/notes.txt", "100/config.json", "100"},
		},
		{
			name:          "old mp3 sharing the episode prefix not deleted",
			retentionDays: 30,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "100/rt100_notes.mp3", now.AddDate(0, 0, -60))
				makeFile(t, dir, "100/rt100_2026_02_30_12_00_00.mp3", now.AddDate(0, 0, -60))
				makeFile(t, dir, "100/rt100_2026_02_09_12_00_00.MP3", now.AddDate(0, 0, -60))
				makeFile(t, dir, "100/rt100_2026_02_08_12_00_00.mp3", now.AddDate(0, 0, -45))
			},
			wantRemoved: []string{"100/rt100_2026_02_08_12_00_00.mp3"},
			wantKept: []string{
				"100/rt100_notes.mp3",
				"100/rt100_2026_02_30_12_00_00.mp3",
				"100/rt100_2026_02_09_12_00_00.MP3",
				"100",
			},
		},
		{
			name:          "unrelated mp3 files in subdirectories not deleted",
			retentionDays: 30,
			createDir:     true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				makeFile(t, dir, "music/song.mp3", now.AddDate(0, 0, -60))
				makeFile(t, dir, "100/random.mp3", now.AddDate(0, 0, -60))
				makeFile(t, dir, "100/rt100_2026_02_08_12_00_00.mp3", now.AddDate(0, 0, -45))
			},
			wantRemoved: []string{"100/rt100_2026_02_08_12_00_00.mp3"},
			wantKept:    []string{"music/song.mp3", "100/random.mp3", "100"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			recDir := filepath.Join(dir, "recordings")

			if tt.createDir {
				require.NoError(t, os.MkdirAll(recDir, 0o750))
			}

			tt.setup(t, recDir)

			err := Purge(recDir, tt.retentionDays, now)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			for _, p := range tt.wantRemoved {
				_, err := os.Stat(filepath.Join(recDir, p))
				assert.True(t, os.IsNotExist(err), "expected %s to be removed", p)
			}
			for _, p := range tt.wantKept {
				_, err := os.Stat(filepath.Join(recDir, p))
				assert.NoError(t, err, "expected %s to be kept", p)
			}
		})
	}
}

// makeFile creates a file at the given relative path under dir with the specified modification time.
func makeFile(t *testing.T, dir, relPath string, modTime time.Time) {
	t.Helper()
	fullPath := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o750))
	require.NoError(t, os.WriteFile(fullPath, []byte("test data"), 0o600))
	require.NoError(t, os.Chtimes(fullPath, modTime, modTime))
}

func TestIsRecorderFile(t *testing.T) {
	t.Parallel()

	const prefix = "rt100_"

	tests := []struct {
		name string
		file string
		want bool
	}{
		{name: "canonical recording name", file: "rt100_2026_02_08_12_00_00.mp3", want: true},
		{name: "generated by RecordingFileName", file: RecordingFileName("100", time.Date(2026, 3, 28, 20, 0, 0, 0, time.UTC)), want: true},
		{name: "prefix shared with unrelated file", file: "rt100_notes.mp3", want: false},
		{name: "prefix only", file: "rt100_.mp3", want: false},
		{name: "no timestamp separator", file: "rt100_20260208120000.mp3", want: false},
		{name: "trailing suffix after timestamp", file: "rt100_2026_02_08_12_00_00_take2.mp3", want: false},
		{name: "uppercase extension", file: "rt100_2026_02_08_12_00_00.MP3", want: false},
		{name: "vbrfix temp file", file: ".rt100_2026_02_08_12_00_00.mp3.vbrfix.tmp", want: false},
		{name: "month out of range", file: "rt100_2026_13_08_12_00_00.mp3", want: false},
		{name: "day out of range", file: "rt100_2026_02_30_12_00_00.mp3", want: false},
		{name: "hour out of range", file: "rt100_2026_02_08_25_00_00.mp3", want: false},
		{name: "different episode", file: "rt1000_2026_02_08_12_00_00.mp3", want: false},
		{name: "unrelated mp3", file: "song.mp3", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isRecorderFile(tt.file, prefix))
		})
	}
}
