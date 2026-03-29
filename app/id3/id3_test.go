package id3

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			PutSyncsafe(dst, tc.val)
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
			assert.Equal(t, tc.want, ReadSyncsafe(tc.data))
		})
	}
}

func TestPutReadSyncsafeRoundtrip(t *testing.T) {
	for _, val := range []int{0, 1, 127, 128, 255, 256, 1000, 16383, 16384, 100000} {
		dst := make([]byte, 4)
		PutSyncsafe(dst, val)
		assert.Equal(t, val, ReadSyncsafe(dst), "roundtrip for %d", val)
	}
}

func TestTextFrame(t *testing.T) {
	frame := TextFrame("TIT2", "Hello")
	assert.Equal(t, "TIT2", string(frame[0:4]))
	sz := ReadSyncsafe(frame[4:8])
	assert.Equal(t, len(frame)-10, sz)
	assert.Equal(t, byte(3), frame[10], "encoding should be UTF-8")
	assert.Equal(t, "Hello", string(frame[11:10+sz]))
}

func TestWriteHeader(t *testing.T) {
	var buf bytes.Buffer
	frames := TextFrame("TIT2", "Test")
	require.NoError(t, WriteHeader(&buf, frames))

	data := buf.Bytes()
	assert.Equal(t, "ID3", string(data[0:3]))
	assert.Equal(t, byte(4), data[3], "version 2.4")
	size := ReadSyncsafe(data[6:10])
	assert.Equal(t, len(frames), size)
	assert.Equal(t, frames, data[10:])
}

func TestInjectFrames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.mp3")

	// create a minimal ID3v2 file
	var buf bytes.Buffer
	origFrames := TextFrame("TIT2", "Original")
	require.NoError(t, WriteHeader(&buf, origFrames))
	audioData := "fake-audio-data-12345"
	buf.WriteString(audioData)
	require.NoError(t, os.WriteFile(filePath, buf.Bytes(), 0o600))

	// inject extra frames
	extraFrames := TextFrame("TLEN", "9690000")
	require.NoError(t, InjectFrames(filePath, extraFrames))

	data, err := os.ReadFile(filePath) //nolint:gosec // test file
	require.NoError(t, err)

	assert.Equal(t, "ID3", string(data[:3]))
	newSize := ReadSyncsafe(data[6:10])
	assert.Equal(t, len(origFrames)+len(extraFrames), newSize, "tag size should include both original and extra frames")
	assert.True(t, strings.HasSuffix(string(data), audioData), "audio data should be intact")
	assert.Contains(t, string(data), "Original")
	assert.Contains(t, string(data), "TLEN")
	assert.Contains(t, string(data), "9690000")

	// verify ReadTLEN can read back the injected value
	assert.Equal(t, int64(9690), ReadTLEN(filePath), "ReadTLEN should return duration in seconds")
}

func TestReadTLEN_NoTLEN(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "no-tlen.mp3")

	var buf bytes.Buffer
	require.NoError(t, WriteHeader(&buf, TextFrame("TIT2", "Test")))
	buf.WriteString("audio")
	require.NoError(t, os.WriteFile(filePath, buf.Bytes(), 0o600))

	assert.Equal(t, int64(0), ReadTLEN(filePath), "should return 0 when TLEN is absent")
}

func TestReadTLEN_NonExistent(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(0), ReadTLEN("/nonexistent/file.mp3"))
}

func TestFindTLEN(t *testing.T) {
	t.Parallel()
	// craft raw frame bytes: TIT2 frame + TLEN frame
	frames := TextFrame("TIT2", "Test")
	frames = append(frames, TextFrame("TLEN", "9698763")...)
	assert.Equal(t, int64(9698), findTLEN(frames), "should find TLEN in frame sequence")
}

func TestFindTLEN_Empty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(0), findTLEN(nil))
	assert.Equal(t, int64(0), findTLEN([]byte{}))
}
