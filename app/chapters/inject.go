package chapters

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/radio-t/stream-recorder/app/id3"
)

// chapFrame builds an ID3v2.4 CHAP frame with embedded TIT2 and optional WXXX sub-frames.
func chapFrame(elemID, title, link string, start, end time.Duration) []byte {
	var times [16]byte
	binary.BigEndian.PutUint32(times[0:4], uint32(start.Milliseconds())) //nolint:gosec // duration always positive and small
	binary.BigEndian.PutUint32(times[4:8], uint32(end.Milliseconds()))   //nolint:gosec // duration always positive and small
	binary.BigEndian.PutUint32(times[8:12], 0xFFFFFFFF)                  // start offset: unused
	binary.BigEndian.PutUint32(times[12:16], 0xFFFFFFFF)                 // end offset: unused

	data := make([]byte, 0, len(elemID)+1+16)
	data = append(data, elemID...)
	data = append(data, 0) // null terminator
	data = append(data, times[:]...)
	data = append(data, id3.TextFrame("TIT2", title)...)
	if link != "" {
		data = append(data, wxxxFrame(link)...)
	}

	frame := make([]byte, 10+len(data))
	copy(frame[0:4], "CHAP")
	id3.PutSyncsafe(frame[4:8], len(data))
	copy(frame[10:], data)
	return frame
}

// wxxxFrame builds an ID3v2.4 WXXX (user-defined URL link) frame.
func wxxxFrame(url string) []byte {
	data := make([]byte, 0, 2+len(url))
	data = append(data, 0x00, 0x00) // encoding (ISO-8859-1) + empty description null terminator
	data = append(data, url...)

	frame := make([]byte, 10+len(data))
	copy(frame[0:4], "WXXX")
	id3.PutSyncsafe(frame[4:8], len(data))
	copy(frame[10:], data)
	return frame
}

// ctocFrame builds an ID3v2.4 CTOC (table of contents) frame referencing the given chapter IDs.
func ctocFrame(chapIDs []string) []byte {
	data := []byte("toc0\x00")                    // element ID null-terminated
	data = append(data, 0x03, byte(len(chapIDs))) //nolint:gosec // flags (top-level + ordered) + entry count
	for _, elemID := range chapIDs {
		data = append(data, []byte(elemID)...)
		data = append(data, 0)
	}

	frame := make([]byte, 10+len(data))
	copy(frame[0:4], "CTOC")
	id3.PutSyncsafe(frame[4:8], len(data))
	copy(frame[10:], data)
	return frame
}

// BuildChapterFrames builds CHAP and CTOC frame bytes for the given chapters.
// caps at 255 chapters (ID3v2 CTOC entry count is a single byte).
func BuildChapterFrames(chapters []Chapter) []byte {
	if len(chapters) == 0 {
		return nil
	}
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
		frames = append(frames, chapFrame(chapID, ch.Title, ch.Link, ch.Offset, endTime)...)
	}
	return append(frames, ctocFrame(chapIDs)...)
}
