package server

import (
	"archive/zip"
	"net/http"
	"os"
	"path"
	"strings"
)

// DownloadRecordHandler allows user to download a single record
func (s *Server) DownloadRecordHandler(w http.ResponseWriter, r *http.Request) {
	file, _ := strings.CutPrefix(r.URL.Path, "/record/")
	if file == "" {
		http.Error(w, "Invalid file", http.StatusBadRequest)
		return
	}
	s.downloadFile(w, r, file)
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request, file string) {
	filePath := path.Join(s.dir, file)

	w.Header().Set("Content-Disposition", "attachment; filename="+file)
	w.Header().Set("Content-Type", "application/octet-stream")

	http.ServeFile(w, r, filePath)
}

// DownloadEpisodeHandler zips files for a signle episode directory and downloads it to user
func (s *Server) DownloadEpisodeHandler(w http.ResponseWriter, r *http.Request) {
	folder, _ := strings.CutPrefix(r.URL.Path, "/episode/")

	fs := os.DirFS(path.Join(s.dir, folder))

	writer := zip.NewWriter(w)
	defer writer.Close() //nolint: errcheck

	err := writer.AddFS(fs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename="+folder+".zip")
}
