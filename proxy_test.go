package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleRe(t *testing.T) {
	valid := []string{"bsky.app", "alice.bsky.social", "x.co", "a1-b2.example.com"}
	invalid := []string{"", ".", "-leading.example", "trailing.example-", "spaces in.it", "a/b.c", strings.Repeat("a", 260) + ".com"}
	for _, h := range valid {
		if !handleRe.MatchString(h) {
			t.Errorf("handleRe rejected %q", h)
		}
	}
	for _, h := range invalid {
		if handleRe.MatchString(h) {
			t.Errorf("handleRe accepted %q", h)
		}
	}
}

func TestPostURIRe(t *testing.T) {
	valid := []string{
		"at://did:plc:z72i7hdynmk6r22z27h6tvur/app.bsky.feed.post/3l6oveex3ii2l",
		"at://alice.bsky.social/app.bsky.feed.post/3k",
		"at://did:web:example.com/app.bsky.feed.post/abc.def~ghi",
	}
	invalid := []string{
		"",
		"https://bsky.app/profile/x/post/y",
		"at://did:plc:x/app.bsky.feed.like/3k",
		"at://did:plc:x/app.bsky.feed.post/",
		"at://did:plc:x/app.bsky.feed.post/3k/extra",
		"at://did:plc:x/app.bsky.feed.post/3k?x=1",
	}
	for _, u := range valid {
		if !postURIRe.MatchString(u) {
			t.Errorf("postURIRe rejected %q", u)
		}
	}
	for _, u := range invalid {
		if postURIRe.MatchString(u) {
			t.Errorf("postURIRe accepted %q", u)
		}
	}
}

// newTestServer wires a server against fake upstream and CDN, returning the
// thread-endpoint call counter for cache assertions.
func newTestServer(t *testing.T, actors string) (*server, *atomic.Int32, string) {
	t.Helper()
	var threadCalls atomic.Int32
	var cdnBase string

	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/xrpc/com.atproto.identity.resolveHandle":
			if r.URL.Query().Get("handle") == "missing.example.com" {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"InvalidRequest","message":"Unable to resolve handle"}`)
				return
			}
			fmt.Fprint(w, `{"did":"did:plc:abc"}`)
		case "/xrpc/app.bsky.feed.getPostThread":
			threadCalls.Add(1)
			fmt.Fprintf(w, `{"thread":{"post":{"author":{"handle":"alice.test","avatar":"%s/img/avatar/plain/did:plc:abc/bafkexample"}},"replies":[]}}`, cdnBase)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstreamSrv.Close)

	cdnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/img/avatar/plain/did:plc:abc/bafkexample":
			w.Header().Set("Content-Type", "image/webp")
			w.Write([]byte("img-bytes"))
		case "/img/avatar/plain/did:plc:abc/notimage":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(cdnSrv.Close)
	cdnBase = cdnSrv.URL

	s := &server{
		up:      newUpstream(upstreamSrv.URL),
		cdn:     newUpstream(cdnSrv.URL),
		threads: newCache(45*time.Second, 16),
		avatars: newCache(45*time.Second, 16),
		actors:  splitList(actors),
	}
	return s, &threadCalls, cdnSrv.URL
}

func doGet(s http.HandlerFunc, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestResolveHandle(t *testing.T) {
	s, _, _ := newTestServer(t, "")

	w := doGet(s.resolveHandle, "http://proxy.test/xrpc/com.atproto.identity.resolveHandle?handle=alice.test")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "did:plc:abc") {
		t.Fatalf("body = %q", w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=45" {
		t.Fatalf("Cache-Control = %q", cc)
	}

	w = doGet(s.resolveHandle, "http://proxy.test/xrpc/com.atproto.identity.resolveHandle?handle=..bad..")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid handle: status = %d, want 400", w.Code)
	}

	w = doGet(s.resolveHandle, "http://proxy.test/xrpc/com.atproto.identity.resolveHandle?handle=missing.example.com")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("upstream error: status = %d, want 400 passed through", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("upstream error Cache-Control = %q, want no-store", cc)
	}
	if !strings.Contains(w.Body.String(), "Unable to resolve handle") {
		t.Fatalf("upstream error body not passed through: %q", w.Body.String())
	}
}

func TestGetPostThread(t *testing.T) {
	s, threadCalls, _ := newTestServer(t, "")
	uri := "at://did:plc:abc/app.bsky.feed.post/3k"

	w := doGet(s.getPostThread, "http://proxy.test/xrpc/app.bsky.feed.getPostThread?uri="+uri+"&depth=3")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"avatar":"http://proxy.test/avatar/img/avatar/plain/did:plc:abc/bafkexample"`) {
		t.Fatalf("avatar not rewritten to proxy: %s", w.Body.String())
	}

	doGet(s.getPostThread, "http://proxy.test/xrpc/app.bsky.feed.getPostThread?uri="+uri+"&depth=3")
	if n := threadCalls.Load(); n != 1 {
		t.Fatalf("upstream called %d times for cached thread, want 1", n)
	}

	for _, q := range []string{
		"uri=https://bsky.app/profile/x/post/y",
		"uri=" + uri + "&depth=-1",
		"uri=" + uri + "&depth=1001",
		"uri=" + uri + "&depth=x",
	} {
		w = doGet(s.getPostThread, "http://proxy.test/xrpc/app.bsky.feed.getPostThread?"+q)
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", q, w.Code)
		}
	}
}

func TestActorAllowlist(t *testing.T) {
	s, _, _ := newTestServer(t, "alice.test,did:plc:abc")

	w := doGet(s.getPostThread, "http://proxy.test/xrpc/app.bsky.feed.getPostThread?uri=at://did:plc:abc/app.bsky.feed.post/3k")
	if w.Code != http.StatusOK {
		t.Fatalf("allowlisted DID: status = %d, want 200", w.Code)
	}

	w = doGet(s.getPostThread, "http://proxy.test/xrpc/app.bsky.feed.getPostThread?uri=at://did:plc:other/app.bsky.feed.post/3k")
	if w.Code != http.StatusForbidden {
		t.Fatalf("other DID: status = %d, want 403", w.Code)
	}

	w = doGet(s.resolveHandle, "http://proxy.test/xrpc/com.atproto.identity.resolveHandle?handle=alice.test")
	if w.Code != http.StatusOK {
		t.Fatalf("allowlisted handle: status = %d, want 200", w.Code)
	}

	w = doGet(s.resolveHandle, "http://proxy.test/xrpc/com.atproto.identity.resolveHandle?handle=other.test")
	if w.Code != http.StatusForbidden {
		t.Fatalf("other handle: status = %d, want 403", w.Code)
	}
}

func TestUpstreamBodyCap(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(make([]byte, maxUpstreamBody+1024))
	}))
	t.Cleanup(big.Close)
	body, _, err := newUpstream(big.URL).get("/x", nil, "application/json")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(body) > maxUpstreamBody {
		t.Fatalf("body length %d exceeds cap %d", len(body), maxUpstreamBody)
	}
}

func TestErrorBodiesAreGenericJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSONError(w, http.StatusBadRequest, "InvalidRequest")
	var parsed map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if len(parsed) != 1 || parsed["error"] != "InvalidRequest" {
		t.Fatalf("error body = %v, want only {error: InvalidRequest}", parsed)
	}
}
