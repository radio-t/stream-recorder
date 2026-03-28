// Main entrypoint to run stream-recorder
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/bcrypt"

	"github.com/radio-t/stream-recorder/app/chapters"
	"github.com/radio-t/stream-recorder/app/recorder"
	"github.com/radio-t/stream-recorder/app/server"
)

var opts struct { //nolint:gochecknoglobals
	Stream        string `default:"https://stream.radio-t.com"                                                     description:"Stream url"                                                    env:"STREAM"         long:"stream"         short:"s"`
	Site          string `default:"https://radio-t.com/site-api/last/1"                                            description:"Radio-t API"                                                   env:"SITE"           long:"site"`
	Dir           string `default:"./"                                                                             description:"Recording directory"                                           env:"DIR"            long:"dir"            short:"d"`
	Port          string `description:"If provided will start API server on the port otherwise server is disabled" env:"PORT"                                                                  long:"port"           short:"p"`
	Dbg           bool   `description:"Enable debug logging"                                                       env:"DBG"                                                                   long:"dbg"`
	Schedule      bool   `description:"Enable time-based recording (Sat 20:00 UTC, 2h before / 4h after)"         env:"SCHEDULE"                                                              long:"schedule"`
	RetentionDays int    `default:"30"                                                                             description:"Delete recordings older than N days, 0=disabled"                env:"RETENTION_DAYS" long:"retention-days"`
	NewsAPI       string `default:"https://news.radio-t.com/api/v1"                                                description:"News API base URL for chapter markers, empty to disable"       env:"NEWS_API"       long:"news"`
	AuthPasswd    string `description:"bcrypt hash of password for POST /record auth"                                 env:"AUTH_PASSWD"                                                            long:"auth-passwd"`
}

var revision = "local" //nolint: gochecknoglobals

