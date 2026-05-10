package recorder

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixVBRHeader_NoFFmpeg(t *testing.T) {
	t.Setenv("PATH", "")

	dir := t.TempDir()
	filePath := filepath.Join(dir, "audio.mp3")
	original := []byte("untouched")
	require.NoError(t, os.WriteFile(filePath, original, 0o600))

	require.NoError(t, FixVBRHeader(filePath))

	got, err := os.ReadFile(filePath) //nolint:gosec
	require.NoError(t, err)
	assert.Equal(t, original, got, "file should be unchanged when ffmpeg is missing")
}

func TestFixVBRHeader_WithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "audio.mp3")
	require.NoError(t, generateTestMP3(filePath))

	require.NoError(t, FixVBRHeader(filePath))

	data, err := os.ReadFile(filePath) //nolint:gosec
	require.NoError(t, err)
	hasXing := bytes.Contains(data, []byte("Xing")) || bytes.Contains(data, []byte("Info"))
	assert.True(t, hasXing, "remuxed file should contain Xing/Info VBR header")
}

// generateTestMP3 produces a tiny silent MP3 via ffmpeg's lavfi anullsrc.
func generateTestMP3(path string) error {
	cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", //nolint:gosec,noctx // test helper, fixed args
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=stereo", "-t", "0.5",
		"-c:a", "libmp3lame", "-b:a", "128k", path)
	return cmd.Run()
}
