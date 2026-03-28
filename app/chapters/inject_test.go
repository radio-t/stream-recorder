package chapters

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// writeTestMP3 creates a minimal MP3 file with an ID3v2.4 header and the given audio data.
func writeTestMP3(t *testing.T, filePath, audioData string) {
	t.Helper()
	var buf bytes.Buffer
	frames := id3TextFrame("TIT2", "Test")
	header := []byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}
	putSyncsafe(header[6:10], len(frames))
	buf.Write(header)
	buf.Write(frames)
	buf.Write([]byte(audioData))
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o750))
	require.NoError(t, os.WriteFile(filePath, buf.Bytes(), 0o600))
}

func TestInjectChapters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.mp3")
	audioData := "fake-mp3-audio-data-1234567890"
	writeTestMP3(t, filePath, audioData)

	chapters := []Chapter{
		{Title: "Introduction", Link: "https://example.com/intro", Offset: 0},
		{Title: "Main Topic", Link: "https://example.com/main", Offset: 5 * time.Minute},
		{Title: "Wrap Up", Link: "", Offset: 45 * time.Minute},
	}
	require.NoError(t, InjectChapters(filePath, chapters))

	data, err := os.ReadFile(filePath) //nolint:gosec // test file
	require.NoError(t, err)

	assert.Equal(t, "ID3", string(data[:3]))
	assert.True(t, strings.HasSuffix(string(data), audioData),
		"audio data should be intact after chapter injection")
	assert.Contains(t, string(data), "CHAP")
	assert.Contains(t, string(data), "CTOC")
	assert.Contains(t, string(data), "Introduction")
	assert.Contains(t, string(data), "Main Topic")
	assert.Contains(t, string(data), "Wrap Up")
}

func TestInjectChaptersEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.mp3")
	writeTestMP3(t, filePath, "audio")

	original, err := os.ReadFile(filePath) //nolint:gosec // test file
	require.NoError(t, err)

	require.NoError(t, InjectChapters(filePath, nil))

	after, err := os.ReadFile(filePath) //nolint:gosec // test file
	require.NoError(t, err)
	assert.Equal(t, original, after, "file should be unchanged with no chapters")
}

func TestInjectChaptersNonexistentFile(t *testing.T) {
	t.Parallel()
	chapters := []Chapter{
		{Title: "Test", Offset: 0},
	}
	err := InjectChapters("/nonexistent/path/file.mp3", chapters)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chapter injection")
}

func TestInjectChaptersNonID3File(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plain.mp3")

	// write a file without an ID3 header (raw MP3 frame, at least 10 bytes for header read)
	require.NoError(t, os.WriteFile(filePath, []byte{0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, 0o600))

	chapters := []Chapter{
		{Title: "Test", Offset: 0},
	}
	err := InjectChapters(filePath, chapters)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an ID3v2 file")
}
