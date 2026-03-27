package recorder

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

	baseTime := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
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
				return Article{Title: "First Topic", Link: "https://example.com/1"}, nil
			case testTopic2:
				return Article{Title: "Second Topic", Link: "https://example.com/2"}, nil
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

	tracker := NewChapterTracker(mock, nowFn)
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

	tracker := NewChapterTracker(mock, nowFn)
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

	tracker := NewChapterTracker(mock, time.Now)

	done := make(chan struct{})
	go func() {
		tracker.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Run returned as expected
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

	tracker := NewChapterTracker(mock, nowFn)
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

	tracker := NewChapterTracker(mock, nowFn)
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

	tracker := NewChapterTracker(mock, nowFn)
	tracker.Run(ctx)

	// should only have the first chapter; second was skipped due to article fetch error
	chapters := tracker.Chapters()
	require.Len(t, chapters, 1)
	assert.Equal(t, "First", chapters[0].Title)
}

func TestChapterTracker_ChaptersThreadSafe(t *testing.T) {
	t.Parallel()

	tracker := NewChapterTracker(nil, time.Now)

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
