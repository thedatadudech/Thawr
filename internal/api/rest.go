package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
)

// RESTDeps are the collaborators of the REST handler.
type RESTDeps struct {
	Status StatusSource
	// UI is the embedded admin UI root (index.html at the top).
	UI     fs.FS
	Logger *slog.Logger
}

// NewREST returns the HTTP handler for the admin API, the embedded UI
// and the relay upgrade path. It serves both the HTTPS listener and the
// local admin socket.
func NewREST(deps RESTDeps) (http.Handler, error) {
	if deps.Status == nil {
		return nil, fmt.Errorf("api: StatusSource required")
	}
	if deps.UI == nil {
		return nil, fmt.Errorf("api: UI filesystem required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		st, err := deps.Status.Status(r.Context())
		if err != nil {
			deps.Logger.Error("status", "err", err)
			writeError(w, http.StatusInternalServerError, "status unavailable")
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "unknown endpoint")
	})
	// TODO(2026-09-02): spec 005 replaces this with the relay upgrade.
	mux.HandleFunc("/relay", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotImplemented, "relay not available yet")
	})
	mux.Handle("/", http.FileServerFS(deps.UI))
	return mux, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
