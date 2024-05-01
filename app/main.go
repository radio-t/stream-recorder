// Main entrypoint to run stream-recorder
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/radio-t/stream-recorder/app/recorder"
	"github.com/radio-t/stream-recorder/app/server"
)

var opts struct {
	Stream string `default:"https://stream.radio-t.com" description:"Stream url" env:"STREAM" long:"stream" short:"s"`
	Site   string `default:"https://radio-t.com/site-api/last/1" description:"Radio-t API" env:"SITE" long:"site"`
	Dir    string `default:"./" description:"Recording directory" env:"DIR" long:"dir" short:"d"`
	Port   string `description:"If provided will start API server on the port otherwise server is disabled" env:"PORT" long:"port" short:"p"`
	Debug  bool   `description:"Enable debug logging" env:"DEBUG" long:"dbg" short:"D"`
}

var revision = "local"

func main() {
	if _, err := flags.Parse(&opts); err != nil {
		slog.Error("[ERROR] failed to parse flags: %v", err)
		os.Exit(1)
	}

	if opts.Debug {
		slog.Info("debug mode")
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	slog.Info("Starting stream-recorder", slog.String("revision", revision))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	c := recorder.NewClient(http.DefaultClient, opts.Stream, opts.Site)

	l := recorder.NewListener(c)

	if opts.Port != "" {
		slog.Info("Healthcheck enabled")

		go func() {
			s := server.NewServer(opts.Port, opts.Dir, revision)
			err := s.Start(ctx)

			if err != nil {
				slog.Error("Server", err)
				cancel()
			}

			if err = s.Stop(ctx); err != nil {
				slog.Error("Server stopping error", err)
			}
		}()
	}

	run(ctx, l, recorder.NewRecorder(opts.Dir))
}

func run(ctx context.Context, l *recorder.Listener, r *recorder.Recorder) {
	ticker := time.NewTicker(time.Second * 5)
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
				slog.Error("error while listening", err)

			default:
				err = r.Record(ctx, stream)
				if err != nil {
					slog.Error("error while recording", err)
					return
				}
			}
		}
	}
}
