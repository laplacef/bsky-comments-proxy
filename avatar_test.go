package main

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAvatarPathRe(t *testing.T) {
	valid := []string{
		"img/avatar/plain/did:plc:abc/bafkexample",
		"img/avatar/plain/did:plc:abc/bafkexample@jpeg",
		"img/avatar_thumbnail/plain/did:plc:abc/bafkexample@png",
	}
	invalid := []string{
		"",
		"img/banner/plain/did:plc:abc/bafkexample",
		"img/avatar/plain/../bafkexample",
		"img/avatar/plain/.%2e/bafkexample",
		"img/avatar/plain/did:plc:abc/bafk/extra",
		"img/avatar/plain/did:plc:abc/bafk@jpeg@png",
		"other/avatar/plain/did:plc:abc/bafk",
	}
	for _, p := range valid {
		if !avatarPathRe.MatchString(p) {
			t.Errorf("avatarPathRe rejected %q", p)
		}
	}
	for _, p := range invalid {
		if avatarPathRe.MatchString(p) {
			t.Errorf("avatarPathRe accepted %q", p)
		}
	}
}

func TestAvatarHandler(t *testing.T) {
	s, _, _ := newTestServer(t, "")

	w := doGet(s.avatar, "http://proxy.test/avatar/img/avatar/plain/did:plc:abc/bafkexample")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/webp" {
		t.Fatalf("Content-Type = %q, want image/webp", ct)
	}
	if w.Body.String() != "img-bytes" {
		t.Fatalf("body = %q", w.Body.String())
	}

	w = doGet(s.avatar, "http://proxy.test/avatar/img/banner/plain/did:plc:abc/bafkexample")
	if w.Code != http.StatusNotFound {
		t.Fatalf("banner path: status = %d, want 404", w.Code)
	}

	w = doGet(s.avatar, "http://proxy.test/avatar/img/avatar/plain/did:plc:abc/notimage")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("non-image content: status = %d, want 502", w.Code)
	}

	w = doGet(s.avatar, "http://proxy.test/avatar/img/avatar/plain/did:plc:abc/gone")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing upstream image: status = %d, want 404", w.Code)
	}
	var parsed map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil || parsed["error"] != "UpstreamError" {
		t.Fatalf("CDN error body not generic: %q", w.Body.String())
	}
}

func TestPublicBase(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://proxy.test/x", nil)

	s := &server{publicURL: "https://comments.example.com"}
	if got := s.publicBase(r); got != "https://comments.example.com" {
		t.Errorf("flag override: %q", got)
	}

	s = &server{}
	if got := s.publicBase(r); got != "http://proxy.test" {
		t.Errorf("plain request: %q", got)
	}

	fwd := httptest.NewRequest(http.MethodGet, "http://proxy.test/x", nil)
	fwd.Header.Set("X-Forwarded-Proto", "https, http")
	if got := s.publicBase(fwd); got != "https://proxy.test" {
		t.Errorf("forwarded proto: %q", got)
	}

	tlsReq := httptest.NewRequest(http.MethodGet, "https://proxy.test/x", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	if got := s.publicBase(tlsReq); got != "https://proxy.test" {
		t.Errorf("tls request: %q", got)
	}
}

func TestRewriteAvatars(t *testing.T) {
	s := &server{cdn: newUpstream("https://cdn.example")}
	r := httptest.NewRequest(http.MethodGet, "http://proxy.test/x", nil)

	in := []byte(`{
		"thread": {
			"post": {"author": {"avatar": "https://cdn.example/img/avatar/plain/did:plc:a/b1"}},
			"replies": [
				{"post": {"author": {"avatar": "https://elsewhere.example/img/avatar/plain/did:plc:c/b2"}}},
				{"post": {"author": {"avatar": 42}}}
			]
		}
	}`)
	out := s.rewriteAvatars(r, in)

	var doc struct {
		Thread struct {
			Post struct {
				Author struct {
					Avatar string `json:"avatar"`
				} `json:"author"`
			} `json:"post"`
			Replies []struct {
				Post struct {
					Author struct {
						Avatar any `json:"avatar"`
					} `json:"author"`
				} `json:"post"`
			} `json:"replies"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("rewritten body is not JSON: %v", err)
	}
	if got := doc.Thread.Post.Author.Avatar; got != "http://proxy.test/avatar/img/avatar/plain/did:plc:a/b1" {
		t.Errorf("cdn avatar not rewritten: %q", got)
	}
	if got := doc.Thread.Replies[0].Post.Author.Avatar; got != "https://elsewhere.example/img/avatar/plain/did:plc:c/b2" {
		t.Errorf("non-cdn avatar should be untouched: %v", got)
	}
	if got := doc.Thread.Replies[1].Post.Author.Avatar; got != float64(42) {
		t.Errorf("non-string avatar should be untouched: %v", got)
	}

	if got := s.rewriteAvatars(r, []byte("not json")); string(got) != "not json" {
		t.Errorf("invalid JSON should pass through unchanged: %q", got)
	}
}
