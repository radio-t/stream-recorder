package server

import (
	"net/http"
)

type Server struct {
	port     string
	dir      string
	revision string
}

func NewServer(port, dir, rev string) *Server {
	return &Server{
		port:     port,
		dir:      dir,
		revision: rev,
	}
}

func (s *Server) Start() {
	http.Handle("/download/", http.StripPrefix("/download/", http.FileServer(http.Dir(s.dir))))
	http.HandleFunc("/health", s.HealthHandler)
	http.HandleFunc("/records", s.Index)
	http.ListenAndServe(":"+s.port, nil) //nolint:errcheck,gosec
}
