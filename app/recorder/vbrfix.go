package recorder

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const ffmpegBin = "ffmpeg"

// FixVBRHeader remuxes the file with ffmpeg -c copy to add a Xing/Info VBR header
// to the first audio frame. Without that header, players estimate duration from
// the first frame's bitrate, which is wrong for variable-bitrate streams and
// makes a 3h recording appear as 8h+ in Apple/Safari.
//
// Skips silently when ffmpeg is not on PATH so the binary still works in
// environments without the dependency.
func FixVBRHeader(filePath string) error {
	return FixVBRHeaderContext(context.Background(), filePath)
}

// FixVBRHeaderContext is FixVBRHeader bounded by ctx: cancelling it kills ffmpeg and removes
// the temporary output, leaving the original file untouched.
func FixVBRHeaderContext(ctx context.Context, filePath string) error {
	bin, err := exec.LookPath(ffmpegBin)
	if err != nil {
		slog.Info("ffmpeg not found, skipping VBR header fix", slog.String("file", filePath))
		return nil
	}

	tmpPath := filepath.Join(filepath.Dir(filePath), "."+filepath.Base(filePath)+".vbrfix.tmp")
	cmd := exec.CommandContext(ctx, bin, "-y", "-loglevel", "error", "-i", filePath, "-c", "copy", "-f", "mp3", tmpPath) //nolint:gosec // filePath is server-controlled; runs once at end of recording
	// give up on the output pipes shortly after the kill, so a surviving descendant holding
	// them open cannot keep the call blocked
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpPath) //nolint:errcheck,gosec
		return fmt.Errorf("ffmpeg remux failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return os.Rename(tmpPath, filePath)
}
