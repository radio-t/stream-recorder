package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
)

type Episode struct {
	Name string `json:"name"`
	File string `json:"file"`
}

type Index struct {
	Episodes []Episode `json:"episodes"`
}

func (i *Index) AddEpisode(name, file string) {
	i.Episodes = append(i.Episodes, Episode{name, file})
}

func (s *Server) Index(w http.ResponseWriter, r *http.Request) {
	index := &Index{} //nolint:exhaustruct

	dirs, err := os.ReadDir(s.dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for _, dir := range dirs {
		e, err := os.ReadDir(path.Join(s.dir, dir.Name()))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, file := range e {
			index.AddEpisode(dir.Name(), path.Join(dir.Name(), file.Name()))
		}
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data) //nolint:errcheck
}
