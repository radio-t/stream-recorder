package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radio-t/stream-recorder/app/chapters"
	"github.com/radio-t/stream-recorder/app/recorder"
)

type mockStreamListener struct {
	listenFn func(ctx context.Context) (*recorder.Stream, error)
	calls    atomic.Int32
}

func (m *mockStreamListener) Listen(ctx context.Context) (*recorder.Stream, error) {
	m.calls.Add(1)
	return m.listenFn(ctx)
}

type mockStreamRecorder struct {
	recordFn func(ctx context.Context, s *recorder.Stream) (string, error)
}

func (m *mockStreamRecorder) Record(ctx context.Context, s *recorder.Stream) (string, error) {
	if m.recordFn != nil {
		return m.recordFn(ctx, s)
	}
	return "", nil
}

func TestRun_NoSchedule(t *testing.T) {
	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return nil, recorder.ErrNotFound
		},
	}
	mr := &mockStreamRecorder{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cfg := runConfig{
		schedule:     false,
		tickInterval: 10 * time.Millisecond,
		nowFn:        time.Now,
	}

	run(ctx, ml, mr, cfg, &recordingState{})
	assert.Positive(t, ml.calls.Load(), "listener should be called when schedule is disabled")
}

func TestRun_ScheduleInsideWindow(t *testing.T) {
	// saturday 21:00 UTC — inside the hardcoded window (Sat 18:00 - Sun 00:00)
	fixedTime := time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC)

	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return nil, recorder.ErrNotFound
		},
	}
	mr := &mockStreamRecorder{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cfg := runConfig{
		schedule:     true,
		tickInterval: 10 * time.Millisecond,
		nowFn:        func() time.Time { return fixedTime },
	}

	run(ctx, ml, mr, cfg, &recordingState{})
	assert.Positive(t, ml.calls.Load(), "listener should be called inside recording window")
}

func TestRun_ScheduleOutsideWindow(t *testing.T) {
	// monday 10:00 UTC — outside the hardcoded window
	fixedTime := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return nil, recorder.ErrNotFound
		},
	}
	mr := &mockStreamRecorder{}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cfg := runConfig{
		schedule:     true,
		tickInterval: 10 * time.Millisecond,
		nowFn:        func() time.Time { return fixedTime },
	}

	run(ctx, ml, mr, cfg, &recordingState{})
	assert.Equal(t, int32(0), ml.calls.Load(), "listener should not be called outside recording window")
}

func TestRun_ForceRecordOverridesSchedule(t *testing.T) {
	// monday 10:00 UTC — outside the hardcoded window
	fixedTime := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	var recorded atomic.Bool
	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) (string, error) {
			recorded.Store(true)
			return "", nil
		},
	}

	var forceFlag atomic.Bool
	forceFlag.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cfg := runConfig{
		schedule:     true,
		tickInterval: 10 * time.Millisecond,
		nowFn:        func() time.Time { return fixedTime },
	}
	state := &recordingState{forceRecord: &forceFlag}

	run(ctx, ml, mr, cfg, state)

	assert.True(t, recorded.Load(), "recording should happen outside window when force flag is set")
	assert.False(t, forceFlag.Load(), "force flag should be cleared after recording session")
}

func TestStartPurge_DisabledWhenZeroRetention(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)

	// create an old file that should not be deleted when purge is disabled
	makeTestFile(t, dir, "100/old.mp3", now.AddDate(0, 0, -60))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	startPurge(ctx, &wg, purgeConfig{
		dir:           dir,
		retentionDays: 0,
		interval:      10 * time.Millisecond,
		nowFn:         func() time.Time { return now },
	})

	wg.Wait()

	// file should still exist since purge is disabled
	_, err := os.Stat(filepath.Join(dir, "100", "old.mp3"))
	assert.NoError(t, err, "old file should remain when retention is 0")
}

func TestStartPurge_RunsOnStartup(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)

	makeTestFile(t, dir, "100/rt100_2026_02_08_12_00_00.mp3", now.AddDate(0, 0, -45))
	makeTestFile(t, dir, "200/rt200_2026_03_20_12_00_00.mp3", now.AddDate(0, 0, -5))

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	startPurge(ctx, &wg, purgeConfig{
		dir:           dir,
		retentionDays: 30,
		interval:      time.Hour, // long interval so only startup purge runs
		nowFn:         func() time.Time { return now },
	})

	// startup purge runs synchronously before the goroutine, so it's already done
	cancel()
	wg.Wait()

	// old file should be purged on startup
	_, err := os.Stat(filepath.Join(dir, "100", "rt100_2026_02_08_12_00_00.mp3"))
	assert.True(t, os.IsNotExist(err), "old file should be purged on startup")

	// new file should remain
	_, err = os.Stat(filepath.Join(dir, "200", "rt200_2026_03_20_12_00_00.mp3"))
	assert.NoError(t, err, "new file should remain after startup purge")
}

