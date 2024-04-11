// Package server is a http server to view and access recorded episodes
package server

import (
	"net/http"
)

// Server is the main struct for the server
type Server struct {
	port     string
	dir      string
	revision string
}

// NewServer creates a new server
func NewServer(port, dir, rev string) *Server {
	return &Server{
		port:     port,
		dir:      dir,
		revision: rev,
	}
}

// Start binds endpoints with methods and starts the server
func (s *Server) Start() {
	http.HandleFunc("/episode/", s.DownloadEpisodeHandler)
	http.HandleFunc("/record/", s.DownloadRecordHandler)
	http.HandleFunc("/health", s.HealthHandler)
	http.HandleFunc("/", s.IndexHandler)
	http.ListenAndServe(":"+s.port, nil) //nolint:errcheck,gosec
}
