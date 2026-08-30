package chapters

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testTopic1 = "topic1"
	testTopic2 = "topic2"
)

func TestChapterTracker_CollectsTopicChanges(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 3, 27, 20, 30, 0, 0, time.UTC) // during the show
	var currentTime atomic.Int64
	currentTime.Store(baseTime.UnixNano())
	nowFn := func() time.Time { return time.Unix(0, currentTime.Load()) }

	var waitCalls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return testTopic1, nil
		},
		FetchArticleFunc: func(_ context.Context, id string) (Article, error) {
			switch id {
			case testTopic1:
				return Article{Title: "First Topic", Link: "https://example.com/1",
					ActiveTS: baseTime.Add(-5 * time.Minute)}, nil // activated at 20:25, after show start
			case testTopic2:
				return Article{Title: "Second Topic", Link: "https://example.com/2",
					ActiveTS: baseTime.Add(10 * time.Minute)}, nil
			default:
				return Article{}, fmt.Errorf("unknown article %s", id)
			}
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			call := waitCalls.Add(1)
			switch call {
			case 1:
				// simulate 10 minutes passing, topic changes
				currentTime.Store(baseTime.Add(10 * time.Minute).UnixNano())
				return testTopic2, nil
			default:
				// cancel after collecting second chapter
				cancel()
				return "", ctx.Err()
			}
		},
	}

	tracker := NewChapterTracker(mock, nowFn, 20)
	tracker.Run(ctx)

	chapters := tracker.Chapters()
	require.Len(t, chapters, 2)

	assert.Equal(t, "First Topic", chapters[0].Title)
	assert.Equal(t, "https://example.com/1", chapters[0].Link)
	assert.Equal(t, time.Duration(0), chapters[0].Offset)

	assert.Equal(t, "Second Topic", chapters[1].Title)
	assert.Equal(t, "https://example.com/2", chapters[1].Link)
	assert.Equal(t, 10*time.Minute, chapters[1].Offset)
}

func TestChapterTracker_StaleTopicAddsIntro(t *testing.T) {
	t.Parallel()

	// recording starts at 20:15 UTC (15 min into the show window)
	baseTime := time.Date(2026, 3, 28, 20, 15, 0, 0, time.UTC)
	var currentTime atomic.Int64
	currentTime.Store(baseTime.UnixNano())
	nowFn := func() time.Time { return time.Unix(0, currentTime.Load()) }

	var waitCalls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return testTopic1, nil
		},
		FetchArticleFunc: func(_ context.Context, id string) (Article, error) {
			switch id {
			case testTopic1:
				// topic was activated at 19:00 — before show start (20:00), stale
				return Article{Title: "Old Topic", Link: "https://example.com/old",
					ActiveTS: time.Date(2026, 3, 28, 19, 0, 0, 0, time.UTC)}, nil
			case testTopic2:
				// topic activated at 20:30 — during the show, valid
				return Article{Title: "New Topic", Link: "https://example.com/new",
					ActiveTS: time.Date(2026, 3, 28, 20, 30, 0, 0, time.UTC)}, nil
			default:
				return Article{}, fmt.Errorf("unknown article %s", id)
			}
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			call := waitCalls.Add(1)
			if call == 1 {
				currentTime.Store(baseTime.Add(15 * time.Minute).UnixNano())
				return testTopic2, nil
			}
			cancel()
			return "", ctx.Err()
		},
	}

	tracker := NewChapterTracker(mock, nowFn, 20)
	tracker.Run(ctx)

	chapters := tracker.Chapters()
	require.Len(t, chapters, 2)

	assert.Equal(t, "Вступление", chapters[0].Title, "topic from before show start should be replaced with intro")
	assert.Empty(t, chapters[0].Link, "intro chapter should have no link")
	assert.Equal(t, time.Duration(0), chapters[0].Offset)

	assert.Equal(t, "New Topic", chapters[1].Title)
	assert.Equal(t, 15*time.Minute, chapters[1].Offset)
}

