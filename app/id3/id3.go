// Package id3 provides ID3v2.4 frame building and injection primitives.
package id3

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// TextFrame builds an ID3v2.4 text frame (UTF-8 encoding).
func TextFrame(id, text string) []byte {
	data := append([]byte{3}, []byte(text)...) // 0x03 = UTF-8 encoding
	frame := make([]byte, 10+len(data))
	copy(frame[0:4], id)
	PutSyncsafe(frame[4:8], len(data))
	// frame[8:10] = flags, left as 0x0000
	copy(frame[10:], data)
	return frame
}

// WriteHeader writes a complete ID3v2.4 header wrapping the given frames.
func WriteHeader(w io.Writer, frames []byte) error {
	header := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}
	PutSyncsafe(header[6:10], len(frames))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(frames)
	return err
}

// InjectFrames appends extra frames into an existing MP3 file's ID3v2 header.
// uses a single-pass copy to a temp file, then atomic rename to replace the original.
func InjectFrames(filePath string, extraFrames []byte) error {
	srcInfo, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}

	src, err := os.Open(filePath) //nolint:gosec // caller provides path
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer src.Close() //nolint:errcheck

	header := make([]byte, 10)
	if _, err := io.ReadFull(src, header); err != nil {
		return fmt.Errorf("read ID3 header: %w", err)
	}
	if string(header[0:3]) != "ID3" {
		return fmt.Errorf("not an ID3v2 file")
	}
	oldSize := ReadSyncsafe(header[6:10])
	PutSyncsafe(header[6:10], oldSize+len(extraFrames))

	tmpPath, err := rewriteFile(filepath.Dir(filePath), header, src, int64(oldSize), extraFrames)
	if err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, srcInfo.Mode().Perm()); err != nil {
		os.Remove(tmpPath) //nolint:errcheck,gosec
		return fmt.Errorf("set permissions: %w", err)
	}
	return os.Rename(tmpPath, filePath)
}

// rewriteFile creates a temp file with: updated header + existing frames + extra frames + audio.
func rewriteFile(dir string, header []byte, src io.Reader, frameSize int64, extra []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, "id3-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		tmp.Close() //nolint:errcheck,gosec
		if !ok {
			os.Remove(tmpPath) //nolint:errcheck,gosec
		}
	}()

	if _, err := tmp.Write(header); err != nil {
		return "", fmt.Errorf("write header: %w", err)
	}
	if _, err := io.CopyN(tmp, src, frameSize); err != nil {
		return "", fmt.Errorf("copy existing frames: %w", err)
	}
	if _, err := tmp.Write(extra); err != nil {
		return "", fmt.Errorf("write extra frames: %w", err)
	}
	if _, err := io.Copy(tmp, src); err != nil {
		return "", fmt.Errorf("copy audio data: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}
	ok = true
	return tmpPath, nil
}

// ReadTLEN reads the TLEN (track length) value from an MP3 file's ID3v2 header.
// returns the duration in seconds, or 0 if TLEN is not found or the file is not valid ID3v2.
func ReadTLEN(filePath string) int64 {
	f, err := os.Open(filePath) //nolint:gosec // caller provides path
	if err != nil {
		return 0
	}
	defer f.Close() //nolint:errcheck

	header := make([]byte, 10)
	if _, err := io.ReadFull(f, header); err != nil || string(header[0:3]) != "ID3" {
		return 0
	}

	tagSize := ReadSyncsafe(header[6:10])
	const maxTagSize = 1 << 20 // 1MB cap to avoid excessive allocation on corrupt files
	if tagSize <= 0 || tagSize > maxTagSize {
		return 0
	}

	buf := make([]byte, tagSize)
	if _, err := io.ReadFull(f, buf); err != nil {
		return 0
	}
	return findTLEN(buf)
}

// findTLEN scans ID3 tag frame data for TLEN and returns duration in seconds.
func findTLEN(buf []byte) int64 {
	for len(buf) >= 10 {
		frameID := string(buf[0:4])
		sz := ReadSyncsafe(buf[4:8])
		if sz == 0 || 10+sz > len(buf) {
			return 0
		}
		if frameID == "TLEN" && sz > 1 {
			if ms, err := strconv.ParseInt(string(buf[11:10+sz]), 10, 64); err == nil {
				return ms / 1000 //nolint:mnd
			}
		}
		buf = buf[10+sz:]
	}
	return 0
}

// PutSyncsafe encodes size as a 4-byte syncsafe integer (7 bits per byte).
func PutSyncsafe(dst []byte, size int) {
	dst[0] = byte(size>>21) & 0x7f //nolint:gosec
	dst[1] = byte(size>>14) & 0x7f //nolint:gosec
	dst[2] = byte(size>>7) & 0x7f  //nolint:gosec
	dst[3] = byte(size) & 0x7f     //nolint:gosec
}

// ReadSyncsafe decodes a 4-byte syncsafe integer (7 bits per byte).
func ReadSyncsafe(b []byte) int {
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3])
}
