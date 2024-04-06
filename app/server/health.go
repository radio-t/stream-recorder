package server

import (
	"fmt"
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

// Health handler for server
func (s *Server) HealthHandler(w http.ResponseWriter, _ *http.Request) {
	warning, err := s.checkHealth()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error())) //nolint:errcheck
		return
	}

	if warning != "" {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(warning)) //nolint:errcheck
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) getCapacity() (int, error) {
	stats := &syscall.Statfs_t{} //nolint:exhaustruct

	abs, err := filepath.Abs(s.dir)
	if err != nil {
		return 0, fmt.Errorf("failed to get absolute path: %w", err)
	}

	err = syscall.Statfs(abs, stats)
	if err != nil {
		return 0, fmt.Errorf("failed to make Statfs call: %w, path: %s", err, s.dir)
	}

	onehundred := 100.0
	blocks := float64(stats.Blocks)
	free := float64(stats.Bfree)
	capacity := ((blocks - free) / blocks) * onehundred

	return int(capacity), nil
}
