package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"syscall"
)

func (s *Server) checkHealth() (string, error) {
	warncapacity := 80

	capacity, err := s.getCapacity()
	if err != nil {
		return "", err
	}

	if capacity > warncapacity {
		return fmt.Sprintf("disk is %d%% full", capacity), nil
	}

	return "", nil
}

// HealthHandler for server
func (s *Server) HealthHandler(w http.ResponseWriter, _ *http.Request) {
	warning, err := s.checkHealth()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, err = w.Write([]byte(err.Error()))
		if err != nil {
			slog.Error("error writing response:", slog.String("", err.Error()))
		}
		return
	}

	if warning != "" {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte(warning))
		if err != nil {
			slog.Error("error writing response:", slog.String("", err.Error()))
		}
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) getCapacity() (int, error) {
	stats := &syscall.Statfs_t{}

	abs, err := filepath.Abs(s.dir)
	if err != nil {
		return 0, fmt.Errorf("failed to get absolute path: %w", err)
	}

	err = syscall.Statfs(abs, stats)
	if err != nil {
		return 0, fmt.Errorf("failed to make Statfs call: %w, path: %s", err, s.dir)
	}

	total := stats.Blocks * uint64(stats.Bsize)
	free := stats.Bfree * uint64(stats.Bsize)
	used := total - free

	capacity := int(used * 100 / total)

	return capacity, nil
}
