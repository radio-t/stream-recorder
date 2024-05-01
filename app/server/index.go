package server

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path"
)

//go:embed static/index.html
var indexTemplateFS embed.FS

type episode struct {
	Name  string
	Files []string
}

func newIndex(p string) (*index, error) {
	i := &index{
		Episodes: make(map[string]*episode),
	}

	dirs, err := os.ReadDir(p)
	if err != nil {
		return &index{}, fmt.Errorf("error reading main dir, %w", err)
	}

	for _, dir := range dirs {
		e, err := os.ReadDir(path.Join(p, dir.Name()))
		if err != nil {
			return &index{}, fmt.Errorf("error reading episode dir, %w", err)
		}
		for _, file := range e {
			i.addEpisode(dir.Name(), path.Join(dir.Name(), file.Name()))
		}
	}

	return i, nil
}

type index struct {
	Episodes map[string]*episode `json:"episodes"`
}

func (i *index) addEpisode(name, p string) {
	e, ok := i.Episodes[name]
	if ok {
		e.Files = append(e.Files, p)
	} else {
		i.Episodes[name] = &episode{
			Name:  name,
			Files: []string{p},
		}
	}
}

// IndexHandler to view recorded episodes
func (s *Server) IndexHandler(w http.ResponseWriter, _ *http.Request) {
	data, err := newIndex(s.dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	t, err := template.ParseFS(indexTemplateFS, "static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := t.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
