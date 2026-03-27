package recorder

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// WriteID3v2Header writes a minimal ID3v2.4 header with title, artist and recording date.
func WriteID3v2Header(w io.Writer, episode string, recorded time.Time) error {
	frames := id3TextFrame("TIT2", "Radio-T "+episode)
	frames = append(frames, id3TextFrame("TPE1", "Radio-T")...)
	frames = append(frames, id3TextFrame("TDRC", recorded.Format("2006-01-02 15:04"))...)

	// ID3v2.4 header: "ID3" + version 2.4 + no flags + syncsafe size
	header := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}
	putSyncsafe(header[6:10], len(frames))

	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(frames)
	return err
}

// id3TextFrame builds an ID3v2.4 text frame (UTF-8 encoding).
func id3TextFrame(id, text string) []byte {
	data := append([]byte{3}, []byte(text)...) // 0x03 = UTF-8 encoding
	frame := make([]byte, 10+len(data))
	copy(frame[0:4], id)
	putSyncsafe(frame[4:8], len(data))
	// frame[8:10] = flags, left as 0x0000
	copy(frame[10:], data)
	return frame
}

// putSyncsafe encodes size as a 4-byte syncsafe integer (7 bits per byte).
func putSyncsafe(dst []byte, size int) {
	dst[0] = byte(size>>21) & 0x7f //nolint:gosec
	dst[1] = byte(size>>14) & 0x7f //nolint:gosec
	dst[2] = byte(size>>7) & 0x7f  //nolint:gosec
	dst[3] = byte(size) & 0x7f     //nolint:gosec
}

// readSyncsafe decodes a 4-byte syncsafe integer (7 bits per byte).
func readSyncsafe(b []byte) int {
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3])
}

// id3ChapFrame builds an ID3v2.4 CHAP frame with embedded TIT2 and optional WXXX sub-frames.
func id3ChapFrame(id, title, link string, start, end time.Duration) []byte {
	var times [16]byte
	binary.BigEndian.PutUint32(times[0:4], uint32(start.Milliseconds())) //nolint:gosec // duration always positive and small
	binary.BigEndian.PutUint32(times[4:8], uint32(end.Milliseconds()))   //nolint:gosec // duration always positive and small
	binary.BigEndian.PutUint32(times[8:12], 0xFFFFFFFF)                  // start offset: unused
	binary.BigEndian.PutUint32(times[12:16], 0xFFFFFFFF)                 // end offset: unused

	data := make([]byte, 0, len(id)+1+16)
	data = append(data, id...)
	data = append(data, 0) // null terminator
	data = append(data, times[:]...)
	data = append(data, id3TextFrame("TIT2", title)...)
	if link != "" {
		data = append(data, id3WXXXFrame(link)...)
	}

	frame := make([]byte, 10+len(data))
	copy(frame[0:4], "CHAP")
	putSyncsafe(frame[4:8], len(data))
	copy(frame[10:], data)
	return frame
}

// id3WXXXFrame builds an ID3v2.4 WXXX (user-defined URL link) frame.
func id3WXXXFrame(url string) []byte {
	data := make([]byte, 0, 2+len(url))
	data = append(data, 0x00, 0x00) // encoding (ISO-8859-1) + empty description null terminator
	data = append(data, url...)

	frame := make([]byte, 10+len(data))
	copy(frame[0:4], "WXXX")
	putSyncsafe(frame[4:8], len(data))
	copy(frame[10:], data)
	return frame
}

// id3CTOCFrame builds an ID3v2.4 CTOC (table of contents) frame referencing the given chapter IDs.
func id3CTOCFrame(chapIDs []string) []byte {
	data := []byte("toc0\x00")                    // element ID null-terminated
	data = append(data, 0x03, byte(len(chapIDs))) //nolint:gosec // flags (top-level + ordered) + entry count
	for _, id := range chapIDs {
		data = append(data, []byte(id)...)
		data = append(data, 0)
	}

	frame := make([]byte, 10+len(data))
	copy(frame[0:4], "CTOC")
	putSyncsafe(frame[4:8], len(data))
	copy(frame[10:], data)
	return frame
}

// InjectChapters opens an MP3 file and writes CHAP/CTOC frames into its ID3v2 header.
// uses a single-pass copy to a temp file, then atomic rename to replace the original.
// does nothing when chapters is empty.
func InjectChapters(filePath string, chapters []Chapter) error {
	if len(chapters) == 0 {
		return nil
	}

	src, err := os.Open(filePath) //nolint:gosec // caller provides path
	if err != nil {
		return fmt.Errorf("open file for chapter injection: %w", err)
	}
	defer src.Close() //nolint:errcheck

	// read existing ID3 header
	header := make([]byte, 10)
	if _, err := io.ReadFull(src, header); err != nil {
		return fmt.Errorf("read ID3 header: %w", err)
	}
	if string(header[0:3]) != "ID3" {
		return fmt.Errorf("not an ID3v2 file")
	}
	oldSize := readSyncsafe(header[6:10])

	chapFrames := buildChapterFrames(chapters)
	newSize := oldSize + len(chapFrames)
	if newSize > 0x0FFFFFFF {
		return fmt.Errorf("ID3 tag size %d exceeds syncsafe maximum", newSize)
	}

	// update header with new size, build the new file in a temp, then rename atomically
	putSyncsafe(header[6:10], newSize)
	tmpPath, err := writeChapteredFile(filepath.Dir(filePath), header, src, int64(oldSize), chapFrames)
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, filePath)
}

// writeChapteredFile creates a temp file containing: updated ID3 header + existing frames +
// chapter frames + remaining audio from src. Returns the temp file path on success.
// On failure, the temp file is removed.
func writeChapteredFile(dir string, header []byte, src io.Reader, frameSize int64, chapFrames []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, "chapters-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file for chapter injection: %w", err)
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
	if _, err := tmp.Write(chapFrames); err != nil {
		return "", fmt.Errorf("write chapter frames: %w", err)
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

// buildChapterFrames builds CHAP and CTOC frame bytes for the given chapters.
// caps at 255 chapters (ID3v2 CTOC entry count is a single byte).
func buildChapterFrames(chapters []Chapter) []byte {
	if len(chapters) > 255 { //nolint:mnd // ID3v2 CTOC entry count is a single byte
		chapters = chapters[:255]
	}
	var frames []byte
	chapIDs := make([]string, len(chapters))
	for i, ch := range chapters {
		chapID := fmt.Sprintf("chp%d", i)
		chapIDs[i] = chapID
		var endTime time.Duration
		if i+1 < len(chapters) {
			endTime = chapters[i+1].Offset
		} else {
			endTime = time.Duration(0xFFFFFFFF) * time.Millisecond // unknown end
		}
		frames = append(frames, id3ChapFrame(chapID, ch.Title, ch.Link, ch.Offset, endTime)...)
	}
	return append(frames, id3CTOCFrame(chapIDs)...)
}
