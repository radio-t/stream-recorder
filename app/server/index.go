package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
)

type episode struct {
	Name string `json:"name"`
	File string `json:"file"`
}

type index struct {
	episodes []episode `json:"episodes"` //nolint:govet
}

func (i *index) addEpisode(name, file string) {
	i.episodes = append(i.episodes, episode{name, file})
}

// IndexHandler to view recorded episodes
func (s *Server) IndexHandler(w http.ResponseWriter, _ *http.Request) {
	index := &index{} //nolint:exhaustruct

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
			index.addEpisode(dir.Name(), path.Join(dir.Name(), file.Name()))
		}
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data) //nolint:errcheck,gosec
}