func TestChapterTracker_RestartKeepsCurrentTopic(t *testing.T) {
	// recorder restarts at 21:00, topic was set at 20:15 (during the show) — should keep it
	t.Parallel()

	baseTime := time.Date(2026, 3, 28, 21, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return testTopic1, nil
		},
		FetchArticleFunc: func(_ context.Context, _ string) (Article, error) {
			// topic set at 20:15 — after show start, valid even though 45min before restart
			return Article{Title: "Current Topic", Link: "https://example.com/current",
				ActiveTS: time.Date(2026, 3, 28, 20, 15, 0, 0, time.UTC)}, nil
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			cancel()
			return "", ctx.Err()
		},
	}

	tracker := NewChapterTracker(mock, func() time.Time { return baseTime }, 20)
	tracker.Run(ctx)

	chapters := tracker.Chapters()
	require.Len(t, chapters, 1)
	assert.Equal(t, "Current Topic", chapters[0].Title, "topic activated during show should be kept on restart")
}

func TestChapterTracker_APIErrorRetry(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return baseTime }

	var waitCalls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return testTopic1, nil
		},
		FetchArticleFunc: func(_ context.Context, id string) (Article, error) {
			return Article{Title: "Topic " + id, Link: "https://example.com/" + id}, nil
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			call := waitCalls.Add(1)
			switch call {
			case 1:
				return "", fmt.Errorf("connection refused")
			case 2:
				return testTopic2, nil // successful retry
			default:
				cancel()
				return "", ctx.Err()
			}
		},
	}

	tracker := NewChapterTracker(mock, nowFn, 20)
	tracker.retryDelay = time.Millisecond // fast retry for test
	tracker.Run(ctx)

	// should have retried after the error and recovered the chapter
	assert.GreaterOrEqual(t, int(waitCalls.Load()), 2, "should have retried after error")
	chapters := tracker.Chapters()
	require.Len(t, chapters, 2, "should have initial chapter + recovered chapter after retry")
	assert.Equal(t, "Topic "+testTopic1, chapters[0].Title)
	assert.Equal(t, "Topic "+testTopic2, chapters[1].Title)
}

func TestChapterTracker_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(ctx context.Context) (string, error) {
			return "", ctx.Err()
		},
		FetchArticleFunc: func(_ context.Context, _ string) (Article, error) {
			return Article{}, fmt.Errorf("should not be called")
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			return "", fmt.Errorf("should not be called")
		},
	}

	tracker := NewChapterTracker(mock, time.Now, 20)

	done := make(chan struct{})
	go func() {
		tracker.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// run returned as expected
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	assert.Empty(t, tracker.Chapters())
}

func TestChapterTracker_SameIDNoNewChapter(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return baseTime }

	var waitCalls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return testTopic1, nil
		},
		FetchArticleFunc: func(_ context.Context, _ string) (Article, error) {
			return Article{Title: "Topic One", Link: "https://example.com/1"}, nil
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			call := waitCalls.Add(1)
			if call == 1 {
				return testTopic1, nil // same ID, no change
			}
			cancel()
			return "", ctx.Err()
		},
	}

	tracker := NewChapterTracker(mock, nowFn, 20)
	tracker.Run(ctx)

	chapters := tracker.Chapters()
	require.Len(t, chapters, 1, "should only have the initial chapter, not a duplicate")
	assert.Equal(t, "Topic One", chapters[0].Title)
}

