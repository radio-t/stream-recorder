package recorder

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Chapter represents a single chapter marker with its title, link, and offset from recording start.
type Chapter struct {
	Title  string
	Link   string
	Offset time.Duration
}

// NewsProvider provides news data for chapter tracking.
//
//go:generate moq -out newsprovider_moq_test.go . NewsProvider
type NewsProvider interface {
	FetchActiveID(ctx context.Context) (string, error)
	FetchArticle(ctx context.Context, id string) (Article, error)
	WaitActiveChange(ctx context.Context, timeout time.Duration) (string, error)
}

// ChapterTracker collects chapter markers by long-polling the news API for topic changes.
type ChapterTracker struct {
	news       NewsProvider
	nowFn      func() time.Time
	retryDelay time.Duration

	mu       sync.RWMutex
	chapters []Chapter
}

// NewChapterTracker creates a new chapter tracker with the given news provider and time function.
func NewChapterTracker(news NewsProvider, nowFn func() time.Time) *ChapterTracker {
	return &ChapterTracker{ //nolint:exhaustruct // mu and chapters use zero values
		news:       news,
		nowFn:      nowFn,
		retryDelay: 5 * time.Second,
	}
}

// Run starts tracking topic changes. It blocks until ctx is cancelled.
func (ct *ChapterTracker) Run(ctx context.Context) {
	startTime := ct.nowFn()
	activeID := ct.fetchInitialTopic(ctx, startTime)
	if ctx.Err() != nil {
		return
	}
	ct.pollForChanges(ctx, startTime, activeID)
}

// fetchInitialTopic fetches the current active topic and records it as the first chapter.
func (ct *ChapterTracker) fetchInitialTopic(ctx context.Context, startTime time.Time) string {
	id, err := ct.news.FetchActiveID(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("failed to fetch initial active topic", "error", err)
		}
		return ""
	}
	ct.fetchAndAddChapter(ctx, id, startTime, startTime)
	return id
}

// pollForChanges long-polls the news API for topic changes until ctx is cancelled.
func (ct *ChapterTracker) pollForChanges(ctx context.Context, startTime time.Time, activeID string) {
	for {
		newID, err := ct.news.WaitActiveChange(ctx, 30*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("failed to wait for topic change", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(ct.retryDelay):
				continue
			}
		}

		if newID == activeID {
			continue
		}
		activeID = newID
		ct.fetchAndAddChapter(ctx, newID, startTime, ct.nowFn())
	}
}

// Chapters returns the collected chapter markers (thread-safe).
func (ct *ChapterTracker) Chapters() []Chapter {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	result := make([]Chapter, len(ct.chapters))
	copy(result, ct.chapters)
	return result
}

// fetchAndAddChapter fetches article details and adds a chapter with the calculated offset.
func (ct *ChapterTracker) fetchAndAddChapter(ctx context.Context, id string, startTime, now time.Time) {
	article, err := ct.news.FetchArticle(ctx, id)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("failed to fetch article", "id", id, "error", err)
		}
		return
	}

	ct.mu.Lock()
	ct.chapters = append(ct.chapters, Chapter{
		Title:  article.Title,
		Link:   article.Link,
		Offset: now.Sub(startTime),
	})
	ct.mu.Unlock()
}
