package recorder

import (
	"encoding/binary"
	"io"
	"time"
)

// WriteID3v2Header writes a minimal ID3v2.3 header with title, artist and recording date.
func WriteID3v2Header(w io.Writer, episode string, recorded time.Time) error {
	frames := id3TextFrame("TIT2", "Radio-T "+episode)
	frames = append(frames, id3TextFrame("TPE1", "Radio-T")...)
	frames = append(frames, id3TextFrame("TDRC", recorded.Format("2006-01-02 15:04"))...)

	// ID3v2.3 header: "ID3" + version 2.3 + no flags + syncsafe size
	header := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 0}
	putSyncsafe(header[6:10], len(frames))

	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(frames)
	return err
}

// id3TextFrame builds an ID3v2.3 text frame (UTF-8 encoding).
func id3TextFrame(id, text string) []byte {
	data := append([]byte{3}, []byte(text)...) // 0x03 = UTF-8 encoding
	frame := make([]byte, 10+len(data))
	copy(frame[0:4], id)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(data))) //nolint:gosec // length always small
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
