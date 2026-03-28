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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/didip/tollbooth/v8"
	"github.com/didip/tollbooth/v8/limiter"
	"github.com/go-pkgz/routegroup"
	"golang.org/x/crypto/bcrypt"
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
	authPasswd   string // bcrypt hash; empty means auth disabled
	forceRecord  *atomic.Bool
	recording    *atomic.Bool
	done         chan struct{}
	closeOnce    sync.Once
}

// NewServer creates a new server and sets up handlers.
// authPasswd is a bcrypt hash; when non-empty, POST /record requires authentication.
// forceRecord is an optional flag shared with the recording loop; POST /record sets it to true.
// recording is an optional flag indicating whether a stream is currently being recorded.
func NewServer(port, dir, authPasswd string, forceRecord, recording *atomic.Bool) *Server {
	t, err := template.ParseFS(indexTemplateFS, "static/index.html")
	if err != nil {
		panic(fmt.Sprintf("failed to parse index template: %v", err))
	}

	s := Server{ //nolint:exhaustruct
		dir:          dir,
		template:     t,
		warnCapacity: 80, //nolint:mnd
		authPasswd:   authPasswd,
		forceRecord:  forceRecord,
		recording:    recording,
		done:         make(chan struct{}),
	}

	router := routegroup.New(http.NewServeMux())
	router.HandleFunc("GET /episode/{folder}", s.DownloadEpisodeHandler)
	router.HandleFunc("GET /episode/{folder}/{file}", s.DownloadFileHandler)
	router.HandleFunc("GET /live/{filename...}", s.LiveStreamHandler)
	router.HandleFunc("GET /health", s.HealthHandler)
	router.HandleFunc("GET /{$}", s.IndexHandler)
	if forceRecord != nil {
		recordHandler := http.HandlerFunc(s.ForceRecordHandler)
		if authPasswd != "" {
			// apply rate limiter (1 req/sec) to prevent brute force when auth is enabled
			lmt := tollbooth.NewLimiter(1, &limiter.ExpirableOptions{DefaultExpirationTTL: time.Hour}) //nolint:exhaustruct // ExpireJobInterval is deprecated
			// periodically evict expired token buckets to prevent memory growth from one-off IPs
			go func() {
				ticker := time.NewTicker(30 * time.Minute) //nolint:mnd
				defer ticker.Stop()
				for {
					select {
					case <-s.done:
						return
					case <-ticker.C:
						lmt.DeleteExpiredTokenBuckets()
					}
				}
			}()
			router.Handle("POST /record", tollbooth.HTTPMiddleware(lmt)(recordHandler))
		} else {
			router.Handle("POST /record", recordHandler)
		}
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

// Stop shuts down the server and releases background resources
func (s *Server) Stop(ctx context.Context) error {
	s.closeOnce.Do(func() { close(s.done) })
	return s.srv.Shutdown(ctx)
}

// listEpisodes reads the directory and returns a list of episodes with their files
func listEpisodes(dir string) ([]episode, error) { //nolint:gocyclo // flat structure, readable as-is
	dirs, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading main dir: %w", err)
	}

	episodes := make([]episode, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, d.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading episode dir: %w", err)
		}
		var fileNames []string
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".tmp") {
				continue // skip transient temp files (e.g. chapter injection)
			}
			fileNames = append(fileNames, f.Name())
		}
		episodes = append(episodes, episode{Name: d.Name(), Files: fileNames})
	}

	// sort episodes numerically so "1000" comes after "999"
	sort.Slice(episodes, func(i, j int) bool {
		ni, ei := strconv.Atoi(episodes[i].Name)
		nj, ej := strconv.Atoi(episodes[j].Name)
		if ei == nil && ej == nil {
			return ni < nj
		}
		if ei != nil && ej != nil {
			return episodes[i].Name < episodes[j].Name
		}
		return ei == nil // numeric names first
	})

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
		AuthEnabled     bool
	}{
		Episodes:        episodes,
		ShowForceRecord: s.forceRecord != nil,
		Recording:       recording,
		RecordRequested: s.forceRecord != nil && s.forceRecord.Load(),
		ActiveFile:      activeFile,
		ActiveEpisode:   activeEpisode,
		AuthEnabled:     s.authPasswd != "",
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
// when authPasswd is set, requires valid password via form body or basic auth header.
func (s *Server) ForceRecordHandler(w http.ResponseWriter, r *http.Request) {
	if s.authPasswd != "" && !s.checkAuth(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.forceRecord.Store(true)
	slog.Info("force recording triggered via API")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// checkAuth validates the password from form body or basic auth header against the bcrypt hash.
func (s *Server) checkAuth(r *http.Request) bool {
	// check form value first (web UI)
	if pwd := r.PostFormValue("password"); pwd != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(s.authPasswd), []byte(pwd)); err != nil {
			slog.Warn("auth failed", slog.String("source", "form"), slog.String("err", err.Error())) //nolint:gosec // error from bcrypt is not user-controlled
			return false
		}
		return true
	}

	// check basic auth header (curl/API)
	if _, pwd, ok := r.BasicAuth(); ok {
		if err := bcrypt.CompareHashAndPassword([]byte(s.authPasswd), []byte(pwd)); err != nil {
			slog.Warn("auth failed", slog.String("source", "basic-auth"), slog.String("err", err.Error())) //nolint:gosec // error from bcrypt is not user-controlled
			return false
		}
		return true
	}

	return false
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
	return s != "" && s != "." && !strings.Contains(s, "..") && !strings.Contains(s, "/")
}

// DownloadFileHandler serves a single file from an episode directory.
// if ?download is set, forces a download instead of inline playback.
func (s *Server) DownloadFileHandler(w http.ResponseWriter, r *http.Request) {
	folder, file := r.PathValue("folder"), r.PathValue("file")
	if !validPathSegment(folder) || !validPathSegment(file) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(s.dir, folder, file)
	fi, err := os.Stat(filePath) //nolint:gosec,nolintlint // path components validated above
	if err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}

	if r.URL.Query().Has("download") {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file))
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
	return last.Name, filepath.Join(s.dir, last.Name, last.Files[len(last.Files)-1])
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

	dirPath := filepath.Join(s.dir, folder)
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
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", folder+".zip"))

	writer := zip.NewWriter(w)
	defer writer.Close() //nolint:errcheck

	entries, readErr := os.ReadDir(dirPath)
	if readErr != nil {
		slog.Error("error reading episode dir for zip", slog.String("error", readErr.Error())) //nolint:gosec // error from local filesystem
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".tmp") {
			continue // skip transient temp files (e.g. chapter injection)
		}
		fp := filepath.Join(dirPath, entry.Name())
		src, openErr := os.Open(fp) //nolint:gosec // folder validated above
		if openErr != nil {
			continue
		}
		dst, createErr := writer.Create(entry.Name())
		if createErr != nil {
			src.Close() //nolint:errcheck,gosec
			continue
		}
		io.Copy(dst, src) //nolint:errcheck,gosec
		src.Close()       //nolint:errcheck,gosec
	}
}
