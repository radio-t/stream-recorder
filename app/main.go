package main

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/radio-t/stream-recorder/app/client"
	"github.com/radio-t/stream-recorder/app/server"
)

//go:generate swag init  --output server/static --outputTypes yaml,json

var revision = "local" //nolint: gochecknoglobals

// @title			stream-recorder
// @description	stream-recorder is a tool to record live streams.
func main() {
	if revision == "local" {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	config := NewConfig()

	slog.Info("Starting stream-recorder", slog.String("revision", revision))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	myclient := client.NewClient(config.Stream, config.Site)

	recorder := NewRecorder(config.Dir)

	streamlistener := NewListener(myclient)

	wg := sync.WaitGroup{}

	if config.Port != "" {
		slog.Info("Healthcheck enabled")

		wg.Add(1)
		go func() {
			defer wg.Done()
			s := server.NewServer(config.Port, config.Dir, revision)
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
			case errors.Is(err, client.ErrNotFound):
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
