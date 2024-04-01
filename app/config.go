package main

import (
	"flag"
	"fmt"
	"os"
)

type Config struct {
	Stream string // TODO: validate:url"
	Site   string
	Dir    string
	Port   string
}

func NewConfig() Config {
	var (
		stream = flag.String("stream", "https://stream.radio-t.com", "Stream url")
		site   = flag.String("site", "https://radio-t.com/site-api/last/1", "Radio-t API")
		dir    = flag.String("dir", "./", "Recording directory")
		port   = flag.String("port", "", "If provided app will start REST API server on the port otherwise server is disabled")
	)

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, `stream-recorder is used to listen and record audio stream when it is available,
save as episodes in provided directory and naming them after calling site api

Options:`)
		flag.PrintDefaults()
	}

	flag.Parse()

	return Config{
		Stream: *stream,
		Site:   *site,
		Dir:    *dir,
		Port:   *port,
	}
}
