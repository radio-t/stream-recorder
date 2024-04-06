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
	"github.com/radio-t/stream-recorder/app/server"
)

var opts struct {
	Stream string `short:"s" long:"stream" env:"STREAM" default:"https://stream.radio-t.com" description:"Stream url"`
	Site   string `long:"site" env:"SITE" default:"https://radio-t.com/site-api/last/1" description:"Radio-t API"`
	Dir    string `short:"d" long:"dir" env:"DIR" default:"./" description:"Recording directory"`
	Port   string `short:"p" long:"port" env:"PORT" description:"If provided app will start REST API server on the port otherwise server is disabled"`
}

//go:generate swag init  --output server/static --outputTypes yaml,json

var revision = "local" //nolint: gochecknoglobals

// @title			stream-recorder
// @description	stream-recorder is a tool to record live streams.
func main() {
	if revision == "local" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	if _, err := flags.Parse(&opts); err != nil {
		slog.Error("[ERROR] failed to parse flags: %v", err)
		os.Exit(1)
	}

	slog.Info("Starting stream-recorder", slog.String("revision", revision))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	myclient := NewClient(opts.Stream, opts.Site)

	recorder := NewRecorder(opts.Dir)

	streamlistener := NewListener(myclient)

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
		Run(ctx, streamlistener, recorder)
	}()

	wg.Wait()
}

func Run(ctx context.Context, l *Listener, r *Recorder) {
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
			case errors.Is(err, ErrNotFound):
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
