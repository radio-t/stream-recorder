package recorder

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteID3v2Header(t *testing.T) {
	var buf bytes.Buffer
	ts := time.Date(2026, 3, 27, 10, 30, 0, 0, time.UTC)
	require.NoError(t, WriteID3v2Header(&buf, "999", ts))

	data := buf.Bytes()

	// verify ID3v2.4 header
	assert.Equal(t, "ID3", string(data[0:3]), "should start with ID3 magic")
	assert.Equal(t, byte(4), data[3], "major version should be 4")
	assert.Equal(t, byte(0), data[4], "minor version should be 0")

	// decode syncsafe size and verify it matches remaining bytes
	size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
	assert.Equal(t, len(data)-10, size, "syncsafe size should match frame data length")

	// parse frames from the body
	frames := data[10:]
	found := map[string]string{}
	for len(frames) >= 10 {
		id := string(frames[0:4])
		sz := readSyncsafe(frames[4:8])
		if sz == 0 || 10+sz > len(frames) {
			break
		}
		// skip 2-byte flags and 1-byte encoding prefix
		text := string(frames[11 : 10+sz])
		found[id] = text
		frames = frames[10+sz:]
	}

	assert.Equal(t, "Radio-T 999", found["TIT2"], "title should contain episode number")
	assert.Equal(t, "Radio-T", found["TPE1"], "artist should be Radio-T")
	assert.Equal(t, "2026-03-27 10:30", found["TDRC"], "recording date should be formatted")
}

func TestPutSyncsafe(t *testing.T) {
	tests := []struct {
		name string
		val  int
		want []byte
	}{
		{name: "zero", val: 0, want: []byte{0, 0, 0, 0}},
		{name: "127", val: 127, want: []byte{0, 0, 0, 127}},
		{name: "128", val: 128, want: []byte{0, 0, 1, 0}},
		{name: "255", val: 255, want: []byte{0, 0, 1, 127}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := make([]byte, 4)
			putSyncsafe(dst, tc.val)
			assert.Equal(t, tc.want, dst)
		})
	}
}

func TestReadSyncsafe(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{name: "zero", data: []byte{0, 0, 0, 0}, want: 0},
		{name: "127", data: []byte{0, 0, 0, 127}, want: 127},
		{name: "128", data: []byte{0, 0, 1, 0}, want: 128},
		{name: "255", data: []byte{0, 0, 1, 127}, want: 255},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, readSyncsafe(tc.data))
		})
	}
}

func TestReadPutSyncsafeRoundtrip(t *testing.T) {
	for _, val := range []int{0, 1, 127, 128, 255, 256, 1000, 16383, 16384, 100000} {
		dst := make([]byte, 4)
		putSyncsafe(dst, val)
		assert.Equal(t, val, readSyncsafe(dst), "roundtrip for %d", val)
	}
}

func TestID3ChapFrame(t *testing.T) {
	frame := id3ChapFrame("chp0", "Test Chapter", "https://example.com", 0, 30*time.Second)

	// verify frame header
	assert.Equal(t, "CHAP", string(frame[0:4]))
	size := readSyncsafe(frame[4:8])
	assert.Equal(t, len(frame)-10, size)
	assert.Equal(t, []byte{0, 0}, frame[8:10], "flags should be zero")

	body := frame[10:]

	// element ID: "chp0\0"
	assert.Equal(t, []byte("chp0\x00"), body[0:5])

	// time fields start at offset 5 (after element ID + null)
	startMs := binary.BigEndian.Uint32(body[5:9])
	endMs := binary.BigEndian.Uint32(body[9:13])
	startOff := binary.BigEndian.Uint32(body[13:17])
	endOff := binary.BigEndian.Uint32(body[17:21])
	assert.Equal(t, uint32(0), startMs, "start time 0ms")
	assert.Equal(t, uint32(30000), endMs, "end time 30000ms")
	assert.Equal(t, uint32(0xFFFFFFFF), startOff, "start offset unused")
	assert.Equal(t, uint32(0xFFFFFFFF), endOff, "end offset unused")

	// embedded TIT2 sub-frame
	subFrames := body[21:]
	assert.Equal(t, "TIT2", string(subFrames[0:4]))
	tit2Size := readSyncsafe(subFrames[4:8])
	assert.Equal(t, byte(3), subFrames[10], "TIT2 encoding should be UTF-8")
	tit2Text := string(subFrames[11 : 10+tit2Size])
	assert.Equal(t, "Test Chapter", tit2Text)

	// embedded WXXX sub-frame after TIT2
	wxxx := subFrames[10+tit2Size:]
	assert.Equal(t, "WXXX", string(wxxx[0:4]))
	wxxxSize := readSyncsafe(wxxx[4:8])
	assert.Equal(t, byte(0), wxxx[10], "WXXX encoding should be ISO-8859-1")
	assert.Equal(t, byte(0), wxxx[11], "WXXX description should be empty")
	url := string(wxxx[12 : 10+wxxxSize])
	assert.Equal(t, "https://example.com", url)
}

func TestID3ChapFrameNoLink(t *testing.T) {
	frame := id3ChapFrame("chp1", "No Link Chapter", "", 5*time.Second, 10*time.Second)

	body := frame[10:]
	assert.Equal(t, []byte("chp1\x00"), body[0:5])

	// verify start/end times
	startMs := binary.BigEndian.Uint32(body[5:9])
	endMs := binary.BigEndian.Uint32(body[9:13])
	assert.Equal(t, uint32(5000), startMs)
	assert.Equal(t, uint32(10000), endMs)

	// should have TIT2 but no WXXX
	subFrames := body[21:]
	assert.Equal(t, "TIT2", string(subFrames[0:4]))
	tit2Size := readSyncsafe(subFrames[4:8])

	remaining := subFrames[10+tit2Size:]
	assert.Empty(t, remaining, "no WXXX frame when link is empty")
}

func TestID3CTOCFrame(t *testing.T) {
	frame := id3CTOCFrame([]string{"chp0", "chp1", "chp2"})

	assert.Equal(t, "CTOC", string(frame[0:4]))
	size := readSyncsafe(frame[4:8])
	assert.Equal(t, len(frame)-10, size)
	assert.Equal(t, []byte{0, 0}, frame[8:10], "flags should be zero")

	body := frame[10:]
	assert.Equal(t, []byte("toc0\x00"), body[0:5])
	assert.Equal(t, byte(0x03), body[5], "flags: top-level + ordered")
	assert.Equal(t, byte(3), body[6], "entry count")

	entries := body[7:]
	assert.Equal(t, []byte("chp0\x00chp1\x00chp2\x00"), entries)
}