func main() {
	if _, err := flags.Parse(&opts); err != nil {
		var flagErr *flags.Error
		if errors.As(err, &flagErr) && flagErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		slog.Error("failed to parse flags", slog.String("err", err.Error()))
		os.Exit(1)
	}

	if opts.Dbg {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	slog.Info("Starting stream-recorder", slog.String("revision", revision))

	if opts.Schedule {
		slog.Info("Schedule enabled: recording window Sat 18:00-00:00 UTC")
	}

	if opts.RetentionDays < 0 {
		slog.Error("retention-days must be non-negative", slog.Int("value", opts.RetentionDays))
		os.Exit(1)
	}

	if opts.AuthPasswd != "" {
		if _, err := bcrypt.Cost([]byte(opts.AuthPasswd)); err != nil {
			slog.Error("auth-passwd is not a valid bcrypt hash", slog.String("err", err.Error()))
			os.Exit(1)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var forceRecord, recording atomic.Bool

	client := recorder.NewClient(http.DefaultClient, opts.Stream, opts.Site)
	rec := recorder.NewRecorder(opts.Dir, func() { recording.Store(true) })
	listener := recorder.NewListener(client)

	wg := sync.WaitGroup{}
	startServer(ctx, &wg, opts.Port, opts.Dir, opts.AuthPasswd, &forceRecord, &recording, makeScheduleStatusFn(opts.Schedule))
	startPurge(ctx, &wg, purgeConfig{
		dir:           opts.Dir,
		retentionDays: opts.RetentionDays,
		interval:      24 * time.Hour, //nolint:mnd
		nowFn:         time.Now,
	})

	cfg := newRunConfig(opts.Schedule, opts.NewsAPI)
	state := &recordingState{forceRecord: &forceRecord, recording: &recording} //nolint:exhaustruct // wasInWindow, pollCount start at zero

	wg.Add(1)
	go func() {
		defer wg.Done()
		run(ctx, listener, rec, cfg, state)
	}()

	wg.Wait()
}

// streamListener listens for an available stream.
type streamListener interface {
	Listen(ctx context.Context) (*recorder.Stream, error)
}

// streamRecorder records a stream to disk.
type streamRecorder interface {
	Record(ctx context.Context, s *recorder.Stream) (string, error)
}

// chapterProvider tracks chapter changes during recording.
type chapterProvider interface {
	Run(ctx context.Context)
	Chapters() []chapters.Chapter
}

// runConfig holds immutable configuration for the recording loop.
type runConfig struct {
	schedule          bool // enable time-based recording window
	tickInterval      time.Duration
	nowFn             func() time.Time
	newChapterTracker func() chapterProvider                 // nil = chapter tracking disabled
	injectChapters    func(string, []chapters.Chapter) error // defaults to chapters.InjectChapters
}

// recordingState holds mutable runtime state for the recording loop.
type recordingState struct {
	forceRecord *atomic.Bool
	recording   *atomic.Bool
	wasInWindow bool // tracks previous window state for transition logging
	pollCount   int  // counts polls within current state for throttled debug logging
}

// newRunConfig creates a runConfig with standard defaults, optionally enabling chapter tracking.
func newRunConfig(schedule bool, newsAPI string) runConfig {
	cfg := runConfig{ //nolint:exhaustruct // newChapterTracker and injectChapters set conditionally below
		schedule:     schedule,
		tickInterval: 5 * time.Second, //nolint:mnd
		nowFn:        time.Now,
	}
	if newsAPI != "" {
		slog.Info("Chapter tracking enabled", slog.String("news_api", newsAPI))
		cfg.newChapterTracker = func() chapterProvider {
			nc := chapters.NewNewsClient(http.DefaultClient, newsAPI)
			return chapters.NewChapterTracker(nc, time.Now)
		}
		cfg.injectChapters = chapters.InjectChapters
	}
	return cfg
}

// startServer starts the HTTP server if a port is configured.
func startServer(ctx context.Context, wg *sync.WaitGroup, port, dir, authPasswd string,
	forceRecord, recording *atomic.Bool, scheduleFn func() server.ScheduleStatus) {
	if port == "" {
		return
	}
	slog.Info("Healthcheck enabled")

	s := server.NewServer(port, dir, authPasswd, forceRecord, recording, scheduleFn)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", slog.String("error", err.Error()))
		}
	}()

	wg.Add(1)
	go func() { //nolint:gosec // need fresh context for graceful shutdown after cancellation
		defer wg.Done()
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Stop(shutdownCtx); err != nil {
			slog.Error("server shutdown error", slog.String("error", err.Error()))
		}
	}()
}

// purgeConfig holds configuration for the auto-purge goroutine.
type purgeConfig struct {
	dir           string
	retentionDays int
	interval      time.Duration
	nowFn         func() time.Time
}

// startPurge runs purge immediately on startup and then periodically at the configured interval.
// does nothing when retentionDays is 0 or negative.
func startPurge(ctx context.Context, wg *sync.WaitGroup, cfg purgeConfig) {
	if cfg.retentionDays <= 0 {
		return
	}

	slog.Info("Auto-purge enabled",
		slog.Int("retention_days", cfg.retentionDays),
		slog.Duration("interval", cfg.interval))

	// run on startup
	if err := recorder.Purge(cfg.dir, cfg.retentionDays, cfg.nowFn()); err != nil {
		slog.Error("startup purge failed", slog.String("err", err.Error()))
	} else {
		slog.Info("startup purge completed")
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(cfg.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := recorder.Purge(cfg.dir, cfg.retentionDays, cfg.nowFn()); err != nil {
					slog.Error("periodic purge failed", slog.String("err", err.Error()))
				} else {
					slog.Info("periodic purge completed")
				}
			}
		}
	}()
}

// hoursInWeek is the number of hours in a week
const hoursInWeek = 7 * 24

// schedule constants for Radio-T show: Saturday 20:00 UTC, 2h before, 4h after
// see https://radio-t.com/online/
const (
	showDay         = time.Saturday
	showHour        = 20
	showBeforeHours = 2
	showAfterHours  = 4
)

