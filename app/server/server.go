// Package server is a http server to view and access recorded episodes
package server

import (
	"context"
	"net/http"
	"time"
)

// Server is the main struct for the server
type Server struct {
	port     string
	dir      string
	revision string
	srv      *http.Server
}

// NewServer creates a new server and setup handler
func NewServer(port, dir, rev string) *Server {
	s := Server{
		port:     port,
		dir:      dir,
		revision: rev,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/episode/", s.DownloadEpisodeHandler)
	mux.HandleFunc("/record/", s.DownloadRecordHandler)
	mux.HandleFunc("/health", s.HealthHandler)
	mux.HandleFunc("/", s.IndexHandler)

	s.srv = &http.Server{
		Addr: ":"+port,
		Handler: mux,
		ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	return &s
}

// Start starts the server
func (s *Server) Start(_ context.Context) error {
	return s.srv.ListenAndServe()
}

// Stop shutdowns the server
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
