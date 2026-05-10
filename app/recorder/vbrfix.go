package recorder

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	bin, err := exec.LookPath(ffmpegBin)
	if err != nil {
		slog.Info("ffmpeg not found, skipping VBR header fix", slog.String("file", filePath))
		return nil
	}

	tmpPath := filepath.Join(filepath.Dir(filePath), "."+filepath.Base(filePath)+".vbrfix.tmp")
	cmd := exec.Command(bin, "-y", "-loglevel", "error", "-i", filePath, "-c", "copy", "-f", "mp3", tmpPath) //nolint:gosec,noctx // filePath is server-controlled; runs once at end of recording
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpPath) //nolint:errcheck,gosec
		return fmt.Errorf("ffmpeg remux failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return os.Rename(tmpPath, filePath)
}