func TestStartPurge_RunsPeriodically(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	startPurge(ctx, &wg, purgeConfig{
		dir:           dir,
		retentionDays: 30,
		interval:      20 * time.Millisecond,
		nowFn:         func() time.Time { return now },
	})

	// add an old file after startup purge has run
	makeTestFile(t, dir, "300/rt300_2026_02_13_12_00_00.mp3", now.AddDate(0, 0, -40))

	// wait for periodic purge to pick it up
	assert.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, "300", "rt300_2026_02_13_12_00_00.mp3"))
		return os.IsNotExist(err)
	}, time.Second, 10*time.Millisecond, "periodic purge should remove old file")

	cancel()
	wg.Wait()
}

// makeTestFile creates a file at the given relative path under dir with the specified modification time.
func makeTestFile(t *testing.T, dir, relPath string, modTime time.Time) {
	t.Helper()
	fullPath := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o750))
	require.NoError(t, os.WriteFile(fullPath, []byte("test data"), 0o600))
	require.NoError(t, os.Chtimes(fullPath, modTime, modTime))
}

const testRecordedPath = "/tmp/test.mp3"

// mockChapterProvider implements chapterProvider for testing.
type mockChapterProvider struct {
	runCalled chan struct{} // closed when Run starts
	chapters  []chapters.Chapter
}

func (m *mockChapterProvider) Run(ctx context.Context) {
	close(m.runCalled)
	<-ctx.Done()
}

func (m *mockChapterProvider) Chapters() []chapters.Chapter {
	return m.chapters
}

func TestPollAndRecord_WithChapterTracking(t *testing.T) {
	chaps := []chapters.Chapter{
		{Title: "Topic 1", Link: "https://example.com/1", Offset: 0},
		{Title: "Topic 2", Link: "https://example.com/2", Offset: 5 * time.Minute},
	}

	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  chaps,
	}

	var injectedPath string
	var injectedChaps []chapters.Chapter

	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) (string, error) {
			<-tracker.runCalled // wait for tracker to start
			return testRecordedPath, nil
		},
	}

	cfg := runConfig{
		tickInterval: 10 * time.Millisecond,
		nowFn:        time.Now,
		newChapterTracker: func() chapterProvider {
			return tracker
		},
		injectChapters: func(path string, chs []chapters.Chapter) error {
			injectedPath = path
			injectedChaps = chs
			return nil
		},
	}

	ctx := context.Background()
	done := pollAndRecord(ctx, ml, mr, cfg, &recordingState{})

	assert.False(t, done)
	assert.Equal(t, testRecordedPath, injectedPath, "chapters should be injected into recorded file")
	assert.Equal(t, chaps, injectedChaps, "all collected chapters should be passed to injection")
}

func TestPollAndRecord_ChapterTrackingDisabled(t *testing.T) {
	var recorded bool
	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) (string, error) {
			recorded = true
			return testRecordedPath, nil
		},
	}

	cfg := runConfig{
		tickInterval:      10 * time.Millisecond,
		nowFn:             time.Now,
		newChapterTracker: nil, // disabled
	}

	ctx := context.Background()
	done := pollAndRecord(ctx, ml, mr, cfg, &recordingState{})

	assert.False(t, done)
	assert.True(t, recorded, "recording should work without chapter tracking")
}

func TestPollAndRecord_NoChaptersCollected(t *testing.T) {
	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  nil, // no chapters collected
	}

	var injectionCalled bool

	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) (string, error) {
			<-tracker.runCalled
			return testRecordedPath, nil
		},
	}

	cfg := runConfig{
		tickInterval: 10 * time.Millisecond,
		nowFn:        time.Now,
		newChapterTracker: func() chapterProvider {
			return tracker
		},
		injectChapters: func(_ string, _ []chapters.Chapter) error {
			injectionCalled = true
			return nil
		},
	}

	ctx := context.Background()
	pollAndRecord(ctx, ml, mr, cfg, &recordingState{})

	assert.False(t, injectionCalled, "injection should not be called when no chapters collected")
}

func TestPollAndRecord_ChapterTrackingRecordingFails(t *testing.T) {
	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  []chapters.Chapter{{Title: "Topic", Offset: 0}},
	}

	var injectionCalled bool

	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) (string, error) {
			<-tracker.runCalled
			return "", fmt.Errorf("recording failed")
		},
	}

	cfg := runConfig{
		tickInterval: 10 * time.Millisecond,
		nowFn:        time.Now,
		newChapterTracker: func() chapterProvider {
			return tracker
		},
		injectChapters: func(_ string, _ []chapters.Chapter) error {
			injectionCalled = true
			return nil
		},
	}

	ctx := context.Background()
	done := pollAndRecord(ctx, ml, mr, cfg, &recordingState{})

	assert.False(t, done)
	assert.False(t, injectionCalled, "injection should not be called when recording fails")
}

