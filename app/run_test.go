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

	run(ctx, ml, mr, cfg)
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

	run(ctx, ml, mr, cfg)
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

	run(ctx, ml, mr, cfg)
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
		forceRecord:  &forceFlag,
	}

	run(ctx, ml, mr, cfg)

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
	chapters  []recorder.Chapter
}

func (m *mockChapterProvider) Run(ctx context.Context) {
	close(m.runCalled)
	<-ctx.Done()
}

func (m *mockChapterProvider) Chapters() []recorder.Chapter {
	return m.chapters
}

func TestPollAndRecord_WithChapterTracking(t *testing.T) {
	chapters := []recorder.Chapter{
		{Title: "Topic 1", Link: "https://example.com/1", Offset: 0},
		{Title: "Topic 2", Link: "https://example.com/2", Offset: 5 * time.Minute},
	}

	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  chapters,
	}

	var injectedPath string
	var injectedChapters []recorder.Chapter

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
		injectChapters: func(path string, chs []recorder.Chapter) error {
			injectedPath = path
			injectedChapters = chs
			return nil
		},
	}

	ctx := context.Background()
	done := pollAndRecord(ctx, ml, mr, cfg)

	assert.False(t, done)
	assert.Equal(t, testRecordedPath, injectedPath, "chapters should be injected into recorded file")
	assert.Equal(t, chapters, injectedChapters, "all collected chapters should be passed to injection")
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
	done := pollAndRecord(ctx, ml, mr, cfg)

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
		injectChapters: func(_ string, _ []recorder.Chapter) error {
			injectionCalled = true
			return nil
		},
	}

	ctx := context.Background()
	pollAndRecord(ctx, ml, mr, cfg)

	assert.False(t, injectionCalled, "injection should not be called when no chapters collected")
}

func TestPollAndRecord_ChapterTrackingRecordingFails(t *testing.T) {
	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  []recorder.Chapter{{Title: "Topic", Offset: 0}},
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
		injectChapters: func(_ string, _ []recorder.Chapter) error {
			injectionCalled = true
			return nil
		},
	}

	ctx := context.Background()
	done := pollAndRecord(ctx, ml, mr, cfg)

	assert.False(t, done)
	assert.False(t, injectionCalled, "injection should not be called when recording fails")
}

func TestPollAndRecord_ChapterInjectionError(t *testing.T) {
	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  []recorder.Chapter{{Title: "Topic", Offset: 0}},
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
		injectChapters: func(_ string, _ []recorder.Chapter) error {
			return fmt.Errorf("injection failed")
		},
	}

	ctx := context.Background()
	done := pollAndRecord(ctx, ml, mr, cfg)

	// injection error should be logged but not stop the recording loop
	assert.False(t, done, "injection error should not stop the recording loop")
}

func TestPollAndRecord_ChapterInjectionOnContextCancel(t *testing.T) {
	// verify chapters are injected when recording is stopped by context cancellation (SIGINT)
	chapters := []recorder.Chapter{
		{Title: "Topic 1", Link: "https://example.com/1", Offset: 0},
		{Title: "Topic 2", Link: "https://example.com/2", Offset: 5 * time.Minute},
	}

	tracker := &mockChapterProvider{
		runCalled: make(chan struct{}),
		chapters:  chapters,
	}

	var injectedPath string
	var injectedChapters []recorder.Chapter

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
		injectChapters: func(path string, chs []recorder.Chapter) error {
			injectedPath = path
			injectedChapters = chs
			return nil
		},
	}

	done := pollAndRecord(ctx, ml, mr, cfg)

	assert.True(t, done, "should signal clean shutdown")
	assert.Equal(t, testRecordedPath, injectedPath, "chapters should be injected even on context cancellation")
	assert.Equal(t, chapters, injectedChapters, "all collected chapters should be passed to injection")
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
		injectChapters: func(_ string, _ []recorder.Chapter) error {
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	run(ctx, ml, mr, cfg)

	assert.Equal(t, trackerCount.Load(), recordCount.Load(),
		"a new chapter tracker should be created for each recording")
	assert.GreaterOrEqual(t, recordCount.Load(), int32(2),
		"should have recorded at least twice in 100ms with 10ms tick")
}
