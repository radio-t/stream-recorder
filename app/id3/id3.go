// Package id3 provides ID3v2.4 frame building and injection primitives.
package id3

import (
	"context"
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

// maxTagInspect caps how much of an existing tag is read into memory to locate its padding.
// a larger tag is copied verbatim, as it was before padding was taken into account; the files
// this package writes carry a few kilobytes of frames, so the cap is never reached in practice.
const maxTagInspect = 1 << 20

// maxSyncsafeTagSize is the largest value a syncsafe tag size can hold.
const maxSyncsafeTagSize = 1<<28 - 1

// InjectFrames appends extra frames into an existing MP3 file's ID3v2 header.
// uses a single-pass copy to a temp file, then atomic rename to replace the original.
// any padding the existing tag ends with is dropped, since the spec places padding after
// the last frame and parsers stop reading there.
func InjectFrames(filePath string, extraFrames []byte) error {
	return InjectFramesContext(context.Background(), filePath, extraFrames)
}

// InjectFramesContext is InjectFrames bounded by ctx: cancelling it aborts the copy between
// reads and removes the temporary file, leaving the original in place.
func InjectFramesContext(ctx context.Context, filePath string, extraFrames []byte) error {
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

	existing, copySize, err := readTrimmedFrames(src, header)
	if err != nil {
		return err
	}
	newSize := len(existing) + int(copySize) + len(extraFrames)
	if newSize > maxSyncsafeTagSize {
		return fmt.Errorf("resulting tag of %d bytes exceeds the ID3v2 limit", newSize)
	}
	PutSyncsafe(header[6:10], newSize)

	tmpPath, err := rewriteFile(ctx, filepath.Dir(filePath), header, existing, src, copySize, extraFrames)
	if err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, srcInfo.Mode().Perm()); err != nil {
		os.Remove(tmpPath) //nolint:errcheck,gosec
		return fmt.Errorf("set permissions: %w", err)
	}
	return os.Rename(tmpPath, filePath)
}

// readTrimmedFrames reads the existing frame region and returns it without its padding.
// only a plain ID3v2.4 tag is walked: another major version sizes its frames differently, and
// an extended header, a footer or unsynchronisation all change the layout the scanner assumes.
// for those, and for a tag too large to inspect, nothing is read and the number of frame bytes
// left to copy from src is returned instead, which is what this package did before.
func readTrimmedFrames(src io.Reader, header []byte) (existing []byte, copySize int64, err error) {
	size := ReadSyncsafe(header[6:10])
	if header[3] != 4 || header[5] != 0 || size <= 0 || size > maxTagInspect {
		return nil, int64(size), nil
	}

	existing = make([]byte, size)
	if _, err := io.ReadFull(src, existing); err != nil {
		return nil, 0, fmt.Errorf("read ID3 frames: %w", err)
	}
	return existing[:frameRegionEnd(existing)], 0, nil
}

// frameRegionEnd returns the length of the actual frames in a tag's frame region, excluding
// the padding it may end with. a region this code cannot walk is kept whole, so nothing that
// might still hold data is dropped.
func frameRegionEnd(frames []byte) int {
	pos := 0
	for pos+10 <= len(frames) {
		if frames[pos] == 0 {
			break // frame IDs are alphanumeric, so padding may start here
		}
		size := ReadSyncsafe(frames[pos+4 : pos+8])
		if size <= 0 || pos+10+size > len(frames) {
			return len(frames)
		}
		pos += 10 + size
	}
	// the remainder is padding only when every byte of it is zero
	for _, b := range frames[pos:] {
		if b != 0 {
			return len(frames)
		}
	}
	return pos
}

// rewriteFile creates a temp file with: updated header + existing frames + extra frames + audio.
// existing holds already-read frame bytes, frameSize the number of frame bytes still to copy
// from src; exactly one of the two is used.
func rewriteFile(ctx context.Context, dir string, header, existing []byte, src io.Reader,
	frameSize int64, extra []byte) (string, error) {
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
	if _, err := tmp.Write(existing); err != nil {
		return "", fmt.Errorf("write existing frames: %w", err)
	}
	if _, err := io.CopyN(tmp, &ctxReader{ctx: ctx, r: src}, frameSize); err != nil {
		return "", fmt.Errorf("copy existing frames: %w", err)
	}
	if _, err := tmp.Write(extra); err != nil {
		return "", fmt.Errorf("write extra frames: %w", err)
	}
	if _, err := io.Copy(tmp, &ctxReader{ctx: ctx, r: src}); err != nil {
		return "", fmt.Errorf("copy audio data: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}
	ok = true
	return tmpPath, nil
}

// ctxReader aborts a copy between reads once the context is done, so rewriting a multi-gigabyte
// recording does not have to run to completion before the process can exit.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p) //nolint:wrapcheck // transparent pass-through
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