// inScheduleWindow checks whether now falls within the recording window
// around the Radio-T show (Saturday 20:00 UTC, recording from 18:00 to 00:00 Sunday).
func inScheduleWindow(now time.Time) bool {
	schedHour := int(showDay)*24 + showHour
	startHour := (schedHour - showBeforeHours + hoursInWeek) % hoursInWeek
	endHour := (schedHour + showAfterHours) % hoursInWeek

	now = now.UTC()
	nowHour := int(now.Weekday())*24 + now.Hour()

	if startHour <= endHour {
		return nowHour >= startHour && nowHour < endHour
	}
	// window wraps around the week boundary (Saturday evening to Sunday morning)
	return nowHour >= startHour || nowHour < endHour
}

// timeToShow returns the duration until the show starts (Saturday showHour UTC).
// returns 0 if the show has already started or it's not Saturday.
func timeToShow(now time.Time) time.Duration {
	now = now.UTC()
	if now.Weekday() != showDay {
		return 0
	}
	showStart := time.Date(now.Year(), now.Month(), now.Day(), showHour, 0, 0, 0, time.UTC)
	if !showStart.After(now) {
		return 0
	}
	return showStart.Sub(now)
}

// timeToNextWindow returns the duration until the next recording window opens.
func timeToNextWindow(now time.Time) time.Duration {
	now = now.UTC()
	windowStartHour := showHour - showBeforeHours
	daysUntilSat := int((showDay - now.Weekday() + 7) % 7)
	windowStart := time.Date(now.Year(), now.Month(), now.Day()+daysUntilSat, windowStartHour, 0, 0, 0, time.UTC)
	if !windowStart.After(now) {
		windowStart = windowStart.AddDate(0, 0, 7)
	}
	return windowStart.Sub(now)
}

