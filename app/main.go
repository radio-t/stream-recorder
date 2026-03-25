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
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/radio-t/stream-recorder/app/recorder"
	"github.com/radio-t/stream-recorder/app/server"
)

var opts struct { //nolint:gochecknoglobals
	Stream string `default:"https://stream.radio-t.com"                                                     description:"Stream url"          env:"STREAM" long:"stream" short:"s"`
	Site   string `default:"https://radio-t.com/site-api/last/1"                                            description:"Radio-t API"         env:"SITE"   long:"site"`
	Dir    string `default:"./"                                                                             description:"Recording directory" env:"DIR"    long:"dir"    short:"d"`
	Port   string `description:"If provided will start API server on the port otherwise server is disabled" env:"PORT"                        long:"port"  short:"p"`
	Dbg    bool   `description:"Enable debug logging"                                                       env:"DBG"                         long:"dbg"`
}

var revision = "local" //nolint: gochecknoglobals

func main() {
	if _, err := flags.Parse(&opts); err != nil {
		slog.Error("failed to parse flags", slog.String("err", err.Error()))
		os.Exit(1)
	}

	if opts.Dbg {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	slog.Info("Starting stream-recorder", slog.String("revision", revision))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	client := recorder.NewClient(http.DefaultClient, opts.Stream, opts.Site)
	rec := recorder.NewRecorder(opts.Dir)
	listener := recorder.NewListener(client)

	wg := sync.WaitGroup{}

	if opts.Port != "" {
		slog.Info("Healthcheck enabled")

		s := server.NewServer(opts.Port, opts.Dir)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("server error", slog.String("error", err.Error()))
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.Stop(shutdownCtx); err != nil {
				slog.Error("server shutdown error", slog.String("error", err.Error()))
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		run(ctx, listener, rec)
	}()

	wg.Wait()
}

func run(ctx context.Context, l *recorder.Listener, r *recorder.Recorder) {
	ticker := time.NewTicker(time.Second * 5) //nolint:mnd
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down")
			return
		case <-ticker.C:
			stream, err := l.Listen(ctx)
			switch {
			case errors.Is(err, recorder.ErrNotFound):
				slog.Debug("stream is not available")

			case err != nil:
				slog.Error("error while listening", slog.String("err", err.Error()))

			default:
				err = r.Record(ctx, stream)
				if err != nil {
					slog.Error("error while recording", slog.String("err", err.Error()))
					return
				}
			}
		}
	}
}
