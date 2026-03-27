// Package server is a http server to view and access recorded episodes
package server

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	forceRecord  *atomic.Bool
	recording    *atomic.Bool
}

// NewServer creates a new server and sets up handlers.
// forceRecord is an optional flag shared with the recording loop; POST /record sets it to true.
// recording is an optional flag indicating whether a stream is currently being recorded.
func NewServer(port, dir string, forceRecord, recording *atomic.Bool) *Server {
	t, err := template.ParseFS(indexTemplateFS, "static/index.html")
	if err != nil {
		panic(fmt.Sprintf("failed to parse index template: %v", err))
	}

	s := Server{ //nolint:exhaustruct
		dir:          dir,
		template:     t,
		warnCapacity: 80, //nolint:mnd
		forceRecord:  forceRecord,
		recording:    recording,
	}

	router := routegroup.New(http.NewServeMux())
	router.HandleFunc("GET /episode/{folder}", s.DownloadEpisodeHandler)
	router.HandleFunc("GET /episode/{folder}/{file}", s.DownloadFileHandler)
	router.HandleFunc("GET /live/{filename...}", s.LiveStreamHandler)
	router.HandleFunc("GET /health", s.HealthHandler)
	router.HandleFunc("GET /{$}", s.IndexHandler)
	if forceRecord != nil {
		router.HandleFunc("POST /record", s.ForceRecordHandler)
	}

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

	recording := s.recording != nil && s.recording.Load()

	var activeFile, activeEpisode string
	if recording && len(episodes) > 0 {
		last := episodes[len(episodes)-1]
		activeEpisode = last.Name
		if len(last.Files) > 0 {
			activeFile = last.Name + "/" + last.Files[len(last.Files)-1]
		}
	}

	data := struct {
		Episodes        []episode
		ShowForceRecord bool
		Recording       bool
		RecordRequested bool
		ActiveFile      string
		ActiveEpisode   string
	}{
		Episodes:        episodes,
		ShowForceRecord: s.forceRecord != nil,
		Recording:       recording,
		RecordRequested: s.forceRecord != nil && s.forceRecord.Load(),
		ActiveFile:      activeFile,
		ActiveEpisode:   activeEpisode,
	}

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

// ForceRecordHandler sets the force-record flag to trigger recording outside the schedule window.
func (s *Server) ForceRecordHandler(w http.ResponseWriter, r *http.Request) {
	s.forceRecord.Store(true)
	slog.Info("force recording triggered via API")
	http.Redirect(w, r, "/", http.StatusSeeOther)
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

// validPathSegment returns true if the segment is safe to use in a file path.
func validPathSegment(s string) bool {
	return s != "" && s != "." && !strings.Contains(s, "..")
}

// DownloadFileHandler serves a single file from an episode directory.
// if ?download is set, forces a download instead of inline playback.
func (s *Server) DownloadFileHandler(w http.ResponseWriter, r *http.Request) {
	folder, file := r.PathValue("folder"), r.PathValue("file")
	if !validPathSegment(folder) || !validPathSegment(file) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	filePath := path.Join(s.dir, folder, file)
	fi, err := os.Stat(filePath) //nolint:gosec,nolintlint // path components validated above
	if err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}

	if r.URL.Query().Has("download") {
		w.Header().Set("Content-Disposition", "attachment; filename="+file)
	}
	http.ServeFile(w, r, filePath)
}

// LiveStreamHandler streams the currently recording file from its current write position.
// returns 404 if nothing is being recorded.
func (s *Server) LiveStreamHandler(w http.ResponseWriter, r *http.Request) {
	if s.recording == nil || !s.recording.Load() {
		http.Error(w, "not recording", http.StatusNotFound)
		return
	}

	_, filePath := s.activeFileInfo()
	if filePath == "" {
		http.Error(w, "no active file", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(filePath) //nolint:gosec // path from listEpisodes, not user input
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close() //nolint:errcheck

	// seek to near the end to start from "now" rather than the beginning
	fi, err := f.Stat()
	if err == nil && fi.Size() > 0 {
		f.Seek(fi.Size(), io.SeekStart) //nolint:errcheck,gosec // best-effort seek
	}

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-cache, no-store")

	s.tailFile(f, w, r, flusher)
}

// activeFileInfo returns the episode name and absolute path of the file currently being recorded.
func (s *Server) activeFileInfo() (episodeName, filePath string) {
	episodes, err := listEpisodes(s.dir)
	if err != nil || len(episodes) == 0 {
		return "", ""
	}
	last := episodes[len(episodes)-1]
	if len(last.Files) == 0 {
		return "", ""
	}
	return last.Name, path.Join(s.dir, last.Name, last.Files[len(last.Files)-1])
}

// tailFile reads from f and writes to w, waiting for more data when EOF is reached
// as long as recording is active.
func (s *Server) tailFile(f *os.File, w http.ResponseWriter, r *http.Request, flusher http.Flusher) {
	buf := make([]byte, 32*1024) //nolint:mnd
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return // client disconnected
			}
			flusher.Flush()
		}

		switch {
		case readErr == nil:
			continue
		case readErr != io.EOF:
			return // real read error
		case s.recording == nil || !s.recording.Load():
			return // recording finished, file is complete
		}

		// at EOF but still recording — wait for more data
		select {
		case <-r.Context().Done():
			return
		case <-time.After(250 * time.Millisecond): //nolint:mnd
		}
	}
}

// DownloadEpisodeHandler zips files for a single episode directory and downloads it
func (s *Server) DownloadEpisodeHandler(w http.ResponseWriter, r *http.Request) {
	folder := r.PathValue("folder")
	if !validPathSegment(folder) {
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
