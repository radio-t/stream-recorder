package recorder

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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

func TestFixVBRHeaderContext_Cancellation(t *testing.T) {
	binDir := t.TempDir()
	// a stand-in ffmpeg that never finishes, so only cancellation can end the call
	// absolute path: PATH is replaced below, so a bare sleep would not be found
	script := "#!/bin/sh\nexec /bin/sleep 60\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "ffmpeg"), []byte(script), 0o700)) //nolint:gosec // must be executable
	t.Setenv("PATH", binDir)

	dir := t.TempDir()
	filePath := filepath.Join(dir, "audio.mp3")
	original := []byte("untouched")
	require.NoError(t, os.WriteFile(filePath, original, 0o600))

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := FixVBRHeaderContext(ctx, filePath)

	elapsed := time.Since(start)
	require.Error(t, err, "a cancelled remux should report the failure")
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond, "the call should have waited for the deadline")
	assert.Less(t, elapsed, 30*time.Second, "cancellation should kill ffmpeg rather than wait for it")
	require.Error(t, ctx.Err(), "the context should be the reason the remux ended")

	got, readErr := os.ReadFile(filePath) //nolint:gosec // test file
	require.NoError(t, readErr)
	assert.Equal(t, original, got, "the original file must survive a cancelled remux")

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 1, "the temporary output should be removed")
}
