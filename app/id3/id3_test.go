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

func TestInjectFrames_DropsExistingPadding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.mp3")

	// tag padded the way ffmpeg pads the one it writes on remux
	origFrames := TextFrame("TIT2", "Original")
	padding := make([]byte, 11)
	var buf bytes.Buffer
	require.NoError(t, WriteHeader(&buf, append(append([]byte{}, origFrames...), padding...)))
	audioData := "fake-audio-data-12345"
	buf.WriteString(audioData)
	require.NoError(t, os.WriteFile(filePath, buf.Bytes(), 0o600))

	extraFrames := TextFrame("TLEN", "9690000")
	require.NoError(t, InjectFrames(filePath, extraFrames))

	data, err := os.ReadFile(filePath) //nolint:gosec // test file
	require.NoError(t, err)

	newSize := ReadSyncsafe(data[6:10])
	assert.Equal(t, len(origFrames)+len(extraFrames), newSize, "padding should be dropped, not kept before the new frames")
	assert.True(t, strings.HasSuffix(string(data), audioData), "audio data should be intact")
	assert.Contains(t, string(data), "Original")
	assert.Equal(t, int64(9690), ReadTLEN(filePath), "injected frames must sit before any padding to be readable")
}

func TestFrameRegionEnd(t *testing.T) {
	t.Parallel()

	frame := TextFrame("TIT2", "Original")

	tests := []struct {
		name   string
		frames []byte
		want   int
	}{
		{name: "empty region", frames: nil, want: 0},
		{name: "single frame, no padding", frames: frame, want: len(frame)},
		{name: "frame followed by padding", frames: append(append([]byte{}, frame...), make([]byte, 11)...), want: len(frame)},
		{name: "padding only", frames: make([]byte, 20), want: 0},
		{name: "padding shorter than a frame header", frames: append(append([]byte{}, frame...), make([]byte, 4)...), want: len(frame)},
		{name: "short non-zero tail is kept", frames: append(append([]byte{}, frame...), 1, 2, 3), want: len(frame) + 3},
		{name: "zero at a frame boundary followed by data keeps the region",
			frames: append(append([]byte{}, frame...), append([]byte{0}, frame...)...), want: len(frame)*2 + 1},
		{name: "two frames", frames: append(append([]byte{}, frame...), frame...), want: 2 * len(frame)},
		{name: "truncated frame keeps the whole region", frames: frame[:len(frame)-2], want: len(frame) - 2},
		{name: "oversized frame length keeps the whole region", frames: oversizedFrame(), want: len(oversizedFrame())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, frameRegionEnd(tt.frames))
		})
	}
}

// oversizedFrame builds a frame header claiming more data than the region holds.
func oversizedFrame() []byte {
	f := TextFrame("TIT2", "Original")
	PutSyncsafe(f[4:8], 1000)
	return f
}

func TestInjectFrames_KeepsUnsupportedTagLayouts(t *testing.T) {
	t.Parallel()

	origFrames := TextFrame("TIT2", "Original")
	padding := make([]byte, 11)
	extraFrames := TextFrame("TLEN", "9690000")

	tests := []struct {
		name    string
		version byte
		flags   byte
	}{
		{name: "id3v2.3 tag", version: 3, flags: 0},
		{name: "unsynchronised tag", version: 4, flags: 0x80},
		{name: "extended header", version: 4, flags: 0x40},
		{name: "tag with footer", version: 4, flags: 0x10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			filePath := filepath.Join(dir, "test.mp3")

			body := append(append([]byte{}, origFrames...), padding...)
			header := []byte{'I', 'D', '3', tt.version, 0, tt.flags, 0, 0, 0, 0}
			PutSyncsafe(header[6:10], len(body))
			var buf bytes.Buffer
			buf.Write(header)
			buf.Write(body)
			buf.WriteString("fake-audio-data-12345")
			require.NoError(t, os.WriteFile(filePath, buf.Bytes(), 0o600))

			require.NoError(t, InjectFrames(filePath, extraFrames))

			data, err := os.ReadFile(filePath) //nolint:gosec // test file
			require.NoError(t, err)
			assert.Equal(t, len(body)+len(extraFrames), ReadSyncsafe(data[6:10]),
				"a tag layout the scanner does not understand should be copied verbatim")
			assert.Contains(t, string(data), "Original", "existing frames must be preserved")
		})
	}
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
