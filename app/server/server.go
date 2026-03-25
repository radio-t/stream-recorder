// Package server is a http server to view and access recorded episodes
package server

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-pkgz/routegroup"
)

//go:embed static/index.html
var indexTemplateFS embed.FS

// episode represents a recorded episode directory with its files
type episode struct {
	Name  string
	Files []string
}

// Server is the main struct for the server
type Server struct {
	dir          string
	srv          *http.Server
	template     *template.Template
	warnCapacity int
}

// NewServer creates a new server and sets up handlers
func NewServer(port, dir string) *Server {
	t, err := template.ParseFS(indexTemplateFS, "static/index.html")
	if err != nil {
		panic(fmt.Sprintf("failed to parse index template: %v", err))
	}

	s := Server{ //nolint:exhaustruct
		dir:          dir,
		template:     t,
		warnCapacity: 80, //nolint:mnd
	}

	router := routegroup.New(http.NewServeMux())
	router.HandleFunc("GET /episode/{folder}", s.DownloadEpisodeHandler)
	router.HandleFunc("GET /health", s.HealthHandler)
	router.HandleFunc("GET /{$}", s.IndexHandler)

	s.srv = &http.Server{ //nolint:exhaustruct
		Addr:              ":" + port,
		Handler:           router,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      0, // disable for large file downloads
		IdleTimeout:       120 * time.Second,
	}

	return &s
}

// Start starts the server
func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

// Stop shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

// listEpisodes reads the directory and returns a list of episodes with their files
func listEpisodes(dir string) ([]episode, error) {
	dirs, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading main dir: %w", err)
	}

	episodes := make([]episode, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(path.Join(dir, d.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading episode dir: %w", err)
		}
		var fileNames []string
		for _, f := range files {
			fileNames = append(fileNames, f.Name())
		}
		episodes = append(episodes, episode{Name: d.Name(), Files: fileNames})
	}
	return episodes, nil
}

// IndexHandler renders the page listing recorded episodes
func (s *Server) IndexHandler(w http.ResponseWriter, _ *http.Request) {
	episodes, err := listEpisodes(s.dir)
	if err != nil {
		slog.Error("failed to list episodes", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Episodes []episode
	}{Episodes: episodes}

	var buf bytes.Buffer
	if err := s.template.Execute(&buf, data); err != nil {
		slog.Error("failed to render template", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if _, err := buf.WriteTo(w); err != nil {
		slog.Error("error writing response", slog.String("error", err.Error()))
	}
}

// HealthHandler returns 200 if disk usage is below warning threshold
func (s *Server) HealthHandler(w http.ResponseWriter, _ *http.Request) {
	if err := s.checkHealth(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		if _, writeErr := w.Write([]byte(err.Error())); writeErr != nil {
			slog.Error("error writing response", slog.String("error", writeErr.Error()))
		}
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) checkHealth() error {
	capacity, err := getCapacity(s.dir)
	if err != nil {
		return err
	}

	if capacity > s.warnCapacity {
		return fmt.Errorf("disk is %d%% full", capacity)
	}

	return nil
}

func getCapacity(dir string) (int, error) {
	stats := &syscall.Statfs_t{} //nolint:exhaustruct

	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0, fmt.Errorf("failed to get absolute path: %w", err)
	}

	if err = syscall.Statfs(abs, stats); err != nil {
		return 0, fmt.Errorf("failed to make Statfs call: %w, path: %s", err, dir)
	}

	total := stats.Blocks * uint64(stats.Bsize) //nolint:gosec,nolintlint // bsize is always positive
	if total == 0 {
		return 0, nil
	}
	free := stats.Bfree * uint64(stats.Bsize) //nolint:gosec,nolintlint // bsize is always positive
	used := total - free
	capacity := int(used * 100 / total) //nolint:gosec // overflow is not possible here

	return capacity, nil
}

// DownloadEpisodeHandler zips files for a single episode directory and downloads it
func (s *Server) DownloadEpisodeHandler(w http.ResponseWriter, r *http.Request) {
	folder := r.PathValue("folder")
	if folder == "" || folder == "." || strings.Contains(folder, "..") {
		http.Error(w, "invalid episode", http.StatusBadRequest)
		return
	}

	dirPath := path.Join(s.dir, folder)
	fi, err := os.Stat(dirPath) //nolint:gosec,nolintlint // folder validated above: no ".." or "/"
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !fi.IsDir() {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename="+folder+".zip")

	writer := zip.NewWriter(w)
	defer writer.Close() //nolint:errcheck

	if err := writer.AddFS(os.DirFS(dirPath)); err != nil {
		slog.Error("error creating zip", slog.String("error", err.Error())) //nolint:gosec,nolintlint // error from local filesystem
	}
}
