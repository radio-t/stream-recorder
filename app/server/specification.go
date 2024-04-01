package server

import (
	"net/http"

	_ "embed" //nplint:golint
)

//go:embed static/index.html
var elements []byte

//go:embed static/swagger.json
var swaggerJSONFile []byte

//go:embed static/swagger.yaml
var swaggerYAMLFile []byte

func specification(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write(elements); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func swaggerJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(swaggerJSONFile); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func swaggerYAML(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	if _, err := w.Write(swaggerYAMLFile); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