func TestPollAndRecord_ChapterInjectionError(t *testing.T) {
	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  []chapters.Chapter{{Title: "Topic", Offset: 0}},
	}

	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) (string, error) {
			<-tracker.runCalled
			return testRecordedPath, nil
		},
	}

	cfg := runConfig{
		tickInterval: 10 * time.Millisecond,
		nowFn:        time.Now,
		newChapterTracker: func() chapterProvider {
			return tracker
		},
		injectChapters: func(_ string, _ []chapters.Chapter) error {
			return fmt.Errorf("injection failed")
		},
	}

	ctx := context.Background()
	done := pollAndRecord(ctx, ml, mr, cfg, &recordingState{})

	// injection error should be logged but not stop the recording loop
	assert.False(t, done, "injection error should not stop the recording loop")
}

func TestPollAndRecord_ChapterInjectionOnContextCancel(t *testing.T) {
	// verify chapters are injected when recording is stopped by context cancellation (SIGINT)
	chaps := []chapters.Chapter{
		{Title: "Topic 1", Link: "https://example.com/1", Offset: 0},
		{Title: "Topic 2", Link: "https://example.com/2", Offset: 5 * time.Minute},
	}

	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  chaps,
	}

	var injectedPath string
	var injectedChaps []chapters.Chapter

	ctx, cancel := context.WithCancel(context.Background())

	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) (string, error) {
			<-tracker.runCalled
			cancel() // simulate SIGINT
			return testRecordedPath, ctx.Err()
		},
	}

	cfg := runConfig{
		tickInterval: 10 * time.Millisecond,
		nowFn:        time.Now,
		newChapterTracker: func() chapterProvider {
			return tracker
		},
		injectChapters: func(path string, chs []chapters.Chapter) error {
			injectedPath = path
			injectedChaps = chs
			return nil
		},
	}

	done := pollAndRecord(ctx, ml, mr, cfg, &recordingState{})

	assert.True(t, done, "should signal clean shutdown")
	assert.Equal(t, testRecordedPath, injectedPath, "chapters should be injected even on context cancellation")
	assert.Equal(t, chaps, injectedChaps, "all collected chapters should be passed to injection")
}

func TestInScheduleWindow(t *testing.T) {
	t.Parallel()

	// hardcoded: Saturday 20:00 UTC, 2h before (18:00), 4h after (00:00 Sunday)
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{
			name: "inside window, during show",
			now:  time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC), // saturday 21:00
			want: true,
		},
		{
			name: "inside window, at show start",
			now:  time.Date(2026, 3, 28, 20, 0, 0, 0, time.UTC), // saturday 20:00
			want: true,
		},
		{
			name: "inside window, at window start boundary",
			now:  time.Date(2026, 3, 28, 18, 0, 0, 0, time.UTC), // saturday 18:00
			want: true,
		},
		{
			name: "inside window, mid-hour",
			now:  time.Date(2026, 3, 28, 19, 30, 0, 0, time.UTC), // saturday 19:30
			want: true,
		},
		{
			name: "inside window, just before end",
			now:  time.Date(2026, 3, 28, 23, 59, 0, 0, time.UTC), // saturday 23:59
			want: true,
		},
		{
			name: "outside window, hour before start",
			now:  time.Date(2026, 3, 28, 17, 0, 0, 0, time.UTC), // saturday 17:00
			want: false,
		},
		{
			name: "outside window, at end boundary (exclusive)",
			now:  time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC), // sunday 00:00
			want: false,
		},
		{
			name: "outside window, Monday",
			now:  time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC), // monday 10:00
			want: false,
		},
		{
			name: "outside window, Saturday morning",
			now:  time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC), // saturday 10:00
			want: false,
		},
		{
			name: "outside window, Sunday afternoon",
			now:  time.Date(2026, 3, 29, 14, 0, 0, 0, time.UTC), // sunday 14:00
			want: false,
		},
		{
			name: "non-UTC timezone converted correctly (MSK inside window)",
			now:  time.Date(2026, 3, 28, 23, 0, 0, 0, time.FixedZone("MSK", 3*60*60)), // 23:00 MSK = 20:00 UTC
			want: true,
		},
		{
			name: "non-UTC timezone converted correctly (MSK outside window)",
			now:  time.Date(2026, 3, 28, 20, 0, 0, 0, time.FixedZone("MSK", 3*60*60)), // 20:00 MSK = 17:00 UTC
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, inScheduleWindow(tt.now))
		})
	}
}

func TestRun_ChapterTrackingPerRecording(t *testing.T) {
	// verify that a new tracker is created for each recording session
	var trackerCount atomic.Int32
	var recordCount atomic.Int32

	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) (string, error) {
			recordCount.Add(1)
			return testRecordedPath, nil
		},
	}

	cfg := runConfig{
		tickInterval: 10 * time.Millisecond,
		nowFn:        time.Now,
		newChapterTracker: func() chapterProvider {
			trackerCount.Add(1)
			return &mockChapterProvider{
				runCalled: make(chan struct{}),
				chapters:  nil,
			}
		},
		injectChapters: func(_ string, _ []chapters.Chapter) error {
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	run(ctx, ml, mr, cfg, &recordingState{})

	assert.Equal(t, trackerCount.Load(), recordCount.Load(),
		"a new chapter tracker should be created for each recording")
	assert.GreaterOrEqual(t, recordCount.Load(), int32(2),
		"should have recorded at least twice in 100ms with 10ms tick")
}
