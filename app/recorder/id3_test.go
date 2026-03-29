package recorder_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radio-t/stream-recorder/app/id3"
	"github.com/radio-t/stream-recorder/app/recorder"
)

func TestWriteID3v2Header(t *testing.T) {
	var buf bytes.Buffer
	ts := time.Date(2026, 3, 27, 10, 30, 0, 0, time.UTC)
	require.NoError(t, recorder.WriteID3v2Header(&buf, "999", ts))

	data := buf.Bytes()

	// verify ID3v2.4 header
	assert.Equal(t, "ID3", string(data[0:3]), "should start with ID3 magic")
	assert.Equal(t, byte(4), data[3], "major version should be 4")
	assert.Equal(t, byte(0), data[4], "minor version should be 0")

	// decode syncsafe size and verify it matches remaining bytes
	size := id3.ReadSyncsafe(data[6:10])
	assert.Equal(t, len(data)-10, size, "syncsafe size should match frame data length")

	// parse frames from the body
	frames := data[10:]
	found := map[string]string{}
	for len(frames) >= 10 {
		frameID := string(frames[0:4])
		sz := id3.ReadSyncsafe(frames[4:8])
		if sz == 0 || 10+sz > len(frames) {
			break
		}
		// skip 2-byte flags and 1-byte encoding prefix
		text := string(frames[11 : 10+sz])
		found[frameID] = text
		frames = frames[10+sz:]
	}

	assert.Equal(t, "Radio-T 999", found["TIT2"], "title should contain episode number")
	assert.Equal(t, "Radio-T", found["TPE1"], "artist should be Radio-T")
	assert.Equal(t, "Radio-T", found["TALB"], "album should be Radio-T")
	assert.Equal(t, "999", found["TRCK"], "track number should be episode number")
	assert.Equal(t, "Podcast", found["TCON"], "genre should be Podcast")
	assert.Equal(t, "Some rights reserved, Radio-T", found["TCOP"], "copyright should be set")
	assert.Equal(t, "2026-03-27 10:30", found["TDRC"], "recording date should be formatted")
}

func TestTLENFrame(t *testing.T) {
	frame := recorder.TLENFrame(2*time.Hour + 41*time.Minute + 30*time.Second)
	assert.Equal(t, "TLEN", string(frame[0:4]))
	sz := id3.ReadSyncsafe(frame[4:8])
	// skip flags (2 bytes) + encoding byte (1 byte)
	text := string(frame[11 : 10+sz])
	assert.Equal(t, "9690000", text, "TLEN should be duration in milliseconds")
}