// fmtDuration formats a duration as a human-readable string like "1h32m" or "45m".
func fmtDuration(d time.Duration) string {
	d = d.Truncate(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60 //nolint:mnd
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// fmtBytes formats a byte count as a human-readable string.
func fmtBytes(b int64) string {
	const (
		mb = 1024 * 1024
		gb = 1024 * 1024 * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(mb))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// makeScheduleStatusFn returns a function for the server to query schedule window status.
// returns nil when schedule is disabled.
func makeScheduleStatusFn(schedule bool) func() server.ScheduleStatus {
	if !schedule {
		return nil
	}
	return func() server.ScheduleStatus {
		now := time.Now()
		if !inScheduleWindow(now) {
			return server.ScheduleStatus{} //nolint:exhaustruct // zero value means outside window
		}
		status := "show in progress"
		if ttShow := timeToShow(now); ttShow > 0 {
			status = fmtDuration(ttShow) + " to show"
		}
		return server.ScheduleStatus{InWindow: true, ShowStatus: status}
	}
}

// loopAction indicates whether the recording loop should continue or stop.
type loopAction bool

const (
	continueLoop loopAction = false
	stopLoop     loopAction = true
)

func run(ctx context.Context, l streamListener, r streamRecorder, cfg runConfig, state *recordingState) {
	ticker := time.NewTicker(cfg.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down")
			return
		case <-ticker.C:
			if pollAndRecord(ctx, l, r, cfg, state) == stopLoop {
				return
			}
		}
	}
}

// debugLogInterval controls how often repetitive debug messages are emitted (every N polls).
// with a 5s tick interval this means debug messages appear roughly every minute.
const debugLogInterval = 12

// logWindowTransitions detects and logs recording window entry/exit transitions.
// returns whether the current time is inside the recording window.
func logWindowTransitions(cfg runConfig, state *recordingState) bool {
	inWindow := !cfg.schedule || inScheduleWindow(cfg.nowFn())
	if !cfg.schedule {
		return inWindow
	}

	if inWindow && !state.wasInWindow {
		state.pollCount = 0
		if ttShow := timeToShow(cfg.nowFn()); ttShow > 0 {
			slog.Info("entered recording window", slog.String("show_in", fmtDuration(ttShow)))
		} else {
			slog.Info("entered recording window, show in progress")
		}
	} else if !inWindow && state.wasInWindow {
		state.pollCount = 0
		slog.Info("exited recording window")
	}
	state.wasInWindow = inWindow
	return inWindow
}

// pollAndRecord checks the schedule, listens for a stream and records it.
// returns stopLoop when the context is cancelled and the loop should exit.
func pollAndRecord(ctx context.Context, l streamListener, r streamRecorder, cfg runConfig, state *recordingState) loopAction {
	forced := state.forceRecord != nil && state.forceRecord.Load()
	inWindow := logWindowTransitions(cfg, state)

	if !forced && !inWindow {
		state.pollCount++
		if state.pollCount == 1 || state.pollCount%debugLogInterval == 0 {
			slog.Debug("outside recording window", slog.String("next_in", fmtDuration(timeToNextWindow(cfg.nowFn()))))
		}
		return continueLoop
	}

	stream, err := l.Listen(ctx)
	switch {
	case errors.Is(err, recorder.ErrNotFound):
		state.pollCount++
		if state.pollCount == 1 || state.pollCount%debugLogInterval == 0 {
			slog.Debug("waiting for stream", slog.Int("checks", state.pollCount))
		}
		return continueLoop
	case err != nil:
		slog.Error("error while listening", slog.String("err", err.Error()))
	default:
		state.pollCount = 0
		return recordStream(ctx, r, stream, cfg, state, forced)
	}
	return continueLoop
}

// recordStream records a single stream session, managing force and recording flags.
// when chapter tracking is configured, starts a tracker goroutine alongside the recording
// and injects collected chapters into the file after recording completes.
// returns stopLoop when the context is cancelled and the loop should exit.
func recordStream(ctx context.Context, r streamRecorder, stream *recorder.Stream, cfg runConfig, state *recordingState, forced bool) loopAction {
	if forced {
		state.forceRecord.Store(false)
	}

	// start chapter tracking if configured
	var tracker chapterProvider
	var trackerCancel context.CancelFunc
	var trackerDone chan struct{}
	if cfg.newChapterTracker != nil {
		tracker = cfg.newChapterTracker()
		var trackerCtx context.Context
		trackerCtx, trackerCancel = context.WithCancel(ctx)
		trackerDone = make(chan struct{})
		go func() {
			defer close(trackerDone)
			tracker.Run(trackerCtx)
		}()
	}

	startTime := cfg.nowFn()
	filePath, err := r.Record(ctx, stream)
	duration := cfg.nowFn().Sub(startTime)

	// stop chapter tracker and wait for it to finish before reading chapters
	if trackerCancel != nil {
		trackerCancel()
		<-trackerDone
	}

	// clear recording flag before chapter injection so live stream clients
	// stop reading the file before it gets rewritten with chapter frames
	if state.recording != nil {
		state.recording.Store(false)
	}

	if err != nil {
		if ctx.Err() != nil {
			logRecordingFinished("recording stopped", stream.Number, filePath, duration)
			if filePath != "" {
				maybeInjectChapters(tracker, filePath, cfg.injectChapters)
			}
			return stopLoop // clean shutdown
		}
		slog.Error("error while recording", slog.String("err", err.Error()))
		slog.Info("stream lost, waiting for reconnect", slog.String("episode", stream.Number))
		return continueLoop
	}

	logRecordingFinished("finished recording", stream.Number, filePath, duration)
	maybeInjectChapters(tracker, filePath, cfg.injectChapters)
	return continueLoop
}

// logRecordingFinished logs a recording completion message with duration and file size.
func logRecordingFinished(msg, episode, filePath string, duration time.Duration) {
	attrs := []any{slog.String("episode", episode), slog.String("duration", fmtDuration(duration))}
	if filePath != "" {
		if fi, err := os.Stat(filePath); err == nil {
			attrs = append(attrs, slog.String("size", fmtBytes(fi.Size())))
		}
	}
	slog.Info(msg, attrs...)
}

// maybeInjectChapters injects collected chapter markers into the recorded file.
// does nothing when tracker is nil or no chapters were collected.
func maybeInjectChapters(tracker chapterProvider, filePath string, inject func(string, []chapters.Chapter) error) {
	if tracker == nil || inject == nil {
		return
	}
	chaps := tracker.Chapters()
	if len(chaps) == 0 {
		return
	}
	if err := inject(filePath, chaps); err != nil {
		slog.Error("failed to inject chapters", slog.String("err", err.Error()))
	} else {
		slog.Info("injected chapter markers", slog.Int("count", len(chaps)))
	}
}
