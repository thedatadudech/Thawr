package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

type fakeStatus struct {
	st  Status
	err error
}

func (f fakeStatus) Status(context.Context) (Status, error) { return f.st, f.err }

func newTestHandler(t *testing.T, src StatusSource) http.Handler {
	t.Helper()
	ui := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<h1>Thawr</h1>")}}
	h, err := NewREST(RESTDeps{Status: src, UI: ui})
	if err != nil {
		t.Fatalf("NewREST: %v", err)
	}
	return h
}

func TestStatusEndpoint(t *testing.T) {
	want := Status{Version: "1.2.3", UptimeSeconds: 42, PeerCount: 3, NetmapGeneration: 7, TLSFingerprint: "sha256:ab", HubPublicKey: "k"}
	h := newTestHandler(t, fakeStatus{st: want})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status code %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q", ct)
	}
	var got Status
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStatusEndpointError(t *testing.T) {
	h := newTestHandler(t, fakeStatus{err: errors.New("db down")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/status", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status code %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "db down") {
		t.Error("internal error text leaked to client")
	}
}

func TestStatusMethodNotAllowed(t *testing.T) {
	h := newTestHandler(t, fakeStatus{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/status", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status code %d, want 405", rec.Code)
	}
}

func TestUnknownAPIRoute(t *testing.T) {
	h := newTestHandler(t, fakeStatus{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status code %d, want 404", rec.Code)
	}
}

func TestRelayNotImplemented(t *testing.T) {
	h := newTestHandler(t, fakeStatus{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/relay", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status code %d, want 501", rec.Code)
	}
}

func TestUIServed(t *testing.T) {
	h := newTestHandler(t, fakeStatus{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Thawr") {
		t.Errorf("UI: code %d body %q", rec.Code, rec.Body.String())
	}
}

func TestNewRESTRequiresDeps(t *testing.T) {
	if _, err := NewREST(RESTDeps{}); err == nil {
		t.Error("expected error without deps")
	}
}
