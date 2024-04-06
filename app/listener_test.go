package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

)

func TestListener(t *testing.T) {
	apiServer := createTestAPIServer()
	defer apiServer.Close()

	testcases := []struct {
		name                 string
		streamServerFunction func(w http.ResponseWriter, r *http.Request)
		expected             string
		errorFunc            assert.ErrorAssertionFunc
	}{
		{
			name: "happy test",
			streamServerFunction: func(w http.ResponseWriter, r *http.Request) {
				random := rand.New(rand.NewSource(0)) //nolint:gosec
				fmt.Fprintf(w, "%d", random.Int())
			},
			expected:  "8717895732",
			errorFunc: assert.NoError,
		},
		{
			name: "404 test",
			streamServerFunction: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			errorFunc: assert.Error,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			streamServer := prepareStreamServer(tc.streamServerFunction)
			defer streamServer.Close()

			c := NewClient(streamServer.URL, apiServer.URL)

			l := NewListener(c)

			s, err := l.Listen(context.TODO())

			tc.errorFunc(t, err)
			if err != nil {
				return
			}

			assert.NotNil(t, s.Body)

			defer s.Body.Close()

			buf := make([]byte, 10)
			_, err = s.Body.Read(buf)
			require.NoError(t, err)

			got := string(buf)

			assert.Equal(t, tc.expected, got, fmt.Sprintf(`expected stream of: %q but got: %q`, tc.expected, got))
		})
	}
}

func prepareStreamServer(f func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(f))
}

func createTestAPIServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testepisode := &Entry{Title: "streamrecorderepisode test"}
		entries := []Entry{*testepisode}
		data, err := json.Marshal(entries)
		if err != nil {
			w.Write([]byte(err.Error())) //nolint:errcheck
			w.WriteHeader(http.StatusInternalServerError)
		}

		w.WriteHeader(http.StatusOK)
		w.Write(data) //nolint:errcheck
	}))
}
