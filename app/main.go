// Main entrypoint to run stream-recorder
package main

import (
	"context"
	"errors"
	"log/slog"
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
	Stream string `default:"https://stream.radio-t.com" description:"Stream url" env:"STREAM" long:"stream" short:"s"`
	Site   string `default:"https://radio-t.com/site-api/last/1" description:"Radio-t API" env:"SITE" long:"site"`
	Dir    string `default:"./" description:"Recording directory" env:"DIR" long:"dir" short:"d"`
	Port   string `description:"If provided will start API server on the port otherwise server is disabled" env:"PORT" long:"port" short:"p"`
}

var revision = "local" //nolint: gochecknoglobals

func main() {
	if revision == "local" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	if _, err := flags.Parse(&opts); err != nil {
		slog.Error("failed to parse flags", slog.String("err", err.Error()))
		os.Exit(1)
	}

	slog.Info("Starting stream-recorder", slog.String("revision", revision))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	myclient := recorder.NewClient(opts.Stream, opts.Site)

	r := recorder.NewRecorder(opts.Dir)

	streamlistener := recorder.NewListener(myclient)

	wg := sync.WaitGroup{}

	if opts.Port != "" {
		slog.Info("Healthcheck enabled")

		wg.Add(1)
		go func() {
			defer wg.Done()
			s := server.NewServer(opts.Port, opts.Dir, revision)
			go s.Start()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		run(ctx, streamlistener, r)
	}()

	wg.Wait()
}

func run(ctx context.Context, l *recorder.Listener, r *recorder.Recorder) {
	ticker := time.NewTicker(time.Second * 5) //nolint:gomnd
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