func TestChapterTracker_InitialFetchError(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
	var currentTime atomic.Int64
	currentTime.Store(baseTime.UnixNano())
	nowFn := func() time.Time { return time.Unix(0, currentTime.Load()) }

	var waitCalls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return "", fmt.Errorf("service unavailable")
		},
		FetchArticleFunc: func(_ context.Context, _ string) (Article, error) {
			return Article{Title: "Later Topic", Link: "https://example.com/2"}, nil
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			call := waitCalls.Add(1)
			if call == 1 {
				currentTime.Store(baseTime.Add(5 * time.Minute).UnixNano())
				return testTopic2, nil
			}
			cancel()
			return "", ctx.Err()
		},
	}

	tracker := NewChapterTracker(mock, nowFn, 20)
	tracker.Run(ctx)

	// should still collect chapters from long-poll even if initial fetch failed
	chapters := tracker.Chapters()
	require.Len(t, chapters, 1)
	assert.Equal(t, "Later Topic", chapters[0].Title)
	assert.Equal(t, 5*time.Minute, chapters[0].Offset)
}

func TestChapterTracker_ArticleFetchError(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return baseTime }

	var waitCalls atomic.Int32
	var articleCalls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return testTopic1, nil
		},
		FetchArticleFunc: func(_ context.Context, _ string) (Article, error) {
			call := articleCalls.Add(1)
			if call == 1 {
				return Article{Title: "First", Link: "https://example.com/1"}, nil
			}
			// fail on subsequent article fetches
			return Article{}, fmt.Errorf("article fetch failed")
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			call := waitCalls.Add(1)
			if call == 1 {
				return testTopic2, nil // article fetch will fail for this
			}
			cancel()
			return "", ctx.Err()
		},
	}

	tracker := NewChapterTracker(mock, nowFn, 20)
	tracker.Run(ctx)

	// only the first chapter; the second topic stays pending and is never fetched successfully
	chapters := tracker.Chapters()
	require.Len(t, chapters, 1)
	assert.Equal(t, "First", chapters[0].Title)
}

func TestChapterTracker_RetriesFailedArticleFetch(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 3, 28, 20, 30, 0, 0, time.UTC) // during the show
	var currentTime atomic.Int64
	currentTime.Store(baseTime.UnixNano())
	nowFn := func() time.Time { return time.Unix(0, currentTime.Load()) }

	var waitCalls, topic2Calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return testTopic1, nil
		},
		FetchArticleFunc: func(_ context.Context, id string) (Article, error) {
			switch id {
			case testTopic1:
				return Article{Title: "First Topic", Link: "https://example.com/1", ActiveTS: baseTime}, nil
			case testTopic2:
				if topic2Calls.Add(1) == 1 {
					return Article{}, fmt.Errorf("article fetch failed")
				}
				return Article{Title: "Second Topic", Link: "https://example.com/2",
					ActiveTS: baseTime.Add(10 * time.Minute)}, nil
			default:
				return Article{}, fmt.Errorf("unknown article %s", id)
			}
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			switch waitCalls.Add(1) {
			case 1: // topic changes, its article fetch fails
				currentTime.Store(baseTime.Add(10 * time.Minute).UnixNano())
				return testTopic2, nil
			case 2: // same topic reported again, article fetch succeeds this time
				currentTime.Store(baseTime.Add(25 * time.Minute).UnixNano())
				return testTopic2, nil
			default:
				cancel()
				return "", ctx.Err()
			}
		},
	}

	tracker := NewChapterTracker(mock, nowFn, 20)
	tracker.Run(ctx)

	chapters := tracker.Chapters()
	require.Len(t, chapters, 2, "the retried topic should be recorded")
	assert.Equal(t, "First Topic", chapters[0].Title)
	assert.Equal(t, "Second Topic", chapters[1].Title)
	assert.Equal(t, "https://example.com/2", chapters[1].Link)
	assert.Equal(t, 10*time.Minute, chapters[1].Offset, "offset should be the first observation, not the retry")
}

