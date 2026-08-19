// Package chapters provides chapter tracking for recorded streams using the Radio-T news API.
package chapters

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// introChapterTitle is used in place of a topic that was already active before the show started.
const introChapterTitle = "Вступление"

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
	news          NewsProvider
	nowFn         func() time.Time
	showStartHour int // hour (UTC) when the show starts, for stale topic detection
	retryDelay    time.Duration

	mu       sync.RWMutex
	chapters []Chapter
}

// NewChapterTracker creates a new chapter tracker with the given news provider, time function
// and show start hour (UTC). Topics activated before the show start hour are treated as stale.
func NewChapterTracker(news NewsProvider, nowFn func() time.Time, showStartHour int) *ChapterTracker {
	return &ChapterTracker{ //nolint:exhaustruct // mu and chapters use zero values
		news:          news,
		nowFn:         nowFn,
		showStartHour: showStartHour,
		retryDelay:    5 * time.Second,
	}
}

// Run starts tracking topic changes. It blocks until ctx is cancelled.
func (ct *ChapterTracker) Run(ctx context.Context) {
	startTime := ct.nowFn()
	activeID, pendingID := ct.fetchInitialTopic(ctx, startTime)
	if ctx.Err() != nil {
		return
	}
	ct.pollForChanges(ctx, startTime, activeID, pendingID)
}

// fetchInitialTopic fetches the current active topic and records it as the first chapter.
// if the topic was activated before the show start (20:00 UTC), it's a leftover and we insert
// a "Вступление" intro chapter instead. This works correctly on restarts because a topic set
// during the show (e.g. at 20:15) will have ActiveTS after 20:00.
// returns the id of the recorded topic, or an empty id plus the pending one when the article
// fetch failed, so the topic can still be picked up by a later poll.
func (ct *ChapterTracker) fetchInitialTopic(ctx context.Context, startTime time.Time) (activeID, pendingID string) {
	id, err := ct.news.FetchActiveID(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("failed to fetch initial active topic", "error", err)
		}
		return "", ""
	}

	if !ct.fetchAndAddChapter(ctx, id, startTime, 0, true) {
		return "", id
	}
	return id, ""
}

// isStaleTopicForShow checks if a topic was activated before the show started.
// the show starts at showStartHour UTC on the same day as the recording.
// returns true if the topic predates the show start, meaning it's a leftover.
func (ct *ChapterTracker) isStaleTopicForShow(activeTS, recordingStart time.Time) bool {
	if activeTS.IsZero() {
		return false // no timestamp available, assume topic is current
	}
	showStart := time.Date(
		recordingStart.Year(), recordingStart.Month(), recordingStart.Day(),
		ct.showStartHour, 0, 0, 0, time.UTC)
	return activeTS.Before(showStart)
}

// pollForChanges long-polls the news API for topic changes until ctx is cancelled.
// a topic whose article could not be fetched stays pending: activeID is left untouched so the
// next poll reporting the same id retries it, keeping the offset of the first observation.
func (ct *ChapterTracker) pollForChanges(ctx context.Context, startTime time.Time, activeID, pendingID string) {
	var pendingOffset time.Duration
	pendingIsInitial := pendingID != ""

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

		offset, initial := ct.nowFn().Sub(startTime), false
		if newID == pendingID {
			offset, initial = pendingOffset, pendingIsInitial
		}
		// any other id ends the pending topic's activation, so if it comes back later
		// it starts a fresh chapter instead of reusing the offset of the failed attempt
		pendingID, pendingOffset, pendingIsInitial = "", 0, false

		if newID == activeID {
			continue
		}

		if ct.fetchAndAddChapter(ctx, newID, startTime, offset, initial) {
			activeID = newID
			continue
		}
		pendingID, pendingOffset, pendingIsInitial = newID, offset, initial
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

// fetchAndAddChapter fetches article details and appends a chapter at the given offset.
// when initial is set, a topic activated before the show start is replaced with an intro chapter.
// returns false when the article could not be fetched, leaving the chapter unrecorded so the
// caller can retry the same id later.
func (ct *ChapterTracker) fetchAndAddChapter(ctx context.Context, id string, startTime time.Time,
	offset time.Duration, initial bool) bool {
	article, err := ct.news.FetchArticle(ctx, id)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("failed to fetch article", "id", id, "error", err)
		}
		return false
	}

	chapter := Chapter{Title: article.Title, Link: article.Link, Offset: offset}
	if initial && ct.isStaleTopicForShow(article.ActiveTS, startTime) {
		chapter = Chapter{Title: introChapterTitle, Link: "", Offset: offset}
		slog.Info("initial topic predates show, added intro chapter",
			"active_since", article.ActiveTS.Format(time.RFC3339),
			"recording_start", startTime.Format(time.RFC3339))
	}

	ct.mu.Lock()
	ct.chapters = append(ct.chapters, chapter)
	ct.mu.Unlock()
	return true
}
