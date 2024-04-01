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
} //	@name	Episode

type Index struct {
	Episodes []Episode `json:"episodes"`
} //	@name	Index

func (i *Index) AddEpisode(name, file string) {
	i.Episodes = append(i.Episodes, Episode{name, file})
}

// Index
//
//	@Router			 /records [get]
//	@Summary		 Records list
//	@Description Provides list of recorded episodes.
//	@Produce		 json
//	@Success		 200	{object}	Index
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
