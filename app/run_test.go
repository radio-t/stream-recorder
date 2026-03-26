package main

import (
	"context"
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
	recordFn func(ctx context.Context, s *recorder.Stream) error
}

func (m *mockStreamRecorder) Record(ctx context.Context, s *recorder.Stream) error {
	if m.recordFn != nil {
		return m.recordFn(ctx, s)
	}
	return nil
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
	// Saturday 21:00 UTC — inside the hardcoded window (Sat 18:00 - Sun 00:00)
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
	// Monday 10:00 UTC — outside the hardcoded window
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
	// Monday 10:00 UTC — outside the hardcoded window
	fixedTime := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)

	var recorded atomic.Bool
	ml := &mockStreamListener{
		listenFn: func(_ context.Context) (*recorder.Stream, error) {
			return &recorder.Stream{Number: "999", Body: io.NopCloser(strings.NewReader("test"))}, nil
		},
	}
	mr := &mockStreamRecorder{
		recordFn: func(_ context.Context, _ *recorder.Stream) error {
			recorded.Store(true)
			return nil
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
