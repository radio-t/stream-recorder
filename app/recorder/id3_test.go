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

	// verify ID3v2.3 header
	assert.Equal(t, "ID3", string(data[0:3]), "should start with ID3 magic")
	assert.Equal(t, byte(3), data[3], "major version should be 3")
	assert.Equal(t, byte(0), data[4], "minor version should be 0")

	// decode syncsafe size and verify it matches remaining bytes
	size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
	assert.Equal(t, len(data)-10, size, "syncsafe size should match frame data length")

	// parse frames from the body
	frames := data[10:]
	found := map[string]string{}
	for len(frames) >= 10 {
		id := string(frames[0:4])
		sz := int(binary.BigEndian.Uint32(frames[4:8]))
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