func TestChapterTracker_PendingTopicResetByAnotherTopic(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 3, 28, 20, 30, 0, 0, time.UTC) // during the show
	var currentTime atomic.Int64
	currentTime.Store(baseTime.UnixNano())
	nowFn := func() time.Time { return time.Unix(0, currentTime.Load()) }

	var waitCalls, topic2Calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	mock := &NewsProviderMock{
		FetchActiveIDFunc: func(_ context.Context) (string, error) {
			return testTopic1, nil
		},
		FetchArticleFunc: func(_ context.Context, id string) (Article, error) {
			switch id {
			case testTopic1:
				return Article{Title: "First Topic", Link: "https://example.com/1", ActiveTS: baseTime}, nil
			case testTopic2:
				if topic2Calls.Add(1) == 1 {
					return Article{}, fmt.Errorf("article fetch failed")
				}
				return Article{Title: "Second Topic", Link: "https://example.com/2",
					ActiveTS: baseTime.Add(40 * time.Minute)}, nil
			default:
				return Article{}, fmt.Errorf("unknown article %s", id)
			}
		},
		WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
			switch waitCalls.Add(1) {
			case 1: // topic 2 becomes active, its article fetch fails
				currentTime.Store(baseTime.Add(10 * time.Minute).UnixNano())
				return testTopic2, nil
			case 2: // topic 1 is active again, ending topic 2's activation
				currentTime.Store(baseTime.Add(12 * time.Minute).UnixNano())
				return testTopic1, nil
			case 3: // topic 2 activated afresh, article fetch succeeds
				currentTime.Store(baseTime.Add(40 * time.Minute).UnixNano())
				return testTopic2, nil
			default:
				cancel()
				return "", ctx.Err()
			}
		},
	}

	tracker := NewChapterTracker(mock, nowFn, 20)
	tracker.Run(ctx)

	chapters := tracker.Chapters()
	require.Len(t, chapters, 2)
	assert.Equal(t, "Second Topic", chapters[1].Title)
	assert.Equal(t, 40*time.Minute, chapters[1].Offset,
		"a re-activated topic should use its new offset, not the one from the failed attempt")
}

func TestChapterTracker_RetriesFailedInitialArticleFetch(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2026, 3, 28, 20, 30, 0, 0, time.UTC) // during the show
	showStart := time.Date(2026, 3, 28, 20, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		activeTS  time.Time
		wantTitle string
		wantLink  string
	}{
		{
			name:      "topic activated during the show",
			activeTS:  showStart.Add(15 * time.Minute),
			wantTitle: "First Topic",
			wantLink:  "https://example.com/1",
		},
		{
			name:      "topic predating the show still becomes the intro chapter",
			activeTS:  showStart.Add(-time.Hour),
			wantTitle: introChapterTitle,
			wantLink:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var currentTime atomic.Int64
			currentTime.Store(baseTime.UnixNano())
			nowFn := func() time.Time { return time.Unix(0, currentTime.Load()) }

			var waitCalls, articleCalls atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())

			mock := &NewsProviderMock{
				FetchActiveIDFunc: func(_ context.Context) (string, error) {
					return testTopic1, nil
				},
				FetchArticleFunc: func(_ context.Context, _ string) (Article, error) {
					if articleCalls.Add(1) == 1 {
						return Article{}, fmt.Errorf("article fetch failed")
					}
					return Article{Title: "First Topic", Link: "https://example.com/1", ActiveTS: tt.activeTS}, nil
				},
				WaitActiveChangeFunc: func(_ context.Context, _ time.Duration) (string, error) {
					if waitCalls.Add(1) == 1 { // same topic still active, retry succeeds
						currentTime.Store(baseTime.Add(5 * time.Minute).UnixNano())
						return testTopic1, nil
					}
					cancel()
					return "", ctx.Err()
				},
			}

			tracker := NewChapterTracker(mock, nowFn, 20)
			tracker.Run(ctx)

			chapters := tracker.Chapters()
			require.Len(t, chapters, 1, "the initial topic should be recovered on retry")
			assert.Equal(t, tt.wantTitle, chapters[0].Title)
			assert.Equal(t, tt.wantLink, chapters[0].Link)
			assert.Equal(t, time.Duration(0), chapters[0].Offset, "recovered initial topic starts at the recording start")
		})
	}
}

