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

	"github.com/radio-t/stream-recorder/app/id3"
)

func TestChapFrame(t *testing.T) {
	frame := chapFrame("chp0", "Test Chapter", "https://example.com", 0, 30*time.Second)

	// verify frame header
	assert.Equal(t, "CHAP", string(frame[0:4]))
	size := id3.ReadSyncsafe(frame[4:8])
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
	tit2Size := id3.ReadSyncsafe(subFrames[4:8])
	assert.Equal(t, byte(3), subFrames[10], "TIT2 encoding should be UTF-8")
	tit2Text := string(subFrames[11 : 10+tit2Size])
	assert.Equal(t, "Test Chapter", tit2Text)

	// embedded WXXX sub-frame after TIT2
	wxxx := subFrames[10+tit2Size:]
	assert.Equal(t, "WXXX", string(wxxx[0:4]))
	wxxxSize := id3.ReadSyncsafe(wxxx[4:8])
	assert.Equal(t, byte(0), wxxx[10], "WXXX encoding should be ISO-8859-1")
	assert.Equal(t, byte(0), wxxx[11], "WXXX description should be empty")
	url := string(wxxx[12 : 10+wxxxSize])
	assert.Equal(t, "https://example.com", url)
}

func TestChapFrameNoLink(t *testing.T) {
	frame := chapFrame("chp1", "No Link Chapter", "", 5*time.Second, 10*time.Second)

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
	tit2Size := id3.ReadSyncsafe(subFrames[4:8])

	remaining := subFrames[10+tit2Size:]
	assert.Empty(t, remaining, "no WXXX frame when link is empty")
}

func TestCTOCFrame(t *testing.T) {
	frame := ctocFrame([]string{"chp0", "chp1", "chp2"})

	assert.Equal(t, "CTOC", string(frame[0:4]))
	size := id3.ReadSyncsafe(frame[4:8])
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
	frames := id3.TextFrame("TIT2", "Test")
	require.NoError(t, id3.WriteHeader(&buf, frames))
	buf.Write([]byte(audioData))
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o750))
	require.NoError(t, os.WriteFile(filePath, buf.Bytes(), 0o600))
}

func TestBuildChapterFrames(t *testing.T) {
	t.Parallel()
	chaps := []Chapter{
		{Title: "Introduction", Link: "https://example.com/intro", Offset: 0},
		{Title: "Main Topic", Link: "https://example.com/main", Offset: 5 * time.Minute},
		{Title: "Wrap Up", Link: "", Offset: 45 * time.Minute},
	}
	frames := BuildChapterFrames(chaps)
	assert.Contains(t, string(frames), "CHAP")
	assert.Contains(t, string(frames), "CTOC")
	assert.Contains(t, string(frames), "Introduction")
	assert.Contains(t, string(frames), "Main Topic")
	assert.Contains(t, string(frames), "Wrap Up")
}

func TestBuildChapterFramesEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, BuildChapterFrames(nil))
	assert.Nil(t, BuildChapterFrames([]Chapter{}))
}

func TestInjectFramesWithChapters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.mp3")
	audioData := "fake-mp3-audio-data-1234567890"
	writeTestMP3(t, filePath, audioData)

	chaps := []Chapter{
		{Title: "Introduction", Link: "https://example.com/intro", Offset: 0},
		{Title: "Main Topic", Link: "https://example.com/main", Offset: 5 * time.Minute},
		{Title: "Wrap Up", Link: "", Offset: 45 * time.Minute},
	}
	require.NoError(t, id3.InjectFrames(filePath, BuildChapterFrames(chaps)))

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

func TestInjectFramesNonID3File(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plain.mp3")

	// write a file without an ID3 header (raw MP3 frame, at least 10 bytes for header read)
	require.NoError(t, os.WriteFile(filePath, []byte{0xFF, 0xFB, 0x90, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, 0o600))

	err := id3.InjectFrames(filePath, BuildChapterFrames([]Chapter{{Title: "Test", Offset: 0}}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an ID3v2 file")
}
