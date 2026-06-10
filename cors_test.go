package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func corsGet(t *testing.T, origin, method string) *httptest.ResponseRecorder {
	t.Helper()
	h := cors(splitList("https://blog.example"), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest(method, "http://proxy.test/x", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCORS(t *testing.T) {
	w := corsGet(t, "https://blog.example", http.MethodGet)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://blog.example" {
		t.Errorf("allowed origin: ACAO = %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}

	w = corsGet(t, "https://evil.example", http.MethodGet)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unknown origin must get no ACAO, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary must be set regardless of origin, got %q", got)
	}

	w = corsGet(t, "", http.MethodGet)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("no origin must get no ACAO, got %q", got)
	}

	w = corsGet(t, "https://blog.example", http.MethodOptions)
	if w.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET" {
		t.Errorf("preflight methods = %q, want GET", got)
	}
}
