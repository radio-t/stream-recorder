package recorder

import (
	"strconv"
	"time"

	"github.com/radio-t/stream-recorder/app/id3"
)

// WriteID3v2Header writes an ID3v2.4 header with title, artist, recording date and podcast metadata.
func WriteID3v2Header(w interface{ Write([]byte) (int, error) }, episode string, recorded time.Time) error {
	frames := id3.TextFrame("TIT2", "Radio-T "+episode)
	frames = append(frames, id3.TextFrame("TPE1", "Radio-T")...)
	frames = append(frames, id3.TextFrame("TALB", "Radio-T")...)
	frames = append(frames, id3.TextFrame("TRCK", episode)...)
	frames = append(frames, id3.TextFrame("TCON", "Podcast")...)
	frames = append(frames, id3.TextFrame("TCOP", "Some rights reserved, Radio-T")...)
	frames = append(frames, id3.TextFrame("TDRC", recorded.Format("2006-01-02 15:04"))...)
	return id3.WriteHeader(w, frames)
}

// TLENFrame builds a TLEN (track length in milliseconds) ID3v2 text frame.
func TLENFrame(duration time.Duration) []byte {
	return id3.TextFrame("TLEN", strconv.FormatInt(duration.Milliseconds(), 10))
}