func TestChapterTracker_ChaptersThreadSafe(t *testing.T) {
	t.Parallel()

	tracker := NewChapterTracker(nil, time.Now, 20)

	// concurrently read Chapters while adding chapters
	done := make(chan struct{})
	go func() {
		for i := range 100 {
			tracker.mu.Lock()
			tracker.chapters = append(tracker.chapters, Chapter{
				Title:  fmt.Sprintf("Chapter %d", i),
				Offset: time.Duration(i) * time.Minute,
			})
			tracker.mu.Unlock()
		}
		close(done)
	}()

	// read concurrently
	for range 50 {
		_ = tracker.Chapters()
	}
	<-done

	chapters := tracker.Chapters()
	assert.Len(t, chapters, 100)
}

func TestChapterTracker_IsStaleTopicForShow(t *testing.T) {
	t.Parallel()

	// zones the recorder may run in when TZ is not forced to UTC
	bst := time.FixedZone("BST", 1*60*60)
	msk := time.FixedZone("MSK", 3*60*60)
	cdt := time.FixedZone("CDT", -5*60*60) // the live deployment's zone

	showStart := time.Date(2026, 3, 28, 20, 0, 0, 0, time.UTC) // Saturday 20:00 UTC

	tests := []struct {
		name           string
		activeTS       time.Time
		recordingStart time.Time
		want           bool
	}{
		{
			name:           "zero timestamp is never stale",
			activeTS:       time.Time{},
			recordingStart: showStart.Add(30 * time.Minute),
			want:           false,
		},
		{
			name:           "activated an hour before the show",
			activeTS:       showStart.Add(-time.Hour),
			recordingStart: showStart.Add(30 * time.Minute),
			want:           true,
		},
		{
			name:           "activated during the show",
			activeTS:       showStart.Add(15 * time.Minute),
			recordingStart: showStart.Add(30 * time.Minute),
			want:           false,
		},
		{
			name:           "activated exactly at the show start",
			activeTS:       showStart,
			recordingStart: showStart.Add(30 * time.Minute),
			want:           false,
		},
		{
			name:           "late recording start, already next day in a +1 zone",
			activeTS:       showStart.Add(15 * time.Minute),
			recordingStart: showStart.Add(3*time.Hour + 30*time.Minute).In(bst), // Sun 00:30 BST
			want:           false,
		},
		{
			name:           "late recording start, already next day in a +3 zone",
			activeTS:       showStart.Add(15 * time.Minute),
			recordingStart: showStart.Add(3*time.Hour + 30*time.Minute).In(msk), // Sun 02:30 MSK
			want:           false,
		},
		{
			// the live deployment runs at UTC-5 and records the show from about 19:56 UTC,
			// so a session started manually after midnight UTC still belongs to that show
			name:           "recording started after midnight UTC belongs to the evening's show",
			activeTS:       showStart.Add(2 * time.Hour),
			recordingStart: showStart.Add(5*time.Hour + 30*time.Minute).In(cdt), // Sun 01:30 UTC, Sat 20:30 local
			want:           false,
		},
		{
			name:           "leftover topic is still stale for a recording started after midnight UTC",
			activeTS:       showStart.Add(-2 * time.Hour),
			recordingStart: showStart.Add(5*time.Hour + 30*time.Minute).In(cdt),
			want:           true,
		},
		{
			name:           "recording started long after the show belongs to that show, not the next",
			activeTS:       showStart.Add(time.Hour),
			recordingStart: showStart.Add(11 * time.Hour), // Sun 07:00 UTC
			want:           false,
		},
		{
			name:           "stale topic still detected in a +3 zone past local midnight",
			activeTS:       showStart.Add(-time.Hour),
			recordingStart: showStart.Add(3*time.Hour + 30*time.Minute).In(msk),
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tracker := NewChapterTracker(nil, time.Now, 20)
			assert.Equal(t, tt.want, tracker.isStaleTopicForShow(tt.activeTS, tt.recordingStart))
		})
	}
}
