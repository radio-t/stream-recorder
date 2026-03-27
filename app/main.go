// Main entrypoint to run stream-recorder
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/radio-t/stream-recorder/app/recorder"
	"github.com/radio-t/stream-recorder/app/server"
)

var opts struct { //nolint:gochecknoglobals
	Stream        string `default:"https://stream.radio-t.com"                                                     description:"Stream url"                                       env:"STREAM"         long:"stream"         short:"s"`
	Site          string `default:"https://radio-t.com/site-api/last/1"                                            description:"Radio-t API"                                      env:"SITE"           long:"site"`
	Dir           string `default:"./"                                                                             description:"Recording directory"                              env:"DIR"            long:"dir"            short:"d"`
	Port          string `description:"If provided will start API server on the port otherwise server is disabled" env:"PORT"                                                    long:"port"           short:"p"`
	Dbg           bool   `description:"Enable debug logging"                                                       env:"DBG"                                                     long:"dbg"`
	Schedule      bool   `description:"Enable time-based recording (Sat 20:00 UTC, 2h before / 4h after)"         env:"SCHEDULE"                                                long:"schedule"`
	RetentionDays int    `default:"30"                                                                             description:"Delete recordings older than N days, 0=disabled" env:"RETENTION_DAYS" long:"retention-days"`
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

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var forceRecord, recording atomic.Bool

	client := recorder.NewClient(http.DefaultClient, opts.Stream, opts.Site)
	rec := recorder.NewRecorder(opts.Dir)
	listener := recorder.NewListener(client)

	wg := sync.WaitGroup{}
	startServer(ctx, &wg, opts.Port, opts.Dir, &forceRecord, &recording)
	startPurge(ctx, &wg, purgeConfig{
		dir:           opts.Dir,
		retentionDays: opts.RetentionDays,
		interval:      24 * time.Hour, //nolint:mnd
		nowFn:         time.Now,
	})

	cfg := runConfig{
		schedule:     opts.Schedule,
		tickInterval: 5 * time.Second, //nolint:mnd
		nowFn:        time.Now,
		forceRecord:  &forceRecord,
		recording:    &recording,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		run(ctx, listener, rec, cfg)
	}()

	wg.Wait()
}

// streamListener listens for an available stream.
type streamListener interface {
	Listen(ctx context.Context) (*recorder.Stream, error)
}

// streamRecorder records a stream to disk.
type streamRecorder interface {
	Record(ctx context.Context, s *recorder.Stream) error
}

// runConfig holds configuration for the recording loop.
type runConfig struct {
	schedule     bool // enable time-based recording window
	tickInterval time.Duration
	nowFn        func() time.Time
	forceRecord  *atomic.Bool
	recording    *atomic.Bool
}

// startServer starts the HTTP server if a port is configured.
func startServer(ctx context.Context, wg *sync.WaitGroup, port, dir string, forceRecord, recording *atomic.Bool) {
	if port == "" {
		return
	}
	slog.Info("Healthcheck enabled")

	s := server.NewServer(port, dir, forceRecord, recording)
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

func run(ctx context.Context, l streamListener, r streamRecorder, cfg runConfig) {
	ticker := time.NewTicker(cfg.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down")
			return
		case <-ticker.C:
			if done := pollAndRecord(ctx, l, r, cfg); done {
				return
			}
		}
	}
}

// pollAndRecord checks the schedule, listens for a stream and records it.
// returns true when the context is cancelled and the loop should exit.
func pollAndRecord(ctx context.Context, l streamListener, r streamRecorder, cfg runConfig) bool {
	forced := cfg.forceRecord != nil && cfg.forceRecord.Load()
	if !forced && cfg.schedule && !recorder.InScheduleWindow(cfg.nowFn()) {
		slog.Debug("outside recording window")
		return false
	}

	stream, err := l.Listen(ctx)
	switch {
	case errors.Is(err, recorder.ErrNotFound):
		slog.Debug("stream is not available")
		return false
	case err != nil:
		slog.Error("error while listening", slog.String("err", err.Error()))
	default:
		return recordStream(ctx, r, stream, cfg, forced)
	}
	return false
}

// recordStream records a single stream session, managing force and recording flags.
// returns true when the context is cancelled and the loop should exit.
func recordStream(ctx context.Context, r streamRecorder, stream *recorder.Stream, cfg runConfig, forced bool) bool {
	if forced {
		cfg.forceRecord.Store(false)
	}
	if cfg.recording != nil {
		cfg.recording.Store(true)
		defer cfg.recording.Store(false)
	}
	if err := r.Record(ctx, stream); err != nil {
		if ctx.Err() != nil {
			return true // clean shutdown
		}
		slog.Error("error while recording", slog.String("err", err.Error()))
	}
	return false
}
